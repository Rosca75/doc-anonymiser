// engine/exportfmt/pdfinplace.go — IN-PLACE PDF text anonymisation.
//
// Why this exists: the OOXML formats (docx/pptx/xlsx) are anonymised by
// splicing the replacement characters into the ORIGINAL file bytes and
// copying every other part untouched, so the exported copy is visually
// identical to the source apart from the replaced words. PDF could not do
// this before: the previous exporter threw the original away and
// regenerated a brand-new document from the extracted plain text (see
// ExportPDF in pdf.go), which is why an anonymised PDF looked nothing like
// its source. This file brings PDF up to the same philosophy: the original
// document is preserved and only the text that must change is edited.
//
// How it works: the PDF text lives inside compressed content streams,
// often encoded through subset CID fonts (Word/Outlook exports) where the
// stream bytes are glyph indices, not letters. Editing those bytes safely
// requires a real PDF engine, so we drive Google's PDFium through
// klippa-app/go-pdfium's WebAssembly backend (pure Go via the wazero
// runtime, no CGo — matching the local-only, CGo-free guarantee in
// CLAUDE.md §4). For each page we:
//
//  1. Read every text object's Unicode text (PDFium decodes the CID/ToUnicode
//     mapping for us).
//  2. Run the SAME span machinery as the body pipeline over that text
//     (Config.AnonymiseText). If nothing changes, the object is left
//     byte-identical.
//  3. Otherwise we REMOVE the original text object (so the source value is
//     truly gone from the file, not merely covered) and add a replacement
//     text object carrying the anonymised string at the same position,
//     scale and colour.
//
// The subset-font glyph problem (a placeholder such as "[PERSON_1]" may need
// glyphs the embedded subset font never contained) is sidestepped exactly
// as the owner approved: the replacement object uses a STANDARD font
// (Helvetica, the metric-compatible core-font equivalent of Arial) which is
// always available, so the placeholder always renders. Only the edited runs
// change font; everything else keeps its original typeface.
//
// PDFium's text-editing methods are part of its "experimental" API, so the
// shipped binary must be built with `-tags pdfium_experimental` (wired into
// wails.json and CI). Without the tag these calls return an error at
// runtime; anonymisePDFInPlace detects any failure and the caller falls
// back to the regenerated-PDF path, so export never breaks.
package exportfmt

import (
	"bytes"
	"fmt"
	"strings"
	"sync"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/structs"
	"github.com/klippa-app/go-pdfium/webassembly"
)

// replacementFont is the standard core font used for every edited run. It
// is one of the 14 fonts every PDF reader guarantees, so its glyphs (the
// bracket, underscore and digits a placeholder needs) always render.
// Helvetica is the metric equivalent of Arial.
const replacementFont = "Helvetica"

// pdfiumPool is the single shared PDFium WebAssembly worker pool. PDFium is
// not internally re-entrant, so the pool serialises access; we keep exactly
// one worker because export is a one-at-a-time user action. It is created
// once, lazily, on first PDF export.
var (
	pdfiumPool pdfium.Pool
	pdfiumErr  error
	pdfiumOnce sync.Once
	// pdfiumMu serialises whole export calls: one document open/edit/save
	// cycle at a time, which the single worker requires anyway.
	pdfiumMu sync.Mutex
)

// pdfiumInstance lazily starts the WebAssembly pool and returns a worker
// instance. Callers MUST Close the returned instance to release the worker.
func pdfiumInstance() (pdfium.Pdfium, error) {
	pdfiumOnce.Do(func() {
		pdfiumPool, pdfiumErr = webassembly.Init(webassembly.Config{
			MinIdle:  1,
			MaxIdle:  1,
			MaxTotal: 1,
		})
	})
	if pdfiumErr != nil {
		return nil, fmt.Errorf("the PDF engine could not start (%v); PDF export is unavailable in this build", pdfiumErr)
	}
	inst, err := pdfiumPool.GetInstance(0)
	if err != nil {
		return nil, fmt.Errorf("the PDF engine is busy or unavailable (%v); try the export again", err)
	}
	return inst, nil
}

// pendingReplacement captures everything needed to rebuild one edited text
// run after the loop that discovered it: the object to delete, its new
// text, and the geometry/colour to reproduce.
type pendingReplacement struct {
	original references.FPDF_PAGEOBJECT
	newText  string
	fontSize float32
	matrix   structs.FPDF_FS_MATRIX
	color    structs.FPDF_COLOR
}

// anonymisePDFInPlace edits the ORIGINAL pdf bytes so that only the text
// requiring anonymisation changes, and returns the new document. Any error
// (including the experimental API being unavailable) is returned so the
// caller can fall back to the regenerated-PDF path; a returned error never
// means a partially-edited file was emitted.
func anonymisePDFInPlace(raw []byte, cfg Config) ([]byte, error) {
	if cfg.Registry == nil {
		return nil, fmt.Errorf("no session registry available for the PDF export")
	}

	pdfiumMu.Lock()
	defer pdfiumMu.Unlock()

	inst, err := pdfiumInstance()
	if err != nil {
		return nil, err
	}
	defer inst.Close()

	doc, err := inst.OpenDocument(&requests.OpenDocument{File: &raw})
	if err != nil {
		return nil, fmt.Errorf("the PDF could not be opened for editing (%v)", err)
	}
	defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})

	pageCount, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return nil, fmt.Errorf("the PDF page count could not be read (%v)", err)
	}

	edits := 0
	for i := 0; i < pageCount.PageCount; i++ {
		n, err := anonymisePDFPage(inst, doc.Document, i, cfg)
		if err != nil {
			return nil, err
		}
		edits += n
	}

	// Save (non-incremental so the removed text objects are truly dropped
	// rather than shadowed by an appended update that still carries the
	// original bytes).
	var out bytes.Buffer
	if _, err := inst.FPDF_SaveAsCopy(&requests.FPDF_SaveAsCopy{
		Document:   doc.Document,
		Flags:      requests.SaveFlagNoIncremental,
		FileWriter: &out,
	}); err != nil {
		return nil, fmt.Errorf("the anonymised PDF could not be written (%v)", err)
	}
	saved := out.Bytes()
	if len(saved) == 0 {
		return nil, fmt.Errorf("the PDF engine produced an empty document")
	}

	// Authoritative leak self-check: re-extract with PDFium (which reads
	// CID fonts reliably, unlike the plain-text importer) and fail if any
	// registry original survived the edit.
	if err := assertNoOriginalsPDFium(inst, saved, cfg); err != nil {
		return nil, err
	}
	return saved, nil
}

// anonymisePDFPage edits one page and returns the number of text objects it
// replaced. It gathers replacements first, then applies them, so removing
// objects never disturbs the iteration.
func anonymisePDFPage(inst pdfium.Pdfium, docRef references.FPDF_DOCUMENT, index int, cfg Config) (int, error) {
	pg, err := inst.FPDF_LoadPage(&requests.FPDF_LoadPage{Document: docRef, Index: index})
	if err != nil {
		return 0, fmt.Errorf("page %d could not be loaded (%v)", index+1, err)
	}
	defer inst.FPDF_ClosePage(&requests.FPDF_ClosePage{Page: pg.Page})
	pageArg := requests.Page{ByReference: &pg.Page}

	tp, err := inst.FPDFText_LoadPage(&requests.FPDFText_LoadPage{Page: pageArg})
	if err != nil {
		return 0, fmt.Errorf("page %d text could not be read (%v)", index+1, err)
	}
	defer inst.FPDFText_ClosePage(&requests.FPDFText_ClosePage{TextPage: tp.TextPage})

	count, err := inst.FPDFPage_CountObjects(&requests.FPDFPage_CountObjects{Page: pageArg})
	if err != nil {
		return 0, fmt.Errorf("page %d objects could not be counted (%v)", index+1, err)
	}

	var pending []pendingReplacement
	for i := 0; i < count.Count; i++ {
		obj, err := inst.FPDFPage_GetObject(&requests.FPDFPage_GetObject{Page: pageArg, Index: i})
		if err != nil {
			return 0, fmt.Errorf("page %d object %d could not be read (%v)", index+1, i, err)
		}
		ty, err := inst.FPDFPageObj_GetType(&requests.FPDFPageObj_GetType{PageObject: obj.PageObject})
		if err != nil {
			return 0, fmt.Errorf("page %d object %d type could not be read (%v)", index+1, i, err)
		}
		if ty.Type != enums.FPDF_PAGEOBJ_TEXT {
			continue // images, paths, shadings: left byte-identical
		}
		got, err := inst.FPDFTextObj_GetText(&requests.FPDFTextObj_GetText{PageObject: obj.PageObject, TextPage: tp.TextPage})
		if err != nil {
			return 0, fmt.Errorf("page %d text object could not be decoded (%v)", index+1, err)
		}
		anon, n := cfg.AnonymiseText(got.Text)
		if n == 0 || anon == got.Text {
			continue // nothing sensitive in this run
		}

		size, err := inst.FPDFTextObj_GetFontSize(&requests.FPDFTextObj_GetFontSize{PageObject: obj.PageObject})
		if err != nil {
			return 0, fmt.Errorf("page %d font size could not be read (%v)", index+1, err)
		}
		mtx, err := inst.FPDFPageObj_GetMatrix(&requests.FPDFPageObj_GetMatrix{PageObject: obj.PageObject})
		if err != nil {
			return 0, fmt.Errorf("page %d text position could not be read (%v)", index+1, err)
		}
		// Colour is best-effort: default to opaque black if unreadable.
		color := structs.FPDF_COLOR{R: 0, G: 0, B: 0, A: 255}
		if col, err := inst.FPDFPageObj_GetFillColor(&requests.FPDFPageObj_GetFillColor{PageObject: obj.PageObject}); err == nil {
			color = col.FillColor
		}
		pending = append(pending, pendingReplacement{
			original: obj.PageObject,
			newText:  anon,
			fontSize: size.FontSize,
			matrix:   mtx.Matrix,
			color:    color,
		})
	}

	for _, p := range pending {
		if err := applyPDFReplacement(inst, docRef, pageArg, p); err != nil {
			return 0, fmt.Errorf("page %d could not be edited (%v)", index+1, err)
		}
	}

	if len(pending) > 0 {
		if _, err := inst.FPDFPage_GenerateContent(&requests.FPDFPage_GenerateContent{Page: pageArg}); err != nil {
			return 0, fmt.Errorf("page %d changes could not be finalised (%v)", index+1, err)
		}
	}
	return len(pending), nil
}

// applyPDFReplacement removes one original text object and inserts its
// anonymised replacement in the standard font at the same geometry.
func applyPDFReplacement(inst pdfium.Pdfium, docRef references.FPDF_DOCUMENT, pageArg requests.Page, p pendingReplacement) error {
	newObj, err := inst.FPDFPageObj_NewTextObj(&requests.FPDFPageObj_NewTextObj{
		Document: docRef,
		Font:     replacementFont,
		FontSize: p.fontSize,
	})
	if err != nil {
		return fmt.Errorf("replacement text could not be created (%v)", err)
	}
	if _, err := inst.FPDFText_SetText(&requests.FPDFText_SetText{PageObject: newObj.PageObject, Text: p.newText}); err != nil {
		return fmt.Errorf("replacement text could not be set (%v)", err)
	}
	// Reproduce the original placement/scale/rotation and colour.
	if _, err := inst.FPDFPageObj_SetMatrix(&requests.FPDFPageObj_SetMatrix{PageObject: newObj.PageObject, Transform: p.matrix}); err != nil {
		return fmt.Errorf("replacement text could not be positioned (%v)", err)
	}
	if _, err := inst.FPDFPageObj_SetFillColor(&requests.FPDFPageObj_SetFillColor{PageObject: newObj.PageObject, FillColor: p.color}); err != nil {
		return fmt.Errorf("replacement text colour could not be set (%v)", err)
	}
	if _, err := inst.FPDFPage_InsertObject(&requests.FPDFPage_InsertObject{Page: pageArg, PageObject: newObj.PageObject}); err != nil {
		return fmt.Errorf("replacement text could not be inserted (%v)", err)
	}
	// Remove the original run and free it: the source value leaves the file.
	if _, err := inst.FPDFPage_RemoveObject(&requests.FPDFPage_RemoveObject{Page: pageArg, PageObject: p.original}); err != nil {
		return fmt.Errorf("original text could not be removed (%v)", err)
	}
	if _, err := inst.FPDFPageObj_Destroy(&requests.FPDFPageObj_Destroy{PageObject: p.original}); err != nil {
		return fmt.Errorf("original text could not be released (%v)", err)
	}
	return nil
}

// assertNoOriginalsPDFium re-opens the produced PDF with PDFium and fails if
// any non-allowlisted registry original still reads back, mirroring the
// regenerated-PDF path's guarantee but with a CID-aware reader.
func assertNoOriginalsPDFium(inst pdfium.Pdfium, pdfBytes []byte, cfg Config) error {
	text, err := extractAllTextPDFium(inst, pdfBytes)
	if err != nil {
		return fmt.Errorf("the anonymised PDF failed its self-check read (%v); nothing was exported", err)
	}
	lower := strings.ToLower(text)
	collapsed := strings.Join(strings.Fields(lower), " ")
	for _, e := range cfg.Registry.Entries() {
		if allowlisted(cfg.Allowlist, e.Original) {
			continue
		}
		needle := strings.ToLower(e.Original)
		if strings.Contains(lower, needle) || strings.Contains(collapsed, strings.Join(strings.Fields(needle), " ")) {
			return fmt.Errorf(
				"self-check failed: the anonymised PDF still contains %q; the file was NOT exported. Re-run the pipeline and try again, or export as .md instead",
				e.Placeholder+" = "+redactTerm(e.Original))
		}
	}
	return nil
}

// extractAllTextPDFium reads every page's text from an in-memory PDF.
func extractAllTextPDFium(inst pdfium.Pdfium, pdfBytes []byte) (string, error) {
	doc, err := inst.OpenDocument(&requests.OpenDocument{File: &pdfBytes})
	if err != nil {
		return "", err
	}
	defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})
	pageCount, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i := 0; i < pageCount.PageCount; i++ {
		pageText, err := inst.GetPageText(&requests.GetPageText{Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: i}}})
		if err != nil {
			return "", err
		}
		b.WriteString(pageText.Text)
		b.WriteString("\n")
	}
	return b.String(), nil
}

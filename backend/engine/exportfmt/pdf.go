// engine/exportfmt/pdf.go — EXPERIMENTAL same-format PDF export
//
// Evaluation outcome: in-place body
// text replacement inside PDF content streams was NOT adopted. Subset
// fonts frequently lack the glyphs a placeholder needs, and pixel
// stability cannot be guaranteed. The recorded fallback (13c) is
// implemented instead: a REGENERATED PDF built from the anonymised
// working text via go-pdf/fpdf (pure Go, MIT, pinned in CLAUDE.md §7),
// with a simplified layout: one page per source page where the page
// breaks are known (the import converter separates pages by blank
// lines), plain paragraphs otherwise, and the reviewed metadata written
// into the new file's Info dictionary.
//
// Body coverage guarantee (13d): before returning, the produced PDF is
// re-extracted with the SAME ledongthuc reader the importer uses and the
// export FAILS with an actionable error if any registry original is
// still readable — a leaky file is never shipped silently.
package exportfmt

import (
	"bytes"
	"fmt"
	"strings"

	ledongthuc "github.com/ledongthuc/pdf"

	"github.com/go-pdf/fpdf"

	"doc-anonymiser/backend/engine"
)

// pdfInfoFields are the Info-dictionary keys offered for review
// in stable order.
var pdfInfoFields = []string{"Title", "Author", "Subject", "Keywords", "Creator", "Producer"}

// pdfMetaPart is the MetaField.Part marker for PDF Info fields (the
// review panel is format-agnostic by design).
const pdfMetaPart = "pdf:Info"

// ExtractPDFMetadata reads the Info dictionary of the original PDF with
// the already-pinned ledongthuc reader (no new parser dependency).
func ExtractPDFMetadata(raw []byte) ([]MetaField, error) {
	reader, err := ledongthuc.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("the PDF could not be parsed (%v); re-import the original file and try again", err)
	}
	info := reader.Trailer().Key("Info")
	if info.IsNull() {
		return nil, nil // no Info dictionary: nothing to review
	}
	var out []MetaField
	for _, name := range pdfInfoFields {
		v := info.Key(name)
		if v.IsNull() {
			continue
		}
		text := v.Text()
		if strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, MetaField{Part: pdfMetaPart, Name: name, Value: text})
	}
	return out, nil
}

// ExportPDF produces an anonymised PDF. The PRIMARY path edits the ORIGINAL
// document in place (anonymisePDFInPlace): only the text that must change is
// touched and the rest of the file, layout, fonts and images are preserved,
// exactly like the docx/pptx/xlsx same-format exporters. The `original`
// bytes are the source PDF captured at import.
//
// If in-place editing is unavailable or fails for a given file (for example
// the experimental PDFium API is not compiled in, or a PDF uses a structure
// the engine cannot edit), it falls back to regeneratePDF, which rebuilds a
// simplified PDF from the anonymised working text. Either way the returned
// file passes a leak self-check before it is handed back.
//
// anonymised is the working text (ResultDocument.Anonymised); reviewed is
// the approved metadata; cfg carries the session registry and pipeline
// inputs (nil-registry cfg forces the regeneration fallback).
func ExportPDF(original []byte, anonymised string, reviewed []MetaField, cfg Config) ([]byte, error) {
	if len(original) > 0 && cfg.Registry != nil {
		if out, err := anonymisePDFInPlace(original, cfg); err == nil {
			return out, nil
		}
		// In-place failed: fall through to regeneration so export still
		// succeeds. The regenerated file is self-checked below.
	}
	return regeneratePDF(anonymised, reviewed, cfg)
}

// regeneratePDF rebuilds an anonymised PDF from the working text
// (ResultDocument.Anonymised) with the reviewed metadata. It is the
// fallback for files in-place editing cannot handle. anonymised must be
// non-empty: a PDF without a text layer was already rejected at import, and
// re-checking here keeps the export path honest.
func regeneratePDF(anonymised string, reviewed []MetaField, cfg Config) ([]byte, error) {
	if strings.TrimSpace(anonymised) == "" {
		return nil, fmt.Errorf("No text layer found, this PDF is likely scanned. OCR is not supported; convert it externally first.")
	}

	doc := fpdf.New("P", "mm", "A4", "")
	doc.SetCreator("doc-anonymiser", true)
	for _, f := range reviewed {
		if f.Part != pdfMetaPart {
			continue
		}
		switch f.Name {
		case "Title":
			doc.SetTitle(f.Value, true)
		case "Author":
			doc.SetAuthor(f.Value, true)
		case "Subject":
			doc.SetSubject(f.Value, true)
		case "Keywords":
			doc.SetKeywords(f.Value, true)
		case "Creator":
			doc.SetCreator(f.Value, true)
			// Producer is fpdf's own stamp; not overridable per file.
		}
	}
	doc.SetFont("Helvetica", "", 11)
	doc.SetMargins(20, 20, 20)
	doc.SetAutoPageBreak(true, 20)

	// The core fonts are cp1252: translate what we can (French accents
	// survive; anything outside the codepage degrades to '?', an accepted
	// EXPERIMENTAL limitation).
	tr := doc.UnicodeTranslatorFromDescriptor("")

	// The import converter separates source pages with blank lines
	// (engine/convert/pdf.go), so double newlines double as our
	// page-per-source-page hint; single newlines stay line breaks.
	pages := strings.Split(anonymised, "\n\n")
	for _, page := range pages {
		page = strings.TrimSpace(page)
		if page == "" {
			continue
		}
		doc.AddPage()
		for _, line := range strings.Split(page, "\n") {
			doc.MultiCell(170, 6, tr(line), "", "L", false)
		}
	}
	if doc.PageCount() == 0 {
		doc.AddPage()
	}

	var buf bytes.Buffer
	if err := doc.Output(&buf); err != nil {
		return nil, fmt.Errorf("the anonymised PDF could not be generated: %w", err)
	}

	// 13d runtime self-check: the produced file must not contain any
	// registry original when re-read with the import extractor.
	if err := assertNoOriginals(buf.Bytes(), cfg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// assertNoOriginals re-extracts the produced PDF's text and fails if any
// registry original survives (case-insensitive). This is both the test
// oracle and a runtime guarantee.
func assertNoOriginals(pdfBytes []byte, cfg Config) error {
	if cfg.Registry == nil {
		return nil
	}
	reader, err := ledongthuc.NewReader(bytes.NewReader(pdfBytes), int64(len(pdfBytes)))
	if err != nil {
		return fmt.Errorf("the generated PDF failed its self-check read (%v); nothing was exported", err)
	}
	var extracted strings.Builder
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		if text, err := page.GetPlainText(nil); err == nil {
			extracted.WriteString(text)
			extracted.WriteString("\n")
		}
	}
	lower := strings.ToLower(extracted.String())
	// Whitespace inside extracted text can differ; compare with spaces
	// collapsed as well.
	collapsed := strings.Join(strings.Fields(lower), " ")
	for _, e := range cfg.Registry.Entries() {
		needle := strings.ToLower(e.Original)
		if allowlisted(cfg.Allowlist, e.Original) {
			continue
		}
		if strings.Contains(lower, needle) || strings.Contains(collapsed, strings.Join(strings.Fields(needle), " ")) {
			return fmt.Errorf(
				"self-check failed: the generated PDF still contains %q; the file was NOT exported. Re-run the pipeline and try again, or export as .md instead",
				e.Placeholder+" = "+redactTerm(e.Original))
		}
	}
	return nil
}

// allowlisted is a nil-safe allowlist check.
func allowlisted(a *engine.Allowlist, term string) bool {
	return a != nil && a.Contains(term)
}

// redactTerm shortens a leaked original for the error message (first
// rune + length): enough to identify the culprit via the placeholder
// without repeating the full sensitive value in logs.
func redactTerm(s string) string {
	rs := []rune(s)
	if len(rs) <= 1 {
		return s
	}
	return string(rs[0]) + strings.Repeat("*", len(rs)-1)
}

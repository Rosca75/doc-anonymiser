// engine/exportfmt/pdfinplace.go — the in-place PDF export: the produced file
// is the ORIGINAL's bytes with the pipeline's replacements applied, never a
// regenerated layout.
//
// The binding is string-driven: the same Config the OOXML same-format export
// uses computes the replacements over each page's derived text (the exact
// text the import handed the pipeline, re-derived from the same line model),
// and the location ladder (pdfladder.go) finds each string on its page. Rung
// 1 replaces in place through the library; every lower rung redacts, with the
// placeholder drawn as the redaction's own overlay text in explicit white,
// because the apply path draws overlays black otherwise and black-on-black is
// extractable text nobody can see.
//
// Three disciplines hold the leak-critical path:
//
//   - An occurrence the whole ladder cannot locate REFUSES the export before
//     anything is written: a half-anonymised PDF that looks finished is worse
//     than a refusal, and the .md export is always the way out.
//   - The save is RemoveUnusedObjects() then WriteTo, never WriteTo alone:
//     the library serialises every object in its table, including one an edit
//     orphaned, so a naked save keeps the pre-edit content stream readable.
//   - The whole-file leak scan (pdfscan.go) runs over the produced bytes as a
//     BLOCKING self-check: a file that still carries a registry original is
//     never handed back, and the failure names the surface it leaked in.
//
// Non-content surfaces follow the same contract as the OOXML exports:
// annotation contents, outline titles, the Info dictionary and the XMP packet
// are rewritten through the pipeline's own span machinery (plus the user's
// metadata review), and embedded file attachments and JavaScript actions are
// DROPPED from the produced copy and reported, never carried silently: an
// attachment is an inner document the pipeline never read.
package exportfmt

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	asposepdf "github.com/aspose-pdf-foss/aspose-pdf-foss-for-go"

	"doc-anonymiser/backend/engine/convert"
)

// pdfXMPMetaPart marks XMP-packet fields in the metadata review, beside
// pdfMetaPart's Info fields, so the review panel (format-agnostic by design)
// shows both stores of the same document.
const pdfXMPMetaPart = "pdf:XMP"

// ExtractPDFMetadata reads the metadata review's fields for a PDF: the Info
// dictionary AND the XMP packet, because both stores travel inside the file
// and a document whose body is anonymised while its XMP still names the
// author is not anonymised at all.
func ExtractPDFMetadata(raw []byte) (fields []MetaField, err error) {
	defer func() {
		if r := recover(); r != nil {
			fields = nil
			err = fmt.Errorf("the PDF could not be parsed (internal reader error: %v); re-import the original file and try again", r)
		}
	}()
	doc, err := asposepdf.OpenStream(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("the PDF could not be parsed (%v); re-import the original file and try again", err)
	}

	info, err := doc.Info()
	if err == nil {
		for _, f := range []struct{ name, value string }{
			{"Title", info.Title},
			{"Author", info.Author},
			{"Subject", info.Subject},
			{"Keywords", info.Keywords},
			{"Creator", info.Creator},
			{"Producer", info.Producer},
		} {
			value := decodePDFInfoText(f.value)
			if strings.TrimSpace(value) == "" {
				continue
			}
			fields = append(fields, MetaField{Part: pdfMetaPart, Name: f.name, Value: value})
		}
	}

	// The XMP fields the export can write back field by field. A packet that
	// does not parse is absent from the review; the export drops it rather
	// than carrying it through unchecked, and reports the drop.
	if xmp, xerr := doc.XMP(); xerr == nil && !xmp.IsEmpty() {
		for _, f := range []struct{ name, value string }{
			{"Title", xmp.Title},
			{"Author", strings.Join(xmp.Authors, "; ")},
			{"Description", xmp.Description},
			{"CreatorTool", xmp.CreatorTool},
		} {
			if strings.TrimSpace(f.value) == "" {
				continue
			}
			fields = append(fields, MetaField{Part: pdfXMPMetaPart, Name: f.name, Value: f.value})
		}
	}
	return fields, nil
}

// PDFTextPlan is the location ladder's dry-run answer for one document: what
// an in-place export WOULD do, per rung, and what it could not locate. The
// export review panel shows it before the file is written, because "12
// replaced in line, 3 across fragments" is a fact the user should read before
// opening the produced file, and a refusal should never be a surprise.
type PDFTextPlan struct {
	Counts    PDFRungCounts  `json:"counts"`
	Unlocated []PDFUnlocated `json:"unlocated,omitempty"`
}

// PDFExportResult is one successful in-place export: the produced bytes plus
// everything the caller reports about them.
type PDFExportResult struct {
	Data []byte
	// Counts is the ladder tally of the body-text replacements.
	Counts PDFRungCounts
	// Extras counts replacements made in surfaces the preview does not show
	// (annotation contents, outline titles), the docx document_extras shape.
	Extras int
	// Dropped lists what the produced copy no longer carries (attachments,
	// JavaScript, an unparseable XMP packet), one sentence each, reported and
	// never silent.
	Dropped []string
	// Unscannable names produced-file streams the leak scan could only read
	// as raw bytes (image codecs), so the caller can state the honest limit.
	Unscannable []string
}

// livePDFSearcher is pdfSearcher over a real page.
type livePDFSearcher struct{ page *asposepdf.Page }

func (s livePDFSearcher) search(query string, regex bool) []asposepdf.TextMatch {
	matches, err := s.page.SearchText(query, asposepdf.SearchOptions{Regex: regex})
	if err != nil {
		// An unsearchable query is a miss, not a failure: the lower rungs
		// exist for exactly this.
		return nil
	}
	return matches
}

// pdfPageWork is the located work for one page: the strings to replace in
// place and the rectangles to redact.
type pdfPageWork struct {
	replace []pdfReplace
	redacts []pdfRedact
}

// pdfReplace is one rung-1 string replacement on a page.
type pdfReplace struct {
	original, placeholder string
}

// pdfRedact is one redaction rectangle; Overlay is empty for a wrapped tail's
// plain box (one placeholder per value, drawn over the head).
type pdfRedact struct {
	rect    asposepdf.Rectangle
	overlay string
}

// openPDFForExport opens the original bytes and derives the per-page layouts,
// with the same recover shield and error classification the import uses.
func openPDFForExport(raw []byte) (doc *asposepdf.Document, layouts []convert.PDFPageLayout, err error) {
	defer func() {
		if r := recover(); r != nil {
			doc, layouts = nil, nil
			err = fmt.Errorf("the PDF could not be parsed for export (internal reader error: %v); re-import the original file and try again", r)
		}
	}()
	doc, err = asposepdf.OpenStream(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, fmt.Errorf("the original PDF could not be opened for export (%v); re-import the original file and try again", err)
	}
	for _, page := range doc.Pages() {
		libLines, err := page.ExtractTextWithLayout()
		if err != nil {
			return nil, nil, fmt.Errorf("page %d of the original PDF could not be read for export (%v); re-import the original file and try again", page.Number(), err)
		}
		layouts = append(layouts, convert.LayoutFromTextLines(libLines))
	}
	return doc, layouts, nil
}

// locatePDFWork runs the pipeline's replacement decisions and the ladder over
// every page, returning the per-page work, the rung tally and the occurrences
// nothing could locate. Shared by the dry-run plan and the real export so the
// panel can never promise what the export will not do.
func locatePDFWork(doc *asposepdf.Document, layouts []convert.PDFPageLayout, cfg Config) (work []pdfPageWork, counts PDFRungCounts, unlocated []PDFUnlocated) {
	pages := doc.Pages()
	work = make([]pdfPageWork, len(pages))
	for pi, page := range pages {
		if pi >= len(layouts) {
			break
		}
		layout := layouts[pi]
		// The page text the pipeline saw: the SAME derivation and the SAME
		// repair the import applies, so the strings the spans carry are the
		// strings detection matched.
		pageText := convert.RepairPDFText(convert.PDFPageText(layout))

		// One ladder decision per DISTINCT string on the page: the binding is
		// string-driven, and every occurrence of one string on one page gets
		// one treatment. Placeholders are stable, so repeated spans agree.
		type job struct{ original, placeholder string }
		seen := map[string]bool{}
		var jobs []job
		for _, r := range cfg.Replacements(pageText) {
			original := pageText[r.Start:r.End]
			if seen[original] {
				continue
			}
			seen[original] = true
			jobs = append(jobs, job{original: original, placeholder: r.Text})
		}
		// Longer strings first: a value containing another value must be
		// located and redacted before the shorter claim can shadow it.
		sort.SliceStable(jobs, func(i, j int) bool { return len(jobs[i].original) > len(jobs[j].original) })

		searcher := livePDFSearcher{page: page}
		for _, j := range jobs {
			located := locatePDFValue(j.original, j.placeholder, searcher, layout)
			switch located.rung {
			case rungLiteral:
				counts.Literal += len(located.occurrences)
				work[pi].replace = append(work[pi].replace, pdfReplace(j))
			case rungTolerant:
				counts.Tolerant += len(located.occurrences)
			case rungFragment:
				counts.Fragment += len(located.occurrences)
			case rungWrapped:
				counts.Wrapped += len(located.occurrences)
			case rungUnlocated:
				unlocated = append(unlocated, PDFUnlocated{Placeholder: j.placeholder, Page: pi + 1})
				continue
			}
			if !located.replaceInPlace {
				for _, rects := range located.occurrences {
					for ri, rect := range rects {
						overlay := ""
						if ri == 0 {
							overlay = j.placeholder
						}
						work[pi].redacts = append(work[pi].redacts, pdfRedact{rect: rect, overlay: overlay})
					}
				}
			}
		}
	}
	return work, counts, unlocated
}

// PlanPDFText is the dry run behind the export review panel: the same open,
// the same layouts, the same ladder, and NOTHING written.
func PlanPDFText(raw []byte, cfg Config) (*PDFTextPlan, error) {
	doc, layouts, err := openPDFForExport(raw)
	if err != nil {
		return nil, err
	}
	_, counts, unlocated := locatePDFWork(doc, layouts, cfg)
	return &PDFTextPlan{Counts: counts, Unlocated: unlocated}, nil
}

// pdfRefusal is D6's refusal: an occurrence the whole ladder cannot locate
// blocks the export BEFORE anything is produced, naming each placeholder and
// page, and the .md export as the way out. The original values never appear
// in the message: the placeholder is the name a refusal may speak.
func pdfRefusal(unlocated []PDFUnlocated) error {
	parts := make([]string, 0, len(unlocated))
	for _, u := range unlocated {
		parts = append(parts, fmt.Sprintf("%s on page %d", u.Placeholder, u.Page))
	}
	return fmt.Errorf(
		"the PDF was NOT exported: %d replacement(s) could not be located in the original file's layout (%s). "+
			"An in-place export that misses a replacement would look finished while still carrying the original text. "+
			"Export this document as .md instead, which always contains the fully anonymised text",
		len(unlocated), strings.Join(parts, ", "))
}

// ExportPDFInPlace produces the anonymised same-format PDF copy from the
// original bytes. reviewed carries the metadata review's decisions (Info and
// XMP fields); cfg is the last run's inputs plus the session registry,
// exactly as the OOXML exports take them.
func ExportPDFInPlace(raw []byte, reviewed []MetaField, cfg Config) (*PDFExportResult, error) {
	doc, layouts, err := openPDFForExport(raw)
	if err != nil {
		return nil, err
	}

	work, counts, unlocated := locatePDFWork(doc, layouts, cfg)
	if len(unlocated) > 0 {
		return nil, pdfRefusal(unlocated)
	}

	result := &PDFExportResult{Counts: counts}
	pages := doc.Pages()

	// The body pass: rung-1 replacements first, then every redaction, applied
	// once for the whole document. ReplaceText redraws in a metric-compatible
	// face at the same baseline and size; ApplyRedactions removes the covered
	// glyphs and draws each overlay, so after this block the original strings
	// have LEFT the content streams rather than being covered.
	for pi, page := range pages {
		if pi >= len(work) {
			break
		}
		for _, r := range work[pi].replace {
			if _, err := page.ReplaceText(r.original, r.placeholder); err != nil {
				return nil, fmt.Errorf("replacing text on page %d failed (%v); export this document as .md instead", pi+1, err)
			}
		}
		for _, rd := range work[pi].redacts {
			redact := asposepdf.NewRedactAnnotation(page, rd.rect)
			if rd.overlay != "" {
				redact.SetOverlayText(rd.overlay)
				// The overlay colour is EXPLICIT white: the apply path draws
				// it black otherwise, and black on the black box is
				// extractable text nobody can see.
				redact.SetOverlayTextStyle(asposepdf.TextStyle{
					Size:  pdfOverlaySize(rd.rect),
					Color: &asposepdf.Color{R: 1, G: 1, B: 1, A: 1},
				})
			}
			// A redact annotation is built UNBOUND: constructing it does
			// nothing until it is added to its page's collection, so the
			// construct and the Add are one gesture.
			if err := page.Annotations().Add(redact); err != nil {
				return nil, fmt.Errorf("adding a redaction on page %d failed (%v); export this document as .md instead", pi+1, err)
			}
		}
	}
	if err := doc.ApplyRedactions(); err != nil {
		return nil, fmt.Errorf("applying the redactions failed (%v); export this document as .md instead", err)
	}

	// The non-content surfaces, after the body pass so the annotation walk
	// sees only the document's own annotations (the redact annotations are
	// consumed by ApplyRedactions above).
	result.Extras += scrubPDFAnnotations(pages, cfg)
	result.Extras += scrubPDFOutlines(doc.Outlines(), cfg)
	applyPDFInfo(doc, reviewed, cfg)
	if dropped := applyPDFXMP(doc, reviewed, cfg); dropped != "" {
		result.Dropped = append(result.Dropped, dropped)
	}
	if n := doc.EmbeddedFiles().Count(); n > 0 {
		doc.EmbeddedFiles().Clear()
		result.Dropped = append(result.Dropped, fmt.Sprintf(
			"%d embedded file attachment(s) were removed from the anonymised copy: an attachment is a separate document this application never read, so it cannot be carried through as anonymised", n))
	}
	if n := doc.JavaScript().Count(); n > 0 {
		doc.JavaScript().Clear()
		result.Dropped = append(result.Dropped, fmt.Sprintf(
			"%d JavaScript action(s) were removed from the anonymised copy: script text is not document text the pipeline reads, so it cannot be carried through as anonymised", n))
	}

	// The save discipline: RemoveUnusedObjects() before EVERY WriteTo. The
	// library serialises every object in its table, including one an edit
	// orphaned, so a naked WriteTo keeps the pre-edit content stream readable
	// in the produced file.
	doc.RemoveUnusedObjects()
	var buf bytes.Buffer
	if _, err := doc.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("the anonymised PDF could not be written (%v); export this document as .md instead", err)
	}
	result.Data = buf.Bytes()

	// The whole-file leak self-check, BLOCKING: a produced file that still
	// carries a registry original is never handed back.
	findings, unscannable, err := ScanPDFForNeedles(result.Data, pdfLeakNeedles(cfg))
	if err != nil {
		return nil, fmt.Errorf("the produced PDF failed its leak self-check read (%v); nothing was exported", err)
	}
	result.Unscannable = unscannable
	if len(findings) > 0 {
		f := findings[0]
		return nil, fmt.Errorf(
			"self-check failed: the produced PDF still contains %s in %s (object %d); the file was NOT exported. Export this document as .md instead",
			pdfLeakLabel(cfg, f.Needle), f.Surface, f.Object)
	}
	return result, nil
}

// pdfOverlaySize fits the overlay placeholder to its redaction box: most of
// the box's height, clamped so a tall box does not shout and a thin one stays
// legible on the rasterised page.
func pdfOverlaySize(r asposepdf.Rectangle) float64 {
	size := (r.URY - r.LLY) * 0.75
	if size < 4 {
		size = 4
	}
	if size > 12 {
		size = 12
	}
	return size
}

// scrubPDFAnnotations rewrites every annotation's contents through the same
// span machinery as body text, returning the replacement count (the
// document_extras shape: hits in surfaces the preview does not show).
func scrubPDFAnnotations(pages []*asposepdf.Page, cfg Config) int {
	extras := 0
	for _, page := range pages {
		coll := page.Annotations()
		for i := 0; i < coll.Count(); i++ {
			a := coll.At(i)
			if a == nil {
				continue
			}
			text := a.Contents()
			if text == "" {
				continue
			}
			if anon, n := cfg.AnonymiseText(text); n > 0 {
				a.SetContents(anon)
				extras += n
			}
		}
	}
	return extras
}

// scrubPDFOutlines rewrites every outline title, recursively.
func scrubPDFOutlines(o *asposepdf.OutlineItemCollection, cfg Config) int {
	if o == nil {
		return 0
	}
	extras := 0
	if title := o.Title(); title != "" {
		if anon, n := cfg.AnonymiseText(title); n > 0 {
			o.SetTitle(anon)
			extras += n
		}
	}
	for _, child := range o.All() {
		extras += scrubPDFOutlines(child, cfg)
	}
	return extras
}

// applyPDFInfo writes the Info dictionary of the PRODUCED file: every field
// scrubbed through the pipeline first (so an unreviewed field cannot carry an
// original), then the review's decisions applied on top, because a reviewed
// value is the user's word over the proposal.
func applyPDFInfo(doc *asposepdf.Document, reviewed []MetaField, cfg Config) {
	info, err := doc.Info()
	if err != nil {
		// An unreadable Info dictionary carries nothing the scan will not
		// catch; leave it for the leak check to judge.
		return
	}
	scrub := func(s string) string { out, _ := cfg.AnonymiseText(decodePDFInfoText(s)); return out }
	info.Title = scrub(info.Title)
	info.Author = scrub(info.Author)
	info.Subject = scrub(info.Subject)
	info.Keywords = scrub(info.Keywords)
	info.Creator = scrub(info.Creator)
	info.Producer = scrub(info.Producer)
	for k, v := range info.Custom {
		info.Custom[k] = scrub(v)
	}
	for _, f := range reviewed {
		if f.Part != pdfMetaPart {
			continue
		}
		switch f.Name {
		case "Title":
			info.Title = f.Value
		case "Author":
			info.Author = f.Value
		case "Subject":
			info.Subject = f.Value
		case "Keywords":
			info.Keywords = f.Value
		case "Creator":
			info.Creator = f.Value
		case "Producer":
			info.Producer = f.Value
		}
	}
	doc.SetInfo(info)
}

// applyPDFXMP rewrites the XMP packet field by field through the same
// machinery, honouring the review where it spoke. A packet the library cannot
// parse is DROPPED rather than copied through, and the drop is reported: a
// packet nobody could scrub is a surface nobody checked.
func applyPDFXMP(doc *asposepdf.Document, reviewed []MetaField, cfg Config) (dropped string) {
	xmp, err := doc.XMP()
	if err != nil {
		doc.ClearXMP()
		return "the XMP metadata packet could not be parsed field by field, so it was removed from the anonymised copy rather than carried through unchecked"
	}
	if xmp.IsEmpty() {
		return ""
	}
	scrub := func(s string) string { out, _ := cfg.AnonymiseText(s); return out }
	xmp.Title = scrub(xmp.Title)
	for i := range xmp.Authors {
		xmp.Authors[i] = scrub(xmp.Authors[i])
	}
	xmp.Description = scrub(xmp.Description)
	for i := range xmp.Keywords {
		xmp.Keywords[i] = scrub(xmp.Keywords[i])
	}
	xmp.CreatorTool = scrub(xmp.CreatorTool)
	for i := range xmp.Custom {
		xmp.Custom[i].Value = scrub(xmp.Custom[i].Value)
	}
	for _, f := range reviewed {
		if f.Part != pdfXMPMetaPart {
			continue
		}
		switch f.Name {
		case "Title":
			xmp.Title = f.Value
		case "Author":
			xmp.Authors = []string{f.Value}
		case "Description":
			xmp.Description = f.Value
		case "CreatorTool":
			xmp.CreatorTool = f.Value
		}
	}
	if err := doc.SetXMP(xmp); err != nil {
		doc.ClearXMP()
		return "the XMP metadata packet could not be rewritten, so it was removed from the anonymised copy rather than carried through unchecked"
	}
	return ""
}

// decodePDFInfoText turns an Info-dictionary string, which the library hands
// back as the file's raw string bytes, into readable text: a UTF-16BE value
// (the encoding most writers use for Info fields) is transcoded, anything
// else passes through. Without this a reviewed field shows byte salad and the
// scrub cannot match a name stored two bytes per character.
func decodePDFInfoText(s string) string {
	return decodePDFTextEncoding([]byte(s))
}

// pdfLeakNeedles is the leak scan's needle list: every registry original that
// is not allowlisted, the same set the OOXML archive check proves absent.
func pdfLeakNeedles(cfg Config) []string {
	if cfg.Registry == nil {
		return nil
	}
	var needles []string
	for _, e := range cfg.Registry.Entries() {
		if allowlisted(cfg.Allowlist, e.Original) {
			continue
		}
		needles = append(needles, e.Original)
	}
	return needles
}

// pdfLeakLabel names a leaked needle in a message without repeating it: the
// placeholder plus the redacted form.
func pdfLeakLabel(cfg Config, needle string) string {
	if cfg.Registry != nil {
		for _, e := range cfg.Registry.Entries() {
			if e.Original == needle {
				return e.Placeholder + " = " + redactTerm(needle)
			}
		}
	}
	return redactTerm(needle)
}

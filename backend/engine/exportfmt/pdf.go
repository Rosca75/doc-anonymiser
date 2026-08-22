// engine/exportfmt/pdf.go — the REGENERATED PDF export, retained without a
// production caller.
//
// The production PDF export is the in-place replacement (pdfinplace.go): the
// original file's bytes with the pipeline's replacements applied. This file
// is the previous mechanism, a new simplified PDF built from the anonymised
// working text via go-pdf/fpdf, and it stays compiled, tested and unwired
// until the owner explicitly confirms the in-place path's tests are
// successful against the tagged pre-change release (the decommissioning gate
// in CLAUDE.md §7's pin rows); it is then deleted together with its
// dependency, never re-wired as a fallback: the refusal names the .md export
// as the way out, and a second PDF writer kept for a rare failure path would
// be unreviewed code in the leak-critical path.
//
// Body coverage guarantee: before returning, the produced PDF is re-extracted
// with the ledongthuc reader and the export FAILS with an actionable error if
// any registry original is still readable — a leaky file is never shipped
// silently.
package exportfmt

import (
	"bytes"
	"fmt"
	"strings"

	ledongthuc "github.com/ledongthuc/pdf"

	"github.com/go-pdf/fpdf"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/engine/convert"
)

// pdfMetaPart is the MetaField.Part marker for PDF Info fields (the
// review panel is format-agnostic by design).
const pdfMetaPart = "pdf:Info"

// ExportPDF regenerates an anonymised PDF from the working text
// (ResultDocument.Anonymised) with the reviewed metadata. anonymised
// must be non-empty: a PDF without a text layer was already rejected at
// import, and re-checking here keeps the export path honest.
func ExportPDF(anonymised string, reviewed []MetaField, cfg Config) ([]byte, error) {
	if strings.TrimSpace(anonymised) == "" {
		// The one definition of the scanned-PDF sentence, so the wording
		// cannot drift from the import refusal it mirrors.
		return nil, fmt.Errorf("%s", convert.ErrScannedPDF)
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

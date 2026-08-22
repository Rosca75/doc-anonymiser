// engine/convert/pdf.go — .pdf → markdown text (one-way), EXPERIMENTAL.
//
// Extraction reads the PDF through the vendored pure-Go library's layout
// extraction (pdflayout.go): fragments with rectangles, grouped into lines by
// the split and join rules the line model owns, so the text the pipeline
// detects on is text that was actually contiguous on the page. The derived
// text then gets the nb1 spacing-repair heuristic, line by line. PDF text
// extraction is limited by design (CLAUDE.md §5): PDFs store positioned
// glyph runs, not words, so kerning can split words apart. Two repairs are
// applied per line (ported from the nb1 notebook):
//
//  1. Interleaved-capitals repair: a run of k ≥ 2 single uppercase letters
//     followed by k all-uppercase fragments is re-woven pairwise —
//     "B R IDDING ULES" → "BIDDING RULES". (These are the first letters of
//     consecutive words pulled out in front by a kerned drop-cap layout.)
//  2. Whitespace collapse: runs of spaces become one space.
//
// A PDF with no extractable text is rejected with the exact actionable
// message from CLAUDE.md §5 — it is almost certainly a scan. A file so
// damaged that no page survives the open gets its OWN message, because
// telling the user their truncated file "is likely scanned" sends them to an
// OCR tool that cannot help.
//
// The ledongthuc-based extractor is kept beside the production one, with no
// production caller: it is the deep tier's comparison baseline, and it leaves
// together with its dependency under the owner's decommissioning gate
// (CLAUDE.md §7's pin rows).
package convert

import (
	"bytes"
	"fmt"
	"strings"

	pdflib "github.com/ledongthuc/pdf"
)

// ErrScannedPDF is the CLAUDE.md §5 rejection message for PDFs without a
// text layer. Kept as a variable so the UI test can assert the exact text.
const ErrScannedPDF = "No text layer found, this PDF is likely scanned. OCR is not supported; convert it externally first."

// pdfPageSeparator joins consecutive PDF pages in the working markdown. It is
// also the boundary the page-scoped local-model scan slices on, which is why the
// exact per-page texts are returned separately by PDFWithPages: a blank line
// can occur INSIDE a page too, so the markdown alone cannot be split back into
// pages reliably.
const pdfPageSeparator = "\n\n"

// PDF converts raw .pdf bytes to repaired plain text (valid markdown by
// construction — plain paragraphs). Pages are separated by blank lines.
//
// It is the thin wrapper kept for callers that only need the joined markdown;
// PDFWithPages does the work and additionally returns the per-page texts.
func PDF(raw []byte) (markdown string, warnings []string, err error) {
	markdown, _, warnings, err = PDFWithPages(raw)
	return markdown, warnings, err
}

// PDFWithPages is PDF plus the per-page text slice, in page order.
//
// The pages slice is what the page-scoped local-model scan addresses (CLAUDE.md
// §5): the user picks "pages 2 to 4" and only those page texts are sent to the
// model. Joining pages with pdfPageSeparator reproduces markdown exactly, so
// the two returns never drift.
func PDFWithPages(raw []byte) (markdown string, pages []string, warnings []string, err error) {
	layouts, err := PDFLayouts(raw)
	if err != nil {
		return "", nil, nil, err
	}
	if len(layouts) == 0 {
		// The library salvage-opens truncated files with the pages still in
		// the bytes, so ZERO pages means nothing survived: the file is
		// damaged, not scanned, and the scanned message would mislead.
		return "", nil, nil, fmt.Errorf(
			"the PDF could not be read as pages, the file is likely damaged or truncated; re-export or re-print the original to PDF and import that file instead")
	}

	var repaired bool
	for _, layout := range layouts {
		text := PDFPageText(layout)
		fixed := RepairPDFText(text)
		if fixed != text {
			repaired = true
		}
		if strings.TrimSpace(fixed) != "" {
			pages = append(pages, strings.TrimSpace(fixed))
		}
	}

	if len(pages) == 0 {
		// Pages opened but none carries text: the exact scanned-PDF message
		// from CLAUDE.md §5.
		return "", nil, nil, fmt.Errorf("%s", ErrScannedPDF)
	}

	warnings = append(warnings, "PDF text extraction is EXPERIMENTAL, always review the converted text before anonymising")
	if repaired {
		warnings = append(warnings, "spacing repair was applied to fix kerning artefacts, verify that words were re-joined correctly")
	}
	return strings.Join(pages, pdfPageSeparator) + "\n", pages, warnings, nil
}

// PDFPageText is THE derivation of one page's pipeline text from its line
// model, BEFORE the spacing repair. The exporter uses it too (repairing the
// result exactly as PDFWithPages does), so what the pipeline detected on and
// what the exporter searches for are the same string by construction.
func PDFPageText(layout PDFPageLayout) string {
	return layout.text()
}

// classifyPDFOpenError turns the library's open error into the actionable
// message the import shows, keeping the password case distinguishable: an
// encrypted file has a remedy (remove the password) a damaged one does not.
func classifyPDFOpenError(err error) error {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "encrypt") || strings.Contains(msg, "password") {
		return fmt.Errorf(
			"the PDF is password-protected (%v); remove the password (open it and save an unprotected copy) and import that copy instead", err)
	}
	return fmt.Errorf(
		"the file is not a readable PDF (%v), if it is password-protected, remove the password first", err)
}

// pdfDamagedError wraps a parser panic or a page-read failure into the
// actionable damaged-file message.
func pdfDamagedError(cause interface{}) error {
	return fmt.Errorf(
		"the PDF could not be parsed (internal reader error: %v), the file may be damaged or use unsupported features; try re-printing it to PDF and importing again", cause)
}

// PDFWithPagesLedongthuc is the ledongthuc-based extractor: per-page plain
// text plus the same spacing repair. It has NO production caller; the deep
// tier measures the production extractor against it on the reference
// documents, and it is deleted together with its dependency once the owner
// confirms the decommissioning gate.
func PDFWithPagesLedongthuc(raw []byte) (markdown string, pages []string, warnings []string, err error) {
	// ledongthuc/pdf can panic on malformed files (it was written for
	// well-formed input). A panic must become an actionable error, never
	// crash the app — hence the recover.
	defer func() {
		if r := recover(); r != nil {
			markdown, pages, warnings = "", nil, nil
			err = pdfDamagedError(r)
		}
	}()

	reader, err := pdflib.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", nil, nil, fmt.Errorf(
			"the file is not a readable PDF (%v), if it is password-protected, remove the password first", err)
	}

	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		// nil font map: we only want plain text, not styled runs.
		text, err := page.GetPlainText(nil)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("page %d could not be extracted (%v), its text is missing from the output", i, err))
			continue
		}
		fixed := RepairPDFText(text)
		if strings.TrimSpace(fixed) != "" {
			pages = append(pages, strings.TrimSpace(fixed))
		}
	}
	if len(pages) == 0 {
		return "", nil, nil, fmt.Errorf("%s", ErrScannedPDF)
	}
	return strings.Join(pages, pdfPageSeparator) + "\n", pages, warnings, nil
}

// RepairPDFText applies the nb1 spacing-repair heuristic to extracted PDF
// text, line by line. Exported so the table-driven tests can pin the exact
// nb1 example cases.
//
// Repairs:   "B R IDDING ULES"      → "BIDDING RULES"
//
//	"double spaces here"  → "double spaces here"
//	"Tymoﬁejewicz"         → "Tymofiejewicz"  (ligature fold)
//
// Left alone: lowercase text, single stray capitals ("I", "A" as words),
//
//	and anything that does not match the k-letters/k-fragments
//	shape — a heuristic must never mangle normal text more than the
//	defect it fixes.
func RepairPDFText(s string) string {
	// Ligature fold FIRST, before any tokenisation: PDF fonts routinely
	// encode "fi", "fl", "ffi" ... as a single presentation-form glyph
	// (Unicode U+FB00..U+FB06), so "Tymofiejewicz" extracts as
	// "Tymoﬁejewicz". Left alone, the same real person then reads as TWO
	// different values (one spelt with the ligature, one without), so the
	// detector proposes both, the registry gives them two placeholders, and
	// worst of all the un-folded spelling can survive into the exported copy
	// UN-anonymised. Folding to ASCII collapses them back to one value.
	s = foldLigatures(s)

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = repairLine(line)
	}
	return strings.Join(lines, "\n")
}

// ligatureReplacer maps the Latin presentation-form ligatures a PDF text
// layer can carry back to their plain ASCII letters. Table-driven (via
// strings.Replacer) so extending it is a one-line change; these seven are
// the whole Unicode "Alphabetic Presentation Forms" Latin ligature block
// (U+FB00..U+FB06). "ﬅ"/"ﬆ" (long-s t / s t) both fold to "st".
var ligatureReplacer = strings.NewReplacer(
	"ﬀ", "ff",
	"ﬁ", "fi",
	"ﬂ", "fl",
	"ﬃ", "ffi",
	"ﬄ", "ffl",
	"ﬅ", "st",
	"ﬆ", "st",
)

// foldLigatures replaces every ligature glyph in the text with its ASCII
// spelling. Cheap enough to run on every extracted page: one pass, no
// allocation when the text contains no ligature.
func foldLigatures(s string) string {
	return ligatureReplacer.Replace(s)
}

// FoldPDFLigatures is foldLigatures for the in-place PDF export: the pipeline
// text is ligature-folded, so a locator comparing pipeline strings against
// raw fragment text must fold the fragments the same way or a value spelt
// with a ligature on the page can never be matched.
func FoldPDFLigatures(s string) string {
	return foldLigatures(s)
}

// repairLine fixes one line: interleaved-capitals first, then whitespace
// collapse (rebuilding from fields collapses any run of spaces).
func repairLine(line string) string {
	tokens := strings.Fields(line)
	if len(tokens) == 0 {
		return ""
	}

	var out []string
	for i := 0; i < len(tokens); {
		// Count the run of single-uppercase-letter tokens starting at i.
		k := 0
		for i+k < len(tokens) && isSingleUpper(tokens[i+k]) {
			k++
		}
		// The repair needs k ≥ 2 leading letters AND k all-uppercase
		// fragments right after them. k = 1 is deliberately skipped:
		// "I", "A" and initials are legitimate single-letter words and
		// merging them would corrupt normal prose.
		if k >= 2 && i+2*k <= len(tokens) && allUpperFragments(tokens[i+k:i+2*k]) {
			for j := 0; j < k; j++ {
				out = append(out, tokens[i+j]+tokens[i+k+j])
			}
			i += 2 * k
			continue
		}
		out = append(out, tokens[i])
		i++
	}
	return strings.Join(out, " ")
}

// isSingleUpper reports whether the token is exactly one ASCII uppercase
// letter (the shape of a kerning-detached first letter).
func isSingleUpper(tok string) bool {
	return len(tok) == 1 && tok[0] >= 'A' && tok[0] <= 'Z'
}

// allUpperFragments reports whether every token is an all-uppercase word of
// at least two letters (the shape of the decapitated word remainders).
func allUpperFragments(toks []string) bool {
	for _, t := range toks {
		if len(t) < 2 {
			return false
		}
		for _, c := range t {
			if c < 'A' || c > 'Z' {
				return false
			}
		}
	}
	return true
}

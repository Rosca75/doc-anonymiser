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
package convert

import (
	"fmt"
	"strings"
)

// ErrScannedPDF is the CLAUDE.md §5 rejection message for PDFs without a
// text layer. Kept as a variable so the UI test can assert the exact text.
const ErrScannedPDF = "No text layer found, this PDF is likely scanned. OCR is not supported; convert it externally first."

// ErrUnmappablePDF is the rejection message for a PDF whose text layer cannot
// be read as CHARACTERS. Kept as a variable for the same reason as
// ErrScannedPDF: the exact text is asserted.
//
// It is a THIRD refusal beside the scanned one and the damaged one, and it
// needs its own words because it is a different problem with a different
// remedy. The file has a text layer, so the scanned message would send the
// user to an OCR tool they do not need; the file is not damaged either, so the
// damaged message would tell them to re-print a file that opens perfectly.
// What is missing is the mapping from the glyphs the file draws back to
// characters, which happens when a producer embeds subset fonts without a
// usable ToUnicode CMap.
//
// This refusal exists because the alternative is SILENT. A document like this
// extracts thousands of characters, none of which is a letter, so detection
// finds nothing and the interface truthfully reports nothing found. A user is
// entitled to read that as "there is nothing to anonymise" and export, and the
// document is full of names. An honest refusal is the only safe answer.
const ErrUnmappablePDF = "The text in this PDF cannot be read as characters: its fonts carry no usable character map, so every glyph extracts as unknown. Nothing can be detected in it, and a run would report finding nothing in a document that is not empty. This is common in files written by Microsoft Print To PDF. Re-export the original to PDF from the application that made it (in Word or PowerPoint, File then Save as, choosing PDF) and import that file instead."

// unmappableRune is what an extractor yields for a glyph it cannot map back to
// a character.
const unmappableRune = '\ufffd'

// maxUnmappableShare is the share of non-blank characters that may be
// unmappable before the extraction is refused.
//
// 0.3 sits in a wide empty gap rather than on a boundary, which is why it is
// not tuned finer. Measured over the committed fixtures, a healthy document
// runs 0.0% to 0.2% (a stray symbol glyph, a logo drawn as text), and a
// document whose fonts carry no usable map runs 100%. Anything from a few
// percent to ninety would separate the two equally well; a third is chosen so
// that a document which is mostly readable is never refused for a page of
// symbols, while one nobody can detect in is always refused.
const maxUnmappableShare = 0.3

// unmappableShare is the fraction of non-blank runes that came back unmappable.
func unmappableShare(text string) float64 {
	total, bad := 0, 0
	for _, r := range text {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' {
			continue
		}
		total++
		if r == unmappableRune {
			bad++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(bad) / float64(total)
}

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

	// The text layer exists but may be unreadable AS TEXT. Checked after the
	// scanned refusal (that one is about there being no text at all) and
	// before any warning, because this is a refusal and not a caveat.
	if unmappableShare(strings.Join(pages, pdfPageSeparator)) > maxUnmappableShare {
		return "", nil, nil, fmt.Errorf("%s", ErrUnmappablePDF)
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
			"the PDF is password-protected (%w); remove the password (open it and save an unprotected copy) and import that copy instead", err)
	}
	return fmt.Errorf(
		"the file is not a readable PDF (%w), if it is password-protected, remove the password first", err)
}

// pdfDamagedError wraps a parser panic or a page-read failure into the
// actionable damaged-file message.
func pdfDamagedError(cause interface{}) error {
	return fmt.Errorf(
		"the PDF could not be parsed (internal reader error: %v), the file may be damaged or use unsupported features; try re-printing it to PDF and importing again", cause)
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

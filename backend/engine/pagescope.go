// engine/pagescope.go — addressable sub-units of a document for the
// page-scoped local-AI scan (CLAUDE.md §5).
//
// The reported problem was that handing a whole document to the local AI is
// "too much": a small model on a laptop chokes on a large file. The fix is to
// let the user aim the scan at ONE document and, within it, a page/segment
// range. This file is the engine half of that: it turns a Document into a
// count of addressable units and a way to slice the working markdown down to a
// range of them.
//
// A "page" here means the document's OWN unit, the same word already shown in
// the import list (Document.Unit): pages for PDF and DOCX, slides for PPTX,
// rows for CSV and flat XLSX, lines for TXT and MD. Where the boundaries are
// not recoverable from the markdown after conversion (PDF pages, DOCX pages)
// the converter pre-split them into Document.Pages; every other unit is derived
// here, on demand, from the markdown or the grid, so there is one source of
// truth and no risk of the two drifting.
//
// A document with no finer boundary than itself (a complex XLSX sheet rendered
// as one JSON block, or a DOCX Word never paginated) reports a PageCount of 1:
// honest, and the UI simply offers to scan it whole.
package engine

import (
	"fmt"
	"strings"
)

// PageCount reports how many addressable sub-units (pages/slides/rows/lines)
// the local AI can be scoped to for this document. It is always >= 1: even a
// document with no internal boundaries is one scannable unit.
func (d Document) PageCount() int {
	if n := len(d.Pages); n > 0 {
		// Pre-split by the converter (PDF pages, DOCX pages).
		return n
	}
	switch {
	case d.Unit == UnitRow && d.Grid != nil:
		// A grid's records are its rows; the header is not one of them.
		if n := gridRows(d.Grid); n > 0 {
			return n
		}
	case d.Unit == UnitSlide:
		if n := len(slideOffsets(d.Markdown)); n > 0 {
			return n
		}
	case d.Unit == UnitLine:
		if n := countLines(d.Markdown); n > 0 {
			return n
		}
	}
	// No finer boundary than the whole document.
	return 1
}

// PageRangeMarkdown returns the working-form markdown for the sub-units in the
// inclusive, 1-based range [from, to]. It is what the page-scoped scan actually
// sends to the model.
//
// The range is validated against PageCount, because a stale UI (the user
// removed a document, or edited the numbers by hand) must get an actionable
// error rather than a silently truncated or panicking slice.
func (d Document) PageRangeMarkdown(from, to int) (string, error) {
	count := d.PageCount()
	if from < 1 || to < 1 || from > to || to > count {
		return "", fmt.Errorf(
			"page range %d-%d is out of bounds for %q, which has %d %s(s); pick a range within 1-%d",
			from, to, d.Name, count, d.pageUnitWord(), count)
	}

	if len(d.Pages) > 0 {
		// Pre-split units join back with the format's own separator. Only
		// PDF and DOCX populate Pages, and both read as page-per-block, so a
		// blank line between them matches how the full markdown was built.
		return strings.Join(d.Pages[from-1:to], "\n\n"), nil
	}

	switch {
	case d.Unit == UnitRow && d.Grid != nil:
		// Re-render the selected records as their own table so the header row
		// (column names) still gives the model the context a bare data row
		// lacks. Grid[0] is the header; data record k is Grid[k].
		sub := make([][]string, 0, to-from+2)
		sub = append(sub, d.Grid[0])
		sub = append(sub, d.Grid[from:to+1]...)
		return GridToMarkdownTable(sub), nil
	case d.Unit == UnitSlide:
		offs := slideOffsets(d.Markdown)
		if len(offs) > 0 {
			return sliceByOffsets(d.Markdown, offs, from, to), nil
		}
	case d.Unit == UnitLine:
		offs := lineOffsets(d.Markdown)
		if len(offs) > 0 {
			return sliceByOffsets(d.Markdown, offs, from, to), nil
		}
	}
	// Single-unit document: the only valid range is the whole thing.
	return d.Markdown, nil
}

// pageUnitWord is the singular unit word for an error message, falling back to
// the generic "unit" when the document has no natural unit.
func (d Document) pageUnitWord() string {
	if d.Unit != "" {
		return d.Unit
	}
	return "unit"
}

// sliceByOffsets returns the markdown covering units [from, to] given the byte
// offset at which each unit starts. offs holds one start per unit, ascending;
// the slice runs from unit `from`'s start to unit `to+1`'s start (or the end of
// the text for the last unit). Callers guarantee 1 <= from <= to <= len(offs).
func sliceByOffsets(md string, offs []int, from, to int) string {
	start := offs[from-1]
	end := len(md)
	if to < len(offs) {
		end = offs[to]
	}
	return strings.TrimRight(md[start:end], "\n")
}

// lineOffsets returns the byte offset at which each line of md begins. It
// mirrors countLines: a trailing newline does not open an extra empty line, so
// the offset count equals PageCount for a line-unit document.
func lineOffsets(md string) []int {
	if md == "" {
		return nil
	}
	offs := []int{0}
	// TrimSuffix so a final "\n" does not register a phantom empty last line.
	trimmed := strings.TrimSuffix(md, "\n")
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] == '\n' {
			offs = append(offs, i+1)
		}
	}
	return offs
}

// slideOffsets returns the byte offset of each "## Slide " section heading in a
// PPTX working form. The heading shape is generated by convert.Pptx, so it is a
// reliable boundary; anything before the first heading (there is none in
// practice) is not counted as a slide.
func slideOffsets(md string) []int {
	const marker = "## Slide "
	var offs []int
	for i := 0; i < len(md); {
		// A heading is a marker at the very start or right after a newline.
		if (i == 0 || md[i-1] == '\n') && strings.HasPrefix(md[i:], marker) {
			offs = append(offs, i)
			// Skip past this marker so a heading is never matched twice.
			i += len(marker)
			continue
		}
		i++
	}
	return offs
}

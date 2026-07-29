// Package convert holds the one-way binary-format → markdown converters
// (CLAUDE.md §3/§4): .docx, .pptx, .xlsx and .pdf come in as raw bytes and
// leave as markdown text (plus, for xlsx, per-sheet Grid/JSON models).
//
// Hard rules for this package (CLAUDE.md §4, "Converters are pure Go"):
//   - Only the Go standard library, excelize and ledongthuc/pdf. No CGo.
//   - One-way: the app NEVER exports back to these binary formats.
//   - No imports of package engine (engine imports us, not the reverse) and
//     no filesystem access — bytes in, bytes/strings out.
//
// This file holds the small helpers shared by more than one converter.
package convert

import "strings"

// escapeTableCell makes a string safe inside a markdown table cell: pipes
// would be read as column separators and newlines would break the row.
// Shared by the docx and pptx table renderers.
func escapeTableCell(cell string) string {
	cell = strings.ReplaceAll(cell, "|", "\\|")
	cell = strings.ReplaceAll(cell, "\r\n", " ")
	cell = strings.ReplaceAll(cell, "\n", " ")
	return strings.TrimSpace(cell)
}

// tableToMarkdown renders rows of cells as a GitHub markdown table, first
// row as header. Rows shorter than the header are padded so the table stays
// well-formed. Returns "" for an empty table.
func tableToMarkdown(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	// The header defines the column count; find the widest row so no cell
	// is dropped when a data row is wider than the header (rare but legal
	// in both OOXML table models).
	width := 0
	for _, r := range rows {
		if len(r) > width {
			width = len(r)
		}
	}
	var b strings.Builder
	writeRow := func(row []string) {
		b.WriteString("|")
		for c := 0; c < width; c++ {
			cell := ""
			if c < len(row) {
				cell = row[c]
			}
			b.WriteString(" " + escapeTableCell(cell) + " |")
		}
		b.WriteString("\n")
	}
	writeRow(rows[0])
	b.WriteString("|")
	for c := 0; c < width; c++ {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")
	for _, row := range rows[1:] {
		writeRow(row)
	}
	return b.String()
}

// wrapEmphasis wraps run text in markdown bold/italic markers, moving any
// leading/trailing whitespace OUTSIDE the markers — "** text**" is not valid
// markdown emphasis, " **text**" is.
func wrapEmphasis(text string, bold, italic bool) string {
	if text == "" || (!bold && !italic) {
		return text
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return text // whitespace-only runs carry no emphasis
	}
	lead := text[:strings.Index(text, trimmed)]
	trail := text[len(lead)+len(trimmed):]
	marker := ""
	if bold {
		marker += "**"
	}
	if italic {
		marker += "*"
	}
	return lead + marker + trimmed + marker + trail
}

// engine/csvmd.go — CSV ⇄ markdown-table conversion (CLAUDE.md §3).
//
// Three jobs live here, and only here:
//
//  1. ParseCSV: raw CSV bytes → Grid ([][]string), tolerating real-world
//     mess (quoted fields, embedded commas/newlines, ragged rows).
//  2. GridToMarkdownTable: Grid → GitHub-style markdown table used as the
//     working form for preview and anonymisation.
//  3. GridToCSV: Grid → CSV bytes for the round-trip export required by
//     CLAUDE.md §5 ("CSV imports ... round-trip back to CSV on export").
//
// The round-trip contract: for a well-formed CSV (rectangular, LF line
// endings, minimal quoting) ParseCSV → GridToCSV reproduces the input
// byte-identically. Ragged input is REPAIRED (rows padded with empty cells)
// and a warning is attached to the Document, so the round-trip of ragged
// input is intentionally the repaired form, not the original bytes.
package engine

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"
)

// ParseCSV reads raw CSV bytes into a rectangular rows×columns grid.
//
// Returned values:
//   - grid: every row padded to the width of the widest row, so downstream
//     code (markdown rendering, cell-wise anonymisation, CSV export) can
//     assume a rectangle.
//   - warnings: human-readable notes about repairs performed (currently:
//     ragged rows). These end up on Document.Warnings for the UI.
//   - err: only for input that cannot be interpreted as CSV at all
//     (e.g. a bare quote mid-field) or an empty file.
func ParseCSV(raw []byte) (grid [][]string, warnings []string, err error) {
	r := csv.NewReader(bytes.NewReader(raw))
	// FieldsPerRecord = -1 disables the "every record must have the same
	// number of fields" check: hand-edited CSVs are frequently ragged and
	// we repair them below instead of rejecting the whole file.
	r.FieldsPerRecord = -1
	// Keep leading spaces inside fields — trimming would silently change
	// document content, which an anonymiser must never do.
	r.TrimLeadingSpace = false

	grid, err = r.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(grid) == 0 {
		return nil, nil, fmt.Errorf("the file is empty (no rows found)")
	}

	// Find the widest row, then pad narrower rows with empty cells so the
	// grid is rectangular. Record a warning when we had to repair.
	width := 0
	for _, row := range grid {
		if len(row) > width {
			width = len(row)
		}
	}
	ragged := 0
	for i, row := range grid {
		if len(row) < width {
			padded := make([]string, width)
			copy(padded, row)
			grid[i] = padded
			ragged++
		}
	}
	if ragged > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%d row(s) had fewer columns than the widest row (%d columns) and were padded with empty cells, check the source file if columns look shifted",
			ragged, width))
	}
	return grid, warnings, nil
}

// GridToMarkdownTable renders a grid as a GitHub-style markdown table with
// the first row as header. Cell content is escaped so pipes cannot break
// the table structure and embedded newlines cannot break the row. This is
// the CSV working form every anonymisation pass operates on.
func GridToMarkdownTable(grid [][]string) string {
	if len(grid) == 0 {
		return ""
	}

	// escape makes one cell safe inside a markdown table row.
	escape := func(cell string) string {
		cell = strings.ReplaceAll(cell, "|", "\\|")
		cell = strings.ReplaceAll(cell, "\r\n", " ")
		cell = strings.ReplaceAll(cell, "\n", " ")
		return cell
	}

	var b strings.Builder
	writeRow := func(row []string) {
		b.WriteString("|")
		for _, cell := range row {
			b.WriteString(" " + escape(cell) + " |")
		}
		b.WriteString("\n")
	}

	// Header row (first CSV row by convention), then the separator row
	// that markdown table syntax requires, then the data rows. ParseCSV
	// guarantees a rectangular grid, so every row matches the header.
	writeRow(grid[0])
	b.WriteString("|")
	for range grid[0] {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")
	for _, row := range grid[1:] {
		writeRow(row)
	}
	return b.String()
}

// GridToCSV serialises a grid back to CSV bytes for export. It uses the
// standard library writer with LF line endings and minimal quoting. Fixture
// tests pin the byte shape here, and caller-visible export behaviour builds on
// that canonical form.
func GridToCSV(grid [][]string) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.WriteAll(grid); err != nil {
		// Practically unreachable with a bytes.Buffer destination, but
		// the error path is kept honest rather than swallowed.
		return nil, fmt.Errorf("could not serialise the table back to CSV: %w", err)
	}
	return buf.Bytes(), nil
}

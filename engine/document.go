// Package engine holds ALL business logic of doc-anonymiser and is strictly
// UI-agnostic (CLAUDE.md §4): it never imports Wails, never opens file
// dialogs and never reads user-chosen filesystem paths. Documents arrive as
// a filename + raw bytes (handed over by app.go) and leave as bytes again.
// That keeps everything in this package unit-testable without a GUI.
//
// This file defines the Document model and the ingestion logic for the three
// supported formats: .txt, .csv and .md (CLAUDE.md §5). Anything else is
// rejected with an actionable error message.
package engine

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"path/filepath"
	"strings"
)

// Format identifies which of the three supported input formats a Document
// was loaded from. It drives both processing (CSV keeps its grid model) and
// export (CSV round-trips back to CSV, see CLAUDE.md §5 "Process order").
type Format string

const (
	FormatTXT Format = "txt"
	FormatCSV Format = "csv"
	FormatMD  Format = "md"
)

// Document is the in-memory working form of one imported file.
//
// The original bytes are kept verbatim in Raw and are never written back to
// the source path (CLAUDE.md §4, "Originals are immutable"). All processing
// happens on the Markdown working form; CSV files additionally retain their
// parsed Grid so they can round-trip back to CSV on export.
type Document struct {
	// Name is the base filename as chosen by the user (e.g. "notes.txt").
	// It is display metadata only — the engine never touches the path.
	Name string
	// Format is the detected input format (txt / csv / md).
	Format Format
	// Raw is the untouched original file content.
	Raw []byte
	// Markdown is the working form every pipeline pass operates on.
	// txt → same text (line endings normalised), md → passthrough,
	// csv → rendered markdown table.
	Markdown string
	// Grid is the parsed cell model for CSV documents (rows × columns),
	// kept so an anonymised CSV can be exported as CSV again. It is nil
	// for txt and md documents.
	Grid [][]string
}

// Load turns a filename plus raw bytes into a Document in markdown working
// form. It is the single entry point for ingestion; app.go calls it after
// the user picks files in the native dialog (or drops them on the window).
//
// Unsupported extensions are rejected here as well as in the file-dialog
// filter, because drag-and-drop bypasses the dialog (CLAUDE.md §5).
func Load(name string, raw []byte) (Document, error) {
	// Detect the format from the file extension, case-insensitively, so
	// "REPORT.TXT" from an old Windows share still works.
	ext := strings.ToLower(filepath.Ext(name))

	switch ext {
	case ".txt":
		return Document{
			Name:     name,
			Format:   FormatTXT,
			Raw:      raw,
			Markdown: normaliseLineEndings(string(raw)),
		}, nil

	case ".md":
		// Markdown is already our working form — pass it through with
		// only line-ending normalisation so downstream regexes can
		// assume "\n" everywhere.
		return Document{
			Name:     name,
			Format:   FormatMD,
			Raw:      raw,
			Markdown: normaliseLineEndings(string(raw)),
		}, nil

	case ".csv":
		grid, err := parseCSV(raw)
		if err != nil {
			// Wrap with context the owner can act on: which file,
			// what we expected, what to try.
			return Document{}, fmt.Errorf(
				"could not read %q as CSV: %w — check that the file is a plain comma-separated file (open it in a text editor to verify); if it is a semicolon-separated export, re-export it with commas",
				name, err)
		}
		return Document{
			Name:     name,
			Format:   FormatCSV,
			Raw:      raw,
			Markdown: gridToMarkdownTable(grid),
			Grid:     grid,
		}, nil

	default:
		// Clear, actionable rejection for everything else (.docx, .pdf,
		// .pptx, .xlsx, ...): conversion of those is deferred to v2
		// (see BUILD.md "Deferred to v2").
		return Document{}, fmt.Errorf(
			"unsupported file type %q (file %q): doc-anonymiser accepts .txt, .csv and .md only — for Word/PDF/PowerPoint/Excel files, export or save them as one of those formats first",
			ext, name)
	}
}

// normaliseLineEndings converts Windows (\r\n) and old-Mac (\r) line endings
// to plain \n. Every later pipeline pass (regexes in particular) can then
// safely assume Unix line endings.
func normaliseLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

// parseCSV reads raw CSV bytes into a rows×columns grid using the standard
// library parser. FieldsPerRecord = -1 tolerates ragged rows (common in
// hand-edited CSVs) instead of failing the whole import.
func parseCSV(raw []byte) ([][]string, error) {
	r := csv.NewReader(bytes.NewReader(raw))
	r.FieldsPerRecord = -1
	grid, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(grid) == 0 {
		return nil, fmt.Errorf("the file is empty (no rows found)")
	}
	return grid, nil
}

// gridToMarkdownTable renders a CSV grid as a GitHub-style markdown table,
// treating the first row as the header. Pipe characters inside cells are
// escaped so they cannot break the table structure. The full CSV ⇄ markdown
// round-trip logic will live in csvmd.go in a later phase; this renderer is
// the "CSV → markdown" half needed for preview/processing.
func gridToMarkdownTable(grid [][]string) string {
	if len(grid) == 0 {
		return ""
	}

	// escape makes a cell safe inside a markdown table: pipes would
	// otherwise be read as column separators, and embedded newlines
	// would break the row.
	escape := func(cell string) string {
		cell = strings.ReplaceAll(cell, "|", "\\|")
		cell = strings.ReplaceAll(cell, "\r\n", " ")
		cell = strings.ReplaceAll(cell, "\n", " ")
		return cell
	}

	var b strings.Builder

	// Header row (first CSV row by convention).
	header := grid[0]
	b.WriteString("|")
	for _, cell := range header {
		b.WriteString(" " + escape(cell) + " |")
	}
	b.WriteString("\n")

	// Separator row required by markdown table syntax.
	b.WriteString("|")
	for range header {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")

	// Data rows. Rows shorter than the header are padded implicitly by
	// simply writing what exists; rows longer keep their extra cells.
	for _, row := range grid[1:] {
		b.WriteString("|")
		for _, cell := range row {
			b.WriteString(" " + escape(cell) + " |")
		}
		b.WriteString("\n")
	}

	return b.String()
}

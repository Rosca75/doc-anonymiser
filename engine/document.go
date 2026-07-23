// Package engine holds ALL business logic of doc-anonymiser and is strictly
// UI-agnostic (CLAUDE.md §4): it never imports Wails, never opens file
// dialogs and never reads user-chosen filesystem paths. Documents arrive as
// a filename + raw bytes (handed over by app.go) and leave as bytes again.
// That keeps everything in this package unit-testable without a GUI.
//
// This file defines the Document model and the ingestion logic for the
// text-based formats: .txt, .csv and .md (CLAUDE.md §5). Binary Office/PDF
// formats are handled by engine/convert/* and wired in via LoadAll.
// Anything else is rejected with an actionable error message.
package engine

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Format identifies which supported input format a Document was loaded
// from. It drives both processing (CSV keeps its grid model) and export
// (CSV round-trips back to CSV, see CLAUDE.md §5 "Process order").
type Format string

const (
	FormatTXT Format = "txt"
	FormatCSV Format = "csv"
	FormatMD  Format = "md"
)

// LargeFileThreshold is the size above which a file gets a "very large"
// warning (still processed — the warning just explains possible slowness).
// 10 MB per BUILD.md Phase 1 activity 3.
const LargeFileThreshold = 10 * 1024 * 1024

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
	// Grid is the parsed cell model for CSV documents (rows × columns,
	// rectangular — ragged input is padded). It is kept so an anonymised
	// CSV can be exported as CSV again. nil for txt and md documents.
	Grid [][]string
	// Warnings collects non-fatal ingestion notes shown to the user in
	// the import list (ragged CSV repaired, empty file, very large file).
	// A warning never blocks processing — that is what errors are for.
	Warnings []string
}

// Load turns a filename plus raw bytes into a Document in markdown working
// form. It is the single entry point for text-format ingestion; app.go
// calls it after the user picks files in the native dialog (or drops them
// on the window).
//
// Unsupported extensions are rejected here as well as in the file-dialog
// filter, because drag-and-drop bypasses the dialog (CLAUDE.md §5).
func Load(name string, raw []byte) (Document, error) {
	// Detect the format from the file extension, case-insensitively, so
	// "REPORT.TXT" from an old Windows share still works. Extension-only
	// detection is a deliberate CLAUDE.md §5 / BUILD.md Phase 1 decision:
	// no content sniffing.
	ext := strings.ToLower(filepath.Ext(name))

	// Every text format must be valid UTF-8 — the anonymisation regexes
	// and the markdown working form assume it. Reject other encodings
	// with a message that names the file and says how to fix it.
	if !utf8.Valid(raw) {
		return Document{}, fmt.Errorf(
			"file %q is not valid UTF-8 text: it is probably saved in a legacy encoding such as Windows-1252 or Latin-1 — open it in a text editor (e.g. Notepad++ or VS Code) and re-save it with UTF-8 encoding, then import it again",
			name)
	}

	// Size warnings are shared by all formats and never block processing.
	var warnings []string
	if len(raw) == 0 {
		warnings = append(warnings, "the file is empty — nothing to anonymise")
	} else if len(raw) > LargeFileThreshold {
		warnings = append(warnings, fmt.Sprintf(
			"very large file (%.1f MB) — processing may take a while; the preview may be truncated",
			float64(len(raw))/(1024*1024)))
	}

	switch ext {
	case ".txt":
		return Document{
			Name:     name,
			Format:   FormatTXT,
			Raw:      raw,
			Markdown: normaliseLineEndings(string(raw)),
			Warnings: warnings,
		}, nil

	case ".md":
		// Markdown is already our working form — pass it through with
		// only line-ending normalisation so downstream regexes can
		// assume "\n" everywhere. Content inside code fences is treated
		// like any other text in v1 (BUILD.md Phase 1 activity 4).
		return Document{
			Name:     name,
			Format:   FormatMD,
			Raw:      raw,
			Markdown: normaliseLineEndings(string(raw)),
			Warnings: warnings,
		}, nil

	case ".csv":
		grid, csvWarnings, err := ParseCSV(raw)
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
			Markdown: GridToMarkdownTable(grid),
			Grid:     grid,
			Warnings: append(warnings, csvWarnings...),
		}, nil

	default:
		// Clear, actionable rejection for everything else. The binary
		// Office/PDF formats are accepted via LoadAll (Phase 1B); this
		// text-only entry point lists what IT accepts.
		return Document{}, fmt.Errorf(
			"unsupported file type %q (file %q): doc-anonymiser accepts .txt, .csv, .md, .docx, .pptx, .xlsx and .pdf — rename or convert the file to one of those formats first",
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

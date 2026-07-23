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

	"doc-anonymiser/engine/convert"
)

// Format identifies which supported input format a Document was loaded
// from. It drives both processing (CSV keeps its grid model) and export
// (CSV round-trips back to CSV, see CLAUDE.md §5 "Process order").
type Format string

const (
	FormatTXT Format = "txt"
	FormatCSV Format = "csv"
	FormatMD  Format = "md"
	// Binary formats converted one-way to markdown (engine/convert/*).
	FormatDOCX Format = "docx"
	FormatPPTX Format = "pptx"
	// A flat xlsx sheet behaves like a CSV import (Grid set, CSV
	// round-trip on export); a complex sheet becomes structured JSON in a
	// fenced code block (JSON field set, export as .md or .json).
	FormatXLSX     Format = "xlsx"
	FormatXLSXJSON Format = "xlsx-json"
	// PDF support is EXPERIMENTAL (CLAUDE.md §5) and labelled as such.
	FormatPDF Format = "pdf"
)

// LargeFileThreshold is the size above which a file gets a "very large"
// warning (still processed — the warning just explains possible slowness).
// 10 MB per BUILD.md Phase 1 activity 3.
const LargeFileThreshold = 10 * 1024 * 1024

// MaxPreviewLines caps how many lines of a document the UI preview shows
// (BUILD.md Phase 10: render the first 5 000 lines; the FULL content is
// still processed by the pipeline — only the preview is cut).
const MaxPreviewLines = 5000

// PreviewMarkdown returns the preview-safe version of a working form: the
// first MaxPreviewLines lines, plus a flag telling the UI to show a
// truncation notice. Small documents come back unchanged.
func PreviewMarkdown(md string) (preview string, truncated bool) {
	count := 0
	for i := 0; i < len(md); i++ {
		if md[i] == '\n' {
			count++
			if count == MaxPreviewLines {
				return md[:i], true
			}
		}
	}
	return md, false
}

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
	// JSON holds the structured cell JSON of a COMPLEX xlsx sheet
	// (FormatXLSXJSON) so it can be exported as .json after
	// anonymisation. Empty for every other format.
	JSON string
	// Warnings collects non-fatal ingestion notes shown to the user in
	// the import list (ragged CSV repaired, empty file, very large file,
	// dropped images, complex-sheet routing, PDF repair applied).
	// A warning never blocks processing — that is what errors are for.
	Warnings []string
}

// LoadAll is the single ingestion entry point used by app.go: it accepts
// every supported extension and returns one or MORE Documents — an xlsx
// workbook yields one Document per sheet (CLAUDE.md §5), every other
// format yields exactly one.
func LoadAll(name string, raw []byte) ([]Document, error) {
	ext := strings.ToLower(filepath.Ext(name))

	// Shared size warnings (empty / very large) apply to every format.
	var sizeWarnings []string
	if len(raw) == 0 {
		sizeWarnings = append(sizeWarnings, "the file is empty — nothing to anonymise")
	} else if len(raw) > LargeFileThreshold {
		sizeWarnings = append(sizeWarnings, fmt.Sprintf(
			"very large file (%.1f MB) — processing may take a while; the preview may be truncated",
			float64(len(raw))/(1024*1024)))
	}

	switch ext {
	case ".txt", ".csv", ".md":
		doc, err := Load(name, raw)
		if err != nil {
			return nil, err
		}
		return []Document{doc}, nil

	case ".docx":
		md, warns, err := convert.Docx(raw)
		if err != nil {
			return nil, fmt.Errorf("could not import %q: %w", name, err)
		}
		return []Document{{
			Name:     name,
			Format:   FormatDOCX,
			Raw:      raw,
			Markdown: md,
			Warnings: append(sizeWarnings, warns...),
		}}, nil

	case ".pptx":
		md, warns, err := convert.Pptx(raw)
		if err != nil {
			return nil, fmt.Errorf("could not import %q: %w", name, err)
		}
		return []Document{{
			Name:     name,
			Format:   FormatPPTX,
			Raw:      raw,
			Markdown: md,
			Warnings: append(sizeWarnings, warns...),
		}}, nil

	case ".xlsx":
		sheets, workbookWarns, err := convert.Xlsx(raw)
		if err != nil {
			return nil, fmt.Errorf("could not import %q: %w", name, err)
		}
		docs := make([]Document, 0, len(sheets))
		for i, s := range sheets {
			doc := Document{
				// Per-sheet naming convention from CLAUDE.md §5:
				// "<workbook>.xlsx#<sheet>".
				Name:     name + "#" + s.Name,
				Raw:      raw,
				Warnings: s.Warnings,
			}
			// Workbook-level warnings (size, skipped empty sheets) are
			// attached once, to the first sheet, not duplicated per sheet.
			if i == 0 {
				doc.Warnings = append(append([]string{}, sizeWarnings...),
					append(workbookWarns, s.Warnings...)...)
			}
			if s.Flat {
				// FLAT sheet: same downstream behaviour as a CSV import
				// (markdown-table working form + Grid for round-trip).
				doc.Format = FormatXLSX
				doc.Grid = s.Grid
				doc.Markdown = GridToMarkdownTable(s.Grid)
			} else {
				// COMPLEX sheet: structured JSON anonymised as text
				// inside a fenced code block.
				doc.Format = FormatXLSXJSON
				doc.JSON = s.JSON
				doc.Markdown = "```json\n" + s.JSON + "\n```\n"
			}
			docs = append(docs, doc)
		}
		return docs, nil

	case ".pdf":
		md, warns, err := convert.PDF(raw)
		if err != nil {
			return nil, fmt.Errorf("could not import %q: %w", name, err)
		}
		return []Document{{
			Name:     name,
			Format:   FormatPDF,
			Raw:      raw,
			Markdown: md,
			Warnings: append(sizeWarnings, warns...),
		}}, nil

	default:
		return nil, fmt.Errorf(
			"unsupported file type %q (file %q): doc-anonymiser accepts .txt, .csv, .md, .docx, .pptx, .xlsx and .pdf — rename or convert the file to one of those formats first",
			ext, name)
	}
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

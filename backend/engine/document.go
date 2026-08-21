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

	"doc-anonymiser/backend/engine/convert"
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
// 10 MB per activity 3.
const LargeFileThreshold = 10 * 1024 * 1024

// MaxPreviewLines caps how many lines of a document the UI preview shows
// (: render the first 5 000 lines; the FULL content is
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
	// UnitCount and Unit are the document's SIZE IN ITS OWN TERMS, for the
	// import list: "6 pages", "12 slides",
	// "48 rows", "412 lines". A byte size tells a user nothing about whether
	// they picked the right file; the number of pages does.
	//
	// Unit is one of "page", "slide", "row" or "line", singular. The UI
	// pluralises, because only the UI knows the number it is about to print.
	//
	// The fallback to "line" is not a failure case, it is the COMMON case: a
	// page count has to come from what the writing application cached in
	// docProps/app.xml, and that part is optional (see ooxml.AppProps for the
	// two caveats). Counting lines is always possible and never wrong, so it
	// is what every format that cannot do better reports.
	UnitCount int
	Unit      string
	// Pages holds the working form pre-split into the document's addressable
	// sub-units, for the page-scoped local-model scan (CLAUDE.md §5): the user
	// picks one document and a page/segment range and only that text is sent
	// to the model. It is populated ONLY for formats whose boundaries are not
	// recoverable from Markdown after the fact: PDF pages (a blank line can
	// sit inside a page too) and DOCX pages (Word's cached break positions).
	// For every other format the boundaries live in Markdown or the Grid and
	// are derived on demand (see pagescope.go), so Pages stays nil and is not
	// a second source of truth. nil or a single entry means "no finer
	// boundary than the whole document".
	Pages []string
}

// The unit names Document.Unit may hold. Constants rather than bare strings so
// a typo in one converter cannot invent a fifth unit the UI has no plural for.
const (
	UnitPage  = "page"
	UnitSlide = "slide"
	UnitRow   = "row"
	UnitLine  = "line"
)

// countLines is the universal fallback unit: how many lines the markdown
// working form has.
//
// A trailing newline does NOT count as an extra empty line, because "412
// lines" for a file whose last line ends properly should not become 413. An
// empty document is 0 lines rather than 1.
func countLines(markdown string) int {
	if markdown == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(markdown, "\n"), "\n") + 1
}

// gridRows reports how many RECORDS a grid holds, which is its rows minus the
// header row, so "48 rows" means 48 records rather than 47 records and a
// header. A grid with only a header is 0 rows, which is honest: there is
// nothing in it to anonymise.
//
// It lives here rather than in engine/convert because it is about the Grid
// model, which is this package's, and because both CSV and a flat xlsx sheet
// need it.
func gridRows(grid [][]string) int {
	if len(grid) <= 1 {
		return 0
	}
	return len(grid) - 1
}

// setUnitCount fills a Document's UnitCount and Unit, falling back to lines
// whenever the preferred count is unavailable or zero.
//
// The zero check is what stops "0 pages" reaching the UI: a docx whose
// app.xml is missing, or present but caching Pages as 0, has to say something
// true instead, and "412 lines" is true for every text-bearing document.
//
// @param doc the document to annotate, modified in place
// @param count the preferred count (0 when unavailable)
// @param unit the unit that count is in
func setUnitCount(doc *Document, count int, unit string) {
	if count > 0 && unit != "" {
		doc.UnitCount = count
		doc.Unit = unit
		return
	}
	doc.UnitCount = countLines(doc.Markdown)
	doc.Unit = UnitLine
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
		sizeWarnings = append(sizeWarnings, "the file is empty, nothing to anonymise")
	} else if len(raw) > LargeFileThreshold {
		sizeWarnings = append(sizeWarnings, fmt.Sprintf(
			"very large file (%.1f MB), processing may take a while; the preview may be truncated",
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
		md, pages, warns, err := convert.DocxWithPages(raw)
		if err != nil {
			return nil, fmt.Errorf("could not import %q: %w", name, err)
		}
		doc := Document{
			Name:     name,
			Format:   FormatDOCX,
			Raw:      raw,
			Markdown: md,
			Pages:    pages,
			Warnings: append(sizeWarnings, warns...),
		}
		// A page count is what a Word user recognises, but it can only come
		// from what Word cached at save time, and plenty of files carry no
		// cached count at all. Zero falls back to lines.
		setUnitCount(&doc, convert.DocxPages(raw), UnitPage)
		return []Document{doc}, nil

	case ".pptx":
		md, warns, err := convert.Pptx(raw)
		if err != nil {
			return nil, fmt.Errorf("could not import %q: %w", name, err)
		}
		doc := Document{
			Name:     name,
			Format:   FormatPPTX,
			Raw:      raw,
			Markdown: md,
			Warnings: append(sizeWarnings, warns...),
		}
		// Counting slide parts beats reading the cached <Slides> property: the
		// part count comes from the archive's shape, so it cannot go stale.
		setUnitCount(&doc, convert.PptxSlides(raw), UnitSlide)
		return []Document{doc}, nil

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
				// A grid's own unit is its records, not its lines.
				setUnitCount(&doc, gridRows(s.Grid), UnitRow)
			} else {
				// COMPLEX sheet: structured JSON anonymised as text
				// inside a fenced code block.
				doc.Format = FormatXLSXJSON
				doc.JSON = s.JSON
				doc.Markdown = "```json\n" + s.JSON + "\n```\n"
				// A complex sheet has no meaningful row count (that is exactly
				// why it was routed to JSON), so it reports lines.
				setUnitCount(&doc, 0, "")
			}
			docs = append(docs, doc)
		}
		return docs, nil

	case ".pdf":
		md, pages, warns, err := convert.PDFWithPages(raw)
		if err != nil {
			return nil, fmt.Errorf("could not import %q: %w", name, err)
		}
		doc := Document{
			Name:     name,
			Format:   FormatPDF,
			Raw:      raw,
			Markdown: md,
			Pages:    pages,
			Warnings: append(sizeWarnings, warns...),
		}
		// The PDF's own page tree, so this figure is exact.
		setUnitCount(&doc, convert.PDFPages(raw), UnitPage)
		return []Document{doc}, nil

	default:
		return nil, fmt.Errorf(
			"unsupported file type %q (file %q): doc-anonymiser accepts .txt, .csv, .md, .docx, .pptx, .xlsx and .pdf, rename or convert the file to one of those formats first",
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
	// detection is a deliberate CLAUDE.md §5 /  decision:
	// no content sniffing.
	ext := strings.ToLower(filepath.Ext(name))

	// Every text format must be valid UTF-8 — the anonymisation regexes
	// and the markdown working form assume it. Reject other encodings
	// with a message that names the file and says how to fix it.
	if !utf8.Valid(raw) {
		return Document{}, fmt.Errorf(
			"file %q is not valid UTF-8 text: it is probably saved in a legacy encoding such as Windows-1252 or Latin-1, open it in a text editor (e.g. Notepad++ or VS Code) and re-save it with UTF-8 encoding, then import it again",
			name)
	}

	// Size warnings are shared by all formats and never block processing.
	var warnings []string
	if len(raw) == 0 {
		warnings = append(warnings, "the file is empty, nothing to anonymise")
	} else if len(raw) > LargeFileThreshold {
		warnings = append(warnings, fmt.Sprintf(
			"very large file (%.1f MB), processing may take a while; the preview may be truncated",
			float64(len(raw))/(1024*1024)))
	}

	switch ext {
	case ".txt":
		doc := Document{
			Name:     name,
			Format:   FormatTXT,
			Raw:      raw,
			Markdown: normaliseLineEndings(string(raw)),
			Warnings: warnings,
		}
		// Plain text has no pages or slides; its own unit is the line.
		setUnitCount(&doc, 0, "")
		return doc, nil

	case ".md":
		// Markdown is already our working form — pass it through with
		// only line-ending normalisation so downstream regexes can
		// assume "\n" everywhere. Content inside code fences is treated
		// like any other text in v1.
		doc := Document{
			Name:     name,
			Format:   FormatMD,
			Raw:      raw,
			Markdown: normaliseLineEndings(string(raw)),
			Warnings: warnings,
		}
		setUnitCount(&doc, 0, "")
		return doc, nil

	case ".csv":
		grid, csvWarnings, err := ParseCSV(raw)
		if err != nil {
			// Wrap with context the owner can act on: which file,
			// what we expected, what to try.
			return Document{}, fmt.Errorf(
				"could not read %q as CSV: %w, check that the file is a plain comma-separated file (open it in a text editor to verify); if it is a semicolon-separated export, re-export it with commas",
				name, err)
		}
		doc := Document{
			Name:     name,
			Format:   FormatCSV,
			Raw:      raw,
			Markdown: GridToMarkdownTable(grid),
			Grid:     grid,
			Warnings: append(warnings, csvWarnings...),
		}
		// A CSV's unit is its records, so the header row does not count.
		setUnitCount(&doc, gridRows(grid), UnitRow)
		return doc, nil

	default:
		// Clear, actionable rejection for everything else. The binary
		// Office/PDF formats are accepted via LoadAll; this
		// text-only entry point lists what IT accepts.
		return Document{}, fmt.Errorf(
			"unsupported file type %q (file %q): doc-anonymiser accepts .txt, .csv, .md, .docx, .pptx, .xlsx and .pdf, rename or convert the file to one of those formats first",
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

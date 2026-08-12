// engine/export.go — egress helpers (CLAUDE.md §5: every
// export is an explicit user action; no export path produces a binary
// Office/PDF format).
//
// Naming convention: exported files keep their original base name with an
// "_anon" suffix and the export extension, e.g. "notes.txt" →
// "notes_anon.txt", "workbook.xlsx#Clients" → "workbook.xlsx_Clients_anon.csv"
// (the '#' is replaced — it is illegal in Windows filenames).
package engine

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
)

// ExportExtensions lists the offered export formats per document format
// The first entry is the default. Since
//
//	the SOURCE format is offered too (same-format
//
// export, listed last so text formats stay the default): the copy is
// produced by rewriting the in-memory original bytes; the file on disk
// is never touched.
func ExportExtensions(f Format) []string {
	switch f {
	case FormatCSV:
		return []string{"csv", "md"}
	case FormatXLSX: // flat sheets: CSV round-trip plus the native workbook
		return []string{"csv", "md", "xlsx"}
	case FormatXLSXJSON:
		return []string{"md", "json", "xlsx"}
	case FormatTXT:
		return []string{"txt", "md"}
	case FormatDOCX:
		return []string{"md", "txt", "docx"}
	case FormatPPTX:
		return []string{"md", "txt", "pptx"}
	case FormatPDF:
		// The PDF same-format copy is EXPERIMENTAL:
		// a regenerated simplified layout, never the original design.
		return []string{"md", "txt", "pdf"}
	default: // md → markdown (or plain text)
		return []string{"md", "txt"}
	}
}

// SameFormatExtensions lists the extensions served by the same-format
// rewriter rather than ExportBytes.
var SameFormatExtensions = map[string]bool{"docx": true, "pptx": true, "xlsx": true, "pdf": true}

// ExportBytes renders one anonymised document in the requested extension.
// Same-format extensions are served by engine/exportfmt (they need the
// original bytes and the registry), never by this text path.
func ExportBytes(rd ResultDocument, ext string) ([]byte, error) {
	if SameFormatExtensions[ext] {
		return nil, fmt.Errorf(
			"the .%s copy is a same-format export; use the same-format button (app side: SaveDocument routes it), not the text export path", ext)
	}
	allowed := false
	for _, e := range ExportExtensions(rd.Format) {
		if e == ext {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf(
			"documents imported as %s cannot be exported as .%s, offered formats: %s",
			rd.Format, ext, strings.Join(ExportExtensions(rd.Format), ", "))
	}
	switch ext {
	case "csv":
		// The anonymised Grid is the source of truth for CSV round-trip
		// (pipeline guarantee: grid and markdown never disagree).
		return GridToCSV(rd.Grid)
	case "json":
		return []byte(rd.JSON), nil
	default: // "md" and "txt" both emit the anonymised working form
		return []byte(rd.Anonymised), nil
	}
}

// ExportFileName builds "<base>_anon.<ext>" from the document name,
// sanitising the '#' used by per-sheet xlsx names ('#' is illegal in
// Windows filenames). Examples:
//
//	"notes.txt"              + "txt" → "notes_anon.txt"
//	"workbook.xlsx#Clients"  + "csv" → "workbook.xlsx_Clients_anon.csv"
func ExportFileName(docName, ext string) string {
	name := strings.ReplaceAll(docName, "#", "_")
	// Strip the original extension only when the name actually ends in
	// one (a short alphanumeric suffix). Sheet names like
	// "workbook.xlsx_Clients" must stay intact.
	if i := strings.LastIndex(name, "."); i > 0 {
		suffix := name[i+1:]
		if len(suffix) >= 1 && len(suffix) <= 4 && isAlnum(suffix) {
			name = name[:i]
		}
	}
	return name + "_anon." + ext
}

// SameFormatFileName proposes an anonymised export filename
// Phase 12d): the base name goes through the registry mapping
// (case-insensitive, longest original first, same code as the
// post-pass), the '#' of xlsx sheet names is sanitised as usual, and if
// the result STILL contains a known original the proposal falls back to
// "document_anon_N.<ext>" rather than leaking a name. The user can edit
// the proposal in the review panel and again in the save dialog.
func SameFormatFileName(docName, ext string, entries []MappingEntry, fallbackN int) string {
	name := strings.ReplaceAll(docName, "#", "_")
	if i := strings.LastIndex(name, "."); i > 0 {
		suffix := name[i+1:]
		if len(suffix) >= 1 && len(suffix) <= 4 && isAlnum(suffix) {
			name = name[:i]
		}
	}
	for _, e := range entries { // Entries() order: longest original first
		name = replaceKnownOriginal(name, e, func() {})
	}
	// Leak check: if any known original survives (e.g. glued into a
	// longer word so the boundary rule skipped it), use the generic name.
	lower := strings.ToLower(name)
	for _, e := range entries {
		if strings.Contains(lower, strings.ToLower(e.Original)) {
			return fmt.Sprintf("document_anon_%d.%s", fallbackN, ext)
		}
	}
	// Placeholder brackets are legal in Windows filenames; underscores
	// keep the result tidy anyway.
	name = strings.ReplaceAll(strings.ReplaceAll(name, "[", ""), "]", "")
	return name + "_anon." + ext
}

// isAlnum reports whether s is ASCII letters/digits only.
func isAlnum(s string) bool {
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// BuildExportZip packs every anonymised document into a single zip using
// each document's DEFAULT export format.
func BuildExportZip(results *Results) ([]byte, error) {
	if results == nil || len(results.Documents) == 0 {
		return nil, fmt.Errorf("there are no results to export, run the pipeline first")
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, rd := range results.Documents {
		ext := ExportExtensions(rd.Format)[0]
		data, err := ExportBytes(rd, ext)
		if err != nil {
			return nil, err
		}
		fw, err := w.Create(ExportFileName(rd.Name, ext))
		if err != nil {
			return nil, fmt.Errorf("could not add %q to the zip: %w", rd.Name, err)
		}
		if _, err := fw.Write(data); err != nil {
			return nil, fmt.Errorf("could not write %q into the zip: %w", rd.Name, err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("could not finalise the zip: %w", err)
	}
	return buf.Bytes(), nil
}

// MappingToCSV renders the registry export as CSV (original, placeholder,
// category, count) — the re-identification key (handle with the same care
// as the session file).
func MappingToCSV(entries []MappingEntry) ([]byte, error) {
	grid := [][]string{{"original", "placeholder", "category", "count"}}
	for _, e := range entries {
		grid = append(grid, []string{e.Original, e.Placeholder, e.Category, fmt.Sprintf("%d", e.Count)})
	}
	return GridToCSV(grid)
}

// lowered is a tiny helper shared with session.go (strings.ToLower kept in
// one spot for the registry key convention).
func lowered(s string) string {
	return strings.ToLower(s)
}

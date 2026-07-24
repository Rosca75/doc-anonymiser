// engine/export.go — egress helpers for Phase 9 (CLAUDE.md §5: every
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
// (BUILD.md Phase 9 activity 1). The first entry is the default.
func ExportExtensions(f Format) []string {
	switch f {
	case FormatCSV, FormatXLSX: // grid documents round-trip to CSV
		return []string{"csv", "md"}
	case FormatTXT:
		return []string{"txt", "md"}
	case FormatXLSXJSON:
		return []string{"md", "json"}
	default: // md, docx, pptx, pdf → markdown (or plain text)
		return []string{"md", "txt"}
	}
}

// ExportBytes renders one anonymised document in the requested extension.
func ExportBytes(rd ResultDocument, ext string) ([]byte, error) {
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
// each document's DEFAULT export format (BUILD.md Phase 9 activity 2).
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

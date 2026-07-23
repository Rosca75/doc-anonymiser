// engine/session_test.go — Phase 9 tests: zip contents, CSV round-trip
// export equals the anonymised grid, session save→load equality, and the
// mapping export golden file.
package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

// runExportFixture runs a tiny two-document pipeline (one text, one CSV)
// shared by the export tests.
func runExportFixture(t *testing.T) (*Results, *Registry) {
	t.Helper()
	csvDoc, err := Load("clients.csv", []byte("name,email\nMarie Duval,marie.duval@example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	res, err := Run(context.Background(), PipelineInput{
		Documents: []Document{
			{Name: "notes.txt", Format: FormatTXT, Markdown: "Alpine Trust wrote to marie.duval@example.com."},
			csvDoc,
		},
		Entities:  []Entity{{Category: "client_names", Canonical: "Alpine Trust"}},
		Level:     LevelMedium,
		Allowlist: NewEmptyAllowlist(),
		Registry:  reg,
	})
	if err != nil {
		t.Fatal(err)
	}
	return res, reg
}

func TestBuildExportZipContents(t *testing.T) {
	res, _ := runExportFixture(t)
	data, err := BuildExportZip(res)
	if err != nil {
		t.Fatalf("BuildExportZip: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip unreadable: %v", err)
	}

	got := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = string(content)
	}

	// Default formats: txt for the text doc, csv for the grid doc; names
	// carry the _anon suffix.
	if !strings.Contains(got["notes_anon.txt"], "[CLIENT_1]") {
		t.Errorf("notes_anon.txt missing or not anonymised: %v", got)
	}
	if !strings.Contains(got["clients_anon.csv"], "[EMAIL_1]") {
		t.Errorf("clients_anon.csv missing or not anonymised: %v", got)
	}
	// The empty-results guard must be actionable.
	if _, err := BuildExportZip(&Results{}); err == nil {
		t.Error("empty results must be rejected")
	}
}

func TestCSVExportEqualsAnonymisedGrid(t *testing.T) {
	res, _ := runExportFixture(t)
	var csvDoc *ResultDocument
	for i := range res.Documents {
		if res.Documents[i].Format == FormatCSV {
			csvDoc = &res.Documents[i]
		}
	}
	out, err := ExportBytes(*csvDoc, "csv")
	if err != nil {
		t.Fatalf("ExportBytes: %v", err)
	}
	// Re-parse: every cell must equal the anonymised grid exactly.
	grid, _, err := ParseCSV(out)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	for r := range csvDoc.Grid {
		for c := range csvDoc.Grid[r] {
			if grid[r][c] != csvDoc.Grid[r][c] {
				t.Errorf("cell [%d][%d]: exported %q != grid %q", r, c, grid[r][c], csvDoc.Grid[r][c])
			}
		}
	}
	// A format the document does not offer must be refused actionably.
	if _, err := ExportBytes(*csvDoc, "json"); err == nil {
		t.Error("csv document exported as json?!")
	}
}

func TestSessionSaveLoadEquality(t *testing.T) {
	_, reg := runExportFixture(t)
	original := Session{
		Entities:    []Entity{{Category: "client_names", Canonical: "Alpine Trust", ManualVariants: []string{"Alpine"}}},
		AllowTerms:  []string{"CSSF", "Luxembourg"},
		Patterns:    []CustomPattern{{Expr: "PRJ-[0-9]+"}},
		SimpleRules: []SimpleRule{{Find: "x", Replace: "y", CaseSensitive: true}},
		Settings:    SessionSettings{Level: "advanced", OllamaPort: 12345, Model: "qwen2.5:3b-instruct"},
		Registry:    reg.Export(),
	}
	raw, err := SaveSession(original)
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	loaded, err := LoadSession(raw)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	// Round-trip equality (version is stamped by Save).
	original.Version = SessionVersion
	rawAgain, err := SaveSession(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(rawAgain) {
		t.Errorf("session save→load→save changed bytes:\n%s\nvs\n%s", raw, rawAgain)
	}

	// Registry restore: existing mappings keep their placeholders and new
	// assignments continue the numbering (no collisions).
	restored := NewRegistryFromEntries(loaded.Registry)
	if p, ok := restored.Lookup(CatEmail, "marie.duval@example.com"); !ok || p != "[EMAIL_1]" {
		t.Errorf("restored registry lost the email mapping: %q %v", p, ok)
	}
	if p := restored.Assign(CatEmail, "new@example.org"); p != "[EMAIL_2]" {
		t.Errorf("numbering must continue after restore, got %q", p)
	}

	// Corrupt and wrong-version files fail actionably.
	if _, err := LoadSession([]byte("not json")); err == nil || !strings.Contains(err.Error(), "session file") {
		t.Errorf("corrupt session error not actionable: %v", err)
	}
	bad, _ := SaveSession(original)
	badStr := strings.Replace(string(bad), `"version": 1`, `"version": 99`, 1)
	if _, err := LoadSession([]byte(badStr)); err == nil || !strings.Contains(err.Error(), "version") {
		t.Errorf("version mismatch error not actionable: %v", err)
	}
}

func TestMappingExportGolden(t *testing.T) {
	reg := NewRegistry()
	reg.Assign(CatEmail, "marie.duval@example.com")
	reg.Assign("client_names", "Alpine Trust")
	reg.Assign(CatEmail, "marie.duval@example.com") // second occurrence

	out, err := MappingToCSV(reg.Export())
	if err != nil {
		t.Fatalf("MappingToCSV: %v", err)
	}
	want := "original,placeholder,category,count\n" +
		"Alpine Trust,[CLIENT_1],client_names,1\n" +
		"marie.duval@example.com,[EMAIL_1],email,2\n"
	if string(out) != want {
		t.Errorf("mapping CSV golden mismatch:\n--- got ---\n%s--- want ---\n%s", out, want)
	}
}

func TestExportFileName(t *testing.T) {
	tests := [][3]string{
		{"notes.txt", "txt", "notes_anon.txt"},
		{"report.docx", "md", "report_anon.md"},
		{"workbook.xlsx#Clients", "csv", "workbook.xlsx_Clients_anon.csv"},
	}
	for _, tt := range tests {
		if got := ExportFileName(tt[0], tt[1]); got != tt[2] {
			t.Errorf("ExportFileName(%q,%q) = %q, want %q", tt[0], tt[1], got, tt[2])
		}
	}
}

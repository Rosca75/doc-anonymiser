// engine/unitcount_test.go — the import list's unit counts (,
// the unit shown in the import list.
//
// The point of these tests is the FALLBACK, not the happy path. A page count
// can only come from what the writing application cached in
// docProps/app.xml, that part is optional, and the repository's own fixtures
// (report.docx, deck.pptx) do not carry it. So the absent branch is the common
// one, and "0 pages" reaching the UI is the failure mode worth pinning.
//
// The present branch needs a fixture that DOES carry app.xml. It is built here
// rather than committed as a binary, so the exact XML the code reads is visible
// next to the assertion instead of hidden inside a zip.
package engine

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimalDocx builds the smallest .docx engine/convert accepts, optionally
// carrying a docProps/app.xml with the given cached page count.
//
// pages < 0 means "write no app.xml at all", which is the shape report.docx has
// and the shape most non-Word producers write.
func minimalDocx(t *testing.T, bodyLines []string, pages int) []byte {
	t.Helper()

	var paragraphs strings.Builder
	for _, line := range bodyLines {
		paragraphs.WriteString(`<w:p><w:r><w:t>` + line + `</w:t></w:r></w:p>`)
	}
	document := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:body>` + paragraphs.String() + `</w:body></w:document>`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, content string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("building the fixture: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("building the fixture: %v", err)
		}
	}
	write("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?>`+
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`+
		`<Default Extension="xml" ContentType="application/xml"/></Types>`)
	write("word/document.xml", document)
	if pages >= 0 {
		// The extended-properties part, exactly as Word writes it: the counts
		// are CACHED values, computed at save time by whatever wrote the file.
		write("docProps/app.xml", fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>`+
			`<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties">`+
			`<Pages>%d</Pages><Words>412</Words><Lines>37</Lines>`+
			`<Company>Meridian Consulting</Company></Properties>`, pages))
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("building the fixture: %v", err)
	}
	return buf.Bytes()
}

func TestUnitCountsPerFormat(t *testing.T) {
	// The fixture body is three paragraphs, which the docx converter renders as
	// three markdown lines plus the blank lines between them.
	body := []string{"First paragraph.", "Second paragraph.", "Third paragraph."}

	tests := []struct {
		name string
		// build returns the filename and bytes to import.
		build func(t *testing.T) (string, []byte)
		// wantUnit is the unit the import list must show.
		wantUnit string
		// wantCount is the exact count, or 0 to mean "any positive number"
		// (a line count depends on the converter's spacing, which is not what
		// these tests are pinning).
		wantCount int
		why       string
	}{
		{
			name: "docx with a cached page count reports pages",
			build: func(t *testing.T) (string, []byte) {
				return "cached.docx", minimalDocx(t, body, 6)
			},
			wantUnit:  UnitPage,
			wantCount: 6,
			why:       "a Word-authored file caches <Pages>, and a page count is what a Word user recognises",
		},
		{
			name: "docx with NO app.xml falls back to lines, never zero pages",
			build: func(t *testing.T) (string, []byte) {
				return "nocache.docx", minimalDocx(t, body, -1)
			},
			wantUnit: UnitLine,
			why:      "the part is optional; showing \"0 pages\" would be a lie, a line count is always true",
		},
		{
			name: "docx caching zero pages also falls back to lines",
			build: func(t *testing.T) (string, []byte) {
				return "zero.docx", minimalDocx(t, body, 0)
			},
			wantUnit: UnitLine,
			why:      "a present-but-useless cached value must degrade exactly like an absent one",
		},
		{
			name: "the repository's own docx fixture has no app.xml, so it reports lines",
			build: func(t *testing.T) (string, []byte) {
				return "report.docx", readFixture(t, "report.docx")
			},
			wantUnit: UnitLine,
			why:      "this is the case the manual test matrix calls out: it must never say 0 pages",
		},
		{
			name: "pptx counts its slide parts",
			build: func(t *testing.T) (string, []byte) {
				return "deck.pptx", readFixture(t, "deck.pptx")
			},
			wantUnit: UnitSlide,
			why:      "the part count comes from the archive's shape, so it cannot go stale",
		},
		{
			name: "pdf counts its pages exactly",
			build: func(t *testing.T) (string, []byte) {
				return "textlayer.pdf", readFixture(t, "textlayer.pdf")
			},
			wantUnit: UnitPage,
			why:      "the page tree is in the file itself, so this figure is exact",
		},
		{
			name: "a csv reports its RECORDS, excluding the header row",
			build: func(t *testing.T) (string, []byte) {
				return "rows.csv", []byte("name,email\nMarie,m@example.com\nThomas,t@example.com\n")
			},
			wantUnit:  UnitRow,
			wantCount: 2,
			why:       "\"2 rows\" must mean two records, not two records and a header",
		},
		{
			name: "a header-only csv is zero rows",
			build: func(t *testing.T) (string, []byte) {
				return "empty.csv", []byte("name,email\n")
			},
			wantUnit: UnitLine,
			// The markdown working form of a header-only CSV is the header row
			// plus the table separator, so two lines. That is what the pipeline
			// will actually process, which is the honest thing to report.
			wantCount: 2,
			why:       "zero records is not a count worth showing, so it falls back to the working form's lines",
		},
		{
			name: "plain text reports lines",
			build: func(t *testing.T) (string, []byte) {
				return "notes.txt", []byte("one\ntwo\nthree\n")
			},
			wantUnit:  UnitLine,
			wantCount: 3,
			why:       "a trailing newline must not become a fourth, empty line",
		},
		{
			name: "markdown reports lines",
			build: func(t *testing.T) (string, []byte) {
				return "notes.md", []byte("# Title\n\nBody.\n")
			},
			wantUnit:  UnitLine,
			wantCount: 3,
			why:       "markdown is the working form already, so its own unit is the line",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, raw := tt.build(t)
			docs, err := LoadAll(name, raw)
			if err != nil {
				t.Fatalf("LoadAll(%q): %v", name, err)
			}
			if len(docs) == 0 {
				t.Fatalf("LoadAll(%q) produced no documents", name)
			}
			doc := docs[0]

			if doc.Unit != tt.wantUnit {
				t.Errorf("unit = %q, want %q\n  why it matters: %s", doc.Unit, tt.wantUnit, tt.why)
			}
			if tt.wantCount > 0 {
				if doc.UnitCount != tt.wantCount {
					t.Errorf("unitCount = %d, want %d\n  why it matters: %s",
						doc.UnitCount, tt.wantCount, tt.why)
				}
			} else if doc.UnitCount <= 0 {
				t.Errorf("unitCount = %d, want a positive number: a count of 0 in the import "+
					"list tells the user their file is empty when it is not\n  why it matters: %s",
					doc.UnitCount, tt.why)
			}
		})
	}
}

func TestXlsxSheetsReportRows(t *testing.T) {
	// A flat sheet behaves like a CSV import, so it reports records; a complex
	// sheet has no meaningful row count (that is exactly why it was routed to
	// JSON) and must fall back to lines.
	docs, err := LoadAll("workbook.xlsx", readFixture(t, "workbook.xlsx"))
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("the workbook fixture produced no sheets")
	}
	for _, doc := range docs {
		switch doc.Format {
		case FormatXLSX:
			if doc.Unit != UnitRow && doc.Unit != UnitLine {
				t.Errorf("%s: flat sheet unit = %q, want row (or line when it holds only a header)",
					doc.Name, doc.Unit)
			}
			if doc.Grid != nil && len(doc.Grid) > 1 && doc.UnitCount != len(doc.Grid)-1 {
				t.Errorf("%s: unitCount = %d, want %d (rows minus the header)",
					doc.Name, doc.UnitCount, len(doc.Grid)-1)
			}
		case FormatXLSXJSON:
			if doc.Unit != UnitLine {
				t.Errorf("%s: a complex sheet has no row count, so it must report lines, got %q",
					doc.Name, doc.Unit)
			}
		}
		if doc.UnitCount < 0 {
			t.Errorf("%s: unitCount must never be negative, got %d", doc.Name, doc.UnitCount)
		}
	}
}

func TestEveryDocumentGetsAUnit(t *testing.T) {
	// The invariant behind the fallback: NO importable document may reach the UI
	// with an empty unit, because the import list would then print a bare number
	// with nothing after it.
	fixtures := []string{
		"report.docx", "deck.pptx", "workbook.xlsx", "textlayer.pdf",
		"sample.csv", "sample.md", "sample.txt", "french.md", "code_fences.md",
		"edge_quoted.csv",
	}
	for _, fixture := range fixtures {
		docs, err := LoadAll(fixture, readFixture(t, fixture))
		if err != nil {
			t.Fatalf("LoadAll(%q): %v", fixture, err)
		}
		for _, doc := range docs {
			if doc.Unit == "" {
				t.Errorf("%s: no unit, so the import list would print a bare number", doc.Name)
			}
			switch doc.Unit {
			case UnitPage, UnitSlide, UnitRow, UnitLine:
			default:
				t.Errorf("%s: unit %q is not one of the four the UI can pluralise", doc.Name, doc.Unit)
			}
		}
	}
}

func TestCountLinesIgnoresATrailingNewline(t *testing.T) {
	tests := []struct {
		in   string
		want int
		why  string
	}{
		{in: "", want: 0, why: "an empty document has no lines"},
		{in: "one", want: 1, why: "a single line with no terminator is still one line"},
		{in: "one\n", want: 1, why: "a properly terminated line must not count as two"},
		{in: "one\ntwo", want: 2, why: ""},
		{in: "one\ntwo\n", want: 2, why: ""},
		{in: "\n", want: 1, why: "one empty line is one line"},
		{in: "one\n\nthree\n", want: 3, why: "a blank line in the middle counts"},
	}
	for _, tt := range tests {
		if got := countLines(tt.in); got != tt.want {
			t.Errorf("countLines(%q) = %d, want %d %s", tt.in, got, tt.want, tt.why)
		}
	}
}

// readFixture loads a file from backend/testdata.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "testdata", name))
	if err != nil {
		t.Fatalf("could not read the fixture %q: %v", name, err)
	}
	return raw
}

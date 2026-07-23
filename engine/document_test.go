// Table-driven tests for document ingestion (engine/document.go), covering
// the three supported formats plus rejection of an unsupported one, per
// CLAUDE.md §6 ("table-driven unit tests for all engine logic").
package engine

import (
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name string // test-case label shown on failure

		fileName string // simulated filename (drives format detection)
		raw      string // simulated file content

		wantErr        bool   // true when Load must reject the file
		wantFormat     Format // expected detected format
		wantInMarkdown string // substring that must appear in .Markdown
		wantGridRows   int    // expected number of grid rows (CSV only; 0 = expect nil grid)
	}{
		{
			name:       "txt is passed through with CRLF normalised",
			fileName:   "notes.txt",
			raw:        "line one\r\nline two\r\n",
			wantFormat: FormatTXT,
			// If normalisation works, no carriage returns survive and
			// both lines are joined by plain \n.
			wantInMarkdown: "line one\nline two\n",
		},
		{
			name:           "md is passed through as-is",
			fileName:       "report.md",
			raw:            "# Heading\n\nSome **bold** text.\n",
			wantFormat:     FormatMD,
			wantInMarkdown: "# Heading",
		},
		{
			name:       "csv becomes a markdown table and keeps its grid",
			fileName:   "clients.csv",
			raw:        "name,email\nMarie Duval,marie.duval@example.com\n",
			wantFormat: FormatCSV,
			// The header row must be rendered as a markdown table row.
			wantInMarkdown: "| name | email |",
			wantGridRows:   2, // header + one data row
		},
		{
			name:     "docx is rejected with an actionable message",
			fileName: "contract.docx",
			raw:      "irrelevant",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Load(tt.fileName, []byte(tt.raw))

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Load(%q) succeeded, want an unsupported-format error", tt.fileName)
				}
				// The rejection must tell the user what IS supported.
				if !strings.Contains(err.Error(), ".txt") {
					t.Errorf("rejection error should mention the supported formats, got: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Load(%q) returned unexpected error: %v", tt.fileName, err)
			}
			if doc.Format != tt.wantFormat {
				t.Errorf("Format = %q, want %q", doc.Format, tt.wantFormat)
			}
			if doc.Name != tt.fileName {
				t.Errorf("Name = %q, want %q", doc.Name, tt.fileName)
			}
			// Raw must be the untouched original (immutability guarantee).
			if string(doc.Raw) != tt.raw {
				t.Errorf("Raw was modified: got %q, want original %q", doc.Raw, tt.raw)
			}
			if !strings.Contains(doc.Markdown, tt.wantInMarkdown) {
				t.Errorf("Markdown does not contain %q; full markdown:\n%s", tt.wantInMarkdown, doc.Markdown)
			}
			if tt.wantGridRows > 0 {
				if len(doc.Grid) != tt.wantGridRows {
					t.Errorf("Grid has %d rows, want %d", len(doc.Grid), tt.wantGridRows)
				}
			} else if doc.Grid != nil {
				t.Errorf("Grid should be nil for %s documents, got %v", doc.Format, doc.Grid)
			}
		})
	}
}

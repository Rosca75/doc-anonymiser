// Table-driven tests for document ingestion (engine/document.go), covering
// the three supported formats plus rejection of an unsupported one, per
// CLAUDE.md §6 ("table-driven unit tests for all engine logic").
package engine

import (
	"os"
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
			name:     "exe is rejected with an actionable message",
			fileName: "malware.exe",
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

// TestLoadUTF8Validation pins the actionable rejection of non-UTF-8 input
// (BUILD.md Phase 1: "invalid UTF-8 fails with the expected error message
// fragment"). The byte 0xE9 alone is 'é' in Latin-1 but invalid UTF-8.
func TestLoadUTF8Validation(t *testing.T) {
	_, err := Load("legacy.txt", []byte{'c', 'a', 'f', 0xE9})
	if err == nil {
		t.Fatal("Load accepted invalid UTF-8, want rejection")
	}
	for _, fragment := range []string{"legacy.txt", "UTF-8", "re-save"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("error should contain %q for actionability, got: %v", fragment, err)
		}
	}
}

// TestLoadWarnings covers the non-fatal warning paths: empty file and
// very large file (> LargeFileThreshold). Both must still produce a
// usable Document.
func TestLoadWarnings(t *testing.T) {
	t.Run("empty file warns but loads", func(t *testing.T) {
		doc, err := Load("empty.txt", nil)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(doc.Warnings) != 1 || !strings.Contains(doc.Warnings[0], "empty") {
			t.Errorf("want a single 'empty' warning, got %v", doc.Warnings)
		}
	})
	t.Run("very large file warns but loads", func(t *testing.T) {
		big := strings.Repeat("all work and no play makes jack a dull boy\n", (LargeFileThreshold/43)+1)
		doc, err := Load("big.txt", []byte(big))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(doc.Warnings) != 1 || !strings.Contains(doc.Warnings[0], "large") {
			t.Errorf("want a single 'large' warning, got %v", doc.Warnings)
		}
	})
	t.Run("ragged CSV warning reaches the Document", func(t *testing.T) {
		doc, err := Load("ragged.csv", []byte("a,b,c\n1,2\n"))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(doc.Warnings) != 1 || !strings.Contains(doc.Warnings[0], "padded") {
			t.Errorf("want the ragged-row warning, got %v", doc.Warnings)
		}
	})
}

// TestFixtureCodeFences pins today's v1 behaviour for markdown code fences:
// fence content is ordinary text (no special casing — BUILD.md Phase 1
// activity 4). If a future change starts skipping fences, this test forces
// that to be a conscious decision.
func TestFixtureCodeFences(t *testing.T) {
	raw, err := os.ReadFile("../testdata/code_fences.md")
	if err != nil {
		t.Fatalf("fixture missing: %v", err)
	}
	doc, err := Load("code_fences.md", raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// The fence markers AND the fence content are both present verbatim
	// in the working form — nothing is stripped or skipped.
	for _, want := range []string{"```python", "john.farwell@example.com", "Alpine Trust"} {
		if !strings.Contains(doc.Markdown, want) {
			t.Errorf("markdown working form lost %q", want)
		}
	}
}

// TestFixtureFrench proves accented UTF-8 text survives ingestion intact
// (French fixtures are required by CLAUDE.md §6).
func TestFixtureFrench(t *testing.T) {
	raw, err := os.ReadFile("../testdata/french.md")
	if err != nil {
		t.Fatalf("fixture missing: %v", err)
	}
	doc, err := Load("french.md", raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, want := range []string{"Château d'Ambre", "Amélie Lefèvre", "jusqu'à"} {
		if !strings.Contains(doc.Markdown, want) {
			t.Errorf("accented text lost: %q missing", want)
		}
	}
}

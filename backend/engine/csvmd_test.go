// Table-driven tests for engine/csvmd.go: CSV parsing (incl. quoted fields,
// embedded newlines, ragged-row repair), markdown-table rendering, and the
// CSV round-trip guarantee (CLAUDE.md §5, BUILD.md Phase 1).
package engine

import (
	"os"
	"strings"
	"testing"
)

func TestParseCSV(t *testing.T) {
	tests := []struct {
		name string

		raw string

		wantErr      bool
		wantRows     int
		wantCols     int // expected width of EVERY row (grid is rectangular)
		wantWarnings int
		wantCell     [3]interface{} // {row, col, expected string} spot-check; nil = skip
	}{
		{
			name:     "simple two-column file",
			raw:      "name,email\nMarie Duval,marie.duval@example.com\n",
			wantRows: 2,
			wantCols: 2,
			wantCell: [3]interface{}{1, 0, "Marie Duval"},
		},
		{
			name:     "quoted field with embedded newline and comma survives",
			raw:      "id,comment\n1,\"line one\nline two, still cell\"\n",
			wantRows: 2,
			wantCols: 2,
			wantCell: [3]interface{}{1, 1, "line one\nline two, still cell"},
		},
		{
			name:     "escaped double quotes inside a quoted field",
			raw:      "id,comment\n2,\"He said \"\"hello\"\"\"\n",
			wantRows: 2,
			wantCols: 2,
			wantCell: [3]interface{}{1, 1, `He said "hello"`},
		},
		{
			name:         "ragged rows are padded and produce one warning",
			raw:          "a,b,c\n1,2\nonly\n",
			wantRows:     3,
			wantCols:     3,
			wantWarnings: 1,
			// The padded cells must be empty strings, not missing.
			wantCell: [3]interface{}{2, 2, ""},
		},
		{
			name:    "empty file is an error",
			raw:     "",
			wantErr: true,
		},
		{
			name:    "bare quote mid-field is a parse error",
			raw:     "a,b\n1,say \"hi there\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grid, warnings, err := ParseCSV([]byte(tt.raw))

			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseCSV succeeded, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCSV returned unexpected error: %v", err)
			}
			if len(grid) != tt.wantRows {
				t.Fatalf("got %d rows, want %d", len(grid), tt.wantRows)
			}
			// Rectangularity guarantee: every row has the same width.
			for i, row := range grid {
				if len(row) != tt.wantCols {
					t.Errorf("row %d has %d cols, want %d (grid must be rectangular)", i, len(row), tt.wantCols)
				}
			}
			if len(warnings) != tt.wantWarnings {
				t.Errorf("got %d warnings %v, want %d", len(warnings), warnings, tt.wantWarnings)
			}
			if tt.wantCell[2] != nil {
				r, c, want := tt.wantCell[0].(int), tt.wantCell[1].(int), tt.wantCell[2].(string)
				if grid[r][c] != want {
					t.Errorf("cell [%d][%d] = %q, want %q", r, c, grid[r][c], want)
				}
			}
		})
	}
}

func TestGridToMarkdownTable(t *testing.T) {
	tests := []struct {
		name string
		grid [][]string
		want string
	}{
		{
			name: "basic header plus one row",
			grid: [][]string{{"name", "email"}, {"Ana", "ana@example.net"}},
			want: "| name | email |\n| --- | --- |\n| Ana | ana@example.net |\n",
		},
		{
			name: "pipes are escaped and newlines flattened",
			grid: [][]string{{"col"}, {"a|b\nc"}},
			want: "| col |\n| --- |\n| a\\|b c |\n",
		},
		{
			name: "empty grid renders nothing",
			grid: nil,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GridToMarkdownTable(tt.grid); got != tt.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

// TestCSVRoundTrip pins the export guarantee: for well-formed input
// (rectangular, LF endings, minimal quoting) parse → serialise reproduces
// the original bytes exactly. Quoted fields with embedded newlines/commas
// must survive because encoding/csv re-quotes them on output.
func TestCSVRoundTrip(t *testing.T) {
	inputs := []struct {
		name string
		raw  string
	}{
		{"plain", "name,email\nMarie Duval,marie.duval@example.com\n"},
		{"quoted newline and comma", "id,comment\n1,\"line one\nline two, still cell\"\n"},
		{"escaped quotes", "id,comment\n2,\"He said \"\"hello\"\"\"\n"},
	}
	for _, tt := range inputs {
		t.Run(tt.name, func(t *testing.T) {
			grid, _, err := ParseCSV([]byte(tt.raw))
			if err != nil {
				t.Fatalf("ParseCSV: %v", err)
			}
			out, err := GridToCSV(grid)
			if err != nil {
				t.Fatalf("GridToCSV: %v", err)
			}
			if string(out) != tt.raw {
				t.Errorf("round-trip changed bytes:\n in: %q\nout: %q", tt.raw, out)
			}
		})
	}
}

// TestFixtureEdgeQuotedCSV loads the committed edge-case fixture through the
// full Load path, proving the quoted-newline handling end to end and pinning
// the round-trip on a real file.
func TestFixtureEdgeQuotedCSV(t *testing.T) {
	raw, err := os.ReadFile("../testdata/edge_quoted.csv")
	if err != nil {
		t.Fatalf("fixture missing: %v", err)
	}
	doc, err := Load("edge_quoted.csv", raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(doc.Grid) != 3 { // header + 2 data rows
		t.Fatalf("got %d rows, want 3", len(doc.Grid))
	}
	if want := "Multi-line note:\nsecond line, with comma"; doc.Grid[1][1] != want {
		t.Errorf("embedded newline lost: %q", doc.Grid[1][1])
	}
	// The markdown preview must flatten the newline so the table stays valid.
	if strings.Contains(doc.Markdown, "Multi-line note:\nsecond") {
		t.Errorf("markdown table contains a raw newline inside a cell:\n%s", doc.Markdown)
	}
	out, err := GridToCSV(doc.Grid)
	if err != nil {
		t.Fatalf("GridToCSV: %v", err)
	}
	if string(out) != string(raw) {
		t.Errorf("fixture round-trip changed bytes:\n in: %q\nout: %q", raw, out)
	}
}

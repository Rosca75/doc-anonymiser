// engine/allowlist_test.go —  tests: tolerant allowlist
// CSV parsing, the downloadable template, and the surfaced seed defaults.
package engine

import (
	"strings"
	"testing"
)

func TestParseAllowlistCSV(t *testing.T) {
	cases := []struct {
		name    string
		data    string
		want    []string
		wantErr string // fragment of the expected error ("" = no error)
	}{
		{
			name: "header row with term column",
			data: "term\nCSSF\nIFRS 17\n",
			want: []string{"CSSF", "IFRS 17"},
		},
		{
			name: "no header, first column",
			data: "CSSF\nECB\n",
			want: []string{"CSSF", "ECB"},
		},
		{
			name: "term column not first",
			data: "note,term\nregulator,CSSF\nstandard,IFRS 17\n",
			want: []string{"CSSF", "IFRS 17"},
		},
		{
			name: "UTF-8 BOM and CRLF",
			data: "\xEF\xBB\xBFterm\r\nCSSF\r\n",
			want: []string{"CSSF"},
		},
		{
			name: "quoted terms with embedded commas",
			data: "term\n\"Basel III, revised\"\nCSSF\n",
			want: []string{"Basel III, revised", "CSSF"},
		},
		{
			name:    "semicolon-delimited rejected with actionable message",
			data:    "CSSF;regulator\nECB;central bank\n",
			wantErr: "semicolon",
		},
		{
			name: "case-insensitive duplicate collapse keeps first spelling",
			data: "term\nCSSF\ncssf\nCssf\nECB\n",
			want: []string{"CSSF", "ECB"},
		},
		{
			name: "whitespace trimmed and empties dropped",
			data: "term\n  CSSF  \n\n   \nECB\n",
			want: []string{"CSSF", "ECB"},
		},
		{
			name: "empty file is an empty list, not an error",
			data: "",
			want: []string{},
		},
		{
			name:    "unbalanced quote names the bad line",
			data:    "term\nCSSF\n\"broken\nECB\n",
			wantErr: "line",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAllowlistCSV([]byte(tc.data))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v (terms %v)", tc.wantErr, err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("terms = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("term[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestAllowlistTemplateRoundTrip: the shipped template must parse through
// its own parser and yield the three example terms.
func TestAllowlistTemplateRoundTrip(t *testing.T) {
	terms, err := ParseAllowlistCSV(AllowlistTemplateCSV())
	if err != nil {
		t.Fatalf("the template must parse cleanly: %v", err)
	}
	want := []string{"CSSF", "IFRS 17", "Luxembourg"}
	if len(terms) != len(want) {
		t.Fatalf("template terms = %v, want %v", terms, want)
	}
	for i := range want {
		if terms[i] != want[i] {
			t.Errorf("template term[%d] = %q, want %q", i, terms[i], want[i])
		}
	}
}

// TestDefaultAllowlistTerms: the seed is surfaced (non-empty, sorted,
// contains the documented regulators) and returns a COPY (mutating the
// result must not corrupt the seed).
func TestDefaultAllowlistTerms(t *testing.T) {
	terms := DefaultAllowlistTerms()
	if len(terms) == 0 {
		t.Fatal("the seeded defaults must be surfaced")
	}
	found := false
	for _, term := range terms {
		if term == "CSSF" {
			found = true
		}
	}
	if !found {
		t.Error("the seed must contain CSSF")
	}
	terms[0] = "MUTATED"
	if DefaultAllowlistTerms()[0] == "MUTATED" {
		t.Error("DefaultAllowlistTerms must return a copy, not the seed slice")
	}
}

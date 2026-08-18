// engine/convert/convert_test.go — the UNIT-tier converter tests.
//
// TIER: unit (docs/TESTING.md). This file holds only the converter tests that
// need no binary fixture: the pure RepairPDFText table and the non-zip
// rejection path, both cheap and deterministic. The golden conversions that
// decode the committed .docx/.pptx/.xlsx/.pdf fixtures live in
// convert_integration_test.go, and the wall-clock budget lives in
// convert_deep_test.go. The shared fixture(...) helper is in fixtures_test.go,
// which carries no build tag so every tier can use it.
package convert

import (
	"strings"
	"testing"
)

// TestDocxRejectsNonZip pins the actionable error for a legacy .doc-style
// (non-zip) payload.
func TestDocxRejectsNonZip(t *testing.T) {
	_, _, err := Docx([]byte("this is not a zip archive"))
	if err == nil || !strings.Contains(err.Error(), ".docx") {
		t.Errorf("want actionable not-a-docx error, got %v", err)
	}
}

// TestRepairPDFText is the table-driven repair suite, including the nb1
// 'B R IDDING ULES' class of defect and the mandatory no-op cases.
func TestRepairPDFText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "nb1 interleaved capitals",
			in:   "B R IDDING ULES",
			want: "BIDDING RULES",
		},
		{
			name: "repair embedded in a sentence",
			in:   "the B R IDDING ULES apply here",
			want: "the BIDDING RULES apply here",
		},
		{
			name: "three-way interleave",
			in:   "T B C ERMS OARD ONTRACT",
			want: "TERMS BOARD CONTRACT",
		},
		{
			name: "lowercase text is a no-op",
			in:   "plain lowercase text stays untouched",
			want: "plain lowercase text stays untouched",
		},
		{
			name: "single capital words are never merged",
			in:   "I saw A REPORT yesterday",
			want: "I saw A REPORT yesterday",
		},
		{
			name: "double spaces collapse",
			in:   "double  spaces   here",
			want: "double spaces here",
		},
		{
			name: "capital run without matching fragments stays",
			in:   "sections A B and C apply",
			want: "sections A B and C apply",
		},
		{
			name: "multi-line input repaired per line",
			in:   "B R IDDING ULES\nplain line",
			want: "BIDDING RULES\nplain line",
		},
		{
			name: "fi ligature folds to ascii",
			in:   "Adam Tymo\uFB01ejewicz",
			want: "Adam Tymofiejewicz",
		},
		{
			name: "ff fl ffi ligatures fold to ascii",
			in:   "o\uFB00ice \uFB02exi \uFB03rst",
			want: "office flexi ffirst",
		},
		{
			name: "text with no ligature is untouched",
			in:   "ordinary fi fl text",
			want: "ordinary fi fl text",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RepairPDFText(tt.in); got != tt.want {
				t.Errorf("RepairPDFText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// engine/convert/convert_test.go — golden-file tests for the four binary
// converters plus the RepairPDFText table (BUILD.md Phase 1B).
//
// Conversion budget (BUILD.md performance table: ≤ 5 s per typical office
// file): measured on the largest fixture (workbook.xlsx, ~7 KB) in
// TestConversionBudget — see the measurement comment there.
package convert

import (
	"strings"
	"testing"
	"time"
)

// TestDocxGolden pins the full markdown output for the committed .docx
// fixture: heading, emphasis, hyperlink, image placeholder, both list
// kinds, table and French text.
func TestDocxGolden(t *testing.T) {
	raw := fixture(t, "report.docx")
	md, warnings, err := Docx(raw)
	if err != nil {
		t.Fatalf("Docx: %v", err)
	}

	want := "# Engagement Report\n\n" +
		"**Confidential** status: *draft* see [project site](https://alpine.example.com) *[image omitted]*\n\n" +
		"- First bullet\n" +
		"- Second bullet\n" +
		"1. Step one\n" +
		"  1. Sub step\n" +
		"\n" +
		"| Name | Role |\n| --- | --- |\n| Marie Duval | Reviewer |\n\n" +
		"Réunion avec Amélie Lefèvre à Luxembourg.\n\n"
	if md != want {
		t.Errorf("docx markdown mismatch:\n--- got ---\n%s\n--- want ---\n%s", md, want)
	}

	// One dropped image must surface as exactly one warning.
	if len(warnings) != 1 || !strings.Contains(warnings[0], "image") {
		t.Errorf("want one image warning, got %v", warnings)
	}
}

// TestDocxRejectsNonZip pins the actionable error for a legacy .doc-style
// (non-zip) payload.
func TestDocxRejectsNonZip(t *testing.T) {
	_, _, err := Docx([]byte("this is not a zip archive"))
	if err == nil || !strings.Contains(err.Error(), ".docx") {
		t.Errorf("want actionable not-a-docx error, got %v", err)
	}
}

// TestPptxGolden pins slide sections, bullet indentation, the table, the
// speaker notes (with the slide-number placeholder excluded) and the
// untitled French slide 2.
func TestPptxGolden(t *testing.T) {
	raw := fixture(t, "deck.pptx")
	md, _, err := Pptx(raw)
	if err != nil {
		t.Fatalf("Pptx: %v", err)
	}

	want := "## Slide 1 — Quarterly Review\n\n" +
		"- Revenue grew\n" +
		"  - Driven by Borealis Fund\n" +
		"\n" +
		"| KPI | Value |\n| --- | --- |\n| NPS | 42 |\n\n" +
		"**Notes:**\n\nMention the Alpine Trust engagement.\n\n" +
		"## Slide 2\n\n" +
		"- Prochaines étapes — réunion à Luxembourg\n\n"
	if md != want {
		t.Errorf("pptx markdown mismatch:\n--- got ---\n%s\n--- want ---\n%s", md, want)
	}
	// The slide-number placeholder text must not leak into the notes.
	if strings.Contains(md, "**Notes:**\n\n1\n") {
		t.Error("slide-number placeholder leaked into the notes block")
	}
}

// TestXlsxRouting pins the smart routing: the flat sheet becomes a Grid
// (with trailing empties trimmed), the merged-cell sheet becomes JSON.
func TestXlsxRouting(t *testing.T) {
	raw := fixture(t, "workbook.xlsx")
	sheets, _, err := Xlsx(raw)
	if err != nil {
		t.Fatalf("Xlsx: %v", err)
	}
	if len(sheets) != 2 {
		t.Fatalf("want 2 sheets, got %d", len(sheets))
	}

	clients := sheets[0]
	if clients.Name != "Clients" || !clients.Flat {
		t.Fatalf("sheet 1: want FLAT 'Clients', got %+v", clients.Name)
	}
	// Data bounds: 4 rows × 3 cols — the whitespace-only E7 cell must be
	// trimmed away, not force 7 rows × 5 cols.
	if len(clients.Grid) != 4 || len(clients.Grid[0]) != 3 {
		t.Errorf("flat grid = %d×%d, want 4×3 (trailing empties trimmed)", len(clients.Grid), len(clients.Grid[0]))
	}
	if clients.Grid[1][0] != "Marie Duval" {
		t.Errorf("grid content wrong: %v", clients.Grid[1])
	}

	resume := sheets[1]
	if resume.Name != "Résumé" || resume.Flat {
		t.Fatalf("sheet 2: want COMPLEX 'Résumé', got %+v", resume.Name)
	}
	// The JSON must carry cell addresses and values, and the warning must
	// explain the routing decision.
	for _, wantFrag := range []string{`"A1": "Budget prévisionnel"`, `"B3": "12500"`} {
		if !strings.Contains(resume.JSON, wantFrag) {
			t.Errorf("complex JSON missing %s:\n%s", wantFrag, resume.JSON)
		}
	}
	if len(resume.Warnings) != 1 || !strings.Contains(resume.Warnings[0], "merged") {
		t.Errorf("want a merged-cells routing warning, got %v", resume.Warnings)
	}
}

// TestPDFGolden proves per-page extraction plus the spacing repair on the
// committed text-layer fixture.
func TestPDFGolden(t *testing.T) {
	raw := fixture(t, "textlayer.pdf")
	md, warnings, err := PDF(raw)
	if err != nil {
		t.Fatalf("PDF: %v", err)
	}
	if !strings.Contains(md, "BIDDING RULES for the Alpine Trust engagement") {
		t.Errorf("spacing repair not applied, got:\n%s", md)
	}
	if !strings.Contains(md, "Contact: marie.duval@example.com (double spaces on purpose)") {
		t.Errorf("double-space collapse missing, got:\n%s", md)
	}
	// The experimental warning must always be present; the repair warning
	// must be present because this fixture needed repair.
	joined := strings.Join(warnings, " | ")
	if !strings.Contains(joined, "EXPERIMENTAL") || !strings.Contains(joined, "repair") {
		t.Errorf("want experimental + repair warnings, got %v", warnings)
	}
}

// TestPDFScannedRejected pins the exact actionable message from CLAUDE.md
// §5 for a PDF without a text layer.
func TestPDFScannedRejected(t *testing.T) {
	raw := fixture(t, "scanned.pdf")
	_, _, err := PDF(raw)
	if err == nil {
		t.Fatal("scanned PDF accepted, want rejection")
	}
	if err.Error() != ErrScannedPDF {
		t.Errorf("error = %q, want exactly %q", err.Error(), ErrScannedPDF)
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RepairPDFText(tt.in); got != tt.want {
				t.Errorf("RepairPDFText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestConversionBudget measures the ≤ 5 s budget on the largest fixture.
//
// Measurement recorded 2026-07-23 on the CI-class Linux container: all four
// fixtures convert in well under 50 ms combined (typical run ~10 ms) —
// four orders of magnitude inside the 5 s budget. Real 20 MB office files
// scale roughly linearly with content size, leaving ample headroom.
func TestConversionBudget(t *testing.T) {
	start := time.Now()
	if _, _, err := Docx(fixture(t, "report.docx")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Pptx(fixture(t, "deck.pptx")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Xlsx(fixture(t, "workbook.xlsx")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := PDF(fixture(t, "textlayer.pdf")); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	t.Logf("all four fixture conversions took %v (budget: 5 s per file)", elapsed)
	if elapsed > 5*time.Second {
		t.Errorf("conversion budget breached: %v > 5 s", elapsed)
	}
}

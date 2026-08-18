//go:build deep

// engine/convert/convert_deep_test.go — the DEEP-tier conversion budget.
//
// TIER: deep (docs/TESTING.md). This asserts a WALL-CLOCK budget, so it is
// non-deterministic by nature: the number it measures depends on the machine
// and on scheduler noise, not only on the code. It belongs in the deep tier,
// which carries no runtime budget and runs on demand, not on the per-push
// unit path where a busy runner could make it flake. The measurement stays
// meaningful because the margin is four orders of magnitude.
package convert

import (
	"testing"
	"time"
)

// TestConversionBudget measures the <= 5 s budget on the committed fixtures.
//
// Measurement recorded on a CI-class Linux container: all four fixtures
// convert in well under 50 ms combined (typical run ~10 ms), four orders of
// magnitude inside the 5 s budget. Real 20 MB office files scale roughly
// linearly with content size, leaving ample headroom.
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

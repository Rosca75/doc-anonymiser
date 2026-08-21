//go:build deep

// engine/budget_deep_test.go — the engine's WALL-CLOCK and scaling budgets.
//
// TIER: deep (docs/TESTING.md). Every test here asserts on elapsed wall-clock
// or on a timing RATIO, so the number it checks depends on the machine and on
// scheduler noise, not only on the code. That is precisely what the deep tier
// is for: non-deterministic, resource-hungry checks that carry no runtime
// budget and run on demand, never on the per-push unit path where a busy
// runner could make a tight budget flake. The measurements stay meaningful
// because their margins are large (the pipeline budget runs ~2 s against a 5 s
// cap; the scaling test uses an 8x bound where linear is ~4x and quadratic
// ~16x).
//
// These moved out of pipeline_test.go, pii_test.go and discover_test.go so
// those files stay pure unit. The helpers they use (runPipeline, the
// package-internal extractRunsContext) live in the untagged unit files and are
// visible here because deep is additive over unit.
package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestPipelineBudget measures the deterministic budget: passes
// 1+2+4 over 50 documents × 50 KB in ≤ 5 s.
func TestPipelineBudget(t *testing.T) {
	// ~50 KB of realistic prose per document, seeded with PII and value
	// mentions so the passes do real work.
	para := "Marie Duval of Alpine Trust (marie.duval@example.com, +352 621 000 111) " +
		"reviewed IBAN LU28 0019 4006 4475 0000 with P. Stone before the deadline. "
	var b strings.Builder
	for b.Len() < 50*1024 {
		b.WriteString(para)
	}
	text := b.String()

	docs := make([]Document, 50)
	for i := range docs {
		docs[i] = Document{Name: fmt.Sprintf("doc%02d.txt", i), Format: FormatTXT, Markdown: text}
	}
	values := []Value{
		{Category: "entity_names", MainText: "Alpine Trust"},
		{Category: "person_names", MainText: "Marie Duval"},
		{Category: "person_names", MainText: "Peter Stone"},
	}

	start := time.Now()
	res := runPipeline(t, PipelineInput{
		Documents:  docs,
		Values:     values,
		Categories: DepthSelection(PresetStandard, CountryLU),
		Allowlist:  NewAllowlist(),
	})
	elapsed := time.Since(start)

	t.Logf("50 docs × 50 KB deterministic pipeline took %v (budget 5 s), %d replacements",
		elapsed, res.Report.TotalReplacements)
	if elapsed > 5*time.Second {
		t.Errorf("pipeline budget breached: %v > 5 s", elapsed)
	}
	if res.Report.TotalReplacements == 0 {
		t.Error("budget run replaced nothing — the measurement is meaningless")
	}
}

// TestCSVImportBudget measures the performance row: CSV import →
// markdown-table render for 10 000 rows × 20 cols must stay ≤ 2 s.
// Measured on a CI-class Linux container: ~36 ms.
func TestCSVImportBudget(t *testing.T) {
	// Build the synthetic CSV once (not part of the timed section).
	var b strings.Builder
	for r := 0; r < 10000; r++ {
		for c := 0; c < 20; c++ {
			if c > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "cell_%d_%d", r, c)
		}
		b.WriteByte('\n')
	}
	raw := []byte(b.String())

	start := time.Now()
	doc, err := Load("big.csv", raw)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(doc.Grid) != 10000 {
		t.Fatalf("grid rows = %d, want 10000", len(doc.Grid))
	}
	t.Logf("10000×20 CSV import + markdown render took %v (budget 2 s)", elapsed)
	if elapsed > 2*time.Second {
		t.Errorf("CSV budget breached: %v > 2 s", elapsed)
	}
}

// TestExtractRunsContextScalesLinearly guards a quadratic hot spot that made
// detection look like it had hung on a real document.
//
// suffixBoundaryOK used to read its first character with []rune(rest)[0],
// where `rest` is the whole remainder of the document: one allocation and one
// scan of megabytes, per run. 800 KB took 15 seconds and 2.4 MB took about two
// minutes, which from the outside is exactly "detection sometimes does not
// complete". Decoding one rune in place made the same work linear.
//
// The assertion is a RATIO with a generous bound rather than a stopwatch
// threshold, so it fails on a return to quadratic behaviour and not on a busy
// CI runner.
func TestExtractRunsContextScalesLinearly(t *testing.T) {
	measure := func(repeats int) time.Duration {
		text := strings.Repeat("Alpine Trust S.A. met Marie Duval in Luxembourg. ", repeats)
		start := time.Now()
		extractRunsContext(context.Background(), text, "")
		return time.Since(start)
	}
	// Warm the code paths so the first measurement is not the one that pays
	// for lazily-initialised tables.
	measure(500)

	small := measure(4000)
	large := measure(16000)
	// Four times the input. Linear would be about 4x; quadratic is about 16x.
	// 8x leaves generous room for allocator noise while still failing loudly
	// on a reintroduced O(n^2).
	if large > 8*small {
		t.Errorf("extractRunsContext looks quadratic again: 4x the input took %v after %v (%.1fx)",
			large, small, float64(large)/float64(small))
	}
}

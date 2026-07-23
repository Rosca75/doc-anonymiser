// engine/pii_test.go — table-driven tests for pass 1 (BUILD.md Phase 2):
// per-category positives and negatives, IBAN checksum, level awareness,
// overlap resolution, registry stability, and the CSV budget measurement.
//
// Budget measurements (BUILD.md performance table), recorded 2026-07-23 on
// the CI-class Linux container:
//   - CSV import → markdown render, 10 000 rows × 20 cols: ~36 ms
//     (budget ≤ 2 s) — see TestCSVImportBudget.
//   - The 50-document pipeline budget is measured in Phase 4
//     (pipeline_test.go), where the full pass chain exists.
package engine

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestDetectPIICategories runs one positive and one negative case per
// category (BUILD.md Phase 2 unit-test requirements).
func TestDetectPIICategories(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		level    Level
		category string
		want     string // expected matched text ("" = expect NO match of category)
	}{
		// --- email ---
		{"email positive", "contact marie.duval@example.com today", LevelSoft, CatEmail, "marie.duval@example.com"},
		{"email negative: no TLD", "user@localhost is not external", LevelSoft, CatEmail, ""},
		// --- url ---
		{"url positive", "see https://intra.example.com/report?id=1 now", LevelSoft, CatURL, "https://intra.example.com/report?id=1"},
		{"url with credentials", "wget http://user:secret@host.example.com/f", LevelSoft, CatURL, "http://user:secret@host.example.com/f"},
		{"url negative: bare domain", "visit example.com sometime", LevelSoft, CatURL, ""},
		// --- iban ---
		{"iban positive spaced", "account LU28 0019 4006 4475 0000 please", LevelSoft, CatIBAN, "LU28 0019 4006 4475 0000"},
		{"iban positive compact", "send to DE89370400440532013000 now", LevelSoft, CatIBAN, "DE89370400440532013000"},
		{"iban negative: mutated digit fails checksum", "account LU28 0019 4006 4475 0001 please", LevelSoft, CatIBAN, ""},
		{"iban negative: country word", "LUXEMBOURG is not an IBAN", LevelSoft, CatIBAN, ""},
		// --- vat ---
		{"vat positive LU", "VAT number LU12345678 on file", LevelSoft, CatVAT, "LU12345678"},
		{"vat positive FR", "TVA FR40303265045 enregistrée", LevelSoft, CatVAT, "FR40303265045"},
		{"vat negative: too short", "code LU1234 is not a VAT number", LevelSoft, CatVAT, ""},
		// --- matricule ---
		{"matricule positive", "matricule 1893120105732 registered", LevelSoft, CatMatricule, "1893120105732"},
		{"matricule negative: 12 digits", "ref 189312010573 stays", LevelSoft, CatMatricule, ""},
		{"matricule negative: 14 digits", "ref 18931201057321 stays", LevelSoft, CatMatricule, ""},
		// --- phone ---
		{"phone positive international", "call +352 621 000 111 today", LevelSoft, CatPhone, "+352 621 000 111"},
		{"phone positive FR mobile", "tél. 06 12 34 56 78 merci", LevelSoft, CatPhone, "06 12 34 56 78"},
		{"phone negative: plain year", "the year 2026 report", LevelSoft, CatPhone, ""},
		// --- amount (advanced only) ---
		{"amount positive euro sign", "fee of €1,500.00 agreed", LevelAdvanced, CatAmount, "€1,500.00"},
		{"amount positive suffix", "budget 12 500 EUR total", LevelAdvanced, CatAmount, "12 500 EUR"},
		{"amount negative: bare number", "about 1500 items", LevelAdvanced, CatAmount, ""},
		// --- date (advanced only) ---
		{"date positive iso", "due 2026-07-23 latest", LevelAdvanced, CatDate, "2026-07-23"},
		{"date positive eu numeric", "signed 23/07/2026 in Luxembourg", LevelAdvanced, CatDate, "23/07/2026"},
		{"date positive written en", "meeting on 23 July 2026 confirmed", LevelAdvanced, CatDate, "23 July 2026"},
		{"date positive written fr", "réunion le 23 juillet 2026 confirmée", LevelAdvanced, CatDate, "23 juillet 2026"},
		{"date negative: bare year", "since 2026 things changed", LevelAdvanced, CatDate, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spans := DetectPII(tt.text, tt.level)
			var got []string
			for _, s := range spans {
				if s.Category == tt.category {
					got = append(got, s.Original)
				}
			}
			if tt.want == "" {
				if len(got) > 0 {
					t.Errorf("expected no %s match, got %v", tt.category, got)
				}
				return
			}
			if len(got) == 0 {
				t.Fatalf("expected %s match %q, got none (all spans: %+v)", tt.category, tt.want, spans)
			}
			if got[0] != tt.want {
				t.Errorf("matched %q, want %q", got[0], tt.want)
			}
		})
	}
}

// TestLevelAwareness pins the level matrix: amounts and dates fire at
// advanced only; hard PII fires at every level.
func TestLevelAwareness(t *testing.T) {
	text := "pay €500 to LU28 0019 4006 4475 0000 on 2026-07-23"
	for _, level := range []Level{LevelSoft, LevelMedium} {
		spans := DetectPII(text, level)
		for _, s := range spans {
			if s.Category == CatAmount || s.Category == CatDate {
				t.Errorf("%s fired at level %s, want advanced only", s.Category, level)
			}
		}
		if !hasCategory(spans, CatIBAN) {
			t.Errorf("IBAN must fire at level %s", level)
		}
	}
	adv := DetectPII(text, LevelAdvanced)
	if !hasCategory(adv, CatAmount) || !hasCategory(adv, CatDate) {
		t.Errorf("advanced level must add amount+date, got %+v", adv)
	}
}

func hasCategory(spans []Span, cat string) bool {
	for _, s := range spans {
		if s.Category == cat {
			return true
		}
	}
	return false
}

// TestOverlapResolution: an email inside a URL must resolve to the URL
// (longest match wins), deterministically.
func TestOverlapResolution(t *testing.T) {
	text := "profile at https://example.com/u/marie.duval@example.com end"
	spans := ResolveOverlaps(DetectPII(text, LevelMedium))
	if len(spans) != 1 {
		t.Fatalf("want exactly 1 span after resolution, got %+v", spans)
	}
	if spans[0].Category != CatURL {
		t.Errorf("URL must win over embedded email, got %s", spans[0].Category)
	}
	if spans[0].Original != "https://example.com/u/marie.duval@example.com" {
		t.Errorf("unexpected span text %q", spans[0].Original)
	}
}

// TestApplySpans proves back-to-front replacement keeps offsets valid and
// the registry produces stable, numbered placeholders.
func TestApplySpans(t *testing.T) {
	text := "mail marie.duval@example.com or peter.stone@example.org, mail marie.duval@example.com again"
	reg := NewRegistry()
	spans := ResolveOverlaps(DetectPII(text, LevelMedium))
	out := ApplySpans(text, spans, func(s Span) string {
		return reg.Assign(s.Category, s.CanonicalOrOriginal())
	})
	want := "mail [EMAIL_1] or [EMAIL_2], mail [EMAIL_1] again"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

// TestRegistryStability: the same value seen in two documents (and in a
// different case) maps to the same placeholder; counts accumulate.
func TestRegistryStability(t *testing.T) {
	reg := NewRegistry()
	p1 := reg.Assign(CatEmail, "Marie.Duval@example.com")
	p2 := reg.Assign(CatEmail, "marie.duval@example.com") // doc B, different case
	if p1 != p2 || p1 != "[EMAIL_1]" {
		t.Errorf("same email must share a placeholder: %q vs %q", p1, p2)
	}
	if p3 := reg.Assign(CatEmail, "other@example.net"); p3 != "[EMAIL_2]" {
		t.Errorf("second email should be [EMAIL_2], got %q", p3)
	}
	// Category label mapping: person_names → PERSON etc. (CLAUDE.md §5).
	if p := reg.Assign("person_names", "Marie Duval"); p != "[PERSON_1]" {
		t.Errorf("person placeholder = %q, want [PERSON_1]", p)
	}
	if p := reg.Assign("client_names", "Alpine Trust"); p != "[CLIENT_1]" {
		t.Errorf("client placeholder = %q, want [CLIENT_1]", p)
	}

	export := reg.Export()
	if len(export) != 4 {
		t.Fatalf("want 4 mapping entries, got %d", len(export))
	}
	// The doubly-seen email must report Count 2 for the key export.
	for _, e := range export {
		if e.Placeholder == "[EMAIL_1]" && e.Count != 2 {
			t.Errorf("[EMAIL_1] count = %d, want 2", e.Count)
		}
	}
}

// TestCSVImportBudget measures the Phase-2 performance row: CSV import →
// markdown-table render for 10 000 rows × 20 cols must stay ≤ 2 s.
// Measured 2026-07-23 on the CI-class Linux container: ~36 ms.
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

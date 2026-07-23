// engine/entities_test.go — Phase 3 tests: variant-expansion goldens
// (including French particles), allowlist precedence, word-boundary
// behaviour and custom-pattern validation.
package engine

import (
	"strings"
	"testing"
)

// TestExpandVariantsGolden pins the exact variant sets (BUILD.md Phase 3:
// "variant expansion golden tests, including French particles").
func TestExpandVariantsGolden(t *testing.T) {
	tests := []struct {
		name   string
		entity Entity
		want   []string // exact expected set, longest first
	}{
		{
			name:   "simple person",
			entity: Entity{Category: "person_names", Canonical: "Marie Duval"},
			want:   []string{"Marie-Duval", "Marie Duval", "M. Duval", "M.Duval", "Duval", "Marie"},
		},
		{
			name:   "french particles",
			entity: Entity{Category: "person_names", Canonical: "Jean de la Croix"},
			want: []string{
				"Jean-de-la-Croix", "Jean de la Croix", "J. de la Croix",
				"J.de la Croix", "de la Croix", "Croix", "Jean",
			},
		},
		{
			name:   "hyphenated first name",
			entity: Entity{Category: "person_names", Canonical: "Jean-Claude Muller"},
			want: []string{
				"Jean Claude Muller", "Jean-Claude Muller", "Jean-Claude-Muller",
				"J. Muller", "Jean-Claude", "J.Muller", "Muller",
			},
		},
		{
			name:   "organisation with legal suffix",
			entity: Entity{Category: "client_names", Canonical: "Alpine Trust S.A."},
			want:   []string{"Alpine Trust S.A.", "Alpine Trust"},
		},
		{
			name:   "organisation with sarl suffix",
			entity: Entity{Category: "client_names", Canonical: "Borealis Partners S.à r.l."},
			want:   []string{"Borealis Partners S.à r.l.", "Borealis Partners"},
		},
		{
			name: "manual variants are kept",
			entity: Entity{
				Category:       "person_names",
				Canonical:      "Peter Stone",
				ManualVariants: []string{"Pete"},
			},
			want: []string{"Peter-Stone", "Peter Stone", "P. Stone", "P.Stone", "Peter", "Stone", "Pete"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandVariants(tt.entity)
			if len(got) != len(tt.want) {
				t.Fatalf("variants = %v, want %v", got, tt.want)
			}
			// Sets must match; order is longest-first but ties may vary,
			// so compare as sets AND check monotonic length.
			wantSet := map[string]bool{}
			for _, w := range tt.want {
				wantSet[w] = true
			}
			for i, v := range got {
				if !wantSet[v] {
					t.Errorf("unexpected variant %q (all: %v)", v, got)
				}
				if i > 0 && len([]rune(got[i-1])) < len([]rune(v)) {
					t.Errorf("variants not longest-first: %v", got)
				}
			}
		})
	}
}

// TestDetectEntitiesBoundaries covers the substring / punctuation rules:
// "Alten" fires standalone and next to punctuation, never inside
// "Altenberg"; accented names work despite RE2's ASCII-only \b.
func TestDetectEntitiesBoundaries(t *testing.T) {
	entities := []Entity{{Category: "client_names", Canonical: "Alten"}}
	allow := NewEmptyAllowlist()

	tests := []struct {
		name    string
		text    string
		matches int
	}{
		{"standalone word matches", "the Alten contract", 1},
		{"substring does not match", "meeting in Altenberg today", 0},
		{"prefix substring does not match", "the Walten case", 0},
		{"punctuation-adjacent matches", "(Alten), then Alten.", 2},
		{"case-insensitive match", "ALTEN delivered", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spans := DetectEntities(tt.text, entities, allow)
			if len(spans) != tt.matches {
				t.Errorf("got %d matches (%+v), want %d", len(spans), spans, tt.matches)
			}
		})
	}

	t.Run("accented name with unicode boundaries", func(t *testing.T) {
		ents := []Entity{{Category: "person_names", Canonical: "Amélie Lefèvre"}}
		spans := DetectEntities("Réunion avec Amélie Lefèvre demain.", ents, allow)
		var got []string
		for _, s := range spans {
			got = append(got, s.Original)
		}
		joined := strings.Join(got, "|")
		if !strings.Contains(joined, "Amélie Lefèvre") {
			t.Errorf("full accented name not matched: %v", got)
		}
	})
}

// TestAllowlistBeatsEntity: a term that is BOTH an entity and allowlisted
// is never replaced (manual test matrix scenario 5: "CSSF").
func TestAllowlistBeatsEntity(t *testing.T) {
	allow := NewAllowlist() // seeds CSSF
	entities := []Entity{{Category: "client_names", Canonical: "CSSF"}}
	spans := DetectEntities("reported to the CSSF yesterday", entities, allow)
	if len(spans) != 0 {
		t.Errorf("allowlisted entity was matched: %+v", spans)
	}

	// And the shared filter drops allowlisted spans from other passes too.
	pii := []Span{{Start: 0, End: 4, Category: CatEmail, Original: "CSSF"}}
	if got := FilterAllowed(pii, allow); len(got) != 0 {
		t.Errorf("FilterAllowed kept an allowlisted span: %+v", got)
	}
}

// TestAllowlistEditing covers add/remove/case-insensitivity.
func TestAllowlistEditing(t *testing.T) {
	a := NewEmptyAllowlist()
	a.Add("Basel III")
	if !a.Contains("basel iii") || !a.Contains(" Basel III ") {
		t.Error("Contains must be case- and whitespace-insensitive")
	}
	a.Remove("BASEL III")
	if a.Contains("Basel III") {
		t.Error("Remove failed")
	}
	if a.Contains("never added") {
		t.Error("empty allowlist claims to contain a term")
	}
}

// TestCustomPatterns covers validation errors and span detection.
func TestCustomPatterns(t *testing.T) {
	if err := ValidateCustomPattern("PRJ-[0-9]+"); err != nil {
		t.Errorf("valid pattern rejected: %v", err)
	}
	if err := ValidateCustomPattern("["); err == nil || !strings.Contains(err.Error(), "regular expression") {
		t.Errorf("invalid pattern must fail actionably, got %v", err)
	}
	if err := ValidateCustomPattern("  "); err == nil {
		t.Error("empty pattern must be rejected")
	}

	allow := NewEmptyAllowlist()
	allow.Add("PRJ-999")
	spans := DetectCustomPatterns("codes PRJ-123 and PRJ-999 apply", []CustomPattern{{Expr: "PRJ-[0-9]+"}}, allow)
	if len(spans) != 1 || spans[0].Original != "PRJ-123" {
		t.Errorf("want only PRJ-123 (PRJ-999 allowlisted), got %+v", spans)
	}
	if spans[0].Category != "custom_patterns" {
		t.Errorf("category = %q, want custom_patterns", spans[0].Category)
	}
}

// TestEntityReplacementEndToEnd: variants + overlap resolution + registry,
// proving longest-match-first ("Marie Duval" wins over "Marie" and
// "Duval") and consistent placeholders for every variant of one entity.
func TestEntityReplacementEndToEnd(t *testing.T) {
	text := "Marie Duval met M. Duval's team; Marie signed."
	entities := []Entity{{Category: "person_names", Canonical: "Marie Duval"}}
	reg := NewRegistry()

	spans := ResolveOverlaps(DetectEntities(text, entities, NewEmptyAllowlist()))
	out := ApplySpans(text, spans, func(cat, original string) string {
		// Every variant maps to the entity's CANONICAL placeholder — the
		// registry is keyed on the canonical name, not the variant, so
		// "M. Duval" and "Marie" share [PERSON_1]. The pipeline (Phase 4)
		// wires this canonicalisation; the test inlines it.
		return reg.Assign(cat, "Marie Duval")
	})
	want := "[PERSON_1] met [PERSON_1]'s team; [PERSON_1] signed."
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

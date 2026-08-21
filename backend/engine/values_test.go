// engine/entities_test.go — tests variant-expansion goldens
// (including French particles), allowlist precedence, word-boundary
// behaviour and custom-pattern validation.
package engine

import (
	"strings"
	"testing"
)

// TestExpandVariantsGolden pins the exact variant sets (:
// "variant expansion golden tests, including French particles").
func TestExpandVariantsGolden(t *testing.T) {
	tests := []struct {
		name  string
		value Value
		want  []string // exact expected set, longest first
	}{
		{
			name:  "simple person",
			value: Value{Category: "person_names", MainText: "Marie Duval"},
			want:  []string{"Marie-Duval", "Marie Duval", "M. Duval", "M.Duval", "Duval", "Marie"},
		},
		{
			name:  "french particles",
			value: Value{Category: "person_names", MainText: "Jean de la Croix"},
			want: []string{
				"Jean-de-la-Croix", "Jean de la Croix", "J. de la Croix",
				"J.de la Croix", "de la Croix", "Croix", "Jean",
			},
		},
		{
			name:  "hyphenated first name",
			value: Value{Category: "person_names", MainText: "Jean-Claude Muller"},
			want: []string{
				"Jean Claude Muller", "Jean-Claude Muller", "Jean-Claude-Muller",
				"J. Muller", "Jean-Claude", "J.Muller", "Muller",
			},
		},
		{
			name:  "organisation with legal suffix",
			value: Value{Category: "entity_names", MainText: "Alpine Trust S.A."},
			want:  []string{"Alpine Trust S.A.", "Alpine Trust"},
		},
		{
			name:  "organisation with sarl suffix",
			value: Value{Category: "entity_names", MainText: "Borealis Partners S.à r.l."},
			want:  []string{"Borealis Partners S.à r.l.", "Borealis Partners"},
		},
		{
			name: "manual variants are kept",
			value: Value{
				Category:  "person_names",
				MainText:  "Peter Stone",
				Spellings: []string{"Pete"},
			},
			want: []string{"Peter-Stone", "Peter Stone", "P. Stone", "P.Stone", "Peter", "Stone", "Pete"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandSpellings(tt.value)
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
	values := []Value{{Category: "entity_names", MainText: "Alten"}}
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
			spans := DetectValues(tt.text, values, allow)
			if len(spans) != tt.matches {
				t.Errorf("got %d matches (%+v), want %d", len(spans), spans, tt.matches)
			}
		})
	}

	t.Run("accented name with unicode boundaries", func(t *testing.T) {
		ents := []Value{{Category: "person_names", MainText: "Amélie Lefèvre"}}
		spans := DetectValues("Réunion avec Amélie Lefèvre demain.", ents, allow)
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

// TestAllowlistBeatsEntity: a term that is BOTH a Value and allowlisted
// is never replaced (manual test matrix scenario 5: "CSSF").
func TestAllowlistBeatsEntity(t *testing.T) {
	allow := NewAllowlist() // seeds CSSF
	values := []Value{{Category: "entity_names", MainText: "CSSF"}}
	spans := DetectValues("reported to the CSSF yesterday", values, allow)
	if len(spans) != 0 {
		t.Errorf("allowlisted value was matched: %+v", spans)
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
// "Duval") and consistent placeholders for every variant of one value.
func TestEntityReplacementEndToEnd(t *testing.T) {
	text := "Marie Duval met M. Duval's team; Marie signed."
	values := []Value{{Category: "person_names", MainText: "Marie Duval"}}
	reg := NewRegistry()

	spans := ResolveOverlaps(DetectValues(text, values, NewEmptyAllowlist()))
	// Every spelling maps to the Value's MAIN TEXT placeholder, the
	// registry is keyed on Span.MainText, so "M. Duval" and "Marie"
	// share [PERSON_1].
	out := ApplySpans(text, spans, func(s Span) string {
		return reg.Assign(s.Category, s.MainTextOrOriginal())
	})
	want := "[PERSON_1] met [PERSON_1]'s team; [PERSON_1] signed."
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

// --- Variant expansion classes -------------------------------------------

func TestVariantExpansionClassPerCategory(t *testing.T) {
	// Three classes, one per row: a category gets person-style expansion,
	// organisation-style, or none at all. Asserted together because the bug this
	// guards against is a category quietly moving between them.
	cases := []struct {
		name     string
		value    Value
		want     []string
		wantOnly bool // the mainText is the ONLY variant
	}{
		{
			name:  "a person expands into initials and surname",
			value: Value{Category: CatPersonNames, MainText: "Marie Duval"},
			want:  []string{"M. Duval", "Duval", "Marie"},
		},
		{
			name:  "an organisation loses its legal suffix",
			value: Value{Category: CatEntityNames, MainText: "Alpine Trust S.A."},
			want:  []string{"Alpine Trust"},
		},
		{
			name:  "a product is an organisation-style name",
			value: Value{Category: CatProductNames, MainText: "Meridian Suite Ltd"},
			want:  []string{"Meridian Suite"},
		},
		{
			// Stripping a token that resembles a legal suffix off a code would
			// invent a variant matching a DIFFERENT code.
			name:     "a reference code is expanded literally",
			value:    Value{Category: CatIdentifierNames, MainText: "PRJ-4471-SE"},
			wantOnly: true,
		},
		{
			// Nothing is known about the shape of a value filed under "other",
			// so nothing can be inferred from it.
			name:     "an other name is expanded literally",
			value:    Value{Category: CatOtherNames, MainText: "Helios Ltd"},
			wantOnly: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExpandSpellings(tc.value)
			if tc.wantOnly {
				if len(got) != 1 || got[0] != tc.value.MainText {
					t.Fatalf("want the mainText alone, got %v", got)
				}
				return
			}
			for _, want := range tc.want {
				if !containsString(got, want) {
					t.Errorf("missing variant %q in %v", want, got)
				}
			}
		})
	}
}

func TestLiteralOnlyCategoriesStillTakeListedSpellings(t *testing.T) {
	// No AUTOMATIC expansion is not the same as no variants: a spelling the user
	// typed is an explicit instruction, and the automatic rules are what cannot
	// be trusted on a code.
	got := ExpandSpellings(Value{
		Category:  CatIdentifierNames,
		MainText:  "PRJ-4471",
		Spellings: []string{"PRJ 4471"},
	})
	if !containsString(got, "PRJ 4471") {
		t.Errorf("a manual variant must survive on a literal-only category, got %v", got)
	}
}

// TestCuratedSpellings covers the spelling-policy model: once the policy is
// curated, main text plus Spellings IS the complete replacement set. The chips on
// a Value's card are then exactly what the run replaces, which is what makes
// deleting a spelling stick without recording a rule that suppresses it.
func TestCuratedSpellings(t *testing.T) {
	cases := []struct {
		name  string
		value Value
		want  []string
	}{
		{
			name: "a curated Value expands to exactly its list",
			value: Value{
				Category:       CatPersonNames,
				MainText:       "Marie Duval",
				Spellings:      []string{"Duval", "Mimi"},
				SpellingPolicy: SpellingPolicyCurated,
			},
			// No "M. Duval", no "Marie": those are derived, and this Value's
			// spellings are the user's.
			want: []string{"Marie Duval", "Duval", "Mimi"},
		},
		{
			name: "an explicit automatic policy derives as usual",
			value: Value{
				Category:       CatEntityNames,
				MainText:       "Alpine Trust S.A.",
				SpellingPolicy: SpellingPolicyAutomatic,
			},
			want: []string{"Alpine Trust S.A.", "Alpine Trust"},
		},
		{
			name: "an absent policy derives as usual",
			value: Value{
				Category: CatEntityNames,
				MainText: "Alpine Trust S.A.",
			},
			want: []string{"Alpine Trust S.A.", "Alpine Trust"},
		},
		{
			name: "minSpellingLen still guards a curated list",
			value: Value{
				Category: CatPersonNames,
				MainText: "Marie Duval",
				// "Du" is two runes: replacing it would shred ordinary text,
				// whether it was derived or typed.
				Spellings:      []string{"Du", "Duval"},
				SpellingPolicy: SpellingPolicyCurated,
			},
			want: []string{"Marie Duval", "Duval"},
		},
	}
	for _, tc := range cases {
		got := ExpandSpellings(tc.value)
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			continue
		}
		for _, w := range tc.want {
			if !containsString(got, w) {
				t.Errorf("%s: missing %q, got %v", tc.name, w, got)
			}
		}
	}
}

// TestDetectEntitiesStampsTheEntitysOrigin: provenance has to survive the
// accept, or the only trace of which route found a value is its confidence
// score, and confidence is also the MinConfidence floor's input.
func TestDetectEntitiesStampsTheEntitysOrigin(t *testing.T) {
	text := "Meridian and Delta Industries both appear here.\n"
	spans := DetectValues(text, []Value{
		{Category: CatEntityNames, MainText: "Meridian", DiscoveryMethods: []string{MethodLocalAI}},
		// No matchClass stated: a value the user typed, which is what declared means.
		{Category: CatEntityNames, MainText: "Delta Industries"},
	}, NewEmptyAllowlist())

	got := map[string]string{}
	for _, s := range spans {
		got[s.MainText] = s.MatchClass
	}
	if got["Meridian"] != MatchClassLocalAIDiscovered {
		t.Errorf("an AI value must produce AI spans, got %q", got["Meridian"])
	}
	if got["Delta Industries"] != MatchClassUserDefined {
		t.Errorf("a Value with no matchClass must read as declared, got %q", got["Delta Industries"])
	}
}

func TestOneLegalSuffixTableServesBothDetectionAndExpansion(t *testing.T) {
	// Two tables meant heuristic discovery could propose "Bidco SCSp" from a form
	// only IT knew, and the expansion could then not produce "Bidco".
	proposed := HeuristicDiscoverWithOptions("Bidco SCSp signed the deed.\n",
		NewEmptyAllowlist(), HeuristicDiscoveryOptions{})
	if len(proposed) == 0 || proposed[0].MainText != "Bidco SCSp" {
		t.Fatalf("detection should propose the suffixed name, got %+v", proposed)
	}
	expanded := ExpandSpellings(Value{Category: CatEntityNames, MainText: proposed[0].MainText})
	if !containsString(expanded, "Bidco") {
		t.Errorf("expansion must strip the same suffix detection recognised, got %v", expanded)
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

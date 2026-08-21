// engine/confidence_test.go — Confidence as DATA
//
// Confidence has three jobs and no fourth. It is one of the comparators overlap
// resolution reaches for after the match class; it feeds the heuristic pass's
// own floor, before a Suggestion is ever shown; and it is REPORTED, which is how
// a reviewer sees that a corroborating check did not pass.
//
// It is deliberately not a lever over what a run replaces. That is what this
// file locks: what each producer stamps, so a Value that states nothing is
// trusted rather than treated as a machine guess. The switch that DOES remove a
// span, the checksum question, is in checksum_test.go.
package engine

import "testing"

func TestEntityConfidenceDefaultsToManual(t *testing.T) {
	// A value that states nothing is one the user declared, and must
	// never be filterable as if it were a machine guess.
	spans := DetectValues("Marie Duval called.",
		[]Value{{Category: "person_names", MainText: "Marie Duval"}},
		NewEmptyAllowlist())
	if len(spans) == 0 {
		t.Fatal("expected at least one value span")
	}
	for _, s := range spans {
		if s.Confidence != ConfidenceManualDefault {
			t.Errorf("span %q scored %v, want the manual default %v",
				s.Original, s.Confidence, ConfidenceManualDefault)
		}
	}
}

func TestEntityConfidenceIsKeptWhenStated(t *testing.T) {
	spans := DetectValues("Anouk Berger called.",
		[]Value{{Category: "person_names", MainText: "Anouk Berger", Confidence: ConfidenceLLMDefault}},
		NewEmptyAllowlist())
	if len(spans) == 0 {
		t.Fatal("expected at least one value span")
	}
	for _, s := range spans {
		if s.Confidence != ConfidenceLLMDefault {
			t.Errorf("span %q scored %v, want the stated %v",
				s.Original, s.Confidence, ConfidenceLLMDefault)
		}
	}
}

// TestEffectiveConfidenceTrustsAnUnscoredSpan: zero means "not scored", not
// "scored zero". Reading it as the latter would rank a producer that forgot the
// stamp below every other one, which turns a forgotten field into a lost
// replacement instead of an error.
func TestEffectiveConfidenceTrustsAnUnscoredSpan(t *testing.T) {
	if got := effectiveConfidence(Span{Category: CatPhone, Original: "+352 621 000 000"}); got != ConfidenceDeterministic {
		t.Errorf("an unscored span reads as %v, want %v", got, ConfidenceDeterministic)
	}
	if got := effectiveConfidence(Span{Confidence: ConfidenceLLMDefault}); got != ConfidenceLLMDefault {
		t.Errorf("a scored span reads as %v, want its own %v", got, ConfidenceLLMDefault)
	}
}

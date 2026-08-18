// engine/confidence_test.go — the detection-confidence floor
//
// Two things are locked here, and the first matters more than the second:
//
//  1. The DEFAULT changes nothing. A zero MinConfidence must keep replacing
//     everything, because a setting that quietly removes replacements would be
//     a data-leak bug, not a preference.
//  2. Above the AI tier, values accepted from a Local AI Suggestion stop being
//     replaced while everything the user declared keeps being replaced.
package engine

import (
	"context"
	"strings"
	"testing"
)

// runWith is the shared harness: one document, one declared value, one value
// accepted from a Local AI Suggestion, at the given confidence floor.
func runWith(t *testing.T, minConfidence float32) string {
	t.Helper()
	const text = "Marie Duval met Anouk Berger about the audit.\n"
	res, err := Run(context.Background(), PipelineInput{
		Documents: []Document{{Name: "note.txt", Format: FormatTXT, Markdown: text}},
		Values: []Value{
			// A value the user declared themselves: high trust.
			{Category: "person_names", MainText: "Marie Duval"},
			// A value accepted from a Local AI Suggestion: lower trust.
			{Category: "person_names", MainText: "Anouk Berger", DiscoveryMethods: []string{MethodLocalAI}, Confidence: ConfidenceLLMDefault},
		},
		Level:         LevelMedium,
		MinConfidence: minConfidence,
		Allowlist:     NewEmptyAllowlist(),
	})
	if err != nil {
		t.Fatalf("Run returned an unexpected error: %v", err)
	}
	if len(res.Documents) != 1 {
		t.Fatalf("expected 1 result document, got %d", len(res.Documents))
	}
	return res.Documents[0].Anonymised
}

func TestMinConfidenceDefaultReplacesEverything(t *testing.T) {
	got := runWith(t, 0)
	for _, name := range []string{"Marie Duval", "Anouk Berger"} {
		if strings.Contains(got, name) {
			t.Errorf("at the default floor %q must still be replaced, got:\n%s", name, got)
		}
	}
	if !strings.Contains(got, "[PERSON_") {
		t.Errorf("expected person placeholders in:\n%s", got)
	}
}

func TestMinConfidenceAboveAITierKeepsListedValuesOnly(t *testing.T) {
	// 0.9 sits between the AI tier (0.8) and the user tier (0.95).
	got := runWith(t, 0.9)
	if strings.Contains(got, "Marie Duval") {
		t.Errorf("a value the user listed must still be replaced, got:\n%s", got)
	}
	if !strings.Contains(got, "Anouk Berger") {
		t.Errorf("a value accepted from an AI Suggestion must be left alone above its tier, got:\n%s", got)
	}
}

func TestMinConfidenceAboveUserTierKeepsPatternMatchesOnly(t *testing.T) {
	// 0.99 is above every value tier, so only pattern matches survive.
	res, err := Run(context.Background(), PipelineInput{
		Documents: []Document{{
			Name: "note.txt", Format: FormatTXT,
			Markdown: "Marie Duval, marie@example.com\n",
		}},
		Values:        []Value{{Category: "person_names", MainText: "Marie Duval"}},
		Level:         LevelMedium,
		MinConfidence: 0.99,
		Allowlist:     NewEmptyAllowlist(),
	})
	if err != nil {
		t.Fatalf("Run returned an unexpected error: %v", err)
	}
	got := res.Documents[0].Anonymised
	if !strings.Contains(got, "Marie Duval") {
		t.Errorf("above the user tier the listed value must be left alone, got:\n%s", got)
	}
	if strings.Contains(got, "marie@example.com") {
		t.Errorf("a pattern match must still be replaced at any floor up to 1.0, got:\n%s", got)
	}
}

func TestFilterByMinConfidence(t *testing.T) {
	spans := []Span{
		{Category: CatEmail, Original: "a@b.c", Confidence: 1.0},
		{Category: "person_names", Original: "Marie", Confidence: ConfidenceManualDefault},
		{Category: "person_names", Original: "Anouk", Confidence: ConfidenceLLMDefault},
		// A span that states no confidence counts as 1.0.
		{Category: CatPhone, Original: "+352 621 000 000"},
	}
	cases := []struct {
		name string
		min  float32
		want int
	}{
		{"zero is a no-op", 0, 4},
		{"negative is a no-op", -1, 4},
		{"below every tier keeps everything", 0.5, 4},
		{"above the AI tier drops the AI span", 0.9, 3},
		{"above the user tier drops both value spans", 0.99, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// FilterByMinConfidence reuses the backing array, so each case
			// gets its own copy.
			input := make([]Span, len(spans))
			copy(input, spans)
			got := FilterByMinConfidence(input, tc.min)
			if len(got) != tc.want {
				t.Errorf("min %v: kept %d spans, want %d (%+v)", tc.min, len(got), tc.want, got)
			}
		})
	}
}

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

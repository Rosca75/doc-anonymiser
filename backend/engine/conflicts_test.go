// engine/conflicts_test.go — the conflict rules: what refuses a run, what only
// warns about one, and what is deliberately neither.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestValidationMarshalsWithLowercaseKeys locks the JSON contract the Anonymise
// screen reads. The results ride to the frontend on the "pipeline:done" event,
// and the frontend reads results.validation.blocking[].message and .fix to
// explain a refused run. If these tags regress to Go's capitalised field names,
// the run is refused but the screen shows a silent 0/0/0 mismatch instead, which
// is the exact bug this contract prevents.
func TestValidationMarshalsWithLowercaseKeys(t *testing.T) {
	res := ValidationResult{
		Blocking: []Conflict{{
			Kind:     "collision",
			Severity: "block",
			Value:    "Mendonça",
			Message:  "two values claim the same spelling",
			Fix:      "remove one of them",
			Refs:     []ValueRef{{Kind: "entity", Category: "person_names", Canonical: "mendonça"}},
		}},
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	for _, key := range []string{
		`"blocking"`, `"kind"`, `"severity"`, `"value"`, `"message"`, `"fix"`,
		`"refs"`, `"category"`, `"canonical"`,
	} {
		if !strings.Contains(got, key) {
			t.Errorf("expected key %s in %s", key, got)
		}
	}
	for _, bad := range []string{`"Blocking"`, `"Message"`, `"Fix"`, `"Kind"`} {
		if strings.Contains(got, bad) {
			t.Errorf("capitalised key %s leaked into %s", bad, got)
		}
	}
}

// blocking runs ValidateValues over one declaration set and returns the kinds
// it refused, so a table case can name them.
func blocking(t *testing.T, in ValidationInput) []Conflict {
	t.Helper()
	if in.Allowlist == nil {
		in.Allowlist = NewEmptyAllowlist()
	}
	return ValidateValues(in).Blocking
}

func TestBlockingConflicts(t *testing.T) {
	cases := []struct {
		name      string
		in        ValidationInput
		wantKind  string
		wantValue string
	}{
		{
			name: "the same string declared in two active categories",
			in: ValidationInput{
				Entities: []Entity{
					{Category: CatEntityNames, Canonical: "Atlas"},
					{Category: CatProjectNames, Canonical: "atlas"},
				},
				Categories: PresetSelection(LevelMedium),
			},
			wantKind:  "ambiguity",
			wantValue: "atlas",
		},
		{
			name: "a spelling two different values would both claim",
			in: ValidationInput{
				Entities: []Entity{
					{Category: CatPersonNames, Canonical: "Marie Duval"},
					{Category: CatPersonNames, Canonical: "Marie Dupont"},
				},
				Categories: PresetSelection(LevelMedium),
			},
			wantKind:  "collision",
			wantValue: "Marie",
		},
		{
			name: "a declared value that is also allowlisted",
			in: ValidationInput{
				Entities:   []Entity{{Category: CatEntityNames, Canonical: "CSSF"}},
				Categories: PresetSelection(LevelMedium),
				Allowlist:  allowlistWith("CSSF"),
			},
			wantKind:  "collision",
			wantValue: "CSSF",
		},
		{
			name: "a rule that looks for a placeholder, so it would rewrite anonymised text",
			in: ValidationInput{
				SimpleRules: []SimpleRule{{Find: "call [PERSON_1] back", Replace: "redacted"}},
				Categories:  PresetSelection(LevelMedium),
			},
			wantKind:  "reserved",
			wantValue: "call [PERSON_1] back",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := blocking(t, tc.in)
			if len(got) == 0 {
				t.Fatalf("want a blocking conflict, got none")
			}
			if got[0].Kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", got[0].Kind, tc.wantKind)
			}
			if !strings.EqualFold(got[0].Value, tc.wantValue) {
				t.Errorf("value = %q, want %q", got[0].Value, tc.wantValue)
			}
			if got[0].Severity != "block" {
				t.Errorf("severity = %q, want block", got[0].Severity)
			}
			// Every message has to say what failed AND how to fix it: the UI
			// shows both verbatim and the user has no other source.
			if got[0].Message == "" || got[0].Fix == "" {
				t.Errorf("a conflict needs a message and a fix, got %+v", got[0])
			}
		})
	}
}

func TestAnInactiveCategoryIsNotAConflict(t *testing.T) {
	// The values of a switched-off category are not going to be replaced, so
	// nothing about them can be ambiguous. Refusing the run over them would
	// block on a decision the user already made.
	sel := PresetSelection(LevelMedium)
	sel[CatProjectNames] = false

	got := blocking(t, ValidationInput{
		Entities: []Entity{
			{Category: CatEntityNames, Canonical: "Atlas"},
			{Category: CatProjectNames, Canonical: "Atlas"},
		},
		Categories: sel,
	})
	if len(got) != 0 {
		t.Errorf("an off category must not conflict, got %+v", got)
	}
}

func TestAnUnassignedCustomPlaceholderIsAllowed(t *testing.T) {
	// The shipped select-and-replace flow mints a [CUSTOM_N] and the App
	// reserves it. Blocking it would refuse the application's own feature.
	registry := NewRegistry()
	registry.Assign(CatPersonNames, "Marie Duval") // takes [PERSON_1], not [CUSTOM_9]

	got := blocking(t, ValidationInput{
		SimpleRules: []SimpleRule{{Find: "4471-B", Replace: "[CUSTOM_9]"}},
		Categories:  PresetSelection(LevelMedium),
		Registry:    registry,
	})
	if len(got) != 0 {
		t.Errorf("an unassigned placeholder replacement must be allowed, got %+v", got)
	}
}

func TestAReplacementTheRegistryAlreadyOwnsIsRefused(t *testing.T) {
	registry := NewRegistry()
	taken := registry.Assign(CatPersonNames, "Marie Duval")

	got := blocking(t, ValidationInput{
		SimpleRules: []SimpleRule{{Find: "4471-B", Replace: taken}},
		Categories:  PresetSelection(LevelMedium),
		Registry:    registry,
	})
	if len(got) != 1 || got[0].Kind != "reserved" {
		t.Fatalf("want one reserved conflict, got %+v", got)
	}
	if !strings.Contains(got[0].Message, "Marie Duval") {
		t.Errorf("the message must name the value that already owns it, got %q", got[0].Message)
	}
}

func TestABlockingConflictAbortsBeforeTheRegistryIsTouched(t *testing.T) {
	// A half-run that assigned placeholders for a configuration the user was
	// just told is invalid cannot be undone without a new session.
	registry := NewRegistry()
	res, err := Run(context.Background(), PipelineInput{
		Documents: []Document{{Name: "a.txt", Format: FormatTXT, Markdown: "Atlas met Atlas."}},
		Entities: []Entity{
			{Category: CatEntityNames, Canonical: "Atlas"},
			{Category: CatProjectNames, Canonical: "Atlas"},
		},
		Level:     LevelMedium,
		Allowlist: NewEmptyAllowlist(),
		Registry:  registry,
	})
	if err != nil {
		t.Fatalf("a blocking conflict is reported in the results, not as an error: %v", err)
	}
	if len(res.Validation.Blocking) == 0 {
		t.Fatal("the run must report the conflict")
	}
	if len(res.Documents) != 0 {
		t.Errorf("no document may be processed: %+v", res.Documents)
	}
	if len(registry.Export()) != 0 {
		t.Errorf("no placeholder may be assigned: %+v", registry.Export())
	}
}

// allowlistWith builds an allowlist holding exactly the given terms.
func allowlistWith(terms ...string) *Allowlist {
	allow := NewEmptyAllowlist()
	for _, t := range terms {
		allow.Add(t)
	}
	return allow
}

// --- Overlap warnings ----------------------------------------------------

func TestOverlapWarningsComeFromTheResolverItself(t *testing.T) {
	// A declared value that is also an email. Pass 1 carries 1.0 against a
	// declaration's 0.95, so the email wins, and the run says so: the warning is
	// derived from what the resolver DISCARDED, not from a parallel check that
	// could disagree with it.
	const text = "Write to marie.duval@example.com about the mandate."

	res, err := Run(context.Background(), PipelineInput{
		Documents: []Document{{Name: "a.txt", Format: FormatTXT, Markdown: text}},
		Entities: []Entity{
			{Category: CatPersonNames, Canonical: "marie.duval@example.com"},
		},
		Level:     LevelMedium,
		Allowlist: NewEmptyAllowlist(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Documents[0].Anonymised, "[EMAIL_1]") {
		t.Fatalf("the regex match must win, got %q", res.Documents[0].Anonymised)
	}

	var overlap *Conflict
	for i, w := range res.Validation.Warnings {
		if w.Kind == "overlap" {
			overlap = &res.Validation.Warnings[i]
		}
	}
	if overlap == nil {
		t.Fatalf("the run must warn about the overlap it resolved: %+v", res.Validation.Warnings)
	}
	if overlap.Severity != "warn" {
		t.Errorf("an overlap is resolved, not refused, so it warns: %+v", overlap)
	}
	if overlap.Value != "marie.duval@example.com" {
		t.Errorf("the warning must name the value that lost, got %q", overlap.Value)
	}
}

func TestOverlapWarningsAreDeduplicatedAndCapped(t *testing.T) {
	// Overlaps are per OCCURRENCE, so one recurring collision in a large
	// document would otherwise emit thousands of identical rows.
	text := strings.Repeat("Write to marie.duval@example.com today. ", 500)

	res, err := Run(context.Background(), PipelineInput{
		Documents: []Document{{Name: "a.txt", Format: FormatTXT, Markdown: text}},
		Entities: []Entity{
			{Category: CatPersonNames, Canonical: "marie.duval@example.com"},
		},
		Level:     LevelMedium,
		Allowlist: NewEmptyAllowlist(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	overlaps := 0
	for _, w := range res.Validation.Warnings {
		if w.Kind == "overlap" {
			overlaps++
		}
	}
	if overlaps != 1 {
		t.Errorf("500 occurrences of one collision must warn once, got %d", overlaps)
	}
	if len(res.Validation.Warnings) > maxOverlapWarnings {
		t.Errorf("warnings must be capped at %d, got %d", maxOverlapWarnings, len(res.Validation.Warnings))
	}
}

func TestResolveOverlapsAndItsLosersAgree(t *testing.T) {
	// ResolveOverlaps is a wrapper, so the two can never disagree about what is
	// kept. Asserted because the wrapper is the only reason existing callers did
	// not have to change.
	spans := []Span{
		{Start: 0, End: 20, Category: CatEmail, Original: "a@example.com", Confidence: 1.0},
		{Start: 2, End: 10, Category: CatPersonNames, Original: "example", Confidence: 0.95},
		{Start: 40, End: 50, Category: CatPersonNames, Original: "Marie Duval", Confidence: 0.95},
	}
	kept, dropped := ResolveOverlapsWithLosers(spans)

	if len(kept) != 2 || len(dropped) != 1 {
		t.Fatalf("want 2 kept and 1 dropped, got %d and %d", len(kept), len(dropped))
	}
	if dropped[0].Original != "example" {
		t.Errorf("the covered span must be the dropped one, got %+v", dropped[0])
	}
	if len(ResolveOverlaps(spans)) != len(kept) {
		t.Error("the wrapper must keep exactly what the full function keeps")
	}
}

func TestOverlapCollectionStopsCostingOnceItHasSaidEverything(t *testing.T) {
	// Overlaps are per occurrence, and a document full of name variants discards
	// several spans per replacement. Gathering them all cost the deterministic
	// pipeline a third of its time budget, so the collector is asked BEFORE the
	// resolution whether it still wants them.
	w := newOverlapWarnings()
	if !w.wants() {
		t.Fatal("a fresh collector must want the first losers")
	}

	// One distinct value repeated far past the work budget: the warning cap
	// alone would never stop it, because there is only ever one warning.
	span := Span{Category: CatPersonNames, Original: "Marie Duval", Confidence: 0.95}
	for i := 0; i < maxOverlapSpansExamined+10; i++ {
		w.add([]Span{span})
	}

	if len(w.conflicts()) != 1 {
		t.Errorf("one repeated collision is one warning, got %d", len(w.conflicts()))
	}
	if w.wants() {
		t.Error("the collector must stop asking once it has spent its work budget")
	}
	if w.examined > maxOverlapSpansExamined {
		t.Errorf("examined %d spans, the budget is %d", w.examined, maxOverlapSpansExamined)
	}
}

func TestManyDistinctOverlapsStopAtTheWarningCap(t *testing.T) {
	// The other half of the pair: a document that genuinely collides in many
	// different ways stops at a list the user can still read.
	w := newOverlapWarnings()
	for i := 0; i < maxOverlapWarnings*2; i++ {
		w.add([]Span{{
			Category:   CatPersonNames,
			Original:   fmt.Sprintf("Person %d", i),
			Confidence: 0.95,
		}})
	}
	if len(w.conflicts()) != maxOverlapWarnings {
		t.Errorf("want the cap of %d warnings, got %d", maxOverlapWarnings, len(w.conflicts()))
	}
	if w.wants() {
		t.Error("a full list must stop asking for more")
	}
}

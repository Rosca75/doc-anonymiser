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
			Refs:     []ValueRef{{Kind: "value", Category: "person_names", MainText: "mendonça"}},
		}},
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	for _, key := range []string{
		`"blocking"`, `"kind"`, `"severity"`, `"value"`, `"message"`, `"fix"`,
		`"refs"`, `"category"`, `"mainText"`,
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
				Values: []Value{
					{Category: CatEntityNames, MainText: "Atlas"},
					{Category: CatProjectNames, MainText: "atlas"},
				},
				Categories: PresetSelection(LevelMedium),
			},
			wantKind:  "ambiguity",
			wantValue: "atlas",
		},
		{
			name: "a spelling two different values would both claim",
			in: ValidationInput{
				Values: []Value{
					{Category: CatPersonNames, MainText: "Marie Duval"},
					{Category: CatPersonNames, MainText: "Marie Dupont"},
				},
				Categories: PresetSelection(LevelMedium),
			},
			wantKind:  "collision",
			wantValue: "Marie",
		},
		{
			name: "a declared value that is also allowlisted",
			in: ValidationInput{
				Values:     []Value{{Category: CatEntityNames, MainText: "CSSF"}},
				Categories: PresetSelection(LevelMedium),
				Allowlist:  allowlistWith("CSSF"),
			},
			wantKind:  "collision",
			wantValue: "CSSF",
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
		Values: []Value{
			{Category: CatEntityNames, MainText: "Atlas"},
			{Category: CatProjectNames, MainText: "Atlas"},
		},
		Categories: sel,
	})
	if len(got) != 0 {
		t.Errorf("an off category must not conflict, got %+v", got)
	}
}

func TestABlockingConflictAbortsBeforeTheRegistryIsTouched(t *testing.T) {
	// A half-run that assigned placeholders for a configuration the user was
	// just told is invalid cannot be undone without a new session.
	registry := NewRegistry()
	res, err := Run(context.Background(), PipelineInput{
		Documents: []Document{{Name: "a.txt", Format: FormatTXT, Markdown: "Atlas met Atlas."}},
		Values: []Value{
			{Category: CatEntityNames, MainText: "Atlas"},
			{Category: CatProjectNames, MainText: "Atlas"},
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
		Values: []Value{
			{Category: CatPersonNames, MainText: "marie.duval@example.com"},
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
		Values: []Value{
			{Category: CatPersonNames, MainText: "marie.duval@example.com"},
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

// TestAllowlistCollisionStatesItsResolution: the engine says HOW to clear the
// conflict, not only that it exists.
//
// The refusal reaches the user on two screens, and each inferring the fix from a
// ref kind is two places deciding one thing. The Fix sentence stays what a human
// reads; the resolution is what a button performs.
func TestAllowlistCollisionStatesItsResolution(t *testing.T) {
	allow := NewEmptyAllowlist()
	allow.Add("Luxembourg")
	res := ValidateValues(ValidationInput{
		Values:     []Value{{Category: CatCountryNames, MainText: "Luxembourg"}},
		Allowlist:  allow,
		Categories: CategorySelection{CatCountryNames: true},
	})
	if len(res.Blocking) != 1 {
		t.Fatalf("expected one blocking conflict, got %d: %+v", len(res.Blocking), res.Blocking)
	}
	c := res.Blocking[0]
	if c.Resolution == nil {
		t.Fatal("the allowlist collision states no resolution, so both screens showing it " +
			"have to infer the fix from a ref kind, and two inferences can disagree")
	}
	if c.Resolution.Action != ResolutionDropAllowTerm {
		t.Errorf("the resolution action is %q, want %q", c.Resolution.Action, ResolutionDropAllowTerm)
	}
	if c.Resolution.Term != "Luxembourg" {
		t.Errorf("the resolution names the term %q, want the spelling the user sees, %q",
			c.Resolution.Term, "Luxembourg")
	}
	if c.Fix == "" {
		t.Error("the sentence a human reads is still required beside the action a button performs")
	}
}

// TestAmbiguityStatesNoResolution: a conflict no SINGLE gesture clears carries no
// resolution, and that is honest rather than missing. An ambiguity is cleared by
// deleting one of two Values, and only the user can say which.
func TestAmbiguityStatesNoResolution(t *testing.T) {
	res := ValidateValues(ValidationInput{
		Values: []Value{
			{Category: CatEntityNames, MainText: "Delta"},
			{Category: CatPersonNames, MainText: "Delta"},
		},
		Allowlist:  NewEmptyAllowlist(),
		Categories: CategorySelection{CatEntityNames: true, CatPersonNames: true},
	})
	if len(res.Blocking) == 0 {
		t.Fatal("the same name under two active categories must block the run")
	}
	for _, c := range res.Blocking {
		if c.Resolution != nil {
			t.Errorf("the %s conflict on %q states resolution %+v; nothing here can be fixed "+
				"without the user choosing which of the two Values to drop",
				c.Kind, c.Value, c.Resolution)
		}
	}
}

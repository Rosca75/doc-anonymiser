// engine/placeholder_override_test.go — the editable placeholder
// (BUILD-05 Phase 3).
//
// The registry is the re-identification key, so the risk in letting a user
// rename a placeholder is not a bad-looking name: it is AMBIGUITY. Two
// originals sharing one placeholder means the key can no longer say which
// original a placeholder came from, and the anonymisation silently stops being
// reversible. That is what most of these tests are about.
package engine

import (
	"strings"
	"testing"
)

func TestSetPlaceholderAcceptsAValidRename(t *testing.T) {
	reg := NewRegistry()
	reg.Assign("entity_names", "Meridian Consulting")
	reg.Assign("entity_names", "Meridian Consulting") // a second occurrence

	if err := reg.SetPlaceholder("entity_names", "Meridian Consulting", "[BANK_A_1]"); err != nil {
		t.Fatalf("a valid rename must be accepted: %v", err)
	}

	got, ok := reg.Lookup("entity_names", "Meridian Consulting")
	if !ok || got != "[BANK_A_1]" {
		t.Errorf("Lookup after rename = %q %v, want [BANK_A_1] true", got, ok)
	}
	// A rename must not reset the occurrence count: it renames the placeholder,
	// it does not un-count what has been replaced.
	for _, e := range reg.Export() {
		if e.Placeholder == "[BANK_A_1]" && e.Count != 2 {
			t.Errorf("occurrence count = %d, want 2: a rename must not reset counting", e.Count)
		}
	}
	// And the lookup stays case-insensitive, like every other registry lookup.
	if got, ok := reg.Lookup("entity_names", "MERIDIAN CONSULTING"); !ok || got != "[BANK_A_1]" {
		t.Errorf("case-insensitive lookup after rename = %q %v", got, ok)
	}
}

func TestSetPlaceholderRefusesAMalformedPlaceholder(t *testing.T) {
	// Each of these would survive being stored and then fail somewhere the user
	// cannot see: a placeholder has to be findable again by the key AND survive
	// being written into a Word or PowerPoint file, where a line break can split
	// a run.
	tests := []struct {
		in  string
		why string
	}{
		{in: "ENTITY_1", why: "no brackets, so it would not stand out in the text"},
		{in: "[client_1]", why: "lower case, so it would read as ordinary prose"},
		{in: "[CLIENT]", why: "no number, so two clients could not be told apart"},
		{in: "[ENTITY_0]", why: "zero, which no automatic assignment hands out"},
		{in: "[CLIENT 1]", why: "a space, which a docx line break can split"},
		{in: "[ENTITY_1] extra", why: "trailing text: the whole value is the placeholder"},
		{in: "", why: "empty"},
		{in: "   ", why: "whitespace only"},
		{in: "[_1]", why: "no name at all"},
		{in: "[ENTITY_1", why: "unclosed bracket"},
		{in: "[ENTITY_-1]", why: "a negative number"},
	}

	for _, tt := range tests {
		reg := NewRegistry()
		reg.Assign("entity_names", "Meridian Consulting")
		err := reg.SetPlaceholder("entity_names", "Meridian Consulting", tt.in)
		if err == nil {
			t.Errorf("%q must be refused (%s)", tt.in, tt.why)
			continue
		}
		// The message has to SHOW the shape, not just say "invalid": the owner
		// is not going to read the regex.
		if !strings.Contains(err.Error(), "[NAME_1]") {
			t.Errorf("%q: the refusal must show the expected shape, got: %v", tt.in, err)
		}
		// And the stored placeholder must be untouched.
		if got, _ := reg.Lookup("entity_names", "Meridian Consulting"); got != "[ENTITY_1]" {
			t.Errorf("%q: a refused rename must leave the placeholder alone, got %q", tt.in, got)
		}
	}
}

func TestSetPlaceholderRefusesACollision(t *testing.T) {
	reg := NewRegistry()
	reg.Assign("entity_names", "Meridian Consulting") // [ENTITY_1]
	reg.Assign("entity_names", "Aurora Group")        // [ENTITY_2]
	reg.Assign("person_names", "Marie Duval")         // [PERSON_1]

	// Same category.
	err := reg.SetPlaceholder("entity_names", "Aurora Group", "[ENTITY_1]")
	if err == nil {
		t.Fatal("renaming onto another value's placeholder must be refused")
	}
	if !strings.Contains(err.Error(), "Meridian Consulting") {
		t.Errorf("the refusal must name the value that already holds it, got: %v", err)
	}
	if !strings.Contains(err.Error(), "re-identification key") {
		t.Errorf("the refusal must say WHY it matters, got: %v", err)
	}

	// ACROSS categories too: the exported key is one flat list, so [PERSON_1]
	// meaning two different things is ambiguous even when the two sit in
	// different categories.
	if err := reg.SetPlaceholder("entity_names", "Aurora Group", "[PERSON_1]"); err == nil {
		t.Error("a collision across categories must be refused as well")
	}
	if got, _ := reg.Lookup("entity_names", "Aurora Group"); got != "[ENTITY_2]" {
		t.Errorf("a refused rename must leave the placeholder alone, got %q", got)
	}
}

func TestSetPlaceholderToItsOwnValueIsAcceptedAndRecorded(t *testing.T) {
	// Not a collision with itself. It is also the path a session load takes:
	// the saved registry already carries the renamed placeholder, so re-applying
	// it is what repopulates the override set.
	reg := NewRegistry()
	reg.Assign("entity_names", "Meridian Consulting")
	if err := reg.SetPlaceholder("entity_names", "Meridian Consulting", "[ENTITY_1]"); err != nil {
		t.Fatalf("renaming to the current value must be accepted: %v", err)
	}
	if len(reg.Overrides()) != 1 {
		t.Errorf("it must still be recorded as an override, got %v", reg.Overrides())
	}
}

func TestSetPlaceholderRefusesAnUnknownValue(t *testing.T) {
	reg := NewRegistry()
	err := reg.SetPlaceholder("entity_names", "Never Seen", "[ENTITY_9]")
	if err == nil {
		t.Fatal("there is nothing to rename before the value has a placeholder")
	}
	if !strings.Contains(err.Error(), "run the anonymisation") {
		t.Errorf("the refusal must say what to do first, got: %v", err)
	}
	// It must NOT invent an entry: a typo in the category would otherwise
	// create a second mapping no pass ever uses.
	if len(reg.Export()) != 0 {
		t.Errorf("a refused rename must not create an entry, got %v", reg.Export())
	}
}

func TestAssignSkipsANumberAnOverrideTook(t *testing.T) {
	// Renaming [ENTITY_1] to [ENTITY_3] means the third automatic client must
	// not also become [ENTITY_3]. The counter advances past it, so the numbers
	// simply have a gap.
	reg := NewRegistry()
	reg.Assign("entity_names", "First") // [ENTITY_1]
	if err := reg.SetPlaceholder("entity_names", "First", "[ENTITY_3]"); err != nil {
		t.Fatalf("SetPlaceholder: %v", err)
	}
	second := reg.Assign("entity_names", "Second")
	third := reg.Assign("entity_names", "Third")

	if second == "[ENTITY_3]" || third == "[ENTITY_3]" {
		t.Errorf("automatic assignment reused the overridden placeholder: %q, %q", second, third)
	}
	seen := map[string]string{}
	for _, e := range reg.Export() {
		if other, dup := seen[e.Placeholder]; dup {
			t.Errorf("%s is shared by %q and %q: the key would be ambiguous",
				e.Placeholder, other, e.Original)
		}
		seen[e.Placeholder] = e.Original
	}
}

func TestOverridesReportOnlyWhatTheUserRenamed(t *testing.T) {
	reg := NewRegistry()
	reg.Assign("entity_names", "Meridian Consulting")
	reg.Assign("person_names", "Marie Duval")
	if len(reg.Overrides()) != 0 {
		t.Errorf("nothing has been renamed yet, got %v", reg.Overrides())
	}

	if err := reg.SetPlaceholder("entity_names", "Meridian Consulting", "[BANK_A_1]"); err != nil {
		t.Fatalf("SetPlaceholder: %v", err)
	}
	overrides := reg.Overrides()
	if len(overrides) != 1 {
		t.Fatalf("Overrides() = %v, want exactly one entry", overrides)
	}
	// Keyed "category|lower-cased original", exactly as the registry keys its
	// entries, so ApplyOverrides can find the same entry again.
	if got := overrides["entity_names|meridian consulting"]; got != "[BANK_A_1]" {
		t.Errorf("Overrides() = %v, want entity_names|meridian consulting to be [BANK_A_1]", overrides)
	}

	// A rename that KEEPS the category's own label is still an override. This is
	// the case an "does it look automatic?" guess would get wrong, which is why
	// the set is recorded rather than inferred.
	if err := reg.SetPlaceholder("person_names", "Marie Duval", "[PERSON_9]"); err != nil {
		t.Fatalf("SetPlaceholder: %v", err)
	}
	if len(reg.Overrides()) != 2 {
		t.Errorf("a same-label rename must count as an override too, got %v", reg.Overrides())
	}

	// The returned map must be a COPY: a caller mutating it must not be able to
	// rewrite the registry's own record.
	reg.Overrides()["entity_names|meridian consulting"] = "[TAMPERED_1]"
	if reg.Overrides()["entity_names|meridian consulting"] != "[BANK_A_1]" {
		t.Error("Overrides() must return a copy, not the live map")
	}
}

func TestSessionRoundTripsPlaceholderOverrides(t *testing.T) {
	reg := NewRegistry()
	reg.Assign("entity_names", "Meridian Consulting")
	reg.Assign("person_names", "Marie Duval")
	if err := reg.SetPlaceholder("entity_names", "Meridian Consulting", "[BANK_A_1]"); err != nil {
		t.Fatalf("SetPlaceholder: %v", err)
	}

	raw, err := SaveSession(Session{
		Settings:             SessionSettings{Level: "medium", OllamaPort: 11434},
		Registry:             reg.Export(),
		PlaceholderOverrides: reg.Overrides(),
	})
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	loaded, err := LoadSession(raw)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}

	restored, failures := NewRegistryFromSession(loaded)
	if len(failures) != 0 {
		t.Fatalf("restoring the overrides must not fail: %v", failures)
	}
	if got, ok := restored.Lookup("entity_names", "Meridian Consulting"); !ok || got != "[BANK_A_1]" {
		t.Errorf("the renamed placeholder did not survive the round trip: %q %v", got, ok)
	}
	// The KNOWLEDGE that it was deliberate has to survive too, or saving again
	// would demote it to an automatic assignment.
	if len(restored.Overrides()) != 1 {
		t.Errorf("Overrides() after reload = %v, want the one rename", restored.Overrides())
	}
	// And an untouched value stays untouched.
	if got, ok := restored.Lookup("person_names", "Marie Duval"); !ok || got != "[PERSON_1]" {
		t.Errorf("an unrenamed value changed: %q %v", got, ok)
	}
}

func TestApplyOverridesCollectsFailuresInsteadOfAborting(t *testing.T) {
	// A session file carrying one stale override (its value is no longer in the
	// registry) must still restore the others: one bad entry cannot cost the
	// user the rest.
	reg := NewRegistry()
	reg.Assign("entity_names", "Meridian Consulting")
	reg.Assign("person_names", "Marie Duval")

	failures := reg.ApplyOverrides(map[string]string{
		"entity_names|meridian consulting": "[BANK_A_1]",
		"entity_names|long gone":           "[GONE_1]",    // no such entry
		"person_names|marie duval":         "not a shape", // malformed
		"malformed key":                    "[X_1]",       // no separator
	})
	if len(failures) != 3 {
		t.Errorf("failures = %v, want three (missing, malformed placeholder, malformed key)", failures)
	}
	if got, _ := reg.Lookup("entity_names", "Meridian Consulting"); got != "[BANK_A_1]" {
		t.Errorf("the good override must still have applied, got %q", got)
	}
	if got, _ := reg.Lookup("person_names", "Marie Duval"); got != "[PERSON_1]" {
		t.Errorf("a failed override must leave its value alone, got %q", got)
	}
}

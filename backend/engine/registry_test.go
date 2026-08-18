// engine/registry_test.go — regression tests for registry-specific behaviour:
// explicit placeholder-label coverage and deterministic entry ordering.
package engine

import "testing"

func TestEveryCategoryHasPlaceholderLabel(t *testing.T) {
	for _, category := range append(append([]string{}, AllPIICategories...), AllEntityCategories...) {
		if _, ok := placeholderLabels[category]; !ok {
			t.Fatalf("placeholder label missing for category %q", category)
		}
	}
}

func TestRegistryEntriesKeepStableOrderForEqualLengths(t *testing.T) {
	reg := NewRegistry()
	reg.entries[CatEntityNames+"|bravo"] = &MappingEntry{Original: "Bravo", Placeholder: "[ENTITY_2]", Category: CatEntityNames, Count: 1}
	reg.entries[CatEntityNames+"|alpha"] = &MappingEntry{Original: "Alpha", Placeholder: "[ENTITY_1]", Category: CatEntityNames, Count: 1}
	reg.entries[CatEntityNames+"|charlie"] = &MappingEntry{Original: "Charlie", Placeholder: "[ENTITY_3]", Category: CatEntityNames, Count: 1}

	got := reg.Entries()
	if len(got) != 3 {
		t.Fatalf("entries = %d, want 3", len(got))
	}
	if got[0].Original != "Charlie" || got[1].Original != "Alpha" || got[2].Original != "Bravo" {
		t.Fatalf("equal-length entries must stay in deterministic export order, got %+v", got)
	}
}

// --- Rename --------------------------------------------------------------

func TestRenameIsAddressedByPlaceholder(t *testing.T) {
	// Nothing called Rename until, and it was written with both a
	// deferred Unlock and an explicit one, so the deferred one released a mutex
	// SetPlaceholder had already released. Go answers that with a fatal error,
	// not a recoverable panic: the whole application died on a button the UI
	// offers. This test's real job is calling the function at all.
	reg := NewRegistry()
	original := reg.Assign(CatPersonNames, "Marie Duval")

	if err := reg.Rename(original, "[CHAIR_1]"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	got, ok := reg.Lookup(CatPersonNames, "Marie Duval")
	if !ok || got != "[CHAIR_1]" {
		t.Errorf("after the rename the value maps to %q (%v), want [CHAIR_1]", got, ok)
	}
	// Addressing by placeholder means the OLD one stops being an address.
	if err := reg.Rename(original, "[CHAIR_2]"); err == nil {
		t.Error("renaming from a placeholder that no longer exists must be refused")
	}
	// And the rename is recorded as deliberate, so a save writes it as an
	// override rather than demoting it to an automatic assignment.
	if len(reg.Overrides()) != 1 {
		t.Errorf("Overrides() = %v, want the one rename", reg.Overrides())
	}
}

func TestRenameRefusesAPlaceholderAnotherValueOwns(t *testing.T) {
	reg := NewRegistry()
	first := reg.Assign(CatPersonNames, "Marie Duval")
	second := reg.Assign(CatPersonNames, "Jean Weber")

	if err := reg.Rename(second, first); err == nil {
		t.Fatal("two originals behind one placeholder makes the key ambiguous and must be refused")
	}
	if got, _ := reg.Lookup(CatPersonNames, "Jean Weber"); got != second {
		t.Errorf("a refused rename must change nothing, got %q", got)
	}
}

// --- Restoring spent numbers ---------------------------------------------

func TestReloadDoesNotFreeTheNumbersARemovalRefusedToFree(t *testing.T) {
	// Forget deliberately does not free the number: an export may already carry
	// it. Before the retired set was in memory only, so saving and
	// reloading the session handed the number straight back out, which is the
	// same ambiguity arriving one round trip later.
	reg := NewRegistry()
	reg.Assign(CatPersonNames, "Marie Duval") // [PERSON_1]
	reg.Assign(CatPersonNames, "Jean Weber")  // [PERSON_2]
	if _, ok := reg.Forget(CatPersonNames, "Jean Weber"); !ok {
		t.Fatal("Forget did not find the entry")
	}
	session := Session{
		Version:             SessionVersion,
		Registry:            reg.Export(),
		RetiredPlaceholders: reg.Retired(),
	}
	restored, failures, err := NewRegistryFromSession(session)
	if err != nil {
		t.Fatalf("NewRegistryFromSession: %v", err)
	}
	if len(failures) != 0 {
		t.Fatalf("no overrides were saved, so none can fail: %v", failures)
	}

	if got := restored.Assign(CatPersonNames, "Someone New"); got == "[PERSON_2]" {
		t.Error("a retired number was handed out again after a reload")
	}
	// The counter moved with the set, or the numbering would climb back over
	// the retired one on every single assignment instead of once.
	if got := restored.Assign(CatPersonNames, "Another One"); got != "[PERSON_4]" {
		t.Errorf("numbering after the reload = %q, want [PERSON_4] (3 taken by the previous assign)", got)
	}
}

func TestACorruptKeyIsAnErrorRatherThanAPanic(t *testing.T) {
	// Two entries claiming one value. This runs behind a bound method on a file
	// the user picked, so panicking took the application down on a bad file.
	_, err := NewRegistryFromEntries([]MappingEntry{
		{Original: "Alpine Trust", Placeholder: "[ENTITY_1]", Category: CatEntityNames, Count: 1},
		{Original: "ALPINE TRUST", Placeholder: "[PERSON_1]", Category: CatPersonNames, Count: 1},
	})
	if err == nil {
		t.Fatal("a duplicated original must be reported as an error")
	}
}

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

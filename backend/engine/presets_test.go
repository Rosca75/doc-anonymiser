// engine/presets_test.go — the preset table's own guards.
//
// Four of these are the reason the table is data rather than a function, and
// every one of them fails silently without a test:
//
//   - a Categories entry outside its own scope would be IGNORED by ApplyPreset,
//     so the chip would look right and switch nothing;
//   - a preset writing outside its scope is the cross-section reach the whole
//     model exists to remove, and it is invisible from the rail;
//   - the depth sets are a transcription, so only an equality check against the
//     sets recorded in the change order proves the transcription;
//   - Soft and Standard are identical in the patterns scope, which is a fact
//     about the depths, so the first-match rule needs pinning or the row starts
//     flickering between two chips that both match.
package engine

import (
	"sort"
	"strings"
	"testing"
)

// depthPresets is the stored preset map for one depth applied to both rows: the
// shape Settings.Presets and SessionSettings.Presets carry. A helper because a
// dozen tests need "the session was on Standard" and hand-writing two keys each
// time is a dozen chances to key it wrong.
func depthPresets(id string) map[string]string {
	return map[string]string{
		PresetKey(ScopePatterns, FamilyDepth): id,
		PresetKey(ScopeNames, FamilyDepth):    id,
	}
}

// TestEveryPresetStaysInsideItsScope: the build-breaking check the change order
// asks for. ApplyPreset writes only its scope's categories, so a key outside it
// is a line in the table that does nothing at all.
func TestEveryPresetStaysInsideItsScope(t *testing.T) {
	for _, preset := range AllPresets {
		owned := PresetScopeCategories(preset.Scope)
		if len(owned) == 0 {
			t.Errorf("preset %q (%s/%s) is filed under scope %q, which owns no categories: "+
				"applying it would write nothing",
				preset.ID, preset.Family, preset.Scope, preset.Scope)
			continue
		}
		for _, category := range preset.Categories {
			if !contains(owned, category) {
				t.Errorf("preset %q (%s/%s) names %q, which is not in its scope.\n"+
					"ApplyPreset writes only the scope's own categories, so this key is\n"+
					"a chip that appears to switch something and does not. Either move the\n"+
					"category into this scope or declare a second row in the scope that owns it.",
					preset.ID, preset.Family, preset.Scope, category)
			}
		}
	}
}

// TestNoPresetTouchesAnAlwaysOnCategory: custom_patterns has no switch anywhere
// in the interface, so a preset must neither set nor clear it. A preset that
// cleared it would be a pattern editor whose patterns never run.
func TestNoPresetTouchesAnAlwaysOnCategory(t *testing.T) {
	for _, preset := range AllPresets {
		for _, category := range AlwaysOnCategories {
			if contains(preset.Categories, category) {
				t.Errorf("preset %q (%s/%s) names the always-on category %q",
					preset.ID, preset.Family, preset.Scope, category)
			}
			if contains(PresetScopeCategories(preset.Scope), category) {
				t.Errorf("scope %q owns the always-on category %q, so applying a preset "+
					"would clear a category with no switch", preset.Scope, category)
			}
		}
	}
	// And the whole-selection builder puts it back on, since that is where it
	// comes from at all.
	for _, category := range AlwaysOnCategories {
		if !DefaultSelection(CountryLU)[category] {
			t.Errorf("DefaultSelection leaves the always-on category %q off", category)
		}
	}
}

// TestEveryScopedCategoryBelongsToExactlyOneScope: the two scopes plus the
// always-on list have to cover CategorySelection exactly. A category in no scope
// is one no preset can ever reach; a category in two would be written twice, and
// the second write would silently win.
func TestEveryScopedCategoryBelongsToExactlyOneScope(t *testing.T) {
	seen := map[string]int{}
	for _, scope := range AllPresetScopes {
		for _, category := range PresetScopeCategories(scope) {
			seen[category]++
		}
	}
	for _, category := range append(append([]string(nil), AllPIICategories...), AllValueCategories...) {
		want := 1
		if contains(AlwaysOnCategories, category) {
			want = 0
		}
		if seen[category] != want {
			t.Errorf("category %q appears in %d preset scopes, want %d",
				category, seen[category], want)
		}
	}
}

// TestApplyPresetLeavesTheOtherScopeUntouched is the point of the whole order,
// asserted in BOTH directions: a patterns preset cannot move a name category and
// a names preset cannot move a pattern category. Proven by a wiring check rather
// than by reading the table, because the failure is a chip in one rail section
// changing a checkbox in another, which nothing on screen would announce.
func TestApplyPresetLeavesTheOtherScopeUntouched(t *testing.T) {
	// Start from a selection that is deliberately NOT any preset: every name
	// category on, every pattern category off. Whichever scope is not written
	// has to come back exactly like this.
	start := CategorySelection{}
	for _, category := range PresetScopeCategories(ScopePatterns) {
		start[category] = false
	}
	for _, category := range PresetScopeCategories(ScopeNames) {
		start[category] = true
	}

	for _, tc := range []struct{ write, untouched string }{
		{ScopePatterns, ScopeNames},
		{ScopeNames, ScopePatterns},
	} {
		for _, id := range PresetIDsFor(tc.write, FamilyDepth) {
			preset, ok := FindPreset(tc.write, FamilyDepth, id)
			if !ok {
				t.Fatalf("the table lost %s/%s/%s", tc.write, FamilyDepth, id)
			}
			got := ApplyPreset(start, preset)
			for _, category := range PresetScopeCategories(tc.untouched) {
				if got[category] != start[category] {
					t.Errorf("applying %s/%s changed %q, which belongs to the %s scope.\n"+
						"A chip in one rail section must not move a checkbox in another.",
						tc.write, id, category, tc.untouched)
				}
			}
			// And it DID write its own scope, or the test above would pass for
			// a preset that does nothing at all.
			for _, category := range PresetScopeCategories(tc.write) {
				if got[category] != contains(preset.Categories, category) {
					t.Errorf("applying %s/%s left %q at %v, want %v",
						tc.write, id, category, got[category],
						contains(preset.Categories, category))
				}
			}
		}
	}
}

// TestDepthPresetsFillTheRecordedSets: the depth table is a TRANSCRIPTION, so the
// sets are written out here independently and compared. Nothing about what Soft,
// Standard and Thorough switch on may change without this test changing with it,
// and the framework-agreement numbers are the other half of that proof.
func TestDepthPresetsFillTheRecordedSets(t *testing.T) {
	hard := []string{
		"email", "url", "iban", "vat", "matricule", "phone",
		"credit_card", "uk_nhs", "ip_address", "mac_address", "crypto",
		"database_uri", "de_steuer_id", "es_nif", "bic",
	}
	thoroughPatterns := append(append([]string(nil), hard...),
		"amount", "date", "address", "postal_code")
	softNames := []string{"entity_names", "project_names", "identifier_names"}
	standardNames := append(append([]string(nil), softNames...),
		"person_names", "product_names", "brand_names")
	thoroughNames := append(append([]string(nil), standardNames...),
		"other_names", "country_names", "nationality_names", "business_sector_names")

	for _, tc := range []struct {
		scope, id string
		want      []string
	}{
		{ScopePatterns, PresetSoft, hard},
		// Soft and Standard are IDENTICAL here. That is a fact about the depths:
		// Standard differs from Soft only in the name categories.
		{ScopePatterns, PresetStandard, hard},
		{ScopePatterns, PresetThorough, thoroughPatterns},
		{ScopeNames, PresetSoft, softNames},
		{ScopeNames, PresetStandard, standardNames},
		{ScopeNames, PresetThorough, thoroughNames},
	} {
		preset, ok := FindPreset(tc.scope, FamilyDepth, tc.id)
		if !ok {
			t.Fatalf("no %s preset in the %s scope", tc.id, tc.scope)
		}
		if got, want := sortedCopy(preset.Categories), sortedCopy(tc.want); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s/%s fills %v, want %v", tc.scope, tc.id, got, want)
		}
	}
}

// TestSoftAndStandardAreIdenticalInThePatternsScope, and the row therefore reads
// Soft: the first-match rule, pinned so it cannot regress into a row that
// flickers between two chips that both match.
func TestSoftAndStandardAreIdenticalInThePatternsScope(t *testing.T) {
	soft, _ := FindPreset(ScopePatterns, FamilyDepth, PresetSoft)
	standard, _ := FindPreset(ScopePatterns, FamilyDepth, PresetStandard)
	if strings.Join(sortedCopy(soft.Categories), ",") !=
		strings.Join(sortedCopy(standard.Categories), ",") {
		t.Fatalf("Soft and Standard have diverged in the patterns scope: %v versus %v",
			soft.Categories, standard.Categories)
	}
	// Soft comes first in the table, so it is what the row reads as, for both
	// depths' selections.
	for _, id := range []string{PresetSoft, PresetStandard} {
		got, ok := MatchingPreset(DepthSelection(id, CountryLU), ScopePatterns, FamilyDepth, CountryLU)
		if !ok || got.ID != PresetSoft {
			t.Errorf("the patterns row at %s reads as %q (found=%v), want soft: "+
				"the FIRST match in table order wins", id, got.ID, ok)
		}
	}
	// The names row still tells them apart, which is where the difference is.
	for _, id := range []string{PresetSoft, PresetStandard, PresetThorough} {
		got, ok := MatchingPreset(DepthSelection(id, CountryLU), ScopeNames, FamilyDepth, CountryLU)
		if !ok || got.ID != id {
			t.Errorf("the names row at %s reads as %q (found=%v), want %s",
				id, got.ID, ok, id)
		}
	}
}

// TestASelectionMatchingNoPresetReadsAsCustom, PER ROW: flipping one pattern
// category must not make the names row read as Custom too, or the rail would
// report a change the user did not make.
func TestASelectionMatchingNoPresetReadsAsCustom(t *testing.T) {
	sel := DepthSelection(PresetStandard, CountryLU)
	sel[CatAmount] = true // a Thorough-only pattern category, on its own

	if _, ok := MatchingPreset(sel, ScopePatterns, FamilyDepth, CountryLU); ok {
		t.Errorf("the patterns row must read as Custom once a single pattern category diverges")
	}
	got, ok := MatchingPreset(sel, ScopeNames, FamilyDepth, CountryLU)
	if !ok || got.ID != PresetStandard {
		t.Errorf("the names row reads as %q (found=%v) after a PATTERN category moved, "+
			"want standard: the rows are derived independently", got.ID, ok)
	}

	// And the derived map leaves the Custom row out altogether, which is how
	// Custom is representable at all.
	all := MatchingPresets(sel, CountryLU)
	if _, present := all[PresetKey(ScopePatterns, FamilyDepth)]; present {
		t.Errorf("MatchingPresets stored a key for a Custom row: %v", all)
	}
	if all[PresetKey(ScopeNames, FamilyDepth)] != PresetStandard {
		t.Errorf("MatchingPresets = %v, want the names row on standard", all)
	}
}

// TestMatchingPresetFollowsTheDocumentCountry: the national identifier
// categories are expected OFF wherever they do not apply, whatever the preset
// names. Without that mask every document ever loaded would read as Custom,
// because no country has all four.
func TestMatchingPresetFollowsTheDocumentCountry(t *testing.T) {
	for _, country := range SupportedCountries {
		sel := DepthSelection(PresetThorough, CountryLU)
		for _, category := range CountryIDCategories {
			sel[category] = CategoryAppliesTo(category, country)
		}
		got, ok := MatchingPreset(sel, ScopePatterns, FamilyDepth, country)
		if !ok {
			t.Errorf("%s: a Thorough selection with the country's own identifiers reads as Custom",
				country)
			continue
		}
		// Soft first, because Soft and Standard are identical here.
		if got.ID != PresetSoft && got.ID != PresetThorough {
			t.Errorf("%s: the patterns row reads as %q", country, got.ID)
		}
	}
}

// TestDefaultSelectionIsTheStandardDepth: the pipeline's fallback, spelled out.
// It is what a run with no selection obeys, so a drift here changes what every
// caller that passes nil detects.
func TestDefaultSelectionIsTheStandardDepth(t *testing.T) {
	def := DefaultSelection(CountryLU)
	std := DepthSelection(PresetStandard, CountryLU)
	for _, category := range append(append([]string(nil), AllPIICategories...), AllValueCategories...) {
		if def[category] != std[category] {
			t.Errorf("DefaultSelection has %q at %v, the Standard depth has it at %v",
				category, def[category], std[category])
		}
	}
	// A map, not a shared package variable: a caller writing into one default
	// must not change what the next run detects.
	def[CatEmail] = false
	if !DefaultSelection(CountryLU)[CatEmail] {
		t.Errorf("DefaultSelection returns shared state: writing into one result changed the next")
	}
}

// TestValidatePresetsRefusesWhatNoRowHolds, with a message naming what is valid.
// The person who sees it has a hand-edited session file, not this table.
func TestValidatePresetsRefusesWhatNoRowHolds(t *testing.T) {
	if err := ValidatePresets(nil); err != nil {
		t.Errorf("an absent preset map is Custom on every row and must be valid, got %v", err)
	}
	if err := ValidatePresets(depthPresets(PresetStandard)); err != nil {
		t.Errorf("the default presets must be valid, got %v", err)
	}
	for _, tc := range []struct {
		name    string
		presets map[string]string
		names   string
	}{
		{"unknown scope", map[string]string{"pattern.depth": PresetSoft}, "patterns.depth"},
		{"unknown family", map[string]string{"patterns.regulatory": PresetSoft}, "patterns.depth"},
		{"no separator", map[string]string{"depth": PresetSoft}, "patterns.depth"},
		{"unknown preset", map[string]string{"patterns.depth": "paranoid"}, "soft"},
	} {
		err := ValidatePresets(tc.presets)
		if err == nil {
			t.Errorf("%s: %v was accepted", tc.name, tc.presets)
			continue
		}
		if !strings.Contains(err.Error(), tc.names) {
			t.Errorf("%s: %q does not name what is valid (%q)", tc.name, err, tc.names)
		}
	}
}

// TestPresetKeyRoundTrips: the storage key is the only thing tying a stored row
// back to the table, so splitting it has to be exact.
func TestPresetKeyRoundTrips(t *testing.T) {
	for _, scope := range AllPresetScopes {
		for _, family := range PresetFamilies(scope) {
			key := PresetKey(scope, family)
			gotScope, gotFamily, ok := SplitPresetKey(key)
			if !ok || gotScope != scope || gotFamily != family {
				t.Errorf("SplitPresetKey(%q) = %q, %q, %v", key, gotScope, gotFamily, ok)
			}
			if !contains(AllPresetKeys(), key) {
				t.Errorf("AllPresetKeys omits %q: the error messages would not name it", key)
			}
		}
	}
}

// TestEveryFamilyHasAtLeastOnePresetPerScopeItAppearsIn: a family listed for a
// scope with nothing behind it would render an empty chip row.
func TestEveryFamilyHasAtLeastOnePresetPerScopeItAppearsIn(t *testing.T) {
	for _, scope := range AllPresetScopes {
		families := PresetFamilies(scope)
		if len(families) == 0 {
			t.Errorf("scope %q has no preset family at all, so its section renders no chips", scope)
		}
		for _, family := range families {
			if len(PresetsFor(scope, family)) == 0 {
				t.Errorf("family %q is listed for scope %q with no presets in it", family, scope)
			}
		}
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

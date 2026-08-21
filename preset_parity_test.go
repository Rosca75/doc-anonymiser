// preset_parity_test.go — the JavaScript to Go PRESET parity guard.
//
// A preset is a contract, exactly as a category is, and it fails silently when
// the two sides drift. The rail renders its chips from the JS table and writes the
// selection from the JS categories; the engine derives the run report's presets
// from the SAME question against the Go table. So a preset only Go knows is a chip
// the user cannot reach, one only the frontend knows is a chip that writes a set
// the engine never agreed to, and a preset the two sides fill DIFFERENTLY is the
// worst of the three: the chip looks active, the run obeys the selection, and the
// report names a preset that switched on something else.
//
// It parses the literal PRESETS table out of frontend/state.js, resolving the
// spread constants against the category lists frontendCategories already reads.
// That is why the JS table spreads those constants rather than repeating the keys:
// a recogniser added to a tier arrives in the presets on both sides at once.
package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
)

// presetTableRe grabs the whole PRESETS array literal.
var presetTableRe = regexp.MustCompile(`(?s)export const PRESETS = \[(.*?)\n\];`)

// presetRowRe splits that body into one match per row object. Rows are the only
// braces in the literal, so a non-greedy run between them is exact.
var presetRowRe = regexp.MustCompile(`(?s)\{(.*?)\}`)

// presetFieldRe pulls one quoted field off a row ("scope", "family", "id",
// "label").
var presetFieldRe = regexp.MustCompile(`(\w+): "([^"]*)"`)

// presetCategoriesRe pulls a row's categories array body.
var presetCategoriesRe = regexp.MustCompile(`(?s)categories: \[(.*?)\]`)

// spreadRe pulls the spread constant names out of that body.
var spreadRe = regexp.MustCompile(`\.\.\.([A-Z_]+)`)

// jsPreset is one parsed row.
type jsPreset struct {
	scope, family, id, label string
	categories               []string
}

// frontendPresets parses the mirrored table. It fails LOUDLY rather than passing
// on an empty set, because a guard that silently reads nothing is worse than no
// guard.
func frontendPresets(t *testing.T) []jsPreset {
	t.Helper()
	raw, err := os.ReadFile("frontend/state.js")
	if err != nil {
		t.Fatalf("could not read frontend/state.js: %v", err)
	}
	table := presetTableRe.FindStringSubmatch(string(raw))
	if table == nil {
		t.Fatal("frontend/state.js declares no PRESETS table: the rail's chips would come " +
			"from nowhere and this guard would compare nothing")
	}
	byList, _ := frontendCategories(t)

	var out []jsPreset
	for _, row := range presetRowRe.FindAllStringSubmatch(table[1], -1) {
		body := row[1]
		p := jsPreset{}
		for _, f := range presetFieldRe.FindAllStringSubmatch(body, -1) {
			switch f[1] {
			case "scope":
				p.scope = f[2]
			case "family":
				p.family = f[2]
			case "id":
				p.id = f[2]
			case "label":
				p.label = f[2]
			}
		}
		cats := presetCategoriesRe.FindStringSubmatch(body)
		if cats == nil {
			t.Fatalf("the PRESETS row %q/%q names no categories array", p.scope, p.id)
		}
		for _, sp := range spreadRe.FindAllStringSubmatch(cats[1], -1) {
			keys, present := byList[sp[1]]
			if !present {
				t.Fatalf("the PRESETS row %q/%q spreads %s, which frontend/state.js does not "+
					"declare as a category list this guard can read", p.scope, p.id, sp[1])
			}
			p.categories = append(p.categories, keys...)
		}
		// A bare quoted key is allowed too, so the table is not forced to invent
		// a constant for a one-off category.
		for _, q := range quotedRe.FindAllStringSubmatch(cats[1], -1) {
			p.categories = append(p.categories, q[1])
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		t.Fatal("the PRESETS table parsed to no rows at all")
	}
	return out
}

// TestPresetTableParity: the same rows, in the same order, filling the same
// categories, on both sides.
//
// ORDER matters twice over. It is the chips' display order, and it is the
// first-match rule both sides apply when deriving which preset a selection reads
// as: with the two tables in different orders, a selection that matches two rows
// would read as one preset in the rail and another in the run report.
func TestPresetTableParity(t *testing.T) {
	js := frontendPresets(t)
	if len(js) != len(engine.AllPresets) {
		t.Fatalf("frontend/state.js PRESETS has %d rows, engine.AllPresets has %d.\n"+
			"Adding a preset is one row per scope on EACH side; a row on one side only is\n"+
			"either a chip the engine never agreed to or a preset the user cannot reach.",
			len(js), len(engine.AllPresets))
	}
	for i, want := range engine.AllPresets {
		got := js[i]
		if got.scope != want.Scope || got.family != want.Family || got.id != want.ID {
			t.Errorf("row %d is %s/%s/%s in frontend/state.js and %s/%s/%s in engine.AllPresets.\n"+
				"The order is the chips' display order AND the first-match rule, so it has to match.",
				i, got.scope, got.family, got.id, want.Scope, want.Family, want.ID)
			continue
		}
		if got.label != want.Label {
			t.Errorf("%s/%s is labelled %q in frontend/state.js and %q in engine.AllPresets: "+
				"the rail renders the JS label and the run report renders the Go one, so a "+
				"reader would see two names for one preset",
				want.Scope, want.ID, got.label, want.Label)
		}
		gotCats, wantCats := sortedUnique(got.categories), sortedUnique(want.Categories)
		if strings.Join(gotCats, ",") != strings.Join(wantCats, ",") {
			t.Errorf("%s/%s fills %v in frontend/state.js and %v in engine.AllPresets.\n"+
				"The chip writes the JS set and the run obeys it; the report names the preset by\n"+
				"the Go set. When the two differ the chip looks active and switched on something\n"+
				"the named preset does not include.",
				want.Scope, want.ID, gotCats, wantCats)
		}
	}
}

// TestPresetScopesAgreeAcrossTheBridge: the scopes themselves, in rail order. A
// scope only one side knows owns no categories on the other, so every preset filed
// under it silently writes nothing.
func TestPresetScopesAgreeAcrossTheBridge(t *testing.T) {
	raw, err := os.ReadFile("frontend/state.js")
	if err != nil {
		t.Fatalf("could not read frontend/state.js: %v", err)
	}
	m := regexp.MustCompile(`export const PRESET_SCOPES = \[(.*?)\]`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("frontend/state.js declares no PRESET_SCOPES list")
	}
	// The list is built from the two named constants, so read those instead of
	// quoted strings: naming a scope by position in the list is exactly the
	// arithmetic the named constants exist to avoid.
	var js []string
	for _, name := range regexp.MustCompile(`PRESET_SCOPE_([A-Z]+)`).FindAllStringSubmatch(m[1], -1) {
		js = append(js, strings.ToLower(name[1]))
	}
	if strings.Join(js, ",") != strings.Join(engine.AllPresetScopes, ",") {
		t.Errorf("PRESET_SCOPES is %v, engine.AllPresetScopes is %v", js, engine.AllPresetScopes)
	}
}

// TestEveryPresetFamilyHasARowLabel: the rail titles each chip row from
// copy.js RAIL.presetFamilyLabel, keyed by family. An unlabelled family renders a
// row titled after a JSON key, which is the unexplained jargon the copy rules
// forbid, and it is the reason a family can be added without touching a view.
func TestEveryPresetFamilyHasARowLabel(t *testing.T) {
	raw, err := os.ReadFile("frontend/copy.js")
	if err != nil {
		t.Fatalf("could not read frontend/copy.js: %v", err)
	}
	copyJS := string(raw)
	seen := map[string]bool{}
	for _, scope := range engine.AllPresetScopes {
		for _, family := range engine.PresetFamilies(scope) {
			if seen[family] {
				continue
			}
			seen[family] = true
			if !strings.Contains(copyJS, family+": \"") {
				t.Errorf("copy.js RAIL.presetFamilyLabel has no entry for the %q family: "+
					"its chip row would be titled after an identifier", family)
			}
		}
	}
}

// TestEveryPresetScopeHasARunNoteLabel: the Anonymise run note names the scope a
// preset row belongs to, so an unlabelled scope reads as "patterns" in a sentence
// meant for a person.
func TestEveryPresetScopeHasARunNoteLabel(t *testing.T) {
	raw, err := os.ReadFile("frontend/copy.js")
	if err != nil {
		t.Fatalf("could not read frontend/copy.js: %v", err)
	}
	block := regexp.MustCompile(`(?s)presetScopeLabel: \{(.*?)\}`).FindStringSubmatch(string(raw))
	if block == nil {
		t.Fatal("copy.js declares no ANONYMISE.presetScopeLabel table")
	}
	for _, scope := range engine.AllPresetScopes {
		if !strings.Contains(block[1], scope+": \"") {
			t.Errorf("copy.js presetScopeLabel has no entry for the %q scope", scope)
		}
	}
}

// sortedUnique is what the category comparison needs: the tables build a row by
// concatenating tier lists, so order is not meaningful and a key could in
// principle appear twice.
func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

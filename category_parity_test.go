// category_parity_test.go — the JavaScript to Go category parity guard
//
// The bug this exists to prevent already happened once:  added
// eight recognizers to engine/pii.go and engine/pipeline.go, and nobody
// added them to frontend/state.js. They detected nothing in practice,
// because the Configure screen never sent a switch for them and never
// showed one either. Nothing failed; the feature was simply invisible.
//
// So this test reads frontend/state.js, extracts the category key lists it
// declares, and compares that set against the categories the Go engine
// actually knows. A recognizer added on one side and forgotten on the
// other now breaks the build with a message naming it.
//
// It parses literal string arrays only, which is all state.js uses for
// these lists, and it fails loudly if it cannot find them rather than
// silently passing on an empty set.
package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
)

// categoryListRe matches `export const NAME = ["a", "b", ...];` including
// multi-line array literals, which is exactly how state.js declares each
// category group.
var categoryListRe = regexp.MustCompile(
	`(?s)export const ([A-Z_]*CATEGORIES) = \[(.*?)\]`)

// quotedRe pulls the double-quoted strings out of one array literal body.
var quotedRe = regexp.MustCompile(`"([a-z0-9_]+)"`)

// frontendCategories returns every category key state.js lists, keyed by
// the constant it came from, plus the flat union of all of them.
func frontendCategories(t *testing.T) (map[string][]string, map[string]bool) {
	t.Helper()
	raw, err := os.ReadFile("frontend/state.js")
	if err != nil {
		t.Fatalf("could not read frontend/state.js: %v", err)
	}

	byList := map[string][]string{}
	union := map[string]bool{}
	for _, m := range categoryListRe.FindAllStringSubmatch(string(raw), -1) {
		name, body := m[1], m[2]
		// ALL_CATEGORIES is built by spreading the others, so its literal
		// body carries only "custom_patterns". Collect it anyway: that is
		// how custom_patterns enters the union.
		var keys []string
		for _, q := range quotedRe.FindAllStringSubmatch(body, -1) {
			keys = append(keys, q[1])
			union[q[1]] = true
		}
		byList[name] = keys
	}

	if len(byList) == 0 {
		t.Fatal("found no `export const ..._CATEGORIES = [...]` list in frontend/state.js; " +
			"this guard cannot work, fix the parser or the declaration style")
	}
	return byList, union
}

func TestEveryEngineCategoryIsKnownToTheFrontend(t *testing.T) {
	_, union := frontendCategories(t)

	var missing []string
	for _, category := range append(
		append([]string{}, engine.AllPIICategories...),
		engine.AllEntityCategories...) {
		if !union[category] {
			missing = append(missing, category)
		}
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Errorf("the Go engine detects categories the Configure screen never shows or sends: %s\n"+
			"Add each one to a list in frontend/state.js, to CATEGORY_LABELS in frontend/copy.js, "+
			"and to a group in frontend/views/identifyrail.js. Detecting a category the UI cannot "+
			"switch means it is silently inactive (BUILD-04 CR9).",
			strings.Join(missing, ", "))
	}
}

func TestFrontendInventsNoCategory(t *testing.T) {
	_, union := frontendCategories(t)

	known := map[string]bool{}
	for _, c := range engine.AllPIICategories {
		known[c] = true
	}
	for _, c := range engine.AllEntityCategories {
		known[c] = true
	}

	var unknown []string
	for c := range union {
		if !known[c] {
			unknown = append(unknown, c)
		}
	}
	sort.Strings(unknown)

	if len(unknown) > 0 {
		t.Errorf("frontend/state.js lists categories the engine does not know: %s\n"+
			"A switch for a category no pass reads does nothing at all.",
			strings.Join(unknown, ", "))
	}
}

func TestExtendedRecognizersAreOnAtEveryPreset(t *testing.T) {
	//  made the extended recognizers hard PII, active at every
	// level. The frontend preset function mirrors this by hand, so pin the
	// Go side here and the JS side in state.test.js.
	extended := []string{
		engine.CatCreditCard, engine.CatNHS, engine.CatIPAddress,
		engine.CatMACAddress, engine.CatCrypto, engine.CatDatabaseURI,
		engine.CatDESteuerID, engine.CatESNIF,
	}
	for _, level := range []engine.Level{engine.LevelSoft, engine.LevelMedium, engine.LevelAdvanced} {
		sel := engine.PresetSelection(level)
		for _, c := range extended {
			if !sel[c] {
				t.Errorf("%s: %s must be on at every preset", level, c)
			}
		}
	}
}

func TestEveryEngineCategoryHasAFrontendLabel(t *testing.T) {
	// A category with no CATEGORY_LABELS entry renders as its raw
	// identifier ("de_steuer_id") with no example, which is exactly the
	// unexplained jargon the copy rules forbid.
	raw, err := os.ReadFile("frontend/copy.js")
	if err != nil {
		t.Fatalf("could not read frontend/copy.js: %v", err)
	}
	copyJS := string(raw)

	var missing []string
	for _, category := range append(
		append([]string{}, engine.AllPIICategories...),
		engine.AllEntityCategories...) {
		if !strings.Contains(copyJS, fmt.Sprintf("\n  %s: [", category)) {
			missing = append(missing, category)
		}
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Errorf("no CATEGORY_LABELS entry in frontend/copy.js for: %s\n"+
			"Each needs a plain-language label and one example.",
			strings.Join(missing, ", "))
	}
}

func TestCountryTableParity(t *testing.T) {
	raw, err := os.ReadFile("frontend/countries.js")
	if err != nil {
		t.Fatalf("could not read frontend/countries.js: %v", err)
	}
	js := string(raw)
	for category, countries := range engine.CategoryCountriesCopy() {
		if !strings.Contains(js, fmt.Sprintf("%s:", category)) {
			t.Fatalf("frontend/countries.js is missing CATEGORY_COUNTRIES row for %s", category)
		}
		for _, code := range countries {
			if !strings.Contains(js, fmt.Sprintf(`"%s"`, code)) {
				t.Fatalf("frontend/countries.js is missing country %s for %s", code, category)
			}
		}
	}
}

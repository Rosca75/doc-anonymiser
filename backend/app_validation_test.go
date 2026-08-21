// app_validation_test.go — the UNIT-tier bound Value surface.
//
// TIER: unit (docs/TESTING.md). These are the hermetic, in-memory tests of the
// App's value and validation methods: the spelling-expansion adapter, pattern
// validation, live match counting, curated-spelling expansion, and the offline
// discovery and intersection-check methods, none of which reach a model. The
// tests that drive discovery against a MOCK Ollama server live in
// app_validation_integration_test.go.
package backend

import (
	"context"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
)

func TestExpandEntityVariantsAdapter(t *testing.T) {
	app := NewApp()
	got := app.ExpandValueSpellings(engine.Value{Category: "person_names", MainText: "Marie Duval"})
	joined := strings.Join(got, "|")
	for _, want := range []string{"Marie Duval", "M. Duval", "Duval", "Marie"} {
		if !strings.Contains(joined, want) {
			t.Errorf("adapter lost variant %q: %v", want, got)
		}
	}
}

func TestDetectionOverZeroFilesFailsActionably(t *testing.T) {
	if _, err := NewApp().RunDetection([]string{"ghost.txt"}, nil, nil); err == nil {
		t.Error("detection over zero files must fail rather than report an empty success")
	}
}

func TestValidateAndTestPattern(t *testing.T) {
	app := NewApp()
	if msg := app.ValidatePattern("PRJ-[0-9]+"); msg != "" {
		t.Errorf("valid pattern flagged: %s", msg)
	}
	if msg := app.ValidatePattern("["); msg == "" || !strings.Contains(msg, "regular expression") {
		t.Errorf("invalid pattern needs an actionable message, got %q", msg)
	}

	app.docs = []engine.Document{{
		Name: "a.txt", Format: engine.FormatTXT,
		Markdown: "codes PRJ-1 PRJ-2 PRJ-1 here",
	}}
	samples, err := app.PatternMatches("PRJ-[0-9]+")
	if err != nil {
		t.Fatalf("PatternMatches: %v", err)
	}
	// Distinct matches only.
	if len(samples) != 2 || samples[0] != "PRJ-1" || samples[1] != "PRJ-2" {
		t.Errorf("samples = %v, want [PRJ-1 PRJ-2]", samples)
	}
	if _, err := app.PatternMatches("["); err == nil {
		t.Error("PatternMatches must reject an invalid pattern")
	}
}

// TestCountTermMatches: word-boundary counting for the live preview
// ("Lux" must not match inside "Luxembourg").
func TestCountTermMatches(t *testing.T) {
	app := NewApp()
	app.docs = []engine.Document{
		{Name: "a.txt", Format: engine.FormatTXT, Markdown: "Lux is nice. Luxembourg is bigger. lux again."},
		{Name: "b.txt", Format: engine.FormatTXT, Markdown: "No mention here."},
		{Name: "c.txt", Format: engine.FormatTXT, Markdown: "Lux once more."},
	}
	cases := []struct {
		term      string
		count     int
		documents int
	}{
		{"Lux", 3, 2},        // case-insensitive, boundary-anchored
		{"Luxembourg", 1, 1}, // the long word matches itself only
		{"absent", 0, 0},
		{"", 0, 0},
	}
	for _, tc := range cases {
		got := app.CountTermMatches(tc.term)
		if got.Count != tc.count || got.Documents != tc.documents {
			t.Errorf("CountTermMatches(%q) = %+v, want {%d %d}", tc.term, got, tc.count, tc.documents)
		}
	}
}

// TestCuratedSpellings: once the user has curated a Value's spellings, the engine
// derives nothing further, so the chips on the card are exactly what the run
// replaces. This is what makes a deleted spelling stay deleted: there is no
// derivation left to produce it again.
func TestCuratedSpellings(t *testing.T) {
	got := engine.ExpandSpellings(engine.Value{
		Category:       "person_names",
		MainText:       "Jean Muller",
		Spellings:      []string{"Jean"},
		SpellingPolicy: engine.SpellingPolicyCurated,
	})
	for _, v := range got {
		if strings.EqualFold(v, "J. Muller") || strings.EqualFold(v, "Muller") {
			t.Errorf("a curated Value still derived %q: %v", v, got)
		}
	}
	want := map[string]bool{"Jean Muller": true, "Jean": true}
	if len(got) != len(want) {
		t.Fatalf("a curated Value expands to its main text plus its own spellings, got %v", got)
	}
	for _, v := range got {
		if !want[v] {
			t.Errorf("unexpected spelling %q in a curated expansion: %v", v, got)
		}
	}
}

// TestOfflineRouteReturnsSuggestionsNotValues: discovery produces Suggestions
// for review; the App holds no entity state to mutate, and nothing is replaced
// until the user accepts a suggestion. This runs the offline (heuristic) route
// only, so it reaches no model and stays a unit test.
func TestOfflineRouteReturnsSuggestionsNotValues(t *testing.T) {
	app := NewApp()
	// The route is off by default now, so the test that exercises it turns it on:
	// what is asserted here is the SHAPE of what it returns, not whether a fresh
	// session runs it.
	app.settings.UseHeuristicDiscovery = true
	app.settings.HeuristicDiscovery = engine.HeuristicDiscoveryOptions{} // no filtering
	app.docs = []engine.Document{
		{
			Name: "a.txt", Format: engine.FormatTXT,
			Markdown: "Meeting with Marie Duval about Alpine Trust S.A. Later Marie Duval called again.",
		},
	}

	res, err := app.RunDetection([]string{"a.txt"}, []string{"CSSF"}, nil)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	if res.Cancelled {
		t.Errorf("a completed run must not report itself cancelled: %+v", res)
	}
	byText := map[string]engine.Suggestion{}
	for _, c := range res.Suggestions {
		byText[c.MainText] = c
	}
	if c, ok := byText["Marie Duval"]; !ok || c.Category != engine.CatPersonNames || c.Count < 2 {
		t.Errorf("the person suggestion is wrong: %+v", res.Suggestions)
	}
	if c, ok := byText["Alpine Trust S.A."]; !ok || c.Category != engine.CatEntityNames {
		t.Errorf("the legal-suffix suggestion is wrong: %+v", res.Suggestions)
	}
}

// TestCheckIntersectionsAgreesWithARealRun: the bound method and the pipeline
// must reach the same verdict on the same input. That is why the check reuses
// the engine's own detection rather than a parallel one, and it is what this
// test holds: a warning that disagrees with the run describes something that
// did not happen, and the user cannot act on it.
func TestCheckIntersectionsAgreesWithARealRun(t *testing.T) {
	const value = "marie.duval@example.com"
	app := NewApp()
	app.docs = []engine.Document{{
		Name: "a.txt", Format: engine.FormatTXT,
		Markdown: "Write to " + value + " today.\n",
	}}
	entities := []engine.Value{{Category: engine.CatPersonNames, MainText: value}}

	res, err := app.CheckIntersections(CheckIntersectionsRequest{Values: entities})
	if err != nil {
		t.Fatalf("CheckIntersections: %v", err)
	}
	if len(res.Intersections) != 1 {
		t.Fatalf("the declared value is covered by the email signal, got %+v", res.Intersections)
	}
	row := res.Intersections[0]
	if row.Value != value || row.Category != engine.CatPersonNames {
		t.Errorf("the row must name the value that lost, got %+v", row)
	}

	// The check must not have touched the registry: it runs while the user is
	// still editing values, and minting a placeholder there would spend a
	// number on a configuration that may never be run.
	if app.registry != nil && len(app.registry.Entries()) != 0 {
		t.Errorf("CheckIntersections must mint nothing, got %+v", app.registry.Entries())
	}

	// Now run for real and compare verdicts.
	if _, err := app.runPipelineBlocking(context.Background(), RunRequest{
		Values: entities,
	}); err != nil {
		t.Fatalf("runPipelineBlocking: %v", err)
	}
	var owner string
	for _, e := range app.registry.Entries() {
		if strings.EqualFold(e.Original, value) {
			owner = e.Category
		}
	}
	if owner != row.WinnerCategory {
		t.Errorf("the check predicted %s and the run filed the value under %s",
			row.WinnerCategory, owner)
	}
}

// TestCheckIntersectionsIsQuietWhenNothingOverlaps: an empty list is the
// normal answer, not an error. A screen that calls this on every edit must not
// have to distinguish "nothing overlaps" from "the call failed".
func TestCheckIntersectionsIsQuietWhenNothingOverlaps(t *testing.T) {
	app := NewApp()
	app.docs = []engine.Document{{
		Name: "a.txt", Format: engine.FormatTXT,
		Markdown: "Alpine Trust met Borealis Capital on the Tuesday.\n",
	}}
	res, err := app.CheckIntersections(CheckIntersectionsRequest{
		Values: []engine.Value{
			{Category: engine.CatEntityNames, MainText: "Alpine Trust"},
			{Category: engine.CatBrandNames, MainText: "Borealis Capital"},
		},
	})
	if err != nil {
		t.Fatalf("CheckIntersections: %v", err)
	}
	if len(res.Intersections) != 0 {
		t.Errorf("nothing overlaps here, got %+v", res.Intersections)
	}
}

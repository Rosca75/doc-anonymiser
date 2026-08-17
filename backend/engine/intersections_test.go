package engine

import (
	"context"
	"strings"
	"testing"
)

// scopeFor is the detection configuration these tests share: every category on,
// no confidence floor, Luxembourg, nothing allowlisted.
func scopeFor(entities []Entity, patterns []CustomPattern) detectionScope {
	return NewDetectionScope(entities, patterns,
		PresetSelection(LevelAdvanced), 0, CountryLU, NewEmptyAllowlist(), false)
}

// findIntersection returns the row for one value under one category.
func findIntersection(rows []Intersection, value, category string) (Intersection, bool) {
	for _, r := range rows {
		if strings.EqualFold(r.Value, value) && r.Category == category {
			return r, true
		}
	}
	return Intersection{}, false
}

// TestIntersectionEmailCoversADeclaredValue: the commonest shape. The user
// declared an address as a person, and pass 1 recognises it as an email, so the
// value is NEVER replaced under its own type. That is the case worth warning
// about, and Occurrences == TotalOccurrences is how the view knows.
func TestIntersectionEmailCoversADeclaredValue(t *testing.T) {
	const value = "marie.duval@example.com"
	docs := []Document{{Name: "a.txt", Format: FormatTXT,
		Markdown: "Write to " + value + " today, or to " + value + " tomorrow.\n"}}

	rows := DetectIntersections(docs,
		scopeFor([]Entity{{Category: CatPersonNames, Canonical: value}}, nil))

	row, ok := findIntersection(rows, value, CatPersonNames)
	if !ok {
		t.Fatalf("the declared value must be reported as covered, got %+v", rows)
	}
	if row.WinnerCategory != CatEmail || row.WinnerOrigin != OriginNative {
		t.Errorf("native detection must be named as the winner, got %s / %s",
			row.WinnerCategory, row.WinnerOrigin)
	}
	if row.Origin != OriginDeclared {
		t.Errorf("the losing claim is the user's declaration, got %q", row.Origin)
	}
	if row.Occurrences != 2 || row.TotalOccurrences != 2 {
		t.Errorf("both occurrences are covered, got %d of %d",
			row.Occurrences, row.TotalOccurrences)
	}
	if len(row.Documents) != 1 || row.Documents[0] != "a.txt" {
		t.Errorf("the message needs somewhere to point, got %v", row.Documents)
	}
}

// TestIntersectionPartialCoverage: a value covered in some places and free in
// others is a milder note, so the counts must differ. Reporting "3 of 3" for a
// value that also appears on its own is the difference between a note and an
// alarm.
func TestIntersectionPartialCoverage(t *testing.T) {
	// "Meridian" is a declared value. A custom pattern claims it only where it
	// is followed by a code, so one of the three occurrences is covered.
	docs := []Document{{Name: "a.txt", Format: FormatTXT,
		Markdown: "Meridian alone. Meridian again. Meridian-4471 is coded.\n"}}

	rows := DetectIntersections(docs, scopeFor(
		[]Entity{{Category: CatEntityNames, Canonical: "Meridian"}},
		[]CustomPattern{{Expr: `Meridian-[0-9]+`}}))

	row, ok := findIntersection(rows, "Meridian", CatEntityNames)
	if !ok {
		t.Fatalf("the partially covered value must be reported, got %+v", rows)
	}
	if row.Occurrences != 1 {
		t.Errorf("exactly one occurrence is covered, got %d", row.Occurrences)
	}
	if row.TotalOccurrences != 3 {
		t.Errorf("the value occurs three times in all, got %d", row.TotalOccurrences)
	}
	if row.WinnerCategory != CatCustomPatterns {
		t.Errorf("the pattern must be named as the winner, got %s", row.WinnerCategory)
	}
}

// TestIntersectionAutoCoversAI: the two detector routes against each other.
// Smart detection outranks the local AI, so an auto value covering an AI one
// reports the AI value as the loser.
func TestIntersectionAutoCoversAI(t *testing.T) {
	docs := []Document{{Name: "a.txt", Format: FormatTXT,
		Markdown: "The Helios Fund closed in June.\n"}}

	rows := DetectIntersections(docs, scopeFor([]Entity{
		// The longer name from Smart detection...
		{Category: CatEntityNames, Canonical: "Helios Fund", Origin: OriginAuto},
		// ...covering the shorter one the AI proposed as a brand.
		{Category: CatBrandNames, Canonical: "Helios", Origin: OriginAI},
	}, nil))

	row, ok := findIntersection(rows, "Helios", CatBrandNames)
	if !ok {
		t.Fatalf("the AI value must be reported as covered, got %+v", rows)
	}
	if row.Origin != OriginAI || row.WinnerOrigin != OriginAuto {
		t.Errorf("Smart detection must supersede the local AI, got %s losing to %s",
			row.Origin, row.WinnerOrigin)
	}
}

// TestIntersectionSilentWhenValuesDoNotCoOccur: two values that never claim the
// same characters are not an intersection, they are simply two values. Warning
// about them trains the user to ignore the warning.
func TestIntersectionSilentWhenValuesDoNotCoOccur(t *testing.T) {
	docs := []Document{{Name: "a.txt", Format: FormatTXT,
		Markdown: "Alpine Trust met Borealis Capital on the Tuesday.\n"}}

	rows := DetectIntersections(docs, scopeFor([]Entity{
		{Category: CatEntityNames, Canonical: "Alpine Trust"},
		{Category: CatBrandNames, Canonical: "Borealis Capital", Origin: OriginAI},
	}, nil))

	if len(rows) != 0 {
		t.Errorf("two values that do not cover each other must report nothing, got %+v", rows)
	}
}

// TestIntersectionRespectsTheAllowlist: an allowlisted term is never replaced
// by any pass, so it can never cover anything. The check must see the same
// allowlist the run does, or it warns about an overlap that will not happen.
func TestIntersectionRespectsTheAllowlist(t *testing.T) {
	const value = "marie.duval@example.com"
	docs := []Document{{Name: "a.txt", Format: FormatTXT,
		Markdown: "Write to " + value + " today.\n"}}

	allow := NewEmptyAllowlist()
	allow.Add(value)
	scope := NewDetectionScope(
		[]Entity{{Category: CatPersonNames, Canonical: value}}, nil,
		PresetSelection(LevelAdvanced), 0, CountryLU, allow, false)

	if rows := DetectIntersections(docs, scope); len(rows) != 0 {
		t.Errorf("an allowlisted value is replaced by nothing, so it overlaps nothing, got %+v", rows)
	}
}

// TestCheckAgreesWithTheRun is the guard against the pre-run check and the
// pipeline disagreeing. It is why the check reuses detectText and the shared
// comparator instead of a parallel implementation: a second implementation can
// describe a decision the run did not make, and a warning nobody can act on is
// worse than no warning.
func TestCheckAgreesWithTheRun(t *testing.T) {
	const value = "marie.duval@example.com"
	docs := []Document{{Name: "a.txt", Format: FormatTXT,
		Markdown: "Write to " + value + " today.\n"}}
	entities := []Entity{{Category: CatPersonNames, Canonical: value}}

	rows := DetectIntersections(docs, scopeFor(entities, nil))
	row, ok := findIntersection(rows, value, CatPersonNames)
	if !ok {
		t.Fatalf("expected the check to report the overlap, got %+v", rows)
	}

	reg := NewRegistry()
	if _, err := Run(context.Background(), PipelineInput{
		Documents: docs, Entities: entities,
		Level: LevelAdvanced, Country: CountryLU,
		Allowlist: NewEmptyAllowlist(), Registry: reg,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var owner string
	for _, e := range reg.Entries() {
		if strings.EqualFold(e.Original, value) {
			owner = e.Category
		}
	}
	if owner != row.WinnerCategory {
		t.Errorf("the check said %s would win and the run filed the value under %s.\n"+
			"They must agree, or the warning describes something that did not happen.",
			row.WinnerCategory, owner)
	}
}

package engine

import (
	"context"
	"strings"
	"testing"
)

// scopeFor is the detection configuration these tests share: every category on,
// no confidence floor, Luxembourg, nothing allowlisted.
func scopeFor(values []Value, patterns []CustomPattern) detectionScope {
	return NewDetectionScope(values, patterns,
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
	docs := []Document{{
		Name: "a.txt", Format: FormatTXT,
		Markdown: "Write to " + value + " today, or to " + value + " tomorrow.\n",
	}}

	rows := DetectIntersections(docs,
		scopeFor([]Value{{Category: CatPersonNames, MainText: value}}, nil))

	row, ok := findIntersection(rows, value, CatPersonNames)
	if !ok {
		t.Fatalf("the declared value must be reported as covered, got %+v", rows)
	}
	if row.WinnerCategory != CatEmail || row.WinnerMatchClass != MatchClassBuiltInPattern {
		t.Errorf("native detection must be named as the winner, got %s / %s",
			row.WinnerCategory, row.WinnerMatchClass)
	}
	if row.MatchClass != MatchClassUserDefined {
		t.Errorf("the losing claim is the user's declaration, got %q", row.MatchClass)
	}
	if row.Occurrences != 2 || row.TotalOccurrences != 2 {
		t.Errorf("both occurrences are covered, got %d of %d",
			row.Occurrences, row.TotalOccurrences)
	}
	if len(row.Documents) != 1 || row.Documents[0] != "a.txt" {
		t.Errorf("the message needs somewhere to point, got %v", row.Documents)
	}
	// The winner sat on the value's own text, so there is no other literal to
	// name and the message says the value once instead of repeating it as its
	// own spelling.
	if len(row.MatchedTexts) != 0 {
		t.Errorf("the covered text IS the value, so nothing extra is reported, got %v",
			row.MatchedTexts)
	}
}

// TestIntersectionPartialCoverageIsNotReported: a value covered in some places
// and free in others is NOT a warning.
//
// It still gets its own placeholder everywhere nothing covers it, and the covered
// occurrences are redacted by the winner, so the message would name no leak and
// no action. Only total coverage is worth saying, because then the value gets no
// placeholder of its own at all.
func TestIntersectionPartialCoverageIsNotReported(t *testing.T) {
	// "Meridian" is a declared value. A custom pattern claims it only where it
	// is followed by a code, so one of the three occurrences is covered.
	docs := []Document{{
		Name: "a.txt", Format: FormatTXT,
		Markdown: "Meridian alone. Meridian again. Meridian-4471 is coded.\n",
	}}

	rows := DetectIntersections(docs, scopeFor(
		[]Value{{Category: CatEntityNames, MainText: "Meridian"}},
		[]CustomPattern{{Expr: `Meridian-[0-9]+`}}))

	if row, ok := findIntersection(rows, "Meridian", CatEntityNames); ok {
		t.Errorf("a value covered %d of %d times is not reported, got %+v",
			row.Occurrences, row.TotalOccurrences, row)
	}
}

// TestIntersectionMatchedTextDiffersFromValue: the casing case. The entity
// "Coca" occurs only inside email domains, where the document spells it "coca",
// so a message quoting the declared form would claim a string the document does
// not hold at that position.
func TestIntersectionMatchedTextDiffersFromValue(t *testing.T) {
	docs := []Document{{
		Name: "a.txt", Format: FormatTXT,
		Markdown: "Write to sales@coca.us, or to legal@coca.us instead.\n",
	}}

	rows := DetectIntersections(docs,
		scopeFor([]Value{{Category: CatEntityNames, MainText: "Coca"}}, nil))

	row, ok := findIntersection(rows, "Coca", CatEntityNames)
	if !ok {
		t.Fatalf("the declared entity is covered in every occurrence, got %+v", rows)
	}
	if row.Occurrences != row.TotalOccurrences {
		t.Errorf("every occurrence sits inside an address, got %d of %d",
			row.Occurrences, row.TotalOccurrences)
	}
	if len(row.MatchedTexts) != 1 || row.MatchedTexts[0] != "coca" {
		t.Errorf("the literal covered text is what the document holds (\"coca\"), got %v",
			row.MatchedTexts)
	}
}

// TestIntersectionReportsFragmentSpellings: the fragment case, and the reason
// MatchedTexts is a SET.
//
// A person's derived spellings match separately inside an address ("pierre" and
// "dupont" in pierre.dupont@coca.us, because the value pass treats "." and "@"
// as word boundaries). The full name never appears there at all, so the message
// has to name the fragments, both of them, in the order the document holds them.
func TestIntersectionReportsFragmentSpellings(t *testing.T) {
	docs := []Document{{
		Name: "a.txt", Format: FormatTXT,
		Markdown: "Send it to pierre.dupont@coca.us before Friday.\n",
	}}

	rows := DetectIntersections(docs,
		scopeFor([]Value{{Category: CatPersonNames, MainText: "Pierre Dupont"}}, nil))

	row, ok := findIntersection(rows, "Pierre Dupont", CatPersonNames)
	if !ok {
		t.Fatalf("the person occurs only inside the address, got %+v", rows)
	}
	if row.Occurrences != row.TotalOccurrences {
		t.Errorf("the address covers every occurrence, got %d of %d",
			row.Occurrences, row.TotalOccurrences)
	}
	want := []string{"pierre", "dupont"}
	if len(row.MatchedTexts) != len(want) {
		t.Fatalf("both fragments are covered, so both are named: want %v, got %v",
			want, row.MatchedTexts)
	}
	for i, w := range want {
		if row.MatchedTexts[i] != w {
			t.Errorf("the fragments are listed in document order: want %v, got %v",
				want, row.MatchedTexts)
			break
		}
	}
}

// TestIntersectionAutoCoversTheLLM: the two detector routes against each other.
// Heuristic discovery outranks the local model, so an auto value covering a model one
// reports the local model value as the loser.
func TestIntersectionAutoCoversTheLLM(t *testing.T) {
	docs := []Document{{
		Name: "a.txt", Format: FormatTXT,
		Markdown: "The Helios Fund closed in June.\n",
	}}

	rows := DetectIntersections(docs, scopeFor([]Value{
		// The longer name from heuristic discovery...
		{Category: CatEntityNames, MainText: "Helios Fund", DiscoveryMethods: []string{MethodHeuristic}},
		// ...covering the shorter one the local model proposed as a brand.
		{Category: CatBrandNames, MainText: "Helios", DiscoveryMethods: []string{MethodLocalLLM}},
	}, nil))

	row, ok := findIntersection(rows, "Helios", CatBrandNames)
	if !ok {
		t.Fatalf("the local model value must be reported as covered, got %+v", rows)
	}
	if row.MatchClass != MatchClassLocalLLMDiscovered || row.WinnerMatchClass != MatchClassRulesDiscovered {
		t.Errorf("heuristic discovery must supersede the local model, got %s losing to %s",
			row.MatchClass, row.WinnerMatchClass)
	}
}

// TestIntersectionSilentWhenValuesDoNotCoOccur: two values that never claim the
// same characters are not an intersection, they are simply two values. Warning
// about them trains the user to ignore the warning.
func TestIntersectionSilentWhenValuesDoNotCoOccur(t *testing.T) {
	docs := []Document{{
		Name: "a.txt", Format: FormatTXT,
		Markdown: "Alpine Trust met Borealis Capital on the Tuesday.\n",
	}}

	rows := DetectIntersections(docs, scopeFor([]Value{
		{Category: CatEntityNames, MainText: "Alpine Trust"},
		{Category: CatBrandNames, MainText: "Borealis Capital", DiscoveryMethods: []string{MethodLocalLLM}},
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
	docs := []Document{{
		Name: "a.txt", Format: FormatTXT,
		Markdown: "Write to " + value + " today.\n",
	}}

	allow := NewEmptyAllowlist()
	allow.Add(value)
	scope := NewDetectionScope(
		[]Value{{Category: CatPersonNames, MainText: value}}, nil,
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
	docs := []Document{{
		Name: "a.txt", Format: FormatTXT,
		Markdown: "Write to " + value + " today.\n",
	}}
	values := []Value{{Category: CatPersonNames, MainText: value}}

	rows := DetectIntersections(docs, scopeFor(values, nil))
	row, ok := findIntersection(rows, value, CatPersonNames)
	if !ok {
		t.Fatalf("expected the check to report the overlap, got %+v", rows)
	}

	reg := NewRegistry()
	if _, err := Run(context.Background(), PipelineInput{
		Documents: docs, Values: values,
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

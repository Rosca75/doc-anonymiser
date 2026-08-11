// engine/selection_test.go — BUILD-02 Phase 3 tests: the granular
// CategorySelection drives the pipeline (one row per category), presets
// reproduce the v1 level behaviour byte for byte, and mixed selections
// behave.
package engine

import (
	"context"
	"strings"
	"testing"
)

// selectionFixture contains at least one instance of every selectable
// category kind (PII + entities via the entity list below).
const selectionFixture = `Contact info.desk@example.com or +352 621 000 111.
Site https://example.com/x holds IBAN LU28 0019 4006 4475 0000 and VAT LU12345678.
Matricule 1893120105732 was billed EUR 12,500 on 2026-01-15.
Client Alpine Trust S.A. runs Project Borealis with Paul Stone (internal) and Marie Curie.
Office in Metropolis for Acme Corp. Code PRJ-42 applies.`

// selectionEntities declares one entity per entity category.
var selectionEntities = []Entity{
	{Category: CatEntityNames, Canonical: "Alpine Trust S.A."},
	{Category: CatProjectNames, Canonical: "Project Borealis"},
	{Category: CatEntityNames, Canonical: "Paul Stone"},
	{Category: CatPersonNames, Canonical: "Marie Curie"},
	{Category: CatOrganisationNames, Canonical: "Acme Corp"},
	{Category: CatLocationNames, Canonical: "Metropolis"},
}

var selectionPatterns = []CustomPattern{{Expr: `PRJ-[0-9]+`}}

// runWithSelection is the shared harness: one document, the full entity
// set, a given selection.
func runWithSelection(t *testing.T, sel CategorySelection) *Results {
	t.Helper()
	res, err := Run(context.Background(), PipelineInput{
		Documents:  []Document{{Name: "f.txt", Format: FormatTXT, Markdown: selectionFixture}},
		Entities:   selectionEntities,
		Patterns:   selectionPatterns,
		Categories: sel,
		Allowlist:  NewEmptyAllowlist(),
	})
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}
	return res
}

// TestSingleCategorySelection: enabling exactly one category anonymises
// only that category (one table row per selectable category).
func TestSingleCategorySelection(t *testing.T) {
	cases := []struct {
		category string
		// wantGone is a fragment that must be replaced when the category
		// is on (and must survive when everything else is on instead).
		wantGone string
	}{
		{CatEmail, "info.desk@example.com"},
		{CatURL, "https://example.com/x"},
		{CatIBAN, "LU28 0019 4006 4475 0000"},
		{CatVAT, "LU12345678"},
		{CatMatricule, "1893120105732"},
		{CatPhone, "+352 621 000 111"},
		{CatAmount, "EUR 12,500"},
		{CatDate, "2026-01-15"},
		{CatEntityNames, "Alpine Trust S.A."},
		{CatProjectNames, "Project Borealis"},
		{CatEntityNames, "Paul Stone"},
		{CatPersonNames, "Marie Curie"},
		{CatOrganisationNames, "Acme Corp"},
		{CatLocationNames, "Metropolis"},
		{CatCustomPatterns, "PRJ-42"},
	}
	// Known single-category cross-matches, documented, not bugs: a
	// fragment of one category can ALSO match another category's pattern,
	// and with the longer category switched off the shorter match fires.
	//   - phone digits inside an IBAN ("0019 4006 ..." looks like an
	//     international number once the IBAN pattern is off),
	// With both categories on, overlap resolution keeps the longer span,
	// which is the behaviour users actually see at preset levels.
	crossMatches := map[string]map[string]bool{
		"phone": {CatIBAN: true},
	}

	for _, tc := range cases {
		t.Run(tc.category, func(t *testing.T) {
			sel := CategorySelection{tc.category: true}
			out := runWithSelection(t, sel).Documents[0].Anonymised
			if strings.Contains(out, tc.wantGone) {
				t.Errorf("category %s on: %q must be replaced, output: %s", tc.category, tc.wantGone, out)
			}
			// Every OTHER case's fragment must be untouched.
			for _, other := range cases {
				if other.category == tc.category || crossMatches[tc.category][other.category] {
					continue
				}
				if !strings.Contains(out, other.wantGone) {
					t.Errorf("category %s on: %q (category %s) must SURVIVE, output: %s",
						tc.category, other.wantGone, other.category, out)
				}
			}
		})
	}
}

// TestPresetEquivalence: a nil selection at level L and
// PresetSelection(L) produce byte-identical output (the regression anchor
// pinning v1 behaviour).
func TestPresetEquivalence(t *testing.T) {
	for _, level := range []Level{LevelSoft, LevelMedium, LevelAdvanced} {
		t.Run(string(level), func(t *testing.T) {
			byLevel, err := Run(context.Background(), PipelineInput{
				Documents: []Document{{Name: "f.txt", Format: FormatTXT, Markdown: selectionFixture}},
				Entities:  selectionEntities,
				Patterns:  selectionPatterns,
				Level:     level,
				Allowlist: NewEmptyAllowlist(),
			})
			if err != nil {
				t.Fatal(err)
			}
			bySelection, err := Run(context.Background(), PipelineInput{
				Documents:  []Document{{Name: "f.txt", Format: FormatTXT, Markdown: selectionFixture}},
				Entities:   selectionEntities,
				Patterns:   selectionPatterns,
				Level:      level,
				Categories: PresetSelection(level),
				Allowlist:  NewEmptyAllowlist(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if byLevel.Documents[0].Anonymised != bySelection.Documents[0].Anonymised {
				t.Errorf("level %s and PresetSelection(%s) diverge:\n%s\n---\n%s",
					level, level, byLevel.Documents[0].Anonymised, bySelection.Documents[0].Anonymised)
			}
		})
	}
}

// TestMixedSelection: persons on + emails off leaves emails intact, and
// the allowlist still wins over any enabled selection.
func TestMixedSelection(t *testing.T) {
	sel := PresetSelection(LevelMedium)
	sel[CatEmail] = false
	sel[CatLocationNames] = true // a custom mix: medium plus locations

	allow := NewEmptyAllowlist()
	allow.Add("Metropolis") // allowlisted although location_names is on

	res, err := Run(context.Background(), PipelineInput{
		Documents:  []Document{{Name: "f.txt", Format: FormatTXT, Markdown: selectionFixture}},
		Entities:   selectionEntities,
		Patterns:   selectionPatterns,
		Categories: sel,
		Allowlist:  allow,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := res.Documents[0].Anonymised
	if !strings.Contains(out, "info.desk@example.com") {
		t.Errorf("emails off: the address must survive, output: %s", out)
	}
	if !strings.Contains(out, "[ENTITY_1]") {
		t.Errorf("clients on: Alpine Trust must be replaced, output: %s", out)
	}
	if strings.Contains(out, "Marie Curie") {
		t.Errorf("persons on: Marie Curie must be replaced, output: %s", out)
	}
	if !strings.Contains(out, "Metropolis") {
		t.Errorf("allowlist must win over the enabled location_names selection, output: %s", out)
	}
}

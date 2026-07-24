// engine/selection_test.go — BUILD-02 Phase 3 tests: the granular
// CategorySelection drives the pipeline (one row per category), presets
// reproduce the v1 level behaviour byte for byte, mixed selections behave,
// and the schema-2 session migration renames the internal category without
// losing data.
package engine

import (
	"context"
	"os"
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
	{Category: "client_names", Canonical: "Alpine Trust S.A."},
	{Category: "project_names", Canonical: "Project Borealis"},
	{Category: "internal_names", Canonical: "Paul Stone"},
	{Category: "person_names", Canonical: "Marie Curie"},
	{Category: "organisation_names", Canonical: "Acme Corp"},
	{Category: "location_names", Canonical: "Metropolis"},
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
		{"client_names", "Alpine Trust S.A."},
		{"project_names", "Project Borealis"},
		{"internal_names", "Paul Stone"},
		{"person_names", "Marie Curie"},
		{"organisation_names", "Acme Corp"},
		{"location_names", "Metropolis"},
		{"custom_patterns", "PRJ-42"},
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
	sel["location_names"] = true // a custom mix: medium plus locations

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
	if !strings.Contains(out, "[CLIENT_1]") {
		t.Errorf("clients on: Alpine Trust must be replaced, output: %s", out)
	}
	if strings.Contains(out, "Marie Curie") {
		t.Errorf("persons on: Marie Curie must be replaced, output: %s", out)
	}
	if !strings.Contains(out, "Metropolis") {
		t.Errorf("allowlist must win over the enabled location_names selection, output: %s", out)
	}
}

// TestSessionMigrationV1: loading a v1 session file with the old
// pwc_internal_names key yields internal_names entities, [INTERNAL_N]
// registry rows with the counters preserved, and a re-run reproduces the
// identical placeholders.
func TestSessionMigrationV1(t *testing.T) {
	raw, err := os.ReadFile("../testdata/session_v1.anonsession.json")
	if err != nil {
		t.Fatalf("fixture missing: %v", err)
	}
	s, err := LoadSession(raw)
	if err != nil {
		t.Fatalf("v1 session must load: %v", err)
	}
	if s.Schema != SessionSchema {
		t.Errorf("schema after migration = %d, want %d", s.Schema, SessionSchema)
	}
	for _, e := range s.Entities {
		if e.Category == "pwc_internal_names" {
			t.Errorf("entity %q kept the old category", e.Canonical)
		}
	}
	var stone MappingEntry
	for _, r := range s.Registry {
		if r.Category == "pwc_internal_names" || strings.Contains(r.Placeholder, "PWC_INTERNAL") {
			t.Errorf("registry row not migrated: %+v", r)
		}
		if r.Original == "Paul Stone" {
			stone = r
		}
	}
	if stone.Placeholder != "[INTERNAL_1]" || stone.Category != "internal_names" || stone.Count != 3 {
		t.Errorf("Paul Stone row = %+v, want [INTERNAL_1]/internal_names/count 3", stone)
	}

	// Re-run with the restored registry: the same entity must keep its
	// migrated placeholder AND new internal names continue the numbering.
	reg := NewRegistryFromEntries(s.Registry)
	res, err := Run(context.Background(), PipelineInput{
		Documents: []Document{{Name: "f.txt", Format: FormatTXT,
			Markdown: "Paul Stone met Nora Klein (internal)."}},
		Entities: append(s.Entities, Entity{Category: "internal_names", Canonical: "Nora Klein"}),
		Registry: reg,
		Allowlist: func() *Allowlist {
			a := NewEmptyAllowlist()
			for _, term := range s.AllowTerms {
				a.Add(term)
			}
			return a
		}(),
	})
	if err != nil {
		t.Fatal(err)
	}
	out := res.Documents[0].Anonymised
	if !strings.Contains(out, "[INTERNAL_1]") {
		t.Errorf("migrated entity must keep [INTERNAL_1], output: %s", out)
	}
	// [INTERNAL_2] existed in the fixture (Ana Weber), so the next fresh
	// internal name must take [INTERNAL_3] — continuous counters.
	if !strings.Contains(out, "[INTERNAL_3]") {
		t.Errorf("new internal name must continue numbering at [INTERNAL_3], output: %s", out)
	}

	// A saved session carries the current schema from now on.
	saved, err := SaveSession(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), `"schema": 2`) {
		t.Error("saved session must carry schema 2")
	}
}

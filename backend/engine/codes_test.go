// engine/codes_test.go — the code-shape detector: what it matches, what it
// deliberately does not, and the boundary it must not cross into pass 1.
package engine

import (
	"strings"
	"testing"
)

func TestDetectCodesShapesAndCategories(t *testing.T) {
	cases := []struct {
		name       string
		text       string
		wantText   string
		wantCat    string
		wantScore  float32
		wantNoHits bool
	}{
		{
			name:      "a bare code is an identifier",
			text:      "The file PRJ-4471 was archived.",
			wantText:  "PRJ-4471",
			wantCat:   CatIdentifierNames,
			wantScore: confidenceCodeBare,
		},
		{
			name:      "a reference cue raises the confidence",
			text:      "Ref. INV-88213 is still open.",
			wantText:  "INV-88213",
			wantCat:   CatIdentifierNames,
			wantScore: confidenceCodeCued,
		},
		{
			name:      "a project cue changes the category",
			text:      "Le projet ATLAS-2024 démarre en mars.",
			wantText:  "ATLAS-2024",
			wantCat:   CatProjectNames,
			wantScore: confidenceCodeCued,
		},
		{
			name:      "a space is a separator like any other",
			text:      "Invoice INV 88213 was paid.",
			wantText:  "INV 88213",
			wantCat:   CatIdentifierNames,
			wantScore: confidenceCodeCued,
		},
		{
			name:      "a trailing block is part of the code",
			text:      "Projet ATLAS-2024-A lancé.",
			wantText:  "ATLAS-2024-A",
			wantCat:   CatProjectNames,
			wantScore: confidenceCodeCued,
		},
		// The deliberate non-matches. Each is a shape that LOOKS close and
		// belongs to something else, and each would be noise in the review list.
		{
			name:       "letters and digits with no separator are a tax number, which pass 1 owns",
			text:       "VAT LU12345678 applies.",
			wantNoHits: true,
		},
		{
			name:       "one leading letter is a list bullet, not a code",
			text:       "See A-123 below.",
			wantNoHits: true,
		},
		{
			name:       "two digits is a quantity or a page number",
			text:       "Section PRJ-44 covers it.",
			wantNoHits: true,
		},
		{
			name:       "letters alone are an acronym, and belong to the run detector",
			text:       "The CSSF published guidance.",
			wantNoHits: true,
		},
		{
			name:       "digits first is a date, and belongs to pass 1",
			text:       "Signed on 2024-01-15 in Luxembourg.",
			wantNoHits: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectCodes(tc.text, NewEmptyAllowlist())
			if tc.wantNoHits {
				if len(got) != 0 {
					t.Fatalf("want no suggestions, got %+v", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("want exactly one suggestion, got %+v", got)
			}
			if got[0].MainText != tc.wantText {
				t.Errorf("text = %q, want %q", got[0].MainText, tc.wantText)
			}
			if got[0].Category != tc.wantCat {
				t.Errorf("category = %q, want %q", got[0].Category, tc.wantCat)
			}
			if got[0].Confidence != tc.wantScore {
				t.Errorf("confidence = %v, want %v", got[0].Confidence, tc.wantScore)
			}
		})
	}
}

func TestDetectCodesFindsBothOfTwoAdjacentCodes(t *testing.T) {
	// The regex verifies its boundary on the match OFFSETS rather than
	// consuming the character either side. A consuming guard eats the separator
	// and the second code of a pair is never matched, which is a leak: the same
	// mistake the 13-digit and German tax-ID patterns carried.
	got := DetectCodes("Refs INV-88213, INV-88214 both apply.", NewEmptyAllowlist())
	if len(got) != 2 {
		t.Fatalf("both adjacent codes must be found, got %+v", got)
	}
}

func TestDetectCodesCountsAndAllowlist(t *testing.T) {
	text := "PRJ-4471 opened. PRJ-4471 closed. INV-88213 paid."
	got := DetectCodes(text, NewEmptyAllowlist())
	if len(got) != 2 || got[0].MainText != "PRJ-4471" || got[0].Count != 2 {
		t.Fatalf("the repeated code must lead with count 2, got %+v", got)
	}

	allow := NewEmptyAllowlist()
	allow.Add("PRJ-4471")
	got = DetectCodes(text, allow)
	for _, c := range got {
		if c.MainText == "PRJ-4471" {
			t.Errorf("an allowlisted code must never be proposed, got %+v", got)
		}
	}
}

// TestCodeDetectorDoesNotOverlapPassOne is the boundary test.
//
// The code detector overlaps pass 1's territory by construction: dates, IBANs,
// VAT numbers, card numbers and national identifiers are all letters and digits
// in a row. Two detectors fighting over one span is resolved silently and
// correctly by the registry's one-value-one-placeholder rule, so without this
// test nobody would ever learn it was happening.
func TestCodeDetectorDoesNotOverlapPassOne(t *testing.T) {
	const fixture = `Contact info.desk@example.com about IBAN LU28 0019 4006 4475 0000.
VAT LU12345678 and matricule 1893120105732 are on file.
Card 4111 1111 1111 1111 was charged EUR 12,500 on 2026-01-15.
German tax ID 12 345 678 901 and Spanish NIF 12345678Z apply.
Ref. INV-88213 covers the project ATLAS-2024.`

	sel := PresetSelection(LevelAdvanced)
	pii := DetectPIISelected(fixture, sel, CountryLU)

	for _, code := range DetectCodes(fixture, NewEmptyAllowlist()) {
		start := strings.Index(fixture, code.MainText)
		end := start + len(code.MainText)
		for _, span := range pii {
			if start < span.End && span.Start < end {
				t.Errorf("the code %q (%d:%d) overlaps the %s span %q (%d:%d); "+
					"one of the two detectors is claiming the other's territory",
					code.MainText, start, end, span.Category, fixture[span.Start:span.End], span.Start, span.End)
			}
		}
	}
}

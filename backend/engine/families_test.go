package engine

import (
	"strings"
	"testing"
)

// cand is a candidate as a detection route emits one.
func cand(category, text string, count int) Candidate {
	return Candidate{Category: category, Text: text, Count: count}
}

// mainsOf lists the folded output's main values, in order.
func mainsOf(rows []Candidate) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Text
	}
	return out
}

// TestFoldValueFamilies is the table. Every case is a fold that would be wrong
// without the rule it names.
func TestFoldValueFamilies(t *testing.T) {
	cases := []struct {
		name string
		in   []Candidate
		// wantMains is the expected output in order; wantVariants maps a main
		// value to the spellings folded into it.
		wantMains    []string
		wantVariants map[string][]string
	}{
		{
			name: "the shorter form becomes the main value",
			in: []Candidate{
				cand(CatBrandNames, "Coca-Cola company", 2),
				cand(CatBrandNames, "Coca-Cola", 5),
			},
			// The longer form was found FIRST, and the shorter one still wins:
			// left as two values the shorter fires inside the longer and the
			// text reads "[BRAND_1] company".
			wantMains:    []string{"Coca-Cola"},
			wantVariants: map[string][]string{"Coca-Cola": {"Coca-Cola company"}},
		},
		{
			name: "a chain folds transitively into one family",
			in: []Candidate{
				cand(CatBrandNames, "Coca-Cola Ltd.", 1),
				cand(CatBrandNames, "Coca", 3),
				cand(CatBrandNames, "Coca-Cola", 4),
			},
			wantMains: []string{"Coca"},
			wantVariants: map[string][]string{
				"Coca": {"Coca-Cola Ltd.", "Coca-Cola"},
			},
		},
		{
			name: "a cross-category pair is left alone",
			// A person "Delta" and an organisation "Delta Industries" are an
			// intersection, not a family: folding them would file a human being
			// under an organisation.
			in: []Candidate{
				cand(CatPersonNames, "Delta", 2),
				cand(CatEntityNames, "Delta Industries", 3),
			},
			wantMains: []string{"Delta", "Delta Industries"},
		},
		{
			name: "a substring that is not at a word boundary is left alone",
			// "Alten" occurs inside "Altenberg" and they are two different
			// names. Same boundary rule the entity pass matches with.
			in: []Candidate{
				cand(CatEntityNames, "Alten", 2),
				cand(CatEntityNames, "Altenberg", 3),
			},
			wantMains: []string{"Alten", "Altenberg"},
		},
		{
			name: "a two-character stem does not become a main value",
			// Promoting it would shred ordinary text everywhere it appeared.
			in: []Candidate{
				cand(CatEntityNames, "BV", 2),
				cand(CatEntityNames, "BV Holdings", 3),
			},
			wantMains: []string{"BV", "BV Holdings"},
		},
		{
			name: "equal-length ties break on occurrence count",
			// Two rows tie for shortest. The spelling the documents actually
			// use more often is the better main value, and picking by count
			// rather than by input order is what makes the answer the same on
			// every run.
			in: []Candidate{
				cand(CatEntityNames, "Delta Group", 2),
				cand(CatEntityNames, "Delta", 3),
				cand(CatEntityNames, "Delta", 9),
			},
			wantMains: []string{"Delta"},
			wantVariants: map[string][]string{
				// The tied duplicate is not a second spelling of itself.
				"Delta": {"Delta Group"},
			},
		},
		{
			name: "values that are not spellings of each other stay separate",
			in: []Candidate{
				cand(CatEntityNames, "Alpine Trust", 2),
				cand(CatEntityNames, "Borealis Capital", 3),
			},
			wantMains: []string{"Alpine Trust", "Borealis Capital"},
		},
	}

	for _, tc := range cases {
		got := FoldValueFamilies(tc.in, NewEmptyAllowlist())
		if strings.Join(mainsOf(got), "|") != strings.Join(tc.wantMains, "|") {
			t.Errorf("%s: mains = %v, want %v", tc.name, mainsOf(got), tc.wantMains)
			continue
		}
		for _, row := range got {
			want, ok := tc.wantVariants[row.Text]
			if !ok {
				if len(row.Variants) != 0 {
					t.Errorf("%s: %q must fold nothing, got %v", tc.name, row.Text, row.Variants)
				}
				continue
			}
			if strings.Join(row.Variants, "|") != strings.Join(want, "|") {
				t.Errorf("%s: %q variants = %v, want %v", tc.name, row.Text, row.Variants, want)
			}
		}
	}
}

// TestFoldSkipsAnAllowlistedMember: an allowlisted term is replaced by nothing,
// so folding one into a family would make the family's placeholder depend on a
// value that never applies.
func TestFoldSkipsAnAllowlistedMember(t *testing.T) {
	allow := NewEmptyAllowlist()
	allow.Add("Coca-Cola")

	got := FoldValueFamilies([]Candidate{
		cand(CatBrandNames, "Coca-Cola", 5),
		cand(CatBrandNames, "Coca-Cola company", 2),
	}, allow)

	if len(got) != 2 {
		t.Fatalf("an allowlisted member must not be folded, got %v", mainsOf(got))
	}
	for _, row := range got {
		if len(row.Variants) != 0 {
			t.Errorf("%q folded something despite the allowlist: %v", row.Text, row.Variants)
		}
	}
}

// TestFoldSumsTheFamilyWeight: the review row should say how often the value
// occurs in ANY of its spellings. Reporting only the shortest form's count
// would rank a folded family below values it actually outnumbers.
func TestFoldSumsTheFamilyWeight(t *testing.T) {
	got := FoldValueFamilies([]Candidate{
		cand(CatBrandNames, "Coca-Cola", 5),
		cand(CatBrandNames, "Coca-Cola company", 2),
	}, NewEmptyAllowlist())

	if len(got) != 1 {
		t.Fatalf("want one folded row, got %v", mainsOf(got))
	}
	if got[0].Count != 7 {
		t.Errorf("the family's weight is every spelling's, got %d want 7", got[0].Count)
	}
}

// TestFoldIsIdempotent: folding an already-folded list changes nothing. The
// merged output of two routes can contain rows that were folded per route, and
// a second pass must not shuffle a main value or lose a variant.
func TestFoldIsIdempotent(t *testing.T) {
	once := FoldValueFamilies([]Candidate{
		cand(CatBrandNames, "Coca-Cola Ltd.", 1),
		cand(CatBrandNames, "Coca", 3),
		cand(CatBrandNames, "Coca-Cola", 4),
	}, NewEmptyAllowlist())
	twice := FoldValueFamilies(once, NewEmptyAllowlist())

	if strings.Join(mainsOf(once), "|") != strings.Join(mainsOf(twice), "|") {
		t.Errorf("folding twice changed the mains: %v then %v", mainsOf(once), mainsOf(twice))
	}
	if strings.Join(once[0].Variants, "|") != strings.Join(twice[0].Variants, "|") {
		t.Errorf("folding twice changed the variants: %v then %v",
			once[0].Variants, twice[0].Variants)
	}
}

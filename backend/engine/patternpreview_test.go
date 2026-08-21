// engine/patternpreview_test.go — the built-in pattern preview.
//
// The preview's whole value is that it reports what pass 1 WOULD do, so these
// tests are about agreement with pass 1 (the same gates, the same regions, the
// same country scoping) rather than about pattern quality, which pii_test.go
// already owns.
package engine

import "testing"

// selection is a CategorySelection with exactly the named categories on, so a
// case cannot accidentally depend on a preset's other members.
func selection(categories ...string) CategorySelection {
	sel := CategorySelection{}
	for _, c := range categories {
		sel[c] = true
	}
	return sel
}

// TestPreviewGroupsByCategoryAndText is the tab's basic promise: one row per
// distinct matched text, with the occurrences counted and the files named.
func TestPreviewGroupsByCategoryAndText(t *testing.T) {
	docs := []Document{
		{Name: "a.md", Markdown: "write to marie.duval@example.com and again marie.duval@example.com"},
		{Name: "b.md", Markdown: "cc marie.duval@example.com and jean.muller@example.com"},
	}
	got := PreviewPatternMatches(docs, selection(CatEmail), CountryLU, false, nil)
	if len(got) != 2 {
		t.Fatalf("want 2 grouped matches, got %d: %+v", len(got), got)
	}
	// Sorted by count descending, so the address in three places comes first.
	if got[0].Text != "marie.duval@example.com" || got[0].Count != 3 {
		t.Errorf("want marie.duval@example.com x3 first, got %q x%d", got[0].Text, got[0].Count)
	}
	if len(got[0].Documents) != 2 {
		t.Errorf("want both files credited once each, got %v", got[0].Documents)
	}
	if got[1].Text != "jean.muller@example.com" || got[1].Count != 1 {
		t.Errorf("want jean.muller@example.com x1 second, got %q x%d", got[1].Text, got[1].Count)
	}
	for _, m := range got {
		if m.Category != CatEmail {
			t.Errorf("match %q filed under %q, want %q", m.Text, m.Category, CatEmail)
		}
	}
}

// TestPreviewHonoursTheAllowlist: the allowlist is the single veto every span
// producer consults, and the session exclusions are enforced through it. A
// preview showing a value the user REMOVED would read as the removal undone.
func TestPreviewHonoursTheAllowlist(t *testing.T) {
	docs := []Document{{Name: "a.md", Markdown: "info@example.com and marie.duval@example.com"}}
	allow := NewEmptyAllowlist()
	allow.Add("info@example.com")
	got := PreviewPatternMatches(docs, selection(CatEmail), CountryLU, false, allow)
	if len(got) != 1 || got[0].Text != "marie.duval@example.com" {
		t.Fatalf("want the allowlisted address gone, got %+v", got)
	}
}

// TestPreviewHonoursTheChecksumSwitch: the switch is the user's lever over a
// checksum that did not pass, so the preview must obey it or the tab would show
// rows a run then leaves in place.
func TestPreviewHonoursTheChecksumSwitch(t *testing.T) {
	// A shape-valid IBAN whose mod-97 check fails scores ConfidenceChecksumFailed
	// rather than being vetoed (CLAUDE.md §5).
	docs := []Document{{Name: "a.md", Markdown: "account LU28 0019 4006 4475 0001 please"}}

	kept := PreviewPatternMatches(docs, selection(CatIBAN), CountryLU, false, nil)
	if len(kept) != 1 {
		t.Fatalf("want the checksum-failed IBAN previewed with the switch off, got %+v", kept)
	}
	if kept[0].Confidence >= ConfidenceDeterministic {
		t.Errorf("want the failed check to lower the confidence, got %v", kept[0].Confidence)
	}
	if dropped := PreviewPatternMatches(docs, selection(CatIBAN), CountryLU, true, nil); len(dropped) != 0 {
		t.Errorf("want it dropped with the switch on, got %+v", dropped)
	}
}

// TestPreviewScopesByCountry: a category outside the document country does not
// run, exactly as pass 1 does not run it. The tab reports the ACTIVE list so it
// can say "that category never ran" rather than "it found nothing".
func TestPreviewScopesByCountry(t *testing.T) {
	docs := []Document{{Name: "a.md", Markdown: "the office is at L-1234 Luxembourg"}}
	sel := selection(CatPostalCode)

	if got := PreviewPatternMatches(docs, sel, CountryLU, false, nil); len(got) != 1 {
		t.Fatalf("want the Luxembourg postal code previewed under LU, got %+v", got)
	}
	if got := PreviewPatternMatches(docs, sel, CountryDE, false, nil); len(got) != 0 {
		t.Errorf("want no postal code under DE, got %+v", got)
	}

	if active := ActivePatternCategories(sel, CountryDE); len(active) != 0 {
		t.Errorf("want postal codes reported as not active under DE, got %v", active)
	}
	if active := ActivePatternCategories(sel, CountryLU); len(active) != 1 || active[0] != CatPostalCode {
		t.Errorf("want exactly postal codes active under LU, got %v", active)
	}
}

// TestActivePatternCategoriesKeepsEngineOrder: the frontend groups sections in
// the order this list arrives, so it has to be the engine's stable order rather
// than map iteration order.
func TestActivePatternCategoriesKeepsEngineOrder(t *testing.T) {
	sel := selection(CatAddress, CatEmail, CatIBAN)
	got := ActivePatternCategories(sel, CountryLU)
	want := []string{CatEmail, CatIBAN, CatAddress} // AllPIICategories' order
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
}

// TestPreviewReadsGridCellsNotTheRenderedTable: a CSV is detected cell by cell,
// so previewing over the rendered markdown table would report matches the run
// does not make. This is the one place the preview could quietly disagree with
// pass 1, which is why it has its own test.
func TestPreviewReadsGridCellsNotTheRenderedTable(t *testing.T) {
	docs := []Document{{
		Name:     "people.csv",
		Format:   FormatCSV,
		Markdown: "| name | mail |\n| --- | --- |\n| Marie | marie.duval@example.com |",
		Grid: [][]string{
			{"name", "mail"},
			{"Marie", "marie.duval@example.com"},
		},
	}}
	got := PreviewPatternMatches(docs, selection(CatEmail), CountryLU, false, nil)
	if len(got) != 1 || got[0].Count != 1 {
		t.Fatalf("want the cell's address counted exactly once, got %+v", got)
	}
}

// TestPreviewIsDeterministic: the tab re-renders on every repaint, so two runs
// over the same batch must produce the same order.
func TestPreviewIsDeterministic(t *testing.T) {
	docs := []Document{{
		Name:     "a.md",
		Markdown: "x@example.com and y@example.com and z@example.com",
	}}
	sel := selection(CatEmail)
	first := PreviewPatternMatches(docs, sel, CountryLU, false, nil)
	for i := 0; i < 5; i++ {
		again := PreviewPatternMatches(docs, sel, CountryLU, false, nil)
		if len(again) != len(first) {
			t.Fatalf("run %d returned %d matches, first returned %d", i, len(again), len(first))
		}
		for j := range first {
			if again[j].Text != first[j].Text || again[j].Category != first[j].Category {
				t.Fatalf("run %d reordered: %q where %q was", i, again[j].Text, first[j].Text)
			}
		}
	}
}

// TestPreviewOnTheRealFixtureShowsAddressesAndPostalCodes is the reported
// workflow, on the document it was reported against: notice that street
// addresses and postal codes were not ticked, tick them, run detection, and SEE
// what they matched.
//
// It uses the real contract rather than a constructed string because the two
// categories are exactly the ones a synthetic fixture makes look easy: an
// address line is anchored on a street-type gazetteer inside a document whose
// runs Word had fragmented, and pass 1's own suite (framework_agreement_test.go)
// measures the same two values from the other side.
func TestPreviewOnTheRealFixtureShowsAddressesAndPostalCodes(t *testing.T) {
	docs := []Document{{
		Name:     "framework_agreement.docx",
		Markdown: loadFixtureMarkdown(t, "framework_agreement.docx"),
	}}
	sel := selection(CatAddress, CatPostalCode)

	got := PreviewPatternMatches(docs, sel, CountryLU, false, nil)
	byCategory := map[string]int{}
	for _, m := range got {
		byCategory[m.Category]++
		if m.Count < 1 || len(m.Documents) != 1 {
			t.Errorf("every match must carry its occurrences and its file: %+v", m)
		}
	}
	if byCategory[CatAddress] == 0 {
		t.Errorf("want the contract's address lines previewed, got %+v", got)
	}
	if byCategory[CatPostalCode] == 0 {
		t.Errorf("want the contract's postal codes previewed, got %+v", got)
	}

	// And the counterpart the tab depends on: with the two categories NOT
	// ticked, they are not in the active list, so the tab can say they never ran
	// instead of showing two empty sections that look like a failure to match.
	if active := ActivePatternCategories(selection(CatEmail), CountryLU); len(active) != 1 {
		t.Errorf("want only the ticked category reported active, got %v", active)
	}
}

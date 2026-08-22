// engine/exportfmt/pdfcensus_test.go — the ladder census and the detection
// counts, as helpers every tier can call.
//
// NO BUILD TAG, on purpose. These two measurements are the export's strongest
// guards, and they used to live in the deep tier alone, which meant they ran
// only on one machine, only with two environment variables set, and only over
// documents nobody else can read. An occurrence the ladder cannot locate
// REFUSES a user's export, so the rule that there are none has to be enforced
// where every push can see it: the integration tier runs both over committed
// fixtures, and the deep tier reuses the same helpers over the confidential
// reference documents for scale.
//
// Both are COUNTS over strings that never leave the process. The needles come
// from detection over the extracted text, so on a confidential document they
// are real values; nothing here logs one, and nothing here returns one to a
// caller that logs it.
package exportfmt

import (
	"testing"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/engine/convert"
)

// ladderCensus is which rung locates each string a run would replace, over one
// document, through the PRODUCTION ladder and the PRODUCTION pipeline text.
type ladderCensus struct {
	literal   int
	tolerant  int
	fragment  int
	wrapped   int
	unlocated int
	pages     int
	// needles is how many occurrences were examined, so a census of zero
	// cannot read as a clean result. A document detection finds nothing in
	// proves nothing about the ladder.
	needles int
}

// total is every located occurrence plus the unlocated ones.
func (c ladderCensus) total() int {
	return c.literal + c.tolerant + c.fragment + c.wrapped + c.unlocated
}

// runLadderCensus walks every page of a PDF, asks detection what a run would
// replace on that page, and records which rung finds each one there.
//
// It deliberately opens through openPDFForExport, the same entry point the
// export uses, so the census cannot pass over a document the export would fail
// to open.
func runLadderCensus(t *testing.T, raw []byte) ladderCensus {
	t.Helper()
	doc, layouts, err := openPDFForExport(raw)
	if err != nil {
		t.Fatalf("opening the document for the ladder census: %v", err)
	}
	var c ladderCensus
	pages := doc.Pages()
	c.pages = len(pages)
	for pi, page := range pages {
		if pi >= len(layouts) {
			break
		}
		pageText := convert.RepairPDFText(convert.PDFPageText(layouts[pi]))
		searcher := livePDFSearcher{page: page}
		for _, needle := range gatherNeedles(t, pageText) {
			c.needles++
			switch locatePDFValue(needle, "[CENSUS]", searcher, layouts[pi]).rung {
			case rungLiteral:
				c.literal++
			case rungTolerant:
				c.tolerant++
			case rungFragment:
				c.fragment++
			case rungWrapped:
				c.wrapped++
			default:
				c.unlocated++
			}
		}
	}
	return c
}

// reportCensus logs the whole rung distribution and fails on any unlocated
// occurrence.
//
// The distribution is logged even when the census passes, because it is DATA
// and not just a pass. Occurrences migrating from the literal rung to the
// fragment walk is a real statement about extraction (the same text is now
// being drawn in more pieces, or read in more pieces) and it is worth seeing
// before it becomes a refusal, which is what UNLOCATED means for a user.
func reportCensus(t *testing.T, label string, c ladderCensus) {
	t.Helper()
	t.Logf("ladder census %s: pages %d, occurrences %d -> literal %d, tolerant %d, fragment %d, wrapped %d, UNLOCATED %d",
		label, c.pages, c.needles, c.literal, c.tolerant, c.fragment, c.wrapped, c.unlocated)
	if c.needles == 0 {
		t.Errorf("ladder census %s examined NO occurrences; detection found nothing in this document, so the census proves nothing about the ladder. Fix the fixture or the selection, do not read this as a pass", label)
	}
	if c.unlocated != 0 {
		t.Errorf("ladder census %s: %d occurrence(s) are UNLOCATED. Each one REFUSES a user's export of this document, naming the placeholder and the page. Either the extraction changed what the pipeline detects, or the ladder lost a rung; counts only in any note, never the strings",
			label, c.unlocated)
	}
}

// detectionCounts runs the offline detection the floors are measured on:
// pass 1's preview over every category plus heuristic discovery at defaults,
// over one extraction, returning values-found-per-category. Counts only.
func detectionCounts(t *testing.T, name, markdown string) map[string]int {
	t.Helper()
	counts := map[string]int{}
	doc, err := engine.Load(name+".md", []byte(markdown))
	if err != nil {
		t.Fatalf("loading the extraction as a document: %v", err)
	}
	sel := engine.CategorySelection{}
	for _, cat := range engine.AllPIICategories {
		sel[cat] = true
	}
	for _, m := range engine.PreviewPatternMatches([]engine.Document{doc}, sel, engine.CountryLU, false, nil) {
		counts[m.Category]++
	}
	for _, s := range engine.HeuristicDiscoverWithOptions(markdown, nil, engine.DefaultHeuristicDiscoveryOptions()) {
		counts[s.Category]++
	}
	return counts
}

// countAll totals a per-category count map.
func countAll(counts map[string]int) int {
	total := 0
	for _, n := range counts {
		total += n
	}
	return total
}

// gatherNeedles runs the same detection the counts use and returns the strings
// a run would replace: pattern-preview texts plus heuristic suggestion main
// texts. The strings stay inside the process; only counts derived from them are
// ever logged.
func gatherNeedles(t *testing.T, markdown string) []string {
	t.Helper()
	doc, err := engine.Load("needles.md", []byte(markdown))
	if err != nil {
		t.Fatalf("loading extraction: %v", err)
	}
	sel := engine.CategorySelection{}
	for _, cat := range engine.AllPIICategories {
		sel[cat] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range engine.PreviewPatternMatches([]engine.Document{doc}, sel, engine.CountryLU, false, nil) {
		if !seen[m.Text] {
			seen[m.Text] = true
			out = append(out, m.Text)
		}
	}
	for _, s := range engine.HeuristicDiscoverWithOptions(markdown, nil, engine.DefaultHeuristicDiscoveryOptions()) {
		if !seen[s.MainText] {
			seen[s.MainText] = true
			out = append(out, s.MainText)
		}
	}
	return out
}

// referenceFloorTolerance is how far below its floor ONE category may fall
// before the gate fails.
//
// It is 1, and it is not slack for its own sake. The line model decides where
// a line splits and whether a wrapped line joins on measured geometry
// (CLAUDE.md §5), so a value sitting exactly on one of those thresholds moves
// in or out of the count when an unrelated threshold is tuned. A value that
// leaves the count that way is usually text that was never contiguous on the
// page: a run the split rule refuses to glue is a value the document does not
// contain, and counting it was the defect.
//
// What the gate protects against is a CLASS collapsing: a category losing
// several values, or all of them, because a rule stopped firing. One value is
// threshold noise; two is a change somebody has to explain.
const referenceFloorTolerance = 1

// assertCategoryFloors fails when a category yields materially fewer values
// than recorded. See referenceFloorTolerance for how much one category may
// move and why.
func assertCategoryFloors(t *testing.T, label string, floors, got map[string]int) {
	t.Helper()
	if len(floors) == 0 {
		t.Fatalf("%s: no recorded floors; add them (counts only) before measuring against them", label)
	}
	t.Logf("%s: total values found %d", label, countAll(got))
	for cat, floor := range floors {
		n := got[cat]
		t.Logf("%s category %s: found %d, floor %d", label, cat, n, floor)
		if n < floor-referenceFloorTolerance {
			t.Errorf("%s: category %s found %d values against a floor of %d (tolerance %d); a category losing more than one value is an extraction regression, not threshold noise. If the document itself changed, re-baseline the floors as its own change",
				label, cat, n, floor, referenceFloorTolerance)
		}
	}
	for cat, n := range got {
		if _, ok := floors[cat]; !ok {
			t.Logf("%s: category %s is above the recorded floors entirely: %d values found, no floor recorded", label, cat, n)
		}
	}
}

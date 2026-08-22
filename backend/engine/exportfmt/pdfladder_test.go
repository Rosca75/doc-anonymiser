// engine/exportfmt/pdfladder_test.go — the location ladder's rung selection,
// one case per rung, over synthetic fragment geometries and a fake searcher.
//
// White-box (package exportfmt): the rungs are deliberately unexported
// internals of the export, and which rung answers is exactly the behaviour
// under test.
package exportfmt

import (
	"regexp"
	"testing"

	asposepdf "github.com/aspose-pdf-foss/aspose-pdf-foss-for-go"

	"doc-anonymiser/backend/engine/convert"
)

// fakeSearcher mimics the library's page search over a set of fragments: a
// literal query matches inside ONE fragment's text only (the documented
// single-text-operation blindness the fragment rung exists for); a regex
// query is matched the same way.
type fakeSearcher struct {
	layout convert.PDFPageLayout
}

func (s fakeSearcher) search(query string, regex bool) []asposepdf.TextMatch {
	var re *regexp.Regexp
	if regex {
		r, err := regexp.Compile(query)
		if err != nil {
			return nil
		}
		re = r
	} else {
		re = regexp.MustCompile(regexp.QuoteMeta(query))
	}
	var out []asposepdf.TextMatch
	for _, line := range s.layout.Lines {
		for _, f := range line.Fragments {
			for _, loc := range re.FindAllStringIndex(f.Text, -1) {
				n := len([]rune(f.Text))
				glyph := f.Width / float64(n)
				r0 := len([]rune(f.Text[:loc[0]]))
				r1 := len([]rune(f.Text[:loc[1]]))
				out = append(out, asposepdf.TextMatch{
					Text:       f.Text[loc[0]:loc[1]],
					PageNumber: 1,
					Rect: asposepdf.Rectangle{
						LLX: f.X + float64(r0)*glyph, LLY: f.Y,
						URX: f.X + float64(r1)*glyph, URY: f.Y + f.Height,
					},
				})
			}
		}
	}
	return out
}

// ladderFrag builds a fragment with Courier-exact geometry (0.6 em glyphs).
func ladderFrag(text string, x, y, size float64) convert.PDFFragment {
	return convert.PDFFragment{
		Text: text, X: x, Y: y,
		Width: 0.6 * size * float64(len([]rune(text))), Height: size, FontSize: size,
	}
}

func oneLineLayout(frags ...convert.PDFFragment) convert.PDFPageLayout {
	return convert.PDFPageLayout{Lines: []convert.PDFLine{{Fragments: frags}}}
}

func TestPDFLadderRungSelection(t *testing.T) {
	t.Run("redaction/rung1_literal_replaces_when_the_growth_fits", func(t *testing.T) {
		// The value ends its line: the grown placeholder has empty space to
		// the right, so rung 1 replaces in place.
		layout := oneLineLayout(ladderFrag("Countersigned by Quentin Marsh", 72, 500, 12))
		got := locatePDFValue("Quentin Marsh", "[PERSON_1]", fakeSearcher{layout}, layout)
		if got.rung != rungLiteral || !got.replaceInPlace {
			t.Errorf("rung = %v replaceInPlace = %v, want the literal rung replacing in place: the value sits at line end with room to grow", got.rung, got.replaceInPlace)
		}
		if len(got.occurrences) != 1 {
			t.Errorf("occurrences = %d, want 1", len(got.occurrences))
		}
	})

	t.Run("redaction/rung1_falls_to_redaction_when_the_growth_would_overlap", func(t *testing.T) {
		// The value sits mid-line with text right behind it: the replacement
		// grows rightward without reflow, so replacing in place would paint
		// over the neighbour. The occurrence is redacted instead.
		layout := oneLineLayout(ladderFrag("Signed by Jean on behalf of the supervisory board today", 72, 500, 12))
		got := locatePDFValue("Jean", "[PERSON_1_WITH_A_LONG_TAIL]", fakeSearcher{layout}, layout)
		if got.rung != rungTolerant || got.replaceInPlace {
			t.Errorf("rung = %v replaceInPlace = %v, want the redaction gesture: the grown replacement reaches the same-line neighbour", got.rung, got.replaceInPlace)
		}
	})

	t.Run("redaction/rung2_tolerant_pattern_finds_a_repaired_seam", func(t *testing.T) {
		// The pipeline holds "BIDDING" where the page draws "BIDD ING": the
		// literal search misses, the whitespace-tolerant pattern does not.
		layout := oneLineLayout(ladderFrag("BIDD ING rules apply", 72, 500, 12))
		got := locatePDFValue("BIDDING", "[OTHER_1]", fakeSearcher{layout}, layout)
		if got.rung != rungTolerant {
			t.Errorf("rung = %v, want the tolerant rung: the value exists on the page with one collapsed seam", got.rung)
		}
	})

	t.Run("redaction/rung3_fragment_walk_spans_two_draw_operations", func(t *testing.T) {
		// One name, two text operations on one line: invisible to both search
		// rungs (the search sees one operation at a time), located by walking
		// the line's fragments.
		a := ladderFrag("Sylvie", 72, 500, 28)
		b := ladderFrag("Renard", a.X+a.Width+6.7, 500, 28)
		layout := oneLineLayout(a, b)
		got := locatePDFValue("Sylvie Renard", "[PERSON_1]", fakeSearcher{layout}, layout)
		if got.rung != rungFragment {
			t.Fatalf("rung = %v, want the fragment walk: the value spans two draw operations on one line", got.rung)
		}
		rect := got.occurrences[0][0]
		if rect.LLX > a.X+0.5 || rect.URX < b.X+b.Width-0.5 {
			t.Errorf("occurrence rect (%.1f..%.1f) does not cover both fragments (%.1f..%.1f)", rect.LLX, rect.URX, a.X, b.X+b.Width)
		}
	})

	t.Run("redaction/rung3_matches_a_spelling_no_extraction_contains", func(t *testing.T) {
		// The pipeline's text is ligature-folded, so it holds a spelling that
		// exists verbatim in NO extraction; the walk folds the fragments the
		// same way and still finds it.
		layout := oneLineLayout(ladderFrag("Report by Soﬁa Verne", 72, 500, 12))
		got := locatePDFValue("Sofia Verne", "[PERSON_1]", fakeSearcher{layout}, layout)
		if got.rung != rungFragment {
			t.Errorf("rung = %v, want the fragment walk: the pipeline's folded spelling must still be locatable", got.rung)
		}
	})

	t.Run("redaction/rung4_wrapped_value_yields_head_and_tail_boxes", func(t *testing.T) {
		layout := convert.PDFPageLayout{Lines: []convert.PDFLine{
			{Fragments: []convert.PDFFragment{ladderFrag("The renewal was approved by Victor", 72, 500, 12)}},
			{Fragments: []convert.PDFFragment{ladderFrag("Beaulieu on behalf of the board", 72, 484, 12)}},
		}}
		got := locatePDFValue("Victor Beaulieu", "[PERSON_1]", fakeSearcher{layout}, layout)
		if got.rung != rungWrapped {
			t.Fatalf("rung = %v, want the wrapped rung: the value breaks across two stacked lines", got.rung)
		}
		if len(got.occurrences) != 1 || len(got.occurrences[0]) != 2 {
			t.Fatalf("occurrences = %+v, want one occurrence of two rectangles (head and tail)", got.occurrences)
		}
		head, tail := got.occurrences[0][0], got.occurrences[0][1]
		if head.LLY <= tail.LLY {
			t.Errorf("head (%.1f) must sit above tail (%.1f): the placeholder overlay rides the head", head.LLY, tail.LLY)
		}
	})

	t.Run("errors/rung5_unlocated_when_nothing_matches", func(t *testing.T) {
		// The interleaved-capitals repair reorders letters, so the repaired
		// string matches no rung; the export's refusal owns what happens next.
		layout := oneLineLayout(ladderFrag("B R IDDING ULES apply here", 72, 500, 12))
		got := locatePDFValue("BIDDING RULES", "[OTHER_1]", fakeSearcher{layout}, layout)
		if got.rung != rungUnlocated {
			t.Errorf("rung = %v, want UNLOCATED: a reordered repair is not locatable and must reach the refusal, never a silent skip", got.rung)
		}
	})
}

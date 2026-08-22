// engine/convert/pdflayout_test.go — the fragment line model's rules, over
// synthetic fragment geometries: the split rule (fragments that merely share
// a baseline are not one line), the join rule (a wrapped continuation flows
// back into its line), and the text derivation both feed.
//
// The split table carries the ten measured gaps from a real deck: five real
// word spacings that must NOT split (6.7 to 8.6 pt) and five baseline-sharing
// gaps that MUST (42.9 to 948.4 pt), each tried at 12 pt and at 28 pt because
// the threshold is a multiple of the font size, never a point value.
package convert

import (
	"strings"
	"testing"
)

// frag builds one fragment at x with the given text, sized for the font: a
// Courier-like 0.6 em glyph advance keeps the geometry exact and readable.
func frag(text string, x, size float64) PDFFragment {
	return PDFFragment{
		Text: text, X: x, Y: 500, Width: 0.6 * size * float64(len([]rune(text))),
		Height: size, FontSize: size,
	}
}

// fragAt is frag with an explicit baseline, for the join-rule tables.
func fragAt(text string, x, y, size float64) PDFFragment {
	f := frag(text, x, size)
	f.Y = y
	return f
}

func TestSplitLineOnGaps(t *testing.T) {
	// The measured populations (points): real word spacing against
	// merely-shared baselines.
	noSplit := []float64{6.7, 6.7, 8.0, 8.2, 8.6}
	mustSplit := []float64{42.9, 383.2, 384.8, 948.4, 948.4}

	for _, size := range []float64{12, 28} {
		for _, gap := range noSplit {
			t.Run("extraction/keeps_one_line", func(t *testing.T) {
				a := frag("Sylvie", 72, size)
				b := frag("Renard", a.X+a.Width+gap, size)
				lines := splitLineOnGaps([]PDFFragment{a, b})
				if len(lines) != 1 {
					t.Errorf("a %.1f pt gap at %.0f pt split the line into %d; real word spacing must stay one line", gap, size, len(lines))
				}
			})
		}
		for _, gap := range mustSplit {
			t.Run("extraction/splits_shared_baseline", func(t *testing.T) {
				a := frag("Bertrand", 72, size)
				b := frag("Malraux", a.X+a.Width+gap, size)
				lines := splitLineOnGaps([]PDFFragment{a, b})
				if len(lines) != 2 {
					t.Errorf("a %.1f pt gap at %.0f pt stayed one line; fragments that merely share a baseline manufacture a value", gap, size)
				}
			})
		}
	}

	t.Run("extraction/split_keeps_fragment_order_and_content", func(t *testing.T) {
		a := frag("Left", 72, 12)
		b := frag("Middle", a.X+a.Width+5, 12)
		c := frag("Right", b.X+b.Width+400, 12)
		lines := splitLineOnGaps([]PDFFragment{a, b, c})
		if len(lines) != 2 {
			t.Fatalf("got %d lines, want 2 (split only at the 400 pt gap)", len(lines))
		}
		if got := lineText(lines[0]); got != "Left Middle" {
			t.Errorf("first line text = %q, want %q", got, "Left Middle")
		}
		if got := lineText(lines[1]); got != "Right" {
			t.Errorf("second line text = %q, want %q", got, "Right")
		}
	})
}

func TestLineTextSpacing(t *testing.T) {
	t.Run("extraction/kerning_gap_joins_without_space", func(t *testing.T) {
		// A sub-word-space gap is kerning: the runs are one word.
		a := frag("Bid", 72, 12)
		b := frag("ding", a.X+a.Width+1.0, 12)
		if got := lineText(PDFLine{Fragments: []PDFFragment{a, b}}); got != "Bidding" {
			t.Errorf("lineText = %q, want %q (a 1 pt gap at 12 pt is kerning, not a space)", got, "Bidding")
		}
	})
	t.Run("extraction/word_gap_joins_with_one_space", func(t *testing.T) {
		a := frag("Sylvie", 72, 12)
		b := frag("Renard", a.X+a.Width+3.0, 12)
		if got := lineText(PDFLine{Fragments: []PDFFragment{a, b}}); got != "Sylvie Renard" {
			t.Errorf("lineText = %q, want %q (a 3 pt gap at 12 pt is a word space)", got, "Sylvie Renard")
		}
	})
}

// wrapPair builds the geometry of a genuine wrap: a full first line and its
// continuation, sharing a left margin, one line height apart.
func wrapPair() []PDFLine {
	return []PDFLine{
		{Fragments: []PDFFragment{fragAt("The contract was signed for Nordwind", 72, 340, 12)}},
		{Fragments: []PDFFragment{fragAt("Associates by the managing director", 72, 327, 12)}},
	}
}

func TestMarkWrappedJoins(t *testing.T) {
	t.Run("extraction/genuine_wrap_joins", func(t *testing.T) {
		lines := wrapPair()
		markWrappedJoins(lines)
		if !lines[0].JoinsNext {
			t.Error("a full line with a same-margin continuation one line height below must join; the wrapped value is otherwise two half-values")
		}
	})

	t.Run("extraction/blank_line_between_the_halves_is_stepped_over", func(t *testing.T) {
		pair := wrapPair()
		lines := []PDFLine{
			pair[0],
			{Fragments: []PDFFragment{fragAt(" ", 72, 334, 12)}},
			pair[1],
		}
		lines[2].Fragments[0].Y = 327
		markWrappedJoins(lines)
		if !lines[0].JoinsNext || !lines[1].JoinsNext {
			t.Errorf("an interleaved whitespace-only line must be joined through (got %v, %v); otherwise the join never fires on writers that paint one", lines[0].JoinsNext, lines[1].JoinsNext)
		}
	})

	blocked := []struct {
		name   string
		mutate func(lines []PDFLine)
		why    string
	}{
		{"terminal_punctuation_ends_the_line", func(lines []PDFLine) {
			lines[0].Fragments[0].Text = "The heading stands alone."
		}, "a line ending a sentence is complete; joining glued headings and invented names"},
		{"paragraph_gap_is_not_a_wrap", func(lines []PDFLine) {
			lines[1].Fragments[0].Y = 340 - 2.5*12
		}, "a baseline further than the gate is a paragraph break"},
		{"different_left_margin_is_a_new_block", func(lines []PDFLine) {
			lines[1].Fragments[0].X = 130
		}, "an indented successor starts its own block"},
		{"short_first_line_ended_by_choice", func(lines []PDFLine) {
			lines[0].Fragments[0].Text = "Nordwind"
			lines[0].Fragments[0].Width = 8 * 7.2
		}, "a line stopping short of its block's right edge did not wrap"},
	}
	for _, tt := range blocked {
		t.Run("extraction/no_join_when_"+tt.name, func(t *testing.T) {
			lines := wrapPair()
			tt.mutate(lines)
			markWrappedJoins(lines)
			if lines[0].JoinsNext {
				t.Errorf("the lines joined; %s", tt.why)
			}
		})
	}
}

func TestPDFPageTextDerivation(t *testing.T) {
	t.Run("extraction/joined_lines_flow_with_a_space", func(t *testing.T) {
		lines := wrapPair()
		markWrappedJoins(lines)
		text := PDFPageText(PDFPageLayout{Lines: lines})
		if !strings.Contains(text, "Nordwind Associates") {
			t.Errorf("derived text %q does not read the wrapped value whole", text)
		}
		if strings.Count(text, "\n") != 0 {
			t.Errorf("derived text %q still breaks the joined pair with a newline", text)
		}
	})
	t.Run("extraction/split_lines_stay_separate", func(t *testing.T) {
		a := frag("Bertrand", 72, 28)
		b := frag("Malraux", a.X+a.Width+384.8, 28)
		lines := splitLineOnGaps([]PDFFragment{a, b})
		markWrappedJoins(lines)
		text := PDFPageText(PDFPageLayout{Lines: lines})
		if strings.Contains(text, "Bertrand Malraux") {
			t.Errorf("derived text %q manufactures a value out of fragments that merely share a baseline", text)
		}
	})
}

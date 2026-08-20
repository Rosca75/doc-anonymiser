// engine/imaging/font8x8_test.go — the vendored font table, the accent folding
// and the box's text layout, unit tier.
//
// TIER: unit (docs/TESTING.md).
//
// WHITE BOX on purpose (package imaging). The glyph table is vendored DATA, and
// the failure it can have is a transcription slip: one row of one glyph wrong, or
// a whole glyph left empty. Neither is visible from outside the package, and
// neither shows up as an error: the box simply comes out with a hole in the
// caption. Asserting the table directly is the only way to catch that, and
// docs/TESTING.md allows a white-box test where unexported behaviour is the
// subject.
package imaging

import (
	"strings"
	"testing"
)

// TestEveryPrintableGlyphIsDrawn: every printable ASCII character has ink,
// except the space, which is defined by having none. A glyph of all zeroes is
// how a transcription slip shows itself.
func TestEveryPrintableGlyphIsDrawn(t *testing.T) {
	t.Run("fonts/glyphs_every_printable_has_ink", func(t *testing.T) {
		if got := len(glyphs8x8); got != int(lastGlyph-firstGlyph+1) {
			t.Fatalf("the table holds %d glyphs, want %d (ASCII %d to %d)",
				got, int(lastGlyph-firstGlyph+1), firstGlyph, lastGlyph)
		}
		for r := rune(firstGlyph); r <= lastGlyph; r++ {
			glyph := glyphFor(r)
			ink := false
			for _, row := range glyph {
				if row != 0 {
					ink = true
					break
				}
			}
			if r == ' ' {
				if ink {
					t.Errorf("the space glyph carries ink (%v); a space is defined by having "+
						"none", glyph)
				}
				continue
			}
			if !ink {
				t.Errorf("the glyph for %q (U+%04X) is empty, so it draws as a hole in the "+
					"caption; the table is mis-transcribed at that row", r, r)
			}
		}
	})
}

// TestGlyphShapesAreRightWayRound: a spot check on characters whose shape is
// unmistakable, because "every glyph has ink" would pass on a table that was
// mirrored or upside down.
//
// Bit 0 is the LEFTMOST pixel, so the expected pattern is read left to right.
func TestGlyphShapesAreRightWayRound(t *testing.T) {
	render := func(r rune) []string {
		glyph := glyphFor(r)
		out := make([]string, 0, glyphHeight)
		for _, row := range glyph {
			var b strings.Builder
			for col := 0; col < glyphWidth; col++ {
				if row&(1<<uint(col)) != 0 {
					b.WriteByte('#')
					continue
				}
				b.WriteByte('.')
			}
			out = append(out, b.String())
		}
		return out
	}

	cases := []struct {
		name string
		r    rune
		want []string
	}{
		{
			// An underscore sits on the BOTTOM row and nowhere else. A flipped
			// table would put it on the top one.
			name: "fonts/glyphs_underscore_is_at_the_bottom",
			r:    '_',
			want: []string{
				"........", "........", "........", "........",
				"........", "........", "........", "########",
			},
		},
		{
			// A full stop sits low and to the left of centre. A mirrored table
			// would put it to the right.
			name: "fonts/glyphs_full_stop_is_low_and_left_of_centre",
			r:    '.',
			want: []string{
				"........", "........", "........", "........",
				"........", "..##....", "..##....", "........",
			},
		},
		{
			// A hyphen is one horizontal bar in the middle.
			name: "fonts/glyphs_hyphen_is_one_middle_bar",
			r:    '-',
			want: []string{
				"........", "........", "........", "######..",
				"........", "........", "........", "........",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := render(c.r)
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Errorf("the glyph for %q renders as\n%s\nwant\n%s",
						c.r, strings.Join(got, "\n"), strings.Join(c.want, "\n"))
					return
				}
			}
		})
	}
}

// TestFoldToDrawable: what a French, German or Luxembourgish caption becomes.
// Folding rather than dropping, because a name with its accents removed is still
// the name the reviewer typed.
func TestFoldToDrawable(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"fonts/folds_french_accents", "Réunion à Genève, Amélie Lefèvre", "Reunion a Geneve, Amelie Lefevre"},
		{"fonts/folds_french_circumflex_and_cedilla", "Contrôle française, hôpital", "Controle francaise, hopital"},
		{"fonts/folds_german_umlauts", "Müller, Grüße, Österreich, Ähnlich", "Muller, Grusse, Osterreich, Ahnlich"},
		{"fonts/folds_sharp_s_to_two_letters", "Straße", "Strasse"},
		{"fonts/folds_ligatures", "Œuvre, Ægis", "OEuvre, AEgis"},
		{"fonts/folds_typographic_punctuation", "“Acme” — l’an…", `"Acme" - l'an...`},
		{"fonts/folds_currency_and_marks", "12 € (©)", "12 EUR ((c))"},
		{"fonts/folds_whitespace_to_spaces", "one\ttwo\nthree", "one two three"},
		{"fonts/leaves_ascii_alone", "Alpine Trust 2026 (draft) #4", "Alpine Trust 2026 (draft) #4"},
		{"fonts/unmappable_becomes_a_question_mark", "Проект 東京", "?????? ??"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := foldToDrawable(c.in); got != c.want {
				t.Errorf("foldToDrawable(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestEveryFoldIsDrawable: the folding table must not map a character to
// something the glyph table cannot draw, or the fold would just move the hole.
func TestEveryFoldIsDrawable(t *testing.T) {
	t.Run("fonts/folds_land_on_drawable_characters", func(t *testing.T) {
		for from, to := range asciiFolds {
			if to == "" {
				t.Errorf("the fold for %q (U+%04X) is empty; a character that folds to nothing "+
					"closes the gap silently and should be left to the question mark", from, from)
				continue
			}
			for _, r := range to {
				if r < firstGlyph || r > lastGlyph {
					t.Errorf("%q (U+%04X) folds to %q, which contains %q (U+%04X); the glyph "+
						"table has no shape for it", from, from, to, r, r)
				}
			}
		}
	})
}

// TestWrapText: the wrap is on spaces where it can be and inside a word where it
// must be. A 40-character reference code has no space to break at, and letting
// it run past the edge would draw it over the border.
func TestWrapText(t *testing.T) {
	cases := []struct {
		name string
		text string
		cols int
		want []string
	}{
		{"fonts/wrap_on_spaces", "Alpine Trust logo removed", 12, []string{"Alpine Trust", "logo removed"}},
		{"fonts/wrap_one_word_per_line", "Alpine Trust", 6, []string{"Alpine", "Trust"}},
		{"fonts/wrap_hard_breaks_a_long_word", "AAAABBBBCCCC", 4, []string{"AAAA", "BBBB", "CCCC"}},
		{"fonts/wrap_collapses_runs_of_spaces", "a    b", 3, []string{"a b"}},
		{"fonts/wrap_of_whitespace_is_nothing", "   ", 8, nil},
		{"fonts/wrap_with_no_columns_is_nothing", "text", 0, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := wrapText(c.text, c.cols)
			if len(got) != len(c.want) {
				t.Fatalf("wrapText(%q, %d) = %q, want %q", c.text, c.cols, got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("wrapText(%q, %d) = %q, want %q", c.text, c.cols, got, c.want)
				}
			}
			for i, line := range got {
				if len([]rune(line)) > c.cols {
					t.Errorf("line %d (%q) is %d characters, above the %d the box holds",
						i, line, len([]rune(line)), c.cols)
				}
			}
		})
	}
}

// TestLayoutBoxTextPicksTheLargestFittingScale: the scale is derived from the box
// and the text, so a big box gets big letters and a small one still gets legible
// ones.
func TestLayoutBoxTextPicksTheLargestFittingScale(t *testing.T) {
	cases := []struct {
		name      string
		w, h      int
		text      string
		wantScale int
		wantLines []string
	}{
		{
			// One short word in a wide box. The margin grows with the scale, so
			// the biggest scale that holds "Logo" on ONE line is 8: it leaves
			// four columns and three rows, where scale 9 leaves three columns
			// and would break the word.
			name: "fonts/layout_large_box_large_scale",
			w:    400, h: 400, text: "Logo",
			wantScale: 8, wantLines: []string{"Logo"},
		},
		{
			// A 120x60 box: at scale 1 the text area is 104x44, which is 13
			// columns and 4 rows.
			name: "fonts/layout_small_box_wraps",
			w:    120, h: 60, text: "Alpine Trust logo removed",
			wantScale: 1, wantLines: []string{"Alpine Trust", "logo removed"},
		},
		{
			// Too small for one legible character: the box carries no caption,
			// which is the honest degradation. The picture is still replaced.
			name: "fonts/layout_tiny_box_draws_nothing",
			w:    16, h: 16, text: "Logo",
			wantScale: 0, wantLines: nil,
		},
		{
			name: "fonts/layout_no_text_draws_nothing",
			w:    200, h: 200, text: "",
			wantScale: 0, wantLines: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := layoutBoxText(c.w, c.h, c.text)
			if got.Scale != c.wantScale {
				t.Errorf("layoutBoxText(%d, %d, %q).Scale = %d, want %d (lines %q)",
					c.w, c.h, c.text, got.Scale, c.wantScale, got.Lines)
			}
			if len(got.Lines) != len(c.wantLines) {
				t.Fatalf("lines = %q, want %q", got.Lines, c.wantLines)
			}
			for i := range c.wantLines {
				if got.Lines[i] != c.wantLines[i] {
					t.Errorf("lines = %q, want %q", got.Lines, c.wantLines)
					return
				}
			}
			if got.Scale == 0 {
				return
			}
			// Whatever it chose has to FIT, which is the property the scale
			// search exists for.
			margin := glyphAdvance * got.Scale
			for _, line := range got.Lines {
				if width := len([]rune(line)) * glyphAdvance * got.Scale; width > c.w-2*margin {
					t.Errorf("the line %q is %d pixels wide at scale %d, above the %d the box "+
						"holds inside its margin", line, width, got.Scale, c.w-2*margin)
				}
			}
			if height := len(got.Lines) * lineAdvance * got.Scale; height > c.h-2*margin {
				t.Errorf("%d lines are %d pixels tall at scale %d, above the %d the box holds "+
					"inside its margin", len(got.Lines), height, got.Scale, c.h-2*margin)
			}
		})
	}
}

// TestLayoutBoxTextTruncatesWhatCannotFit: at scale 1 there is nothing left to
// shrink, so the text is cut and the cut is MARKED. A box that silently drops
// half the caption lies about what it says.
func TestLayoutBoxTextTruncatesWhatCannotFit(t *testing.T) {
	t.Run("fonts/layout_marks_a_truncation", func(t *testing.T) {
		// A 60x40 box at scale 1 holds 44/8 = 5 columns and 24/10 = 2 rows.
		got := layoutBoxText(60, 40, "Alpine Trust logo removed from this slide")
		if got.Scale != 1 {
			t.Fatalf("scale = %d, want 1: there is nothing smaller to fall back to", got.Scale)
		}
		if len(got.Lines) != 2 {
			t.Fatalf("lines = %q, want 2 of them", got.Lines)
		}
		if !strings.HasSuffix(got.Lines[1], "...") {
			t.Errorf("the last line %q does not end in three dots, so the user cannot tell the "+
				"caption was cut", got.Lines[1])
		}
		if strings.ContainsRune(strings.Join(got.Lines, ""), '…') {
			t.Error("the truncation used the ellipsis character, which the glyph table has no " +
				"shape for; it must be three ASCII periods")
		}
	})
}

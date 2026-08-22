// engine/exportfmt/pdfladder.go — the PDF location ladder: how one string the
// pipeline decided to replace is found on the page it sits on.
//
// The pipeline works in STRINGS over the derived page text; the PDF stores
// positioned glyph FRAGMENTS. The ladder bridges the two, rung by rung, each
// rung more expensive and less precise than the one above it:
//
//  1. LITERAL: the library's text search finds the string whole, and the
//     grown replacement fits its line, so ReplaceText redraws it in place.
//  2. TOLERANT: the search finds it through a whitespace-tolerant pattern
//     (the converter's repairs may have collapsed a seam), or it was found
//     literally but the replacement would overrun a neighbour; either way the
//     occurrence is REDACTED with the placeholder drawn as overlay text.
//  3. FRAGMENT WALK: the search finds nothing because the value is split
//     across draw operations, or because the pipeline's spelling differs from
//     any extraction in whitespace or ligatures. The walk matches the value
//     across CONSECUTIVE fragments of one line, whitespace-insensitively and
//     ligature-folded, and redacts the union of their rectangles.
//  4. WRAPPED: the value breaks across two stacked lines; the head fragment
//     run ends one line and the tail starts the next, both are redacted, the
//     placeholder rides the head.
//  5. UNLOCATED: nothing found. The export refuses (pdfinplace.go owns the
//     refusal) rather than shipping a file that still carries the value.
//
// The rung decisions are pure functions over the line model plus a search
// interface, so the selection logic is unit-testable without a document.
package exportfmt

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	asposepdf "github.com/aspose-pdf-foss/aspose-pdf-foss-for-go"

	"doc-anonymiser/backend/engine/convert"
)

// PDFRungCounts is one export's ladder tally, reported to the export review
// panel and the run report: "12 replaced literally, 3 across fragments" is
// the honest description of what the produced file looks like.
type PDFRungCounts struct {
	Literal  int `json:"literal"`
	Tolerant int `json:"tolerant"`
	Fragment int `json:"fragment"`
	Wrapped  int `json:"wrapped"`
}

// Total is the number of located occurrences across every rung.
func (c PDFRungCounts) Total() int {
	return c.Literal + c.Tolerant + c.Fragment + c.Wrapped
}

// PDFUnlocated is one occurrence the whole ladder failed on: the value's
// placeholder (never the value itself; the refusal redacts) and the page
// whose derived text contains it.
type PDFUnlocated struct {
	Placeholder string `json:"placeholder"`
	Page        int    `json:"page"`
}

// pdfRung names a ladder rung for one located value on one page.
type pdfRung int

const (
	rungUnlocated pdfRung = iota
	rungLiteral
	rungTolerant
	rungFragment
	rungWrapped
)

// pdfSearcher is the one seam between the rung selection and the library:
// a page-scoped text search. The production implementation is the library's
// Page.SearchText; the unit tests fake it.
type pdfSearcher interface {
	// search returns the page's matches for query, treated as a literal
	// string or an RE2 pattern. An error reads as "no matches": an
	// unsearchable value is exactly what the lower rungs exist for.
	search(query string, regex bool) []asposepdf.TextMatch
}

// pdfLocated is the ladder's answer for one (value, placeholder) on one page.
type pdfLocated struct {
	rung pdfRung
	// replaceInPlace is true only on rung 1: apply through ReplaceText.
	replaceInPlace bool
	// occurrences carries, per occurrence, the rectangles to redact. The
	// FIRST rectangle of each occurrence receives the placeholder overlay;
	// any further rectangle (a wrapped tail) is a plain redaction box.
	occurrences [][]asposepdf.Rectangle
}

// locatePDFValue runs the whole ladder for one value on one page.
func locatePDFValue(value, placeholder string, s pdfSearcher, layout convert.PDFPageLayout) pdfLocated {
	// Rung 1: literal, gated by the fits-check. The replacement redraws at
	// the same size and grows RIGHTWARD without reflow, so a match whose
	// grown rectangle would reach a same-line neighbour must not be replaced
	// in place: it falls to the redaction gesture instead, where the box is
	// the original's own size.
	if matches := s.search(value, false); len(matches) > 0 {
		if allReplacementsFit(matches, placeholder, value, layout) {
			return pdfLocated{rung: rungLiteral, replaceInPlace: true, occurrences: rectsPerMatch(matches)}
		}
		return pdfLocated{rung: rungTolerant, occurrences: rectsPerMatch(matches)}
	}

	// Rung 2: the whitespace-tolerant pattern, for a seam the converter's
	// repairs collapsed. Redacted, never replaced: the matched text is not
	// the pipeline's string, so redrawing "the rest of the line" cannot be
	// trusted to reproduce it.
	if matches := s.search(pdfTolerantPattern(value), true); len(matches) > 0 {
		return pdfLocated{rung: rungTolerant, occurrences: rectsPerMatch(matches)}
	}

	// Rung 3: the fragment walk over the line model.
	if occ := pdfFragmentWalk(value, layout); len(occ) > 0 {
		return pdfLocated{rung: rungFragment, occurrences: occ}
	}

	// Rung 4: wrapped across two stacked lines.
	if occ := pdfWrappedLocate(value, layout); len(occ) > 0 {
		return pdfLocated{rung: rungWrapped, occurrences: occ}
	}

	return pdfLocated{rung: rungUnlocated}
}

// rectsPerMatch shapes search matches into the one-rect-per-occurrence form.
func rectsPerMatch(matches []asposepdf.TextMatch) [][]asposepdf.Rectangle {
	out := make([][]asposepdf.Rectangle, 0, len(matches))
	for _, m := range matches {
		out = append(out, []asposepdf.Rectangle{m.Rect})
	}
	return out
}

// pdfPlaceholderEmWidth estimates a placeholder's drawn width per rune, in
// ems. Placeholders are brackets, capitals, digits and underscores; 0.62 em
// is a deliberate overestimate of their average width in the metric-compatible
// faces the library redraws in, because the fits-check must err towards the
// redaction gesture: a box that was not strictly needed is cosmetic, an
// overlap is a replacement painted over a neighbour.
const pdfPlaceholderEmWidth = 0.62

// allReplacementsFit is rung 1's gate: for every match, the replacement's
// estimated grown rectangle stays clear of whatever follows on the same line.
func allReplacementsFit(matches []asposepdf.TextMatch, placeholder, value string, layout convert.PDFPageLayout) bool {
	for _, m := range matches {
		if !replacementFits(m, placeholder, layout) {
			return false
		}
	}
	return true
}

// replacementFits measures one match: the placeholder, drawn at the match's
// own size from its left edge, must end before the next thing on the line.
func replacementFits(m asposepdf.TextMatch, placeholder string, layout convert.PDFPageLayout) bool {
	line, frag := fragmentAt(layout, m.Rect)
	if frag == nil {
		// The match cannot be tied back to the model (an annotation match, or
		// a layout drift); without geometry the fit cannot be proven, and an
		// unproven fit is a fail.
		return false
	}
	size := frag.FontSize
	if size <= 0 {
		size = m.Rect.URY - m.Rect.LLY
	}
	grown := m.Rect.LLX + pdfPlaceholderEmWidth*size*float64(utf8.RuneCountInString(placeholder))

	// The nearest obstruction to the right of the match on its own line:
	// either the remainder of the match's own fragment (text that continues
	// immediately after the matched glyphs), or the next fragment's start.
	obstruction := lineObstructionAfter(*line, *frag, m.Rect.URX)
	return grown <= obstruction
}

// fragmentAt finds the model line and fragment a match rectangle sits in, by
// baseline proximity and horizontal overlap.
func fragmentAt(layout convert.PDFPageLayout, r asposepdf.Rectangle) (*convert.PDFLine, *convert.PDFFragment) {
	for li := range layout.Lines {
		line := &layout.Lines[li]
		for fi := range line.Fragments {
			f := &line.Fragments[fi]
			if abs64(f.Y-r.LLY) > maxf(f.Height, 1) {
				continue
			}
			if r.LLX >= f.X-0.5 && r.LLX < f.X+f.Width+0.5 {
				return line, f
			}
		}
	}
	return nil, nil
}

// lineObstructionAfter returns the X at which the next content after rightX
// begins on the line: the match's own fragment continuing past the match, or
// the next fragment. With nothing after, the line owns the space to infinity
// (the gate measured a rightward-growing replacement touching nothing).
func lineObstructionAfter(line convert.PDFLine, frag convert.PDFFragment, rightX float64) float64 {
	// Text continuing inside the same fragment starts immediately after the
	// matched glyphs.
	if frag.X+frag.Width > rightX+0.5 {
		return rightX
	}
	obstruction := 1e18
	for _, f := range line.Fragments {
		if f.X > rightX+0.5 && f.X < obstruction {
			obstruction = f.X
		}
	}
	return obstruction
}

// pdfTolerantPattern derives the RE2 pattern of rung 2: the literal with
// every seam tolerating the repairs the converter applies (an optional space
// between any two characters the repair may have re-joined, and any
// whitespace run where the text has one space).
func pdfTolerantPattern(literal string) string {
	var sb strings.Builder
	for _, r := range literal {
		if r == ' ' {
			sb.WriteString(`\s+`)
			continue
		}
		sb.WriteString(regexp.QuoteMeta(string(r)))
		sb.WriteString(` ?`)
	}
	return strings.TrimSuffix(sb.String(), ` ?`)
}

// pdfNormalize is the comparison form rungs 3 and 4 match in: every
// whitespace removed and ligatures folded, because those are exactly the two
// rewrites (the spacing repair, the ligature fold, the line assembly's
// inserted spaces) that make the pipeline's spelling differ from what the
// page's fragments carry. Case stays significant: a registry original is an
// exact string.
func pdfNormalize(s string) string {
	folded := convert.FoldPDFLigatures(s)
	var b strings.Builder
	b.Grow(len(folded))
	for _, r := range folded {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// lineRuneSpans maps one line's normalized text back to geometry: the
// normalized concatenation of its fragments plus, per normalized rune, the
// owning fragment index and the rune's index within that fragment's own
// normalized text.
type lineRuneSpans struct {
	text  string
	owner []int // fragment index per normalized rune
	local []int // rune index within the owning fragment's normalized text
	count []int // normalized rune count per fragment
}

func buildLineRuneSpans(line convert.PDFLine) lineRuneSpans {
	var s lineRuneSpans
	s.count = make([]int, len(line.Fragments))
	var b strings.Builder
	for fi, f := range line.Fragments {
		norm := pdfNormalize(f.Text)
		k := 0
		for range norm {
			s.owner = append(s.owner, fi)
			s.local = append(s.local, k)
			k++
		}
		s.count[fi] = k
		b.WriteString(norm)
	}
	s.text = b.String()
	return s
}

// pdfFragmentWalk is rung 3: every occurrence of the value across consecutive
// fragments of one line, as the union rectangle of the glyph span it covers.
func pdfFragmentWalk(value string, layout convert.PDFPageLayout) [][]asposepdf.Rectangle {
	needle := pdfNormalize(value)
	if needle == "" {
		return nil
	}
	nRunes := utf8.RuneCountInString(needle)
	var out [][]asposepdf.Rectangle
	for _, line := range layout.Lines {
		spans := buildLineRuneSpans(line)
		runeStarts := runeStartOffsets(spans.text)
		from := 0
		for {
			at := strings.Index(spans.text[from:], needle)
			if at < 0 {
				break
			}
			byteStart := from + at
			r0 := runeIndexOf(runeStarts, byteStart)
			if rect, ok := spanRect(line, spans, r0, r0+nRunes); ok {
				out = append(out, []asposepdf.Rectangle{rect})
			}
			from = byteStart + len(needle)
		}
	}
	return out
}

// runeStartOffsets lists each rune's byte offset in s.
func runeStartOffsets(s string) []int {
	offsets := make([]int, 0, len(s))
	for i := range s {
		offsets = append(offsets, i)
	}
	return offsets
}

// runeIndexOf finds the rune index whose byte offset is byteAt.
func runeIndexOf(offsets []int, byteAt int) int {
	for i, off := range offsets {
		if off == byteAt {
			return i
		}
	}
	return len(offsets)
}

// spanRect unions the glyph cells of the normalized-rune span [r0, r1) into
// one rectangle. A fragment's interior positions are interpolated uniformly
// from its width; where the span starts or ends INSIDE a fragment the edge is
// padded outward by half a glyph, because the redaction removes glyphs whose
// CENTER falls inside the rectangle and an undershot edge would leave the
// value's first or last letter standing.
func spanRect(line convert.PDFLine, spans lineRuneSpans, r0, r1 int) (asposepdf.Rectangle, bool) {
	if r0 < 0 || r1 > len(spans.owner) || r0 >= r1 {
		return asposepdf.Rectangle{}, false
	}
	var rect asposepdf.Rectangle
	found := false
	for k := r0; k < r1; k++ {
		f := line.Fragments[spans.owner[k]]
		n := spans.count[spans.owner[k]]
		glyph := f.Width / float64(n)
		x0 := f.X + float64(spans.local[k])*glyph
		x1 := x0 + glyph
		// Pad the span's outer edges by half a glyph, clamped to the
		// fragment, so an interpolation undershoot cannot leave an edge
		// letter's center outside the box.
		if k == r0 {
			x0 = maxf(f.X, x0-glyph/2)
		}
		if k == r1-1 {
			x1 = minf(f.X+f.Width, x1+glyph/2)
		}
		cell := asposepdf.Rectangle{LLX: x0, LLY: f.Y, URX: x1, URY: f.Y + f.Height}
		if !found {
			rect, found = cell, true
			continue
		}
		rect = unionRects(rect, cell)
	}
	return rect, found
}

// pdfMaxWrapLineHeights bounds rung 4's geometry: the tail line's baseline
// sits below the head's within this many line heights, or the two lines are
// not a wrap (the same bound the gate's wrapped prototype measured with).
const pdfMaxWrapLineHeights = 3.0

// pdfWrappedLocate is rung 4: the value split at a whitespace point, its head
// ending one line and its tail starting the next stacked line. The occurrence
// is two rectangles: the head's (which carries the placeholder overlay) and
// the tail's.
func pdfWrappedLocate(value string, layout convert.PDFPageLayout) [][]asposepdf.Rectangle {
	fields := strings.Fields(value)
	if len(fields) < 2 {
		return nil
	}
	var out [][]asposepdf.Rectangle
	for i := 0; i+1 < len(layout.Lines); i++ {
		head, tail := layout.Lines[i], layout.Lines[i+1]
		if len(head.Fragments) == 0 || len(tail.Fragments) == 0 {
			continue
		}
		drop := head.Fragments[0].Y - tail.Fragments[0].Y
		height := maxf(head.Fragments[0].Height, 1)
		if drop <= 0 || drop > pdfMaxWrapLineHeights*height {
			continue
		}
		headSpans := buildLineRuneSpans(head)
		tailSpans := buildLineRuneSpans(tail)
		for split := 1; split < len(fields); split++ {
			headNorm := pdfNormalize(strings.Join(fields[:split], " "))
			tailNorm := pdfNormalize(strings.Join(fields[split:], " "))
			if headNorm == "" || tailNorm == "" {
				continue
			}
			if !strings.HasSuffix(headSpans.text, headNorm) || !strings.HasPrefix(tailSpans.text, tailNorm) {
				continue
			}
			headRunes := utf8.RuneCountInString(headSpans.text)
			headRect, ok1 := spanRect(head, headSpans, headRunes-utf8.RuneCountInString(headNorm), headRunes)
			tailRect, ok2 := spanRect(tail, tailSpans, 0, utf8.RuneCountInString(tailNorm))
			if ok1 && ok2 {
				out = append(out, []asposepdf.Rectangle{headRect, tailRect})
				break
			}
		}
	}
	return out
}

// unionRects is the smallest rectangle containing both.
func unionRects(a, b asposepdf.Rectangle) asposepdf.Rectangle {
	if b.LLX < a.LLX {
		a.LLX = b.LLX
	}
	if b.LLY < a.LLY {
		a.LLY = b.LLY
	}
	if b.URX > a.URX {
		a.URX = b.URX
	}
	if b.URY > a.URY {
		a.URY = b.URY
	}
	return a
}

func abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

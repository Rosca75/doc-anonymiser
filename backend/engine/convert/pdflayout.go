// engine/convert/pdflayout.go — the PDF extraction line model: fragments
// with rectangles, grouped into lines the pipeline can trust.
//
// A PDF stores positioned glyph runs, not words or lines. The library's
// layout extraction groups runs that share a baseline into one reading-order
// line, and that grouping has two failure shapes this model exists to fix:
//
//  1. Two runs that merely SHARE A BASELINE are not necessarily one line of
//     text. On a landscape slide, a label at the left edge and a label at the
//     right edge sit on one baseline with hundreds of points between them, and
//     reading them as one line manufactures a string that was never contiguous
//     text, which the detector then offers as a value. splitLineOnGaps cuts a
//     line wherever the horizontal gap between two consecutive fragments
//     exceeds a plausible word space.
//
//  2. A sentence WRAPPED across a visual line break is one piece of text in
//     two lines. A value that wraps is then two half-values, and detection
//     never sees it whole. markWrappedJoins joins a continuation line back to
//     its predecessor, but only when the geometry agrees that it IS a
//     continuation: joining on punctuation alone glues headings together and
//     invents names that never existed.
//
// The markdown the pipeline consumes is DERIVED from this model
// (PDFPageLayout.text), so extraction and export can never disagree about
// what a line is: the exporter walks the same lines, with the same
// rectangles, to locate what the pipeline decided to replace.
package convert

import (
	"bytes"
	"strings"

	asposepdf "github.com/aspose-pdf-foss/aspose-pdf-foss-for-go"
)

// PDFFragment is one contiguous glyph run: the unit a PDF actually stores.
// Coordinates are PDF user space (points, origin bottom-left); Y is the
// baseline and Height the ascent-to-descent extent above it, matching what
// the library's own search rectangles use, so a redaction drawn from these
// numbers covers exactly what a search-found redaction would.
type PDFFragment struct {
	Text     string
	X        float64 // left edge, points from the page's left
	Y        float64 // baseline, points from the page's bottom
	Width    float64 // advance width of the whole run
	Height   float64 // text height (ascent minus descent)
	FontSize float64 // effective size in points
}

// PDFLine is one visual line: fragments that share a baseline AND are close
// enough horizontally to be one piece of text (rule 1 above).
//
// JoinsNext marks a line whose text CONTINUES into the next line of the same
// page (rule 2): the derived page text joins the two with a space instead of
// a newline, so a wrapped value reads whole, while the fragments keep their
// own per-line rectangles for the exporter's wrapped-occurrence handling.
type PDFLine struct {
	Fragments []PDFFragment
	JoinsNext bool
}

// PDFPageLayout is one page's line model plus everything derived from it.
type PDFPageLayout struct {
	Lines []PDFLine
}

// pdfSplitGapSpaces is rule 1's threshold, in units of ONE WORD SPACE
// (a quarter of the font size, the proportional-face average). The rule is a
// multiple of the fragment's own space width, never an absolute point value,
// because an absolute rule cannot serve a 12 pt contract and a 28 pt slide
// title at once. Three spaces separates the measured populations with margin:
// real word spacing between same-line runs measures up to about 2.9 spaces at
// 12 pt, while the nearest merely-baseline-sharing pair measures over 14.
const pdfSplitGapSpaces = 3.0

// pdfSpaceWidth is the plausible width of one word space for a fragment pair:
// a quarter of the SMALLER adjacent font size, because a space typed in
// either font justifies the separation. The 1 pt floor keeps degenerate
// (zero-size) fragments from making every gap a split.
func pdfSpaceWidth(a, b PDFFragment) float64 {
	size := a.FontSize
	if b.FontSize > 0 && (size == 0 || b.FontSize < size) {
		size = b.FontSize
	}
	w := size * 0.25
	if w < 1 {
		w = 1
	}
	return w
}

// pdfWordGapSpaces is the threshold, in word spaces, above which two adjacent
// fragments get a SPACE between them in the derived text (below it the gap is
// kerning or tracking and the runs are one word). 0.8 of a space mirrors the
// library's own line assembly rule (0.2 of the font size), so the derived
// text agrees with what the extraction parity gate measured.
const pdfWordGapSpaces = 0.8

// splitLineOnGaps applies rule 1: cut the line between two consecutive
// fragments when the horizontal gap between them exceeds pdfSplitGapSpaces
// word spaces. Fragments are assumed left-to-right, as the library's line
// grouping emits them.
func splitLineOnGaps(frags []PDFFragment) []PDFLine {
	if len(frags) == 0 {
		return nil
	}
	var out []PDFLine
	start := 0
	for i := 1; i < len(frags); i++ {
		prev, cur := frags[i-1], frags[i]
		gap := cur.X - (prev.X + prev.Width)
		if gap > pdfSplitGapSpaces*pdfSpaceWidth(prev, cur) {
			out = append(out, PDFLine{Fragments: frags[start:i:i]})
			start = i
		}
	}
	out = append(out, PDFLine{Fragments: frags[start:]})
	return out
}

// lineText derives one line's text from its fragments: runs joined directly
// when the gap between them is kerning-sized, with one space when it is a
// word space (rule pdfWordGapSpaces). This is THE definition of a line's text;
// nothing else may re-derive it.
func lineText(l PDFLine) string {
	var b strings.Builder
	for i, f := range l.Fragments {
		if i > 0 {
			prev := l.Fragments[i-1]
			gap := f.X - (prev.X + prev.Width)
			if gap > pdfWordGapSpaces*pdfSpaceWidth(prev, f) {
				b.WriteByte(' ')
			}
		}
		b.WriteString(f.Text)
	}
	return b.String()
}

// Geometry gates for rule 2, each expressed against the line's own metrics
// so the rule serves every text size at once:
//
//   - a continuation baseline sits within 1.6 line heights of its predecessor
//     (further apart is a paragraph break, not a wrap; a contract set at 1.5
//     line spacing wraps at just over 1.5 heights, which is why the gate is
//     not exactly 1.5);
//   - the two lines share a left margin within one word space (a wrap returns
//     to the block's left edge; an indent is a new block);
//   - the predecessor reaches within one character of the BLOCK's right edge
//     (a wrap happens because the line was full; a short line ended by
//     choice, and joining short neighbouring lines is how a list of separate
//     rows becomes one manufactured string).
const (
	pdfJoinMaxBaselineGapLineHeights = 1.6
	pdfJoinMaxLeftMarginSpaces       = 1.0
	pdfJoinRightEdgeSlackEms         = 1.0
)

// pdfTerminalPunctuation ends a sentence or a heading; a line ending with one
// is complete and never joined into the next.
const pdfTerminalPunctuation = ".!?:;"

// markWrappedJoins applies rule 2 over one page's split lines, in visual
// order: it sets JoinsNext on a line whose successor the geometry identifies
// as its wrapped continuation. Only the FLAG is set; the fragments stay in
// their own lines so the exporter can still redact each half where it sits.
//
// A whitespace-only line between the two halves is stepped over (and joined
// through), because some writers paint a bare space between a paragraph's
// visual lines: without dropping it the join never fires.
func markWrappedJoins(lines []PDFLine) {
	for i := 0; i+1 < len(lines); i++ {
		next := i + 1
		for next < len(lines) && strings.TrimSpace(lineText(lines[next])) == "" {
			next++
		}
		if next >= len(lines) {
			return
		}
		if joinsAsWrap(lines, i, next) {
			for k := i; k < next; k++ {
				lines[k].JoinsNext = true
			}
		}
	}
}

// joinsAsWrap decides whether lines[next] is the wrapped continuation of
// lines[i]. All four gates must agree; any doubt reads as "two lines",
// because a wrong join manufactures text exactly as rule 1's missing split
// did.
func joinsAsWrap(lines []PDFLine, i, next int) bool {
	prev, cur := lines[i], lines[next]
	if len(prev.Fragments) == 0 || len(cur.Fragments) == 0 {
		return false
	}
	prevText := strings.TrimSpace(lineText(prev))
	if prevText == "" || strings.ContainsRune(pdfTerminalPunctuation, rune(prevText[len(prevText)-1])) {
		return false
	}

	pf, cf := prev.Fragments[0], cur.Fragments[0]
	size := pf.FontSize
	if size <= 0 {
		size = 12
	}
	lineHeight := pf.Height
	if lineHeight <= 0 {
		lineHeight = size
	}

	// Baseline distance: the continuation sits BELOW, within 1.5 line heights.
	drop := pf.Y - cf.Y
	if drop <= 0 || drop > pdfJoinMaxBaselineGapLineHeights*lineHeight {
		return false
	}
	// Shared left margin, within one word space.
	if abs(pf.X-cf.X) > pdfJoinMaxLeftMarginSpaces*pdfSpaceWidth(pf, cf) {
		return false
	}
	// The previous line reaches near its BLOCK's right edge: the block is the
	// run of consecutive lines sharing this left margin, and "near" is two
	// characters. A line that stops short of where its own block shows text
	// can fit ended by choice, so its successor is a new line, not a wrap.
	if lineRight(prev) < blockRightEdge(lines, i)-pdfJoinRightEdgeSlackEms*size {
		return false
	}
	return true
}

// lineRight is a line's right edge: the furthest fragment end.
func lineRight(l PDFLine) float64 {
	right := 0.0
	for _, f := range l.Fragments {
		if end := f.X + f.Width; end > right {
			right = end
		}
	}
	return right
}

// blockRightEdge is the right edge of the paragraph block line i belongs to:
// the maximal run of consecutive lines sharing line i's left margin (within
// one word space). It is what "the line was full" is measured against.
func blockRightEdge(lines []PDFLine, i int) float64 {
	anchor := lines[i].Fragments[0]
	sameMargin := func(l PDFLine) bool {
		if len(l.Fragments) == 0 {
			return false
		}
		f := l.Fragments[0]
		return abs(f.X-anchor.X) <= pdfJoinMaxLeftMarginSpaces*pdfSpaceWidth(f, anchor)
	}
	lo, hi := i, i
	for lo > 0 && sameMargin(lines[lo-1]) {
		lo--
	}
	for hi+1 < len(lines) && sameMargin(lines[hi+1]) {
		hi++
	}
	right := 0.0
	for k := lo; k <= hi; k++ {
		if r := lineRight(lines[k]); r > right {
			right = r
		}
	}
	return right
}

// text derives the page's text from the line model: one line per visual line,
// except that a line marked JoinsNext flows into its successor with a single
// space, so a wrapped value reads whole to the pipeline.
func (p PDFPageLayout) text() string {
	var b strings.Builder
	for i, l := range p.Lines {
		if i > 0 {
			if p.Lines[i-1].JoinsNext {
				b.WriteByte(' ')
			} else {
				b.WriteByte('\n')
			}
		}
		b.WriteString(lineText(l))
	}
	return b.String()
}

// LayoutFromTextLines adapts one page's library extraction into the model:
// every library line is split on rule 1, then the page's split lines are
// scanned for rule 2's wrapped continuations. Exported because the in-place
// PDF export locates replacements against the SAME model the import derived
// its text from, over a document it already holds open.
func LayoutFromTextLines(libLines []asposepdf.TextLine) PDFPageLayout {
	var lines []PDFLine
	for _, ll := range libLines {
		frags := make([]PDFFragment, 0, len(ll.Fragments))
		for _, f := range ll.Fragments {
			if f.Text == "" {
				continue
			}
			frags = append(frags, PDFFragment{
				Text: f.Text, X: f.X, Y: f.Y,
				Width: f.Width, Height: f.Height, FontSize: f.FontSize,
			})
		}
		lines = append(lines, splitLineOnGaps(frags)...)
	}
	markWrappedJoins(lines)
	return PDFPageLayout{Lines: lines}
}

// PDFLayouts opens the raw PDF bytes through the library's bytes-only entry
// point and returns the per-page line model. It is the shared foundation of
// the import extraction (PDFWithPages derives the working markdown from it)
// and the in-place export (which locates every replacement against the same
// lines). Error classification (damaged, encrypted, scanned) is the caller's:
// this function only reads.
func PDFLayouts(raw []byte) (layouts []PDFPageLayout, err error) {
	// The library parses arbitrary bytes; a panic on a malformed file must
	// become an actionable error, never crash the application.
	defer func() {
		if r := recover(); r != nil {
			layouts = nil
			err = pdfDamagedError(r)
		}
	}()
	doc, err := asposepdf.OpenStream(bytes.NewReader(raw))
	if err != nil {
		return nil, classifyPDFOpenError(err)
	}
	for _, page := range doc.Pages() {
		libLines, err := page.ExtractTextWithLayout()
		if err != nil {
			return nil, pdfDamagedError(err)
		}
		layouts = append(layouts, LayoutFromTextLines(libLines))
	}
	return layouts, nil
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

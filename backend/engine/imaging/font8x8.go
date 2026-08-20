// engine/imaging/font8x8.go — the box text's letters, as a vendored bitmap
// table.
//
// The standard library has no font rasteriser and this application takes no new
// Go module for pictures, so the text drawn into a box treatment comes from an
// 8x8 bitmap font vendored as Go source. It is an ASSET, the same way the
// Material Symbols SVGs under frontend/assets/icons are: data with a licence,
// not code with a dependency.
//
// SOURCE AND LICENCE: the "basic" block of Daniel Hepper's font8x8
// (https://github.com/dhepper/font8x8), which is in the PUBLIC DOMAIN. It is
// itself a transcription of the IBM PC 8x8 ROM font. Covers ASCII 32 to 126.
//
// Bit order: each of the eight bytes is one pixel ROW, and bit 0 is the
// LEFTMOST pixel of that row. That is the table's own convention and changing
// it would mirror every glyph.
//
// The SVG box treatment does NOT use this table: an SVG can name a real font
// family, so it does, and the difference in letterforms between the two boxes
// is expected rather than a defect.
package imaging

import "strings"

// glyphWidth and glyphHeight are one cell of the table, in pixels at scale 1.
const (
	glyphWidth  = 8
	glyphHeight = 8
	// glyphAdvance is how far the pen moves per character. It equals the cell
	// width because the table's glyphs leave their rightmost column blank, which
	// is the gap between letters.
	glyphAdvance = glyphWidth
	// lineAdvance is the cell height plus two rows of leading, so a descender
	// (g, j, p, q, y, which use the table's last row) does not touch the line
	// below it.
	lineAdvance = glyphHeight + 2
	// firstGlyph is the rune the table starts at.
	firstGlyph = ' '
	// lastGlyph is the rune the table ends at.
	lastGlyph = '~'
)

// glyphs8x8 holds ASCII 32 to 126, in order. See the file header for the
// source, the licence and the bit order.
var glyphs8x8 = [lastGlyph - firstGlyph + 1][glyphHeight]byte{
	{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // U+0020 (space)
	{0x18, 0x3C, 0x3C, 0x18, 0x18, 0x00, 0x18, 0x00}, // U+0021 (!)
	{0x36, 0x36, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // U+0022 (")
	{0x36, 0x36, 0x7F, 0x36, 0x7F, 0x36, 0x36, 0x00}, // U+0023 (#)
	{0x0C, 0x3E, 0x03, 0x1E, 0x30, 0x1F, 0x0C, 0x00}, // U+0024 ($)
	{0x00, 0x63, 0x33, 0x18, 0x0C, 0x66, 0x63, 0x00}, // U+0025 (%)
	{0x1C, 0x36, 0x1C, 0x6E, 0x3B, 0x33, 0x6E, 0x00}, // U+0026 (&)
	{0x06, 0x06, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00}, // U+0027 (')
	{0x18, 0x0C, 0x06, 0x06, 0x06, 0x0C, 0x18, 0x00}, // U+0028 (()
	{0x06, 0x0C, 0x18, 0x18, 0x18, 0x0C, 0x06, 0x00}, // U+0029 ())
	{0x00, 0x66, 0x3C, 0xFF, 0x3C, 0x66, 0x00, 0x00}, // U+002A (*)
	{0x00, 0x0C, 0x0C, 0x3F, 0x0C, 0x0C, 0x00, 0x00}, // U+002B (+)
	{0x00, 0x00, 0x00, 0x00, 0x00, 0x0C, 0x0C, 0x06}, // U+002C (,)
	{0x00, 0x00, 0x00, 0x3F, 0x00, 0x00, 0x00, 0x00}, // U+002D (-)
	{0x00, 0x00, 0x00, 0x00, 0x00, 0x0C, 0x0C, 0x00}, // U+002E (.)
	{0x60, 0x30, 0x18, 0x0C, 0x06, 0x03, 0x01, 0x00}, // U+002F (/)
	{0x3E, 0x63, 0x73, 0x7B, 0x6F, 0x67, 0x3E, 0x00}, // U+0030 (0)
	{0x0C, 0x0E, 0x0C, 0x0C, 0x0C, 0x0C, 0x3F, 0x00}, // U+0031 (1)
	{0x1E, 0x33, 0x30, 0x1C, 0x06, 0x33, 0x3F, 0x00}, // U+0032 (2)
	{0x1E, 0x33, 0x30, 0x1C, 0x30, 0x33, 0x1E, 0x00}, // U+0033 (3)
	{0x38, 0x3C, 0x36, 0x33, 0x7F, 0x30, 0x78, 0x00}, // U+0034 (4)
	{0x3F, 0x03, 0x1F, 0x30, 0x30, 0x33, 0x1E, 0x00}, // U+0035 (5)
	{0x1C, 0x06, 0x03, 0x1F, 0x33, 0x33, 0x1E, 0x00}, // U+0036 (6)
	{0x3F, 0x33, 0x30, 0x18, 0x0C, 0x0C, 0x0C, 0x00}, // U+0037 (7)
	{0x1E, 0x33, 0x33, 0x1E, 0x33, 0x33, 0x1E, 0x00}, // U+0038 (8)
	{0x1E, 0x33, 0x33, 0x3E, 0x30, 0x18, 0x0E, 0x00}, // U+0039 (9)
	{0x00, 0x0C, 0x0C, 0x00, 0x00, 0x0C, 0x0C, 0x00}, // U+003A (:)
	{0x00, 0x0C, 0x0C, 0x00, 0x00, 0x0C, 0x0C, 0x06}, // U+003B (;)
	{0x18, 0x0C, 0x06, 0x03, 0x06, 0x0C, 0x18, 0x00}, // U+003C (<)
	{0x00, 0x00, 0x3F, 0x00, 0x00, 0x3F, 0x00, 0x00}, // U+003D (=)
	{0x06, 0x0C, 0x18, 0x30, 0x18, 0x0C, 0x06, 0x00}, // U+003E (>)
	{0x1E, 0x33, 0x30, 0x18, 0x0C, 0x00, 0x0C, 0x00}, // U+003F (?)
	{0x3E, 0x63, 0x7B, 0x7B, 0x7B, 0x03, 0x1E, 0x00}, // U+0040 (@)
	{0x0C, 0x1E, 0x33, 0x33, 0x3F, 0x33, 0x33, 0x00}, // U+0041 (A)
	{0x3F, 0x66, 0x66, 0x3E, 0x66, 0x66, 0x3F, 0x00}, // U+0042 (B)
	{0x3C, 0x66, 0x03, 0x03, 0x03, 0x66, 0x3C, 0x00}, // U+0043 (C)
	{0x1F, 0x36, 0x66, 0x66, 0x66, 0x36, 0x1F, 0x00}, // U+0044 (D)
	{0x7F, 0x46, 0x16, 0x1E, 0x16, 0x46, 0x7F, 0x00}, // U+0045 (E)
	{0x7F, 0x46, 0x16, 0x1E, 0x16, 0x06, 0x0F, 0x00}, // U+0046 (F)
	{0x3C, 0x66, 0x03, 0x03, 0x73, 0x66, 0x7C, 0x00}, // U+0047 (G)
	{0x33, 0x33, 0x33, 0x3F, 0x33, 0x33, 0x33, 0x00}, // U+0048 (H)
	{0x1E, 0x0C, 0x0C, 0x0C, 0x0C, 0x0C, 0x1E, 0x00}, // U+0049 (I)
	{0x78, 0x30, 0x30, 0x30, 0x33, 0x33, 0x1E, 0x00}, // U+004A (J)
	{0x67, 0x66, 0x36, 0x1E, 0x36, 0x66, 0x67, 0x00}, // U+004B (K)
	{0x0F, 0x06, 0x06, 0x06, 0x46, 0x66, 0x7F, 0x00}, // U+004C (L)
	{0x63, 0x77, 0x7F, 0x7F, 0x6B, 0x63, 0x63, 0x00}, // U+004D (M)
	{0x63, 0x67, 0x6F, 0x7B, 0x73, 0x63, 0x63, 0x00}, // U+004E (N)
	{0x1C, 0x36, 0x63, 0x63, 0x63, 0x36, 0x1C, 0x00}, // U+004F (O)
	{0x3F, 0x66, 0x66, 0x3E, 0x06, 0x06, 0x0F, 0x00}, // U+0050 (P)
	{0x1E, 0x33, 0x33, 0x33, 0x3B, 0x1E, 0x38, 0x00}, // U+0051 (Q)
	{0x3F, 0x66, 0x66, 0x3E, 0x36, 0x66, 0x67, 0x00}, // U+0052 (R)
	{0x1E, 0x33, 0x07, 0x0E, 0x38, 0x33, 0x1E, 0x00}, // U+0053 (S)
	{0x3F, 0x2D, 0x0C, 0x0C, 0x0C, 0x0C, 0x1E, 0x00}, // U+0054 (T)
	{0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x3F, 0x00}, // U+0055 (U)
	{0x33, 0x33, 0x33, 0x33, 0x33, 0x1E, 0x0C, 0x00}, // U+0056 (V)
	{0x63, 0x63, 0x63, 0x6B, 0x7F, 0x77, 0x63, 0x00}, // U+0057 (W)
	{0x63, 0x63, 0x36, 0x1C, 0x1C, 0x36, 0x63, 0x00}, // U+0058 (X)
	{0x33, 0x33, 0x33, 0x1E, 0x0C, 0x0C, 0x1E, 0x00}, // U+0059 (Y)
	{0x7F, 0x63, 0x31, 0x18, 0x4C, 0x66, 0x7F, 0x00}, // U+005A (Z)
	{0x1E, 0x06, 0x06, 0x06, 0x06, 0x06, 0x1E, 0x00}, // U+005B ([)
	{0x03, 0x06, 0x0C, 0x18, 0x30, 0x60, 0x40, 0x00}, // U+005C (\)
	{0x1E, 0x18, 0x18, 0x18, 0x18, 0x18, 0x1E, 0x00}, // U+005D (])
	{0x08, 0x1C, 0x36, 0x63, 0x00, 0x00, 0x00, 0x00}, // U+005E (^)
	{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xFF}, // U+005F (_)
	{0x0C, 0x0C, 0x18, 0x00, 0x00, 0x00, 0x00, 0x00}, // U+0060 (`)
	{0x00, 0x00, 0x1E, 0x30, 0x3E, 0x33, 0x6E, 0x00}, // U+0061 (a)
	{0x07, 0x06, 0x06, 0x3E, 0x66, 0x66, 0x3B, 0x00}, // U+0062 (b)
	{0x00, 0x00, 0x1E, 0x33, 0x03, 0x33, 0x1E, 0x00}, // U+0063 (c)
	{0x38, 0x30, 0x30, 0x3E, 0x33, 0x33, 0x6E, 0x00}, // U+0064 (d)
	{0x00, 0x00, 0x1E, 0x33, 0x3F, 0x03, 0x1E, 0x00}, // U+0065 (e)
	{0x1C, 0x36, 0x06, 0x0F, 0x06, 0x06, 0x0F, 0x00}, // U+0066 (f)
	{0x00, 0x00, 0x6E, 0x33, 0x33, 0x3E, 0x30, 0x1F}, // U+0067 (g)
	{0x07, 0x06, 0x36, 0x6E, 0x66, 0x66, 0x67, 0x00}, // U+0068 (h)
	{0x0C, 0x00, 0x0E, 0x0C, 0x0C, 0x0C, 0x1E, 0x00}, // U+0069 (i)
	{0x30, 0x00, 0x30, 0x30, 0x30, 0x33, 0x33, 0x1E}, // U+006A (j)
	{0x07, 0x06, 0x66, 0x36, 0x1E, 0x36, 0x67, 0x00}, // U+006B (k)
	{0x0E, 0x0C, 0x0C, 0x0C, 0x0C, 0x0C, 0x1E, 0x00}, // U+006C (l)
	{0x00, 0x00, 0x33, 0x7F, 0x7F, 0x6B, 0x63, 0x00}, // U+006D (m)
	{0x00, 0x00, 0x1F, 0x33, 0x33, 0x33, 0x33, 0x00}, // U+006E (n)
	{0x00, 0x00, 0x1E, 0x33, 0x33, 0x33, 0x1E, 0x00}, // U+006F (o)
	{0x00, 0x00, 0x3B, 0x66, 0x66, 0x3E, 0x06, 0x0F}, // U+0070 (p)
	{0x00, 0x00, 0x6E, 0x33, 0x33, 0x3E, 0x30, 0x78}, // U+0071 (q)
	{0x00, 0x00, 0x3B, 0x6E, 0x66, 0x06, 0x0F, 0x00}, // U+0072 (r)
	{0x00, 0x00, 0x3E, 0x03, 0x1E, 0x30, 0x1F, 0x00}, // U+0073 (s)
	{0x08, 0x0C, 0x3E, 0x0C, 0x0C, 0x2C, 0x18, 0x00}, // U+0074 (t)
	{0x00, 0x00, 0x33, 0x33, 0x33, 0x33, 0x6E, 0x00}, // U+0075 (u)
	{0x00, 0x00, 0x33, 0x33, 0x33, 0x1E, 0x0C, 0x00}, // U+0076 (v)
	{0x00, 0x00, 0x63, 0x6B, 0x7F, 0x7F, 0x36, 0x00}, // U+0077 (w)
	{0x00, 0x00, 0x63, 0x36, 0x1C, 0x36, 0x63, 0x00}, // U+0078 (x)
	{0x00, 0x00, 0x33, 0x33, 0x33, 0x3E, 0x30, 0x1F}, // U+0079 (y)
	{0x00, 0x00, 0x3F, 0x19, 0x0C, 0x26, 0x3F, 0x00}, // U+007A (z)
	{0x38, 0x0C, 0x0C, 0x07, 0x0C, 0x0C, 0x38, 0x00}, // U+007B ({)
	{0x18, 0x18, 0x18, 0x00, 0x18, 0x18, 0x18, 0x00}, // U+007C (|)
	{0x07, 0x0C, 0x0C, 0x38, 0x0C, 0x0C, 0x07, 0x00}, // U+007D (})
	{0x6E, 0x3B, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // U+007E (~)
}

// asciiFolds maps the non-ASCII characters a French, German or Luxembourgish
// document actually contains to the nearest form the table can draw.
//
// Folding rather than dropping, because a name with its accents removed is
// still the name the reviewer typed, where a name with its accented letters
// missing is a different word.
//
// The keys are the characters themselves, because a table of accents written as
// escapes is a table nobody can check; the two characters with no visible shape
// of their own (the non-breaking space and the non-breaking hyphen) are escapes,
// because an invisible key is one a later edit deletes by accident.
var asciiFolds = map[rune]string{
	// French, German and Luxembourgish letters.
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a",
	'À': "A", 'Á': "A", 'Â': "A", 'Ã': "A", 'Ä': "A", 'Å': "A",
	'æ': "ae", 'Æ': "AE",
	'ç': "c", 'Ç': "C",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e",
	'È': "E", 'É': "E", 'Ê': "E", 'Ë': "E",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i",
	'Ì': "I", 'Í': "I", 'Î': "I", 'Ï': "I",
	'ñ': "n", 'Ñ': "N",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ø': "o",
	'Ò': "O", 'Ó': "O", 'Ô': "O", 'Õ': "O", 'Ö': "O", 'Ø': "O",
	'œ': "oe", 'Œ': "OE",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u",
	'Ù': "U", 'Ú': "U", 'Û': "U", 'Ü': "U",
	'ý': "y", 'ÿ': "y", 'Ý': "Y",
	// The sharp s is two letters, not one: "Straße" reads as "Strasse" and not
	// as "Strae".
	'ß': "ss",
	// Typographic punctuation a word processor inserts on the user's behalf.
	'‘': "'", '’': "'", '‚': "'", '‛': "'",
	'“': `"`, '”': `"`, '„': `"`, '‟': `"`,
	'‐': "-", '\u2011': "-", '‒': "-", '–': "-", '—': "-", '―': "-",
	'…':      "...",
	'\u00A0': " ",
	'«':      `"`, '»': `"`,
	'·': ".", '•': "*",
	'€': "EUR", '£': "GBP", '©': "(c)", '®': "(R)", '°': "deg",
}

// foldToDrawable turns arbitrary text into characters this table can draw.
//
// Anything the table has no glyph for and the fold list does not name becomes
// "?", so the box shows that something was there rather than silently closing
// the gap. Tabs and newlines become spaces, because the layout below owns
// line breaking and a literal newline in the middle of a wrapped line would
// fight it.
func foldToDrawable(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteByte(' ')
		case r >= firstGlyph && r <= lastGlyph:
			b.WriteRune(r)
		default:
			if folded, ok := asciiFolds[r]; ok {
				b.WriteString(folded)
				continue
			}
			b.WriteByte('?')
		}
	}
	return b.String()
}

// glyphFor returns one drawable character's bitmap. It is only ever called with
// what foldToDrawable produced, so an out-of-range rune is a defect rather than
// a user's input, and it draws as a space rather than panicking inside an
// export.
func glyphFor(r rune) [glyphHeight]byte {
	if r < firstGlyph || r > lastGlyph {
		return glyphs8x8[0]
	}
	return glyphs8x8[r-firstGlyph]
}

// textLayout is the answer to "how does this text fit in this box": the scale
// factor to draw at, and the lines to draw.
type textLayout struct {
	// Scale is the integer pixel size of one bitmap pixel, 1 or more. Zero
	// means the box is too small for even one character, and nothing is drawn.
	Scale int
	Lines []string
}

// layoutBoxText wraps text into the largest integer scale factor at which it
// fits the box with a one-glyph margin on every side.
//
// Integer scaling only, because the source is a bitmap: a fractional scale means
// resampling one-pixel strokes, and the result is a grey blur where the letters
// were.
//
// The search runs TWICE, and the second pass is what keeps captions readable.
// The margin grows with the scale, so the usable grid narrows as the letters get
// bigger, and the largest scale that technically "fits" can be one that snaps
// "Logo" into "Lo" and "go". So the first pass only accepts a scale wide enough
// to hold the longest WORD whole, and the second pass, reached only when no scale
// can do that (a forty-character reference code in a small box), breaks inside
// words. Below that, the last line is truncated with three ASCII periods rather
// than the ellipsis character, which the table has no glyph for: a box that
// silently drops half the caption lies about what it says.
//
// @param w, h the box in pixels
// @param text the text to draw, already folded to drawable characters
// @return the scale and the wrapped lines; Scale 0 when nothing fits
func layoutBoxText(w, h int, text string) textLayout {
	if text == "" || w <= 0 || h <= 0 {
		return textLayout{}
	}
	// The largest scale at which one character plus its margins still fits.
	// Derived rather than picked, so a big box gets big text and a small one is
	// not handed a scale it can never satisfy.
	maxScale := w / (glyphAdvance * 3)
	if byHeight := h / (lineAdvance * 3); byHeight < maxScale {
		maxScale = byHeight
	}
	if maxScale < 1 {
		maxScale = 1
	}

	longest := longestWordLen(text)
	for scale := maxScale; scale >= 1; scale-- {
		cols, rows := boxGrid(w, h, scale)
		if cols < longest || rows < 1 {
			continue
		}
		if lines := wrapText(text, cols); len(lines) <= rows {
			return textLayout{Scale: scale, Lines: lines}
		}
	}
	for scale := maxScale; scale >= 1; scale-- {
		cols, rows := boxGrid(w, h, scale)
		if cols < 1 || rows < 1 {
			continue
		}
		lines := wrapText(text, cols)
		if len(lines) <= rows {
			return textLayout{Scale: scale, Lines: lines}
		}
		if scale == 1 {
			return textLayout{Scale: 1, Lines: truncateLines(lines, rows, cols)}
		}
	}
	// A box smaller than about 24 by 30 pixels holds no legible character at
	// all, so it gets the plain rectangle. That is the honest degradation: the
	// picture is still replaced, it just carries no caption.
	return textLayout{}
}

// boxGrid is how many characters and how many lines a box holds at one scale,
// inside its one-glyph margin.
func boxGrid(w, h, scale int) (cols, rows int) {
	margin := glyphAdvance * scale
	return (w - 2*margin) / (glyphAdvance * scale), (h - 2*margin) / (lineAdvance * scale)
}

// longestWordLen is the width, in characters, of the longest unbreakable run.
func longestWordLen(text string) int {
	longest := 0
	for _, word := range strings.Fields(text) {
		if n := len([]rune(word)); n > longest {
			longest = n
		}
	}
	return longest
}

// wrapText breaks text into lines of at most cols characters, on spaces where it
// can and inside a word where it must (a 40-character reference code has no
// space to break at, and pushing it past the edge would draw it over the border).
func wrapText(text string, cols int) []string {
	if cols < 1 {
		return nil
	}
	var lines []string
	for _, word := range strings.Fields(text) {
		for len([]rune(word)) > cols {
			runes := []rune(word)
			lines = append(lines, string(runes[:cols]))
			word = string(runes[cols:])
		}
		if len(lines) == 0 {
			lines = append(lines, word)
			continue
		}
		last := lines[len(lines)-1]
		if last == "" {
			lines[len(lines)-1] = word
			continue
		}
		if len([]rune(last))+1+len([]rune(word)) <= cols {
			lines[len(lines)-1] = last + " " + word
			continue
		}
		lines = append(lines, word)
	}
	if len(lines) == 0 {
		// Text made only of whitespace wraps to nothing, which is the same as
		// no text at all.
		return nil
	}
	return lines
}

// truncateLines keeps the lines that fit and marks the cut with three dots, so
// the box shows that it is not showing everything.
func truncateLines(lines []string, rows, cols int) []string {
	if rows < 1 || len(lines) <= rows {
		return lines
	}
	kept := append([]string(nil), lines[:rows]...)
	last := []rune(kept[rows-1])
	const mark = "..."
	if len(last)+len(mark) <= cols {
		kept[rows-1] = string(last) + mark
		return kept
	}
	if len(last) >= len(mark) {
		kept[rows-1] = string(last[:len(last)-len(mark)]) + mark
	}
	return kept
}

// engine/imaging/treat.go — the replacement bytes for one picture under one
// decision.
//
// ONE entry point, Treat, so nothing can apply a treatment through a side door:
// the export, the preview and every test go through it, which is what makes the
// preview a promise the export keeps rather than a separate drawing of the same
// idea.
//
// Two rules shape everything here:
//
//  1. The replacement comes back in the SOURCE's own encoding. The archive's
//     [Content_Types].xml maps a part's extension to a MIME type, so PNG bytes
//     written into image2.jpeg are a file Word may refuse to draw. The encoding
//     is read from the BYTES handed in, not from the asset's reported format,
//     because an SVG asset is two parts (a PNG fallback and the SVG itself) and
//     both have to be treated in their own encoding.
//  2. The original samples are destroyed, never hidden. That is why blur is
//     mosaic-then-smooth and not a Gaussian: a Gaussian is partly invertible and
//     a light one over text is simply readable, where a block mean throws the
//     samples away and nothing can bring them back.
package imaging

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"strings"
)

// The box palette. Grey on grey on purpose: a replacement box has to read as a
// deliberate redaction rather than as a broken image or as part of the design.
var (
	boxFill   = color.RGBA{R: 0xE5, G: 0xE7, B: 0xEB, A: 0xFF}
	boxStroke = color.RGBA{R: 0x9C, G: 0xA3, B: 0xAF, A: 0xFF}
	boxInk    = color.RGBA{R: 0x2D, G: 0x2D, B: 0x2D, A: 0xFF}
)

const (
	// boxFallbackW and boxFallbackH are the last resort for a picture whose
	// pixel size nothing states: not its own header, not the scan, and not the
	// frame it is drawn in. A shape is needed to draw a rectangle at all, and
	// the OOXML frame rescales whatever it is given.
	boxFallbackW = 640
	boxFallbackH = 480
	// emuPerInch and screenDPI convert an OOXML display frame to pixels. Used
	// ONLY as a fallback size, because a frame says how large the picture is
	// DRAWN, which is a different question from how many pixels it holds.
	emuPerInch = 914400
	screenDPI  = 96
	// jpegQuality is high enough that the box's border and the mosaic's block
	// edges do not acquire ringing of their own.
	jpegQuality = 90
	// MaxTreatPixels is the largest picture this package will decode in order to
	// change it, and it matches the thumbnailer's limit for the same reason: a
	// 40-megapixel RGBA buffer is about 160 MB, and an application that swaps
	// while saving a file is one the user cannot trust.
	MaxTreatPixels = MaxThumbnailSourcePixels
)

// Treat produces the REPLACEMENT bytes for one asset under one decision.
//
// It returns nil bytes for TreatmentKeep: there is nothing to write, and a
// caller that writes nil writes the original through untouched.
//
// @param src the picture part's own bytes, as the archive holds them
// @param a the asset the decision names, as the scan listed it
// @param d the decision
// @return the replacement bytes, or nil for keep, and an error naming the fix
func Treat(src []byte, a Asset, d Decision) ([]byte, error) {
	if !d.Anonymises() {
		return nil, nil
	}
	// Validated here as well as at the bound method, because this is the last
	// gate before bytes are written: a decision that reached an export without
	// passing the interface (a restored session, a test, a future caller) must
	// still be refused rather than silently drawn wrong.
	if err := d.Validate(a); err != nil {
		return nil, err
	}

	format := Sniff(src)
	switch d.Treatment {
	case TreatmentRemove:
		return blankBytes(format), nil
	case TreatmentBox:
		return boxBytes(src, a, format, d.BoxText)
	case TreatmentBlur:
		return blurBytes(src, format, d.BlurStrength)
	default:
		// Validate has already refused every other value; this exists so the
		// function has one exit per branch rather than a fallthrough nobody can
		// see the consequence of.
		return nil, fmt.Errorf("unknown image treatment %q, expected one of: keep, box, blur, remove",
			d.Treatment)
	}
}

// --- remove ---------------------------------------------------------------

// blankBytes is what replaces a removed picture's bytes.
//
// The bytes are overwritten even though the export also deletes the picture
// ELEMENT, because an orphan picture part left inside the zip is a leak that
// looks like a redaction. A shape's fill and a slide background have no element
// to delete, and for them these bytes ARE the whole removal: a fully
// transparent pixel draws nothing.
//
// A JPEG source is answered with a PNG, because JPEG has no transparency at all
// and a white square is not "removed". The part's declared content type then
// disagrees with its bytes, which costs nothing where the element is gone and
// shows as an undrawn picture where it is a fill, which is what remove means.
func blankBytes(format Format) []byte {
	if format == FormatSVG {
		return []byte(`<?xml version="1.0" encoding="UTF-8" standalone="no"?>` +
			`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1" viewBox="0 0 1 1"/>`)
	}
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1)) // zero value is transparent
	// A 1x1 PNG cannot fail to encode, so the error is impossible here; it is
	// still not discarded silently, because a caller reading nil bytes as
	// "keep" would ship the original.
	if err := png.Encode(&buf, img); err != nil {
		return []byte{}
	}
	return buf.Bytes()
}

// --- box ------------------------------------------------------------------

// boxBytes draws the replacement rectangle at the source's own pixel size.
func boxBytes(src []byte, a Asset, format Format, text string) ([]byte, error) {
	w, h := boxSize(src, a, format)
	if format == FormatSVG {
		return svgBox(w, h, text), nil
	}
	return encodeRaster(drawBox(w, h, text), format)
}

// boxSize decides how large the replacement is, in pixels, best answer first.
//
// The chain matters because each step answers a weaker question than the one
// before: the part's own header is the truth; the scan's recorded size is the
// same truth read earlier; the display frame says only how large the picture is
// DRAWN; and the fallback says nothing at all. A legacy VML picture states its
// size in a CSS style attribute the scanner does not read, so it reaches the
// last step, and the frame it is drawn in rescales whatever it is given.
//
// Above MaxTreatPixels the replacement is scaled DOWN rather than refused: the
// OOXML frame decides the drawn size, so a smaller rectangle fills exactly the
// same space, and refusing would mean a picture the user asked to redact stays
// in the file.
func boxSize(src []byte, a Asset, format Format) (w, h int) {
	w, h = sizeFromSource(src, a, format)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if w*h > MaxTreatPixels {
		w, h = ThumbnailSize(w, h, maxSideFor(w, h))
	}
	return w, h
}

// sizeFromSource walks the chain boxSize documents, best answer first.
func sizeFromSource(src []byte, a Asset, format Format) (w, h int) {
	if mw, mh, ok := Measure(src, format); ok && mw > 0 && mh > 0 {
		return mw, mh
	}
	if a.Width > 0 && a.Height > 0 {
		return a.Width, a.Height
	}
	for _, occ := range a.Occurrences {
		if occ.DisplayCX > 0 && occ.DisplayCY > 0 {
			return emuToPixels(occ.DisplayCX), emuToPixels(occ.DisplayCY)
		}
	}
	return boxFallbackW, boxFallbackH
}

// maxSideFor is the longest side that keeps a picture of this shape under
// MaxTreatPixels, so the scaled-down box still has the source's aspect ratio.
func maxSideFor(w, h int) int {
	longest, shortest := w, h
	if h > w {
		longest, shortest = h, w
	}
	if shortest <= 0 {
		return 1
	}
	// side * (side * shortest/longest) <= max  =>  side <= sqrt(max * longest/shortest)
	side := int(math.Sqrt(float64(MaxTreatPixels) * float64(longest) / float64(shortest)))
	if side < 1 {
		side = 1
	}
	return side
}

// emuToPixels converts an OOXML display measurement to pixels at 96 dpi.
func emuToPixels(emu int) int {
	px := emu * screenDPI / emuPerInch
	if px < 1 {
		return 1
	}
	return px
}

// drawBox paints the rectangle, its border and the wrapped text.
func drawBox(w, h int, text string) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fillRect(img, 0, 0, w, h, boxFill)
	// A one-pixel border, so the replacement reads as a placed rectangle rather
	// than as a gap in the page.
	fillRect(img, 0, 0, w, 1, boxStroke)
	fillRect(img, 0, h-1, w, 1, boxStroke)
	fillRect(img, 0, 0, 1, h, boxStroke)
	fillRect(img, w-1, 0, 1, h, boxStroke)

	layout := layoutBoxText(w, h, foldToDrawable(text))
	if layout.Scale == 0 || len(layout.Lines) == 0 {
		return img
	}
	// The block's own height drops the trailing leading, so the text sits
	// optically centred rather than pushed one line-gap high.
	blockH := len(layout.Lines)*lineAdvance*layout.Scale - 2*layout.Scale
	top := (h - blockH) / 2
	for i, line := range layout.Lines {
		runes := []rune(line)
		lineW := len(runes) * glyphAdvance * layout.Scale
		x := (w - lineW) / 2
		y := top + i*lineAdvance*layout.Scale
		for _, r := range runes {
			drawGlyph(img, x, y, layout.Scale, glyphFor(r))
			x += glyphAdvance * layout.Scale
		}
	}
	return img
}

// fillRect paints an axis-aligned rectangle, clipped to the image.
func fillRect(img *image.RGBA, x, y, w, h int, c color.RGBA) {
	for py := y; py < y+h; py++ {
		for px := x; px < x+w; px++ {
			if !(image.Point{X: px, Y: py}).In(img.Rect) {
				continue
			}
			img.SetRGBA(px, py, c)
		}
	}
}

// drawGlyph paints one bitmap cell, each source pixel as a scale x scale block.
func drawGlyph(img *image.RGBA, x, y, scale int, glyph [glyphHeight]byte) {
	for row := 0; row < glyphHeight; row++ {
		bits := glyph[row]
		if bits == 0 {
			continue
		}
		for col := 0; col < glyphWidth; col++ {
			// Bit 0 is the LEFTMOST pixel; see the font file's header.
			if bits&(1<<uint(col)) == 0 {
				continue
			}
			fillRect(img, x+col*scale, y+row*scale, scale, scale, boxInk)
		}
	}
}

// svgBox emits a new, minimal SVG of the same size carrying the same message.
//
// The vector box is the one place real letterforms are available, so it uses
// them: the application's own font stack, and the text as the user typed it,
// accents included. The raster box cannot, and that difference between the two
// is expected rather than a defect.
func svgBox(w, h int, text string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="no"?>`)
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`, w, h, w, h)
	fmt.Fprintf(&b,
		`<rect x="0.5" y="0.5" width="%s" height="%s" fill="#E5E7EB" stroke="#9CA3AF" stroke-width="1"/>`,
		svgLen(w-1), svgLen(h-1))

	size, lines := svgTextLayout(w, h, text)
	if size > 0 && len(lines) > 0 {
		// dominant-baseline centres the block vertically without needing the
		// font's metrics, which nothing here can read.
		fmt.Fprintf(&b, `<text x="%d" y="%d" font-family="Helvetica, Arial, sans-serif" `+
			`font-size="%d" fill="#2D2D2D" text-anchor="middle" dominant-baseline="middle">`,
			w/2, h/2, size)
		// The first line's offset lifts the block so its MIDDLE sits on the
		// centre line, whatever the line count.
		first := -float64(len(lines)-1) * svgLineFactor / 2
		for i, line := range lines {
			dy := "0"
			if i == 0 && len(lines) > 1 {
				dy = fmt.Sprintf("%.2fem", first)
			} else if i > 0 {
				dy = fmt.Sprintf("%.2fem", svgLineFactor)
			}
			fmt.Fprintf(&b, `<tspan x="%d" dy="%s">%s</tspan>`, w/2, dy, escapeXML(line))
		}
		b.WriteString(`</text>`)
	}
	b.WriteString(`</svg>`)
	return []byte(b.String())
}

// The SVG text metrics this package assumes, because it cannot read a font.
const (
	// svgAdvanceFactor is the average advance of a Helvetica character as a
	// fraction of the font size. Deliberately generous: over-estimating wraps a
	// line early, and under-estimating writes over the border.
	svgAdvanceFactor = 0.62
	// svgLineFactor is the line height as a multiple of the font size.
	svgLineFactor = 1.25
)

// svgTextLayout picks the largest whole font size at which the wrapped text
// fits inside the box's margin, and the lines to draw.
//
// It walks sizes downwards rather than solving for one, because the wrap depends
// on the size and the size depends on the wrap.
func svgTextLayout(w, h int, text string) (size int, lines []string) {
	trimmed := strings.Join(strings.Fields(text), " ")
	if trimmed == "" || w <= 0 || h <= 0 {
		return 0, nil
	}
	margin := min(w, h) / 20
	if margin < 2 {
		margin = 2
	}
	availW := float64(w - 2*margin)
	availH := float64(h - 2*margin)
	if availW <= 0 || availH <= 0 {
		return 0, nil
	}

	maxSize := min(w, h) / 3
	if maxSize < 1 {
		maxSize = 1
	}
	for candidate := maxSize; candidate >= 1; candidate-- {
		cols := int(availW / (float64(candidate) * svgAdvanceFactor))
		if cols < 1 {
			continue
		}
		wrapped := wrapText(trimmed, cols)
		if len(wrapped) == 0 {
			return 0, nil
		}
		if float64(len(wrapped))*float64(candidate)*svgLineFactor <= availH {
			return candidate, wrapped
		}
	}
	// A box too small for one legible character carries no caption, exactly as
	// the raster box does.
	return 0, nil
}

// svgLen formats a non-negative length, because a 1px-wide box would otherwise
// ask for a negative rect.
func svgLen(v int) string {
	if v < 0 {
		v = 0
	}
	return fmt.Sprintf("%d", v)
}

// escapeXML makes one line safe inside an SVG text node. It goes through
// encoding/xml rather than a hand-written replacer, so the escaping is the same
// one every other part of this application relies on.
func escapeXML(s string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		// EscapeText only fails when the writer does, and a bytes.Buffer does
		// not. Dropping the text is still safer than emitting it unescaped.
		return ""
	}
	return buf.String()
}

// --- blur -----------------------------------------------------------------

// blurBytes destroys a raster picture's detail at the requested strength.
func blurBytes(src []byte, format Format, strength int) ([]byte, error) {
	if format != FormatPNG && format != FormatJPEG {
		return nil, fmt.Errorf("a blur can only be applied to a PNG or JPEG picture, and this "+
			"one is a %s file; replace it with a box, or remove it", format)
	}
	w, h, ok := Measure(src, format)
	if !ok || w <= 0 || h <= 0 {
		return nil, fmt.Errorf("this picture's header does not decode as %s, so it cannot be "+
			"blurred; remove it instead, which needs no decoding", format)
	}
	if w*h > MaxTreatPixels {
		return nil, fmt.Errorf("this picture is %dx%d pixels (%d megapixels), above the %d "+
			"megapixel limit for blurring; replace it with a box or remove it, neither of which "+
			"has to decode it",
			w, h, w*h/1_000_000, MaxTreatPixels/1_000_000)
	}

	decoded, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("this picture could not be decoded (%v), so it cannot be blurred; "+
			"replace it with a box or remove it", err)
	}
	img := toRGBA(decoded)
	mosaic(img, mosaicFactor(w, h, strength))
	return encodeRaster(smooth3x3(img), format)
}

// mosaicFactor is the block size a blur uses, in pixels.
//
// It is RELATIVE to the picture's own smaller side, because a fixed pixel radius
// does nothing to a 4000-pixel screenshot and obliterates a 60-pixel icon. At
// strength 5 a 600-pixel photo gets 15-pixel blocks and a 4000-pixel screenshot
// gets 100-pixel ones, which is the scale invariance the relative rule buys.
//
// @param w, h the picture in pixels
// @param strength 1 to 10; 0 means "not stated" and reads as the default
// @return the block side, never below 2 (a 1-pixel block destroys nothing) and
//
//	never above the picture's smaller side
func mosaicFactor(w, h, strength int) int {
	if strength <= 0 {
		strength = DefaultBlurStrength
	}
	if strength > MaxBlurStrength {
		strength = MaxBlurStrength
	}
	smaller := min(w, h)
	if smaller < 1 {
		return 1
	}
	f := int(math.Round(float64(smaller) * float64(strength) / 200.0))
	if f < 2 {
		f = 2
	}
	if f > smaller {
		f = smaller
	}
	return f
}

// mosaic replaces every f x f block with the block's own mean, IN PLACE.
//
// This is the step that destroys the information rather than moving it around:
// after it, a block holds one value where it held f*f of them, so the picture
// carries at most ceil(w/f) * ceil(h/f) distinct values whatever it started
// with. Nothing downstream can recover what was averaged away.
func mosaic(img *image.RGBA, f int) {
	if f < 1 {
		return
	}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += f {
		for x := b.Min.X; x < b.Max.X; x += f {
			x1, y1 := x+f, y+f
			if x1 > b.Max.X {
				x1 = b.Max.X
			}
			if y1 > b.Max.Y {
				y1 = b.Max.Y
			}
			var sumR, sumG, sumB, sumA, n uint64
			for py := y; py < y1; py++ {
				for px := x; px < x1; px++ {
					i := img.PixOffset(px, py)
					sumR += uint64(img.Pix[i+0])
					sumG += uint64(img.Pix[i+1])
					sumB += uint64(img.Pix[i+2])
					sumA += uint64(img.Pix[i+3])
					n++
				}
			}
			if n == 0 {
				continue
			}
			mean := color.RGBA{
				R: uint8(sumR / n),
				G: uint8(sumG / n),
				B: uint8(sumB / n),
				A: uint8(sumA / n),
			}
			for py := y; py < y1; py++ {
				for px := x; px < x1; px++ {
					img.SetRGBA(px, py, mean)
				}
			}
		}
	}
}

// smooth3x3 runs ONE box-blur pass over the mosaic, so the result reads as
// deliberately obscured rather than as a broken file. It adds no security and
// takes none away: the samples were already thrown away by mosaic.
func smooth3x3(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			var sumR, sumG, sumB, sumA, n uint32
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					px, py := x+dx, y+dy
					// Edges sample fewer neighbours rather than wrapping or
					// clamping to a constant, which would darken the border.
					if px < b.Min.X || px >= b.Max.X || py < b.Min.Y || py >= b.Max.Y {
						continue
					}
					i := src.PixOffset(px, py)
					sumR += uint32(src.Pix[i+0])
					sumG += uint32(src.Pix[i+1])
					sumB += uint32(src.Pix[i+2])
					sumA += uint32(src.Pix[i+3])
					n++
				}
			}
			if n == 0 {
				continue
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(sumR / n),
				G: uint8(sumG / n),
				B: uint8(sumB / n),
				A: uint8(sumA / n),
			})
		}
	}
	return dst
}

// --- shared ---------------------------------------------------------------

// toRGBA gives the pixel-addressable buffer the treatments work on. An image
// that already is one is used as it is, so a PNG does not pay for a copy.
func toRGBA(src image.Image) *image.RGBA {
	if rgba, ok := src.(*image.RGBA); ok {
		return rgba
	}
	b := src.Bounds()
	// The destination is anchored at the origin, because every treatment above
	// indexes from 0 and a source with a non-zero origin (a sub-image) would
	// otherwise write outside itself.
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bl, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(r >> 8),
				G: uint8(g >> 8),
				B: uint8(bl >> 8),
				A: uint8(a >> 8),
			})
		}
	}
	return dst
}

// encodeRaster writes the replacement in the source's own encoding.
func encodeRaster(img image.Image, format Format) ([]byte, error) {
	var buf bytes.Buffer
	switch format {
	case FormatPNG:
		if err := png.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("the replacement picture could not be encoded as PNG (%v); "+
				"this is a defect, please report it with the document that caused it", err)
		}
	case FormatJPEG:
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
			return nil, fmt.Errorf("the replacement picture could not be encoded as JPEG (%v); "+
				"this is a defect, please report it with the document that caused it", err)
		}
	default:
		return nil, fmt.Errorf("a replacement picture cannot be written as a %s file; "+
			"this picture can be removed, or kept as it is", format)
	}
	return buf.Bytes(), nil
}

// engine/imaging/thumbnail.go — the preview a reviewer actually looks at.
//
// The review screen shows one small picture per asset, and a small picture has
// to be MADE: handing a 4000-pixel screenshot to the WebView for a 160-pixel
// slot means decoding and holding megabytes per row.
//
// The scaler is a box filter written out here rather than image/draw, because
// the standard library's draw does not resample: it would give nearest-neighbour
// aliasing, and a thumbnail full of aliasing tells the reviewer less about the
// picture than one that is simply smaller.
//
// SVG assets are never thumbnailed. Their bytes go to the interface as they
// are, and the interface renders them through an <img src="data:image/svg+xml">
// tag and never by inlining the element into the page: an <img> context runs no
// script and an inlined <svg> element does.
package imaging

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
)

// MaxThumbnailSourcePixels is the largest source picture the thumbnailer will
// decode. Above it the answer is an actionable error rather than an allocation:
// decoding 40 megapixels costs about 160 MB of RGBA, and a desktop application
// that swaps while drawing a preview is one the user cannot work in.
const MaxThumbnailSourcePixels = 40 * 1000 * 1000

// Thumbnail bounds for maxPx. Below the floor the result carries no
// information; above the ceiling it is not a thumbnail any more.
const (
	MinThumbnailPx = 16
	MaxThumbnailPx = 2048
)

// Thumbnail decodes a raster asset, scales it to fit maxPx on its longest side
// with a box filter, and returns PNG bytes.
//
// It never scales UP: a 40x40 icon comes back 40x40, because a blurry
// enlargement tells the reviewer less than the real thing.
//
// @param raw one picture part's bytes (PNG or JPEG)
// @param maxPx the longest side of the result, clamped to [16, 2048]
// @return PNG bytes and the size they decode to
func Thumbnail(raw []byte, maxPx int) (pngBytes []byte, w, h int, err error) {
	if maxPx < MinThumbnailPx {
		maxPx = MinThumbnailPx
	}
	if maxPx > MaxThumbnailPx {
		maxPx = MaxThumbnailPx
	}

	format := Sniff(raw)
	if format != FormatPNG && format != FormatJPEG {
		return nil, 0, 0, fmt.Errorf(
			"a preview can only be made from a PNG or JPEG picture, and this one is a %s file; "+
				"it is still listed and can still be removed", format)
	}

	// The size is read from the header FIRST, so an oversized picture is refused
	// before it is materialised rather than after.
	srcW, srcH, ok := Measure(raw, format)
	if !ok || srcW <= 0 || srcH <= 0 {
		return nil, 0, 0, fmt.Errorf(
			"this picture could not be read (its header does not decode as %s), so no preview "+
				"can be made; the picture is still listed and can still be removed", format)
	}
	if srcW*srcH > MaxThumbnailSourcePixels {
		return nil, 0, 0, fmt.Errorf(
			"this picture is %dx%d pixels (%d megapixels), which is above the %d megapixel limit "+
				"for making a preview; the picture is still listed and can still be treated, only "+
				"its preview is not shown",
			srcW, srcH, srcW*srcH/1_000_000, MaxThumbnailSourcePixels/1_000_000)
	}

	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, fmt.Errorf(
			"this picture could not be decoded (%v), so no preview can be made; it is still "+
				"listed and can still be removed", err)
	}

	dstW, dstH := ThumbnailSize(srcW, srcH, maxPx)
	scaled := boxScale(src, dstW, dstH)

	var buf bytes.Buffer
	if err := png.Encode(&buf, scaled); err != nil {
		return nil, 0, 0, fmt.Errorf("the preview could not be encoded as PNG (%v); this is a "+
			"defect, please report it with the document that caused it", err)
	}
	return buf.Bytes(), dstW, dstH, nil
}

// ThumbnailSize is the scaling arithmetic on its own, so the never-scale-up rule
// and the rounding are one testable thing rather than a line inside the decoder.
//
// The result keeps the source's aspect ratio and is never zero in either
// direction: a 1000x1 strip scaled to 200 is 200x1, not 200x0, because an image
// with a zero side cannot be encoded at all.
func ThumbnailSize(srcW, srcH, maxPx int) (w, h int) {
	if srcW <= 0 || srcH <= 0 {
		return 0, 0
	}
	longest := srcW
	if srcH > longest {
		longest = srcH
	}
	if longest <= maxPx {
		// Never scale up.
		return srcW, srcH
	}
	scale := float64(maxPx) / float64(longest)
	w = int(float64(srcW)*scale + 0.5)
	h = int(float64(srcH)*scale + 0.5)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}

// boxScale averages each destination pixel's footprint in the source.
//
// Plain 8-bit averaging, deliberately: the alternative is converting to linear
// light and back, which changes the result by an amount nobody reviewing a
// thumbnail can see and adds a table this package would have to carry.
//
// Alpha is averaged with the colours rather than premultiplied, which is right
// for the previews this draws (opaque photographs and logos on a flat
// background) and visibly wrong only for a picture whose transparent pixels
// carry a different colour from its opaque ones.
func boxScale(src image.Image, dstW, dstH int) *image.RGBA {
	bounds := src.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	if dstW == 0 || dstH == 0 {
		return dst
	}

	for y := 0; y < dstH; y++ {
		// The footprint of this destination row in the source, as a half-open
		// range. Computing it from the destination index (rather than stepping a
		// float) is what keeps every source pixel counted exactly once.
		y0 := y * srcH / dstH
		y1 := (y + 1) * srcH / dstH
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < dstW; x++ {
			x0 := x * srcW / dstW
			x1 := (x + 1) * srcW / dstW
			if x1 <= x0 {
				x1 = x0 + 1
			}

			var sumR, sumG, sumB, sumA, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					// RGBA() returns 16-bit values; shifting by 8 brings them
					// back to the 8-bit space the destination stores.
					r, g, b, a := src.At(bounds.Min.X+sx, bounds.Min.Y+sy).RGBA()
					sumR += uint64(r >> 8)
					sumG += uint64(g >> 8)
					sumB += uint64(b >> 8)
					sumA += uint64(a >> 8)
					n++
				}
			}
			if n == 0 {
				continue
			}
			i := dst.PixOffset(x, y)
			dst.Pix[i+0] = uint8(sumR / n)
			dst.Pix[i+1] = uint8(sumG / n)
			dst.Pix[i+2] = uint8(sumB / n)
			dst.Pix[i+3] = uint8(sumA / n)
		}
	}
	return dst
}

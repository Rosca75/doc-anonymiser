// engine/imaging/treat_test.go — the three treatments, unit tier.
//
// TIER: unit (docs/TESTING.md). Every case encodes a real picture in memory,
// treats it, and decodes the answer. No file is read and no archive is opened;
// the export's use of these bytes is tested one tier up.
//
// What these tests assert is the CONTRACT the export depends on: the answer
// decodes, it is the source's own pixel size, it is the source's own ENCODING,
// and for a blur the original samples are gone rather than smeared. Appearance
// is deliberately not asserted: "looks blurred" is not a property, and a test
// that pinned pixel values would fail on any future improvement to the filter
// while proving nothing about the leak.
package imaging_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine/imaging"
)

// noisePNG is a picture with a different value in every pixel, so a blur that
// only smeared it would still leave neighbouring pixels distinguishable.
func noisePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// A deterministic spread rather than math/rand: a fixture that
			// changes between runs makes a failure impossible to reproduce.
			v := uint8((x*7 + y*29) % 256)
			img.SetRGBA(x, y, color.RGBA{R: v, G: uint8(255 - int(v)), B: uint8((x + y) % 256), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("could not encode the noise fixture: %v", err)
	}
	return buf.Bytes()
}

// decodeSize reads back what a treatment produced.
func decodeSize(t *testing.T, raw []byte) (image.Image, int, int) {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the treated picture does not decode: %v", err)
	}
	b := img.Bounds()
	return img, b.Dx(), b.Dy()
}

// TestTreatKeepWritesNothing: keep must answer with NO bytes, because the export
// reads nil as "copy the original entry through" and any bytes at all would
// re-encode a picture nobody asked to change.
func TestTreatKeepWritesNothing(t *testing.T) {
	t.Run("redaction/keep_writes_nothing", func(t *testing.T) {
		src := rasterPNG(t, 20, 10)
		asset := imaging.Asset{ID: "ppt/media/image1.png", Format: imaging.FormatPNG, Width: 20, Height: 10}
		for _, d := range []imaging.Decision{{}, {Treatment: imaging.TreatmentKeep}} {
			out, err := imaging.Treat(src, asset, d)
			if err != nil {
				t.Fatalf("Treat(keep): %v, want no error", err)
			}
			if out != nil {
				t.Errorf("Treat(%+v) returned %d bytes, want nil: the export reads nil as "+
					"'leave this entry alone'", d, len(out))
			}
		}
	})
}

// TestTreatBoxKeepsSizeAndEncoding: the two properties the archive depends on.
// A box of the wrong pixel size is redrawn by the frame and looks stretched; a
// box in the wrong encoding is a part whose bytes disagree with the content type
// the archive declares for its extension.
func TestTreatBoxKeepsSizeAndEncoding(t *testing.T) {
	cases := []struct {
		name          string
		src           []byte
		format        imaging.Format
		w, h          int
		wantSignature []byte
	}{
		{
			name: "redaction/box_png_keeps_size_and_encoding",
			src:  rasterPNG(t, 120, 80), format: imaging.FormatPNG, w: 120, h: 80,
			wantSignature: []byte{0x89, 'P', 'N', 'G'},
		},
		{
			name: "redaction/box_jpeg_keeps_size_and_encoding",
			src:  rasterJPEG(t, 200, 200), format: imaging.FormatJPEG, w: 200, h: 200,
			wantSignature: []byte{0xFF, 0xD8, 0xFF},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			asset := imaging.Asset{ID: "ppt/media/image1", Format: c.format, Width: c.w, Height: c.h}
			out, err := imaging.Treat(c.src, asset,
				imaging.Decision{Treatment: imaging.TreatmentBox, BoxText: "Client logo removed"})
			if err != nil {
				t.Fatalf("Treat(box): %v, want no error", err)
			}
			if !bytes.HasPrefix(out, c.wantSignature) {
				t.Errorf("the box came back as %s, want the source's %s encoding",
					imaging.Sniff(out), c.format)
			}
			if _, gotW, gotH := decodeSize(t, out); gotW != c.w || gotH != c.h {
				t.Errorf("the box is %dx%d, want the source's %dx%d: the frame rescales "+
					"whatever it is given, so a different shape reads as stretched",
					gotW, gotH, c.w, c.h)
			}
		})
	}
}

// TestTreatBoxDrawsTextOnlyWhenGiven: the centre of the box carries ink when
// there is a caption and none when there is not, so the empty case really is the
// plain rectangle the model promises.
func TestTreatBoxDrawsTextOnlyWhenGiven(t *testing.T) {
	asset := imaging.Asset{ID: "ppt/media/image1.png", Format: imaging.FormatPNG, Width: 240, Height: 120}
	src := rasterPNG(t, 240, 120)

	cases := []struct {
		name    string
		text    string
		wantInk bool
	}{
		{"redaction/box_with_text_has_ink", "Acme group logo", true},
		{"redaction/box_without_text_is_plain", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := imaging.Treat(src, asset,
				imaging.Decision{Treatment: imaging.TreatmentBox, BoxText: c.text})
			if err != nil {
				t.Fatalf("Treat(box): %v", err)
			}
			img, w, h := decodeSize(t, out)
			// The middle half of the box, so the border is never counted as ink.
			ink := 0
			for y := h / 4; y < 3*h/4; y++ {
				for x := w / 4; x < 3*w/4; x++ {
					r, g, b, _ := img.At(x, y).RGBA()
					// The fill is #E5E7EB and the ink is #2D2D2D, so anything
					// dark in the middle is a glyph.
					if r>>8 < 0x80 && g>>8 < 0x80 && b>>8 < 0x80 {
						ink++
					}
				}
			}
			if c.wantInk && ink == 0 {
				t.Errorf("the box carries no ink in its middle, so the caption %q was not drawn", c.text)
			}
			if !c.wantInk && ink != 0 {
				t.Errorf("an empty caption drew %d dark pixels; an empty box must be a plain "+
					"rectangle", ink)
			}
		})
	}
}

// TestTreatBoxFallsBackToTheFrame: a legacy VML picture states its size in a CSS
// style attribute the scanner does not read, so nothing knows its pixel size and
// the display frame is all there is. The chain must reach it rather than
// defaulting straight to 640x480.
func TestTreatBoxFallsBackToTheFrame(t *testing.T) {
	t.Run("redaction/box_falls_back_to_the_display_frame", func(t *testing.T) {
		// A part that sniffs as PNG (so box is allowed) but whose header does not
		// decode, which is exactly the unreadable-part case the scan warns about.
		broken := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, []byte("not a real PNG")...)
		asset := imaging.Asset{
			ID:     "word/media/image3.png",
			Format: imaging.FormatPNG,
			Occurrences: []imaging.Occurrence{{
				Part: "word/document.xml", Kind: imaging.KindPicture,
				// 2 inches by 1 inch, in EMU.
				DisplayCX: 1828800, DisplayCY: 914400,
			}},
		}
		out, err := imaging.Treat(broken, asset, imaging.Decision{Treatment: imaging.TreatmentBox})
		if err != nil {
			t.Fatalf("Treat(box) on an undecodable part: %v, want the frame's size instead", err)
		}
		_, w, h := decodeSize(t, out)
		if w != 192 || h != 96 {
			t.Errorf("the box is %dx%d, want 192x96 (the 2x1 inch frame at 96 dpi)", w, h)
		}
	})
}

// TestTreatBoxOnSVGIsSVG: an SVG asset's box must come back as SVG, with the
// caption escaped and a real font family named. The vector box is the one place
// real letterforms exist, so it uses them.
func TestTreatBoxOnSVGIsSVG(t *testing.T) {
	t.Run("redaction/box_svg_is_svg", func(t *testing.T) {
		src := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" ` +
			`width="300" height="150"><rect width="300" height="150" fill="#123456"/></svg>`)
		asset := imaging.Asset{ID: "ppt/media/image2.svg", Format: imaging.FormatSVG, Width: 300, Height: 150}

		out, err := imaging.Treat(src, asset,
			imaging.Decision{Treatment: imaging.TreatmentBox, BoxText: `Schéma "Borealis" & co`})
		if err != nil {
			t.Fatalf("Treat(box) on an SVG: %v", err)
		}
		if imaging.Sniff(out) != imaging.FormatSVG {
			t.Fatalf("the treated SVG sniffs as %s, want svg", imaging.Sniff(out))
		}
		got := string(out)
		for _, want := range []string{
			`width="300"`, `height="150"`,
			"Helvetica, Arial, sans-serif",
			// The accents survive, because a vector box names a real font.
			"Schéma",
			// And the markup-significant characters are escaped, or the SVG is
			// no longer well formed.
			"&amp;", "&#34;",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("the SVG box does not contain %q:\n%s", want, got)
			}
		}
		if strings.Contains(got, "#123456") {
			t.Error("the original SVG's own paint survived into the replacement, so the " +
				"picture was not replaced at all")
		}
	})
}

// TestTreatRemoveIsAOnePixelBlank: remove overwrites the bytes even though the
// export also deletes the element, because an orphan picture part inside the zip
// is a leak that LOOKS like a redaction.
func TestTreatRemoveIsAOnePixelBlank(t *testing.T) {
	cases := []struct {
		name   string
		src    []byte
		format imaging.Format
		// wantFormat is what the replacement must be. A JPEG source answers with
		// a PNG because JPEG has no transparency at all.
		wantFormat imaging.Format
	}{
		{"redaction/remove_png", rasterPNG(t, 120, 80), imaging.FormatPNG, imaging.FormatPNG},
		{"redaction/remove_jpeg", rasterJPEG(t, 64, 64), imaging.FormatJPEG, imaging.FormatPNG},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			asset := imaging.Asset{ID: "ppt/media/image1", Format: c.format}
			out, err := imaging.Treat(c.src, asset, imaging.Decision{Treatment: imaging.TreatmentRemove})
			if err != nil {
				t.Fatalf("Treat(remove): %v", err)
			}
			if got := imaging.Sniff(out); got != c.wantFormat {
				t.Errorf("the blank is a %s, want %s", got, c.wantFormat)
			}
			img, w, h := decodeSize(t, out)
			if w != 1 || h != 1 {
				t.Errorf("the blank is %dx%d, want 1x1", w, h)
			}
			if _, _, _, alpha := img.At(0, 0).RGBA(); alpha != 0 {
				t.Errorf("the blank pixel's alpha is %d, want 0: a fill or a background has no "+
					"element to delete, so these bytes ARE the whole removal", alpha)
			}
			if bytes.Contains(out, c.src) || len(out) >= len(c.src) {
				t.Errorf("the replacement is %d bytes against the source's %d; the original "+
					"pixels must not survive", len(out), len(c.src))
			}
		})
	}
}

// TestTreatRemoveOnSVG: an SVG source answers with an empty SVG, so the part's
// declared content type still matches its bytes.
func TestTreatRemoveOnSVG(t *testing.T) {
	t.Run("redaction/remove_svg", func(t *testing.T) {
		src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10">` +
			`<text>pierre.dupont@tpps.com</text></svg>`)
		asset := imaging.Asset{ID: "ppt/media/image3.svg", Format: imaging.FormatSVG}
		out, err := imaging.Treat(src, asset, imaging.Decision{Treatment: imaging.TreatmentRemove})
		if err != nil {
			t.Fatalf("Treat(remove) on an SVG: %v", err)
		}
		if imaging.Sniff(out) != imaging.FormatSVG {
			t.Errorf("the blank sniffs as %s, want svg", imaging.Sniff(out))
		}
		if strings.Contains(string(out), "pierre.dupont") {
			t.Error("the original SVG's text survived the removal, which is the leak the " +
				"bytes-are-overwritten rule exists to prevent")
		}
	})
}

// TestTreatBlurDestroysDetail: the property is INFORMATION LOSS, not appearance.
//
// A source with a different value in every pixel comes back with ONE value per
// mosaic block: the block's mean. That is checked on each block's interior,
// away from the edges the smoothing pass mixes, because a smear would leave
// neighbouring pixels inside a block distinguishable and a block mean cannot.
//
// The exact count the mosaic leaves before smoothing is asserted white-box in
// treat_internal_test.go; observable from out here is that the block structure
// survived at all, which is what "the samples are gone" looks like from the
// outside.
func TestTreatBlurDestroysDetail(t *testing.T) {
	t.Run("redaction/blur_destroys_detail", func(t *testing.T) {
		const w, h = 120, 90
		src := noisePNG(t, w, h)
		asset := imaging.Asset{ID: "ppt/media/image1.png", Format: imaging.FormatPNG, Width: w, Height: h}

		out, err := imaging.Treat(src, asset,
			imaging.Decision{Treatment: imaging.TreatmentBlur, BlurStrength: 10})
		if err != nil {
			t.Fatalf("Treat(blur): %v", err)
		}
		if bytes.Equal(out, src) {
			t.Fatal("the blurred bytes equal the source bytes: nothing was applied")
		}
		img, gotW, gotH := decodeSize(t, out)
		if gotW != w || gotH != h {
			t.Fatalf("the blur is %dx%d, want the source's %dx%d", gotW, gotH, w, h)
		}

		// Strength 10 on a 90-pixel-high picture is a 5-pixel block, so each
		// block has a 3x3 interior the smoothing pass could not reach across.
		const f = 5
		for by := 0; by+f <= gotH; by += f {
			for bx := 0; bx+f <= gotW; bx += f {
				first := img.At(bx+1, by+1)
				for y := by + 1; y < by+f-1; y++ {
					for x := bx + 1; x < bx+f-1; x++ {
						if img.At(x, y) != first {
							t.Fatalf("inside the block at (%d,%d) the pixel (%d,%d) is %v and "+
								"(%d,%d) is %v; a block mean makes them equal, and a smear "+
								"does not",
								bx, by, x, y, img.At(x, y), bx+1, by+1, first)
						}
					}
				}
			}
		}

		// And the source really did carry detail at that scale, or the check
		// above would pass on any picture at all.
		source, _, _ := decodeSize(t, src)
		if source.At(1, 1) == source.At(2, 2) {
			t.Error("the noise fixture is flat at the block scale, so this test proves nothing; " +
				"fix the fixture rather than the assertion")
		}
	})
}

// TestTreatBlurScalesWithTheStrength: a higher strength must destroy more, or the
// control the interface offers does nothing the user can see.
func TestTreatBlurScalesWithTheStrength(t *testing.T) {
	t.Run("redaction/blur_scales_with_the_strength", func(t *testing.T) {
		const w, h = 200, 200
		src := noisePNG(t, w, h)
		asset := imaging.Asset{ID: "ppt/media/image1.png", Format: imaging.FormatPNG, Width: w, Height: h}

		counts := map[int]int{}
		for _, strength := range []int{1, 10} {
			out, err := imaging.Treat(src, asset,
				imaging.Decision{Treatment: imaging.TreatmentBlur, BlurStrength: strength})
			if err != nil {
				t.Fatalf("Treat(blur, %d): %v", strength, err)
			}
			img, _, _ := decodeSize(t, out)
			distinct := map[color.RGBA]bool{}
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					r, g, b, a := img.At(x, y).RGBA()
					distinct[color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}] = true
				}
			}
			counts[strength] = len(distinct)
		}
		if counts[10] >= counts[1] {
			t.Errorf("strength 10 left %d distinct colours and strength 1 left %d; the dial "+
				"must destroy more as it rises", counts[10], counts[1])
		}
	})
}

// TestTreatBlurRefusesWhatItCannotDecode: the refusal names the two treatments
// that need no decoding, so the user is never left with a picture they cannot
// act on.
func TestTreatBlurRefusesWhatItCannotDecode(t *testing.T) {
	t.Run("errors/blur_refuses_an_undecodable_part", func(t *testing.T) {
		broken := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, []byte("truncated")...)
		asset := imaging.Asset{ID: "word/media/image1.png", Format: imaging.FormatPNG}
		_, err := imaging.Treat(broken, asset,
			imaging.Decision{Treatment: imaging.TreatmentBlur, BlurStrength: 5})
		if err == nil {
			t.Fatal("blurring a part that does not decode was accepted, want a refusal")
		}
		if !strings.Contains(err.Error(), "remove") {
			t.Errorf("the refusal does not name a way out:\n%v", err)
		}
	})
}

// TestTreatRefusesADecisionThePictureCannotCarry: Treat is the LAST gate before
// bytes are written, so it validates too. A decision that reached it from a
// restored session, a test or a future caller must be refused rather than drawn
// wrong.
func TestTreatRefusesADecisionThePictureCannotCarry(t *testing.T) {
	t.Run("errors/treat_validates_before_writing", func(t *testing.T) {
		svg := imaging.Asset{ID: "ppt/media/image2.png", Format: imaging.FormatSVG}
		_, err := imaging.Treat(rasterPNG(t, 10, 10), svg,
			imaging.Decision{Treatment: imaging.TreatmentBlur, BlurStrength: 5})
		if err == nil {
			t.Fatal("Treat accepted a blur on an SVG asset, which Validate refuses; the two " +
				"must not be able to disagree")
		}
	})
}

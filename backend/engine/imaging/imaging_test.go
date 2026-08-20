// engine/imaging/imaging_test.go — the format sniffer, the measurer and the
// thumbnailer, unit tier.
//
// TIER: unit (docs/TESTING.md). Pure functions over bytes built in the test.
package imaging_test

import (
	"bytes"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine/imaging"
)

// rasterPNG and rasterJPEG build real pictures, because sniffing a hand-written
// header would not prove the measurer can read one.
func rasterPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 60, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("could not encode the test PNG: %v", err)
	}
	return buf.Bytes()
}

func rasterJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("could not encode the test JPEG: %v", err)
	}
	return buf.Bytes()
}

// TestSniff: the format is decided by the BYTES. The case that matters is the
// part named .png holding JPEG bytes, which is common enough in real documents
// that trusting the extension would mis-encode a treated picture.
func TestSniff(t *testing.T) {
	jpegBytes := rasterJPEG(t, 8, 8)
	pngBytes := rasterPNG(t, 8, 8)

	cases := []struct {
		name string
		raw  []byte
		want imaging.Format
	}{
		{"png_signature", pngBytes, imaging.FormatPNG},
		{"jpeg_signature", jpegBytes, imaging.FormatJPEG},
		{"png_named_part_holding_jpeg_bytes", jpegBytes, imaging.FormatJPEG},
		{"svg_with_xml_declaration", []byte(`<?xml version="1.0"?><svg viewBox="0 0 10 10"/>`), imaging.FormatSVG},
		{"svg_without_declaration", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="4"/>`), imaging.FormatSVG},
		{"xml_that_is_not_svg", []byte(`<?xml version="1.0"?><w:document/>`), imaging.FormatOther},
		{"emf_or_anything_else", []byte{0x01, 0x00, 0x00, 0x00, 0x6C}, imaging.FormatOther},
		{"empty_part", nil, imaging.FormatOther},
	}
	for _, c := range cases {
		t.Run("extraction/sniff_"+c.name, func(t *testing.T) {
			if got := imaging.Sniff(c.raw); got != c.want {
				t.Errorf("Sniff(%d bytes) = %q, want %q", len(c.raw), got, c.want)
			}
		})
	}
}

// TestMeasure: the size comes from the header for raster pictures and from the
// SVG's own attributes for vector ones, with the viewBox as the fallback that
// an Office-exported SVG usually needs.
func TestMeasure(t *testing.T) {
	cases := []struct {
		name   string
		raw    []byte
		format imaging.Format
		w, h   int
		ok     bool
	}{
		{"png_header", rasterPNG(t, 320, 200), imaging.FormatPNG, 320, 200, true},
		{"jpeg_header", rasterJPEG(t, 64, 48), imaging.FormatJPEG, 64, 48, true},
		{"svg_width_height", []byte(`<svg width="300" height="150"/>`), imaging.FormatSVG, 300, 150, true},
		{"svg_pixel_units", []byte(`<svg width="300px" height="150px"/>`), imaging.FormatSVG, 300, 150, true},
		{"svg_viewbox_only", []byte(`<svg viewBox="0 0 240 120"/>`), imaging.FormatSVG, 240, 120, true},
		{"svg_percentage_falls_back_to_viewbox", []byte(`<svg width="100%" height="100%" viewBox="0 0 40 20"/>`), imaging.FormatSVG, 40, 20, true},
		{"svg_states_nothing", []byte(`<svg/>`), imaging.FormatSVG, 0, 0, false},
		{"undecodable_png", []byte("\x89PNG\r\n\x1a\nrubbish"), imaging.FormatPNG, 0, 0, false},
		{"other_format", []byte{0x01, 0x02}, imaging.FormatOther, 0, 0, false},
	}
	for _, c := range cases {
		t.Run("extraction/measure_"+c.name, func(t *testing.T) {
			w, h, ok := imaging.Measure(c.raw, c.format)
			if w != c.w || h != c.h || ok != c.ok {
				t.Errorf("Measure(%q) = %dx%d ok=%v, want %dx%d ok=%v",
					c.format, w, h, ok, c.w, c.h, c.ok)
			}
		})
	}
}

// TestThumbnailSize: the scaling arithmetic, including the rule that a small
// picture is never enlarged. A blurry enlargement tells a reviewer less about
// the picture than the real thing does.
func TestThumbnailSize(t *testing.T) {
	cases := []struct {
		name         string
		srcW, srcH   int
		maxPx        int
		wantW, wantH int
	}{
		{"landscape_fits_the_longest_side", 1000, 400, 200, 200, 80},
		{"portrait_fits_the_longest_side", 400, 1000, 200, 80, 200},
		{"square", 500, 500, 250, 250, 250},
		{"never_scales_up", 40, 40, 320, 40, 40},
		{"exactly_at_the_limit", 320, 100, 320, 320, 100},
		{"thin_strip_keeps_a_pixel", 1000, 1, 200, 200, 1},
		{"empty_source", 0, 0, 200, 0, 0},
	}
	for _, c := range cases {
		t.Run("extraction/thumbnail_"+c.name, func(t *testing.T) {
			w, h := imaging.ThumbnailSize(c.srcW, c.srcH, c.maxPx)
			if w != c.wantW || h != c.wantH {
				t.Errorf("ThumbnailSize(%d, %d, %d) = %dx%d, want %dx%d",
					c.srcW, c.srcH, c.maxPx, w, h, c.wantW, c.wantH)
			}
		})
	}
}

// TestThumbnail: the output is a decodable PNG at the computed size, whatever
// the source encoding was.
func TestThumbnail(t *testing.T) {
	t.Run("extraction/thumbnail_scales_and_encodes_png", func(t *testing.T) {
		out, w, h, err := imaging.Thumbnail(rasterPNG(t, 1000, 400), 200)
		if err != nil {
			t.Fatalf("Thumbnail of a 1000x400 PNG: %v, want no error", err)
		}
		if w != 200 || h != 80 {
			t.Errorf("the thumbnail reports %dx%d, want 200x80", w, h)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
		if err != nil {
			t.Fatalf("the thumbnail does not decode: %v", err)
		}
		if format != "png" {
			t.Errorf("the thumbnail is a %s, want a png", format)
		}
		if cfg.Width != 200 || cfg.Height != 80 {
			t.Errorf("the thumbnail decodes as %dx%d, want 200x80", cfg.Width, cfg.Height)
		}
	})

	t.Run("extraction/thumbnail_from_jpeg_source", func(t *testing.T) {
		out, w, h, err := imaging.Thumbnail(rasterJPEG(t, 600, 600), 100)
		if err != nil {
			t.Fatalf("Thumbnail of a JPEG: %v, want no error", err)
		}
		if w != 100 || h != 100 {
			t.Errorf("the thumbnail reports %dx%d, want 100x100", w, h)
		}
		if _, format, err := image.DecodeConfig(bytes.NewReader(out)); err != nil || format != "png" {
			t.Errorf("a JPEG source must still give a PNG preview, got format %q err %v", format, err)
		}
	})

	t.Run("extraction/thumbnail_never_scales_up", func(t *testing.T) {
		_, w, h, err := imaging.Thumbnail(rasterPNG(t, 40, 30), 320)
		if err != nil {
			t.Fatalf("Thumbnail of a small picture: %v, want no error", err)
		}
		if w != 40 || h != 30 {
			t.Errorf("a 40x30 source came back %dx%d, want 40x30", w, h)
		}
	})

	t.Run("errors/thumbnail_refuses_a_vector_source", func(t *testing.T) {
		_, _, _, err := imaging.Thumbnail([]byte(`<svg viewBox="0 0 10 10"/>`), 200)
		if err == nil {
			t.Fatal("Thumbnail of an SVG must fail: an SVG is handed to the interface as it is")
		}
		if !strings.Contains(err.Error(), "PNG or JPEG") {
			t.Errorf("the error is %q, and it must say which formats have a preview", err)
		}
	})

	t.Run("errors/thumbnail_refuses_an_undecodable_source", func(t *testing.T) {
		_, _, _, err := imaging.Thumbnail([]byte("\x89PNG\r\n\x1a\nrubbish"), 200)
		if err == nil {
			t.Fatal("Thumbnail of a part that does not decode must fail, got no error")
		}
		if !strings.Contains(err.Error(), "still listed") {
			t.Errorf("the error is %q, and it must say the picture can still be treated", err)
		}
	})

	t.Run("errors/thumbnail_refuses_an_enormous_source", func(t *testing.T) {
		// The refusal is read from the HEADER, so this test never allocates the
		// picture it describes: a hand-written PNG header claiming 20000x20000
		// is 400 megapixels and 88 bytes.
		huge := hugePNGHeader(t, 20000, 20000)
		_, _, _, err := imaging.Thumbnail(huge, 200)
		if err == nil {
			t.Fatal("Thumbnail of a 400 megapixel picture must be refused, got no error")
		}
		if !strings.Contains(err.Error(), "megapixel") {
			t.Errorf("the error is %q, and it must say the picture is too large to preview", err)
		}
	})
}

// hugePNGHeader writes a PNG header declaring an enormous size, with no pixel
// data behind it. That is exactly the point: the refusal must happen from the
// HEADER, before anything is decoded, or the guard costs what it is guarding
// against.
func hugePNGHeader(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})

	ihdr := make([]byte, 0, 17)
	ihdr = append(ihdr, 'I', 'H', 'D', 'R')
	ihdr = appendUint32(ihdr, uint32(w))
	ihdr = appendUint32(ihdr, uint32(h))
	ihdr = append(ihdr, 8, 6, 0, 0, 0) // 8-bit depth, truecolour with alpha

	buf.Write(appendUint32(nil, uint32(len(ihdr)-4)))
	buf.Write(ihdr)
	buf.Write(appendUint32(nil, crc32.ChecksumIEEE(ihdr)))
	return buf.Bytes()
}

// appendUint32 appends a big-endian length or checksum, as every PNG chunk
// header carries.
func appendUint32(b []byte, v uint32) []byte {
	return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// TestNotApplicable: a format with no image review answers with a reason CODE,
// never a sentence, because the interface owns its own copy.
func TestNotApplicable(t *testing.T) {
	t.Run("config/not_applicable_carries_a_code", func(t *testing.T) {
		inv := imaging.NotApplicable(imaging.ReasonPDFImagesRemoved)
		if inv.Applicable {
			t.Error("NotApplicable must not be applicable")
		}
		if inv.Reason != "pdf_images_removed" {
			t.Errorf("the reason is %q, want the code pdf_images_removed", inv.Reason)
		}
		if inv.Assets == nil {
			t.Error("the asset list must be empty rather than absent, so the interface never has " +
				"to distinguish null from empty")
		}
	})
}

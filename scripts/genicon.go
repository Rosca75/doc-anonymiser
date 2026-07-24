//go:build ignore

// scripts/genicon.go — generates the application icon (BUILD-02 Phase 1g,
// fallback route 2: the Rosca75/pptx-compressor icon pattern was not
// available in-session, so this stdlib-only generator is the recorded
// fallback; swap in the real pattern when the owner provides it).
//
// Run with:  go run scripts/genicon.go
//
// Output: build/appicon.png, a 1024x1024 PNG that Wails v2 picks up as the
// Windows executable icon. Design: white rounded square, a black document
// glyph with three redaction bars, the middle bar in the brand signature
// orange #FD5108 (docs/brand/color-palette.json). Pure Go stdlib (image,
// image/png), no dependencies — consistent with the zero-CGo rule.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

const size = 1024

var (
	white  = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	black  = color.NRGBA{0x00, 0x00, 0x00, 0xFF}
	orange = color.NRGBA{0xFD, 0x51, 0x08, 0xFF} // brand signature orange
)

func main() {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))

	// Transparent canvas, then the white rounded square (radius 160 px).
	drawRoundedRect(img, 0, 0, size, size, 160, white)

	// Document glyph: a black-outlined page, centred. The page is drawn as
	// a filled black rounded rectangle with a white inset (outline effect).
	pageX0, pageY0 := 272, 160
	pageX1, pageY1 := 752, 864
	drawRoundedRect(img, pageX0, pageY0, pageX1, pageY1, 40, black)
	drawRoundedRect(img, pageX0+28, pageY0+28, pageX1-28, pageY1-28, 24, white)

	// Three redaction bars across the page body; the MIDDLE one is orange
	// (the single accent element, per the brand one-loud-element rule).
	barX0, barX1 := pageX0+80, pageX1-80
	bars := []struct {
		y0, y1 int
		col    color.NRGBA
	}{
		{330, 410, black},
		{470, 550, orange},
		{610, 690, black},
	}
	for _, b := range bars {
		drawRoundedRect(img, barX0, b.y0, barX1, b.y1, 20, b.col)
	}

	if err := os.MkdirAll("build", 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "could not create build/:", err)
		os.Exit(1)
	}
	out := filepath.Join("build", "appicon.png")
	f, err := os.Create(out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not create", out, ":", err)
		os.Exit(1)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fmt.Fprintln(os.Stderr, "could not encode PNG:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", out)
}

// drawRoundedRect fills the rectangle (x0,y0)-(x1,y1) with col, rounding
// the four corners with the given radius (simple per-pixel distance test —
// fine for a one-shot generator).
func drawRoundedRect(img *image.NRGBA, x0, y0, x1, y1, r int, col color.NRGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			// Corner test: inside the radius region, require the pixel to
			// be within distance r of the corner circle centre.
			cx, cy := x, y
			inCorner := false
			switch {
			case x < x0+r && y < y0+r:
				cx, cy, inCorner = x0+r, y0+r, true
			case x >= x1-r && y < y0+r:
				cx, cy, inCorner = x1-r-1, y0+r, true
			case x < x0+r && y >= y1-r:
				cx, cy, inCorner = x0+r, y1-r-1, true
			case x >= x1-r && y >= y1-r:
				cx, cy, inCorner = x1-r-1, y1-r-1, true
			}
			if inCorner {
				dx, dy := x-cx, y-cy
				if dx*dx+dy*dy > r*r {
					continue
				}
			}
			img.SetNRGBA(x, y, col)
		}
	}
}

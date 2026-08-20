// engine/imaging/treat_internal_test.go — the blur maths, unit tier.
//
// TIER: unit (docs/TESTING.md).
//
// WHITE BOX on purpose (package imaging, not imaging_test). The property that
// makes a blur a redaction rather than a decoration is what the MOSAIC step
// leaves behind, before the smoothing pass runs over it: at most one value per
// block, whatever the source held. Once the smoothing pass has run, that count
// is no longer observable, so the only place to assert it is from inside.
// docs/TESTING.md allows a white-box test exactly where unexported behaviour is
// genuinely the subject, and here it is the subject.
package imaging

import (
	"image"
	"image/color"
	"testing"
)

// TestMosaicFactor: the table in the change order, which is the scale-invariance
// promise. A fixed pixel radius does nothing to a 4000-pixel screenshot and
// obliterates a 60-pixel icon, so the block is a fraction of the smaller side.
func TestMosaicFactor(t *testing.T) {
	cases := []struct {
		name     string
		w, h     int
		strength int
		want     int
	}{
		{"redaction/mosaic_factor_icon_weakest", 200, 200, 1, 2},
		{"redaction/mosaic_factor_icon_default", 200, 200, 5, 5},
		{"redaction/mosaic_factor_icon_strongest", 200, 200, 10, 10},
		{"redaction/mosaic_factor_photo_weakest", 1200, 800, 1, 4},
		{"redaction/mosaic_factor_photo_default", 1200, 800, 5, 20},
		{"redaction/mosaic_factor_photo_strongest", 1200, 800, 10, 40},
		{"redaction/mosaic_factor_screenshot_weakest", 3840, 2160, 1, 11},
		{"redaction/mosaic_factor_screenshot_default", 3840, 2160, 5, 54},
		{"redaction/mosaic_factor_screenshot_strongest", 3840, 2160, 10, 108},
		// A one-pixel block destroys nothing, so the floor is two.
		{"redaction/mosaic_factor_floor_is_two", 40, 40, 1, 2},
		// An unstated strength reads as the default, the way every absent value
		// in this application does.
		{"redaction/mosaic_factor_absent_strength_is_the_default", 600, 600, 0, 15},
		// A block can never exceed the picture: a one-pixel part (the blank a
		// removal writes) must not ask for a two-pixel block.
		{"redaction/mosaic_factor_never_exceeds_the_picture", 1, 1, 10, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mosaicFactor(c.w, c.h, c.strength); got != c.want {
				t.Errorf("mosaicFactor(%d, %d, %d) = %d, want %d",
					c.w, c.h, c.strength, got, c.want)
			}
		})
	}
}

// TestMosaicThrowsTheSamplesAway is the load-bearing one: after the mosaic, the
// picture holds at most ceil(w/f) * ceil(h/f) distinct values, whatever it held
// before. That number IS the definition of "the samples are gone": nothing
// downstream, and no filter run backwards, can recover what was averaged into a
// single value.
func TestMosaicThrowsTheSamplesAway(t *testing.T) {
	cases := []struct {
		name string
		w, h int
		f    int
	}{
		{"redaction/mosaic_exact_blocks", 120, 90, 6},
		{"redaction/mosaic_partial_edge_blocks", 121, 91, 6},
		{"redaction/mosaic_one_block", 20, 20, 20},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			img := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
			for y := 0; y < c.h; y++ {
				for x := 0; x < c.w; x++ {
					// Every pixel distinct in at least one channel, so a filter
					// that preserved any detail would show up as a higher count.
					img.SetRGBA(x, y, color.RGBA{
						R: uint8(x % 256), G: uint8(y % 256), B: uint8((x * y) % 256), A: 255,
					})
				}
			}
			mosaic(img, c.f)

			distinct := map[color.RGBA]bool{}
			for y := 0; y < c.h; y++ {
				for x := 0; x < c.w; x++ {
					distinct[img.RGBAAt(x, y)] = true
				}
			}
			ceilDiv := func(a, b int) int { return (a + b - 1) / b }
			want := ceilDiv(c.w, c.f) * ceilDiv(c.h, c.f)
			if len(distinct) > want {
				t.Errorf("the mosaic left %d distinct values for a %dx%d picture in %d-pixel "+
					"blocks; at most %d are possible, so detail survived the averaging",
					len(distinct), c.w, c.h, c.f, want)
			}

			// And every pixel of one block really is the block's own value, or
			// the count above could be met by leaving part of the picture alone.
			for by := 0; by < c.h; by += c.f {
				for bx := 0; bx < c.w; bx += c.f {
					first := img.RGBAAt(bx, by)
					for y := by; y < by+c.f && y < c.h; y++ {
						for x := bx; x < bx+c.f && x < c.w; x++ {
							if img.RGBAAt(x, y) != first {
								t.Fatalf("the block at (%d,%d) is not uniform: (%d,%d) is %v, "+
									"want %v", bx, by, x, y, img.RGBAAt(x, y), first)
							}
						}
					}
				}
			}
		})
	}
}

// TestSmoothKeepsTheSizeAndTheBlockInteriors: the smoothing pass is cosmetic and
// must stay cosmetic. It may soften the block edges; it must not change the
// picture's size, and it cannot reintroduce detail, because it only ever mixes
// values the mosaic already reduced to block means.
func TestSmoothKeepsTheSizeAndTheBlockInteriors(t *testing.T) {
	t.Run("redaction/smooth_is_cosmetic", func(t *testing.T) {
		const w, h, f = 60, 60, 10
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0, A: 255})
			}
		}
		mosaic(img, f)
		out := smooth3x3(img)

		if b := out.Bounds(); b.Dx() != w || b.Dy() != h {
			t.Fatalf("the smoothed picture is %dx%d, want %dx%d", b.Dx(), b.Dy(), w, h)
		}
		// One pixel in from every block edge, the 3x3 window sees only this
		// block, so the value is untouched.
		for by := 0; by < h; by += f {
			for bx := 0; bx < w; bx += f {
				want := img.RGBAAt(bx, by)
				if got := out.RGBAAt(bx+f/2, by+f/2); got != want {
					t.Errorf("the block at (%d,%d) changed value in its middle: %v, want %v",
						bx, by, got, want)
				}
			}
		}
	})
}

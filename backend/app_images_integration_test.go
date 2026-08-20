//go:build integration

// app_images_integration_test.go — the bound image methods over the committed
// binary fixtures.
//
// TIER: integration (docs/TESTING.md). It imports real .docx and .pptx files
// through engine.LoadAll, exactly as a user's import does, and then asks the
// bound layer for their pictures and one preview. What it adds over the unit
// tier is the WIRING: that the format dispatch reaches the right scanner and
// that a preview built from Document.Raw decodes.
package backend

import (
	"encoding/base64"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "image/jpeg"
	_ "image/png"

	"doc-anonymiser/backend/engine"
)

// importFixture loads one committed fixture the way an import does, so the test
// exercises the same Document the application holds.
func importFixture(t *testing.T, name string) *App {
	t.Helper()
	path := filepath.Join("testdata", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read the fixture %s: %v; generate it with "+
			"`go test -tags=integration ./backend/engine/convert/` and commit it", path, err)
	}
	docs, err := engine.LoadAll(name, raw)
	if err != nil {
		t.Fatalf("importing %s: %v", name, err)
	}
	app := NewApp()
	app.docs = docs
	return app
}

// TestListDocumentImagesThroughTheBoundLayer: one pass per format that HAS an
// image review, asserting the dispatch reaches the right scanner.
func TestListDocumentImagesThroughTheBoundLayer(t *testing.T) {
	cases := []struct {
		name       string
		fixture    string
		wantAssets int
		wantFirst  string
	}{
		{"extraction/images_bound_pptx", "images.pptx", 4, "ppt/media/image1.png"},
		{"extraction/images_bound_docx", "images.docx", 5, "word/media/image1.png"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			app := importFixture(t, c.fixture)
			inv, err := app.ListDocumentImages(c.fixture)
			if err != nil {
				t.Fatalf("ListDocumentImages(%s): %v", c.fixture, err)
			}
			if !inv.Applicable {
				t.Fatalf("%s must be applicable for image review", c.fixture)
			}
			if len(inv.Assets) != c.wantAssets {
				t.Fatalf("got %d assets, want %d", len(inv.Assets), c.wantAssets)
			}
			if inv.Assets[0].ID != c.wantFirst {
				t.Errorf("the first asset is %q, want %q: assets are listed in document order",
					inv.Assets[0].ID, c.wantFirst)
			}

			// The second call must come back from the cache, which is what the
			// repaint depends on. Equality of the answer is what can be
			// asserted here; the cache map itself is asserted in the unit test.
			again, err := app.ListDocumentImages(c.fixture)
			if err != nil {
				t.Fatalf("the second ListDocumentImages: %v", err)
			}
			if len(again.Assets) != len(inv.Assets) {
				t.Errorf("the cached answer holds %d assets, want the same %d",
					len(again.Assets), len(inv.Assets))
			}
		})
	}
}

// TestImageThumbnailThroughTheBoundLayer: a preview is a data URL whose payload
// decodes as an image, at or below the size asked for.
func TestImageThumbnailThroughTheBoundLayer(t *testing.T) {
	t.Run("extraction/images_bound_thumbnail", func(t *testing.T) {
		app := importFixture(t, "images.pptx")
		thumb, err := app.ImageThumbnail("images.pptx", "ppt/media/image1.png", 40)
		if err != nil {
			t.Fatalf("ImageThumbnail: %v", err)
		}
		const prefix = "data:image/png;base64,"
		if !strings.HasPrefix(thumb.DataURL, prefix) {
			t.Fatalf("the data URL starts %.40q, want the prefix %q", thumb.DataURL, prefix)
		}
		payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(thumb.DataURL, prefix))
		if err != nil {
			t.Fatalf("the data URL payload is not decodable base64: %v", err)
		}
		cfg, _, err := image.DecodeConfig(strings.NewReader(string(payload)))
		if err != nil {
			t.Fatalf("the preview does not decode as an image: %v", err)
		}
		if cfg.Width != thumb.Width || cfg.Height != thumb.Height {
			t.Errorf("the preview decodes as %dx%d and reports %dx%d; the layout reserves space "+
				"from the reported size", cfg.Width, cfg.Height, thumb.Width, thumb.Height)
		}
		if cfg.Width > 40 || cfg.Height > 40 {
			t.Errorf("the preview is %dx%d, want nothing above the 40 pixels asked for",
				cfg.Width, cfg.Height)
		}
	})

	t.Run("extraction/images_bound_svg_preview", func(t *testing.T) {
		app := importFixture(t, "images.pptx")
		thumb, err := app.ImageThumbnail("images.pptx", "ppt/media/image2.png", 40)
		if err != nil {
			t.Fatalf("ImageThumbnail of the SVG picture: %v", err)
		}
		if !strings.HasPrefix(thumb.DataURL, "data:image/svg+xml;base64,") {
			t.Fatalf("an SVG picture's preview starts %.40q, want an image/svg+xml data URL: the "+
				"vector is what the user sees, and it is rendered through an <img> tag",
				thumb.DataURL)
		}
	})
}

// TestExportsAreUntouchedByTheImageScan: this batch adds a way to LOOK at the
// pictures and changes nothing about what leaves the application. The same-format
// export of a scanned document must be byte for byte what it was.
func TestExportsAreUntouchedByTheImageScan(t *testing.T) {
	t.Run("roundtrip/images_scan_changes_no_bytes", func(t *testing.T) {
		app := importFixture(t, "images.pptx")
		before := append([]byte(nil), app.docs[0].Raw...)

		if _, err := app.ListDocumentImages("images.pptx"); err != nil {
			t.Fatalf("ListDocumentImages: %v", err)
		}
		if _, err := app.ImageThumbnail("images.pptx", "ppt/media/image1.png", 64); err != nil {
			t.Fatalf("ImageThumbnail: %v", err)
		}

		if string(app.docs[0].Raw) != string(before) {
			t.Error("scanning or previewing a document must not change the bytes captured at " +
				"import: they are what every export is produced from")
		}
	})
}

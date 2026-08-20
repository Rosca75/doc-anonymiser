// app_images_test.go — the bound image methods, unit tier.
//
// TIER: unit (docs/TESTING.md). Every case here is dispatch and cache
// behaviour over documents built in memory: which formats have an image review,
// what an unknown name answers, and when a cached scan stops being valid. The
// scan itself is tested in engine/imaging, and the two formats that HAVE a scan
// go through the bound layer one tier up, over the committed fixtures.
package backend

import (
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/engine/imaging"
)

// TestListDocumentImagesNotApplicable: a format with no image review answers
// with a reason CODE and no error. The screen has something true to say, and
// the sentence it says lives in the frontend's copy module.
func TestListDocumentImagesNotApplicable(t *testing.T) {
	cases := []struct {
		name       string
		format     engine.Format
		wantReason string
	}{
		{"config/images_pdf_already_removed", engine.FormatPDF, imaging.ReasonPDFImagesRemoved},
		{"config/images_txt_not_supported", engine.FormatTXT, imaging.ReasonFormatNotSupported},
		{"config/images_csv_not_supported", engine.FormatCSV, imaging.ReasonFormatNotSupported},
		{"config/images_xlsx_not_supported", engine.FormatXLSX, imaging.ReasonFormatNotSupported},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			app := NewApp()
			app.docs = []engine.Document{{Name: "file", Format: c.format, Markdown: "text"}}

			inv, err := app.ListDocumentImages("file")
			if err != nil {
				t.Fatalf("a format with no image review must not be an error, got %v", err)
			}
			if inv.Applicable {
				t.Fatalf("%s must not be applicable for image review", c.format)
			}
			if inv.Reason != c.wantReason {
				t.Errorf("the reason is %q, want %q", inv.Reason, c.wantReason)
			}
			if inv.Assets == nil {
				t.Error("the asset list must be empty rather than absent, so the interface never " +
					"has to distinguish null from empty")
			}
		})
	}
}

// TestListDocumentImagesUnknownDocument: an unknown name is an error that says
// what to do, in the shape the other bound methods use.
func TestListDocumentImagesUnknownDocument(t *testing.T) {
	t.Run("errors/images_unknown_document", func(t *testing.T) {
		app := NewApp()
		_, err := app.ListDocumentImages("nowhere.pptx")
		if err == nil {
			t.Fatal("listing the images of a document that is not imported must fail")
		}
		if !strings.Contains(err.Error(), "nowhere.pptx") {
			t.Errorf("the error is %q, and it must name the document asked for", err)
		}
		if !strings.Contains(err.Error(), "import it again") {
			t.Errorf("the error is %q, and it must say what to do about it", err)
		}
	})
}

// TestImageThumbnailUnknownAsset: an asset ID the inventory does not hold is an
// error naming both the picture and the document, because the usual cause is a
// list drawn before the document was re-imported.
func TestImageThumbnailUnknownAsset(t *testing.T) {
	t.Run("errors/images_unknown_asset", func(t *testing.T) {
		app := NewApp()
		app.docs = []engine.Document{{Name: "notes.txt", Format: engine.FormatTXT, Markdown: "text"}}

		_, err := app.ImageThumbnail("notes.txt", "word/media/image1.png", 64)
		if err == nil {
			t.Fatal("previewing a picture that is not in the document must fail")
		}
		if !strings.Contains(err.Error(), "word/media/image1.png") || !strings.Contains(err.Error(), "notes.txt") {
			t.Errorf("the error is %q, and it must name the picture and the document", err)
		}
	})
}

// TestImageScanCacheFollowsTheDocuments: the cached inventory describes bytes,
// so it must not outlive them. A stale scan would list the pictures of a file
// the user has replaced, and in the next batch a decision taken against it would
// be applied to the wrong picture.
func TestImageScanCacheFollowsTheDocuments(t *testing.T) {
	newApp := func() *App {
		app := NewApp()
		app.docs = []engine.Document{{Name: "notes.txt", Format: engine.FormatTXT, Markdown: "text"}}
		if _, err := app.ListDocumentImages("notes.txt"); err != nil {
			t.Fatalf("the first list must succeed: %v", err)
		}
		if len(app.imageScans) != 1 {
			t.Fatalf("the inventory must be cached after the first list, got %d entries", len(app.imageScans))
		}
		return app
	}

	t.Run("config/images_cache_dropped_on_remove", func(t *testing.T) {
		app := newApp()
		app.RemoveDocument("notes.txt")
		if len(app.imageScans) != 0 {
			t.Fatalf("removing a document must drop its cached scan, %d entries left", len(app.imageScans))
		}
		if _, err := app.ListDocumentImages("notes.txt"); err == nil {
			t.Error("after a removal the document is gone, so listing its images must fail rather " +
				"than answering from a cache")
		}
	})

	t.Run("config/images_cache_dropped_on_reimport", func(t *testing.T) {
		app := newApp()
		app.mu.Lock()
		app.upsertDocLocked(engine.Document{Name: "notes.txt", Format: engine.FormatPDF, Markdown: "text"})
		app.mu.Unlock()
		if len(app.imageScans) != 0 {
			t.Fatalf("re-importing a document must drop its cached scan, %d entries left", len(app.imageScans))
		}
		inv, err := app.ListDocumentImages("notes.txt")
		if err != nil {
			t.Fatalf("listing after a re-import: %v", err)
		}
		if inv.Reason != imaging.ReasonPDFImagesRemoved {
			t.Errorf("the reason is %q, want the re-imported document's own answer %q; a stale "+
				"cache answers for the file that was replaced", inv.Reason, imaging.ReasonPDFImagesRemoved)
		}
	})

	t.Run("config/images_cache_dropped_on_session_reset", func(t *testing.T) {
		app := newApp()
		if err := app.ResetSession(); err != nil {
			t.Fatalf("ResetSession: %v", err)
		}
		if len(app.imageScans) != 0 {
			t.Fatalf("a session reset must drop every cached scan, %d entries left", len(app.imageScans))
		}
	})

	t.Run("config/images_cache_survives_a_run_reset", func(t *testing.T) {
		app := newApp()
		app.ResetRun()
		if len(app.imageScans) != 1 {
			t.Errorf("a run reset must KEEP the cached scans: they describe the imported bytes, "+
				"which a run does not touch; %d entries left", len(app.imageScans))
		}
	})
}

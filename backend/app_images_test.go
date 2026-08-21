// app_images_test.go — the bound image methods, unit tier.
//
// TIER: unit (docs/TESTING.md). Every case here is dispatch and cache
// behaviour over documents built in memory: which formats have an image review,
// what an unknown name answers, and when a cached scan stops being valid. The
// scan itself is tested in engine/imaging, and the two formats that HAVE a scan
// go through the bound layer one tier up, over the committed fixtures.
package backend

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
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

// imagesApp is an App holding one imported .pptx built here, so the decision
// store can be exercised without a committed fixture.
//
// It is a deck rather than a document because the store does not care which, and
// one small archive keeps every case in this file at the unit tier.
func imagesApp(t *testing.T) *App {
	t.Helper()
	raw := onePicturePptx(t)
	app := NewApp()
	app.docs = []engine.Document{{
		Name:   "deck.pptx",
		Format: engine.FormatPPTX,
		Raw:    raw,
	}}
	return app
}

// onePicturePptx is a one-slide deck holding one PNG picture.
func onePicturePptx(t *testing.T) []byte {
	t.Helper()
	const ns = ` xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
		` xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"` +
		` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	slide := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<p:sld` + ns + `><p:cSld><p:spTree>` +
		`<p:pic><p:nvPicPr><p:cNvPr id="4" name="Alpine Trust logo"/></p:nvPicPr>` +
		`<p:blipFill><a:blip r:embed="rId1"/></p:blipFill>` +
		`<p:spPr><a:xfrm><a:ext cx="1828800" cy="1219200"/></a:xfrm></p:spPr></p:pic>` +
		`</p:spTree></p:cSld></p:sld>`

	img := image.NewRGBA(image.Rect(0, 0, 60, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 60; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 6), B: 90, A: 255})
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatalf("could not encode the picture: %v", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := []struct{ name, content string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Default Extension="png" ContentType="image/png"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`},
		{"ppt/slides/slide1.xml", slide},
		{"ppt/slides/_rels/slide1.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/image1.png"/>` +
			`</Relationships>`},
		{"ppt/media/image1.png", pngBuf.String()},
	}
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatalf("zip create %s: %v", e.name, err)
		}
		if _, err := w.Write([]byte(e.content)); err != nil {
			t.Fatalf("zip write %s: %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// TestSetImageDecisionStoresOnlyWhatChanged: a keep is the ABSENCE of a decision,
// so the store holds what the user changed and nothing else. Storing keeps would
// make "nothing was decided" indistinguishable from "everything was reviewed and
// kept" only by counting, which is the sort of distinction that goes wrong.
func TestSetImageDecisionStoresOnlyWhatChanged(t *testing.T) {
	t.Run("config/image_decision_keep_is_absence", func(t *testing.T) {
		app := imagesApp(t)
		const asset = "ppt/media/image1.png"

		if err := app.SetImageDecision("deck.pptx", asset,
			imaging.Decision{Treatment: imaging.TreatmentBox, BoxText: "Logo"}); err != nil {
			t.Fatalf("SetImageDecision(box): %v", err)
		}
		if got := app.imageDecisions["deck.pptx"][asset].Treatment; got != imaging.TreatmentBox {
			t.Fatalf("the stored treatment is %q, want box", got)
		}

		// And the inventory reports it, so the screen has one call rather than
		// two and cannot draw a row whose decision it has not read.
		inv, err := app.ListDocumentImages("deck.pptx")
		if err != nil {
			t.Fatalf("ListDocumentImages: %v", err)
		}
		if inv.Assets[0].Decision.BoxText != "Logo" {
			t.Errorf("the listed asset carries %+v, want the box text the user typed",
				inv.Assets[0].Decision)
		}

		if err := app.SetImageDecision("deck.pptx", asset,
			imaging.Decision{Treatment: imaging.TreatmentKeep}); err != nil {
			t.Fatalf("SetImageDecision(keep): %v", err)
		}
		if _, still := app.imageDecisions["deck.pptx"]; still {
			t.Errorf("keeping the only decided picture left an entry behind: %+v",
				app.imageDecisions)
		}
		inv, err = app.ListDocumentImages("deck.pptx")
		if err != nil {
			t.Fatalf("ListDocumentImages: %v", err)
		}
		if inv.Assets[0].Decision.Anonymises() {
			t.Errorf("the listed asset still reports %+v after being kept", inv.Assets[0].Decision)
		}
	})
}

// TestSetImageDecisionRefusesWhatTheAssetCannotCarry: the refusal has to reach
// the user beside the control that caused it, not at export time, when the only
// thing left to do about it is start again.
func TestSetImageDecisionRefusesWhatTheAssetCannotCarry(t *testing.T) {
	cases := []struct {
		name     string
		doc      string
		asset    string
		d        imaging.Decision
		wantSays string
	}{
		{
			name: "errors/image_decision_unknown_document",
			doc:  "missing.pptx", asset: "ppt/media/image1.png",
			d:        imaging.Decision{Treatment: imaging.TreatmentRemove},
			wantSays: "not imported",
		},
		{
			name: "errors/image_decision_unknown_asset",
			doc:  "deck.pptx", asset: "ppt/media/image9.png",
			d:        imaging.Decision{Treatment: imaging.TreatmentRemove},
			wantSays: "reopen the image list",
		},
		{
			name: "errors/image_decision_unknown_treatment",
			doc:  "deck.pptx", asset: "ppt/media/image1.png",
			d:        imaging.Decision{Treatment: "redact"},
			wantSays: "keep, box, blur, remove",
		},
		{
			name: "errors/image_decision_box_text_too_long",
			doc:  "deck.pptx", asset: "ppt/media/image1.png",
			d: imaging.Decision{
				Treatment: imaging.TreatmentBox,
				BoxText:   strings.Repeat("x", imaging.MaxBoxText+1),
			},
			wantSays: "shorten it",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			app := imagesApp(t)
			err := app.SetImageDecision(c.doc, c.asset, c.d)
			if err == nil {
				t.Fatalf("SetImageDecision(%+v) was accepted, want a refusal", c.d)
			}
			if !strings.Contains(err.Error(), c.wantSays) {
				t.Errorf("the refusal does not mention %q:\n%v", c.wantSays, err)
			}
			if len(app.imageDecisions) != 0 {
				t.Errorf("a refused decision was stored anyway: %+v", app.imageDecisions)
			}
		})
	}
}

// TestResetImageDecisions: the bulk "keep them all", and the refusal that makes a
// stale document name an error the caller can act on rather than a silent
// success.
func TestResetImageDecisions(t *testing.T) {
	t.Run("config/image_decisions_reset", func(t *testing.T) {
		app := imagesApp(t)
		if err := app.SetImageDecision("deck.pptx", "ppt/media/image1.png",
			imaging.Decision{Treatment: imaging.TreatmentRemove}); err != nil {
			t.Fatalf("SetImageDecision: %v", err)
		}
		if err := app.ResetImageDecisions("deck.pptx"); err != nil {
			t.Fatalf("ResetImageDecisions: %v", err)
		}
		if len(app.imageDecisions["deck.pptx"]) != 0 {
			t.Errorf("the decisions survived the reset: %+v", app.imageDecisions)
		}
	})

	t.Run("errors/image_decisions_reset_unknown_document", func(t *testing.T) {
		app := imagesApp(t)
		err := app.ResetImageDecisions("missing.pptx")
		if err == nil {
			t.Fatal("clearing the decisions of a document that is not imported was accepted; " +
				"a stale name must be an error, not a silent success")
		}
		if !strings.Contains(err.Error(), "not imported") {
			t.Errorf("the refusal does not say why:\n%v", err)
		}
	})
}

// TestPreviewImageTreatmentShowsTheRealTreatment: the preview runs the real
// treatment, so it cannot promise something the export does not do.
func TestPreviewImageTreatmentShowsTheRealTreatment(t *testing.T) {
	app := imagesApp(t)
	const asset = "ppt/media/image1.png"

	t.Run("redaction/preview_box_is_the_boxed_picture", func(t *testing.T) {
		kept, err := app.PreviewImageTreatment("deck.pptx", asset, imaging.Decision{}, 60)
		if err != nil {
			t.Fatalf("PreviewImageTreatment(keep): %v", err)
		}
		boxed, err := app.PreviewImageTreatment("deck.pptx", asset,
			imaging.Decision{Treatment: imaging.TreatmentBox, BoxText: "Logo"}, 60)
		if err != nil {
			t.Fatalf("PreviewImageTreatment(box): %v", err)
		}
		if boxed.DataURL == kept.DataURL {
			t.Error("the box preview is identical to the untreated one, so the user is " +
				"approving a redaction they cannot see")
		}
		if !strings.HasPrefix(boxed.DataURL, "data:image/png;base64,") {
			t.Errorf("the preview is not a PNG data URL: %.40s", boxed.DataURL)
		}
		if boxed.Width != 60 || boxed.Height != 40 {
			t.Errorf("the preview draws at %dx%d, want the picture's own 60x40 (it is already "+
				"below the requested 60 on its longest side, and previews never scale up)",
				boxed.Width, boxed.Height)
		}
	})

	t.Run("config/preview_records_nothing", func(t *testing.T) {
		if _, err := app.PreviewImageTreatment("deck.pptx", asset,
			imaging.Decision{Treatment: imaging.TreatmentRemove}, 60); err != nil {
			t.Fatalf("PreviewImageTreatment(remove): %v", err)
		}
		if len(app.imageDecisions) != 0 {
			t.Errorf("previewing a treatment recorded it: %+v; the preview is a question, not "+
				"an answer", app.imageDecisions)
		}
	})

	t.Run("errors/preview_refuses_an_impossible_treatment", func(t *testing.T) {
		if _, err := app.PreviewImageTreatment("deck.pptx", asset,
			imaging.Decision{Treatment: "redact"}, 60); err == nil {
			t.Fatal("previewing an unknown treatment was accepted; the preview and the export " +
				"must refuse the same things")
		}
	})
}

// TestResetSessionDropsImageDecisions: a clean sheet inherits nothing. The
// decisions deliberately survive a RE-IMPORT, because an asset ID is stable
// across one, and just as deliberately do not survive this.
func TestResetSessionDropsImageDecisions(t *testing.T) {
	t.Run("config/image_decisions_cleared_by_reset_session", func(t *testing.T) {
		app := imagesApp(t)
		if err := app.SetImageDecision("deck.pptx", "ppt/media/image1.png",
			imaging.Decision{Treatment: imaging.TreatmentRemove}); err != nil {
			t.Fatalf("SetImageDecision: %v", err)
		}
		if err := app.ResetSession(); err != nil {
			t.Fatalf("ResetSession: %v", err)
		}
		if len(app.imageDecisions) != 0 {
			t.Errorf("the picture decisions survived a session reset: %+v", app.imageDecisions)
		}
	})
}

// TestImagePlanForNeedsDecisions: with nothing decided the exporter is handed an
// EMPTY plan, so a document nobody reviewed exports exactly as it did before this
// pass existed and the scan is never paid for.
func TestImagePlanForNeedsDecisions(t *testing.T) {
	t.Run("config/image_plan_empty_without_decisions", func(t *testing.T) {
		app := imagesApp(t)
		if plan := app.imagePlanFor("deck.pptx"); !plan.Empty() || len(plan.Inventory.Assets) != 0 {
			t.Errorf("a document with no decisions produced a plan carrying %d assets; it must "+
				"be empty, and the scan must not be run for it", len(plan.Inventory.Assets))
		}

		if err := app.SetImageDecision("deck.pptx", "ppt/media/image1.png",
			imaging.Decision{Treatment: imaging.TreatmentBlur, BlurStrength: 4}); err != nil {
			t.Fatalf("SetImageDecision: %v", err)
		}
		plan := app.imagePlanFor("deck.pptx")
		if plan.Empty() {
			t.Fatal("a decided picture produced an empty plan, so the export would ignore it")
		}
		if len(plan.Inventory.Assets) != 1 {
			t.Errorf("the plan carries %d assets, want the document's 1", len(plan.Inventory.Assets))
		}
		if got := plan.Summary(); got.Blurred != 1 {
			t.Errorf("the plan's summary is %+v, want 1 blurred", got)
		}
	})
}

// TestImageDecisionsSurviveTheSession: the App's half of the session round trip.
// Without it a restored session exports every picture as it came in, silently,
// while the screen the user saved from said they were anonymised.
func TestImageDecisionsSurviveTheSession(t *testing.T) {
	t.Run("roundtrip/image_decisions_through_the_app", func(t *testing.T) {
		app := imagesApp(t)
		const asset = "ppt/media/image1.png"
		want := imaging.Decision{Treatment: imaging.TreatmentBlur, BlurStrength: 7}
		if err := app.SetImageDecision("deck.pptx", asset, want); err != nil {
			t.Fatalf("SetImageDecision: %v", err)
		}

		saved, err := engine.SaveSession(engine.Session{
			Settings:       engine.SessionSettings{Presets: depthPresets(engine.PresetStandard), OllamaPort: 11434},
			ImageDecisions: app.imageDecisionsSnapshot(),
		})
		if err != nil {
			t.Fatalf("SaveSession: %v", err)
		}
		loaded, err := engine.LoadSession(saved)
		if err != nil {
			t.Fatalf("LoadSession: %v", err)
		}

		fresh := imagesApp(t)
		if _, err := fresh.applyRestoredSession(loaded); err != nil {
			t.Fatalf("applyRestoredSession: %v", err)
		}
		if got := fresh.imageDecisions["deck.pptx"][asset]; got != want {
			t.Errorf("the restored decision is %+v, want %+v", got, want)
		}

		// The App must hold its OWN maps, or editing a decision after a load
		// would reach into the session struct the caller still holds.
		if err := fresh.SetImageDecision("deck.pptx", asset,
			imaging.Decision{Treatment: imaging.TreatmentRemove}); err != nil {
			t.Fatalf("SetImageDecision after the load: %v", err)
		}
		if loaded.ImageDecisions["deck.pptx"][asset] != want {
			t.Errorf("changing a restored decision reached back into the loaded session: %+v",
				loaded.ImageDecisions["deck.pptx"][asset])
		}
	})

	t.Run("roundtrip/image_decisions_absent_restores_as_none", func(t *testing.T) {
		app := imagesApp(t)
		if err := app.SetImageDecision("deck.pptx", "ppt/media/image1.png",
			imaging.Decision{Treatment: imaging.TreatmentRemove}); err != nil {
			t.Fatalf("SetImageDecision: %v", err)
		}
		// A session file written before anyone reviewed a picture says nothing
		// about them, and loading it must CLEAR what the live session held: a
		// restore that kept the current decisions would apply them to a
		// configuration the user did not save.
		if _, err := app.applyRestoredSession(engine.Session{
			Version:  engine.SessionVersion,
			Settings: engine.SessionSettings{Presets: depthPresets(engine.PresetStandard), OllamaPort: 11434},
		}); err != nil {
			t.Fatalf("applyRestoredSession: %v", err)
		}
		if len(app.imageDecisions) != 0 {
			t.Errorf("restoring a session with no picture decisions left %+v behind",
				app.imageDecisions)
		}
	})
}

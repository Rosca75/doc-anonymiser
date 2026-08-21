// app_export_test.go — the export-layer bound methods' guards.
//
// The Wails runtime refuses a context it was not given by a lifecycle hook, and
// there is none headless, so the clipboard write itself cannot be exercised
// here. What CAN be exercised, and is what matters, is the guard in front of it:
// an empty or over-long input must be refused with a sentence that says what to
// do, before anything reaches the runtime. That is why the guard is its own
// function.
package backend

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/engine/imaging"
)

// TestCopyTextRejectsAnEmptySelection: nothing to copy is a mistake worth
// naming, not a silent no-op. A silent success would read as a clipboard that
// stopped working.
func TestCopyTextRejectsAnEmptySelection(t *testing.T) {
	for _, text := range []string{"", "   ", "\n\t"} {
		err := validateCopyText(text)
		if err == nil {
			t.Errorf("CopyText(%q) must be refused", text)
			continue
		}
		if !strings.Contains(err.Error(), "select some text") {
			t.Errorf("the message must say what to do, got: %v", err)
		}
	}
}

// TestCopyTextRejectsAnOverLongSelection: the cap is a MIS-DRAG guard. The
// panel copies a value out of a preview, and a drag that ran away down the pane
// would otherwise push a whole document through the clipboard.
func TestCopyTextRejectsAnOverLongSelection(t *testing.T) {
	err := validateCopyText(strings.Repeat("x", maxCopyTextBytes+1))
	if err == nil {
		t.Fatal("a selection past the cap must be refused")
	}
	for _, want := range []string{"too long", "single value"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message must mention %q, got: %v", want, err)
		}
	}
	// The length is named, because "too long" without a number is not
	// actionable: the user cannot tell how much to trim.
	if !strings.Contains(err.Error(), "characters") {
		t.Errorf("the message must say how long the selection was, got: %v", err)
	}
}

// TestCopyTextAcceptsANormalSelection: a value-sized string passes the guard,
// and so does one exactly at the cap. The bound is inclusive.
func TestCopyTextAcceptsANormalSelection(t *testing.T) {
	if err := validateCopyText("Marie Duval"); err != nil {
		t.Errorf("a normal selection must pass the guard, got: %v", err)
	}
	if err := validateCopyText(strings.Repeat("x", maxCopyTextBytes)); err != nil {
		t.Errorf("the cap is inclusive, got: %v", err)
	}
}

// --- The report's picture section ------------------------------------------

// TestReportNamesTheAnonymisedPictures: the exported report says what happened
// to the pictures, because a report that describes a document as anonymised
// without mentioning them is read as covering them.
//
// The anonymised pictures are LISTED and the kept ones counted, so the two cases
// here are a document with a mix of treatments and one with pictures nobody
// decided anything about.
func TestReportNamesTheAnonymisedPictures(t *testing.T) {
	t.Run("roundtrip/report_names_the_anonymised_pictures", func(t *testing.T) {
		app := reportImagesApp(t)
		if err := app.SetImageDecision("deck.pptx", "ppt/media/image1.png", imaging.Decision{
			Treatment: imaging.TreatmentBox, BoxText: "Client logo removed",
		}); err != nil {
			t.Fatalf("SetImageDecision: %v", err)
		}
		if err := app.SetImageDecision("deck.pptx", "ppt/media/image2.png", imaging.Decision{
			Treatment: imaging.TreatmentRemove,
		}); err != nil {
			t.Fatalf("SetImageDecision: %v", err)
		}

		sections := app.imageReports([]string{"deck.pptx", "quiet.pptx", "notes.txt"})
		if len(sections) != 2 {
			t.Fatalf("the report carries %d picture sections, want 2: the two decks. A .txt file "+
				"has no pictures, and a section saying so on every text document would bury the "+
				"decks that do. Got %+v", len(sections), sections)
		}

		reviewed := sections[0]
		if reviewed.Document != "deck.pptx" {
			t.Errorf("the first section is %q, want deck.pptx: the sections follow the run's own "+
				"document order", reviewed.Document)
		}
		if reviewed.Kept != 1 {
			t.Errorf("the reviewed deck reports %d kept pictures, want its 1 undecided one",
				reviewed.Kept)
		}
		if len(reviewed.Anonymised) != 2 {
			t.Fatalf("the reviewed deck lists %d anonymised pictures, want 2", len(reviewed.Anonymised))
		}

		boxed := reviewed.Anonymised[0]
		if boxed.Name != "Alpine Trust logo" || boxed.Treatment != string(imaging.TreatmentBox) {
			t.Errorf("the first anonymised picture is %q treated %q, want the boxed Alpine Trust logo",
				boxed.Name, boxed.Treatment)
		}
		if boxed.BoxText != "Client logo removed" {
			t.Errorf("the box text is %q, want %q: it is the user's own sentence and the only part "+
				"of a treatment that cannot be read off the treatment's name",
				boxed.BoxText, "Client logo removed")
		}
		if got := strings.Join(boxed.Locations, ", "); got != "Slide 1, Slide 2" {
			t.Errorf("the boxed picture's locations are %q, want \"Slide 1, Slide 2\": one decision "+
				"covers every place the picture appears, so the report names them all", got)
		}
		if removed := reviewed.Anonymised[1]; removed.BoxText != "" {
			t.Errorf("the removed picture reports a box text (%q); only a box draws one, and "+
				"reporting it would describe a rectangle the export never writes", removed.BoxText)
		}

		quiet := sections[1]
		if quiet.Kept != 2 || len(quiet.Anonymised) != 0 {
			t.Errorf("the undecided deck reports %d kept and %d anonymised, want 2 and 0: the count "+
				"is what tells a reader \"no pictures\" from \"pictures, all kept\"",
				quiet.Kept, len(quiet.Anonymised))
		}
	})
}

// TestReportBytesCarryThePictureSection: both exported shapes carry it. The
// markdown is what a human reads and the JSON is what a machine reads, and a
// section present in one and absent from the other is a report that contradicts
// itself.
func TestReportBytesCarryThePictureSection(t *testing.T) {
	t.Run("roundtrip/report_bytes_carry_the_picture_section", func(t *testing.T) {
		app := reportImagesApp(t)
		if err := app.SetImageDecision("deck.pptx", "ppt/media/image1.png", imaging.Decision{
			Treatment: imaging.TreatmentBlur, BlurStrength: 7,
		}); err != nil {
			t.Fatalf("SetImageDecision: %v", err)
		}
		app.results = &engine.Results{
			Documents: []engine.ResultDocument{{Name: "deck.pptx"}, {Name: "notes.txt"}},
			Report:    engine.Report{Presets: depthPresets(engine.PresetStandard)},
		}

		md, name, err := app.reportBytes("md")
		if err != nil {
			t.Fatalf("the markdown report failed: %v", err)
		}
		if name != "anonymisation_report.md" {
			t.Errorf("the markdown report is named %q", name)
		}
		text := string(md)
		for _, want := range []string{"## Pictures", "### deck.pptx", "Alpine Trust logo", "blur"} {
			if !strings.Contains(text, want) {
				t.Errorf("the markdown report does not contain %q; it reads:\n%s", want, text)
			}
		}
		if strings.Contains(text, "### notes.txt") {
			t.Error("the markdown report opens a picture section for a text document, which has no " +
				"pictures to report on")
		}

		data, name, err := app.reportBytes("json")
		if err != nil {
			t.Fatalf("the JSON report failed: %v", err)
		}
		if name != "anonymisation_report.json" {
			t.Errorf("the JSON report is named %q", name)
		}
		var payload struct {
			Presets map[string]string `json:"presets"`
			Images  []struct {
				Document   string `json:"document"`
				Kept       int    `json:"kept"`
				Anonymised []struct {
					Name      string   `json:"name"`
					Treatment string   `json:"treatment"`
					Locations []string `json:"locations"`
				} `json:"anonymised"`
			} `json:"images"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("the JSON report does not parse: %v", err)
		}
		wantRow := engine.PresetKey(engine.ScopePatterns, engine.FamilyDepth)
		if payload.Presets[wantRow] != engine.PresetStandard {
			t.Errorf("the JSON report's presets are %v, want %s on the %s row: the engine's "+
				"report is EMBEDDED, so every field a reader of an earlier report knows stays "+
				"where it was", payload.Presets, engine.PresetStandard, wantRow)
		}
		if len(payload.Images) != 1 || len(payload.Images[0].Anonymised) != 1 {
			t.Fatalf("the JSON report's picture sections are %+v, want one deck with one "+
				"anonymised picture", payload.Images)
		}
		if got := payload.Images[0].Anonymised[0].Treatment; got != string(imaging.TreatmentBlur) {
			t.Errorf("the JSON report names the treatment %q, want %q", got, imaging.TreatmentBlur)
		}
	})
}

// TestReportBytesWithoutPicturesIsUnchanged: a batch of text files produces the
// report it always did. A heading over nothing is worse than no heading: it
// reads as a feature that ran and found nothing to do.
func TestReportBytesWithoutPicturesIsUnchanged(t *testing.T) {
	t.Run("roundtrip/report_without_pictures_has_no_section", func(t *testing.T) {
		app := NewApp()
		app.docs = []engine.Document{{Name: "notes.txt", Format: engine.FormatTXT, Markdown: "text"}}
		app.results = &engine.Results{
			Documents: []engine.ResultDocument{{Name: "notes.txt"}},
			Report:    engine.Report{Presets: depthPresets(engine.PresetStandard)},
		}

		md, _, err := app.reportBytes("md")
		if err != nil {
			t.Fatalf("the markdown report failed: %v", err)
		}
		if strings.Contains(string(md), "Pictures") {
			t.Errorf("a batch with no pictures produced a picture section:\n%s", md)
		}
		if got := string(md); got != app.results.Report.ToMarkdown() {
			t.Errorf("the markdown report is not the engine's own report:\n%s", got)
		}

		data, _, err := app.reportBytes("json")
		if err != nil {
			t.Fatalf("the JSON report failed: %v", err)
		}
		if strings.Contains(string(data), "\"images\"") {
			t.Errorf("a batch with no pictures produced an images key:\n%s", data)
		}
		engineJSON, err := app.results.Report.ToJSON()
		if err != nil {
			t.Fatalf("the engine's own report failed to serialise: %v", err)
		}
		if !bytes.Equal(data, engineJSON) {
			t.Errorf("the JSON report is not byte for byte the engine's own report:\n%s", data)
		}
	})
}

// TestReportBytesRefusesAnUnknownFormat: the refusal names what was expected,
// because the caller is the frontend and the fix is one of two strings.
func TestReportBytesRefusesAnUnknownFormat(t *testing.T) {
	t.Run("errors/report_unknown_format", func(t *testing.T) {
		app := NewApp()
		app.results = &engine.Results{}
		if _, _, err := app.reportBytes("pdf"); err == nil ||
			!strings.Contains(err.Error(), "expected json or md") {
			t.Errorf("the refusal is %v, and it must name the two formats that work", err)
		}
	})
}

// TestSameFormatMetadataCountsThePictures: the export screen's sentence.
//
// The all-kept case is the one that matters: a user who never opened the IMAGE
// tab has decided nothing, and this call is what tells them, at the moment the
// file is written, that the pictures are going out as they came in.
func TestSameFormatMetadataCountsThePictures(t *testing.T) {
	t.Run("roundtrip/same_format_meta_counts_the_pictures", func(t *testing.T) {
		app := reportImagesApp(t)

		summary, ok := app.imageSummaryFor("deck.pptx")
		if !ok {
			t.Fatal("a deck with pictures must answer with a count, or the review panel says nothing")
		}
		if summary.Kept != 3 || summary.Anonymised() != 0 {
			t.Errorf("the untouched deck counts %+v, want its 3 pictures kept", summary)
		}

		if err := app.SetImageDecision("deck.pptx", "ppt/media/image2.png", imaging.Decision{
			Treatment: imaging.TreatmentRemove,
		}); err != nil {
			t.Fatalf("SetImageDecision: %v", err)
		}
		summary, _ = app.imageSummaryFor("deck.pptx")
		if summary.Removed != 1 || summary.Kept != 2 {
			t.Errorf("after one removal the deck counts %+v, want 1 removed and 2 kept", summary)
		}
	})
}

// TestSameFormatMetadataSaysNothingWithoutPictures: a format with no image
// review answers false, so the panel renders no line at all. A line reading
// "0 images" on a PDF would contradict the IMAGE tab, which says a PDF export
// has already dropped every picture.
func TestSameFormatMetadataSaysNothingWithoutPictures(t *testing.T) {
	cases := []struct {
		name   string
		format engine.Format
	}{
		{"config/same_format_meta_silent_on_pdf", engine.FormatPDF},
		{"config/same_format_meta_silent_on_xlsx", engine.FormatXLSX},
		{"config/same_format_meta_silent_on_txt", engine.FormatTXT},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			app := NewApp()
			app.docs = []engine.Document{{Name: "file", Format: c.format, Markdown: "text"}}
			if _, ok := app.imageSummaryFor("file"); ok {
				t.Errorf("%s has no image review, so it must answer with no count at all", c.format)
			}
		})
	}
}

// reportImagesApp is an App holding two imported decks: one with three pictures,
// the first of which is used on two slides, and a second with two pictures that
// nobody decides anything about. A .txt document sits beside them, because
// "which documents get a section" is half of what the report has to get right.
func reportImagesApp(t *testing.T) *App {
	t.Helper()
	app := NewApp()
	app.docs = []engine.Document{
		{Name: "deck.pptx", Format: engine.FormatPPTX, Raw: threePicturePptx(t)},
		{Name: "quiet.pptx", Format: engine.FormatPPTX, Raw: twoPicturePptx(t)},
		{Name: "notes.txt", Format: engine.FormatTXT, Markdown: "text"},
	}
	return app
}

// threePicturePptx is a two-slide deck: the logo appears on both slides (one
// asset, two occurrences), and each slide carries one picture of its own.
func threePicturePptx(t *testing.T) []byte {
	t.Helper()
	return buildPictureDeck(t, [][]deckPicture{
		{{name: "Alpine Trust logo", rID: "rId1"}, {name: "Schéma Borealis", rID: "rId2"}},
		{{name: "Alpine Trust logo", rID: "rId1"}, {name: "Photo équipe", rID: "rId3"}},
	}, map[string][]string{
		"slide1": {"rId1:../media/image1.png", "rId2:../media/image2.png"},
		"slide2": {"rId1:../media/image1.png", "rId3:../media/image3.png"},
	})
}

// twoPicturePptx is a one-slide deck with two pictures and no shared asset.
func twoPicturePptx(t *testing.T) []byte {
	t.Helper()
	return buildPictureDeck(t, [][]deckPicture{
		{{name: "Cover", rID: "rId1"}, {name: "Organigramme", rID: "rId2"}},
	}, map[string][]string{
		"slide1": {"rId1:../media/image1.png", "rId2:../media/image2.png"},
	})
}

// deckPicture is one picture shape to write into a slide.
type deckPicture struct {
	name string
	rID  string
}

// buildPictureDeck assembles a minimal .pptx from a list of slides and their
// relationships.
//
// It builds the archive rather than reading a committed fixture so these cases
// stay at the unit tier: what is under test is which documents get a section and
// what a section says, not real-format behaviour, which the committed fixtures
// cover one tier up.
//
// @param slides one entry per slide, holding that slide's picture shapes in order
// @param rels per slide ("slide1"), the "rId:target" pairs its pictures point at
func buildPictureDeck(t *testing.T, slides [][]deckPicture, rels map[string][]string) []byte {
	t.Helper()
	const ns = ` xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
		` xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"` +
		` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`

	entries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Default Extension="png" ContentType="image/png"/></Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`,
	}

	media := map[string]bool{}
	for i, pictures := range slides {
		slide := fmt.Sprintf("slide%d", i+1)
		body := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<p:sld` + ns + `><p:cSld><p:spTree>`
		for _, pic := range pictures {
			body += `<p:pic><p:nvPicPr><p:cNvPr id="4" name="` + pic.name + `"/></p:nvPicPr>` +
				`<p:blipFill><a:blip r:embed="` + pic.rID + `"/></p:blipFill>` +
				`<p:spPr><a:xfrm><a:ext cx="1828800" cy="1219200"/></a:xfrm></p:spPr></p:pic>`
		}
		entries["ppt/slides/"+slide+".xml"] = body + `</p:spTree></p:cSld></p:sld>`

		relsXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
		for _, pair := range rels[slide] {
			id, target, ok := strings.Cut(pair, ":")
			if !ok {
				t.Fatalf("the relationship %q is not in \"rId:target\" form", pair)
			}
			relsXML += `<Relationship Id="` + id + `" Type="http://schemas.openxmlformats.org/` +
				`officeDocument/2006/relationships/image" Target="` + target + `"/>`
			media[strings.TrimPrefix(target, "../")] = true
		}
		entries["ppt/slides/_rels/"+slide+".xml.rels"] = relsXML + `</Relationships>`
	}
	for part := range media {
		entries["ppt/"+part] = deckPicturePNG(t)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// deckPicturePNG is a small PNG with detail in it, so a treatment applied to it
// cannot come back byte-identical.
func deckPicturePNG(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 6), G: uint8(y * 8), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("could not encode the deck's picture: %v", err)
	}
	return buf.String()
}

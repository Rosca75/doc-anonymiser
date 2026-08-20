// engine/exportfmt/images_test.go — the picture pass, unit tier.
//
// TIER: unit (docs/TESTING.md). Every case assembles a tiny archive in memory,
// exports it and reads the result back. No committed fixture is opened and no
// file is written; the format round-trips over the real fixtures are in
// images_integration_test.go, and the treatments themselves are tested in
// engine/imaging.
//
// What these own is the pass's own rules: an empty plan changes nothing, a
// removal deletes exactly one element and leaves its neighbours alone, and a crop
// is dropped for the treatments that redraw the picture and kept for the one that
// does not.
package exportfmt

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine/imaging"
)

// detailedPNG is a real picture with real DETAIL in it, because the treatments
// decode what they are given and a flat colour would blur to itself: a fixture a
// blur cannot change proves nothing about the blur.
func detailedPNG(t *testing.T, w, h int, tint color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((int(tint.R) + x*3) % 256),
				G: uint8((int(tint.G) + y*5) % 256),
				B: uint8((int(tint.B) + x*y) % 256),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("could not encode the picture fixture: %v", err)
	}
	return buf.Bytes()
}

// picturesPptx builds a one-slide deck with two pictures, the first of them
// CROPPED. Two pictures in one part is the case that matters: a removal has to
// take one element and leave the other exactly where it was.
func picturesPptx(t *testing.T) []byte {
	t.Helper()
	const ns = ` xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
		` xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"` +
		` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`

	pic := func(name, rID, crop string) string {
		return `<p:pic><p:nvPicPr><p:cNvPr id="4" name="` + name + `"/></p:nvPicPr>` +
			`<p:blipFill><a:blip r:embed="` + rID + `"/>` + crop +
			`<a:stretch><a:fillRect/></a:stretch></p:blipFill>` +
			`<p:spPr><a:xfrm><a:ext cx="1828800" cy="1219200"/></a:xfrm></p:spPr></p:pic>`
	}
	slide := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<p:sld` + ns + `><p:cSld><p:spTree>` +
		`<p:sp><p:txBody><a:p><a:r><a:t>Alpine Trust review</a:t></a:r></a:p></p:txBody></p:sp>` +
		pic("Alpine Trust logo", "rId1", `<a:srcRect l="10000" t="5000"/>`) +
		pic("Site photo", "rId2", "") +
		`</p:spTree></p:cSld></p:sld>`

	return buildZip(t, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Default Extension="png" ContentType="image/png"/></Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`,
		"ppt/slides/slide1.xml": slide,
		"ppt/slides/_rels/slide1.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/image1.png"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/image2.png"/>` +
			`</Relationships>`,
		"ppt/media/image1.png": string(detailedPNG(t, 120, 80, color.RGBA{R: 220, G: 40, B: 40, A: 255})),
		"ppt/media/image2.png": string(detailedPNG(t, 60, 60, color.RGBA{R: 40, G: 180, B: 90, A: 255})),
	})
}

// planFor scans the deck and attaches the given decisions, exactly as the bound
// layer does: a plan whose inventory came from anywhere else could name an
// occurrence the archive does not have.
func planFor(t *testing.T, raw []byte, decisions map[string]imaging.Decision) ImagePlan {
	t.Helper()
	inv, err := imaging.ScanPptx(raw)
	if err != nil {
		t.Fatalf("the fixture does not scan: %v", err)
	}
	return ImagePlan{Inventory: inv, Decisions: decisions}
}

// zipEntry reads one entry out of an archive.
func zipEntry(t *testing.T, raw []byte, name string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("the produced file is not a zip archive: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("could not open %q: %v", name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("could not read %q: %v", name, err)
		}
		return data
	}
	t.Fatalf("the produced archive has no entry %q", name)
	return nil
}

// parses reports whether a part is still well-formed XML, which is the thing a
// byte-range deletion can break.
func parses(data []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		if _, err := dec.Token(); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// TestExportWithNoDecisionsIsUnchanged is the regression guard for every existing
// user: a document nobody reviewed must export exactly as it did before this pass
// existed, byte for byte.
func TestExportWithNoDecisionsIsUnchanged(t *testing.T) {
	raw := picturesPptx(t)

	cases := []struct {
		name string
		plan ImagePlan
	}{
		{"roundtrip/images_no_decisions_zero_plan", ImagePlan{}},
		{
			// The inventory is present and every asset is left at keep, which is
			// what the screen hands over once a user has opened the tab and
			// changed nothing.
			name: "roundtrip/images_no_decisions_all_kept",
			plan: planFor(t, raw, map[string]imaging.Decision{
				"ppt/media/image1.png": {Treatment: imaging.TreatmentKeep},
			}),
		},
	}

	baseline, _, _, err := ExportPptx(raw, testConfig())
	if err != nil {
		t.Fatalf("ExportPptx without a plan: %v", err)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.Images = c.plan
			out, _, summary, err := ExportPptx(raw, cfg)
			if err != nil {
				t.Fatalf("ExportPptx: %v", err)
			}
			if !bytes.Equal(out, baseline) {
				t.Error("an export that changes no picture is not byte-identical to one with " +
					"no plan at all; every existing user's output has moved")
			}
			if summary.Anonymised() != 0 {
				t.Errorf("the summary reports %d anonymised pictures, want 0: %+v",
					summary.Anonymised(), summary)
			}
		})
	}
}

// TestExportRemovesTheElementAndTheBytes: removal is ONE action with two halves,
// and neither is optional. The element goes so nothing draws it, and the bytes go
// because an orphan picture part inside the zip is a leak that looks like a
// redaction.
func TestExportRemovesTheElementAndTheBytes(t *testing.T) {
	t.Run("roundtrip/images_remove_element_and_bytes", func(t *testing.T) {
		raw := picturesPptx(t)
		cfg := testConfig()
		cfg.Images = planFor(t, raw, map[string]imaging.Decision{
			"ppt/media/image1.png": {Treatment: imaging.TreatmentRemove},
		})

		out, _, summary, err := ExportPptx(raw, cfg)
		if err != nil {
			t.Fatalf("ExportPptx: %v", err)
		}
		if summary.Removed != 1 || summary.Kept != 1 {
			t.Errorf("the summary is %+v, want 1 removed and 1 kept", summary)
		}

		slide := zipEntry(t, out, "ppt/slides/slide1.xml")
		if err := parses(slide); err != nil {
			t.Fatalf("the slide no longer parses after the deletion: %v\n%s", err, slide)
		}
		got := string(slide)
		if strings.Contains(got, `name="Alpine Trust logo"`) {
			t.Error("the removed picture's element is still in the slide, so the file still " +
				"draws it")
		}
		// The neighbour, and the text beside it, must be exactly where they were.
		// A back-to-front splice bug shows up here and nowhere else.
		if !strings.Contains(got, `name="Site photo"`) {
			t.Error("the picture that was KEPT went with the removal; the splice took the " +
				"wrong range")
		}
		if !strings.Contains(got, "Alpine Trust review") {
			t.Error("the slide's text did not survive the picture deletion")
		}

		// The bytes: overwritten for the removed picture, untouched for the kept
		// one.
		removedBytes := zipEntry(t, out, "ppt/media/image1.png")
		if bytes.Equal(removedBytes, zipEntry(t, raw, "ppt/media/image1.png")) {
			t.Error("the removed picture's bytes are still in the archive; an orphan part is " +
				"exactly the leak this rule exists to prevent")
		}
		if !bytes.Equal(zipEntry(t, out, "ppt/media/image2.png"), zipEntry(t, raw, "ppt/media/image2.png")) {
			t.Error("the kept picture's bytes were rewritten; only what the user decided on " +
				"may change")
		}
	})
}

// TestExportStripsTheCropOnlyWhenItRedraws: a source rectangle CROPS the picture
// inside its frame. A crop of a replacement box would show one corner of the
// rectangle with the caption outside the frame, so box and blur drop it; keep
// changes nothing at all and must leave it alone.
func TestExportStripsTheCropOnlyWhenItRedraws(t *testing.T) {
	cases := []struct {
		name      string
		decision  imaging.Decision
		wantCrop  bool
		wantBytes bool // whether the media part must have been rewritten
	}{
		{
			name:     "roundtrip/images_srcrect_stripped_for_box",
			decision: imaging.Decision{Treatment: imaging.TreatmentBox, BoxText: "Client logo"},
			wantCrop: false, wantBytes: true,
		},
		{
			name:     "roundtrip/images_srcrect_stripped_for_blur",
			decision: imaging.Decision{Treatment: imaging.TreatmentBlur, BlurStrength: 8},
			wantCrop: false, wantBytes: true,
		},
		{
			name:     "roundtrip/images_srcrect_kept_for_keep",
			decision: imaging.Decision{Treatment: imaging.TreatmentKeep},
			wantCrop: true, wantBytes: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := picturesPptx(t)
			cfg := testConfig()
			cfg.Images = planFor(t, raw, map[string]imaging.Decision{
				"ppt/media/image1.png": c.decision,
			})
			out, _, _, err := ExportPptx(raw, cfg)
			if err != nil {
				t.Fatalf("ExportPptx: %v", err)
			}

			slide := string(zipEntry(t, out, "ppt/slides/slide1.xml"))
			if err := parses([]byte(slide)); err != nil {
				t.Fatalf("the slide no longer parses: %v\n%s", err, slide)
			}
			if hasCrop := strings.Contains(slide, "srcRect"); hasCrop != c.wantCrop {
				t.Errorf("the crop is present = %v, want %v; the slide reads:\n%s",
					hasCrop, c.wantCrop, slide)
			}
			// Everything else in the element survives: the pass touches the crop
			// and nothing else.
			if !strings.Contains(slide, `name="Alpine Trust logo"`) ||
				!strings.Contains(slide, `r:embed="rId1"`) ||
				!strings.Contains(slide, `cx="1828800"`) {
				t.Errorf("the picture element lost something other than its crop:\n%s", slide)
			}

			changed := !bytes.Equal(
				zipEntry(t, out, "ppt/media/image1.png"),
				zipEntry(t, raw, "ppt/media/image1.png"))
			if changed != c.wantBytes {
				t.Errorf("the picture's bytes were rewritten = %v, want %v", changed, c.wantBytes)
			}
		})
	}
}

// TestExportTreatsTheCompanionSVG: an SVG picture is a PNG fallback plus the SVG
// itself, both parts carry the picture, and Office draws whichever one it can. A
// treatment that reached only one of them means a boxed logo comes back sharp on
// the machine that renders the other.
func TestExportTreatsTheCompanionSVG(t *testing.T) {
	t.Run("roundtrip/images_companion_svg_is_treated_too", func(t *testing.T) {
		const ns = ` xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
			` xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"` +
			` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"` +
			` xmlns:asvg="http://schemas.microsoft.com/office/drawing/2016/SVG/main"`
		slide := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<p:sld` + ns + `><p:cSld><p:spTree>` +
			`<p:pic><p:nvPicPr><p:cNvPr id="4" name="Schema Borealis"/></p:nvPicPr>` +
			`<p:blipFill><a:blip r:embed="rId1"><a:extLst>` +
			`<a:ext uri="{96DAC541-7B7A-43D3-8B79-37D633B846F1}"><asvg:svgBlip r:embed="rId2"/></a:ext>` +
			`</a:extLst></a:blip></p:blipFill>` +
			`<p:spPr><a:xfrm><a:ext cx="1828800" cy="1219200"/></a:xfrm></p:spPr></p:pic>` +
			`</p:spTree></p:cSld></p:sld>`
		raw := buildZip(t, map[string]string{
			"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
				`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
				`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
				`<Default Extension="xml" ContentType="application/xml"/>` +
				`<Default Extension="png" ContentType="image/png"/>` +
				`<Default Extension="svg" ContentType="image/svg+xml"/></Types>`,
			"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
				`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`,
			"ppt/slides/slide1.xml": slide,
			"ppt/slides/_rels/slide1.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
				`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
				`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/image1.png"/>` +
				`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/image2.svg"/>` +
				`</Relationships>`,
			"ppt/media/image1.png": string(detailedPNG(t, 300, 150, color.RGBA{R: 31, G: 119, B: 180, A: 255})),
			"ppt/media/image2.svg": `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" ` +
				`width="300" height="150"><text x="10" y="20">pierre.dupont@tpps.com</text></svg>`,
		})

		cfg := testConfig()
		cfg.Images = planFor(t, raw, map[string]imaging.Decision{
			"ppt/media/image1.png": {Treatment: imaging.TreatmentBox, BoxText: "Diagram removed"},
		})
		out, _, summary, err := ExportPptx(raw, cfg)
		if err != nil {
			t.Fatalf("ExportPptx: %v", err)
		}
		if summary.Boxed != 1 {
			t.Errorf("the summary is %+v, want 1 boxed", summary)
		}

		fallback := zipEntry(t, out, "ppt/media/image1.png")
		if bytes.Equal(fallback, zipEntry(t, raw, "ppt/media/image1.png")) {
			t.Error("the PNG fallback was not treated, so a viewer that cannot draw SVG shows " +
				"the original picture")
		}
		if imaging.Sniff(fallback) != imaging.FormatPNG {
			t.Errorf("the fallback came back as %s, want png: the archive declares the "+
				"extension's type", imaging.Sniff(fallback))
		}
		vector := zipEntry(t, out, "ppt/media/image2.svg")
		if imaging.Sniff(vector) != imaging.FormatSVG {
			t.Errorf("the companion came back as %s, want svg", imaging.Sniff(vector))
		}
		if strings.Contains(string(vector), "pierre.dupont") {
			t.Error("the companion SVG still carries the original's text, so the picture was " +
				"boxed on one machine and not on the other")
		}
	})
}

// TestExportRefusesAnImpossibleDecision: the exporter is the last gate, and a
// decision that reached it without passing the interface (a restored session, a
// hand-built plan) must fail the export rather than write a picture the user
// never approved.
func TestExportRefusesAnImpossibleDecision(t *testing.T) {
	t.Run("errors/images_impossible_decision_fails_the_export", func(t *testing.T) {
		raw := picturesPptx(t)
		cfg := testConfig()
		cfg.Images = planFor(t, raw, map[string]imaging.Decision{
			"ppt/media/image1.png": {Treatment: "redact"},
		})
		if _, _, _, err := ExportPptx(raw, cfg); err == nil {
			t.Fatal("an unknown treatment produced a file; the export must refuse rather than " +
				"guess what the user meant")
		} else if !strings.Contains(err.Error(), "ppt/media/image1.png") {
			t.Errorf("the failure does not name the picture that caused it:\n%v", err)
		}
	})
}

// TestExportRemovesTheWholeWordDrawing: Word nests the picture that carries the
// NAME (pic:pic) inside the drawing that carries the FRAME (w:drawing), so a
// removal has a choice of elements and only one of them is right.
//
// Deleting the inner one leaves a w:drawing with an extent, a docPr and no
// graphic content, which Word draws as a broken placeholder rather than as
// nothing. The outermost picture element is what goes.
func TestExportRemovesTheWholeWordDrawing(t *testing.T) {
	t.Run("roundtrip/images_remove_takes_the_outer_drawing", func(t *testing.T) {
		const ns = ` xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"` +
			` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"` +
			` xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
			` xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"` +
			` xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"`
		document := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<w:document` + ns + `><w:body>` +
			`<w:p><w:r><w:t>Alpine Trust engagement</w:t></w:r></w:p>` +
			`<w:p><w:r><w:drawing><wp:inline><wp:extent cx="2743200" cy="1828800"/>` +
			`<wp:docPr id="1" name="Alpine Trust logo"/><a:graphic><a:graphicData>` +
			`<pic:pic><pic:nvPicPr><pic:cNvPr id="1" name="Alpine Trust logo"/></pic:nvPicPr>` +
			`<pic:blipFill><a:blip r:embed="rId1"/></pic:blipFill></pic:pic>` +
			`</a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>` +
			`</w:body></w:document>`
		raw := buildZip(t, map[string]string{
			"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
				`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
				`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
				`<Default Extension="xml" ContentType="application/xml"/>` +
				`<Default Extension="png" ContentType="image/png"/></Types>`,
			"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
				`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
				`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
				`</Relationships>`,
			"word/document.xml": document,
			"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
				`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
				`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/>` +
				`</Relationships>`,
			"word/media/image1.png": string(detailedPNG(t, 120, 80, color.RGBA{R: 220, G: 40, B: 40, A: 255})),
		})

		inv, err := imaging.ScanDocx(raw)
		if err != nil {
			t.Fatalf("the fixture does not scan: %v", err)
		}
		cfg := testConfig()
		cfg.Images = ImagePlan{
			Inventory: inv,
			Decisions: map[string]imaging.Decision{
				"word/media/image1.png": {Treatment: imaging.TreatmentRemove},
			},
		}
		out, _, _, summary, err := ExportDocx(raw, cfg)
		if err != nil {
			t.Fatalf("ExportDocx: %v", err)
		}
		if summary.Removed != 1 {
			t.Errorf("the summary is %+v, want 1 removed", summary)
		}

		body := string(zipEntry(t, out, "word/document.xml"))
		if err := parses([]byte(body)); err != nil {
			t.Fatalf("the document no longer parses after the deletion: %v\n%s", err, body)
		}
		for _, gone := range []string{"w:drawing", "wp:extent", "wp:docPr", "pic:pic", "a:blip"} {
			if strings.Contains(body, gone) {
				t.Errorf("%q survived the removal, so the whole drawing was not taken:\n%s",
					gone, body)
			}
		}
		// The paragraph and the run that held it survive as an empty run, and
		// the text beside them is untouched.
		if !strings.Contains(body, "Alpine Trust engagement") {
			t.Error("the document's text did not survive the picture deletion")
		}
	})
}

// engine/imaging/scan_test.go — the OOXML picture scanner, unit tier.
//
// TIER: unit (docs/TESTING.md). Every case builds its own tiny archive in
// memory, so nothing here reads a file or costs more than a few milliseconds.
// The committed fixtures are exercised one tier up, in scan_integration_test.go.
//
// The zip builder below is a deliberate DUPLICATE of the twenty lines in
// engine/convert/fixtures_test.go. It is duplicated rather than shared because a
// test helper in package convert cannot be imported from here at all (Go does
// not export test files), and moving it into a non-test package would put test
// scaffolding into the shipped binary. Nobody should "fix" this by wiring the
// two together.
package imaging_test

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"doc-anonymiser/backend/engine/imaging"
)

// --- helpers -------------------------------------------------------------

// buildArchive assembles a deterministic zip from entry name to content.
func buildArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range entries {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatalf("could not create the archive entry %q: %v", name, err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatalf("could not write the archive entry %q: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("could not close the test archive: %v", err)
	}
	return buf.Bytes()
}

// tinyPNG is a real, decodable PNG of the given size.
func tinyPNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("could not encode the test PNG: %v", err)
	}
	return buf.String()
}

const pptxNS = ` xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
	` xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"` +
	` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"` +
	` xmlns:asvg="http://schemas.microsoft.com/office/drawing/2016/SVG/main"`

const docxNS = ` xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"` +
	` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"` +
	` xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
	` xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"` +
	` xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"` +
	` xmlns:v="urn:schemas-microsoft-com:vml"`

// relsPart builds a relationships part from id/target pairs. A target starting
// with "http" is written as an EXTERNAL relationship, which is what a linked
// picture has.
func relsPart(pairs ...[2]string) string {
	out := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	for _, p := range pairs {
		mode := ""
		if strings.HasPrefix(p[1], "http") || strings.HasPrefix(p[1], "file:") {
			mode = ` TargetMode="External"`
		}
		out += `<Relationship Id="` + p[0] + `" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="` + p[1] + `"` + mode + `/>`
	}
	return out + `</Relationships>`
}

// pptxPic is one picture shape on a slide.
func pptxPic(name, rID, extra string) string {
	return `<p:pic><p:nvPicPr><p:cNvPr id="7" name="` + name + `"/></p:nvPicPr>` +
		`<p:blipFill><a:blip r:embed="` + rID + `">` + extra + `</a:blip></p:blipFill>` +
		`<p:spPr><a:xfrm><a:ext cx="914400" cy="457200"/></a:xfrm></p:spPr></p:pic>`
}

// slidePart wraps shapes into a slide, optionally hidden.
func slidePart(hidden bool, shapes string) string {
	show := ""
	if hidden {
		show = ` show="0"`
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><p:sld` + pptxNS + show +
		`><p:cSld><p:spTree>` + shapes + `</p:spTree></p:cSld></p:sld>`
}

// docxDrawing is one inline or floating picture in a document body.
func docxDrawing(wrapper, name, rID string) string {
	return `<w:p><w:r><w:drawing><wp:` + wrapper + `><wp:extent cx="1828800" cy="914400"/>` +
		`<wp:docPr id="3" name="` + name + `"/><a:graphic><a:graphicData>` +
		`<pic:pic><pic:nvPicPr><pic:cNvPr id="3" name="` + name + `"/></pic:nvPicPr>` +
		`<pic:blipFill><a:blip r:embed="` + rID + `"/></pic:blipFill></pic:pic>` +
		`</a:graphicData></a:graphic></wp:` + wrapper + `></w:drawing></w:r></w:p>`
}

// locationsOf lists every occurrence location of an inventory, in order, as
// "asset ID -> location" so a failure message says which picture moved.
func locationsOf(inv imaging.Inventory) []string {
	var out []string
	for _, a := range inv.Assets {
		for _, o := range a.Occurrences {
			out = append(out, a.ID+" -> "+o.Location)
		}
	}
	return out
}

// hasWarning reports whether the inventory carries a warning code.
func hasWarning(inv imaging.Inventory, code string) bool {
	for _, w := range inv.Warnings {
		if w == code {
			return true
		}
	}
	return false
}

// --- pptx ----------------------------------------------------------------

// TestScanPptx covers the deck rules together, because they are one walk: a
// picture reused on two slides is ONE asset, the master and a hidden slide are
// listed like any other place, and an SVG picture reports itself as SVG even
// though the relationship points at its PNG fallback.
func TestScanPptx(t *testing.T) {
	svgExt := `<a:extLst><a:ext uri="{96DAC541-7B7A-43D3-8B79-37D633B846F1}">` +
		`<asvg:svgBlip r:embed="rId3"/></a:ext></a:extLst>`

	deck := buildArchive(t, map[string]string{
		"ppt/slides/slide1.xml": slidePart(false,
			pptxPic("Alpine Trust logo", "rId1", "")+pptxPic("Diagram", "rId2", svgExt)),
		"ppt/slides/slide2.xml":                        slidePart(false, ""),
		"ppt/slides/slide3.xml":                        slidePart(false, pptxPic("Alpine Trust logo", "rId1", "")),
		"ppt/slides/slide4.xml":                        slidePart(true, pptxPic("Team photo", "rId1", "")),
		"ppt/slideMasters/slideMaster1.xml":            `<?xml version="1.0"?><p:sldMaster` + pptxNS + `><p:cSld><p:spTree>` + pptxPic("Watermark", "rId1", "") + `</p:spTree></p:cSld></p:sldMaster>`,
		"ppt/slides/_rels/slide1.xml.rels":             relsPart([2]string{"rId1", "../media/image1.png"}, [2]string{"rId2", "../media/image2.png"}, [2]string{"rId3", "../media/image3.svg"}),
		"ppt/slides/_rels/slide3.xml.rels":             relsPart([2]string{"rId1", "../media/image1.png"}),
		"ppt/slides/_rels/slide4.xml.rels":             relsPart([2]string{"rId1", "../media/image5.png"}),
		"ppt/slideMasters/_rels/slideMaster1.xml.rels": relsPart([2]string{"rId1", "../media/image4.png"}),
		"ppt/media/image1.png":                         tinyPNG(t, 120, 80),
		"ppt/media/image2.png":                         tinyPNG(t, 64, 64),
		"ppt/media/image3.svg":                         `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300 150"><rect width="300" height="150"/></svg>`,
		"ppt/media/image4.png":                         tinyPNG(t, 32, 32),
		"ppt/media/image5.png":                         tinyPNG(t, 48, 48),
	})

	inv, err := imaging.ScanPptx(deck)
	if err != nil {
		t.Fatalf("ScanPptx of a well-formed deck: %v, want no error", err)
	}

	t.Run("extraction/scan_pptx_asset_count", func(t *testing.T) {
		if len(inv.Assets) != 4 {
			t.Fatalf("got %d assets, want 4 (the logo counts ONCE for two slides); assets: %v",
				len(inv.Assets), locationsOf(inv))
		}
	})

	t.Run("extraction/scan_pptx_shared_asset", func(t *testing.T) {
		logo := inv.Assets[0]
		if logo.ID != "ppt/media/image1.png" {
			t.Fatalf("first asset is %q, want ppt/media/image1.png (assets are in document order)", logo.ID)
		}
		if len(logo.Occurrences) != 2 {
			t.Fatalf("the shared logo has %d occurrences, want 2 (slide 1 and slide 3)", len(logo.Occurrences))
		}
		if logo.Name != "Alpine Trust logo" {
			t.Errorf("the logo is named %q, want %q: an asset is named by what it calls itself",
				logo.Name, "Alpine Trust logo")
		}
		if logo.Width != 120 || logo.Height != 80 {
			t.Errorf("the logo measures %dx%d, want 120x80 (read from the PNG header)", logo.Width, logo.Height)
		}
	})

	t.Run("extraction/scan_pptx_locations", func(t *testing.T) {
		want := []string{
			"ppt/media/image1.png -> Slide 1",
			"ppt/media/image1.png -> Slide 3",
			"ppt/media/image2.png -> Slide 1",
			"ppt/media/image5.png -> Hidden slide 4",
			"ppt/media/image4.png -> Slide master",
		}
		got := locationsOf(inv)
		if strings.Join(got, " | ") != strings.Join(want, " | ") {
			t.Errorf("locations are\n  %v\nwant\n  %v", got, want)
		}
	})

	t.Run("extraction/scan_pptx_svg_pair", func(t *testing.T) {
		var svg imaging.Asset
		for _, a := range inv.Assets {
			if a.ID == "ppt/media/image2.png" {
				svg = a
			}
		}
		if svg.Format != imaging.FormatSVG {
			t.Errorf("the SVG picture reports format %q, want %q: SVG is what the user sees and "+
				"what decides which treatments are offered", svg.Format, imaging.FormatSVG)
		}
		if svg.Companion != "ppt/media/image3.svg" {
			t.Errorf("the SVG picture's companion is %q, want ppt/media/image3.svg: a treatment "+
				"that misses it leaves the untreated vector in the file", svg.Companion)
		}
		if svg.ID != "ppt/media/image2.png" {
			t.Errorf("the SVG asset's ID is %q, want the PNG fallback: that is what the "+
				"relationship points at and what an export must keep valid", svg.ID)
		}
	})

	t.Run("extraction/scan_pptx_frame_and_kind", func(t *testing.T) {
		occ := inv.Assets[0].Occurrences[0]
		if occ.Kind != imaging.KindPicture {
			t.Errorf("a p:pic occurrence has kind %q, want %q", occ.Kind, imaging.KindPicture)
		}
		if occ.DisplayCX != 914400 || occ.DisplayCY != 457200 {
			t.Errorf("the drawn frame is %dx%d EMU, want 914400x457200: PowerPoint states it "+
				"AFTER the blip, so a scan that reads it before finds nothing",
				occ.DisplayCX, occ.DisplayCY)
		}
		if occ.Ordinal != 0 {
			t.Errorf("the first picture of a part has ordinal %d, want 0", occ.Ordinal)
		}
	})
}

// TestScanPptxFillAndBackground: a picture that is a shape's fill or a slide's
// background has no picture element of its own, and the kind says so, because
// that is what decides whether removing it can delete anything.
func TestScanPptxFillAndBackground(t *testing.T) {
	slide := slidePart(false,
		`<p:bg><p:bgPr><a:blipFill><a:blip r:embed="rId1"/></a:blipFill></p:bgPr></p:bg>`+
			`<p:sp><p:nvSpPr><p:nvPr/></p:nvSpPr><p:spPr><a:blipFill><a:blip r:embed="rId2"/></a:blipFill></p:spPr></p:sp>`)

	deck := buildArchive(t, map[string]string{
		"ppt/slides/slide1.xml":            slide,
		"ppt/slides/_rels/slide1.xml.rels": relsPart([2]string{"rId1", "../media/bg.png"}, [2]string{"rId2", "../media/fill.png"}),
		"ppt/media/bg.png":                 tinyPNG(t, 10, 10),
		"ppt/media/fill.png":               tinyPNG(t, 10, 10),
	})

	inv, err := imaging.ScanPptx(deck)
	if err != nil {
		t.Fatalf("ScanPptx: %v, want no error", err)
	}
	t.Run("extraction/scan_pptx_kind_background", func(t *testing.T) {
		if got := inv.Assets[0].Occurrences[0].Kind; got != imaging.KindBackground {
			t.Errorf("a picture inside p:bg has kind %q, want %q: it has no element to delete, so "+
				"removing it means overwriting its bytes", got, imaging.KindBackground)
		}
	})
	t.Run("extraction/scan_pptx_kind_fill", func(t *testing.T) {
		if got := inv.Assets[1].Occurrences[0].Kind; got != imaging.KindFill {
			t.Errorf("a shape's blipFill has kind %q, want %q", got, imaging.KindFill)
		}
	})
}

// TestScanPptxNotesLocation: a notes part says nothing about which slide it
// belongs to, so the slide's own relationships are what name it. A deck whose
// notes are numbered differently from its slides is the case that proves it.
func TestScanPptxNotesLocation(t *testing.T) {
	notes := `<?xml version="1.0"?><p:notes` + pptxNS + `><p:cSld><p:spTree>` +
		pptxPic("Screenshot", "rId1", "") + `</p:spTree></p:cSld></p:notes>`

	deck := buildArchive(t, map[string]string{
		"ppt/slides/slide1.xml": slidePart(false, ""),
		"ppt/slides/slide2.xml": slidePart(false, ""),
		"ppt/slides/_rels/slide2.xml.rels": `<?xml version="1.0"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId9" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/notesSlide" Target="../notesSlides/notesSlide1.xml"/>` +
			`</Relationships>`,
		"ppt/notesSlides/notesSlide1.xml":            notes,
		"ppt/notesSlides/_rels/notesSlide1.xml.rels": relsPart([2]string{"rId1", "../media/shot.png"}),
		"ppt/media/shot.png":                         tinyPNG(t, 20, 20),
	})

	inv, err := imaging.ScanPptx(deck)
	if err != nil {
		t.Fatalf("ScanPptx: %v, want no error", err)
	}
	if len(inv.Assets) != 1 {
		t.Fatalf("got %d assets, want 1 (the picture on the notes page)", len(inv.Assets))
	}
	if got := inv.Assets[0].Occurrences[0].Location; got != "Notes on slide 2" {
		t.Errorf("the notes picture is located %q, want %q: notesSlide1 belongs to SLIDE 2 here, "+
			"and only the slide's relationships say so", got, "Notes on slide 2")
	}
}

// TestScanPptxEmpty: a deck with no pictures is an empty list, not an error and
// not an inapplicable format. The screen has something true to say about it.
func TestScanPptxEmpty(t *testing.T) {
	t.Run("extraction/scan_empty_deck", func(t *testing.T) {
		deck := buildArchive(t, map[string]string{
			"ppt/slides/slide1.xml": slidePart(false, ""),
		})
		inv, err := imaging.ScanPptx(deck)
		if err != nil {
			t.Fatalf("ScanPptx of a deck with no pictures: %v, want no error", err)
		}
		if !inv.Applicable {
			t.Error("a deck with no pictures must stay applicable: the format HAS an image review, " +
				"this file simply has nothing in it")
		}
		if len(inv.Assets) != 0 {
			t.Errorf("got %d assets, want 0", len(inv.Assets))
		}
	})
}

// --- docx ----------------------------------------------------------------

// TestScanDocx covers the four shapes Word writes a picture in, plus the page
// rule: a page number is given only where Word cached its breaks.
func TestScanDocx(t *testing.T) {
	body := `<?xml version="1.0"?><w:document` + docxNS + `><w:body>` +
		docxDrawing("inline", "Logo", "rId1") +
		docxDrawing("anchor", "Floating photo", "rId2") +
		`<w:p><w:r><w:pict><v:shape><v:imagedata r:id="rId3"/></v:shape></w:pict></w:r></w:p>` +
		`<w:p><w:r><w:lastRenderedPageBreak/><w:t>Second page</w:t></w:r></w:p>` +
		docxDrawing("inline", "Chart", "rId4") +
		`</w:body></w:document>`

	doc := buildArchive(t, map[string]string{
		"word/document.xml": body,
		"word/header1.xml": `<?xml version="1.0"?><w:hdr` + docxNS + `>` +
			docxDrawing("inline", "Letterhead", "rId1") + `</w:hdr>`,
		"word/_rels/document.xml.rels": relsPart([2]string{"rId1", "media/image1.png"},
			[2]string{"rId2", "media/image2.png"}, [2]string{"rId3", "media/image3.png"},
			[2]string{"rId4", "media/image4.png"}),
		"word/_rels/header1.xml.rels": relsPart([2]string{"rId1", "media/image5.png"}),
		"word/media/image1.png":       tinyPNG(t, 20, 20),
		"word/media/image2.png":       tinyPNG(t, 20, 20),
		"word/media/image3.png":       tinyPNG(t, 20, 20),
		"word/media/image4.png":       tinyPNG(t, 20, 20),
		"word/media/image5.png":       tinyPNG(t, 20, 20),
	})

	inv, err := imaging.ScanDocx(doc)
	if err != nil {
		t.Fatalf("ScanDocx of a well-formed document: %v, want no error", err)
	}

	t.Run("extraction/scan_docx_every_form", func(t *testing.T) {
		if len(inv.Assets) != 5 {
			t.Fatalf("got %d assets, want 5 (inline, floating, legacy VML, after the page break, "+
				"and the header); locations: %v", len(inv.Assets), locationsOf(inv))
		}
	})

	t.Run("extraction/scan_docx_locations", func(t *testing.T) {
		want := []string{
			"word/media/image1.png -> Page 1",
			"word/media/image2.png -> Page 1",
			"word/media/image3.png -> Page 1",
			"word/media/image4.png -> Page 2",
			"word/media/image5.png -> Header",
		}
		got := locationsOf(inv)
		if strings.Join(got, " | ") != strings.Join(want, " | ") {
			t.Errorf("locations are\n  %v\nwant\n  %v", got, want)
		}
	})

	t.Run("extraction/scan_docx_legacy_vml", func(t *testing.T) {
		vml := inv.Assets[2]
		if vml.ID != "word/media/image3.png" {
			t.Fatalf("the legacy VML picture resolved to %q, want word/media/image3.png", vml.ID)
		}
		if vml.Occurrences[0].Kind != imaging.KindPicture {
			t.Errorf("a v:imagedata inside w:pict has kind %q, want %q: it is a picture element "+
				"and removing it deletes that element", vml.Occurrences[0].Kind, imaging.KindPicture)
		}
	})
}

// TestScanDocxWithoutCachedBreaks: with no cached page breaks in the file, Word
// never rendered the document, so there IS no page number. Inventing one would
// be worse than the honest answer, because the user is looking at the file.
func TestScanDocxWithoutCachedBreaks(t *testing.T) {
	doc := buildArchive(t, map[string]string{
		"word/document.xml": `<?xml version="1.0"?><w:document` + docxNS + `><w:body>` +
			docxDrawing("inline", "Logo", "rId1") + `</w:body></w:document>`,
		"word/_rels/document.xml.rels": relsPart([2]string{"rId1", "media/image1.png"}),
		"word/media/image1.png":        tinyPNG(t, 20, 20),
	})
	inv, err := imaging.ScanDocx(doc)
	if err != nil {
		t.Fatalf("ScanDocx: %v, want no error", err)
	}
	if got := inv.Assets[0].Occurrences[0].Location; got != "Body" {
		t.Errorf("with no cached page breaks the location is %q, want %q", got, "Body")
	}
}

// --- relationships -------------------------------------------------------

// TestScanRelationshipTargets: a deck's target is written relative to
// ppt/slides/ and a document's relative to word/, and both must clean to the
// media part they mean. Trimming "../" by hand is what gets this wrong.
func TestScanRelationshipTargets(t *testing.T) {
	t.Run("extraction/scan_rels_pptx_parent_target", func(t *testing.T) {
		deck := buildArchive(t, map[string]string{
			"ppt/slides/slide1.xml":            slidePart(false, pptxPic("Logo", "rId1", "")),
			"ppt/slides/_rels/slide1.xml.rels": relsPart([2]string{"rId1", "../media/x.png"}),
			"ppt/media/x.png":                  tinyPNG(t, 8, 8),
		})
		inv, err := imaging.ScanPptx(deck)
		if err != nil {
			t.Fatalf("ScanPptx: %v", err)
		}
		if inv.Assets[0].ID != "ppt/media/x.png" {
			t.Errorf("a target of ../media/x.png from ppt/slides/ resolved to %q, want ppt/media/x.png",
				inv.Assets[0].ID)
		}
	})

	t.Run("extraction/scan_rels_docx_sibling_target", func(t *testing.T) {
		doc := buildArchive(t, map[string]string{
			"word/document.xml": `<?xml version="1.0"?><w:document` + docxNS + `><w:body>` +
				docxDrawing("inline", "Logo", "rId1") + `</w:body></w:document>`,
			"word/_rels/document.xml.rels": relsPart([2]string{"rId1", "media/x.png"}),
			"word/media/x.png":             tinyPNG(t, 8, 8),
		})
		inv, err := imaging.ScanDocx(doc)
		if err != nil {
			t.Fatalf("ScanDocx: %v", err)
		}
		if inv.Assets[0].ID != "word/media/x.png" {
			t.Errorf("a target of media/x.png from word/ resolved to %q, want word/media/x.png",
				inv.Assets[0].ID)
		}
	})

	t.Run("extraction/scan_rels_linked_image", func(t *testing.T) {
		deck := buildArchive(t, map[string]string{
			"ppt/slides/slide1.xml": slidePart(false,
				`<p:pic><p:nvPicPr><p:cNvPr id="1" name="Linked chart"/></p:nvPicPr>`+
					`<p:blipFill><a:blip r:link="rId1"/></p:blipFill></p:pic>`),
			"ppt/slides/_rels/slide1.xml.rels": relsPart([2]string{"rId1", "file:///C:/pictures/chart.png"}),
		})
		inv, err := imaging.ScanPptx(deck)
		if err != nil {
			t.Fatalf("ScanPptx: %v", err)
		}
		if len(inv.Assets) != 1 {
			t.Fatalf("got %d assets, want 1: a linked picture is still listed, because it is still "+
				"in the document", len(inv.Assets))
		}
		if !inv.Assets[0].Linked {
			t.Error("a picture reached through r:link must come back Linked: its bytes are not in " +
				"the archive, so it can be removed and nothing else")
		}
		if inv.Assets[0].Bytes != 0 {
			t.Errorf("a linked picture reports %d bytes, want 0: there are none here", inv.Assets[0].Bytes)
		}
		if !hasWarning(inv, imaging.WarnLinkedImages) {
			t.Errorf("warnings are %v, want the %q code", inv.Warnings, imaging.WarnLinkedImages)
		}
	})
}

// --- failures ------------------------------------------------------------

// TestScanCorruptPart: a part that is not well-formed XML fails with a message
// naming the part and saying what to do, rather than a parser's own wording.
func TestScanCorruptPart(t *testing.T) {
	t.Run("errors/scan_corrupt_part", func(t *testing.T) {
		deck := buildArchive(t, map[string]string{
			"ppt/slides/slide1.xml": `<?xml version="1.0"?><p:sld` + pptxNS + `><p:cSld <<<`,
		})
		_, err := imaging.ScanPptx(deck)
		if err == nil {
			t.Fatal("scanning a part that is not well-formed XML must fail, got no error")
		}
		if !strings.Contains(err.Error(), "ppt/slides/slide1.xml") {
			t.Errorf("the error is %q, and it must name the part it failed on", err)
		}
		if !strings.Contains(err.Error(), "re-export") {
			t.Errorf("the error is %q, and it must say what to do about it", err)
		}
	})

	t.Run("errors/scan_not_an_archive", func(t *testing.T) {
		_, err := imaging.ScanDocx([]byte("this is not a zip file at all"))
		if err == nil {
			t.Fatal("scanning bytes that are not a zip archive must fail, got no error")
		}
		if !strings.Contains(err.Error(), "zip archive") {
			t.Errorf("the error is %q, and it must explain that Word files are zip archives", err)
		}
	})

	t.Run("errors/scan_unreadable_picture", func(t *testing.T) {
		// A part that claims to be a PNG and is not: the header signature is
		// there and nothing decodes behind it.
		deck := buildArchive(t, map[string]string{
			"ppt/slides/slide1.xml":            slidePart(false, pptxPic("Broken", "rId1", "")),
			"ppt/slides/_rels/slide1.xml.rels": relsPart([2]string{"rId1", "../media/broken.png"}),
			"ppt/media/broken.png":             "\x89PNG\r\n\x1a\n and then nothing that decodes",
		})
		inv, err := imaging.ScanPptx(deck)
		if err != nil {
			t.Fatalf("an undecodable picture must not fail the whole scan, got %v", err)
		}
		if len(inv.Assets) != 1 {
			t.Fatalf("got %d assets, want 1: a picture that cannot be decoded is still listed, or "+
				"the user believes it was reviewed", len(inv.Assets))
		}
		if inv.Assets[0].Bytes == 0 {
			t.Error("an undecodable picture must still report its size in bytes")
		}
		if inv.Assets[0].Width != 0 || inv.Assets[0].Height != 0 {
			t.Errorf("an undecodable picture reports %dx%d pixels, want 0x0",
				inv.Assets[0].Width, inv.Assets[0].Height)
		}
		if !hasWarning(inv, imaging.WarnUnreadablePart) {
			t.Errorf("warnings are %v, want the %q code", inv.Warnings, imaging.WarnUnreadablePart)
		}
	})
}

// engine/convert/fixtures_test.go — test-helper fixture generators
//
// The .docx and .pptx fixtures are assembled as raw OOXML zip structures
// with the standard library only; the .xlsx fixture is generated with
// excelize; the .pdf fixtures are hand-constructed raw PDF syntax with a
// programmatically computed xref table. All fixture content is obviously
// fictional, in English and French.
//
// fixture(t, name) is the single access point: it returns the committed
// file from testdata/ when present, and (re)generates + writes it when
// absent — so the fixtures stay reproducible from code while golden tests
// always run against the committed bytes.
package convert

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// fixture returns the named testdata file, generating it first if missing.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", name)
	if data, err := os.ReadFile(path); err == nil {
		return data
	}
	data := generateFixture(t, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("could not write fixture %s: %v", path, err)
	}
	t.Logf("generated fixture %s (%d bytes)", path, len(data))
	return data
}

// generateFixture builds the named fixture from scratch.
func generateFixture(t *testing.T, name string) []byte {
	t.Helper()
	switch name {
	case "report.docx":
		return buildDocxFixture(t)
	case "deck.pptx":
		return buildPptxFixture(t)
	case "images.docx":
		return buildImagesDocxFixture(t)
	case "images.pptx":
		return buildImagesPptxFixture(t)
	case "workbook.xlsx":
		return buildXlsxFixture(t)
	case "textlayer.pdf":
		return buildPDF([]string{
			"B R IDDING ULES for the Alpine Trust engagement",
			"Contact: marie.duval@example.com  (double  spaces on purpose)",
		})
	case "scanned.pdf":
		// An "image-only" page: the content stream draws a rectangle and
		// contains no text operators at all — the scanned-PDF shape.
		return buildImageOnlyPDF()
	default:
		t.Fatalf("unknown fixture %q", name)
		return nil
	}
}

// buildZip assembles a deterministic zip archive (fixed timestamps, sorted
// entry order) from entry name → content.
func buildZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var names []string
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, n := range names {
		fh := &zip.FileHeader{Name: n, Method: zip.Deflate}
		fh.Modified = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		fw, err := w.CreateHeader(fh)
		if err != nil {
			t.Fatalf("zip create %s: %v", n, err)
		}
		if _, err := fw.Write([]byte(entries[n])); err != nil {
			t.Fatalf("zip write %s: %v", n, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// --- .docx fixture -------------------------------------------------------

// buildDocxFixture assembles a minimal-but-valid .docx exercising every
// mapped feature: Heading1, bold/italic runs, a hyperlink, an image
// placeholder, bulleted + numbered lists, a table, and French text.
func buildDocxFixture(t *testing.T) []byte {
	const documentXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>` +
		// H1 heading via the built-in Heading1 paragraph style.
		`<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Engagement Report</w:t></w:r></w:p>` +
		// Mixed-formatting paragraph: bold, plain, italic, hyperlink, image.
		`<w:p><w:r><w:rPr><w:b/></w:rPr><w:t>Confidential</w:t></w:r>` +
		`<w:r><w:t xml:space="preserve"> status: </w:t></w:r>` +
		`<w:r><w:rPr><w:i/></w:rPr><w:t>draft</w:t></w:r>` +
		`<w:r><w:t xml:space="preserve"> see </w:t></w:r>` +
		`<w:hyperlink r:id="rId1"><w:r><w:t>project site</w:t></w:r></w:hyperlink>` +
		`<w:r><w:t xml:space="preserve"> </w:t></w:r>` +
		`<w:r><w:drawing></w:drawing></w:r></w:p>` +
		// Bulleted list (numId 1 → bullet) and numbered list (numId 2 →
		// decimal) with one nested level.
		`<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>First bullet</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>Second bullet</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="2"/></w:numPr></w:pPr><w:r><w:t>Step one</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:numPr><w:ilvl w:val="1"/><w:numId w:val="2"/></w:numPr></w:pPr><w:r><w:t>Sub step</w:t></w:r></w:p>` +
		// 2×2 table, first row is the header.
		`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>Name</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Role</w:t></w:r></w:p></w:tc></w:tr>` +
		`<w:tr><w:tc><w:p><w:r><w:t>Marie Duval</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Reviewer</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
		// French paragraph (accents prove UTF-8 survives the round trip).
		`<w:p><w:r><w:t>Réunion avec Amélie Lefèvre à Luxembourg.</w:t></w:r></w:p>` +
		`</w:body></w:document>`

	const numberingXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/></w:lvl></w:abstractNum>` +
		`<w:abstractNum w:abstractNumId="1"><w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/></w:lvl><w:lvl w:ilvl="1"><w:numFmt w:val="decimal"/></w:lvl></w:abstractNum>` +
		`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>` +
		`<w:num w:numId="2"><w:abstractNumId w:val="1"/></w:num>` +
		`</w:numbering>`

	const docRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://alpine.example.com" TargetMode="External"/>` +
		`</Relationships>`

	return buildZip(t, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":            documentXML,
		"word/numbering.xml":           numberingXML,
		"word/_rels/document.xml.rels": docRelsXML,
	})
}

// --- .pptx fixture -------------------------------------------------------

// buildPptxFixture assembles a two-slide deck: slide 1 has a title made of
// three lines separated by soft breaks (the shape a real deck's cover slide
// has: title, author, date in ONE paragraph), a body with two outline levels,
// a table and speaker notes (resolved via rels); slide 2 has an untitled
// French body only.
func buildPptxFixture(t *testing.T) []byte {
	const ns = ` xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`

	slide1 := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld` + ns + `><p:cSld><p:spTree>` +
		`<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:p>` +
		`<a:r><a:t>Quarterly Review</a:t></a:r><a:br/>` +
		`<a:r><a:t>Prepared by Marie Duval</a:t></a:r><a:br/>` +
		`<a:r><a:t>Internal draft, 12 February 2024</a:t></a:r>` +
		`</a:p></p:txBody></p:sp>` +
		`<p:sp><p:nvSpPr><p:nvPr><p:ph type="body"/></p:nvPr></p:nvSpPr><p:txBody>` +
		`<a:p><a:r><a:t>Revenue grew</a:t></a:r></a:p>` +
		`<a:p><a:pPr lvl="1"/><a:r><a:t>Driven by Borealis Fund</a:t></a:r></a:p>` +
		`</p:txBody></p:sp>` +
		`<p:graphicFrame><a:graphic><a:graphicData><a:tbl>` +
		`<a:tr><a:tc><a:txBody><a:p><a:r><a:t>KPI</a:t></a:r></a:p></a:txBody></a:tc><a:tc><a:txBody><a:p><a:r><a:t>Value</a:t></a:r></a:p></a:txBody></a:tc></a:tr>` +
		`<a:tr><a:tc><a:txBody><a:p><a:r><a:t>NPS</a:t></a:r></a:p></a:txBody></a:tc><a:tc><a:txBody><a:p><a:r><a:t>42</a:t></a:r></a:p></a:txBody></a:tc></a:tr>` +
		`</a:tbl></a:graphicData></a:graphic></p:graphicFrame>` +
		`</p:spTree></p:cSld></p:sld>`

	// Slide 2: the placeholder has no type attribute → body by convention.
	slide2 := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld` + ns + `><p:cSld><p:spTree>` +
		`<p:sp><p:nvSpPr><p:nvPr><p:ph/></p:nvPr></p:nvSpPr><p:txBody>` +
		`<a:p><a:r><a:t>Prochaines étapes — réunion à Luxembourg</a:t></a:r></a:p>` +
		`</p:txBody></p:sp>` +
		`</p:spTree></p:cSld></p:sld>`

	// Notes for slide 1: a body placeholder with the actual notes and a
	// slide-number placeholder whose "1" must NOT leak into the output.
	notes1 := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:notes` + ns + `><p:cSld><p:spTree>` +
		`<p:sp><p:nvSpPr><p:nvPr><p:ph type="body"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>Mention the Alpine Trust engagement.</a:t></a:r></a:p></p:txBody></p:sp>` +
		`<p:sp><p:nvSpPr><p:nvPr><p:ph type="sldNum"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>1</a:t></a:r></a:p></p:txBody></p:sp>` +
		`</p:spTree></p:cSld></p:notes>`

	slide1Rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/notesSlide" Target="../notesSlides/notesSlide1.xml"/></Relationships>`

	return buildZip(t, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/></Types>`,
		"_rels/.rels":                      `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`,
		"ppt/slides/slide1.xml":            slide1,
		"ppt/slides/slide2.xml":            slide2,
		"ppt/slides/_rels/slide1.xml.rels": slide1Rels,
		"ppt/notesSlides/notesSlide1.xml":  notes1,
	})
}

// --- .xlsx fixture -------------------------------------------------------

// buildXlsxFixture generates a workbook with excelize: a FLAT sheet
// ("Clients": clean header + data, some trailing empties to trim) and a
// COMPLEX sheet ("Résumé": merged title cell over a small block).
func buildXlsxFixture(t *testing.T) []byte {
	f := excelize.NewFile()
	defer f.Close()

	// FLAT sheet — rename the default sheet.
	if err := f.SetSheetName("Sheet1", "Clients"); err != nil {
		t.Fatal(err)
	}
	flat := [][]interface{}{
		{"name", "client", "email"},
		{"Marie Duval", "Alpine Trust", "marie.duval@example.com"},
		{"Peter Stone", "Alpine Trust", "peter.stone@example.org"},
		{"Ana Silva", "Borealis Fund", "ana.silva@example.net"},
	}
	for i, row := range flat {
		if err := f.SetSheetRow("Clients", fmt.Sprintf("A%d", i+1), &row); err != nil {
			t.Fatal(err)
		}
	}
	// A whitespace-only cell beyond the data proves data-bounds trimming.
	if err := f.SetCellValue("Clients", "E7", " "); err != nil {
		t.Fatal(err)
	}

	// COMPLEX sheet — a merged title over two columns forces JSON routing.
	if _, err := f.NewSheet("Résumé"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("Résumé", "A1", "Budget prévisionnel"); err != nil {
		t.Fatal(err)
	}
	if err := f.MergeCell("Résumé", "A1", "B1"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("Résumé", "A2", "Phase"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("Résumé", "B2", "Montant"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("Résumé", "A3", "Cadrage"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("Résumé", "B3", "12500"); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// --- .pdf fixtures -------------------------------------------------------

// buildPDF hand-constructs a minimal single-page text PDF (raw PDF syntax,
// stdlib only) whose xref offsets are computed programmatically. Each
// element of lines becomes one text line on the page.
func buildPDF(lines []string) []byte {
	// Content stream: position the cursor and emit one Tj per line.
	var content strings.Builder
	y := 720
	for _, line := range lines {
		// Escape the PDF string delimiters.
		esc := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(line)
		fmt.Fprintf(&content, "BT /F1 12 Tf 72 %d Td (%s) Tj ET\n", y, esc)
		y -= 16
	}
	return assemblePDF(content.String())
}

// buildImageOnlyPDF constructs a page whose content stream only draws a
// filled rectangle — no text operators — mimicking a scanned page.
func buildImageOnlyPDF() []byte {
	return assemblePDF("0.5 g 100 100 400 600 re f\n")
}

// assemblePDF wraps a content stream into a complete one-page PDF file
// with a correct xref table.
func assemblePDF(content string) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1) // index 0 is the free object
	for i, obj := range objects {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xrefPos := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objects)+1)
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefPos)
	return buf.Bytes()
}

// --- image fixtures ------------------------------------------------------

// The two image fixtures exist for engine/imaging's scan, not for the
// converters: they are generated here because this is where the repository's
// binary fixtures are generated, and a second generator would be a second set
// of rules for what a fixture looks like.
//
// Their pictures are drawn in code with image/png and image/jpeg so the bytes
// are reproducible, and their SVG is a literal string. Content is obviously
// fictional and in English and French, per docs/TESTING.md.

// solidPNG encodes a flat colour PNG of the given size.
func solidPNG(t *testing.T, w, h int, c color.RGBA) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("could not encode the PNG fixture picture: %v", err)
	}
	return buf.String()
}

// solidJPEG encodes a flat colour JPEG of the given size.
func solidJPEG(t *testing.T, w, h int, c color.RGBA) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("could not encode the JPEG fixture picture: %v", err)
	}
	return buf.String()
}

// fixtureSVG is the vector picture of the two image fixtures. It states a
// viewBox and no width or height, which is the shape an Office-exported SVG
// usually has.
const fixtureSVG = `<?xml version="1.0" encoding="UTF-8"?>` +
	`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300 150">` +
	`<rect width="300" height="150" fill="#1f77b4"/>` +
	`<text x="20" y="80" fill="#ffffff">Schéma Borealis</text></svg>`

// buildImagesPptxFixture assembles a deck whose pictures exercise every rule
// the scan has: the same picture used on two slides, a picture on the master,
// an SVG picture (a PNG fallback plus the SVG in an extension), and a picture
// on a HIDDEN slide.
func buildImagesPptxFixture(t *testing.T) []byte {
	t.Helper()
	const ns = ` xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
		` xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"` +
		` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"` +
		` xmlns:asvg="http://schemas.microsoft.com/office/drawing/2016/SVG/main"`

	// pic builds one picture shape: a name, a blip pointing at a relationship,
	// and a drawn frame in EMU.
	pic := func(name, rID, extra string) string {
		return `<p:pic><p:nvPicPr><p:cNvPr id="4" name="` + name + `" descr="` + name + `"/></p:nvPicPr>` +
			`<p:blipFill><a:blip r:embed="` + rID + `">` + extra + `</a:blip></p:blipFill>` +
			`<p:spPr><a:xfrm><a:ext cx="1828800" cy="1219200"/></a:xfrm></p:spPr></p:pic>`
	}
	svgExt := `<a:extLst><a:ext uri="{96DAC541-7B7A-43D3-8B79-37D633B846F1}">` +
		`<asvg:svgBlip r:embed="rId3"/></a:ext></a:extLst>`
	text := func(body string) string {
		return `<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody>` +
			`<a:p><a:r><a:t>` + body + `</a:t></a:r></a:p></p:txBody></p:sp>`
	}

	slide1 := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<p:sld` + ns + `><p:cSld><p:spTree>` + text("Alpine Trust review") +
		pic("Alpine Trust logo", "rId1", "") +
		pic("Schéma Borealis", "rId2", svgExt) +
		`</p:spTree></p:cSld></p:sld>`

	slide2 := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<p:sld` + ns + `><p:cSld><p:spTree>` + text("Prochaines étapes") +
		`</p:spTree></p:cSld></p:sld>`

	// Slide 3 uses the SAME logo as slide 1: one asset, two occurrences.
	slide3 := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<p:sld` + ns + `><p:cSld><p:spTree>` + text("Governance") +
		pic("Alpine Trust logo", "rId1", "") +
		`</p:spTree></p:cSld></p:sld>`

	// Slide 4 is hidden, and its picture is the one a reviewer most needs told
	// about, because it is the one they cannot reach by scrolling.
	slide4 := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<p:sld` + ns + ` show="0"><p:cSld><p:spTree>` + text("Annexe retirée") +
		pic("Photo équipe", "rId1", "") +
		`</p:spTree></p:cSld></p:sld>`

	master := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<p:sldMaster` + ns + `><p:cSld><p:spTree>` +
		pic("Watermark", "rId1", "") +
		`</p:spTree></p:cSld></p:sldMaster>`

	rels := func(pairs ...[2]string) string {
		out := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
		for _, p := range pairs {
			out += `<Relationship Id="` + p[0] + `" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="` + p[1] + `"/>`
		}
		return out + `</Relationships>`
	}

	return buildZip(t, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/><Default Extension="jpeg" ContentType="image/jpeg"/><Default Extension="svg" ContentType="image/svg+xml"/></Types>`,
		"_rels/.rels":                                  `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`,
		"ppt/slides/slide1.xml":                        slide1,
		"ppt/slides/slide2.xml":                        slide2,
		"ppt/slides/slide3.xml":                        slide3,
		"ppt/slides/slide4.xml":                        slide4,
		"ppt/slideMasters/slideMaster1.xml":            master,
		"ppt/slides/_rels/slide1.xml.rels":             rels([2]string{"rId1", "../media/image1.png"}, [2]string{"rId2", "../media/image2.png"}, [2]string{"rId3", "../media/image3.svg"}),
		"ppt/slides/_rels/slide3.xml.rels":             rels([2]string{"rId1", "../media/image1.png"}),
		"ppt/slides/_rels/slide4.xml.rels":             rels([2]string{"rId1", "../media/image5.png"}),
		"ppt/slideMasters/_rels/slideMaster1.xml.rels": rels([2]string{"rId1", "../media/image4.jpeg"}),
		"ppt/media/image1.png":                         solidPNG(t, 120, 80, color.RGBA{R: 220, G: 40, B: 40, A: 255}),
		"ppt/media/image2.png":                         solidPNG(t, 300, 150, color.RGBA{R: 31, G: 119, B: 180, A: 255}),
		"ppt/media/image3.svg":                         fixtureSVG,
		"ppt/media/image4.jpeg":                        solidJPEG(t, 200, 200, color.RGBA{R: 40, G: 40, B: 220, A: 255}),
		"ppt/media/image5.png":                         solidPNG(t, 64, 64, color.RGBA{R: 40, G: 180, B: 90, A: 255}),
	})
}

// buildImagesDocxFixture assembles a document whose pictures cover the shapes
// Word writes: an inline picture, a floating one, the legacy VML form still
// produced by pasted content, one after a cached page break (so it is on page
// 2), and one in a header.
func buildImagesDocxFixture(t *testing.T) []byte {
	t.Helper()
	const ns = ` xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"` +
		` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"` +
		` xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
		` xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"` +
		` xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"` +
		` xmlns:v="urn:schemas-microsoft-com:vml"`

	// drawing builds an inline or floating picture, which differ only in their
	// wrapper element.
	drawing := func(wrapper, name, rID string) string {
		return `<w:p><w:r><w:drawing><wp:` + wrapper + `><wp:extent cx="2743200" cy="1828800"/>` +
			`<wp:docPr id="1" name="` + name + `"/><a:graphic><a:graphicData>` +
			`<pic:pic><pic:nvPicPr><pic:cNvPr id="1" name="` + name + `"/></pic:nvPicPr>` +
			`<pic:blipFill><a:blip r:embed="` + rID + `"/></pic:blipFill></pic:pic>` +
			`</a:graphicData></a:graphic></wp:` + wrapper + `></w:drawing></w:r></w:p>`
	}

	documentXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document` + ns + `><w:body>` +
		`<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Alpine Trust engagement</w:t></w:r></w:p>` +
		drawing("inline", "Alpine Trust logo", "rId1") +
		drawing("anchor", "Site photo", "rId2") +
		// The legacy VML form: no DrawingML blip at all.
		`<w:p><w:r><w:pict><v:shape id="_x0000_i1025"><v:imagedata r:id="rId3"/></v:shape></w:pict></w:r></w:p>` +
		// Word's cached break: everything after it is on page 2.
		`<w:p><w:r><w:lastRenderedPageBreak/><w:t>Annexe: réunion à Luxembourg</w:t></w:r></w:p>` +
		drawing("inline", "Organigramme", "rId4") +
		`</w:body></w:document>`

	headerXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:hdr` + ns + `>` + drawing("inline", "Letterhead", "rId1") + `</w:hdr>`

	rels := func(pairs ...[2]string) string {
		out := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
		for _, p := range pairs {
			out += `<Relationship Id="` + p[0] + `" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="` + p[1] + `"/>`
		}
		return out + `</Relationships>`
	}

	return buildZip(t, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/><Default Extension="jpeg" ContentType="image/jpeg"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":            documentXML,
		"word/header1.xml":             headerXML,
		"word/_rels/document.xml.rels": rels([2]string{"rId1", "media/image1.png"}, [2]string{"rId2", "media/image2.jpeg"}, [2]string{"rId3", "media/image3.png"}, [2]string{"rId4", "media/image4.png"}),
		"word/_rels/header1.xml.rels":  rels([2]string{"rId1", "media/image5.png"}),
		"word/media/image1.png":        solidPNG(t, 120, 80, color.RGBA{R: 220, G: 40, B: 40, A: 255}),
		"word/media/image2.jpeg":       solidJPEG(t, 200, 200, color.RGBA{R: 40, G: 40, B: 220, A: 255}),
		"word/media/image3.png":        solidPNG(t, 48, 48, color.RGBA{R: 90, G: 90, B: 90, A: 255}),
		"word/media/image4.png":        solidPNG(t, 300, 200, color.RGBA{R: 40, G: 180, B: 90, A: 255}),
		"word/media/image5.png":        solidPNG(t, 600, 100, color.RGBA{R: 10, G: 10, B: 10, A: 255}),
	})
}

// engine/convert/fixtures_test.go — test-helper fixture generators
// (BUILD.md Phase 1B activity 6).
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

// --- .docx fixture ------------------------------------------------------

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

// --- .pptx fixture ------------------------------------------------------

// buildPptxFixture assembles a two-slide deck: slide 1 has a title, a body
// with two outline levels, a table and speaker notes (resolved via rels);
// slide 2 has an untitled French body only.
func buildPptxFixture(t *testing.T) []byte {
	const ns = ` xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`

	slide1 := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld` + ns + `><p:cSld><p:spTree>` +
		`<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>Quarterly Review</a:t></a:r></a:p></p:txBody></p:sp>` +
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

// --- .xlsx fixture ------------------------------------------------------

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

// --- .pdf fixtures ------------------------------------------------------

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

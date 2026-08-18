//go:build integration

// engine/exportfmt/exportfmt_integration_test.go — the same-format export
// round-trips.
//
// TIER: integration (docs/TESTING.md). Each test rewrites a real OOXML archive
// and re-imports it through the existing converter, asserting that
// placeholders replace originals, untouched zip entries stay bit-identical,
// spanning replacements land correctly, and xlsx keeps its formulas and
// numeric types. That is real-format behaviour and full round-trip wiring,
// which the integration tier owns. The shared helpers (fixture, testConfig,
// zipEntryHashes, buildZip) are in the untagged exportfmt_test.go.
package exportfmt

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/engine/convert"
)

// TestDocxRoundTrip: the exported copy re-imports through the existing
// converter with every expected placeholder and no original term, and
// every entry without a rewriter is bit-identical.
func TestDocxRoundTrip(t *testing.T) {
	raw := fixture(t, "report.docx")
	cfg := testConfig(
		engine.Value{Category: "person_names", MainText: "Marie Duval"},
		engine.Value{Category: "person_names", MainText: "Amélie Lefèvre"},
	)
	cfg.Allowlist.Add("Luxembourg")

	out, extras, bodyCount, err := ExportDocx(raw, cfg)
	if err != nil {
		t.Fatalf("ExportDocx: %v", err)
	}
	if bodyCount == 0 {
		t.Fatal("no replacements made in the body")
	}
	if extras.Total() != 0 {
		t.Errorf("fixture has no headers/footers, extras = %v", extras)
	}

	md, _, err := convert.Docx(out)
	if err != nil {
		t.Fatalf("exported docx no longer converts: %v", err)
	}
	for _, gone := range []string{"Marie Duval", "Amélie Lefèvre"} {
		if strings.Contains(md, gone) {
			t.Errorf("original %q leaked into the export:\n%s", gone, md)
		}
	}
	for _, want := range []string{"[PERSON_1]", "[PERSON_2]", "Luxembourg", "Engagement Report"} {
		if !strings.Contains(md, want) {
			t.Errorf("expected %q in the re-converted markdown:\n%s", want, md)
		}
	}

	// Byte-identical passthrough: same entry names, untouched entries
	// hash-identical.
	before := zipEntryHashes(t, raw)
	after := zipEntryHashes(t, out)
	if len(before) != len(after) {
		t.Fatalf("entry count changed: %d → %d", len(before), len(after))
	}
	for name, h := range before {
		if name == "word/document.xml" {
			continue
		}
		if after[name] != h {
			t.Errorf("untouched entry %q changed", name)
		}
	}
}

// TestPptxRoundTrip: slides AND speaker notes are rewritten; masters and
// friends stay untouched.
func TestPptxRoundTrip(t *testing.T) {
	raw := fixture(t, "deck.pptx")
	cfg := testConfig(
		engine.Value{Category: "entity_names", MainText: "Alpine Trust"},
		engine.Value{Category: "entity_names", MainText: "Borealis Fund"},
	)

	out, total, err := ExportPptx(raw, cfg)
	if err != nil {
		t.Fatalf("ExportPptx: %v", err)
	}
	if total < 2 {
		t.Fatalf("want replacements on slide and notes, got %d", total)
	}

	md, _, err := convert.Pptx(out)
	if err != nil {
		t.Fatalf("exported pptx no longer converts: %v", err)
	}
	if strings.Contains(md, "Borealis Fund") || strings.Contains(md, "Alpine Trust") {
		t.Errorf("originals leaked:\n%s", md)
	}
	if !strings.Contains(md, "[ENTITY_1]") || !strings.Contains(md, "[ENTITY_2]") {
		t.Errorf("placeholders missing (slide body and notes):\n%s", md)
	}

	before := zipEntryHashes(t, raw)
	after := zipEntryHashes(t, out)
	for name, h := range before {
		if isPptxTextPart(name) {
			continue
		}
		if after[name] != h {
			t.Errorf("untouched entry %q changed", name)
		}
	}
}

// TestSpanningReplacement: an entity split across two runs (bold
// mid-name) is replaced correctly, the document stays valid XML, and the
// replacement lands in the FIRST run.
func TestSpanningReplacement(t *testing.T) {
	documentXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		`<w:p><w:r><w:rPr><w:b/></w:rPr><w:t>Marie</w:t></w:r>` +
		`<w:r><w:t xml:space="preserve"> Duval attended with Peter</w:t></w:r>` +
		`<w:r><w:t xml:space="preserve"> Stone.</w:t></w:r></w:p>` +
		`</w:body></w:document>`
	raw := buildZip(t, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   documentXML,
	})

	cfg := testConfig(
		engine.Value{Category: "person_names", MainText: "Marie Duval"},
		engine.Value{Category: "person_names", MainText: "Peter Stone"},
	)
	out, _, _, err := ExportDocx(raw, cfg)
	if err != nil {
		t.Fatalf("ExportDocx: %v", err)
	}

	// Still valid XML?
	r, err := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatal(err)
	}
	var doc []byte
	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			src, _ := f.Open()
			doc, _ = io.ReadAll(src)
			src.Close()
		}
	}
	var anyXML interface{}
	if err := xml.Unmarshal(doc, &anyXML); err != nil {
		t.Fatalf("rewritten document.xml is not valid XML: %v\n%s", err, doc)
	}

	text := string(doc)
	if strings.Contains(text, "Marie") || strings.Contains(text, "Duval") ||
		strings.Contains(text, "Stone") {
		t.Errorf("split-run originals leaked:\n%s", text)
	}
	// The spanning replacement is written wholly into the run where it
	// STARTS: [PERSON_1] sits in the bold run, [PERSON_2] in run 2.
	if !strings.Contains(text, "<w:rPr><w:b/></w:rPr><w:t>[PERSON_1]</w:t>") {
		t.Errorf("spanning replacement must land in its first (bold) run:\n%s", text)
	}
	if !strings.Contains(text, "[PERSON_2]") {
		t.Errorf("second spanning replacement missing:\n%s", text)
	}

	md, _, err := convert.Docx(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "[PERSON_1]") || !strings.Contains(md, "[PERSON_2]") {
		t.Errorf("re-converted markdown lost placeholders:\n%s", md)
	}
}

// TestDocxHeaderExtras: headers were dropped at import, but the
// deterministic PII pass still cleans them on export, counted in Extras.
func TestDocxHeaderExtras(t *testing.T) {
	raw := buildZip(t, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:r><w:t>Body text.</w:t></w:r></w:p></w:body></w:document>`,
		"word/header1.xml": `<?xml version="1.0"?><w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			`<w:p><w:r><w:t>Contact marie.duval@example.com today</w:t></w:r></w:p></w:hdr>`,
	})

	out, extras, _, err := ExportDocx(raw, testConfig())
	if err != nil {
		t.Fatalf("ExportDocx: %v", err)
	}
	if extras["word/header1.xml"] != 1 || extras.Total() != 1 {
		t.Errorf("header email must be counted in extras, got %v", extras)
	}
	r, _ := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	for _, f := range r.File {
		if f.Name != "word/header1.xml" {
			continue
		}
		src, _ := f.Open()
		content, _ := io.ReadAll(src)
		src.Close()
		if strings.Contains(string(content), "marie.duval@example.com") {
			t.Errorf("header email leaked:\n%s", content)
		}
		if !strings.Contains(string(content), "[EMAIL_1]") {
			t.Errorf("header email placeholder missing:\n%s", content)
		}
	}
}

// TestXlsxRoundTrip: string cells are rewritten, formulas and numeric
// cells are untouched, merged cells survive.
func TestXlsxRoundTrip(t *testing.T) {
	// Bespoke workbook: strings + a numeric cell + a formula + a merge.
	src := excelize.NewFile()
	if err := src.SetSheetName("Sheet1", "Data"); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(src.SetCellStr("Data", "A1", "Client"))
	must(src.SetCellStr("Data", "A2", "Alpine Trust"))
	must(src.SetCellStr("Data", "B2", "marie.duval@example.com"))
	must(src.SetCellValue("Data", "C2", 12500)) // numeric, must survive as a number
	must(src.SetCellFormula("Data", "D2", "C2*2"))
	must(src.MergeCell("Data", "A4", "B4"))
	must(src.SetCellStr("Data", "A4", "Alpine Trust summary"))
	var buf bytes.Buffer
	must(src.Write(&buf))
	src.Close()

	cfg := testConfig(engine.Value{Category: "entity_names", MainText: "Alpine Trust"})
	out, total, err := ExportXlsx(buf.Bytes(), cfg)
	if err != nil {
		t.Fatalf("ExportXlsx: %v", err)
	}
	if total < 3 {
		t.Errorf("want at least 3 replacements (2 client + email), got %d", total)
	}

	res, err := excelize.OpenReader(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("exported workbook no longer opens: %v", err)
	}
	defer res.Close()

	if v, _ := res.GetCellValue("Data", "A2"); v != "[ENTITY_1]" {
		t.Errorf("A2 = %q, want [ENTITY_1]", v)
	}
	if v, _ := res.GetCellValue("Data", "B2"); v != "[EMAIL_1]" {
		t.Errorf("B2 = %q, want [EMAIL_1]", v)
	}
	if v, _ := res.GetCellValue("Data", "C2"); v != "12500" {
		t.Errorf("numeric cell changed: %q", v)
	}
	if ct, _ := res.GetCellType("Data", "C2"); ct == excelize.CellTypeSharedString || ct == excelize.CellTypeInlineString {
		t.Error("numeric cell was stringified")
	}
	if f, _ := res.GetCellFormula("Data", "D2"); f != "C2*2" {
		t.Errorf("formula changed: %q", f)
	}
	if v, _ := res.GetCellValue("Data", "A4"); !strings.Contains(v, "[ENTITY_1]") {
		t.Errorf("merged cell not rewritten: %q", v)
	}
	merges, _ := res.GetMergeCells("Data")
	if len(merges) != 1 {
		t.Errorf("merged region lost: %v", merges)
	}
}

// TestEmptyMappingNoOp: with nothing to replace, the export re-converts
// to markdown identical to the input's conversion (no-op safety).
func TestEmptyMappingNoOp(t *testing.T) {
	raw := fixture(t, "report.docx")
	cfg := testConfig() // no entities; medium level PII finds nothing here

	before, _, err := convert.Docx(raw)
	if err != nil {
		t.Fatal(err)
	}
	out, _, count, err := ExportDocx(raw, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("no-op export made %d replacements", count)
	}
	after, _, err := convert.Docx(out)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Errorf("no-op export changed the conversion:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

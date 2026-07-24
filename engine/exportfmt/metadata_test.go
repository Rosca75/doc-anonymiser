// engine/exportfmt/metadata_test.go — BUILD-02 Phase 12 tests: property
// extraction, pipeline proposals (allowlist wins), reviewed rewrite
// round-trip, non-string custom properties untouched, and the filename
// mapping table.
package exportfmt

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"doc-anonymiser/engine"
)

// buildPropsDocx assembles a minimal docx with authored core, app and
// custom properties (the props.docx equivalent from the plan, built by a
// test helper).
func buildPropsDocx(t *testing.T) []byte {
	t.Helper()
	return buildZip(t, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Body about Alpine Trust.</w:t></w:r></w:p></w:body></w:document>`,
		"docProps/core.xml": `<?xml version="1.0"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/">` +
			`<dc:title>Alpine Trust engagement report</dc:title>` +
			`<dc:subject>CSSF filing</dc:subject>` +
			`<dc:creator>Marie Duval</dc:creator>` +
			`<cp:lastModifiedBy>Marie Duval</cp:lastModifiedBy>` +
			`<cp:keywords>alpine; audit</cp:keywords>` +
			`<dc:description>Prepared for Alpine Trust &amp; partners</dc:description>` +
			`</cp:coreProperties>`,
		"docProps/app.xml": `<?xml version="1.0"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties">` +
			`<Company>Alpine Trust S.A.</Company><Manager>Marie Duval</Manager><Pages>3</Pages></Properties>`,
		"docProps/custom.xml": `<?xml version="1.0"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/custom-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes">` +
			`<property fmtid="{D5CDD505-2E9C-101B-9397-08002B2CF9AE}" pid="2" name="Client"><vt:lpwstr>Alpine Trust</vt:lpwstr></property>` +
			`<property fmtid="{D5CDD505-2E9C-101B-9397-08002B2CF9AE}" pid="3" name="Reviewed"><vt:bool>true</vt:bool></property>` +
			`</Properties>`,
	})
}

func TestMetadataExtractAndPropose(t *testing.T) {
	raw := buildPropsDocx(t)
	fields, err := ExtractMetadata(raw)
	if err != nil {
		t.Fatalf("ExtractMetadata: %v", err)
	}
	byName := map[string]MetaField{}
	for _, f := range fields {
		byName[f.Part+"|"+f.Name] = f
	}
	if f := byName["docProps/core.xml|creator"]; f.Value != "Marie Duval" {
		t.Errorf("creator = %+v", f)
	}
	if f := byName["docProps/app.xml|Company"]; f.Value != "Alpine Trust S.A." {
		t.Errorf("Company = %+v", f)
	}
	if f := byName["docProps/custom.xml|Client"]; f.Value != "Alpine Trust" {
		t.Errorf("custom Client = %+v", f)
	}
	if f := byName["docProps/core.xml|description"]; f.Value != "Prepared for Alpine Trust & partners" {
		t.Errorf("entity-decoded description = %+v", f)
	}
	if _, ok := byName["docProps/custom.xml|Reviewed"]; ok {
		t.Error("non-string custom property must not be harvested")
	}

	// Proposals: pipeline path, allowlist wins ("CSSF" survives in the
	// subject although it looks like an organisation).
	cfg := testConfig(
		engine.Entity{Category: "client_names", Canonical: "Alpine Trust"},
		engine.Entity{Category: "person_names", Canonical: "Marie Duval"},
	)
	cfg.Allowlist.Add("CSSF")
	props := ProposeMetadata(fields, cfg)
	byKey := map[string]MetaProposal{}
	for _, p := range props {
		byKey[p.Part+"|"+p.Name] = p
	}
	if p := byKey["docProps/core.xml|creator"]; !p.Changed || !strings.Contains(p.Proposed, "[PERSON_") {
		t.Errorf("creator proposal = %+v", p)
	}
	if p := byKey["docProps/core.xml|subject"]; strings.Contains(p.Proposed, "[") || !strings.Contains(p.Proposed, "CSSF") {
		t.Errorf("allowlisted CSSF must survive in the subject: %+v", p)
	}
	if p := byKey["docProps/app.xml|Pages"]; p.Part != "" {
		t.Errorf("Pages is not a harvested field: %+v", p)
	}
}

func TestMetadataRewriteRoundTrip(t *testing.T) {
	raw := buildPropsDocx(t)
	reviewed := []MetaField{
		{Part: "docProps/core.xml", Name: "creator", Value: "[PERSON_1]"},
		{Part: "docProps/core.xml", Name: "title", Value: "[CLIENT_1] engagement report"},
		{Part: "docProps/app.xml", Name: "Company", Value: "[CLIENT_1]"},
		{Part: "docProps/custom.xml", Name: "Client", Value: "[CLIENT_1]"},
		// A rejected field simply is not in this list: lastModifiedBy
		// keeps "Marie Duval".
	}
	out, err := RewriteMetadata(raw, reviewed)
	if err != nil {
		t.Fatalf("RewriteMetadata: %v", err)
	}
	fields, err := ExtractMetadata(out)
	if err != nil {
		t.Fatalf("re-extract: %v", err)
	}
	byName := map[string]string{}
	for _, f := range fields {
		byName[f.Part+"|"+f.Name] = f.Value
	}
	if byName["docProps/core.xml|creator"] != "[PERSON_1]" ||
		byName["docProps/core.xml|title"] != "[CLIENT_1] engagement report" ||
		byName["docProps/app.xml|Company"] != "[CLIENT_1]" ||
		byName["docProps/custom.xml|Client"] != "[CLIENT_1]" {
		t.Errorf("reviewed values did not round-trip: %v", byName)
	}
	if byName["docProps/core.xml|lastModifiedBy"] != "Marie Duval" {
		t.Errorf("rejected field must keep its original value: %v", byName)
	}

	// The boolean custom property survived verbatim.
	outStr := extractEntry(t, out, "docProps/custom.xml")
	if !strings.Contains(outStr, "<vt:bool>true</vt:bool>") {
		t.Errorf("non-string custom property was touched:\n%s", outStr)
	}
	// Word count metadata part in app.xml untouched too.
	if app := extractEntry(t, out, "docProps/app.xml"); !strings.Contains(app, "<Pages>3</Pages>") {
		t.Errorf("unharvested app field was touched:\n%s", app)
	}
}

// extractEntry reads one entry of a zip archive as a string.
func extractEntry(t *testing.T, raw []byte, name string) string {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.File {
		if f.Name == name {
			src, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer src.Close()
			content, err := io.ReadAll(src)
			if err != nil {
				t.Fatal(err)
			}
			return string(content)
		}
	}
	t.Fatalf("entry %q missing", name)
	return ""
}

func TestSameFormatFileName(t *testing.T) {
	reg := engine.NewRegistry()
	reg.Assign("client_names", "Alpine Trust")
	reg.Assign("person_names", "Amélie Lefèvre")
	entries := reg.Entries()

	cases := []struct {
		name, docName, ext, want string
	}{
		{"client term replaced", "Alpine Trust report.docx", "docx", "CLIENT_1 report_anon.docx"},
		{"entirely one entity falls back", "Alpine Trust.docx", "docx", "CLIENT_1_anon.docx"},
		{"unicode name replaced", "notes Amélie Lefèvre.pptx", "pptx", "notes PERSON_1_anon.pptx"},
		{"sheet hash sanitised", "workbook.xlsx#Clients", "xlsx", "workbook.xlsx_Clients_anon.xlsx"},
		{"glued original falls back", "AlpineTrustAlpine Trust.docx", "docx", "document_anon_7.docx"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := engine.SameFormatFileName(tc.docName, tc.ext, entries, 7)
			if got != tc.want {
				t.Errorf("SameFormatFileName(%q) = %q, want %q", tc.docName, got, tc.want)
			}
		})
	}
}

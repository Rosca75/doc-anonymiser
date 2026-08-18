//go:build integration

// engine/exportfmt/pdf_integration_test.go — PDF metadata extraction and the
// regenerated-PDF fallback round-trip.
//
// TIER: integration (docs/TESTING.md). These read a committed PDF fixture and
// regenerate a whole PDF through fpdf, then re-extract it with the import
// reader: real-format behaviour and a full round-trip. The fixture-free
// ExportPDF guards (empty-text rejection, the leak self-check) are business
// rules and live in the unit-tier exportfmt_test.go instead. The helpers
// pdfFixture and extractAllPDFText are shared from the untagged
// exportfmt_test.go.
package exportfmt

import (
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/engine/convert"
)

func TestExtractPDFMetadata(t *testing.T) {
	// textlayer.pdf carries no Info dictionary: no fields, no error.
	fields, err := ExtractPDFMetadata(pdfFixture(t, "textlayer.pdf"))
	if err != nil {
		t.Fatalf("ExtractPDFMetadata: %v", err)
	}
	if len(fields) != 0 {
		t.Errorf("fixture has no Info dict, got %+v", fields)
	}
	// Garbage bytes produce an actionable error.
	if _, err := ExtractPDFMetadata([]byte("not a pdf")); err == nil {
		t.Error("garbage must be rejected")
	}
}

func TestExportPDFFallbackRoundTrip(t *testing.T) {
	// Convert the fixture, anonymise its working text through a real
	// registry, regenerate the PDF and re-extract: placeholders present,
	// originals gone.
	raw := pdfFixture(t, "textlayer.pdf")
	md, _, err := convert.PDF(raw)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(engine.Value{Category: "entity_names", MainText: "Alpine Trust"})
	anonymised, n := cfg.AnonymiseText(md)
	if n == 0 || strings.Contains(anonymised, "Alpine Trust") {
		t.Fatalf("working text not anonymised: %q", anonymised)
	}

	reviewed := []MetaField{
		{Part: "pdf:Info", Name: "Title", Value: "[ENTITY_1] engagement"},
		{Part: "pdf:Info", Name: "Author", Value: "[PERSON_1]"},
	}
	out, err := ExportPDF(anonymised, reviewed, cfg)
	if err != nil {
		t.Fatalf("ExportPDF: %v", err)
	}

	// Re-extract with the import reader: placeholders in, originals out.
	extracted := extractAllPDFText(t, out)
	if !strings.Contains(extracted, "[ENTITY_1]") {
		t.Errorf("placeholder missing from the regenerated PDF text: %q", extracted)
	}
	if strings.Contains(extracted, "Alpine Trust") {
		t.Errorf("original leaked into the regenerated PDF: %q", extracted)
	}

	// The reviewed metadata landed in the new Info dictionary.
	meta, err := ExtractPDFMetadata(out)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]string{}
	for _, f := range meta {
		byName[f.Name] = f.Value
	}
	if byName["Title"] != "[ENTITY_1] engagement" || byName["Author"] != "[PERSON_1]" {
		t.Errorf("reviewed metadata not applied: %v", byName)
	}
}

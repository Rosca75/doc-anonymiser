// engine/exportfmt/pdf_test.go —  tests: PDF metadata
// extraction, the regenerated-PDF fallback with reviewed metadata, the
// scanned-PDF rejection, and the runtime leak self-check failure path.
package exportfmt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bytes"
	ledongthuc "github.com/ledongthuc/pdf"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/engine/convert"
)

// pdfFixture loads a committed PDF fixture.
func pdfFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("fixture %s missing, run 'go test ./engine/convert/' once to generate it: %v", name, err)
	}
	return data
}

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

func TestExportPDFRejectsEmptyText(t *testing.T) {
	// A scanned PDF never yields working text; the export refuses with
	// the pinned scanned-PDF message.
	_, err := ExportPDF("   ", nil, testConfig())
	if err == nil || !strings.Contains(err.Error(), "No text layer found") {
		t.Errorf("empty text must be rejected with the scanned-PDF message, got %v", err)
	}
}

func TestExportPDFSelfCheckBlocksLeaks(t *testing.T) {
	// Inject a mapping the generator "ignores": the anonymised text
	// still contains a registry original, so the self-check must FAIL
	// the export and no bytes may be returned.
	cfg := testConfig()
	cfg.Registry.Assign("entity_names", "Zephyr Capital")
	out, err := ExportPDF("This text still mentions Zephyr Capital openly.", nil, cfg)
	if err == nil || out != nil {
		t.Fatalf("leaky export must fail, got bytes=%v err=%v", out != nil, err)
	}
	if !strings.Contains(err.Error(), "self-check failed") {
		t.Errorf("error must name the self-check: %v", err)
	}
	if strings.Contains(err.Error(), "Zephyr Capital") {
		t.Errorf("the full original must not be repeated in the error: %v", err)
	}
}

// extractAllPDFText reads every page of a PDF with the import reader.
func extractAllPDFText(t *testing.T, raw []byte) string {
	t.Helper()
	reader, err := ledongthuc.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("generated PDF unreadable: %v", err)
	}
	var b strings.Builder
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		if text, err := page.GetPlainText(nil); err == nil {
			b.WriteString(text)
			b.WriteString("\n")
		}
	}
	return b.String()
}

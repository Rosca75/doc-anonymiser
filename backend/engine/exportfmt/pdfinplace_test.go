// engine/exportfmt/pdfinplace_test.go — in-place PDF anonymisation tests.
//
// These exercise the PDFium-backed editor and therefore require the
// experimental build tag: run with `go test -tags pdfium_experimental`.
// Without the tag the editor returns an error and ExportPDF falls back to
// regeneration (covered by pdf_test.go), so these are gated to the tag.
//go:build pdfium_experimental

package exportfmt

import (
	"strings"
	"testing"

	"doc-anonymiser/backend/engine"
)

// TestExportPDFInPlacePreservesAndAnonymises edits the committed classic
// fixture in place: the placeholder appears, the original is gone, and the
// document is genuinely edited (not regenerated) so the other text object
// survives untouched.
func TestExportPDFInPlacePreservesAndAnonymises(t *testing.T) {
	raw := pdfFixture(t, "textlayer.pdf")
	cfg := testConfig(engine.Entity{Category: "entity_names", Canonical: "Alpine Trust"})
	// Seed the registry the way a real run would, so AnonymiseText has a
	// placeholder to emit for the known original.
	cfg.Registry.Assign("entity_names", "Alpine Trust")

	out, err := ExportPDF(raw, "unused when in-place succeeds", nil, cfg)
	if err != nil {
		t.Fatalf("ExportPDF in-place: %v", err)
	}

	inst, err := pdfiumInstance()
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close()
	text, err := extractAllTextPDFium(inst, out)
	if err != nil {
		t.Fatalf("re-extract: %v", err)
	}
	if strings.Contains(text, "Alpine Trust") {
		t.Errorf("original leaked into the edited PDF: %q", text)
	}
	if !strings.Contains(text, "[ENTITY_1]") {
		t.Errorf("placeholder missing from the edited PDF: %q", text)
	}
	// The surrounding non-PII text of both objects must survive, proving we
	// edited in place rather than regenerating from scratch. (The email on
	// the second line is itself PII and is correctly replaced by [EMAIL_1].)
	if !strings.Contains(text, "engagement") || !strings.Contains(text, "double spaces on purpose") {
		t.Errorf("untouched text was lost, editor did not preserve the page: %q", text)
	}
	if !strings.Contains(text, "[EMAIL_1]") {
		t.Errorf("regex PII in a second object was not anonymised in place: %q", text)
	}
}

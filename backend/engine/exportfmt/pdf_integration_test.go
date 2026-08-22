//go:build integration

// engine/exportfmt/pdf_integration_test.go — PDF metadata extraction.
//
// TIER: integration (docs/TESTING.md). It reads a committed PDF fixture, so
// it is real-format behaviour rather than pure logic. The in-place export's
// own round-trips (the ladder, the refusal, the surface scrubbing, the leak
// scan) live in pdfinplace_integration_test.go; this file covers only the
// metadata READ, which the review panel depends on before any export runs.
// The pdfFixture helper is shared from the untagged exportfmt_test.go.
package exportfmt

import (
	"testing"
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

//go:build integration

// engine/convert/pdffoss_gate_integration_test.go — the adoption gate's
// extraction measurements (docs/change-13b.md step 7.1, the fixture half of
// G4 and D8).
//
// Integration tier: it reads committed binary fixtures from disk and runs the
// vendored PDF library over them, which is real-format I/O rather than pure
// logic. Deterministic and hermetic: no network, no service.
//
// The question: does the library's extraction read AT LEAST what the current
// ledongthuc-based converter reads, on files whose planted content is known?
// Counts and presence checks only; the reference-document half of G4 is the
// deep tier.
package convert

import (
	"bytes"
	"strings"
	"testing"

	asposepdf "github.com/aspose-pdf-foss/aspose-pdf-foss-for-go"
	pdflib "github.com/ledongthuc/pdf"
)

// gateOpen opens fixture bytes through the library's bytes-only entry point,
// the only constructor the engine is allowed (pdf_boundary_test.go).
func gateOpen(t *testing.T, raw []byte) *asposepdf.Document {
	t.Helper()
	doc, err := asposepdf.OpenStream(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the library could not open the fixture (%v); the fixture is committed, so this is a library defect", err)
	}
	return doc
}

// libraryPageTexts extracts every page's text in the library's default
// (visual reading order) mode.
func libraryPageTexts(t *testing.T, doc *asposepdf.Document) []string {
	t.Helper()
	texts, err := doc.ExtractText()
	if err != nil {
		t.Fatalf("Document.ExtractText: %v", err)
	}
	return texts
}

func TestPDFFossGateExtraction(t *testing.T) {
	// The names buildPDFGateText plants, one per behaviour the extraction has
	// to preserve: prose on each page, an email address, a 9 pt value, and
	// the two halves of the line-wrapped value.
	planted := []string{
		"Ostrell Group", "Harriet Volkmer", "harriet.volkmer@ostrell.example",
		"Quentin Marsh",
		"Societe Miradour", "Jean-Baptiste Ferrand", "Luxembourg",
		"Victor", "Beaulieu",
	}

	t.Run("extraction/parity_every_planted_name_in_both_extractors", func(t *testing.T) {
		raw := fixture(t, "pdf_gate_text.pdf")

		_, incumbentPages, _, err := PDFWithPages(raw)
		if err != nil {
			t.Fatalf("the incumbent extractor failed on the gate fixture: %v", err)
		}
		incumbent := strings.Join(incumbentPages, "\n")

		doc := gateOpen(t, raw)
		libPages := libraryPageTexts(t, doc)
		library := strings.Join(libPages, "\n")

		for _, name := range planted {
			if !strings.Contains(incumbent, name) {
				t.Errorf("planted name %q is missing from the INCUMBENT extraction; the fixture or ledongthuc changed underneath the gate", name)
			}
			if !strings.Contains(library, name) {
				t.Errorf("planted name %q is missing from the LIBRARY extraction; G4 (no extraction regression) fails on the fixture half", name)
			}
		}
	})

	t.Run("extraction/page_shape_matches_the_incumbent", func(t *testing.T) {
		raw := fixture(t, "pdf_gate_text.pdf")
		_, incumbentPages, _, err := PDFWithPages(raw)
		if err != nil {
			t.Fatalf("the incumbent extractor failed: %v", err)
		}
		doc := gateOpen(t, raw)
		if got, want := doc.PageCount(), len(incumbentPages); got != want {
			t.Errorf("library PageCount() = %d, incumbent produced %d pages; the page shape feeds ScanChunks and must not drift", got, want)
		}
		libPages := libraryPageTexts(t, doc)
		if got, want := len(libPages), len(incumbentPages); got != want {
			t.Errorf("library ExtractText returned %d page texts, incumbent %d", got, want)
		}
	})

	// The D8 measurement's fixture half: how often would RepairPDFText still
	// change a line of each extractor's output? textlayer.pdf carries the
	// planted kerning defect ("B R IDDING ULES"), so the incumbent count is
	// known to be nonzero there; what the gate records is the library's count
	// beside it.
	t.Run("extraction/spacing_repair_counts_per_extractor", func(t *testing.T) {
		for _, name := range []string{"textlayer.pdf", "pdf_gate_text.pdf"} {
			raw := fixture(t, name)

			countRepairs := func(text string) int {
				n := 0
				for _, line := range strings.Split(text, "\n") {
					if RepairPDFText(line) != line {
						n++
					}
				}
				return n
			}

			_, incumbentPages, _, err := PDFWithPages(raw)
			if err != nil {
				t.Fatalf("incumbent extraction of %s: %v", name, err)
			}
			// PDFWithPages already ran the repair, so the incumbent's count is
			// measured on the RAW ledongthuc text, the same footing as the
			// library's.
			incumbentRaw := rawLedongthucText(t, raw)
			doc := gateOpen(t, raw)
			libText := strings.Join(libraryPageTexts(t, doc), "\n")

			t.Logf("D8 fixture measurement, %s: lines the repair would change: incumbent(raw)=%d library=%d (post-repair incumbent pages: %d)",
				name, countRepairs(incumbentRaw), countRepairs(libText), len(incumbentPages))

			// The gate's assertion: the repair is IDEMPOTENT and harmless over
			// the library's output (a repair that mangles the new extractor's
			// text would force D8's retirement branch by itself).
			repairedOnce := RepairPDFText(libText)
			if RepairPDFText(repairedOnce) != repairedOnce {
				t.Errorf("RepairPDFText is not idempotent over the library's extraction of %s", name)
			}
		}
	})

	// G9's scanned-file half lives beside extraction because the refusal IS
	// an extraction outcome: the library must agree with the incumbent that
	// scanned.pdf has no text layer, so convert.ErrScannedPDF keeps firing
	// byte-identically when the extractor changes in 13c.
	t.Run("errors/scanned_pdf_is_detectable_as_textless", func(t *testing.T) {
		raw := fixture(t, "scanned.pdf")
		doc := gateOpen(t, raw)
		for i, text := range libraryPageTexts(t, doc) {
			if strings.TrimSpace(text) != "" {
				t.Errorf("scanned.pdf page %d extracted %q through the library; the textless check behind ErrScannedPDF would stop firing", i+1, text)
			}
		}
	})
}

// rawLedongthucText extracts with ledongthuc directly, WITHOUT the repair, so
// the two extractors' repair counts are measured on the same footing (raw
// text on both sides). Duplicating the extraction loop here is deliberate:
// PDFWithPages applies the repair inline, and exporting a no-repair mode from
// production code for one measurement would be an API with one caller, in a
// test.
func rawLedongthucText(t *testing.T, raw []byte) string {
	t.Helper()
	reader, err := pdflib.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("ledongthuc could not open the fixture: %v", err)
	}
	var out strings.Builder
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		if text, err := page.GetPlainText(nil); err == nil {
			out.WriteString(text)
			out.WriteString("\n")
		}
	}
	return out.String()
}

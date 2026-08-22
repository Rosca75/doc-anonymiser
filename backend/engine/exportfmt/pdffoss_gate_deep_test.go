//go:build deep

// engine/exportfmt/pdffoss_gate_deep_test.go — the adoption gate's
// reference-document measurements (criteria G4, G5, G7 and G8 on the owner's
// real documents), against the PRODUCTION extraction and the PRODUCTION
// location ladder.
//
// Deep tier: it reads confidential documents that live OUTSIDE the
// repository, measures wall clock, and its results depend on the machine. The
// documents are reached ONLY through environment variables and every number
// reported is a COUNT or a duration: no string from their content may reach a
// fixture, a log line, a commit message, a document in docs/ or a memory.
//
// On a machine without the documents every test SKIPS with the message that
// says how to run it, so the tier is never red for a missing input.
package exportfmt

import (
	"bytes"
	"image"
	"os"
	"strings"
	"testing"
	"time"

	asposepdf "github.com/aspose-pdf-foss/aspose-pdf-foss-for-go"

	"doc-anonymiser/backend/engine/convert"
)

// referenceDocs lists the env-var-supplied confidential documents. The deck
// entry applies only when the owner supplies it as a PDF pair.
var referenceDocs = []struct {
	label  string
	envVar string
}{
	{"reference_pdf", "DOC_ANONYMISER_REFERENCE_PDF"},
	{"reference_deck", "DOC_ANONYMISER_REFERENCE_DECK"},
}

// referenceFloors is G4: the minimum number of values each category must
// yield per reference document, as a FLOOR rather than as a comparison
// against a second extractor.
//
// A floor is what the question reduces to when there is one extraction path.
// Comparing two parsers asks the stronger question, and it is answerable only
// for as long as a whole second parser is carried in the tree to answer it;
// carrying one for a test is a dependency in the shipped module, a second
// reading of every document, and a number that moves when the comparison
// library moves. A recorded floor asks the part that matters (does this
// extraction still find what it found when this was measured) and costs
// nothing.
//
// The numbers were measured on the owner's machine on 2026-08-22, over the
// production extraction, through detectionCounts. Counts only: no string from
// either document appears here, which is the rule the file header states.
//
// To RE-BASELINE (the right move when the reference documents themselves
// change, and the only move that is not a silent weakening): run this test,
// read the "G4 <label> category <cat>" log lines, and write the new numbers
// in. Do it as its own change, never folded into an extraction change: a
// commit that both alters extraction and moves the floor it is measured
// against cannot be reviewed.
var referenceFloors = map[string]map[string]int{
	"reference_pdf": {
		"entity_names": 1,
		"email":        4,
		"phone":        2,
		"date":         3,
		"postal_code":  1,
		"address":      1,
		"person_names": 20,
	},
	"reference_deck": {
		"date":             1,
		"person_names":     27,
		"identifier_names": 2,
	},
}

// referenceBytes reads one reference document, skipping with an actionable
// message when it is not supplied on this machine.
func referenceBytes(t *testing.T, envVar string) []byte {
	t.Helper()
	path := os.Getenv(envVar)
	if path == "" {
		t.Skipf("%s is unset; export it to the confidential reference document's path on the owner's machine to run this gate measurement, or drop -tags=deep", envVar)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s names %s, which could not be read (%v); fix the path or unset the variable", envVar, path, err)
	}
	return raw
}

// TestPDFFossGateReferenceDocuments takes every reference-document
// measurement in one pass per document, so each file is opened and extracted
// once per extractor rather than once per criterion.
func TestPDFFossGateReferenceDocuments(t *testing.T) {
	for _, ref := range referenceDocs {
		ref := ref
		t.Run("extraction/"+ref.label, func(t *testing.T) {
			raw := referenceBytes(t, ref.envVar)

			startLib := time.Now()
			_, libraryPages, _, err := convert.PDFWithPages(raw)
			libImport := time.Since(startLib)
			if err != nil {
				t.Fatalf("the production extractor failed on the reference document: %v", err)
			}

			// G8: import inside an ABSOLUTE budget. It used to be a ratio
			// against the second extractor's wall clock on the same run,
			// which was the better measurement while that extractor existed
			// and is unavailable now that it does not. 10s is generous
			// against the measured tens of milliseconds precisely so that it
			// catches an order-of-magnitude regression and nothing else: a
			// tight budget on a machine-dependent number is a test that fails
			// for reasons nobody can act on.
			t.Logf("G8 %s: production import %v (%d pages)", ref.label, libImport, len(libraryPages))
			if libImport > 10*time.Second {
				t.Errorf("G8 %s: production import took %v, over the 10s budget; extraction has regressed by an order of magnitude", ref.label, libImport)
			}

			// G4: detection counts per category over the production
			// extraction, against the recorded floors. See referenceFloors
			// for why this is a floor and not a comparison, and
			// referenceFloorTolerance for how much one category may move.
			libraryText := strings.Join(libraryPages, "\n\n")
			libCounts := detectionCounts(t, ref.label+"_production", libraryText)
			assertCategoryFloors(t, "G4 "+ref.label, referenceFloors[ref.label], libCounts)

			// G7: the ladder census, through the shared helper the
			// integration tier runs over the committed fixtures. Same
			// measurement, same assertion; this tier only adds scale.
			reportCensus(t, ref.label, runLadderCensus(t, raw))

			doc, _, err := openPDFForExport(raw)
			if err != nil {
				t.Fatalf("opening the reference document for the round trip: %v", err)
			}

			// G5 and G8 on the real file: open, save with the production
			// discipline, rasterise, count.
			var buf bytes.Buffer
			startExport := time.Now()
			doc.RemoveUnusedObjects()
			if _, err := doc.WriteTo(&buf); err != nil {
				t.Fatalf("WriteTo on the reference document: %v", err)
			}
			exportClock := time.Since(startExport)
			t.Logf("G8 %s: no-edit save wall clock %v (budget: 30s per document)", ref.label, exportClock)
			if exportClock > 30*time.Second {
				t.Errorf("G8 %s: export took %v, over the 30s budget", ref.label, exportClock)
			}
			reopened, err := asposepdf.OpenStream(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("re-opening the round-tripped reference document: %v", err)
			}
			original, err := asposepdf.OpenStream(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("re-opening the original reference document: %v", err)
			}
			for p := 1; p <= original.PageCount() && p <= 3; p++ {
				a, err := original.RenderImage(p, asposepdf.RenderOptions{DPI: 72})
				if err != nil {
					t.Fatalf("rendering page %d before: %v", p, err)
				}
				b, err := reopened.RenderImage(p, asposepdf.RenderOptions{DPI: 72})
				if err != nil {
					t.Fatalf("rendering page %d after: %v", p, err)
				}
				t.Logf("G5 %s page %d: %d differing pixels across a no-edit round trip", ref.label, p, deepPixelDiff(a, b))
			}
		})
	}
}

// deepPixelDiff counts differing pixels between two renders. The integration
// tier has a maskable variant; this file builds under -tags=deep alone, so it
// carries its own unmasked count.
func deepPixelDiff(a, b image.Image) int {
	bounds := a.Bounds()
	diff := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab != bb || aa != ba {
				diff++
			}
		}
	}
	return diff
}

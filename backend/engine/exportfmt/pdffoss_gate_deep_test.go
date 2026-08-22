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

	"doc-anonymiser/backend/engine"
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

// countAll totals a per-category count map.
func countAll(counts map[string]int) int {
	total := 0
	for _, n := range counts {
		total += n
	}
	return total
}

// detectionCounts runs the offline detection the gate compares: pass 1's
// preview over every category plus heuristic discovery at defaults, over one
// extraction, and returns values-found-per-category. Counts only.
func detectionCounts(t *testing.T, name, markdown string) map[string]int {
	t.Helper()
	counts := map[string]int{}
	doc, err := engine.Load(name+".md", []byte(markdown))
	if err != nil {
		t.Fatalf("loading the extraction as a document: %v", err)
	}
	sel := engine.CategorySelection{}
	for _, cat := range engine.AllPIICategories {
		sel[cat] = true
	}
	for _, m := range engine.PreviewPatternMatches([]engine.Document{doc}, sel, engine.CountryLU, false, nil) {
		counts[m.Category]++
	}
	for _, s := range engine.HeuristicDiscoverWithOptions(markdown, nil, engine.DefaultHeuristicDiscoveryOptions()) {
		counts[s.Category]++
	}
	return counts
}

// TestPDFFossGateReferenceDocuments takes every reference-document
// measurement in one pass per document, so each file is opened and extracted
// once per extractor rather than once per criterion.
func TestPDFFossGateReferenceDocuments(t *testing.T) {
	for _, ref := range referenceDocs {
		ref := ref
		t.Run("extraction/"+ref.label, func(t *testing.T) {
			raw := referenceBytes(t, ref.envVar)

			// G8 baseline: the retained ledongthuc extractor's import wall
			// clock, measured first so the budget has a denominator from the
			// same machine and run.
			startIncumbent := time.Now()
			_, incumbentPages, _, err := convert.PDFWithPagesLedongthuc(raw)
			incumbentImport := time.Since(startIncumbent)
			if err != nil {
				t.Fatalf("the ledongthuc extractor failed on the reference document: %v", err)
			}

			startLib := time.Now()
			_, libraryPages, _, err := convert.PDFWithPages(raw)
			libImport := time.Since(startLib)
			if err != nil {
				t.Fatalf("the production extractor failed on the reference document: %v", err)
			}

			// G8: import within 3x the baseline, with a 2 s absolute floor
			// beneath the ratio; the measured numbers land in the findings
			// log as counts and durations.
			t.Logf("G8 %s: baseline import %v, production import %v (pages: %d and %d)",
				ref.label, incumbentImport, libImport, len(incumbentPages), len(libraryPages))
			if libImport > 3*incumbentImport && libImport > 2*time.Second {
				t.Errorf("G8 %s: production import %v exceeds 3x the baseline's %v", ref.label, libImport, incumbentImport)
			}

			// G4: detection counts per category over each extraction. The
			// production extraction (fragment split, wrapped join, spacing
			// repair) must find at least what the baseline finds, per
			// category.
			incumbentText := strings.Join(incumbentPages, "\n\n")
			libraryText := strings.Join(libraryPages, "\n\n")
			incumbentCounts := detectionCounts(t, ref.label+"_incumbent", incumbentText)
			libCounts := detectionCounts(t, ref.label+"_production", libraryText)
			t.Logf("G4 %s: totals, baseline %d, production %d", ref.label, countAll(incumbentCounts), countAll(libCounts))
			for cat, n := range incumbentCounts {
				t.Logf("G4 %s category %s: baseline %d, production %d", ref.label, cat, n, libCounts[cat])
				if libCounts[cat] < n {
					t.Errorf("G4 %s: category %s regressed from %d to %d values under the production extraction", ref.label, cat, n, libCounts[cat])
				}
			}
			for cat, n := range libCounts {
				if _, ok := incumbentCounts[cat]; !ok {
					t.Logf("G4 %s: category %s found ONLY by the production extraction: %d values", ref.label, cat, n)
				}
			}

			// G7: the ladder census, re-run against the PRODUCTION ladder
			// over the PRODUCTION pipeline text: for every string detection
			// would replace on a page, which rung locates it there. The
			// target is known now, so this is an ASSERTION, not a log line:
			// UNLOCATED must be 0, or every survivor is explained in the
			// findings log (docs/change-13.md §7) before the batch is
			// accepted.
			doc, layouts, err := openPDFForExport(raw)
			if err != nil {
				t.Fatalf("opening the reference document for the census: %v", err)
			}
			var literal, tolerant, fragment, wrapped, unlocated int
			pages := doc.Pages()
			for pi, page := range pages {
				if pi >= len(layouts) {
					break
				}
				pageText := convert.RepairPDFText(convert.PDFPageText(layouts[pi]))
				searcher := livePDFSearcher{page: page}
				for _, needle := range gatherNeedles(t, pageText) {
					located := locatePDFValue(needle, "[CENSUS]", searcher, layouts[pi])
					switch located.rung {
					case rungLiteral:
						literal++
					case rungTolerant:
						tolerant++
					case rungFragment:
						fragment++
					case rungWrapped:
						wrapped++
					default:
						unlocated++
					}
				}
			}
			t.Logf("G7 %s ladder census (production ladder, production pipeline text): literal %d, tolerant %d, fragment %d, wrapped %d, UNLOCATED %d",
				ref.label, literal, tolerant, fragment, wrapped, unlocated)
			if unlocated != 0 {
				t.Errorf("G7 %s: %d occurrence(s) remain UNLOCATED; the acceptance target is 0. Each survivor must be explained in docs/change-13.md §7 (counts only, never the strings) before 13c is accepted",
					ref.label, unlocated)
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

// gatherNeedles runs the same offline detection the counts use and returns
// the strings a run would replace: pattern-preview texts plus heuristic
// suggestion main texts. The strings stay inside this process; only counts
// derived from them are logged.
func gatherNeedles(t *testing.T, markdown string) []string {
	t.Helper()
	doc, err := engine.Load("needles.md", []byte(markdown))
	if err != nil {
		t.Fatalf("loading extraction: %v", err)
	}
	sel := engine.CategorySelection{}
	for _, cat := range engine.AllPIICategories {
		sel[cat] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range engine.PreviewPatternMatches([]engine.Document{doc}, sel, engine.CountryLU, false, nil) {
		if !seen[m.Text] {
			seen[m.Text] = true
			out = append(out, m.Text)
		}
	}
	for _, s := range engine.HeuristicDiscoverWithOptions(markdown, nil, engine.DefaultHeuristicDiscoveryOptions()) {
		if !seen[s.MainText] {
			seen[s.MainText] = true
			out = append(out, s.MainText)
		}
	}
	return out
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

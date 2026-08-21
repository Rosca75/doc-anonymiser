//go:build deep

// engine/exportfmt/pdffoss_gate_deep_test.go — the adoption gate's
// reference-document measurements (docs/change-13b.md step 9; criteria G4,
// G5, G7 and G8 on the owner's real documents).
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
// entry applies only when the owner supplies it as a PDF pair
// (docs/change-13b.md step 9).
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

			// G8 baseline: the incumbent's import wall clock, measured first
			// so the budget has a denominator from the same machine and run.
			startIncumbent := time.Now()
			_, incumbentPages, _, err := convert.PDFWithPages(raw)
			incumbentImport := time.Since(startIncumbent)
			if err != nil {
				t.Fatalf("the incumbent extractor failed on the reference document: %v", err)
			}

			startLib := time.Now()
			doc, err := asposepdf.OpenStream(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("the library could not open the reference document: %v", err)
			}
			libTexts, err := doc.ExtractText()
			if err != nil {
				t.Fatalf("the library could not extract the reference document: %v", err)
			}
			libImport := time.Since(startLib)

			// G8: the recommended budget is import within 3x the incumbent
			// (docs/change-13b.md step 1); the measured numbers land in the
			// findings log.
			t.Logf("G8 %s: incumbent import %v, library import %v (pages: incumbent %d, library %d)",
				ref.label, incumbentImport, libImport, len(incumbentPages), len(libTexts))
			if libImport > 3*incumbentImport && libImport > 2*time.Second {
				t.Errorf("G8 %s: library import %v exceeds 3x the incumbent's %v", ref.label, libImport, incumbentImport)
			}

			// G4: detection counts per category over each extraction. The
			// library must find at least what the incumbent finds, per
			// category.
			incumbentCounts := detectionCounts(t, ref.label+"_incumbent", strings.Join(incumbentPages, "\n\n"))
			libCounts := detectionCounts(t, ref.label+"_library", strings.Join(libTexts, "\n\n"))
			for cat, n := range incumbentCounts {
				t.Logf("G4 %s category %s: incumbent %d, library %d", ref.label, cat, n, libCounts[cat])
				if libCounts[cat] < n {
					t.Errorf("G4 %s: category %s regressed from %d to %d values under the library's extraction", ref.label, cat, n, libCounts[cat])
				}
			}
			for cat, n := range libCounts {
				if _, ok := incumbentCounts[cat]; !ok {
					t.Logf("G4 %s: category %s found ONLY by the library's extraction: %d values", ref.label, cat, n)
				}
			}

			// D8: how many lines would the spacing repair still change, per
			// extractor's raw output.
			repairCount := func(text string) int {
				n := 0
				for _, line := range strings.Split(text, "\n") {
					if convert.RepairPDFText(line) != line {
						n++
					}
				}
				return n
			}
			t.Logf("D8 %s: lines the spacing repair would change in the library's extraction: %d", ref.label, repairCount(strings.Join(libTexts, "\n")))

			// G7: the ladder census. For every value detection would replace,
			// which rung locates it: the literal search, the
			// whitespace-tolerant pattern, the wrapped two-fragment step, or
			// none (UNLOCATED, D6's refusal).
			var literal, tolerant, wrapped, unlocated int
			for _, m := range gatherNeedles(t, strings.Join(libTexts, "\n\n")) {
				switch {
				case searchCount(t, doc, m, asposepdf.SearchOptions{}) > 0:
					literal++
				case searchCount(t, doc, tolerantPattern(m), asposepdf.SearchOptions{Regex: true}) > 0:
					tolerant++
				case wrappedLocatable(t, doc, m):
					wrapped++
				default:
					unlocated++
				}
			}
			t.Logf("G7 %s ladder census: literal %d, tolerant-pattern %d, wrapped %d, UNLOCATED %d",
				ref.label, literal, tolerant, wrapped, unlocated)

			// G5 on the real file: open, WriteTo, rasterise, count.
			var buf bytes.Buffer
			startExport := time.Now()
			if _, err := doc.WriteTo(&buf); err != nil {
				t.Fatalf("WriteTo on the reference document: %v", err)
			}
			exportClock := time.Since(startExport)
			t.Logf("G8 %s: no-edit WriteTo wall clock %v (budget: 30s per document)", ref.label, exportClock)
			if exportClock > 30*time.Second {
				t.Errorf("G8 %s: export took %v, over the 30s budget", ref.label, exportClock)
			}
			reopened, err := asposepdf.OpenStream(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("re-opening the round-tripped reference document: %v", err)
			}
			for p := 1; p <= doc.PageCount() && p <= 3; p++ {
				a, err := doc.RenderImage(p, asposepdf.RenderOptions{DPI: 72})
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

// searchCount counts SearchText matches document-wide, treating an error as
// zero (an unsearchable needle is exactly what the census is counting).
func searchCount(t *testing.T, doc *asposepdf.Document, query string, opts asposepdf.SearchOptions) int {
	t.Helper()
	matches, err := doc.SearchText(query, opts)
	if err != nil {
		return 0
	}
	return len(matches)
}

// tolerantPattern derives the RE2 pattern D5's second matching tier uses: the
// literal with every space seam tolerating the repairs the converter applies
// (an optional space at each inter-character seam the repair may have
// collapsed, and any whitespace run where the text has one space).
func tolerantPattern(literal string) string {
	var sb strings.Builder
	for _, r := range literal {
		if r == ' ' {
			sb.WriteString(`\s+`)
			continue
		}
		sb.WriteString(regexpQuoteRune(r))
		sb.WriteString(` ?`)
	}
	return strings.TrimSuffix(sb.String(), ` ?`)
}

// regexpQuoteRune escapes one rune for RE2.
func regexpQuoteRune(r rune) string {
	if strings.ContainsRune(`\.+*?()|[]{}^$`, r) {
		return `\` + string(r)
	}
	return string(r)
}

// wrappedLocatable prototypes the wrapped-match rung: the value's head is
// findable at some line, its tail at some line, and at least one head/tail
// pair sits on vertically adjacent lines.
func wrappedLocatable(t *testing.T, doc *asposepdf.Document, value string) bool {
	t.Helper()
	fields := strings.Fields(value)
	if len(fields) < 2 {
		return false
	}
	for split := 1; split < len(fields); split++ {
		head := strings.Join(fields[:split], " ")
		tail := strings.Join(fields[split:], " ")
		headMatches, err1 := doc.SearchText(head)
		tailMatches, err2 := doc.SearchText(tail)
		if err1 != nil || err2 != nil {
			continue
		}
		for _, h := range headMatches {
			for _, ta := range tailMatches {
				if h.PageNumber == ta.PageNumber && ta.Rect.URY < h.Rect.LLY &&
					h.Rect.LLY-ta.Rect.URY < 3*(h.Rect.URY-h.Rect.LLY) {
					return true
				}
			}
		}
	}
	return false
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

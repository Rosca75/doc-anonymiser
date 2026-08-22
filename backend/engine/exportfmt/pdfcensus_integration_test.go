//go:build integration

// engine/exportfmt/pdfcensus_integration_test.go — the ladder census and the
// per-category floors over the COMMITTED fixtures.
//
// TIER: integration (docs/TESTING.md). It reads committed PDF fixtures and runs
// the production extraction and the production ladder over them: real-format
// behaviour, deterministic, no network, no secrets.
//
// WHY THIS TIER. An occurrence the ladder cannot locate REFUSES a user's
// export. That rule has to be enforced where every push can see it, and the
// same measurements over the owner's confidential reference documents cannot
// do it: they need two environment variables and two files that exist on one
// machine. This file is the gate; the deep tier is the scale check.
//
// The fixtures are chosen for their SHAPE, and between them they cover the two
// ways a PDF's text defeats a naive search:
//
//   - framework_contract.pdf is Word print-to-PDF: justified paragraphs, so
//     values wrap across lines and word spacing is stretched.
//   - nstar_contoso_flyer.pdf is a generated flyer layout: short lines, wide
//     spacing, values sitting alone in their own text runs.
//   - working_deck.pdf is PowerPoint print-to-PDF, drawn with /Type0
//     /Identity-H fonts whose one-line ToUnicode CMaps are what the vendor
//     patch decodes. It is the only fixture whose text arrives through that
//     path at all, which is the reason it is here.
//
// All three are safe to read and quote, unlike the deep tier's inputs.
//
// One gap stays OPEN and is recorded rather than implied away: no committed
// fixture reaches the FRAGMENT WALK. Measured, this deck's values land
// literally (6) or through the tolerant rung (2), and the flyer and the
// contract reach neither. Only the owner's confidential reference deck has
// produced fragment-walk occurrences (10, plus 3 wrapped), so that rung is
// gated by the deep tier alone. Reading the passes below as coverage of the
// whole ladder would be wrong.
package exportfmt

import (
	"strings"
	"testing"

	"doc-anonymiser/backend/engine/convert"
)

// committedFloors is the per-category floor for each committed fixture, the
// same measurement referenceFloors records for the confidential documents and
// with the same tolerance.
//
// Measured 2026-08-22 over the production extraction. Re-baseline by running
// this test, reading the "category ... found N" lines and writing the numbers
// in, as its own change: a commit that both alters extraction and moves the
// floor it is measured against cannot be reviewed.
var committedFloors = map[string]map[string]int{
	"framework_contract.pdf": {
		"entity_names":     2,
		"date":             1,
		"identifier_names": 1,
		"person_names":     9,
	},
	"working_deck.pdf": {
		"url":          3,
		"person_names": 4,
	},
	"nstar_contoso_flyer.pdf": {
		"email":         2,
		"phone":         2,
		"amount":        5,
		"postal_code":   1,
		"url":           1,
		"address":       1,
		"person_names":  13,
		"entity_names":  7,
		"product_names": 4,
	},
}

// TestPDFLadderCensusOverCommittedFixtures is the gate: every string a run
// would replace in these documents must be locatable, and no category may
// quietly stop yielding values.
func TestPDFLadderCensusOverCommittedFixtures(t *testing.T) {
	for _, name := range []string{"framework_contract.pdf", "nstar_contoso_flyer.pdf", "working_deck.pdf"} {
		name := name
		t.Run("redaction/"+name, func(t *testing.T) {
			raw := pdfFixture(t, name)
			reportCensus(t, name, runLadderCensus(t, raw))

			_, pages, _, err := convert.PDFWithPages(raw)
			if err != nil {
				t.Fatalf("extracting %s: %v", name, err)
			}
			counts := detectionCounts(t, name, strings.Join(pages, "\n\n"))
			if floors := committedFloors[name]; len(floors) > 0 {
				assertCategoryFloors(t, name, floors, counts)
				return
			}
			// No floors recorded yet: log what is there so they can be
			// written in, and say plainly that nothing was asserted.
			t.Logf("%s: no floors recorded; measured total %d", name, countAll(counts))
			for cat, n := range counts {
				t.Logf("%s category %s: found %d", name, cat, n)
			}
			t.Errorf("%s has no entry in committedFloors, so its category counts are measured and NOT asserted; add the numbers above", name)
		})
	}
}

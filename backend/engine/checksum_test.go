// engine/checksum_test.go — the "only replace when the checksum matches" switch
//
// A checksum failure never vetoes a span on its own (CLAUDE.md §5): the shape
// is right and the digits do not add up, which is what a mistyped, partly
// redacted or synthetic bank identifier looks like. RequireChecksum is the user
// ASKING for the veto, and this file locks the three things that makes it.
//
//  1. It drops the checksum-failed pattern match and NOTHING else.
//  2. It cannot reach an accepted Value. A Value is replaced because the user
//     accepted it, whatever score the route that found it stamped on it, so no
//     confidence comparison anywhere in the pipeline may drop one. That is the
//     review gate: a run discarding an accepted Value answers "reject" on the
//     user's behalf after the user said accept, and it does so invisibly.
//  3. It leaves `piiPattern.validate` alone. Where a checksum IS the recognizer
//     the veto is mandatory and not the user's to switch, because without it
//     every long digit run in the document becomes a credit card.
package engine

import (
	"context"
	"strings"
	"testing"
)

// The two IBANs the cases below are built from. Both are shape-valid; only the
// second's mod-97 remainder is right, so pass 1 scores the first at
// ConfidenceChecksumFailed and the second at ConfidenceDeterministic.
const (
	checksumFailedIBAN = "LU88 0055 6600 4321 6501"
	checksumValidIBAN  = "LU28 0019 4006 4475 0000"
)

// runChecksum is the shared harness: one document, the given Values, the switch
// in the given position.
func runChecksum(t *testing.T, text string, values []Value, requireChecksum bool) string {
	t.Helper()
	res, err := Run(context.Background(), PipelineInput{
		Documents:       []Document{{Name: "note.md", Format: FormatMD, Markdown: text}},
		Values:          values,
		Categories:      DepthSelection(PresetStandard, CountryLU),
		RequireChecksum: requireChecksum,
		Allowlist:       NewEmptyAllowlist(),
	})
	if err != nil {
		t.Fatalf("Run returned an unexpected error: %v", err)
	}
	if len(res.Documents) != 1 {
		t.Fatalf("expected 1 result document, got %d", len(res.Documents))
	}
	return res.Documents[0].Anonymised
}

// TestAcceptedValueSurvivesItsOriginScore is the regression for the defect this
// switch replaced: the old cross-route floor compared a Value's Confidence,
// which carries the score of whatever originally found it, so raising it
// silently dropped Values the user had already accepted.
//
// 0.6 is below every producer's score, so a Value carrying it is the worst case
// the old filter would have discarded. It must be replaced in BOTH positions of
// the switch, because the switch is about a pattern's check digits and an
// accepted Value has none.
func TestAcceptedValueSurvivesItsOriginScore(t *testing.T) {
	const text = "Anouk Berger signed for the client.\n"
	values := []Value{{
		Category:         "person_names",
		MainText:         "Anouk Berger",
		DiscoveryMethods: []string{MethodHeuristic},
		Confidence:       0.6,
	}}
	for _, requireChecksum := range []bool{false, true} {
		got := runChecksum(t, text, values, requireChecksum)
		if strings.Contains(got, "Anouk Berger") {
			t.Errorf("requireChecksum=%v: the user accepted this Value, so it must be replaced whatever score found it, got:\n%s",
				requireChecksum, got)
		}
		if !strings.Contains(got, "[PERSON_") {
			t.Errorf("requireChecksum=%v: expected a person placeholder in:\n%s", requireChecksum, got)
		}
	}
}

// TestRequireChecksumDropsTheFailedMatchAndNothingElse: the switch is narrow, so
// the test is a document holding one of each thing it could reach.
func TestRequireChecksumDropsTheFailedMatchAndNothingElse(t *testing.T) {
	text := "Bad " + checksumFailedIBAN + ", good " + checksumValidIBAN +
		", write to marie.duval@example.com, and Anouk Berger signed.\n"
	values := []Value{{
		Category:         "person_names",
		MainText:         "Anouk Berger",
		DiscoveryMethods: []string{MethodLocalLLM},
		Confidence:       ConfidenceLLMDefault,
	}}

	off := runChecksum(t, text, values, false)
	for _, gone := range []string{checksumFailedIBAN, checksumValidIBAN, "marie.duval@example.com", "Anouk Berger"} {
		if strings.Contains(off, gone) {
			t.Errorf("with the switch off %q must be replaced, got:\n%s", gone, off)
		}
	}

	on := runChecksum(t, text, values, true)
	if !strings.Contains(on, checksumFailedIBAN) {
		t.Errorf("with the switch on the checksum-failed IBAN must be left in clear, got:\n%s", on)
	}
	for _, gone := range []string{checksumValidIBAN, "marie.duval@example.com", "Anouk Berger"} {
		if strings.Contains(on, gone) {
			t.Errorf("with the switch on %q is not what the switch is about and must still be replaced, got:\n%s",
				gone, on)
		}
	}

	// One span fewer, stated as a count as well as by string, so a change that
	// drops something extra fails here rather than in whatever reads the report.
	offCount := totalReplacements(t, text, values, false)
	onCount := totalReplacements(t, text, values, true)
	if onCount != offCount-1 {
		t.Errorf("the switch must cost exactly one replacement: %d with it on against %d with it off",
			onCount, offCount)
	}
}

// totalReplacements counts every replacement one run made, from the report,
// which is the same number the user is shown.
func totalReplacements(t *testing.T, text string, values []Value, requireChecksum bool) int {
	t.Helper()
	res, err := Run(context.Background(), PipelineInput{
		Documents:       []Document{{Name: "note.md", Format: FormatMD, Markdown: text}},
		Values:          values,
		Categories:      DepthSelection(PresetStandard, CountryLU),
		RequireChecksum: requireChecksum,
		Allowlist:       NewEmptyAllowlist(),
	})
	if err != nil {
		t.Fatalf("Run returned an unexpected error: %v", err)
	}
	return res.Report.TotalReplacements
}

// TestPreviewAndRunAgreeOnPatternMatches: the Identify step SHOWS what pass 1
// would match, so the preview must be given the same switch the run is given or
// the tab promises a replacement the run does not make (CLAUDE.md §5).
func TestPreviewAndRunAgreeOnPatternMatches(t *testing.T) {
	text := "Bad " + checksumFailedIBAN + ", good " + checksumValidIBAN + ", mail marie.duval@example.com\n"
	docs := []Document{{Name: "note.md", Format: FormatMD, Markdown: text}}
	sel := DepthSelection(PresetStandard, CountryLU)

	for _, requireChecksum := range []bool{false, true} {
		preview := PreviewPatternMatches(docs, sel, CountryLU, requireChecksum, NewEmptyAllowlist())
		previewed := map[string]bool{}
		for _, m := range preview {
			previewed[m.Category+"|"+m.Text] = true
		}

		// The run's own answer, read from the report's per-value rows: those are
		// the texts pass 1 actually replaced.
		res, err := Run(context.Background(), PipelineInput{
			Documents:       docs,
			Categories:      sel,
			RequireChecksum: requireChecksum,
			Allowlist:       NewEmptyAllowlist(),
		})
		if err != nil {
			t.Fatalf("requireChecksum=%v: Run returned an unexpected error: %v", requireChecksum, err)
		}
		replaced := map[string]bool{}
		for _, row := range res.Report.Values {
			replaced[row.Category+"|"+row.Original] = true
		}

		for key := range previewed {
			if !replaced[key] {
				t.Errorf("requireChecksum=%v: the preview promised %q and the run did not make it", requireChecksum, key)
			}
		}
		for key := range replaced {
			if !previewed[key] {
				t.Errorf("requireChecksum=%v: the run replaced %q and the preview did not show it", requireChecksum, key)
			}
		}
	}
}

// TestValidateStaysMandatory is the boundary CLAUDE.md §5 draws: a checksum that
// IS the recognizer has to veto, whatever the switch says, because stripping the
// Luhn check off the credit-card rule turns every 16-digit run into a card.
//
// The switch governs only the CORROBORATING checks, so with it OFF a bare digit
// run that fails Luhn must still not be a credit card.
func TestValidateStaysMandatory(t *testing.T) {
	// Shape-valid for the credit-card rule (16 digits, grouped) and Luhn-invalid.
	const notACard = "4111 1111 1111 1112"
	spans := DetectPIISelected(notACard, DepthSelection(PresetStandard, CountryLU), CountryLU)
	for _, s := range spans {
		if s.Category == CatCreditCard {
			t.Fatalf("a digit run failing Luhn is not a credit card: got span %+v", s)
		}
	}
	// And the switch cannot bring it back either way round.
	for _, requireChecksum := range []bool{false, true} {
		got := runChecksum(t, notACard+"\n", nil, requireChecksum)
		if !strings.Contains(got, notACard) {
			t.Errorf("requireChecksum=%v: want the non-card digits left alone, got:\n%s", requireChecksum, got)
		}
	}
}

// TestRejectFailedChecksums is the filter itself, at the level it is written:
// the one score pass 1's corroborating checks mint is dropped and nothing that
// merely scores low is touched with it.
func TestRejectFailedChecksums(t *testing.T) {
	spans := []Span{
		{Category: CatIBAN, Original: checksumValidIBAN, Confidence: ConfidenceDeterministic},
		{Category: CatIBAN, Original: checksumFailedIBAN, Confidence: ConfidenceChecksumFailed},
		{Category: "person_names", Original: "Marie", Confidence: ConfidenceManualDefault},
		{Category: "person_names", Original: "Anouk", Confidence: ConfidenceLLMDefault},
		// A producer that states nothing is trusted, not filtered away.
		{Category: CatPhone, Original: "+352 621 000 000"},
	}
	input := make([]Span, len(spans))
	copy(input, spans)
	got := RejectFailedChecksums(input)
	if len(got) != len(spans)-1 {
		t.Fatalf("kept %d spans, want %d: exactly the checksum-failed one goes (%+v)",
			len(got), len(spans)-1, got)
	}
	for _, s := range got {
		if s.Original == checksumFailedIBAN {
			t.Errorf("the checksum-failed IBAN survived: %+v", s)
		}
	}
}

// engine/framework_agreement_test.go — the fixture pair as a regression suite.
//
// backend/testdata/framework_agreement.docx is an eight-page consultancy
// framework agreement between two Luxembourg parties;
// framework_agreement_anon.docx is the SAME document as a human anonymiser
// produced it. The pair is the only fixture in the repository that exercises a
// real document's run fragmentation, its defined-term vocabulary and its
// signature blocks together, and every assertion here depends on one of those.
//
// The ground truth is DATA (framework_agreement_expected.json), never a golden
// markdown blob. A blob fails on every unrelated converter improvement and
// teaches the next session to regenerate it without reading it; the table fails
// only when a value stops being found, or starts being found under the wrong
// category, or the review list floods again.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// expectedOriginal is one string the reference replaced.
type expectedOriginal struct {
	Text        string `json:"text"`
	Occurrences int    `json:"occurrences"`
}

// expectedPlaceholder is one placeholder the reference minted, and everything
// that became it.
type expectedPlaceholder struct {
	Placeholder string             `json:"placeholder"`
	Category    string             `json:"category"`
	Occurrences int                `json:"occurrences"`
	Originals   []expectedOriginal `json:"originals"`
	// Reachable is HOW this build can find it without help: "pattern" (pass 1),
	// "smart" (a discovery method suggests it) or "manual" (only a declaration or
	// local LLM discovery). It is what keeps the assertions honest about which of them are
	// claims about recall and which are claims about the pipeline.
	Reachable string `json:"reachable"`
}

type expectedGroundTruth struct {
	Placeholders []expectedPlaceholder `json:"placeholders"`
}

func loadGroundTruth(t *testing.T) expectedGroundTruth {
	t.Helper()
	raw, err := os.ReadFile("../testdata/framework_agreement_expected.json")
	if err != nil {
		t.Fatalf("could not read the ground truth: %v", err)
	}
	var out expectedGroundTruth
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("the ground truth is not valid JSON: %v", err)
	}
	if len(out.Placeholders) == 0 {
		t.Fatal("the ground truth lists no placeholders, so this suite would pass by " +
			"asserting nothing")
	}
	return out
}

// loadFixtureMarkdown converts one of the two .docx files to the working form.
func loadFixtureMarkdown(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("../testdata/" + name)
	if err != nil {
		t.Fatalf("could not read %s: %v", name, err)
	}
	docs, err := LoadAll(name, raw)
	if err != nil {
		t.Fatalf("could not convert %s: %v", name, err)
	}
	var b strings.Builder
	for _, d := range docs {
		b.WriteString(d.Markdown)
	}
	return b.String()
}

// fixtureAllowlist is the allowlist the ground truth is measured under: the
// shipped defaults with the country seed for this document's own jurisdiction
// removed.
//
// The seed itself is a reasonable default and stays. In a two-party contract
// between two entities of one country the jurisdiction is part of the identity,
// which is why removing it is the user's first act on this document, and why the
// blocking conflict that refuses the run states the one-gesture fix that clears
// it (conflicts.go ConflictResolution).
func fixtureAllowlist() *Allowlist {
	allow := NewAllowlist()
	allow.Remove("Luxembourg")
	return allow
}

// TestFrameworkAgreementPatternsFindEveryPatternReachableValue: every value the
// ground truth marks "pattern" is found by pass 1, under the category the
// reference filed it under.
//
// This is the whole of §1's pass-1 half held in one place: the checksum-invalid
// IBAN, the two variable-length Luxembourg phone numbers, the three schemeless
// websites, the cued BIC, the two postal codes, the two address lines and both
// spellings of the date.
func TestFrameworkAgreementPatternsFindEveryPatternReachableValue(t *testing.T) {
	src := loadFixtureMarkdown(t, "framework_agreement.docx")
	spans := ResolveOverlaps(DetectPIISelected(src, PresetSelection(LevelAdvanced), CountryLU))

	found := map[string]string{} // original -> category
	for _, s := range spans {
		found[s.Original] = s.Category
	}

	for _, want := range loadGroundTruth(t).Placeholders {
		if want.Reachable != "pattern" {
			continue
		}
		for _, orig := range want.Originals {
			got, ok := found[orig.Text]
			if !ok {
				t.Errorf("pass 1 did not find %q (%s), which the reference replaced with [%s]",
					orig.Text, want.Category, want.Placeholder)
				continue
			}
			if got != want.Category {
				t.Errorf("pass 1 filed %q under %q; the reference filed it under %q, and a "+
					"mislabelled span makes the mapping CSV describe a value the document "+
					"does not contain", orig.Text, got, want.Category)
			}
		}
	}

	// The mapping must not claim a payment card. The IBAN's 16-digit interior
	// passes Luhn, and reporting it as a card asserts the document held one that
	// never existed while the IBAN's country code survives in clear text.
	for _, s := range spans {
		if s.Category == CatCreditCard {
			t.Errorf("a credit card was reported (%q); this document contains none", s.Original)
		}
	}
}

// TestFrameworkAgreementRecall is the criterion that matters most to a user:
// both parties of the contract are SUGGESTED offline, with no local
// AI and nothing typed by hand.
//
// Neither was suggested before the legal-form comma rule. "Contoso, Societe
// Francaise de Transport S.A." is the standard continental legal-name form and
// the dominant one in French and Luxembourg drafting; a comma always terminated
// a run, so what survived was either the legal form with no name in front of it
// or nothing at all.
func TestFrameworkAgreementRecall(t *testing.T) {
	for _, want := range []string{"Contoso", "Northstar"} {
		if !containsSuggestion(fixtureSuggestions(t), CatEntityNames, want) {
			t.Errorf("offline discovery does not suggest %q, so the user has to know to type "+
				"one of the document's own two parties by hand", want)
		}
	}
}

// TestFrameworkAgreementPrecision pins the review list with NUMBERS, so a later
// change that floods it again fails the build.
//
// The list was fifty-three rows carrying four true positives, which is eight per
// cent signal: a review list nobody can get through is worse than one that
// misses a value the user can still type in. Two structural classes accounted
// for most of the noise, and both are now suppressed: the terms the document
// DEFINES about itself, and ALL-CAPS heading text carrying a function word.
func TestFrameworkAgreementPrecision(t *testing.T) {
	const maxRows = 25
	const minTruePositives = 6

	folded := fixtureSuggestions(t)
	if len(folded) > maxRows {
		var names []string
		for _, s := range folded {
			names = append(names, s.MainText)
		}
		t.Errorf("the folded review list is %d rows, at most %d are acceptable:\n  %s",
			len(folded), maxRows, strings.Join(names, "\n  "))
	}

	// A true positive is a row the reference actually replaced, matched on the
	// row's own main text or any of its folded spellings.
	truth := map[string]bool{}
	for _, p := range loadGroundTruth(t).Placeholders {
		for _, o := range p.Originals {
			truth[strings.ToLower(o.Text)] = true
		}
	}
	var hits []string
	for _, s := range folded {
		forms := append([]string{s.MainText}, s.Spellings...)
		for _, f := range forms {
			if truth[strings.ToLower(f)] {
				hits = append(hits, s.MainText)
				break
			}
		}
	}
	if len(hits) < minTruePositives {
		t.Errorf("the review list carries %d true positives, at least %d are required: %v",
			len(hits), minTruePositives, hits)
	}
	t.Logf("review list: %d rows, %d true positives (%d%% precision): %v",
		len(folded), len(hits), 100*len(hits)/max(1, len(folded)), hits)
}

// fixtureSuggestions is what a user actually reviews for this document with
// The offline routes alone: heuristic discovery plus signal-based discovery, merged
// and folded, under the shipped tuning.
func fixtureSuggestions(t *testing.T) []Suggestion {
	t.Helper()
	raw, err := os.ReadFile("../testdata/framework_agreement.docx")
	if err != nil {
		t.Fatalf("could not read the fixture: %v", err)
	}
	docs, err := LoadAll("framework_agreement.docx", raw)
	if err != nil {
		t.Fatalf("could not convert the fixture: %v", err)
	}
	var text strings.Builder
	for _, d := range docs {
		text.WriteString(d.Markdown)
	}

	allow := fixtureAllowlist()
	// The suppressor is part of the offline answer, not a separate step: a
	// defined term is the document's own statement that a phrase is its own
	// vocabulary.
	ApplyDefinedTerms(allow, DiscoverDefinedTerms("framework_agreement.docx", text.String()))

	heuristic, err := HeuristicDiscoverContext(context.Background(), text.String(), allow,
		DefaultHeuristicDiscoveryOptions(), CountryLU)
	if err != nil {
		t.Fatalf("heuristic discovery: %v", err)
	}
	signals := DiscoverFromSignals(SignalDiscoveryInput{
		Documents: docs, Allow: allow, Country: CountryLU,
	})
	return FoldValueFamilies(MergeSuggestions(heuristic, signals), allow)
}

// containsSuggestion reports whether the list carries a row whose main text or
// any folded spelling is `text`, under `category`.
func containsSuggestion(list []Suggestion, category, text string) bool {
	want := strings.ToLower(text)
	for _, s := range list {
		if s.Category != category {
			continue
		}
		for _, f := range append([]string{s.MainText}, s.Spellings...) {
			if strings.ToLower(f) == want {
				return true
			}
		}
	}
	return false
}

// TestFrameworkAgreementReproduction is the pipeline half, and it is the
// important one: it separates a DISCOVERY gap (the engine could not find it)
// from a PIPELINE gap (the engine could not replace it even when told).
//
// The twenty-five Values the reference implies are declared from the ground
// truth, the run is executed, and the result is compared to the reference with
// every [LABEL_N] normalised to [#]: the NUMBERS differ legitimately, because
// they depend on document order, and what has to agree is which stretches of
// text became placeholders at all.
func TestFrameworkAgreementReproduction(t *testing.T) {
	truth := loadGroundTruth(t)
	src := loadFixtureMarkdown(t, "framework_agreement.docx")
	reference := loadFixtureMarkdown(t, "framework_agreement_anon.docx")

	var values []Value
	for _, p := range truth.Placeholders {
		if !isValueCategory(p.Category) || len(p.Originals) == 0 {
			continue // a pattern category is pass 1's job, not a declaration
		}
		v := Value{Category: p.Category, MainText: p.Originals[0].Text}
		for _, extra := range p.Originals[1:] {
			v.Spellings = append(v.Spellings, extra.Text)
		}
		// A family with two spellings is CURATED: the pair is what the reference
		// replaced, and an automatic derivation could add a third form the
		// reference never touched.
		if len(v.Spellings) > 0 {
			v.SpellingPolicy = SpellingPolicyCurated
		}
		values = append(values, v)
	}

	res, err := Run(context.Background(), PipelineInput{
		Documents: []Document{{
			Name: "framework_agreement.docx", Format: FormatDOCX, Markdown: src,
		}},
		Values:    values,
		Level:     LevelAdvanced,
		Country:   CountryLU,
		Allowlist: fixtureAllowlist(),
		Registry:  NewRegistry(),
	})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if len(res.Validation.Blocking) > 0 {
		t.Fatalf("the run was refused: %+v", res.Validation.Blocking)
	}
	if len(res.Documents) != 1 {
		t.Fatalf("expected one result document, got %d", len(res.Documents))
	}

	got := normalisePlaceholders(res.Documents[0].Anonymised)
	want := normalisePlaceholders(reference)
	diff := differingLines(want, got)
	for _, line := range diff {
		t.Logf("hunk: %s", line)
	}

	// Each remaining hunk is matched against the reason it exists, rather than
	// only counted. A budget alone lets a NEW defect hide inside it the moment an
	// accounted-for one is fixed, which is the failure mode a bare number has.
	unexplained := 0
	for _, hunk := range diff {
		if accountedHunk(hunk) == "" {
			unexplained++
			t.Errorf("an unaccounted difference from the reference:\n%s", hunk)
			continue
		}
		t.Logf("accounted for: %s", accountedHunk(hunk))
	}
	if unexplained == 0 && len(diff) > len(accountedHunks) {
		t.Errorf("%d hunks matched %d reasons, so one reason is covering more than the "+
			"difference it describes", len(diff), len(accountedHunks))
	}
}

// accountedHunks are the differences the reference legitimately carries, each
// with the reason it is not a defect. They are recorded so a later session does
// not "fix" the code towards an artefact of the human process.
//
// Two are deliberate non-fixes (docs/change-11 §1.10) and two are hand-edits the
// application should NOT reproduce (§3), which is also why they are written down
// in the ground truth's own outOfScope list.
var accountedHunks = map[string]string{
	// The stray space survives INSIDE the replaced line, so the marker is the
	// placeholder followed by it: "[#] ," where the reference reads "[#],".
	"[#] ,": "the reference tidies a stray space before a comma; tidying the " +
		"user's punctuation is not an anonymiser's job and would make the export differ " +
		"from the original in a way the user did not ask for",
	"Law of 23 July 2016": "a public statute date, which identifies nobody. The engine " +
		"replaces it at the advanced level and the reference keeps it. Left to the user " +
		"rather than fixed with a rule: a date-with-legal-cue suppressor would get the " +
		"engagement date wrong in some other document, and date is advanced-only and " +
		"per-category switchable",
	"Council of 27 April 2016": "the GDPR's own date, for the same reason as the statute " +
		"date above",
	"(Partner)": "the reference drops one role title while keeping (CEO) and (CIO) " +
		"elsewhere. A job title is either identifying or it is not; it cannot be both in " +
		"one document, so this is a hand-edit and the application keeps role titles " +
		"consistently",
}

// accountedHunk returns the reason a hunk is expected, or "" when it is not.
func accountedHunk(hunk string) string {
	for marker, reason := range accountedHunks {
		if strings.Contains(hunk, marker) {
			return reason
		}
	}
	return ""
}

// normalisePlaceholders replaces every [LABEL_N] with [#], so the comparison is
// about WHICH stretches became placeholders rather than about the numbers, which
// depend on document order and differ legitimately.
func normalisePlaceholders(text string) string {
	return anyPlaceholderRe.ReplaceAllString(text, "[#]")
}

// anyPlaceholderRe matches a placeholder ANYWHERE in a line. registry.go's
// placeholderShapeRe is anchored, because it validates one renamed placeholder;
// this one scans running text.
var anyPlaceholderRe = regexp.MustCompile(`\[[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)*_[1-9][0-9]*\]`)

// differingLines returns a readable hunk per line the two texts disagree on.
func differingLines(want, got string) []string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	var out []string
	n := len(wl)
	if len(gl) > n {
		n = len(gl)
	}
	at := func(lines []string, i int) string {
		if i < len(lines) {
			return strings.TrimSpace(lines[i])
		}
		return ""
	}
	for i := 0; i < n; i++ {
		w, g := at(wl, i), at(gl, i)
		if w == g {
			continue
		}
		out = append(out, fmt.Sprintf("line %d\n    reference: %s\n    produced:  %s", i+1, w, g))
	}
	return out
}

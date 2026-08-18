// engine/pipeline_test.go — tests two-document consistency via
// the post-pass, the level matrix goldens, simple-replace ordering, grid
// consistency, and the 50-document performance budget.
//
// Budget measurement: deterministic pipeline
// (passes 1+2+4) over 50 documents × 50 KB, budget ≤ 5 s. Measured
// 2026-07-23 on the CI-class Linux container in TestPipelineBudget:
// ~1.9 s on an extremely PII-dense synthetic corpus (105 300 replacements —
// far denser than real documents), comfortably inside the budget.
package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// runPipeline is a small helper with sensible defaults for tests.
func runPipeline(t *testing.T, in PipelineInput) *Results {
	t.Helper()
	res, err := Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

// TestSuppressRegexPIISkipsPassOne: the "Native detection" master switch, off,
// must stop the deterministic regex PII pass entirely. Even at LevelAdvanced
// (which selects every category) an email and a VAT number survive when
// SuppressRegexPII is true, and are replaced when it is false. The value pass
// is unaffected, so a declared value is still replaced in both cases.
func TestSuppressRegexPIISkipsPassOne(t *testing.T) {
	const text = "Alpine Trust wrote to marie.duval@example.com, VAT LU12345678."
	doc := Document{Name: "a.txt", Format: FormatTXT, Markdown: text}
	value := Value{Category: CatEntityNames, MainText: "Alpine Trust"}

	// Suppressed: the regex signals stay put, the value still goes.
	suppressed := runPipeline(t, PipelineInput{
		Documents:        []Document{doc},
		Values:           []Value{value},
		Level:            LevelAdvanced,
		Country:          CountryLU,
		Allowlist:        NewEmptyAllowlist(),
		SuppressRegexPII: true,
	})
	out := suppressed.Documents[0].Anonymised
	if !strings.Contains(out, "marie.duval@example.com") {
		t.Errorf("with Native detection off the email must survive, got %q", out)
	}
	if !strings.Contains(out, "LU12345678") {
		t.Errorf("with Native detection off the VAT number must survive, got %q", out)
	}
	if strings.Contains(out, "Alpine Trust") {
		t.Errorf("the Value pass is unaffected, so the value must still be replaced, got %q", out)
	}

	// Not suppressed: the same email is replaced (proving the fixture is
	// otherwise detectable), so the flag is the only thing that changed.
	on := runPipeline(t, PipelineInput{
		Documents:        []Document{doc},
		Values:           []Value{value},
		Level:            LevelAdvanced,
		Country:          CountryLU,
		Allowlist:        NewEmptyAllowlist(),
		SuppressRegexPII: false,
	})
	out2 := on.Documents[0].Anonymised
	if strings.Contains(out2, "marie.duval@example.com") {
		t.Errorf("with Native detection on the email must be replaced, got %q", out2)
	}
}

// TestTwoDocumentConsistency: a Value declared for doc A must also be
// replaced in doc B — and a PII value first seen in doc B must be
// retro-replaced in doc A by the post-pass, with the SAME placeholder.
func TestTwoDocumentConsistency(t *testing.T) {
	docA := Document{Name: "a.txt", Format: FormatTXT, Markdown: "Alpine Trust signed. Contact marie.duval@example.com."}
	docB := Document{Name: "b.txt", Format: FormatTXT, Markdown: "Notes about Alpine Trust and marie.duval@example.com follow."}

	res := runPipeline(t, PipelineInput{
		Documents: []Document{docA, docB},
		Values:    []Value{{Category: CatEntityNames, MainText: "Alpine Trust"}},
		Level:     LevelMedium,
		Allowlist: NewEmptyAllowlist(),
	})

	a, b := res.Documents[0].Anonymised, res.Documents[1].Anonymised
	for _, out := range []string{a, b} {
		if strings.Contains(out, "Alpine Trust") || strings.Contains(out, "marie.duval") {
			t.Errorf("real value survived anonymisation: %q", out)
		}
	}
	// Same placeholder everywhere (the registry guarantee).
	if !strings.Contains(a, "[ENTITY_1]") || !strings.Contains(b, "[ENTITY_1]") {
		t.Errorf("client placeholder differs across documents:\nA: %s\nB: %s", a, b)
	}
	if !strings.Contains(a, "[EMAIL_1]") || !strings.Contains(b, "[EMAIL_1]") {
		t.Errorf("email placeholder differs across documents:\nA: %s\nB: %s", a, b)
	}
}

// TestPostPassSpreadsRegistryEntries: a mapping the registry already holds is
// re-applied to EVERY document, including one this run's configuration would
// not detect on its own. That is what keeps a value the session assigned
// earlier from reappearing in clear text in a document imported later.
func TestPostPassSpreadsRegistryEntries(t *testing.T) {
	reg := NewRegistry()
	reg.Assign(CatProjectNames, "Project Borealis") // an earlier run in this session

	res := runPipeline(t, PipelineInput{
		Documents: []Document{{Name: "a.txt", Format: FormatTXT, Markdown: "Early notes on Project Borealis here."}},
		Level:     LevelMedium,
		Allowlist: NewEmptyAllowlist(),
		Registry:  reg,
	})

	a := res.Documents[0].Anonymised
	if strings.Contains(a, "Project Borealis") {
		t.Errorf("post-pass did not re-apply the known mapping: %q", a)
	}
	if !strings.Contains(a, "[PROJECT_1]") {
		t.Errorf("doc A should carry the registry's placeholder after the post-pass: %q", a)
	}
}

// TestLevelMatrix: the same fixture at soft/medium/advanced produces the
// expected differing outputs.
func TestLevelMatrix(t *testing.T) {
	text := "Marie Duval (marie.duval@example.com) met Alpine Trust about Helios on 2026-07-23 for €5,000."
	values := []Value{
		{Category: CatEntityNames, MainText: "Alpine Trust"},
		{Category: CatPersonNames, MainText: "Marie Duval"},
		{Category: CatOtherNames, MainText: "Helios"},
	}
	run := func(level Level) string {
		res := runPipeline(t, PipelineInput{
			Documents: []Document{{Name: "m.txt", Format: FormatTXT, Markdown: text}},
			Values:    values,
			Level:     level,
			Allowlist: NewEmptyAllowlist(),
			Registry:  NewRegistry(), // fresh numbering per level for exact goldens
		})
		return res.Documents[0].Anonymised
	}

	soft := run(LevelSoft)
	// Soft: hard PII + engagement values; person names, other names, dates
	// and amounts stay.
	if soft != "Marie Duval ([EMAIL_1]) met [ENTITY_1] about Helios on 2026-07-23 for €5,000." {
		t.Errorf("soft output unexpected: %q", soft)
	}

	medium := run(LevelMedium)
	// Medium: + person names. Other names, dates and amounts kept.
	if medium != "[PERSON_1] ([EMAIL_1]) met [ENTITY_1] about Helios on 2026-07-23 for €5,000." {
		t.Errorf("medium output unexpected: %q", medium)
	}

	advanced := run(LevelAdvanced)
	// Advanced: + other names, dates, amounts.
	if advanced != "[PERSON_1] ([EMAIL_1]) met [ENTITY_1] about [OTHER_1] on [DATE_1] for [AMOUNT_1]." {
		t.Errorf("advanced output unexpected: %q", advanced)
	}
}

// TestOccurrenceVariantsRecordVariantSpelling: a placeholder that replaced a
// variant spelling records that spelling positionally, so the results view can
// show "Borch (Johannes Borch)" on the mark it actually replaced. A mainText
// match records "" in the same slot list, keeping occurrence i aligned with
// the i-th placeholder in the anonymised text.
func TestOccurrenceVariantsRecordVariantSpelling(t *testing.T) {
	res := runPipeline(t, PipelineInput{
		Documents: []Document{{
			Name:     "n.txt",
			Format:   FormatTXT,
			Markdown: "Johannes Borch met Borch",
		}},
		Values:    []Value{{Category: CatPersonNames, MainText: "Johannes Borch"}},
		Level:     LevelMedium,
		Allowlist: NewEmptyAllowlist(),
	})
	rd := res.Documents[0]
	got := rd.OccurrenceSpellings["[PERSON_1]"]
	want := []string{"", "Borch"}
	if len(got) != len(want) {
		t.Fatalf("occurrence variants = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("occurrence variants = %v, want %v", got, want)
		}
	}
}

// TestOccurrenceSpellingsPrunedWhenAllMainText: a document whose every match
// was the mainText value carries no variant map, so the payload stays lean.
func TestOccurrenceSpellingsPrunedWhenAllMainText(t *testing.T) {
	res := runPipeline(t, PipelineInput{
		Documents: []Document{{
			Name:     "n.txt",
			Format:   FormatTXT,
			Markdown: "Contact marie.duval@example.com now",
		}},
		Level:     LevelMedium,
		Allowlist: NewEmptyAllowlist(),
	})
	if res.Documents[0].OccurrenceSpellings != nil {
		t.Errorf("mainText-only document should carry no variant map, got %v",
			res.Documents[0].OccurrenceSpellings)
	}
}

// TestGridDocumentConsistency: a CSV document's anonymised markdown is
// re-rendered from the anonymised grid, so both always agree, and the
// grid round-trips to CSV with placeholders in place.
func TestGridDocumentConsistency(t *testing.T) {
	doc, err := Load("c.csv", []byte("name,email\nMarie Duval,marie.duval@example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	res := runPipeline(t, PipelineInput{
		Documents: []Document{doc},
		Values:    []Value{{Category: CatPersonNames, MainText: "Marie Duval"}},
		Level:     LevelMedium,
		Allowlist: NewEmptyAllowlist(),
	})
	rd := res.Documents[0]
	if rd.Grid[1][0] != "[PERSON_1]" || rd.Grid[1][1] != "[EMAIL_1]" {
		t.Errorf("grid cells not anonymised: %v", rd.Grid)
	}
	if rd.Anonymised != GridToMarkdownTable(rd.Grid) {
		t.Error("markdown preview and grid disagree")
	}
	csvOut, err := GridToCSV(rd.Grid)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(csvOut), "[PERSON_1],[EMAIL_1]") {
		t.Errorf("CSV export must carry placeholders: %s", csvOut)
	}
}

// TestComplexSheetConsistency: a complex xlsx sheet is one JSON region, and
// the JSON, the fenced markdown preview and the grid-free result must stay in
// step through the detect and apply phases. The three representations are
// assembled in different places, so a split that lost one would show as a
// preview that disagrees with the .json export.
func TestComplexSheetConsistency(t *testing.T) {
	const blob = `{"A1":"Marie Duval","B1":"marie.duval@example.com"}`
	res := runPipeline(t, PipelineInput{
		Documents: []Document{{
			Name: "workbook.xlsx#Sheet1", Format: FormatXLSXJSON, JSON: blob,
			Markdown: "```json\n" + blob + "\n```\n",
		}},
		Values:    []Value{{Category: CatPersonNames, MainText: "Marie Duval"}},
		Level:     LevelMedium,
		Allowlist: NewEmptyAllowlist(),
	})
	rd := res.Documents[0]
	if strings.Contains(rd.JSON, "Marie Duval") || strings.Contains(rd.JSON, "marie.duval@") {
		t.Errorf("the JSON region was not anonymised: %s", rd.JSON)
	}
	if rd.Anonymised != "```json\n"+rd.JSON+"\n```\n" {
		t.Errorf("the preview must be rendered from the anonymised JSON:\n%s", rd.Anonymised)
	}
	if rd.Grid != nil {
		t.Error("a complex sheet has no grid model")
	}
}

// TestReportContents sanity-checks totals and categories.
func TestReportContents(t *testing.T) {
	res := runPipeline(t, PipelineInput{
		Documents: []Document{{Name: "r.txt", Format: FormatTXT, Markdown: "mail marie.duval@example.com now", Warnings: []string{"the file is empty — nothing to anonymise"}}},
		Level:     LevelMedium,
		Allowlist: NewEmptyAllowlist(),
	})
	rep := res.Report
	if rep.TotalReplacements != 1 || rep.ByCategory[CatEmail] != 1 {
		t.Errorf("report totals wrong: %+v", rep)
	}
	if strings.Join(rep.DetectedCategories, ",") != CatEmail {
		t.Errorf("detected categories = %v, want [%s]", rep.DetectedCategories, CatEmail)
	}
	if len(rep.Documents) != 1 || rep.Documents[0].Replacements != 1 {
		t.Errorf("per-document report wrong: %+v", rep.Documents)
	}
	if strings.Join(rep.Documents[0].DetectedCategories, ",") != CatEmail {
		t.Errorf("document detected categories = %v, want [%s]", rep.Documents[0].DetectedCategories, CatEmail)
	}
	// Ingestion warnings must flow into the report.
	if len(rep.Documents[0].Warnings) != 1 {
		t.Errorf("document warnings lost: %+v", rep.Documents[0])
	}
	// Both serialisations must render.
	if _, err := rep.ToJSON(); err != nil {
		t.Errorf("ToJSON: %v", err)
	}
	if md := rep.ToMarkdown(); !strings.Contains(md, "Total replacements: 1") {
		t.Errorf("markdown report incomplete:\n%s", md)
	}
}

// TestPipelineCancellation: a cancelled context stops the run between
// documents and reports the partial progress.
func TestPipelineCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the run even starts

	docs := []Document{
		{Name: "1.txt", Format: FormatTXT, Markdown: "text one"},
		{Name: "2.txt", Format: FormatTXT, Markdown: "text two"},
	}
	res, err := Run(ctx, PipelineInput{Documents: docs, Level: LevelMedium, Allowlist: NewEmptyAllowlist()})
	if err == nil {
		t.Fatal("cancelled run must return the context error")
	}
	if len(res.Documents) != 0 {
		t.Errorf("no documents should complete after immediate cancel, got %d", len(res.Documents))
	}
	if len(res.Report.Warnings) == 0 || !strings.Contains(res.Report.Warnings[0], "cancelled") {
		t.Errorf("cancellation must be recorded in the report: %+v", res.Report.Warnings)
	}
}

// TestPipelineBudget measures the deterministic budget: passes
// 1+2+4 over 50 documents × 50 KB in ≤ 5 s.
func TestPipelineBudget(t *testing.T) {
	// ~50 KB of realistic prose per document, seeded with PII and value
	// mentions so the passes do real work.
	para := "Marie Duval of Alpine Trust (marie.duval@example.com, +352 621 000 111) " +
		"reviewed IBAN LU28 0019 4006 4475 0000 with P. Stone before the deadline. "
	var b strings.Builder
	for b.Len() < 50*1024 {
		b.WriteString(para)
	}
	text := b.String()

	docs := make([]Document, 50)
	for i := range docs {
		docs[i] = Document{Name: fmt.Sprintf("doc%02d.txt", i), Format: FormatTXT, Markdown: text}
	}
	values := []Value{
		{Category: "entity_names", MainText: "Alpine Trust"},
		{Category: "person_names", MainText: "Marie Duval"},
		{Category: "person_names", MainText: "Peter Stone"},
	}

	start := time.Now()
	res := runPipeline(t, PipelineInput{
		Documents: docs,
		Values:    values,
		Level:     LevelMedium,
		Allowlist: NewAllowlist(),
	})
	elapsed := time.Since(start)

	t.Logf("50 docs × 50 KB deterministic pipeline took %v (budget 5 s), %d replacements",
		elapsed, res.Report.TotalReplacements)
	if elapsed > 5*time.Second {
		t.Errorf("pipeline budget breached: %v > 5 s", elapsed)
	}
	if res.Report.TotalReplacements == 0 {
		t.Error("budget run replaced nothing — the measurement is meaningless")
	}
}

// TestAcceptProposalsStampsTheAIOrigin: a proposal that survives the
// hallucination filter becomes a Value carrying its route, not only its
// score. The score alone cannot serve as provenance, because it is also what
// MinConfidence filters on: raising the floor would otherwise reorder which
// route wins.
// TestOwnershipIsDecidedByRuleNotByDocumentOrder is the regression the
// three-phase run exists for.
//
// Two routes can claim the same characters, and each claims them in a
// different document. Detecting and replacing in one step meant
// Registry.Assign's byOriginal index froze whichever claim was ASSIGNED first,
// and assignment order is byte offset within DOCUMENT order. So the category
// the value ended up under, and the placeholder text the user reads and
// exports, depended on the order the files were imported in.
//
// Deciding ownership over the whole batch before any placeholder is minted is
// what makes the answer a rule instead. The table runs both document orders and
// demands the same one.
func TestOwnershipIsDecidedByRuleNotByDocumentOrder(t *testing.T) {
	const value = "Helios"
	// One string, two claims, and each document only exposes one of them: the
	// accepted brand value matches case-insensitively in both files, while the
	// user's case-sensitive pattern can only fire on doc B's lower-cased
	// spelling. The registry keys on the lower-cased string, so both claims are
	// about the SAME value. Whichever is ASSIGNED first would freeze its
	// category, so the answer has to come from the precedence rule rather than
	// from the import order.
	aText := "The " + value + " engagement closed in June.\n"
	bText := "A separate note about helios and its scope.\n"
	values := []Value{{Category: CatBrandNames, MainText: value, DiscoveryMethods: []string{MethodLocalAI}}}
	patterns := []CustomPattern{{Expr: `helios`}}
	docA := Document{Name: "a.txt", Format: FormatTXT, Markdown: aText}
	docB := Document{Name: "b.txt", Format: FormatTXT, Markdown: bText}

	owners := map[string]string{}
	for _, tc := range []struct {
		name string
		docs []Document
	}{
		{"a first", []Document{docA, docB}},
		{"b first", []Document{docB, docA}},
	} {
		reg := NewRegistry()
		res, err := Run(context.Background(), PipelineInput{
			Documents: tc.docs,
			Values:    values,
			Patterns:  patterns,
			Level:     LevelAdvanced, // both types switched on
			Allowlist: NewEmptyAllowlist(),
			Registry:  reg,
		})
		if err != nil {
			t.Fatalf("%s: Run: %v", tc.name, err)
		}

		var owning []MappingEntry
		for _, e := range reg.Entries() {
			if strings.EqualFold(e.Original, value) {
				owning = append(owning, e)
			}
		}
		if len(owning) != 1 {
			t.Fatalf("%s: one string must have exactly one placeholder, got %+v", tc.name, owning)
		}
		owners[tc.name] = owning[0].Category + " " + owning[0].Placeholder

		// Both documents must show the SAME placeholder, or the two halves of
		// one engagement stop agreeing with each other.
		for _, d := range res.Documents {
			if !strings.Contains(d.Anonymised, owning[0].Placeholder) {
				t.Errorf("%s: %s does not carry %s:\n%s",
					tc.name, d.Name, owning[0].Placeholder, d.Anonymised)
			}
		}
	}

	if owners["a first"] != owners["b first"] {
		t.Errorf("the import order decided who owns %q: %q with a first, %q with b first.\n"+
			"Ownership must be decided by the precedence rule over the whole batch, not by\n"+
			"whichever claim reached the registry first.", value, owners["a first"], owners["b first"])
	}
}

// TestUserDefinedBeatsSmartDiscovered: a custom pattern and a discovered Value
// covering the same string resolve to the pattern, because a declaration
// outranks a guess, and exactly one placeholder exists for the string.
func TestUserDefinedBeatsSmartDiscovered(t *testing.T) {
	reg := NewRegistry()
	res, err := Run(context.Background(), PipelineInput{
		Documents: []Document{{
			Name: "a.txt", Format: FormatTXT,
			Markdown: "Project PRJ-4471 is on track. PRJ-4471 again.\n",
		}},
		// The user's own regex...
		Patterns: []CustomPattern{{Expr: `PRJ-[0-9]+`}},
		// ...and the same string as something Smart detection discovered.
		Values: []Value{{
			Category: CatProjectNames, MainText: "PRJ-4471", DiscoveryMethods: []string{MethodHeuristic},
		}},
		Level:     LevelMedium,
		Allowlist: NewEmptyAllowlist(),
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var owning []MappingEntry
	for _, e := range reg.Entries() {
		if strings.EqualFold(e.Original, "PRJ-4471") {
			owning = append(owning, e)
		}
	}
	if len(owning) != 1 {
		t.Fatalf("one string, one placeholder, got %+v", owning)
	}
	if owning[0].Category != CatCustomPatterns {
		t.Errorf("a pattern the user wrote must outrank an auto-detected value, got %s",
			owning[0].Category)
	}
	if strings.Contains(res.Documents[0].Anonymised, "PRJ-4471") {
		t.Errorf("the value must be replaced everywhere:\n%s", res.Documents[0].Anonymised)
	}
}

// TestRunIsDeterministic: the same batch run twice, and once with the document
// slice reversed, yields identical mapping entries. Ownership unification is
// the reason the reversed order can be demanded to match.
func TestRunIsDeterministic(t *testing.T) {
	docs := []Document{
		{
			Name: "a.txt", Format: FormatTXT,
			Markdown: "Alpine Trust S.A. wrote to marie.duval@example.com.\n",
		},
		{
			Name: "b.txt", Format: FormatTXT,
			Markdown: "Alpine Trust and Meridian appear here, plus marie.duval@example.com.\n",
		},
	}
	input := func(order []Document, reg *Registry) PipelineInput {
		return PipelineInput{
			Documents: order,
			Values: []Value{
				{Category: CatEntityNames, MainText: "Alpine Trust S.A."},
				{Category: CatEntityNames, MainText: "Meridian", DiscoveryMethods: []string{MethodHeuristic}},
			},
			Level:     LevelMedium,
			Allowlist: NewEmptyAllowlist(),
			Registry:  reg,
		}
	}
	// A mapping fingerprint that ignores the ORDER entries come back in but not
	// which category owns what, which is the thing under test.
	fingerprint := func(reg *Registry) string {
		var rows []string
		for _, e := range reg.Entries() {
			rows = append(rows, strings.ToLower(e.Original)+"="+e.Category)
		}
		sort.Strings(rows)
		return strings.Join(rows, ",")
	}

	first := NewRegistry()
	if _, err := Run(context.Background(), input(docs, first)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	again := NewRegistry()
	if _, err := Run(context.Background(), input(docs, again)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reversed := NewRegistry()
	if _, err := Run(context.Background(),
		input([]Document{docs[1], docs[0]}, reversed)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if fingerprint(first) != fingerprint(again) {
		t.Errorf("two identical runs disagreed:\n%s\nvs\n%s", fingerprint(first), fingerprint(again))
	}
	if fingerprint(first) != fingerprint(reversed) {
		t.Errorf("reversing the document order changed who owns what:\n%s\nvs\n%s",
			fingerprint(first), fingerprint(reversed))
	}
}

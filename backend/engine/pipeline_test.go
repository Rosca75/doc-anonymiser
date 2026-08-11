// engine/pipeline_test.go — Phase 4 tests: two-document consistency via
// the post-pass, the level matrix goldens, simple-replace ordering, grid
// consistency, and the 50-document performance budget.
//
// Budget measurement (BUILD.md performance table): deterministic pipeline
// (passes 1+2+4) over 50 documents × 50 KB, budget ≤ 5 s. Measured
// 2026-07-23 on the CI-class Linux container in TestPipelineBudget:
// ~1.9 s on an extremely PII-dense synthetic corpus (105 300 replacements —
// far denser than real documents), comfortably inside the budget.
package engine

import (
	"context"
	"fmt"
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

// TestTwoDocumentConsistency: an entity declared for doc A must also be
// replaced in doc B — and a PII value first seen in doc B must be
// retro-replaced in doc A by the post-pass, with the SAME placeholder.
func TestTwoDocumentConsistency(t *testing.T) {
	docA := Document{Name: "a.txt", Format: FormatTXT, Markdown: "Alpine Trust signed. Contact marie.duval@example.com."}
	docB := Document{Name: "b.txt", Format: FormatTXT, Markdown: "Notes about Alpine Trust and marie.duval@example.com follow."}

	res := runPipeline(t, PipelineInput{
		Documents: []Document{docA, docB},
		Entities:  []Entity{{Category: CatEntityNames, Canonical: "Alpine Trust"}},
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

// TestPostPassSpreadsLateEntities: an entity discovered LATE (here: by the
// deep-scan while processing doc B) must also be replaced in doc A, which
// was processed before the entity existed — that is exactly what the pass-4
// registry re-application is for.
func TestPostPassSpreadsLateEntities(t *testing.T) {
	// Fake LLM: proposes "Project Borealis" only when scanning doc B —
	// exactly how a late discovery happens in production.
	llm := &fakeLLM{
		perText: map[string][]ProposedEntity{
			"doc B mentions Project Borealis explicitly.": {
				{Category: CatProjectNames, Text: "Project Borealis"},
			},
		},
	}
	docA := Document{Name: "a.txt", Format: FormatTXT, Markdown: "Early notes on Project Borealis here."}
	docB := Document{Name: "b.txt", Format: FormatTXT, Markdown: "doc B mentions Project Borealis explicitly."}

	res := runPipeline(t, PipelineInput{
		Documents: []Document{docA, docB},
		Level:     LevelMedium,
		Allowlist: NewEmptyAllowlist(),
		LLM:       llm,
	})

	a := res.Documents[0].Anonymised
	if strings.Contains(a, "Project Borealis") {
		t.Errorf("post-pass did not spread the late-discovered entity to doc A: %q", a)
	}
	if !strings.Contains(a, "[PROJECT_1]") {
		t.Errorf("doc A should carry [PROJECT_1] after the post-pass: %q", a)
	}
}

// fakeLLM implements the engine LLM interface for tests.
type fakeLLM struct {
	perText map[string][]ProposedEntity
	err     error
	calls   int
}

func (f *fakeLLM) DeepScan(ctx context.Context, text string, known []Entity) ([]ProposedEntity, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.perText[text], nil
}

// TestHallucinationFilterInPipeline: proposals whose exact string is NOT
// in the source text are dropped; allowlisted proposals are dropped.
func TestHallucinationFilterInPipeline(t *testing.T) {
	text := "The CSSF reviewed the Alpine engagement."
	llm := &fakeLLM{perText: map[string][]ProposedEntity{
		text: {
			{Category: CatEntityNames, Text: "Alpine"},          // real
			{Category: CatEntityNames, Text: "Zenith Holdings"}, // hallucinated
			{Category: CatEntityNames, Text: "CSSF"},            // allowlisted
		},
	}}
	res := runPipeline(t, PipelineInput{
		Documents: []Document{{Name: "x.txt", Format: FormatTXT, Markdown: text}},
		Level:     LevelMedium,
		Allowlist: NewAllowlist(), // seeds CSSF
		LLM:       llm,
	})
	out := res.Documents[0].Anonymised
	if strings.Contains(out, "Alpine") {
		t.Errorf("accepted proposal not replaced: %q", out)
	}
	if !strings.Contains(out, "CSSF") {
		t.Errorf("allowlisted CSSF must survive: %q", out)
	}
	if strings.Contains(out, "Zenith") {
		t.Errorf("hallucinated text appeared in output?! %q", out)
	}
}

// TestLevelMatrix: the same fixture at soft/medium/advanced produces the
// expected differing outputs (BUILD.md Phase 4 golden requirement).
func TestLevelMatrix(t *testing.T) {
	text := "Marie Duval (marie.duval@example.com) met Alpine Trust in Paris on 2026-07-23 for €5,000."
	entities := []Entity{
		{Category: CatEntityNames, Canonical: "Alpine Trust"},
		{Category: CatPersonNames, Canonical: "Marie Duval"},
		{Category: CatLocationNames, Canonical: "Paris"},
	}
	run := func(level Level) string {
		res := runPipeline(t, PipelineInput{
			Documents: []Document{{Name: "m.txt", Format: FormatTXT, Markdown: text}},
			Entities:  entities,
			Level:     level,
			Allowlist: NewEmptyAllowlist(),
			Registry:  NewRegistry(), // fresh numbering per level for exact goldens
		})
		return res.Documents[0].Anonymised
	}

	soft := run(LevelSoft)
	// Soft: hard PII + engagement entities; person names, dates,
	// locations and amounts stay.
	if soft != "Marie Duval ([EMAIL_1]) met [ENTITY_1] in Paris on 2026-07-23 for €5,000." {
		t.Errorf("soft output unexpected: %q", soft)
	}

	medium := run(LevelMedium)
	// Medium: + person names. Dates and locations kept.
	if medium != "[PERSON_1] ([EMAIL_1]) met [ENTITY_1] in Paris on 2026-07-23 for €5,000." {
		t.Errorf("medium output unexpected: %q", medium)
	}

	advanced := run(LevelAdvanced)
	// Advanced: + dates, locations, amounts.
	if advanced != "[PERSON_1] ([EMAIL_1]) met [ENTITY_1] in [LOCATION_1] on [DATE_1] for [AMOUNT_1]." {
		t.Errorf("advanced output unexpected: %q", advanced)
	}
}

// TestSimpleReplaceOrderingAndCase pins rule ordering (later rules see
// earlier output) and the case-sensitivity toggle.
func TestSimpleReplaceOrderingAndCase(t *testing.T) {
	out, counts := ApplySimpleRules("alpha Beta beta", []SimpleRule{
		{Find: "alpha", Replace: "Beta"},                       // 1 hit
		{Find: "beta", Replace: "gamma", CaseSensitive: false}, // sees 3 betas now
	})
	if out != "gamma gamma gamma" {
		t.Errorf("ordered case-insensitive result = %q, want %q", out, "gamma gamma gamma")
	}
	if counts[0] != 1 || counts[1] != 3 {
		t.Errorf("counts = %v, want [1 3]", counts)
	}

	out2, counts2 := ApplySimpleRules("Beta beta", []SimpleRule{
		{Find: "beta", Replace: "x", CaseSensitive: true},
	})
	if out2 != "Beta x" || counts2[0] != 1 {
		t.Errorf("case-sensitive result = %q %v, want 'Beta x' [1]", out2, counts2)
	}

	// In the pipeline, simple-replace runs last and is reported.
	res := runPipeline(t, PipelineInput{
		Documents:   []Document{{Name: "s.txt", Format: FormatTXT, Markdown: "internal codename NIGHTJAR"}},
		Level:       LevelMedium,
		Allowlist:   NewEmptyAllowlist(),
		SimpleRules: []SimpleRule{{Find: "NIGHTJAR", Replace: "[CODENAME]", CaseSensitive: true}},
	})
	if !strings.Contains(res.Documents[0].Anonymised, "[CODENAME]") {
		t.Errorf("simple rule not applied: %q", res.Documents[0].Anonymised)
	}
	if res.Report.ByCategory["simple_replace"] != 1 {
		t.Errorf("simple_replace not reported: %+v", res.Report.ByCategory)
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
		Entities:  []Entity{{Category: CatPersonNames, Canonical: "Marie Duval"}},
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

// TestReportContents sanity-checks totals, categories and the LLM note.
func TestReportContents(t *testing.T) {
	res := runPipeline(t, PipelineInput{
		Documents: []Document{{Name: "r.txt", Format: FormatTXT, Markdown: "mail marie.duval@example.com now", Warnings: []string{"the file is empty — nothing to anonymise"}}},
		Level:     LevelMedium,
		Allowlist: NewEmptyAllowlist(),
	})
	rep := res.Report
	if rep.LLMPass != "skipped (Ollama not available)" {
		t.Errorf("LLM note = %q", rep.LLMPass)
	}
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
// documents and reports the partial progress (Phase 8 relies on this).
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

// TestPipelineBudget measures the BUILD.md deterministic budget: passes
// 1+2+4 over 50 documents × 50 KB in ≤ 5 s.
func TestPipelineBudget(t *testing.T) {
	// ~50 KB of realistic prose per document, seeded with PII and entity
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
	entities := []Entity{
		{Category: "entity_names", Canonical: "Alpine Trust"},
		{Category: "person_names", Canonical: "Marie Duval"},
		{Category: "person_names", Canonical: "Peter Stone"},
	}

	start := time.Now()
	res := runPipeline(t, PipelineInput{
		Documents: docs,
		Entities:  entities,
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

// engine/pipeline.go — pass orchestration per anonymisation level
// (CLAUDE.md §3/§5). This is the app's core: everything else feeds it or
// renders its results.
//
// Fixed pass order (CLAUDE.md §5):
//  1. Deterministic PII regex pass (pii.go).
//  2. Known-entity pass: entities expanded into variants, plus user
//     custom patterns, longest-match-first (entities.go).
//  3. Optional LLM deep-scan (behind the LLM interface; nil = skipped).
//     Every proposal passes the hallucination filter (exact string must
//     occur in the source text) and the allowlist.
//  4. Post-pass: the FULL registry is re-applied to every document, so an
//     entity discovered late (e.g. in doc 40) is also replaced in docs
//     processed earlier — same real-world entity, same placeholder,
//     everywhere.
//     Finally the ordered simple-replace rules run (simplereplace.go).
//
// Grid documents (CSV and flat xlsx sheets) are anonymised CELL BY CELL and
// their markdown preview is re-rendered from the anonymised grid — that
// guarantees the preview and the CSV round-trip export can never disagree.
package engine

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ProposedEntity is what the LLM deep-scan returns: a candidate entity the
// deterministic passes missed. It still has to survive the hallucination
// filter and the allowlist before it is used.
type ProposedEntity struct {
	Category string `json:"category"`
	Text     string `json:"text"`
}

// LLM is the interface the engine consumes for the deep-scan slot
// (CLAUDE.md §4: engine/* receives an interface, never the concrete Ollama
// client — the P4 ONNX fallback would implement the same interface).
type LLM interface {
	// DeepScan proposes residual entities in text, given what is already
	// known. Implementations must honour ctx cancellation (the UI cancel
	// button interrupts mid-call).
	DeepScan(ctx context.Context, text string, known []Entity) ([]ProposedEntity, error)
}

// ProgressEvent is emitted before each per-document stage so the UI can
// render live progress (Phase 8). Stage is one of "deterministic",
// "deep-scan", "post-pass".
type ProgressEvent struct {
	Stage    string `json:"stage"`
	DocIndex int    `json:"docIndex"` // 0-based
	DocCount int    `json:"docCount"`
	DocName  string `json:"docName"`
}

// PipelineInput bundles everything Run needs.
type PipelineInput struct {
	Documents []Document
	Entities  []Entity
	Patterns  []CustomPattern
	// Level is the preset shorthand; kept for reports and as the fallback
	// when Categories is nil.
	Level Level
	// Categories is the granular switch set the pipeline obeys
	// (BUILD-02 Phase 3). nil means "use PresetSelection(Level)", which
	// reproduces the v1 behaviour byte for byte.
	Categories CategorySelection
	// MinConfidence drops every detected span scoring below it, on the
	// BUILD-03 Phase C scale (BUILD-04 CR9). 0 (the default) keeps
	// everything, which reproduces the pre-BUILD-04 behaviour exactly.
	// See FilterByMinConfidence for what each level currently excludes.
	MinConfidence float32
	Allowlist     *Allowlist
	// Registry is the session registry. Pass the same instance across
	// runs to keep placeholders stable for the whole session; nil creates
	// a fresh one (fresh numbering).
	Registry *Registry
	// LLM is the deep-scan slot; nil skips pass 3 (the report notes it).
	LLM LLM
	// SimpleRules run last, in order (simplereplace.go).
	SimpleRules []SimpleRule
	// Progress, when set, receives per-document stage events.
	Progress func(ProgressEvent)
	// OnTrace, when set, receives the final resolved spans (post-filter,
	// post-overlap resolution) that WERE about to be replaced in each
	// document. One SpanTrace per anonymisation call — for grid or JSON
	// documents that means one trace per cell / json region, tagged via
	// SpanTrace.Region. nil disables tracing entirely (zero cost: no
	// collection, no callback dispatch). BUILD-03 Phase E.
	OnTrace func(docName string, traces []SpanTrace)
}

// SpanTrace is one snapshot of the spans a single anonymiseText call was
// about to replace, keyed by the region of the document they came from
// (empty for the document body, "row R col C" for grid cells, "json" for
// complex xlsx sheets). BUILD-03 Phase E.
type SpanTrace struct {
	Region string `json:"region,omitempty"`
	Spans  []Span `json:"spans"`
}

// ResultDocument is one anonymised document plus its statistics.
//
// It deliberately carries NO copy of the source text. There is exactly one
// producer of original text in this application, the imported Document held
// by the App, and the UI reads the ORIGINAL pane from that same producer
// (App.GetDocumentSource). A second copy travelling with the result is what
// would let a preview drift from the file the user imported, and it doubled
// the size of every "pipeline:done" payload for text nobody edited.
type ResultDocument struct {
	Name   string `json:"name"`
	Format Format `json:"format"`
	// Anonymised is the rewritten markdown working form.
	Anonymised string `json:"anonymised"`
	// Grid is the anonymised cell grid for CSV-origin documents (nil
	// otherwise) — the source for CSV round-trip export.
	Grid [][]string `json:"grid,omitempty"`
	// JSON is the anonymised structured JSON for complex xlsx sheets.
	JSON string `json:"json,omitempty"`
	// ByCategory counts replacements per category in this document.
	ByCategory map[string]int `json:"byCategory"`
	// Warnings carries the document's ingestion warnings through to the
	// results screen and report.
	Warnings []string `json:"warnings,omitempty"`
}

// Results is the full outcome of one pipeline run.
type Results struct {
	Documents []ResultDocument `json:"documents"`
	Report    Report           `json:"report"`
}

// CategorySelection is the granular per-category switch set the pipeline
// obeys (BUILD-02 Phase 3): every PII category (email, url, iban, vat,
// matricule, phone, amount, date) and every entity category (entity_names,
// project_names, person_names, custom_patterns,
// organisation_names, location_names) maps to on/off. Levels are PRESETS
// that fill this map (PresetSelection); the UI may then flip individual
// switches ("custom" mode).
type CategorySelection map[string]bool

// AllPIICategories lists the pass-1 categories in a stable order (used by
// presets, tests and the configure UI documentation). BUILD-03 Phase B
// added the extended recognizers after the v1 group so v1 UI ordering is
// preserved and any UI that iterates this list sees new categories at the
// tail.
var AllPIICategories = []string{
	CatEmail, CatURL, CatIBAN, CatVAT, CatMatricule, CatPhone, CatAmount, CatDate,
	// Extended (BUILD-03 Phase B) — hard PII, enabled at every preset.
	CatCreditCard, CatNHS, CatIPAddress, CatMACAddress, CatCrypto,
	CatDatabaseURI, CatDESteuerID, CatESNIF,
}

// AllEntityCategories lists the entity categories in a stable order.
// organisation_names and location_names have no manual-entry UI, but LLM
// proposals use them, so they are selectable switches too.
// entity_names (BUILD-06) is the merge of the former client_names and
// internal_names: the distinction cost the user a decision at every value
// they added, and the pipeline treated the two identically anyway.
var AllEntityCategories = []string{
	"entity_names", "project_names", "person_names",
	"custom_patterns", "organisation_names", "location_names",
}

// PresetSelection reproduces the exact v1 level semantics (CLAUDE.md §5)
// as a CategorySelection:
//
//	soft     = hard PII + entity/project names + custom patterns
//	medium   = soft + person names (the default)
//	advanced = medium + amounts, dates, organisations, locations
func PresetSelection(level Level) CategorySelection {
	sel := CategorySelection{
		CatEmail: true, CatURL: true, CatIBAN: true, CatVAT: true,
		CatMatricule: true, CatPhone: true,
		// BUILD-03 Phase B: extended recognizers are all hard PII and
		// fire at every level, same as the v1 hard-PII group.
		CatCreditCard: true, CatNHS: true, CatIPAddress: true,
		CatMACAddress: true, CatCrypto: true, CatDatabaseURI: true,
		CatDESteuerID: true, CatESNIF: true,
		"entity_names": true, "project_names": true,
		"custom_patterns": true,
	}
	if level == LevelMedium || level == LevelAdvanced {
		sel["person_names"] = true
	}
	if level == LevelAdvanced {
		sel[CatAmount] = true
		sel[CatDate] = true
		sel["organisation_names"] = true
		sel["location_names"] = true
	}
	return sel
}

// Run executes the full pipeline over all documents. It returns partial
// results with an error only for context cancellation; per-document
// problems degrade to warnings instead of failing the batch.
func Run(ctx context.Context, in PipelineInput) (*Results, error) {
	start := time.Now()
	reg := in.Registry
	if reg == nil {
		reg = NewRegistry()
	}
	if in.Level == "" {
		in.Level = LevelMedium // the documented default
	}
	// The granular selection is what the pipeline obeys; a nil selection
	// falls back to the level preset (v1-equivalent behaviour).
	sel := in.Categories
	if sel == nil {
		sel = PresetSelection(in.Level)
	}
	entities := filterEntities(in.Entities, sel)

	res := &Results{
		Report: Report{
			GeneratedAt: time.Now(),
			Level:       in.Level,
			ByCategory:  map[string]int{},
			LLMPass:     "skipped (Ollama not available)",
		},
	}
	if in.LLM != nil {
		res.Report.LLMPass = "completed"
	}

	// --- Passes 1–3, per document. --------------------------------------
	// llmDurations records per-document deep-scan timing for the report
	// (soft budget 30 s / 50 KB, surfaced per BUILD.md Phase 9).
	llmDurations := make([]int64, len(in.Documents))
	for i, doc := range in.Documents {
		// Cancellation is honoured between documents (Phase 8 test);
		// mid-LLM cancellation is the LLM implementation's job via ctx.
		if err := ctx.Err(); err != nil {
			res.Report.Warnings = append(res.Report.Warnings,
				fmt.Sprintf("run cancelled after %d of %d documents", i, len(in.Documents)))
			finishReport(res, start)
			return res, err
		}
		emit(in.Progress, ProgressEvent{Stage: "deterministic", DocIndex: i, DocCount: len(in.Documents), DocName: doc.Name})

		// Pass 3 preparation: deep-scan proposals become extra entities
		// for THIS document (they enter the registry, so the post-pass
		// spreads them to every other document too).
		docEntities := entities
		if in.LLM != nil {
			emit(in.Progress, ProgressEvent{Stage: "deep-scan", DocIndex: i, DocCount: len(in.Documents), DocName: doc.Name})
			llmStart := time.Now()
			proposals, err := in.LLM.DeepScan(ctx, doc.Markdown, entities)
			llmMS := time.Since(llmStart).Milliseconds()
			if err != nil {
				if ctx.Err() != nil { // cancelled mid-call
					finishReport(res, start)
					return res, ctx.Err()
				}
				// Ollama died mid-run: degrade THIS pass with a warning,
				// keep the batch going (CLAUDE.md §4 graceful degradation).
				res.Report.LLMPass = fmt.Sprintf("degraded: %v", err)
				res.Report.Warnings = append(res.Report.Warnings,
					fmt.Sprintf("deep-scan failed on %q, deterministic passes still applied: %v", doc.Name, err))
			} else {
				docEntities = append(docEntities, acceptProposals(proposals, doc.Markdown, in.Allowlist, sel)...)
			}
			llmDurations[i] = llmMS
		}

		rd, traces := anonymiseDocument(doc, docEntities, in.Patterns, sel, in.MinConfidence, in.Allowlist, reg, in.OnTrace != nil)
		if in.OnTrace != nil {
			in.OnTrace(doc.Name, traces)
		}
		res.Documents = append(res.Documents, rd)
	}

	// --- Pass 4: registry post-pass across ALL documents. ---------------
	// Late-discovered entities (or values first seen in doc N) are now in
	// the registry; re-apply every known mapping everywhere.
	entries := reg.Entries() // longest original first
	for i := range res.Documents {
		if err := ctx.Err(); err != nil {
			finishReport(res, start)
			return res, err
		}
		emit(in.Progress, ProgressEvent{Stage: "post-pass", DocIndex: i, DocCount: len(res.Documents), DocName: res.Documents[i].Name})
		applyRegistryPostPass(&res.Documents[i], entries)
	}

	// --- Final pass: ordered simple-replace rules. -----------------------
	if len(in.SimpleRules) > 0 {
		for i := range res.Documents {
			applySimpleRulesToResult(&res.Documents[i], in.SimpleRules)
		}
	}

	// --- Report assembly. -------------------------------------------------
	for i, rd := range res.Documents {
		docTotal := 0
		byCat := map[string]int{}
		for cat, n := range rd.ByCategory {
			docTotal += n
			byCat[cat] = n
			res.Report.ByCategory[cat] += n
		}
		res.Report.TotalReplacements += docTotal
		dr := DocumentReport{
			Name:         rd.Name,
			Replacements: docTotal,
			ByCategory:   byCat,
			Warnings:     rd.Warnings,
		}
		if i < len(llmDurations) {
			dr.LLMDurationMS = llmDurations[i]
		}
		res.Report.Documents = append(res.Report.Documents, dr)
	}
	finishReport(res, start)
	return res, nil
}

// emit calls the progress callback when one is registered.
func emit(fn func(ProgressEvent), ev ProgressEvent) {
	if fn != nil {
		fn(ev)
	}
}

func finishReport(res *Results, start time.Time) {
	res.Report.DurationMS = time.Since(start).Milliseconds()
}

// filterEntities keeps the entities whose category is active at the
// current level.
func filterEntities(entities []Entity, active map[string]bool) []Entity {
	var out []Entity
	for _, e := range entities {
		if active[e.Category] {
			out = append(out, e)
		}
	}
	return out
}

// acceptProposals applies the HALLUCINATION FILTER and the allowlist to
// LLM proposals (CLAUDE.md §5): a proposal is dropped unless its exact
// string occurs in the source text; allowlisted terms are dropped; and
// categories inactive at the current level are dropped. Survivors become
// regular entities (variant expansion included).
func acceptProposals(proposals []ProposedEntity, sourceText string, allow *Allowlist, active map[string]bool) []Entity {
	var out []Entity
	for _, p := range proposals {
		if !strings.Contains(sourceText, p.Text) {
			continue // hallucinated: the model invented a string
		}
		if allow.Contains(p.Text) {
			continue // allowlist wins
		}
		if !active[p.Category] {
			continue // e.g. organisation_names proposed at medium level
		}
		// An AI proposal is trusted LESS than a value the user listed
		// (BUILD-04 CR9): stamping ConfidenceLLMDefault here is what lets
		// PipelineInput.MinConfidence separate the two tiers.
		out = append(out, Entity{
			Category:   p.Category,
			Canonical:  p.Text,
			Confidence: ConfidenceLLMDefault,
		})
	}
	return out
}

// anonymiseDocument runs passes 1+2 (and the already-merged pass-3
// entities) over one document, routing grid documents through per-cell
// processing. When traceEnabled is true it collects the resolved spans of
// every anonymiseText call and returns them for the caller's OnTrace hook.
func anonymiseDocument(doc Document, entities []Entity, patterns []CustomPattern, sel CategorySelection, minConfidence float32, allow *Allowlist, reg *Registry, traceEnabled bool) (ResultDocument, []SpanTrace) {
	rd := ResultDocument{
		Name:       doc.Name,
		Format:     doc.Format,
		ByCategory: map[string]int{},
		Warnings:   doc.Warnings,
	}

	assign := func(s Span) string {
		rd.ByCategory[s.Category]++
		return reg.Assign(s.Category, s.CanonicalOrOriginal())
	}

	// tracer captures the resolved spans of each anonymiseText call when
	// tracing is on, tagging them by region so grid/json documents can be
	// walked cell-by-cell in the trace output.
	var traces []SpanTrace
	makeTraceFn := func(region string) func([]Span) {
		if !traceEnabled {
			return nil
		}
		return func(spans []Span) {
			if len(spans) == 0 {
				return
			}
			traces = append(traces, SpanTrace{Region: region, Spans: spans})
		}
	}

	if doc.Grid != nil {
		// Grid documents: anonymise cell by cell, then re-render the
		// markdown from the anonymised grid so preview == export.
		grid := make([][]string, len(doc.Grid))
		for r, row := range doc.Grid {
			grid[r] = make([]string, len(row))
			for c, cell := range row {
				var region string
				if traceEnabled {
					region = fmt.Sprintf("row %d col %d", r, c)
				}
				grid[r][c] = anonymiseText(cell, entities, patterns, sel, minConfidence, allow, assign, makeTraceFn(region))
			}
		}
		rd.Grid = grid
		rd.Anonymised = GridToMarkdownTable(grid)
		return rd, traces
	}

	if doc.Format == FormatXLSXJSON {
		// Complex sheets: anonymise the raw JSON text, keep both the JSON
		// (for .json export) and the fenced markdown rendering in sync.
		rd.JSON = anonymiseText(doc.JSON, entities, patterns, sel, minConfidence, allow, assign, makeTraceFn("json"))
		rd.Anonymised = "```json\n" + rd.JSON + "\n```\n"
		return rd, traces
	}

	rd.Anonymised = anonymiseText(doc.Markdown, entities, patterns, sel, minConfidence, allow, assign, makeTraceFn(""))
	return rd, traces
}

// anonymiseText is the shared passes-1+2 core over one piece of text.
// The CategorySelection gates BOTH the PII categories (pass 1) and the
// custom-pattern pass; entity categories were already filtered by the
// caller (filterEntities). traceFn, when non-nil, receives the resolved
// spans (post overlap resolution) before replacement — BUILD-03 Phase E.
func anonymiseText(text string, entities []Entity, patterns []CustomPattern, sel CategorySelection, minConfidence float32, allow *Allowlist, assign func(Span) string, traceFn func([]Span)) string {
	spans := FilterAllowed(DetectPIISelected(text, sel), allow)
	spans = append(spans, DetectEntities(text, entities, allow)...)
	if sel["custom_patterns"] {
		spans = append(spans, DetectCustomPatterns(text, patterns, allow)...)
	}
	// The confidence floor is applied BEFORE overlap resolution, so a
	// discarded low-confidence span cannot suppress a stronger one it
	// happens to overlap (BUILD-04 CR9). At the default 0 this is a no-op.
	spans = FilterByMinConfidence(spans, minConfidence)
	resolved := ResolveOverlaps(spans)
	if traceFn != nil {
		traceFn(resolved)
	}
	return ApplySpans(text, resolved, assign)
}

// placeholderRe recognises placeholders already inserted by earlier passes
// so the post-pass never rewrites inside them.
var placeholderRe = regexp.MustCompile(`\[[A-Z][A-Z0-9_]*_[0-9]+\]`)

// applyRegistryPostPass replaces any remaining occurrence of every known
// original with its registered placeholder — across text, grid cells and
// JSON — so cross-document consistency holds (pass 4).
func applyRegistryPostPass(rd *ResultDocument, entries []MappingEntry) {
	replaceIn := func(text string) string {
		for _, e := range entries {
			text = replaceKnownOriginal(text, e, func() { rd.ByCategory[e.Category]++ })
		}
		return text
	}

	if rd.Grid != nil {
		for r, row := range rd.Grid {
			for c, cell := range row {
				rd.Grid[r][c] = replaceIn(cell)
			}
		}
		rd.Anonymised = GridToMarkdownTable(rd.Grid)
		return
	}
	if rd.JSON != "" {
		rd.JSON = replaceIn(rd.JSON)
		rd.Anonymised = "```json\n" + rd.JSON + "\n```\n"
		return
	}
	rd.Anonymised = replaceIn(rd.Anonymised)
}

// replaceKnownOriginal substitutes one registry entry in text with
// word-boundary anchoring (same unicode-aware rule as the entity pass) and
// without touching existing placeholders. onHit is called per replacement
// for statistics.
func replaceKnownOriginal(text string, e MappingEntry, onHit func()) string {
	// Cheap pre-check before the regex machinery.
	if !strings.Contains(strings.ToLower(text), strings.ToLower(e.Original)) {
		return text
	}
	re, err := regexp.Compile(`(?i)` + regexp.QuoteMeta(e.Original))
	if err != nil {
		return text
	}

	// Protect existing placeholders: collect their ranges once and skip
	// matches inside them.
	protected := placeholderRe.FindAllStringIndex(text, -1)
	inProtected := func(start, end int) bool {
		for _, p := range protected {
			if start < p[1] && p[0] < end {
				return true
			}
		}
		return false
	}

	var b strings.Builder
	last := 0
	for _, m := range re.FindAllStringIndex(text, -1) {
		if m[0] < last || inProtected(m[0], m[1]) || !isWordBoundary(text, m[0], m[1]) {
			continue
		}
		b.WriteString(text[last:m[0]])
		b.WriteString(e.Placeholder)
		last = m[1]
		onHit()
	}
	b.WriteString(text[last:])
	return b.String()
}

// DetectKnownOriginals returns spans for every remaining occurrence of a
// known registry original in text, word-boundary anchored and never
// inside an existing placeholder. The span's Canonical is the registry
// original, so Registry.Assign maps it back to the SAME placeholder.
// Callers (the same-format export, BUILD-02 Phase 11) combine these with
// the pass-1/2 spans and run ResolveOverlaps; pass entries longest-first
// (Registry.Entries) so longer originals win ties.
func DetectKnownOriginals(text string, entries []MappingEntry) []Span {
	protected := placeholderRe.FindAllStringIndex(text, -1)
	inProtected := func(start, end int) bool {
		for _, p := range protected {
			if start < p[1] && p[0] < end {
				return true
			}
		}
		return false
	}

	var spans []Span
	for _, e := range entries {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(e.Original)) {
			continue
		}
		re, err := regexp.Compile(`(?i)` + regexp.QuoteMeta(e.Original))
		if err != nil {
			continue
		}
		for _, m := range re.FindAllStringIndex(text, -1) {
			if inProtected(m[0], m[1]) || !isWordBoundary(text, m[0], m[1]) {
				continue
			}
			spans = append(spans, Span{
				Start:     m[0],
				End:       m[1],
				Category:  e.Category,
				Original:  text[m[0]:m[1]],
				Canonical: e.Original,
			})
		}
	}
	return spans
}

// applySimpleRulesToResult runs the ordered manual rules over every
// representation of the document and records counts under the
// "simple_replace" category.
func applySimpleRulesToResult(rd *ResultDocument, rules []SimpleRule) {
	total := 0
	if rd.Grid != nil {
		for r, row := range rd.Grid {
			for c, cell := range row {
				out, counts := ApplySimpleRules(cell, rules)
				rd.Grid[r][c] = out
				total += sum(counts)
			}
		}
		rd.Anonymised = GridToMarkdownTable(rd.Grid)
	} else if rd.JSON != "" {
		out, counts := ApplySimpleRules(rd.JSON, rules)
		rd.JSON = out
		rd.Anonymised = "```json\n" + out + "\n```\n"
		total += sum(counts)
	} else {
		out, counts := ApplySimpleRules(rd.Anonymised, rules)
		rd.Anonymised = out
		total += sum(counts)
	}
	if total > 0 {
		rd.ByCategory["simple_replace"] += total
	}
}

func sum(ns []int) int {
	t := 0
	for _, n := range ns {
		t += n
	}
	return t
}

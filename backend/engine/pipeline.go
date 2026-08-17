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
	"sort"
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
// render live progress. Stage is one of "deterministic",
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
	// nil means "use PresetSelection(Level)", which
	// reproduces the v1 behaviour byte for byte.
	Categories CategorySelection
	// MinConfidence drops every detected span scoring below it, on the
	//  scale. 0 (the default) keeps
	// everything, which reproduces the pre- behaviour exactly.
	// See FilterByMinConfidence for what each level currently excludes.
	MinConfidence float32
	// Country scopes the country-specific regex categories. Empty falls back to
	// Luxembourg, the documented application default.
	Country string
	// SuppressRegexPII is the "Native detection" master switch, inverted: when
	// true, pass 1 (the deterministic regex PII pass in pii.go) is skipped
	// entirely, so NO signal category (email, VAT, IBAN, amount, date, ...) is
	// replaced regardless of what Categories selects. The Categories map is
	// left untouched on purpose: the user's per-category selection is
	// remembered so turning Native detection back on restores it exactly. Only
	// the regex signal categories are affected; the entity pass, custom
	// patterns, the code detector and everything else run unchanged.
	SuppressRegexPII bool
	Allowlist        *Allowlist
	// Registry is the session registry. Pass the same instance across
	// runs to keep placeholders stable for the whole session; nil creates
	// a fresh one (fresh numbering).
	Registry *Registry
	// LLM is the deep-scan slot; nil skips pass 3 (the report notes it).
	LLM LLM
	// SimpleRules run last, in order (simplereplace.go).
	SimpleRules []SimpleRule
	// Removed tracks values the user deleted from the session.
	// They must not appear in any run without explicit restoration.
	Removed []RemovedValue
	// SkipValidation is for tests that deliberately create conflicts.
	SkipValidation bool
	// Progress, when set, receives per-document stage events.
	Progress func(ProgressEvent)
	// OnTrace, when set, receives the final resolved spans (post-filter,
	// post-overlap resolution) that WERE about to be replaced in each
	// document. One SpanTrace per anonymisation call — for grid or JSON
	// documents that means one trace per cell / json region, tagged via
	// SpanTrace.Region. nil disables tracing entirely (zero cost: no
	// collection, no callback dispatch). .
	OnTrace func(docName string, traces []SpanTrace)
}

// SpanTrace is one snapshot of the spans a single anonymiseText call was
// about to replace, keyed by the region of the document they came from
// (empty for the document body, "row R col C" for grid cells, "json" for
// complex xlsx sheets). .
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
	// OccurrenceVariants records, per placeholder, the ordered spellings that
	// produced each occurrence of that placeholder in Anonymised. Slot i holds
	// the text the i-th occurrence of that placeholder replaced ("Borch"), or
	// "" when that occurrence matched the canonical value itself. It lets the
	// results view show the exact variant a mark replaced next to the canonical
	// value ("Borch (Johannes Borch)"). A placeholder absent from this map, or a
	// "" slot, means "the canonical value" and needs no bracketed original.
	//
	// It is deliberately PER DOCUMENT: the Compare view zips this positionally
	// with the placeholder occurrences it renders for the one document on
	// screen. Only occurrences that carry a non-canonical spelling are worth the
	// bytes, so a document whose every hit was the canonical value serialises
	// nothing here.
	OccurrenceVariants map[string][]string `json:"occurrenceVariants,omitempty"`
}

// Results is the full outcome of one pipeline run.
type Results struct {
	Documents []ResultDocument `json:"documents"`
	Report    Report           `json:"report"`
	// Validation contains any conflicts detected before the run.
	// Blocking conflicts mean the pipeline did not run (Documents and Report
	// are empty). Warnings are generated during the run.
	Validation ValidationResult `json:"validation,omitempty"`
}

func detectedCategoriesFromCounts(counts map[string]int) []string {
	var out []string
	for category, count := range counts {
		if count > 0 {
			out = append(out, category)
		}
	}
	sort.Strings(out)
	return out
}

// Entity category identifiers. They are engine CONTRACTS: they appear in
// session files and in the exported re-identification key, so they are never
// renamed to follow a display label. The labels live on the frontend
// (copy.js CATEGORY_LABELS), cross-checked by ../category_parity_test.go.
//
// Every one of them is reachable by manual entry and by the local AI. The
// annotated ones are additionally reachable OFFLINE, by a heuristic detector;
// the rest need the AI or a value the user types, and the frontend label says
// so.
const (
	CatEntityNames     = "entity_names"     // + heuristic: legal-suffix runs
	CatProjectNames    = "project_names"    // + heuristic: codes beside a project cue
	CatProductNames    = "product_names"    // + heuristic: trademark mark, product head noun
	CatBrandNames      = "brand_names"      // AI or manual: a brand is world knowledge
	CatPersonNames     = "person_names"     // + heuristic: title cues, multi-word runs
	CatIdentifierNames = "identifier_names" // + heuristic: reference and contract codes
	CatOtherNames      = "other_names"      // AI or manual: "a name, and none of the above"
	CatCustomPatterns  = "custom_patterns"  // the user's own regexes
)

// CategorySelection is the granular per-category switch set the pipeline
// obeys: every PII category (email, url, iban, vat,
// matricule, phone, amount, date) and every entity category maps to on/off.
// Levels are PRESETS that fill this map (PresetSelection); the UI may then
// flip individual switches ("custom" mode).
type CategorySelection map[string]bool

// AllPIICategories lists the pass-1 categories in a stable order (used by
// presets, tests and the configure UI documentation).
// added the extended recognizers after the v1 group so v1 UI ordering is
// preserved and any UI that iterates this list sees new categories at the
// tail.
var AllPIICategories = []string{
	CatEmail, CatURL, CatIBAN, CatVAT, CatMatricule, CatPhone, CatAmount, CatDate,
	// Extended — hard PII, enabled at every preset.
	CatCreditCard, CatNHS, CatIPAddress, CatMACAddress, CatCrypto,
	CatDatabaseURI, CatDESteuerID, CatESNIF,
}

// AllEntityCategories lists the entity categories in a stable order, mirrored
// by frontend/state.js and checked by ../category_parity_test.go.
var AllEntityCategories = []string{
	CatEntityNames, CatProjectNames, CatProductNames, CatBrandNames,
	CatPersonNames, CatIdentifierNames, CatOtherNames, CatCustomPatterns,
}

// PresetSelection fills a CategorySelection from a level (CLAUDE.md §5):
//
//	soft     = hard PII + entity, project and identifier names + custom patterns
//	medium   = soft + person, product and brand names (the default)
//	advanced = medium + amounts, dates and other names
//
// The tiers are ordered by how much ordinary text each risks catching.
// identifier_names is code-shaped and near-PII, so it sits with the hard group.
// product_names and brand_names can catch a PUBLIC product name, which is a
// per-document allowlist decision rather than a mistake, so they wait for
// medium. other_names is the noisiest by definition, so it waits for advanced.
func PresetSelection(level Level) CategorySelection {
	sel := CategorySelection{
		CatEmail: true, CatURL: true, CatIBAN: true, CatVAT: true,
		CatMatricule: true, CatPhone: true,
		CatCreditCard: true, CatNHS: true, CatIPAddress: true,
		CatMACAddress: true, CatCrypto: true, CatDatabaseURI: true,
		CatDESteuerID: true, CatESNIF: true,
		CatEntityNames: true, CatProjectNames: true, CatIdentifierNames: true,
		CatCustomPatterns: true,
	}
	if level == LevelMedium || level == LevelAdvanced {
		sel[CatPersonNames] = true
		sel[CatProductNames] = true
		sel[CatBrandNames] = true
	}
	if level == LevelAdvanced {
		sel[CatAmount] = true
		sel[CatDate] = true
		sel[CatOtherNames] = true
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
	if in.Country == "" {
		in.Country = CountryLU
	}
	// The granular selection is what the pipeline obeys; a nil selection
	// falls back to the level preset (v1-equivalent behaviour).
	sel := in.Categories
	if sel == nil {
		sel = PresetSelection(in.Level)
	}

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

	// The preamble, in this order and no other:
	//
	//   1. removals first. A removed value stops being a declaration, so it must
	//      not be validated as one: it is enforced through the allowlist, and
	//      validating it would refuse every run after a removal as a value that
	//      is both declared and never-anonymised.
	//   2. validation second, over what is left, BEFORE any text is touched. A
	//      half-run that assigned placeholders for a configuration the user was
	//      just told is invalid is unrecoverable without a new session.
	//   3. reservations third, so a rule's replacement cannot be handed to an
	//      automatic assignment during the run that follows.
	ApplyRemovals(in.Allowlist, in.Removed)
	entities := FilterRemoved(filterEntities(in.Entities, sel), in.Removed)

	res.Validation = ValidateValues(ValidationInput{
		Entities:       entities,
		Patterns:       in.Patterns,
		SimpleRules:    in.SimpleRules,
		Allowlist:      in.Allowlist,
		Categories:     sel,
		Registry:       reg,
		SkipValidation: in.SkipValidation,
	})
	if len(res.Validation.Blocking) > 0 {
		return res, nil
	}

	for _, rule := range in.SimpleRules {
		// An error means the placeholder is already taken, which validation has
		// just cleared, so there is nothing left to report.
		_ = reg.Reserve(rule.Replace)
	}

	// Overlap warnings come from the ONE place the decision is made, the span
	// resolver, and accumulate across every document and grid cell of the run.
	overlaps := newOverlapWarnings()

	// --- Passes 1–3, per document. --------------------------------------
	// llmDurations records per-document deep-scan timing for the report
	// (soft budget 30 s / 50 KB, surfaced per).
	llmDurations := make([]int64, len(in.Documents))
	for i, doc := range in.Documents {
		// Cancellation is honoured between documents;
		// mid-LLM cancellation is the LLM implementation's job via ctx.
		if err := ctx.Err(); err != nil {
			res.Report.Warnings = append(res.Report.Warnings,
				fmt.Sprintf("run cancelled after %d of %d documents", i, len(in.Documents)))
			finishReport(res, start, overlaps)
			return res, err
		}
		emit(in.Progress, ProgressEvent{Stage: "deterministic", DocIndex: i, DocCount: len(in.Documents), DocName: doc.Name})

		// Pass 3 preparation: deep-scan proposals become extra entities
		// for THIS document (they enter the registry, so the post-pass
		// spreads them to every other document too).
		docEntities := append([]Entity(nil), entities...)
		if in.LLM != nil {
			emit(in.Progress, ProgressEvent{Stage: "deep-scan", DocIndex: i, DocCount: len(in.Documents), DocName: doc.Name})
			llmStart := time.Now()
			proposals, err := in.LLM.DeepScan(ctx, doc.Markdown, entities)
			llmMS := time.Since(llmStart).Milliseconds()
			if err != nil {
				if ctx.Err() != nil { // cancelled mid-call
					finishReport(res, start, overlaps)
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

		scope := detectionScope{
			entities:         docEntities,
			patterns:         in.Patterns,
			categories:       sel,
			minConfidence:    in.MinConfidence,
			country:          in.Country,
			allow:            in.Allowlist,
			suppressRegexPII: in.SuppressRegexPII,
		}
		rd, traces := anonymiseDocument(doc, scope, reg, overlaps, in.OnTrace != nil)
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
			finishReport(res, start, overlaps)
			return res, err
		}
		emit(in.Progress, ProgressEvent{Stage: "post-pass", DocIndex: i, DocCount: len(res.Documents), DocName: res.Documents[i].Name})
		applyRegistryPostPass(&res.Documents[i], entries)
	}

	// --- Final pass: ordered simple-replace rules. -----------------------
	// Per-document, per-rule counts are kept so the report can name what each
	// rule rewrote instead of a bare "simple_replace" total.
	simpleCounts := make([][]int, len(res.Documents))
	if len(in.SimpleRules) > 0 {
		for i := range res.Documents {
			simpleCounts[i] = applySimpleRulesToResult(&res.Documents[i], in.SimpleRules)
		}
	}

	// Placeholders whose every occurrence matched the canonical value carry no
	// bracketed original, so their all-"" variant slices are dropped to keep the
	// per-document payload small. Runs after the simple-replace pass so a rule
	// that rewrites to a placeholder keeps its recorded find text.
	for i := range res.Documents {
		pruneCanonicalOnlyVariants(&res.Documents[i])
	}

	// --- Report assembly. -------------------------------------------------
	// The per-VALUE tables are built from the registry and the finished text,
	// once, here. They used to be recomputed in JavaScript on every repaint of
	// the report card, and they were absent from the exported report entirely.
	entries = reg.Entries()
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
			Name:               rd.Name,
			Replacements:       docTotal,
			ByCategory:         byCat,
			Warnings:           rd.Warnings,
			DetectedCategories: detectedCategoriesFromCounts(byCat),
		}
		if i < len(llmDurations) {
			dr.LLMDurationMS = llmDurations[i]
		}
		dr.Values = valueReports(entries, []ResultDocument{rd})
		dr.Values = appendSimpleRuleValues(dr.Values, in.SimpleRules, simpleCounts[i])
		res.Report.Documents = append(res.Report.Documents, dr)
	}
	res.Report.Values = valueReports(entries, res.Documents)
	res.Report.Values = appendSimpleRuleValues(res.Report.Values, in.SimpleRules, sumRuleCounts(in.SimpleRules, simpleCounts))
	res.Report.DetectedCategories = detectedCategoriesFromCounts(res.Report.ByCategory)
	finishReport(res, start, overlaps)
	return res, nil
}

// valueReports lists what each registry entry actually replaced in the given
// documents, most frequent first.
//
// The count is taken from the FINISHED text rather than from the registry's
// own counter, for two reasons: the registry counts per SESSION, so it cannot
// answer a per-document question, and the post-pass and the simple-replace
// rules both rewrite text after the counter was incremented. Counting
// placeholders in the text that will be exported is the only figure that
// matches what the user will see.
//
// A value with no occurrences in scope is omitted: it belongs to another
// document, or to an earlier run whose placeholder the session registry kept.
func valueReports(entries []MappingEntry, docs []ResultDocument) []ValueReport {
	out := make([]ValueReport, 0, len(entries))
	for _, e := range entries {
		count := 0
		for _, d := range docs {
			// Count the placeholder in the text as exported. For a grid or a
			// complex sheet, Anonymised is regenerated from the cells, so it
			// stays the single thing to count.
			count += strings.Count(d.Anonymised, e.Placeholder)
		}
		if count == 0 {
			continue
		}
		out = append(out, ValueReport{
			Original:    e.Original,
			Placeholder: e.Placeholder,
			Category:    e.Category,
			Count:       count,
		})
	}
	sortValueReports(out)
	return out
}

// sortValueReports orders value rows most-frequent first, then by placeholder
// so the report is deterministic (stable golden tests and diffs).
func sortValueReports(out []ValueReport) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Placeholder < out[j].Placeholder
	})
}

// appendSimpleRuleValues folds the manual find-and-replace rules into a value
// report list and re-sorts the result. Without it the "simple_replace"
// category shows a total in the by-category breakdown but drills down to
// nothing, because the rules never touch the registry the other rows come
// from. A rule that matched nothing, or whose replacement is not a value the
// user would recognise, is still listed by find text so the drill-down agrees
// with the total.
func appendSimpleRuleValues(values []ValueReport, rules []SimpleRule, counts []int) []ValueReport {
	for i, rule := range rules {
		if i >= len(counts) || counts[i] == 0 || rule.Find == "" {
			continue
		}
		values = append(values, ValueReport{
			Original:    rule.Find,
			Placeholder: rule.Replace,
			Category:    "simple_replace",
			Count:       counts[i],
		})
	}
	sortValueReports(values)
	return values
}

// sumRuleCounts totals each rule's replacements across every document, so the
// run-level report aggregates what the per-document reports split.
func sumRuleCounts(rules []SimpleRule, perDoc [][]int) []int {
	totals := make([]int, len(rules))
	for _, counts := range perDoc {
		for i, c := range counts {
			if i < len(totals) {
				totals[i] += c
			}
		}
	}
	return totals
}

// pruneCanonicalOnlyVariants drops every placeholder whose recorded
// occurrences were all the canonical value. Those need no bracketed original
// (the tooltip falls back to the mapping), so keeping their all-"" slices only
// grows the payload. A placeholder with even one non-canonical spelling is kept
// whole, "" slots included, so the frontend can line each occurrence up with
// the placeholder it renders.
func pruneCanonicalOnlyVariants(rd *ResultDocument) {
	if rd.OccurrenceVariants == nil {
		return
	}
	for ph, variants := range rd.OccurrenceVariants {
		keep := false
		for _, v := range variants {
			if v != "" {
				keep = true
				break
			}
		}
		if !keep {
			delete(rd.OccurrenceVariants, ph)
		}
	}
	if len(rd.OccurrenceVariants) == 0 {
		rd.OccurrenceVariants = nil
	}
}

// emit calls the progress callback when one is registered.
func emit(fn func(ProgressEvent), ev ProgressEvent) {
	if fn != nil {
		fn(ev)
	}
}

// finishReport stamps the run's duration and folds in the warnings collected
// while it ran. Every exit from Run goes through it, including cancellation, so
// a partial run still reports what it saw.
func finishReport(res *Results, start time.Time, overlaps *overlapWarnings) {
	res.Report.DurationMS = time.Since(start).Milliseconds()
	if overlaps != nil {
		res.Validation.Warnings = append(res.Validation.Warnings, overlaps.conflicts()...)
	}
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
		// stamping ConfidenceLLMDefault here is what lets
		// PipelineInput.MinConfidence separate the two tiers.
		out = append(out, Entity{
			Category:   p.Category,
			Canonical:  p.Text,
			Confidence: ConfidenceLLMDefault,
		})
	}
	return out
}

// detectionScope is the fixed configuration passes 1 and 2 share for one run:
// what to look for, how sure a detection has to be, and what is off limits. It
// is a struct because seven positional parameters threaded through four call
// sites is a shape where a swapped pair compiles and silently changes what gets
// replaced.
type detectionScope struct {
	entities      []Entity
	patterns      []CustomPattern
	categories    CategorySelection
	minConfidence float32
	country       string
	allow         *Allowlist
	// suppressRegexPII, when true, skips pass 1 (the regex PII detectors) for
	// this run: the "Native detection" master switch is off. The entity and
	// custom-pattern passes are unaffected.
	suppressRegexPII bool
}

// anonymiseDocument runs passes 1 and 2 (with the already-merged pass-3
// entities) over one document, routing grid documents through per-cell
// processing.
//
// @param overlaps collects the spans overlap resolution discarded, so the run
//
//	warns about them from the ONE place the decision is made. nil skips it.
//
// @param traceEnabled collects the resolved spans for the caller's OnTrace hook
func anonymiseDocument(doc Document, scope detectionScope, reg *Registry,
	overlaps *overlapWarnings, traceEnabled bool) (ResultDocument, []SpanTrace) {

	rd := ResultDocument{
		Name:               doc.Name,
		Format:             doc.Format,
		ByCategory:         map[string]int{},
		Warnings:           doc.Warnings,
		OccurrenceVariants: map[string][]string{},
	}

	assign := func(s Span) string {
		rd.ByCategory[s.Category]++
		canonical := s.CanonicalOrOriginal()
		ph := reg.Assign(s.Category, canonical)
		// Record the spelling this occurrence actually matched. "" when it was
		// the canonical value, so the tooltip needs no bracketed original; the
		// matched text otherwise ("Borch" for canonical "Johannes Borch"). The
		// closure is ApplySpans' single choke point and it is called in
		// left-to-right offset order, which is exactly the order the frontend
		// walks placeholders in, so slot i lines up with occurrence i.
		variant := ""
		if !strings.EqualFold(s.Original, canonical) {
			variant = s.Original
		}
		rd.OccurrenceVariants[ph] = append(rd.OccurrenceVariants[ph], variant)
		return ph
	}

	// Tracing tags spans by region, so grid and json documents can be walked
	// cell by cell in the trace output.
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
		// Grid documents: anonymise cell by cell, then re-render the markdown
		// from the anonymised grid so preview and export agree.
		grid := make([][]string, len(doc.Grid))
		for r, row := range doc.Grid {
			grid[r] = make([]string, len(row))
			for c, cell := range row {
				var region string
				if traceEnabled {
					region = fmt.Sprintf("row %d col %d", r, c)
				}
				grid[r][c] = anonymiseText(cell, scope, assign, makeTraceFn(region), overlaps)
			}
		}
		rd.Grid = grid
		rd.Anonymised = GridToMarkdownTable(grid)
		return rd, traces
	}

	if doc.Format == FormatXLSXJSON {
		// Complex sheets: anonymise the raw JSON text, keeping both the JSON
		// (for the .json export) and the fenced markdown rendering in sync.
		rd.JSON = anonymiseText(doc.JSON, scope, assign, makeTraceFn("json"), overlaps)
		rd.Anonymised = "```json\n" + rd.JSON + "\n```\n"
		return rd, traces
	}

	rd.Anonymised = anonymiseText(doc.Markdown, scope, assign, makeTraceFn(""), overlaps)
	return rd, traces
}

// anonymiseText is the shared passes-1-and-2 core over one piece of text.
//
// The category selection gates BOTH the PII categories (pass 1) and the
// custom-pattern pass; entity categories were already filtered by the caller
// (filterEntities). When scope.suppressRegexPII is set (the "Native detection"
// master switch is off) pass 1 is skipped entirely, so no signal category is
// replaced; the entity and custom-pattern passes still run.
func anonymiseText(text string, scope detectionScope, assign func(Span) string,
	traceFn func([]Span), overlaps *overlapWarnings) string {

	var spans []Span
	if !scope.suppressRegexPII {
		spans = FilterAllowed(DetectPIISelected(text, scope.categories, scope.country), scope.allow)
	}
	spans = append(spans, DetectEntities(text, scope.entities, scope.allow)...)
	if scope.categories[CatCustomPatterns] {
		spans = append(spans, DetectCustomPatterns(text, scope.patterns, scope.allow)...)
	}
	// The confidence floor is applied BEFORE overlap resolution, so a discarded
	// low-confidence span cannot suppress a stronger one it happens to overlap.
	// At the default 0 this is a no-op.
	spans = FilterByMinConfidence(spans, scope.minConfidence)

	// The losers are collected only while the warning collector still wants
	// them. Gathering them regardless costs an allocation per discarded span on
	// a path that runs over every document, and a document full of name
	// variants discards several per replacement.
	resolved, dropped := resolveOverlaps(spans, overlaps != nil && overlaps.wants())
	if traceFn != nil {
		traceFn(resolved)
	}
	if len(dropped) > 0 {
		overlaps.add(dropped)
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
// Callers (the same-format export) combine these with
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
				// A registry entry is ownership that is already DECIDED: the
				// string earned this placeholder in an earlier pass or an
				// earlier document, and a placeholder that has left the machine
				// can never be re-numbered. So it outranks a fresh detection
				// rather than being re-litigated by one.
				Origin: OriginNative,
			})
		}
	}
	return spans
}

// applySimpleRulesToResult runs the ordered manual rules over every
// representation of the document and records counts under the
// "simple_replace" category. It returns the per-rule replacement counts for
// this document so the report can name what each rule rewrote.
func applySimpleRulesToResult(rd *ResultDocument, rules []SimpleRule) []int {
	perRule := make([]int, len(rules))
	add := func(counts []int) {
		for i, c := range counts {
			if i < len(perRule) {
				perRule[i] += c
			}
		}
	}
	if rd.Grid != nil {
		for r, row := range rd.Grid {
			for c, cell := range row {
				out, counts := ApplySimpleRules(cell, rules)
				rd.Grid[r][c] = out
				add(counts)
			}
		}
		rd.Anonymised = GridToMarkdownTable(rd.Grid)
	} else if rd.JSON != "" {
		out, counts := ApplySimpleRules(rd.JSON, rules)
		rd.JSON = out
		rd.Anonymised = "```json\n" + out + "\n```\n"
		add(counts)
	} else {
		out, counts := ApplySimpleRules(rd.Anonymised, rules)
		rd.Anonymised = out
		add(counts)
	}
	total := 0
	for _, c := range perRule {
		total += c
	}
	if total > 0 {
		rd.ByCategory["simple_replace"] += total
	}
	recordSimpleRuleVariants(rd, rules, perRule)
	return perRule
}

// recordSimpleRuleVariants notes the find text behind each placeholder a rule
// produced, so a mark the rule created hovers with the text it replaced
// ("PwC") rather than only the placeholder's canonical owner. Only a rule
// whose whole replacement IS a placeholder leaves a mark to hover; a rule that
// rewrites to plain text produces nothing the tooltip can land on.
func recordSimpleRuleVariants(rd *ResultDocument, rules []SimpleRule, counts []int) {
	if rd.OccurrenceVariants == nil {
		rd.OccurrenceVariants = map[string][]string{}
	}
	for i, rule := range rules {
		if i >= len(counts) || counts[i] == 0 {
			continue
		}
		if placeholderRe.FindString(rule.Replace) != rule.Replace {
			continue
		}
		for n := 0; n < counts[i]; n++ {
			rd.OccurrenceVariants[rule.Replace] = append(rd.OccurrenceVariants[rule.Replace], rule.Find)
		}
	}
}

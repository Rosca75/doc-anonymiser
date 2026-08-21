// engine/pipeline.go — pass orchestration per anonymisation level
// (CLAUDE.md §3/§5). This is the app's core: everything else feeds it or
// renders its results.
//
// Fixed pass order (CLAUDE.md §5):
//  1. Deterministic PII regex pass (pii.go).
//  2. Known-value pass: values expanded into variants, plus user
//     custom patterns, longest-match-first (values.go).
//  3. Post-pass: the FULL registry is re-applied to every document, so a
//     value declared late (e.g. matched first in doc 40) is also replaced in
//     the documents processed earlier — same real-world subject, same
//     placeholder, everywhere.
//
// Anonymise is DETERMINISTIC end to end. No discovery method runs here, and no
// value is created by the run itself: the local model is an Identify-time
// discovery route whose findings the user accepts as Suggestions first. A run
// that could mint a value the user never saw would walk past the review gate.
//
// Run executes those passes in THREE phases rather than one loop, because
// ownership has to be decided over the whole batch:
//
//	A. detect. Every document, every region, spans only. Nothing is replaced
//	   and no placeholder is minted.
//	B. unify. One string, one owner, picked by the precedence rule
//	   (unifyOwnership). Overruled claims become warnings.
//	C. apply. Overlap resolution and replacement, region by region, reusing
//	   phase A's spans, so no detection work is repeated.
//
// The split exists because Registry.Assign's byOriginal index gives a string to
// its FIRST claimant for the whole session, and with detection and replacement
// in one step the first claimant was decided by byte offset within document
// order. The category a value ended up under therefore depended on the order
// the files were imported in. Phase B makes it a rule instead. The cost is that
// a batch holds its spans until phase C; a span is five words plus two strings,
// so a 10 MB batch replacing every 200 bytes is a few MB, bounded by the same
// import limits as the documents themselves.
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

// ProgressEvent is emitted before each per-document stage so the UI can
// render live progress. Stage is one of "deterministic" or "post-pass".
type ProgressEvent struct {
	Stage    string `json:"stage"`
	DocIndex int    `json:"docIndex"` // 0-based
	DocCount int    `json:"docCount"`
	DocName  string `json:"docName"`
}

// PipelineInput bundles everything Run needs.
type PipelineInput struct {
	Documents []Document
	Values    []Value
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
	// SuppressRegexPII is the Built-in patterns switch, inverted: when
	// true, pass 1 (the deterministic regex PII pass in pii.go) is skipped
	// entirely, so NO signal category (email, VAT, IBAN, amount, date, ...) is
	// replaced regardless of what Categories selects. The Categories map is
	// left untouched on purpose: the user's per-category selection is
	// remembered so turning Native detection back on restores it exactly. Only
	// the regex signal categories are affected; the Value pass, custom
	// patterns, the code detector and everything else run unchanged.
	SuppressRegexPII bool
	Allowlist        *Allowlist
	// Registry is the session registry. Pass the same instance across
	// runs to keep placeholders stable for the whole session; nil creates
	// a fresh one (fresh numbering).
	Registry *Registry
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
	// Grid is the anonymised cell grid for CSV-matchClass documents (nil
	// otherwise) — the source for CSV round-trip export.
	Grid [][]string `json:"grid,omitempty"`
	// JSON is the anonymised structured JSON for complex xlsx sheets.
	JSON string `json:"json,omitempty"`
	// ByCategory counts replacements per category in this document.
	ByCategory map[string]int `json:"byCategory"`
	// Warnings carries the document's ingestion warnings through to the
	// results screen and report.
	Warnings []string `json:"warnings,omitempty"`
	// OccurrenceSpellings records, per placeholder, the ordered spellings that
	// produced each occurrence of that placeholder in Anonymised. Slot i holds
	// the text the i-th occurrence of that placeholder replaced ("Borch"), or
	// "" when that occurrence matched the mainText value itself. It lets the
	// results view show the exact variant a mark replaced next to the mainText
	// value ("Borch (Johannes Borch)"). A placeholder absent from this map, or a
	// "" slot, means "the mainText value" and needs no bracketed original.
	//
	// It is deliberately PER DOCUMENT: the Compare view zips this positionally
	// with the placeholder occurrences it renders for the one document on
	// screen. Only occurrences that carry a non-mainText spelling are worth the
	// bytes, so a document whose every hit was the mainText value serialises
	// nothing here.
	OccurrenceSpellings map[string][]string `json:"occurrenceSpellings,omitempty"`
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

// Value category identifiers. They are engine CONTRACTS: they appear in
// session files and in the exported re-identification key, so they are never
// renamed to follow a display label. The labels live on the frontend
// (copy.js CATEGORY_LABELS), cross-checked by ../category_parity_test.go.
//
// Every one of them is reachable by manual entry and by the local model. The
// annotated ones are additionally reachable OFFLINE, by a heuristic detector;
// the rest need the local model or a value the user types, and the frontend label says
// so.
const (
	CatEntityNames     = "entity_names"     // + heuristic: legal-suffix runs
	CatProjectNames    = "project_names"    // + heuristic: codes beside a project cue
	CatProductNames    = "product_names"    // + heuristic: trademark mark, product head noun
	CatBrandNames      = "brand_names"      // local model or manual: a brand is world knowledge
	CatPersonNames     = "person_names"     // + heuristic: title cues, multi-word runs
	CatIdentifierNames = "identifier_names" // + heuristic: reference and contract codes
	CatOtherNames      = "other_names"      // local model or manual: "a name, and none of the above"
	CatCustomPatterns  = "custom_patterns"  // the user's own regexes
	// CatCountryNames is a country or jurisdiction named in the document. It has
	// its own category because in a two-party contract between two entities of
	// one country the jurisdiction is part of the identity, and filing it under
	// other_names loses that distinction in the mapping CSV.
	CatCountryNames = "country_names" // local model or manual: a gazetteer would fire in prose
	// CatNationalityNames is a nationality or demonym ("Française").
	CatNationalityNames = "nationality_names" // local model or manual: an ordinary adjective
	// CatBusinessSectorNames is an industry or line of business ("Transport").
	CatBusinessSectorNames = "business_sector_names" // local model or manual: an ordinary noun
)

// CategorySelection is the granular per-category switch set the pipeline
// obeys: every PII category (email, url, iban, vat,
// matricule, phone, amount, date) and every value category maps to on/off.
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
	// A bank identifier, and the two location shapes. Appended so an existing UI
	// ordering is preserved and new categories arrive at the tail.
	CatBIC, CatPostalCode, CatAddress,
}

// AllValueCategories lists the value categories in a stable order, mirrored
// by frontend/state.js and checked by ../category_parity_test.go.
var AllValueCategories = []string{
	CatEntityNames, CatProjectNames, CatProductNames, CatBrandNames,
	CatPersonNames, CatIdentifierNames, CatOtherNames, CatCustomPatterns,
	// Appended after the original eight so an existing UI ordering is preserved
	// and any consumer iterating this list sees new categories at the tail.
	CatCountryNames, CatNationalityNames, CatBusinessSectorNames,
}

// isValueCategory reports whether category is one of AllValueCategories. It
// is the check that tells an unrecognised category (a bug upstream, such as
// the JS dropdown that once emitted "person_names,Person names") apart from a
// real category the user has simply switched off: only the first is a run
// warning, because a switched-off category staying silent is by design.
func isValueCategory(category string) bool {
	for _, c := range AllValueCategories {
		if c == category {
			return true
		}
	}
	return false
}

// unrecognisedValueWarnings names every declared Value whose category is not
// a real engine category, so filterValues dropping it silently a moment later
// does not read as a Value that simply matched nothing. It is a WARNING, not a
// blocking conflict: refusing the whole run over one malformed declaration
// would punish the user for a state the pipeline can already resolve (it
// drops exactly that one Value and applies the rest), the same reasoning
// CLAUDE.md gives for intersections.
func unrecognisedValueWarnings(values []Value) []string {
	var warnings []string
	for _, v := range values {
		if !isValueCategory(v.Category) {
			warnings = append(warnings, fmt.Sprintf(
				`the Value "%s" has an unrecognised type and was not applied, remove it and declare it again from the type list`,
				v.MainText))
		}
	}
	return warnings
}

// PresetSelection fills a CategorySelection from a level (CLAUDE.md §5):
//
//	soft     = hard PII + value, project and identifier names + custom patterns
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
		// A BIC identifies a bank account's institution and travels beside the
		// IBAN it belongs to, so it is hard PII and fires at every level.
		CatBIC:         true,
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
		// Locations, and the context values that read like locations. CLAUDE.md
		// §5 puts location names at advanced, and a street address, a postal code
		// and a country are all locations; a nationality and a business sector
		// are the same kind of context value, identifying only in combination.
		sel[CatAddress] = true
		sel[CatPostalCode] = true
		sel[CatCountryNames] = true
		sel[CatNationalityNames] = true
		sel[CatBusinessSectorNames] = true
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
		},
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
	ApplyRemovals(in.Allowlist, in.Removed)
	declared := FilterRemoved(in.Values, in.Removed)
	// Named here, before filterValues drops it below: a category filterValues
	// has never heard of is not a switched-off category, it is a declaration
	// nothing can ever apply.
	res.Report.Warnings = append(res.Report.Warnings, unrecognisedValueWarnings(declared)...)
	values := filterValues(declared, sel)

	res.Validation = ValidateValues(ValidationInput{
		Values:         values,
		Patterns:       in.Patterns,
		Allowlist:      in.Allowlist,
		Categories:     sel,
		Registry:       reg,
		SkipValidation: in.SkipValidation,
	})
	if len(res.Validation.Blocking) > 0 {
		return res, nil
	}

	// Overlap warnings come from the ONE place the decision is made, the span
	// resolver, and accumulate across every document and grid cell of the run.
	overlaps := newOverlapWarnings()

	// --- Phase A: detect, per document. ---------------------------------
	// Nothing is replaced here, and no placeholder is minted. Detection has to
	// finish across the whole batch before ownership can be decided by rule
	// rather than by the order the documents happen to be in.
	plans := make([]documentPlan, 0, len(in.Documents))
	for i, doc := range in.Documents {
		// Cancellation is honoured between documents.
		if err := ctx.Err(); err != nil {
			res.Report.Warnings = append(res.Report.Warnings,
				fmt.Sprintf("run cancelled after %d of %d documents", i, len(in.Documents)))
			finishReport(res, start, overlaps)
			return res, err
		}
		emit(in.Progress, ProgressEvent{Stage: "deterministic", DocIndex: i, DocCount: len(in.Documents), DocName: doc.Name})

		scope := detectionScope{
			values:           values,
			patterns:         in.Patterns,
			categories:       sel,
			minConfidence:    in.MinConfidence,
			country:          in.Country,
			allow:            in.Allowlist,
			suppressRegexPII: in.SuppressRegexPII,
		}
		plans = append(plans, detectDocument(doc, scope))
	}

	// --- Phase B: unify ownership across the whole batch. ----------------
	// One string, one owner, decided by the precedence rule and not by which
	// document happens to be first. The overruled claims become warnings.
	overlaps.addOwnershipLosses(unifyOwnership(plans))

	// --- Phase C: apply, per document. -----------------------------------
	// Resolution here only decides which of two OVERLAPPING stretches of text
	// to replace; who owns a value was settled in phase B. Phase C reuses phase
	// A's spans, so no detection work is repeated.
	for _, plan := range plans {
		if err := ctx.Err(); err != nil {
			res.Report.Warnings = append(res.Report.Warnings,
				fmt.Sprintf("run cancelled after %d of %d documents", len(res.Documents), len(plans)))
			finishReport(res, start, overlaps)
			return res, err
		}
		rd, traces := applyPlan(plan, reg, overlaps, in.OnTrace != nil)
		if in.OnTrace != nil {
			in.OnTrace(plan.doc.Name, traces)
		}
		res.Documents = append(res.Documents, rd)
	}

	// --- Pass 3: registry post-pass across ALL documents. ---------------
	// Values first seen in document N are now in the registry; re-apply every
	// known mapping everywhere.
	entries := reg.Entries() // longest original first
	for i := range res.Documents {
		if err := ctx.Err(); err != nil {
			finishReport(res, start, overlaps)
			return res, err
		}
		emit(in.Progress, ProgressEvent{Stage: "post-pass", DocIndex: i, DocCount: len(res.Documents), DocName: res.Documents[i].Name})
		applyRegistryPostPass(&res.Documents[i], entries)
	}

	// Placeholders whose every occurrence matched the mainText value carry no
	// bracketed original, so their all-"" variant slices are dropped to keep the
	// per-document payload small.
	for i := range res.Documents {
		pruneMainTextOnlySpellings(&res.Documents[i])
	}

	// --- Report assembly. -------------------------------------------------
	// The per-VALUE tables are built from the registry and the finished text,
	// once, here. They used to be recomputed in JavaScript on every repaint of
	// the report card, and they were absent from the exported report entirely.
	entries = reg.Entries()
	for _, rd := range res.Documents {
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
		dr.Values = valueReports(entries, []ResultDocument{rd})
		res.Report.Documents = append(res.Report.Documents, dr)
	}
	res.Report.Values = valueReports(entries, res.Documents)
	res.Report.DetectedCategories = detectedCategoriesFromCounts(res.Report.ByCategory)
	finishReport(res, start, overlaps)
	return res, nil
}

// valueReports lists what each registry entry actually replaced in the given
// documents, most frequent first.
//
// The count is taken from the FINISHED text rather than from the registry's
// own counter, for two reasons: the registry counts per SESSION, so it cannot
// answer a per-document question, and the post-pass rewrites text after the
// counter was incremented. Counting
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

// pruneMainTextOnlySpellings drops every placeholder whose recorded
// occurrences were all the mainText value. Those need no bracketed original
// (the tooltip falls back to the mapping), so keeping their all-"" slices only
// grows the payload. A placeholder with even one non-mainText spelling is kept
// whole, "" slots included, so the frontend can line each occurrence up with
// the placeholder it renders.
func pruneMainTextOnlySpellings(rd *ResultDocument) {
	if rd.OccurrenceSpellings == nil {
		return
	}
	for ph, variants := range rd.OccurrenceSpellings {
		keep := false
		for _, v := range variants {
			if v != "" {
				keep = true
				break
			}
		}
		if !keep {
			delete(rd.OccurrenceSpellings, ph)
		}
	}
	if len(rd.OccurrenceSpellings) == 0 {
		rd.OccurrenceSpellings = nil
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

// filterValues keeps the Values whose category is active in the current
// selection.
func filterValues(values []Value, active map[string]bool) []Value {
	var out []Value
	for _, v := range values {
		if active[v.Category] {
			out = append(out, v)
		}
	}
	return out
}

// detectionScope is the fixed configuration passes 1 and 2 share for one run:
// what to look for, how sure a detection has to be, and what is off limits. It
// is a struct because seven positional parameters threaded through four call
// sites is a shape where a swapped pair compiles and silently changes what gets
// replaced.
type detectionScope struct {
	values        []Value
	patterns      []CustomPattern
	categories    CategorySelection
	minConfidence float32
	country       string
	allow         *Allowlist
	// suppressRegexPII, when true, skips pass 1 (the built-in pattern detectors)
	// for this run, because Built-in patterns is switched off. The Value and
	// custom-pattern passes are unaffected.
	suppressRegexPII bool
}

// ownershipLoss is one claim unification overruled: a route that wanted a
// string filed under its own category, and the route that got it instead.
type ownershipLoss struct {
	loser  Span
	winner Span
}

// unifyOwnership decides, ONCE for the whole batch, which route owns each
// string, and rewrites every other claim on that string to the winner.
//
// This is the structural half of the precedence rule. Without it, resolution
// happens per region, so the same string can be won by a native regex in one
// document and by the local model in another; Registry.Assign's byOriginal index
// then gives the string to whichever claim was ASSIGNED first, and assignment
// order is byte offset within document order. Which route owned a value
// therefore depended on the order the files were imported in, which is what
// made the behaviour look random. Assign cannot fix this itself: changing an
// entry's category after the fact would change its placeholder text, and a
// placeholder that has left the machine can never be re-numbered.
//
// The winner is picked by MatchClassRank first, then by the same tie-breaks
// resolution uses. Start offset is deliberately NOT among them: comparing
// offsets across documents would reintroduce exactly the file-order dependence
// this function exists to remove, so the last tie-break is the category name.
//
// @param plans every document's detections; regions are rewritten in place
// @return the overruled claims, so the run can warn about them
func unifyOwnership(plans []documentPlan) []ownershipLoss {
	// Which (category, mainText, matchClass) owns each string, keyed by the
	// lower-cased registry key the span would use.
	winners := map[string]Span{}
	for _, plan := range plans {
		for _, region := range plan.regions {
			for _, s := range region.spans {
				key := strings.ToLower(s.MainTextOrOriginal())
				cur, seen := winners[key]
				if !seen || supersedesForOwnership(s, cur) {
					winners[key] = s
				}
			}
		}
	}

	var losses []ownershipLoss
	// Report each losing (category, value) pair once: a name that appears two
	// hundred times must not produce two hundred identical warnings.
	reported := map[string]bool{}
	for pi := range plans {
		for ri := range plans[pi].regions {
			spans := plans[pi].regions[ri].spans
			// Rewriting collapses claims onto one owner, so two routes that
			// both matched the same characters become the same span. Keeping
			// both would make the region's resolution discard one as an
			// "overlap" and warn a second time about the event the ownership
			// warning already explains, so identical claims are compacted here.
			// Identity is what decides the replacement: the stretch of text,
			// the category it is filed under and the registry key. Confidence
			// and the matched spelling can differ between two claims that
			// produce byte-for-byte the same output.
			type claim struct {
				start, end int
				category   string
				key        string
			}
			seen := make(map[claim]bool, len(spans))
			kept := spans[:0]
			for _, s := range spans {
				key := strings.ToLower(s.MainTextOrOriginal())
				win := winners[key]
				if s.Category != win.Category || s.MatchClass != win.MatchClass {
					lossKey := s.MatchClass + "|" + s.Category + "|" + key
					if !reported[lossKey] {
						reported[lossKey] = true
						losses = append(losses, ownershipLoss{loser: s, winner: win})
					}
					// The claim becomes the winner's, so the registry sees one
					// owner for this string wherever it occurs. Start, End and
					// Original stay as they are: they describe THIS occurrence,
					// and the text being replaced has not changed.
					s.Category = win.Category
					s.MatchClass = win.MatchClass
					s.MainText = win.MainTextOrOriginal()
				}
				id := claim{s.Start, s.End, s.Category, strings.ToLower(s.MainTextOrOriginal())}
				if seen[id] {
					continue
				}
				seen[id] = true
				kept = append(kept, s)
			}
			plans[pi].regions[ri].spans = kept
		}
	}
	return losses
}

// supersedesForOwnership reports whether a should own a string instead of b.
// Same order as resolveOverlaps minus the length and start comparators: every
// span in a group covers the same value, so length cannot separate them, and
// start would make the answer depend on file order.
func supersedesForOwnership(a, b Span) bool {
	ra, rb := MatchClassRank(a.MatchClass), MatchClassRank(b.MatchClass)
	if ra != rb {
		return ra < rb
	}
	ca, cb := effectiveConfidence(a), effectiveConfidence(b)
	if ca != cb {
		return ca > cb
	}
	return a.Category < b.Category
}

// planRegion is one piece of text a document is anonymised in, with the spans
// detected in it. Regions mirror the document's own shape: one for a markdown
// body, one per grid cell, one for a complex sheet's JSON blob. Grid
// coordinates are carried so the apply phase can rebuild the grid; they are -1
// for the two single-region shapes.
type planRegion struct {
	row, col int
	// region is the trace tag ("" for a body, "row R col C", "json").
	region string
	text   string
	spans  []Span
}

// documentPlan is one document's detections, held between the detect phase and
// the apply phase.
//
// The two phases are separate so ownership can be decided across the WHOLE
// batch before a single placeholder is minted. Detecting and replacing in one
// step meant resolution happened per region, so two occurrences of the same
// string in different documents could be won by different routes, and
// Registry.Assign's byOriginal index then froze whichever claim was assigned
// first, which is byte-offset order within document order. Which route owned a
// value therefore depended on the order the files were imported in.
type documentPlan struct {
	doc     Document
	regions []planRegion
}

// detectDocument runs passes 1 and 2 (with the already-merged pass-3 values)
// over one document and returns what it found, replacing nothing. Grid
// documents are detected cell by cell, exactly as they are replaced, so the
// preview and the CSV round-trip cannot disagree.
func detectDocument(doc Document, scope detectionScope) documentPlan {
	plan := documentPlan{doc: doc}

	if doc.Grid != nil {
		for r, row := range doc.Grid {
			for c, cell := range row {
				plan.regions = append(plan.regions, planRegion{
					row: r, col: c,
					region: fmt.Sprintf("row %d col %d", r, c),
					text:   cell,
					spans:  detectText(cell, scope),
				})
			}
		}
		return plan
	}

	if doc.Format == FormatXLSXJSON {
		plan.regions = append(plan.regions, planRegion{
			row: -1, col: -1, region: "json",
			text: doc.JSON, spans: detectText(doc.JSON, scope),
		})
		return plan
	}

	plan.regions = append(plan.regions, planRegion{
		row: -1, col: -1, region: "",
		text: doc.Markdown, spans: detectText(doc.Markdown, scope),
	})
	return plan
}

// applyPlan replaces the planned spans, region by region, and assembles the
// document's result.
//
// @param overlaps collects the spans overlap resolution discarded, so the run
//
//	warns about them from the ONE place the decision is made. nil skips it.
//
// @param traceEnabled collects the resolved spans for the caller's OnTrace hook
func applyPlan(plan documentPlan, reg *Registry,
	overlaps *overlapWarnings, traceEnabled bool,
) (ResultDocument, []SpanTrace) {
	doc := plan.doc
	rd := ResultDocument{
		Name:                doc.Name,
		Format:              doc.Format,
		ByCategory:          map[string]int{},
		Warnings:            doc.Warnings,
		OccurrenceSpellings: map[string][]string{},
	}

	assign := func(s Span) string {
		rd.ByCategory[s.Category]++
		mainText := s.MainTextOrOriginal()
		ph := reg.Assign(s.Category, mainText)
		// Record the spelling this occurrence actually matched. "" when it was
		// the mainText value, so the tooltip needs no bracketed original; the
		// matched text otherwise ("Borch" for mainText "Johannes Borch"). The
		// closure is ApplySpans' single choke point and it is called in
		// left-to-right offset order, which is exactly the order the frontend
		// walks placeholders in, so slot i lines up with occurrence i.
		variant := ""
		if !strings.EqualFold(s.Original, mainText) {
			variant = s.Original
		}
		rd.OccurrenceSpellings[ph] = append(rd.OccurrenceSpellings[ph], variant)
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
		// Grid documents: replace cell by cell, then re-render the markdown
		// from the anonymised grid so preview and export agree.
		grid := make([][]string, len(doc.Grid))
		for r, row := range doc.Grid {
			grid[r] = make([]string, len(row))
			copy(grid[r], row)
		}
		for _, reg := range plan.regions {
			traceRegion := ""
			if traceEnabled {
				traceRegion = reg.region
			}
			grid[reg.row][reg.col] = applySpansToText(
				reg.text, reg.spans, assign, makeTraceFn(traceRegion), overlaps)
		}
		rd.Grid = grid
		rd.Anonymised = GridToMarkdownTable(grid)
		return rd, traces
	}

	// The two single-region shapes. A plan always carries exactly one region
	// for them, built by detectDocument.
	only := plan.regions[0]
	text := applySpansToText(only.text, only.spans, assign, makeTraceFn(only.region), overlaps)

	if doc.Format == FormatXLSXJSON {
		// Complex sheets: keep both the JSON (for the .json export) and the
		// fenced markdown rendering in sync.
		rd.JSON = text
		rd.Anonymised = "```json\n" + rd.JSON + "\n```\n"
		return rd, traces
	}

	rd.Anonymised = text
	return rd, traces
}

// detectText is the shared passes-1-and-2 core over one piece of text. It
// DETECTS only: nothing is resolved and nothing is replaced, because which
// route owns a value is decided over the whole batch (unifyOwnership) rather
// than per region.
//
// The category selection gates BOTH the PII categories (pass 1) and the
// custom-pattern pass; value categories were already filtered by the caller
// (filterValues). When scope.suppressRegexPII is set (the "Native detection"
// master switch is off) pass 1 is skipped entirely, so no signal category is
// replaced; the value and custom-pattern passes still run.
func detectText(text string, scope detectionScope) []Span {
	var spans []Span
	if !scope.suppressRegexPII {
		spans = FilterAllowed(DetectPIISelected(text, scope.categories, scope.country), scope.allow)
	}
	spans = append(spans, DetectValues(text, scope.values, scope.allow)...)
	if scope.categories[CatCustomPatterns] {
		spans = append(spans, DetectCustomPatterns(text, scope.patterns, scope.allow)...)
	}
	// The confidence floor is applied BEFORE overlap resolution, so a discarded
	// low-confidence span cannot suppress a stronger one it happens to overlap.
	// At the default 0 this is a no-op.
	return FilterByMinConfidence(spans, scope.minConfidence)
}

// applySpansToText resolves the overlaps among one region's spans and replaces
// what survives. By the time it runs, ownership is already settled across the
// batch, so resolution here only decides which of two OVERLAPPING stretches of
// text to replace, never which route owns a value.
func applySpansToText(text string, spans []Span, assign func(Span) string,
	traceFn func([]Span), overlaps *overlapWarnings,
) string {
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
// word-boundary anchoring (same unicode-aware rule as the Value pass) and
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
// inside an existing placeholder. The span's MainText is the registry
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
				Start:    m[0],
				End:      m[1],
				Category: e.Category,
				Original: text[m[0]:m[1]],
				MainText: e.Original,
				// A registry entry is ownership that is already DECIDED: the
				// string earned this placeholder in an earlier pass or an
				// earlier document, and a placeholder that has left the machine
				// can never be re-numbered. So it outranks a fresh detection
				// rather than being re-litigated by one.
				MatchClass: MatchClassBuiltInPattern,
			})
		}
	}
	return spans
}

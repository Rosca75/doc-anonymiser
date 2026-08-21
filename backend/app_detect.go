// app_detect.go — ONE bound method for the whole detection run, returning ONE
// list of Suggestions.
//
// Two things are unified here, and both were sources of real failures.
//
// The RUN is one call. One context in one cancellation slot, one monotonic
// progress stream, and exactly one terminal event ("detection:done" or
// "detection:error") whatever happens, including a panic. Two sequential bridge
// calls meant two cancellation slots with a window where Cancel was a silent
// no-op, a progress bar that rewound when the second pass started over with a
// different denominator, and no terminal event at all, so only a `finally` in
// the caller could ever clear the bar.
//
// The RESULT is one list. Every discovery method reports Suggestions into the
// same merged list, whichever route it belongs to, because the user is reviewing
// potential Values rather than a detector's output. Two lists, one per route,
// forced the frontend to map each into its own state, and the mapping for the
// Local AI route silently discarded the folded spellings, so a family the engine
// had already collapsed arrived as a bare string.
package backend

import (
	"context"
	"fmt"
	"strings"
	"time"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/ollama"
)

// Detection phases name one route in the run. These are engine tokens, not
// labels: the frontend maps them to the words the user reads.
const (
	PhaseSmart   = "smart"
	PhaseLocalAI = "local_ai"
)

// DetectionProgress is the payload of the "detection:progress" event.
//
// Fraction is computed HERE, in Go, and is guaranteed to be non-decreasing
// across the whole run. The frontend used to derive a percentage from
// (docIndex+1)/docCount per pass, which is why the bar visibly rewound when
// the second pass started over with a different denominator. A progress bar
// that goes backwards is worse than none: it reads as a run that restarted.
type DetectionProgress struct {
	Phase      string `json:"phase"`
	PhaseIndex int    `json:"phaseIndex"` // 0-based
	PhaseCount int    `json:"phaseCount"`
	DocIndex   int    `json:"docIndex"` // 0-based, within this phase
	DocCount   int    `json:"docCount"`
	DocName    string `json:"docName"`
	// ChunkIndex/ChunkCount are the position INSIDE one document's AI scan:
	// which request is being sent, and how many the scan needs. Zero when the
	// phase sends no requests. Without them a single large file sits on one
	// unchanging caption for minutes and reads as hung.
	ChunkIndex int `json:"chunkIndex"`
	ChunkCount int `json:"chunkCount"`
	// UnitFrom/UnitTo are the document's OWN unit numbers this request covers,
	// and UnitWord is the singular word for them ("slide", "page", "row",
	// "line"). Together they let the caption say "slides 4 to 6 of 15", in the
	// words the import list already uses; a request number alone means nothing
	// to the person watching. Zero and empty when the phase sends no requests.
	UnitFrom int    `json:"unitFrom"`
	UnitTo   int    `json:"unitTo"`
	UnitWord string `json:"unitWord"`
	// Fraction is the whole run's progress, 0 to 1.
	Fraction float64 `json:"fraction"`
}

// DetectionSkip records a file a phase deliberately did not read.
type DetectionSkip struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// DetectionResult is the outcome of one detection run, and the payload of
// the "detection:done" event.
//
// Errors is a LIST rather than a returned error: one file that the model choked
// on must not throw away the Suggestions found in the other nine. The caller
// shows them as warnings.
type DetectionResult struct {
	// Suggestions is every unreviewed potential Value the run found, from every
	// method, merged through engine.MergeSuggestions. A Suggestion says which
	// methods found it on itself (DiscoveryMethods), so route membership is a
	// property of the row rather than of which list it landed in.
	Suggestions []engine.Suggestion `json:"suggestions"`
	// Phases lists the routes that actually ran, so the UI can say "smart
	// detection only" without re-deriving it from the settings.
	Phases    []string        `json:"phases"`
	Skipped   []DetectionSkip `json:"skipped"`
	Errors    []string        `json:"errors"`
	Cancelled bool            `json:"cancelled"`
	Status    string          `json:"status"`

	// AIRequests and AISilentRequests are how many requests the Local AI route
	// sent and how many of those came back with nothing. They exist because
	// "0 suggestions" means two different things, and only one of them is about
	// the document: a model that answered nothing fifteen times reads exactly
	// like a document with no names in it, and the user gets no hint that
	// another model or a smaller slice would change the answer.
	AIRequests       int `json:"aiRequests"`
	AISilentRequests int `json:"aiSilentRequests"`
	// AITruncatedRequests is how many requests the model was still answering
	// when it hit its generation cap. It is reported beside the silent count
	// and never folded into it, because the two say opposite things about the
	// document: a silent request found nothing, a truncated one found more than
	// it was allowed to finish listing. What it did finish is kept, so a
	// truncated request usually still contributes values; the number is what
	// tells the user some may be missing from those pages.
	AITruncatedRequests int `json:"aiTruncatedRequests"`
	// AISecondsPerRequest is MEASURED, not estimated: the AI phase's wall clock
	// divided by its requests. It is what lets a user judge the speed of a scan
	// on their OWN document and their own machine, which no fixed guidance in a
	// tooltip can do. Zero when the route did not run.
	AISecondsPerRequest float64 `json:"aiSecondsPerRequest"`

	// PatternMatches and PatternCategories are built-in pattern matching's
	// READ-ONLY preview: what the switched-on signal categories claim in the
	// batch as it stands, and which of those categories actually ran.
	//
	// They are not Suggestions and must never become any: a built-in pattern
	// produces DIRECT matches, which pass 1 applies without review because the
	// pattern is a rule the user chose. What the user does decide is which
	// categories are on, and until this preview existed there was no way to
	// check that decision short of anonymising the whole batch and reading the
	// result. So the run reports the matches and the Identify step SHOWS them.
	//
	// PatternCategories is reported beside the matches because "found nothing"
	// and "never ran" are different facts and only the second is actionable: a
	// category switched off, or outside the document country, is absent from
	// this list and its section says so.
	PatternMatches    []engine.PatternMatch `json:"patternMatches"`
	PatternCategories []string              `json:"patternCategories"`
	// BuiltInPatternsOn is the master switch as it stood when the run started.
	// Off means every signal category is silent whatever the category switches
	// say, which is a different sentence for the user to read than "none of the
	// categories you chose applies here".
	BuiltInPatternsOn bool `json:"builtInPatternsOn"`
}

// AIScope narrows the local-AI route to one document and, within it, an
// arbitrary SET of its own sub-units (pages/slides/rows/lines — see
// engine.Document.PageCount).
//
// It exists because handing a whole document to a small local model is "too
// much" (the reported problem): the scan stalls or the context window
// overflows. Scoping is deliberately LOCAL-AI ONLY — the offline Smart route is
// cheap and reads everything — so it lives here rather than in Settings, and it
// is a per-run choice that is never persisted to a session.
//
// Pages is a 1-based set already parsed, sorted and de-duplicated by the
// frontend (state.js parsePageSpec) from the user's free-text spec, so it can
// express a single page, a contiguous range, or a discontiguous mix
// ("12,13,18-20"). An EMPTY Pages means "the whole selected document"; a nil
// *AIScope, or one with an empty DocName, means "every document, whole" — the
// unchanged behaviour.
type AIScope struct {
	DocName string `json:"docName"` // "" = every document
	Pages   []int  `json:"pages"`   // 1-based set; empty = the whole selected document
}

// active reports whether this scope actually narrows anything.
func (s *AIScope) active() bool {
	return s != nil && s.DocName != ""
}

// RunDetection runs every enabled detection route over the named files, in
// order, under ONE cancellation context.
//
// Which routes run is decided HERE, from the stored settings, not by the caller.
// The Local AI route additionally requires Ollama to actually answer: probing
// rather than trusting the stored flag is what stops a stale "on" from starting
// a model that is not running. A frontend that asks for a route the user
// switched off does not get it.
//
// aiScope narrows the LOCAL-AI route only (Smart detection always reads every
// file); nil leaves the AI route reading every document whole.
//
// It always emits exactly one terminal event before returning.
func (a *App) RunDetection(fileNames []string, allowTerms []string, aiScope *AIScope) (*DetectionResult, error) {
	docs := a.docsByName(fileNames)
	if len(docs) == 0 {
		return nil, fmt.Errorf(
			"no matching imported files to scan: import documents on the Import step first, then run detection")
	}

	a.mu.Lock()
	settings := a.settings
	llm := a.llm
	if a.cancelDetection != nil {
		a.mu.Unlock()
		return nil, fmt.Errorf("a detection run is already in progress, cancel it or wait for it to finish")
	}
	ctx, cancel := context.WithCancel(context.Background())
	// ONE slot for the WHOLE run, both phases: the gap between two separately
	// cancellable calls is what made Cancel a no-op between the passes.
	a.cancelDetection = cancel
	a.mu.Unlock()
	defer func() {
		cancel()
		a.mu.Lock()
		a.cancelDetection = nil
		a.mu.Unlock()
	}()

	// Read what the documents DEFINE about themselves before the allowlist is
	// built, so the suppressor is in force for this run rather than the next one.
	// A defined term is the document's own statement that a phrase is part of its
	// machinery, and the largest single class of false positives in the review
	// list is exactly those phrases.
	a.rememberDefinedTerms(docs)

	// The shared builder: the detection routes must not
	// re-propose a value the user removed, or a removal reads as undone the
	// moment detection runs again.
	allow := a.allowlistFor(allowTerms)
	llm.Allow = allow.Contains

	// The AI route needs the switch, a reachable Ollama, and something to
	// read. Probing here rather than trusting the stored flag is what stops a
	// stale "on" from starting a model that is not running.
	useLocalAI := settings.UseLocalAI && llm.Probe().Available
	phases := []string{}
	// The Smart PHASE is Smart detection's two DISCOVERY methods: heuristic
	// discovery and signal-based discovery. Built-in pattern matching is not a
	// discovery method and therefore not a phase: it produces direct matches at
	// anonymisation time, so its switch does not appear here.
	if settings.UseHeuristicDiscovery || a.signalDiscoveryOn(settings) {
		phases = append(phases, PhaseSmart)
	}
	if useLocalAI {
		phases = append(phases, PhaseLocalAI)
	}

	res := &DetectionResult{Phases: phases, Suggestions: []engine.Suggestion{}}

	// The built-in pattern preview, taken from the settings as they stand NOW.
	// It runs before the phases and outside them, because it is not a phase: it
	// is a read of a deterministic rule set, it emits no Suggestion, and it must
	// be available even when every discovery route is off. A user who ticks
	// "street addresses" and presses Run detection is asking exactly this
	// question, and answering it only when some OTHER route happens to be on
	// would make the answer depend on an unrelated switch.
	a.previewBuiltInPatterns(docs, settings, allow, res)

	if len(phases) == 0 {
		// With the patterns on, the run still did something the user can see, so
		// it is not the dead end the message below describes.
		if res.BuiltInPatternsOn {
			res.Status = builtInOnlyStatus(res)
		} else {
			res.Status = "no detection route is switched on, turn on Smart detection or Local AI in Configure"
		}
		a.emit("detection:done", res)
		return res, nil
	}

	// A panic in a pass must not leave the UI with a spinning bar and no
	// event: the terminal event is the contract, so it is deferred.
	emitted := false
	defer func() {
		if !emitted {
			a.emit("detection:error", map[string]string{
				"message": "the detection run stopped unexpectedly; the application is still usable, please try again",
			})
		}
	}()

	for phaseIndex, phase := range phases {
		report := func(p DetectionProgress) {
			p.Phase = phase
			p.PhaseIndex = phaseIndex
			p.PhaseCount = len(phases)
			p.Fraction = overallFraction(phaseIndex, len(phases), p.DocIndex, p.DocCount, p.ChunkIndex, p.ChunkCount)
			a.emit("detection:progress", p)
		}
		if phase == PhaseSmart {
			a.runSmartPhase(ctx, docs, allow, settings, res, report)
		} else {
			a.runLocalAIPhase(ctx, docs, llm, settings.AIDetailLevel, aiScope, res, report)
		}
		if ctx.Err() != nil {
			break
		}
	}

	// Fold Value families over the unified list, BEFORE anything else looks at
	// it. Per method would leave a heuristic "Coca-Cola" and a model "Coca-Cola
	// company" unfolded, which is exactly the pair that has to become one Value:
	// left apart, the shorter one fires inside the longer one, the text reads
	// "[BRAND_1] company", and two numbers are spent on one company.
	//
	// Folding first also means the classification call below asks about the
	// list every other part of the system works with, instead of paying the
	// model to categorise "Borch" and "Johannes Borch" as two separate names.
	res.Suggestions = engine.FoldValueFamilies(res.Suggestions, allow)

	// With the Local AI route on, the model also RE-FILES what Smart detection
	// found (only main texts and short context snippets travel, never documents).
	// A classification failure degrades to the heuristic categories with a note:
	// the findings are the point, the labels are polish.
	if useLocalAI && ctx.Err() == nil && len(res.Suggestions) > 0 {
		if err := a.refineCategories(ctx, llm, docs, settings.AIDetailLevel, aiScope, res); err != nil && ctx.Err() == nil {
			res.Errors = append(res.Errors,
				fmt.Sprintf("the local AI could not refine the categories, the offline guesses were kept: %v", err))
		}
		// Re-fold, because a refined CATEGORY can create a family that did not
		// exist a moment ago: folding only ever happens within one category, so
		// a person "Delta" and an organisation "Delta Industries" are two rows
		// until the model files both under organisations, and from that instant
		// they are one Value with two spellings. Re-folding an already-folded
		// list is a no-op, which is why this is cheap rather than a second rule.
		res.Suggestions = engine.FoldValueFamilies(res.Suggestions, allow)
	}

	res.Cancelled = ctx.Err() != nil
	res.Status = detectionStatus(res, len(docs))
	emitted = true
	a.emit("detection:done", res)
	return res, nil
}

// previewBuiltInPatterns fills the result's read-only built-in pattern preview.
//
// It reuses the run's OWN allowlist, so a session exclusion and a defined term
// suppress a previewed match exactly as they suppress a replaced one: a preview
// that showed a value the user had removed would read as the removal having been
// undone. The category selection and the confidence floor come from the stored
// settings for the same reason the pipeline takes them from there.
//
// A nil or empty selection means "use the preset", which is the pipeline's own
// reading of the field (engine.Run): the preview must not report a different set
// of categories from the one a run would use.
func (a *App) previewBuiltInPatterns(docs []engine.Document, settings Settings,
	allow *engine.Allowlist, res *DetectionResult,
) {
	res.PatternMatches = []engine.PatternMatch{}
	res.PatternCategories = []string{}
	res.BuiltInPatternsOn = settings.UseBuiltInPatterns
	if !settings.UseBuiltInPatterns {
		return
	}
	sel := settings.Categories
	if len(sel) == 0 {
		sel = engine.PresetSelection(engine.Level(settings.Level))
	}
	res.PatternCategories = engine.ActivePatternCategories(sel, settings.Country)
	if len(res.PatternCategories) == 0 {
		return
	}
	res.PatternMatches = engine.PreviewPatternMatches(
		docs, sel, settings.Country, settings.MinConfidence, allow)
}

// builtInOnlyStatus is the status line for a run in which built-in pattern
// matching was the only thing switched on. It exists so that run reads as a run
// that did something, which it did: the matches are on the Built-in patterns
// tab.
func builtInOnlyStatus(res *DetectionResult) string {
	if len(res.PatternCategories) == 0 {
		return "built-in pattern matching is on but none of the selected categories applies to the document country, " +
			"choose categories in Configure or turn on Smart detection or Local AI"
	}
	if len(res.PatternMatches) == 0 {
		return "no discovery route is switched on, and the built-in patterns matched nothing in these files"
	}
	return fmt.Sprintf(
		"no discovery route is switched on; the built-in patterns matched %d value(s), see the Built-in patterns tab",
		len(res.PatternMatches))
}

// rememberDefinedTerms reads every document's own vocabulary and stores it, so
// allowlistFor can veto it and the never-anonymise list can SHOW it.
//
// It replaces the stored list rather than adding to it: the list describes the
// documents currently imported, and a term from a document that has since been
// removed would go on suppressing a value nothing defines any more.
func (a *App) rememberDefinedTerms(docs []engine.Document) {
	var found []engine.DefinedTerm
	for _, doc := range docs {
		found = append(found, engine.DiscoverDefinedTerms(doc.Name, doc.Markdown)...)
	}
	a.mu.Lock()
	a.definedTerms = found
	a.mu.Unlock()
}

// DefinedTerms is the bound method the never-anonymise list reads: the terms the
// imported documents declare about themselves, with the idiom that introduced
// each one.
//
// They are shown rather than applied in silence, and each carries a delete, for
// the reason a session exclusion is visible and reversible: a negative rule the
// user cannot see is a rule they cannot undo.
func (a *App) DefinedTerms() []engine.DefinedTerm {
	return a.definedTermsSnapshot()
}

// ForgetDefinedTerm drops ONE suppressed defined term, so the value it was
// hiding can be suggested again. It returns the list that remains.
//
// Matching is case-insensitive on the term, which is how the allowlist matches
// it, so what the user clicks is what stops being suppressed.
func (a *App) ForgetDefinedTerm(term string) []engine.DefinedTerm {
	want := strings.ToLower(strings.TrimSpace(term))
	a.mu.Lock()
	kept := make([]engine.DefinedTerm, 0, len(a.definedTerms))
	for _, t := range a.definedTerms {
		if strings.ToLower(strings.TrimSpace(t.Term)) == want {
			continue
		}
		kept = append(kept, t)
	}
	a.definedTerms = kept
	a.mu.Unlock()
	return a.definedTermsSnapshot()
}

// CancelDetection aborts the in-flight detection run. A no-op when idle.
//
// One cancellation slot serves every route, so this reaches whichever one is
// running, including mid-file: the offline scanner and the chunked model calls
// both take the same context.
func (a *App) CancelDetection() {
	a.mu.Lock()
	cancel := a.cancelDetection
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// refineCategories asks the local model to categorise what Smart detection
// found, and applies whatever it recognised.
//
// Only Smart detection's own Suggestions are sent. Re-filing the model's own
// findings would be asking it to grade its own work, and it would let one pass
// overwrite a category the same model had just chosen.
//
// It obeys the SAME scope as the discovery call, through the same helper. This
// is the larger of the two model calls, so a scope that narrowed only the other
// one left the user's "just page 1" reading the whole batch: about half a scoped
// run's prompt tokens, spent on text the user had explicitly excluded. A
// suggestion the scoped text does not contain simply keeps its offline
// category, which is the same graceful outcome a classification failure already
// produces.
func (a *App) refineCategories(ctx context.Context, llm *ollama.Client,
	docs []engine.Document, level string, scope *AIScope, res *DetectionResult,
) error {
	// The problems are discarded here on purpose: runLocalAIPhase has already
	// run with this same scope and reported them, and saying "page 9 is out of
	// bounds" twice for one run reads as two separate faults.
	var scoped string
	if scope.active() {
		units, _ := scopedUnits(docs, scope)
		texts := make([]string, 0, len(units))
		for _, u := range units {
			// The scoped text is built from the SAME slices the discovery call
			// sent, not re-derived: two implementations of "what the scope
			// selects" agree right up until one of them is changed.
			slices, err := u.slices(level, llm.PromptBudgetBytes())
			if err != nil {
				continue
			}
			texts = append(texts, sliceText(slices))
		}
		scoped = strings.Join(texts, "\n")
	}

	var smart []engine.Suggestion
	for _, s := range res.Suggestions {
		if !smartDiscovered(s) {
			continue
		}
		if scope.active() && !occursInScope(scoped, s) {
			continue
		}
		smart = append(smart, s)
	}
	if len(smart) == 0 {
		return nil
	}
	refiled, err := llm.ClassifySuggestions(ctx, smart)
	if err != nil {
		return err
	}
	refined := map[string]string{}
	for _, s := range refiled {
		refined[s.MainText] = s.Category
	}
	for i := range res.Suggestions {
		if !smartDiscovered(res.Suggestions[i]) {
			continue
		}
		if category, ok := refined[res.Suggestions[i].MainText]; ok {
			res.Suggestions[i].Category = category
		}
	}
	return nil
}

// smartDiscovered reports whether a Suggestion came from one of Smart
// detection's discovery methods.
func smartDiscovered(s engine.Suggestion) bool {
	for _, m := range s.DiscoveryMethods {
		if m == engine.MethodHeuristic || m == engine.MethodSignal {
			return true
		}
	}
	return false
}

// occursInScope reports whether a Suggestion is about text the scope selected.
//
// Any of its spellings counts, not only its main text: a folded family is ONE
// row, so a page carrying "Johannes Borch" is asking about the row whose main
// text is "Borch" even though the shorter form was folded from elsewhere.
// Matching is exact, like the hallucination filter, because both discovery and
// signal-based discovery preserve the document's own casing.
func occursInScope(scoped string, s engine.Suggestion) bool {
	if strings.Contains(scoped, s.MainText) {
		return true
	}
	for _, spelling := range s.Spellings {
		if strings.Contains(scoped, spelling) {
			return true
		}
	}
	return false
}

// signalDiscoveryOn reports whether any built-in signal may derive Suggestions,
// which is what makes signal-based discovery worth running as a phase.
func (a *App) signalDiscoveryOn(s Settings) bool {
	for _, source := range engine.AllSignalSources {
		if engine.SignalSourceEnabled(s.SignalSuggestionSources, source) {
			return true
		}
	}
	return false
}

// runSmartPhase is Smart detection's discovery half: heuristic discovery over
// every document, plus signal-based discovery over the whole batch.
//
// Every document is scanned; one cancelled mid-scan contributes what it found
// before stopping. Both methods report into the SAME merged list, so a name both
// of them find is one row carrying both methods rather than two rows the user has
// to notice are the same.
func (a *App) runSmartPhase(ctx context.Context, docs []engine.Document, allow *engine.Allowlist,
	settings Settings, res *DetectionResult, report func(DetectionProgress),
) {
	var batches [][]engine.Suggestion
	// Partial work is kept whatever ends the loop: a cancellation returning
	// straight out used to throw away everything the earlier files produced.
	defer func() { res.Suggestions = mergeInto(res.Suggestions, batches) }()

	if settings.UseHeuristicDiscovery {
		for i, doc := range docs {
			if ctx.Err() != nil {
				return
			}
			report(DetectionProgress{DocIndex: i, DocCount: len(docs), DocName: doc.Name})

			found, err := engine.HeuristicDiscoverContext(
				ctx, doc.Markdown, allow, settings.HeuristicDiscovery, settings.Country)
			if err != nil && ctx.Err() == nil {
				// Not a cancellation: record it and carry on with the next file.
				res.Errors = append(res.Errors,
					fmt.Sprintf("heuristic discovery failed on %q: %v", doc.Name, err))
				continue
			}
			stamped := make([]engine.Suggestion, 0, len(found))
			for _, one := range found {
				stamped = append(stamped, one.WithMethod(engine.MethodHeuristic))
			}
			batches = append(batches, stamped)
		}
	}

	if ctx.Err() != nil {
		return
	}
	// Signal-based discovery reads the WHOLE BATCH at once rather than one
	// document at a time: the evidence is an email in one file and the text it
	// points at is usually in another, so per document it would find almost
	// nothing.
	batches = append(batches, engine.DiscoverFromSignals(engine.SignalDiscoveryInput{
		Documents: docs,
		Sources:   settings.SignalSuggestionSources,
		Country:   settings.Country,
		Allow:     allow,
	}))
}

// mergeInto folds fresh batches into the run's Suggestion list through the
// engine's single merge rule. Both phases call it, so neither can invent its own
// idea of what a duplicate is.
func mergeInto(existing []engine.Suggestion, batches [][]engine.Suggestion) []engine.Suggestion {
	return engine.MergeSuggestions(append([][]engine.Suggestion{existing}, batches...)...)
}

// scanUnit pairs a document with the indices of its OWN units the local AI
// should read for it: every unit normally, or the selected pages when scoped.
//
// It carries indices rather than joined text because the text of one request is
// not the text of one document: the engine packs the units into request-sized
// slices (engine.ScanChunks), and it can only do that if it still knows where
// the unit boundaries are. Joined markdown has thrown that away.
type scanUnit struct {
	doc engine.Document
	// units is a 1-based set of doc's own units; empty means every unit, which
	// is also what engine.ScanChunks reads an empty list as.
	units []int
}

// slices turns one scanUnit into the requests the local AI actually sends, at
// the given detail level and under the given per-request ceiling. It is the ONE
// place a scope becomes text, so the discovery call and the classification call
// cannot disagree about what the scope selected.
func (u scanUnit) slices(level string, hardMaxBytes int) ([]engine.ScanChunk, error) {
	return engine.ScanChunks(u.doc, u.units, level, hardMaxBytes)
}

// scopedUnits is THE ONE definition of "what this run's scope selects".
//
// Both model calls read it: the discovery call, which scans the text, and the
// classification call, which decides which suggestions the scope even makes
// this run's business. A second implementation of the same idea is exactly the
// bug this shape prevents, because the two would agree right up until one of
// them was changed.
//
// It answers in the document's own UNITS rather than in joined text, because the
// slicing that follows needs the boundaries: a scope is "these slides of this
// deck", and turning that into one string immediately loses where a slide ends.
//
// It returns the problems rather than recording them, so the caller decides
// whether this is the run's first look at the scope (report them) or its
// second (stay quiet). Neither problem is a failure: an out-of-range page and
// a scope naming a document that is not imported are both the user's request,
// reported so the run can finish cleanly.
func scopedUnits(docs []engine.Document, scope *AIScope) (units []scanUnit, problems []string) {
	scopeMatched := false
	for _, doc := range docs {
		if scope.active() {
			if doc.Name != scope.DocName {
				continue
			}
			scopeMatched = true
			// An empty page set means "the whole selected document", which is
			// also how an empty unit list reads downstream; a non-empty set
			// narrows to exactly those pages.
			if len(scope.Pages) == 0 {
				units = append(units, scanUnit{doc: doc})
				continue
			}
			// Validate the indices HERE, so a stale or mistyped page is
			// reported once, as this run's first look at the scope, rather
			// than surfacing later as a slicing failure.
			if _, err := doc.PagesMarkdown(scope.Pages); err != nil {
				problems = append(problems, err.Error())
				continue
			}
			units = append(units, scanUnit{doc: doc, units: scope.Pages})
			continue
		}
		units = append(units, scanUnit{doc: doc})
	}
	if scope.active() && !scopeMatched {
		problems = append(problems, fmt.Sprintf(
			"the local AI was scoped to %q, which is not among the imported documents; import it or clear the scope",
			scope.DocName))
	}
	return units, problems
}

// scanJob is one document's worth of local-AI work: the slices to send, the
// word for its own units, and the text the hallucination filter reads.
type scanJob struct {
	name   string
	unit   string
	slices []engine.ScanChunk
	source string
}

// aiScanPlan is what a scope and a detail level become before a single request
// is sent: the jobs to run, the documents with nothing to read, and the problems
// worth telling the user about.
type aiScanPlan struct {
	jobs     []scanJob
	skipped  []DetectionSkip
	problems []string
}

// requests is how many model requests the plan implies, which is the number the
// rail shows before the user pays it.
func (p aiScanPlan) requests() int {
	total := 0
	for _, job := range p.jobs {
		total += len(job.slices)
	}
	return total
}

// planAIScan is THE ONE place a scope and a detail level become requests.
//
// Both the run and the estimate go through it, and that is the whole reason it
// exists: a read-out predicting a different number from the run is worse than no
// read-out, and two implementations of the packing rule disagree the moment
// either one is changed.
//
// Slicing happens up front rather than per document as the loop reaches it, so
// the request count is known before the first request is sent and the warning
// about a long scan can precede the wait instead of following it.
func planAIScan(docs []engine.Document, llm *ollama.Client, level string, scope *AIScope) aiScanPlan {
	units, problems := scopedUnits(docs, scope)
	plan := aiScanPlan{problems: problems}

	for _, u := range units {
		slices, err := u.slices(level, llm.PromptBudgetBytes())
		if err != nil {
			plan.problems = append(plan.problems, err.Error())
			continue
		}
		if len(slices) == 0 {
			plan.skipped = append(plan.skipped, DetectionSkip{
				Name:   u.doc.Name,
				Reason: "no text for the local AI to read. Smart detection still read it.",
			})
			continue
		}
		if len(slices) > ollama.LargeScanRequests {
			plan.problems = append(plan.problems, fmt.Sprintf(
				"%q needs %d local AI requests and will take a while; cancel and scope the local AI to a page range if that is longer than you want to wait",
				u.doc.Name, len(slices)))
		}
		// The source text for the hallucination filter is the scanned text
		// itself, built from the SAME slices, so a name the model read in one
		// slice is not dropped for being absent from another.
		plan.jobs = append(plan.jobs, scanJob{
			name:   u.doc.Name,
			unit:   u.doc.Unit,
			slices: slices,
			source: sliceText(slices),
		})
	}
	return plan
}

// EstimateAIRequests reports how many model requests the current scope and
// detail level imply, so the rail can show the cost of a choice BEFORE the user
// pays it.
//
// It answers "if the Local AI route runs, this is what it would send": it does
// not probe Ollama and it does not care whether the route is switched on,
// because the question the rail asks is what the choice in front of the user
// would cost. It reaches no model and mutates nothing, so it is safe to call on
// every edit of the scope or the level.
//
// The count comes from planAIScan, the same helper the run itself uses. A second
// formula here would be a number that disagrees with reality as soon as either
// copy changes, and the user would have no way of telling which was lying.
//
// A problem deciding the slices (a page out of range, a scope naming a document
// that is not imported, a scan large enough to be worth warning about) is NOT an
// error here. The run reports those, and the run also sends exactly the requests
// this counts in spite of them, so failing would break the one property the
// number is for: the estimate equals what the run then does. Only having nothing
// to estimate is an error.
func (a *App) EstimateAIRequests(fileNames []string, aiScope *AIScope) (int, error) {
	docs := a.docsByName(fileNames)
	if len(docs) == 0 {
		return 0, fmt.Errorf(
			"no matching imported files to estimate: import documents on the Import step first")
	}

	a.mu.Lock()
	level := a.settings.AIDetailLevel
	llm := a.llm
	a.mu.Unlock()

	return planAIScan(docs, llm, level, aiScope).requests(), nil
}

// runLocalAIPhase is the Local AI route: one request per slice, with the slices
// aligned to each document's OWN units by the engine.
//
// A large document is scanned and WARNED about, never refused. The user asked for
// the scan, every request reports progress and the cancel button reaches
// mid-scan, so a slow scan is a cost they can watch and stop; dropping the
// document from the route leaves them with the offline findings and no idea why.
//
// The only document that is skipped is one with no scannable text at all, which
// is a genuine "there is nothing here to read" rather than a limit.
//
// When scope is active the route reads ONE document. If the scope names a set
// of pages it reads only those (CLAUDE.md §5); with no pages it reads the whole
// selected document. The Smart route is unaffected: it already read everything.
func (a *App) runLocalAIPhase(ctx context.Context, docs []engine.Document, llm *ollama.Client,
	level string, scope *AIScope, res *DetectionResult, report func(DetectionProgress),
) {
	plan := planAIScan(docs, llm, level, scope)
	res.Errors = append(res.Errors, plan.problems...)
	res.Skipped = append(res.Skipped, plan.skipped...)
	jobs := plan.jobs

	// Partial work is kept whatever ends the loop, which is why the merge is
	// deferred: a cancellation returning straight out would throw away
	// everything the files before it had produced.
	var batches [][]engine.Suggestion
	// The timing is measured around the requests and reported however the loop
	// ends, so a cancelled scan still says how long its requests took.
	started := time.Now()
	defer func() {
		res.Suggestions = mergeInto(res.Suggestions, batches)
		if res.AIRequests > 0 {
			res.AISecondsPerRequest = time.Since(started).Seconds() / float64(res.AIRequests)
		}
		// Every request answering nothing is the case that reads as a clean
		// document and is not one. It names the MODEL, because "your model found
		// nothing" is the actionable half, and it goes in Errors because the
		// interface already renders those as problems the run finished with. A
		// parallel notes channel would be a second thing to render and a second
		// thing to forget.
		if res.AIRequests > 0 && res.AISilentRequests == res.AIRequests {
			res.Errors = append(res.Errors, fmt.Sprintf(
				"the local AI model %q returned nothing for all %d request(s), so this run found no values through it; a larger model usually changes that",
				llm.Model, res.AIRequests))
		}
	}()

	for i, job := range jobs {
		if ctx.Err() != nil {
			return
		}
		report(DetectionProgress{DocIndex: i, DocCount: len(jobs), DocName: job.name})

		texts := make([]string, 0, len(job.slices))
		for _, s := range job.slices {
			texts = append(texts, s.Text)
		}
		outcome, err := llm.DiscoverSlices(ctx, texts, job.source, func(index, total int) {
			p := DetectionProgress{
				DocIndex: i, DocCount: len(jobs), DocName: job.name,
				ChunkIndex: index, ChunkCount: total,
				UnitWord: job.unit,
			}
			// The unit numbers of the slice about to be sent, so the caption can
			// say "slides 4 to 6" in the word the import list already uses.
			if index >= 0 && index < len(job.slices) {
				p.UnitFrom = job.slices[index].FromUnit
				p.UnitTo = job.slices[index].ToUnit
			}
			report(p)
		})
		// The counts accumulate across documents, and they accumulate even when
		// the file failed: a request that was sent was sent, and the run's
		// honesty about what the model did is what these numbers are for.
		res.AIRequests += outcome.Requests
		res.AISilentRequests += outcome.Silent
		res.AITruncatedRequests += outcome.Truncated
		// A cut-off reply is reported PER DOCUMENT rather than per request: the
		// user acts on it by scoping the scan, and a scope names a document and
		// its pages. One line per file says which file to aim at; one line per
		// request would say the same thing ten times.
		if outcome.Truncated > 0 {
			word := scanUnitWord(job.unit)
			res.Errors = append(res.Errors, fmt.Sprintf(
				"the local AI ran out of room on %d of %d request(s) for %q, so those %ss may be missing values; what the model had already listed was kept. Scan fewer %ss at a time, or try another model",
				outcome.Truncated, outcome.Requests, job.name, word, word))
		}
		// Partial per-slice proposals survive a mid-file cancellation.
		if len(outcome.Suggestions) > 0 {
			batches = append(batches, outcome.Suggestions)
		}
		if err != nil {
			if ctx.Err() != nil {
				return // cancelled: keep what we have
			}
			// One file the model choked on must not throw away the other
			// nine, so this is a recorded problem rather than a returned error.
			res.Errors = append(res.Errors,
				fmt.Sprintf("the local AI failed on %q: %v", job.name, err))
		}
	}
}

// scanUnitWord is the noun a message uses for one of a document's own units.
// A format that reports none leaves it empty, and "unit" is the same word the
// engine falls back to: a sentence with a hole where a noun should be reads as
// a bug, and the general word still tells the user what to scope.
func scanUnitWord(unit string) string {
	if unit == "" {
		return "unit"
	}
	return unit
}

// sliceText joins slices back into the text they cover, which is what the
// hallucination filter is checked against. It exists so the filter reads exactly
// what was sent: rebuilding the scanned text from the document instead would let
// a name outside the scope pass a filter the scope should have vetoed.
func sliceText(slices []engine.ScanChunk) string {
	texts := make([]string, 0, len(slices))
	for _, s := range slices {
		texts = append(texts, s.Text)
	}
	return strings.Join(texts, "\n")
}

// overallFraction maps a position inside one phase onto the whole run.
//
// Phases are sequential, and each phase's own fraction only ever grows, so
// the result is non-decreasing even when the second phase reads FEWER files
// than the first (oversized documents are skipped by the AI route).
func overallFraction(phaseIndex, phaseCount, docIndex, docCount, chunkIndex, chunkCount int) float64 {
	if phaseCount <= 0 {
		return 0
	}
	within := 0.0
	if docCount > 0 {
		perDoc := 1.0 / float64(docCount)
		within = float64(docIndex) * perDoc
		if chunkCount > 0 {
			within += perDoc * float64(chunkIndex) / float64(chunkCount)
		}
	}
	if within > 1 {
		within = 1
	}
	return (float64(phaseIndex) + within) / float64(phaseCount)
}

// detectionStatus is the one sentence the UI shows when a run ends. It says
// what happened, including the two things the old code computed and then
// dropped on the floor: that a run was cancelled, and that files were skipped.
//
// When the AI phase ran it also names the REQUEST count, so the summary of a
// run that found nothing distinguishes "the model was asked fifteen times" from
// "there was one small thing to read" without the reader opening anything else.
func detectionStatus(res *DetectionResult, docCount int) string {
	switch {
	case res.Cancelled:
		return fmt.Sprintf("cancelled: %d suggestion(s) found before it stopped",
			len(res.Suggestions))
	case len(res.Errors) > 0:
		return fmt.Sprintf("finished with %d problem(s): %d suggestion(s) from %d file(s)%s",
			len(res.Errors), len(res.Suggestions), docCount, aiRequestSummary(res))
	default:
		return fmt.Sprintf("scanned %d file(s), %d suggestion(s)%s",
			docCount, len(res.Suggestions), aiRequestSummary(res))
	}
}

// aiRequestSummary is the trailing clause naming what the local AI was asked,
// and empty when the route did not run, so a Smart-only run's summary is
// unchanged.
func aiRequestSummary(res *DetectionResult) string {
	if res.AIRequests == 0 {
		return ""
	}
	// The two clauses are separate because they are separate facts, and a run
	// can carry both: some requests found nothing, others were still answering
	// when they ran out of room.
	var clauses []string
	if res.AISilentRequests > 0 {
		clauses = append(clauses, fmt.Sprintf("%d returned nothing", res.AISilentRequests))
	}
	if res.AITruncatedRequests > 0 {
		clauses = append(clauses, fmt.Sprintf("%d ran out of room", res.AITruncatedRequests))
	}
	if len(clauses) == 0 {
		return fmt.Sprintf(" (local AI: %d request(s))", res.AIRequests)
	}
	return fmt.Sprintf(" (local AI: %d request(s), %s)",
		res.AIRequests, strings.Join(clauses, ", "))
}

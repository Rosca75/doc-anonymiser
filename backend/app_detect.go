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
	// ChunkIndex/ChunkCount are the position INSIDE one document's AI scan.
	// Zero when the phase does not chunk. Without them a single large file
	// sits on one unchanging caption for minutes and reads as hung.
	ChunkIndex int `json:"chunkIndex"`
	ChunkCount int `json:"chunkCount"`
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
	if len(phases) == 0 {
		res.Status = "no detection route is switched on, turn on Smart detection or Local AI in Configure"
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
			a.runLocalAIPhase(ctx, docs, llm, aiScope, res, report)
		}
		if ctx.Err() != nil {
			break
		}
	}

	// With the Local AI route on, the model also RE-FILES what Smart detection
	// found (only main texts and short context snippets travel, never documents).
	// A classification failure degrades to the heuristic categories with a note:
	// the findings are the point, the labels are polish.
	if useLocalAI && ctx.Err() == nil && len(res.Suggestions) > 0 {
		if err := a.refineCategories(ctx, llm, res); err != nil && ctx.Err() == nil {
			res.Errors = append(res.Errors,
				fmt.Sprintf("the local AI could not refine the categories, the offline guesses were kept: %v", err))
		}
	}

	// Fold Value families ONCE, over the unified list. Per method would leave a
	// heuristic "Coca-Cola" and a model "Coca-Cola company" unfolded, which is
	// exactly the pair that has to become one Value: left apart, the shorter one
	// fires inside the longer one, the text reads "[BRAND_1] company", and two
	// numbers are spent on one company.
	res.Suggestions = engine.FoldValueFamilies(res.Suggestions, allow)

	res.Cancelled = ctx.Err() != nil
	res.Status = detectionStatus(res, len(docs))
	emitted = true
	a.emit("detection:done", res)
	return res, nil
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
func (a *App) refineCategories(ctx context.Context, llm *ollama.Client, res *DetectionResult) error {
	var smart []engine.Suggestion
	for _, s := range res.Suggestions {
		if smartDiscovered(s) {
			smart = append(smart, s)
		}
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

// runLocalAIPhase is the Local AI route. Oversized documents are SKIPPED and
// said so, rather than failing the run: the context window is a fact about
// the model, not a mistake the user made.
//
// When scope is active the route reads ONE document. If the scope names a set
// of pages it reads only those (CLAUDE.md §5); with no pages it reads the whole
// selected document. Either way the point is the same: keep the text handed to
// a small local model small. The Smart route is unaffected — it already read
// everything.
func (a *App) runLocalAIPhase(ctx context.Context, docs []engine.Document, llm *ollama.Client,
	scope *AIScope, res *DetectionResult, report func(DetectionProgress),
) {
	// scanUnit pairs a document with the exact text the AI should read for it:
	// the whole markdown normally, or the selected pages when scoped.
	type scanUnit struct {
		name string
		text string
	}
	var units []scanUnit
	scopeMatched := false
	for _, doc := range docs {
		if scope.active() {
			if doc.Name != scope.DocName {
				continue
			}
			scopeMatched = true
			// An empty page set means "the whole selected document"; a
			// non-empty set narrows to exactly those pages.
			if len(scope.Pages) == 0 {
				units = append(units, scanUnit{name: doc.Name, text: doc.Markdown})
				continue
			}
			text, err := doc.PagesMarkdown(scope.Pages)
			if err != nil {
				// An out-of-range scope is the user's request, not a file
				// problem: report it and let the run finish cleanly.
				res.Errors = append(res.Errors, err.Error())
				continue
			}
			units = append(units, scanUnit{name: doc.Name, text: text})
			continue
		}
		units = append(units, scanUnit{name: doc.Name, text: doc.Markdown})
	}
	if scope.active() && !scopeMatched {
		res.Errors = append(res.Errors, fmt.Sprintf(
			"the local AI was scoped to %q, which is not among the imported documents; import it or clear the scope",
			scope.DocName))
	}

	var readable []scanUnit
	for _, u := range units {
		if chunks := llm.EstimateChunks(u.text); chunks > ollama.MaxChunksPerDocument {
			res.Skipped = append(res.Skipped, DetectionSkip{
				Name: u.name,
				Reason: fmt.Sprintf(
					"too large for the local AI (%d chunks, the limit is %d). Smart detection still read it; to include it here, split it into smaller files or scope the local AI to a page range.",
					chunks, ollama.MaxChunksPerDocument),
			})
			continue
		}
		readable = append(readable, u)
	}

	// Partial work is kept whatever ends the loop, which is why the merge is
	// deferred: a cancellation used to return straight out and throw away
	// everything the files before it had produced.
	var batches [][]engine.Suggestion
	defer func() { res.Suggestions = mergeInto(res.Suggestions, batches) }()

	for i, u := range readable {
		if ctx.Err() != nil {
			return
		}
		report(DetectionProgress{DocIndex: i, DocCount: len(readable), DocName: u.name})

		proposals, err := llm.DiscoverWithProgress(ctx, u.text, func(index, total int) {
			report(DetectionProgress{
				DocIndex: i, DocCount: len(readable), DocName: u.name,
				ChunkIndex: index, ChunkCount: total,
			})
		})
		// Partial chunk proposals survive a mid-file cancellation.
		if len(proposals) > 0 {
			batches = append(batches, proposals)
		}
		if err != nil {
			if ctx.Err() != nil {
				return // cancelled: keep what we have
			}
			// One file the model choked on must not throw away the other
			// nine. This is the case that used to abort the whole run with an
			// error the caller turned into a toast and nothing else.
			res.Errors = append(res.Errors,
				fmt.Sprintf("the local AI failed on %q: %v", u.name, err))
		}
	}
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
func detectionStatus(res *DetectionResult, docCount int) string {
	switch {
	case res.Cancelled:
		return fmt.Sprintf("cancelled: %d suggestion(s) found before it stopped",
			len(res.Suggestions))
	case len(res.Errors) > 0:
		return fmt.Sprintf("finished with %d problem(s): %d suggestion(s) from %d file(s)",
			len(res.Errors), len(res.Suggestions), docCount)
	default:
		return fmt.Sprintf("scanned %d file(s), %d suggestion(s)",
			docCount, len(res.Suggestions))
	}
}

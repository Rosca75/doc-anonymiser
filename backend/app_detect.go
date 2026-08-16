// app_detect.go — ONE bound method for the whole detection run.
//
// The reported problem was "detection sometimes does not complete, and the
// progress is difficult to follow". It was not one bug, it was the shape of
// the old design: the frontend made TWO bridge calls in sequence
// (RunSmartDetection, then RunDiscovery), which meant
//
//   - two cancellation slots, with a window between them where Cancel was a
//     silent no-op while the button still said "Cancel";
//   - two progress streams, so the bar rewound to file 1 of a SMALLER total
//     when the second pass started;
//   - no terminal event of any kind, so the only thing that could clear the
//     progress bar was a `finally` in the caller: any escape in between (a
//     render error, a lost promise) left it spinning forever;
//   - a status and a cancelled flag that both passes returned and the caller
//     read neither, so a cancelled run reported "detection done".
//
// RunDetection replaces both calls with one: one context in one cancellation
// slot, one monotonic progress stream, and exactly one terminal event
// ("detection:done" or "detection:error") whatever happens, including a
// panic. The old methods stay for the tests that drive one pass at a time.
package backend

import (
	"context"
	"fmt"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/ollama"
)

// DetectionPhase names one route in the run. These are engine tokens, not
// labels: the frontend maps them to the words the user reads.
const (
	PhaseSmart = "smart"
	PhaseAI    = "ai"
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
// Errors is a LIST rather than a returned error: one file that the model
// choked on must not throw away the candidates found in the other nine. The
// caller shows them as warnings.
type DetectionResult struct {
	Candidates []engine.Candidate      `json:"candidates"`
	Proposals  []engine.ProposedEntity `json:"proposals"`
	// Phases lists the routes that actually ran, so the UI can say "smart
	// detection only" without re-deriving it from the settings.
	Phases    []string        `json:"phases"`
	Skipped   []DetectionSkip `json:"skipped"`
	Errors    []string        `json:"errors"`
	Cancelled bool            `json:"cancelled"`
	Status    string          `json:"status"`
}

// AIScope narrows the local-AI route to one document and a range of its own
// sub-units (pages/slides/rows/lines — see engine.Document.PageCount).
//
// It exists because handing a whole document to a small local model is "too
// much" (the reported problem): the scan stalls or the context window
// overflows. Scoping is deliberately LOCAL-AI ONLY — the offline Smart route is
// cheap and reads everything — so it lives here rather than in Settings, and it
// is a per-run choice that is never persisted to a session.
//
// A nil *AIScope, or one with an empty DocName, means "every document, whole"
// — the unchanged behaviour.
type AIScope struct {
	DocName  string `json:"docName"`  // "" = every document
	FromPage int    `json:"fromPage"` // 1-based inclusive
	ToPage   int    `json:"toPage"`   // 1-based inclusive
}

// active reports whether this scope actually narrows anything.
func (s *AIScope) active() bool {
	return s != nil && s.DocName != ""
}

// RunDetection runs every enabled detection route over the named files, in
// order, under ONE cancellation context.
//
// Which routes run is decided HERE, from the stored settings, not by the
// caller: UseSmartDetect and UseAI are the switches on the Configure rail, and
// the AI route additionally requires Ollama to actually answer. A frontend that
// asks for a route the user switched off does not get it.
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
	useAI := settings.UseAI && llm.Probe().Available
	phases := []string{}
	if settings.UseSmartDetect {
		phases = append(phases, PhaseSmart)
	}
	if useAI {
		phases = append(phases, PhaseAI)
	}

	res := &DetectionResult{Phases: phases, Candidates: []engine.Candidate{}, Proposals: []engine.ProposedEntity{}}
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
			a.runSmartPhase(ctx, docs, allow, settings.SmartDetect, settings.Country, res, report)
		} else {
			a.runAIPhase(ctx, docs, llm, aiScope, res, report)
		}
		if ctx.Err() != nil {
			break
		}
	}

	// With the AI route on, the model also REFINES the offline route's
	// categories (span classification: only candidate texts and short context
	// snippets travel, never documents). This path existed and was reachable
	// from the bridge, but nothing in the UI ever asked for it, so the local
	// AI never improved the guesses the heuristic makes from word shape alone.
	// A classification failure degrades to the heuristic categories with a
	// note: the offline route's findings are the point, the labels are polish.
	if useAI && ctx.Err() == nil && len(res.Candidates) > 0 {
		if err := a.refineCategories(ctx, llm, res); err != nil && ctx.Err() == nil {
			res.Errors = append(res.Errors,
				fmt.Sprintf("the local AI could not refine the categories, the offline guesses were kept: %v", err))
		}
	}

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

// refineCategories asks the local model to categorise the offline route's
// candidates, and applies whatever it recognised.
func (a *App) refineCategories(ctx context.Context, llm *ollama.Client, res *DetectionResult) error {
	proposals, err := llm.ClassifyCandidates(ctx, res.Candidates)
	if err != nil {
		return err
	}
	refined := map[string]string{}
	for _, p := range proposals {
		refined[p.Text] = p.Category
	}
	for i := range res.Candidates {
		if category, ok := refined[res.Candidates[i].Text]; ok {
			res.Candidates[i].Category = category
		}
	}
	return nil
}

// runSmartPhase is the offline route. Every document is scanned; a document
// that is cancelled mid-scan contributes what it found before stopping.
// country scopes the organisation-keyword signal to the document country.
func (a *App) runSmartPhase(ctx context.Context, docs []engine.Document, allow *engine.Allowlist,
	opts engine.SmartDetectOptions, country string, res *DetectionResult, report func(DetectionProgress),
) {
	merged := map[string]*engine.Candidate{}
	var order []string
	for i, doc := range docs {
		if ctx.Err() != nil {
			return
		}
		report(DetectionProgress{DocIndex: i, DocCount: len(docs), DocName: doc.Name})

		found, err := engine.SmartDetectContext(ctx, doc.Markdown, allow, opts, country)
		if err != nil && ctx.Err() == nil {
			// Not a cancellation: record it and carry on with the next file.
			res.Errors = append(res.Errors, fmt.Sprintf("smart detection failed on %q: %v", doc.Name, err))
			continue
		}
		for _, cand := range found {
			m, ok := merged[cand.Text]
			if !ok {
				copyCand := cand
				merged[cand.Text] = &copyCand
				order = append(order, cand.Text)
				continue
			}
			m.Count += cand.Count
			// The strongest sighting across documents wins: a name seen once
			// in one file and next to a legal form in another is as good as
			// the legal-form sighting.
			if cand.Confidence > m.Confidence {
				m.Confidence = cand.Confidence
			}
			for _, snippet := range cand.Contexts {
				if len(m.Contexts) >= 3 {
					break
				}
				m.Contexts = append(m.Contexts, snippet)
			}
		}
	}

	for _, key := range order {
		res.Candidates = append(res.Candidates, *merged[key])
	}
}

// runAIPhase is the local-model route. Oversized documents are SKIPPED and
// said so, rather than failing the run: the context window is a fact about
// the model, not a mistake the user made.
//
// When scope is active the route reads ONE document and only the requested
// page range of it (CLAUDE.md §5), which is the whole point of the feature:
// keep the text handed to a small local model small. The Smart route is
// unaffected — it already read everything.
func (a *App) runAIPhase(ctx context.Context, docs []engine.Document, llm *ollama.Client,
	scope *AIScope, res *DetectionResult, report func(DetectionProgress)) {

	// scanUnit pairs a document with the exact text the AI should read for it:
	// the whole markdown normally, or one page range when scoped.
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
			text, err := doc.PageRangeMarkdown(scope.FromPage, scope.ToPage)
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
	// deferred: a cancellation used to return straight out of the loop and throw
	// away every proposal the files before it had produced.
	var batches [][]engine.ProposedEntity
	defer func() { res.Proposals = ollama.MergeProposals(batches...) }()

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
			len(res.Candidates)+len(res.Proposals))
	case len(res.Errors) > 0:
		return fmt.Sprintf("finished with %d problem(s): %d suggestion(s) from %d file(s)",
			len(res.Errors), len(res.Candidates)+len(res.Proposals), docCount)
	default:
		return fmt.Sprintf("scanned %d file(s), %d suggestion(s)",
			docCount, len(res.Candidates)+len(res.Proposals))
	}
}

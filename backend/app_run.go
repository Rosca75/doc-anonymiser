// app_run.go — bound methods for the Run screen (Phase 8): pipeline
// execution in a goroutine with progress events, cancellation, and the
// fast "something missed?" re-run path. Thin adapters (CLAUDE.md §3) —
// the engine does all the work.
package backend

import (
	"context"
	"fmt"

	"doc-anonymiser/backend/engine"
)

// RunRequest is the frontend's pipeline configuration: the reviewed
// entities, allowlist, patterns, rules and whether to use the LLM
// deep-scan. Level/model/port come from the stored settings.
type RunRequest struct {
	Entities   []engine.Entity        `json:"entities"`
	AllowTerms []string               `json:"allowTerms"`
	Patterns   []engine.CustomPattern `json:"patterns"`
	// Categories is the granular per-category switch set from the
	// Configure screen (BUILD-02 Phase 3). nil falls back to the stored
	// settings, then to the level preset.
	Categories  engine.CategorySelection `json:"categories"`
	SimpleRules []engine.SimpleRule      `json:"simpleRules"`
	// UseDeepScan enables pass 3. The UI only offers it when Ollama is
	// available; the flag is also forced off server-side when it is not.
	UseDeepScan bool `json:"useDeepScan"`
}

// MappingInfo is the per-placeholder lookup the results view uses for
// hover tooltips and click-to-reassign (BUILD-02 Phase 10a). MEMORY NOTE:
// this is the re-identification key; it lives in the app process exactly
// like the Go-side registry and is never written anywhere by this path.
type MappingInfo struct {
	Original string `json:"original"`
	Category string `json:"category"`
}

// pipelineDonePayload is the "pipeline:done" event body: the results
// plus the placeholder → original mapping (embedded, so the frontend
// keeps reading results fields as before and finds .mapping next to them).
type pipelineDonePayload struct {
	*engine.Results
	Mapping map[string]MappingInfo `json:"mapping"`
}

// GetMapping returns the current placeholder → {original, category}
// lookup from the session registry (empty before the first run). The
// fast re-run path refreshes the frontend copy through this method.
func (a *App) GetMapping() map[string]MappingInfo {
	a.mu.Lock()
	reg := a.registry
	a.mu.Unlock()
	out := map[string]MappingInfo{}
	if reg == nil {
		return out
	}
	for _, e := range reg.Export() {
		out[e.Placeholder] = MappingInfo{Original: e.Original, Category: e.Category}
	}
	return out
}

// RunPipeline starts the pipeline in a goroutine and returns immediately.
// Progress arrives on the "pipeline:progress" event (one per document per
// stage) and the full results on "pipeline:done" — the window must never
// freeze during a long run (BUILD.md performance table).
func (a *App) RunPipeline(req RunRequest) error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return fmt.Errorf("a run is already in progress, cancel it or wait for it to finish")
	}
	if len(a.docs) == 0 {
		a.mu.Unlock()
		return fmt.Errorf("no documents to process, import files on the Import screen first")
	}
	a.running = true
	ctx, cancel := context.WithCancel(context.Background())
	a.cancelRun = cancel
	a.mu.Unlock()

	go func() {
		defer func() {
			a.mu.Lock()
			a.running = false
			a.cancelRun = nil
			a.mu.Unlock()
		}()
		results, err := a.runPipelineBlocking(ctx, req)
		if err != nil && results == nil {
			// Only construction-level failures land here; a cancelled
			// run still carries partial results.
			a.emit("pipeline:done", pipelineDonePayload{
				Results: &engine.Results{Report: engine.Report{
					Warnings: []string{err.Error()},
				}},
				Mapping: a.GetMapping(),
			})
			return
		}
		// The mapping rides along so the results view can show originals
		// on hover without a second round-trip (BUILD-02 Phase 10a).
		a.emit("pipeline:done", pipelineDonePayload{Results: results, Mapping: a.GetMapping()})
	}()
	return nil
}

// runPipelineBlocking assembles the engine input from the request + stored
// session state and runs it synchronously. Split from RunPipeline so unit
// tests can call it without goroutines or Wails events.
func (a *App) runPipelineBlocking(ctx context.Context, req RunRequest) (*engine.Results, error) {
	a.mu.Lock()
	docs := make([]engine.Document, len(a.docs))
	copy(docs, a.docs)
	level := engine.Level(a.settings.Level)
	// The request's selection wins; stored settings are the fallback so
	// exports and re-runs behave like the last configured run.
	categories := req.Categories
	if categories == nil {
		categories = a.settings.Categories
	}
	// The confidence floor is a SETTING, not a per-run input: it lives in
	// the Configure screen and applies to every run and fast re-run alike
	// (BUILD-04 CR9).
	minConfidence := a.settings.MinConfidence
	useAI := a.settings.UseAI
	llm := a.llm
	// The registry lives for the whole session so placeholders stay
	// stable across runs and late-imported batches (CLAUDE.md §5).
	if a.registry == nil {
		a.registry = engine.NewRegistry()
	}
	reg := a.registry
	a.mu.Unlock()

	// The allowlist and the removals come from one place, so a value the user
	// removed cannot be honoured here and forgotten by the same-format export
	// (BUILD-06 Phase 4).
	allow := a.allowlistFor(req.AllowTerms)
	removed := a.removedValues()

	// Set when a requested deep scan does not run, so the report says why
	// rather than leaving the user to wonder what the checkbox did.
	deepScanSkipped := ""

	input := engine.PipelineInput{
		Documents:     docs,
		Entities:      req.Entities,
		Patterns:      req.Patterns,
		Level:         level,
		Categories:    categories,
		MinConfidence: minConfidence,
		Country:       a.settings.Country,
		Allowlist:     allow,
		Registry:      reg,
		SimpleRules:   req.SimpleRules,
		Removed:       removed,
		Progress: func(ev engine.ProgressEvent) {
			a.emit("pipeline:progress", ev)
		},
	}
	// GO decides whether the AI pass runs, not the caller (BUILD-06). The
	// request asking for it is necessary but not sufficient: the user's Local
	// AI switch has to be on and Ollama has to actually answer. Settings.UseAI
	// was stored, serialised into every session file, and read by no decision
	// path at all, which meant the only thing standing between a switched-off
	// route and a running model was a boolean the frontend computed.
	if req.UseDeepScan {
		switch {
		case !useAI:
			res := "the local AI is switched off in Configure, so the deep scan was skipped"
			deepScanSkipped = res
		case !llm.Probe().Available:
			deepScanSkipped = "Ollama did not answer, so the deep scan was skipped and the deterministic passes ran alone"
		default:
			// The allowlist veto inside the client mirrors the engine's own
			// check (allowlist wins in every pass).
			llm.Allow = allow.Contains
			input.LLM = llm
		}
	}

	results, err := engine.Run(ctx, input)
	if results != nil && deepScanSkipped != "" {
		results.Report.LLMPass = deepScanSkipped
		results.Report.Warnings = append(results.Report.Warnings, deepScanSkipped)
	}
	if results != nil {
		a.mu.Lock()
		a.results = results
		// Remember the run inputs so the same-format export (BUILD-02 Phase 11)
		// re-runs the identical span machinery over the original bytes.
		//
		// Stored AFTER the run, not before (BUILD-06 Phase 8). A run that
		// validation refused, or that failed outright, used to leave the export
		// faithfully reproducing a request the application had just declared
		// invalid: the user saw an error on screen and a same-format file that
		// disagreed with it.
		if len(results.Validation.Blocking) == 0 {
			reqCopy := req
			a.lastReq = &reqCopy
		}
		a.mu.Unlock()
	}
	// Cancellation still returns the partial results to the caller; the
	// report carries the "cancelled after N documents" warning.
	if err != nil && ctx.Err() == nil {
		return results, err
	}
	return results, nil
}

// CancelPipeline aborts the running pipeline (between documents, or
// mid-LLM-call via HTTP context cancellation). A no-op when idle.
func (a *App) CancelPipeline() {
	a.mu.Lock()
	cancel := a.cancelRun
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// FastRerun is the "something missed?" loop (BUILD.md Phase 8): re-run the
// deterministic passes only — never the LLM — with the (updated) entities
// and rules, reusing the session registry so existing placeholders keep
// their numbers. Fast enough to run synchronously.
func (a *App) FastRerun(req RunRequest) (*engine.Results, error) {
	req.UseDeepScan = false // the fast path never re-runs the LLM
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil, fmt.Errorf("a run is already in progress, wait for it to finish before re-running")
	}
	a.mu.Unlock()
	return a.runPipelineBlocking(context.Background(), req)
}

// GetResults returns the latest results (view refresh after navigation).
func (a *App) GetResults() *engine.Results {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.results
}

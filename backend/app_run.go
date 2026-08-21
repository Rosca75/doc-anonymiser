// app_run.go — bound methods for the Run screen: pipeline
// execution in a goroutine with progress events, cancellation, and the
// fast "Add missed Value" re-run path. Thin adapters (CLAUDE.md §3) —
// the engine does all the work.
package backend

import (
	"context"
	"fmt"

	"doc-anonymiser/backend/engine"
)

// RunRequest is the frontend's pipeline configuration: the reviewed
// entities, allowlist, patterns and rules. Level/model/port come from the
// stored settings.
type RunRequest struct {
	Values     []engine.Value         `json:"values"`
	AllowTerms []string               `json:"allowTerms"`
	Patterns   []engine.CustomPattern `json:"patterns"`
	// Categories is the granular per-category switch set from the
	// Configure screen. nil falls back to the stored
	// settings, then to the level preset.
	Categories engine.CategorySelection `json:"categories"`
	// SuppressRegexPII is the "Native detection" master switch, inverted: true
	// means the deterministic regex PII pass (pass 1) is skipped for this run,
	// so no signal category is replaced. The frontend sends it as
	// !settings.useBuiltInPatterns.
	SuppressRegexPII bool `json:"suppressRegexPII"`
}

// MappingInfo is the per-placeholder lookup the results view uses for
// hover tooltips and click-to-reassign. MEMORY NOTE:
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
// freeze during a long run.
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
		// on hover without a second round-trip.
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
	// The request's selection wins; stored settings are the fallback so
	// exports and re-runs behave like the last configured run.
	categories := req.Categories
	if categories == nil {
		categories = a.settings.Categories
	}
	// The checksum switch is a SETTING, not a per-run input: it lives in the
	// Configure rail and applies to every run and fast re-run alike.
	requireChecksum := a.settings.RequireChecksum
	// The registry lives for the whole session so placeholders stay
	// stable across runs and late-imported batches (CLAUDE.md §5).
	if a.registry == nil {
		a.registry = engine.NewRegistry()
	}
	reg := a.registry
	a.mu.Unlock()

	// The allowlist and the removals come from one place, so a value the user
	// removed cannot be honoured here and forgotten by the same-format export
	allow := a.allowlistFor(req.AllowTerms)
	removed := a.removedValues()

	input := engine.PipelineInput{
		Documents:        docs,
		Values:           req.Values,
		Patterns:         req.Patterns,
		Categories:       categories,
		RequireChecksum:  requireChecksum,
		Country:          a.settings.Country,
		Allowlist:        allow,
		Registry:         reg,
		Removed:          removed,
		SuppressRegexPII: req.SuppressRegexPII,
		Progress: func(ev engine.ProgressEvent) {
			a.emit("pipeline:progress", ev)
		},
	}

	results, err := engine.Run(ctx, input)
	if results != nil {
		a.mu.Lock()
		a.results = results
		// Remember the run inputs so the same-format export
		// re-runs the identical span machinery over the original bytes.
		//
		// Stored AFTER the run, not before. A run that
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

// FastRerun is the "Add missed Value" loop: re-run the
// deterministic passes only with the (updated) entities
// and rules, reusing the session registry so existing placeholders keep
// their numbers. Fast enough to run synchronously.
func (a *App) FastRerun(req RunRequest) (*engine.Results, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil, fmt.Errorf("a run is already in progress, wait for it to finish before re-running")
	}
	a.mu.Unlock()
	return a.runPipelineBlocking(context.Background(), req)
}

// latestResults is the last run's output. Like documentInfos, it is not bound:
// results reach the frontend on the "pipeline:done" event and as FastRerun's
// return value.
func (a *App) latestResults() *engine.Results {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.results
}

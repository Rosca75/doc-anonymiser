// app_run.go — bound methods for the Run screen (Phase 8): pipeline
// execution in a goroutine with progress events, cancellation, and the
// fast "something missed?" re-run path. Thin adapters (CLAUDE.md §3) —
// the engine does all the work.
package main

import (
	"context"
	"fmt"

	"doc-anonymiser/engine"
)

// RunRequest is the frontend's pipeline configuration: the reviewed
// entities, allowlist, patterns, rules and whether to use the LLM
// deep-scan. Level/model/port come from the stored settings.
type RunRequest struct {
	Entities    []engine.Entity        `json:"entities"`
	AllowTerms  []string               `json:"allowTerms"`
	Patterns    []engine.CustomPattern `json:"patterns"`
	SimpleRules []engine.SimpleRule    `json:"simpleRules"`
	// UseDeepScan enables pass 3. The UI only offers it when Ollama is
	// available; the flag is also forced off server-side when it is not.
	UseDeepScan bool `json:"useDeepScan"`
}

// RunPipeline starts the pipeline in a goroutine and returns immediately.
// Progress arrives on the "pipeline:progress" event (one per document per
// stage) and the full results on "pipeline:done" — the window must never
// freeze during a long run (BUILD.md performance table).
func (a *App) RunPipeline(req RunRequest) error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return fmt.Errorf("a run is already in progress — cancel it or wait for it to finish")
	}
	if len(a.docs) == 0 {
		a.mu.Unlock()
		return fmt.Errorf("no documents to process — import files on the Import screen first")
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
			a.emit("pipeline:done", engine.Results{Report: engine.Report{
				Warnings: []string{err.Error()},
			}})
			return
		}
		a.emit("pipeline:done", results)
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
	llm := a.llm
	// The registry lives for the whole session so placeholders stay
	// stable across runs and late-imported batches (CLAUDE.md §5).
	if a.registry == nil {
		a.registry = engine.NewRegistry()
	}
	reg := a.registry
	a.mu.Unlock()

	allow := engine.NewEmptyAllowlist()
	for _, t := range req.AllowTerms {
		allow.Add(t)
	}

	input := engine.PipelineInput{
		Documents:   docs,
		Entities:    req.Entities,
		Patterns:    req.Patterns,
		Level:       level,
		Allowlist:   allow,
		Registry:    reg,
		SimpleRules: req.SimpleRules,
		Progress: func(ev engine.ProgressEvent) {
			a.emit("pipeline:progress", ev)
		},
	}
	if req.UseDeepScan {
		// The allowlist veto inside the client mirrors the engine's own
		// check (allowlist wins in every pass).
		llm.Allow = allow.Contains
		input.LLM = llm
	}

	results, err := engine.Run(ctx, input)
	if results != nil {
		a.mu.Lock()
		a.results = results
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
		return nil, fmt.Errorf("a run is already in progress — wait for it to finish before re-running")
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

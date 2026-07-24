// app_entities.go — bound methods for the Entities screen (Phase 7):
// LLM discovery over selected files, variant expansion for the review
// table, and custom-pattern validation/testing. Thin adapters only
// (CLAUDE.md §3): all logic lives in engine/* and ollama/*.
package main

import (
	"context"
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"doc-anonymiser/engine"
	"doc-anonymiser/ollama"
)

// runtimeEventsEmit is an indirection over the Wails event runtime so unit
// tests (which have no Wails context) never touch it — emit() already
// guards on a nil ctx, and this var could be swapped in tests if needed.
var runtimeEventsEmit = func(a *App, name string, payload interface{}) {
	runtime.EventsEmit(a.ctx, name, payload)
}

// RunDiscovery executes the Phase-A discovery prompt on the named imported
// files (the user picks representative ones) and returns merged,
// deduplicated proposals for the review table. allowTerms is the current
// session allowlist — allowlisted proposals are vetoed inside the client.
//
// Progress is emitted per file on the "discovery:progress" event so the UI
// can show which file is being scanned.
func (a *App) RunDiscovery(fileNames []string, allowTerms []string) ([]engine.ProposedEntity, error) {
	docs := a.docsByName(fileNames)
	if len(docs) == 0 {
		return nil, fmt.Errorf("no matching imported files to scan, import documents first, then pick at least one for discovery")
	}

	// Wire the allowlist veto exactly once per call: the same allowlist
	// the pipeline will use later (allowlist wins in EVERY pass).
	allow := engine.NewEmptyAllowlist()
	for _, t := range allowTerms {
		allow.Add(t)
	}
	a.mu.Lock()
	llm := a.llm
	a.mu.Unlock()
	llm.Allow = allow.Contains

	var batches [][]engine.ProposedEntity
	for i, doc := range docs {
		a.emit("discovery:progress", map[string]interface{}{
			"docIndex": i, "docCount": len(docs), "docName": doc.Name,
		})
		proposals, err := llm.Discover(context.Background(), doc.Markdown)
		if err != nil {
			return nil, fmt.Errorf("discovery failed on %q: %w", doc.Name, err)
		}
		batches = append(batches, proposals)
	}
	// Merge per-file results, deduplicating per category (Phase 5).
	return ollama.MergeProposals(batches...), nil
}

// ExpandEntityVariants returns the automatic + manual variants of one
// entity for the expandable variant list in the review table.
func (a *App) ExpandEntityVariants(e engine.Entity) []string {
	return engine.ExpandVariants(e)
}

// ValidatePattern compile-checks a user regex. It returns the error as a
// STRING ("" = valid) instead of a Go error, because a validation failure
// is expected feedback for the live checker, not a rejected promise.
func (a *App) ValidatePattern(expr string) string {
	if err := engine.ValidateCustomPattern(expr); err != nil {
		return err.Error()
	}
	return ""
}

// PatternMatches runs a (valid) custom pattern over every loaded document
// and returns up to 20 sample matches for the tester UI.
func (a *App) PatternMatches(expr string) ([]string, error) {
	if err := engine.ValidateCustomPattern(expr); err != nil {
		return nil, err
	}
	a.mu.Lock()
	docs := a.docs
	a.mu.Unlock()

	const maxSamples = 20
	var samples []string
	seen := map[string]bool{}
	for _, doc := range docs {
		spans := engine.DetectCustomPatterns(doc.Markdown, []engine.CustomPattern{{Expr: expr}}, nil)
		for _, s := range spans {
			if seen[s.Original] {
				continue // show each distinct match once
			}
			seen[s.Original] = true
			samples = append(samples, s.Original)
			if len(samples) >= maxSamples {
				return samples, nil
			}
		}
	}
	return samples, nil
}

// docsByName returns the loaded documents matching the given names, in
// request order. Unknown names are silently skipped (the UI list and the
// Go list can only diverge for a moment during removal).
func (a *App) docsByName(names []string) []engine.Document {
	a.mu.Lock()
	defer a.mu.Unlock()
	byName := map[string]engine.Document{}
	for _, d := range a.docs {
		byName[d.Name] = d
	}
	var out []engine.Document
	for _, n := range names {
		if d, ok := byName[n]; ok {
			out = append(out, d)
		}
	}
	return out
}

// emit fires a frontend event, tolerating the headless case (a.ctx is nil
// in unit tests — events are UI sugar, never load-bearing).
func (a *App) emit(name string, payload interface{}) {
	if a.ctx == nil {
		return
	}
	runtimeEventsEmit(a, name, payload)
}

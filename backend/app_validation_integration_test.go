//go:build integration

// app_validation_integration_test.go — multi-file discovery through the bound
// App against a MOCK Ollama server.
//
// TIER: integration (docs/TESTING.md). Each test drives App.RunDetection with
// Local LLM discovery on, answered by an httptest stand-in for Ollama, and
// asserts the wiring: cross-file merge and dedupe, the allowlist veto on model
// proposals, oversized-file skipping, cancellation keeping partial work, and
// per-file progress. It stays hermetic (loopback httptest, zero real network),
// which is why it is integration and not deep. newTestApp is also used by
// app_detect_integration_test.go, which is compiled in the same tier.
package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/ollama"
)

// newTestApp builds an App whose Ollama client points at a mock server
// answering /api/chat with per-request content derived from the prompt.
func newTestApp(t *testing.T, replyFor func(userPrompt string) string) *App {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"qwen3.5:0.8b"}]}`))
		case "/api/chat":
			var req struct {
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			user := ""
			for _, m := range req.Messages {
				if m.Role == "user" {
					user = m.Content
				}
			}
			resp, _ := json.Marshal(map[string]interface{}{
				"message": map[string]string{"role": "assistant", "content": replyFor(user)},
			})
			w.Write(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	app := NewApp()
	app.llm = ollama.New(srv.URL)
	return app
}

// llmOnlyApp is newTestApp with the local model route on and the offline route off, so a
// test about what the model proposes is not also reading heuristic findings.
func llmOnlyApp(t *testing.T, replyFor func(userPrompt string) string) *App {
	t.Helper()
	app := newTestApp(t, replyFor)
	app.settings.UseLocalLLM = true
	app.settings.UseHeuristicDiscovery = false
	// The offline phase is heuristic discovery OR any signal reading, and the
	// readings default on, so they are switched off explicitly: with any of
	// them live the rules phase still runs (and reports progress), and a test
	// about what the model proposes would be reading offline events too.
	app.settings.SignalSuggestionSources = allSignalReadingsOff()
	return app
}

func TestDetectionMergesAndDedupesAcrossFiles(t *testing.T) {
	// Two documents name the same entity with different casing plus one distinct
	// person each. The merged result carries the entity once, in the spelling
	// seen first, and both people.
	app := llmOnlyApp(t, func(user string) string {
		if strings.Contains(user, "doc one") {
			return `{"entity_names":["Alpine Trust"],"person_names":["Marie Duval"]}`
		}
		return `{"entity_names":["ALPINE TRUST"],"person_names":["Peter Stone"]}`
	})
	app.docs = []engine.Document{
		{Name: "one.txt", Format: engine.FormatTXT, Markdown: "doc one: Alpine Trust with Marie Duval"},
		{Name: "two.txt", Format: engine.FormatTXT, Markdown: "doc two: ALPINE TRUST with Peter Stone"},
	}

	res, err := app.RunDetection([]string{"one.txt", "two.txt"}, nil, nil)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	if len(res.Suggestions) != 3 {
		t.Fatalf("want 3 merged proposals with the entity deduped, got %+v", res.Suggestions)
	}
	if res.Suggestions[0].MainText != "Alpine Trust" {
		t.Errorf("the first-seen spelling must win, got %q", res.Suggestions[0].MainText)
	}
	if res.Cancelled {
		t.Errorf("a completed run must not report itself cancelled: %+v", res)
	}
}

func TestDetectionRespectsTheAllowlist(t *testing.T) {
	app := llmOnlyApp(t, func(string) string {
		return `{"entity_names":["CSSF","Alpine Trust"],"person_names":[]}`
	})
	app.docs = []engine.Document{
		{Name: "a.txt", Format: engine.FormatTXT, Markdown: "CSSF and Alpine Trust"},
	}

	res, err := app.RunDetection([]string{"a.txt"}, []string{"CSSF"}, nil)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	if len(res.Suggestions) != 1 || res.Suggestions[0].MainText != "Alpine Trust" {
		t.Errorf("an allowlisted proposal must be vetoed, got %+v", res.Suggestions)
	}
}

// TestDetectionScansALargeFileAndWarnsAboutIt: a document needing many requests
// is SCANNED, with a warning about the time, and never dropped from the route.
// The user asked for the scan, every request reports progress and cancel reaches
// mid-scan, so slowness is a cost they can see and stop; refusing leaves them
// with the offline findings and nothing saying why.
func TestDetectionScansALargeFileAndWarnsAboutIt(t *testing.T) {
	app := llmOnlyApp(t, func(string) string { return `{"entity_names":[],"person_names":[]}` })
	app.docs = []engine.Document{
		{Name: "small.txt", Format: engine.FormatTXT, Unit: engine.UnitLine,
			Markdown: "Alpine Trust is small."},
		{Name: "huge.txt", Format: engine.FormatTXT, Unit: engine.UnitLine,
			Markdown: strings.Repeat("line of text\n", 20000)},
	}

	res, err := app.RunDetection([]string{"small.txt", "huge.txt"}, nil, nil)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	for _, skip := range res.Skipped {
		if skip.Name == "huge.txt" {
			t.Fatalf("a large document must be scanned, not skipped: %+v", skip)
		}
	}
	var warned bool
	for _, msg := range res.Errors {
		if strings.Contains(msg, "huge.txt") && strings.Contains(msg, "requests") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("a scan needing many requests must warn about the time it takes, got %+v", res.Errors)
	}
}

// TestDetectionSkipsOnlyADocumentWithNoTextToRead: size no longer refuses a
// scan, so the skip has exactly one producer left, and it is a real case. Keeping
// it meaningful is what stops the field, and the interface that renders it, from
// becoming decoration.
func TestDetectionSkipsOnlyADocumentWithNoTextToRead(t *testing.T) {
	app := llmOnlyApp(t, func(string) string { return `{"entity_names":[],"person_names":[]}` })
	app.docs = []engine.Document{
		{Name: "blank.txt", Format: engine.FormatTXT, Unit: engine.UnitLine, Markdown: "  \n\t\n"},
		{Name: "real.txt", Format: engine.FormatTXT, Unit: engine.UnitLine, Markdown: "Alpine Trust."},
	}

	res, err := app.RunDetection([]string{"blank.txt", "real.txt"}, nil, nil)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Name != "blank.txt" {
		t.Fatalf("a whitespace-only document is the one thing left to skip, got %+v", res.Skipped)
	}
	if !strings.Contains(res.Skipped[0].Reason, "Heuristic discovery") {
		t.Errorf("the reason must say the file was still read offline, got %q", res.Skipped[0].Reason)
	}
}

// TestDetectionCancellationKeepsWhatItFound: cancelling mid-run returns the
// partial proposals and no error. Partial work is worth keeping, and the caller
// decides how to describe it.
func TestDetectionCancellationKeepsWhatItFound(t *testing.T) {
	var calls atomic.Int32
	app := llmOnlyApp(t, func(string) string {
		calls.Add(1)
		return `{"entity_names":["Alpine Trust"],"person_names":[]}`
	})
	app.docs = []engine.Document{
		{Name: "one.txt", Format: engine.FormatTXT, Markdown: "Alpine Trust one"},
		{Name: "two.txt", Format: engine.FormatTXT, Markdown: "Alpine Trust two"},
		{Name: "three.txt", Format: engine.FormatTXT, Markdown: "Alpine Trust three"},
	}

	// Cancel as soon as the second file starts: the run stops after the file in
	// flight and keeps its proposals.
	old := runtimeEventsEmit
	runtimeEventsEmit = func(a *App, _ string, payload interface{}) {
		if p, ok := payload.(DetectionProgress); ok && p.DocIndex == 1 {
			a.CancelDetection()
		}
	}
	defer func() { runtimeEventsEmit = old }()
	app.ctx = context.Background() // any non-nil ctx routes emit() to the stub

	res, err := app.RunDetection([]string{"one.txt", "two.txt", "three.txt"}, nil, nil)
	if err != nil {
		t.Fatalf("a cancelled run must not be an error: %v", err)
	}
	if !res.Cancelled {
		t.Errorf("the run must report itself cancelled: %+v", res)
	}
	if len(res.Suggestions) == 0 {
		t.Error("partial proposals must survive cancellation")
	}
	if calls.Load() > 2 {
		t.Errorf("the scan must stop after the cancelled file, made %d chat calls", calls.Load())
	}
}

// TestDetectionReportsProgressPerFile: one event per file at least, each naming
// the file, so a long run never looks hung.
func TestDetectionReportsProgressPerFile(t *testing.T) {
	app := llmOnlyApp(t, func(string) string { return `{"entity_names":[],"person_names":[]}` })
	app.docs = []engine.Document{
		{Name: "one.txt", Format: engine.FormatTXT, Markdown: "text one"},
		{Name: "two.txt", Format: engine.FormatTXT, Markdown: "text two"},
	}

	var named []string
	old := runtimeEventsEmit
	runtimeEventsEmit = func(_ *App, _ string, payload interface{}) {
		if p, ok := payload.(DetectionProgress); ok && p.ChunkCount == 0 {
			named = append(named, p.DocName)
		}
	}
	defer func() { runtimeEventsEmit = old }()
	app.ctx = context.Background()

	if _, err := app.RunDetection([]string{"one.txt", "two.txt"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(named) != 2 || named[0] != "one.txt" || named[1] != "two.txt" {
		t.Errorf("want one progress event per file, in order, got %v", named)
	}
}

// app_hardening_test.go — Phase 10 tests: error-message format, the
// large-file truncated preview, and mid-run Ollama failure degrading the
// LLM pass instead of failing the batch.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"doc-anonymiser/engine"
	"doc-anonymiser/ollama"
)

// TestErrorMessageFormat samples representative user-facing errors and
// asserts each states BOTH what failed and how to fix it (CLAUDE.md §2:
// error messages must be actionable). The em dash separates the failure
// from the remedy in our message convention.
func TestErrorMessageFormat(t *testing.T) {
	app := NewApp()

	collect := func() []error {
		_, e1 := engine.Load("legacy.txt", []byte{0xFF, 0xFE, 0x00})
		_, e2 := engine.LoadAll("archive.zip", []byte("x"))
		e3 := app.RunPipeline(RunRequest{}) // no documents
		_, e4 := engine.LoadSession([]byte("not json"))
		_, e5 := engine.ExportBytes(engine.ResultDocument{Format: engine.FormatCSV}, "json")
		_, e6 := app.ApplySettings(Settings{Level: "extreme", OllamaPort: 11434})
		return []error{e1, e2, e3, e4, e5, e6}
	}
	for i, err := range collect() {
		if err == nil {
			t.Errorf("case %d: expected an error", i)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, "—") {
			t.Errorf("case %d: message lacks the failure—remedy structure: %q", i, msg)
		}
		if len(msg) < 30 {
			t.Errorf("case %d: message too terse to be actionable: %q", i, msg)
		}
	}
}

// TestLargeFilePreviewTruncation: a >5000-line document previews truncated
// with the flag set, while the stored document (what the pipeline reads)
// keeps every line.
func TestLargeFilePreviewTruncation(t *testing.T) {
	lines := strings.Repeat("line of text\n", engine.MaxPreviewLines+100)
	app := NewApp()
	docs, err := engine.LoadAll("big.txt", []byte(lines))
	if err != nil {
		t.Fatal(err)
	}
	app.docs = docs

	infos := app.ListDocuments()
	if !infos[0].PreviewTruncated {
		t.Fatal("preview of a huge document must be marked truncated")
	}
	if got := strings.Count(infos[0].Markdown, "\n"); got >= engine.MaxPreviewLines {
		t.Errorf("preview has %d newlines, want < %d", got, engine.MaxPreviewLines)
	}
	// The full content is still what the pipeline processes.
	if got := strings.Count(app.docs[0].Markdown, "\n"); got != engine.MaxPreviewLines+100 {
		t.Errorf("stored document was truncated too (%d lines) — the pipeline must see everything", got)
	}

	// Small documents are never flagged.
	small, _ := engine.PreviewMarkdown("just\nthree\nlines")
	if small != "just\nthree\nlines" {
		t.Error("small document preview must be unchanged")
	}
}

// TestMidRunOllamaCrashDegrades: the mock Ollama answers the first chat
// call then dies; a 2-document deep-scan run must still anonymise BOTH
// documents deterministically, mark the LLM pass degraded and record a
// warning — never fail the batch (CLAUDE.md §4 graceful degradation).
func TestMidRunOllamaCrashDegrades(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"m"}]}`))
		case "/api/chat":
			if calls.Add(1) > 1 {
				// Simulate the crash: hijack and slam the connection.
				hj, _ := w.(http.Hijacker)
				conn, _, _ := hj.Hijack()
				conn.Close()
				return
			}
			resp, _ := json.Marshal(map[string]interface{}{
				"message": map[string]string{"role": "assistant",
					"content": `{"client_names":[],"project_names":[],"pwc_internal_names":[],"person_names":[]}`},
			})
			w.Write(resp)
		}
	}))
	defer srv.Close()

	app := NewApp()
	app.llm = ollama.New(srv.URL)
	app.docs = []engine.Document{
		{Name: "1.txt", Format: engine.FormatTXT, Markdown: "first mail one@example.com"},
		{Name: "2.txt", Format: engine.FormatTXT, Markdown: "second mail two@example.com"},
	}

	res, err := app.runPipelineBlocking(context.Background(), RunRequest{UseDeepScan: true})
	if err != nil {
		t.Fatalf("mid-run crash must not fail the batch: %v", err)
	}
	if len(res.Documents) != 2 {
		t.Fatalf("both documents must be processed, got %d", len(res.Documents))
	}
	// The deterministic passes still did their job on the failed file.
	if !strings.Contains(res.Documents[1].Anonymised, "[EMAIL_") {
		t.Errorf("deterministic passes skipped on the degraded document: %q", res.Documents[1].Anonymised)
	}
	if !strings.HasPrefix(res.Report.LLMPass, "degraded") {
		t.Errorf("LLM pass note = %q, want degraded", res.Report.LLMPass)
	}
	found := false
	for _, w := range res.Report.Warnings {
		if strings.Contains(w, "deep-scan failed") && strings.Contains(w, "2.txt") {
			found = true
		}
	}
	if !found {
		t.Errorf("degradation warning missing: %+v", res.Report.Warnings)
	}
}

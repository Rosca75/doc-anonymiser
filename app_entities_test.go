// app_entities_test.go — Phase 7 Go-side tests: the ExpandEntityVariants
// bound-method adapter and multi-file discovery merge/dedupe against a
// mocked Ollama server (zero real network).
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"doc-anonymiser/engine"
	"doc-anonymiser/ollama"
)

// newTestApp builds an App whose Ollama client points at a mock server
// answering /api/chat with per-request content derived from the prompt.
func newTestApp(t *testing.T, replyFor func(userPrompt string) string) *App {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"qwen2.5:3b-instruct"}]}`))
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

func TestExpandEntityVariantsAdapter(t *testing.T) {
	app := NewApp()
	got := app.ExpandEntityVariants(engine.Entity{Category: "person_names", Canonical: "Marie Duval"})
	joined := strings.Join(got, "|")
	for _, want := range []string{"Marie Duval", "M. Duval", "Duval", "Marie"} {
		if !strings.Contains(joined, want) {
			t.Errorf("adapter lost variant %q: %v", want, got)
		}
	}
}

func TestRunDiscoveryMergesAndDedupes(t *testing.T) {
	// Two docs mention the same client with different casing plus one
	// distinct person each; the merged result must contain the client
	// once (first spelling) and both persons.
	app := newTestApp(t, func(user string) string {
		if strings.Contains(user, "doc one") {
			return `{"client_names":["Alpine Trust"],"project_names":[],"internal_names":[],"person_names":["Marie Duval"]}`
		}
		return `{"client_names":["ALPINE TRUST"],"project_names":[],"internal_names":[],"person_names":["Peter Stone"]}`
	})
	app.docs = []engine.Document{
		{Name: "one.txt", Format: engine.FormatTXT, Markdown: "doc one: Alpine Trust with Marie Duval"},
		{Name: "two.txt", Format: engine.FormatTXT, Markdown: "doc two: ALPINE TRUST with Peter Stone"},
	}

	got, err := app.RunDiscovery([]string{"one.txt", "two.txt"}, nil)
	if err != nil {
		t.Fatalf("RunDiscovery: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 merged proposals (client deduped), got %+v", got)
	}
	if got[0].Text != "Alpine Trust" {
		t.Errorf("first-seen spelling must win, got %q", got[0].Text)
	}
}

func TestRunDiscoveryRespectsAllowlist(t *testing.T) {
	app := newTestApp(t, func(string) string {
		return `{"client_names":["CSSF","Alpine Trust"],"project_names":[],"internal_names":[],"person_names":[]}`
	})
	app.docs = []engine.Document{{Name: "a.txt", Format: engine.FormatTXT, Markdown: "CSSF and Alpine Trust"}}

	got, err := app.RunDiscovery([]string{"a.txt"}, []string{"CSSF"})
	if err != nil {
		t.Fatalf("RunDiscovery: %v", err)
	}
	if len(got) != 1 || got[0].Text != "Alpine Trust" {
		t.Errorf("allowlisted proposal must be vetoed, got %+v", got)
	}
}

func TestRunDiscoveryNoMatchingFiles(t *testing.T) {
	app := NewApp()
	if _, err := app.RunDiscovery([]string{"ghost.txt"}, nil); err == nil {
		t.Error("discovery over zero files must fail actionably")
	}
}

func TestValidateAndTestPattern(t *testing.T) {
	app := NewApp()
	if msg := app.ValidatePattern("PRJ-[0-9]+"); msg != "" {
		t.Errorf("valid pattern flagged: %s", msg)
	}
	if msg := app.ValidatePattern("["); msg == "" || !strings.Contains(msg, "regular expression") {
		t.Errorf("invalid pattern needs an actionable message, got %q", msg)
	}

	app.docs = []engine.Document{{Name: "a.txt", Format: engine.FormatTXT,
		Markdown: "codes PRJ-1 PRJ-2 PRJ-1 here"}}
	samples, err := app.PatternMatches("PRJ-[0-9]+")
	if err != nil {
		t.Fatalf("PatternMatches: %v", err)
	}
	// Distinct matches only.
	if len(samples) != 2 || samples[0] != "PRJ-1" || samples[1] != "PRJ-2" {
		t.Errorf("samples = %v, want [PRJ-1 PRJ-2]", samples)
	}
	if _, err := app.PatternMatches("["); err == nil {
		t.Error("PatternMatches must reject an invalid pattern")
	}
}

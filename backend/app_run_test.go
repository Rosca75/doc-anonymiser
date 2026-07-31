// app_run_test.go — Phase 8 Go tests: cancellation stops the pipeline
// between documents, and the fast re-run path applies a new entity without
// touching the LLM again.
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

func TestRunPipelineCancellation(t *testing.T) {
	app := NewApp()
	app.docs = []engine.Document{
		{Name: "1.txt", Format: engine.FormatTXT, Markdown: "one"},
		{Name: "2.txt", Format: engine.FormatTXT, Markdown: "two"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the run starts: it must stop immediately

	res, err := app.runPipelineBlocking(ctx, RunRequest{})
	if err != nil {
		t.Fatalf("cancellation is reported via the results, not an error here: %v", err)
	}
	if len(res.Documents) != 0 {
		t.Errorf("cancelled run processed %d documents, want 0", len(res.Documents))
	}
	if len(res.Report.Warnings) == 0 || !strings.Contains(res.Report.Warnings[0], "cancelled") {
		t.Errorf("report must record the cancellation: %+v", res.Report.Warnings)
	}
}

// TestFastRerunAppliesEntityWithoutLLM: after a first run that used the
// (mock) LLM, adding an entity and fast-rerunning must (a) apply the new
// entity, (b) keep existing placeholders stable via the session registry,
// and (c) make ZERO additional LLM calls.
func TestFastRerunAppliesEntityWithoutLLM(t *testing.T) {
	var chatCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"m"}]}`))
		case "/api/chat":
			chatCalls.Add(1)
			resp, _ := json.Marshal(map[string]interface{}{
				"message": map[string]string{"role": "assistant",
					"content": `{"entity_names":["Alpine Trust"],"project_names":[],"person_names":[]}`},
			})
			w.Write(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	app := NewApp()
	app.llm = ollama.New(srv.URL)
	// The deep scan is gated on the Local AI switch in Go (BUILD-06), so a
	// test that exercises it has to turn the route on, exactly like a user.
	app.settings.UseAI = true
	app.docs = []engine.Document{{
		Name: "a.txt", Format: engine.FormatTXT,
		Markdown: "Alpine Trust met Marie Duval by mail marie.duval@example.com.",
	}}

	// First run WITH deep-scan: the mock proposes "Alpine Trust".
	res1, err := app.runPipelineBlocking(context.Background(), RunRequest{UseDeepScan: true})
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	out1 := res1.Documents[0].Anonymised
	if !strings.Contains(out1, "[ENTITY_1]") || !strings.Contains(out1, "[EMAIL_1]") {
		t.Fatalf("first run output unexpected: %q", out1)
	}
	if strings.Contains(out1, "Marie Duval") == false {
		t.Fatalf("precondition: Marie Duval should survive the first run (no entity yet): %q", out1)
	}
	callsAfterFirst := chatCalls.Load()
	if callsAfterFirst == 0 {
		t.Fatal("the first run should have called the LLM")
	}

	// The user notices the missed person and adds them, then fast-reruns.
	res2, err := app.FastRerun(RunRequest{
		Entities:    []engine.Entity{{Category: "person_names", Canonical: "Marie Duval"}},
		UseDeepScan: true, // must be ignored by the fast path
	})
	if err != nil {
		t.Fatalf("fast rerun: %v", err)
	}
	out2 := res2.Documents[0].Anonymised
	if strings.Contains(out2, "Marie Duval") {
		t.Errorf("new entity not applied on fast rerun: %q", out2)
	}
	// Registry stability: the client and email keep their numbers.
	if !strings.Contains(out2, "[ENTITY_1]") || !strings.Contains(out2, "[EMAIL_1]") {
		t.Errorf("existing placeholders must stay stable: %q", out2)
	}
	if chatCalls.Load() != callsAfterFirst {
		t.Errorf("fast rerun must not call the LLM (calls went %d → %d)", callsAfterFirst, chatCalls.Load())
	}
	// The stored results are refreshed for the export screen.
	if app.GetResults() != res2 {
		t.Error("GetResults must return the latest run")
	}
}

func TestRunPipelineRejectsEmptyAndConcurrent(t *testing.T) {
	app := NewApp()
	if err := app.RunPipeline(RunRequest{}); err == nil {
		t.Error("running with no documents must fail actionably")
	}
	app.docs = []engine.Document{{Name: "a.txt", Format: engine.FormatTXT, Markdown: "x"}}
	app.running = true // simulate an in-flight run
	if err := app.RunPipeline(RunRequest{}); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Errorf("concurrent run must be rejected, got %v", err)
	}
	if _, err := app.FastRerun(RunRequest{}); err == nil {
		t.Error("fast rerun during a run must be rejected")
	}
}

// TestApplySettingsRoundTrip (BUILD-02 Phase 6): ContextSize, UseAI and
// Categories survive ApplySettings and reach the Ollama client / pipeline
// configuration.
func TestApplySettingsRoundTrip(t *testing.T) {
	app := NewApp()
	sel := engine.PresetSelection(engine.LevelSoft)
	sel["email"] = false

	// The probe will fail (no server on that port) but ApplySettings must
	// still store the settings; availability is status, not an error.
	_, err := app.ApplySettings(Settings{
		Level:       "soft",
		Categories:  sel,
		OllamaPort:  18434,
		Model:       "custom:3b",
		ContextSize: 16384,
		UseAI:       true,
	})
	if err != nil {
		t.Fatalf("ApplySettings: %v", err)
	}
	got := app.GetSettings()
	if got.ContextSize != 16384 || !got.UseAI || got.Model != "custom:3b" {
		t.Errorf("settings not stored: %+v", got)
	}
	if got.Categories["email"] || !got.Categories["entity_names"] {
		t.Errorf("categories not stored: %+v", got.Categories)
	}
	if app.llm.ContextSize != 16384 {
		t.Errorf("client ContextSize = %d, want 16384", app.llm.ContextSize)
	}

	// Invalid context size is rejected with an actionable message.
	if _, err := app.ApplySettings(Settings{Level: "soft", OllamaPort: 11434, ContextSize: -1}); err == nil {
		t.Error("negative context size must be rejected")
	}
}

// TestPipelineDonePayloadIncludesMapping (BUILD-02 Phase 10a): after a
// run, GetMapping resolves placeholders to originals, and the
// pipeline:done payload embeds the mapping next to the results fields.
func TestPipelineDonePayloadIncludesMapping(t *testing.T) {
	app := NewApp()
	app.docs = []engine.Document{{Name: "a.txt", Format: engine.FormatTXT,
		Markdown: "mail one@example.com from Alpine"}}

	res, err := app.runPipelineBlocking(context.Background(), RunRequest{
		Entities: []engine.Entity{{Category: "entity_names", Canonical: "Alpine"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Documents) != 1 {
		t.Fatal("run produced no results")
	}

	mapping := app.GetMapping()
	if info, ok := mapping["[EMAIL_1]"]; !ok || info.Original != "one@example.com" {
		t.Errorf("mapping missing the email entry: %+v", mapping)
	}
	if info, ok := mapping["[ENTITY_1]"]; !ok || info.Original != "Alpine" || info.Category != "entity_names" {
		t.Errorf("mapping missing the client entry: %+v", mapping)
	}

	// The payload embeds results fields AND the mapping side by side
	// (what the frontend actually receives after JSON serialisation).
	payload, err := json.Marshal(pipelineDonePayload{Results: res, Mapping: mapping})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["documents"]; !ok {
		t.Error("payload must keep the results fields inline")
	}
	if _, ok := decoded["mapping"]; !ok {
		t.Error("payload must carry the mapping")
	}
}

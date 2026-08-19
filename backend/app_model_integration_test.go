//go:build integration

// app_model_integration_test.go — which model a session actually posts to.
//
// TIER: integration (docs/TESTING.md). Every test here drives App.ProbeOllama
// and App.RunDetection against a MOCK Ollama server (httptest) whose /api/tags
// list is the variable, and asserts the model name that reaches the WIRE. That
// is bound-app wiring across the external boundary with a stand-in for the real
// server, which the integration tier owns; it stays hermetic (loopback httptest,
// zero real network).
//
// The invariant they guard: the effective model is always one the probe just
// saw. A model name is only a default if the machine has it, and a name it does
// not have fails at the very END of a run the user already waited for, reported
// as a per-file detection problem rather than as the configuration mistake it
// is. Asserting on the wire rather than on the settings is deliberate: the
// settings agreeing with themselves is not the property that was broken.
package backend

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"

	"doc-anonymiser/backend/engine"
	"doc-anonymiser/backend/ollama"
)

// modelServer is an Ollama stand-in serving a chosen /api/tags list and
// recording the model name of every /api/chat call, so a test can prove which
// model a run posted to rather than which one the settings held.
type modelServer struct {
	*httptest.Server
	mu     sync.Mutex
	models []string
	posted []string
}

func newModelServer(t *testing.T, installed ...string) *modelServer {
	t.Helper()
	ms := &modelServer{models: installed}
	ms.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			ms.mu.Lock()
			list := make([]map[string]string, 0, len(ms.models))
			for _, m := range ms.models {
				list = append(list, map[string]string{"name": m})
			}
			ms.mu.Unlock()
			body, _ := json.Marshal(map[string]interface{}{"models": list})
			w.Write(body)
		case "/api/chat":
			var req struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			ms.mu.Lock()
			ms.posted = append(ms.posted, req.Model)
			ms.mu.Unlock()
			resp, _ := json.Marshal(map[string]interface{}{
				"message": map[string]string{
					"role":    "assistant",
					"content": `{"entity_names":[],"person_names":[]}`,
				},
			})
			w.Write(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ms.Close)
	return ms
}

// postedModels is every model name the server was asked for, in order.
func (ms *modelServer) postedModels() []string {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return append([]string(nil), ms.posted...)
}

// port is the stand-in's loopback port, for the tests that go through
// ApplySettings: that method rebuilds the client from the port, so a test using
// it has to name the stand-in in the settings rather than only on the client.
func (ms *modelServer) port(t *testing.T) int {
	t.Helper()
	u, err := url.Parse(ms.URL)
	if err != nil {
		t.Fatalf("parsing the stand-in URL %q: %v", ms.URL, err)
	}
	n, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("the stand-in URL %q has no numeric port: %v", ms.URL, err)
	}
	return n
}

// modelApp is an App pointed at the given stand-in, with the Local AI route on
// and the offline route off so the only model traffic is the one under test.
func modelApp(t *testing.T, ms *modelServer, stored string) *App {
	t.Helper()
	app := NewApp()
	app.llm = ollama.New(ms.URL)
	app.settings.Model = stored
	app.llm.Model = stored
	app.settings.UseLocalAI = true
	app.settings.UseHeuristicDiscovery = false
	app.docs = []engine.Document{
		{Name: "a.txt", Format: engine.FormatTXT, Markdown: "Alpine Trust met Marie Duval."},
	}
	return app
}

// TestAFreshSessionRunsOnAnInstalledModel is the reported failure: nothing has
// ever been chosen, the pinned default is not installed, and the run must not
// post to a name the server does not have.
func TestAFreshSessionRunsOnAnInstalledModel(t *testing.T) {
	ms := newModelServer(t, "Qwen3.5-0.8B-Q8_0:latest", "Qwen3.5-4B-Q4_K_M:latest")
	app := modelApp(t, ms, "")

	state := app.ProbeOllama()
	if !state.Status.Available {
		t.Fatalf("the stand-in must probe as available, got %+v", state.Status)
	}
	if state.Model != "Qwen3.5-0.8B-Q8_0:latest" {
		t.Fatalf("with nothing stored and the pin absent, the first installed model must be resolved, got %q (installed %v)",
			state.Model, state.Status.Models)
	}

	if _, err := app.RunDetection([]string{"a.txt"}, nil, nil); err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	posted := ms.postedModels()
	if len(posted) == 0 {
		t.Fatal("the run posted no chat request, so there is nothing to assert about the model")
	}
	for i, got := range posted {
		if got != "Qwen3.5-0.8B-Q8_0:latest" {
			t.Errorf("request %d posted model %q, want the resolved %q: an uninstalled name fails the run at its end",
				i, got, "Qwen3.5-0.8B-Q8_0:latest")
		}
	}
	if got := app.GetSettings().Model; got != "Qwen3.5-0.8B-Q8_0:latest" {
		t.Errorf("settings.Model = %q, want the resolved model so the dropdown shows what will run", got)
	}
}

// TestThePinnedDefaultOutranksTagOrdering: the preference order is the contract,
// and the pin only loses to a choice the user made, never to the server's list.
func TestThePinnedDefaultOutranksTagOrdering(t *testing.T) {
	ms := newModelServer(t, "some-other-model:latest", ollama.DefaultModel)
	app := modelApp(t, ms, "")

	if got := app.ProbeOllama().Model; got != ollama.DefaultModel {
		t.Fatalf("resolved %q, want the pinned default %q: the first tag must not outrank the pin",
			got, ollama.DefaultModel)
	}
	if _, err := app.RunDetection([]string{"a.txt"}, nil, nil); err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	for i, got := range ms.postedModels() {
		if got != ollama.DefaultModel {
			t.Errorf("request %d posted %q, want %q", i, got, ollama.DefaultModel)
		}
	}
}

// TestAStoredModelStillInstalledIsLeftAlone: the user's choice outranks the pin,
// which is the whole reason the pin is a preference and not a rule.
func TestAStoredModelStillInstalledIsLeftAlone(t *testing.T) {
	ms := newModelServer(t, ollama.DefaultModel, "Qwen3.5-4B-Q4_K_M:latest")
	app := modelApp(t, ms, "Qwen3.5-4B-Q4_K_M:latest")

	if got := app.ProbeOllama().Model; got != "Qwen3.5-4B-Q4_K_M:latest" {
		t.Fatalf("resolved %q, want the stored choice kept: a pinned default must not overrule a model the user picked", got)
	}
	if _, err := app.RunDetection([]string{"a.txt"}, nil, nil); err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	for i, got := range ms.postedModels() {
		if got != "Qwen3.5-4B-Q4_K_M:latest" {
			t.Errorf("request %d posted %q, want the stored %q", i, got, "Qwen3.5-4B-Q4_K_M:latest")
		}
	}
}

// TestAStoredModelThatIsGoneIsReplaced: a model uninstalled since it was chosen
// (or restored from a session file written on another machine) is not a name to
// keep posting to.
func TestAStoredModelThatIsGoneIsReplaced(t *testing.T) {
	ms := newModelServer(t, ollama.DefaultModel)
	app := modelApp(t, ms, "a-model-nobody-has:latest")

	if got := app.ProbeOllama().Model; got != ollama.DefaultModel {
		t.Fatalf("resolved %q, want %q: a stored model the probe cannot see is not a model that can run",
			got, ollama.DefaultModel)
	}
}

// TestAFailedProbeDoesNotRewriteTheModel: an unreachable Ollama says nothing
// about which models exist, so it must not throw away the user's choice.
func TestAFailedProbeDoesNotRewriteTheModel(t *testing.T) {
	app := NewApp()
	// A port with nothing behind it: httptest hands out a live one, so this uses
	// a closed server's URL instead of guessing a free port.
	ms := newModelServer(t, ollama.DefaultModel)
	url := ms.URL
	ms.Close()

	app.llm = ollama.New(url)
	app.settings.Model = "Qwen3.5-4B-Q4_K_M:latest"

	state := app.ProbeOllama()
	if state.Status.Available {
		t.Fatalf("a closed server must probe as unavailable, got %+v", state.Status)
	}
	if state.Model != "Qwen3.5-4B-Q4_K_M:latest" {
		t.Errorf("resolved %q, want the stored model untouched: a stopped server must not erase a choice", state.Model)
	}
	if got := app.GetSettings().Model; got != "Qwen3.5-4B-Q4_K_M:latest" {
		t.Errorf("settings.Model = %q, want it untouched by a failed probe", got)
	}
}

// TestAServerWithNoModelsResolvesToNothing: there is nothing to run, so there is
// nothing to name. The probe's own Detail already says how to fix it.
func TestAServerWithNoModelsResolvesToNothing(t *testing.T) {
	ms := newModelServer(t)
	app := modelApp(t, ms, "")

	state := app.ProbeOllama()
	if !state.Status.Available {
		t.Fatalf("a server answering /api/tags is available even with no models, got %+v", state.Status)
	}
	if state.Model != "" {
		t.Errorf("resolved %q, want empty: inventing a model name for a server with none would put an unrunnable name in the dropdown", state.Model)
	}
}

// TestApplySettingsResolvesTheModelToo: the settings write is the other place a
// probe happens, and it is the one that can change the PORT, so the models
// reachable after it are not necessarily the ones the last probe saw.
func TestApplySettingsResolvesTheModelToo(t *testing.T) {
	ms := newModelServer(t, "Qwen3.5-0.8B-Q8_0:latest")
	app := modelApp(t, ms, "")

	s := app.GetSettings()
	s.Model = "a-model-nobody-has:latest"
	// ApplySettings rebuilds the client from the PORT, so the settings have to
	// name the stand-in or the probe inside it would reach a real Ollama (or
	// nothing) instead.
	s.OllamaPort = ms.port(t)
	state, err := app.ApplySettings(s)
	if err != nil {
		t.Fatalf("ApplySettings: %v", err)
	}
	if state.Model != "Qwen3.5-0.8B-Q8_0:latest" {
		t.Fatalf("ApplySettings resolved %q, want the installed %q", state.Model, "Qwen3.5-0.8B-Q8_0:latest")
	}
	if got := app.GetSettings().Model; got != "Qwen3.5-0.8B-Q8_0:latest" {
		t.Errorf("settings.Model = %q after ApplySettings, want the resolved name stored", got)
	}
}

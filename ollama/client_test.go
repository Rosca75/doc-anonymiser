// ollama/client_test.go — Phase 5 tests, all against httptest.Server mocks:
// ZERO real network calls (BUILD.md Phase 5 definition of done). The mock
// listens on 127.0.0.1, which is loopback, so even the local-only guarantee
// holds during tests.
package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"doc-anonymiser/engine"
)

// newTestClient points a Client at a mock server. httptest serves on
// 127.0.0.1:<random-port>, which passes the loopback check in New.
func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(srv.URL), srv
}

// chatReplyServer builds a mock that answers /api/tags happily and returns
// the given content string from /api/chat.
func chatReplyServer(t *testing.T, content string) *Client {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"qwen2.5:3b-instruct"}]}`))
		case "/api/chat":
			// Sanity-check the pinned request contract (CLAUDE.md §8):
			// format:json and stream:false.
			var req map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("chat request not JSON: %v", err)
			}
			if req["format"] != "json" || req["stream"] != false {
				t.Errorf("chat request must pin format:json, stream:false — got %v", req)
			}
			resp, _ := json.Marshal(map[string]interface{}{
				"message": map[string]string{"role": "assistant", "content": content},
			})
			w.Write(resp)
		default:
			http.NotFound(w, r)
		}
	})
	return c
}

func TestNewEnforcesLoopback(t *testing.T) {
	// A remote host must be rejected and silently replaced by the
	// loopback default — the local-only guarantee (CLAUDE.md §4).
	c := New("http://evil.example.com:11434")
	if c.BaseURL != DefaultBaseURL {
		t.Errorf("non-loopback host accepted: %s", c.BaseURL)
	}
	// Loopback with a custom port is the supported override.
	c2 := New("http://127.0.0.1:12345")
	if c2.BaseURL != "http://127.0.0.1:12345" {
		t.Errorf("loopback port override rejected: %s", c2.BaseURL)
	}
}

func TestProbeHappyPath(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"models":[{"name":"qwen2.5:3b-instruct"},{"name":"llama3.2:3b"}]}`))
	})
	st := c.Probe()
	if !st.Available || len(st.Models) != 2 || st.Models[0] != "qwen2.5:3b-instruct" {
		t.Errorf("probe mis-parsed: %+v", st)
	}
	models, err := c.ListModels()
	if err != nil || len(models) != 2 {
		t.Errorf("ListModels: %v %v", models, err)
	}
}

func TestProbeOllamaDown(t *testing.T) {
	// Point at a closed port: connection refused is the normal
	// "not installed / not started" state, never an error.
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // now nothing listens there

	c := New(url)
	st := c.Probe()
	if st.Available {
		t.Fatal("Probe claims availability with nothing listening")
	}
	// The detail must be actionable: name the address and the fix.
	if !strings.Contains(st.Detail, "ollama.com") || !strings.Contains(st.Detail, url) {
		t.Errorf("detail not actionable: %s", st.Detail)
	}
	if _, err := c.ListModels(); err == nil {
		t.Error("ListModels must fail when Ollama is down")
	}
}

func TestChatTooOld(t *testing.T) {
	// /api/tags works but /api/chat 404s → the pinned "too old" message
	// (CLAUDE.md §7).
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		http.NotFound(w, r)
	})
	_, err := c.Chat(context.Background(), "", "sys", "user")
	if err == nil || err.Error() != ErrTooOld {
		t.Errorf("want %q, got %v", ErrTooOld, err)
	}
}

func TestDiscoverHappyPath(t *testing.T) {
	text := "Alpine Trust hired PwC for Project Borealis. Contact Marie Duval."
	c := chatReplyServer(t, `{"client_names":["Alpine Trust"],"project_names":["Project Borealis"],"internal_names":[],"person_names":["Marie Duval"]}`)

	got, err := c.Discover(context.Background(), text)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := []engine.ProposedEntity{
		{Category: "client_names", Text: "Alpine Trust"},
		{Category: "project_names", Text: "Project Borealis"},
		{Category: "person_names", Text: "Marie Duval"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("proposal %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestDiscoverStripsCodeFences(t *testing.T) {
	text := "Alpine Trust appears here."
	c := chatReplyServer(t, "```json\n{\"client_names\":[\"Alpine Trust\"],\"project_names\":[],\"internal_names\":[],\"person_names\":[]}\n```")
	got, err := c.Discover(context.Background(), text)
	if err != nil || len(got) != 1 || got[0].Text != "Alpine Trust" {
		t.Errorf("fenced JSON not tolerated: %+v %v", got, err)
	}
}

func TestDiscoverMalformedReply(t *testing.T) {
	c := chatReplyServer(t, `the model rambles instead of emitting JSON`)
	_, err := c.Discover(context.Background(), "any text")
	if err == nil || !strings.Contains(err.Error(), "JSON") || !strings.Contains(err.Error(), "model") {
		t.Errorf("malformed reply needs an actionable error, got %v", err)
	}
}

func TestDeepScanHallucinationFilterAndAllowlist(t *testing.T) {
	text := "Residual mention of Borealis Fund and the CSSF here."
	c := chatReplyServer(t, `{"client_names":["Borealis Fund","Fabricated Corp","CSSF"],"project_names":[],"internal_names":[],"person_names":[]}`)
	// Wire the allowlist veto exactly as app.go does.
	allow := engine.NewAllowlist() // seeds CSSF
	c.Allow = allow.Contains

	got, err := c.DeepScan(context.Background(), text, []engine.Entity{{Category: "client_names", Canonical: "Alpine Trust"}})
	if err != nil {
		t.Fatalf("DeepScan: %v", err)
	}
	if len(got) != 1 || got[0].Text != "Borealis Fund" {
		t.Errorf("filter failed: want only Borealis Fund, got %+v", got)
	}
}

func TestDeepScanContextCancellation(t *testing.T) {
	// The mock hangs until the request context is cancelled, proving a
	// cancelled UI run aborts the HTTP call rather than waiting 120 s.
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.DeepScan(ctx, "text", nil)
	if err == nil {
		t.Fatal("cancelled DeepScan must return an error")
	}
}

func TestMergeProposals(t *testing.T) {
	a := []engine.ProposedEntity{
		{Category: "client_names", Text: "Alpine Trust"},
		{Category: "person_names", Text: "Marie Duval"},
	}
	b := []engine.ProposedEntity{
		{Category: "client_names", Text: "ALPINE TRUST"}, // dup, other case
		{Category: "client_names", Text: "Borealis Fund"},
		{Category: "person_names", Text: "  "}, // blank: dropped
	}
	got := MergeProposals(a, b)
	if len(got) != 3 {
		t.Fatalf("want 3 merged proposals, got %+v", got)
	}
	// First-seen spelling wins.
	if got[0].Text != "Alpine Trust" || got[2].Text != "Borealis Fund" {
		t.Errorf("merge order/spelling wrong: %+v", got)
	}
}

// TestPipelineWithOllamaClient wires the real Client (against the mock
// server) into engine.Run, proving the LLM slot end to end headlessly.
func TestPipelineWithOllamaClient(t *testing.T) {
	text := "Final note about Zephyr Capital."
	c := chatReplyServer(t, `{"client_names":["Zephyr Capital"],"project_names":[],"internal_names":[],"person_names":[]}`)

	res, err := engine.Run(context.Background(), engine.PipelineInput{
		Documents: []engine.Document{{Name: "z.txt", Format: engine.FormatTXT, Markdown: text}},
		Level:     engine.LevelMedium,
		Allowlist: engine.NewEmptyAllowlist(),
		LLM:       c,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := res.Documents[0].Anonymised
	if strings.Contains(out, "Zephyr") || !strings.Contains(out, "[CLIENT_1]") {
		t.Errorf("deep-scan finding not applied: %q", out)
	}
	if res.Report.LLMPass != "completed" {
		t.Errorf("report LLM note = %q", res.Report.LLMPass)
	}
}

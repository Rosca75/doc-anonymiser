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
	"sync/atomic"
	"testing"
	"unicode/utf8"

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

// --- BUILD-02 Phase 5: error mapping, num_ctx, chunking ---------------------

// TestChat400ContextOverflow: an HTTP 400 with a context-window error body
// must surface the context problem and must NOT claim the model is not
// installed (the reported bug this phase fixes).
func TestChat400ContextOverflow(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"the prompt exceeds the maximum context length"}`))
			return
		}
		w.Write([]byte(`{"models":[]}`))
	})
	_, err := c.Chat(context.Background(), "m", "sys", "user")
	if err == nil {
		t.Fatal("400 must be an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "context") {
		t.Errorf("400 context overflow must mention the context window: %q", msg)
	}
	if strings.Contains(msg, "not installed") {
		t.Errorf("400 must never be blamed on a missing model: %q", msg)
	}
}

// TestChat404ModelNotFound: a 404 whose body names the model means "not
// pulled", with the pull command in the message.
func TestChat404ModelNotFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"model 'missing:3b' not found, try pulling it first"}`))
			return
		}
		w.Write([]byte(`{"models":[]}`))
	})
	_, err := c.Chat(context.Background(), "missing:3b", "sys", "user")
	if err == nil || !strings.Contains(err.Error(), "not installed") ||
		!strings.Contains(err.Error(), "missing:3b") {
		t.Errorf("404 model-not-found must name the model and the fix, got %v", err)
	}
	if err != nil && err.Error() == ErrTooOld {
		t.Error("a model-not-found 404 must not be mistaken for an old Ollama")
	}
}

// TestChatNumCtx: num_ctx must travel in the request body when
// ContextSize is set, and stay absent when 0.
func TestChatNumCtx(t *testing.T) {
	var lastBody map[string]interface{}
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			// Decode into a FRESH map each call; decoding into an existing
			// map would merge keys and hide a removed "options" field.
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			lastBody = body
			resp, _ := json.Marshal(map[string]interface{}{
				"message": map[string]string{"role": "assistant", "content": "{}"},
			})
			w.Write(resp)
			return
		}
		w.Write([]byte(`{"models":[]}`))
	})

	c.ContextSize = 4096
	if _, err := c.Chat(context.Background(), "m", "s", "u"); err != nil {
		t.Fatal(err)
	}
	opts, ok := lastBody["options"].(map[string]interface{})
	if !ok || opts["num_ctx"] != float64(4096) {
		t.Errorf("num_ctx missing from request: %v", lastBody)
	}

	c.ContextSize = 0
	if _, err := c.Chat(context.Background(), "m", "s", "u"); err != nil {
		t.Fatal(err)
	}
	if _, present := lastBody["options"]; present {
		t.Errorf("options must be omitted when ContextSize is 0: %v", lastBody)
	}
}

// TestChunkText: table-driven chunker behaviour.
func TestChunkText(t *testing.T) {
	t.Run("empty text is one empty chunk", func(t *testing.T) {
		got := chunkText("", 100, 10)
		if len(got) != 1 || got[0] != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("text under budget is one chunk", func(t *testing.T) {
		got := chunkText("short text", 100, 10)
		if len(got) != 1 || got[0] != "short text" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("paragraph boundary preferred over mid-word", func(t *testing.T) {
		text := strings.Repeat("a", 60) + "\n\n" + strings.Repeat("b", 60)
		got := chunkText(text, 100, 8)
		if len(got) < 2 {
			t.Fatalf("expected a split, got %d chunk(s)", len(got))
		}
		if !strings.HasSuffix(got[0], "\n\n") {
			t.Errorf("first chunk must end at the paragraph break, got %q", got[0][len(got[0])-10:])
		}
	})
	t.Run("consecutive chunks overlap", func(t *testing.T) {
		words := strings.Repeat("word ", 100) // 500 bytes of words
		got := chunkText(words, 100, 20)
		if len(got) < 2 {
			t.Fatalf("expected several chunks, got %d", len(got))
		}
		// The tail of chunk 1 must reappear at the head of chunk 2.
		tail := got[0][len(got[0])-10:]
		if !strings.Contains(got[1][:30], tail[:5]) {
			t.Errorf("no overlap between chunks: %q ... %q", got[0], got[1])
		}
	})
	t.Run("rune safety on multi-byte French text", func(t *testing.T) {
		text := strings.Repeat("éàüöè ", 200) // 7 bytes per group
		for _, chunk := range chunkText(text, 128, 16) {
			if !utf8.ValidString(chunk) {
				t.Fatalf("chunk splits a UTF-8 sequence: %q", chunk)
			}
		}
	})
	t.Run("giant unbroken token still terminates", func(t *testing.T) {
		got := chunkText(strings.Repeat("x", 1000), 100, 10)
		if len(got) < 10 {
			t.Errorf("expected ~11 chunks, got %d", len(got))
		}
	})
}

// TestChunkCap: a document beyond the chunk cap fails with the actionable
// size message instead of running for an hour.
func TestChunkCap(t *testing.T) {
	c := New("")
	c.ContextSize = 512 // tiny budget: 512*3*3/4 = 1152 bytes per chunk
	huge := strings.Repeat("line of text\n", 20000) // ~260 KB >> 64 chunks
	if _, err := c.Chunks(huge); err == nil || !strings.Contains(err.Error(), "Smart detection") {
		t.Errorf("oversize document must fail with the split/smart-detection advice, got %v", err)
	}
	if n := c.EstimateChunks(huge); n <= MaxChunksPerDocument {
		t.Errorf("EstimateChunks must report the real chunk count, got %d", n)
	}
}

// TestDiscoverAcrossChunks: a 3-chunk document merges and deduplicates
// per-chunk proposals, and the hallucination filter runs against the FULL
// text (an entity only present in chunk 3 survives a chunk-1 proposal
// check).
func TestDiscoverAcrossChunks(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		// Every chunk proposes the same client (dedupe) plus one unique
		// name; "Zephyr Capital" is proposed by chunk 1 although it only
		// occurs later in the document (full-text filter must keep it).
		n := calls.Add(1)
		content := `{"client_names":["Alpine Trust","Zephyr Capital"],"project_names":[],"internal_names":[],"person_names":[]}`
		if n > 1 {
			content = `{"client_names":["Alpine Trust"],"project_names":[],"internal_names":[],"person_names":[]}`
		}
		resp, _ := json.Marshal(map[string]interface{}{
			"message": map[string]string{"role": "assistant", "content": content},
		})
		w.Write(resp)
	})

	c.ContextSize = 512 // 1152-byte chunks force several chunks
	text := "Alpine Trust opening. " + strings.Repeat("filler words here. ", 150) +
		" Zephyr Capital appears only at the end."
	got, err := c.Discover(context.Background(), text)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if calls.Load() < 2 {
		t.Fatalf("expected several chunk calls, got %d", calls.Load())
	}
	var names []string
	for _, p := range got {
		names = append(names, p.Text)
	}
	if len(got) != 2 || got[0].Text != "Alpine Trust" || got[1].Text != "Zephyr Capital" {
		t.Errorf("merged proposals wrong: %v", names)
	}
}

// TestDiscoverCancelBetweenChunks: chunk 2 answers normally but the user
// cancels during it; the loop must stop BEFORE chunk 3 (the between-chunk
// ctx check), returning the partial proposals with the context error.
func TestDiscoverCancelBetweenChunks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		if calls.Add(1) == 2 {
			cancel() // the user hits Cancel while chunk 2 is in flight
		}
		resp, _ := json.Marshal(map[string]interface{}{
			"message": map[string]string{"role": "assistant",
				"content": `{"client_names":["Alpine Trust"],"project_names":[],"internal_names":[],"person_names":[]}`},
		})
		w.Write(resp)
	})

	c.ContextSize = 512
	// Enough text for at least 3 chunks at the 1152-byte budget.
	text := "Alpine Trust opening. " + strings.Repeat("filler words here. ", 300)
	got, err := c.Discover(ctx, text)
	if err == nil {
		t.Fatal("cancelled discovery must return the context error")
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("loop must stop after the cancelled chunk, made %d calls", n)
	}
	if len(got) != 1 || got[0].Text != "Alpine Trust" {
		t.Errorf("partial proposals must survive cancellation: %+v", got)
	}
}

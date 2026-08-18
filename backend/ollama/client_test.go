// ollama/client_test.go — tests all against httptest.Server mocks:
// ZERO real network calls. The mock
// listens on 127.0.0.1, which is loopback, so even the local-only guarantee
// holds during tests.
package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"doc-anonymiser/backend/engine"
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
	text := "Alpine Trust hired Meridian Consulting for Project Borealis. Contact Marie Duval."
	c := chatReplyServer(t, `{"entity_names":["Alpine Trust"],"project_names":["Project Borealis"],"person_names":["Marie Duval"]}`)

	got, err := c.Discover(context.Background(), text)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := []engine.Suggestion{
		{Category: "entity_names", MainText: "Alpine Trust"},
		{Category: "project_names", MainText: "Project Borealis"},
		{Category: "person_names", MainText: "Marie Duval"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		// Compared field by field: a Suggestion carries slices, so
		// the struct is not comparable with ==. Discover proposes bare strings
		// and never folds, so an empty Variants list is part of the contract.
		if got[i].Category != want[i].Category || got[i].MainText != want[i].MainText {
			t.Errorf("proposal %d = %+v, want %+v", i, got[i], want[i])
		}
		if len(got[i].Spellings) != 0 {
			t.Errorf("proposal %d must carry no variants, got %v", i, got[i].Spellings)
		}
	}
}

func TestDiscoverStripsCodeFences(t *testing.T) {
	text := "Alpine Trust appears here."
	c := chatReplyServer(t, "```json\n{\"entity_names\":[\"Alpine Trust\"],\"project_names\":[],\"person_names\":[]}\n```")
	got, err := c.Discover(context.Background(), text)
	if err != nil || len(got) != 1 || got[0].MainText != "Alpine Trust" {
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

// TestDiscoverHallucinationFilterAndAllowlist: a name the model invented is
// dropped because it does not occur in the text, and an allowlisted term is
// dropped because the never-anonymise list vetoes every producer.
func TestDiscoverHallucinationFilterAndAllowlist(t *testing.T) {
	text := "Mention of Borealis Fund and the CSSF here."
	c := chatReplyServer(t, `{"entity_names":["Borealis Fund","Fabricated Corp","CSSF"],"project_names":[],"person_names":[]}`)
	// Wire the allowlist veto exactly as app.go does.
	allow := engine.NewAllowlist() // seeds CSSF
	c.Allow = allow.Contains

	got, err := c.Discover(context.Background(), text)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 1 || got[0].MainText != "Borealis Fund" {
		t.Errorf("filter failed: want only Borealis Fund, got %+v", got)
	}
}

func TestDiscoverContextCancellation(t *testing.T) {
	// The mock hangs until the request context is cancelled, proving a
	// cancelled UI run aborts the HTTP call rather than waiting 120 s.
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Discover(ctx, "text")
	if err == nil {
		t.Fatal("cancelled Discover must return an error")
	}
}

func TestMergeProposals(t *testing.T) {
	a := []engine.Suggestion{
		{Category: "entity_names", MainText: "Alpine Trust"},
		{Category: "person_names", MainText: "Marie Duval"},
	}
	b := []engine.Suggestion{
		{Category: "entity_names", MainText: "ALPINE TRUST"}, // dup, other case
		{Category: "entity_names", MainText: "Borealis Fund"},
		{Category: "person_names", MainText: "  "}, // blank: dropped
	}
	got := MergeSuggestions(a, b)
	if len(got) != 3 {
		t.Fatalf("want 3 merged proposals, got %+v", got)
	}
	// First-seen spelling wins.
	if got[0].MainText != "Alpine Trust" || got[2].MainText != "Borealis Fund" {
		t.Errorf("merge order/spelling wrong: %+v", got)
	}
}

// TestAnonymiseNeverCallsOllama: Anonymise is deterministic end to end. If a
// run could reach the model it could mint a value the user never reviewed,
// which is the review gate being walked past rather than enforced.
func TestAnonymiseNeverCallsOllama(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"{}"}}`))
	})
	_ = c // the client exists and is reachable; the pipeline must still not use it

	res, err := engine.Run(context.Background(), engine.PipelineInput{
		Documents: []engine.Document{{
			Name: "z.txt", Format: engine.FormatTXT,
			Markdown: "Final note about Zephyr Capital.",
		}},
		Values:    []engine.Value{{Category: engine.CatEntityNames, MainText: "Zephyr Capital"}},
		Level:     engine.LevelMedium,
		Allowlist: engine.NewEmptyAllowlist(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out := res.Documents[0].Anonymised; strings.Contains(out, "Zephyr") {
		t.Errorf("the declared value was not replaced: %q", out)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("Anonymise reached Ollama %d time(s); it must never call a discovery method", got)
	}
}

// --- error mapping, num_ctx, chunking ------------------------------------

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
	c.ContextSize = 512                             // tiny budget: 512*3*3/4 = 1152 bytes per chunk
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
		content := `{"entity_names":["Alpine Trust","Zephyr Capital"],"project_names":[],"person_names":[]}`
		if n > 1 {
			content = `{"entity_names":["Alpine Trust"],"project_names":[],"person_names":[]}`
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
		names = append(names, p.MainText)
	}
	if len(got) != 2 || got[0].MainText != "Alpine Trust" || got[1].MainText != "Zephyr Capital" {
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
			"message": map[string]string{
				"role":    "assistant",
				"content": `{"entity_names":["Alpine Trust"],"project_names":[],"person_names":[]}`,
			},
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
	if len(got) != 1 || got[0].MainText != "Alpine Trust" {
		t.Errorf("partial proposals must survive cancellation: %+v", got)
	}
}

// --- Suggestion classification -------------------------------------------

// TestClassifySuggestions: categories come back per suggestion; a name the
// server "invents" (not among the inputs) is dropped by the verbatim
// filter; allowlisted texts are vetoed.
func TestClassifySuggestions(t *testing.T) {
	c := chatReplyServer(t, `{"entity_names":["Alpine Trust","Fabricated Corp"],"project_names":[],"person_names":["Marie Duval","CSSF"]}`)
	allow := engine.NewAllowlist() // seeds CSSF
	c.Allow = allow.Contains

	got, err := c.ClassifySuggestions(context.Background(), []engine.Suggestion{
		{MainText: "Alpine Trust", Category: "person_names", Contexts: []string{"audit of Alpine Trust started"}},
		{MainText: "Marie Duval", Category: "entity_names"},
		{MainText: "CSSF", Category: "entity_names"},
	})
	if err != nil {
		t.Fatalf("ClassifySuggestions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 classified survivors, got %+v", got)
	}
	byText := map[string]string{}
	for _, p := range got {
		byText[p.MainText] = p.Category
	}
	if byText["Alpine Trust"] != "entity_names" || byText["Marie Duval"] != "person_names" {
		t.Errorf("classification wrong: %v", byText)
	}
	if _, ok := byText["Fabricated Corp"]; ok {
		t.Error("invented suggestion must be dropped by the verbatim filter")
	}
	if _, ok := byText["CSSF"]; ok {
		t.Error("allowlisted suggestion must be vetoed")
	}
}

// TestClassifySuggestionsBatching: 200 suggestions with contexts stay under
// the byte budget per request (several requests, each bounded).
func TestClassifySuggestionsBatching(t *testing.T) {
	var maxBody atomic.Int64
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		calls.Add(1)
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		for _, m := range req.Messages[1:] { // user message only
			if int64(len(m.Content)) > maxBody.Load() {
				maxBody.Store(int64(len(m.Content)))
			}
		}
		resp, _ := json.Marshal(map[string]interface{}{
			"message": map[string]string{
				"role":    "assistant",
				"content": `{"entity_names":[],"project_names":[],"person_names":[]}`,
			},
		})
		w.Write(resp)
	})

	c.ContextSize = 1024 // budget 2304 bytes per prompt
	var suggestions []engine.Suggestion
	for i := 0; i < 200; i++ {
		suggestions = append(suggestions, engine.Suggestion{
			MainText: strings.Repeat("N", 20) + string(rune('A'+i%26)),
			Contexts: []string{strings.Repeat("context words here ", 5)},
		})
	}
	if _, err := c.ClassifySuggestions(context.Background(), suggestions); err != nil {
		t.Fatalf("ClassifySuggestions: %v", err)
	}
	budget := c.ContextSize * 3 * 3 / 4
	if maxBody.Load() > int64(budget)+256 {
		t.Errorf("a batch exceeded the byte budget: %d > %d", maxBody.Load(), budget)
	}
	if calls.Load() < 2 {
		t.Errorf("200 padded suggestions must need several batches, got %d call(s)", calls.Load())
	}
}

// TestPromptsAndParserAgreeOnTheCategoryKeys is the parity guard for this file.
//
// Three lists have to name the same categories: the keys each prompt demands,
// the keys parseSuggestionJSON reads back, and the engine's own category set. A key
// in a prompt that parseSuggestionJSON does not know is dropped on parse; a key here
// that no prompt requests is a category the model is never asked to fill.
// Either way the category is dead and every test still passes, which is exactly
// how organisation_names survived for three phases.
func TestPromptsAndParserAgreeOnTheCategoryKeys(t *testing.T) {
	prompts := map[string]string{
		"discover": discoverSystemPrompt,
		"classify": classifySystemPrompt,
	}

	for _, category := range engine.AllValueCategories {
		// custom_patterns is the user's own regex; a model has nothing to say
		// about it, so it is deliberately outside this contract.
		if category == engine.CatCustomPatterns {
			continue
		}
		if !slices.Contains(promptCategories, category) {
			t.Errorf("the engine category %q is not in promptCategories, so the parser drops it", category)
		}
		for name, prompt := range prompts {
			if !strings.Contains(prompt, `"`+category+`"`) {
				t.Errorf("the %s prompt never asks for %q, so the model cannot fill it", name, category)
			}
		}
	}

	for _, category := range promptCategories {
		if !slices.Contains(engine.AllValueCategories, category) {
			t.Errorf("promptCategories names %q, which the engine does not have", category)
		}
	}
}

func TestParseEntityJSONAcceptsEveryKeyAndIgnoresUnknownOnes(t *testing.T) {
	reply := `{"entity_names":["Alpine Trust"],"project_names":["Atlas"],
	  "product_names":["Meridian Suite"],"brand_names":["Helios"],
	  "person_names":["Marie Duval"],"identifier_names":["INV-88213"],
	  "other_names":["Borealis"],"planet_names":["Mars"]}`

	got, err := parseSuggestionJSON(reply)
	if err != nil {
		t.Fatalf("parseSuggestionJSON: %v", err)
	}
	if len(got) != 7 {
		t.Fatalf("want one proposal per known key, got %d: %+v", len(got), got)
	}
	for _, p := range got {
		if p.MainText == "Mars" {
			t.Error("an unknown key must be ignored, not passed through")
		}
	}
}

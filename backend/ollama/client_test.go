// ollama/client_test.go — tests all against httptest.Server mocks:
// ZERO real network calls. The mock
// listens on 127.0.0.1, which is loopback, so even the local-only guarantee
// holds during tests.
package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

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
//
// It also asserts the pinned request contract on EVERY call it serves, which is
// most of the calls in this file. That is the point of putting the assertions
// here rather than in one dedicated test: the contract has to hold on the
// discovery call and the classification call alike, and a helper every path
// goes through is the only place that covers both without being remembered.
func chatReplyServer(t *testing.T, content string) *Client {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"qwen3.5:0.8b"}]}`))
		case "/api/chat":
			var req map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("chat request not JSON: %v", err)
			}
			assertPinnedChatRequest(t, req)
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

// assertPinnedChatRequest holds one decoded /api/chat body to the request
// contract this package guarantees (CLAUDE.md §8). Each assertion names the
// failure it prevents, because every one of them is a setting whose absence is
// silent: the request still succeeds and the reply is simply worse.
func assertPinnedChatRequest(t *testing.T, req map[string]interface{}) {
	t.Helper()

	if req["stream"] != false {
		t.Errorf("chat request must pin stream:false, got %v", req["stream"])
	}

	// think:false, at the TOP LEVEL. Ollama's options object is a map, so a
	// key it does not recognise there is dropped in silence: a think flag
	// nested in options reads as set on our side and never arrives, and the
	// model reasons for thousands of tokens before answering.
	if think, ok := req["think"].(bool); !ok || think {
		t.Errorf("chat request must send think:false at the top level, got %v", req["think"])
	}

	// keep_alive, also top level and for the same reason: it is not an entry
	// in the options map. Without it Ollama's five-minute default unloads the
	// model while the user reviews the previous run's suggestions.
	if keepAlive, ok := req["keep_alive"].(string); !ok || keepAlive == "" {
		t.Errorf("chat request must send a top-level keep_alive, got %v", req["keep_alive"])
	}

	opts, _ := req["options"].(map[string]interface{})
	for _, stray := range []string{"think", "keep_alive"} {
		if _, present := opts[stray]; present {
			t.Errorf("%q must not be nested in options: Ollama drops unknown option keys in silence, so it would never arrive", stray)
		}
	}

	assertSuggestionSchemaFormat(t, req["format"])
}

// assertSuggestionSchemaFormat checks that format carries the flat suggestion
// schema rather than the loose "json" string.
func assertSuggestionSchemaFormat(t *testing.T, format interface{}) {
	t.Helper()

	schema, ok := format.(map[string]interface{})
	if !ok {
		t.Fatalf("format must carry a JSON Schema object, got %#v", format)
	}
	if schema["type"] != "object" {
		t.Errorf("the schema must describe an object, got %v", schema["type"])
	}

	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("the schema must carry a properties object, got %#v", schema["properties"])
	}
	var required []string
	for _, r := range schema["required"].([]interface{}) {
		required = append(required, r.(string))
	}
	for _, category := range promptCategories {
		property, ok := properties[category].(map[string]interface{})
		if !ok {
			t.Errorf("the schema has no property for %q, so the model is forbidden to fill it", category)
			continue
		}
		items, _ := property["items"].(map[string]interface{})
		if property["type"] != "array" || items["type"] != "string" {
			t.Errorf("%q must be an array of strings in the schema, got %#v", category, property)
		}
		if !slices.Contains(required, category) {
			t.Errorf("%q is not in the schema's required list, which is what makes the model visit every category", category)
		}
	}

	// No $defs and no $ref, anywhere. Sub-4B models degrade badly on schemas
	// with reusable definitions: they echo the schema's own structure back in
	// place of the extracted values, so the reply parses and contains nothing.
	rendered, _ := json.Marshal(schema)
	for _, banned := range []string{"$defs", "$ref"} {
		if strings.Contains(string(rendered), banned) {
			t.Errorf("the schema must stay flat: %s makes a small model echo the schema back instead of filling it. Schema: %s",
				banned, rendered)
		}
	}
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
		w.Write([]byte(`{"models":[{"name":"qwen3.5:0.8b"},{"name":"llama3.2:3b"}]}`))
	})
	st := c.Probe()
	if !st.Available || len(st.Models) != 2 || st.Models[0] != "qwen3.5:0.8b" {
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
	_, err := c.Chat(context.Background(), "", "sys", "user", suggestionSchema())
	if err == nil || err.Error() != ErrTooOld {
		t.Errorf("want %q, got %v", ErrTooOld, err)
	}
}

func TestDiscoverHappyPath(t *testing.T) {
	text := "Alpine Trust hired Meridian Consulting for Project Borealis. Contact Marie Duval."
	c := chatReplyServer(t, `{"entity_names":["Alpine Trust"],"project_names":["Project Borealis"],"person_names":["Marie Duval"]}`)

	out, err := c.DiscoverSlices(context.Background(), []string{text}, text, nil)
	if err != nil {
		t.Fatalf("DiscoverSlices: %v", err)
	}
	got := out.Suggestions
	if out.Requests != 1 {
		t.Errorf("one slice must be one request, got %d", out.Requests)
	}
	if out.Silent != 0 {
		t.Errorf("a request that returned three names is not silent, got Silent=%d", out.Silent)
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
		// The Local AI score is stamped at the boundary, beside the provenance.
		// Without it the Suggestion arrives at 0, which the engine reads as
		// "not stated" and therefore as a user declaration at 0.95, and the
		// Minimum confidence control silently stops distinguishing the two.
		if got[i].Confidence != engine.ConfidenceLLMDefault {
			t.Errorf("proposal %d confidence = %v, want engine.ConfidenceLLMDefault (%v)",
				i, got[i].Confidence, engine.ConfidenceLLMDefault)
		}
	}
}

func TestDiscoverStripsCodeFences(t *testing.T) {
	text := "Alpine Trust appears here."
	c := chatReplyServer(t, "```json\n{\"entity_names\":[\"Alpine Trust\"],\"project_names\":[],\"person_names\":[]}\n```")
	out, err := c.DiscoverSlices(context.Background(), []string{text}, text, nil)
	if err != nil || len(out.Suggestions) != 1 || out.Suggestions[0].MainText != "Alpine Trust" {
		t.Errorf("fenced JSON not tolerated: %+v %v", out.Suggestions, err)
	}
}

func TestDiscoverMalformedReply(t *testing.T) {
	c := chatReplyServer(t, `the model rambles instead of emitting JSON`)
	_, err := c.DiscoverSlices(context.Background(), []string{"any text"}, "any text", nil)
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

	out, err := c.DiscoverSlices(context.Background(), []string{text}, text, nil)
	if err != nil {
		t.Fatalf("DiscoverSlices: %v", err)
	}
	if len(out.Suggestions) != 1 || out.Suggestions[0].MainText != "Borealis Fund" {
		t.Errorf("filter failed: want only Borealis Fund, got %+v", out.Suggestions)
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
	_, err := c.DiscoverSlices(ctx, []string{"text"}, "text", nil)
	if err == nil {
		t.Fatal("a cancelled scan must return an error")
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
	_, err := c.Chat(context.Background(), "m", "sys", "user", suggestionSchema())
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
	_, err := c.Chat(context.Background(), "missing:3b", "sys", "user", suggestionSchema())
	if err == nil || !strings.Contains(err.Error(), "not installed") ||
		!strings.Contains(err.Error(), "missing:3b") {
		t.Errorf("404 model-not-found must name the model and the fix, got %v", err)
	}
	if err != nil && err.Error() == ErrTooOld {
		t.Error("a model-not-found 404 must not be mistaken for an old Ollama")
	}
}

// recordingChatServer answers /api/chat with an empty JSON object and hands the
// decoded request body back through the callback, so a test can assert on
// exactly what was sent. A FRESH map is decoded per call: decoding into an
// existing one would merge keys and hide a field the request stopped sending.
func recordingChatServer(t *testing.T, record func(map[string]interface{})) *Client {
	t.Helper()
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			record(body)
			resp, _ := json.Marshal(map[string]interface{}{
				"message": map[string]string{"role": "assistant", "content": "{}"},
			})
			w.Write(resp)
			return
		}
		w.Write([]byte(`{"models":[]}`))
	})
	return c
}

// TestChatOptionsContract pins the whole options object, not only num_ctx.
//
// Every value here is one a model tag would otherwise supply, and every one of
// them is silent when it goes missing: the request still succeeds and the reply
// is simply slower, longer or different between two runs of the same page.
func TestChatOptionsContract(t *testing.T) {
	var lastBody map[string]interface{}
	c := recordingChatServer(t, func(body map[string]interface{}) { lastBody = body })

	c.ContextSize = 4096
	if _, err := c.Chat(context.Background(), "m", "s", "u", suggestionSchema()); err != nil {
		t.Fatal(err)
	}
	opts, ok := lastBody["options"].(map[string]interface{})
	if !ok {
		t.Fatalf("the request must carry an options object, got %v", lastBody)
	}
	if opts["num_ctx"] != float64(4096) {
		t.Errorf("num_ctx = %v, want 4096 (from ContextSize); body was %v", opts["num_ctx"], lastBody)
	}

	// The pinned extraction sampling. temperature is the one to read twice:
	// see TestChatTemperatureZeroIsNotDroppedByOmitempty.
	for key, want := range map[string]float64{
		"temperature":      0,
		"top_k":            1,
		"top_p":            1,
		"presence_penalty": 0,
		"repeat_penalty":   1,
		"seed":             float64(extractionSeed),
		"num_predict":      float64(maxReplyTokens),
	} {
		got, present := opts[key]
		if !present {
			t.Errorf("options.%s is missing, so the model keeps whatever its tag shipped with; options were %v", key, opts)
			continue
		}
		if got != want {
			t.Errorf("options.%s = %v, want %v", key, got, want)
		}
	}

	// ContextSize 0 means "let the model default apply", so num_ctx drops out
	// while the sampling options stay: they are ours to pin either way.
	c.ContextSize = 0
	if _, err := c.Chat(context.Background(), "m", "s", "u", suggestionSchema()); err != nil {
		t.Fatal(err)
	}
	opts, ok = lastBody["options"].(map[string]interface{})
	if !ok {
		t.Fatalf("the sampling options must survive ContextSize 0, got %v", lastBody)
	}
	if _, present := opts["num_ctx"]; present {
		t.Errorf("num_ctx must be omitted when ContextSize is 0, got %v", opts["num_ctx"])
	}
	if _, present := opts["temperature"]; !present {
		t.Errorf("the pinned sampling must not depend on ContextSize, options were %v", opts)
	}
}

// TestChatTemperatureZeroIsNotDroppedByOmitempty guards the reason every
// sampling field in chatOptions is a POINTER.
//
// The failure it prevents: a plain float64 with omitempty marshals 0 away, so
// the request would silently carry no temperature and the model would keep
// sampling at whatever its tag shipped with. Nothing else would notice: the
// call succeeds, the JSON parses, and two runs of one page merely stop
// agreeing with each other.
func TestChatTemperatureZeroIsNotDroppedByOmitempty(t *testing.T) {
	var raw []byte
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			raw, _ = io.ReadAll(r.Body)
			w.Write([]byte(`{"message":{"content":"{}"}}`))
			return
		}
		w.Write([]byte(`{"models":[]}`))
	})
	if _, err := c.Chat(context.Background(), "m", "s", "u", suggestionSchema()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"temperature":0`) {
		t.Errorf("temperature 0 must appear literally in the request body, got %s", raw)
	}
}

// TestChatThinkIsTopLevelAndFalse states, in its own name, the regression no
// other test would catch: nested in options the flag is dropped in SILENCE,
// because Ollama's options object is a map of unknown keys. The request would
// look correct on this side and the model would reason anyway.
func TestChatThinkIsTopLevelAndFalse(t *testing.T) {
	var lastBody map[string]interface{}
	c := recordingChatServer(t, func(body map[string]interface{}) { lastBody = body })

	if _, err := c.Chat(context.Background(), "m", "s", "u", suggestionSchema()); err != nil {
		t.Fatal(err)
	}
	think, ok := lastBody["think"].(bool)
	if !ok || think {
		t.Errorf("think must be top-level false, got %v (body %v)", lastBody["think"], lastBody)
	}
	opts, _ := lastBody["options"].(map[string]interface{})
	if _, present := opts["think"]; present {
		t.Error("think must NOT be inside options: an unrecognised key there is dropped in silence, so the flag never reaches Ollama")
	}
}

// TestDiscoverIgnoresAThinkingReply: a reply carrying BOTH a reasoning trace
// and valid JSON must be parsed from content. The two fields are separate, and
// reading the wrong one would turn a good answer into "the model rambled".
func TestDiscoverIgnoresAThinkingReply(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		resp, _ := json.Marshal(map[string]interface{}{
			"message": map[string]string{
				"role":     "assistant",
				"thinking": "Let me consider whether Alpine Trust is an organisation...",
				"content":  `{"entity_names":["Alpine Trust"],"project_names":[],"person_names":[]}`,
			},
		})
		w.Write(resp)
	})

	const text = "Alpine Trust appears here."
	out, err := c.DiscoverSlices(context.Background(), []string{text}, text, nil)
	if err != nil {
		t.Fatalf("DiscoverSlices: %v", err)
	}
	if len(out.Suggestions) != 1 || out.Suggestions[0].MainText != "Alpine Trust" {
		t.Errorf("the parser must read message.content, not message.thinking; got %+v", out.Suggestions)
	}
}

// TestWarmLoadsWithoutGenerating: Warm must load the weights and nothing else.
// An EMPTY messages array is the documented way to do that; num_predict 0 is
// not, which is why the assertion is on the messages array.
func TestWarmLoadsWithoutGenerating(t *testing.T) {
	var lastBody map[string]interface{}
	c := recordingChatServer(t, func(body map[string]interface{}) { lastBody = body })

	if err := c.Warm(context.Background()); err != nil {
		t.Fatalf("Warm on a healthy server must succeed: %v", err)
	}
	messages, ok := lastBody["messages"].([]interface{})
	if !ok || len(messages) != 0 {
		t.Errorf("Warm must post an EMPTY messages array, got %v", lastBody["messages"])
	}
	if keepAlive, ok := lastBody["keep_alive"].(string); !ok || keepAlive == "" {
		t.Errorf("Warm must send keep_alive, or the model it just loaded falls out of memory again: %v", lastBody)
	}
}

// TestWarmFailureIsNotFatal documents that Warm's error is safe to ignore,
// which is what App.warmLocalAI does: a warm-up that did not happen costs
// latency, never correctness.
func TestWarmFailureIsNotFatal(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens there any more

	if err := New(url).Warm(context.Background()); err == nil {
		t.Error("Warm against a closed port must report an error rather than pretend it warmed")
	}
}

// TestDiscoverAcrossSlices: several slices merge and deduplicate their
// proposals, and the hallucination filter runs against the WHOLE scanned text,
// so an entity that only occurs in the last slice survives a first-slice
// proposal.
func TestDiscoverAcrossSlices(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		// Every slice proposes the same client (dedupe); "Zephyr Capital" is
		// proposed by slice 1 although it only occurs in the last one, which
		// the whole-text filter must keep.
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

	slices := []string{
		"Alpine Trust opening.",
		strings.Repeat("filler words here. ", 20),
		"Zephyr Capital appears only at the end.",
	}
	source := strings.Join(slices, "\n")
	out, err := c.DiscoverSlices(context.Background(), slices, source, nil)
	if err != nil {
		t.Fatalf("DiscoverSlices: %v", err)
	}
	if out.Requests != len(slices) {
		t.Errorf("Requests must equal the number of slices sent: got %d, want %d", out.Requests, len(slices))
	}
	if got := int(calls.Load()); got != len(slices) {
		t.Errorf("the server must see one call per slice: got %d, want %d", got, len(slices))
	}
	if out.Silent != 0 {
		t.Errorf("every slice returned a name it could keep, so none is silent: Silent=%d", out.Silent)
	}
	var names []string
	for _, p := range out.Suggestions {
		names = append(names, p.MainText)
	}
	if len(out.Suggestions) != 2 || names[0] != "Alpine Trust" || names[1] != "Zephyr Capital" {
		t.Errorf("merged proposals wrong: %v", names)
	}
}

// TestTruncatedReplyIsReportedAsTruncation: a reply cut off at the generation
// cap is CUT OFF, not malformed. Reported as malformed it sends the user looking
// for a better model, when what they can actually do is send less text per
// request, which is what the message has to name. The message must not name the
// detail level, which is a remedy the user is already on by default.
func TestTruncatedReplyIsReportedAsTruncation(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		// What a degenerate reply that ran past num_predict actually looks like:
		// valid JSON up to the cut, then nothing.
		resp, _ := json.Marshal(map[string]interface{}{
			"message":     map[string]string{"role": "assistant", "content": `{"entity_names":["Alpine Trust","Borealis`},
			"done_reason": "length",
			"eval_count":  1024,
		})
		w.Write(resp)
	})

	_, err := c.Chat(context.Background(), "m", "system", "Alpine Trust and Borealis Fund.", nil)
	if err == nil {
		t.Fatal("a reply cut off at the generation cap must be reported, not returned as a whole answer")
	}
	var cut *TruncatedReplyError
	if !errors.As(err, &cut) {
		t.Fatalf("truncation must be its own error type, so a caller can salvage rather than abort: %T %v", err, err)
	}
	if cut.Content == "" {
		t.Error("the partial reply must travel on the error, or there is nothing to salvage")
	}
	msg := err.Error()
	if strings.Contains(msg, "JSON") {
		t.Errorf("truncation must not be reported as malformed JSON: %v", msg)
	}
	for _, want := range []string{"cut off", "1024", "fewer"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the truncation error must mention %q so the user knows what to change: %v", want, msg)
		}
	}
	// Thorough is already the default detail level, so naming it as the fix
	// tells the user to do what they are doing.
	for _, banned := range []string{"Thorough", "detail level"} {
		if strings.Contains(msg, banned) {
			t.Errorf("the truncation message must not name %q: it is already the default, so it is not a remedy: %v", banned, msg)
		}
	}
}

// TestTruncationDegradesOneSliceAndTheScanContinues is the guard for the whole
// of this behaviour: a cut-off reply must cost the user that ONE slice's tail
// and nothing else. Aborting the document instead means one dense page in the
// middle leaves every page after it unread, and the run reports a fraction of
// the document as though that were all there was.
func TestTruncationDegradesOneSliceAndTheScanContinues(t *testing.T) {
	var calls int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		n := atomic.AddInt32(&calls, 1)
		payload := map[string]interface{}{
			"message": map[string]string{"role": "assistant",
				"content": `{"entity_names":["Zephyr Capital"],"person_names":[]}`},
			"done_reason": "stop",
		}
		if n == 1 {
			// The first slice runs long: one name finished, the next cut in half.
			payload = map[string]interface{}{
				"message": map[string]string{"role": "assistant",
					"content": `{"entity_names":["Alpine Trust","Borea`},
				"done_reason": "length",
				"eval_count":  1024,
			}
		}
		resp, _ := json.Marshal(payload)
		w.Write(resp)
	})

	const source = "Alpine Trust and Borealis Fund. Zephyr Capital countersigned."
	out, err := c.DiscoverSlices(context.Background(),
		[]string{"Alpine Trust and Borealis Fund.", "Zephyr Capital countersigned."}, source, nil)
	if err != nil {
		t.Fatalf("a cut-off reply must degrade its own slice, not end the scan: %v", err)
	}
	if got := int(atomic.LoadInt32(&calls)); got != 2 {
		t.Fatalf("every slice must still be sent after a truncation, got %d request(s), want 2", got)
	}
	if out.Requests != 2 || out.Truncated != 1 {
		t.Errorf("want 2 requests with 1 truncated, got requests=%d truncated=%d", out.Requests, out.Truncated)
	}
	// A truncated request is NOT silent: it had more to say, which is the
	// opposite fact about the document.
	if out.Silent != 0 {
		t.Errorf("a truncated request must not also be counted silent, got silent=%d", out.Silent)
	}
	var names []string
	for _, s := range out.Suggestions {
		names = append(names, s.MainText)
	}
	slices.Sort(names)
	if !slices.Contains(names, "Alpine Trust") {
		t.Errorf("the names the model finished writing before the cut must be salvaged, got %v", names)
	}
	if !slices.Contains(names, "Zephyr Capital") {
		t.Errorf("the slice after the truncated one must still be scanned, got %v", names)
	}
	if slices.Contains(names, "Borea") {
		t.Errorf("the half-written name must not survive: the hallucination filter is what makes salvage safe, got %v", names)
	}
}

// TestSalvageSuggestionJSON: what a cut-off reply managed to finish saying is an
// answer. Every case here is a shape a real truncated reply took on the
// reference documents.
func TestSalvageSuggestionJSON(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		want  []string
	}{
		{
			name:  "nothing at all",
			reply: "",
			want:  nil,
		},
		{
			name:  "cut inside a name",
			reply: `{"entity_names":["Alpine Trust","Bore`,
			want:  []string{"Alpine Trust"},
		},
		{
			name:  "closed categories then a cut one",
			reply: `{"entity_names":["Alpine Trust"],"person_names":["Ada Byron","Bo`,
			want:  []string{"Ada Byron", "Alpine Trust"},
		},
		{
			name:  "cut just after a comma",
			reply: `{"person_names":["Ada Byron",`,
			want:  []string{"Ada Byron"},
		},
		{
			name:  "a fenced reply is still read",
			reply: "```json\n{\"entity_names\":[\"Alpine Trust\",\"Bore",
			want:  []string{"Alpine Trust"},
		},
		{
			name:  "a key the engine does not know is skipped",
			reply: `{"made_up_names":["Ignored"],"entity_names":["Alpine Trust`,
			want:  nil,
		},
		{
			name:  "a degenerate repeat loop yields its one string, over and over",
			reply: `{"entity_names":["Alpine Trust"],"project_names":["Loop","Loop","Loop","Lo`,
			want:  []string{"Alpine Trust", "Loop", "Loop", "Loop"},
		},
		{
			name:  "a complete reply reads exactly as the full parser reads it",
			reply: `{"entity_names":["Alpine Trust"],"person_names":["Ada Byron"]}`,
			want:  []string{"Ada Byron", "Alpine Trust"},
		},
	}
	for _, tc := range cases {
		t.Run("detection/"+tc.name, func(t *testing.T) {
			var got []string
			for _, s := range salvageSuggestionJSON(tc.reply) {
				got = append(got, s.MainText)
				if s.Confidence != engine.ConfidenceLLMDefault {
					t.Errorf("a salvaged suggestion must carry the model's confidence like any other, got %v", s.Confidence)
				}
				if !slices.Contains(s.DiscoveryMethods, engine.MethodLocalAI) {
					t.Errorf("a salvaged suggestion must say the model found it, got %v", s.DiscoveryMethods)
				}
			}
			slices.Sort(got)
			want := slices.Clone(tc.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("salvage of %q: got %v, want %v", tc.reply, got, want)
			}
		})
	}
}

// TestNormalReplyIsNotMistakenForTruncation: Ollama sends done_reason on every
// reply, so a "stop" must stay a clean answer.
func TestNormalReplyIsNotMistakenForTruncation(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		resp, _ := json.Marshal(map[string]interface{}{
			"message":     map[string]string{"role": "assistant", "content": `{"entity_names":["Alpine Trust"],"person_names":[]}`},
			"done_reason": "stop",
			"eval_count":  42,
		})
		w.Write(resp)
	})

	const text = "Alpine Trust signed."
	out, err := c.DiscoverSlices(context.Background(), []string{text}, text, nil)
	if err != nil {
		t.Fatalf("a reply that finished normally must not be an error: %v", err)
	}
	if len(out.Suggestions) != 1 {
		t.Errorf("want the one suggestion the model returned, got %+v", out.Suggestions)
	}
}

// TestDiscoverSlicesCountsSilence: a well-formed empty reply is DATA, not a
// failure. The counts are what let the caller tell "your model said nothing"
// from "this document holds nothing", which one number cannot do.
func TestDiscoverSlicesCountsSilence(t *testing.T) {
	c := chatReplyServer(t, `{"entity_names":[],"project_names":[],"product_names":[],"brand_names":[],"person_names":[],"identifier_names":[],"other_names":[]}`)

	slices := []string{"Alpine Trust opening.", "Borealis Fund replied."}
	out, err := c.DiscoverSlices(context.Background(), slices, strings.Join(slices, "\n"), nil)
	if err != nil {
		t.Fatalf("an empty but well-formed reply must not be an error: %v", err)
	}
	if out.Requests != 2 || out.Silent != 2 {
		t.Errorf("two empty replies must count as two silent requests: Requests=%d Silent=%d",
			out.Requests, out.Silent)
	}
	if len(out.Suggestions) != 0 {
		t.Errorf("an empty reply yields no suggestions, got %+v", out.Suggestions)
	}
}

// TestDiscoverSlicesSilenceIsJudgedAfterFiltering: a reply full of names the
// hallucination filter drops told the user nothing, so it counts as silence.
// Counting before the filter would report a busy model on a document where
// every suggestion was invented.
func TestDiscoverSlicesSilenceIsJudgedAfterFiltering(t *testing.T) {
	c := chatReplyServer(t, `{"entity_names":["Nowhere Corp","Invented SA"],"project_names":[],"person_names":[]}`)

	const text = "This document names nobody the model returned."
	out, err := c.DiscoverSlices(context.Background(), []string{text}, text, nil)
	if err != nil {
		t.Fatalf("DiscoverSlices: %v", err)
	}
	if out.Requests != 1 || out.Silent != 1 {
		t.Errorf("a reply whose every name was invented is silence: Requests=%d Silent=%d",
			out.Requests, out.Silent)
	}
}

// TestDiscoverSlicesReportsProgressBeforeEachRequest: onSlice fires BEFORE the
// request, which is the whole point of it: a caption that appears after the
// reply describes a wait that has already ended.
func TestDiscoverSlicesReportsProgressBeforeEachRequest(t *testing.T) {
	c := chatReplyServer(t, `{"entity_names":[],"project_names":[],"person_names":[]}`)

	var seen [][2]int
	slices := []string{"one", "two", "three"}
	if _, err := c.DiscoverSlices(context.Background(), slices, strings.Join(slices, "\n"),
		func(index, total int) { seen = append(seen, [2]int{index, total}) }); err != nil {
		t.Fatalf("DiscoverSlices: %v", err)
	}
	want := [][2]int{{0, 3}, {1, 3}, {2, 3}}
	if len(seen) != len(want) {
		t.Fatalf("onSlice must fire once per slice: got %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("progress %d = %v, want %v", i, seen[i], want[i])
		}
	}
}

// TestDiscoverCancelBetweenSlices: slice 2 answers normally but the user
// cancels during it; the loop must stop BEFORE slice 3 (the between-request ctx
// check), returning the partial proposals with the context error.
func TestDiscoverCancelBetweenSlices(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		if calls.Add(1) == 2 {
			cancel() // the user hits Cancel while slice 2 is in flight
		}
		resp, _ := json.Marshal(map[string]interface{}{
			"message": map[string]string{
				"role":    "assistant",
				"content": `{"entity_names":["Alpine Trust"],"project_names":[],"person_names":[]}`,
			},
		})
		w.Write(resp)
	})

	slices := []string{"Alpine Trust opening.", "second slice", "third slice"}
	out, err := c.DiscoverSlices(ctx, slices, strings.Join(slices, "\n"), nil)
	if err == nil {
		t.Fatal("cancelled discovery must return the context error")
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("loop must stop after the cancelled slice, made %d calls", n)
	}
	if len(out.Suggestions) != 1 || out.Suggestions[0].MainText != "Alpine Trust" {
		t.Errorf("partial proposals must survive cancellation: %+v", out.Suggestions)
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
// the byte budget per request (several requests, each bounded), and each row
// carries at most ONE trimmed context snippet.
//
// The snippet count is pinned rather than left incidental. A Suggestion may
// carry three, and on a document of one kind the second and third are usually
// the same sentence again: prompt tokens spent re-reading text the model saw in
// the line above. The byte budget alone would not notice them coming back,
// because more rows per request simply become more requests.
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
			for _, line := range strings.Split(strings.TrimSpace(m.Content), "\n") {
				_, context, found := strings.Cut(line, " | context: ")
				if !found {
					continue
				}
				if strings.Contains(context, " ... ") {
					t.Errorf("a classify row carries more than one context snippet: %q", line)
				}
				if n := len([]rune(context)); n > classifyContextRunes {
					t.Errorf("a context snippet is %d runes, want at most %d: %q", n, classifyContextRunes, context)
				}
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
// FOUR lists have to name the same categories: the keys each prompt demands,
// the keys parseSuggestionJSON reads back, the schema the reply is constrained
// to, and the engine's own category set. A key in a prompt that
// parseSuggestionJSON does not know is dropped on parse; a key here that no
// prompt requests is a category the model is never asked to fill; and a
// category missing from the schema is one the model is FORBIDDEN to fill, which
// is the quietest failure of the three. Either way the category is dead and
// every other test still passes, which is exactly how organisation_names
// survived for three phases.
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

	schema := suggestionSchema()
	properties, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]string)
	for _, category := range promptCategories {
		if !slices.Contains(engine.AllValueCategories, category) {
			t.Errorf("promptCategories names %q, which the engine does not have", category)
		}
		// The schema is what makes the category reachable at all: a property
		// the schema omits is one the grammar forbids the model to emit.
		if _, ok := properties[category]; !ok {
			t.Errorf("suggestionSchema has no property for %q, so the model is forbidden to fill it", category)
		}
		if !slices.Contains(required, category) {
			t.Errorf("suggestionSchema does not require %q, so the model may skip the category entirely", category)
		}
	}
	if len(properties) != len(promptCategories) || len(required) != len(promptCategories) {
		t.Errorf("the schema must describe exactly the prompt categories: %d properties and %d required, want %d of each",
			len(properties), len(required), len(promptCategories))
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

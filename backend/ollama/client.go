// Package ollama is THE ONLY package in this repository that talks to the
// local Ollama server over HTTP (see CLAUDE.md §4, "One-file external
// boundary"). Everything else — in particular engine/* — consumes the
// LLM boundary interface, never this concrete client. That keeps the planned
// P4 fallback (ONNX-in-WebView) a contained refactor.
//
// Local-only guarantee: the base URL host is locked to the loopback address
// 127.0.0.1. Only the port may be changed by the user in settings. Do not
// "improve" this into a configurable remote host — it would break the
// non-negotiable local-only guarantee in CLAUDE.md §4.
//
// API surface used (pinned in CLAUDE.md §7, "Ollama HTTP API as of 2026"):
//   - GET  /api/tags  — probe + model list
//   - POST /api/chat  — {"format":<JSON Schema>,"stream":false} completions
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"doc-anonymiser/backend/engine"
)

// DefaultBaseURL is the standard Ollama endpoint on the local machine.
// The host part is intentionally hardcoded to loopback (CLAUDE.md §8).
const DefaultBaseURL = "http://127.0.0.1:11434"

// DefaultModel is the settings DEFAULT only — the effective model is a
// user setting populated from /api/tags and must never be hardcoded
// anywhere else (CLAUDE.md §7).
//
// The pin is Apache-2.0 and ships as a Q8_0 build, both of which are
// requirements rather than incidentals: this tool reads client documents at a
// professional-services firm, so a research-only licence is a compliance
// problem, and a BF16 build has no fast CPU dot-product kernel without
// AVX512-BF16 (see the quantisation rule in CLAUDE.md §7). On the measured
// reference page it found sixteen names in 5.3 seconds, against the 4B's
// seventeen in 12.2.
const DefaultModel = "qwen3.5:0.8b"

// ErrTooOld is the pinned message for an Ollama old enough to miss
// /api/chat (CLAUDE.md §7: probe succeeds but chat 404s WITHOUT a
// model-not-found body; a 404 naming the model means the model is simply
// not pulled — see Chat).
const ErrTooOld = "Ollama too old, please update"

// DefaultContextSize is the default num_ctx sent to Ollama
// Phase 5b). 8192 replaces the implicit model default (often 2048 for
// small models), because whole document chunks are sent as prompts. The
// user can change it in Configure; 0 means "let the model default apply"
// (the option is omitted from the request).
const DefaultContextSize = 8192

// chunkOverlapBytes is how much consecutive chunks overlap so an entity
// name sitting exactly on a chunk boundary is still seen whole by at
// least one chunk.
const chunkOverlapBytes = 512

// MaxChunksPerDocument caps how many chunks one document may produce.
// Beyond this the scan would take unreasonably long on a small local
// model; the caller gets an actionable error instead.
const MaxChunksPerDocument = 64

// The sampling settings every request pins, rather than inheriting whatever
// the model tag was published with.
//
// Discovery and classification are TRANSCRIPTION tasks: every answer is text
// already present in the prompt, so there is nothing for creative sampling to
// contribute and a great deal for it to spoil. Greedy decoding also makes two
// runs over the same page agree, which is what lets the user's review of the
// first run mean anything about the second.
//
// The penalties are pinned NEUTRAL deliberately. A presence or repeat penalty
// punishes re-emitting a token that already appeared, and copying a name
// verbatim out of the document is precisely that, so a tag shipping a penalty
// pushes the model away from the one behaviour the prompt demands.
const (
	extractionTemperature     = 0.0
	extractionTopK            = 1
	extractionTopP            = 1.0
	extractionPresencePenalty = 0.0
	extractionRepeatPenalty   = 1.0
	// A fixed seed, so a re-run of one page produces the same suggestion list.
	// Any value works; what matters is that it never changes between runs.
	extractionSeed = 7
)

// maxReplyTokens caps how much the model may generate for one chunk. The
// schema-constrained replies this client asks for run to roughly a hundred
// tokens, so the cap is a runaway guard rather than a budget: it stops a
// degenerate reply holding chatClient's timeout open for two minutes.
//
// It is only safe BECAUSE every request sets think:false. With thinking on the
// reasoning trace is generated first and spends the whole budget, so the cap
// makes matters worse: the JSON is then truncated or never begins.
const maxReplyTokens = 512

// detectionKeepAlive is how long Ollama holds the model in memory after a
// reply. Ollama's own default is five minutes, which is shorter than the time a
// user spends reviewing one run's suggestions, so the next run pays the model
// load again. It is a duration and never -1: see chatRequest.KeepAlive.
const detectionKeepAlive = "30m"

// warmTimeout bounds Warm. A pre-load that has not finished in this long is
// not worth waiting for: it is an optimisation, and the run it would have
// helped can pay the load itself.
const warmTimeout = 30 * time.Second

// OllamaStatus is the result of probing the local Ollama server. It is sent
// to the frontend as-is (via app.go), so field names are chosen to read well
// in JavaScript after Wails' JSON serialisation.
type OllamaStatus struct {
	// Available is true when GET /api/tags answered successfully.
	Available bool `json:"available"`
	// Models lists the model names installed in Ollama (e.g.
	// "qwen3.5:0.8b"). Empty when Available is false.
	Models []string `json:"models"`
	// Detail is a human-readable explanation of the status, shown in the
	// UI tooltip. It must always be actionable (what failed, how to fix).
	Detail string `json:"detail"`
}

// Client talks to a local Ollama server using only the Go standard library.
type Client struct {
	// BaseURL of the Ollama server, e.g. "http://127.0.0.1:11434".
	// Constructed from settings; the host always remains loopback.
	BaseURL string
	// Model is the chat model to use; a user setting defaulting to
	// DefaultModel and normally picked from the ListModels() dropdown.
	Model string
	// Allow, when set, vetoes LLM suggestions (wired by app.go to the
	// session allowlist's Contains). The engine applies the allowlist
	// again — belt and braces, because CLAUDE.md §5 says the allowlist
	// wins in EVERY pass.
	Allow func(string) bool
	// ContextSize is the num_ctx option sent with every chat request
	// Defaults to DefaultContextSize in New; 0 omits
	// the option so the model default applies. It also drives the
	// document chunk budget (promptBudgetBytes).
	ContextSize int

	// probeClient carries a short timeout so a missing Ollama never hangs
	// the UI; chatClient allows slow small-model generations (120 s,
	// ) — both honour context cancellation on top.
	probeClient *http.Client
	chatClient  *http.Client
}

// New returns a Client for the given base URL (pass "" for the default).
// Any non-loopback host is REJECTED and replaced by the default: the
// local-only guarantee is enforced here, not merely documented.
func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if u, err := url.Parse(baseURL); err != nil ||
		(u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL:     baseURL,
		Model:       DefaultModel,
		ContextSize: DefaultContextSize,
		probeClient: &http.Client{Timeout: 2 * time.Second},
		chatClient:  &http.Client{Timeout: 120 * time.Second},
	}
}

// tagsResponse mirrors just the part of the GET /api/tags JSON body we need:
//
//	{"models": [{"name": "qwen3.5:0.8b", ...}, ...]}
type tagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// Probe checks whether Ollama is reachable and which models it has, by
// calling GET /api/tags (the cheapest "are you there?" endpoint). It never
// returns an error: unavailability is a normal state expressed through
// OllamaStatus, because the app must degrade gracefully (CLAUDE.md §4).
func (c *Client) Probe() OllamaStatus {
	resp, err := c.probeClient.Get(c.BaseURL + "/api/tags")
	if err != nil {
		// Typical case: nothing listening on the port (Ollama not
		// installed or not started). The Detail string doubles as the
		// UI tooltip, so it says how to fix the situation.
		return OllamaStatus{
			Available: false,
			Detail: fmt.Sprintf(
				"Ollama not detected on %s, install it from ollama.com and start it to enable the AI features (the app works fine without it). Technical detail: %v",
				c.BaseURL, err),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return OllamaStatus{
			Available: false,
			Detail: fmt.Sprintf(
				"Something answered on %s but not like Ollama (GET /api/tags returned HTTP %d, expected 200). Check that the port in settings really belongs to Ollama.",
				c.BaseURL, resp.StatusCode),
		}
	}

	var tags tagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return OllamaStatus{
			Available: false,
			Detail: fmt.Sprintf(
				"Ollama answered on %s but its /api/tags response could not be parsed (%v). Try updating Ollama to a recent version.",
				c.BaseURL, err),
		}
	}

	names := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		names = append(names, m.Name)
	}

	detail := fmt.Sprintf("Ollama detected on %s with %d model(s).", c.BaseURL, len(names))
	if len(names) == 0 {
		detail = fmt.Sprintf(
			"Ollama detected on %s but no models are installed, run 'ollama pull %s' to enable the AI features.",
			c.BaseURL, DefaultModel)
	}
	return OllamaStatus{Available: true, Models: names, Detail: detail}
}

// ListModels returns the installed model names (for the settings dropdown).
// Unlike Probe it DOES return an error, because the caller explicitly asked
// for models and deserves to know why there are none.
func (c *Client) ListModels() ([]string, error) {
	status := c.Probe()
	if !status.Available {
		return nil, fmt.Errorf("%s", status.Detail)
	}
	return status.Models, nil
}

// --- /api/chat ----------------------------------------------------------

// chatRequest is the POST /api/chat body (CLAUDE.md §8). stream=false gives
// one complete reply; the rest of the fields exist to make a small local model
// behave like an extraction engine rather than a chat partner.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	// Format is one of two things: the string "json", which is Ollama's loose
	// JSON mode, or a JSON Schema object, which constrains the reply's SHAPE
	// as well as its syntax. The callers in this file send the schema
	// (suggestionSchema); the type is any so the looser mode stays expressible
	// and so a request that wants no formatting at all can omit it.
	Format any  `json:"format,omitempty"`
	Stream bool `json:"stream"`
	// Think disables a reasoning model's hidden reasoning. It is a TOP-LEVEL
	// field because Ollama's own options object is a map: a key it does not
	// recognise there is dropped in silence, so a think flag nested in options
	// reads as "set" on this side and never arrives.
	//
	// It is load-bearing rather than a tuning knob. Setting format does NOT stop
	// a reasoning model reasoning: the grammar is applied only once the model
	// transitions from thinking to content, so with thinking on the whole
	// reasoning trace is generated first, unconstrained, at full cost, and
	// returned in a field this client does not even read. On a small model asked
	// to extract names from one page that trace ran to thousands of tokens and
	// exhausted the reply budget, so the JSON never arrived.
	//
	// A pointer, so "unset" and "deliberately false" stay distinct: an omitted
	// field means "let the model decide", exactly as Options already works.
	Think *bool `json:"think,omitempty"`
	// KeepAlive is how long Ollama holds the model in memory after the reply.
	// TOP-LEVEL for the same reason as Think: it is not an entry in the options
	// map. Ollama's own default is five minutes, which is shorter than a user
	// spends reviewing suggestions between two runs, so every second run pays
	// the model load again. It is deliberately NOT -1 (load until the process
	// exits): this is a desktop application on someone's work laptop, and
	// pinning a model in RAM until reboot is not ours to decide.
	KeepAlive string `json:"keep_alive,omitempty"`
	// Options carries model options; NumCtx sets the context window. A
	// nil pointer omits the object entirely so the model default applies
	// (a pointer with omitempty gives an unambiguous "unset" state).
	Options *chatOptions `json:"options,omitempty"`
}

// chatOptions mirrors the Ollama options object.
//
// Every sampling field is a pointer because "unset" and "deliberately zero" are
// different requests and a plain float with omitempty cannot tell them apart:
// temperature 0 is exactly the value extraction wants, and it would marshal
// away.
type chatOptions struct {
	NumCtx     int `json:"num_ctx,omitempty"`
	NumPredict int `json:"num_predict,omitempty"`
	// Extraction is a transcription task, not a creative one: the answer is
	// text already present in the prompt. Greedy decoding makes two runs of
	// one document agree, which is what lets the review gate mean something.
	Temperature *float64 `json:"temperature,omitempty"`
	TopK        *int     `json:"top_k,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	Seed        *int     `json:"seed,omitempty"`
	// A presence penalty punishes re-emitting a token that already appeared.
	// Copying a name verbatim out of the document IS re-emitting tokens that
	// already appeared, so a model tag shipping a presence penalty works
	// against the task and lengthens the reply. Both penalties are pinned
	// neutral here rather than left to whatever the tag was built with.
	PresencePenalty *float64 `json:"presence_penalty,omitempty"`
	RepeatPenalty   *float64 `json:"repeat_penalty,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// Thinking is where Ollama puts a reasoning model's hidden trace. Nothing
	// in this client reads it for meaning: it is decoded so that a reply which
	// still carries reasoning can be SEEN rather than silently discarded.
	// omitempty matters because this same struct carries the messages SENT;
	// without it every outgoing message would ship an empty thinking key.
	Thinking string `json:"thinking,omitempty"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
	Error   string      `json:"error"`
}

// Chat sends one system+user exchange and returns the assistant's raw
// content. format is what the reply is constrained to (see chatRequest.Format);
// the callers in this file pass suggestionSchema(). ctx cancellation aborts the
// request mid-flight (the pipeline cancel button relies on this).
func (c *Client) Chat(ctx context.Context, model, systemPrompt, userPrompt string, format any) (string, error) {
	if model == "" {
		model = c.Model
	}
	reqBody := c.buildChatRequest(model, []chatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}, format)

	out, err := c.postChat(ctx, c.chatClient, model, reqBody)
	if err != nil {
		return "", err
	}
	return out.Message.Content, nil
}

// Warm loads the model into Ollama's memory without asking it to generate
// anything, so the first real detection run of a session does not begin with a
// multi-second model load inside a wait the user is watching.
//
// An EMPTY messages array is the documented way to load a model without
// generating; num_predict 0 is not, so do not reach for that instead.
//
// It is cheap to call and can never hang the caller: it carries its own short
// timeout on top of ctx. Its error is safe to ignore, because a warm-up that
// did not happen costs latency and not correctness.
func (c *Client) Warm(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, warmTimeout)
	defer cancel()

	// No format: there is no reply to shape. keep_alive and the model name are
	// the whole point of the call, and both come from the shared builder so the
	// warm-up cannot hold the model for a different period than a real call.
	req := c.buildChatRequest(c.Model, []chatMessage{}, nil)
	_, err := c.postChat(ctx, c.chatClient, c.Model, req)
	return err
}

// buildChatRequest is the ONE place a POST /api/chat body is shaped, so the
// discovery call, the classification call and the warm-up cannot drift apart
// on the settings that decide how the model behaves.
func (c *Client) buildChatRequest(model string, messages []chatMessage, format any) chatRequest {
	// Addressable copies: every sampling option is a pointer so that a
	// deliberate zero survives omitempty (see chatOptions).
	think := false
	temperature := extractionTemperature
	topK := extractionTopK
	topP := extractionTopP
	seed := extractionSeed
	presencePenalty := extractionPresencePenalty
	repeatPenalty := extractionRepeatPenalty

	req := chatRequest{
		Model:     model,
		Messages:  messages,
		Format:    format,
		Stream:    false,
		Think:     &think,
		KeepAlive: detectionKeepAlive,
		Options: &chatOptions{
			NumPredict:      maxReplyTokens,
			Temperature:     &temperature,
			TopK:            &topK,
			TopP:            &topP,
			Seed:            &seed,
			PresencePenalty: &presencePenalty,
			RepeatPenalty:   &repeatPenalty,
		},
	}
	if c.ContextSize > 0 {
		// num_ctx only travels when explicitly configured; 0 keeps the
		// model default.
		req.Options.NumCtx = c.ContextSize
	}
	return req
}

// postChat sends one built request and maps Ollama's answer onto an actionable
// error or a decoded reply. Chat and Warm share it so there is one description
// of what each HTTP status means.
func (c *Client) postChat(ctx context.Context, httpClient *http.Client, model string, reqBody chatRequest) (chatResponse, error) {
	var out chatResponse
	body, err := json.Marshal(reqBody)
	if err != nil {
		return out, fmt.Errorf("could not build the Ollama request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return out, fmt.Errorf("could not build the Ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return out, fmt.Errorf(
			"could not reach Ollama on %s (%v), check that Ollama is still running, then re-probe in settings", c.BaseURL, err)
	}
	defer resp.Body.Close()

	// Distinct error surfaces per status. The old
	// two-branch mapping wrongly blamed "model not installed" for ANY
	// non-200, including HTTP 400 context overflows — never again.
	if resp.StatusCode != http.StatusOK {
		// Ollama sends {"error":"..."} bodies; read a bounded excerpt.
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var apiErr struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(bodyBytes, &apiErr)

		switch resp.StatusCode {
		case http.StatusNotFound:
			// A 404 whose body names the model means "not pulled";
			// a bare 404 means the /api/chat endpoint itself is missing,
			// which is the pinned pre-chat-API "too old" case.
			if strings.Contains(strings.ToLower(apiErr.Error), "model") {
				return out, fmt.Errorf(
					"model %q is not installed; run 'ollama pull %s' or pick another model in settings", model, model)
			}
			return out, fmt.Errorf("%s", ErrTooOld)
		case http.StatusBadRequest:
			msg := fmt.Sprintf("Ollama rejected the request (HTTP 400): %s", apiErr.Error)
			low := strings.ToLower(apiErr.Error)
			if strings.Contains(low, "context") || strings.Contains(low, "length") {
				msg += " The document chunk was too large for the model's context window; lower the chunk size or raise the context size in Configure."
			}
			return out, fmt.Errorf("%s", msg)
		default:
			excerpt := apiErr.Error
			if excerpt == "" {
				excerpt = strings.TrimSpace(string(bodyBytes))
				if len(excerpt) > 200 {
					excerpt = excerpt[:200]
				}
			}
			return out, fmt.Errorf(
				"Ollama answered HTTP %d on /api/chat (expected 200): %s; check that the Ollama server is healthy, then re-probe in settings",
				resp.StatusCode, excerpt)
		}
	}

	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, fmt.Errorf("Ollama's /api/chat reply could not be parsed (%v), try updating Ollama", err)
	}
	if out.Error != "" {
		return out, fmt.Errorf("Ollama reported an error: %s, the model %q may not be installed; run 'ollama pull %s'", out.Error, model, model)
	}
	return out, nil
}

// --- Discovery (Phase-A prompt) ------------------------------------------

// discoverSystemPrompt demands STRICT JSON with the exact CLAUDE.md §5
// category keys. The "verbatim" instruction feeds the hallucination filter:
// anything not copied exactly from the text is dropped afterwards anyway.
const discoverSystemPrompt = `You are an entity extraction engine for confidential business documents.
Extract proper names from the user's document and respond with ONLY a JSON object, no prose, using exactly these keys:
{"entity_names": [], "project_names": [], "product_names": [], "brand_names": [], "person_names": [], "identifier_names": [], "other_names": []}
Rules:
- entity_names: named organisations, companies, teams and internal systems, whether they are clients, counterparties or internal.
- project_names: engagement, project or workstream names and code names.
- product_names: named products, platforms, modules and software.
- brand_names: brand or trade names, which are how something is marketed rather than the company that owns it.
- person_names: every natural person, including members of staff. A human being is NEVER an entity_names.
- identifier_names: reference, contract, invoice and case codes.
- other_names: a proper name that is none of the above. Use it sparingly, and never as a place to put something you could file elsewhere.
- Copy every name VERBATIM from the document. Never invent, translate or reformat names.
- Use [] for a category with no findings.`

// Discover runs the Phase-A prompt on one document's text and returns raw
// category → names suggestions for the review screen. Long documents are
// CHUNKED: each chunk is scanned in sequence, ctx
// cancellation is honoured between chunks, per-chunk suggestions merge
// through MergeSuggestions, and the hallucination filter runs against the
// FULL document text (an entity split across a boundary is covered by the
// chunk overlap). The caller (app.go) merges multi-file results.
func (c *Client) Discover(ctx context.Context, text string) ([]engine.Suggestion, error) {
	return c.DiscoverWithProgress(ctx, text, nil)
}

// DiscoverWithProgress is Discover with a per-chunk callback, so a long
// document reports progress instead of sitting frozen on one caption for
// minutes. onChunk is called BEFORE each chunk is sent, with the
// 0-based index and the total; nil disables it.
func (c *Client) DiscoverWithProgress(ctx context.Context, text string, onChunk func(index, total int)) ([]engine.Suggestion, error) {
	return c.scanChunks(ctx, text, onChunk, func(chunk string) (string, error) {
		return c.Chat(ctx, c.Model, discoverSystemPrompt, chunk, suggestionSchema())
	})
}

// scanChunks is the chunk loop behind Discover: chunk, chat per chunk via
// chat(), parse, merge, then hallucination-filter against the whole document. On
// mid-loop cancellation the Suggestions gathered so far are returned WITH the
// context error, so callers can keep partial results.
func (c *Client) scanChunks(ctx context.Context, text string, onChunk func(index, total int), chat func(chunk string) (string, error)) ([]engine.Suggestion, error) {
	chunks, err := c.Chunks(text)
	if err != nil {
		return nil, err
	}
	var batches [][]engine.Suggestion
	for i, chunk := range chunks {
		if onChunk != nil {
			onChunk(i, len(chunks))
		}
		if err := ctx.Err(); err != nil {
			return c.filterSuggestions(mergeBatches(batches), text), err
		}
		reply, err := chat(chunk)
		if err != nil {
			return c.filterSuggestions(mergeBatches(batches), text), err
		}
		suggestions, err := parseSuggestionJSON(reply)
		if err != nil {
			return c.filterSuggestions(mergeBatches(batches), text), err
		}
		batches = append(batches, suggestions)
	}
	// Hallucination filter (CLAUDE.md §5) against the FULL text plus the
	// allowlist veto.
	return c.filterSuggestions(mergeBatches(batches), text), nil
}

// mergeBatches is MergeSuggestions over a batch slice (a readability helper for
// scanChunks).
func mergeBatches(batches [][]engine.Suggestion) []engine.Suggestion {
	return MergeSuggestions(batches...)
}

// filterSuggestions applies the hallucination filter (exact string must occur
// in the source text) and the allowlist veto. The engine repeats both
// checks — defence in depth, not redundancy to remove.
func (c *Client) filterSuggestions(suggestions []engine.Suggestion, sourceText string) []engine.Suggestion {
	var out []engine.Suggestion
	for _, p := range suggestions {
		if !strings.Contains(sourceText, p.MainText) {
			continue // hallucinated
		}
		if c.Allow != nil && c.Allow(p.MainText) {
			continue // allowlist wins
		}
		out = append(out, p)
	}
	return out
}

// MergeSuggestions merges discovery results through the engine's single merge
// rule. It is re-exported here only because this file's callers are inside the
// Ollama boundary; the RULE lives in the engine so every producer shares one.
func MergeSuggestions(batches ...[]engine.Suggestion) []engine.Suggestion {
	return engine.MergeSuggestions(batches...)
}

// --- Suggestion classification -------------------------------------------

// classifySystemPrompt asks for one category per suggestion, strict JSON
// with the exact category keys. Only suggestion TEXTS and short context
// snippets are sent, never whole documents, which structurally ends the
// context-overflow class of bugs for this path.
const classifySystemPrompt = `You are an entity classification engine for confidential business documents.
The user sends a list of suggestion names, each with short context snippets from the document.
Assign every suggestion to exactly ONE category and respond with ONLY a JSON object, no prose, using exactly these keys:
{"entity_names": [], "project_names": [], "product_names": [], "brand_names": [], "person_names": [], "identifier_names": [], "other_names": []}
Rules:
- entity_names: named organisations, companies, teams and internal systems, whether they are clients, counterparties or internal.
- project_names: engagement, project or workstream names and code names.
- product_names: named products, platforms, modules and software.
- brand_names: brand or trade names, which are how something is marketed rather than the company that owns it.
- person_names: every natural person, including members of staff. A human being is NEVER an entity_names.
- identifier_names: reference, contract, invoice and case codes.
- other_names: a proper name that is none of the above. Use it sparingly, and never as a place to put something you could file elsewhere.
- Copy every suggestion VERBATIM into one list. Never invent, translate or reformat names.
- Use [] for a category with no suggestions.`

// A classified row carries ONE short context snippet, and no more.
//
// The snippet is there to disambiguate a name, which one sentence around it
// already does. A Suggestion may carry three, and on a document of one kind
// (an email thread, a contract) the second and third are usually the same
// header or the same clause quoted again: prompt tokens spent re-reading text
// the model has already seen in the line above.
const classifyContextRunes = 40

// trimContext shortens one context snippet to classifyContextRunes, cutting on
// a RUNE boundary so a French document's accented characters cannot be split
// into a byte that means nothing.
func trimContext(context string) string {
	context = strings.TrimSpace(context)
	runes := []rune(context)
	if len(runes) <= classifyContextRunes {
		return context
	}
	return strings.TrimSpace(string(runes[:classifyContextRunes]))
}

// ClassifySuggestions re-files Smart detection's Suggestions through the local
// model: they travel in byte-budgeted batches, each reply is parsed with the
// usual tolerant parser, and any returned text that is not one of the INPUT main
// texts verbatim is dropped (hallucination filter), as is anything the allowlist
// vetoes. Only main texts and short context snippets are sent, never whole
// documents.
func (c *Client) ClassifySuggestions(ctx context.Context, suggestions []engine.Suggestion) ([]engine.Suggestion, error) {
	if len(suggestions) == 0 {
		return nil, nil
	}

	// Verbatim-input filter set (the classification counterpart of the
	// document hallucination filter).
	valid := make(map[string]bool, len(suggestions))
	for _, sugg := range suggestions {
		valid[sugg.MainText] = true
	}

	budget := c.promptBudgetBytes()
	var batches [][]engine.Suggestion
	batch := strings.Builder{}
	flush := func() error {
		if batch.Len() == 0 {
			return nil
		}
		reply, err := c.Chat(ctx, c.Model, classifySystemPrompt, batch.String(), suggestionSchema())
		batch.Reset()
		if err != nil {
			return err
		}
		suggestions, err := parseSuggestionJSON(reply)
		if err != nil {
			return err
		}
		batches = append(batches, suggestions)
		return nil
	}

	for _, sugg := range suggestions {
		if err := ctx.Err(); err != nil {
			return MergeSuggestions(batches...), err
		}
		line := "- " + sugg.MainText
		if len(sugg.Contexts) > 0 {
			line += " | context: " + trimContext(sugg.Contexts[0])
		}
		line += "\n"
		if batch.Len() > 0 && batch.Len()+len(line) > budget {
			if err := flush(); err != nil {
				return MergeSuggestions(batches...), err
			}
		}
		batch.WriteString(line)
	}
	if err := flush(); err != nil {
		return MergeSuggestions(batches...), err
	}

	var out []engine.Suggestion
	for _, p := range MergeSuggestions(batches...) {
		if !valid[p.MainText] {
			continue // invented or reformatted: dropped
		}
		if c.Allow != nil && c.Allow(p.MainText) {
			continue // allowlist wins
		}
		out = append(out, p)
	}
	return out, nil
}

// --- JSON reply parsing ---------------------------------------------------

// promptCategories are the exact keys every prompt in this file demands, and the
// only keys parseSuggestionJSON reads back.
//
// The three lists have to move together. A key in a prompt that is missing here
// is silently dropped on parse, and a key here that no prompt requests is a
// category the model is never asked to fill: either way the category is dead
// and nothing fails.
//
// custom_patterns is deliberately absent: it is the user's own regex, and a
// model has nothing to say about it.
var promptCategories = []string{
	engine.CatEntityNames,
	engine.CatProjectNames,
	engine.CatProductNames,
	engine.CatBrandNames,
	engine.CatPersonNames,
	engine.CatIdentifierNames,
	engine.CatOtherNames,
}

// suggestionSchema is the reply shape the model is CONSTRAINED to, rather than
// merely asked for: an object whose every property is an array of strings, with
// all of them required.
//
// It is a recall win and not only a safety one. Requiring every category to be
// present makes the model visit each one instead of stopping after the two it
// thought of first, which on one measured page took a small model from six
// names to sixteen. It costs effectively nothing: masking the next token
// against a grammar is microseconds against a CPU decode step of tens of
// milliseconds, and a stricter grammar lets more tokens be fast-forwarded
// rather than sampled.
//
// It is DERIVED from promptCategories rather than written out, so the schema
// cannot fall out of step with the parser and the prompts. That keeps
// TestPromptsAndParserAgreeOnTheCategoryKeys covering it: a new engine category
// added to the prompts but not here would otherwise be a category the model is
// forbidden to fill, which no other test would notice.
//
// The schema is FLAT on purpose. Sub-4B models degrade badly on schemas
// containing $defs or $ref, echoing the schema's own structure back in place of
// the extracted values, so there are no reusable definitions here even though
// every property is identical.
func suggestionSchema() map[string]any {
	properties := make(map[string]any, len(promptCategories))
	required := make([]string, 0, len(promptCategories))
	for _, category := range promptCategories {
		properties[category] = map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string"},
		}
		required = append(required, category)
	}
	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

// parseSuggestionJSON tolerantly parses the model's JSON reply into unified
// Suggestions: accidental markdown code fences are stripped, unknown keys
// ignored, and each known key may hold a list of strings. A reply that still
// fails to parse produces an actionable error (the model or prompt needs
// attention, and the user should know which model misbehaved).
//
// Every Suggestion it produces is stamped with the Local AI discovery method
// HERE, at the boundary, so no caller has to remember to do it and nothing the
// model found can reach the review list without saying where it came from.
func parseSuggestionJSON(reply string) ([]engine.Suggestion, error) {
	cleaned := strings.TrimSpace(reply)
	// Strip ```json ... ``` fences some models add despite format:json.
	if strings.HasPrefix(cleaned, "```") {
		cleaned = strings.TrimPrefix(cleaned, "```json")
		cleaned = strings.TrimPrefix(cleaned, "```")
		cleaned = strings.TrimSuffix(strings.TrimSpace(cleaned), "```")
		cleaned = strings.TrimSpace(cleaned)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(cleaned), &raw); err != nil {
		return nil, fmt.Errorf(
			"the model's reply was not the expected JSON object (%v), try a stronger model in settings (reply started with: %.80q)",
			err, cleaned)
	}

	var out []engine.Suggestion
	for _, cat := range promptCategories {
		val, ok := raw[cat]
		if !ok {
			continue // missing key = no findings; tolerated
		}
		var names []string
		if err := json.Unmarshal(val, &names); err != nil {
			// Category present but not a string list — tolerate a single
			// string too, otherwise skip the category rather than fail
			// the whole scan.
			var one string
			if json.Unmarshal(val, &one) == nil && one != "" {
				names = []string{one}
			} else {
				continue
			}
		}
		for _, n := range names {
			n = strings.TrimSpace(n)
			if n != "" {
				out = append(out, engine.Suggestion{Category: cat, MainText: n, Count: 1}.WithMethod(engine.MethodLocalAI))
			}
		}
	}
	return out, nil
}

// --- Document chunking ------------------------------

// promptBudgetBytes derives the per-chunk byte budget from the configured
// context size: roughly 3 bytes of typical text per token, with 25% of
// the window reserved for the system prompt and the model's reply.
func (c *Client) promptBudgetBytes() int {
	ctxSize := c.ContextSize
	if ctxSize <= 0 {
		ctxSize = DefaultContextSize
	}
	return ctxSize * 3 * 3 / 4
}

// Chunks splits a document into prompt-sized chunks, enforcing the
// MaxChunksPerDocument cap with an actionable error.
func (c *Client) Chunks(text string) ([]string, error) {
	budget := c.promptBudgetBytes()
	chunks := chunkText(text, budget, chunkOverlapBytes)
	if len(chunks) > MaxChunksPerDocument {
		return nil, fmt.Errorf(
			"this document is very large (%d chunks of %d KB); split it into smaller files or run Smart detection instead",
			len(chunks), budget/1024)
	}
	return chunks, nil
}

// EstimateChunks reports how many chunks a document would produce, so the
// UI can warn BEFORE starting a discovery run.
func (c *Client) EstimateChunks(text string) int {
	return len(chunkText(text, c.promptBudgetBytes(), chunkOverlapBytes))
}

// chunkText splits text into chunks of at most budgetBytes, preferring to
// cut at a paragraph break, then a line break, then a space, and never
// inside a UTF-8 sequence (rune-safe). Consecutive chunks overlap by
// roughly overlapBytes so entities on a boundary are seen whole at least
// once. The empty string yields a single empty chunk (callers treat the
// document uniformly).
func chunkText(text string, budgetBytes, overlapBytes int) []string {
	if budgetBytes <= 0 {
		budgetBytes = DefaultContextSize * 3 * 3 / 4
	}
	if len(text) <= budgetBytes {
		return []string{text}
	}
	// Overlap must stay well under the budget or the loop cannot advance.
	if overlapBytes >= budgetBytes/2 {
		overlapBytes = budgetBytes / 4
	}

	var chunks []string
	start := 0
	for start < len(text) {
		end := start + budgetBytes
		if end >= len(text) {
			chunks = append(chunks, text[start:])
			break
		}
		// Prefer natural boundaries inside the second half of the window
		// (searching the whole window could produce degenerate tiny
		// chunks on paragraph-dense text).
		windowStart := start + budgetBytes/2
		cut := -1
		for _, sep := range []string{"\n\n", "\n", " "} {
			if idx := strings.LastIndex(text[windowStart:end], sep); idx >= 0 {
				cut = windowStart + idx + len(sep)
				break
			}
		}
		if cut < 0 {
			// No boundary at all (one enormous token): cut at a rune edge.
			cut = end
			for cut > start && (text[cut]&0xC0) == 0x80 {
				cut--
			}
		}
		chunks = append(chunks, text[start:cut])

		// Next chunk starts overlapBytes BEFORE the cut, aligned forward
		// to a rune boundary, and always past the previous start.
		next := cut - overlapBytes
		if next <= start {
			next = cut
		}
		for next < len(text) && (text[next]&0xC0) == 0x80 {
			next++
		}
		start = next
	}
	return chunks
}

// Package ollama is THE ONLY package in this repository that talks to the
// local Ollama server over HTTP (see CLAUDE.md §4, "One-file external
// boundary"). Everything else — in particular engine/* — consumes the
// engine.LLM interface, never this concrete client. That keeps the planned
// P4 fallback (ONNX-in-WebView) a contained refactor.
//
// Local-only guarantee: the base URL host is locked to the loopback address
// 127.0.0.1. Only the port may be changed by the user in settings. Do not
// "improve" this into a configurable remote host — it would break the
// non-negotiable local-only guarantee in CLAUDE.md §4.
//
// API surface used (pinned in CLAUDE.md §7, "Ollama HTTP API as of 2026"):
//   - GET  /api/tags  — probe + model list
//   - POST /api/chat  — {"format":"json","stream":false} completions
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

	"doc-anonymiser/engine"
)

// DefaultBaseURL is the standard Ollama endpoint on the local machine.
// The host part is intentionally hardcoded to loopback (CLAUDE.md §8).
const DefaultBaseURL = "http://127.0.0.1:11434"

// DefaultModel is the settings DEFAULT only — the effective model is a
// user setting populated from /api/tags and must never be hardcoded
// anywhere else (CLAUDE.md §7).
const DefaultModel = "qwen2.5:3b-instruct"

// ErrTooOld is the pinned message for an Ollama old enough to miss
// /api/chat (CLAUDE.md §7: probe succeeds but chat 404s WITHOUT a
// model-not-found body; a 404 naming the model means the model is simply
// not pulled — see Chat).
const ErrTooOld = "Ollama too old, please update"

// DefaultContextSize is the default num_ctx sent to Ollama (BUILD-02
// Phase 5b). 8192 replaces the implicit model default (often 2048 for
// small models), because whole document chunks are sent as prompts. The
// user can change it in Configure; 0 means "let the model default apply"
// (the option is omitted from the request).
const DefaultContextSize = 8192

// chunkOverlapBytes is how much consecutive chunks overlap so an entity
// name sitting exactly on a chunk boundary is still seen whole by at
// least one chunk (BUILD-02 Phase 5c).
const chunkOverlapBytes = 512

// MaxChunksPerDocument caps how many chunks one document may produce.
// Beyond this the scan would take unreasonably long on a small local
// model; the caller gets an actionable error instead (BUILD-02 Phase 5d).
const MaxChunksPerDocument = 64

// OllamaStatus is the result of probing the local Ollama server. It is sent
// to the frontend as-is (via app.go), so field names are chosen to read well
// in JavaScript after Wails' JSON serialisation.
type OllamaStatus struct {
	// Available is true when GET /api/tags answered successfully.
	Available bool `json:"available"`
	// Models lists the model names installed in Ollama (e.g.
	// "qwen2.5:3b-instruct"). Empty when Available is false.
	Models []string `json:"models"`
	// Detail is a human-readable explanation of the status, shown in the
	// UI tooltip. It must always be actionable (what failed, how to fix).
	Detail string `json:"detail"`
}

// Client talks to a local Ollama server using only the Go standard library.
// It implements engine.LLM (the deep-scan slot of the pipeline).
type Client struct {
	// BaseURL of the Ollama server, e.g. "http://127.0.0.1:11434".
	// Constructed from settings; the host always remains loopback.
	BaseURL string
	// Model is the chat model to use; a user setting defaulting to
	// DefaultModel and normally picked from the ListModels() dropdown.
	Model string
	// Allow, when set, vetoes LLM proposals (wired by app.go to the
	// session allowlist's Contains). The engine applies the allowlist
	// again — belt and braces, because CLAUDE.md §5 says the allowlist
	// wins in EVERY pass.
	Allow func(string) bool
	// ContextSize is the num_ctx option sent with every chat request
	// (BUILD-02 Phase 5b). Defaults to DefaultContextSize in New; 0 omits
	// the option so the model default applies. It also drives the
	// document chunk budget (promptBudgetBytes).
	ContextSize int

	// probeClient carries a short timeout so a missing Ollama never hangs
	// the UI; chatClient allows slow small-model generations (120 s,
	// BUILD.md Phase 5) — both honour context cancellation on top.
	probeClient *http.Client
	chatClient  *http.Client
}

// Compile-time proof that *Client satisfies the engine's LLM interface.
// If the interface drifts, the build breaks here with a clear message.
var _ engine.LLM = (*Client)(nil)

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
//	{"models": [{"name": "qwen2.5:3b-instruct", ...}, ...]}
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

// chatRequest is the POST /api/chat body. Format "json" instructs Ollama
// to constrain the output to valid JSON (CLAUDE.md §8: discovery and
// deep-scan prompts must set it); stream=false gives one complete reply.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Format   string        `json:"format"`
	Stream   bool          `json:"stream"`
	// Options carries model options; NumCtx sets the context window. A
	// nil pointer omits the object entirely so the model default applies
	// (a pointer because json "omitzero" needs Go 1.24 and we pin 1.23).
	Options *chatOptions `json:"options,omitempty"`
}

// chatOptions mirrors the Ollama options object; only num_ctx is used.
type chatOptions struct {
	NumCtx int `json:"num_ctx,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
	Error   string      `json:"error"`
}

// Chat sends one system+user exchange and returns the assistant's raw
// content. ctx cancellation aborts the request mid-flight (the pipeline
// cancel button relies on this).
func (c *Client) Chat(ctx context.Context, model, systemPrompt, userPrompt string) (string, error) {
	if model == "" {
		model = c.Model
	}
	reqBody := chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Format: "json",
		Stream: false,
	}
	if c.ContextSize > 0 {
		// num_ctx only travels when explicitly configured; 0 keeps the
		// model default (BUILD-02 Phase 5b).
		reqBody.Options = &chatOptions{NumCtx: c.ContextSize}
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("could not build the Ollama request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("could not build the Ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.chatClient.Do(req)
	if err != nil {
		return "", fmt.Errorf(
			"could not reach Ollama on %s (%v), check that Ollama is still running, then re-probe in settings", c.BaseURL, err)
	}
	defer resp.Body.Close()

	// Distinct error surfaces per status (BUILD-02 Phase 5a). The old
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
				return "", fmt.Errorf(
					"model %q is not installed; run 'ollama pull %s' or pick another model in settings", model, model)
			}
			return "", fmt.Errorf("%s", ErrTooOld)
		case http.StatusBadRequest:
			msg := fmt.Sprintf("Ollama rejected the request (HTTP 400): %s", apiErr.Error)
			low := strings.ToLower(apiErr.Error)
			if strings.Contains(low, "context") || strings.Contains(low, "length") {
				msg += " The document chunk was too large for the model's context window; lower the chunk size or raise the context size in Configure."
			}
			return "", fmt.Errorf("%s", msg)
		default:
			excerpt := apiErr.Error
			if excerpt == "" {
				excerpt = strings.TrimSpace(string(bodyBytes))
				if len(excerpt) > 200 {
					excerpt = excerpt[:200]
				}
			}
			return "", fmt.Errorf(
				"Ollama answered HTTP %d on /api/chat (expected 200): %s; check that the Ollama server is healthy, then re-probe in settings",
				resp.StatusCode, excerpt)
		}
	}

	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("Ollama's /api/chat reply could not be parsed (%v), try updating Ollama", err)
	}
	if out.Error != "" {
		return "", fmt.Errorf("Ollama reported an error: %s, the model %q may not be installed; run 'ollama pull %s'", out.Error, model, model)
	}
	return out.Message.Content, nil
}

// --- Discovery (Phase-A prompt) ------------------------------------------

// discoverSystemPrompt demands STRICT JSON with the exact CLAUDE.md §5
// category keys. The "verbatim" instruction feeds the hallucination filter:
// anything not copied exactly from the text is dropped afterwards anyway.
const discoverSystemPrompt = `You are an entity extraction engine for confidential business documents.
Extract proper names from the user's document and respond with ONLY a JSON object, no prose, using exactly these keys:
{"client_names": [], "project_names": [], "internal_names": [], "person_names": []}
Rules:
- client_names: companies/organisations that are clients or counterparties.
- project_names: engagement or project code names.
- internal_names: internal staff, teams or internal systems.
- person_names: natural persons not already in internal_names.
- Copy every name VERBATIM from the document. Never invent, translate or reformat names.
- Use [] for a category with no findings.`

// Discover runs the Phase-A prompt on one document's text and returns raw
// category → names proposals for the review screen. Long documents are
// CHUNKED (BUILD-02 Phase 5c): each chunk is scanned in sequence, ctx
// cancellation is honoured between chunks, per-chunk proposals merge
// through MergeProposals, and the hallucination filter runs against the
// FULL document text (an entity split across a boundary is covered by the
// chunk overlap). The caller (app.go) merges multi-file results.
func (c *Client) Discover(ctx context.Context, text string) ([]engine.ProposedEntity, error) {
	return c.scanChunks(ctx, text, func(chunk string) (string, error) {
		return c.Chat(ctx, c.Model, discoverSystemPrompt, chunk)
	})
}

// scanChunks is the shared chunk loop for Discover and DeepScan: chunk,
// chat per chunk via chat(), parse, merge, then hallucination-filter
// against the whole document. On mid-loop cancellation the proposals
// gathered so far are returned WITH the context error, so callers can
// keep partial results (BUILD-02 Phase 7d).
func (c *Client) scanChunks(ctx context.Context, text string, chat func(chunk string) (string, error)) ([]engine.ProposedEntity, error) {
	chunks, err := c.Chunks(text)
	if err != nil {
		return nil, err
	}
	var batches [][]engine.ProposedEntity
	for _, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return c.filterProposals(ollamaMerge(batches), text), err
		}
		reply, err := chat(chunk)
		if err != nil {
			return c.filterProposals(ollamaMerge(batches), text), err
		}
		proposals, err := parseEntityJSON(reply)
		if err != nil {
			return c.filterProposals(ollamaMerge(batches), text), err
		}
		batches = append(batches, proposals)
	}
	// Hallucination filter (CLAUDE.md §5) against the FULL text plus the
	// allowlist veto.
	return c.filterProposals(ollamaMerge(batches), text), nil
}

// ollamaMerge is MergeProposals over a batch slice (tiny readability
// helper for scanChunks).
func ollamaMerge(batches [][]engine.ProposedEntity) []engine.ProposedEntity {
	return MergeProposals(batches...)
}

// --- Deep-scan (residual pass) -------------------------------------------

const deepScanSystemPromptPrefix = `You are an entity extraction engine performing a FINAL review of a business document that was already partially anonymised (placeholders look like [CLIENT_1]).
Find ONLY residual proper names that are still visible and were missed. Respond with ONLY a JSON object, no prose, using exactly these keys:
{"client_names": [], "project_names": [], "internal_names": [], "person_names": []}
Rules:
- Copy every name VERBATIM from the document. Never invent names.
- Do NOT report placeholders like [CLIENT_1] or names from the known list below.
- Use [] for a category with no findings.`

// DeepScan implements engine.LLM: it proposes residual entities for one
// document, excluding what is already known, and applies the hallucination
// filter and allowlist veto before returning (BUILD.md Phase 5).
func (c *Client) DeepScan(ctx context.Context, text string, known []engine.Entity) ([]engine.ProposedEntity, error) {
	system := deepScanSystemPromptPrefix
	if len(known) > 0 {
		var names []string
		for _, e := range known {
			names = append(names, e.Canonical)
		}
		system += "\nKnown (do not report): " + strings.Join(names, "; ")
	}

	return c.scanChunks(ctx, text, func(chunk string) (string, error) {
		return c.Chat(ctx, c.Model, system, chunk)
	})
}

// filterProposals applies the hallucination filter (exact string must occur
// in the source text) and the allowlist veto. The engine repeats both
// checks — defence in depth, not redundancy to remove.
func (c *Client) filterProposals(proposals []engine.ProposedEntity, sourceText string) []engine.ProposedEntity {
	var out []engine.ProposedEntity
	for _, p := range proposals {
		if !strings.Contains(sourceText, p.Text) {
			continue // hallucinated
		}
		if c.Allow != nil && c.Allow(p.Text) {
			continue // allowlist wins
		}
		out = append(out, p)
	}
	return out
}

// MergeProposals merges multi-file discovery results, deduplicating
// case-insensitively per category while keeping first-seen spelling and
// order (BUILD.md Phase 5 activity 4).
func MergeProposals(batches ...[]engine.ProposedEntity) []engine.ProposedEntity {
	seen := map[string]bool{}
	var out []engine.ProposedEntity
	for _, batch := range batches {
		for _, p := range batch {
			key := p.Category + "|" + strings.ToLower(strings.TrimSpace(p.Text))
			if strings.TrimSpace(p.Text) == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, p)
		}
	}
	return out
}

// --- JSON reply parsing ---------------------------------------------------

// entityCategories are the exact keys the prompts demand (CLAUDE.md §5).
var entityCategories = []string{"client_names", "project_names", "internal_names", "person_names"}

// parseEntityJSON tolerantly parses the model's JSON reply: accidental
// markdown code fences are stripped, unknown keys ignored, and each known
// key may hold a list of strings. A reply that still fails to parse
// produces an actionable error (the model or prompt needs attention, and
// the user should know which model misbehaved).
func parseEntityJSON(reply string) ([]engine.ProposedEntity, error) {
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

	var out []engine.ProposedEntity
	for _, cat := range entityCategories {
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
				out = append(out, engine.ProposedEntity{Category: cat, Text: n})
			}
		}
	}
	return out, nil
}

// --- Document chunking (BUILD-02 Phase 5c/5d) ------------------------------

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
// MaxChunksPerDocument cap with an actionable error (BUILD-02 Phase 5d).
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
// UI can warn BEFORE starting a discovery run (BUILD-02 Phase 5d/7e).
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

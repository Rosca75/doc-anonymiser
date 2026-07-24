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
// /api/chat (CLAUDE.md §7: probe succeeds but chat 404s).
const ErrTooOld = "Ollama too old, please update"

// maxPromptBytes caps how much document text is sent per LLM call. Small
// local models have limited context windows; beyond this size the tail is
// cut and the UI-visible behaviour stays correct (the deterministic passes
// always see the FULL text — only the LLM suggestion pass is truncated).
const maxPromptBytes = 24 * 1024

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
	body, err := json.Marshal(chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Format: "json",
		Stream: false,
	})
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

	// A 404 on /api/chat while /api/tags works means a pre-chat-API
	// Ollama — the pinned "too old" case (CLAUDE.md §7).
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("%s", ErrTooOld)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf(
			"Ollama answered HTTP %d on /api/chat (expected 200), the model %q may not be installed; run 'ollama pull %s' or pick another model in settings",
			resp.StatusCode, model, model)
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
{"client_names": [], "project_names": [], "pwc_internal_names": [], "person_names": []}
Rules:
- client_names: companies/organisations that are clients or counterparties.
- project_names: engagement or project code names.
- pwc_internal_names: PwC staff, teams or internal systems.
- person_names: natural persons not already in pwc_internal_names.
- Copy every name VERBATIM from the document. Never invent, translate or reformat names.
- Use [] for a category with no findings.`

// Discover runs the Phase-A prompt on one document's text and returns raw
// category → names proposals for the review screen. The caller (app.go)
// merges multi-file results with MergeProposals.
func (c *Client) Discover(ctx context.Context, text string) ([]engine.ProposedEntity, error) {
	reply, err := c.Chat(ctx, c.Model, discoverSystemPrompt, clipText(text))
	if err != nil {
		return nil, err
	}
	proposals, err := parseEntityJSON(reply)
	if err != nil {
		return nil, err
	}
	// Hallucination filter: exact-string occurrence in the source
	// (CLAUDE.md §5) plus the allowlist veto.
	return c.filterProposals(proposals, text), nil
}

// --- Deep-scan (residual pass) -------------------------------------------

const deepScanSystemPromptPrefix = `You are an entity extraction engine performing a FINAL review of a business document that was already partially anonymised (placeholders look like [CLIENT_1]).
Find ONLY residual proper names that are still visible and were missed. Respond with ONLY a JSON object, no prose, using exactly these keys:
{"client_names": [], "project_names": [], "pwc_internal_names": [], "person_names": []}
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

	reply, err := c.Chat(ctx, c.Model, system, clipText(text))
	if err != nil {
		return nil, err
	}
	proposals, err := parseEntityJSON(reply)
	if err != nil {
		return nil, err
	}
	return c.filterProposals(proposals, text), nil
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
var entityCategories = []string{"client_names", "project_names", "pwc_internal_names", "person_names"}

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

// clipText truncates very long documents to the LLM prompt cap (see
// maxPromptBytes), cutting at a rune boundary.
func clipText(text string) string {
	if len(text) <= maxPromptBytes {
		return text
	}
	cut := maxPromptBytes
	for cut > 0 && (text[cut]&0xC0) == 0x80 {
		cut-- // do not split a UTF-8 sequence
	}
	return text[:cut]
}

// Package ollama is THE ONLY package in this repository that talks to the
// local Ollama server over HTTP (see CLAUDE.md §4, "One-file external
// boundary"). Everything else — in particular engine/* — consumes the LLM
// interface defined below, never this concrete client. That keeps the
// planned P4 fallback (ONNX-in-WebView) a contained refactor.
//
// Local-only guarantee: the base URL host is locked to the loopback address
// 127.0.0.1. Only the port may be changed by the user in settings. Do not
// "improve" this into a configurable remote host — it would break the
// non-negotiable local-only guarantee in CLAUDE.md §4.
package ollama

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// DefaultBaseURL is the standard Ollama endpoint on the local machine.
// The host part is intentionally hardcoded to loopback (CLAUDE.md §8).
const DefaultBaseURL = "http://127.0.0.1:11434"

// ErrNotImplemented is returned by LLM methods that are stubs in this
// bootstrap phase. Callers (engine/pipeline.go, once implemented) must treat
// it as "feature not available yet", not as a fatal error.
var ErrNotImplemented = errors.New(
	"ollama: this LLM feature is not implemented yet (bootstrap stub) — " +
		"it will be built in a later phase, see BUILD.md")

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

// Entity is one entity proposed by the LLM (discovery or deep-scan).
// Category must be one of the CLAUDE.md §5 entity categories
// (client_names, project_names, pwc_internal_names, person_names, ...).
type Entity struct {
	Text     string `json:"text"`
	Category string `json:"category"`
}

// LLM is the interface the engine consumes for AI-assisted passes.
// engine/* must depend on this interface only — never on *Client — so the
// implementation can be swapped (Ollama today, ONNX-in-WebView as the P4
// fallback) without touching business logic.
type LLM interface {
	// Discover asks the model to find engagement entities (clients,
	// projects, people, ...) in the given markdown text. Results are raw
	// LLM proposals: the engine still applies the hallucination filter
	// (exact-string-occurrence check) and the allowlist.
	Discover(text string) ([]Entity, error)
	// DeepScan asks the model to find residual entities AFTER the
	// deterministic passes have run. Same post-filtering rules apply.
	DeepScan(text string) ([]Entity, error)
}

// Client talks to a local Ollama server using only the Go standard library.
// It implements the LLM interface (with stubs, for now).
type Client struct {
	// BaseURL of the Ollama server, e.g. "http://127.0.0.1:11434".
	// Constructed from settings; the host must remain loopback.
	BaseURL string
	// httpClient carries the short probe timeout so a missing Ollama never
	// hangs the UI.
	httpClient *http.Client
}

// Compile-time proof that *Client satisfies the LLM interface. If a method
// signature drifts, the build breaks here with a clear message instead of
// failing at some distant call site.
var _ LLM = (*Client)(nil)

// New returns a Client for the given base URL (pass "" for the default
// loopback URL). The 2-second timeout keeps the startup probe snappy:
// if Ollama is not running, the connection is refused almost instantly;
// the timeout only matters when something is listening but unresponsive.
func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL:    baseURL,
		httpClient: &http.Client{Timeout: 2 * time.Second},
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
// calling GET /api/tags (the cheapest "are you there?" endpoint the Ollama
// API offers). It never returns an error: unavailability is a normal state
// expressed through OllamaStatus, because the app must degrade gracefully
// (CLAUDE.md §4) rather than treat a missing Ollama as a failure.
func (c *Client) Probe() OllamaStatus {
	resp, err := c.httpClient.Get(c.BaseURL + "/api/tags")
	if err != nil {
		// Typical case: nothing listening on the port (Ollama not
		// installed or not started). The Detail string doubles as the
		// UI tooltip, so it says how to fix the situation.
		return OllamaStatus{
			Available: false,
			Detail: fmt.Sprintf(
				"Ollama not detected on %s — install it from ollama.com and start it to enable the AI features (the app works fine without it). Technical detail: %v",
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

	// Collect installed model names for the model-selector dropdown.
	names := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		names = append(names, m.Name)
	}

	detail := fmt.Sprintf("Ollama detected on %s with %d model(s).", c.BaseURL, len(names))
	if len(names) == 0 {
		detail = fmt.Sprintf(
			"Ollama detected on %s but no models are installed — run 'ollama pull qwen2.5:3b-instruct' to enable the AI features.",
			c.BaseURL)
	}
	return OllamaStatus{Available: true, Models: names, Detail: detail}
}

// Discover is a bootstrap stub. The real implementation (later phase, see
// BUILD.md) will POST /api/chat with "format":"json" and "stream":false and
// parse strict-JSON entity lists keyed by the CLAUDE.md §5 categories.
func (c *Client) Discover(text string) ([]Entity, error) {
	return nil, ErrNotImplemented
}

// DeepScan is a bootstrap stub — same contract as Discover, but run after
// the deterministic passes to catch residual entities.
func (c *Client) DeepScan(text string) ([]Entity, error) {
	return nil, ErrNotImplemented
}

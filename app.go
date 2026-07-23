// app.go — the Wails bound struct. This is the ONLY seam between the
// frontend and Go: every method here must be a thin adapter that delegates
// straight to engine/* or ollama/* (CLAUDE.md §3 — no business logic in
// this file). The frontend calls these methods exclusively through
// static/api.js.
package main

import (
	"context"

	"doc-anonymiser/ollama"
)

// App is bound to the frontend by main.go. Its exported methods become
// window.go.main.App.<Method> in JavaScript.
type App struct {
	// ctx is the Wails runtime context, stored at startup so future
	// methods can open native dialogs, emit events, etc.
	ctx context.Context
	// llm is the Ollama client. It is created once here and handed to
	// engine code as the ollama.LLM interface — engine/* must never see
	// the concrete client (CLAUDE.md §4, one-file external boundary).
	llm *ollama.Client
}

// NewApp constructs the bound struct. Kept trivial on purpose: anything
// interesting belongs in engine/* so it stays headless-testable.
func NewApp() *App {
	return &App{
		// "" selects the default loopback base URL (127.0.0.1:11434).
		// A user-overridden port (settings, later phase) will be passed
		// here instead.
		llm: ollama.New(""),
	}
}

// startup is called by Wails once the runtime is ready (see main.go).
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// Ping proves the JavaScript ↔ Go bridge end to end: the home screen calls
// it on load and renders the returned string. If "pong" does not appear in
// the UI, the bridge (not the business logic) is broken.
func (a *App) Ping() string {
	return "pong"
}

// ProbeOllama reports whether a local Ollama server is reachable and which
// models it offers. The frontend uses the result to render the status badge
// and to enable/disable every LLM-dependent control (CLAUDE.md §4,
// graceful degradation). It never fails: "not available" is a normal state
// carried inside the returned status, not an error.
func (a *App) ProbeOllama() ollama.OllamaStatus {
	return a.llm.Probe()
}

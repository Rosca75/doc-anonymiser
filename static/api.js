// api.js — THE ONLY file allowed to call Go bound methods (CLAUDE.md §4).
//
// Wails exposes bound Go methods as window.go.main.App.<Method>, each
// returning a Promise. Every view/module must import these wrappers instead
// of touching window.go directly, so the Go↔JS surface stays greppable in
// exactly one place.

/**
 * bridge() returns the bound App object, or throws a readable error when
 * the Wails bridge is not present (e.g. the page was opened in a normal
 * browser instead of the app window).
 */
function bridge() {
  const app = window.go?.main?.App;
  if (!app) {
    throw new Error(
      "Wails bridge not available — this page must run inside the " +
      "doc-anonymiser application window, not a regular browser."
    );
  }
  return app;
}

/**
 * ping() proves the JS↔Go bridge end to end.
 * @returns {Promise<string>} resolves to "pong" when the bridge works.
 */
export function ping() {
  return bridge().Ping();
}

/**
 * probeOllama() asks the Go side whether a local Ollama server is running.
 * @returns {Promise<{available: boolean, models: string[], detail: string}>}
 *          never rejects for "Ollama missing" — that is a normal state
 *          expressed in the returned object (graceful degradation).
 */
export function probeOllama() {
  return bridge().ProbeOllama();
}

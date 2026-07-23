// api.js — THE ONLY file allowed to call Go bound methods (CLAUDE.md §4).
//
// Wails exposes bound Go methods as window.go.main.App.<Method>, each
// returning a Promise, and runtime events on window.runtime. Every
// view/module must go through these wrappers so the Go↔JS surface stays
// greppable in exactly one place.

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

/** ping() proves the JS↔Go bridge end to end (resolves to "pong"). */
export function ping() {
  return bridge().Ping();
}

/**
 * probeOllama() asks Go whether a local Ollama server is running.
 * Never rejects for "Ollama missing" — that is a normal state inside the
 * returned {available, models, detail} object (graceful degradation).
 */
export function probeOllama() {
  return bridge().ProbeOllama();
}

// --- Import ------------------------------------------------------------

/** importFiles() opens the native multi-file dialog; resolves to an
 *  ImportResult {documents, errors}. */
export function importFiles() {
  return bridge().ImportFiles();
}

/** removeDocument(name) drops one document; resolves to ImportResult. */
export function removeDocument(name) {
  return bridge().RemoveDocument(name);
}

/** listDocuments() returns the current DocumentInfo list. */
export function listDocuments() {
  return bridge().ListDocuments();
}

// --- Settings ----------------------------------------------------------

/** getSettings() resolves to {level, ollamaPort, model}. */
export function getSettings() {
  return bridge().GetSettings();
}

/** applySettings(settings) stores settings and resolves to the fresh
 *  OllamaStatus (rejects with an actionable message on bad input). */
export function applySettings(settings) {
  return bridge().ApplySettings(settings);
}

/** listOllamaModels() resolves to the installed model names. */
export function listOllamaModels() {
  return bridge().ListOllamaModels();
}

// --- Entities screen (Phase 7) ------------------------------------------

/** runDiscovery(fileNames, allowTerms) resolves to merged proposals
 *  [{category, text}]. Rejects with an actionable message on failure. */
export function runDiscovery(fileNames, allowTerms) {
  return bridge().RunDiscovery(fileNames, allowTerms);
}

/** expandVariants(entity) resolves to the variant list of one entity
 *  ({category, canonical, manualVariants}). */
export function expandVariants(entity) {
  return bridge().ExpandEntityVariants(entity);
}

/** validatePattern(expr) resolves to "" (valid) or the error message. */
export function validatePattern(expr) {
  return bridge().ValidatePattern(expr);
}

/** patternMatches(expr) resolves to up to 20 sample matches across the
 *  loaded documents. */
export function patternMatches(expr) {
  return bridge().PatternMatches(expr);
}

// --- Events ------------------------------------------------------------

/**
 * onEvent(name, handler) subscribes to a Wails runtime event (e.g.
 * "documents:changed" emitted after a drag-drop import). Returns an
 * unsubscribe function; a missing runtime (plain browser) is a no-op so
 * the UI still renders.
 */
export function onEvent(name, handler) {
  const rt = window.runtime;
  if (!rt?.EventsOn) return () => {};
  rt.EventsOn(name, handler);
  return () => rt.EventsOff?.(name);
}

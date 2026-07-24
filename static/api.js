// api.js, THE ONLY file allowed to call Go bound methods (CLAUDE.md §4).
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
      "Wails bridge not available. This page must run inside the " +
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
 * Never rejects for "Ollama missing", that is a normal state inside the
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

// --- Allowlist (BUILD-02 Phase 4) -----------------------------------------

/** defaultAllowlist() resolves to the seeded never-anonymise terms shown
 *  in the UI at startup (removable like any other term). */
export function defaultAllowlist() {
  return bridge().DefaultAllowlist();
}

/** importAllowlistCSV() opens a native dialog for a CSV of terms;
 *  resolves to the parsed terms (null when the user cancels). */
export function importAllowlistCSV() {
  return bridge().ImportAllowlistCSV();
}

/** saveAllowlistTemplate() saves the downloadable CSV template. */
export function saveAllowlistTemplate() {
  return bridge().SaveAllowlistTemplate();
}

// --- Entities screen (Phase 7) ------------------------------------------

/** runDiscovery(fileNames, allowTerms) resolves to a DiscoveryResult
 *  {proposals: [{category, text}], status, cancelled}. A cancelled run
 *  resolves with partial proposals; only real failures reject. */
export function runDiscovery(fileNames, allowTerms) {
  return bridge().RunDiscovery(fileNames, allowTerms);
}

/** cancelDiscovery() aborts the in-flight discovery run (no-op if idle). */
export function cancelDiscovery() {
  return bridge().CancelDiscovery();
}

/** estimateDiscovery(fileNames) resolves to per-file size estimates
 *  [{name, chunks, tooLarge, message}] so oversized files can be
 *  excluded BEFORE the run starts. */
export function estimateDiscovery(fileNames) {
  return bridge().EstimateDiscovery(fileNames);
}

/** expandVariants(entity) resolves to the variant list of one entity
 *  ({category, canonical, manualVariants, excludedVariants}). */
export function expandVariants(entity) {
  return bridge().ExpandEntityVariants(entity);
}

/** runSmartDetection(fileNames, allowTerms, classify) resolves to a
 *  SmartDetectionResult {candidates, status, cancelled}. Works fully
 *  offline; classify=true refines categories via the local AI. */
export function runSmartDetection(fileNames, allowTerms, classify) {
  return bridge().RunSmartDetection(fileNames, allowTerms, !!classify);
}

/** countTermMatches(term) resolves to {count, documents} for the live
 *  manual-entry preview. */
export function countTermMatches(term) {
  return bridge().CountTermMatches(term);
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

// --- Run screen (Phase 8) -------------------------------------------------

/** runPipeline(request) starts the pipeline; resolves immediately (results
 *  arrive on the "pipeline:done" event, progress on "pipeline:progress"). */
export function runPipeline(request) {
  return bridge().RunPipeline(request);
}

/** cancelPipeline() aborts the in-flight run. */
export function cancelPipeline() {
  return bridge().CancelPipeline();
}

/** fastRerun(request) re-runs the deterministic passes only (no LLM),
 *  resolving directly to the fresh Results. */
export function fastRerun(request) {
  return bridge().FastRerun(request);
}

/** getResults() resolves to the latest Results (or null). */
export function getResults() {
  return bridge().GetResults();
}

/** getMapping() resolves to the placeholder → {original, category}
 *  lookup for tooltips and reassignment (empty before the first run). */
export function getMapping() {
  return bridge().GetMapping();
}

// --- Export screen (Phase 9) -----------------------------------------------

/** exportDocumentFormats(name) resolves to the offered extensions
 *  (default first) for one result document. */
export function exportDocumentFormats(name) {
  return bridge().ExportDocumentFormats(name);
}

/** saveDocument(name, ext) opens a save dialog for one document. */
export function saveDocument(name, ext) {
  return bridge().SaveDocument(name, ext);
}

/** getSameFormatMetadata(name, ext) resolves to {fields, filename}: the
 *  document properties with proposed replacements plus the proposed
 *  anonymised filename, for the review panel (BUILD-02 Phase 12). */
export function getSameFormatMetadata(name, ext) {
  return bridge().GetSameFormatMetadata(name, ext);
}

/** saveSameFormat(name, ext, fields, filename) writes the same-format
 *  copy with the REVIEWED metadata values and filename. */
export function saveSameFormat(name, ext, fields, filename) {
  return bridge().SaveSameFormat(name, ext, fields, filename);
}

/** exportAllZip() saves every anonymised document into one zip. */
export function exportAllZip() {
  return bridge().ExportAllZip();
}

/** copyDocument(name) puts the anonymised text on the clipboard. */
export function copyDocument(name) {
  return bridge().CopyDocument(name);
}

/** exportMapping(format) saves the re-identification key ("csv"/"json").
 *  Call ONLY after the user confirmed the sensitivity warning. */
export function exportMapping(format) {
  return bridge().ExportMapping(format);
}

/** exportReport(format) saves the run report ("json"/"md"). */
export function exportReport(format) {
  return bridge().ExportReport(format);
}

/** saveSession(request) persists the session (entities, allowlist,
 *  patterns, rules, settings, registry). Warn the user first, the file
 *  contains the re-identification key. */
export function saveSession(request) {
  return bridge().SaveSessionToFile(request);
}

/** loadSession() opens a session file; resolves to the Session object or
 *  null when the user cancels the dialog. */
export function loadSession() {
  return bridge().LoadSessionFromFile();
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

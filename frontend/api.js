// api.js, THE ONLY file allowed to call Go bound methods (CLAUDE.md §4).
//
// Wails exposes bound Go methods as window.go.backend.App.<Method>, each
// returning a Promise, and runtime events on window.runtime. Every
// view/module must go through these wrappers so the Go↔JS surface stays
// greppable in exactly one place.
//
// The namespace is "backend" (not "main") because the App struct lives in
// package backend (backend/app.go). Wails derives the window.go.<pkg>.<Struct>
// path from the bound struct's package, so the backend/ split moved this
// from window.go.main.App to window.go.backend.App. If every bound call ever
// starts throwing "Wails bridge not available", check this namespace first.

// EVERY wrapper below is `async`, and that is load-bearing rather than a style
// choice. bridge() THROWS when the bridge is absent, and a plain function that
// throws does so SYNCHRONOUSLY: the caller never gets a promise, so the
// `.catch()` every call site already writes is dead code and the exception
// escapes into whatever was rendering at the time. main.js boot() survived it
// (it awaits), but views/export.js ensureFormats() did not, and one missing
// bridge took the whole Export screen down with an uncaught error.
//
// Marking each wrapper `async` turns that throw into a REJECTED PROMISE, which
// is what every caller in this frontend is written against. It changes nothing
// when the bridge is present (an async function returning a promise is
// transparent) and it is what makes api.js's documented graceful degradation
// (frontend/CLAUDE.md) actually true. `onEvent` is deliberately NOT async: it
// returns nothing and no caller awaits it.
//
// Found by the Linux rendering harness (scripts/uitest/renderharness), where
// there is never a bridge, which is exactly the condition this contract is about.

/**
 * bridge() returns the bound App object, or throws a readable error when
 * the Wails bridge is not present (e.g. the page was opened in a normal
 * browser instead of the app window). Callers reach it only through the async
 * wrappers below, so what a caller sees is a rejection, never a synchronous
 * throw.
 */
function bridge() {
  const app = window.go?.backend?.App;
  if (!app) {
    throw new Error(
      "Wails bridge not available. This page must run inside the " +
      "doc-anonymiser application window, not a regular browser."
    );
  }
  return app;
}

/** ping() proves the JS↔Go bridge end to end (resolves to "pong"). */
export async function ping() {
  return bridge().Ping();
}

/**
 * probeOllama() asks Go whether a local Ollama server is running.
 * Never rejects for "Ollama missing", that is a normal state inside the
 * returned {available, models, detail} object (graceful degradation).
 */
export async function probeOllama() {
  return bridge().ProbeOllama();
}

// --- Documentation window --------------------------------

/** documentationURL() resolves to the asset path of the bundled
 *  documentation page, as declared by Go (app.go DocumentationURL). */
export async function documentationURL() {
  return bridge().DocumentationURL();
}

/**
 * openDocumentation() opens the bundled documentation in a SEPARATE
 * window.
 *
 * The URL comes from Go and is a path relative to the application's own
 * embedded asset server, so the new window loads embedded bytes and never
 * touches the network (CLAUDE.md section 4). It is deliberately NOT
 * runtime.BrowserOpenURL: that would hand the page to the system browser,
 * which cannot reach the embedded assets.
 *
 * Wails v2 drives a single native window per process (see app.go
 * DocumentationURL for the full reasoning), so the second window is one
 * the WebView opens itself. `name` keeps it to ONE documentation window:
 * clicking Documentation again focuses the existing one instead of
 * stacking copies.
 *
 * @returns {Promise<void>} rejects with an actionable message when the
 *   WebView refuses to open the window.
 */
export async function openDocumentation() {
  const url = await documentationURL();
  const opened = window.open(url, "doc-anonymiser-docs", "width=980,height=820");
  if (!opened) {
    throw new Error(
      "The documentation window could not be opened. If a popup blocker is " +
      "active for this application window, allow popups for it and try again.");
  }
  opened.focus?.();
}

// --- Import ------------------------------------------------------------

/** importFiles() opens the native multi-file dialog; resolves to an
 *  ImportResult {documents, errors}. */
export async function importFiles() {
  return bridge().ImportFiles();
}

/** removeDocument(name) drops one document; resolves to ImportResult. */
export async function removeDocument(name) {
  return bridge().RemoveDocument(name);
}

/**
 * resetRun() discards everything the last pipeline run produced on the Go side:
 * the placeholder registry (so numbering restarts from 1), the results, the
 * remembered request and the removed-value list. The documents and the settings
 * are kept. nav.js calls it when a backward move leaves the Anonymise step, so a
 * re-run does not inherit the previous run's numbers or its removals.
 */
export async function resetRun() {
  return bridge().ResetRun();
}

/**
 * resetSession() returns the Go session to a freshly launched state: no
 * documents, no registry, no results, no removed values, default settings. The
 * Import step's "start over" action calls it, paired with the frontend
 * resetState(), so a user beginning a separate anonymisation inherits nothing.
 * Rejects if a run or detection is still in progress.
 */
export async function resetSession() {
  return bridge().ResetSession();
}

/**
 * getDocumentSource(name) resolves to the SOURCE text of one imported
 * document, `{found, markdown, truncated, isGrid}`.
 *
 * This is the one place original text comes from. The Anonymise screen's
 * ORIGINAL pane uses it whenever the import list in the store does not hold
 * the document (a result left on screen after the file was removed, a view
 * restored by navigation). An unknown name resolves with `found: false`; it
 * does not reject.
 */
export async function getDocumentSource(name) {
  return bridge().GetDocumentSource(name);
}

// --- Settings ----------------------------------------------------------

/** applySettings(settings) stores settings and resolves to the fresh
 *  OllamaStatus (rejects with an actionable message on bad input). */
export async function applySettings(settings) {
  return bridge().ApplySettings(settings);
}

/** listOllamaModels() resolves to the installed model names. */
export async function listOllamaModels() {
  return bridge().ListOllamaModels();
}

// --- Allowlist -----------------------------------------

/** defaultAllowlist() resolves to the built-in suggested never-anonymise terms.
 *  They are a template source for the "Suggested terms" the user can add, not a
 *  seeded list: CR6 stopped auto-adding them, so nothing is in the allowlist at
 *  startup unless the user (or a restored session) put it there. */
export async function defaultAllowlist() {
  return bridge().DefaultAllowlist();
}

/** importAllowlistCSV() opens a native dialog for a CSV of terms;
 *  resolves to the parsed terms (null when the user cancels). */
export async function importAllowlistCSV() {
  return bridge().ImportAllowlistCSV();
}

/** saveAllowlistTemplate() saves the downloadable CSV template. */
export async function saveAllowlistTemplate() {
  return bridge().SaveAllowlistTemplate();
}

// --- Values screen ------------------------------------------

/**
 * runDetection(fileNames, allowTerms, aiScope) runs EVERY enabled detection
 * route in one call and resolves to a DetectionResult
 * {suggestions, proposals, phases, skipped, errors, cancelled, status}.
 *
 * This is the UI's detection entry point. It replaced two separate
 * calls whose lifecycles could not be reconciled: one cancellation slot, one
 * monotonic progress stream ("detection:progress") and exactly one terminal
 * event ("detection:done" or "detection:error") now cover the whole run.
 * Which routes run is decided in Go from the stored switches.
 *
 * aiScope, when set, restricts the LOCAL AI route to one document and, within
 * it, a set of its own units (page/slide/row/line): {docName, pages}, where
 * pages is a 1-based number[]. An empty pages array means the whole selected
 * document; null (the default) means every document, whole. It never affects
 * the Smart detection route, which always reads everything.
 *
 * A cancelled run RESOLVES with the partial findings and cancelled: true;
 * only a failure to start (no matching documents, a run already in flight)
 * rejects.
 */
export async function runDetection(fileNames, allowTerms, aiScope = null) {
  return bridge().RunDetection(fileNames, allowTerms, aiScope);
}

/** cancelDetection() aborts the in-flight detection run (no-op if idle).
 *  It shares Go's single cancellation slot, so it reaches whichever route is
 *  running, including mid-file. */
export async function cancelDetection() {
  return bridge().CancelDetection();
}

/** expandSpellings(value) resolves to the complete spelling list of one Value
 *  ({category, mainText, spellings, spellingPolicy}). A "curated" policy means
 *  Go derives nothing and returns exactly the list it was given. */
export async function expandSpellings(value) {
  return bridge().ExpandValueSpellings(value);
}

/** countTermMatches(term) resolves to {count, documents} for the live
 *  manual-entry preview. */
export async function countTermMatches(term) {
  return bridge().CountTermMatches(term);
}

/** validatePattern(expr) resolves to "" (valid) or the error message. */
export async function validatePattern(expr) {
  return bridge().ValidatePattern(expr);
}

/** patternMatches(expr) resolves to up to 20 sample matches across the
 *  loaded documents. */
export async function patternMatches(expr) {
  return bridge().PatternMatches(expr);
}

// --- Values, placeholders and removals (step 3,  Phases 4 and 5) ---
//
// These supersede setEntityPlaceholder / entityPlaceholder above, which are
// addressed by (category, mainText) and live on step 2, where the registry does
// not exist yet. Everything here is addressed BY PLACEHOLDER, because on step 3
// the user is looking at report rows and at marks in the Compare pane and both
// carry the placeholder.

/** valuePlaceholders() resolves to one row per value the session replaced:
 *  [{original, placeholder, category, count}], sorted by category then number.
 *  Empty before the first run, which is an empty table and not an error. */
export async function valuePlaceholders() {
  return bridge().ValuePlaceholders();
}

/**
 * setValuePlaceholder(current, next) renames a placeholder.
 *
 * REJECTS with an actionable message when the shape is wrong, when `current` is
 * not one this session assigned, or when `next` already belongs to another
 * value. The rename takes effect on the NEXT run, not retroactively: the text on
 * screen was produced with the old placeholder.
 */
export async function setValuePlaceholder(current, next) {
  return bridge().SetValuePlaceholder(current, next);
}

/** removeValue(placeholder) deletes a value from the session and resolves to
 *  {original, category, placeholder, derivedSpellings}. It does NOT re-run: the caller
 *  re-runs, because Go re-running from inside a bound method is a deadlock
 *  shape (RunPipeline holds an in-progress guard, FastRerun is synchronous). */
export async function removeValue(placeholder) {
  return bridge().RemoveValue(placeholder);
}

/** restoreValue(placeholder) undoes a removal. The value returns on the next
 *  run with a NEW number: the old one stays retired. */
export async function restoreValue(placeholder) {
  return bridge().RestoreValue(placeholder);
}

/** listRemovedValues() resolves to the collapsed removed list. */
export async function listRemovedValues() {
  return bridge().ListRemovedValues();
}

/** validateValues(request) resolves to {blocking, warnings}. A blocking
 *  conflict refuses the run, so a screen calls this to say so beforehand. */
export async function validateValues(request) {
  return bridge().ValidateValues(request);
}

/**
 * checkIntersections(request) resolves to {intersections} : the values another
 * detection route also claims, so the Identify screen can warn on the card that
 * owns a value instead of the user finding out after a run.
 *
 * It mutates nothing on the Go side (no placeholder is minted, the registry is
 * untouched), so it is safe to call while the user is still editing values. An
 * empty list is the normal "nothing overlaps" answer, not an error.
 */
export async function checkIntersections(request) {
  return bridge().CheckIntersections(request);
}

// --- Run screen -------------------------------------------------

/** runPipeline(request) starts the pipeline; resolves immediately (results
 *  arrive on the "pipeline:done" event, progress on "pipeline:progress"). */
export async function runPipeline(request) {
  return bridge().RunPipeline(request);
}

/** cancelPipeline() aborts the in-flight run. */
export async function cancelPipeline() {
  return bridge().CancelPipeline();
}

/** fastRerun(request) re-runs the deterministic passes only (no LLM),
 *  resolving directly to the fresh Results. */
export async function fastRerun(request) {
  return bridge().FastRerun(request);
}

/** getMapping() resolves to the placeholder → {original, category}
 *  lookup for tooltips and reassignment (empty before the first run). */
export async function getMapping() {
  return bridge().GetMapping();
}

// --- Export screen -----------------------------------------------

/** exportDocumentFormats(name) resolves to the offered extensions
 *  (default first) for one result document. */
export async function exportDocumentFormats(name) {
  return bridge().ExportDocumentFormats(name);
}

/** saveDocument(name, ext) opens a save dialog for one document. */
export async function saveDocument(name, ext) {
  return bridge().SaveDocument(name, ext);
}

/** getSameFormatMetadata(name, ext) resolves to {fields, filename}: the
 *  document properties with proposed replacements plus the proposed
 *  anonymised filename, for the review panel. */
export async function getSameFormatMetadata(name, ext) {
  return bridge().GetSameFormatMetadata(name, ext);
}

/** saveSameFormat(name, ext, fields, filename) writes the same-format
 *  copy with the REVIEWED metadata values and filename. */
export async function saveSameFormat(name, ext, fields, filename) {
  return bridge().SaveSameFormat(name, ext, fields, filename);
}

/**
 * chooseExportFolder() opens the native folder picker and resolves to the
 * chosen path, or "" when the user cancelled.
 * Nothing is written; it only picks.
 */
export async function chooseExportFolder() {
  return bridge().ChooseExportFolder();
}

/**
 * exportAllZipTo(dir) writes the batch zip straight into a folder the user
 * already chose, with no second dialog, and resolves to the full path written.
 *
 * This is the only write with no dialog in front of it, and it is allowed
 * because the folder was chosen explicitly and the zip carries no
 * re-identification key. An existing archive is never
 * overwritten: the new one is numbered.
 */
export async function exportAllZipTo(dir) {
  return bridge().ExportAllZipTo(dir);
}

/** copyDocument(name) puts the anonymised text on the clipboard. */
export async function copyDocument(name) {
  return bridge().CopyDocument(name);
}

/**
 * copyText(text) puts an arbitrary short string on the clipboard.
 *
 * Clipboard access goes through Go, as copyDocument does. Rejects with an
 * actionable message when the selection is empty or longer than the cap, which
 * is a mis-drag guard rather than a product limit.
 */
export async function copyText(text) {
  return bridge().CopyText(text);
}

/** exportMapping(format) saves the re-identification key ("csv"/"json").
 *  Call ONLY after the user confirmed the sensitivity warning. */
export async function exportMapping(format) {
  return bridge().ExportMapping(format);
}

/** exportReport(format) saves the run report ("json"/"md"). */
export async function exportReport(format) {
  return bridge().ExportReport(format);
}

/** saveSession(request) persists the session (values, allowlist, patterns,
 *  settings, registry). Warn the user first, the file contains the
 *  re-identification key. */
export async function saveSession(request) {
  return bridge().SaveSessionToFile(request);
}

/** loadSession() opens a session file; resolves to the Session object or
 *  null when the user cancels the dialog. */
export async function loadSession() {
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

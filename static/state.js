// state.js — the single source of truth for frontend state (CLAUDE.md §4).
//
// Minimal store: a plain object plus subscribe/notify. Views never keep
// their own copies of shared data — they read from getState() and
// re-render when notified. Mutations go through setState() (shallow merge)
// or the exported reducer functions, so every state change is observable
// in one place.
//
// This module is PURE JavaScript with no DOM and no Wails access, so it is
// unit-testable with `node --test` (state.test.js) — the reason views must
// stay logic-free (BUILD.md Phase 6).

// WIZARD_STEPS defines the fixed wizard order (CLAUDE.md wizard flow).
export const WIZARD_STEPS = ["import", "configure", "entities", "run", "export"];

// The initial application state. Every field is documented; grow it here,
// never ad hoc in views.
const initialState = {
  // Bridge self-test: null = not yet run, "pong" = OK, anything else =
  // error message to display.
  bridge: null,

  // Last Ollama probe result ({available, models, detail}) or null.
  // state.ollama?.available drives EVERY LLM control's disabled state.
  ollama: null,

  // Current wizard step, one of WIZARD_STEPS.
  step: "import",

  // Imported documents: array of DocumentInfo objects from Go
  // ({name, format, sizeBytes, warnings, markdown, experimental, isGrid}).
  documents: [],

  // Import errors from the last import action (shown as dismissible list).
  importErrors: [],

  // Name of the document selected in the preview pane (or null).
  previewDoc: null,

  // Settings mirror (source of truth lives in Go; this copy renders the
  // Configure screen): {level, ollamaPort, model}.
  settings: { level: "medium", ollamaPort: 11434, model: "" },

  // Entity review state (Phase 7): array of
  // {category, canonical, manualVariants, status: "accepted"|"denied"}.
  entities: [],

  // Allowlist terms (display spellings).
  allowlist: [],

  // Custom regex patterns: array of {expr, error} (error = compile
  // message or null).
  patterns: [],

  // Ordered simple-replace rules: {find, replace, caseSensitive}.
  simpleRules: [],

  // Pipeline execution state (Phase 8).
  running: false,
  progress: null, // {stage, docIndex, docCount, docName} or null
  results: null,  // engine.Results mirror or null
};

// state is module-private; mutate only via setState/reducers.
let state = structuredClone(initialState);

// Subscribers are callbacks invoked after every state change.
const listeners = new Set();

/** getState() returns the live state object (treat as read-only). */
export function getState() {
  return state;
}

/** resetState() restores the initial state (used by tests and session load). */
export function resetState() {
  state = structuredClone(initialState);
  notify();
}

/**
 * setState(patch) shallow-merges the patch and notifies subscribers.
 * @param {object} patch fields to update.
 */
export function setState(patch) {
  Object.assign(state, patch);
  notify();
}

/** subscribe(fn) registers a callback; returns an unsubscribe function. */
export function subscribe(fn) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

function notify() {
  for (const fn of listeners) fn(state);
}

// --- Navigation guards ------------------------------------------------------

/**
 * canGoTo(step, s) implements the wizard guards: no step beyond Import
 * without documents, and no Export before a run produced results.
 * @param {string} step target step name
 * @param {object} [s] state (defaults to the live state; injectable for tests)
 * @returns {boolean}
 */
export function canGoTo(step, s = state) {
  if (!WIZARD_STEPS.includes(step)) return false;
  const idx = WIZARD_STEPS.indexOf(step);
  if (idx > 0 && s.documents.length === 0) return false; // nothing imported yet
  if (step === "export" && !s.results) return false;     // nothing to export yet
  return true;
}

/** goTo(step) navigates when the guard allows it; returns whether it did. */
export function goTo(step) {
  if (!canGoTo(step)) return false;
  setState({ step });
  return true;
}

/** nextStep()/prevStep() move linearly through the wizard. */
export function nextStep() {
  const idx = WIZARD_STEPS.indexOf(state.step);
  return idx < WIZARD_STEPS.length - 1 && goTo(WIZARD_STEPS[idx + 1]);
}

export function prevStep() {
  const idx = WIZARD_STEPS.indexOf(state.step);
  return idx > 0 && goTo(WIZARD_STEPS[idx - 1]);
}

// --- Document reducers -------------------------------------------------------

/**
 * applyImportResult(result) folds an ImportResult from Go into the state,
 * keeping the preview selection valid.
 */
export function applyImportResult(result) {
  const documents = result.documents ?? [];
  const previewStillValid = documents.some((d) => d.name === state.previewDoc);
  setState({
    documents,
    importErrors: result.errors ?? [],
    previewDoc: previewStillValid ? state.previewDoc : (documents[0]?.name ?? null),
  });
}

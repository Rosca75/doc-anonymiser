// state.js, the single source of truth for frontend state (CLAUDE.md §4).
//
// Minimal store: a plain object plus subscribe/notify. Views never keep
// their own copies of shared data, they read from getState() and
// re-render when notified. Mutations go through setState() (shallow merge)
// or the exported reducer functions, so every state change is observable
// in one place.
//
// This module is PURE JavaScript with no DOM and no Wails access, so it is
// unit-testable with `node --test` (state.test.js), the reason views must
// stay logic-free (BUILD.md Phase 6).

// WIZARD_STEPS defines the fixed wizard order (CLAUDE.md wizard flow).
export const WIZARD_STEPS = ["import", "configure", "entities", "run", "export"];

// The initial application state. Every field is documented; grow it here,
// never ad hoc in views.
const initialState = {
  // Top-level screen: "home" (landing page), "wizard" (the 5-step flow) or
  // "docs" (documentation placeholder). Leaving the wizard NEVER clears
  // wizard state, so documents and entities survive navigation
  // (BUILD-02 Phase 2a).
  screen: "home",

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

  // Import screen split ratio: fraction of width given to the document
  // list pane (the preview takes the rest). User-draggable divider,
  // clamped to keep both panes usable (BUILD-02 Phase 2g).
  importSplit: 0.5,

  // Settings mirror (source of truth lives in Go; this copy renders the
  // Configure screen): {level, categories, ollamaPort, model}. level is
  // the LAST CHOSEN PRESET; categories is the granular switch set the
  // pipeline obeys (BUILD-02 Phase 3). The categories map is filled below
  // after presetCategories is defined.
  settings: { level: "medium", categories: null, ollamaPort: 11434, model: "" },

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

// --- Category presets (BUILD-02 Phase 3, mirrors engine.PresetSelection) ----

// Category keys, split by preset tier. Must stay in sync with the Go side
// (engine/pipeline.go AllPIICategories / AllEntityCategories).
export const HARD_PII_CATEGORIES = ["email", "url", "iban", "vat", "matricule", "phone"];
export const ADVANCED_PII_CATEGORIES = ["amount", "date"];
export const NAME_CATEGORIES = ["client_names", "project_names", "internal_names", "person_names"];
export const ADVANCED_ENTITY_CATEGORIES = ["organisation_names", "location_names"];
export const ALL_CATEGORIES = [
  ...HARD_PII_CATEGORIES, ...ADVANCED_PII_CATEGORIES,
  ...NAME_CATEGORIES, "custom_patterns", ...ADVANCED_ENTITY_CATEGORIES,
];

/**
 * presetCategories(level) reproduces engine.PresetSelection exactly:
 * soft = hard PII + client/project/internal + custom patterns;
 * medium = soft + persons; advanced = everything.
 */
export function presetCategories(level) {
  const sel = {};
  for (const c of ALL_CATEGORIES) sel[c] = false;
  for (const c of HARD_PII_CATEGORIES) sel[c] = true;
  sel.client_names = sel.project_names = sel.internal_names = true;
  sel.custom_patterns = true;
  if (level === "medium" || level === "advanced") sel.person_names = true;
  if (level === "advanced") {
    for (const c of [...ADVANCED_PII_CATEGORIES, ...ADVANCED_ENTITY_CATEGORIES]) sel[c] = true;
  }
  return sel;
}

initialState.settings.categories = presetCategories("medium");

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

// --- Category selection reducers (BUILD-02 Phase 3) --------------------------

/**
 * applyPreset(level) sets the level AND fills the category switches from
 * the preset (the "Soft/Standard/Thorough" chips of the configure screen).
 */
export function applyPreset(level) {
  setState({
    settings: { ...state.settings, level, categories: presetCategories(level) },
  });
}

/**
 * toggleCategory(key, on) flips one granular switch. The level is kept as
 * the last chosen preset (the UI shows "Custom" when the selection
 * diverges, see selectionPresetName).
 */
export function toggleCategory(key, on) {
  if (!ALL_CATEGORIES.includes(key)) return false;
  setState({
    settings: {
      ...state.settings,
      categories: { ...state.settings.categories, [key]: !!on },
    },
  });
  return true;
}

/**
 * selectionPresetName(categories) returns "soft" | "medium" | "advanced"
 * when the selection exactly matches that preset, else "custom".
 */
export function selectionPresetName(categories) {
  for (const level of ["soft", "medium", "advanced"]) {
    const preset = presetCategories(level);
    if (ALL_CATEGORIES.every((c) => !!categories?.[c] === preset[c])) return level;
  }
  return "custom";
}

// --- Screen navigation (BUILD-02 Phase 2a) -----------------------------------

/** SCREENS: the valid top-level screens. */
export const SCREENS = ["home", "wizard", "docs"];

/**
 * goToScreen(name) switches the top-level screen. Wizard state (documents,
 * entities, step, ...) is deliberately untouched, so Home and back loses
 * nothing. Unknown names are rejected (returns false).
 */
export function goToScreen(name) {
  if (!SCREENS.includes(name)) return false;
  setState({ screen: name });
  return true;
}

/**
 * setImportSplit(ratio) stores the import-screen divider position as a
 * fraction of the width given to the list pane. Non-numbers are rejected;
 * numbers are clamped to [0.2, 0.8] so neither pane can collapse.
 * Returns the stored value (or null when rejected).
 */
export function setImportSplit(ratio) {
  if (typeof ratio !== "number" || Number.isNaN(ratio)) return null;
  const clamped = Math.min(0.8, Math.max(0.2, ratio));
  setState({ importSplit: clamped });
  return clamped;
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

// --- Entity review reducers (Phase 7) ----------------------------------------
//
// Entities are keyed by (category, canonical), one row per real-world
// entity. status "accepted" entities feed the pipeline; "denied" ones are
// kept visible (struck through) so the user sees what discovery proposed.

/** entityKey(category, canonical), case-insensitive identity of a row. */
export function entityKey(category, canonical) {
  return `${category}|${canonical.trim().toLowerCase()}`;
}

/**
 * addEntities(items) adds proposals or manual entries, skipping duplicates.
 * items: [{category, canonical, variants?}], variants (from Go expansion)
 * may be attached later via setEntityVariants.
 */
export function addEntities(items) {
  const existing = new Set(state.entities.map((e) => entityKey(e.category, e.canonical)));
  const added = [];
  for (const item of items) {
    const canonical = (item.canonical ?? "").trim();
    if (!canonical || existing.has(entityKey(item.category, canonical))) continue;
    existing.add(entityKey(item.category, canonical));
    added.push({
      category: item.category,
      canonical,
      manualVariants: item.manualVariants ?? [],
      variants: item.variants ?? [],
      status: "accepted",
    });
  }
  if (added.length) setState({ entities: [...state.entities, ...added] });
  return added.length;
}

/** setEntityStatus(category, canonical, status), accept or deny a row. */
export function setEntityStatus(category, canonical, status) {
  setState({
    entities: state.entities.map((e) =>
      entityKey(e.category, e.canonical) === entityKey(category, canonical)
        ? { ...e, status }
        : e),
  });
}

/**
 * editEntity(category, oldCanonical, newCanonical) renames a row (the
 * "edit" action of the review table). A rename that would collide with an
 * existing row is rejected (returns false).
 */
export function editEntity(category, oldCanonical, newCanonical) {
  const next = (newCanonical ?? "").trim();
  if (!next) return false;
  const collision = state.entities.some((e) =>
    entityKey(e.category, e.canonical) === entityKey(category, next) &&
    entityKey(e.category, e.canonical) !== entityKey(category, oldCanonical));
  if (collision) return false;
  setState({
    entities: state.entities.map((e) =>
      entityKey(e.category, e.canonical) === entityKey(category, oldCanonical)
        ? { ...e, canonical: next, variants: [] } // variants re-expanded by the view
        : e),
  });
  return true;
}

/** removeEntity(category, canonical) deletes a row outright. */
export function removeEntity(category, canonical) {
  setState({
    entities: state.entities.filter((e) =>
      entityKey(e.category, e.canonical) !== entityKey(category, canonical)),
  });
}

/** setEntityVariants stores the Go-expanded variant list on a row. */
export function setEntityVariants(category, canonical, variants) {
  setState({
    entities: state.entities.map((e) =>
      entityKey(e.category, e.canonical) === entityKey(category, canonical)
        ? { ...e, variants }
        : e),
  });
}

/** addManualVariant appends a user-typed variant to a row (deduplicated). */
export function addManualVariant(category, canonical, variant) {
  const v = (variant ?? "").trim();
  if (!v) return;
  setState({
    entities: state.entities.map((e) => {
      if (entityKey(e.category, e.canonical) !== entityKey(category, canonical)) return e;
      if (e.manualVariants.some((m) => m.toLowerCase() === v.toLowerCase())) return e;
      return { ...e, manualVariants: [...e.manualVariants, v], variants: [] };
    }),
  });
}

/** acceptedEntities(s), the pipeline-ready entity list. */
export function acceptedEntities(s = state) {
  return s.entities
    .filter((e) => e.status === "accepted")
    .map((e) => ({ category: e.category, canonical: e.canonical, manualVariants: e.manualVariants }));
}

// --- Allowlist / pattern reducers ---------------------------------------------

/** addAllowTerm(term), case-insensitive dedupe, keeps typed spelling. */
export function addAllowTerm(term) {
  const t = (term ?? "").trim();
  if (!t || state.allowlist.some((x) => x.toLowerCase() === t.toLowerCase())) return;
  setState({ allowlist: [...state.allowlist, t] });
}

export function removeAllowTerm(term) {
  setState({ allowlist: state.allowlist.filter((x) => x.toLowerCase() !== term.toLowerCase()) });
}

/** addPattern(expr, error) stores a custom regex with its validation state. */
export function addPattern(expr, error = null) {
  const e = (expr ?? "").trim();
  if (!e || state.patterns.some((p) => p.expr === e)) return;
  setState({ patterns: [...state.patterns, { expr: e, error }] });
}

export function removePattern(expr) {
  setState({ patterns: state.patterns.filter((p) => p.expr !== expr) });
}

/** validPatterns(s), the pipeline-ready pattern list (compile-clean only). */
export function validPatterns(s = state) {
  return s.patterns.filter((p) => !p.error).map((p) => ({ expr: p.expr }));
}

// --- Simple-replace rule reducers (Phase 8) ----------------------------------
//
// Rules are ORDERED: rule 1 runs before rule 2 and later rules see earlier
// output (engine/simplereplace.go). Hence the move reducer.

/** addSimpleRule({find, replace, caseSensitive}) appends a rule. */
export function addSimpleRule(rule) {
  const find = (rule.find ?? "").trim();
  if (!find) return false; // an empty needle is a no-op rule
  setState({
    simpleRules: [...state.simpleRules, {
      find,
      replace: rule.replace ?? "",
      caseSensitive: !!rule.caseSensitive,
    }],
  });
  return true;
}

/** removeSimpleRule(index) deletes the rule at the given position. */
export function removeSimpleRule(index) {
  setState({ simpleRules: state.simpleRules.filter((_, i) => i !== index) });
}

/** moveSimpleRule(index, delta) reorders a rule (delta ±1); returns
 *  whether a move happened. */
export function moveSimpleRule(index, delta) {
  const to = index + delta;
  if (index < 0 || index >= state.simpleRules.length || to < 0 || to >= state.simpleRules.length) {
    return false;
  }
  const rules = [...state.simpleRules];
  [rules[index], rules[to]] = [rules[to], rules[index]];
  setState({ simpleRules: rules });
  return true;
}

/** buildRunRequest(useDeepScan, s) assembles the Go RunRequest from the
 *  current state, the single place the pipeline payload is shaped. */
export function buildRunRequest(useDeepScan, s = state) {
  return {
    entities: acceptedEntities(s),
    allowTerms: s.allowlist,
    patterns: validPatterns(s),
    // The granular selection travels with every run request so the Go
    // pipeline always sees exactly what the configure screen shows.
    categories: s.settings.categories ?? presetCategories(s.settings.level),
    simpleRules: s.simpleRules,
    useDeepScan: !!useDeepScan,
  };
}

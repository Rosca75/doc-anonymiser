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
// BUILD-04 CR3 renamed the third step's token from "entities" to
// "values". Saved sessions written before that rename are migrated by
// migrateStep() below, so an older file never lands the wizard on a step
// that no longer exists.
export const WIZARD_STEPS = ["import", "configure", "values", "run", "export"];

// The initial application state. Every field is documented; grow it here,
// never ad hoc in views.
const initialState = {
  // Top-level screen: "home" (landing page) or "wizard" (the 5-step
  // flow). Leaving the wizard NEVER clears wizard state, so documents and
  // values survive navigation (BUILD-02 Phase 2a). Documentation is not a
  // screen: it opens in its own window (BUILD-04 CR6).
  screen: "home",

  // Shell-level error message, or null. Used for failures that belong to
  // the application chrome rather than to any one view, such as the
  // documentation window refusing to open. Rendered as a dismissible
  // banner above the active view (BUILD-04 CR6).
  shellError: null,

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
  // useAI is the master "Use local AI" toggle (BUILD-02 Phase 6d):
  // null = not yet decided (defaults to Ollama availability after the
  // first probe), true/false = explicit user choice.
  // contextSize is the Ollama num_ctx setting (Phase 5b), default 8192.
  // minConfidence is the detection-confidence floor (BUILD-04 CR9), 0 to
  // 1 on the engine's scale. 0 is the default and keeps every detection,
  // which is exactly the behaviour before the setting existed.
  settings: {
    level: "medium", categories: null, ollamaPort: 11434, model: "",
    contextSize: 8192, useAI: null, minConfidence: 0,
    // smartDetect is the BUILD-04 CR13 tuning for the offline Smart
    // detection pass, matching engine.SmartDetectOptions field for field.
    // The defaults are the STRICTER ones (engine
    // DefaultSmartDetectOptions), because over-detection was the reported
    // problem; a user who wants everything back sets them to 0/false.
    smartDetect: {
      minLength: 4,
      minOccurrences: 1,
      excludeCommonWords: true,
      minConfidence: 0.5,
    },
  },

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

  // Discovery run state (BUILD-02 Phase 7c):
  // {running, current, total, file} or null when idle.
  discovery: null,

  // Unified candidate review list (BUILD-02 Phase 9b): candidates from
  // any discovery method wait HERE until explicitly accepted; nothing
  // flows into entities without user confirmation. Each row:
  // {source: "smart"|"local-ai"|"cloud-ai", text, category, count, contexts}.
  candidates: [],

  // Placeholder → {original, category} lookup from the last run
  // (BUILD-02 Phase 10a). MEMORY NOTE: this is the re-identification
  // key; it stays in the app process like the Go registry and is
  // cleared with the results.
  mapping: null,

  // Same-format metadata review state per document (BUILD-02 Phase 12):
  // {docName: {ext, filename, fields: [{part, name, value, proposed,
  // changed, finalValue}]}}. Decisions persist for the session so the
  // review only happens before the FIRST same-format export of a file.
  metaReview: {},
};

// --- Category presets (BUILD-02 Phase 3, mirrors engine.PresetSelection) ----

// Category keys, split by preset tier. Must stay in sync with the Go side
// (engine/pipeline.go AllPIICategories / AllEntityCategories).
export const HARD_PII_CATEGORIES = ["email", "url", "iban", "vat", "matricule", "phone"];
// The extended recognizers BUILD-03 added to the engine. They are HARD
// PII exactly like the group above, so every preset switches them on
// (engine/pipeline.go PresetSelection). BUILD-04 CR9 finally surfaces
// them in the Configure screen; until then they were detectable but
// invisible, which is why the list is separate rather than merged: the
// Configure screen shows them as their own group.
export const EXTENDED_PII_CATEGORIES = [
  "credit_card", "uk_nhs", "ip_address", "mac_address",
  "crypto", "database_uri", "de_steuer_id", "es_nif",
];
export const ADVANCED_PII_CATEGORIES = ["amount", "date"];
export const NAME_CATEGORIES = ["client_names", "project_names", "internal_names", "person_names"];
export const ADVANCED_ENTITY_CATEGORIES = ["organisation_names", "location_names"];
export const ALL_CATEGORIES = [
  ...HARD_PII_CATEGORIES, ...EXTENDED_PII_CATEGORIES, ...ADVANCED_PII_CATEGORIES,
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
  // Extended recognizers are hard PII at every level, matching the Go
  // PresetSelection exactly (BUILD-04 CR9).
  for (const c of EXTENDED_PII_CATEGORIES) sel[c] = true;
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
 * setCategoryGroup(keys, on) flips a whole group of switches in ONE
 * state change (BUILD-04 CR10, the "Select all" / "Deselect all" buttons
 * on each Configure group).
 *
 * Doing it in one setState rather than looping toggleCategory matters:
 * every setState repaints, so a loop over eight keys would repaint eight
 * times and, before the CR12 fix, scroll the page eight times.
 *
 * Unknown keys are ignored rather than rejected, so a group definition
 * that drifts ahead of ALL_CATEGORIES degrades to flipping what it can.
 * Returns how many switches were actually changed.
 *
 * @param {string[]} keys category keys to set
 * @param {boolean} on the value to set them all to
 * @returns {number} how many switches changed value
 */
export function setCategoryGroup(keys, on) {
  const value = !!on;
  const categories = { ...state.settings.categories };
  let changed = 0;
  for (const key of keys ?? []) {
    if (!ALL_CATEGORIES.includes(key)) continue;
    if (!!categories[key] === value) continue;
    categories[key] = value;
    changed++;
  }
  if (changed) setState({ settings: { ...state.settings, categories } });
  return changed;
}

/**
 * setMinConfidence(value) stores the detection-confidence floor
 * (BUILD-04 CR9). Values outside 0 to 1 are rejected (returns null)
 * rather than clamped, so a bad caller is visible instead of silently
 * changing what gets replaced.
 * @param {number} value the floor, 0 (keep everything) to 1 (strictest)
 * @returns {number|null} the stored value, or null when rejected
 */
export function setMinConfidence(value) {
  if (typeof value !== "number" || Number.isNaN(value) || value < 0 || value > 1) {
    return null;
  }
  setState({ settings: { ...state.settings, minConfidence: value } });
  return value;
}

// SMART_DETECT_DEFAULTS mirrors engine.DefaultSmartDetectOptions. It is
// exported so a session loaded from an older file, which has no
// smartDetect block at all, can be filled with the same defaults a fresh
// session starts from (BUILD-04 CR13, ground rule 5).
export const SMART_DETECT_DEFAULTS = {
  minLength: 4,
  minOccurrences: 1,
  excludeCommonWords: true,
  minConfidence: 0.5,
};

/**
 * setSmartDetectOptions(patch) merges a partial tuning into the settings
 * (BUILD-04 CR13). Only the four known keys are accepted, and each is
 * validated: a bad value is IGNORED rather than stored, because these
 * options decide what the user gets to review, and a silently broken one
 * would look like Smart detection being broken.
 * @param {object} patch any subset of the four options
 * @returns {object} the stored options after the merge
 */
export function setSmartDetectOptions(patch) {
  const current = { ...SMART_DETECT_DEFAULTS, ...(state.settings.smartDetect ?? {}) };
  const next = { ...current };
  const p = patch ?? {};
  if (Number.isInteger(p.minLength) && p.minLength >= 0 && p.minLength <= 40) {
    next.minLength = p.minLength;
  }
  if (Number.isInteger(p.minOccurrences) && p.minOccurrences >= 0 && p.minOccurrences <= 100) {
    next.minOccurrences = p.minOccurrences;
  }
  if (typeof p.excludeCommonWords === "boolean") {
    next.excludeCommonWords = p.excludeCommonWords;
  }
  if (typeof p.minConfidence === "number" && !Number.isNaN(p.minConfidence) &&
      p.minConfidence >= 0 && p.minConfidence <= 1) {
    next.minConfidence = p.minConfidence;
  }
  setState({ settings: { ...state.settings, smartDetect: next } });
  return next;
}

/**
 * smartDetectOptions(s) is the tuning to SEND to Go: the stored options
 * with every default filled in, so a session written before CR13 (no
 * smartDetect block) still produces a complete payload.
 */
export function smartDetectOptions(s = state) {
  return { ...SMART_DETECT_DEFAULTS, ...(s.settings.smartDetect ?? {}) };
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

// --- Local-AI gating (BUILD-02 Phase 6d) --------------------------------------

/**
 * setUseAI(on) records the user's explicit "Use local AI" choice.
 */
export function setUseAI(on) {
  setState({ settings: { ...state.settings, useAI: !!on } });
}

/**
 * defaultUseAIFromProbe(available) fills the toggle's DEFAULT after the
 * first Ollama probe: on when detected, off otherwise. A prior explicit
 * user choice (true/false) is never overwritten.
 */
export function defaultUseAIFromProbe(available) {
  if (state.settings.useAI !== null) return;
  setState({ settings: { ...state.settings, useAI: !!available } });
}

/**
 * llmEnabled(s) is THE gate for every AI-dependent control (discovery,
 * deep-scan): the master toggle must be on AND Ollama must be reachable.
 */
export function llmEnabled(s = state) {
  return !!(s.settings.useAI && s.ollama?.available);
}

// --- Screen navigation (BUILD-02 Phase 2a) -----------------------------------

/** SCREENS: the valid top-level screens.
 *
 * "docs" was retired by BUILD-04 CR6: the documentation is no longer an
 * in-app screen, it opens in a separate window (api.js
 * openDocumentation). goToScreen("docs") therefore returns false now,
 * exactly like any other unknown name. */
export const SCREENS = ["home", "wizard"];

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

// LEGACY_STEP_TOKENS maps wizard step tokens written by OLDER versions of
// the application onto their current names (BUILD-04 CR3). Session files
// and any other persisted step reference pass through migrateStep() so a
// rename in the UI can never strand a user on a step that no longer
// exists. Grow this table on every future token rename; never remove an
// entry, old files keep existing forever.
export const LEGACY_STEP_TOKENS = {
  entities: "values",
};

/**
 * migrateStep(step) returns the CURRENT token for a possibly legacy step
 * name. Unknown tokens (a corrupted or hand-edited file) fall back to
 * "import", the only step that is always reachable.
 * @param {string} step token read from a persisted session
 * @returns {string} a token guaranteed to be in WIZARD_STEPS
 */
export function migrateStep(step) {
  const migrated = LEGACY_STEP_TOKENS[step] ?? step;
  return WIZARD_STEPS.includes(migrated) ? migrated : "import";
}

// --- Per-step reset (BUILD-04 CR16) -----------------------------------------

/**
 * STEP_RESETS says what each wizard step OWNS, and therefore what going
 * back past it clears. It is a table rather than a switch so the answer
 * to "what does this step own?" is readable in one place, and so the
 * cross-step test matrix can iterate it.
 *
 * Two things are deliberately absent from every entry:
 *
 *   documents  Imported files are step 1 data and survive ALL navigation
 *              (BUILD-04 section 4.2). Re-importing a batch because the
 *              user stepped back one screen would be indefensible.
 *   allowlist  The never-anonymise list is shared by the Configure and
 *              Values screens and is curated across the whole session, so
 *              it belongs to neither step.
 *
 * The Ollama connection settings (port, model, context size, useAI) are
 * likewise left alone by the configure reset: they describe the machine,
 * not the choices made about this batch of documents.
 */
export const STEP_RESETS = {
  // Import owns only the error strip of the last import action.
  import: () => ({ importErrors: [] }),
  // Configure owns what to anonymise and how strictly.
  configure: () => ({
    settings: {
      ...state.settings,
      categories: presetCategories(state.settings.level),
      minConfidence: 0,
      smartDetect: { ...SMART_DETECT_DEFAULTS },
    },
  }),
  // Values owns the values to replace and everything proposed for review.
  values: () => ({
    entities: [],
    candidates: [],
    patterns: [],
    discovery: null,
  }),
  // Run owns the run itself and everything it produced.
  run: () => ({
    running: false,
    progress: null,
    results: null,
    mapping: null,
  }),
  // Export owns the per-document metadata review decisions.
  export: () => ({ metaReview: {} }),
};

/**
 * resetStep(step) clears exactly what that step owns (BUILD-04 CR16).
 * Unknown steps are rejected rather than silently ignored, so a typo in a
 * caller cannot quietly skip a reset the user confirmed.
 * @param {string} step a token from WIZARD_STEPS
 * @returns {boolean} whether a reset was applied
 */
export function resetStep(step) {
  const reset = STEP_RESETS[step];
  if (!reset) return false;
  setState(reset());
  return true;
}

/**
 * isBackward(from, to) reports whether moving from one step to another
 * goes BACK through the wizard. main.js asks before navigating, because
 * only a backward move offers to reset the step being left.
 * @param {string} from the current step
 * @param {string} to the requested step
 * @returns {boolean}
 */
export function isBackward(from, to) {
  const fromIndex = WIZARD_STEPS.indexOf(from);
  const toIndex = WIZARD_STEPS.indexOf(to);
  if (fromIndex < 0 || toIndex < 0) return false;
  return toIndex < fromIndex;
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
      // variants: null = "not yet expanded" (the view shows a pending
      // placeholder and triggers expansion); [] = "expanded, none found"
      // (explicit empty state). Distinguishing the two is the heart of
      // the BUILD-02 Phase 7a fix.
      variants: item.variants ?? null,
      variantError: null,
      excludedVariants: item.excludedVariants ?? [],
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
        // variants: null = pending; ONLY this row re-expands (Phase 7a).
        ? { ...e, canonical: next, variants: null, variantError: null }
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

/** setEntityVariants stores the Go-expanded variant list on a row
 *  ([] is a valid "no variants" answer, distinct from pending null). */
export function setEntityVariants(category, canonical, variants) {
  setState({
    entities: state.entities.map((e) =>
      entityKey(e.category, e.canonical) === entityKey(category, canonical)
        ? { ...e, variants: variants ?? [], variantError: null }
        : e),
  });
}

/** setEntityVariantError records a failed expansion so the row shows the
 *  Go error text instead of a forever-pending placeholder (Phase 7a). */
export function setEntityVariantError(category, canonical, message) {
  setState({
    entities: state.entities.map((e) =>
      entityKey(e.category, e.canonical) === entityKey(category, canonical)
        ? { ...e, variantError: message ?? "expansion failed" }
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
      // Adding a variant re-expands ONLY this row (variants back to
      // pending null, Phase 7a).
      return { ...e, manualVariants: [...e.manualVariants, v], variants: null, variantError: null };
    }),
  });
}

/** acceptedEntities(s), the pipeline-ready entity list (manual and
 *  excluded variants travel to Go so expansion matches the UI). */
export function acceptedEntities(s = state) {
  return s.entities
    .filter((e) => e.status === "accepted")
    .map((e) => ({
      category: e.category,
      canonical: e.canonical,
      manualVariants: e.manualVariants,
      excludedVariants: e.excludedVariants ?? [],
    }));
}

// --- Candidate review reducers (BUILD-02 Phase 9b) ----------------------------
//
// The review gate: discovery methods ADD candidates; only an explicit
// accept turns a candidate into an entity. Candidates are keyed by
// lower-cased text (one row per distinct name across sources).

/** candidateKey(text), case-insensitive identity of a candidate row. */
export function candidateKey(text) {
  return (text ?? "").trim().toLowerCase();
}

/**
 * addCandidates(items, source) merges discovery output into the review
 * list. Existing rows keep their spot (first source wins); names already
 * present as entities are skipped. Returns how many rows were added.
 */
export function addCandidates(items, source) {
  const existing = new Set(state.candidates.map((c) => candidateKey(c.text)));
  const asEntities = new Set(state.entities.map((e) => e.canonical.trim().toLowerCase()));
  const added = [];
  for (const item of items ?? []) {
    const text = (item.text ?? "").trim();
    const key = candidateKey(text);
    if (!text || existing.has(key) || asEntities.has(key)) continue;
    existing.add(key);
    added.push({
      source,
      text,
      category: item.category ?? "person_names",
      count: item.count ?? 0,
      contexts: item.contexts ?? [],
    });
  }
  if (added.length) setState({ candidates: [...state.candidates, ...added] });
  return added.length;
}

/**
 * acceptCandidate(text) promotes one candidate into the entity list
 * (with its current category) and removes it from review. Returns
 * whether an entity was added.
 */
export function acceptCandidate(text) {
  const key = candidateKey(text);
  const cand = state.candidates.find((c) => candidateKey(c.text) === key);
  if (!cand) return false;
  const added = addEntities([{ category: cand.category, canonical: cand.text }]);
  setState({ candidates: state.candidates.filter((c) => candidateKey(c.text) !== key) });
  return added > 0;
}

/** rejectCandidate(text) drops a candidate without a trace. */
export function rejectCandidate(text) {
  const key = candidateKey(text);
  setState({ candidates: state.candidates.filter((c) => candidateKey(c.text) !== key) });
}

/**
 * updateCandidate(text, patch) edits a candidate in place (inline text
 * or category change before accepting). A text edit that collides with
 * another row is rejected (returns false).
 */
export function updateCandidate(text, patch) {
  const key = candidateKey(text);
  const next = (patch.text ?? text).trim();
  if (!next) return false;
  if (candidateKey(next) !== key &&
      state.candidates.some((c) => candidateKey(c.text) === candidateKey(next))) {
    return false;
  }
  setState({
    candidates: state.candidates.map((c) =>
      candidateKey(c.text) === key ? { ...c, ...patch, text: next } : c),
  });
  return true;
}

/**
 * bulkSelection(category, onlyTexts) is the shared row picker for the two
 * bulk actions (BUILD-04 CR15). Returns a predicate over candidates.
 *
 * onlyTexts, when given, restricts the action to those exact values. The
 * Values screen always passes the currently FILTERED set, so a bulk
 * button acts on exactly the rows the user can see and never on rows a
 * search box is hiding. Omitting it keeps the original whole-category
 * behaviour, which is what a caller with no filter wants.
 */
function bulkSelection(category, onlyTexts) {
  const allowed = onlyTexts ? new Set(onlyTexts.map(candidateKey)) : null;
  return (c) => c.category === category && (!allowed || allowed.has(candidateKey(c.text)));
}

/**
 * acceptAllInCategory(category, onlyTexts) bulk-accepts candidates of a
 * category, optionally restricted to onlyTexts; returns how many entities
 * were added.
 */
export function acceptAllInCategory(category, onlyTexts) {
  const inBatch = bulkSelection(category, onlyTexts);
  const batch = state.candidates.filter(inBatch);
  if (!batch.length) return 0;
  const added = addEntities(batch.map((c) => ({ category: c.category, canonical: c.text })));
  setState({ candidates: state.candidates.filter((c) => !inBatch(c)) });
  return added;
}

/**
 * denyAllInCategory(category, onlyTexts) is the mirror of
 * acceptAllInCategory (BUILD-04 CR15): it DROPS the candidates instead of
 * promoting them, exactly as rejectCandidate does one at a time. Nothing
 * is added to the entity list and nothing is remembered, so a denied
 * suggestion simply stops taking up review space.
 *
 * @param {string} category the engine category key
 * @param {string[]} [onlyTexts] restrict to these values (the filtered set)
 * @returns {number} how many candidates were removed
 */
export function denyAllInCategory(category, onlyTexts) {
  const inBatch = bulkSelection(category, onlyTexts);
  const removed = state.candidates.filter(inBatch).length;
  if (!removed) return 0;
  setState({ candidates: state.candidates.filter((c) => !inBatch(c)) });
  return removed;
}

// --- Variant regrouping (BUILD-02 Phase 9d) -----------------------------------

/**
 * moveVariant(fromCategory, fromCanonical, toCategory, toCanonical,
 * variant) moves one variant spelling between entities: the source
 * excludes it (so its automatic expansion stops matching it) and the
 * target gains it as a manual variant. Both rows re-expand (variants
 * back to pending). Pure reducer; the drag-and-drop wiring only calls
 * it. Returns false for self-drops, unknown rows, or a variant the
 * source does not actually carry.
 */
export function moveVariant(fromCategory, fromCanonical, toCategory, toCanonical, variant) {
  const v = (variant ?? "").trim();
  if (!v) return false;
  const fromKey = entityKey(fromCategory, fromCanonical);
  const toKey = entityKey(toCategory, toCanonical);
  if (fromKey === toKey) return false; // cannot drop onto self

  const from = state.entities.find((e) => entityKey(e.category, e.canonical) === fromKey);
  const to = state.entities.find((e) => entityKey(e.category, e.canonical) === toKey);
  if (!from || !to) return false;

  // The variant must actually belong to the source row (expanded list or
  // manual additions); otherwise this is a stale drop.
  const lower = v.toLowerCase();
  const carried =
    (from.variants ?? []).some((x) => x.toLowerCase() === lower) ||
    (from.manualVariants ?? []).some((x) => x.toLowerCase() === lower);
  if (!carried) return false;

  setState({
    entities: state.entities.map((e) => {
      const key = entityKey(e.category, e.canonical);
      if (key === fromKey) {
        return {
          ...e,
          manualVariants: (e.manualVariants ?? []).filter((x) => x.toLowerCase() !== lower),
          excludedVariants: [...(e.excludedVariants ?? []), v],
          variants: null, // re-expand ONLY the touched rows
          variantError: null,
        };
      }
      if (key === toKey) {
        const dup = (e.manualVariants ?? []).some((x) => x.toLowerCase() === lower);
        return {
          ...e,
          manualVariants: dup ? e.manualVariants : [...(e.manualVariants ?? []), v],
          // Un-exclude in case the variant is coming back home.
          excludedVariants: (e.excludedVariants ?? []).filter((x) => x.toLowerCase() !== lower),
          variants: null,
          variantError: null,
        };
      }
      return e;
    }),
  });
  return true;
}

// --- Reassignment helpers (BUILD-02 Phase 10d) --------------------------------

/**
 * entityAutocomplete(query, s) filters entity canonicals for the
 * reassignment popover: case-insensitive, prefix matches rank before
 * substring matches, each entry {category, canonical, label}.
 */
export function entityAutocomplete(query, s = state) {
  const q = (query ?? "").trim().toLowerCase();
  if (!q) return [];
  const prefix = [];
  const substring = [];
  for (const e of s.entities) {
    const lower = e.canonical.toLowerCase();
    const item = { category: e.category, canonical: e.canonical };
    if (lower.startsWith(q)) prefix.push(item);
    else if (lower.includes(q)) substring.push(item);
  }
  return [...prefix, ...substring];
}

/**
 * reassignOriginal(original, toCategory, toCanonical) makes `original`
 * a manual variant of the target entity. If `original` currently exists
 * as an entity of its own (it earned its own placeholder), that entity
 * is removed so exactly one entity matches the text after the fast
 * re-run. Returns false when the target does not exist.
 */
export function reassignOriginal(original, toCategory, toCanonical) {
  const text = (original ?? "").trim();
  if (!text) return false;
  const target = state.entities.find((e) =>
    entityKey(e.category, e.canonical) === entityKey(toCategory, toCanonical));
  if (!target) return false;
  if (entityKey(toCategory, toCanonical) === entityKey(toCategory, text)) return false;

  // Drop a same-named standalone entity (any category) before rerouting.
  const standalone = state.entities.find((e) => e.canonical.toLowerCase() === text.toLowerCase());
  if (standalone) removeEntity(standalone.category, standalone.canonical);

  addManualVariant(toCategory, toCanonical, text);
  return true;
}

// --- Same-format metadata review (BUILD-02 Phase 12c) --------------------------

/** setMetaReview(docName, review) stores (or replaces) one document's
 *  metadata review state; pass null to clear it. */
export function setMetaReview(docName, review) {
  const next = { ...state.metaReview };
  if (review === null) delete next[docName];
  else next[docName] = review;
  setState({ metaReview: next });
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

/**
 * clearAllowlist() empties the never-anonymise list in one step
 * (BUILD-04 CR11). The engine defaults are NOT re-seeded: they are
 * available again through the panel's import and the startup seeding, so
 * clearing stays a clear rather than a reset to something else.
 * @returns {number} how many terms were removed
 */
export function clearAllowlist() {
  const removed = state.allowlist.length;
  if (removed) setState({ allowlist: [] });
  return removed;
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

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

import {
  COUNTRIES, DEFAULT_COUNTRY, countryIDCategories, COUNTRY_ID_CATEGORIES,
} from "./countries.js";

// WIZARD_STEPS defines the fixed wizard order (CLAUDE.md wizard flow).
//
// BUILD-05 cut five steps to four: Configure stopped being a screen and
// became the left rail of Identify, which now owns both the choices
// (categories, confidence, local AI) and the values those choices produce.
// The tokens ARE the visible labels this time, capitalised: Import, Identify,
// Anonymise, Export.
//
// There is deliberately no migration table for the old tokens. Session files
// are read only by the version that wrote them (BUILD-05 decision 1), and an
// unknown persisted token falls back to "import", the one step that is always
// reachable. A table of every token this application ever used would grow
// forever to serve files the session loader now refuses anyway.
//
// The four tokens are cross-checked against main.js STEP_LABELS and VIEWS,
// STEP_RESETS below, and copy.js NAV.stepNames by ../step_parity_test.go.
export const WIZARD_STEPS = ["import", "identify", "anonymise", "export"];

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

  // Source text fetched from Go for documents the import list no longer holds
  // (see documentSource() below): {name: {markdown, truncated, isGrid}}. It is
  // a cache, never a second source of truth: `documents` always wins.
  sourceCache: {},

  // Name of the document selected in the preview pane (or null).
  previewDoc: null,

  // The DOCUMENT COUNTRY (BUILD-05 Phase 5, decision 2), an entry code from
  // countries.js. It is frontend-only and does exactly two things: it swaps the
  // example strings beside the phone / VAT / national identification labels,
  // and it switches the three country-specific ID categories. There is no
  // locale-aware engine behind it and engine/pii.go is untouched.
  documentCountry: DEFAULT_COUNTRY,

  // Settings mirror (source of truth lives in Go; this copy renders the
  // Configure screen): {level, categories, ollamaPort, model}. level is
  // the LAST CHOSEN PRESET; categories is the granular switch set the
  // pipeline obeys (BUILD-02 Phase 3). The categories map is filled below
  // after presetCategories is defined.
  // The three DETECTION ROUTES (BUILD-06), each with its own switch, because
  // they are three separate ways of finding values and the user turns them on
  // and off independently:
  //   useSmartDetect  the offline heuristic pass. ON by default, and
  //                   deactivable. Its scope (the categories, the preset, the
  //                   confidence floor) and its tuning are the settings it
  //                   reads, which is why the rail nests them inside it.
  //   useAI           the local model (Ollama). OFF by default. Detecting
  //                   Ollama ENABLES the switch, it never flips it: turning on
  //                   a route that sends the document to a model, however
  //                   local, is the user's decision to make.
  //   useCloudAI      not built (BUILD-05 decision 8). Always false, kept here
  //                   so the rail has something honest to render.
  // contextSize is the Ollama num_ctx setting (Phase 5b), default 8192.
  // minConfidence is the detection-confidence floor (BUILD-04 CR9), 0 to
  // 1 on the engine's scale. 0 is the default and keeps every detection,
  // which is exactly the behaviour before the setting existed.
  settings: {
    level: "medium", categories: null, ollamaPort: 11434, model: "", country: DEFAULT_COUNTRY,
    contextSize: 8192, useAI: false, useSmartDetect: true, useCloudAI: false,
    minConfidence: 0,
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

  // Values added on the Anonymise screen that a fast re-run has not applied yet
  // (BUILD-05 Phase 7): [{category, text}]. They are ALSO added to `entities`
  // straight away, so a re-run picks them up; this list exists only so the user
  // can see what is waiting and remove one before re-running. Cleared by the
  // re-run that applies them.
  pendingValues: [],

  // Warnings from the last run that the user has dismissed, by id
  // (BUILD-05 Phase 7). A warning is advice, not an error: once it has been
  // read it should stop taking up room in the report, but it must come back
  // if a new run raises it again, which is why the reset for the Anonymise
  // step empties this list.
  dismissedWarnings: [],

  // Detection run state (BUILD-06), or null when idle:
  // {running, phase, phaseIndex, phaseCount, current, total, file,
  //  chunk, chunkCount, fraction, startedAt}.
  //
  // `fraction` comes from GO and is guaranteed non-decreasing across the whole
  // run. The frontend used to compute a percentage per pass, which is why the
  // bar rewound when the second pass started over with a smaller denominator.
  // It is never recomputed here.
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

  // The notice strip (BUILD-05 Phase 1): {text, tone} or null. ONE notice at
  // a time, deliberately: a stack of them turns into a log nobody reads, and
  // the newest statement is always the one that matters. tone is
  // "ok" | "info" | "warn" (brand.css notice tint pairs).
  notice: null,

  // The in-app confirm's pending question (BUILD-05 decision 10), or null
  // when nothing is being asked: {title, body, confirmLabel, cancelLabel,
  // keyBearing}. The PROMISE that resolves it is module-private below, not
  // stored here, because state must stay clonable (resetState uses
  // structuredClone, which cannot copy a function).
  confirm: null,

  // The destination folder for the batch zip (BUILD-05 Phase 3, decision 4), or
  // "" when none has been chosen. It is FRONTEND state, not a Go setting: it is
  // a convenience for one batch and has no business sitting in a session file
  // next to the re-identification key. It drives the zip and nothing else;
  // every other export keeps its own save dialog.
  exportDir: "",

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
// COUNTRIES_BY_CODE is the membership check setDocumentCountry needs: a Set
// rather than a repeated find(), and built once at module load.
const COUNTRIES_BY_CODE = new Set(COUNTRIES.map((c) => c.code));
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
// entity_names (BUILD-06) is the merge of the former client_names and
// internal_names. Mirrors engine.AllEntityCategories; the pairing is enforced
// by ../category_parity_test.go.
export const NAME_CATEGORIES = ["entity_names", "project_names", "person_names"];
export const ADVANCED_ENTITY_CATEGORIES = ["organisation_names", "location_names"];
export const ALL_CATEGORIES = [
  ...HARD_PII_CATEGORIES, ...EXTENDED_PII_CATEGORIES, ...ADVANCED_PII_CATEGORIES,
  ...NAME_CATEGORIES, "custom_patterns", ...ADVANCED_ENTITY_CATEGORIES,
];

/**
 * presetCategories(level) reproduces engine.PresetSelection exactly:
 * soft = hard PII + entity/project names + custom patterns;
 * medium = soft + persons; advanced = everything.
 */
export function presetCategories(level) {
  const sel = {};
  for (const c of ALL_CATEGORIES) sel[c] = false;
  for (const c of HARD_PII_CATEGORIES) sel[c] = true;
  // Extended recognizers are hard PII at every level, matching the Go
  // PresetSelection exactly (BUILD-04 CR9).
  for (const c of EXTENDED_PII_CATEGORIES) sel[c] = true;
  sel.entity_names = sel.project_names = true;
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

/** resetState() restores the initial state (used by tests and session load).
 *
 *  A question that was on screen is answered "no" first: dropping the state
 *  without settling the promise would leave whoever awaited askConfirm()
 *  hanging forever. */
export function resetState() {
  if (confirmResolve) {
    const stale = confirmResolve;
    confirmResolve = null;
    stale(false);
  }
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

// --- Notices and the in-app confirm (BUILD-05 Phase 1) ----------------------

// NOTICE_TONES are the only tones there are. An unknown tone is stored as
// "info" rather than rejected: a notice is the application telling the user
// something, so a typo in the tone must not swallow the sentence.
export const NOTICE_TONES = ["ok", "info", "warn"];

/**
 * setNotice(text, tone) shows one statement in the notice strip, replacing
 * whatever was there. Empty text CLEARS the strip, so a caller that computes
 * its message does not need a second branch.
 * @param {string} text the sentence to show
 * @param {string} [tone] "ok" | "info" | "warn" (default "info")
 * @returns {object|null} the stored notice, or null when cleared
 */
export function setNotice(text, tone = "info") {
  const message = (text ?? "").trim();
  if (!message) {
    clearNotice();
    return null;
  }
  const notice = { text: message, tone: NOTICE_TONES.includes(tone) ? tone : "info" };
  setState({ notice });
  return notice;
}

/** clearNotice() dismisses the notice strip. */
export function clearNotice() {
  if (state.notice) setState({ notice: null });
}

// confirmResolve holds the resolve function of the promise askConfirm handed
// out, for exactly as long as the question is on screen. It lives here rather
// than in the state object because state has to stay structuredClone-able
// (resetState), and because nothing outside this module should be able to
// answer a question the user has not answered.
let confirmResolve = null;

/**
 * askConfirm(question) shows the in-app confirm and resolves to the user's
 * answer (BUILD-05 decision 10, replacing every native confirm()).
 *
 * Asking while a question is already open ANSWERS THE OLD ONE "no" and then
 * asks the new one. That is the safe direction: the alternative is either
 * losing the new question silently or leaving the first promise pending
 * forever, and a promise that never settles is a caller stuck mid-action.
 *
 * @param {object} question
 * @param {string} question.title the short heading
 * @param {string} question.body what will happen, in full sentences
 * @param {string} [question.confirmLabel] the affirmative button (default
 *   "Continue")
 * @param {string} [question.cancelLabel] default "Cancel"
 * @param {boolean} [question.keyBearing] the action exposes the
 *   re-identification key; tints the dialog accordingly
 * @returns {Promise<boolean>} true when confirmed, false when cancelled
 */
export function askConfirm(question) {
  if (confirmResolve) {
    const stale = confirmResolve;
    confirmResolve = null;
    stale(false);
  }
  return new Promise((resolve) => {
    confirmResolve = resolve;
    setState({
      confirm: {
        title: question?.title ?? "Confirm",
        body: question?.body ?? "",
        confirmLabel: question?.confirmLabel ?? "Continue",
        cancelLabel: question?.cancelLabel ?? "Cancel",
        keyBearing: !!question?.keyBearing,
      },
    });
  });
}

/**
 * answerConfirm(answer) closes the open question and settles its promise.
 * modal.js calls it from the two buttons and from the Escape key (which
 * answers false, the same as Cancel).
 * @param {boolean} answer
 * @returns {boolean} whether there was a question to answer
 */
export function answerConfirm(answer) {
  const resolve = confirmResolve;
  confirmResolve = null;
  if (state.confirm) setState({ confirm: null });
  if (resolve) resolve(!!answer);
  return !!resolve;
}

// --- Category selection reducers (BUILD-02 Phase 3) --------------------------

/**
 * applyPreset(level) sets the level AND fills the category switches from
 * the preset (the "Soft/Standard/Thorough" chips of the configure screen).
 */
export function applyPreset(level) {
  setState({
    settings: {
      ...state.settings,
      level,
      // The preset, then the CURRENT COUNTRY's identifier switches on top of it
      // (BUILD-05 Phase 5). Every preset switches all three country-specific
      // identifiers on, because to the engine they are hard PII; re-applying the
      // country here is what stops picking a preset silently re-enabling the
      // German tax identifier on a French document. The country is an
      // orthogonal choice, so a preset must not overrule it.
      categories: {
        ...presetCategories(level),
        ...countryIDCategories(state.documentCountry),
        matricule: state.documentCountry === "LU",
      },
    },
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
 * setDocumentCountry(code) records the document country and applies its
 * consequence: the three country-specific ID categories are switched to match
 * (BUILD-05 Phase 5, decision 2).
 *
 * Both halves happen in ONE state change, so the country selector repaints once
 * rather than three times, and so the country and the switches can never be
 * observed disagreeing.
 *
 * The switch set is applied WHOLE, not additively: picking France turns the
 * German and Spanish identifiers OFF rather than leaving them on beside
 * nothing. That is the behaviour the mock-up's script has, and it is the only
 * one that makes the control mean anything.
 *
 * An unknown code is REJECTED rather than stored, because the country drives
 * which categories are on: silently accepting a code the table does not know
 * would leave the selector showing one country and the switches set for
 * another.
 *
 * @param {string} code a country code from countries.js
 * @returns {string|null} the stored code, or null when rejected
 */
export function setDocumentCountry(code) {
  const applies = countryIDCategories(code);
  // countryIDCategories falls back to the default for an unknown code, so the
  // membership check has to happen here rather than being inferred from it.
  if (!COUNTRIES_BY_CODE.has(code)) return null;

  const categories = { ...state.settings.categories };
  for (const key of COUNTRY_ID_CATEGORIES) {
    // Guard against a table that drifts ahead of ALL_CATEGORIES: a switch for a
    // category the engine does not know does nothing, so do not invent one.
    if (!ALL_CATEGORIES.includes(key)) continue;
    categories[key] = applies[key];
  }
  setState({
    documentCountry: code,
    settings: { ...state.settings, country: code, categories: { ...categories, matricule: code === "LU" } },
  });
  return code;
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
  const countrySpecific = countryIDCategories(state.documentCountry);
  for (const level of ["soft", "medium", "advanced"]) {
    const preset = presetCategories(level);
    const matches = ALL_CATEGORIES.every((c) => {
      const expected = COUNTRY_ID_CATEGORIES.includes(c)
      	? (preset[c] && !!countrySpecific[c])
      	: preset[c];
      return !!categories?.[c] === expected;
    });
    if (matches) return level;
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
 * setUseSmartDetect(on) turns the offline heuristic route on or off.
 *
 * It is ON by default: it needs nothing installed, it is the route that works
 * on every machine, and a user who has just imported documents expects the
 * app to look at them. It is switchable because its suggestions are guesses,
 * and someone who only wants the deterministic PII pass plus their own listed
 * values should be able to say so.
 */
export function setUseSmartDetect(on) {
  setState({ settings: { ...state.settings, useSmartDetect: !!on } });
}

/**
 * detectionRoutesOn(s) is how many ways of FINDING values are enabled. Zero
 * means the detect button has nothing to run, which the UI says rather than
 * running an empty pass and reporting "0 suggestions" as if it had looked.
 */
export function detectionRoutesOn(s = state) {
  return (s.settings.useSmartDetect ? 1 : 0) + (llmEnabled(s) ? 1 : 0);
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

// setImportSplit() is GONE (BUILD-05 decision 6). It stored the import screen's
// draggable pane divider position. The mock-up uses a fixed two-column grid, and
// a divider that only ever moved between 20% and 80% of the width was three
// state transitions and a pointer-capture dance to save the user a decision they
// did not actually have.

// --- Navigation guards ------------------------------------------------------

/**
 * canGoTo(step, s) implements the wizard guards. There are three, and they all
 * live here rather than in the five places that navigate, so the step bar and
 * all four screen footers give the same answer (nav.js, frontend/CLAUDE.md).
 *
 *   1. no step beyond Import without documents;
 *   2. no Anonymise while suggestions are still waiting to be reviewed
 *      (BUILD-06 Phase 7);
 *   3. no Export before a run produced results.
 *
 * Rule 2 is the review gate. Detection produces SUGGESTIONS, not decisions, and
 * an unreviewed suggestion is neither accepted nor rejected: walking past them
 * into the run silently answers "reject" for the user on values the detector
 * thought were worth asking about. The gate is never a dead end, because
 * rejecting is one action away (rejectAllShown, and views/identify.js readyHint
 * says so in the footer).
 *
 * Rule 2 is deliberately NOT applied to Export. Reaching Export at all requires
 * results, which means a run already happened with the review done; refusing to
 * show the user the output of a finished run because a later detection pass
 * added suggestions would strand them on a screen with nothing to do.
 *
 * @param {string} step target step name
 * @param {object} [s] state (defaults to the live state; injectable for tests)
 * @returns {boolean}
 */
export function canGoTo(step, s = state) {
  if (!WIZARD_STEPS.includes(step)) return false;
  const idx = WIZARD_STEPS.indexOf(step);
  if (idx > 0 && s.documents.length === 0) return false;      // nothing imported yet
  if (step === "anonymise" && s.candidates.length > 0) return false; // still unreviewed
  if (step === "export" && !s.results) return false;          // nothing to export yet
  return true;
}

/** goTo(step) navigates when the guard allows it; returns whether it did. */
export function goTo(step) {
  if (!canGoTo(step)) return false;
  setState({ step });
  return true;
}

/**
 * knownStep(step) returns a token guaranteed to be in WIZARD_STEPS, falling
 * back to "import" for anything it does not recognise.
 *
 * This replaced BUILD-04's LEGACY_STEP_TOKENS + migrateStep() pair, which
 * translated retired tokens onto current ones. BUILD-05 decision 1 removed
 * that machinery rather than extending it: a session file whose schema version
 * this build does not know is refused outright by the loader, so a step token
 * from an older file never reaches here in the first place. What is left is
 * the honest case, a corrupted or hand-edited value, and the answer to that is
 * the step that is always reachable rather than a guess.
 *
 * @param {string} step a token from a persisted session or a URL
 * @returns {string} a token guaranteed to be in WIZARD_STEPS
 */
export function knownStep(step) {
  return WIZARD_STEPS.includes(step) ? step : "import";
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
  // Identify owns BOTH halves of its screen (BUILD-05 Phase 2): the rail's
  // choices, which used to belong to a Configure step of its own, and the
  // workspace's values, suggestions and patterns. They are reset together
  // because that is now one screen: resetting half of it would leave a
  // category selection that no longer matches the values it produced.
  identify: () => ({
    documentCountry: DEFAULT_COUNTRY,
    settings: {
      ...state.settings,
      // The preset first, then the DEFAULT COUNTRY's identifier switches on top
      // of it. Order matters: every preset switches all three country-specific
      // identifiers on (they are hard PII to the engine), so applying the
      // preset alone would leave the rail showing Luxembourg with the German
      // and Spanish tax identifiers active, which is precisely the disagreement
      // setDocumentCountry exists to prevent.
      categories: {
        ...presetCategories(state.settings.level),
        ...countryIDCategories(DEFAULT_COUNTRY),
        matricule: true,
      },
      country: DEFAULT_COUNTRY,
      minConfidence: 0,
      smartDetect: { ...SMART_DETECT_DEFAULTS },
    },
    entities: [],
    candidates: [],
    patterns: [],
    discovery: null,
  }),
  // Anonymise owns the run itself, everything it produced, and the two
  // editing surfaces that only exist once there is a result to edit: the
  // ordered find-and-replace rules and the dismissed-warning list.
  anonymise: () => ({
    running: false,
    progress: null,
    results: null,
    mapping: null,
    simpleRules: [],
    dismissedWarnings: [],
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

/**
 * nextStep() moves one step forward. Forward moves never ask anything, so this
 * is the whole of the operation.
 *
 * There is no prevStep() any more (BUILD-05 Phase 9). It moved one step BACK
 * without the reset question, which made it a way around the rule nav.js exists
 * to enforce: an accidental caller would silently skip the confirmation the user
 * is owed. Backward movement goes through nav.js goBack().
 */
export function nextStep() {
  const idx = WIZARD_STEPS.indexOf(state.step);
  return idx < WIZARD_STEPS.length - 1 && goTo(WIZARD_STEPS[idx + 1]);
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
    // A fresh import list makes every cached source stale (a re-imported file
    // with the same name is a DIFFERENT file). Dropping the cache is cheaper
    // and safer than deciding which entries survived.
    sourceCache: {},
  });
}

/**
 * documentSource(s, name) is THE way any view asks for a document's source
 * text, and the reason items 1 and 4 cannot come back: nothing else in the
 * frontend is allowed to call something "the original".
 *
 * It reads the import list first (already in the store, already truncated by
 * Go), then the cache filled by cacheDocumentSource() for documents the list
 * no longer holds.
 *
 * @param {object} s state
 * @param {string} name document name
 * @returns {{markdown: string, truncated: boolean, isGrid: boolean, found: boolean}|null}
 *          null means "not known yet", which is a caller's cue to fetch;
 *          `found: false` means "asked, and the document is gone".
 */
export function documentSource(s, name) {
  if (!name) return null;
  const imported = (s.documents ?? []).find((d) => d.name === name);
  if (imported) {
    return {
      markdown: imported.markdown ?? "",
      truncated: !!imported.previewTruncated,
      isGrid: !!imported.isGrid,
      found: true,
    };
  }
  return s.sourceCache?.[name] ?? null;
}

/**
 * cacheDocumentSource(name, source) stores a source fetched from Go.
 * A `found: false` answer is cached as an empty source, so a missing document
 * is asked about once rather than on every repaint.
 */
export function cacheDocumentSource(name, source) {
  if (!name) return;
  setState({
    sourceCache: {
      ...state.sourceCache,
      [name]: {
        markdown: source?.markdown ?? "",
        truncated: !!source?.truncated,
        isGrid: !!source?.isGrid,
        found: !!source?.found,
      },
    },
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

// setEntityStatus() and editEntity() are GONE (BUILD-05 Phase 9).
//
// setEntityStatus flipped a row between "accepted" and "denied". The redesign
// has no denied state: a suggestion is accepted or rejected in the Suggestions
// tab, and a value that reaches My values is a value that will be replaced. The
// `status` field and the filter in acceptedEntities() below survive as the belt
// to that brace, because a row that somehow arrived denied must still not reach
// the pipeline.
//
// editEntity renamed a value in place, an action the review TABLE offered.
// The value cards that replaced it do not: a value is keyed by (category,
// canonical), so a rename is a remove and an add, which is what the card's two
// controls already do and what the registry needs anyway to keep its
// placeholder mapping honest.

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

// updateCandidate() is GONE (BUILD-05 Phase 9). It edited a suggestion's text or
// category in place, before accepting it. The suggestions table is now a review
// gate with exactly two answers per row, accept or reject: editing a suggestion
// meant deciding what it should have been, which is what My values is for, and
// having it in both places meant two ways to create the same value.

// acceptAllInCategory() and denyAllInCategory() are GONE (BUILD-05 Phase 9),
// along with the bulkSelection() picker they shared.
//
// They bulk-acted on ONE category at a time, because the BUILD-04 suggestions
// table grouped its bulk buttons per category. The BUILD-05 table sorts and
// filters across every category at once, so "the rows shown" is not a category,
// and acceptAllShown() / rejectAllShown() below take the visible values directly.
// Keeping the per-category pair as well would have meant two bulk paths with
// different notions of what a bulk action covers.

/**
 * acceptAllShown(texts) bulk-accepts the candidates whose values are listed,
 * ACROSS categories (BUILD-05 Phase 6).
 *
 * This is the mock-up's "Accept all shown". It differs from
 * acceptAllInCategory in exactly one way that matters: the suggestions table
 * sorts and filters across every category at once, so "shown" is not a
 * category, it is whatever survived the search, the type filter and the source
 * filter. A per-category button cannot express that, and asking the user to
 * press it once per category would defeat the point of a bulk action.
 *
 * Each accepted candidate keeps its OWN category rather than being coerced into
 * a shared one: the bulk action is about which rows, not about what they are.
 *
 * @param {string[]} texts the values currently visible, in any order
 * @returns {number} how many entities were added
 */
export function acceptAllShown(texts) {
  const shown = new Set((texts ?? []).map(candidateKey));
  if (shown.size === 0) return 0;
  const batch = state.candidates.filter((c) => shown.has(candidateKey(c.text)));
  if (!batch.length) return 0;
  const added = addEntities(batch.map((c) => ({ category: c.category, canonical: c.text })));
  setState({ candidates: state.candidates.filter((c) => !shown.has(candidateKey(c.text))) });
  return added;
}

/**
 * rejectAllShown(texts) is the mirror of acceptAllShown: it DROPS those
 * candidates instead of promoting them, exactly as rejectCandidate does one at
 * a time. Nothing is added to the value list and nothing is remembered, so a
 * rejected suggestion simply stops taking up review space.
 *
 * @param {string[]} texts the values currently visible
 * @returns {number} how many candidates were removed
 */
export function rejectAllShown(texts) {
  const shown = new Set((texts ?? []).map(candidateKey));
  if (shown.size === 0) return 0;
  const removed = state.candidates.filter((c) => shown.has(candidateKey(c.text))).length;
  if (!removed) return 0;
  setState({ candidates: state.candidates.filter((c) => !shown.has(candidateKey(c.text))) });
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

// --- The Anonymise screen's editing surfaces (BUILD-05 Phase 7) ---------------

/**
 * addPendingValue(category, text) records a value the user added on the
 * Anonymise screen and adds it to the value list in the same breath.
 *
 * The two go together on purpose. The entity is what makes a fast re-run replace
 * the value; the pending chip is what tells the user it will not happen until
 * they press it. Recording only one of the two would either lose the value or
 * lie about when it takes effect.
 *
 * A value that is already pending, or already an accepted value, is rejected:
 * the chip would be a duplicate and the entity would be too, so the honest
 * answer is "nothing happened" rather than a second chip that does nothing.
 *
 * @param {string} category the engine category identifier
 * @param {string} text the missed value
 * @returns {boolean} whether a chip was added
 */
export function addPendingValue(category, text) {
  const value = (text ?? "").trim();
  if (!value) return false;
  const lower = value.toLowerCase();
  if (state.pendingValues.some((p) => p.text.toLowerCase() === lower)) return false;
  if (state.entities.some((e) => e.canonical.toLowerCase() === lower)) return false;

  addEntities([{ category, canonical: value }]);
  setState({ pendingValues: [...state.pendingValues, { category, text: value }] });
  return true;
}

/**
 * removePendingValue(text) takes a value back off the pending list AND out of
 * the value list, because the chip's ✕ has to undo the whole of what Add did.
 * @param {string} text the value to drop
 */
export function removePendingValue(text) {
  const lower = (text ?? "").trim().toLowerCase();
  const pending = state.pendingValues.find((p) => p.text.toLowerCase() === lower);
  if (!pending) return;
  setState({ pendingValues: state.pendingValues.filter((p) => p !== pending) });
  removeEntity(pending.category, pending.text);
}

/**
 * clearPendingValues() empties the list, called by the re-run that applies them.
 * The ENTITIES stay: they are what the re-run just used.
 * @returns {number} how many chips were cleared
 */
export function clearPendingValues() {
  const cleared = state.pendingValues.length;
  if (cleared) setState({ pendingValues: [] });
  return cleared;
}

/**
 * dismissWarning(id) hides one run warning (BUILD-05 Phase 7).
 *
 * The id is the warning's own TEXT. Go sends warnings as plain strings with no
 * identifier, and inventing an index would break the moment a run produced them
 * in a different order: the same warning would come back undismissed, or a
 * different one would be hidden. The text is stable for as long as the warning
 * means the same thing, which is exactly the right lifetime.
 *
 * A new run re-raises everything, because resetStep("anonymise") empties this
 * list: a warning about the run you are looking at now has to be visible.
 *
 * @param {string} id the warning text
 * @returns {boolean} whether it was newly dismissed
 */
export function dismissWarning(id) {
  const text = (id ?? "").trim();
  if (!text || state.dismissedWarnings.includes(text)) return false;
  setState({ dismissedWarnings: [...state.dismissedWarnings, text] });
  return true;
}

/**
 * visibleWarnings(s) is the run's warnings minus the dismissed ones. Pure, so
 * the view renders it without deciding anything.
 * @param {object} [s] state
 * @returns {string[]}
 */
export function visibleWarnings(s = state) {
  const dismissed = new Set(s.dismissedWarnings ?? []);
  return (s.results?.report?.warnings ?? []).filter((w) => !dismissed.has(w));
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

/**
 * setExportDir(dir) remembers the folder the batch zip will be written to.
 * @param {string} dir an absolute path, or "" to forget it
 * @returns {string} the stored value
 */
export function setExportDir(dir) {
  const value = (dir ?? "").trim();
  setState({ exportDir: value });
  return value;
}

/**
 * startNewBatch() clears THIS batch and keeps the setup (BUILD-05 Phase 8).
 *
 * The split is the whole point, so it is spelled out rather than left to a
 * reader of the object literal:
 *
 *   CLEARED   the documents, the run and everything it produced (results, the
 *             mapping, the pending values, the dismissed warnings), the values
 *             and suggestions, the custom patterns, the find-and-replace rules,
 *             and the per-document metadata review decisions. All of it is about
 *             the batch that just finished.
 *   KEPT      the settings (categories, confidence, smart-detection tuning, the
 *             Ollama connection), the document country, and the never-anonymise
 *             list. All of it is about HOW this user works, and re-entering it
 *             for every batch is exactly the tedium the button exists to avoid.
 *
 * The MAPPING is cleared, and that matters for more than tidiness: it is the
 * re-identification key of the previous batch, and leaving it in memory across
 * an explicit "start again" would keep sensitive data alive with nothing on
 * screen referring to it.
 *
 * The Go-side registry is deliberately NOT reset here. A follow-up batch reusing
 * the same placeholders for the same real-world values is the documented
 * behaviour of a session (CLAUDE.md §5), and this button starts a new batch
 * within a session rather than a new session.
 *
 * The wizard returns to Import, because that is the only step a cleared batch can
 * stand on.
 *
 * @returns {object} what was cleared, for the confirming caller's notice
 */
export function startNewBatch() {
  const cleared = {
    documents: state.documents.length,
    values: state.entities.length,
    rules: state.simpleRules.length,
  };
  setState({
    step: "import",
    documents: [],
    previewDoc: null,
    importErrors: [],
    sourceCache: {},
    entities: [],
    candidates: [],
    patterns: [],
    simpleRules: [],
    discovery: null,
    running: false,
    progress: null,
    results: null,
    mapping: null,
    resultDoc: null,
    pendingValues: [],
    dismissedWarnings: [],
    metaReview: {},
    exportDir: state.exportDir,
    notice: null,
  });
  return cleared;
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

// clearAllowlist() is GONE (BUILD-05 Phase 9). It emptied the never-anonymise
// list in one action, from a button in the panel header that the chip layout no
// longer has, and it was the last thing on that screen behind a native confirm()
// (decision 10). Removing terms one chip at a time is the ordinary case, and a
// list of a dozen seeded terms is not one anyone needs to empty in a single
// press. The engine defaults are still seeded at startup and still removable.

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

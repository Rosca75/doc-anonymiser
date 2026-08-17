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
// stay logic-free.

import {
  COUNTRIES, DEFAULT_COUNTRY, countryIDCategories, COUNTRY_ID_CATEGORIES,
} from "./countries.js";

// WIZARD_STEPS defines the fixed wizard order (CLAUDE.md wizard flow).
//
//  cut five steps to four: Configure stopped being a screen and
// became the left rail of Identify, which now owns both the choices
// (categories, confidence, local AI) and the values those choices produce.
// The tokens ARE the visible labels this time, capitalised: Import, Identify,
// Anonymise, Export.
//
// There is deliberately no migration table for the old tokens. Session files
// are read only by the version that wrote them, and an
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
  // values survive navigation. Documentation is not a
  // screen: it opens in its own window.
  screen: "home",

  // Shell-level error message, or null. Used for failures that belong to
  // the application chrome rather than to any one view, such as the
  // documentation window refusing to open. Rendered as a dismissible
  // banner above the active view.
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

  // The DOCUMENT COUNTRY, an entry code from
  // countries.js. It is frontend-only and does exactly two things: it swaps the
  // example strings beside the phone / VAT / national identification labels,
  // and it switches the three country-specific ID categories. There is no
  // locale-aware engine behind it and engine/pii.go is untouched.
  documentCountry: DEFAULT_COUNTRY,

  // Settings mirror (source of truth lives in Go; this copy renders the
  // Configure screen): {level, categories, ollamaPort, model}. level is
  // the LAST CHOSEN PRESET; categories is the granular switch set the
  // pipeline obeys. The categories map is filled below
  // after presetCategories is defined.
  // The three DETECTION ROUTES, each with its own switch, because
  // they are three separate ways of finding values and the user turns them on
  // and off independently:
  //   useSmartDetect the offline heuristic pass. ON by default, and
  //                   deactivable. Its scope (the categories, the preset, the
  //                   confidence floor) and its tuning are the settings it
  //                   reads, which is why the rail nests them inside it.
  //   useAI the local model (Ollama). OFF by default. Detecting
  //                   Ollama ENABLES the switch, it never flips it: turning on
  //                   a route that sends the document to a model, however
  //                   local, is the user's decision to make.
  //   (there is no useCloudAI. The cloud route is not built,
  //   and the rail renders a static "not built yet" panel rather
  //   than reading a flag. The field existed anyway, was pushed to Go on every
  //   settings change, and Go discarded it: a setting nothing reads and nothing
  //   can change is a contract the next reader has to disprove. BRIDGE.md said
  //   it did not exist, and BRIDGE.md was right.)
  // contextSize is the Ollama num_ctx setting, default 8192.
  // minConfidence is the detection-confidence floor, 0 to
  // 1 on the engine's scale. 0 is the default and keeps every detection,
  // which is exactly the behaviour before the setting existed.
  settings: {
    level: "medium", categories: null, ollamaPort: 11434, model: "", country: DEFAULT_COUNTRY,
    contextSize: 8192, useAI: false, useSmartDetect: true,
    minConfidence: 0,
    // smartDetect is the tuning for the offline Smart
    // detection pass, matching engine.SmartDetectOptions field for field.
    // The defaults are the STRICTER ones (engine
    // DefaultSmartDetectOptions), because over-detection was the reported
    // problem; a user who wants everything back sets them to 0/false.
    smartDetect: {
      minLength: 4,
      minOccurrences: 1,
      excludeCommonWords: true,
      minConfidence: 0.5,
      strictness: "balanced",
    },
  },

  // Local-AI SCAN SCOPE (CLAUDE.md §5): which ONE document, and which range of
  // its own units (pages/slides/rows/lines), the local model reads. Handing a
  // whole document to a small local model is "too much", so the user can aim
  // the scan. docName "" means "every document, whole", the unchanged
  // behaviour. It is a transient per-run choice, deliberately NOT part of
  // settings, so it never travels in a session file and never reaches Go
  // through applySettings.
  aiScope: { docName: "", fromPage: 1, toPage: 1 },

  // Entity review state: array of
  // {category, canonical, manualVariants, status: "accepted"|"denied"}.
  entities: [],

  // Allowlist terms (display spellings).
  allowlist: [],

  // Custom regex patterns: array of {expr, error} (error = compile
  // message or null).
  patterns: [],

  // Ordered simple-replace rules: {find, replace, caseSensitive}.
  simpleRules: [],

  // Pipeline execution state.
  running: false,
  progress: null, // {stage, docIndex, docCount, docName} or null
  results: null,  // engine.Results mirror or null

  // The document the Compare pane shows on step 3, by name, or null for the
  // first one. It is a view choice, but it lives here because the reset table
  // is what clears it: introduced ad hoc by the view, it survived resetStep and
  // pointed the pane at a document from a previous run.
  resultDoc: null,

  // The step 3 Replaced values table, both halves, mirrored from the Go
  // registry rather than derived from the report text: the registry is what the
  // renaming and the removals act on, so a row built from anything else could
  // offer an edit with no entry behind it.
  //   replacedValues [{original, placeholder, category, count}]
  //   removedValues  [{original, category, placeholder, variants}]
  replacedValues: [],
  removedValues: [],

  // Warnings from the last run that the user has dismissed, by id
  // A warning is advice, not an error: once it has been
  // read it should stop taking up room in the report, but it must come back
  // if a new run raises it again, which is why the reset for the Anonymise
  // step empties this list.
  dismissedWarnings: [],

  // Detection run state, or null when idle:
  // {running, phase, phaseIndex, phaseCount, current, total, file,
  //  chunk, chunkCount, fraction, startedAt}.
  //
  // `fraction` comes from GO and is guaranteed non-decreasing across the whole
  // run. The frontend used to compute a percentage per pass, which is why the
  // bar rewound when the second pass started over with a smaller denominator.
  // It is never recomputed here.
  discovery: null,

  // Unified candidate review list: candidates from
  // any discovery method wait HERE until explicitly accepted; nothing
  // flows into entities without user confirmation. Each row:
  // {source: "smart"|"local-ai"|"cloud-ai", text, category, count, contexts}.
  candidates: [],

  // Placeholder → {original, category} lookup from the last run
  // MEMORY NOTE: this is the re-identification
  // key; it stays in the app process like the Go registry and is
  // cleared with the results.
  mapping: null,

  // The notice strip: {text, tone} or null. ONE notice at
  // a time, deliberately: a stack of them turns into a log nobody reads, and
  // the newest statement is always the one that matters. tone is
  // "ok" | "info" | "warn" (brand.css notice tint pairs).
  notice: null,

  // The in-app confirm's pending question, or null
  // when nothing is being asked: {title, body, confirmLabel, cancelLabel,
  // keyBearing}. A question may instead carry `choices: [{id, label}]`, which
  // turns it into a pick-one dialog (askChoice) rather than yes/no; the two
  // buttons are then replaced by one button per choice. The PROMISE that
  // resolves it is module-private below, not stored here, because state must
  // stay clonable (resetState uses structuredClone, which cannot copy a
  // function).
  confirm: null,

  // The destination folder for the batch zip, or
  // "" when none has been chosen. It is FRONTEND state, not a Go setting: it is
  // a convenience for one batch and has no business sitting in a session file
  // next to the re-identification key. It drives the zip and nothing else;
  // every other export keeps its own save dialog.
  exportDir: "",

  // Same-format metadata review state per document:
  // {docName: {ext, filename, fields: [{part, name, value, proposed,
  // changed, finalValue}]}}. Decisions persist for the session so the
  // review only happens before the FIRST same-format export of a file.
  metaReview: {},
};

// --- Category presets ----

// Category keys, split by preset tier. Must stay in sync with the Go side
// (engine/pipeline.go AllPIICategories / AllEntityCategories).
export const HARD_PII_CATEGORIES = ["email", "url", "iban", "vat", "matricule", "phone"];
// COUNTRIES_BY_CODE is the membership check setDocumentCountry needs: a Set
// rather than a repeated find(), and built once at module load.
const COUNTRIES_BY_CODE = new Set(COUNTRIES.map((c) => c.code));
// The extended recognizers added to the engine. They are HARD
// PII exactly like the group above, so every preset switches them on
// (engine/pipeline.go PresetSelection).  finally surfaces
// them in the Configure screen; until then they were detectable but
// invisible, which is why the list is separate rather than merged: the
// Configure screen shows them as their own group.
export const EXTENDED_PII_CATEGORIES = [
  "credit_card", "uk_nhs", "ip_address", "mac_address",
  "crypto", "database_uri", "de_steuer_id", "es_nif",
];
export const ADVANCED_PII_CATEGORIES = ["amount", "date"];
// The categories a DETECTOR or a manual entry can produce, split by the preset
// tier that first switches them on. Together they mirror
// engine.AllEntityCategories, enforced by ../category_parity_test.go.
export const SOFT_NAME_CATEGORIES = ["entity_names", "project_names", "identifier_names"];
export const MEDIUM_NAME_CATEGORIES = ["person_names", "product_names", "brand_names"];
export const ADVANCED_NAME_CATEGORIES = ["other_names"];
export const NAME_CATEGORIES = [
  ...SOFT_NAME_CATEGORIES, ...MEDIUM_NAME_CATEGORIES, ...ADVANCED_NAME_CATEGORIES,
];
// custom_patterns is the user's own regex. It is declarative rather than
// detected, which is why it sits on its own here and in its own rail group.
export const DECLARED_CATEGORIES = ["custom_patterns"];
export const ALL_CATEGORIES = [
  ...HARD_PII_CATEGORIES, ...EXTENDED_PII_CATEGORIES, ...ADVANCED_PII_CATEGORIES,
  ...NAME_CATEGORIES, ...DECLARED_CATEGORIES,
];

/**
 * presetCategories(level) mirrors engine.PresetSelection exactly:
 *
 *   soft hard and extended PII + entity, project and identifier names
 *            + custom patterns
 *   medium soft + person, product and brand names (the default)
 *   advanced medium + amounts, dates and other names
 *
 * @param {string} level "soft", "medium" or "advanced"
 * @returns {object} every category in ALL_CATEGORIES mapped to on/off
 */
export function presetCategories(level) {
  const sel = {};
  for (const c of ALL_CATEGORIES) sel[c] = false;
  const on = [
    ...HARD_PII_CATEGORIES, ...EXTENDED_PII_CATEGORIES,
    ...SOFT_NAME_CATEGORIES, ...DECLARED_CATEGORIES,
  ];
  if (level === "medium" || level === "advanced") on.push(...MEDIUM_NAME_CATEGORIES);
  if (level === "advanced") on.push(...ADVANCED_PII_CATEGORIES, ...ADVANCED_NAME_CATEGORIES);
  for (const c of on) sel[c] = true;
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
    // A choice question resolves to null on cancel, a yes/no one to false;
    // settle whichever is pending with its own "cancelled" value so an awaiter
    // sees the shape it expected rather than a foreign one.
    const stale = confirmResolve;
    const wasChoice = !!state.confirm?.choices;
    confirmResolve = null;
    stale(wasChoice ? null : false);
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

// --- Notices and the in-app confirm ----------------------

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
 * answer).
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
        // No choices: this is the yes/no dialog.
        choices: null,
      },
    });
  });
}

/**
 * askChoice(question) shows the in-app dialog as a PICK-ONE: instead of the
 * yes/no pair it renders one button per choice, and resolves to the chosen
 * id (or null when the user cancels with the backdrop, Escape or Cancel).
 *
 * It shares the confirm slot and the single pending promise with askConfirm,
 * so the same superseding and reset rules apply. It is the mechanism behind
 * "Group with", where the user must say WHICH of the participating values
 * becomes the surviving one before the merge happens.
 *
 * @param {object} question
 * @param {string} question.title the short heading
 * @param {string} question.body what the pick means, in full sentences
 * @param {Array<{id: string, label: string}>} question.choices the options; each
 *   renders as one button whose click resolves to its id
 * @param {string} [question.cancelLabel] default "Cancel"
 * @param {boolean} [question.keyBearing] tints the dialog with the key surface
 * @returns {Promise<string|null>} the chosen id, or null when cancelled
 */
export function askChoice(question) {
  if (confirmResolve) {
    // Superseding a pending question settles it as cancelled. A choice resolves
    // null, a yes/no resolves false: use the shape the pending awaiter expects.
    const stale = confirmResolve;
    const wasChoice = !!state.confirm?.choices;
    confirmResolve = null;
    stale(wasChoice ? null : false);
  }
  return new Promise((resolve) => {
    confirmResolve = resolve;
    setState({
      confirm: {
        title: question?.title ?? "Choose",
        body: question?.body ?? "",
        cancelLabel: question?.cancelLabel ?? "Cancel",
        keyBearing: !!question?.keyBearing,
        choices: (question?.choices ?? []).map((c) => ({ id: c.id, label: c.label })),
      },
    });
  });
}

/**
 * answerConfirm(answer) closes the open question and settles its promise.
 * modal.js calls it from the two buttons and from the Escape key (which
 * answers false, the same as Cancel).
 *
 * When the open question is a CHOICE (askChoice), there is no true/false
 * answer to give: the only thing this path represents is cancellation, so it
 * settles the promise with null, matching askChoice's contract. answerChoice
 * is what carries an actual pick.
 * @param {boolean} answer
 * @returns {boolean} whether there was a question to answer
 */
export function answerConfirm(answer) {
  const resolve = confirmResolve;
  const wasChoice = !!state.confirm?.choices;
  confirmResolve = null;
  if (state.confirm) setState({ confirm: null });
  if (resolve) resolve(wasChoice ? null : !!answer);
  return !!resolve;
}

/**
 * answerChoice(id) settles a choice question with the picked id. modal.js
 * calls it from each choice button. A missing id resolves null, the same as a
 * cancel, so a malformed button can never fabricate a selection.
 * @param {string} id the chosen choice's id
 * @returns {boolean} whether there was a question to answer
 */
export function answerChoice(id) {
  const resolve = confirmResolve;
  confirmResolve = null;
  if (state.confirm) setState({ confirm: null });
  if (resolve) resolve(id ?? null);
  return !!resolve;
}

// --- Category selection reducers --------------------------

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
      // Every preset switches all three country-specific
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
 * state change (, the "Select all" / "Deselect all" buttons
 * on each Configure group).
 *
 * Doing it in one setState rather than looping toggleCategory matters:
 * every setState repaints, so a loop over eight keys would repaint eight
 * times and, before the fix, scroll the page eight times.
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
 * Values outside 0 to 1 are rejected (returns null)
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
// session starts from.
export const SMART_DETECT_DEFAULTS = {
  minLength: 4,
  minOccurrences: 1,
  excludeCommonWords: true,
  minConfidence: 0.5,
  strictness: "balanced",
};

// SMART_STRICTNESS_VALUES are the accepted strictness levels, mirroring the
// engine's StrictnessLenient/Balanced/Strict constants. A value outside this
// set is ignored by the setter, exactly like an out-of-range number.
export const SMART_STRICTNESS_VALUES = ["lenient", "balanced", "strict"];

/**
 * setSmartDetectOptions(patch) merges a partial tuning into the settings
 * Only the known keys are accepted, and each is
 * validated: a bad value is IGNORED rather than stored, because these
 * options decide what the user gets to review, and a silently broken one
 * would look like Smart detection being broken.
 * @param {object} patch any subset of the options
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
  if (typeof p.strictness === "string" && SMART_STRICTNESS_VALUES.includes(p.strictness)) {
    next.strictness = p.strictness;
  }
  setState({ settings: { ...state.settings, smartDetect: next } });
  return next;
}

/**
 * smartDetectOptions(s) is the tuning to SEND to Go: the stored options
 * with every default filled in, so a session written before  (no
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

// --- Local-AI gating --------------------------------------

/**
 * setUseAI(on) records the user's explicit "Use local AI" choice.
 */
export function setUseAI(on) {
  setState({ settings: { ...state.settings, useAI: !!on } });
}

// --- Local-AI scan scope ----------------------------------

/**
 * setAIScope(patch) records which document and page range the local AI reads,
 * clamped to what actually exists. An empty or unknown docName resets the scope
 * to "every document, whole", so a stale selection (a document that was removed)
 * can never send an out-of-range request. from is pinned to <= to, and both to
 * the selected document's addressable unit count (DocumentInfo.pageCount), so
 * the numbers the user sees are always requestable.
 * @param {object} patch any subset of {docName, fromPage, toPage}
 * @returns {object} the stored scope after clamping
 */
export function setAIScope(patch) {
  const next = { ...state.aiScope, ...(patch ?? {}) };
  const doc = state.documents.find((d) => d.name === next.docName);
  if (!next.docName || !doc) {
    const cleared = { docName: "", fromPage: 1, toPage: 1 };
    setState({ aiScope: cleared });
    return cleared;
  }
  const count = Math.max(1, doc.pageCount || 1);
  let from = Number.isInteger(next.fromPage) ? next.fromPage : 1;
  let to = Number.isInteger(next.toPage) ? next.toPage : from;
  from = Math.min(Math.max(1, from), count);
  to = Math.min(Math.max(from, to), count);
  const scope = { docName: next.docName, fromPage: from, toPage: to };
  setState({ aiScope: scope });
  return scope;
}

/**
 * aiScopeArg(s) is the scope to hand runDetection, or null when the local AI
 * should read every document whole. Kept out of the settings payload on
 * purpose: the scope is a per-run choice, not a saved setting.
 */
export function aiScopeArg(s = state) {
  const sc = s.aiScope;
  if (!sc || !sc.docName) return null;
  return { docName: sc.docName, fromPage: sc.fromPage, toPage: sc.toPage };
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

// --- Screen navigation -----------------------------------

/** SCREENS: the valid top-level screens.
 *
 * "docs" was retired by: the documentation is no longer an
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

// --- Navigation guards ------------------------------------------------------

/**
 * canGoTo(step, s) implements the wizard guards. There are three, and they all
 * live here rather than in the five places that navigate, so the step bar and
 * all four screen footers give the same answer (nav.js, frontend/CLAUDE.md).
 *
 *   1. no step beyond Import without documents;
 *   2. no Anonymise while suggestions are still waiting to be reviewed
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
 * This replaced 's LEGACY_STEP_TOKENS + migrateStep() pair, which
 * translated retired tokens onto current ones.  removed
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

// --- Per-step reset -----------------------------------------

/**
 * STEP_RESETS says what each wizard step OWNS, and therefore what going
 * back past it clears. It is a table rather than a switch so the answer
 * to "what does this step own?" is readable in one place, and so the
 * cross-step test matrix can iterate it.
 *
 * Two things are deliberately absent from every entry:
 *
 *   documents Imported files are step 1 data and survive ALL navigation
 *              Re-importing a batch because the
 *              user stepped back one screen would be indefensible.
 *   allowlist The never-anonymise list is shared by the Configure and
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
  // Identify owns BOTH halves of its screen: the rail's
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
    resultDoc: null,
    mapping: null,
    simpleRules: [],
    replacedValues: [],
    removedValues: [],
    dismissedWarnings: [],
  }),
  // Export owns the per-document metadata review decisions.
  export: () => ({ metaReview: {} }),
};

/**
 * resetStep(step) clears exactly what that step owns.
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
 * resetStepsForBackward(current, target) resets every step being LEFT BEHIND by
 * a backward move: all steps AFTER the target, up to and including the current
 * one. The target is where the user lands, so its own data survives (you step
 * back to Identify to review its values, not to lose them); the steps between
 * the target and the current step are the work being discarded.
 *
 * navigateTo used to reset only the current step. A single-step move (Anonymise
 * back to Identify) was therefore correct, but a multi-step move (Anonymise back
 * to Import) reset only Anonymise and left the Identify step's detected values
 * in place, so the "clean" Import screen still carried a previous document's
 * values. Resetting the whole span is the fix, and it keeps the single-step case
 * identical.
 *
 * @param {string} current the step being left
 * @param {string} target the step being moved to
 * @returns {string[]} the step tokens that were reset, in wizard order
 */
export function resetStepsForBackward(current, target) {
  const from = WIZARD_STEPS.indexOf(current);
  const to = WIZARD_STEPS.indexOf(target);
  if (from < 0 || to < 0 || to >= from) return [];
  const reset = [];
  for (let i = to + 1; i <= from; i++) {
    if (resetStep(WIZARD_STEPS[i])) reset.push(WIZARD_STEPS[i]);
  }
  return reset;
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
 * There is no prevStep() any more. It moved one step BACK
 * without the reset question, which made it a way around the rule nav.js exists
 * to enforce: an accidental caller would silently skip the confirmation the user
 * is owed. Backward movement goes through nav.js goBack.
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
  // A local-AI scope that named a document no longer in the list would send an
  // out-of-range request, so it resets to "every document" when its target is
  // gone (or its page count shrank under the stored range).
  const scopedDoc = documents.find((d) => d.name === state.aiScope?.docName);
  const scopeStillValid = scopedDoc &&
    state.aiScope.toPage <= Math.max(1, scopedDoc.pageCount || 1);
  setState({
    documents,
    importErrors: result.errors ?? [],
    previewDoc: previewStillValid ? state.previewDoc : (documents[0]?.name ?? null),
    aiScope: scopeStillValid ? state.aiScope : { docName: "", fromPage: 1, toPage: 1 },
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

// --- Entity review reducers ----------------------------------------
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
 *
 * Adding a value ENABLES its category. A value in "My values" is a value the
 * user has committed to replacing, but the pipeline drops any value whose
 * category switch is off (engine.filterEntities), so a person accepted under
 * the Soft preset, which leaves person_names off, would be listed as "ready to
 * replace" and then silently survive. Detection finds names by shape without
 * regard to the switches, so acceptance is the only moment that can reconcile
 * the two: turning the category on here keeps "My values" and the checkboxes
 * telling the same story, and flips the preset to Custom (selectionPresetName)
 * exactly as ticking the box by hand would.
 */
export function addEntities(items) {
  const existing = new Set(state.entities.map((e) => entityKey(e.category, e.canonical)));
  const added = [];
  // The categories the added values need switched on. Collected so the whole
  // batch (accept-all can add across categories) flips in the one setState.
  const enable = {};
  for (const item of items) {
    const canonical = (item.canonical ?? "").trim();
    if (!canonical || existing.has(entityKey(item.category, canonical))) continue;
    existing.add(entityKey(item.category, canonical));
    // Only a real engine category is switchable; an unknown key would write a
    // phantom switch the pipeline never reads.
    if (ALL_CATEGORIES.includes(item.category)) enable[item.category] = true;
    added.push({
      category: item.category,
      canonical,
      manualVariants: item.manualVariants ?? [],
      // variants: null = "not yet expanded" (the view shows a pending
      // placeholder and triggers expansion); [] = "expanded, none found"
      // (explicit empty state). Distinguishing the two is the heart of
      // the fix.
      variants: item.variants ?? null,
      variantError: null,
      excludedVariants: item.excludedVariants ?? [],
      status: "accepted",
    });
  }
  if (added.length) {
    setState({
      entities: [...state.entities, ...added],
      settings: {
        ...state.settings,
        categories: { ...state.settings.categories, ...enable },
      },
    });
  }
  return added.length;
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
 *  Go error text instead of a forever-pending placeholder. */
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

// --- Candidate review reducers ----------------------------
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
 * acceptAllShown(texts) bulk-accepts the candidates whose values are listed,
 * ACROSS categories.
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

// --- Variant regrouping -----------------------------------

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

// --- Value editing (the My values tab) -----------------------------------
//
// Detection proposes; the user corrects. These reducers are the correction:
// renaming a value, editing a spelling, moving a value to another type,
// merging two values, and clearing the whole list. Each one that changes what
// a value MATCHES resets that row's variants to pending (null), so Go re-runs
// the expansion and the chips shown always describe the value as it stands.

/**
 * renameEntity(category, canonical, newCanonical) changes a value's name.
 *
 * The expansion depends on the name, so the row goes back to pending and Go
 * re-derives the spellings. A rename onto a name the same category already
 * holds is refused rather than silently merged: two values with one name would
 * be exactly the ambiguity conflict detection exists to prevent, and merging is
 * a separate, explicit gesture (groupEntities).
 *
 * @returns {string} "" on success, or a reason ("empty" | "duplicate" |
 *   "not found") the caller can turn into feedback
 */
export function renameEntity(category, canonical, newCanonical) {
  const next = (newCanonical ?? "").trim();
  const key = entityKey(category, canonical);
  const cur = state.entities.find((e) => entityKey(e.category, e.canonical) === key);
  if (!cur) return "not found";
  if (!next) return "empty";
  if (next === cur.canonical) return ""; // unchanged
  const newKey = entityKey(category, next);
  if (newKey !== key && state.entities.some((e) => entityKey(e.category, e.canonical) === newKey)) {
    return "duplicate";
  }
  setState({
    entities: state.entities.map((e) =>
      entityKey(e.category, e.canonical) === key
        ? { ...e, canonical: next, variants: null, variantError: null }
        : e),
  });
  return "";
}

/**
 * renameVariant(category, canonical, oldVariant, newVariant) edits one spelling.
 *
 * A spelling is either automatic (Go expanded it from the name) or manual (the
 * user typed it). Editing either one means the SAME two effects: the old
 * spelling stops applying (it is excluded, so the next expansion cannot bring
 * an automatic one straight back) and the new spelling is added as a manual
 * variant. Editing the spelling that IS the name is a rename of the value, so
 * it routes to renameEntity instead of orphaning the canonical.
 *
 * @returns {string} "" on success, or a reason ("empty" | "not found")
 */
export function renameVariant(category, canonical, oldVariant, newVariant) {
  const next = (newVariant ?? "").trim();
  const old = (oldVariant ?? "").trim();
  if (!next) return "empty";
  if (next.toLowerCase() === old.toLowerCase()) return ""; // unchanged
  if (old.toLowerCase() === (canonical ?? "").trim().toLowerCase()) {
    return renameEntity(category, canonical, next);
  }
  const key = entityKey(category, canonical);
  const cur = state.entities.find((e) => entityKey(e.category, e.canonical) === key);
  if (!cur) return "not found";

  const oldLower = old.toLowerCase();
  const nextLower = next.toLowerCase();
  setState({
    entities: state.entities.map((e) => {
      if (entityKey(e.category, e.canonical) !== key) return e;
      const manual = (e.manualVariants ?? []).filter((x) => x.toLowerCase() !== oldLower);
      // Add the new spelling unless the row already carries it.
      const already = manual.some((x) => x.toLowerCase() === nextLower);
      return {
        ...e,
        manualVariants: already ? manual : [...manual, next],
        // Exclude the old spelling so an automatic expansion cannot re-add it,
        // and un-exclude the new one in case it was excluded before.
        excludedVariants: [
          ...(e.excludedVariants ?? []).filter((x) => x.toLowerCase() !== nextLower),
          old,
        ],
        variants: null,
        variantError: null,
      };
    }),
  });
  return "";
}

/**
 * changeEntityCategory(fromCategory, canonical, toCategory) moves a value to a
 * different type.
 *
 * The type decides the expansion (a person expands to initials and surname, an
 * organisation does not), so the row re-expands. Moving to a type that already
 * holds this exact name is refused, for the same reason a rename onto a taken
 * name is: it would be the ambiguity conflict. Adding to a type switches that
 * type on, exactly as accepting a value into it does (addEntities), so the
 * pipeline does not drop the value it was just told to replace.
 *
 * @returns {string} "" on success, or a reason ("invalid" | "duplicate" |
 *   "not found")
 */
export function changeEntityCategory(fromCategory, canonical, toCategory) {
  if (!ALL_CATEGORIES.includes(toCategory)) return "invalid";
  if (fromCategory === toCategory) return "";
  const fromKey = entityKey(fromCategory, canonical);
  const cur = state.entities.find((e) => entityKey(e.category, e.canonical) === fromKey);
  if (!cur) return "not found";
  const toKey = entityKey(toCategory, canonical);
  if (state.entities.some((e) => entityKey(e.category, e.canonical) === toKey)) {
    return "duplicate";
  }
  setState({
    entities: state.entities.map((e) =>
      entityKey(e.category, e.canonical) === fromKey
        ? { ...e, category: toCategory, variants: null, variantError: null }
        : e),
    settings: {
      ...state.settings,
      categories: { ...state.settings.categories, [toCategory]: true },
    },
  });
  return "";
}

/**
 * changeCandidateCategory(text, toCategory) retypes a suggestion before it is
 * accepted.
 *
 * Detection guesses a type from a value's shape and is often wrong about which
 * KIND of name it found ("Meridian" is as plausibly a project as a company).
 * Fixing it on the suggestion row means the value lands in the right type the
 * moment it is accepted, rather than being accepted wrong and moved after.
 *
 * @returns {boolean} whether a candidate was retyped
 */
export function changeCandidateCategory(text, toCategory) {
  if (!ALL_CATEGORIES.includes(toCategory)) return false;
  const key = candidateKey(text);
  let changed = false;
  const candidates = state.candidates.map((c) => {
    if (candidateKey(c.text) !== key || c.category === toCategory) return c;
    changed = true;
    return { ...c, category: toCategory };
  });
  if (changed) setState({ candidates });
  return changed;
}

/**
 * groupEntities(target, sources) merges one or more values into a target value.
 *
 * Every spelling the sources carried (their name, their automatic variants and
 * their manual ones) becomes a manual variant of the target, and the sources
 * are removed. This is how a user says "these are all the same real-world
 * thing": afterwards one value, with one placeholder, owns every spelling, which
 * is the invariant the collision conflict warns is broken when they are left
 * apart.
 *
 * The target re-expands so its own automatic variants regenerate around the
 * merged set. A source that is the target, or is unknown, is ignored.
 *
 * @param {{category, canonical}} target the value to keep
 * @param {Array<{category, canonical}>} sources the values to fold in
 * @returns {number} how many source values were merged
 */
export function groupEntities(target, sources) {
  const targetKey = entityKey(target?.category, target?.canonical ?? "");
  const keep = state.entities.find((e) => entityKey(e.category, e.canonical) === targetKey);
  if (!keep) return 0;

  const sourceKeys = new Set(
    (sources ?? [])
      .map((sc) => entityKey(sc.category, sc.canonical ?? ""))
      .filter((k) => k !== targetKey));
  if (sourceKeys.size === 0) return 0;

  const folded = state.entities.filter((e) => sourceKeys.has(entityKey(e.category, e.canonical)));
  if (folded.length === 0) return 0;

  // Collect every spelling the sources brought, deduplicated and never equal to
  // the target's own name (that spelling is already the target).
  const targetLower = keep.canonical.trim().toLowerCase();
  const gained = new Map(); // lower -> display
  const take = (v) => {
    const t = (v ?? "").trim();
    const k = t.toLowerCase();
    if (!t || k === targetLower || gained.has(k)) return;
    gained.set(k, t);
  };
  for (const src of folded) {
    take(src.canonical);
    for (const v of src.variants ?? []) take(v);
    for (const v of src.manualVariants ?? []) take(v);
  }

  const existing = new Set((keep.manualVariants ?? []).map((x) => x.toLowerCase()));
  const additions = [...gained.entries()]
    .filter(([lower]) => !existing.has(lower))
    .map(([, display]) => display);

  setState({
    entities: state.entities
      .filter((e) => !sourceKeys.has(entityKey(e.category, e.canonical)))
      .map((e) => {
        if (entityKey(e.category, e.canonical) !== targetKey) return e;
        return {
          ...e,
          manualVariants: [...(e.manualVariants ?? []), ...additions],
          // A folded spelling might have been excluded on the target before;
          // pulling it in un-excludes it so it actually applies.
          excludedVariants: (e.excludedVariants ?? [])
            .filter((x) => !gained.has(x.toLowerCase())),
          variants: null,
          variantError: null,
        };
      }),
  });
  return folded.length;
}

/** clearAllEntities() empties the value list. Returns how many it removed, so a
 *  caller can report it and skip the confirm when there is nothing to clear. */
export function clearAllEntities() {
  const n = state.entities.length;
  if (n) setState({ entities: [] });
  return n;
}

// --- Conflict detection for the My values tab ----------------------------
//
// The engine validates values before a run and refuses to touch any text when
// two values would claim the same string (backend/engine/conflicts.go). By then
// the user has left this screen. entityConflicts computes the SAME blocking
// conflicts here, purely from state, so the values that would refuse the run are
// highlighted on the card that owns them BEFORE the user walks on to Anonymise.
//
// It reproduces three of the engine's blocking checks, the ones about declared
// values: a name declared under two types (ambiguity), a spelling two values
// both claim (collision), and a value that is also on the never-anonymise list.
// The simple-rule checks are the Anonymise screen's, not this tab's.

/**
 * spellingsOf(e) is every lower-cased spelling a value would match: its name,
 * its Go-expanded variants and its manual variants, minus the excluded ones.
 * It mirrors engine.ExpandVariants so the highlight agrees with the run's check.
 * @returns {Map<string,string>} lower-cased spelling -> a display spelling
 */
export function spellingsOf(e) {
  const excluded = new Set((e.excludedVariants ?? []).map((x) => x.trim().toLowerCase()));
  const out = new Map();
  const add = (v) => {
    const t = (v ?? "").trim();
    const k = t.toLowerCase();
    if (!t || excluded.has(k) || out.has(k)) return;
    out.set(k, t);
  };
  add(e.canonical);
  for (const v of e.variants ?? []) add(v);
  for (const v of e.manualVariants ?? []) add(v);
  return out;
}

/** categoryActive(s, category) reports whether a category's switch is on. A
 *  value in an off category is never replaced, so it can never conflict. */
function categoryActive(s, category) {
  const cats = s.settings?.categories;
  return cats ? !!cats[category] : true;
}

/**
 * entityConflicts(s) is the blocking conflicts among the current values, keyed
 * by entity so the view can paint the exact card, name or chip at fault.
 *
 * Only values whose type is switched on are considered, matching the engine:
 * an off category is not going to be replaced, so nothing about it is ambiguous.
 *
 * It returns STRUCTURE, not sentences: the user-visible wording is the view's
 * (copy.js is the single home for it). Each conflict is one of three kinds:
 *   ambiguity  the same name under two types; `withCategory` names the other.
 *   allowlist  the name is also a never-anonymise term.
 *   collision  a spelling two values both claim; `withValue` names the other,
 *              `withKey` is its entityKey (so "group with it" can target it).
 *
 * @param {object} [s] state
 * @returns {Map<string, {nameConflicts: Array, variantConflicts: Map<string,object>, list: Array}>}
 *   entityKey -> that value's conflicts. `nameConflicts` are the ones that
 *   fault the NAME, `variantConflicts` maps a lower-cased spelling to the one
 *   that faults that chip, and `list` is every conflict on the card.
 */
export function entityConflicts(s = state) {
  const active = s.entities.filter((e) => categoryActive(s, e.category));
  const result = new Map();
  const ensure = (key) => {
    if (!result.has(key)) {
      result.set(key, { nameConflicts: [], variantConflicts: new Map(), list: [] });
    }
    return result.get(key);
  };

  // Ambiguity: the same name declared under two different types.
  const byName = new Map(); // lower(canonical) -> [entity]
  for (const e of active) {
    const k = e.canonical.trim().toLowerCase();
    if (!byName.has(k)) byName.set(k, []);
    byName.get(k).push(e);
  }
  for (const group of byName.values()) {
    if (new Set(group.map((e) => e.category)).size < 2) continue;
    for (const e of group) {
      const other = group.find((o) => o.category !== e.category);
      const entry = ensure(entityKey(e.category, e.canonical));
      const conflict = {
        kind: "ambiguity", value: e.canonical, spelling: e.canonical,
        withCategory: other.category,
      };
      entry.nameConflicts.push(conflict);
      entry.list.push(conflict);
    }
  }

  // Allowlist collision: a value that is also on the never-anonymise list.
  const allow = new Set((s.allowlist ?? []).map((t) => t.trim().toLowerCase()));
  if (allow.size) {
    for (const e of active) {
      if (!allow.has(e.canonical.trim().toLowerCase())) continue;
      const entry = ensure(entityKey(e.category, e.canonical));
      const conflict = { kind: "allowlist", value: e.canonical, spelling: e.canonical };
      entry.nameConflicts.push(conflict);
      entry.list.push(conflict);
    }
  }

  // Collision: one spelling claimed by two different values.
  const owners = new Map(); // lower(spelling) -> [{key, e, display}]
  for (const e of active) {
    for (const [lower, display] of spellingsOf(e)) {
      if (!owners.has(lower)) owners.set(lower, []);
      owners.get(lower).push({ key: entityKey(e.category, e.canonical), e, display });
    }
  }
  for (const [lower, list] of owners) {
    if (new Set(list.map((o) => o.key)).size < 2) continue;
    for (const owner of list) {
      const other = list.find((o) => o.key !== owner.key);
      const entry = ensure(owner.key);
      const isName = owner.e.canonical.trim().toLowerCase() === lower;
      // When two values share a name outright, the ambiguity check above has
      // already flagged that name. Reporting the same clash a second time as a
      // canonical collision would show the user two sentences for one problem,
      // so it is suppressed on the name (the card stays flagged either way).
      if (isName && entry.nameConflicts.some((c) => c.kind === "ambiguity")) continue;
      const conflict = {
        kind: "collision", value: owner.display, spelling: owner.display,
        withKey: other.key, withValue: other.e.canonical,
        withCategory: other.e.category,
      };
      if (isName) entry.nameConflicts.push(conflict);
      else entry.variantConflicts.set(lower, conflict);
      entry.list.push(conflict);
    }
  }

  return result;
}

// --- Reassignment helpers --------------------------------

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

// --- The Anonymise screen's editing surfaces ---------------------------------

/**
 * setValueTables(replaced, removed) mirrors the Go registry into the store.
 *
 * Both halves land together because they are one picture: a value moves from
 * one list to the other, and updating them separately shows it in both or in
 * neither for one repaint.
 *
 * @param {Array} replaced rows from api.js valuePlaceholders()
 * @param {Array} removed rows from api.js listRemovedValues()
 */
export function setValueTables(replaced, removed) {
  setState({ replacedValues: replaced ?? [], removedValues: removed ?? [] });
}

/**
 * dismissWarning(id) hides one run warning.
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

/**
 * blockingConflicts(s) is the conflicts that made the last run refuse to touch
 * any text.
 *
 * The engine validates the declared values BEFORE pass 1, and a blocking
 * conflict (two values that would both claim the same spelling, a value that is
 * also allowlisted, and so on) aborts the run before the registry is mutated.
 * When that happens the results carry empty documents and an empty report, so a
 * screen that reads only the report shows a finished run that replaced nothing.
 * That is the mismatch a refused run produces: a zero summary beside a value
 * table still holding an earlier run's registry. This is the one field that
 * says the run was refused, and why, so the Anonymise screen can explain it
 * instead of leaving the user to guess.
 * @param {object} [s] state
 * @returns {Array<{kind, severity, value, message, fix}>}
 */
export function blockingConflicts(s = state) {
  return s.results?.validation?.blocking ?? [];
}

// --- Same-format metadata review --------------------------

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
 * startNewBatch() clears THIS batch and keeps the setup.
 *
 * The split is the whole point, so it is spelled out rather than left to a
 * reader of the object literal:
 *
 *   CLEARED the documents, the run and everything it produced (results, the
 *             mapping, the pending values, the dismissed warnings), the values
 *             and suggestions, the custom patterns, the find-and-replace rules,
 *             and the per-document metadata review decisions. All of it is about
 *             the batch that just finished.
 *   KEPT the settings (categories, confidence, smart-detection tuning, the
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
    replacedValues: [],
    removedValues: [],
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

/** clearAllowlist() empties the never-anonymise list in one action and returns
 *  the number of terms it removed, so the caller can report the count. */
export function clearAllowlist() {
  const cleared = state.allowlist.length;
  setState({ allowlist: [] });
  return cleared;
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

// --- Simple-replace rule reducers ----------------------------------
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

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
  //   (there is no section flag: Smart detection reads ON when any of its
  //   three methods is on, and a stored fourth boolean could disagree with them.)
  //   the offline heuristic pass is ON by default, and
  //                   deactivable. It is now a DERIVED value, written on every
  //                   settings push = (useBuiltInPatterns || useHeuristicDiscovery), so the
  //                   section header and any backward-compat reader still see the
  //                   route as "on" when either half is. Its scope (the
  //                   categories, the preset, the confidence floor) and its
  //                   tuning are the settings it reads, which is why the rail
  //                   nests them inside it.
  //   useBuiltInPatterns the MASTER SWITCH over the regex signal categories
  //                   (email, VAT, IBAN, amount, date, ...). ON by default. OFF
  //                   means no signal category is replaced at anonymisation time,
  //                   whatever the per-category checkboxes say; the checkboxes
  //                   keep their selection so turning it back on restores it.
  //   useHeuristicDiscovery the offline word-frequency pass (engine SmartDetect). ON by
  //                   default.
  //   useLocalAI the local model (Ollama). OFF by default. Detecting
  //                   Ollama ENABLES the switch, it never flips it: turning on
  //                   a route that sends the document to a model, however
  //                   local, is the user's decision to make.
  // aiStrictFormat asks the local AI to answer for EVERY category instead of
  //   only the ones it thought of. OFF by default: it sometimes finds a little
  //   more, and usually takes about twice as long.
  // aiDetailLevel is how much text one local AI request carries: "thorough"
  //   (the default: smaller slices, the most values, the most requests) or
  //   "faster" (larger slices, fewer requests, and nothing found at all on a
  //   small model). Mirrors engine.AllDetailLevels; see AI_DETAIL_LEVELS below.
  // contextSize is the Ollama num_ctx setting, default 8192.
  // minConfidence is the detection-confidence floor, 0 to
  // 1 on the engine's scale. 0 is the default and keeps every detection,
  // which is exactly the behaviour before the setting existed.
  settings: {
    level: "medium", categories: null, ollamaPort: 11434, model: "", country: DEFAULT_COUNTRY,
    contextSize: 8192, useLocalAI: false, aiStrictFormat: false,
    aiDetailLevel: "thorough",
    useBuiltInPatterns: true, useHeuristicDiscovery: true,
    // Which READINGS of which built-in signals may DERIVE Suggestions.
    // Data-driven, keyed by SIGNAL_SOURCES and then by SIGNAL_DERIVATIONS: a new
    // source or reading needs no new field here.
    signalSuggestionSources: {
      email: { "email.person": true, "email.organisation": true },
    },
    minConfidence: 0,
    // heuristicDiscovery is the tuning for the offline Smart
    // detection pass, matching engine.SmartDetectOptions field for field.
    // The defaults are the STRICTER ones (engine
    // DefaultSmartDetectOptions), because over-detection was the reported
    // problem; a user who wants everything back sets them to 0/false.
    heuristicDiscovery: {
      minLength: 4,
      minOccurrences: 1,
      excludeCommonWords: true,
      minConfidence: 0.5,
      strictness: "balanced",
    },
  },

  // Local-AI SCAN SCOPE (CLAUDE.md §5): which ONE document, and which of its own
  // units (pages/slides/rows/lines), the local model reads. Handing a whole
  // document to a small local model is "too much", so the user can aim the scan.
  //   docName ""     means "every document, whole", the unchanged behaviour.
  //   mode "all"     scans the whole selected document.
  //   mode "pages"   scans only the units named in the free-text `pages` spec
  //                  (parsePageSpec parses "14", "12-15", "12,13,18-20").
  // It is a transient per-run choice, deliberately NOT part of settings, so it
  // never travels in a session file and never reaches Go through applySettings.
  aiScope: { docName: "", mode: "all", pages: "" },

  // Entity review state: array of
  // {category, mainText, spellings, status: "accepted"|"denied"}.
  values: [],

  // Allowlist terms (display spellings).
  allowlist: [],

  // Custom regex patterns: array of {expr, error} (error = compile
  // message or null).
  patterns: [],

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
  //   removedValues  [{original, category, placeholder, derivedSpellings}]
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

  // What the LOCAL AI actually did on the last run, or null before the first
  // one: {requests, silent, truncated, secondsPerRequest}, straight from the Go
  // result.
  //
  // It is kept because "0 suggestions" means two different things and only one
  // of them is about the document. The seconds are MEASURED on this machine and
  // this document, which is the only way a user can judge how a scan will feel:
  // no fixed sentence in a tooltip knows their laptop.
  // aiRequestEstimate is how many model requests the CURRENT scope and detail
  // level would send, answered by Go with the same helper the run uses, so the
  // read-out cannot promise a number the run then contradicts. Null before the
  // first answer: a read-out that guesses while it waits is worse than none.
  aiRequestEstimate: null,
  lastAIScan: null,

  // Unified suggestion review list: suggestions from
  // any discovery method wait HERE until explicitly accepted; nothing
  // flows into values without user confirmation. Each row:
  // {source: "smart"|"local-ai", text, category, count, contexts}.
  suggestions: [],

  // The values another detection route also claims, as Go last answered
  // (api.js checkIntersections). They are WARNINGS, never blocking: the
  // precedence rule always has an answer, so refusing the run would punish the
  // user for a configuration the engine can resolve.
  //
  // It is cleared by every change to the value list rather than left to go
  // stale, because a warning describing a configuration the user has already
  // changed is worse than no warning: it is read as a statement about the one
  // in front of them.
  intersections: [],

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

// --- Detection routes, as provenance ------------------------------------
//
// DISCOVERY_METHODS mirrors engine.AllDiscoveryMethods exactly and is checked by
// ../detection_parity_test.go. It is PROVENANCE: which methods found a Value.
//
// A Value carries a SET of these, not one, because several methods can find the
// same thing and two routes agreeing is corroboration worth showing rather than
// a fact to overwrite. Built-in pattern matching and custom pattern matching are
// absent on purpose: they produce direct matches, never Suggestions, so nothing
// they find is ever a Value with provenance to record.
export const DISCOVERY_METHODS = ["manual", "signal", "heuristic", "local_ai"];

// MATCH_CLASSES mirrors engine.AllMatchClasses exactly, in PRECEDENCE order
// (lower index wins), and is checked by ../detection_parity_test.go.
//
// It is a different question from provenance: this one answers "which
// overlapping claim wins". The engine derives it from a Value's discovery
// methods and nothing here writes it onto a Value; the frontend reads it only to
// NAME the winning method in an intersection warning. Keeping the two apart is
// what stops raising the confidence floor from silently reordering precedence.
export const MATCH_CLASSES = [
  "built_in_pattern", "user_defined", "smart_discovered", "local_ai_discovered",
];

// SIGNAL_SOURCES mirrors engine.AllSignalSources exactly and is checked by
// ../detection_parity_test.go. Each entry is a built-in signal that may DERIVE
// Suggestions, and the checklist in the rail is built from this list, so adding
// a source is one constant here and one implementation in Go rather than a new
// row, a new field and a new persisted flag.
export const SIGNAL_SOURCES = ["email"];

// AI_DETAIL_LEVELS mirrors engine.AllDetailLevels exactly, in the order the rail
// offers them, and is checked by ../detection_parity_test.go. The dropdown is
// built from it, so a third option invented here would be a control the user can
// pick and the engine then refuses.
//
// There is deliberately no "whole document in one request" level on either side.
// It measures zero values on every model tried, and a choice whose outcome is
// "finds nothing" is a broken switch rather than an option.
export const AI_DETAIL_LEVELS = ["thorough", "faster"];

// SIGNAL_DERIVATIONS mirrors engine.SignalDerivations exactly and is checked by
// the same guard. Each entry lists, per signal, the READINGS that signal supports,
// in display order: a source is a signal the pattern pass matched, a derivation is
// one reading of it through one mechanism. The rail renders this tree, so a new
// reading is one entry here and one producer in Go.
export const SIGNAL_DERIVATIONS = {
  email: ["email.person", "email.organisation"],
};

/**
 * signalDerivationOn(s, source, derivation) reports whether ONE reading of a
 * signal may derive Suggestions. This is the leaf question, and the only one
 * stored.
 *
 * A MISSING key reads as ON at either level, mirroring
 * engine.SignalDerivationEnabled: a settings object that has not been filled in
 * yet must behave like the shipped default, not like a user who switched
 * everything off. Only an explicit false is off.
 *
 * @param {object} s the state
 * @param {string} source one of SIGNAL_SOURCES
 * @param {string} derivation one of SIGNAL_DERIVATIONS[source]
 * @returns {boolean} whether that reading may produce Suggestions
 */
export function signalDerivationOn(s = state, source, derivation) {
  if (!(SIGNAL_DERIVATIONS[source] ?? []).includes(derivation)) return false;
  return s.settings?.signalSuggestionSources?.[source]?.[derivation] !== false;
}

/**
 * signalSourceOn(s, source) reports whether a signal may derive anything at all,
 * which is true when ANY of its readings is on.
 *
 * DERIVED, never stored. It is the master the rail shows on the signal's own row,
 * and a persisted fourth flag beside the readings it summarises could disagree
 * with them: a row reading "on" while every reading under it is off lies about
 * what a run does. Same reasoning as smartDetectionOn.
 *
 * @param {object} s the state
 * @param {string} source one of SIGNAL_SOURCES
 * @returns {boolean} whether signal-based discovery may use this source
 */
export function signalSourceOn(s = state, source) {
  return (SIGNAL_DERIVATIONS[source] ?? [])
    .some((derivation) => signalDerivationOn(s, source, derivation));
}

/**
 * enabledSignalDerivations(s, source) lists that source's readings currently
 * switched on, in SIGNAL_DERIVATIONS order. The collapsed signal row reads it to
 * say "Off" or to name what is on.
 *
 * @param {object} s the state
 * @param {string} source one of SIGNAL_SOURCES
 * @returns {string[]} the enabled derivation identifiers
 */
export function enabledSignalDerivations(s = state, source) {
  return (SIGNAL_DERIVATIONS[source] ?? [])
    .filter((derivation) => signalDerivationOn(s, source, derivation));
}

/**
 * enabledSignalSources(s) lists the sources with at least one reading on, in
 * SIGNAL_SOURCES order.
 *
 * @param {object} s the state
 * @returns {string[]} the enabled source identifiers
 */
export function enabledSignalSources(s = state) {
  return SIGNAL_SOURCES.filter((source) => signalSourceOn(s, source));
}

/**
 * setSignalDerivation(source, derivation, on) switches ONE reading of one signal.
 *
 * It writes only that leaf. Clearing a reading stops the Suggestions THAT reading
 * produces and leaves the signal's own anonymisation alone, which is governed by
 * Built-in patterns and the category's own switch. That distinction is the whole
 * reason this setting is separate, and it now holds per reading.
 *
 * @param {string} source one of SIGNAL_SOURCES
 * @param {string} derivation one of SIGNAL_DERIVATIONS[source]
 * @param {boolean} on whether it may derive Suggestions
 * @returns {boolean} whether the pair is a known one and was written
 */
export function setSignalDerivation(source, derivation, on) {
  if (!(SIGNAL_DERIVATIONS[source] ?? []).includes(derivation)) return false;
  setState({
    settings: {
      ...state.settings,
      signalSuggestionSources: {
        ...state.settings.signalSuggestionSources,
        [source]: {
          ...state.settings.signalSuggestionSources?.[source],
          [derivation]: !!on,
        },
      },
    },
  });
  return true;
}

/**
 * setSignalSource(source, on) is the MASTER over one signal's readings: it writes
 * EVERY derivation of that source at once.
 *
 * It is not a flag of its own (signalSourceOn derives that for display); it is the
 * one gesture that saves the user N clicks to switch a whole signal off, and the
 * gesture the Smart detection section's own master reaches through.
 *
 * @param {string} source one of SIGNAL_SOURCES
 * @param {boolean} on whether its readings may derive Suggestions
 * @returns {boolean} whether the source is a known one and was written
 */
export function setSignalSource(source, on) {
  if (!SIGNAL_SOURCES.includes(source)) return false;
  const derivations = {};
  for (const derivation of SIGNAL_DERIVATIONS[source] ?? []) {
    derivations[derivation] = !!on;
  }
  setState({
    settings: {
      ...state.settings,
      signalSuggestionSources: {
        ...state.settings.signalSuggestionSources,
        [source]: derivations,
      },
    },
  });
  return true;
}

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
 *
 * It has ONE special case: a patch that changes the values or the patterns
 * invalidates the intersection warnings, because those describe how the value
 * list overlapped the LAST time Go was asked. A stale intersection warning is
 * worse than none: it sits on a card describing a configuration the user has
 * already changed, and it is read as a statement about the one in front of
 * them. Doing it here rather than in each of the dozen reducers that touch the
 * list means a reducer added later cannot forget.
 *
 * @param {object} patch fields to update.
 */
export function setState(patch) {
  const invalidates = ("values" in patch || "patterns" in patch) &&
    !("intersections" in patch);
  Object.assign(state, patch);
  if (invalidates) state.intersections = [];
  notify();
}

/**
 * setIntersections(list) stores Go's answer about which values another route
 * also claims. It is the ONLY writer, and it is deliberately a separate call
 * from the edit that triggered the recheck: the edit clears the list, the
 * answer arrives later, and a screen showing nothing in between is correct.
 *
 * @param {Array} list rows from api.js checkIntersections
 */
export function setIntersections(list) {
  setState({ intersections: Array.isArray(list) ? list : [] });
}

/**
 * intersectionsFor(s) is the intersections keyed by the value they belong to,
 * so a card attaches its own with no searching, exactly as valueConflicts is
 * consumed.
 *
 * @param {object} [s] state
 * @returns {Map<string, object>} valueKey(category, value) -> the row
 */
export function intersectionsFor(s = state) {
  const out = new Map();
  for (const row of s.intersections ?? []) {
    if (!row?.value || !row?.category) continue;
    out.set(valueKey(row.category, row.value), row);
  }
  return out;
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

// HEURISTIC_DISCOVERY_DEFAULTS mirrors engine.DefaultSmartDetectOptions. It is
// exported so a session loaded from an older file, which has no
// heuristicDiscovery block at all, can be filled with the same defaults a fresh
// session starts from.
export const HEURISTIC_DISCOVERY_DEFAULTS = {
  minLength: 4,
  minOccurrences: 1,
  excludeCommonWords: true,
  minConfidence: 0.5,
  strictness: "balanced",
};

// STRICTNESS_VALUES are the accepted strictness levels, mirroring the
// engine's StrictnessLenient/Balanced/Strict constants. A value outside this
// set is ignored by the setter, exactly like an out-of-range number.
export const STRICTNESS_VALUES = ["lenient", "balanced", "strict"];

/**
 * setHeuristicDiscoveryOptions(patch) merges a partial tuning into the settings
 * Only the known keys are accepted, and each is
 * validated: a bad value is IGNORED rather than stored, because these
 * options decide what the user gets to review, and a silently broken one
 * would look like Smart detection being broken.
 * @param {object} patch any subset of the options
 * @returns {object} the stored options after the merge
 */
export function setHeuristicDiscoveryOptions(patch) {
  const current = { ...HEURISTIC_DISCOVERY_DEFAULTS, ...(state.settings.heuristicDiscovery ?? {}) };
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
  if (typeof p.strictness === "string" && STRICTNESS_VALUES.includes(p.strictness)) {
    next.strictness = p.strictness;
  }
  setState({ settings: { ...state.settings, heuristicDiscovery: next } });
  return next;
}

/**
 * heuristicDiscoveryOptions(s) is the tuning to SEND to Go: the stored options
 * with every default filled in, so a session written before  (no
 * heuristicDiscovery block) still produces a complete payload.
 */
export function heuristicDiscoveryOptions(s = state) {
  return { ...HEURISTIC_DISCOVERY_DEFAULTS, ...(s.settings.heuristicDiscovery ?? {}) };
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
 * setUseLocalAI(on) records the user's explicit "Use local AI" choice.
 */
export function setUseLocalAI(on) {
  setState({ settings: { ...state.settings, useLocalAI: !!on } });
}

/**
 * adoptProbe(status) is THE one way a probe result reaches the store: it lands
 * in `state.ollama` and its RESOLVED MODEL is adopted into `settings.model`.
 *
 * Adopting the model is what makes the dropdown show the model that will
 * actually run. Go resolves the effective model from what the probe just saw
 * (the stored choice when it is installed, then the pinned default, then the
 * first installed one), and a store that kept an empty or uninstalled name
 * beside it would leave the interface naming one model while the run posted to
 * another. An empty resolved model leaves the setting alone: there was nothing
 * to run, so there is nothing to adopt, and a stopped server must not erase the
 * user's choice.
 *
 * Detecting Ollama still never flips `useLocalAI`: sending a document to a
 * model is a decision the user makes.
 */
export function adoptProbe(status) {
  const patch = { ollama: status };
  if (status?.model) {
    patch.settings = { ...state.settings, model: status.model };
  }
  setState(patch);
}

// --- Local-AI scan scope ----------------------------------

/**
 * parsePageSpec(spec, maxPage) turns a free-text page spec into a sorted,
 * de-duplicated set of 1-based unit indices.
 *
 * The spec is comma-separated; each token is either a single number "N" or a
 * range "A-B" (inclusive). Whitespace around tokens and either side of the dash
 * is ignored. Indices outside 1..maxPage are dropped silently (a stale UI or a
 * typed number past the end is the user's request, not a crash), and a token
 * that is not a number or a range sets `error` naming the FIRST bad token while
 * still returning whatever valid pages parsed, so the read-out stays useful.
 * An empty or whitespace-only spec is not an error: it resolves to no pages.
 *
 * @param {string} spec the raw text, e.g. "14", "12-15", "12,13,18-20"
 * @param {number} maxPage the selected document's addressable unit count
 * @returns {{pages: number[], error: (string|null)}}
 */
export function parsePageSpec(spec, maxPage) {
  const set = new Set();
  let error = null;
  const max = Number.isInteger(maxPage) && maxPage > 0 ? maxPage : 0;
  const text = String(spec ?? "").trim();
  if (text === "") return { pages: [], error: null };
  for (const rawToken of text.split(",")) {
    const token = rawToken.trim();
    if (token === "") continue;
    const range = token.match(/^(\d+)\s*-\s*(\d+)$/);
    if (range) {
      let lo = parseInt(range[1], 10);
      let hi = parseInt(range[2], 10);
      if (lo > hi) { const t = lo; lo = hi; hi = t; }
      for (let n = lo; n <= hi; n++) {
        if (n >= 1 && n <= max) set.add(n);
      }
      continue;
    }
    if (/^\d+$/.test(token)) {
      const n = parseInt(token, 10);
      if (n >= 1 && n <= max) set.add(n);
      continue;
    }
    // A token that is neither "N" nor "A-B": name the first one that fails,
    // but keep the pages already parsed so the read-out is not blanked.
    if (error === null) error = token;
  }
  const pages = [...set].sort((a, b) => a - b);
  return { pages, error };
}

/**
 * setAIScope(patch) records which document the local AI reads and, within it,
 * whether to scan the whole document ("all") or a set of pages ("pages"). An
 * empty or unknown docName resets the scope to "every document, whole", so a
 * stale selection (a document that was removed) can never send a request for a
 * document that is gone. The `pages` string is stored verbatim; it is parsed
 * against the selected document's unit count only when the bridge arg is built
 * (aiScopeArg), so the read-out and the request always agree.
 * @param {object} patch any subset of {docName, mode, pages}
 * @returns {object} the stored scope
 */
export function setAIScope(patch) {
  const next = { ...state.aiScope, ...(patch ?? {}) };
  const doc = state.documents.find((d) => d.name === next.docName);
  if (!next.docName || !doc) {
    const cleared = { docName: "", mode: "all", pages: "" };
    setState({ aiScope: cleared });
    return cleared;
  }
  const scope = {
    docName: next.docName,
    mode: next.mode === "pages" ? "pages" : "all",
    pages: typeof next.pages === "string" ? next.pages : "",
  };
  setState({ aiScope: scope });
  return scope;
}

/**
 * aiScopeArg(s) is the scope to hand runDetection, or null when the local AI
 * should read every document whole. Kept out of the settings payload on
 * purpose: the scope is a per-run choice, not a saved setting.
 *
 * The backend shape is {docName, pages: number[]} where an EMPTY pages array
 * means "the whole selected document"; that is what "all" mode, and any spec
 * that resolves to nothing, both send. A null return keeps today's meaning:
 * every document, whole.
 */
export function aiScopeArg(s = state) {
  const sc = s.aiScope;
  if (!sc || !sc.docName) return null;
  if (sc.mode !== "pages") return { docName: sc.docName, pages: [] };
  const doc = s.documents.find((d) => d.name === sc.docName);
  const max = doc ? Math.max(1, doc.pageCount || 1) : 0;
  const { pages } = parsePageSpec(sc.pages, max);
  return { docName: sc.docName, pages };
}

/**
 * smartDetectionOn(s) is the Smart detection SECTION's state: on when any of its
 * three methods is on.
 *
 * It is DERIVED, never stored. A fourth persisted boolean would be a second way
 * of saying something the three methods already say, and the two could disagree:
 * a stored "on" beside three methods that are all off is a section that claims to
 * be running and does nothing.
 *
 * @param {object} s the state
 * @returns {boolean} whether Smart detection contributes anything
 */
export function smartDetectionOn(s = state) {
  return !!(s.settings.useBuiltInPatterns || s.settings.useHeuristicDiscovery
    || enabledSignalSources(s).length > 0);
}

/**
 * setSmartDetection(on) is the section-level master: it switches every one of
 * Smart detection's methods in ONE action.
 *
 * The section switch is a convenience over the children, not a state of its own,
 * so switching the section off means switching the three methods off and nothing
 * else. Switching it back on restores all three to their defaults rather than to
 * whatever they were, because a master that remembers a partial configuration is
 * a master that sometimes appears not to work.
 */
export function setSmartDetection(on) {
  const value = !!on;
  // Every source's every READING, not one flag per source: the stored shape is the
  // nested one, and writing a boolean where a map belongs would leave the whole
  // signal method reading as its default for the rest of the session.
  const sources = {};
  for (const source of SIGNAL_SOURCES) {
    sources[source] = {};
    for (const derivation of SIGNAL_DERIVATIONS[source] ?? []) {
      sources[source][derivation] = value;
    }
  }
  setState({
    settings: {
      ...state.settings,
      useBuiltInPatterns: value,
      useHeuristicDiscovery: value,
      signalSuggestionSources: sources,
    },
  });
}

/**
 * setUseBuiltInPatterns(on) switches built-in pattern matching.
 *
 * It is the MASTER over the structured signal categories (email, VAT, IBAN,
 * amount, date, ...). OFF means no signal category is replaced at anonymisation
 * time, whatever the per-category checkboxes say; the selection is left intact so
 * turning it back on restores exactly what was chosen.
 *
 * It does NOT govern whether those signals may DERIVE Suggestions: that is
 * signalSuggestionSources, and the two are separate so clearing one cannot
 * silently clear the other.
 */
export function setUseBuiltInPatterns(on) {
  setState({ settings: { ...state.settings, useBuiltInPatterns: !!on } });
}

/**
 * setUseHeuristicDiscovery(on) switches heuristic discovery: spelling, context,
 * frequency and deterministic gazetteers.
 *
 * ON by default, because it needs nothing installed and a user who has just
 * imported documents expects the application to look at them. Switchable because
 * its output is Suggestions, which are guesses, and someone who wants only the
 * deterministic passes plus their own declared Values should be able to say so.
 */
export function setUseHeuristicDiscovery(on) {
  setState({ settings: { ...state.settings, useHeuristicDiscovery: !!on } });
}

/**
 * detectionRoutesOn(s) is how many DISCOVERY routes are enabled. Zero means the
 * detect button has nothing to run, which the UI says rather than running an
 * empty pass and reporting "0 suggestions" as if it had looked.
 *
 * Built-in pattern matching is deliberately not counted: it produces direct
 * matches at anonymisation time, not Suggestions, so having it on does not give
 * the detect button anything to do.
 */
export function detectionRoutesOn(s = state) {
  const smartDiscovers = s.settings.useHeuristicDiscovery
    || enabledSignalSources(s).length > 0;
  return (smartDiscovers ? 1 : 0) + (llmEnabled(s) ? 1 : 0);
}

/**
 * llmEnabled(s) is THE gate for every AI-dependent control (Local AI
 * detection): the master toggle must be on AND Ollama must be reachable.
 */
export function llmEnabled(s = state) {
  return !!(s.settings.useLocalAI && s.ollama?.available);
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
 * values, step, ...) is deliberately untouched, so Home and back loses
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
  if (step === "anonymise" && s.suggestions.length > 0) return false; // still unreviewed
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
 * The Ollama connection settings (port, model, context size, useLocalAI) are
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
      heuristicDiscovery: { ...HEURISTIC_DISCOVERY_DEFAULTS },
    },
    values: [],
    suggestions: [],
    intersections: [],
    patterns: [],
    discovery: null,
    lastAIScan: null,
  }),
  // Anonymise owns the run itself, everything it produced, and the editing
  // surfaces that only exist once there is a result to edit.
  anonymise: () => ({
    running: false,
    progress: null,
    results: null,
    resultDoc: null,
    mapping: null,
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
  // A local-AI scope that named a document no longer in the list would target a
  // document that is gone, so it resets to "every document" when its target
  // disappears. A document that merely SHRANK needs no reset: the page spec is
  // stored as text and re-parsed against the current unit count at send time
  // (aiScopeArg), so out-of-range units are dropped then, not stored now.
  const scopedDoc = documents.find((d) => d.name === state.aiScope?.docName);
  setState({
    documents,
    importErrors: result.errors ?? [],
    previewDoc: previewStillValid ? state.previewDoc : (documents[0]?.name ?? null),
    aiScope: scopedDoc ? state.aiScope : { docName: "", mode: "all", pages: "" },
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
// Values are keyed by (category, mainText), one row per real-world
// entity. status "accepted" values feed the pipeline; "denied" ones are
// kept visible (struck through) so the user sees what discovery proposed.

/** valueKey(category, mainText), case-insensitive identity of a row. */
export function valueKey(category, mainText) {
  return `${category}|${mainText.trim().toLowerCase()}`;
}

/**
 * addValues(items) adds proposals or manual entries, skipping duplicates.
 * items: [{category, mainText, derivedSpellings?}], derivedSpellings (from Go expansion)
 * may be attached later via setValueSpellings.
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
/**
 * foldIntoFamily(category, mainText) folds a ONE-AT-A-TIME addition into an
 * existing value of the same type when the two are spellings of one thing.
 *
 * Detection folds families over its whole output (engine FoldValueFamilies),
 * but values also arrive singly: the "Add missed Value" row, and the Compare
 * pane's "add as a new value". Without this, typing "Coca-Cola company" beside
 * an existing "Coca-Cola" creates a rival, and the shorter one then fires
 * inside the longer one: the text reads "[BRAND_1] company", the rest of the
 * phrase survives, and two numbers are spent on one company.
 *
 * The SHORTER form is always the main value, so this folds in both directions:
 * a longer addition becomes a spelling of the existing value, and a shorter one
 * takes over as the value's name with the old name kept as a spelling.
 *
 * Same rules as the engine's, and for the same reasons: one category only (a
 * person "Delta" and an organisation "Delta Industries" are an intersection,
 * not a family), word boundaries only ("Alten" is not a spelling of
 * "Altenberg"), and never below MIN_SPELLING_LEN, because promoting a
 * two-character stem to a main value would shred ordinary text.
 *
 * @param {string} category the type the new value would be filed under
 * @param {string} mainText the value being added
 * @returns {{main: string, added: string}|null} the surviving value's name and
 *   the spelling that was folded in, or null when there is no family
 */
export function foldIntoFamily(category, mainText) {
  const added = (mainText ?? "").trim();
  if (!added) return null;

  for (const e of state.values) {
    if (e.category !== category) continue;
    const existing = (e.mainText ?? "").trim();
    if (!existing || existing.toLowerCase() === added.toLowerCase()) continue;

    // Rune length, not byte length: an accented name must not be judged longer
    // than it looks.
    const [shorter, longer] = [...existing].length <= [...added].length
      ? [existing, added] : [added, existing];
    if (!isFamilyPair(shorter, longer)) continue;

    if (shorter === existing) {
      // The addition is the longer form: it becomes a spelling of the value
      // that is already there.
      addSpelling(category, existing, added);
      return { main: existing, added };
    }
    // The addition is SHORTER, so it takes over as the value's name and the
    // old name becomes one of its spellings. Renaming rather than adding a
    // second value is what keeps them sharing one placeholder.
    renameValue(category, existing, added);
    addSpelling(category, added, existing);
    return { main: added, added: existing };
  }
  return null;
}

// MIN_SPELLING_LEN mirrors engine.minVariantLen: a spelling shorter than this is
// never derived or promoted, because replacing every "Al" or "BV" would shred
// ordinary text.
export const MIN_SPELLING_LEN = 3;

/**
 * isFamilyPair(shorter, longer) reports whether the two are spellings of one
 * thing: the shorter occurs inside the longer at WORD BOUNDARIES. It mirrors
 * engine.canJoinFamily, so the frontend's one-at-a-time fold agrees with the
 * one detection already applied.
 */
function isFamilyPair(shorter, longer) {
  if ([...shorter].length < MIN_SPELLING_LEN) return false;
  if ([...shorter].length >= [...longer].length) return false;
  // \p{L} and \p{N} rather than \b: \b is ASCII-only, and an accented name
  // ("Amélie") would fail its boundary check on the wrong side.
  const escaped = shorter.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`(?<![\\p{L}\\p{N}])${escaped}(?![\\p{L}\\p{N}])`, "iu").test(longer);
}

export function addValues(items) {
  const existing = new Set(state.values.map((e) => valueKey(e.category, e.mainText)));
  const added = [];
  // The categories the added values need switched on. Collected so the whole
  // batch (accept-all can add across categories) flips in the one setState.
  const enable = {};
  for (const item of items) {
    const mainText = (item.mainText ?? "").trim();
    if (!mainText || existing.has(valueKey(item.category, mainText))) continue;
    existing.add(valueKey(item.category, mainText));
    // Only a real engine category is switchable; an unknown key would write a
    // phantom switch the pipeline never reads.
    if (ALL_CATEGORIES.includes(item.category)) enable[item.category] = true;
    added.push({
      category: item.category,
      mainText,
      spellings: item.spellings ?? [],
      // derivedSpellings: null = "not yet expanded" (the view shows a pending
      // placeholder and triggers expansion); [] = "expanded, none found"
      // (explicit empty state). Distinguishing the two is the heart of
      // the fix.
      derivedSpellings: item.derivedSpellings ?? null,
      spellingsError: null,
      // PROVENANCE: every method that found this Value. It travels to Go, which
      // reduces the set to one match class when a contest has to be decided. A
      // Value that states nothing was declared by the user.
      discoveryMethods: item.discoveryMethods?.length ? [...item.discoveryMethods] : ["manual"],
      // WHY those methods produced it, carried across from the Suggestion. The
      // workspace renders it, so a Value can explain itself after the fact.
      evidence: item.evidence ?? [],
      // "automatic" means Go may still derive spellings; "curated" means main
      // text plus the listed spellings IS the complete set. Every Value starts
      // uncurated.
      spellingPolicy: item.spellingPolicy === "curated" ? "curated" : "automatic",
      // HOW MUCH this Value is trusted, which is a third thing from provenance
      // and from precedence. 0 means "not stated", which Go reads as a user
      // declaration and scores accordingly, so it is the right default for a
      // Value the user typed and the wrong one to leave on a Local AI finding:
      // that is why the number has to survive the whole way from the bridge to
      // here. Minimum confidence is the control it feeds.
      confidence: typeof item.confidence === "number" ? item.confidence : 0,
      status: "accepted",
    });
  }
  if (added.length) {
    setState({
      values: [...state.values, ...added],
      settings: {
        ...state.settings,
        categories: { ...state.settings.categories, ...enable },
      },
    });
  }
  return added.length;
}

/** deleteValue(category, mainText) deletes a row outright. */
export function deleteValue(category, mainText) {
  setState({
    values: state.values.filter((e) =>
      valueKey(e.category, e.mainText) !== valueKey(category, mainText)),
  });
}

/**
 * deleteValues(keys) deletes several values in ONE state change.
 *
 * One change, not a loop over deleteValue, because each reducer call notifies
 * every subscriber: removing twenty values one at a time repaints the workspace
 * twenty times, and each repaint re-wires the cards the next removal is about to
 * delete. It also makes the removal atomic, so a subscriber can never observe
 * half of a bulk clear.
 *
 * @param {Array<string>} keys valueKeys, as valueKey(category, mainText) builds them
 * @returns {number} how many rows were actually removed, so the caller can report it
 */
export function deleteValues(keys) {
  const wanted = new Set(keys ?? []);
  if (wanted.size === 0) return 0;
  const kept = state.values.filter((e) => !wanted.has(valueKey(e.category, e.mainText)));
  const removed = state.values.length - kept.length;
  if (removed) setState({ values: kept });
  return removed;
}

/** setValueSpellings stores the Go-expanded spelling list on a row
 *  ([] is a valid "no derivedSpellings" answer, distinct from pending null). */
export function setValueSpellings(category, mainText, derivedSpellings) {
  setState({
    values: state.values.map((e) =>
      valueKey(e.category, e.mainText) === valueKey(category, mainText)
        ? { ...e, derivedSpellings: derivedSpellings ?? [], spellingsError: null }
        : e),
  });
}

/** setValueSpellingError records a failed expansion so the row shows the
 *  Go error text instead of a forever-pending placeholder. */
export function setValueSpellingError(category, mainText, message) {
  setState({
    values: state.values.map((e) =>
      valueKey(e.category, e.mainText) === valueKey(category, mainText)
        ? { ...e, spellingsError: message ?? "expansion failed" }
        : e),
  });
}

/** addSpelling appends a user-typed spelling to a row (deduplicated). */
export function addSpelling(category, mainText, spelling) {
  const v = (spelling ?? "").trim();
  if (!v) return;
  setState({
    values: state.values.map((e) => {
      if (valueKey(e.category, e.mainText) !== valueKey(category, mainText)) return e;
      if (e.spellings.some((m) => m.toLowerCase() === v.toLowerCase())) return e;
      // Adding a spelling re-expands ONLY this row (derivedSpellings back to
      // pending null, Phase 7a).
      return { ...e, spellings: [...e.spellings, v], derivedSpellings: null, spellingsError: null };
    }),
  });
}

/**
 * curate(e, derivedSpellings) freezes a value's spellings: the list becomes exactly
 * `derivedSpellings`, and the automatic expansion stops applying.
 *
 * It is what makes a deletion stick without a negative rule. The alternative,
 * a per-value exclusion list, is a rule with no home in the interface: it is
 * invisible except as the absence of a chip, it cannot be undone, and it does
 * the job of the never-anonymise list, which is the one place negative rules
 * are meant to live and be visible. After curation the chips on the card ARE
 * the list, so a deleted spelling is simply not in it any more.
 *
 * The mainText name is kept in the list when it is passed in, because that is
 * how the automatic expansion returns it and how the card draws it: dropping it
 * on curation would silently remove a chip the user never touched. Go
 * deduplicates it against the mainText anyway.
 *
 * @param {object} e the entity to curate
 * @param {string[]} derivedSpellings the complete list of spellings, in display form
 * @returns {object} a new entity; the caller writes it into the store
 */
export function curate(e, derivedSpellings) {
  const seen = new Set();
  const list = [];
  for (const v of derivedSpellings ?? []) {
    const t = (v ?? "").trim();
    const k = t.toLowerCase();
    if (!t || seen.has(k)) continue;
    seen.add(k);
    list.push(t);
  }
  return {
    ...e,
    spellings: list,
    spellingPolicy: "curated",
    // A curated row is settled by definition: Go has nothing left to derive,
    // so the chips are shown straight away rather than through a pending
    // expansion that would come back with the same list.
    derivedSpellings: [],
    spellingsError: null,
  };
}

/**
 * acceptedValues(s) is the pipeline-ready Value list.
 *
 * The spellings and the spelling POLICY travel to Go so its derivation matches
 * the chips on the card, and the discovery methods travel so precedence is
 * decided from the same provenance the workspace shows. Evidence travels too:
 * it is what a session file needs to explain a Value after a reload.
 */
export function acceptedValues(s = state) {
  return s.values
    .filter((v) => v.status === "accepted")
    .map((v) => ({
      category: v.category,
      mainText: v.mainText,
      spellings: v.spellings,
      spellingPolicy: v.spellingPolicy === "curated" ? "curated" : "automatic",
      discoveryMethods: v.discoveryMethods?.length ? v.discoveryMethods : ["manual"],
      evidence: v.evidence ?? [],
    }));
}

/**
 * relatedTo(row, rows) lists the OTHER rows that share a piece of evidence with
 * this one.
 *
 * Shared evidence makes two rows RELATED, never one Value. "Tpps France" and
 * "Tpps S.A." both come from the same email domain, and they may genuinely be two
 * legal entities or two country branches; folding them automatically would give
 * one placeholder to two companies, and the mapping CSV would then say two
 * different organisations were the same one. So this returns a NOTE for the user
 * to act on, and the grouping stays their decision.
 *
 * Relatedness is computed rather than stored, so it cannot go stale against a row
 * the user has since accepted, rejected or retyped.
 *
 * @param {object} row the row to explain
 * @param {object[]} rows every row of the same kind (Suggestions, or Values)
 * @returns {string[]} the other rows' main texts, in their own order
 */
export function relatedTo(row, rows) {
  const keys = new Set((row?.evidence ?? []).map(evidenceKey));
  if (keys.size === 0) return [];
  const self = suggestionKey(row.mainText);
  return (rows ?? [])
    .filter((other) => suggestionKey(other.mainText) !== self
      && (other.evidence ?? []).some((e) => keys.has(evidenceKey(e))))
    .map((other) => other.mainText);
}

/**
 * evidenceKey(e) identifies one piece of evidence by the RELATIONSHIP it records,
 * ignoring which documents it was found in. Mirrors the engine's own key, so the
 * two sides agree on what "the same evidence" means.
 */
function evidenceKey(e) {
  return `${e?.kind ?? ""}|${e?.signalCategory ?? ""}|${e?.signalText ?? ""}`;
}

// --- Suggestion review reducers ----------------------------
//
// The review gate: discovery methods ADD suggestions; only an explicit
// accept turns a suggestion into an entity. Suggestions are keyed by
// lower-cased text (one row per distinct name across sources).

/** suggestionKey(text), case-insensitive identity of a suggestion row. */
export function suggestionKey(text) {
  return (text ?? "").trim().toLowerCase();
}

/**
 * addSuggestions(items) merges ONE unified detection result into the review
 * list.
 *
 * It takes no source argument, and that is the point. Go returns one
 * suggestions list in which each row says which methods found it, so there is
 * no per-route mapping step here to lose anything in. The mapping that existed
 * per route is exactly where the Local AI route's folded spellings used to be
 * discarded: the row was rebuilt as {text, category} and the rest thrown away.
 *
 * A row already in review MERGES rather than being skipped: its methods,
 * evidence, spellings and contexts union, so a second run that finds the same
 * name by a second method updates the row the user is looking at instead of
 * being silently dropped. Names already accepted as Values are skipped, because
 * they are decided.
 *
 * @param {object[]} items the engine's suggestions, in any order
 * @returns {number} how many NEW rows were added
 */
export function addSuggestions(items) {
  const asValues = new Set(state.values.map((v) => v.mainText.trim().toLowerCase()));
  const rows = [...state.suggestions];
  const at = new Map(rows.map((r, i) => [suggestionKey(r.mainText), i]));
  let addedCount = 0;

  for (const item of items ?? []) {
    const mainText = (item.mainText ?? "").trim();
    const key = suggestionKey(mainText);
    if (!mainText || asValues.has(key)) continue;

    const incoming = {
      mainText,
      category: item.category ?? "person_names",
      count: item.count ?? 0,
      contexts: item.contexts ?? [],
      // The longer forms Go folded into this one. Accepting the row carries them
      // across as the Value's spellings, so ONE Value with its spellings reaches
      // the pipeline instead of two rivals, the shorter of which would fire
      // inside the longer and leave the rest of the phrase in clear text.
      spellings: item.spellings ?? [],
      discoveryMethods: item.discoveryMethods ?? [],
      evidence: item.evidence ?? [],
      // HOW MUCH the row is trusted, which the Minimum confidence control acts
      // on once the row is accepted. A Local AI finding arrives at 0.8; 0 means
      // "not stated", which Go reads as a user declaration.
      confidence: typeof item.confidence === "number" ? item.confidence : 0,
    };

    const existing = at.get(key);
    if (existing === undefined) {
      at.set(key, rows.length);
      rows.push(incoming);
      addedCount += 1;
      continue;
    }
    rows[existing] = mergeSuggestionRows(rows[existing], incoming);
  }

  if (rows.length !== state.suggestions.length || addedCount === 0) {
    setState({ suggestions: rows });
  }
  return addedCount;
}

/**
 * mergeSuggestionRows(into, from) unions two rows for the same main text.
 *
 * It mirrors engine.MergeSuggestions: the first-seen spelling wins, counts add
 * up, spellings, contexts, methods and evidence union with the contexts
 * bounded so a second run cannot grow one row without limit, and the strongest
 * confidence wins because two routes agreeing is corroboration. The CATEGORY is
 * kept from the existing row, because the user may already have changed it and a
 * later detection must not silently overrule that.
 */
function mergeSuggestionRows(into, from) {
  return {
    ...into,
    count: (into.count ?? 0) + (from.count ?? 0),
    contexts: mergeContexts(into.contexts, from.contexts),
    spellings: mergeStrings(into.spellings, from.spellings, into.mainText),
    discoveryMethods: mergeStrings(into.discoveryMethods, from.discoveryMethods),
    evidence: mergeEvidence(into.evidence, from.evidence),
    // The STRONGEST confidence wins, as engine.MergeSuggestions does it: two
    // routes agreeing is corroboration, so a row heuristics also found is not
    // demoted to the AI's number just because the AI reported it second.
    confidence: Math.max(into.confidence ?? 0, from.confidence ?? 0),
  };
}

// MAX_SUGGESTION_CONTEXTS mirrors engine maxSuggestionContexts. The row shows a
// few examples, so merging bounds the list rather than losing something the user
// was going to read.
const MAX_SUGGESTION_CONTEXTS = 3;

/** mergeContexts appends the snippets not already present, up to the cap. */
function mergeContexts(into, from) {
  const out = [...(into ?? [])];
  for (const c of from ?? []) {
    if (out.length >= MAX_SUGGESTION_CONTEXTS) break;
    if (c && !out.includes(c)) out.push(c);
  }
  return out;
}

/**
 * mergeStrings unions two lists case-insensitively, keeping first-seen order and
 * dropping `exclude` when given (a Value's spellings never repeat its main text,
 * so carrying it in both places would be two records of one string).
 */
function mergeStrings(into, from, exclude) {
  const seen = new Set();
  if (exclude) seen.add(exclude.trim().toLowerCase());
  const out = [];
  for (const list of [into ?? [], from ?? []]) {
    for (const raw of list) {
      const v = (raw ?? "").trim();
      const k = v.toLowerCase();
      if (!v || seen.has(k)) continue;
      seen.add(k);
      out.push(v);
    }
  }
  return out;
}

/**
 * mergeEvidence unions two evidence lists, deduplicating by RELATIONSHIP (kind,
 * signal category, signal text) and merging each one's document list. The same
 * relationship found in five files is one piece of evidence naming several
 * documents, not five near-identical ones.
 */
function mergeEvidence(into, from) {
  const at = new Map();
  const out = [];
  for (const list of [into ?? [], from ?? []]) {
    for (const e of list) {
      const key = `${e.kind}|${e.signalCategory ?? ""}|${e.signalText ?? ""}`;
      const i = at.get(key);
      if (i === undefined) {
        at.set(key, out.length);
        out.push({ ...e, documents: [...(e.documents ?? [])] });
        continue;
      }
      for (const d of e.documents ?? []) {
        if (d && !out[i].documents.includes(d)) out[i].documents.push(d);
      }
    }
  }
  return out;
}

/**
 * acceptSuggestion(text) promotes one Suggestion into the Value list (with its
 * current category) and removes it from review.
 *
 * Everything the Suggestion knew survives: every discovery method, the evidence
 * behind them, and the spellings that were folded into it. Provenance and
 * evidence are what let the workspace explain a Value after the fact, and the
 * methods are what Go reduces to a match class, so dropping any of them here
 * would silently change both the explanation and which claim wins.
 *
 * @param {string} text the Suggestion's main text
 * @returns {boolean} whether a Value was added
 */
export function acceptSuggestion(text) {
  const key = suggestionKey(text);
  const row = state.suggestions.find((r) => suggestionKey(r.mainText) === key);
  if (!row) return false;
  const added = addValues([valueFromSuggestion(row)]);
  setState({ suggestions: state.suggestions.filter((r) => suggestionKey(r.mainText) !== key) });
  return added > 0;
}

/**
 * valueFromSuggestion(row) is the ONE conversion from a reviewed Suggestion to a
 * Value. Single-accept and bulk-accept both go through it, so neither can lose a
 * field the other keeps.
 *
 * confidence crosses with the rest. It is a CROSS-BRIDGE contract and the Go
 * constants are its source of truth: a Local AI suggestion arrives at 0.8
 * (engine.ConfidenceLLMDefault), and dropping it here would hand the engine a
 * 0, which it reads as "not stated" and scores as a manual declaration at 0.95.
 * Raising Minimum confidence would then leave the model's guesses in place,
 * which is the opposite of what the control says it does. Where two routes
 * found the same thing the merge already kept the higher score, so a row
 * heuristics also found is not demoted by the AI's number.
 */
function valueFromSuggestion(row) {
  return {
    category: row.category,
    mainText: row.mainText,
    spellings: row.spellings ?? [],
    discoveryMethods: row.discoveryMethods ?? [],
    evidence: row.evidence ?? [],
    confidence: row.confidence ?? 0,
  };
}

/** rejectSuggestion(text) drops a Suggestion without a trace. */
export function rejectSuggestion(text) {
  const key = suggestionKey(text);
  setState({ suggestions: state.suggestions.filter((r) => suggestionKey(r.mainText) !== key) });
}

/**
 * acceptAllShown(texts) bulk-accepts the suggestions whose values are listed,
 * ACROSS categories.
 *
 * This is the mock-up's "Accept all shown". It differs from
 * acceptAllInCategory in exactly one way that matters: the suggestions table
 * sorts and filters across every category at once, so "shown" is not a
 * category, it is whatever survived the search, the type filter and the source
 * filter. A per-category button cannot express that, and asking the user to
 * press it once per category would defeat the point of a bulk action.
 *
 * Each accepted suggestion keeps its OWN category rather than being coerced into
 * a shared one: the bulk action is about which rows, not about what they are.
 *
 * @param {string[]} texts the values currently visible, in any order
 * @returns {number} how many values were added
 */
export function acceptAllShown(texts) {
  const shown = new Set((texts ?? []).map(suggestionKey));
  if (shown.size === 0) return 0;
  const batch = state.suggestions.filter((r) => shown.has(suggestionKey(r.mainText)));
  if (!batch.length) return 0;
  const added = addValues(batch.map(valueFromSuggestion));
  setState({ suggestions: state.suggestions.filter((r) => !shown.has(suggestionKey(r.mainText))) });
  return added;
}

/**
 * rejectAllShown(texts) is the mirror of acceptAllShown: it DROPS those
 * suggestions instead of promoting them, exactly as rejectSuggestion does one at
 * a time. Nothing is added to the value list and nothing is remembered, so a
 * rejected suggestion simply stops taking up review space.
 *
 * @param {string[]} texts the values currently visible
 * @returns {number} how many suggestions were removed
 */
export function rejectAllShown(texts) {
  const shown = new Set((texts ?? []).map(suggestionKey));
  if (shown.size === 0) return 0;
  const removed = state.suggestions.filter((r) => shown.has(suggestionKey(r.mainText))).length;
  if (!removed) return 0;
  setState({ suggestions: state.suggestions.filter((r) => !shown.has(suggestionKey(r.mainText))) });
  return removed;
}

// --- Spelling regrouping -----------------------------------

/**
 * moveSpelling(fromCategory, fromMainText, toCategory, toMainText,
 * spelling) moves one spelling spelling between values.
 *
 * The source CURATES without the moved spelling, which is what makes the move
 * stick: its automatic expansion would otherwise regenerate the spelling and
 * two values would claim it again, which is the collision conflict. The target
 * gains it as a manual spelling and re-expands.
 *
 * Pure reducer; the drag-and-drop wiring only calls it. Returns false for
 * self-drops, unknown rows, or a spelling the source does not actually carry.
 */
export function moveSpelling(fromCategory, fromMainText, toCategory, toMainText, spelling) {
  const v = (spelling ?? "").trim();
  if (!v) return false;
  const fromKey = valueKey(fromCategory, fromMainText);
  const toKey = valueKey(toCategory, toMainText);
  if (fromKey === toKey) return false; // cannot drop onto self

  const from = state.values.find((e) => valueKey(e.category, e.mainText) === fromKey);
  const to = state.values.find((e) => valueKey(e.category, e.mainText) === toKey);
  if (!from || !to) return false;

  // The spelling must actually belong to the source row (expanded list or
  // manual additions); otherwise this is a stale drop.
  const lower = v.toLowerCase();
  const carried =
    (from.derivedSpellings ?? []).some((x) => x.toLowerCase() === lower) ||
    (from.spellings ?? []).some((x) => x.toLowerCase() === lower);
  if (!carried) return false;

  setState({
    values: state.values.map((e) => {
      const key = valueKey(e.category, e.mainText);
      if (key === fromKey) {
        // The source keeps every spelling it had except the moved one, and
        // stops re-deriving: an automatic expansion would put it straight back.
        return curate(e, [...spellingsOf(e).values()].filter((x) => x.toLowerCase() !== lower));
      }
      if (key === toKey) {
        const dup = (e.spellings ?? []).some((x) => x.toLowerCase() === lower);
        const spellings = dup ? e.spellings : [...(e.spellings ?? []), v];
        // A target that is already curated keeps its curated list plus the new
        // spelling: re-deriving it would undo an explicit choice.
        if (e.spellingPolicy === "curated") return curate(e, [...(e.derivedSpellings ?? []), ...spellings]);
        return { ...e, spellings, derivedSpellings: null, spellingsError: null };
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
// a value MATCHES resets that row's derivedSpellings to pending (null), so Go re-runs
// the expansion and the chips shown always describe the value as it stands.

/**
 * renameValue(category, mainText, newMainText) changes a value's name.
 *
 * The expansion depends on the name, so the row goes back to pending and Go
 * re-derives the spellings. A rename onto a name the same category already
 * holds is refused rather than silently merged: two values with one name would
 * be exactly the ambiguity conflict detection exists to prevent, and merging is
 * a separate, explicit gesture (groupValues).
 *
 * @returns {string} "" on success, or a reason ("empty" | "duplicate" |
 *   "not found") the caller can turn into feedback
 */
export function renameValue(category, mainText, newMainText) {
  const next = (newMainText ?? "").trim();
  const key = valueKey(category, mainText);
  const cur = state.values.find((e) => valueKey(e.category, e.mainText) === key);
  if (!cur) return "not found";
  if (!next) return "empty";
  if (next === cur.mainText) return ""; // unchanged
  const newKey = valueKey(category, next);
  if (newKey !== key && state.values.some((e) => valueKey(e.category, e.mainText) === newKey)) {
    return "duplicate";
  }
  setState({
    values: state.values.map((e) =>
      valueKey(e.category, e.mainText) === key
        ? { ...e, mainText: next, derivedSpellings: null, spellingsError: null }
        : e),
  });
  return "";
}

/**
 * renameSpelling(category, mainText, oldVariant, newVariant) edits one spelling.
 *
 * A spelling is either automatic (Go expanded it from the name) or manual (the
 * user typed it). Editing either one CURATES the row with the old spelling
 * swapped for the new one: the list becomes the user's, so the next expansion
 * cannot bring the old spelling straight back. Editing the spelling that IS the
 * name is a rename of the value, so it routes to renameValue instead of
 * orphaning the mainText.
 *
 * @returns {string} "" on success, or a reason ("empty" | "not found")
 */
export function renameSpelling(category, mainText, oldVariant, newVariant) {
  const next = (newVariant ?? "").trim();
  const old = (oldVariant ?? "").trim();
  if (!next) return "empty";
  if (next.toLowerCase() === old.toLowerCase()) return ""; // unchanged
  if (old.toLowerCase() === (mainText ?? "").trim().toLowerCase()) {
    return renameValue(category, mainText, next);
  }
  const key = valueKey(category, mainText);
  const cur = state.values.find((e) => valueKey(e.category, e.mainText) === key);
  if (!cur) return "not found";

  const oldLower = old.toLowerCase();
  const nextLower = next.toLowerCase();
  setState({
    values: state.values.map((e) => {
      if (valueKey(e.category, e.mainText) !== key) return e;
      const kept = [...spellingsOf(e).values()].filter((x) => x.toLowerCase() !== oldLower);
      // Add the new spelling unless the row already carries it.
      const already = kept.some((x) => x.toLowerCase() === nextLower);
      return curate(e, already ? kept : [...kept, next]);
    }),
  });
  return "";
}

/**
 * changeValueCategory(fromCategory, mainText, toCategory) moves a value to a
 * different type.
 *
 * The type decides the expansion (a person expands to initials and surname, an
 * organisation does not), so the row re-expands. Moving to a type that already
 * holds this exact name is refused, for the same reason a rename onto a taken
 * name is: it would be the ambiguity conflict. Adding to a type switches that
 * type on, exactly as accepting a value into it does (addValues), so the
 * pipeline does not drop the value it was just told to replace.
 *
 * @returns {string} "" on success, or a reason ("invalid" | "duplicate" |
 *   "not found")
 */
export function changeValueCategory(fromCategory, mainText, toCategory) {
  if (!ALL_CATEGORIES.includes(toCategory)) return "invalid";
  if (fromCategory === toCategory) return "";
  const fromKey = valueKey(fromCategory, mainText);
  const cur = state.values.find((e) => valueKey(e.category, e.mainText) === fromKey);
  if (!cur) return "not found";
  const toKey = valueKey(toCategory, mainText);
  if (state.values.some((e) => valueKey(e.category, e.mainText) === toKey)) {
    return "duplicate";
  }
  setState({
    values: state.values.map((e) =>
      valueKey(e.category, e.mainText) === fromKey
        ? { ...e, category: toCategory, derivedSpellings: null, spellingsError: null }
        : e),
    settings: {
      ...state.settings,
      categories: { ...state.settings.categories, [toCategory]: true },
    },
  });
  return "";
}

/**
 * changeSuggestionCategory(text, toCategory) retypes a suggestion before it is
 * accepted.
 *
 * Detection guesses a type from a value's shape and is often wrong about which
 * KIND of name it found ("Meridian" is as plausibly a project as a company).
 * Fixing it on the suggestion row means the value lands in the right type the
 * moment it is accepted, rather than being accepted wrong and moved after.
 *
 * @returns {boolean} whether a suggestion was retyped
 */
export function changeSuggestionCategory(text, toCategory) {
  if (!ALL_CATEGORIES.includes(toCategory)) return false;
  const key = suggestionKey(text);
  let changed = false;
  const suggestions = state.suggestions.map((c) => {
    if (suggestionKey(c.mainText) !== key || c.category === toCategory) return c;
    changed = true;
    return { ...c, category: toCategory };
  });
  if (changed) setState({ suggestions });
  return changed;
}

/**
 * groupValues(target, sources) merges one or more values into a target value.
 *
 * Every spelling the sources carried (their name, their automatic derivedSpellings and
 * their manual ones) becomes a manual spelling of the target, and the sources
 * are removed. This is how a user says "these are all the same real-world
 * thing": afterwards one value, with one placeholder, owns every spelling, which
 * is the invariant the collision conflict warns is broken when they are left
 * apart.
 *
 * The target re-expands so its own automatic derivedSpellings regenerate around the
 * merged set, unless any participant was curated, in which case the survivor
 * stays curated. A source that is the target, or is unknown, is ignored.
 *
 * @param {{category, mainText}} target the value to keep
 * @param {Array<{category, mainText}>} sources the values to fold in
 * @returns {number} how many source values were merged
 */
export function groupValues(target, sources) {
  const targetKey = valueKey(target?.category, target?.mainText ?? "");
  const keep = state.values.find((e) => valueKey(e.category, e.mainText) === targetKey);
  if (!keep) return 0;

  const sourceKeys = new Set(
    (sources ?? [])
      .map((sc) => valueKey(sc.category, sc.mainText ?? ""))
      .filter((k) => k !== targetKey));
  if (sourceKeys.size === 0) return 0;

  const folded = state.values.filter((e) => sourceKeys.has(valueKey(e.category, e.mainText)));
  if (folded.length === 0) return 0;

  // Collect every spelling the sources brought, deduplicated and never equal to
  // the target's own name (that spelling is already the target).
  const targetLower = keep.mainText.trim().toLowerCase();
  const gained = new Map(); // lower -> display
  const take = (v) => {
    const t = (v ?? "").trim();
    const k = t.toLowerCase();
    if (!t || k === targetLower || gained.has(k)) return;
    gained.set(k, t);
  };
  for (const src of folded) {
    take(src.mainText);
    for (const v of src.derivedSpellings ?? []) take(v);
    for (const v of src.spellings ?? []) take(v);
  }

  const existing = new Set((keep.spellings ?? []).map((x) => x.toLowerCase()));
  const additions = [...gained.entries()]
    .filter(([lower]) => !existing.has(lower))
    .map(([, display]) => display);

  // A merge inherits curation: if the survivor or any folded value had its
  // spellings set by hand, the merged value keeps a settled list rather than
  // letting an automatic expansion re-derive one over the user's choice.
  const curatedMerge = keep.spellingPolicy === "curated"
    || folded.some((e) => e.spellingPolicy === "curated");

  setState({
    values: state.values
      .filter((e) => !sourceKeys.has(valueKey(e.category, e.mainText)))
      .map((e) => {
        if (valueKey(e.category, e.mainText) !== targetKey) return e;
        const spellings = [...(e.spellings ?? []), ...additions];
        // If any participant was curated, the survivor is curated: a merge must
        // not silently re-derive a list the user set by hand.
        if (curatedMerge) return curate(e, [...(e.derivedSpellings ?? []), ...spellings]);
        return { ...e, spellings, derivedSpellings: null, spellingsError: null };
      }),
  });
  return folded.length;
}

/** clearAllValues() empties the value list. Returns how many it removed, so a
 *  caller can report it and skip the confirm when there is nothing to clear. */
export function clearAllValues() {
  const n = state.values.length;
  if (n) setState({ values: [] });
  return n;
}

// --- Conflict detection for the My values tab ----------------------------
//
// The engine validates values before a run and refuses to touch any text when
// two values would claim the same string (backend/engine/conflicts.go). By then
// the user has left this screen. valueConflicts computes the SAME blocking
// conflicts here, purely from state, so the values that would refuse the run are
// highlighted on the card that owns them BEFORE the user walks on to Anonymise.
//
// It reproduces three of the engine's blocking checks, the ones about declared
// values: a name declared under two types (ambiguity), a spelling two values
// both claim (collision), and a value that is also on the never-anonymise list.
// The simple-rule checks are the Anonymise screen's, not this tab's.

/**
 * spellingsOf(e) is every lower-cased spelling a value would match: its name,
 * its Go-expanded derivedSpellings and its manual derivedSpellings. A curated row carries an
 * empty expanded list, so the same walk covers both cases.
 * It mirrors engine.ExpandVariants so the highlight agrees with the run's check.
 * @returns {Map<string,string>} lower-cased spelling -> a display spelling
 */
export function spellingsOf(e) {
  const out = new Map();
  const add = (v) => {
    const t = (v ?? "").trim();
    const k = t.toLowerCase();
    if (!t || out.has(k)) return;
    out.set(k, t);
  };
  add(e.mainText);
  for (const v of e.derivedSpellings ?? []) add(v);
  for (const v of e.spellings ?? []) add(v);
  return out;
}

/** categoryActive(s, category) reports whether a category's switch is on. A
 *  value in an off category is never replaced, so it can never conflict. */
function categoryActive(s, category) {
  const cats = s.settings?.categories;
  return cats ? !!cats[category] : true;
}

/**
 * valueConflicts(s) is the blocking conflicts among the current values, keyed
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
 *              `withKey` is its valueKey (so "group with it" can target it).
 *
 * @param {object} [s] state
 * @returns {Map<string, {nameConflicts: Array, spellingConflicts: Map<string,object>, list: Array}>}
 *   valueKey -> that value's conflicts. `nameConflicts` are the ones that
 *   fault the NAME, `spellingConflicts` maps a lower-cased spelling to the one
 *   that faults that chip, and `list` is every conflict on the card.
 */
export function valueConflicts(s = state) {
  const active = s.values.filter((e) => categoryActive(s, e.category));
  const result = new Map();
  const ensure = (key) => {
    if (!result.has(key)) {
      result.set(key, { nameConflicts: [], spellingConflicts: new Map(), list: [] });
    }
    return result.get(key);
  };

  // Ambiguity: the same name declared under two different types.
  const byName = new Map(); // lower(mainText) -> [entity]
  for (const e of active) {
    const k = e.mainText.trim().toLowerCase();
    if (!byName.has(k)) byName.set(k, []);
    byName.get(k).push(e);
  }
  for (const group of byName.values()) {
    if (new Set(group.map((e) => e.category)).size < 2) continue;
    for (const e of group) {
      const other = group.find((o) => o.category !== e.category);
      const entry = ensure(valueKey(e.category, e.mainText));
      const conflict = {
        kind: "ambiguity", value: e.mainText, spelling: e.mainText,
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
      if (!allow.has(e.mainText.trim().toLowerCase())) continue;
      const entry = ensure(valueKey(e.category, e.mainText));
      const conflict = { kind: "allowlist", value: e.mainText, spelling: e.mainText };
      entry.nameConflicts.push(conflict);
      entry.list.push(conflict);
    }
  }

  // Collision: one spelling claimed by two different values.
  const owners = new Map(); // lower(spelling) -> [{key, e, display}]
  for (const e of active) {
    for (const [lower, display] of spellingsOf(e)) {
      if (!owners.has(lower)) owners.set(lower, []);
      owners.get(lower).push({ key: valueKey(e.category, e.mainText), e, display });
    }
  }
  for (const [lower, list] of owners) {
    if (new Set(list.map((o) => o.key)).size < 2) continue;
    for (const owner of list) {
      const other = list.find((o) => o.key !== owner.key);
      const entry = ensure(owner.key);
      const isName = owner.e.mainText.trim().toLowerCase() === lower;
      // When two values share a name outright, the ambiguity check above has
      // already flagged that name. Reporting the same clash a second time as a
      // mainText collision would show the user two sentences for one problem,
      // so it is suppressed on the name (the card stays flagged either way).
      if (isName && entry.nameConflicts.some((c) => c.kind === "ambiguity")) continue;
      const conflict = {
        kind: "collision", value: owner.display, spelling: owner.display,
        withKey: other.key, withValue: other.e.mainText,
        withCategory: other.e.category,
      };
      if (isName) entry.nameConflicts.push(conflict);
      else entry.spellingConflicts.set(lower, conflict);
      entry.list.push(conflict);
    }
  }

  return result;
}

/**
 * checkValueConflict(category, mainText, s) answers whether DECLARING this
 * Value would conflict, WITHOUT mutating state, so a step 3 declaration can be
 * refused at the point it is typed rather than only at the run that follows
 * it. It runs valueConflicts against a hypothetical state carrying the
 * candidate, with its category forced active: a category the user has never
 * touched reads as off (categoryActive), and an off category can never
 * conflict by design, which would silently let a genuinely ambiguous
 * declaration through the very check meant to catch it.
 *
 * @returns {object[]} the candidate's conflicts (possibly empty)
 */
export function checkValueConflict(category, mainText, s = state) {
  const text = (mainText ?? "").trim();
  if (!text) return [];
  const hypothetical = {
    ...s,
    values: [...s.values, { category, mainText: text }],
    settings: { ...s.settings, categories: { ...s.settings.categories, [category]: true } },
  };
  return valueConflicts(hypothetical).get(valueKey(category, text))?.list ?? [];
}

// --- Reassignment helpers --------------------------------

/**
 * valueAutocomplete(query, s) filters Value main texts for the
 * reassignment popover: case-insensitive, prefix matches rank before
 * substring matches, each entry {category, mainText, label}.
 */
export function valueAutocomplete(query, s = state) {
  const q = (query ?? "").trim().toLowerCase();
  if (!q) return [];
  const prefix = [];
  const substring = [];
  for (const e of s.values) {
    const lower = e.mainText.toLowerCase();
    const item = { category: e.category, mainText: e.mainText };
    if (lower.startsWith(q)) prefix.push(item);
    else if (lower.includes(q)) substring.push(item);
  }
  return [...prefix, ...substring];
}

/**
 * reassignOriginal(original, toCategory, toMainText) makes `original`
 * a manual spelling of the target entity. If `original` currently exists
 * as an entity of its own (it earned its own placeholder), that entity
 * is removed so exactly one entity matches the text after the fast
 * re-run. Returns false when the target does not exist.
 */
export function reassignOriginal(original, toCategory, toMainText) {
  const text = (original ?? "").trim();
  if (!text) return false;
  const target = state.values.find((e) =>
    valueKey(e.category, e.mainText) === valueKey(toCategory, toMainText));
  if (!target) return false;
  if (valueKey(toCategory, toMainText) === valueKey(toCategory, text)) return false;

  // Drop a same-named standalone entity (any category) before rerouting.
  const standalone = state.values.find((e) => e.mainText.toLowerCase() === text.toLowerCase());
  if (standalone) deleteValue(standalone.category, standalone.mainText);

  addSpelling(toCategory, toMainText, text);
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
 * visibleValidationWarnings(s) is the run's OVERLAP warnings minus the
 * dismissed ones: the same warning list `Report.Warnings` carries, but the
 * one the engine computes on every run (a declared Value losing text to a
 * built-in pattern) and that nothing was rendering. `Validation.Warnings` and
 * `Report.Warnings` keep their distinct meanings (blocking aborts, warnings
 * inform); this reads both rather than copying one into the other, so a later
 * reader cannot show the same warning twice.
 *
 * Each entry is a Conflict object (`{message, ...}`); dismissal keys on the
 * MESSAGE text, exactly like visibleWarnings, so dismissWarning needs no
 * change to work for this second source.
 * @param {object} [s] state
 * @returns {string[]}
 */
export function visibleValidationWarnings(s = state) {
  const dismissed = new Set(s.dismissedWarnings ?? []);
  return (s.results?.validation?.warnings ?? [])
    .map((c) => c.message)
    .filter((m) => m && !dismissed.has(m));
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
 *             mapping, the pending values, the dismissed warnings), the Values
 *             and Suggestions, the custom patterns, and the per-document
 *             metadata review decisions. All of it is about the batch that just
 *             finished.
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
    values: state.values.length,
  };
  setState({
    step: "import",
    documents: [],
    previewDoc: null,
    importErrors: [],
    sourceCache: {},
    values: [],
    suggestions: [],
    intersections: [],
    patterns: [],
    discovery: null,
    lastAIScan: null,
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

/** buildRunRequest() assembles the Go RunRequest from the current state, the
 *  single place the pipeline payload is shaped. It takes no arguments: a
 *  leftover boolean from an older caller (e.g. buildRunRequest(false)) is
 *  simply ignored, so retired call sites stay harmless. */
export function buildRunRequest() {
  return {
    values: acceptedValues(state),
    allowTerms: state.allowlist,
    patterns: validPatterns(state),
    // The granular selection travels with every run request so the Go
    // pipeline always sees exactly what the configure screen shows.
    categories: state.settings.categories ?? presetCategories(state.settings.level),
    // The "Native detection" master switch, inverted: when Native detection is
    // off, the Go pipeline skips pass 1 so no regex signal category is replaced.
    suppressRegexPII: !getState().settings.useBuiltInPatterns,
  };
}

/**
 * buildIntersectionRequest() assembles the CheckIntersections payload.
 *
 * It is built from the SAME state buildRunRequest reads, minus the fields a
 * detection does not need. Any drift between the two would make the check answer
 * a different question from the run, and then the warning on a card describes
 * something that will not happen.
 */
export function buildIntersectionRequest() {
  return {
    values: acceptedValues(state),
    allowTerms: state.allowlist,
    patterns: validPatterns(state),
    categories: state.settings.categories ?? presetCategories(state.settings.level),
    suppressRegexPII: !state.settings.useBuiltInPatterns,
  };
}

// state.test.js — dev-time unit tests for the store, runnable with
// `node --test "frontend/**/*.test.js"` (node is present on CI runners; this is NOT an npm
// dependency.
//
// Only pure logic is tested here: state transitions, navigation guards and
// reducers. Views stay logic-free precisely so this file covers what
// matters without a DOM.

import test from "node:test";
import assert from "node:assert/strict";

import {
  COUNTRIES, DEFAULT_COUNTRY, countryIDCategories, COUNTRY_ID_CATEGORIES,
  categoryAppliesTo,
} from "./countries.js";

import {
  getState, setState, resetState, subscribe,
  WIZARD_STEPS, canGoTo, goTo, nextStep,
  goToScreen,
  applyPreset, toggleCategory, activePreset, activePresets, defaultCategories,
  depthSelection, presetScopeCategories, presetsFor, presetFamilies, presetKey,
  findPreset, PRESETS, PRESET_SCOPES, PRESET_SCOPE_PATTERNS, PRESET_SCOPE_NAMES,
  PRESET_FAMILY_DEPTH,
  adoptCategories, ALWAYS_ON_CATEGORIES,
  setUseLocalLLM, adoptProbe,
  detectionRoutesOn, llmEnabled,
  setUseBuiltInPatterns, setUseHeuristicDiscovery,
  addSuggestions, acceptSuggestion, rejectSuggestion, acceptAllShown,
  DISCOVERY_METHODS, MATCH_CLASSES, SIGNAL_SOURCES, SIGNAL_DERIVATIONS,
  CONFLICT_RESOLUTIONS,
  LLM_DETAIL_LEVELS,
  signalSourceOn, enabledSignalSources, setSignalSource,
  signalDerivationOn, enabledSignalDerivations, setSignalDerivation,
  moveSpelling, valueAutocomplete, reassignOriginal,
  applyImportResult,
  IMAGE_TREATMENTS, IMAGE_FORMATS, IMAGE_KINDS, IMAGE_STATUS_FILTERS,
  imageThumbKey, imageDocName, imagesFor, imageAssets, imageStatus,
  imageStatusCounts, filteredImages, treatmentAvailable, treatmentBlockedReason,
  startImageScan, setImageInventory, setImageScanError, cacheImageThumb,
  applyImageDecision, openImageEditor, updateImageEditor, closeImageEditor,
  setAnonymiseTab, setImageFilter, setImageLayout,
  setLLMScope, llmScopeArg, parsePageSpec,
  setNotice, clearNotice, NOTICE_TONES,
  setDocumentCountry,
  setValueTables,
  dismissWarning, visibleWarnings,
  setExportDir, startNewBatch, setMetaReview,
  askConfirm, answerConfirm, askChoice, answerChoice,
  valueKey,
  renameValue, renameSpelling, changeValueCategory, changeSuggestionCategory,
  groupValues, clearAllValues, valueConflicts, spellingsOf, curate,
  setIntersections, intersectionsFor, foldIntoFamily,
} from "./state.js";


// --- Preset helpers for the tests below ----------------------------------
//
// activePreset returns the PRESETS ROW (or null for Custom). The tests read the
// ID, so this names the two lookups once rather than repeating the ?. dance.
const patternsRow = () =>
  activePreset(getState(), PRESET_SCOPE_PATTERNS, PRESET_FAMILY_DEPTH)?.id ?? "custom";
const namesRow = () =>
  activePreset(getState(), PRESET_SCOPE_NAMES, PRESET_FAMILY_DEPTH)?.id ?? "custom";
// applyDepth presses the SAME depth chip in both rows, which is what the old
// single-level preset did in one click and what most of these tests want as a
// starting point. It is two calls on purpose: there is no reducer that writes
// both scopes, because that is the cross-section reach the scoped model removes.
const applyDepth = (id) => {
  applyPreset(PRESET_SCOPE_PATTERNS, PRESET_FAMILY_DEPTH, id);
  applyPreset(PRESET_SCOPE_NAMES, PRESET_FAMILY_DEPTH, id);
};

test("setState merges and notifies subscribers", () => {
  resetState();
  let seen = null;
  const unsub = subscribe((s) => { seen = s.step; });
  setState({ step: "identify" });
  assert.equal(seen, "identify");
  unsub();
});

test("guards: no step beyond import without documents", () => {
  resetState();
  assert.equal(canGoTo("import"), true);
  for (const step of WIZARD_STEPS.slice(1)) {
    assert.equal(canGoTo(step), false, `${step} must be locked without documents`);
  }
  assert.equal(goTo("identify"), false);
  assert.equal(getState().step, "import");
});

test("guards: documents unlock middle steps, results unlock export", () => {
  resetState();
  setState({ documents: [{ name: "a.txt" }] });
  assert.equal(canGoTo("identify"), true);
  assert.equal(canGoTo("anonymise"), true);
  assert.equal(canGoTo("export"), false, "export needs results");
  setState({ results: { documents: [] } });
  assert.equal(canGoTo("export"), true);
});

test("guards: unknown step is rejected", () => {
  resetState();
  assert.equal(canGoTo("teleport"), false);
});

test("nextStep walks the wizard forward and respects the guards", () => {
  // There is no prevStep() any more: moving BACK goes through
  // nav.js goBack(), which asks the reset question first. A reducer that moved
  // back without asking was a way around that rule.
  resetState();
  assert.equal(nextStep(), false, "cannot advance with no documents");
  setState({ documents: [{ name: "a.txt" }] });
  assert.equal(nextStep(), true);
  assert.equal(getState().step, "identify");
  assert.equal(nextStep(), true);
  assert.equal(getState().step, "anonymise");
  assert.equal(nextStep(), false, "export needs results");
  assert.equal(getState().step, "anonymise");
});

test("applyImportResult updates documents, errors and preview selection", () => {
  resetState();
  applyImportResult({
    documents: [{ name: "a.txt" }, { name: "b.csv" }],
    errors: ["bad file"],
  });
  let s = getState();
  assert.equal(s.documents.length, 2);
  assert.deepEqual(s.importErrors, ["bad file"]);
  assert.equal(s.previewDoc, "a.txt", "first document auto-selected");

  // Removing the selected document moves the selection to the first
  // remaining one; keeping it leaves the selection alone.
  setState({ previewDoc: "b.csv" });
  applyImportResult({ documents: [{ name: "b.csv" }] });
  assert.equal(getState().previewDoc, "b.csv");
  applyImportResult({ documents: [{ name: "c.md" }] });
  assert.equal(getState().previewDoc, "c.md");
});

// --- local-model scan scope ----------------------------------------------

test("parsePageSpec parses numbers, ranges and a mix into a sorted set", () => {
  assert.deepEqual(parsePageSpec("12-15,18", 20),
    { pages: [12, 13, 14, 15, 18], error: null });
  assert.deepEqual(parsePageSpec("1-3,7", 20).pages, [1, 2, 3, 7]);
  // De-duplicated and sorted, whatever order the tokens arrive in.
  assert.deepEqual(parsePageSpec("3, 1, 2, 2", 20).pages, [1, 2, 3]);
  // A reversed range still reads low..high.
  assert.deepEqual(parsePageSpec("5-3", 20).pages, [3, 4, 5]);
});

test("parsePageSpec drops out-of-range tokens and reports a bad one", () => {
  // Out-of-range indices are silently ignored, the valid ones survive.
  assert.deepEqual(parsePageSpec("2,99,4", 10).pages, [2, 4]);
  assert.deepEqual(parsePageSpec("0,1", 10).pages, [1]);
  // A malformed token names the first offender but keeps the valid pages.
  const bad = parsePageSpec("2,abc,4", 10);
  assert.deepEqual(bad.pages, [2, 4]);
  assert.equal(bad.error, "abc");
  // Empty or whitespace-only is not an error.
  assert.deepEqual(parsePageSpec("", 10), { pages: [], error: null });
  assert.deepEqual(parsePageSpec("   ", 10), { pages: [], error: null });
});

test("setLLMScope stores the document and mode, defaulting to whole", () => {
  resetState();
  setState({ documents: [{ name: "a.pdf", unit: "page", pageCount: 6 }] });

  // Choosing a document defaults to scanning it whole.
  assert.deepEqual(setLLMScope({ docName: "a.pdf" }),
    { docName: "a.pdf", mode: "all", pages: "" });

  // Switching to a page set stores the raw spec verbatim.
  assert.deepEqual(setLLMScope({ mode: "pages", pages: "2-4" }),
    { docName: "a.pdf", mode: "pages", pages: "2-4" });
});

test("setLLMScope resets to all documents for an unknown or empty name", () => {
  resetState();
  setState({ documents: [{ name: "a.pdf", unit: "page", pageCount: 6 }] });
  setLLMScope({ docName: "a.pdf", mode: "pages", pages: "2-4" });

  assert.deepEqual(setLLMScope({ docName: "gone.pdf" }),
    { docName: "", mode: "all", pages: "" }, "a name not in the list clears the scope");
  assert.equal(llmScopeArg(), null, "and nothing is sent to Go");
});

test("llmScopeArg emits {docName, pages} and null for every document", () => {
  resetState();
  assert.equal(llmScopeArg(), null, "the default is every document, whole");
  setState({ documents: [{ name: "a.pdf", unit: "page", pageCount: 6 }] });

  // Whole selected document: pages is an empty array.
  setLLMScope({ docName: "a.pdf", mode: "all" });
  assert.deepEqual(llmScopeArg(), { docName: "a.pdf", pages: [] });

  // A page set parses against the selected document's unit count.
  setLLMScope({ mode: "pages", pages: "1-3,5" });
  assert.deepEqual(llmScopeArg(), { docName: "a.pdf", pages: [1, 2, 3, 5] });

  // Out-of-range units are dropped at send time.
  setLLMScope({ pages: "5,99" });
  assert.deepEqual(llmScopeArg(), { docName: "a.pdf", pages: [5] });
});

test("a new import drops a scope whose document is gone", () => {
  resetState();
  setState({ documents: [{ name: "a.pdf", unit: "page", pageCount: 6 }] });
  setLLMScope({ docName: "a.pdf", mode: "pages", pages: "3-5" });

  // Re-importing without that document clears the scope.
  applyImportResult({ documents: [{ name: "b.pdf", unit: "page", pageCount: 2 }] });
  assert.deepEqual(getState().llmScope, { docName: "", mode: "all", pages: "" });

  // A document that merely shrank keeps its scope: the spec re-parses at send
  // time, so out-of-range units are dropped then, not stored now.
  setLLMScope({ docName: "b.pdf", mode: "pages", pages: "1-2" });
  applyImportResult({ documents: [{ name: "b.pdf", unit: "page", pageCount: 4 }] });
  assert.deepEqual(getState().llmScope, { docName: "b.pdf", mode: "pages", pages: "1-2" });
});

// --- Value review reducers -----------------------------------------------
import {
  addValues, deleteValue, deleteValues,
  setValueSpellings, addSpelling, acceptedValues,
  addAllowTerm, removeAllowTerm, clearAllowlist, addPattern, removePattern, validPatterns,
} from "./state.js";

test("addValues dedupes case-insensitively and defaults to accepted", () => {
  resetState();
  const added = addValues([
    { category: "entity_names", mainText: "Alpine Trust" },
    { category: "entity_names", mainText: "ALPINE TRUST" }, // dup
    { category: "entity_names", mainText: "  " },           // blank
    { category: "person_names", mainText: "Marie Duval" },
  ]);
  assert.equal(added, 2);
  const s = getState();
  assert.equal(s.values.length, 2);
  assert.equal(s.values[0].status, "accepted");
});

test("adding a value enables its category and flips its row to custom", () => {
  // The reported bug: person values accepted from heuristic discovery under the
  // Soft preset (person_names off) were listed as "ready to replace" and then
  // dropped by the pipeline's category filter. Acceptance must switch the
  // category on, exactly as ticking the box would, so the value survives.
  resetState();
  applyDepth("soft");
  assert.equal(getState().settings.categories.person_names, false, "soft leaves person names off");
  addValues([{ category: "person_names", mainText: "Oscar Liber" }]);
  assert.equal(getState().settings.categories.person_names, true, "the value's category is now on");
  assert.equal(namesRow(), "custom");
  assert.equal(patternsRow(), "soft",
    "a NAME category moved, so the patterns row is untouched: the rows derive independently");
});

test("adding a value already on a preset does not flip the preset", () => {
  // person_names is on at Standard, so accepting one must not read as a manual
  // divergence: the chip should stay on the named preset.
  resetState();
  applyDepth("standard");
  addValues([{ category: "person_names", mainText: "Oscar Liber" }]);
  assert.equal(namesRow(), "standard");
});

test("acceptedValues filters on status, as the belt to a removed brace", () => {
  // setEntityStatus() is gone: the redesign has no denied
  // state, so nothing can produce one. The filter stays, because a row that
  // somehow arrived denied must still not reach the pipeline, and asserting it
  // directly is the only way to keep that guard honest now that no reducer
  // exercises it.
  resetState();
  addValues([{ category: "entity_names", mainText: "Alpine Trust" }]);
  assert.equal(getState().values[0].status, "accepted", "every new value is accepted");
  assert.equal(acceptedValues().length, 1);

  setState({ values: getState().values.map((e) => ({ ...e, status: "denied" })) });
  assert.equal(acceptedValues().length, 0, "a denied Value never reaches the pipeline");
});

test("manual derivedSpellings dedupe and clear the expansion cache", () => {
  resetState();
  addValues([{ category: "person_names", mainText: "Peter Stone", derivedSpellings: ["Peter Stone"] }]);
  addSpelling("person_names", "Peter Stone", "Pete");
  addSpelling("person_names", "Peter Stone", "pete"); // dup, other case
  const e = getState().values[0];
  assert.deepEqual(e.spellings, ["Pete"]);
  assert.equal(e.derivedSpellings, null, "cache back to pending (null) so Go re-expands");
  setValueSpellings("person_names", "Peter Stone", ["Peter Stone", "Pete"]);
  assert.equal(getState().values[0].derivedSpellings.length, 2);
});

test("deleteValue deletes the row", () => {
  resetState();
  addValues([{ category: "entity_names", mainText: "Alpine" }]);
  deleteValue("entity_names", "ALPINE");
  assert.equal(getState().values.length, 0);
});

test("deleteValues removes exactly the named rows, in one state change", () => {
  resetState();
  addValues([
    { category: "entity_names", mainText: "Alpine" },
    { category: "entity_names", mainText: "Meridian" },
    { category: "person_names", mainText: "Marie Duval" },
  ]);

  // One notification for the whole batch: a bulk clear that repainted per row
  // would re-wire the cards the next removal is about to delete.
  let paints = 0;
  const stop = subscribe(() => { paints += 1; });
  try {
    const removed = deleteValues([
      valueKey("entity_names", "Alpine"),
      valueKey("person_names", "Marie Duval"),
    ]);
    assert.equal(removed, 2, "it reports what it actually removed");
    assert.equal(paints, 1, `one state change for the batch, got ${paints}`);
  } finally {
    stop();
  }
  assert.deepEqual(getState().values.map((v) => v.mainText), ["Meridian"]);
});

test("deleteValues ignores a key for a value that is not there, and reports zero", () => {
  resetState();
  addValues([{ category: "entity_names", mainText: "Alpine" }]);
  assert.equal(deleteValues([]), 0, "nothing named, nothing removed");
  assert.equal(deleteValues([valueKey("person_names", "Alpine")]), 0,
    "the same text under another type is a different value");
  assert.deepEqual(getState().values.map((v) => v.mainText), ["Alpine"]);
});

test("allowlist add/remove is case-insensitive on identity", () => {
  resetState();
  addAllowTerm("CSSF");
  addAllowTerm("cssf"); // dup
  assert.deepEqual(getState().allowlist, ["CSSF"]);
  removeAllowTerm("Cssf");
  assert.deepEqual(getState().allowlist, []);
});

test("the allowlist starts empty: nothing is seeded", () => {
  // The engine does not seed defaults (App.allowlistFor is empty) and neither
  // does the frontend anymore, so a fresh state protects nothing until the user
  // adds a term. A test here so a re-introduced seed is caught, not shipped.
  resetState();
  assert.deepEqual(getState().allowlist, []);
});

test("clearAllowlist empties the list and returns the count cleared", () => {
  resetState();
  addAllowTerm("CSSF");
  addAllowTerm("ACME");
  assert.equal(clearAllowlist(), 2);
  assert.deepEqual(getState().allowlist, []);
  // Clearing an already-empty list removes nothing and says so.
  assert.equal(clearAllowlist(), 0);
});

test("patterns: only compile-clean ones feed the pipeline", () => {
  resetState();
  addPattern("PRJ-[0-9]+", null);
  addPattern("[", "does not compile");
  assert.equal(getState().patterns.length, 2);
  assert.deepEqual(validPatterns(), [{ expr: "PRJ-[0-9]+" }]);
  removePattern("[");
  assert.equal(getState().patterns.length, 1);
});

// --- the run request -----------------------------------------------------

import { buildRunRequest } from "./state.js";

test("buildRunRequest assembles only pipeline-ready inputs", () => {
  resetState();
  addValues([
    { category: "entity_names", mainText: "Alpine" },
    { category: "person_names", mainText: "Denied Person" },
  ]);
  // No reducer can deny a value any more, so the state is set
  // directly: the point of the test is that buildRunRequest FILTERS on status.
  setState({
    values: getState().values.map((e) =>
      e.mainText === "Denied Person" ? { ...e, status: "denied" } : e),
  });
  addAllowTerm("CSSF");
  addPattern("PRJ-[0-9]+", null);
  addPattern("[", "broken");

  const req = buildRunRequest();
  assert.deepEqual(req.values, [{
    category: "entity_names", mainText: "Alpine", spellings: [],
    spellingPolicy: "automatic", discoveryMethods: ["manual"], evidence: [],
  }]);
  assert.deepEqual(req.allowTerms, ["CSSF"]);
  assert.deepEqual(req.patterns, [{ expr: "PRJ-[0-9]+" }]);
  // No literal replacement facility exists, so nothing in the request can
  // rewrite text outside the Value model.
  assert.equal(req.simpleRules, undefined);
});

// --- Screen navigation ---------------------------------------------------

test("goToScreen switches screens and rejects unknown names", () => {
  resetState();
  assert.equal(getState().screen, "home");
  assert.equal(goToScreen("wizard"), true);
  assert.equal(getState().screen, "wizard");
  assert.equal(goToScreen("home"), true);
  assert.equal(getState().screen, "home");
  assert.equal(goToScreen("settings"), false);
  assert.equal(getState().screen, "home");
});

test("wizard state survives navigating to home and back", () => {
  resetState();
  setState({ documents: [{ name: "a.txt" }] });
  goToScreen("wizard");
  goTo("identify");
  goToScreen("home");
  goToScreen("wizard");
  assert.equal(getState().step, "identify");
  assert.equal(getState().documents.length, 1);
});

test("the import divider state is gone, not merely unused (decision 6)", async () => {
  // A reducer nothing calls is a reducer someone reinstates. The mock-up uses a
  // fixed two-column grid, so the ratio has no meaning any more.
  const state = await import("./state.js");
  assert.equal(state.setImportSplit, undefined,
    "setImportSplit must be deleted, not left exported");
  assert.ok(!("importSplit" in getState()),
    "importSplit must be gone from the state shape too");
});

test("applyPreset fills the expected switches per depth", () => {
  // The depths are ordered by how much ordinary text each risks catching, and
  // this walks all three in both scopes. It mirrors the engine's preset table;
  // the mirror itself is enforced by ../preset_parity_test.go.
  resetState();
  applyDepth("soft");
  let c = getState().settings.categories;
  assert.equal(c.email, true);
  assert.equal(c.identifier_names, true, "reference codes are near-PII, so soft has them");
  assert.equal(c.person_names, false, "soft leaves persons off");
  assert.equal(c.product_names, false);
  assert.equal(c.amount, false);

  applyDepth("standard");
  c = getState().settings.categories;
  assert.equal(c.person_names, true);
  assert.equal(c.product_names, true, "products and brands join at Standard");
  assert.equal(c.brand_names, true);
  assert.equal(c.other_names, false, "the noisiest category waits for Thorough");
  assert.equal(c.date, false, "Standard leaves dates off");

  applyDepth("thorough");
  c = getState().settings.categories;
  assert.equal(c.date, true);
  assert.equal(c.amount, true);
  assert.equal(c.other_names, true);
});

test("a patterns chip cannot move a name category, and the reverse", () => {
  // THE point of the scoped model, as a wiring test rather than by inspection: a
  // chip in one rail section changing a checkbox in another is invisible from the
  // rail, so nothing on screen would report it.
  resetState();
  applyDepth("thorough");

  // Pressing Soft under Built-in patterns drops the pattern categories Thorough
  // added and leaves every name category exactly where Thorough put it.
  const before = { ...getState().settings.categories };
  applyPreset(PRESET_SCOPE_PATTERNS, PRESET_FAMILY_DEPTH, "soft");
  let after = getState().settings.categories;
  assert.equal(after.amount, false, "the patterns chip did write its own scope");
  for (const key of presetScopeCategories(PRESET_SCOPE_NAMES)) {
    assert.equal(after[key], before[key], `${key} is a name category and must not move`);
  }
  assert.equal(namesRow(), "thorough", "the names row still reads Thorough");

  // And back the other way.
  const patternsBefore = { ...getState().settings.categories };
  applyPreset(PRESET_SCOPE_NAMES, PRESET_FAMILY_DEPTH, "soft");
  after = getState().settings.categories;
  assert.equal(after.other_names, false, "the names chip did write its own scope");
  for (const key of presetScopeCategories(PRESET_SCOPE_PATTERNS)) {
    assert.equal(after[key], patternsBefore[key],
      `${key} is a pattern category and must not move`);
  }
});

test("applyPreset refuses a row or a preset the table does not hold", () => {
  // Applied as an empty set, an unknown preset would clear the whole scope in
  // silence, which is a worse outcome than the chip doing nothing.
  resetState();
  applyDepth("standard");
  const before = { ...getState().settings.categories };
  assert.equal(applyPreset("pattern", PRESET_FAMILY_DEPTH, "soft"), false, "unknown scope");
  assert.equal(applyPreset(PRESET_SCOPE_PATTERNS, "regulatory", "gdpr"), false, "unknown family");
  assert.equal(applyPreset(PRESET_SCOPE_PATTERNS, PRESET_FAMILY_DEPTH, "paranoid"), false,
    "unknown preset");
  assert.deepEqual(getState().settings.categories, before, "nothing was written");
});

test("Soft and Standard are identical in the patterns scope, and the row reads Soft", () => {
  // A fact about the depths, not a bug: Standard differs from Soft only in the
  // name categories. The FIRST match in table order wins, so the row settles on
  // Soft instead of flickering between two chips that both match.
  const soft = findPreset(PRESET_SCOPE_PATTERNS, PRESET_FAMILY_DEPTH, "soft");
  const standard = findPreset(PRESET_SCOPE_PATTERNS, PRESET_FAMILY_DEPTH, "standard");
  assert.deepEqual([...soft.categories].sort(), [...standard.categories].sort());

  resetState();
  for (const id of ["soft", "standard"]) {
    applyDepth(id);
    assert.equal(patternsRow(), "soft", `${id}: the patterns row reads Soft`);
    assert.equal(namesRow(), id, `${id}: the names row still tells the two apart`);
  }
});

test("no preset touches a category with no switch", () => {
  // custom_patterns has no control in the rail, so a preset must neither set nor
  // clear it: a stored false would be a pattern editor whose patterns never run.
  for (const preset of PRESETS) {
    for (const key of ALWAYS_ON_CATEGORIES) {
      assert.ok(!preset.categories.includes(key),
        `${preset.scope}/${preset.id} names the always-on category ${key}`);
      assert.ok(!presetScopeCategories(preset.scope).includes(key),
        `scope ${preset.scope} owns the always-on category ${key}`);
    }
  }
});

test("toggleCategory flips one switch and flags its row as custom", () => {
  resetState();
  applyDepth("standard");
  assert.equal(namesRow(), "standard");
  assert.equal(toggleCategory("email", false), true);
  const c = getState().settings.categories;
  assert.equal(c.email, false);
  assert.equal(c.phone, true, "other switches untouched");
  assert.equal(patternsRow(), "custom");
  assert.equal(namesRow(), "standard", "a PATTERN category moved, so the names row is untouched");
  // Unknown keys are rejected.
  assert.equal(toggleCategory("no_such_category", true), false);
});

test("activePresets leaves out the row that reads Custom", () => {
  // Absence is how Custom is representable on the wire: a map that had to name
  // every row would force the rail to invent a preset for a selection that
  // matches none.
  resetState();
  applyDepth("standard");
  assert.deepEqual(activePresets(getState()), {
    "patterns.depth": "soft", // Soft and Standard are identical here
    "names.depth": "standard",
  });
  toggleCategory("email", false);
  assert.deepEqual(activePresets(getState()), { "names.depth": "standard" });
});

// --- custom_patterns: no switch, therefore never off ---------------------

test("a category with no switch cannot be turned off through toggleCategory", () => {
  // The rail renders no checkbox for custom_patterns, so a stored false could
  // only ever come from a caller out of step with the interface, and it would be
  // invisible from the screen: a pattern editor whose patterns never run.
  resetState();
  for (const key of ALWAYS_ON_CATEGORIES) {
    assert.equal(toggleCategory(key, false), false, `${key} must refuse to be cleared`);
    assert.equal(getState().settings.categories[key], true, `${key} must still be on`);
    // Turning it on is a no-op rather than an error: it is already on.
    assert.equal(toggleCategory(key, true), true);
    assert.equal(getState().settings.categories[key], true);
  }
});

test("every preset leaves the switch-less categories on", () => {
  resetState();
  for (const level of ["soft", "medium", "advanced"]) {
    applyPreset(level);
    for (const key of ALWAYS_ON_CATEGORIES) {
      assert.equal(getState().settings.categories[key], true,
        `${key} must be on after applyPreset("${level}")`);
    }
  }
});

test("adoptCategories forces the switch-less categories on, whatever arrived", () => {
  // A file written while custom_patterns still had a switch can say false. The
  // normaliser is why adopting it cannot leave the user with a control-less
  // category switched off and nothing on screen explaining it.
  const adopted = adoptCategories({ custom_patterns: false, email: false });
  for (const key of ALWAYS_ON_CATEGORIES) assert.equal(adopted[key], true);
  assert.equal(adopted.email, false, "every other key is carried through unchanged");
  // A fresh map, never the argument: the caller's object is not mutated.
  const input = { custom_patterns: false };
  assert.notEqual(adoptCategories(input), input);
  assert.equal(input.custom_patterns, false);
});

test("setDocumentCountry leaves the switch-less categories on", () => {
  // It rewrites the category map wholesale to match the country's identifiers,
  // which is exactly the kind of adoption the normaliser exists for.
  resetState();
  setDocumentCountry("DE");
  for (const key of ALWAYS_ON_CATEGORIES) {
    assert.equal(getState().settings.categories[key], true);
  }
});

test("activePreset recognises each exact preset, per row", () => {
  resetState();
  for (const id of ["soft", "standard", "thorough"]) {
    applyDepth(id);
    assert.equal(namesRow(), id);
  }
  // The patterns row, where Soft and Standard are the same set, reads Soft for
  // both and Thorough for the third.
  applyDepth("thorough");
  assert.equal(patternsRow(), "thorough");
});

test("buildRunRequest carries the category selection", () => {
  resetState();
  applyDepth("soft");
  const req = buildRunRequest();
  // The Soft depth in both scopes, scoped to the document country, which is what
  // the rail actually shows and therefore what the pipeline must obey.
  assert.deepEqual(req.categories, depthSelection("soft", DEFAULT_COUNTRY));
  toggleCategory("iban", false);
  assert.equal(buildRunRequest().categories.iban, false);
});

test("setUseBuiltInPatterns and setUseHeuristicDiscovery flip their flags independently", () => {
  resetState();
  assert.equal(getState().settings.useBuiltInPatterns, true, "Native detection defaults on");
  assert.equal(getState().settings.useHeuristicDiscovery, true, "Auto detection defaults on");
  setUseBuiltInPatterns(false);
  assert.equal(getState().settings.useBuiltInPatterns, false);
  assert.equal(getState().settings.useHeuristicDiscovery, true, "Auto is untouched by Native");
  setUseHeuristicDiscovery(false);
  assert.equal(getState().settings.useHeuristicDiscovery, false);
  setUseBuiltInPatterns(true);
  assert.equal(getState().settings.useBuiltInPatterns, true);
});

test("buildRunRequest carries suppressRegexPII as the inverse of useBuiltInPatterns", () => {
  resetState();
  assert.equal(buildRunRequest().suppressRegexPII, false, "Native on means do not suppress");
  setUseBuiltInPatterns(false);
  assert.equal(buildRunRequest().suppressRegexPII, true, "Native off suppresses the regex pass");
});

// --- Local-model gating --------------------------------------------------

test("llmEnabled requires BOTH the toggle and a reachable Ollama", () => {
  resetState();
  setState({ ollama: { available: true, models: [], detail: "" } });
  setUseLocalLLM(false);
  assert.equal(llmEnabled(), false, "the route off blocks the model even with Ollama up");
  setUseLocalLLM(true);
  assert.equal(llmEnabled(), true);
  setState({ ollama: { available: false, models: [], detail: "" } });
  assert.equal(llmEnabled(), false, "Ollama down blocks the model even with the route on");
});

test("adoptProbe adopts the model the probe resolved", () => {
  // The dropdown has to show the model that will actually RUN. Go resolves an
  // uninstalled name to an installed one, so a store that kept the asked-for
  // name would name one model while the run posted to another.
  resetState();
  adoptProbe({ available: true, models: ["a:1", "b:2"], detail: "ok", model: "b:2" });
  assert.equal(getState().settings.model, "b:2");
  assert.equal(getState().ollama.available, true);
  assert.deepEqual(getState().ollama.models, ["a:1", "b:2"]);
});

test("adoptProbe never flips the Local LLM discovery route on", () => {
  // Detecting Ollama ENABLES the switch, it does not press it: handing a
  // document to a model is the user's decision.
  resetState();
  adoptProbe({ available: true, models: ["a:1"], detail: "ok", model: "a:1" });
  assert.equal(getState().settings.useLocalLLM, false);
  assert.equal(llmEnabled(), false);
});

test("adoptProbe leaves the stored model alone when nothing was resolved", () => {
  // A stopped server says nothing about which models exist, so it must not
  // erase a choice. Same for a reachable server with no models installed.
  resetState();
  adoptProbe({ available: true, models: ["a:1"], detail: "ok", model: "a:1" });
  adoptProbe({ available: false, models: [], detail: "not detected", model: "" });
  assert.equal(getState().settings.model, "a:1", "a failed probe must not clear the model");
  assert.equal(getState().ollama.available, false, "the status itself still lands");
});

test("the two offline routes start on and Local LLM discovery starts off", () => {
  // Built-in patterns and Heuristic discovery need nothing installed, so both run
  // by default. Local LLM discovery hands the document to a model, so the user
  // turns it on themselves, even when Ollama is detected.
  resetState();
  assert.equal(getState().settings.useBuiltInPatterns, true);
  assert.equal(getState().settings.useHeuristicDiscovery, true);
  assert.equal(getState().settings.useLocalLLM, false);
  setState({ ollama: { available: true, models: [], detail: "" } });
  assert.equal(getState().settings.useLocalLLM, false,
    "detecting Ollama must not switch the route on");
});

test("the local model's reply format starts on the fast end", () => {
  // The schema finds a little more on a short dense page and on very small
  // documents, and on a slide-heavy one it costs about twice the time for no more
  // values, while on a small model it finds nothing at all. So the default is off,
  // and it is a real boolean in the store rather than an absent key: the rail draws
  // a checkbox from it and an undefined would render as unchecked by accident
  // rather than by decision.
  resetState();
  assert.equal(getState().settings.llmStrictFormat, false,
    "asking the model for every category is the slow option, so it is opt-in");
  assert.ok("llmStrictFormat" in getState().settings,
    "the setting must exist in the store, not be implied by its absence");
});

test("the local model's detail level starts on the thorough end", () => {
  // Thorough is the end that FINDS things: the faster level trades recall for
  // time, and a trade nobody asked for must not be the one a fresh session makes
  // on the user's behalf. It is a real string in the store rather than an absent
  // key, because the rail marks a dropdown option from it and an undefined would
  // mark nothing, which is how the browser ends up choosing.
  resetState();
  assert.equal(getState().settings.llmDetailLevel, "thorough",
    "the slower, more thorough level is the default");
  assert.ok("llmDetailLevel" in getState().settings,
    "the setting must exist in the store, not be implied by its absence");
});

test("LLM_DETAIL_LEVELS is exactly the two identifiers Go validates", () => {
  // Go refuses a level it cannot size, so a third entry here would be an option
  // the user can pick and the engine then rejects. The list is frozen for the
  // same reason the other mirrored vocabularies are.
  assert.deepEqual(LLM_DETAIL_LEVELS, ["thorough", "faster"]);
  assert.ok(LLM_DETAIL_LEVELS.includes(getState().settings.llmDetailLevel),
    "the default has to be one of the levels the list offers");
});

test("there is no derived section flag: each route is its own stored boolean", () => {
  // A section switch must be the flag it claims to be. A fourth boolean summarising
  // the others can disagree with them, and a section claiming to be on while
  // nothing it names runs is a control that lies about what a run will do.
  resetState();
  for (const dead of ["useSmartDetect", "smartDetection", "useSmartDetection"]) {
    assert.ok(!(dead in getState().settings), `${dead} must not be a persisted flag`);
  }
  for (const key of ["useBuiltInPatterns", "useHeuristicDiscovery", "useLocalLLM"]) {
    assert.equal(typeof getState().settings[key], "boolean", `${key} must be stored`);
  }
});

test("each route reducer writes exactly one flag and leaves the others alone", () => {
  resetState();
  setUseBuiltInPatterns(false);
  assert.equal(getState().settings.useBuiltInPatterns, false);
  assert.equal(getState().settings.useHeuristicDiscovery, true, "untouched");
  assert.deepEqual(enabledSignalSources(getState()), SIGNAL_SOURCES,
    "switching the pattern pass off must not clear a signal reading: the readings " +
    "match their own evidence, and that asymmetry is why the setting is separate");

  resetState();
  setUseHeuristicDiscovery(false);
  assert.equal(getState().settings.useHeuristicDiscovery, false);
  assert.equal(getState().settings.useBuiltInPatterns, true, "untouched");
});

test("detectionRoutesOn counts the DISCOVERY routes that are enabled", () => {
  resetState();
  assert.equal(detectionRoutesOn(), 1, "the offline discovery phase alone");
  setState({ ollama: { available: true, models: [], detail: "" } });
  setUseLocalLLM(true);
  assert.equal(detectionRoutesOn(), 2);
  setUseHeuristicDiscovery(false);
  for (const source of SIGNAL_SOURCES) setSignalSource(source, false);
  assert.equal(detectionRoutesOn(), 1, "local LLM discovery alone");
  setUseLocalLLM(false);
  assert.equal(detectionRoutesOn(), 0, "nothing to run, and the UI must say so");
});

test("built-in pattern matching alone gives the detect button nothing to run", () => {
  // It produces direct matches at anonymisation time, not Suggestions, so it is
  // not a discovery route and must not be counted as one.
  resetState();
  setUseHeuristicDiscovery(false);
  for (const source of SIGNAL_SOURCES) setSignalSource(source, false);
  setUseBuiltInPatterns(true);
  assert.equal(detectionRoutesOn(), 0);
});

// --- Suggestion review gate -----------------------------------------------

test("suggestions wait for explicit accept; accept moves them to values", () => {
  resetState();
  const added = addSuggestions([
    { discoveryMethods: ["heuristic"], mainText: "Alpine Trust", category: "entity_names", count: 3 },
    { discoveryMethods: ["heuristic"], mainText: "Marie Duval", category: "person_names" },
  ]);
  assert.equal(added, 2);
  assert.equal(getState().values.length, 0, "nothing reaches values without accept");

  assert.equal(acceptSuggestion("Alpine Trust"), true);
  assert.equal(getState().values.length, 1);
  assert.equal(getState().values[0].category, "entity_names");
  assert.equal(getState().suggestions.length, 1, "accepted suggestion leaves the review list");
});

test("reject removes, duplicates and existing values are skipped", () => {
  resetState();
  addValues([{ category: "entity_names", mainText: "Known Corp" }]);
  const added = addSuggestions([
    { discoveryMethods: ["local_llm"], mainText: "Known Corp", category: "entity_names" }, // already an entity
    { discoveryMethods: ["local_llm"], mainText: "Fresh Co", category: "entity_names" },
    { discoveryMethods: ["local_llm"], mainText: "fresh co", category: "entity_names" },   // case-insensitive dup
  ]);
  assert.equal(added, 1);
  rejectSuggestion("Fresh Co");
  assert.equal(getState().suggestions.length, 0);
  assert.equal(getState().values.length, 1, "reject never touches values");
});

// --- Spelling regrouping --------------------------------------------------

test("moveSpelling happy path: source curates without it, target gains it", () => {
  resetState();
  addValues([
    { category: "person_names", mainText: "Jean Muller" },
    { category: "person_names", mainText: "J Muller Sr" },
  ]);
  setValueSpellings("person_names", "Jean Muller", ["Jean Muller", "J. Muller"]);
  setValueSpellings("person_names", "J Muller Sr", ["J Muller Sr"]);

  assert.equal(moveSpelling("person_names", "Jean Muller", "person_names", "J Muller Sr", "J. Muller"), "",
    "the family convention is \"\" on success, so a caller cannot read success as an error");
  const from = getState().values.find((e) => e.mainText === "Jean Muller");
  const to = getState().values.find((e) => e.mainText === "J Muller Sr");
  // The source is CURATED without the moved spelling: an automatic expansion
  // would derive "J. Muller" again and both values would claim it.
  assert.equal(from.spellingPolicy, "curated");
  assert.deepEqual(from.spellings, ["Jean Muller"],
    "the source keeps its remaining spellings as its own list");
  assert.deepEqual([...spellingsOf(from).keys()], ["jean muller"]);
  assert.deepEqual(to.spellings, ["J. Muller"]);
  assert.deepEqual(from.derivedSpellings, [], "a curated source has nothing left to expand");
  assert.equal(to.derivedSpellings, null, "target re-expands");
});

test("moveSpelling rejects self-drops, unknown rows and absent derivedSpellings", () => {
  resetState();
  addValues([
    { category: "person_names", mainText: "Jean Muller" },
    { category: "entity_names", mainText: "Alpine" },
  ]);
  setValueSpellings("person_names", "Jean Muller", ["Jean Muller"]);
  setValueSpellings("entity_names", "Alpine", ["Alpine"]);

  assert.equal(moveSpelling("person_names", "Jean Muller", "person_names", "Jean Muller", "Jean Muller"), "self", "self-drop");
  assert.equal(moveSpelling("person_names", "Ghost", "entity_names", "Alpine", "x"), "not found", "unknown source");
  assert.equal(moveSpelling("person_names", "Jean Muller", "entity_names", "Alpine", "Not A Spelling"), "absent", "absent spelling");
  // No state damage from rejected moves.
  assert.equal(getState().values.find((e) => e.mainText === "Alpine").spellings.length, 0);
});

test("moveSpelling across categories touches only the two rows involved", () => {
  resetState();
  addValues([
    { category: "person_names", mainText: "Jean Muller" },
    { category: "entity_names", mainText: "Alpine" },
    { category: "entity_names", mainText: "Borealis" },
  ]);
  setValueSpellings("person_names", "Jean Muller", ["Jean Muller", "Muller"]);
  setValueSpellings("entity_names", "Alpine", ["Alpine"]);
  setValueSpellings("entity_names", "Borealis", ["Borealis"]);

  assert.equal(moveSpelling("person_names", "Jean Muller", "entity_names", "Alpine", "Muller"), "");
  const untouched = getState().values.find((e) => e.mainText === "Borealis");
  assert.deepEqual(untouched.derivedSpellings, ["Borealis"], "third row untouched");
  assert.equal(untouched.spellingPolicy, "automatic");
  // Only the target re-expands. The source is curated, so its list is settled
  // and re-deriving it would put the moved spelling straight back.
  const pendingNames = getState().values.filter((e) => e.derivedSpellings === null).map((e) => e.mainText).sort();
  assert.deepEqual(pendingNames, ["Alpine"]);
  const source = getState().values.find((e) => e.mainText === "Jean Muller");
  assert.equal(source.spellingPolicy, "curated");
  assert.ok(!source.spellings.some((x) => x.toLowerCase() === "muller"));
});

// --- Reassignment helpers ------------------------------------------------

test("valueAutocomplete ranks prefix matches before substring matches", () => {
  resetState();
  addValues([
    { category: "person_names", mainText: "Jean Muller" },
    { category: "person_names", mainText: "Muller Freres" },
    { category: "entity_names", mainText: "Amullertech" },
  ]);
  const got = valueAutocomplete("muller");
  assert.equal(got.length, 3);
  assert.equal(got[0].mainText, "Muller Freres", "prefix match first");
  assert.ok(got.slice(1).map((m) => m.mainText).includes("Jean Muller"));
  assert.deepEqual(valueAutocomplete(""), []);
});

test("reassignOriginal removes a standalone Value and adds the spelling", () => {
  resetState();
  addValues([
    { category: "person_names", mainText: "Jean Muller" },
    { category: "person_names", mainText: "J. Muller" }, // earned its own placeholder
  ]);
  assert.equal(reassignOriginal("J. Muller", "person_names", "Jean Muller"), true);
  const values = getState().values;
  assert.equal(values.length, 1, "the standalone Value is folded in");
  assert.deepEqual(values[0].spellings, ["J. Muller"]);
  assert.equal(values[0].derivedSpellings, null, "target re-expands");
  // Unknown target rejected, state untouched.
  assert.equal(reassignOriginal("X", "person_names", "Ghost"), false);
});

// --- The four-step wizard ------------------------------------------------

import { knownStep } from "./state.js";

test("the wizard has exactly four steps, in order", () => {
  assert.deepEqual(WIZARD_STEPS, ["import", "identify", "anonymise", "export"]);
});

test("knownStep passes current tokens through untouched", () => {
  for (const step of WIZARD_STEPS) {
    assert.equal(knownStep(step), step);
  }
});

test("an unknown persisted step token lands on import", () => {
  // there is no migration table any more. A session file
  // this build does not understand is refused by the loader, so the only
  // tokens that reach here are corrupted or hand-edited ones, and the answer
  // is the one step that is always reachable.
  for (const bad of ["configure", "values", "run", "values", "teleport", "", undefined, null]) {
    assert.equal(knownStep(bad), "import", `knownStep(${JSON.stringify(bad)})`);
  }
});

// --- Documentation is no longer a screen ---------------------------------

import { SCREENS } from "./state.js";

test("SCREENS no longer contains docs", () => {
  assert.deepEqual(SCREENS, ["home", "wizard"]);
});

test("goToScreen(\"docs\") is rejected and leaves the screen untouched", () => {
  resetState();
  assert.equal(goToScreen("wizard"), true);
  assert.equal(goToScreen("docs"), false, "docs is not a screen any more");
  assert.equal(getState().screen, "wizard");
});

test("a failed documentation open is recorded as a dismissible shell error", () => {
  resetState();
  assert.equal(getState().shellError, null);
  setState({ shellError: "The documentation window could not be opened." });
  assert.match(getState().shellError, /could not be opened/);
  // Wizard state is untouched by a chrome-level error.
  assert.equal(getState().step, "import");
  setState({ shellError: null });
  assert.equal(getState().shellError, null);
});

// --- surfaced recognizers, groups, confidence ----------------------------

import {
  EXTENDED_PII_CATEGORIES, ALL_CATEGORIES,
  setCategoryGroup, setMinConfidence,
} from "./state.js";
import { CATEGORY_LABELS } from "./copy.js";

test("the eight BUILD-03 recognizers are known to the store (CR9)", () => {
  const expected = [
    "credit_card", "uk_nhs", "ip_address", "mac_address",
    "crypto", "database_uri", "de_steuer_id", "es_nif",
  ];
  assert.deepEqual(EXTENDED_PII_CATEGORIES, expected);
  for (const key of expected) {
    assert.ok(ALL_CATEGORIES.includes(key), `${key} missing from ALL_CATEGORIES`);
  }
});

test("every category the store knows has a label and an example (CR9)", () => {
  for (const key of ALL_CATEGORIES) {
    const entry = CATEGORY_LABELS[key];
    assert.ok(entry, `no CATEGORY_LABELS entry for ${key}`);
    const [label, example] = entry;
    assert.ok(label && label.length > 2, `${key} has no readable label`);
    assert.ok(example && example.length > 5, `${key} has no example`);
  }
});

test("the extended recognizers are named by every depth preset", () => {
  // The preset TABLE, not a selection: three of them are national identifiers, so
  // the selection a document gets has them scoped to its country. What must hold
  // at every depth is that the preset NAMES them. The Go side is pinned in
  // category_parity_test.go, and the two must not drift.
  for (const preset of presetsFor(PRESET_SCOPE_PATTERNS, PRESET_FAMILY_DEPTH)) {
    for (const key of EXTENDED_PII_CATEGORIES) {
      assert.ok(preset.categories.includes(key), `${key} must be named by ${preset.id}`);
    }
  }
});

test("adding the new categories did not change what a depth switches ON", () => {
  // Regression guard for the depth semantics themselves: Soft must still exclude
  // person names, dates, amounts, organisations and places. Luxembourg, because
  // the national identifiers follow the document country and Thorough switching
  // "everything" on can only mean everything that applies here.
  const soft = depthSelection("soft", "LU");
  assert.equal(soft.person_names, false);
  const standard = depthSelection("standard", "LU");
  assert.equal(standard.person_names, true);
  assert.equal(standard.date, false);
  assert.equal(standard.amount, false);
  const thorough = depthSelection("thorough", "LU");
  for (const key of ALL_CATEGORIES) {
    if (COUNTRY_ID_CATEGORIES.includes(key) && !categoryAppliesTo(key, "LU")) {
      assert.equal(thorough[key], false, `${key} does not apply in Luxembourg`);
      continue;
    }
    assert.equal(thorough[key], true, `thorough must switch ${key} on`);
  }
});

test("setCategoryGroup flips exactly the given keys in one change (CR10)", () => {
  resetState();
  let notifications = 0;
  const unsub = subscribe(() => { notifications++; });

  // The three national identifiers among the extended recognizers follow the
  // document country, so on a fresh Luxembourg session they are already off:
  // the group to flip is the ones that apply here.
  const group = EXTENDED_PII_CATEGORIES.filter((key) =>
    categoryAppliesTo(key, DEFAULT_COUNTRY));
  const changed = setCategoryGroup(group, false);
  assert.equal(changed, group.length, "every one that applies started on and must flip");
  assert.equal(notifications, 1, "a whole group must cost exactly one re-render");

  const s = getState();
  for (const key of group) assert.equal(s.settings.categories[key], false);
  // Nothing outside the group moved.
  assert.equal(s.settings.categories.email, true);
  assert.equal(s.settings.categories.person_names, true);
  unsub();
});

test("setCategoryGroup ignores unknown keys and reports no-op runs (CR10)", () => {
  resetState();
  assert.equal(setCategoryGroup(["not_a_category"], true), 0);
  assert.equal(setCategoryGroup(["email"], true), 0, "email is already on");
  assert.equal(setCategoryGroup(["email"], false), 1);
  assert.equal(setCategoryGroup([], true), 0);
  assert.equal(setCategoryGroup(undefined, true), 0);
});

test("a deselected group makes its row Custom (CR10)", () => {
  resetState();
  applyDepth("standard");
  assert.equal(patternsRow(), "soft");
  setCategoryGroup(EXTENDED_PII_CATEGORIES, false);
  assert.equal(patternsRow(), "custom");
  assert.equal(namesRow(), "standard", "the names row is untouched by a pattern group");
});

test("minConfidence defaults to 0 and round-trips through the setter (CR9)", () => {
  resetState();
  assert.equal(getState().settings.minConfidence, 0,
    "the default must keep every detection");
  assert.equal(setMinConfidence(0.9), 0.9);
  assert.equal(getState().settings.minConfidence, 0.9);
  assert.equal(setMinConfidence(0), 0);
  assert.equal(setMinConfidence(1), 1);
});

test("minConfidence rejects values outside 0 to 1 (CR9)", () => {
  resetState();
  for (const bad of [-0.1, 1.1, NaN, "0.5", null, undefined]) {
    assert.equal(setMinConfidence(bad), null, `${bad} must be rejected`);
  }
  assert.equal(getState().settings.minConfidence, 0, "a rejected value changes nothing");
});

// --- smart-detection tuning and bulk deny --------------------------------

import {
  setHeuristicDiscoveryOptions, heuristicDiscoveryOptions,
  HEURISTIC_DISCOVERY_DEFAULTS,
} from "./state.js";

/** seedSuggestions() puts a mixed review list in the store. */
function seedSuggestions() {
  resetState();
  addSuggestions([
    { discoveryMethods: ["heuristic"], mainText: "Marie Duval", category: "person_names", count: 7 },
    { discoveryMethods: ["heuristic"], mainText: "Anouk Berger", category: "person_names", count: 3 },
    { discoveryMethods: ["heuristic"], mainText: "Alpine Trust", category: "entity_names", count: 3 },
  ]);
}

test("heuristic discovery ships with the stricter defaults (CR13)", () => {
  resetState();
  assert.deepEqual(getState().settings.heuristicDiscovery, HEURISTIC_DISCOVERY_DEFAULTS);
  assert.equal(HEURISTIC_DISCOVERY_DEFAULTS.excludeCommonWords, true);
  assert.ok(HEURISTIC_DISCOVERY_DEFAULTS.minLength > 0);
  // Requiring two occurrences would throw away single-sighting full
  // names, which are the most valuable thing heuristic discovery finds.
  assert.equal(HEURISTIC_DISCOVERY_DEFAULTS.minOccurrences, 1);
});

test("setHeuristicDiscoveryOptions merges a partial patch (CR13)", () => {
  resetState();
  const out = setHeuristicDiscoveryOptions({ minLength: 6 });
  assert.equal(out.minLength, 6);
  assert.equal(out.excludeCommonWords, HEURISTIC_DISCOVERY_DEFAULTS.excludeCommonWords,
    "untouched options keep their value");
  assert.equal(getState().settings.heuristicDiscovery.minLength, 6);
});

test("setHeuristicDiscoveryOptions accepts the permissive extreme (CR13)", () => {
  // Turning every filter off must be reachable: that is the escape hatch
  // for a user who would rather review too much than miss something.
  resetState();
  const out = setHeuristicDiscoveryOptions({
    minLength: 0, minOccurrences: 0, excludeCommonWords: false, minConfidence: 0,
    strictness: "lenient",
  });
  assert.deepEqual(out, {
    minLength: 0, minOccurrences: 0, excludeCommonWords: false, minConfidence: 0,
    strictness: "lenient",
  });
});

test("setHeuristicDiscoveryOptions accepts every strictness level and ignores junk", () => {
  resetState();
  for (const level of ["lenient", "balanced", "strict"]) {
    assert.equal(setHeuristicDiscoveryOptions({ strictness: level }).strictness, level);
  }
  // A value outside the known set is ignored, like an out-of-range number.
  const before = getState().settings.heuristicDiscovery.strictness;
  setHeuristicDiscoveryOptions({ strictness: "aggressive" });
  assert.equal(getState().settings.heuristicDiscovery.strictness, before,
    "an unknown strictness must not be stored");
});

test("setHeuristicDiscoveryOptions ignores invalid values rather than storing them (CR13)", () => {
  resetState();
  const before = { ...getState().settings.heuristicDiscovery };
  const out = setHeuristicDiscoveryOptions({
    minLength: -1, minOccurrences: 2.5, excludeCommonWords: "yes", minConfidence: 4,
  });
  assert.deepEqual(out, before, "every invalid value must be ignored");
  // Unknown keys are simply not carried over.
  setHeuristicDiscoveryOptions({ nonsense: 1 });
  assert.equal(getState().settings.heuristicDiscovery.nonsense, undefined);
});

test("heuristicDiscoveryOptions fills defaults for a session without the block", () => {
  // A session file that says nothing about the tuning has no heuristicDiscovery block.
  resetState();
  setState({ settings: { ...getState().settings, heuristicDiscovery: undefined } });
  assert.deepEqual(heuristicDiscoveryOptions(), HEURISTIC_DISCOVERY_DEFAULTS);
  // A partially written block is completed rather than rejected.
  setState({ settings: { ...getState().settings, heuristicDiscovery: { minLength: 9 } } });
  assert.deepEqual(heuristicDiscoveryOptions(), { ...HEURISTIC_DISCOVERY_DEFAULTS, minLength: 9 });
});

// --- per-step reset ------------------------------------------------------

import { resetStep, isBackward, STEP_RESETS } from "./state.js";

/** fullSession() fills every step's state so a reset is visible. */
function fullSession() {
  resetState();
  setState({
    documents: [{ name: "a.txt" }, { name: "b.csv" }],
    previewDoc: "a.txt",
    importErrors: ["something failed"],
    allowlist: ["CSSF"],
    results: { documents: [{ name: "a.txt" }] },
    mapping: { "[PERSON_1]": { original: "Marie Duval" } },
    metaReview: { "a.txt": { ext: "docx" } },
    running: true,
    progress: { stage: "deterministic" },
    discovery: { running: true },
  });
  addValues([{ category: "person_names", mainText: "Marie Duval" }]);
  addSuggestions([{ discoveryMethods: ["heuristic"], mainText: "Alpine Trust", category: "entity_names" }]);
  addPattern("PRJ-[0-9]+", null);
  setMinConfidence(0.9);
  setCategoryGroup(["email"], false);
}

test("isBackward only reports moves toward the start of the wizard", () => {
  assert.equal(isBackward("anonymise", "identify"), true);
  assert.equal(isBackward("export", "import"), true);
  assert.equal(isBackward("identify", "anonymise"), false);
  assert.equal(isBackward("anonymise", "anonymise"), false);
  assert.equal(isBackward("anonymise", "nowhere"), false, "an unknown step is not a backward move");
});

test("resetStep rejects an unknown step instead of silently doing nothing", () => {
  resetState();
  assert.equal(resetStep("teleport"), false);
  // The retired tokens are unknown steps now, not aliases for the new ones.
  assert.equal(resetStep("configure"), false);
  assert.equal(resetStep("values"), false);
  assert.equal(resetStep("run"), false);
  assert.equal(resetStep("identify"), true);
});

test("every wizard step has a reset entry (CR16)", () => {
  for (const step of WIZARD_STEPS) {
    assert.ok(STEP_RESETS[step], `no reset defined for ${step}`);
  }
});

test("NO reset ever clears the imported documents (CR16)", () => {
  // The one non-negotiable rule of.
  for (const step of WIZARD_STEPS) {
    fullSession();
    resetStep(step);
    assert.equal(getState().documents.length, 2, `${step} reset dropped the documents`);
    assert.equal(getState().previewDoc, "a.txt", `${step} reset lost the preview selection`);
  }
});

test("NO reset ever clears the allowlist (CR16)", () => {
  // It is shared by two steps and curated across the session, so it
  // belongs to neither.
  for (const step of WIZARD_STEPS) {
    fullSession();
    resetStep(step);
    assert.deepEqual(getState().allowlist, ["CSSF"], `${step} reset dropped the allowlist`);
  }
});

test("resetStep(identify) restores the preset and the detection defaults", () => {
  // Identify owns BOTH halves of its screen now: the rail's
  // choices, which used to be a Configure step, and the workspace's values.
  fullSession();
  assert.equal(getState().settings.categories.email, false);
  assert.equal(getState().settings.minConfidence, 0.9);

  assert.equal(resetStep("identify"), true);
  const s = getState();
  // The documented default selection, scoped to the DEFAULT COUNTRY. Every depth
  // preset names all four country-specific identifiers, because to the engine
  // they are hard PII, so the reset has to scope them or the rail would show
  // Luxembourg beside an active German tax identifier.
  assert.deepEqual(s.settings.categories, defaultCategories(DEFAULT_COUNTRY));
  assert.equal(s.documentCountry, DEFAULT_COUNTRY);
  assert.equal(s.settings.minConfidence, 0);
  assert.deepEqual(s.settings.heuristicDiscovery, HEURISTIC_DISCOVERY_DEFAULTS);
});

test("resetStep(identify) clears values, suggestions, patterns and discovery", () => {
  fullSession();
  assert.equal(resetStep("identify"), true);
  const s = getState();
  assert.deepEqual(s.values, []);
  assert.deepEqual(s.suggestions, []);
  assert.deepEqual(s.patterns, []);
  assert.equal(s.discovery, null);
  // The run is untouched: it belongs to the next step.
  assert.ok(s.results);
});

test("resetStep(identify) keeps the machine's connection settings", () => {
  // Port, model and the route switch describe the machine, not this batch of
  // documents, so stepping back must not make the user reconfigure Ollama.
  fullSession();
  setState({ settings: { ...getState().settings, ollamaPort: 12345, model: "custom:7b", useLocalLLM: true } });
  resetStep("identify");
  const s = getState();
  assert.equal(s.settings.ollamaPort, 12345);
  assert.equal(s.settings.model, "custom:7b");
  assert.equal(s.settings.useLocalLLM, true);
});

test("resetStep(anonymise) clears the run and its re-identification mapping", () => {
  fullSession();
  assert.equal(resetStep("anonymise"), true);
  const s = getState();
  assert.equal(s.running, false);
  assert.equal(s.progress, null);
  assert.equal(s.results, null);
  assert.equal(s.mapping, null, "the mapping is part of the run, and is the key");
  // The values that produced the run survive, so re-running is one click.
  assert.equal(s.values.length, 1);
});

test("resetStep(anonymise) clears the editing surfaces the run screen owns", () => {
  // The dismissed warnings only exist once there is a result to edit, so they
  // are the Anonymise step's own data.
  fullSession();
  setState({ dismissedWarnings: ["skipped-pdf"] });
  resetStep("anonymise");
  assert.deepEqual(getState().dismissedWarnings, []);
});

test("resetStep(anonymise) re-locks the export step, which needs results", () => {
  fullSession();
  assert.equal(canGoTo("export"), true);
  resetStep("anonymise");
  assert.equal(canGoTo("export"), false);
});

test("resetStep(export) clears only the metadata review decisions", () => {
  fullSession();
  assert.equal(resetStep("export"), true);
  const s = getState();
  assert.deepEqual(s.metaReview, {});
  assert.ok(s.results, "the results belong to the Anonymise step, not Export");
});

test("resetStep(import) clears the error strip but nothing else", () => {
  fullSession();
  assert.equal(resetStep("import"), true);
  const s = getState();
  assert.deepEqual(s.importErrors, []);
  assert.equal(s.documents.length, 2);
});

// --- resetStepsForBackward -----------------------------------------------
//
// The multi-step backward reset. A single-step move must behave exactly like
// the old resetStep(current); a multi-step move must ALSO clear the steps it
// jumps over, which is the leak fix: jumping from Anonymise back to Import used
// to leave the Identify step's detected values on the "clean" Import screen.

import { resetStepsForBackward } from "./state.js";

test("resetStepsForBackward on a single-step back clears only that step", () => {
  fullSession();
  const reset = resetStepsForBackward("anonymise", "identify");
  assert.deepEqual(reset, ["anonymise"]);
  const s = getState();
  assert.equal(s.results, null, "the Anonymise step's run was cleared");
  assert.equal(s.values.length, 1, "the Identify step it lands on keeps its values");
});

test("resetStepsForBackward on a multi-step back clears every step left behind", () => {
  // Anonymise back to Import jumps over Identify. Its detected values, its
  // suggestions and its patterns are the legacy the user reported seeing on a
  // screen they thought was clean.
  fullSession();
  const reset = resetStepsForBackward("anonymise", "import");
  assert.deepEqual(reset, ["identify", "anonymise"]);
  const s = getState();
  assert.deepEqual(s.values, [], "the Identify values are gone");
  assert.deepEqual(s.suggestions, [], "the suggestions are gone");
  assert.deepEqual(s.patterns, [], "the custom patterns are gone");
  assert.equal(s.results, null, "the run is gone");
  assert.equal(s.documents.length, 2, "but the imported documents survive, as always");
});

test("resetStepsForBackward lands the target's own data untouched", () => {
  // Export back to Anonymise resets only Export; the run the user steps back to
  // review must still be there.
  fullSession();
  setState({ running: false });
  const reset = resetStepsForBackward("export", "anonymise");
  assert.deepEqual(reset, ["export"]);
  assert.ok(getState().results, "the Anonymise results the user stepped back to are kept");
  assert.deepEqual(getState().metaReview, {}, "only the Export step's own data was cleared");
});

test("resetStepsForBackward is a no-op for a forward or same-step request", () => {
  fullSession();
  assert.deepEqual(resetStepsForBackward("identify", "anonymise"), [], "forward resets nothing");
  assert.deepEqual(resetStepsForBackward("anonymise", "anonymise"), [], "same step resets nothing");
  assert.deepEqual(resetStepsForBackward("anonymise", "teleport"), [], "an unknown target resets nothing");
});

// --- Notices -------------------------------------------------------------

test("setNotice stores the sentence and its tone", () => {
  resetState();
  assert.equal(getState().notice, null);
  const notice = setNotice("Saved report.md to the destination folder.", "ok");
  assert.deepEqual(notice, { text: "Saved report.md to the destination folder.", tone: "ok" });
  assert.deepEqual(getState().notice, notice);
});

test("setNotice defaults to info and degrades an unknown tone to info", () => {
  resetState();
  assert.equal(setNotice("a fact").tone, "info");
  // A typo in the tone must not swallow the sentence: the user still needs to
  // read it.
  const bad = setNotice("still shown", "chartreuse");
  assert.equal(bad.tone, "info");
  assert.equal(bad.text, "still shown");
});

test("setNotice keeps only the newest notice", () => {
  resetState();
  setNotice("first", "ok");
  setNotice("second", "warn");
  assert.deepEqual(getState().notice, { text: "second", tone: "warn" });
});

test("setNotice trims, and empty text clears rather than showing a blank strip", () => {
  resetState();
  assert.deepEqual(setNotice("  padded  "), { text: "padded", tone: "info" });
  assert.equal(setNotice("   "), null);
  assert.equal(getState().notice, null);
  setNotice("something");
  assert.equal(setNotice(""), null);
  assert.equal(getState().notice, null);
});

test("clearNotice dismisses the strip and is a no-op when it is already empty", () => {
  resetState();
  let paints = 0;
  const off = subscribe(() => paints++);
  clearNotice();
  assert.equal(paints, 0, "clearing nothing must not repaint");
  setNotice("x");
  clearNotice();
  assert.equal(getState().notice, null);
  off();
});

test("NOTICE_TONES is exactly the three tones brand.css defines", () => {
  assert.deepEqual([...NOTICE_TONES].sort(), ["info", "ok", "warn"]);
});

// --- The in-app confirm --------------------------------------------------

test("askConfirm stores the question with its defaults filled in", () => {
  resetState();
  const answer = askConfirm({ title: "Clear the step", body: "Your documents are kept." });
  const q = getState().confirm;
  assert.equal(q.title, "Clear the step");
  assert.equal(q.body, "Your documents are kept.");
  assert.equal(q.confirmLabel, "Continue");
  assert.equal(q.cancelLabel, "Cancel");
  assert.equal(q.keyBearing, false);
  answerConfirm(false);
  return answer; // settle it so the test does not leave a pending promise
});

test("askConfirm resolves true on confirm and clears the question", async () => {
  resetState();
  const pending = askConfirm({ title: "t", body: "b", confirmLabel: "Export CSV", keyBearing: true });
  assert.equal(getState().confirm.confirmLabel, "Export CSV");
  assert.equal(getState().confirm.keyBearing, true);
  assert.equal(answerConfirm(true), true, "there was a question to answer");
  assert.equal(await pending, true);
  assert.equal(getState().confirm, null);
});

test("askConfirm resolves false on cancel", async () => {
  resetState();
  const pending = askConfirm({ title: "t", body: "b" });
  answerConfirm(false);
  assert.equal(await pending, false);
  assert.equal(getState().confirm, null);
});

test("answerConfirm reports that there was nothing to answer", () => {
  resetState();
  assert.equal(answerConfirm(true), false);
  assert.equal(getState().confirm, null);
});

test("a second question answers the first one no rather than stranding it", async () => {
  resetState();
  const first = askConfirm({ title: "first", body: "b" });
  const second = askConfirm({ title: "second", body: "b" });
  // The new question is the one on screen...
  assert.equal(getState().confirm.title, "second");
  // ...and the old promise settled false instead of hanging forever.
  assert.equal(await first, false);
  answerConfirm(true);
  assert.equal(await second, true);
});

test("resetState answers a pending question no instead of leaving it hanging", async () => {
  resetState();
  const pending = askConfirm({ title: "t", body: "b" });
  resetState();
  assert.equal(await pending, false);
  assert.equal(getState().confirm, null);
});

test("the notice and the question are cleared by resetState", () => {
  resetState();
  setNotice("x", "ok");
  resetState();
  assert.equal(getState().notice, null);
  assert.equal(getState().confirm, null);
});

// --- The in-app pick-one (askChoice) -------------------------------------

test("askChoice stores the choices and resolves to the picked id", async () => {
  resetState();
  const pending = askChoice({
    title: "Choose the main value", body: "Pick one.",
    choices: [{ id: "a", label: "Alpha" }, { id: "b", label: "Beta" }],
  });
  const q = getState().confirm;
  assert.deepEqual(q.choices, [{ id: "a", label: "Alpha" }, { id: "b", label: "Beta" }]);
  assert.equal(answerChoice("b"), true, "there was a question to answer");
  assert.equal(await pending, "b");
  assert.equal(getState().confirm, null);
});

test("askChoice resolves null when cancelled through answerConfirm(false)", async () => {
  resetState();
  const pending = askChoice({ title: "t", body: "b", choices: [{ id: "a", label: "A" }] });
  // The backdrop and Escape route through answerConfirm(false); for a choice
  // that means "cancelled", which is null rather than the yes/no false.
  answerConfirm(false);
  assert.equal(await pending, null);
  assert.equal(getState().confirm, null);
});

test("resetState settles a pending choice with null", async () => {
  resetState();
  const pending = askChoice({ title: "t", body: "b", choices: [{ id: "a", label: "A" }] });
  resetState();
  assert.equal(await pending, null);
  assert.equal(getState().confirm, null);
});

test("answerChoice with no id resolves null so a malformed button invents nothing", async () => {
  resetState();
  const pending = askChoice({ title: "t", body: "b", choices: [{ id: "a", label: "A" }] });
  answerChoice(undefined);
  assert.equal(await pending, null);
});

// --- The document country ------------------------------------------------

test("setDocumentCountry records the country and switches its identifiers", () => {
  resetState();
  assert.equal(getState().documentCountry, DEFAULT_COUNTRY);

  assert.equal(setDocumentCountry("DE"), "DE");
  let s = getState();
  assert.equal(s.documentCountry, "DE");
  assert.equal(s.settings.categories.de_steuer_id, true);
  assert.equal(s.settings.categories.es_nif, false);
  assert.equal(s.settings.categories.uk_nhs, false);
});

test("switching country turns the previous country's identifier OFF", () => {
  // The whole point of the control: Germany then France must not leave the
  // German tax identifier active beside nothing.
  resetState();
  setDocumentCountry("DE");
  assert.equal(getState().settings.categories.de_steuer_id, true);
  setDocumentCountry("FR");
  const s = getState();
  assert.equal(s.documentCountry, "FR");
  for (const key of COUNTRY_ID_CATEGORIES) {
    assert.equal(s.settings.categories[key], false, `${key} must be off for France`);
  }
});

test("setDocumentCountry touches NOTHING but the three identifiers", () => {
  resetState();
  const before = { ...getState().settings.categories };
  setDocumentCountry("UK");
  const after = getState().settings.categories;
  for (const key of Object.keys(before)) {
    if (COUNTRY_ID_CATEGORIES.includes(key)) continue;
    assert.equal(after[key], before[key],
      `${key} changed, but the country only decides the three national identifiers`);
  }
});

test("setDocumentCountry rejects an unknown code instead of storing it", () => {
  // Storing a code the table does not know would leave the selector showing one
  // country and the switches set for another.
  resetState();
  setDocumentCountry("ES");
  const snapshot = { ...getState().settings.categories };
  for (const bad of ["ZZ", "", undefined, null, "de"]) {
    assert.equal(setDocumentCountry(bad), null, JSON.stringify(bad));
    assert.equal(getState().documentCountry, "ES", "the stored country must not change");
    assert.deepEqual(getState().settings.categories, snapshot);
  }
});

test("setDocumentCountry repaints exactly once", () => {
  // Both halves of the change land in ONE setState, so the country and its
  // switches can never be observed disagreeing, and a long category list is not
  // re-rendered four times.
  resetState();
  let paints = 0;
  const off = subscribe(() => paints++);
  setDocumentCountry("DE");
  assert.equal(paints, 1);
  off();
});

test("a preset does not overrule the country's identifier choice", () => {
  // Every depth preset names all four national identifiers, because to the
  // engine they are hard PII. The country is an ORTHOGONAL choice, so applyPreset
  // scopes them: picking Soft on a German document must not silently start
  // looking for Spanish tax numbers.
  resetState();
  setDocumentCountry("DE");
  for (const level of ["soft", "standard", "thorough"]) {
    applyDepth(level);
    const categories = getState().settings.categories;
    assert.equal(categories.de_steuer_id, true, `${level}: Germany's identifier stays on`);
    assert.equal(categories.es_nif, false, `${level}: Spain's must not come back`);
    assert.equal(categories.uk_nhs, false, `${level}: the UK's must not come back`);
  }
});

test("the preset chip does not read Custom just because of the country", () => {
  // The four country-driven identifiers are masked on BOTH sides of the preset
  // comparison. Otherwise a Luxembourg document, where three of them are off,
  // would show "Custom" the instant the user picked Standard, which makes the
  // chips look broken.
  resetState();
  for (const code of COUNTRIES.map((c) => c.code)) {
    setDocumentCountry(code);
    for (const id of ["soft", "standard", "thorough"]) {
      applyDepth(id);
      assert.equal(namesRow(), id, `${code} + ${id} must still read as ${id}`);
      assert.notEqual(patternsRow(), "custom",
        `${code} + ${id}: the patterns row must name a preset, not Custom`);
    }
  }
});

test("a real category change still reads as Custom", () => {
  // The mask must not swallow genuine divergence.
  resetState();
  applyDepth("standard");
  toggleCategory("email", false);
  assert.equal(patternsRow(), "custom");
});

// --- The Anonymise screen's editing surfaces -----------------------------

test("dismissWarning hides one warning and visibleWarnings drops it", () => {
  resetState();
  setState({ results: { report: { warnings: ["pdf skipped", "extras in headers"] } } });
  assert.deepEqual(visibleWarnings(), ["pdf skipped", "extras in headers"]);
  assert.equal(dismissWarning("pdf skipped"), true);
  assert.deepEqual(visibleWarnings(), ["extras in headers"]);
  assert.deepEqual(getState().dismissedWarnings, ["pdf skipped"]);
});

test("dismissWarning is keyed by TEXT, so order changes cannot mis-hide one", () => {
  // Go sends warnings as plain strings with no identifier. An index would hide
  // the wrong warning the moment a run produced them in a different order.
  resetState();
  setState({ results: { report: { warnings: ["a", "b"] } } });
  dismissWarning("b");
  setState({ results: { report: { warnings: ["b", "a"] } } });
  assert.deepEqual(visibleWarnings(), ["a"], "the same warning stays hidden after a reorder");
});

test("dismissWarning ignores an empty id and refuses to double-dismiss", () => {
  resetState();
  setState({ results: { report: { warnings: ["a"] } } });
  assert.equal(dismissWarning(""), false);
  assert.equal(dismissWarning(undefined), false);
  assert.equal(dismissWarning("a"), true);
  assert.equal(dismissWarning("a"), false, "already dismissed");
  assert.deepEqual(getState().dismissedWarnings, ["a"]);
});

test("a new run re-raises every warning", () => {
  // resetStep("anonymise") empties the dismissed list, because a warning about
  // the run you are looking at NOW has to be visible.
  resetState();
  setState({
    documents: [{ name: "a.txt" }],
    results: { report: { warnings: ["pdf skipped"] } },
  });
  dismissWarning("pdf skipped");
  assert.deepEqual(visibleWarnings(), []);
  resetStep("anonymise");
  setState({ results: { report: { warnings: ["pdf skipped"] } } });
  assert.deepEqual(visibleWarnings(), ["pdf skipped"]);
});

test("visibleWarnings copes with no run and no report", () => {
  resetState();
  assert.deepEqual(visibleWarnings(), []);
  setState({ results: {} });
  assert.deepEqual(visibleWarnings(), []);
});

// --- startNewBatch -------------------------------------------------------
//
// The SPLIT is the whole point of this button, so both halves are pinned: what
// it clears, and just as importantly what it keeps. Getting the second half
// wrong would make the button useless (the user re-enters their settings every
// batch) or unsafe (the previous batch's key stays in memory).

/** finishedBatch() is the state at the end of a completed batch. */
function finishedBatch() {
  resetState();
  setState({
    step: "export",
    documents: [{ name: "a.docx" }, { name: "b.pptx" }],
    previewDoc: "a.docx",
    importErrors: ["a stale error"],
    results: { documents: [{ name: "a.docx" }], report: { warnings: ["w"] } },
    mapping: { "[PERSON_1]": { original: "Marie Duval", category: "person_names" } },
    resultDoc: "a.docx",
    metaReview: { "a.docx": { ext: "docx", filename: "a_anon.docx", fields: [] } },
    exportDir: "C:\\Users\\o\\anonymised",
  });
  addValues([{ category: "person_names", mainText: "Marie Duval" }]);
  addSuggestions([{ discoveryMethods: ["heuristic"], mainText: "Thomas Berger", category: "person_names" }]);
  addPattern("INV-\\d{6}");
  setValueTables(
    [{ original: "Marie Duval", placeholder: "[PERSON_1]", category: "person_names", count: 2 }],
    [{ original: "Aurora Group", placeholder: "[ENTITY_1]", category: "entity_names" }]);
  dismissWarning("w");
  addAllowTerm("CSSF");
  setDocumentCountry("DE");
  setMinConfidence(0.9);
}

test("startNewBatch clears everything about THIS batch", () => {
  finishedBatch();
  startNewBatch();
  const s = getState();
  assert.deepEqual(s.documents, []);
  assert.equal(s.previewDoc, null);
  assert.deepEqual(s.importErrors, []);
  assert.deepEqual(s.values, []);
  assert.deepEqual(s.suggestions, []);
  assert.deepEqual(s.patterns, []);
  assert.deepEqual(s.replacedValues, []);
  assert.deepEqual(s.removedValues, []);
  assert.deepEqual(s.dismissedWarnings, []);
  assert.deepEqual(s.metaReview, {});
  assert.equal(s.results, null);
  assert.equal(s.resultDoc, null);
  assert.equal(s.running, false);
  assert.equal(s.progress, null);
  assert.equal(s.discovery, null);
});

test("startNewBatch clears the MAPPING, which is the previous batch's key", () => {
  // Not tidiness: leaving it in memory across an explicit "start again" keeps
  // sensitive data alive with nothing on screen referring to it.
  finishedBatch();
  assert.ok(getState().mapping, "the fixture must have a mapping to clear");
  startNewBatch();
  assert.equal(getState().mapping, null);
});

test("startNewBatch KEEPS the settings, the country and the allowlist", () => {
  // All of it describes how this user works. Re-entering it for every batch is
  // exactly the tedium the button exists to avoid.
  finishedBatch();
  const before = {
    categories: { ...getState().settings.categories },
    minConfidence: getState().settings.minConfidence,
    heuristicDiscovery: { ...getState().settings.heuristicDiscovery },
    ollamaPort: getState().settings.ollamaPort,
    country: getState().documentCountry,
    allowlist: [...getState().allowlist],
    exportDir: getState().exportDir,
  };
  startNewBatch();
  const s = getState();
  assert.deepEqual(s.settings.categories, before.categories);
  assert.equal(s.settings.minConfidence, before.minConfidence);
  assert.deepEqual(s.settings.heuristicDiscovery, before.heuristicDiscovery);
  assert.equal(s.settings.ollamaPort, before.ollamaPort);
  assert.equal(s.documentCountry, before.country, "the country is a setting, not batch data");
  assert.deepEqual(s.allowlist, before.allowlist, "the never-anonymise list is curated across batches");
  assert.equal(s.exportDir, before.exportDir, "the destination folder is a convenience worth keeping");
});

test("startNewBatch returns the wizard to Import", () => {
  // It is the only step a cleared batch can stand on: every guard past it needs
  // documents.
  finishedBatch();
  startNewBatch();
  assert.equal(getState().step, "import");
  assert.deepEqual(WIZARD_STEPS.filter((step) => canGoTo(step)), ["import"]);
});

test("startNewBatch reports what it cleared", () => {
  finishedBatch();
  const cleared = startNewBatch();
  assert.equal(cleared.documents, 2);
  assert.ok(cleared.values >= 1);
});

test("startNewBatch dismisses any notice on screen", () => {
  // A notice about the batch that just ended would read as being about the empty
  // screen that replaced it.
  finishedBatch();
  setNotice("Batch exported to C:\\out.zip", "ok");
  startNewBatch();
  assert.equal(getState().notice, null);
});

test("setExportDir stores and trims, and can forget the folder", () => {
  resetState();
  assert.equal(setExportDir("  C:\\out  "), "C:\\out");
  assert.equal(getState().exportDir, "C:\\out");
  assert.equal(setExportDir(""), "");
  assert.equal(getState().exportDir, "");
  assert.equal(setExportDir(undefined), "");
});

// --- The step 3 value tables ---------------------------------------------

test("setValueTables mirrors both halves of the registry at once", () => {
  // Both lists land together because they are one picture: a value moves from
  // one to the other, and updating them separately shows it in both or in
  // neither for one repaint.
  resetState();
  const replaced = [{ original: "Marie Duval", placeholder: "[PERSON_1]", category: "person_names", count: 3 }];
  const removed = [{ original: "Thomas Berger", placeholder: "[PERSON_2]", category: "person_names" }];

  setValueTables(replaced, removed);  assert.deepEqual(getState().replacedValues, replaced);
  assert.deepEqual(getState().removedValues, removed);
});

test("setValueTables treats a missing list as empty rather than undefined", () => {
  // A bridge that answered with nothing must not leave the view mapping over
  // undefined, which throws while rendering and takes the screen down.
  resetState();
  setValueTables(undefined, undefined);
  assert.deepEqual(getState().replacedValues, []);
  assert.deepEqual(getState().removedValues, []);
});

test("leaving the Anonymise step backwards clears the value tables", () => {
  // They describe a run. Keeping them past a reset would show the previous
  // run's replacements beside no run at all.
  resetState();
  setValueTables(
    [{ original: "Marie Duval", placeholder: "[PERSON_1]", category: "person_names", count: 1 }],
    [{ original: "Thomas Berger", placeholder: "[PERSON_2]", category: "person_names" }]);
  resetStep("anonymise");
  assert.deepEqual(getState().replacedValues, []);
  assert.deepEqual(getState().removedValues, []);
});

test("resultDoc is declared state and a reset clears it", () => {
  // It was introduced ad hoc by the view, against state.js's own rule, so it
  // survived resetStep and pointed the Compare pane at a document from a run
  // that no longer existed.
  resetState();
  assert.ok("resultDoc" in getState(), "the field has to be declared here");
  setState({ resultDoc: "a.docx" });
  resetStep("anonymise");
  assert.equal(getState().resultDoc, null);
});

// --- Value editing (the My values tab) -----------------------------------
//
// Detection proposes and the user corrects: these reducers are the correction.
// Each one that changes what a value MATCHES resets that row's derivedSpellings to
// pending (null) so Go re-runs the expansion around the change.

/** seedValue(category, mainText, derivedSpellings, patch) adds one accepted value with
 *  a settled spelling list, the shape the My values tab actually holds. */
function seedValue(category, mainText, derivedSpellings = [], patch = {}) {
  addValues([{ category, mainText }]);
  setValueSpellings(category, mainText, derivedSpellings);
  if (Object.keys(patch).length) {
    setState({
      values: getState().values.map((e) =>
        valueKey(e.category, e.mainText) === valueKey(category, mainText) ? { ...e, ...patch } : e),
    });
  }
  return getState().values.find((e) => valueKey(e.category, e.mainText) === valueKey(category, mainText));
}

test("renameValue changes the name and re-pends the derivedSpellings", () => {
  resetState();
  seedValue("person_names", "Marie Duvel", ["Marie Duvel", "Marie"]);
  assert.equal(renameValue("person_names", "Marie Duvel", "Marie Duval"), "");
  const e = getState().values[0];
  assert.equal(e.mainText, "Marie Duval");
  assert.equal(e.derivedSpellings, null, "a rename re-expands the row");
});

test("renameValue refuses a name the same type already holds", () => {
  resetState();
  seedValue("person_names", "Marie Duval");
  seedValue("person_names", "Thomas Berger");
  assert.equal(renameValue("person_names", "Thomas Berger", "Marie Duval"), "duplicate");
  // Nothing changed: still two values, both original names.
  assert.equal(getState().values.length, 2);
});

test("renameSpelling curates the list with the old spelling swapped out", () => {
  resetState();
  seedValue("entity_names", "Delta Industries", ["Delta Industries", "Delta"]);
  assert.equal(renameSpelling("entity_names", "Delta Industries", "Delta", "Deltaa"), "");
  const e = getState().values[0];
  assert.equal(e.spellingPolicy, "curated", "editing a spelling makes the list the user's");
  assert.ok(e.spellings.includes("Deltaa"), "the new spelling is in the list");
  assert.ok(!e.spellings.some((x) => x.toLowerCase() === "delta"),
    "the old spelling is gone, and no expansion is left to derive it again");
  assert.deepEqual(e.derivedSpellings, [], "a curated row is settled, not pending");
});

// pendingExpansions is what refreshVariants iterates: a curated row must not
// appear in it, or Go would derive the deleted spelling straight back.
import { pendingExpansions } from "./valuemodel.js";

test("a deleted spelling stays deleted through a refresh", () => {
  // The regression a per-value exclusion list existed to prevent. Curation
  // covers it instead: a settled row is never asked to expand again, so nothing
  // can derive the deleted spelling a second time.
  resetState();
  seedValue("entity_names", "Delta Industries", ["Delta Industries", "Delta"]);
  const before = getState().values[0];
  const kept = [...spellingsOf(before).values()].filter((x) => x !== "Delta");
  setState({ values: [curate(before, kept)] });

  const e = getState().values[0];
  assert.equal(e.spellingPolicy, "curated");
  assert.ok(!spellingsOf(e).has("delta"), "the spelling is gone");
  assert.deepEqual(pendingExpansions(getState().values), [],
    "a curated row is never re-expanded, so nothing brings it back");
});

test("groupValues keeps the survivor curated when any participant was", () => {
  resetState();
  seedValue("entity_names", "Delta Industries", ["Delta Industries", "Delta"]);
  seedValue("entity_names", "Delta Group", ["Delta Group"]);
  // The user curated the value they are about to fold in. A merge must not
  // silently re-derive a list they set by hand.
  const src = getState().values.find((e) => e.mainText === "Delta Group");
  setState({
    values: getState().values.map((e) =>
      e.mainText === "Delta Group" ? curate(e, ["DG"]) : e),
  });
  assert.ok(src);

  assert.equal(groupValues(
    { category: "entity_names", mainText: "Delta Industries" },
    [{ category: "entity_names", mainText: "Delta Group" }]), "");
  const kept = getState().values[0];
  assert.equal(kept.spellingPolicy, "curated", "the merged value stays curated");
  assert.ok(spellingsOf(kept).has("delta group"), "the folded name is a spelling");
  assert.ok(spellingsOf(kept).has("dg"), "so are the folded value's own spellings");
});

test("renameSpelling on the spelling that IS the name renames the value", () => {
  resetState();
  seedValue("entity_names", "Delta Industries", ["Delta Industries", "Delta"]);
  assert.equal(renameSpelling("entity_names", "Delta Industries", "Delta Industries", "Delta Group"), "");
  assert.equal(getState().values[0].mainText, "Delta Group");
});

test("changeValueCategory moves the value and switches the new type on", () => {
  resetState();
  toggleCategory("person_names", false);
  seedValue("entity_names", "Meridian");
  assert.equal(changeValueCategory("entity_names", "Meridian", "person_names"), "");
  const e = getState().values[0];
  assert.equal(e.category, "person_names");
  assert.equal(e.derivedSpellings, null);
  assert.equal(getState().settings.categories.person_names, true,
    "moving a value into a type is the same commitment as accepting one there");
});

test("changeValueCategory refuses a type that already holds the name", () => {
  resetState();
  seedValue("entity_names", "Acme");
  seedValue("person_names", "Acme");
  assert.equal(changeValueCategory("entity_names", "Acme", "person_names"), "duplicate");
});

test("changeSuggestionCategory retypes a suggestion before it is accepted", () => {
  resetState();
  addSuggestions([{ discoveryMethods: ["heuristic"], mainText: "Meridian", category: "person_names" }]);
  assert.equal(changeSuggestionCategory("Meridian", "project_names"), true);
  assert.equal(getState().suggestions[0].category, "project_names");
  // Accepting it now lands it in the corrected type.
  acceptSuggestion("Meridian");
  assert.equal(getState().values[0].category, "project_names");
});

test("groupValues folds sources into the target and removes them", () => {
  resetState();
  seedValue("entity_names", "Meridian Consulting", ["Meridian Consulting"]);
  seedValue("entity_names", "Meridian", ["Meridian"], { spellings: ["Merid"] });
  const before = getState().values.length;
  assert.equal(groupValues(
    { category: "entity_names", mainText: "Meridian Consulting" },
    [{ category: "entity_names", mainText: "Meridian" }]), "");
  const list = getState().values;
  assert.equal(before - list.length, 1,
    "the count a caller wants comes from the store, not from the failure slot");
  assert.equal(list.length, 1, "the source value is gone");
  const kept = list[0];
  assert.equal(kept.mainText, "Meridian Consulting");
  assert.ok(kept.spellings.includes("Meridian"), "the source name becomes a spelling");
  assert.ok(kept.spellings.includes("Merid"), "the source's own spellings come too");
  assert.equal(kept.derivedSpellings, null, "the target re-expands around the merged set");
});

test("clearAllValues empties the list and reports the count", () => {
  resetState();
  seedValue("entity_names", "Acme");
  seedValue("person_names", "Marie Duval");
  assert.equal(clearAllValues(), 2);
  assert.equal(getState().values.length, 0);
  assert.equal(clearAllValues(), 0, "an already-empty list clears nothing");
});

// --- Conflict detection for the My values tab ----------------------------

test("spellingsOf is the name, the expanded derivedSpellings and the manual ones", () => {
  const e = {
    category: "entity_names", mainText: "Delta Industries",
    derivedSpellings: ["Delta Industries", "Delta"], spellings: ["DI"],
  };
  const keys = [...spellingsOf(e).keys()];
  assert.deepEqual(keys.sort(), ["delta", "delta industries", "di"]);
});

test("spellingsOf on a curated value is exactly its curated list", () => {
  // A curated row carries an empty expanded list, so the same walk covers it:
  // what the card shows is what the run replaces.
  const e = curate({
    category: "entity_names", mainText: "Delta Industries",
    derivedSpellings: ["Delta Industries", "Delta"], spellings: ["DI"],
  }, ["Delta Industries", "DI"]);
  const keys = [...spellingsOf(e).keys()];
  assert.deepEqual(keys.sort(), ["delta industries", "di"]);
  assert.ok(!keys.includes("delta"), "a deleted spelling is not re-derived");
});

test("valueConflicts flags the same name under two types", () => {
  resetState();
  seedValue("entity_names", "Acme", ["Acme"]);
  seedValue("person_names", "Acme", ["Acme"]);
  const conflicts = valueConflicts();
  const a = conflicts.get(valueKey("entity_names", "Acme"));
  const b = conflicts.get(valueKey("person_names", "Acme"));
  assert.ok(a && a.nameConflicts.length, "the entity_names card is flagged");
  assert.ok(b && b.nameConflicts.length, "the person_names card is flagged");
  assert.ok(a.list.some((c) => c.kind === "ambiguity"));
});

test("valueConflicts flags a spelling two values both claim", () => {
  resetState();
  seedValue("person_names", "Marie Duval", ["Marie Duval", "Marie"]);
  seedValue("person_names", "Marie Dupont", ["Marie Dupont", "Marie"]);
  const conflicts = valueConflicts();
  const a = conflicts.get(valueKey("person_names", "Marie Duval"));
  assert.ok(a.spellingConflicts.has("marie"), "the shared spelling chip is flagged");
  assert.ok(a.list.some((c) => c.kind === "collision"));
});

test("valueConflicts flags a value that is also on the never-anonymise list", () => {
  resetState();
  seedValue("entity_names", "CSSF", ["CSSF"]);
  addAllowTerm("CSSF");
  const conflicts = valueConflicts();
  const a = conflicts.get(valueKey("entity_names", "CSSF"));
  assert.ok(a && a.list.some((c) => c.kind === "allowlist"));
});

test("valueConflicts ignores values whose type is switched off", () => {
  resetState();
  seedValue("entity_names", "Acme", ["Acme"]);
  seedValue("person_names", "Acme", ["Acme"]);
  // Turn one type off: an off category is never replaced, so it cannot conflict.
  toggleCategory("person_names", false);
  const conflicts = valueConflicts();
  assert.equal(conflicts.size, 0, "with one side off there is no ambiguity to flag");
});

// --- Provenance: which methods found a Value -----------------------------

test("accepting a Suggestion preserves every discovery method and its evidence", () => {
  // Provenance is what the workspace shows and what Go reduces to a match class,
  // so losing it here would silently change both the explanation and which claim
  // wins an overlap.
  resetState();
  addSuggestions([{
    mainText: "Pierre Dupont", category: "person_names", count: 3,
    spellings: ["Dupont"],
    discoveryMethods: ["signal", "heuristic"],
    evidence: [{
      kind: "email_local_part", signalCategory: "email",
      signalText: "pierre.dupont@tpps.com", documents: ["engagement.md"],
    }],
  }]);
  assert.equal(acceptSuggestion("Pierre Dupont"), true);

  const value = getState().values[0];
  assert.deepEqual(value.discoveryMethods, ["signal", "heuristic"]);
  assert.equal(value.evidence.length, 1);
  assert.equal(value.evidence[0].signalText, "pierre.dupont@tpps.com");
  assert.deepEqual(value.spellings, ["Dupont"],
    "the folded spellings must survive the accept, or the shorter form fires " +
    "inside the longer and leaves the rest in clear text");

  // And all of it reaches Go, not just the parts the card happens to render.
  const sent = acceptedValues()[0];
  assert.deepEqual(sent.discoveryMethods, ["signal", "heuristic"]);
  assert.equal(sent.evidence.length, 1);
});

test("acceptAllShown keeps each row's own methods", () => {
  resetState();
  addSuggestions([
    { discoveryMethods: ["heuristic"], mainText: "Meridian", category: "entity_names" },
    { discoveryMethods: ["local_llm"], mainText: "Borealis", category: "entity_names" },
  ]);
  assert.equal(acceptAllShown(["Meridian", "Borealis"]), 2);
  const byName = Object.fromEntries(
    getState().values.map((v) => [v.mainText, v.discoveryMethods]));
  assert.deepEqual(byName, { Meridian: ["heuristic"], Borealis: ["local_llm"] });
});

test("a Value the user typed carries the manual method, and it travels to Go", () => {
  resetState();
  addValues([{ category: "entity_names", mainText: "Alpine" }]);
  assert.deepEqual(getState().values[0].discoveryMethods, ["manual"]);
  assert.deepEqual(acceptedValues()[0].discoveryMethods, ["manual"]);
});

test("a Suggestion two runs both find merges instead of being dropped", () => {
  // Before the unified response the second run's row was skipped as a duplicate,
  // so a method that only the second run used never appeared anywhere.
  resetState();
  assert.equal(addSuggestions([{
    mainText: "Pierre Dupont", category: "person_names", count: 2,
    discoveryMethods: ["heuristic"], contexts: ["first snippet"],
  }]), 1);
  assert.equal(addSuggestions([{
    mainText: "pierre dupont", category: "person_names", count: 1,
    discoveryMethods: ["signal"], spellings: ["Dupont"],
    contexts: ["second snippet"],
    evidence: [{ kind: "email_local_part", signalText: "pierre.dupont@tpps.com" }],
  }]), 0, "the same name is not a second row");

  const row = getState().suggestions[0];
  assert.equal(row.mainText, "Pierre Dupont", "the first-seen spelling wins");
  assert.equal(row.count, 3, "counts add up");
  assert.deepEqual(row.discoveryMethods, ["heuristic", "signal"]);
  assert.deepEqual(row.spellings, ["Dupont"]);
  assert.deepEqual(row.contexts, ["first snippet", "second snippet"]);
  assert.equal(row.evidence.length, 1);
});

// --- Signal-derived Suggestions ------------------------------------------

test("the three shared vocabularies are the ones Go knows", () => {
  // The parity guards in ../detection_parity_test.go check these against Go.
  // Here they are pinned as a list, so a local edit that breaks the shape of the
  // declaration fails in this suite too rather than only in the Go one.
  assert.deepEqual(DISCOVERY_METHODS, ["manual", "signal", "heuristic", "local_llm"]);
  assert.deepEqual(MATCH_CLASSES,
    ["built_in_pattern", "user_defined", "rules_discovered", "local_llm_discovered"]);
  assert.deepEqual(SIGNAL_SOURCES, ["email", "url"]);
});

test("email-derived Suggestions are on by default, and switchable", () => {
  resetState();
  assert.equal(signalSourceOn(getState(), "email"), true);
  assert.deepEqual(enabledSignalSources(getState()), SIGNAL_SOURCES);

  assert.equal(setSignalSource("email", false), true);
  assert.equal(signalSourceOn(getState(), "email"), false);
  assert.deepEqual(enabledSignalSources(getState()), ["url"],
    "and the other signal is untouched: each source is switched on its own");
});

test("switching a signal source off leaves the category switch alone", () => {
  // Acceptance criterion 4: clearing "Email addresses" stops email-DERIVED
  // Suggestions and must not stop email addresses being anonymised. The two are
  // different mechanisms behind different settings, and conflating them is the
  // mistake the separate setting exists to prevent.
  resetState();
  setSignalSource("email", false);
  assert.equal(getState().settings.categories.email, true,
    "the email category must be untouched");
  assert.equal(getState().settings.useBuiltInPatterns, true,
    "built-in pattern matching must be untouched");
});

test("an unknown signal source is refused rather than stored", () => {
  // A key Go does not implement would be a control that appears to do something
  // and does not, so it never reaches the settings at all.
  resetState();
  assert.equal(setSignalSource("telepathy", true), false);
  assert.ok(!("telepathy" in getState().settings.signalSuggestionSources));
});

test("a missing signal-source key reads as ON, never as off", () => {
  // The safe reading of silence is the shipped default. Reading it as "off"
  // would silently disable a source for anyone whose settings predate the key.
  resetState();
  setState({ settings: { ...getState().settings, signalSuggestionSources: {} } });
  assert.equal(signalSourceOn(getState(), "email"), true);
});

// --- Per-reading signal switches -----------------------------------------
//
// A signal supports several READINGS through several mechanisms: an address's
// local part is evidence for a person, its domain for an organisation. Wanting one
// without the other is reasonable, so each is stored and switched on its own and
// the signal above them is a derived master.

test("SIGNAL_DERIVATIONS lists a signal's readings, and every source has some", () => {
  // The tree the rail renders. A source with no readings would render as a master
  // over nothing, which is a control that appears to do something and does not.
  for (const source of SIGNAL_SOURCES) {
    const derivations = SIGNAL_DERIVATIONS[source];
    assert.ok(Array.isArray(derivations) && derivations.length > 0,
      `${source} has no readings, so its row would be a master over nothing`);
  }
  assert.deepEqual(SIGNAL_DERIVATIONS.email, ["email.person", "email.organisation"]);
});

test("every reading is on by default and switchable on its own", () => {
  resetState();
  assert.equal(signalDerivationOn(getState(), "email", "email.person"), true);
  assert.equal(signalDerivationOn(getState(), "email", "email.organisation"), true);

  assert.equal(setSignalDerivation("email", "email.person", false), true);
  assert.equal(signalDerivationOn(getState(), "email", "email.person"), false);
  assert.equal(signalDerivationOn(getState(), "email", "email.organisation"), true,
    "the other reading is untouched: that independence is the whole point");
  assert.deepEqual(enabledSignalDerivations(getState(), "email"), ["email.organisation"]);
});

test("the signal's own state is DERIVED from its readings, never stored", () => {
  // On when ANY reading is on. A stored flag beside the set it summarises could
  // disagree with it, and a signal reading "on" over readings that are all off
  // lies about what a run does.
  resetState();
  setSignalDerivation("email", "email.person", false);
  assert.equal(signalSourceOn(getState(), "email"), true,
    "one reading left on still derives something");
  assert.deepEqual(enabledSignalSources(getState()), SIGNAL_SOURCES);

  setSignalDerivation("email", "email.organisation", false);
  assert.equal(signalSourceOn(getState(), "email"), false,
    "every reading off is the only thing that reads as off");
  assert.deepEqual(enabledSignalSources(getState()), ["url"]);
});

test("setSignalSource is the MASTER: it writes every reading of that signal", () => {
  resetState();
  setSignalSource("email", false);
  for (const derivation of SIGNAL_DERIVATIONS.email) {
    assert.equal(signalDerivationOn(getState(), "email", derivation), false,
      `${derivation} must follow the master, or switching a signal off is an N-click job`);
  }
  setSignalSource("email", true);
  assert.deepEqual(enabledSignalDerivations(getState(), "email"), SIGNAL_DERIVATIONS.email);
});

test("an unknown source-and-reading pair is refused rather than stored", () => {
  // A derivation identifier is only meaningful UNDER its source, so both halves are
  // checked: a valid reading filed under the wrong source is just as dead as an
  // invented one.
  resetState();
  assert.equal(setSignalDerivation("email", "email.telepathy", true), false);
  assert.equal(setSignalDerivation("telepathy", "email.person", true), false);
  assert.ok(!("email.telepathy" in getState().settings.signalSuggestionSources.email));
  assert.ok(!("telepathy" in getState().settings.signalSuggestionSources));
  assert.equal(signalDerivationOn(getState(), "email", "email.telepathy"), false,
    "an unknown reading is never enabled, whatever the stored map says");
});

test("a missing reading key reads as ON at either level, never as off", () => {
  resetState();
  setState({ settings: { ...getState().settings, signalSuggestionSources: {} } });
  assert.equal(signalDerivationOn(getState(), "email", "email.person"), true,
    "no source key at all reads as the shipped default");

  setState({ settings: { ...getState().settings, signalSuggestionSources: { email: {} } } });
  assert.equal(signalDerivationOn(getState(), "email", "email.person"), true,
    "a source present with no readings named reads as the shipped default too");

  setState({ settings: { ...getState().settings,
    signalSuggestionSources: { email: { "email.person": false } } } });
  assert.equal(signalDerivationOn(getState(), "email", "email.organisation"), true,
    "and a partial map fills the rest from the defaults, not from off");
});

test("setSignalSource writes the NESTED shape, so the row master keeps working", () => {
  // The row master reaches signalSuggestionSources wholesale. Writing a boolean
  // where a map belongs would leave the whole signal reading as its default for
  // the rest of the session, so the master would appear not to work.
  resetState();
  for (const source of SIGNAL_SOURCES) setSignalSource(source, false);
  for (const source of SIGNAL_SOURCES) {
    for (const derivation of SIGNAL_DERIVATIONS[source]) {
      assert.equal(signalDerivationOn(getState(), source, derivation), false,
        `${derivation} must be off after the row master switched everything off`);
    }
  }

  setSignalSource("email", true);
  assert.deepEqual(enabledSignalDerivations(getState(), "email"), SIGNAL_DERIVATIONS.email,
    "switching the row back on restores every reading of that source");
});

test("switching ONE reading off leaves the category switch alone", () => {
  // The invariant, now per reading: clearing a reading stops the Suggestions that
  // reading produces and must not stop email addresses being anonymised.
  resetState();
  setSignalDerivation("email", "email.person", false);
  assert.equal(getState().settings.categories.email, true,
    "the email category must be untouched");
  assert.equal(getState().settings.useBuiltInPatterns, true,
    "built-in pattern matching must be untouched");
});

// --- Intersections: values another route also claims ---------------------

test("intersectionsFor keys each row by the value it belongs to", () => {
  resetState();
  seedValue("person_names", "marie.duval@example.com");
  setIntersections([{
    value: "marie.duval@example.com", category: "person_names", matchClass: "user_defined",
    winnerValue: "marie.duval@example.com", winnerCategory: "email", winnerOrigin: "native",
    occurrences: 2, totalOccurrences: 2,
  }]);

  const byKey = intersectionsFor();
  const row = byKey.get(valueKey("person_names", "marie.duval@example.com"));
  assert.ok(row, "the card finds its own row with no searching");
  assert.equal(row.winnerCategory, "email");
  assert.equal(byKey.get(valueKey("entity_names", "Alpine")), undefined);
});

test("a row with no value or no category is not keyable and is skipped", () => {
  // Go always sends both, but a partial row must not poison the map with an
  // entry no card can match.
  resetState();
  setIntersections([{ value: "", category: "email" }, { value: "x" }]);
  assert.equal(intersectionsFor().size, 0);
});

test("editing the values clears the intersection warnings", () => {
  // A stale warning sits on a card describing a configuration the user has
  // already changed, and is read as a statement about the one in front of them.
  resetState();
  seedValue("entity_names", "Alpine");
  setIntersections([{ value: "Alpine", category: "entity_names" }]);
  assert.equal(getState().intersections.length, 1);

  addValues([{ category: "entity_names", mainText: "Borealis" }]);
  assert.deepEqual(getState().intersections, [], "adding a value invalidates the answer");
});

test("editing the patterns clears the intersection warnings too", () => {
  resetState();
  setIntersections([{ value: "Alpine", category: "entity_names" }]);
  addPattern("PRJ-[0-9]+", null);
  assert.deepEqual(getState().intersections, []);
});

test("a patch that carries intersections is the one that survives", () => {
  // setIntersections itself writes values-adjacent state on some paths; the
  // clearing rule must not eat the answer it was given.
  resetState();
  setState({ values: [], intersections: [{ value: "Alpine", category: "entity_names" }] });
  assert.equal(getState().intersections.length, 1);
});

test("starting a new batch drops the intersections with everything else", () => {
  resetState();
  setIntersections([{ value: "Alpine", category: "entity_names" }]);
  startNewBatch();
  assert.deepEqual(getState().intersections, []);
});

// --- Value families: the shorter form is the main value ------------------

test("a longer addition becomes a spelling of the existing shorter value", () => {
  // Left as two values the shorter fires inside the longer and the text reads
  // "[BRAND_1] company", leaking the rest of the phrase.
  resetState();
  seedValue("brand_names", "Coca-Cola");
  const folded = foldIntoFamily("brand_names", "Coca-Cola company");

  assert.deepEqual(folded, { main: "Coca-Cola", added: "Coca-Cola company" });
  assert.equal(getState().values.length, 1, "one value, not two rivals");
  assert.ok(getState().values[0].spellings.includes("Coca-Cola company"));
});

test("a shorter addition takes over as the value's name", () => {
  // The fold works in both directions: the shorter form is always the main
  // value, so the old name becomes one of its spellings.
  resetState();
  seedValue("brand_names", "Coca-Cola company");
  const folded = foldIntoFamily("brand_names", "Coca-Cola");

  assert.deepEqual(folded, { main: "Coca-Cola", added: "Coca-Cola company" });
  assert.equal(getState().values.length, 1);
  assert.equal(getState().values[0].mainText, "Coca-Cola");
  assert.ok(getState().values[0].spellings.includes("Coca-Cola company"),
    "the old name is kept as a spelling, so the two share one placeholder");
});

test("foldIntoFamily refuses across types, off boundaries and below the guard", () => {
  const cases = [
    // A person and an organisation are an intersection, not a family: folding
    // them would file a human being under an organisation.
    { seed: ["person_names", "Delta"], add: ["entity_names", "Delta Industries"] },
    // "Alten" is not a spelling of "Altenberg".
    { seed: ["entity_names", "Alten"], add: ["entity_names", "Altenberg"] },
    // A two-character stem would shred ordinary text if promoted.
    { seed: ["entity_names", "BV"], add: ["entity_names", "BV Holdings"] },
    // Unrelated values.
    { seed: ["entity_names", "Alpine Trust"], add: ["entity_names", "Borealis"] },
  ];
  for (const { seed, add } of cases) {
    resetState();
    seedValue(seed[0], seed[1]);
    assert.equal(foldIntoFamily(add[0], add[1]), null,
      `${seed[1]} and ${add[1]} must not be folded`);
  }
});

test("foldIntoFamily leaves an empty or identical addition alone", () => {
  resetState();
  seedValue("entity_names", "Alpine Trust");
  assert.equal(foldIntoFamily("entity_names", "   "), null);
  assert.equal(foldIntoFamily("entity_names", "alpine trust"), null,
    "the same value is not a spelling of itself");
});

test("an accepted Suggestion carries its folded spellings across", () => {
  resetState();
  addSuggestions([{
    discoveryMethods: ["heuristic"], mainText: "Alpine Trust",
    category: "entity_names", spellings: ["Alpine Trust S.A."],
  }]);
  assert.equal(acceptSuggestion("Alpine Trust"), true);
  assert.deepEqual(getState().values[0].spellings, ["Alpine Trust S.A."],
    "one Value with its spellings reaches the pipeline, not two rivals");
});

test("an accepted local model Suggestion keeps the model confidence, not the manual one", () => {
  // A CROSS-BRIDGE contract, and the Go constants are its source of truth:
  // engine.ConfidenceLLMDefault is 0.8 and engine.ConfidenceManualDefault is
  // 0.95. The number is asserted literally here because that is what the bridge
  // actually carries; if the Go constant moves, this test is meant to fail and
  // be moved with it.
  //
  // The failure it prevents is silent in every other test: a Value that reaches
  // Go with confidence 0 is read as "not stated", which valueConfidence scores
  // as a user declaration. Raising Minimum confidence past 80 would then leave
  // the model's own guesses in place, which is the opposite of what the control
  // promises.
  resetState();
  addSuggestions([{
    discoveryMethods: ["local_llm"], mainText: "Borealis Fund",
    category: "entity_names", confidence: 0.8,
  }]);
  assert.equal(acceptSuggestion("Borealis Fund"), true);
  assert.equal(getState().values[0].confidence, 0.8,
    "the local model score must survive acceptance, or the confidence floor cannot act on it");
});

test("a Value the user declared states no confidence, which Go reads as a declaration", () => {
  // The mirror of the test above, and the reason confidence defaults to 0
  // rather than to some number this side invents: "not stated" is a real state
  // with a meaning the engine owns (ConfidenceManualDefault). A frontend that
  // filled in a default here would make every manual Value filterable by
  // accident.
  resetState();
  addValues([{ category: "person_names", mainText: "Marie Duval" }]);
  assert.equal(getState().values[0].confidence, 0,
    "a manual Value states no confidence and lets the engine's default serve it");
});

// --- What the last local model scan did ----------------------------------

test("nothing is claimed about a local model scan before one has run", () => {
  resetState();
  assert.equal(getState().lastLLMScan, null,
    "an absent scan is null, not a row of zeroes that reads as a scan that found nothing");
});

test("stepping back to Identify forgets the last scan's numbers", () => {
  // The numbers describe a run, and the backward reset discards the run. Left
  // behind, they would describe a scan whose suggestions are already gone, which
  // is worse than showing nothing: the reader has no way to tell.
  resetState();
  setState({ lastLLMScan: { requests: 9, silent: 2, secondsPerRequest: 4 } });
  resetStep("identify");
  assert.equal(getState().lastLLMScan, null,
    "the read-out must not survive the reset of the step it describes");
});


// --- Pictures: the Anonymise step's IMAGE half ---------------------------
//
// The rules the review renders from live here rather than in the view, so they
// are testable without markup. Two of them are load-bearing:
//
//   imageStatus is THE mapping from a treatment to a status. Written out in the
//   banner, the Status column and the tile chip, those three could disagree, and
//   then the Anonymised filter shows a row the column calls Kept.
//
//   treatmentBlockedReason is the frontend half of imaging.Decision.Validate. A
//   control the interface offers that Go refuses is a control that lies, and the
//   refusal then arrives at export time instead of on the panel.

const PICTURE_DOC = "deck.pptx";

/** picture(patch) is one inventory asset. */
function picture(patch) {
  return {
    id: "ppt/media/image1.png", name: "Logo", format: "png", bytes: 1024,
    width: 120, height: 80, companion: "", linked: false,
    occurrences: [{ part: "ppt/slides/slide1.xml", ordinal: 0, kind: "picture", location: "Slide 1" }],
    decision: { treatment: "keep" },
    ...patch,
  };
}

/** seedPictures(assets) puts one imported document with a scanned inventory in
 *  the store. */
function seedPictures(assets) {
  resetState();
  setState({
    documents: [{ name: PICTURE_DOC, format: "pptx" }],
    resultDoc: PICTURE_DOC,
    images: {
      [PICTURE_DOC]: {
        loading: false, error: null,
        inventory: { applicable: true, assets, warnings: [] },
      },
    },
  });
}

test("the picture vocabularies are the engine's, in the engine's order", () => {
  // The Go halves are held to these by ../image_parity_test.go; this case is
  // about the SHAPE, so a reordering here is caught in the frontend suite too.
  assert.deepEqual(IMAGE_TREATMENTS, ["keep", "box", "blur", "remove"]);
  assert.deepEqual(IMAGE_FORMATS, ["png", "jpeg", "svg", "other"]);
  assert.deepEqual(IMAGE_KINDS, ["picture", "fill", "background"]);
  assert.deepEqual(IMAGE_STATUS_FILTERS, ["all", "kept", "anonymised"]);
});

test("a preview is cached per document AND per asset", () => {
  // The asset id is the archive part path, which is unique inside one document
  // and not across a batch: two decks both hold ppt/media/image1.png.
  assert.notEqual(
    imageThumbKey("a.pptx", "ppt/media/image1.png"),
    imageThumbKey("b.pptx", "ppt/media/image1.png"),
    "one deck's logo must not appear in another deck's list");
});

test("the IMAGE half reads the same document selection as the Compare card", () => {
  seedPictures([picture({})]);
  assert.equal(imageDocName(getState()), PICTURE_DOC);
  setState({ resultDoc: "gone.pptx" });
  assert.equal(imageDocName(getState()), PICTURE_DOC,
    "a selection naming a document that is no longer imported falls back to the first one");
});

test("an inventory nobody has asked for is null, and an empty one is an answer", () => {
  resetState();
  assert.equal(imagesFor(getState(), PICTURE_DOC), null,
    "null is the caller's cue to fetch");
  seedPictures([]);
  assert.deepEqual(imageAssets(getState()), [],
    "a document with no pictures is an answer, not a failure");
});

test("a scan in flight keeps the previous answer on screen", () => {
  seedPictures([picture({})]);
  startImageScan(PICTURE_DOC);
  const record = imagesFor(getState(), PICTURE_DOC);
  assert.equal(record.loading, true);
  assert.equal(record.inventory.assets.length, 1,
    "replacing the answer with nothing would blank the list the user is reading");
});

test("a refused scan replaces the answer with the reason", () => {
  seedPictures([picture({})]);
  setImageScanError(PICTURE_DOC, "the document is not imported");
  const record = imagesFor(getState(), PICTURE_DOC);
  assert.equal(record.error, "the document is not imported");
  assert.equal(record.inventory, null);
});

test("keep is the one status that is not a treatment, and an absent decision reads as keep", () => {
  assert.equal(imageStatus(picture({ decision: { treatment: "keep" } })), "kept");
  assert.equal(imageStatus(picture({ decision: {} })), "kept",
    "keep is stored as the ABSENCE of a decision, so an empty one is a kept picture");
  assert.equal(imageStatus(picture({})), "kept");
  for (const treatment of ["box", "blur", "remove"]) {
    assert.equal(imageStatus(picture({ decision: { treatment } })), "anonymised",
      `${treatment} changes the picture, so it is anonymised`);
  }
});

test("the filter counts and the filter itself read the same mapping", () => {
  seedPictures([
    picture({ id: "a", decision: { treatment: "keep" } }),
    picture({ id: "b", decision: { treatment: "box" } }),
    picture({ id: "c", decision: { treatment: "blur" } }),
    picture({ id: "d", decision: { treatment: "remove" } }),
  ]);
  assert.deepEqual(imageStatusCounts(getState()), { all: 4, kept: 1, anonymised: 3 });

  setImageFilter("anonymised");
  assert.deepEqual(filteredImages(getState()).map((a) => a.id), ["b", "c", "d"]);
  setImageFilter("kept");
  assert.deepEqual(filteredImages(getState()).map((a) => a.id), ["a"]);
  setImageFilter("all");
  assert.equal(filteredImages(getState()).length, 4);
});

test("an unknown filter shows everything rather than hiding the document", () => {
  seedPictures([picture({ id: "a" }), picture({ id: "b", decision: { treatment: "box" } })]);
  setImageFilter("invented");
  assert.equal(getState().imageFilter, "all",
    "a filter nobody can clear would hide the whole document");
  assert.equal(filteredImages(getState()).length, 2);
});

test("the tab and the layout fall back rather than storing a token nothing renders", () => {
  resetState();
  setAnonymiseTab("images");
  assert.equal(getState().anonymiseTab, "images");
  setAnonymiseTab("invented");
  assert.equal(getState().anonymiseTab, "text", "text is the half that is always available");
  setImageLayout("tiles");
  assert.equal(getState().imageLayout, "tiles");
  setImageLayout("invented");
  assert.equal(getState().imageLayout, "details");
});

test("a PNG or JPEG can carry every treatment", () => {
  for (const format of ["png", "jpeg"]) {
    for (const treatment of IMAGE_TREATMENTS) {
      assert.equal(treatmentAvailable(picture({ format }), treatment), true,
        `${format} must offer ${treatment}`);
    }
  }
});

test("an SVG cannot be blurred, and the reason is the invariant rather than a limit", () => {
  const svg = picture({ format: "svg" });
  assert.equal(treatmentBlockedReason(svg, "blur"), "svg_blur",
    "a blur filter leaves every original shape and text string in the file, so a control " +
    "that did it would be labelled anonymise while anonymising nothing");
  assert.equal(treatmentAvailable(svg, "box"), true);
  assert.equal(treatmentAvailable(svg, "remove"), true);
  assert.equal(treatmentAvailable(svg, "keep"), true);
});

test("a format the application cannot redraw offers keep and remove only", () => {
  const other = picture({ format: "other" });
  assert.equal(treatmentBlockedReason(other, "box"), "format");
  assert.equal(treatmentBlockedReason(other, "blur"), "format");
  assert.equal(treatmentAvailable(other, "remove"), true);
  assert.equal(treatmentAvailable(other, "keep"), true);
});

test("a linked picture is refused for being elsewhere, not for its format", () => {
  // The scan reports a linked picture as FormatOther, and Go checks the link
  // FIRST because "there are no bytes here" is the true reason: telling the user
  // the format cannot be redrawn would send them looking for a converter that
  // cannot help.
  const linked = picture({ format: "other", linked: true });
  assert.equal(treatmentBlockedReason(linked, "box"), "linked");
  assert.equal(treatmentBlockedReason(linked, "blur"), "linked");
  assert.equal(treatmentAvailable(linked, "remove"), true);
});

test("an accepted decision is written onto the cached inventory", () => {
  seedPictures([picture({ id: "a" }), picture({ id: "b" })]);
  applyImageDecision(PICTURE_DOC, "b", { treatment: "box", boxText: "Client logo" });
  const assets = imageAssets(getState());
  assert.deepEqual(assets[0].decision, { treatment: "keep" }, "only the named asset changes");
  assert.deepEqual(assets[1].decision, { treatment: "box", boxText: "Client logo" });
});

test("the treatment panel keeps a draft, and closing it discards the draft only", () => {
  seedPictures([picture({ id: "a" })]);
  const stored = { treatment: "keep" };
  openImageEditor(PICTURE_DOC, "a", { treatment: "box", boxText: "" });
  updateImageEditor({ draft: { treatment: "box", boxText: "Client logo" } });
  assert.equal(getState().imageEditor.draft.boxText, "Client logo");
  closeImageEditor();
  assert.equal(getState().imageEditor, null);
  assert.deepEqual(imageAssets(getState())[0].decision, stored,
    "cancelling leaves the recorded decision exactly as it was");
});

test("a reply arriving after the panel closed does not reopen it", () => {
  seedPictures([picture({ id: "a" })]);
  openImageEditor(PICTURE_DOC, "a", { treatment: "blur" });
  closeImageEditor();
  updateImageEditor({ preview: "data:image/png;base64,AA" });
  assert.equal(getState().imageEditor, null,
    "a preview must not resurrect a panel the user dismissed");
});

test("a fresh import forgets every cached inventory and preview", () => {
  seedPictures([picture({ id: "a" })]);
  cacheImageThumb(PICTURE_DOC, "a", "data:image/png;base64,AA");
  openImageEditor(PICTURE_DOC, "a", { treatment: "box" });
  applyImportResult({ documents: [{ name: PICTURE_DOC, format: "pptx" }], errors: [] });
  assert.deepEqual(getState().images, {},
    "a file re-imported under the same name is a DIFFERENT file");
  assert.deepEqual(getState().imageThumbs, {});
  assert.equal(getState().imageEditor, null);
});

test("stepping back from Anonymise keeps the picture decisions and closes the panel", () => {
  // The decisions are about the bytes captured at IMPORT and need no run, so
  // discarding them because the user stepped back to review a value would throw
  // away work the run never touched. The panel is different: an editing overlay
  // must not outlive the screen it belongs to.
  seedPictures([picture({ id: "a", decision: { treatment: "box", boxText: "Client logo" } })]);
  cacheImageThumb(PICTURE_DOC, "a", "data:image/png;base64,AA");
  openImageEditor(PICTURE_DOC, "a", { treatment: "box" });
  resetStep("anonymise");
  assert.equal(getState().imageEditor, null);
  assert.deepEqual(imagesFor(getState(), PICTURE_DOC).inventory.assets[0].decision,
    { treatment: "box", boxText: "Client logo" });
  assert.equal(getState().imageThumbs[imageThumbKey(PICTURE_DOC, "a")], "data:image/png;base64,AA");
});

test("a new batch forgets the pictures with the documents", () => {
  seedPictures([picture({ id: "a" })]);
  cacheImageThumb(PICTURE_DOC, "a", "data:image/png;base64,AA");
  startNewBatch();
  assert.deepEqual(getState().images, {});
  assert.deepEqual(getState().imageThumbs, {});
  assert.equal(getState().imageEditor, null);
});

test("setImageInventory stores exactly what Go answered", () => {
  resetState();
  setState({ documents: [{ name: PICTURE_DOC }], resultDoc: PICTURE_DOC });
  const inventory = { applicable: false, reason: "pdf_images_removed", assets: [], warnings: [] };
  setImageInventory(PICTURE_DOC, inventory);
  assert.deepEqual(imagesFor(getState(), PICTURE_DOC),
    { loading: false, error: null, inventory });
});

// --- One return convention across the value-editing family -------------------

test("all five value-editing reducers return \"\"-or-reason", () => {
  // A caller writing `if (moveSpelling(...)) showError()` shows an error on
  // success, and the falsy-on-success neighbours make exactly that mistake
  // natural. So the family has ONE shape: a string, empty when it worked.
  //
  // A count is not a failure, which is why groupValues no longer returns one in
  // the slot the family uses for failure: a caller that needs it reads the store
  // before and after.
  resetState();
  seedValue("entity_names", "Alpha", ["Alpha", "Alph"]);
  seedValue("entity_names", "Beta", ["Beta"]);

  const results = {
    renameValue: renameValue("entity_names", "Alpha", "Alpha Group"),
    renameSpelling: renameSpelling("entity_names", "Alpha Group", "Alph", "Al"),
    changeValueCategory: changeValueCategory("entity_names", "Alpha Group", "brand_names"),
    moveSpelling: moveSpelling("brand_names", "Alpha Group", "entity_names", "Beta", "Al"),
    groupValues: groupValues(
      { category: "entity_names", mainText: "Beta" },
      [{ category: "brand_names", mainText: "Alpha Group" }]),
  };
  for (const [name, got] of Object.entries(results)) {
    assert.equal(typeof got, "string",
      `${name} returned a ${typeof got}; the family's slot is a reason string`);
    assert.equal(got, "", `${name} reported a failure on a valid call: ${got}`);
  }
});

test("every reducer in the family names its refusal rather than returning a bare false", () => {
  resetState();
  seedValue("entity_names", "Alpha", ["Alpha"]);
  seedValue("entity_names", "Beta", ["Beta"]);

  assert.equal(renameValue("entity_names", "Ghost", "X"), "not found");
  assert.equal(renameValue("entity_names", "Alpha", ""), "empty");
  assert.equal(renameValue("entity_names", "Alpha", "Beta"), "duplicate");
  assert.equal(renameSpelling("entity_names", "Alpha", "Alpha2", ""), "empty");
  assert.equal(changeValueCategory("entity_names", "Alpha", "not_a_category"), "invalid");
  assert.equal(changeValueCategory("entity_names", "Ghost", "brand_names"), "not found");
  assert.equal(moveSpelling("entity_names", "Alpha", "entity_names", "Alpha", "Alpha"), "self");
  assert.equal(moveSpelling("entity_names", "Ghost", "entity_names", "Beta", "x"), "not found");
  assert.equal(moveSpelling("entity_names", "Alpha", "entity_names", "Beta", "Nope"), "absent");
  assert.equal(groupValues({ category: "entity_names", mainText: "Ghost" }, []), "not found");
  assert.equal(groupValues({ category: "entity_names", mainText: "Alpha" }, []), "nothing to merge");
});

test("a promotion keeps every form, and the store proves it as a set", () => {
  // Acceptance criterion 8: renaming a MainText onto one of the row's OWN
  // spellings leaves the set of replaced forms unchanged, and no operation
  // reports success while losing a form.
  resetState();
  seedValue("entity_names", "Northstar", ["Northstar", "NStar"]);
  const before = new Set([...spellingsOf(getState().values[0]).keys()]);

  assert.equal(renameValue("entity_names", "Northstar", "NStar"), "");

  const after = new Set([...spellingsOf(getState().values[0]).keys()]);
  assert.deepEqual([...after].sort(), [...before].sort(),
    "the forms this Value replaces are the same before and after; only which one "
    + "is the main text changed");
  assert.equal(getState().values[0].mainText, "NStar");
});

test("a promotion is refused when ANOTHER value already owns the name", () => {
  // The duplicate check comes first and excludes this row: two values with one
  // name is the ambiguity conflict, whether or not one of them spells it.
  resetState();
  seedValue("entity_names", "Northstar", ["Northstar", "NStar"]);
  seedValue("entity_names", "NStar", ["NStar"]);
  assert.equal(renameValue("entity_names", "Northstar", "NStar"), "duplicate");
  assert.equal(getState().values[0].mainText, "Northstar", "nothing moved");
});

test("the allowlist conflict on a card states the same resolution the engine does", () => {
  // The card and the refused-run panel on Anonymise read ONE field. Each
  // inferring the fix from a ref kind is two places deciding one thing, and two
  // inferences can disagree, at which point a button offers a fix the engine
  // never described.
  resetState();
  seedValue("country_names", "Luxembourg", ["Luxembourg"]);
  addAllowTerm("Luxembourg");

  const conflicts = valueConflicts(getState());
  const entry = conflicts.get(valueKey("country_names", "Luxembourg"));
  assert.ok(entry, "the card carries the conflict");
  const c = entry.list.find((x) => x.kind === "allowlist");
  assert.ok(c, "an allowlisted declared value is a blocking conflict");
  assert.deepEqual(c.resolution, { action: "drop_allow_term", term: "Luxembourg" },
    "the resolution names the action and the term, in the vocabulary "
    + "CONFLICT_RESOLUTIONS mirrors from the engine");
  assert.ok(CONFLICT_RESOLUTIONS.includes(c.resolution.action),
    "and the action is one of the shared vocabulary, not an invented string");
});

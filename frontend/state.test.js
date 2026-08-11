// state.test.js — dev-time unit tests for the store, runnable with
// `node --test "frontend/**/*.test.js"` (node is present on CI runners; this is NOT an npm
// dependency — see BUILD.md Phase 6).
//
// Only pure logic is tested here: state transitions, navigation guards and
// reducers. Views stay logic-free precisely so this file covers what
// matters without a DOM.

import test from "node:test";
import assert from "node:assert/strict";

import {
  COUNTRIES, DEFAULT_COUNTRY, countryIDCategories, COUNTRY_ID_CATEGORIES,
} from "./countries.js";

import {
  getState, setState, resetState, subscribe,
  WIZARD_STEPS, canGoTo, goTo, nextStep,
  goToScreen,
  applyPreset, toggleCategory, selectionPresetName, presetCategories,
  setUseAI, setUseSmartDetect, detectionRoutesOn, llmEnabled,
  addCandidates, acceptCandidate, rejectCandidate,
  moveVariant, entityAutocomplete, reassignOriginal,
  applyImportResult,
  setNotice, clearNotice, NOTICE_TONES,
  setDocumentCountry,
  addPendingValue, removePendingValue, clearPendingValues,
  dismissWarning, visibleWarnings,
  setExportDir, startNewBatch, setMetaReview,
  askConfirm, answerConfirm,
} from "./state.js";

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
  // There is no prevStep() any more (BUILD-05 Phase 9): moving BACK goes through
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

// --- Phase 7: entity review reducers -----------------------------------------

import {
  addEntities, removeEntity,
  setEntityVariants, addManualVariant, acceptedEntities,
  addAllowTerm, removeAllowTerm, addPattern, removePattern, validPatterns,
} from "./state.js";

test("addEntities dedupes case-insensitively and defaults to accepted", () => {
  resetState();
  const added = addEntities([
    { category: "entity_names", canonical: "Alpine Trust" },
    { category: "entity_names", canonical: "ALPINE TRUST" }, // dup
    { category: "entity_names", canonical: "  " },           // blank
    { category: "person_names", canonical: "Marie Duval" },
  ]);
  assert.equal(added, 2);
  const s = getState();
  assert.equal(s.entities.length, 2);
  assert.equal(s.entities[0].status, "accepted");
});

test("acceptedEntities filters on status, as the belt to a removed brace", () => {
  // setEntityStatus() is gone (BUILD-05 Phase 9): the redesign has no denied
  // state, so nothing can produce one. The filter stays, because a row that
  // somehow arrived denied must still not reach the pipeline, and asserting it
  // directly is the only way to keep that guard honest now that no reducer
  // exercises it.
  resetState();
  addEntities([{ category: "entity_names", canonical: "Alpine Trust" }]);
  assert.equal(getState().entities[0].status, "accepted", "every new value is accepted");
  assert.equal(acceptedEntities().length, 1);

  setState({ entities: getState().entities.map((e) => ({ ...e, status: "denied" })) });
  assert.equal(acceptedEntities().length, 0, "a denied entity never reaches the pipeline");
});

test("manual variants dedupe and clear the expansion cache", () => {
  resetState();
  addEntities([{ category: "person_names", canonical: "Peter Stone", variants: ["Peter Stone"] }]);
  addManualVariant("person_names", "Peter Stone", "Pete");
  addManualVariant("person_names", "Peter Stone", "pete"); // dup, other case
  const e = getState().entities[0];
  assert.deepEqual(e.manualVariants, ["Pete"]);
  assert.equal(e.variants, null, "cache back to pending (null) so Go re-expands");
  setEntityVariants("person_names", "Peter Stone", ["Peter Stone", "Pete"]);
  assert.equal(getState().entities[0].variants.length, 2);
});

test("removeEntity deletes the row", () => {
  resetState();
  addEntities([{ category: "entity_names", canonical: "Alpine" }]);
  removeEntity("entity_names", "ALPINE");
  assert.equal(getState().entities.length, 0);
});

test("allowlist add/remove is case-insensitive on identity", () => {
  resetState();
  addAllowTerm("CSSF");
  addAllowTerm("cssf"); // dup
  assert.deepEqual(getState().allowlist, ["CSSF"]);
  removeAllowTerm("Cssf");
  assert.deepEqual(getState().allowlist, []);
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

// --- Phase 8: simple-replace rules + run request -----------------------------

import { addSimpleRule, removeSimpleRule, moveSimpleRule, buildRunRequest } from "./state.js";

test("simple rules: add validates, move reorders, remove deletes", () => {
  resetState();
  assert.equal(addSimpleRule({ find: "  " }), false, "empty needle rejected");
  addSimpleRule({ find: "a", replace: "1" });
  addSimpleRule({ find: "b", replace: "2", caseSensitive: true });
  assert.equal(getState().simpleRules.length, 2);

  assert.equal(moveSimpleRule(1, -1), true);
  assert.equal(getState().simpleRules[0].find, "b");
  assert.equal(moveSimpleRule(0, -1), false, "cannot move above the top");
  assert.equal(moveSimpleRule(5, 1), false, "out of range rejected");

  removeSimpleRule(0);
  assert.deepEqual(getState().simpleRules.map((r) => r.find), ["a"]);
});

test("buildRunRequest assembles only pipeline-ready inputs", () => {
  resetState();
  addEntities([
    { category: "entity_names", canonical: "Alpine" },
    { category: "person_names", canonical: "Denied Person" },
  ]);
  // No reducer can deny a value any more (BUILD-05 Phase 9), so the state is set
  // directly: the point of the test is that buildRunRequest FILTERS on status.
  setState({
    entities: getState().entities.map((e) =>
      e.canonical === "Denied Person" ? { ...e, status: "denied" } : e),
  });
  addAllowTerm("CSSF");
  addPattern("PRJ-[0-9]+", null);
  addPattern("[", "broken");
  addSimpleRule({ find: "x", replace: "y" });

  const req = buildRunRequest(true);
  assert.equal(req.useDeepScan, true);
  assert.deepEqual(req.entities, [{ category: "entity_names", canonical: "Alpine", manualVariants: [], excludedVariants: [] }]);
  assert.deepEqual(req.allowTerms, ["CSSF"]);
  assert.deepEqual(req.patterns, [{ expr: "PRJ-[0-9]+" }]);
  assert.equal(req.simpleRules.length, 1);
});

// --- Screen navigation (BUILD-02 Phase 2) -----------------------------------

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

test("applyPreset fills the expected switches per level", () => {
  // The tiers are ordered by how much ordinary text each risks catching, and
  // this walks all three. It mirrors engine.PresetSelection; the pairing itself
  // is enforced by ../category_parity_test.go.
  resetState();
  applyPreset("soft");
  let c = getState().settings.categories;
  assert.equal(c.email, true);
  assert.equal(c.identifier_names, true, "reference codes are near-PII, so soft has them");
  assert.equal(c.person_names, false, "soft leaves persons off");
  assert.equal(c.product_names, false);
  assert.equal(c.amount, false);

  applyPreset("medium");
  c = getState().settings.categories;
  assert.equal(c.person_names, true);
  assert.equal(c.product_names, true, "products and brands join at medium");
  assert.equal(c.brand_names, true);
  assert.equal(c.other_names, false, "the noisiest category waits for advanced");
  assert.equal(c.date, false, "medium leaves dates off");

  applyPreset("advanced");
  c = getState().settings.categories;
  assert.equal(c.date, true);
  assert.equal(c.amount, true);
  assert.equal(c.other_names, true);
  assert.equal(getState().settings.level, "advanced");
});

test("toggleCategory flips one switch and flags the selection as custom", () => {
  resetState();
  applyPreset("medium");
  assert.equal(selectionPresetName(getState().settings.categories), "medium");
  assert.equal(toggleCategory("email", false), true);
  const c = getState().settings.categories;
  assert.equal(c.email, false);
  assert.equal(c.phone, true, "other switches untouched");
  assert.equal(selectionPresetName(c), "custom");
  // Unknown keys are rejected.
  assert.equal(toggleCategory("no_such_category", true), false);
});

test("selectionPresetName recognises each exact preset", () => {
  resetState();
  for (const level of ["soft", "medium", "advanced"]) {
    assert.equal(selectionPresetName({
      ...presetCategories(level),
      ...countryIDCategories(getState().documentCountry),
    }), level);
  }
});

test("buildRunRequest carries the category selection", () => {
  resetState();
  applyPreset("soft");
  const req = buildRunRequest(false);
  // The preset plus the country's identifier switches, which is what the rail
  // actually shows and therefore what the pipeline must obey.
  assert.deepEqual(req.categories, {
    ...presetCategories("soft"),
    ...countryIDCategories(DEFAULT_COUNTRY),
  });
  toggleCategory("iban", false);
  assert.equal(buildRunRequest(false).categories.iban, false);
});

// --- Local-AI gating (BUILD-02 Phase 6) ---------------------------------------

test("llmEnabled requires BOTH the toggle and a reachable Ollama", () => {
  resetState();
  setState({ ollama: { available: true, models: [], detail: "" } });
  setUseAI(false);
  assert.equal(llmEnabled(), false, "toggle off blocks AI even with Ollama up");
  setUseAI(true);
  assert.equal(llmEnabled(), true);
  setState({ ollama: { available: false, models: [], detail: "" } });
  assert.equal(llmEnabled(), false, "Ollama down blocks AI even with toggle on");
});

test("the detection routes start with Smart detection on and both AI routes off", () => {
  // BUILD-06: Smart detection needs nothing installed, so it runs by default.
  // Local AI is a route that hands the document to a model, so the user turns
  // it on themselves, even when Ollama is detected.
  resetState();
  assert.equal(getState().settings.useSmartDetect, true);
  assert.equal(getState().settings.useAI, false);
  // There is no useCloudAI: the cloud route is not built, and the rail renders a
  // static panel rather than reading a flag (BRIDGE.md).
  assert.ok(!("useCloudAI" in getState().settings),
    "a setting Go discards and nothing reads must not exist");
  setState({ ollama: { available: true, models: [], detail: "" } });
  assert.equal(getState().settings.useAI, false, "detecting Ollama must not switch the route on");
});

test("detectionRoutesOn counts the ways of finding values that are enabled", () => {
  resetState();
  assert.equal(detectionRoutesOn(), 1, "Smart detection alone");
  setState({ ollama: { available: true, models: [], detail: "" } });
  setUseAI(true);
  assert.equal(detectionRoutesOn(), 2);
  setUseSmartDetect(false);
  assert.equal(detectionRoutesOn(), 1, "local AI alone");
  setUseAI(false);
  assert.equal(detectionRoutesOn(), 0, "nothing to run, and the UI must say so");
});

// --- Candidate review gate (BUILD-02 Phase 9b) --------------------------------

test("candidates wait for explicit accept; accept moves them to entities", () => {
  resetState();
  const added = addCandidates([
    { text: "Alpine Trust", category: "entity_names", count: 3 },
    { text: "Marie Duval", category: "person_names" },
  ], "smart");
  assert.equal(added, 2);
  assert.equal(getState().entities.length, 0, "nothing reaches entities without accept");

  assert.equal(acceptCandidate("Alpine Trust"), true);
  assert.equal(getState().entities.length, 1);
  assert.equal(getState().entities[0].category, "entity_names");
  assert.equal(getState().candidates.length, 1, "accepted candidate leaves the review list");
});

test("reject removes, duplicates and existing entities are skipped", () => {
  resetState();
  addEntities([{ category: "entity_names", canonical: "Known Corp" }]);
  const added = addCandidates([
    { text: "Known Corp", category: "entity_names" }, // already an entity
    { text: "Fresh Co", category: "entity_names" },
    { text: "fresh co", category: "entity_names" },   // case-insensitive dup
  ], "local-ai");
  assert.equal(added, 1);
  rejectCandidate("Fresh Co");
  assert.equal(getState().candidates.length, 0);
  assert.equal(getState().entities.length, 1, "reject never touches entities");
});

// --- Variant regrouping (BUILD-02 Phase 9d) ------------------------------------

test("moveVariant happy path: source excludes, target gains, both re-pend", () => {
  resetState();
  addEntities([
    { category: "person_names", canonical: "Jean Muller" },
    { category: "person_names", canonical: "J Muller Sr" },
  ]);
  setEntityVariants("person_names", "Jean Muller", ["Jean Muller", "J. Muller"]);
  setEntityVariants("person_names", "J Muller Sr", ["J Muller Sr"]);

  assert.equal(moveVariant("person_names", "Jean Muller", "person_names", "J Muller Sr", "J. Muller"), true);
  const from = getState().entities.find((e) => e.canonical === "Jean Muller");
  const to = getState().entities.find((e) => e.canonical === "J Muller Sr");
  assert.deepEqual(from.excludedVariants, ["J. Muller"]);
  assert.deepEqual(to.manualVariants, ["J. Muller"]);
  assert.equal(from.variants, null, "source re-expands");
  assert.equal(to.variants, null, "target re-expands");
});

test("moveVariant rejects self-drops, unknown rows and absent variants", () => {
  resetState();
  addEntities([
    { category: "person_names", canonical: "Jean Muller" },
    { category: "entity_names", canonical: "Alpine" },
  ]);
  setEntityVariants("person_names", "Jean Muller", ["Jean Muller"]);
  setEntityVariants("entity_names", "Alpine", ["Alpine"]);

  assert.equal(moveVariant("person_names", "Jean Muller", "person_names", "Jean Muller", "Jean Muller"), false, "self-drop");
  assert.equal(moveVariant("person_names", "Ghost", "entity_names", "Alpine", "x"), false, "unknown source");
  assert.equal(moveVariant("person_names", "Jean Muller", "entity_names", "Alpine", "Not A Variant"), false, "absent variant");
  // No state damage from rejected moves.
  assert.equal(getState().entities.find((e) => e.canonical === "Alpine").manualVariants.length, 0);
});

test("moveVariant across categories re-pends only the two touched rows", () => {
  resetState();
  addEntities([
    { category: "person_names", canonical: "Jean Muller" },
    { category: "entity_names", canonical: "Alpine" },
    { category: "entity_names", canonical: "Borealis" },
  ]);
  setEntityVariants("person_names", "Jean Muller", ["Jean Muller", "Muller"]);
  setEntityVariants("entity_names", "Alpine", ["Alpine"]);
  setEntityVariants("entity_names", "Borealis", ["Borealis"]);

  assert.equal(moveVariant("person_names", "Jean Muller", "entity_names", "Alpine", "Muller"), true);
  const untouched = getState().entities.find((e) => e.canonical === "Borealis");
  assert.deepEqual(untouched.variants, ["Borealis"], "third row untouched");
  const pendingNames = getState().entities.filter((e) => e.variants === null).map((e) => e.canonical).sort();
  assert.deepEqual(pendingNames, ["Alpine", "Jean Muller"]);
});

// --- Reassignment helpers (BUILD-02 Phase 10d) --------------------------------

test("entityAutocomplete ranks prefix matches before substring matches", () => {
  resetState();
  addEntities([
    { category: "person_names", canonical: "Jean Muller" },
    { category: "person_names", canonical: "Muller Freres" },
    { category: "entity_names", canonical: "Amullertech" },
  ]);
  const got = entityAutocomplete("muller");
  assert.equal(got.length, 3);
  assert.equal(got[0].canonical, "Muller Freres", "prefix match first");
  assert.ok(got.slice(1).map((m) => m.canonical).includes("Jean Muller"));
  assert.deepEqual(entityAutocomplete(""), []);
});

test("reassignOriginal removes a standalone entity and adds the variant", () => {
  resetState();
  addEntities([
    { category: "person_names", canonical: "Jean Muller" },
    { category: "person_names", canonical: "J. Muller" }, // earned its own placeholder
  ]);
  assert.equal(reassignOriginal("J. Muller", "person_names", "Jean Muller"), true);
  const entities = getState().entities;
  assert.equal(entities.length, 1, "the standalone entity is folded in");
  assert.deepEqual(entities[0].manualVariants, ["J. Muller"]);
  assert.equal(entities[0].variants, null, "target re-expands");
  // Unknown target rejected, state untouched.
  assert.equal(reassignOriginal("X", "person_names", "Ghost"), false);
});

// --- The four-step wizard (BUILD-05 Phase 2) ------------------------------

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
  // BUILD-05 decision 1: there is no migration table any more. A session file
  // this build does not understand is refused by the loader, so the only
  // tokens that reach here are corrupted or hand-edited ones, and the answer
  // is the one step that is always reachable.
  for (const bad of ["configure", "values", "run", "entities", "teleport", "", undefined, null]) {
    assert.equal(knownStep(bad), "import", `knownStep(${JSON.stringify(bad)})`);
  }
});

// --- Documentation is no longer a screen (BUILD-04 CR6) --------------------

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

// --- BUILD-04 Phase 4: surfaced recognizers, groups, confidence ------------

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

test("the extended recognizers are on at every preset (CR9)", () => {
  // Mirrors engine.PresetSelection; the Go side is pinned in
  // category_parity_test.go, and the two must not drift.
  for (const level of ["soft", "medium", "advanced"]) {
    const sel = presetCategories(level);
    for (const key of EXTENDED_PII_CATEGORIES) {
      assert.equal(sel[key], true, `${key} must be on at ${level}`);
    }
  }
});

test("adding the new categories did not change what a preset switches ON", () => {
  // Regression guard for the preset semantics themselves: soft must still
  // exclude person names, dates, amounts, organisations and places.
  const soft = presetCategories("soft");
  assert.equal(soft.person_names, false);
  const medium = presetCategories("medium");
  assert.equal(medium.person_names, true);
  assert.equal(medium.date, false);
  assert.equal(medium.amount, false);
  const advanced = presetCategories("advanced");
  for (const key of ALL_CATEGORIES) {
    assert.equal(advanced[key], true, `thorough must switch ${key} on`);
  }
});

test("setCategoryGroup flips exactly the given keys in one change (CR10)", () => {
  resetState();
  let notifications = 0;
  const unsub = subscribe(() => { notifications++; });

  const group = [...EXTENDED_PII_CATEGORIES];
  const changed = setCategoryGroup(group, false);
  assert.equal(changed, group.length, "all eight started on and must all flip");
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

test("a deselected group makes the selection Custom (CR10)", () => {
  resetState();
  applyPreset("medium");
  assert.equal(selectionPresetName(getState().settings.categories), "medium");
  setCategoryGroup(EXTENDED_PII_CATEGORIES, false);
  assert.equal(selectionPresetName(getState().settings.categories), "custom");
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

// --- BUILD-04 Phase 5: smart-detection tuning and bulk deny ----------------

import {
  setSmartDetectOptions, smartDetectOptions,
  SMART_DETECT_DEFAULTS,
} from "./state.js";

/** seedCandidates() puts a mixed review list in the store. */
function seedCandidates() {
  resetState();
  addCandidates([
    { text: "Marie Duval", category: "person_names", count: 7 },
    { text: "Anouk Berger", category: "person_names", count: 3 },
    { text: "Alpine Trust", category: "entity_names", count: 3 },
  ], "smart");
}

test("smart detection ships with the stricter defaults (CR13)", () => {
  resetState();
  assert.deepEqual(getState().settings.smartDetect, SMART_DETECT_DEFAULTS);
  assert.equal(SMART_DETECT_DEFAULTS.excludeCommonWords, true);
  assert.ok(SMART_DETECT_DEFAULTS.minLength > 0);
  // Requiring two occurrences would throw away single-sighting full
  // names, which are the most valuable thing smart detection finds.
  assert.equal(SMART_DETECT_DEFAULTS.minOccurrences, 1);
});

test("setSmartDetectOptions merges a partial patch (CR13)", () => {
  resetState();
  const out = setSmartDetectOptions({ minLength: 6 });
  assert.equal(out.minLength, 6);
  assert.equal(out.excludeCommonWords, SMART_DETECT_DEFAULTS.excludeCommonWords,
    "untouched options keep their value");
  assert.equal(getState().settings.smartDetect.minLength, 6);
});

test("setSmartDetectOptions accepts the permissive extreme (CR13)", () => {
  // Turning every filter off must be reachable: that is the escape hatch
  // for a user who would rather review too much than miss something.
  resetState();
  const out = setSmartDetectOptions({
    minLength: 0, minOccurrences: 0, excludeCommonWords: false, minConfidence: 0,
  });
  assert.deepEqual(out, {
    minLength: 0, minOccurrences: 0, excludeCommonWords: false, minConfidence: 0,
  });
});

test("setSmartDetectOptions ignores invalid values rather than storing them (CR13)", () => {
  resetState();
  const before = { ...getState().settings.smartDetect };
  const out = setSmartDetectOptions({
    minLength: -1, minOccurrences: 2.5, excludeCommonWords: "yes", minConfidence: 4,
  });
  assert.deepEqual(out, before, "every invalid value must be ignored");
  // Unknown keys are simply not carried over.
  setSmartDetectOptions({ nonsense: 1 });
  assert.equal(getState().settings.smartDetect.nonsense, undefined);
});

test("smartDetectOptions fills defaults for a session without the block (CR13)", () => {
  // A session file written before BUILD-04 has no smartDetect at all.
  resetState();
  setState({ settings: { ...getState().settings, smartDetect: undefined } });
  assert.deepEqual(smartDetectOptions(), SMART_DETECT_DEFAULTS);
  // A partially written block is completed rather than rejected.
  setState({ settings: { ...getState().settings, smartDetect: { minLength: 9 } } });
  assert.deepEqual(smartDetectOptions(), { ...SMART_DETECT_DEFAULTS, minLength: 9 });
});

// --- BUILD-04 Phase 6: per-step reset (CR16) -------------------------------

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
  addEntities([{ category: "person_names", canonical: "Marie Duval" }]);
  addCandidates([{ text: "Alpine Trust", category: "entity_names" }], "smart");
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
  // The one non-negotiable rule of BUILD-04 section 4.2.
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
  // Identify owns BOTH halves of its screen now (BUILD-05 Phase 2): the rail's
  // choices, which used to be a Configure step, and the workspace's values.
  fullSession();
  assert.equal(getState().settings.categories.email, false);
  assert.equal(getState().settings.minConfidence, 0.9);

  assert.equal(resetStep("identify"), true);
  const s = getState();
  // The preset, with the DEFAULT COUNTRY's identifier switches on top. Every
  // preset switches all three country-specific identifiers on, because to the
  // engine they are hard PII, so the reset has to re-apply the country or the
  // rail would show Luxembourg beside an active German tax identifier.
  assert.deepEqual(s.settings.categories, {
    ...presetCategories(s.settings.level),
    ...countryIDCategories(DEFAULT_COUNTRY),
  });
  assert.equal(s.documentCountry, DEFAULT_COUNTRY);
  assert.equal(s.settings.minConfidence, 0);
  assert.deepEqual(s.settings.smartDetect, SMART_DETECT_DEFAULTS);
});

test("resetStep(identify) clears values, suggestions, patterns and discovery", () => {
  fullSession();
  assert.equal(resetStep("identify"), true);
  const s = getState();
  assert.deepEqual(s.entities, []);
  assert.deepEqual(s.candidates, []);
  assert.deepEqual(s.patterns, []);
  assert.equal(s.discovery, null);
  // The run is untouched: it belongs to the next step.
  assert.ok(s.results);
});

test("resetStep(identify) keeps the machine's connection settings", () => {
  // Port, model and the AI toggle describe the machine, not this batch of
  // documents, so stepping back must not make the user reconfigure Ollama.
  fullSession();
  setState({ settings: { ...getState().settings, ollamaPort: 12345, model: "custom:7b", useAI: true } });
  resetStep("identify");
  const s = getState();
  assert.equal(s.settings.ollamaPort, 12345);
  assert.equal(s.settings.model, "custom:7b");
  assert.equal(s.settings.useAI, true);
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
  assert.equal(s.entities.length, 1);
});

test("resetStep(anonymise) clears the editing surfaces the run screen owns", () => {
  // The find-and-replace rules and the dismissed warnings only exist once
  // there is a result to edit, so they are the Anonymise step's own data.
  fullSession();
  setState({
    simpleRules: [{ find: "x", replace: "[CUSTOM_1]", caseSensitive: true }],
    dismissedWarnings: ["skipped-pdf"],
  });
  resetStep("anonymise");
  const s = getState();
  assert.deepEqual(s.simpleRules, []);
  assert.deepEqual(s.dismissedWarnings, []);
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

// --- Notices (BUILD-05 Phase 1) ---------------------------------------

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

// --- The in-app confirm (BUILD-05 decision 10) ------------------------

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

// --- The document country (BUILD-05 Phase 5, decision 2) ----------------

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
  // presetCategories() switches all three national identifiers ON, because to
  // the engine they are hard PII. The country is an ORTHOGONAL choice, so
  // applyPreset re-applies it: picking Soft on a German document must not
  // silently start looking for Spanish tax numbers.
  resetState();
  setDocumentCountry("DE");
  for (const level of ["soft", "medium", "advanced"]) {
    applyPreset(level);
    const categories = getState().settings.categories;
    assert.equal(categories.de_steuer_id, true, `${level}: Germany's identifier stays on`);
    assert.equal(categories.es_nif, false, `${level}: Spain's must not come back`);
    assert.equal(categories.uk_nhs, false, `${level}: the UK's must not come back`);
  }
});

test("the preset chip does not read Custom just because of the country", () => {
  // The three country-driven identifiers are excluded from the preset
  // comparison. Otherwise a Luxembourg document, where all three are off, would
  // show "Custom" the instant the user picked Standard, which makes the chips
  // look broken.
  resetState();
  for (const code of COUNTRIES.map((c) => c.code)) {
    setDocumentCountry(code);
    for (const level of ["soft", "medium", "advanced"]) {
      applyPreset(level);
      assert.equal(selectionPresetName(getState().settings.categories), level,
        `${code} + ${level} must still read as ${level}`);
    }
  }
});

test("a real category change still reads as Custom", () => {
  // The exclusion must not swallow genuine divergence.
  resetState();
  applyPreset("medium");
  toggleCategory("email", false);
  assert.equal(selectionPresetName(getState().settings.categories), "custom");
});

// --- The Anonymise screen's editing surfaces (BUILD-05 Phase 7) ----------

test("addPendingValue records the chip AND adds the value to replace", () => {
  // The two go together: the entity is what makes a fast re-run replace the
  // value, the chip is what says it will not happen until the user presses it.
  resetState();
  assert.equal(addPendingValue("person_names", "P. Stone"), true);
  const s = getState();
  assert.deepEqual(s.pendingValues, [{ category: "person_names", text: "P. Stone" }]);
  assert.equal(s.entities.length, 1);
  assert.equal(s.entities[0].canonical, "P. Stone");
  assert.equal(s.entities[0].category, "person_names");
});

test("addPendingValue trims, and refuses an empty value", () => {
  resetState();
  assert.equal(addPendingValue("person_names", "  P. Stone  "), true);
  assert.equal(getState().pendingValues[0].text, "P. Stone");
  for (const empty of ["", "   ", undefined, null]) {
    assert.equal(addPendingValue("person_names", empty), false, JSON.stringify(empty));
  }
  assert.equal(getState().pendingValues.length, 1);
});

test("addPendingValue refuses a duplicate, case-insensitively", () => {
  resetState();
  addPendingValue("person_names", "P. Stone");
  assert.equal(addPendingValue("person_names", "p. stone"), false);
  assert.equal(addPendingValue("entity_names", "P. STONE"), false,
    "the category does not make it a different value");
  assert.equal(getState().pendingValues.length, 1);
});

test("addPendingValue refuses a value that is already on the list to replace", () => {
  // A chip for something already being replaced would promise a change the
  // re-run cannot make.
  resetState();
  addEntities([{ category: "entity_names", canonical: "Aurora Group" }]);
  assert.equal(addPendingValue("entity_names", "Aurora Group"), false);
  assert.deepEqual(getState().pendingValues, []);
});

test("removePendingValue undoes the WHOLE of what Add did", () => {
  // The chip's cross has to remove the entity too, or the value would keep being
  // replaced with no chip left to say so.
  resetState();
  addPendingValue("person_names", "P. Stone");
  addPendingValue("entity_names", "Aurora Group");
  removePendingValue("P. Stone");
  const s = getState();
  assert.deepEqual(s.pendingValues.map((p) => p.text), ["Aurora Group"]);
  assert.deepEqual(s.entities.map((e) => e.canonical), ["Aurora Group"]);
});

test("removePendingValue ignores a value that is not pending", () => {
  resetState();
  addEntities([{ category: "entity_names", canonical: "Aurora Group" }]);
  removePendingValue("Aurora Group");
  assert.equal(getState().entities.length, 1,
    "a value that was never pending must not be removed by the chip handler");
});

test("clearPendingValues empties the chips but KEEPS the values", () => {
  // The re-run just used those entities; dropping them would undo the re-run.
  resetState();
  addPendingValue("person_names", "P. Stone");
  addPendingValue("entity_names", "Aurora Group");
  assert.equal(clearPendingValues(), 2);
  assert.deepEqual(getState().pendingValues, []);
  assert.equal(getState().entities.length, 2);
  assert.equal(clearPendingValues(), 0, "clearing an empty list is a no-op");
});

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

// --- startNewBatch (BUILD-05 Phase 8) ------------------------------------
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
  addEntities([{ category: "person_names", canonical: "Marie Duval" }]);
  addCandidates([{ text: "Thomas Berger", category: "person_names" }], "smart");
  addPattern("INV-\\d{6}");
  addSimpleRule({ find: "4471-B", replace: "[CUSTOM_1]", caseSensitive: true });
  addPendingValue("entity_names", "Aurora Group");
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
  assert.deepEqual(s.entities, []);
  assert.deepEqual(s.candidates, []);
  assert.deepEqual(s.patterns, []);
  assert.deepEqual(s.simpleRules, []);
  assert.deepEqual(s.pendingValues, []);
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
    smartDetect: { ...getState().settings.smartDetect },
    ollamaPort: getState().settings.ollamaPort,
    country: getState().documentCountry,
    allowlist: [...getState().allowlist],
    exportDir: getState().exportDir,
  };
  startNewBatch();
  const s = getState();
  assert.deepEqual(s.settings.categories, before.categories);
  assert.equal(s.settings.minConfidence, before.minConfidence);
  assert.deepEqual(s.settings.smartDetect, before.smartDetect);
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
  assert.equal(cleared.rules, 1);
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

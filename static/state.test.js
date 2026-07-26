// state.test.js — dev-time unit tests for the store, runnable with
// `node --test static/` (node is present on CI runners; this is NOT an npm
// dependency — see BUILD.md Phase 6).
//
// Only pure logic is tested here: state transitions, navigation guards and
// reducers. Views stay logic-free precisely so this file covers what
// matters without a DOM.

import test from "node:test";
import assert from "node:assert/strict";

import {
  getState, setState, resetState, subscribe,
  WIZARD_STEPS, canGoTo, goTo, nextStep, prevStep,
  goToScreen, setImportSplit,
  applyPreset, toggleCategory, selectionPresetName, presetCategories,
  setUseAI, defaultUseAIFromProbe, llmEnabled,
  addCandidates, acceptCandidate, rejectCandidate, updateCandidate, acceptAllInCategory,
  moveVariant, entityAutocomplete, reassignOriginal,
  applyImportResult,
} from "./state.js";

test("setState merges and notifies subscribers", () => {
  resetState();
  let seen = null;
  const unsub = subscribe((s) => { seen = s.step; });
  setState({ step: "configure" });
  assert.equal(seen, "configure");
  unsub();
});

test("guards: no step beyond import without documents", () => {
  resetState();
  assert.equal(canGoTo("import"), true);
  for (const step of WIZARD_STEPS.slice(1)) {
    assert.equal(canGoTo(step), false, `${step} must be locked without documents`);
  }
  assert.equal(goTo("configure"), false);
  assert.equal(getState().step, "import");
});

test("guards: documents unlock middle steps, results unlock export", () => {
  resetState();
  setState({ documents: [{ name: "a.txt" }] });
  assert.equal(canGoTo("configure"), true);
  assert.equal(canGoTo("run"), true);
  assert.equal(canGoTo("export"), false, "export needs results");
  setState({ results: { documents: [] } });
  assert.equal(canGoTo("export"), true);
});

test("guards: unknown step is rejected", () => {
  resetState();
  assert.equal(canGoTo("teleport"), false);
});

test("next/prev walk the wizard linearly and respect guards", () => {
  resetState();
  assert.equal(nextStep(), false, "cannot advance with no documents");
  setState({ documents: [{ name: "a.txt" }] });
  assert.equal(nextStep(), true);
  assert.equal(getState().step, "configure");
  assert.equal(prevStep(), true);
  assert.equal(getState().step, "import");
  assert.equal(prevStep(), false, "cannot go back from the first step");
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
  addEntities, setEntityStatus, editEntity, removeEntity,
  setEntityVariants, addManualVariant, acceptedEntities,
  addAllowTerm, removeAllowTerm, addPattern, removePattern, validPatterns,
} from "./state.js";

test("addEntities dedupes case-insensitively and defaults to accepted", () => {
  resetState();
  const added = addEntities([
    { category: "client_names", canonical: "Alpine Trust" },
    { category: "client_names", canonical: "ALPINE TRUST" }, // dup
    { category: "client_names", canonical: "  " },           // blank
    { category: "person_names", canonical: "Marie Duval" },
  ]);
  assert.equal(added, 2);
  const s = getState();
  assert.equal(s.entities.length, 2);
  assert.equal(s.entities[0].status, "accepted");
});

test("accept/deny reducers flip status; acceptedEntities filters", () => {
  resetState();
  addEntities([{ category: "client_names", canonical: "Alpine Trust" }]);
  setEntityStatus("client_names", "Alpine Trust", "denied");
  assert.equal(getState().entities[0].status, "denied");
  assert.equal(acceptedEntities().length, 0, "denied entities never reach the pipeline");
  setEntityStatus("client_names", "ALPINE trust", "accepted"); // case-insensitive key
  assert.equal(acceptedEntities().length, 1);
});

test("editEntity renames, clears variants, and rejects collisions", () => {
  resetState();
  addEntities([
    { category: "client_names", canonical: "Alpine", variants: ["Alpine"] },
    { category: "client_names", canonical: "Borealis" },
  ]);
  assert.equal(editEntity("client_names", "Alpine", "Alpine Trust"), true);
  const e = getState().entities[0];
  assert.equal(e.canonical, "Alpine Trust");
  assert.equal(e.variants, null, "variants must be back to pending (null) after a rename");
  assert.equal(editEntity("client_names", "Alpine Trust", "borealis"), false, "collision rejected");
  assert.equal(editEntity("client_names", "Alpine Trust", "   "), false, "blank rejected");
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
  addEntities([{ category: "client_names", canonical: "Alpine" }]);
  removeEntity("client_names", "ALPINE");
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
    { category: "client_names", canonical: "Alpine" },
    { category: "person_names", canonical: "Denied Person" },
  ]);
  setEntityStatus("person_names", "Denied Person", "denied");
  addAllowTerm("CSSF");
  addPattern("PRJ-[0-9]+", null);
  addPattern("[", "broken");
  addSimpleRule({ find: "x", replace: "y" });

  const req = buildRunRequest(true);
  assert.equal(req.useDeepScan, true);
  assert.deepEqual(req.entities, [{ category: "client_names", canonical: "Alpine", manualVariants: [], excludedVariants: [] }]);
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
  goTo("configure");
  goToScreen("home");
  goToScreen("wizard");
  assert.equal(getState().step, "configure");
  assert.equal(getState().documents.length, 1);
});

test("setImportSplit clamps and rejects non-numbers", () => {
  resetState();
  const cases = [
    [0, 0.2],
    [0.1, 0.2],
    [0.5, 0.5],
    [0.95, 0.8],
  ];
  for (const [input, want] of cases) {
    assert.equal(setImportSplit(input), want, `split(${input})`);
    assert.equal(getState().importSplit, want);
  }
  // NaN and non-numbers are rejected, leaving the stored value untouched.
  setImportSplit(0.5);
  assert.equal(setImportSplit(NaN), null);
  assert.equal(setImportSplit("0.7"), null);
  assert.equal(getState().importSplit, 0.5);
});

// --- Category presets and granular switches (BUILD-02 Phase 3) ---------------

test("applyPreset fills the expected switches per level", () => {
  resetState();
  applyPreset("soft");
  let c = getState().settings.categories;
  assert.equal(c.email, true);
  assert.equal(c.person_names, false, "soft leaves persons off");
  assert.equal(c.amount, false);
  applyPreset("medium");
  c = getState().settings.categories;
  assert.equal(c.person_names, true);
  assert.equal(c.date, false, "medium leaves dates off");
  applyPreset("advanced");
  c = getState().settings.categories;
  assert.equal(c.date, true);
  assert.equal(c.organisation_names, true);
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
  for (const level of ["soft", "medium", "advanced"]) {
    assert.equal(selectionPresetName(presetCategories(level)), level);
  }
});

test("buildRunRequest carries the category selection", () => {
  resetState();
  applyPreset("soft");
  const req = buildRunRequest(false);
  assert.deepEqual(req.categories, presetCategories("soft"));
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

test("defaultUseAIFromProbe fills the default once, never overrides a choice", () => {
  resetState();
  assert.equal(getState().settings.useAI, null);
  defaultUseAIFromProbe(true);
  assert.equal(getState().settings.useAI, true, "default follows availability");
  // A later probe result must not flip an established value.
  defaultUseAIFromProbe(false);
  assert.equal(getState().settings.useAI, true);
  // An explicit user choice survives everything.
  setUseAI(false);
  defaultUseAIFromProbe(true);
  assert.equal(getState().settings.useAI, false);
});

// --- Candidate review gate (BUILD-02 Phase 9b) --------------------------------

test("candidates wait for explicit accept; accept moves them to entities", () => {
  resetState();
  const added = addCandidates([
    { text: "Alpine Trust", category: "client_names", count: 3 },
    { text: "Marie Duval", category: "person_names" },
  ], "smart");
  assert.equal(added, 2);
  assert.equal(getState().entities.length, 0, "nothing reaches entities without accept");

  assert.equal(acceptCandidate("Alpine Trust"), true);
  assert.equal(getState().entities.length, 1);
  assert.equal(getState().entities[0].category, "client_names");
  assert.equal(getState().candidates.length, 1, "accepted candidate leaves the review list");
});

test("reject removes, duplicates and existing entities are skipped", () => {
  resetState();
  addEntities([{ category: "client_names", canonical: "Known Corp" }]);
  const added = addCandidates([
    { text: "Known Corp", category: "client_names" }, // already an entity
    { text: "Fresh Co", category: "client_names" },
    { text: "fresh co", category: "client_names" },   // case-insensitive dup
  ], "local-ai");
  assert.equal(added, 1);
  rejectCandidate("Fresh Co");
  assert.equal(getState().candidates.length, 0);
  assert.equal(getState().entities.length, 1, "reject never touches entities");
});

test("edit then accept uses the edited text and category", () => {
  resetState();
  addCandidates([{ text: "alpin trust", category: "person_names" }], "smart");
  assert.equal(updateCandidate("alpin trust", { text: "Alpine Trust", category: "client_names" }), true);
  // A collision with another candidate is rejected.
  addCandidates([{ text: "Other Co", category: "client_names" }], "smart");
  assert.equal(updateCandidate("Other Co", { text: "Alpine Trust" }), false);

  acceptCandidate("Alpine Trust");
  const e = getState().entities[0];
  assert.equal(e.canonical, "Alpine Trust");
  assert.equal(e.category, "client_names");
});

test("bulk accept drains one category only", () => {
  resetState();
  addCandidates([
    { text: "C One", category: "client_names" },
    { text: "C Two", category: "client_names" },
    { text: "P One", category: "person_names" },
  ], "smart");
  assert.equal(acceptAllInCategory("client_names"), 2);
  assert.equal(getState().entities.length, 2);
  assert.equal(getState().candidates.length, 1);
  assert.equal(getState().candidates[0].text, "P One");
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
    { category: "client_names", canonical: "Alpine" },
  ]);
  setEntityVariants("person_names", "Jean Muller", ["Jean Muller"]);
  setEntityVariants("client_names", "Alpine", ["Alpine"]);

  assert.equal(moveVariant("person_names", "Jean Muller", "person_names", "Jean Muller", "Jean Muller"), false, "self-drop");
  assert.equal(moveVariant("person_names", "Ghost", "client_names", "Alpine", "x"), false, "unknown source");
  assert.equal(moveVariant("person_names", "Jean Muller", "client_names", "Alpine", "Not A Variant"), false, "absent variant");
  // No state damage from rejected moves.
  assert.equal(getState().entities.find((e) => e.canonical === "Alpine").manualVariants.length, 0);
});

test("moveVariant across categories re-pends only the two touched rows", () => {
  resetState();
  addEntities([
    { category: "person_names", canonical: "Jean Muller" },
    { category: "client_names", canonical: "Alpine" },
    { category: "client_names", canonical: "Borealis" },
  ]);
  setEntityVariants("person_names", "Jean Muller", ["Jean Muller", "Muller"]);
  setEntityVariants("client_names", "Alpine", ["Alpine"]);
  setEntityVariants("client_names", "Borealis", ["Borealis"]);

  assert.equal(moveVariant("person_names", "Jean Muller", "client_names", "Alpine", "Muller"), true);
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
    { category: "client_names", canonical: "Amullertech" },
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

// --- Step token rename and migration (BUILD-04 CR3) ------------------------

import { migrateStep, LEGACY_STEP_TOKENS } from "./state.js";
import { STEP_BANNERS } from "./copy.js";

test("the wizard's third step token is values, not entities", () => {
  assert.deepEqual(WIZARD_STEPS, ["import", "configure", "values", "run", "export"]);
});

test("migrateStep maps the legacy entities token onto values", () => {
  assert.equal(migrateStep("entities"), "values");
  assert.equal(LEGACY_STEP_TOKENS.entities, "values");
});

test("migrateStep passes current tokens through untouched", () => {
  for (const step of WIZARD_STEPS) {
    assert.equal(migrateStep(step), step);
  }
});

test("migrateStep falls back to import for unknown or missing tokens", () => {
  // A hand-edited or corrupted session must never strand the wizard on a
  // step that does not exist; import is the only always-reachable step.
  assert.equal(migrateStep("teleport"), "import");
  assert.equal(migrateStep(undefined), "import");
  assert.equal(migrateStep(""), "import");
});

test("every wizard step has a banner keyed by its current token", () => {
  for (const step of WIZARD_STEPS) {
    assert.ok(STEP_BANNERS[step], `no step banner for ${step}`);
  }
});

test("no user-visible step wording still says Entities", () => {
  for (const [step, banner] of Object.entries(STEP_BANNERS)) {
    assert.doesNotMatch(banner.title, /entit/i, `${step} title`);
    assert.doesNotMatch(banner.body, /entit/i, `${step} body`);
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
  setCategoryGroup, setMinConfidence, clearAllowlist,
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

test("clearAllowlist empties the list and reports the count (CR11)", () => {
  resetState();
  for (const term of ["CSSF", "EUR", "GDPR"]) addAllowTerm(term);
  assert.equal(getState().allowlist.length, 3);
  assert.equal(clearAllowlist(), 3);
  assert.deepEqual(getState().allowlist, []);
  // Clearing an empty list is a no-op that reports zero.
  assert.equal(clearAllowlist(), 0);
});

test("clearAllowlist touches nothing but the allowlist (CR11)", () => {
  resetState();
  setState({ documents: [{ name: "a.txt" }] });
  addAllowTerm("CSSF");
  addEntities([{ category: "client_names", canonical: "Alpine Trust" }]);
  clearAllowlist();
  assert.equal(getState().entities.length, 1);
  assert.equal(getState().documents.length, 1);
});

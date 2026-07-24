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
  assert.deepEqual(e.variants, [], "variants must be re-expanded after a rename");
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
  assert.deepEqual(e.variants, [], "cache cleared so Go re-expands");
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
  assert.deepEqual(req.entities, [{ category: "client_names", canonical: "Alpine", manualVariants: [] }]);
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
  assert.equal(goToScreen("docs"), true);
  assert.equal(getState().screen, "docs");
  assert.equal(goToScreen("settings"), false);
  assert.equal(getState().screen, "docs");
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

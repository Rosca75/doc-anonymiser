// nav.test.js, tests for nav.js, THE one module that moves the wizard
// (BUILD-05 Phase 2).
//
// Why this file exists: nav.js had no unit test at all, and
// ../frontend_tests_test.go said so out loud. It is not a module that deserved
// an exemption. The step bar and all four screen footers navigate through it, so
// a wrong answer here is wrong on every screen at once, and the two pure
// functions underneath (previousStep, followingStep) are what every footer label
// is derived from.
//
// The wiring half is deliberately NOT tested here. navigateTo, advance and
// goBack ask askConfirm() and then mutate the live store, so testing them would
// mean stubbing the modal and the store: what they do is covered end to end by
// wizardflow.test.js on the store side, and by the layer 3 rendering harness on
// the screen side (docs/UITESTING.md). What IS tested here is everything that
// takes state as an argument and returns a value, which is the part a footer
// depends on.
//
// Run with `node --test "frontend/**/*.test.js"`.

import { test } from "node:test";
import assert from "node:assert/strict";

import { previousStep, followingStep, stepFooterHTML } from "./nav.js";
import { WIZARD_STEPS } from "./state.js";
import { NAV } from "./copy.js";
import { one, textOf, exists } from "./testhtml.js";

/**
 * stateOn(step, patch) is a state parked on one wizard step.
 *
 * Only the fields the navigation guard reads are filled in (state.js canGoTo:
 * documents and results), because a fuller fixture would suggest the guard looks
 * at more than it does.
 */
function stateOn(step, patch = {}) {
  return {
    step,
    documents: [{ name: "a.md", markdown: "text" }],
    results: null,
    ...patch,
  };
}

const FINISHED_RUN = {
  documents: [{ name: "a.md", anonymised: "[PERSON_1]", byCategory: { person_names: 1 } }],
  report: { values: [], byCategory: {}, totalReplacements: 1, documents: [] },
};

// --- previousStep / followingStep ------------------------------------------
//
// These are derived from WIZARD_STEPS rather than hardcoded, so the tests are
// derived from it too: a step inserted in the middle must not need this file
// edited to keep passing, or the test is pinning the list instead of the logic.

test("previousStep and followingStep walk WIZARD_STEPS in order", () => {
  WIZARD_STEPS.forEach((step, index) => {
    const s = stateOn(step);
    assert.equal(previousStep(s), index === 0 ? null : WIZARD_STEPS[index - 1],
      `previousStep on ${step}`);
    assert.equal(followingStep(s),
      index === WIZARD_STEPS.length - 1 ? null : WIZARD_STEPS[index + 1],
      `followingStep on ${step}`);
  });
});

test("the first step has nothing behind it and the last nothing ahead", () => {
  // The two ends are what a footer has to render differently: no back link on
  // Import, no CONTINUE on Export. Asserted explicitly because an off-by-one here
  // would show up as a link to nowhere.
  assert.equal(previousStep(stateOn(WIZARD_STEPS[0])), null);
  assert.equal(followingStep(stateOn(WIZARD_STEPS.at(-1))), null);
});

test("an unknown step token yields no neighbours rather than throwing", () => {
  // A corrupted persisted token reaches the footer before knownStep() has a say
  // (state.js, BUILD-05 decision 1). Returning null on both sides renders a
  // footer with no navigation, which is recoverable; throwing would leave a blank
  // screen with an exception behind it.
  const s = stateOn("teleport");
  assert.equal(previousStep(s), null);
  assert.equal(followingStep(s), null);
});

// --- stepFooterHTML --------------------------------------------------------

test("a middle step's footer labels both directions from copy.js", () => {
  // The point of stepFooterHTML: no screen spells "Back to Import" itself, so a
  // step rename reaches all four footers through copy.js NAV.
  const html = stepFooterHTML({ hint: "1 document ready" }, stateOn("identify"));

  assert.equal(textOf(html, "#step-back").trim(), NAV.back("import"));
  assert.equal(textOf(html, "#step-next").trim(), NAV.next("anonymise"));
  assert.match(html, /1 document ready/);
});

test("the first step's footer offers no way back", () => {
  const html = stepFooterHTML({ hint: "Add a document to begin" }, stateOn("import"));

  assert.ok(!exists(html, "#step-back"),
    "Import is the first step, so a back link would point at nothing");
  assert.equal(textOf(html, "#step-next").trim(), NAV.next("identify"));
});

test("the last step's footer offers no CONTINUE, and rightHTML replaces it", () => {
  // Export has nothing to continue to, and its right-hand action is "start a new
  // batch", which must not look like the CONTINUE button of the other three.
  const html = stepFooterHTML(
    { hint: "Saved", rightHTML: `<button id="new-batch">START A NEW BATCH</button>` },
    stateOn("export", { results: FINISHED_RUN }));

  assert.ok(!exists(html, "#step-next"), "there is no step after Export to continue to");
  assert.ok(exists(html, "#new-batch"), "rightHTML takes the primary slot instead");
  assert.equal(textOf(html, "#step-back").trim(), NAV.back("anonymise"));
});

test("the footer asks the navigation guard whether the next step is reachable", () => {
  // This is the assertion that matters most: the gate is state.js canGoTo, in one
  // place, not a condition each screen re-derives. Anonymise may not continue to
  // Export until there are results.
  const noResults = stepFooterHTML({}, stateOn("anonymise"));
  assert.ok("disabled" in one(noResults, "#step-next").attrs,
    "no results yet, so CONTINUE TO EXPORT must be disabled");

  const withResults = stepFooterHTML({}, stateOn("anonymise", { results: FINISHED_RUN }));
  assert.ok(!("disabled" in one(withResults, "#step-next").attrs),
    "a finished run unlocks the move to Export");
});

test("nothing imported keeps every forward move shut", () => {
  const html = stepFooterHTML({}, stateOn("import", { documents: [] }));
  assert.ok("disabled" in one(html, "#step-next").attrs,
    "the guard refuses every step past Import while no document is loaded");
});

test("an explicit nextDisabled overrides the guard", () => {
  // A screen that knows something the guard does not (a pending edit, a running
  // pass) can shut the move itself. The override has to win, or the option is a
  // lie.
  const html = stepFooterHTML(
    { nextDisabled: true, nextTitle: "Finish the current run first" },
    stateOn("identify"));

  assert.ok("disabled" in one(html, "#step-next").attrs);
  assert.equal(one(html, "#step-next").attrs.title, "Finish the current run first");
});

test("the footer's two buttons carry the ids wireStepFooter looks for", () => {
  // wireStepFooter attaches to #step-back and #step-next. If the markup and the
  // wiring ever disagree, the footer renders perfectly and does nothing at all,
  // which is a bug no other test in this folder would see.
  const html = stepFooterHTML({}, stateOn("identify"));
  assert.ok(one(html, "#step-back"), "wireStepFooter binds #step-back");
  assert.ok(one(html, "#step-next"), "wireStepFooter binds #step-next");
});

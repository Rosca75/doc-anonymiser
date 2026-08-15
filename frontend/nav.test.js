// nav.test.js, tests for nav.js, THE one module that moves the wizard
//
// Why this file exists: nav.js had no unit test at all, and
// ../frontend_tests_test.go said so out loud. It is not a module that deserved
// an exemption. The step bar and all four screen footers navigate through it, so
// a wrong answer here is wrong on every screen at once, and the two pure
// functions underneath (previousStep, followingStep) are what every footer label
// is derived from.
//
// Most of the wiring half is still NOT tested here: navigateTo, advance and
// goBack are covered end to end by wizardflow.test.js on the store side and by
// the layer 3 rendering harness on the screen side (docs/UITESTING.md). What IS
// tested here is everything that takes state as an argument and returns a value,
// which is the part a footer depends on, plus the keyboard shortcuts.
//
// The shortcuts are the exception because the bug they fixed was invisible
// anywhere else: the confirm is state-backed (state.js askConfirm resolves a
// promise when answerConfirm settles it), so a keystroke that skipped the
// question is provable here without a DOM, and nothing in wizardflow.test.js
// would ever have seen it.
//
// Run with `node --test "frontend/**/*.test.js"`.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  previousStep, followingStep, stepFooterHTML, shortcutStep, handleShortcut,
} from "./nav.js";
import { WIZARD_STEPS, getState, setState, resetState, answerConfirm } from "./state.js";
import { NAV } from "./copy.js";
import { one, textOf, exists } from "./testhtml.js";

/**
 * stateOn(step, patch) is a state parked on one wizard step.
 *
 * Only the fields the navigation guard reads are filled in (state.js canGoTo:
 * documents, candidates and results), because a fuller fixture would suggest the
 * guard looks at more than it does. `candidates` joined the list in
 * when the review gate became the guard's third rule.
 */
function stateOn(step, patch = {}) {
  return {
    step,
    documents: [{ name: "a.md", markdown: "text" }],
    candidates: [],
    results: null,
    ...patch,
  };
}

const FINISHED_RUN = {
  documents: [{ name: "a.md", anonymised: "[PERSON_1]", byCategory: { person_names: 1 } }],
  report: { values: [], byCategory: {}, totalReplacements: 1, documents: [] },
};

// --- previousStep / followingStep ----------------------------------------
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
  // (state.js). Returning null on both sides renders a
  // footer with no navigation, which is recoverable; throwing would leave a blank
  // screen with an exception behind it.
  const s = stateOn("teleport");
  assert.equal(previousStep(s), null);
  assert.equal(followingStep(s), null);
});

// --- stepFooterHTML ------------------------------------------------------

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

test("the review gate reaches the footer through the guard, not through the screen", () => {
  // an unreviewed suggestion shuts the move to Anonymise. The
  // footer must inherit that from canGoTo rather than growing a condition of its
  // own, which is the whole reason the guard lives in one place.
  const waiting = stepFooterHTML({}, stateOn("identify", {
    candidates: [{ text: "Alpine Trust", category: "entity_names", count: 4 }],
  }));
  assert.ok("disabled" in one(waiting, "#step-next").attrs,
    "a suggestion is still waiting, so CONTINUE TO ANONYMISE must be disabled");

  const reviewed = stepFooterHTML({}, stateOn("identify"));
  assert.ok(!("disabled" in one(reviewed, "#step-next").attrs),
    "with the review done the move is open again");
});

// --- Keyboard shortcuts --------------------------------------------------

test("shortcutStep maps only Ctrl+O and Ctrl+E, in either case", () => {
  const cases = [
    [{ ctrlKey: true, key: "o" }, "import"],
    [{ ctrlKey: true, key: "O" }, "import"],
    [{ metaKey: true, key: "e" }, "export"],
    [{ ctrlKey: true, key: "E" }, "export"],
    // Not ours: no modifier, the wrong letter, or a combination the browser
    // already owns. Claiming Ctrl+Shift+E would be taking a key we were not
    // given, and preventDefault() on it is invisible and unexplainable.
    [{ key: "o" }, null],
    [{ ctrlKey: true, key: "s" }, null],
    [{ ctrlKey: true, shiftKey: true, key: "e" }, null],
    [{ ctrlKey: true, altKey: true, key: "o" }, null],
    [{ ctrlKey: true }, null],
    [null, null],
  ];
  for (const [ev, want] of cases) {
    assert.equal(shortcutStep(ev), want, JSON.stringify(ev));
  }
});

test("a FORWARD shortcut moves without asking, and only when the guard allows", async () => {
  resetState();
  setState({ documents: [{ name: "a.md" }], step: "import" });

  // Ctrl+E with no results: the guard refuses, and nothing on screen changes.
  assert.equal(await handleShortcut({ ctrlKey: true, key: "e" }), false);
  assert.equal(getState().step, "import", "no results, so Export stays shut");

  setState({ results: FINISHED_RUN });
  assert.equal(await handleShortcut({ ctrlKey: true, key: "e" }), true);
  assert.equal(getState().step, "export");
  assert.equal(getState().screen, "wizard",
    "a step shortcut implies the wizard screen, so it switches to it");
});

test("a BACKWARD shortcut asks first, exactly as the step bar does", async () => {
  // This is the bug the phase closes. main.js called goTo() straight, so Ctrl+O
  // from a later step was a backward move with neither the confirm nor the
  // resetStep that nav.js exists to guarantee: the same movement obeyed the rule
  // from the step bar and skipped it from the keyboard.
  resetState();
  setState({ documents: [{ name: "a.md" }], results: FINISHED_RUN, step: "anonymise" });

  const moving = handleShortcut({ ctrlKey: true, key: "o" });
  assert.ok(getState().confirm, "the shortcut asked the in-app confirm first");

  answerConfirm(false);
  assert.equal(await moving, false, "answering no does not move");
  assert.equal(getState().step, "anonymise");
  assert.ok(getState().results, "and answering no changes nothing at all, not even the reset");

  // And on yes it moves AND resets the step being LEFT (Anonymise, so the run
  // goes with it), which is the half a direct goTo() silently skipped.
  const again = handleShortcut({ ctrlKey: true, key: "o" });
  answerConfirm(true);
  assert.equal(await again, true);
  assert.equal(getState().step, "import");
  assert.equal(getState().results, null, "leaving Anonymise backwards cleared the run");
});

test("a backward move is refused while a run is in progress", async () => {
  // Resetting the Anonymise step from under a running goroutine is the one move
  // that cannot be made clean, so navigateTo refuses it until the run ends. The
  // user cancels on the Anonymise screen instead.
  resetState();
  setState({
    documents: [{ name: "a.md" }], results: FINISHED_RUN, running: true, step: "anonymise",
  });

  const moved = await handleShortcut({ ctrlKey: true, key: "o" });
  assert.equal(moved, false, "the backward shortcut did nothing while the run was live");
  assert.equal(getState().confirm, null, "and it did not even ask");
  assert.equal(getState().step, "anonymise", "the user stays on the run screen");
  assert.ok(getState().results, "nothing was reset");
});

test("a multi-step back clears the steps it jumps over, not just the one it leaves", async () => {
  // The leak: Anonymise back to Import jumps over Identify, whose detected values
  // used to survive onto the "clean" Import screen. navigateTo now resets every
  // step left behind, so the values, the suggestions and the patterns go too.
  // handleShortcut also asks Go to discard the run; there is no bridge here, so
  // that call rejects and is swallowed, which must not stop the frontend reset.
  resetState();
  setState({
    documents: [{ name: "a.md" }],
    results: FINISHED_RUN,
    entities: [{ category: "person_names", canonical: "Marie Duval", status: "accepted" }],
    candidates: [{ text: "Alpine Trust", category: "entity_names", count: 2 }],
    patterns: [{ expr: "PRJ-[0-9]+", error: null }],
    step: "anonymise",
  });

  const moving = handleShortcut({ ctrlKey: true, key: "o" });
  answerConfirm(true);
  assert.equal(await moving, true);
  const s = getState();
  assert.equal(s.step, "import");
  assert.deepEqual(s.entities, [], "the Identify values did not survive the jump to Import");
  assert.deepEqual(s.candidates, [], "nor did the suggestions");
  assert.deepEqual(s.patterns, [], "nor the custom patterns");
  assert.equal(s.results, null, "and the run is gone");
  assert.equal(s.documents.length, 1, "but the imported documents are kept");
});

test("a shortcut suppresses the browser default only for a key it claims", async () => {
  resetState();
  setState({ documents: [{ name: "a.md" }] });

  let prevented = 0;
  const ev = (patch) => ({ preventDefault: () => { prevented++; }, ...patch });

  await handleShortcut(ev({ ctrlKey: true, key: "o" }));
  assert.equal(prevented, 1, "Ctrl+O is ours, so the browser's Open dialog must not fire");

  await handleShortcut(ev({ ctrlKey: true, key: "p" }));
  assert.equal(prevented, 1, "Ctrl+P is not ours and must keep working");
});

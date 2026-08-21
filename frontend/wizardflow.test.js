// wizardflow.test.js, the wizard state-transition matrix
// (, the "robust test procedures for the states between
// steps".
//
// The other suites test reducers one at a time. This one tests the state
// MACHINE: for every reachable session shape, which steps are open, what a
// forward move does, and what a backward move clears. That combination is
// where the reported bugs lived, because each individual reducer was
// right and the interaction between them was not.
//
// The shapes come from the wizard's own preconditions, so the list is
// exhaustive rather than a sample:
//
//   empty nothing imported
//   documents imported, nothing chosen
//   configured categories chosen, no suggestions
//   suggestions suggestions waiting for review
//   values values accepted, no run yet
//   results a run has produced output
//
// The last four all sit on the Identify step, which owns both the choices and
// the values since. They stay separate shapes because the
// guards and the resets treat them differently even though one screen produces
// them all.
//
// Run with `node --test "frontend/**/*.test.js"`.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  getState, setState, resetState,
  WIZARD_STEPS, canGoTo, goTo, nextStep,
  isBackward, resetStep, STEP_RESETS,
  addSuggestions, applyPreset, setRequireChecksum,
  addAllowTerm, defaultCategories,
  PRESET_SCOPE_PATTERNS, PRESET_SCOPE_NAMES, PRESET_FAMILY_DEPTH,
  acceptSuggestion, rejectSuggestion, rejectAllShown,
} from "./state.js";
import { DEFAULT_COUNTRY, countryIDCategories } from "./countries.js";

// --- The session shapes --------------------------------------------------

const SHAPES = {
  empty: () => {
    resetState();
  },
  documents: () => {
    resetState();
    setState({ documents: [{ name: "a.txt" }], previewDoc: "a.txt" });
  },
  configured: () => {
    SHAPES.documents();
    applyPreset(PRESET_SCOPE_PATTERNS, PRESET_FAMILY_DEPTH, "thorough");
    applyPreset(PRESET_SCOPE_NAMES, PRESET_FAMILY_DEPTH, "thorough");
    setRequireChecksum(true);
    addAllowTerm("CSSF");
  },
  suggestions: () => {
    SHAPES.configured();
    addSuggestions([
      { discoveryMethods: ["heuristic"], mainText: "Alpine Trust", category: "entity_names", count: 4 },
      { discoveryMethods: ["heuristic"], mainText: "Marie Duval", category: "person_names", count: 2 },
    ]);
  },
  // "values" is the shape AFTER the review, which is the only way a real session
  // reaches it: every Suggestion has been answered, one accepted and one
  // rejected, so nothing is left waiting. A shape with an accepted Value beside
  // still-unreviewed Suggestions is one the interface cannot produce and one the
  // gate refuses to walk out of.
  values: () => {
    SHAPES.suggestions();
    acceptSuggestion("Marie Duval");
    rejectSuggestion("Alpine Trust");
  },
  results: () => {
    SHAPES.values();
    setState({
      results: { documents: [{ name: "a.txt", anonymised: "[PERSON_1]" }] },
      mapping: { "[PERSON_1]": { original: "Marie Duval", category: "person_names" } },
    });
  },
};

const SHAPE_NAMES = Object.keys(SHAPES);

/** reachableSteps() lists the steps the guards currently allow. */
function reachableSteps() {
  return WIZARD_STEPS.filter((step) => canGoTo(step));
}

// --- Guards, per shape ---------------------------------------------------

test("matrix: which steps each session shape unlocks", () => {
  const expected = {
    // Nothing imported: only Import.
    empty: ["import"],
    // Documents unlock everything except Export, which needs results.
    documents: ["import", "identify", "anonymise"],
    configured: ["import", "identify", "anonymise"],
    // Suggestions waiting for review SHUT Anonymise. This
    // test pinned the opposite until then: "suggestions unlocks anonymise" was
    // asserted as a feature, which is how walking past an unreviewed detection
    // stayed invisible.
    suggestions: ["import", "identify"],
    values: ["import", "identify", "anonymise"],
    // A finished run unlocks Export.
    results: ["import", "identify", "anonymise", "export"],
  };
  for (const name of SHAPE_NAMES) {
    SHAPES[name]();
    assert.deepEqual(reachableSteps(), expected[name], `shape "${name}"`);
  }
});

test("matrix: Import is reachable from every shape, always", () => {
  // There is no state in which a user cannot get back to their documents.
  for (const name of SHAPE_NAMES) {
    SHAPES[name]();
    assert.equal(canGoTo("import"), true, `shape "${name}"`);
  }
});

test("matrix: Export is reachable exactly when results exist", () => {
  for (const name of SHAPE_NAMES) {
    SHAPES[name]();
    assert.equal(canGoTo("export"), !!getState().results, `shape "${name}"`);
  }
});

// --- The review gate -----------------------------------------------------

test("matrix: Anonymise is reachable exactly when no suggestion is waiting", () => {
  // The rule stated directly, across every shape, beside the table above: the
  // table says which steps each shape opens, this says WHY Anonymise is one of
  // them, so a shape added later cannot pass by accident.
  for (const name of SHAPE_NAMES) {
    SHAPES[name]();
    const s = getState();
    const want = s.documents.length > 0 && s.suggestions.length === 0;
    assert.equal(canGoTo("anonymise"), want, `shape "${name}"`);
  }
});

test("matrix: rejecting the waiting suggestions opens the gate", () => {
  // The gate must never be a dead end. A user who decides none of the
  // suggestions are worth keeping (or who switched detection off after it ran)
  // needs ONE action to get moving again, and this is the one the footer hint
  // names.
  SHAPES.suggestions();
  assert.equal(canGoTo("anonymise"), false, "suggestions are waiting");

  const waiting = getState().suggestions.map((r) => r.mainText);
  assert.equal(rejectAllShown(waiting), waiting.length);
  assert.equal(canGoTo("anonymise"), true, "nothing is waiting any more");

  // And the move itself now works, rather than only the guard agreeing it could.
  goTo("identify");
  assert.equal(nextStep(), true);
  assert.equal(getState().step, "anonymise");
});

test("matrix: accepting the waiting suggestions opens the gate too", () => {
  // Accepting is the other half of the review. It promotes the suggestion into
  // a value AND clears it from the review list, so the gate opens for the same
  // reason: nothing is left unanswered.
  SHAPES.suggestions();
  for (const text of getState().suggestions.map((r) => r.mainText)) acceptSuggestion(text);
  assert.deepEqual(getState().suggestions, []);
  assert.equal(canGoTo("anonymise"), true);
});

test("matrix: the gate cannot be walked past by stepping forward repeatedly", () => {
  // The same probe the document guard gets: a shut gate that a determined
  // nextStep() opens is not a gate.
  SHAPES.suggestions();
  goTo("identify");
  for (let i = 0; i < 10; i++) nextStep();
  assert.equal(getState().step, "identify",
    "unreviewed suggestions mean Identify is as far as it goes");
});

// --- Forward moves -------------------------------------------------------

test("matrix: every adjacent forward move, per shape", () => {
  for (const name of SHAPE_NAMES) {
    for (let i = 0; i < WIZARD_STEPS.length - 1; i++) {
      const from = WIZARD_STEPS[i];
      const to = WIZARD_STEPS[i + 1];

      SHAPES[name]();
      if (!canGoTo(from)) continue; // cannot even stand on `from`
      goTo(from);

      const allowed = canGoTo(to);
      assert.equal(nextStep(), allowed,
        `shape "${name}": ${from} to ${to} should ${allowed ? "" : "not "}be allowed`);
      assert.equal(getState().step, allowed ? to : from,
        `shape "${name}": the step after a ${allowed ? "allowed" : "blocked"} move`);
    }
  }
});

test("matrix: a forward move never clears anything", () => {
  // Only a BACKWARD move resets, and only after a confirmation. A forward
  // move that quietly dropped state would be the same bug in reverse.
  for (const name of SHAPE_NAMES) {
    SHAPES[name]();
    if (!canGoTo("identify")) continue;
    goTo("import");
    const before = JSON.stringify(getState());
    nextStep();
    const after = { ...getState(), step: "import" };
    assert.equal(JSON.stringify(after), before, `shape "${name}" lost state moving forward`);
  }
});

test("matrix: the guards cannot be bypassed by walking forward repeatedly", () => {
  SHAPES.empty();
  for (let i = 0; i < 10; i++) nextStep();
  assert.equal(getState().step, "import", "no documents means no progress, ever");

  SHAPES.values();
  goTo("import");
  for (let i = 0; i < 10; i++) nextStep();
  assert.equal(getState().step, "anonymise",
    "without results, Anonymise is as far as it goes");
});

// --- Backward moves and their resets -------------------------------------

test("matrix: there is no reducer that moves BACK without asking", async () => {
  // prevStep() is gone. Backward movement resets the step
  // being left, and the user is asked first (nav.js goBack), so a reducer that
  // stepped back silently was a way around a rule the whole screen depends on.
  //
  // The guard is asserted rather than described, because "we deleted it" is only
  // true until someone adds it back for convenience.
  const state = await import("./state.js");
  assert.equal(state.prevStep, undefined,
    "moving back must go through nav.js goBack(), which asks the reset question");
  // goTo() itself is still free to move backward: it is what nav.js calls AFTER
  // the question has been answered and the reset applied.
  SHAPES.values();
  goTo("anonymise");
  assert.equal(goTo("import"), true);
  assert.equal(getState().step, "import");
});

test("matrix: isBackward agrees with the step order for every pair", () => {
  for (const from of WIZARD_STEPS) {
    for (const to of WIZARD_STEPS) {
      const want = WIZARD_STEPS.indexOf(to) < WIZARD_STEPS.indexOf(from);
      assert.equal(isBackward(from, to), want, `${from} to ${to}`);
    }
  }
});

test("matrix: every backward reset preserves documents and the allowlist", () => {
  // The two invariants of, checked across every
  // shape and every step rather than once.
  for (const name of SHAPE_NAMES) {
    for (const step of WIZARD_STEPS) {
      SHAPES[name]();
      const documentsBefore = getState().documents.length;
      const allowlistBefore = [...getState().allowlist];
      resetStep(step);
      assert.equal(getState().documents.length, documentsBefore,
        `shape "${name}", reset "${step}": documents changed`);
      assert.deepEqual(getState().allowlist, allowlistBefore,
        `shape "${name}", reset "${step}": allowlist changed`);
    }
  }
});

test("matrix: a reset never touches a LATER step's state", () => {
  // Resetting Identify must not throw away a finished run: the user is stepping
  // back to adjust what to look for, not starting over.
  SHAPES.results();
  resetStep("identify");
  assert.ok(getState().results, "the run survived an identify reset");
});

test("matrix: a reset never touches an EARLIER step's state", () => {
  SHAPES.results();
  const categories = { ...getState().settings.categories };
  resetStep("anonymise");
  assert.deepEqual(getState().settings.categories, categories,
    "resetting the run must not undo the category selection");
  assert.equal(getState().values.length, 1,
    "resetting the run must not undo the values");
});

test("matrix: resetting a step re-locks whatever depended on it", () => {
  SHAPES.results();
  assert.equal(canGoTo("export"), true);
  resetStep("anonymise");
  assert.equal(canGoTo("export"), false, "no results means no export");
  // And the wizard cannot be walked into the locked step afterwards.
  goTo("anonymise");
  assert.equal(nextStep(), false);
  assert.equal(getState().step, "anonymise");
});

test("matrix: resetting every step in turn lands on a usable session", () => {
  // The worst case a user can produce by stepping all the way back: the
  // result must be the shape they started from, documents intact, not a
  // half-broken state.
  SHAPES.results();
  for (const step of [...WIZARD_STEPS].reverse()) {
    resetStep(step);
  }
  const s = getState();
  assert.equal(s.documents.length, 1, "the documents are still there");
  assert.deepEqual(s.values, []);
  assert.deepEqual(s.suggestions, []);
  assert.equal(s.results, null);
  assert.equal(s.mapping, null, "the re-identification key is gone with the run");
  assert.deepEqual(s.settings.categories, defaultCategories(DEFAULT_COUNTRY));
  // And the wizard is exactly as open as a fresh import: everything but
  // Export.
  assert.deepEqual(reachableSteps(), ["import", "identify", "anonymise"]);
});

test("matrix: the reset table covers every step and invents none", () => {
  assert.deepEqual(Object.keys(STEP_RESETS).sort(), [...WIZARD_STEPS].sort());
});

// --- Leaving and re-entering the wizard ----------------------------------

test("matrix: navigating to Home and back preserves every shape", () => {
  for (const name of SHAPE_NAMES) {
    SHAPES[name]();
    const step = reachableSteps().at(-1);
    goTo(step);
    const before = JSON.stringify({ ...getState(), screen: "wizard" });

    setState({ screen: "home" });
    setState({ screen: "wizard" });

    assert.equal(JSON.stringify(getState()), before,
      `shape "${name}" changed by leaving and re-entering the wizard`);
  }
});

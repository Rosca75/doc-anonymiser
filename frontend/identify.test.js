// identify.test.js, tests for the Identify screen's footer sentence
//
// The footer sentence is the REASON the disabled CONTINUE gives, and a sentence
// that explains a refusal is logic: it can be wrong in the one way that matters,
// by saying the move is available when the guard refuses it, or saying nothing
// at all while the button sits dead.
//
// What is asserted here is the pair, always together: the guard's answer
// (state.js canGoTo) and the sentence beside the button. The bug this phase
// closed was exactly the two disagreeing, so testing either alone would miss it.
//
// Run with `node --test "frontend/**/*.test.js"`.

import { test } from "node:test";
import assert from "node:assert/strict";

import { gateReason, renderIdentify } from "./views/identify.js";
import { canGoTo, resetState, setState } from "./state.js";
import { container } from "./testdom.js";
import { WORKSPACE } from "./copy.js";

/**
 * screen(patch) is the slice of state the footer sentence reads, plus the two
 * fields the guard needs to agree with it.
 */
function screen(patch = {}) {
  return {
    step: "identify",
    documents: [{ name: "a.md", markdown: "text" }],
    values: [],
    suggestions: [],
    results: null,
    ...patch,
  };
}

const accepted = (n) =>
  Array.from({ length: n }, (_, i) => ({
    category: "person_names", mainText: `Person ${i}`, status: "accepted",
  }));

const waiting = (n) =>
  Array.from({ length: n }, (_, i) => ({
    text: `Value ${i}`, category: "entity_names", count: 1,
  }));

// --- Nothing blocking ----------------------------------------------------

test("with nothing waiting the footer says nothing", () => {
  // There is deliberately no progress read-out here. Counting accepted values
  // narrated the state of the list the user is already looking at, and it was
  // the ONLY thing the footer said whenever the gate was open, so the honest
  // empty case ("0 values ready to replace") read as a problem.
  const s = screen({ values: accepted(3) });
  assert.equal(gateReason(s), "", "nothing is blocking, so there is nothing to say");
  assert.equal(canGoTo("anonymise", s), true);
});

test("an empty review is an open gate, not a refusal", () => {
  // A user who rejected everything has a legitimately empty run ahead of them.
  // The gate is about unreviewed suggestions, not about having accepted any.
  const s = screen();
  assert.equal(gateReason(s), "");
  assert.equal(canGoTo("anonymise", s), true);
});

// --- The gate shut -------------------------------------------------------

test("a waiting suggestion turns the hint into the refusal, and the guard agrees", () => {
  const s = screen({ values: accepted(3), suggestions: waiting(2) });

  assert.equal(canGoTo("anonymise", s), false, "the guard refuses the move");
  assert.equal(gateReason(s), WORKSPACE.reviewGate(2),
    "the footer says why, rather than narrating a count beside a dead button");
});

test("the refusal names the action that clears it", () => {
  // The gate must never be a dead end. A sentence that only counts what is left
  // tells the user they are stuck without telling them how to stop being stuck,
  // and a user who switched detection off after it ran has no other clue that
  // suggestions are still sitting in the list.
  const sentence = gateReason(screen({ suggestions: waiting(1) }));

  assert.match(sentence, /1 suggestion still waiting/, "singular for one");
  assert.match(sentence, /Accept or reject/);
  assert.match(sentence, new RegExp(WORKSPACE.rejectAllShown),
    "it names the bulk button by its own label, so a rename cannot orphan the hint");
  assert.match(sentence, /filter/,
    "the bulk button acts on the rows in view, so a filter must be cleared first");
});

test("the guard and the hint move together across the whole review", () => {
  // Walked as a sequence rather than as three separate fixtures, because the
  // failure this guards against is a screen that changes its mind about the gate
  // halfway through a review.
  for (let left = 3; left >= 0; left--) {
    const s = screen({ values: accepted(3 - left), suggestions: waiting(left) });
    const open = canGoTo("anonymise", s);
    assert.equal(open, left === 0, `${left} waiting`);
    assert.equal(gateReason(s) === "", open,
      `${left} waiting: the footer speaks exactly while the move is refused`);
  }
});

// --- The review panel appears only once a run has happened ----------------

test("the review panel is hidden until a detection run has settled", () => {
  // Before the first run the panel is four empty tabs and a footer refusing to
  // continue, which reads as a broken screen rather than as "nothing has looked
  // yet". The run button is in the rail's head precisely so it survives that:
  // a button that reveals a card cannot live inside the card it reveals.
  resetState();
  setState({ step: "identify", documents: [{ name: "a.md", markdown: "text" }] });
  const root = container();
  renderIdentify(root);

  assert.equal(root.querySelector("#identify-workspace"), null,
    "no review panel before a run");
  assert.ok(root.querySelector("#identify-rail"), "the rail is the whole screen");
  assert.ok(root.querySelector("#btn-detect"),
    "and the run button is on screen, or the panel could never be revealed");
  assert.ok(root.querySelector(".step-footer.standalone"),
    "the footer is a card of its own while there is no workspace to be the foot of");

  setState({ detectionRan: true });
  renderIdentify(root);

  assert.ok(root.querySelector("#identify-workspace"), "the panel appears after the run");
  assert.ok(root.querySelector("#btn-detect"),
    "and the button stays in the rail: the run is repeatable");
  assert.equal(root.querySelector(".step-footer.standalone"), null,
    "the footer moves back into the workspace card's foot");
});

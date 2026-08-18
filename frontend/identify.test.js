// identify.test.js, tests for the Identify screen's footer sentence
//
// views/identify.js used to be an exempt module in ../frontend_tests_test.go:
// "layout and footer only". That stopped being true when the footer sentence
// became the REASON the disabled CONTINUE gives rather than a progress
// read-out. A sentence that explains a refusal is logic, because it can be
// wrong in the one way that matters: it can say the move is available when the
// guard refuses it, or say nothing at all while the button sits dead.
//
// What is asserted here is the pair, always together: the guard's answer
// (state.js canGoTo) and the sentence beside the button. The bug this phase
// closed was exactly the two disagreeing, so testing either alone would miss it.
//
// Run with `node --test "frontend/**/*.test.js"`.

import { test } from "node:test";
import assert from "node:assert/strict";

import { readyHint, gateReason } from "./views/identify.js";
import { canGoTo } from "./state.js";
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

// --- The review done -----------------------------------------------------

test("with the review done the hint counts what the next step will act on", () => {
  assert.equal(readyHint(screen({ values: accepted(3) })),
    WORKSPACE.readyToReplace(3));
  assert.equal(readyHint(screen({ values: accepted(1) })),
    WORKSPACE.readyToReplace(1), "one value reads in the singular");
});

test("zero accepted values is an answer, not an empty hint", () => {
  // A user who rejected everything has a legitimately empty run ahead of them.
  // The footer says so rather than going blank, and the move stays OPEN: the
  // gate is about unreviewed suggestions, not about having accepted any.
  const s = screen();
  assert.equal(readyHint(s), WORKSPACE.readyToReplace(0));
  assert.equal(gateReason(s), "", "nothing is blocking");
  assert.equal(canGoTo("anonymise", s), true);
});

test("a value that is not accepted is not counted as ready", () => {
  // The entity list also carries rejected and pending rows; only accepted ones
  // are what the run will replace.
  const s = screen({
    values: [
      { category: "person_names", mainText: "Kept", status: "accepted" },
      { category: "person_names", mainText: "Dropped", status: "rejected" },
      { category: "person_names", mainText: "Unanswered" },
    ],
  });
  assert.equal(readyHint(s), WORKSPACE.readyToReplace(1));
});

// --- The gate shut -------------------------------------------------------

test("a waiting suggestion turns the hint into the refusal, and the guard agrees", () => {
  const s = screen({ values: accepted(3), suggestions: waiting(2) });

  assert.equal(canGoTo("anonymise", s), false, "the guard refuses the move");
  assert.equal(gateReason(s), WORKSPACE.reviewGate(2));
  assert.equal(readyHint(s), WORKSPACE.reviewGate(2),
    "the footer says why, rather than narrating the count beside a dead button");
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
    assert.equal(readyHint(s) === gateReason(s), !open,
      `${left} waiting: the hint is the refusal exactly while the move is refused`);
  }
});

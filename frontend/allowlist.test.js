// allowlist.test.js, tests for the "Never anonymise" tab's pure render half
// (views/allowlist.js renderAllowlistChips).
//
// Why this file exists: the allowlist no longer seeds any defaults, so its
// bulk "Clear all" control has to earn its place on screen only when there is
// something to clear. That is a rule about what the markup SHOWS, which is
// exactly what these string-level assertions pin without a browser. The DOM
// wiring (the confirm dialog, the click) needs a document and belongs to the
// layer 3 rendering harness (docs/UITESTING.md); the clearing itself is proven
// against the reducer in state.test.js.
//
// Run with `node --test "frontend/**/*.test.js"`.

import { test } from "node:test";
import assert from "node:assert/strict";

import { renderAllowlistChips } from "./views/allowlist.js";
import { getState, resetState, addAllowTerm, setDefinedTerms } from "./state.js";
import { ALLOWLIST } from "./copy.js";
import { one, all, exists, textOf } from "./testhtml.js";

test("the Clear all button is present but disabled when the list is empty", () => {
  resetState();
  const html = renderAllowlistChips(getState());
  assert.ok(exists(html, "#allow-clear"), "the button is always in the add row");
  assert.ok("disabled" in one(html, "#allow-clear").attrs,
    "with nothing to clear the button must be disabled, not hidden");
  assert.equal(textOf(html, "#allow-clear").trim(), ALLOWLIST.clearAll);
});

test("the Clear all button is enabled once a term is present", () => {
  resetState();
  addAllowTerm("CSSF");
  const html = renderAllowlistChips(getState());
  assert.ok(!("disabled" in one(html, "#allow-clear").attrs),
    "a non-empty list can be cleared, so the button is live");
});

test("a fresh allowlist tab shows the empty-list hint, not seeded chips", () => {
  // The seeding is gone: the tab must read as empty on a fresh state, so the
  // user knows nothing is protected yet rather than seeing terms they never
  // chose.
  resetState();
  const html = renderAllowlistChips(getState());
  assert.ok(!exists(html, ".allow-del"), "no chips means nothing was seeded");
  assert.match(html, new RegExp(ALLOWLIST.empty.slice(0, 10)));
});

// --- The terms the DOCUMENTS define -------------------------------------------

test("the defined-terms box says what it is even when nothing is defined", () => {
  // A block that appears only once something is in it teaches the user nothing
  // about why a value stopped being suggested. The heading and the explanation
  // are there from the start.
  resetState();
  const html = renderAllowlistChips(getState());
  assert.ok(exists(html, ".defined-title"), "the heading is always rendered");
  assert.equal(textOf(html, ".defined-title").trim(), ALLOWLIST.definedTitle);
  assert.ok(html.includes(ALLOWLIST.definedEmpty),
    "an empty box explains what would appear in it");
});

test("a defined term is shown with the idiom that introduced it and a remove", () => {
  // The point of the feature is that the suppression is VISIBLE: a rule the user
  // cannot see is one they cannot lift, and this is the largest thing standing
  // between a review list and a usable one.
  resetState();
  setDefinedTerms([
    { term: "Work Order", idiom: "means", document: "a.docx" },
    { term: "Dedicated Advisors", idiom: "parenthetical", document: "a.docx" },
  ]);
  const html = renderAllowlistChips(getState());

  // testhtml supports simple selectors only, so the box is extracted first and
  // queried inside: the same thing a descendant selector would express.
  const box = one(html, ".defined-box").outer;
  const chips = all(box, ".chip-tag");
  assert.equal(chips.length, 2, "one chip per defined term");
  assert.deepEqual(chips.map((c) => c.attrs["data-defined-term"]),
    ["Work Order", "Dedicated Advisors"]);

  const first = chips[0].outer;
  assert.equal(textOf(first, ".chip-note").trim(), ALLOWLIST.definedIdiom("means"),
    "the chip carries WHY the term is suppressed, not just that it is");
  assert.ok(exists(first, ".defined-del"),
    "every entry can be lifted, exactly as a session exclusion can be restored");
  assert.equal(one(first, ".defined-del").attrs["data-term"], "Work Order");

  assert.equal(textOf(chips[1].outer, ".chip-note").trim(),
    ALLOWLIST.definedIdiom("parenthetical"));
});

test("the defined terms are a separate box from the terms the user typed", () => {
  // Merging them would make "delete a term I added" and "stop honouring a
  // definition" the same button, and the two are undone by different gestures.
  resetState();
  addAllowTerm("CSSF");
  setDefinedTerms([{ term: "Work Order", idiom: "means", document: "a.docx" }]);
  const html = renderAllowlistChips(getState());

  // The FIRST .chip-box is the user's own; the second carries .defined-box.
  const boxes = all(html, ".chip-box");
  assert.equal(boxes.length, 2, "two boxes, one per kind of entry");
  const userChips = all(boxes[0].outer, ".chip-tag");
  assert.deepEqual(userChips.map((c) => c.inner.includes("CSSF")), [true],
    "the user's own box holds only what the user put there");
  for (const chip of userChips) {
    assert.ok(!chip.inner.includes("chip-note"),
      "a term the user typed needs no provenance note: they know why it is there");
  }
  assert.equal(all(one(html, ".defined-box").outer, ".chip-tag").length, 1);
});

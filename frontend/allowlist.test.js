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
import { getState, resetState, addAllowTerm } from "./state.js";
import { ALLOWLIST } from "./copy.js";
import { one, exists, textOf } from "./testhtml.js";

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

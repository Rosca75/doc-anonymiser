// scroll.test.js, tests for the generic scroll-preservation helpers
//
// The reported bug: ticking a checkbox, drilling into a value, or adding an
// allowlist term threw the panel back to the top, because the state change
// re-renders the shell and a rewritten element starts at scrollTop 0.
//
// The fix preserves scroll around the repaint itself (main.js paint), so the
// contract these tests pin is:
//   - stableSelector(el) produces a selector that re-resolves to the SAME
//     element after the identical DOM is regenerated,
//   - snapshotScrollPositions() records only the elements that are scrolled,
//   - restoreScrollPositions() puts each offset back on the fresh DOM.
//
// There is no real DOM here (the frontend suite runs under `node --test` with
// zero npm deps), so the tests build a tiny fake element tree and a fake
// `document` whose querySelector resolves a selector by re-deriving
// stableSelector on the fresh tree, which is exactly what the real querySelector
// does structurally.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  stableSelector, snapshotScrollPositions, restoreScrollPositions,
} from "./scroll.js";

/**
 * el(tag, opts) builds a fake element node: enough of the DOM shape for
 * stableSelector (nodeType, tagName, id, parentElement, previousElementSibling)
 * plus mutable scrollTop/scrollLeft.
 */
function el(tag, { id = "", children = [] } = {}) {
  const node = {
    nodeType: 1,
    tagName: tag.toUpperCase(),
    id,
    scrollTop: 0,
    scrollLeft: 0,
    parentElement: null,
    children: [],
    get previousElementSibling() {
      const sibs = this.parentElement ? this.parentElement.children : [];
      const i = sibs.indexOf(this);
      return i > 0 ? sibs[i - 1] : null;
    },
  };
  for (const child of children) {
    child.parentElement = node;
    node.children.push(child);
  }
  return node;
}

/** flatten(root) returns every node in document order (what querySelectorAll("*")
 *  yields). */
function flatten(root) {
  const all = [];
  (function walk(n) { all.push(n); for (const c of n.children) walk(c); })(root);
  return all;
}

/** makeDoc(root) is a fake document. querySelector resolves a selector by
 *  matching stableSelector against every node, which is how a regenerated DOM
 *  re-finds the element the snapshot named. */
function makeDoc(root) {
  const all = flatten(root);
  return {
    querySelectorAll: (sel) => (sel === "*" ? all : []),
    querySelector: (sel) => all.find((n) => stableSelector(n) === sel) ?? null,
  };
}

/** rail() builds the Identify rail card shape: a section#identify-rail with a
 *  head div, a body div (the scroll owner), and a settings-error div. */
function rail() {
  const head = el("div");
  const body = el("div");
  const err = el("div", { id: "settings-error" });
  const section = el("section", { id: "identify-rail", children: [head, body, err] });
  return { section, head, body, err };
}

/** withDocument(doc, fn) installs a fake global document for the duration of fn. */
function withDocument(doc, fn) {
  const previous = globalThis.document;
  globalThis.document = doc;
  try {
    return fn();
  } finally {
    if (previous === undefined) delete globalThis.document;
    else globalThis.document = previous;
  }
}

// --- stableSelector -------------------------------------------------------

test("stableSelector anchors on an id and stops the walk", () => {
  const { section } = rail();
  assert.equal(stableSelector(section), "#identify-rail");
});

test("stableSelector builds an nth-of-type step up to the nearest id", () => {
  const { body } = rail();
  // The body is the 2nd <div> child of #identify-rail (head, body, settings-error).
  assert.equal(stableSelector(body), "#identify-rail > div:nth-of-type(2)");
});

test("stableSelector returns empty for a missing or non-element node", () => {
  assert.equal(stableSelector(null), "");
  assert.equal(stableSelector({ nodeType: 3 }), "");
});

// --- snapshotScrollPositions ----------------------------------------------

test("snapshot records only the elements that are actually scrolled", () => {
  const { section, body } = rail();
  body.scrollTop = 300;
  const snap = withDocument(makeDoc(section), () => snapshotScrollPositions());
  assert.deepEqual(snap, [
    { selector: "#identify-rail > div:nth-of-type(2)", top: 300, left: 0 },
  ]);
});

test("snapshot captures horizontal offset too", () => {
  const { section, body } = rail();
  body.scrollLeft = 42;
  const snap = withDocument(makeDoc(section), () => snapshotScrollPositions());
  assert.equal(snap[0].left, 42);
});

test("snapshot is empty when nothing has scrolled", () => {
  const { section } = rail();
  const snap = withDocument(makeDoc(section), () => snapshotScrollPositions());
  assert.deepEqual(snap, []);
});

// --- restoreScrollPositions -----------------------------------------------

test("a scrolled panel's offset survives the repaint (the reported bug)", () => {
  // Snapshot the OLD DOM (scrolled two thirds down), then build a fresh,
  // structurally identical DOM at the top (what the rewrite produces) and
  // restore onto it.
  const oldTree = rail();
  oldTree.body.scrollTop = 480;
  oldTree.body.scrollLeft = 12;
  const snap = withDocument(makeDoc(oldTree.section), () => snapshotScrollPositions());

  const newTree = rail();
  withDocument(makeDoc(newTree.section), () => restoreScrollPositions(snap));

  assert.equal(newTree.body.scrollTop, 480, "the vertical offset must be restored");
  assert.equal(newTree.body.scrollLeft, 12);
});

test("restore skips a panel the new state no longer renders", () => {
  const snap = [{ selector: "#gone > div:nth-of-type(1)", top: 100, left: 0 }];
  const { section } = rail();
  // Must not throw when the selector resolves to nothing.
  withDocument(makeDoc(section), () => restoreScrollPositions(snap));
});

// --- no-document guards (the unit-test environment) -----------------------

test("snapshot returns an empty list when there is no document", () => {
  const previous = globalThis.document;
  delete globalThis.document;
  try {
    assert.deepEqual(snapshotScrollPositions(), []);
  } finally {
    if (previous !== undefined) globalThis.document = previous;
  }
});

test("restore does nothing (and does not throw) when there is no document", () => {
  const previous = globalThis.document;
  delete globalThis.document;
  try {
    restoreScrollPositions([{ selector: "#x", top: 1, left: 0 }]);
  } finally {
    if (previous !== undefined) globalThis.document = previous;
  }
});

test("restore tolerates an empty or missing snapshot", () => {
  const { section } = rail();
  withDocument(makeDoc(section), () => {
    restoreScrollPositions([]);
    restoreScrollPositions(undefined);
  });
});

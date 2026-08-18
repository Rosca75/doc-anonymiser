// identifyactions.test.js, wiring tests for the My values card actions.
//
// These are the tests that catch a control which RENDERS correctly and does
// nothing. The four actions below were each reported as broken against the built
// application while every render test stayed green, because a render test reads
// the HTML string a view wrote and a browser re-reads it: the parser lower-cases
// attribute names, so a camel-case `data-` attribute reaches the DOM under a key
// no handler looks up and every action on the card resolves against `undefined`.
//
// So these drive the real handlers, wired by the real render path, against
// testdom.js, whose parser lower-cases attribute names exactly as a browser
// does. An action that cannot identify its Value fails here.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  resetState, getState, subscribe,
  addValues, setValueSpellings, valueKey,
} from "./state.js";
import { renderIdentifyWorkspace } from "./views/identifyworkspace.js";
import { container, fire } from "./testdom.js";
import { WORKSPACE } from "./copy.js";

/** seed(category, mainText, spellings) adds one accepted value with a settled
 *  spelling list, so nothing in the render path waits on the (absent) bridge. */
function seed(category, mainText, spellings = [mainText]) {
  addValues([{ category, mainText }]);
  setValueSpellings(category, mainText, spellings);
}

/**
 * workspace() renders the Identify workspace into a fake container and keeps it
 * repainting on every state change, which is what main.js does. It returns the
 * root plus a `stop()` the test must call, because the subscription outlives the
 * test otherwise and the next one repaints through a stale root.
 */
function workspace() {
  const root = container();
  const paint = () => { renderIdentifyWorkspace(root, {}); };
  const unsubscribe = subscribe(paint);
  paint();
  return { root, stop: unsubscribe };
}

/** openValuesTab(root) clicks the My values tab, which is view state inside the
 *  workspace module and only reachable the way the user reaches it. */
async function openValuesTab(root) {
  const tab = root.querySelector('[data-wstab="values"]');
  assert.ok(tab, "the workspace renders a My values tab");
  await fire(tab, "click");
}

/** card(root, category, mainText) is the rendered card for one Value, found the
 *  way every handler finds its own identity: through the dataset. */
function card(root, category, mainText) {
  return root.querySelectorAll(".value-card")
    .find((c) => c.dataset.category === category && c.dataset.mainText === mainText) ?? null;
}

test("a rendered value card carries its identity in dataset.category and dataset.mainText", async () => {
  // The root cause, stated as an assertion: everything below depends on this
  // one lookup, and a camel-case attribute makes it undefined.
  resetState();
  seed("person_names", "Marie Duval");
  const w = workspace();
  try {
    await openValuesTab(w.root);
    const el = w.root.querySelector(".value-card");
    assert.equal(el.dataset.category, "person_names");
    assert.equal(el.dataset.mainText, "Marie Duval",
      "the card's main text must be readable as dataset.mainText after parsing");
  } finally {
    w.stop();
  }
});

test("editing a card's name renames the Value", async () => {
  resetState();
  seed("person_names", "Marie Duval");
  const w = workspace();
  try {
    await openValuesTab(w.root);
    const el = card(w.root, "person_names", "Marie Duval");
    assert.ok(el, "the seeded value has a card");

    // Clicking the name swaps it for an inline input, pre-filled with the value.
    await fire(el.querySelector(".value-name"), "click");
    const input = el.querySelector(".value-name-input");
    assert.ok(input, "clicking the name reveals an inline input");
    assert.equal(input.value, "Marie Duval", "the input starts from the current name");

    input.value = "Marie Dupont";
    await fire(input, "blur");

    assert.deepEqual(getState().values.map((v) => v.mainText), ["Marie Dupont"],
      "the rename reaches the store");
  } finally {
    w.stop();
  }
});

test("the card's remove control deletes the Value", async () => {
  resetState();
  seed("entity_names", "Acme");
  seed("entity_names", "Meridian");
  const w = workspace();
  try {
    await openValuesTab(w.root);
    const el = card(w.root, "entity_names", "Acme");
    await fire(el.querySelector(".value-remove"), "click");

    assert.deepEqual(getState().values.map((v) => v.mainText), ["Meridian"],
      "only the removed value is gone");
  } finally {
    w.stop();
  }
});

test('"Stop replacing this spelling here" drops only that spelling', async () => {
  // A spelling two values both claim is a blocking collision, which is what puts
  // the Solve conflicts panel (and this action) on the card.
  resetState();
  seed("person_names", "Marie Duval", ["Marie Duval", "Marie"]);
  seed("person_names", "Marie Dupont", ["Marie Dupont", "Marie"]);
  const w = workspace();
  try {
    await openValuesTab(w.root);
    const el = card(w.root, "person_names", "Marie Duval");
    await fire(el.querySelector(".value-solve"), "click");

    // The panel is rendered by the repaint the click triggered, so the card has
    // to be looked up again: the previous node is not in the tree any more.
    const opened = card(w.root, "person_names", "Marie Duval");
    const drop = opened.querySelector('.solve-action[data-act="drop-spelling"]');
    assert.ok(drop, "the collision offers dropping the shared spelling");
    assert.equal(drop.dataset.spelling, "Marie");
    await fire(drop, "click");

    const duval = getState().values.find((v) => v.mainText === "Marie Duval");
    const dupont = getState().values.find((v) => v.mainText === "Marie Dupont");
    assert.ok(!duval.spellings.includes("Marie"),
      `the dropped spelling is gone from this value, got ${JSON.stringify(duval.spellings)}`);
    assert.equal(duval.spellingPolicy, "curated",
      "dropping a spelling curates the value, so derivation stops re-adding it");
    assert.ok(dupont.spellings.includes("Marie") || dupont.spellingPolicy !== "curated",
      "the other value keeps its own claim on the spelling");
  } finally {
    w.stop();
  }
});

test('"Merge with" folds the two Values into one family', async () => {
  resetState();
  seed("person_names", "Marie Duval", ["Marie Duval", "Marie"]);
  seed("person_names", "Marie Dupont", ["Marie Dupont", "Marie"]);
  const w = workspace();
  try {
    await openValuesTab(w.root);
    await fire(card(w.root, "person_names", "Marie Duval").querySelector(".value-solve"), "click");

    const opened = card(w.root, "person_names", "Marie Duval");
    const merge = opened.querySelector('.solve-action[data-act="merge"]');
    assert.ok(merge, "the collision offers merging with the other value");
    assert.equal(merge.dataset.withvalue, "Marie Dupont");
    assert.equal(merge.dataset.withcategory, "person_names");
    await fire(merge, "click");

    const values = getState().values;
    assert.equal(values.length, 1, `the merge leaves one value, got ${values.length}`);
    assert.equal(values[0].mainText, "Marie Duval", "the card's own value survives the merge");
    assert.ok(values[0].spellings.includes("Marie Dupont"),
      `the folded value becomes a spelling, got ${JSON.stringify(values[0].spellings)}`);
  } finally {
    w.stop();
  }
});

test("the group picker's checkboxes carry the Value they would fold in", async () => {
  // Unreported but broken by the same attribute: Group with reads each pick's
  // dataset to build the merge, so it has to survive parsing too.
  resetState();
  seed("entity_names", "Acme");
  seed("entity_names", "Meridian");
  const w = workspace();
  try {
    await openValuesTab(w.root);
    await fire(card(w.root, "entity_names", "Acme").querySelector(".value-group"), "click");

    const picks = card(w.root, "entity_names", "Acme").querySelectorAll(".group-pick");
    assert.equal(picks.length, 1, "the other value is offered");
    assert.equal(picks[0].dataset.category, "entity_names");
    assert.equal(picks[0].dataset.mainText, "Meridian",
      "the pick names the value it would fold in");
  } finally {
    w.stop();
  }
});

test("a card that lost its identity says so instead of wiring dead controls", async () => {
  // The permanent version of this whole file: if a card's dataset ever stops
  // carrying category and main text, the user is told, rather than left with a
  // row of buttons that look enabled and change nothing.
  resetState();
  seed("entity_names", "Acme");
  const w = workspace();
  await openValuesTab(w.root);
  // Stop repainting BEFORE stripping the card. A repaint rebuilds the markup
  // from state, so a stripped node would not survive one, and the guard's own
  // notify would trigger the next repaint in an unbroken loop.
  w.stop();

  const stripped = w.root.querySelector(".value-card");
  delete stripped.dataset.mainText;

  // Present the stripped card to the same wiring pass a repaint runs, which is
  // what a browser would hand over if the attribute were misspelled in markup.
  const realQuerySelectorAll = w.root.querySelectorAll.bind(w.root);
  w.root.querySelectorAll = (sel) =>
    (sel === ".value-card" ? [stripped] : realQuerySelectorAll(sel));
  try {
    renderIdentifyWorkspace(w.root, {});
  } finally {
    delete w.root.querySelectorAll;
  }

  assert.equal(getState().notice?.text, WORKSPACE.cardIdentityLost,
    "the user is told the card cannot act");
});

test("valueKey stays the identity the handlers agree on", () => {
  // The card's data-key and every handler's lookup are the same function, so a
  // change to one has to change the other. Cheap, and it pins the pair.
  assert.equal(valueKey("person_names", "Marie Duval"), valueKey("person_names", "Marie Duval"));
  assert.notEqual(valueKey("person_names", "Marie"), valueKey("entity_names", "Marie"));
});

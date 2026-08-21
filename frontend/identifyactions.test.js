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
  resetState, getState, subscribe, answerConfirm,
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

  // The Ctrl+click selection is view state inside the workspace module, so it
  // outlives a test the way the active tab does, and a value seeded again under
  // the same name and type comes back picked. Undone through the real gesture
  // rather than by reaching into the module: a test that pokes at private state
  // stops describing what a user can do.
  for (const picked of root.querySelectorAll(".value-card").filter((c) => c.classList.contains("selected"))) {
    await fire(picked, "click", { ctrlKey: true });
  }
}

/** clearButton(root) is the tab's ONE bulk-removal control, whose label states
 *  which of its two scopes the next press acts on. */
function clearButton(root) {
  const el = root.querySelector("#btn-clear-values");
  assert.ok(el, "the My values tab renders a bulk clear");
  return el;
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

test("Ctrl+click picks a card, and Ctrl+click again lets it go", async () => {
  resetState();
  seed("entity_names", "Acme");
  seed("entity_names", "Meridian");
  const w = workspace();
  try {
    await openValuesTab(w.root);
    await fire(card(w.root, "entity_names", "Acme"), "click", { ctrlKey: true });

    assert.ok(card(w.root, "entity_names", "Acme").classList.contains("selected"),
      "the picked card is tinted");
    assert.ok(!card(w.root, "entity_names", "Meridian").classList.contains("selected"),
      "and only that one is");
    assert.equal(clearButton(w.root).textContent.trim(), WORKSPACE.clearSelected,
      "the bulk button says which of its two jobs the next press does");

    // The same gesture is the way back: a selection with no way to undo it turns
    // a mis-click into a destroyed list.
    await fire(card(w.root, "entity_names", "Acme"), "click", { ctrlKey: true });
    assert.ok(!card(w.root, "entity_names", "Acme").classList.contains("selected"),
      "the second Ctrl+click unpicks it");
    assert.equal(clearButton(w.root).textContent.trim(), WORKSPACE.clearAll,
      "with nothing picked the button is back to the whole list");
  } finally {
    w.stop();
  }
});

test("several cards can be picked at once", async () => {
  resetState();
  seed("entity_names", "Acme");
  seed("entity_names", "Meridian");
  seed("person_names", "Marie Duval");
  const w = workspace();
  try {
    await openValuesTab(w.root);
    await fire(card(w.root, "entity_names", "Acme"), "click", { ctrlKey: true });
    await fire(card(w.root, "person_names", "Marie Duval"), "click", { ctrlKey: true });

    assert.deepEqual(
      w.root.querySelectorAll(".value-card")
        .filter((c) => c.classList.contains("selected"))
        .map((c) => c.dataset.mainText),
      ["Acme", "Marie Duval"],
      "picking a second card keeps the first: this is a multi-selection");
  } finally {
    w.stop();
  }
});

test("a plain click picks nothing, and Ctrl+click on a control does its own job instead", async () => {
  resetState();
  seed("entity_names", "Acme");
  seed("entity_names", "Meridian");
  const w = workspace();
  try {
    await openValuesTab(w.root);

    // Without the modifier the card is untouched: every existing gesture on the
    // card still means what it meant.
    await fire(card(w.root, "entity_names", "Acme"), "click");
    assert.ok(!card(w.root, "entity_names", "Acme").classList.contains("selected"),
      "a plain click selects nothing");

    // A modifier does not stop a button firing, so a Ctrl+click on Remove must
    // remove and NOT also select: one gesture, one meaning.
    await fire(card(w.root, "entity_names", "Acme").querySelector(".value-remove"), "click",
      { ctrlKey: true });
    assert.deepEqual(getState().values.map((v) => v.mainText), ["Meridian"],
      "the control did its own job");
    assert.equal(clearButton(w.root).textContent.trim(), WORKSPACE.clearAll,
      "and nothing was picked on the way");
  } finally {
    w.stop();
  }
});

test("Clear selected removes the picked cards and leaves the rest", async () => {
  resetState();
  seed("entity_names", "Acme");
  seed("entity_names", "Meridian");
  seed("person_names", "Marie Duval");
  const w = workspace();
  try {
    await openValuesTab(w.root);
    await fire(card(w.root, "entity_names", "Acme"), "click", { ctrlKey: true });
    await fire(card(w.root, "person_names", "Marie Duval"), "click", { ctrlKey: true });

    // The confirm is state-backed, so the test answers it the way the modal's
    // button does. It is answered without awaiting the click first: the handler
    // is already parked on the question by the time fire() returns its promise.
    const pressed = fire(clearButton(w.root), "click");
    answerConfirm(true);
    await pressed;

    assert.deepEqual(getState().values.map((v) => v.mainText), ["Meridian"],
      "only the picked values are gone");
    assert.equal(clearButton(w.root).textContent.trim(), WORKSPACE.clearAll,
      "the selection is spent, so the button offers the whole list again");
  } finally {
    w.stop();
  }
});

test("with nothing picked the same button still clears the whole list", async () => {
  resetState();
  seed("entity_names", "Acme");
  seed("entity_names", "Meridian");
  const w = workspace();
  try {
    await openValuesTab(w.root);
    const pressed = fire(clearButton(w.root), "click");
    answerConfirm(true);
    await pressed;

    assert.deepEqual(getState().values, [], "Clear all still empties the list");
  } finally {
    w.stop();
  }
});

test("a selection does not outlive the value it points at", async () => {
  // A key left behind by a removal would leave the button reading "Clear
  // selected" with nothing selected: a press that removes nothing and says
  // nothing about why.
  resetState();
  seed("entity_names", "Acme");
  seed("entity_names", "Meridian");
  const w = workspace();
  try {
    await openValuesTab(w.root);
    await fire(card(w.root, "entity_names", "Acme"), "click", { ctrlKey: true });
    assert.equal(clearButton(w.root).textContent.trim(), WORKSPACE.clearSelected);

    await fire(card(w.root, "entity_names", "Acme").querySelector(".value-remove"), "click");
    assert.equal(clearButton(w.root).textContent.trim(), WORKSPACE.clearAll,
      "the removed card takes its selection with it");
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


test("the group picker sorts by either column and keeps the ticks it already has", async () => {
  // A re-sort must not answer "no" on the user's behalf: it reorders the SAME
  // row nodes rather than repainting, so a tick made before the sort survives it.
  resetState();
  seed("entity_names", "Nimbus");
  seed("person_names", "Zora Blake");
  seed("entity_names", "Meridian");
  const w = workspace();
  try {
    await openValuesTab(w.root);
    const cardEl = card(w.root, "entity_names", "Nimbus");
    await fire(cardEl.querySelector(".value-group"), "click");

    const names = () => [...card(w.root, "entity_names", "Nimbus")
      .querySelectorAll(".group-pick")].map((cb) => cb.dataset.mainText);
    const sortBtn = (column) => [...card(w.root, "entity_names", "Nimbus")
      .querySelectorAll(".sort-btn")].find((b) => b.dataset.sort === column);
    assert.deepEqual(names(), ["Meridian", "Zora Blake"], "value ascending is the default order");

    card(w.root, "entity_names", "Nimbus").querySelectorAll(".group-pick")[0].checked = true;

    await fire(sortBtn("value"), "click");
    assert.deepEqual(names(), ["Zora Blake", "Meridian"], "a second click reverses the column");
    const kept = [...card(w.root, "entity_names", "Nimbus").querySelectorAll(".group-pick")]
      .find((cb) => cb.dataset.mainText === "Meridian");
    assert.equal(kept.checked, true, "the tick travelled with its row");

    await fire(sortBtn("category"), "click");
    assert.deepEqual(names(), ["Meridian", "Zora Blake"], "by category, ENTITY sorts before PERSON");
  } finally {
    w.stop();
  }
});

test("the group picker's filter hides the rows that do not match", async () => {
  resetState();
  seed("entity_names", "Pinnacle");
  seed("entity_names", "Meridian");
  seed("person_names", "Zora Blake");
  const w = workspace();
  try {
    await openValuesTab(w.root);
    await fire(card(w.root, "entity_names", "Pinnacle").querySelector(".value-group"), "click");

    const shown = () => [...card(w.root, "entity_names", "Pinnacle").querySelectorAll(".group-row")]
      .filter((r) => r.style.display !== "none")
      .map((r) => r.querySelector(".group-pick").dataset.mainText);

    const input = card(w.root, "entity_names", "Pinnacle").querySelector("#group-filter");
    input.value = "zora";
    await fire(input, "input");
    assert.deepEqual(shown(), ["Zora Blake"], "only the matching row stays visible");

    input.value = "nothing here";
    await fire(input, "input");
    assert.deepEqual(shown(), [], "a filter matching nothing empties the grid");
    assert.equal(card(w.root, "entity_names", "Pinnacle").querySelector(".group-no-match").hidden, false,
      "and the empty grid says why it is empty");

    // The (x) is the same handler as typing, so clearing brings every row back.
    await fire(card(w.root, "entity_names", "Pinnacle")
      .querySelector('[data-clears="group-filter"]'), "click");
    assert.equal(shown().length, 2, "clearing the filter restores every row");
  } finally {
    w.stop();
  }
});

test("the picker explains itself in a help bubble, not in a paragraph", async () => {
  // The panel opens inside a card in a scrolling list, so the explanation is one
  // hover away and the grid keeps the line the sentence used to cost.
  resetState();
  seed("entity_names", "Halcyon");
  seed("entity_names", "Meridian");
  const w = workspace();
  try {
    await openValuesTab(w.root);
    await fire(card(w.root, "entity_names", "Halcyon").querySelector(".value-group"), "click");

    const panel = card(w.root, "entity_names", "Halcyon").querySelector(".group-panel");
    const bubbles = [...panel.querySelectorAll(".help-bubble")].map((b) => b.textContent.trim());
    assert.ok(bubbles.includes(WORKSPACE.groupWithHint), "the hint is the bubble's text");
    for (const p of panel.querySelectorAll(".hint")) {
      assert.notEqual(p.textContent.trim(), WORKSPACE.groupWithHint,
        "and it is not also a paragraph above the grid");
    }
  } finally {
    w.stop();
  }
});

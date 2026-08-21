// spellingspopup.test.js, render and wiring tests for the spellings popup.
//
// The popup owns one Value's WHOLE spelling list, and every gesture that manages
// it: add, edit, delete, and move to another Value. It exists because the compact
// card deliberately shows only what fits on one line, and the affordances it used
// to carry (a delete per chip, an inline add) are what made its height a function
// of its data.
//
// The wiring half is driven through testdom.js rather than asserted as a string,
// for the reason ../dataset_parity_test.go exists: a browser's parser lower-cases
// attribute names, so a camel-case `data-` attribute renders, satisfies every
// string assertion, and reaches no handler. Every row here carries its spelling
// in a `data-` attribute a handler then reads back, so that failure mode is under
// test rather than assumed away.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  resetState, getState, setState, subscribe,
  addValues, setValueSpellings, answerChoice, spellingsOf,
} from "./state.js";
import {
  renderIdentifyWorkspace, spellingsPopupHTML, applySpellingsSearchFilter,
} from "./views/identifyworkspace.js";
import { container, fire } from "./testdom.js";
import { all, one, textOf, stripTags } from "./testhtml.js";
import { WORKSPACE } from "./copy.js";

/** seed(category, mainText, spellings) adds one accepted value with a settled
 *  spelling list, so nothing in the render path waits on the (absent) bridge. */
function seed(category, mainText, spellings = [mainText]) {
  addValues([{ category, mainText }]);
  setValueSpellings(category, mainText, spellings);
}

/** workspace() renders the Identify workspace into a fake container and keeps it
 *  repainting on every state change, which is what main.js does. */
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

/** openPopup(root, mainText, how) opens the popup the way a user does, from one
 *  of the two controls on the card. */
async function openPopup(root, mainText, how = ".spelling-add") {
  const card = root.querySelectorAll(".value-card")
    .find((c) => c.dataset.mainText === mainText);
  assert.ok(card, `the card for ${mainText} is rendered`);
  const control = card.querySelector(how);
  assert.ok(control, `the card offers ${how}`);
  await fire(control, "click");
  const layer = root.querySelector(".spellings-layer");
  assert.ok(layer, "the popup opened");
  return layer;
}

/** rowFor(layer, spelling) is one listed spelling's row, found the way every
 *  handler finds it: through the dataset the parser lower-cased. */
function rowFor(layer, spelling) {
  return layer.querySelectorAll(".spelling-list-row")
    .find((r) => r.dataset.spellingRow === spelling) ?? null;
}

// --- What the popup shows -------------------------------------------------

test("nothing renders while no popup is open", () => {
  resetState();
  seed("entity_names", "Meridian", ["Meridian Consulting", "Meridian Group", "MC"]);
  const { root, stop } = workspace();
  try {
    // The render half is asserted against the builder, which needs the popup
    // open; the wiring tests below open it through the card.
    setState({});
    const html = spellingsPopupHTML(getState());
    assert.equal(html, "", "nothing is open, so nothing is rendered");
  } finally { stop(); }
});

test("the popup lists the spellings, and only the spellings", async () => {
  resetState();
  seed("entity_names", "Meridian", ["Meridian Consulting", "Meridian Group", "MC"]);
  const { root, stop } = workspace();
  try {
    await openValuesTab(root);
    await openPopup(root, "Meridian");

    const html = spellingsPopupHTML(getState());
    // The family is named in the title, which is why the list needs no row for
    // its head: the main text is not a spelling and neither row action applies
    // to it, so such a row carried nothing but the reason it was unusable.
    assert.match(textOf(html, "div.modal-head"), /Spellings for Meridian/);
    const rows = all(html, "div.spelling-list-row");
    assert.equal(rows.length, 3, "one row per spelling, and nothing else");
    for (const row of rows) {
      assert.ok(row.attrs["data-spelling-row"], "every row is a spelling with actions on it");
    }
    // No count line either: it said what the rows already say, and it made the
    // search look like it had deleted something.
    assert.ok(!/spellings?<\/p>/.test(html), "the list is not counted above itself");
  } finally { stop(); }
});

test("the list carries no ROLE column", async () => {
  // Every row is a spelling now, so a column saying so said it three times.
  resetState();
  seed("entity_names", "Meridian", ["Meridian Consulting", "MC"]);
  const { root, stop } = workspace();
  try {
    await openValuesTab(root);
    await openPopup(root, "Meridian");

    const html = spellingsPopupHTML(getState());
    assert.equal(all(html, "span.spelling-list-role").length, 0, "no role cell on any row");
    const head = stripTags(one(html, "div.spelling-list-head").inner);
    assert.ok(!/ROLE/.test(head), `no role caption over the list, got ${head}`);
  } finally { stop(); }
});

test("the popup says its edits are already applied", async () => {
  // There is no OK button, and the absence is the thing to explain.
  resetState();
  seed("entity_names", "Meridian", ["MC"]);
  const { root, stop } = workspace();
  try {
    await openValuesTab(root);
    await openPopup(root, "Meridian");
    assert.ok(stripTags(spellingsPopupHTML(getState())).includes(WORKSPACE.spellingsPopupLive));
  } finally { stop(); }
});

// --- What the popup DOES --------------------------------------------------

test("both card controls open the same popup", async () => {
  resetState();
  // Long enough to genuinely overflow the one-line budget: a fixture that fits
  // would prove nothing about the overflow control.
  seed("entity_names", "Meridian", [
    "Meridian Consulting Group Societe Anonyme", "Meridian Consulting Group",
    "Meridian Consulting", "Meridian", "MC",
  ]);
  const { root, stop } = workspace();
  try {
    await openValuesTab(root);
    await openPopup(root, "Meridian", ".spelling-more");
    assert.ok(root.querySelector(".spellings-layer"), '"+N more" opens it');

    await fire(root.querySelector(".spellings-close"), "click");
    assert.equal(root.querySelector(".spellings-layer"), null, "and Close dismisses it");

    await openPopup(root, "Meridian", ".spelling-add");
    assert.ok(root.querySelector(".spellings-layer"), '"+ add" opens the same surface');
  } finally { stop(); }
});

test("Add appends a spelling and the card updates live", async () => {
  resetState();
  seed("person_names", "Marie Duval", ["Marie Duval"]);
  const { root, stop } = workspace();
  try {
    await openValuesTab(root);
    const layer = await openPopup(root, "Marie Duval");

    const draft = layer.querySelector("#spelling-draft");
    draft.value = "M. Duval";
    await fire(draft, "input");
    await fire(root.querySelector("#btn-add-spelling"), "click");

    const value = getState().values[0];
    assert.ok([...spellingsOf(value).values()].includes("M. Duval"), "the spelling is on the Value");
    // Adding does NOT curate: a spelling the user typed is one MORE form of the
    // same thing, so the derivation still has work to do and the row goes back to
    // pending for it. Deleting is what curates, because that is the one edit an
    // automatic re-derivation would undo.
    assert.equal(value.derivedSpellings, null, "the row is queued for re-derivation");
    assert.notEqual(value.spellingPolicy, "curated");

    // Live: the compact card behind the popup reads the same store, so the same
    // repaint updates it. No OK, nothing to apply.
    const card = root.querySelectorAll(".value-card")
      .find((c) => c.dataset.mainText === "Marie Duval");
    const chips = card.querySelectorAll(".spelling-chip").map((c) => c.dataset.spelling);
    assert.ok(chips.includes("M. Duval"), `the card shows it too, got ${chips}`);
  } finally { stop(); }
});

test("Delete removes one spelling and curates the value", async () => {
  resetState();
  seed("person_names", "Marie Duval", ["Marie Duval", "Marie"]);
  const { root, stop } = workspace();
  try {
    await openValuesTab(root);
    const layer = await openPopup(root, "Marie Duval");

    const row = rowFor(layer, "Marie");
    assert.ok(row, "the spelling has a row");
    await fire(row.querySelector(".spelling-delete"), "click");

    const value = getState().values[0];
    const left = [...spellingsOf(value).values()];
    assert.ok(!left.includes("Marie"), `the spelling is gone, got ${left}`);
    assert.equal(value.spellingPolicy, "curated",
      "the deletion sticks, or the next derivation puts it straight back");
  } finally { stop(); }
});

test("editing a row's text renames the spelling in place", async () => {
  // Editing rather than delete-then-add: delete-then-add moves the row to the end
  // of the list and curates twice for one correction.
  resetState();
  seed("person_names", "Marie Duval", ["Marie Duval", "Marei"]);
  const { root, stop } = workspace();
  try {
    await openValuesTab(root);
    const layer = await openPopup(root, "Marie Duval");

    const row = rowFor(layer, "Marei");
    await fire(row.querySelector(".spelling-list-edit"), "click");
    const input = row.querySelector(".spelling-input");
    assert.ok(input, "the row reveals an inline input");
    input.value = "Marie";
    await fire(input, "blur");

    const left = [...spellingsOf(getState().values[0]).values()];
    assert.ok(left.includes("Marie"), `the corrected spelling is there, got ${left}`);
    assert.ok(!left.includes("Marei"), "and the typo is not");
  } finally { stop(); }
});

test("Move to sends a spelling to the value the user picks", async () => {
  // This is how a spelling in the "+N more" overflow is regrouped: dragging a
  // chip only reaches the ones the card shows.
  resetState();
  seed("person_names", "Marie Duval", ["Marie Duval", "Duval"]);
  seed("person_names", "Jean Duval", ["Jean Duval"]);
  const { root, stop } = workspace();
  try {
    await openValuesTab(root);
    const layer = await openPopup(root, "Marie Duval");

    const row = rowFor(layer, "Duval");
    const moving = fire(row.querySelector(".spelling-move"), "click");
    // The picker is the shell's own pick-one dialog, the same one "Group with"
    // uses, so there is one way to name a target Value.
    const question = getState().confirm;
    assert.ok(question?.choices?.length, "a target picker is offered");
    assert.deepEqual(question.choices.map((c) => c.label), ["Jean Duval (Person names)"]);
    answerChoice(question.choices[0].id);
    await moving;

    const marie = getState().values.find((v) => v.mainText === "Marie Duval");
    const jean = getState().values.find((v) => v.mainText === "Jean Duval");
    assert.ok(![...spellingsOf(marie).values()].includes("Duval"), "it left the source");
    assert.ok([...spellingsOf(jean).values()].includes("Duval"), "and joined the target");
  } finally { stop(); }
});

test("Move to with no other value says so instead of opening an empty picker", async () => {
  resetState();
  seed("person_names", "Marie Duval", ["Marie Duval", "Duval"]);
  const { root, stop } = workspace();
  try {
    await openValuesTab(root);
    const layer = await openPopup(root, "Marie Duval");
    await fire(rowFor(layer, "Duval").querySelector(".spelling-move"), "click");

    assert.equal(getState().confirm, null, "no picker with nothing in it");
    assert.equal(getState().notice?.text, WORKSPACE.spellingsPopupMoveNone);
  } finally { stop(); }
});

test("the popup's search filters rows in place, without replacing the input", async () => {
  // The same reason the values search filters in place: a re-render rewrites the
  // container with innerHTML and destroys the input mid-type, so focus and the
  // caret are lost after every character.
  resetState();
  seed("entity_names", "Meridian", ["Meridian Consulting", "Meridian Group", "Helios"]);
  const { root, stop } = workspace();
  try {
    await openValuesTab(root);
    const layer = await openPopup(root, "Meridian");

    const search = layer.querySelector("#spellings-search");
    search.value = "helios";
    await fire(search, "input");

    assert.equal(layer.querySelector("#spellings-search"), search,
      "the very same input node, or the caret is lost after every character");
    const visible = layer.querySelectorAll(".spelling-list-row")
      .filter((r) => r.style.display !== "none")
      .map((r) => r.dataset.spellingRow);
    assert.deepEqual(visible, ["Helios"]);
  } finally { stop(); }
});

test("applySpellingsSearchFilter reveals the no-match line when nothing matches", () => {
  resetState();
  seed("entity_names", "Meridian", ["Meridian Consulting"]);
  const { root, stop } = workspace();
  try {
    const rendered = container();
    // Rendered from the builder rather than through the card, because this is the
    // pure filter and it must work on whatever DOM holds the rows.
    rendered.innerHTML =
      `<div class="spelling-list-row" data-spelling-row="a" data-search="alpha"></div>` +
      `<div class="spelling-list-row" data-spelling-row="b" data-search="beta"></div>` +
      `<p class="hint" id="spellings-popup-nomatch" style="display:none"></p>`;
    assert.equal(applySpellingsSearchFilter(rendered, "al"), 1);
    assert.equal(rendered.querySelector("#spellings-popup-nomatch").style.display, "none");
    assert.equal(applySpellingsSearchFilter(rendered, "zzz"), 0);
    assert.equal(rendered.querySelector("#spellings-popup-nomatch").style.display, "");
    assert.equal(applySpellingsSearchFilter(rendered, ""), 2, "an empty search hides nothing");
  } finally { stop(); }
});

test("the popup closes when its Value stops existing", async () => {
  // A Value can be removed from another surface while the popup is open, and a
  // surface pointing at nothing is worse than no surface.
  resetState();
  seed("person_names", "Marie Duval", ["Marie Duval"]);
  const { root, stop } = workspace();
  try {
    await openValuesTab(root);
    await openPopup(root, "Marie Duval");

    setState({ values: [] });
    assert.equal(root.querySelector(".spellings-layer"), null,
      "nothing is rendered for a Value that is gone");
  } finally { stop(); }
});

test("the popup search's ✕ clears the filter and shows every row again", async () => {
  resetState();
  seed("entity_names", "Meridian", ["Meridian Consulting", "Meridian Group", "Helios"]);
  const { root, stop } = workspace();
  try {
    await openValuesTab(root);
    const layer = await openPopup(root, "Meridian");

    const search = layer.querySelector("#spellings-search");
    search.value = "helios";
    await fire(search, "input");
    assert.equal(
      layer.querySelectorAll(".spelling-list-row").filter((r) => r.style.display !== "none").length,
      1, "the filter narrowed the list");

    const clear = layer.querySelector('[data-clears="spellings-search"]');
    assert.ok(clear, "the popup's search carries a ✕ like every other search field");
    await fire(clear, "click");

    assert.equal(search.value, "", "the field is empty");
    assert.equal(layer.querySelector("#spellings-search"), search,
      "the very same input node: clearing filters in place, exactly as typing does");
    const visible = layer.querySelectorAll(".spelling-list-row")
      .filter((r) => r.style.display !== "none");
    assert.equal(visible.length, 3, "every row is back, all three spellings");
  } finally { stop(); }
});

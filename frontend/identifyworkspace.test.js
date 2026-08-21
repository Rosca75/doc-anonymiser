// identifyworkspace.test.js, tests for the detection progress strip
//
// The reported issue was "detection sometimes does not complete, the progress
// is difficult to follow". The Go side owns completion (one terminal event) and
// the progress arithmetic (one monotonic fraction across the whole run); what
// is left here is what the user reads, and these tests pin it:
//
//   the percentage comes from Go and is NEVER recomputed per route, which is
//   what used to make the bar jump backwards mid-run;
//   the caption names the route, the file, the part of the file and the
//   elapsed time, because those are the questions a run that feels stuck
//   raises.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  subscribe, resetState, getState, addValues, setValueSpellings, setState,
  addSpelling, renameValue, changeValueCategory, curate, spellingsOf,
} from "./state.js";
import {
  progressStrip, detectionCaption,
  applyValuesSearchFilter, wireValuesToolbar, valuesTab,
} from "./views/identifyworkspace.js";
import { attr, textOf } from "./testhtml.js";
import { WORKSPACE } from "./copy.js";
import { container, fire } from "./testdom.js";

/** running(patch) is a detection state as main.js builds it from an event. */
function running(patch = {}) {
  return {
    discovery: {
      running: true, phase: "smart", phaseIndex: 0, phaseCount: 1,
      current: 0, total: 3, file: "a.docx",
      chunk: 0, chunkCount: 0, fraction: 0.25, startedAt: Date.now(),
      ...patch,
    },
  };
}

test("no bar unless a run is actually in flight", () => {
  // The gate is `=== true` on purpose: a leftover object must
  // not resurrect the bar.
  assert.equal(progressStrip({ discovery: null }), "");
  assert.equal(progressStrip({ discovery: { running: false, fraction: 0.5 } }), "");
  assert.equal(progressStrip({}), "");
});

test("the bar width is Go's fraction, not a recomputed one", () => {
  const html = progressStrip(running({ fraction: 0.42 }));
  assert.match(attr(html, "div.progress-bar", "class"), /progress-bar/);
  assert.match(html, /width:42%/);
});

test("an out-of-range fraction is clamped rather than rendered as nonsense", () => {
  assert.match(progressStrip(running({ fraction: 1.4 })), /width:100%/);
  assert.match(progressStrip(running({ fraction: -1 })), /width:0%/);
  assert.match(progressStrip(running({ fraction: undefined })), /width:0%/);
});

test("the caption names the route when more than one is running", () => {
  const caption = detectionCaption(running({
    phase: "ai", phaseIndex: 1, phaseCount: 2, startedAt: null,
  }).discovery);
  assert.match(caption, /Local LLM discovery \(2\/2\)/);
  // Two routes read the same files twice; without the route name the second
  // pass looks like the first one starting over.
  assert.match(caption, /a\.docx \(1 of 3\)/);
});

test("the caption reports the position inside a chunked file", () => {
  // A long AI scan used to sit on one unchanging caption for minutes, which
  // is indistinguishable from a hung run.
  const caption = detectionCaption(running({
    phase: "ai", chunk: 6, chunkCount: 20, startedAt: null,
  }).discovery);
  assert.match(caption, /part 7 of 20/);
});

test("a single chunk is not reported: 'part 1 of 1' is noise", () => {
  const caption = detectionCaption(running({ chunkCount: 1, startedAt: null }).discovery);
  assert.ok(!caption.includes("part"), caption);
});

test("the caption carries elapsed time so a slow run does not read as a hung one", () => {
  const caption = detectionCaption(running({ startedAt: Date.now() - 95000 }).discovery);
  assert.match(caption, /1m 35s/);
});

test("the strip renders the caption it computed", () => {
  const state = running({ startedAt: null });
  assert.equal(textOf(progressStrip(state), "#detect-caption"),
    detectionCaption(state.discovery));
});

// --- CR5: the My values search filters in place, without re-rendering -----
//
// The bug: the toolbar called setState on every keystroke, which rewrites the
// workspace via innerHTML and destroys the search input mid-type, losing focus
// and the caret. The fix filters the already-rendered cards in place. There is
// no real DOM here (the suite runs under `node --test`, zero npm deps), so
// these build the minimum shape the code touches: a container whose
// querySelector/querySelectorAll resolve the search input and the value cards,
// and an input that records focus locally.

/** harness(cardSearchTexts) is a fake workspace: value cards carrying a
 *  data-search string, the search input, the hidden no-match line, and a local
 *  activeElement the input's focus() sets. */
function harness(cardSearchTexts) {
  let active = null;
  const listeners = {};
  const input = {
    value: "", selectionStart: 0, style: {}, dataset: {},
    addEventListener(type, fn) { (listeners[type] ??= []).push(fn); },
    fire(type) { for (const fn of (listeners[type] ?? [])) fn(); },
    focus() { active = input; },
    setSelectionRange() {},
  };
  const cards = cardSearchTexts.map((t) => ({ dataset: { search: t }, style: {} }));
  const empty = { style: { display: "none" } };
  // The field's own ✕. It is part of the control, so the harness has to answer
  // for it or the wiring silently binds half of what it renders.
  const clearListeners = {};
  const clear = {
    addEventListener(type, fn) { (clearListeners[type] ??= []).push(fn); },
    fire(type) { for (const fn of (clearListeners[type] ?? [])) fn(); },
  };
  const container = {
    querySelector(sel) {
      if (sel === "#values-search") return input;
      if (sel === '[data-clears="values-search"]') return clear;
      if (sel === ".values-search-empty") return empty;
      return null;
    },
    querySelectorAll(sel) { return sel === ".value-card" ? cards : []; },
  };
  return { container, input, cards, empty, clear, getActive: () => active };
}

test("applyValuesSearchFilter shows matching cards and reveals the empty line when none match", () => {
  const h = harness(["meridian consulting merid", "marie duval"]);

  assert.equal(applyValuesSearchFilter(h.container, "merid"), 1);
  assert.equal(h.cards[0].style.display, "");
  assert.equal(h.cards[1].style.display, "none");
  assert.equal(h.empty.style.display, "none", "some match, so no empty line");

  assert.equal(applyValuesSearchFilter(h.container, "zzz"), 0);
  assert.equal(h.empty.style.display, "", "nothing matches, so the empty line shows");

  assert.equal(applyValuesSearchFilter(h.container, ""), 2, "an empty query shows every card");
  assert.equal(h.cards[0].style.display, "");
  assert.equal(h.cards[1].style.display, "");
});

test("typing in the My values search filters in place, keeps focus, and never repaints", () => {
  resetState();
  let repaints = 0;
  const unsub = subscribe(() => { repaints++; });

  const h = harness(["meridian consulting merid", "marie duval"]);
  wireValuesToolbar(h.container);
  h.input.focus(); // the user is typing into it

  for (const prefix of ["m", "me", "mer", "meri", "merid"]) {
    h.input.value = prefix;
    h.input.fire("input");
    assert.equal(h.getActive(), h.input, `focus stays on the input after "${prefix}"`);
    assert.equal(h.input.value, prefix, "the value grows to the full typed string");
  }
  unsub();

  assert.equal(repaints, 0, "no setState/repaint on any keystroke");
  assert.equal(h.cards[0].style.display, "", "the matching card stays visible");
  assert.equal(h.cards[1].style.display, "none", "the non-matching card is hidden");
});

test("the My values ✕ empties the search and brings every card back", () => {
  // The same handler typing runs, so the filtered rows and the hidden no-match
  // line cannot be left behind by the clear.
  resetState();
  let repaints = 0;
  const unsub = subscribe(() => { repaints++; });

  const h = harness(["meridian consulting merid", "marie duval"]);
  wireValuesToolbar(h.container);

  h.input.value = "zzz";
  h.input.fire("input");
  assert.equal(h.cards[0].style.display, "none", "nothing matches, so every card is hidden");
  assert.equal(h.empty.style.display, "", "and the no-match line is showing");

  h.clear.fire("click");
  unsub();

  assert.equal(h.input.value, "", "the field is empty");
  assert.equal(h.getActive(), h.input, "focus goes back to it: clearing precedes typing again");
  assert.equal(h.cards[0].style.display, "", "every card is back");
  assert.equal(h.cards[1].style.display, "");
  assert.equal(h.empty.style.display, "none", "and the no-match line is hidden again");
  assert.equal(repaints, 0,
    "clearing filters in place too, or it would destroy the very input it just focused");
});

test("the ✕ restores cards a re-render hid while the search was active", async () => {
  // The reported bug lived at the seam between the two ways the list narrows.
  // The search filters cards IN PLACE (no repaint, so the caret survives), but a
  // re-render (a type change, an add, an accept) rebuilds the list, and it used
  // to render only the cards the active search matched. That pruned the rest
  // from the DOM, and the in-place ✕ can only unhide what is present, so clearing
  // the search left the list stuck on the searched subset: the ✕ "did nothing".
  //
  // The render now keeps every type match in the DOM and the search narrows only
  // in place, so this drives the exact sequence and proves the ✕ brings the
  // hidden cards back. Real DOM (testdom), because it is a wiring fact.
  resetState();
  for (const name of ["Marie Duval", "Thomas Berger", "Alice Nowak"]) {
    addValues([{ category: "person_names", mainText: name }]);
    setValueSpellings("person_names", name, [name]);
  }

  const c = container();
  c.innerHTML = valuesTab(getState());
  wireValuesToolbar(c);
  const visible = () =>
    c.querySelectorAll(".value-card").filter((card) => card.style.display !== "none").length;

  // The user searches for one of the three. The toolbar hides the other two.
  const search = c.querySelector("#values-search");
  search.value = "Marie";
  await fire(search, "input");
  assert.equal(visible(), 1, "the search narrows to the one matching card");

  // A repaint lands while the search is still active. Every setState reason (a
  // type change, an add, an accept) rebuilds the list exactly this way.
  c.innerHTML = valuesTab(getState());
  assert.equal(c.querySelectorAll(".value-card").length, 3,
    "the render keeps all three cards in the DOM despite the active search, "
    + "so the in-place clear has something to reveal");
  wireValuesToolbar(c);
  applyValuesSearchFilter(c); // as wireValues does after each render
  assert.equal(visible(), 1, "the live search still narrows to one in place");

  // Clearing with the field's own ✕ brings the other two back.
  await fire(c.querySelector('[data-clears="values-search"]'), "click");
  assert.equal(c.querySelector("#values-search").value, "", "the ✕ empties the field");
  assert.equal(visible(), 3, "and every card is shown again");
});

// --- The card of a CURATED Value, read as the user reads it -------------------
//
// These assertions are on the RENDERED TEXT of the card, through the minimal DOM,
// because that is where the bug was visible and nowhere else: the store held the
// right chips, the reducers ran, ninety-six spelling and value tests passed, and
// the card still said "working out the other spellings..." forever. The absence
// of a test at this level is what let it live, so the test is as much of the fix
// as the code change (docs/TESTING.md: a wiring test when the question is what a
// control DOES).

/** curatedCard(spellings) seeds ONE curated Value and returns its rendered card. */
function seedCurated(category, mainText, spellings) {
  resetState();
  addValues([{ category, mainText }]);
  setValueSpellings(category, mainText, spellings);
  setState({
    values: getState().values.map((e) =>
      e.mainText === mainText ? curate(e, spellings) : e),
  });
}

/** cardText() is the text of the one value card currently rendered. */
function cardText() {
  const c = container();
  c.innerHTML = valuesTab(getState());
  return c.querySelector(".value-card").textContent;
}

test("amending a curated Value leaves its card showing chips and NO pending hint", async () => {
  const gestures = [
    ["addSpelling", () => addSpelling("entity_names", "Northstar", "NStar 2")],
    ["renameValue", () => renameValue("entity_names", "Northstar", "Northstar Group")],
    ["changeValueCategory", () => changeValueCategory("entity_names", "Northstar", "brand_names")],
  ];
  for (const [name, gesture] of gestures) {
    seedCurated("entity_names", "Northstar", ["Northstar", "NStar"]);
    gesture();
    const text = cardText();
    assert.ok(!text.includes(WORKSPACE.spellingsPending),
      `after ${name} the card still claims to be working, and no expansion will ever `
      + `arrive to clear it:\n${text}`);
    assert.ok(text.includes("NStar"),
      `after ${name} the card lost the chips it is meant to be showing:\n${text}`);
  }
});

test("renaming a MainText onto the row's own spelling loses no form", () => {
  // Data loss, not a cosmetic bug: the old main text lived in derivedSpellings,
  // an ordinary rename cleared that cache, and the Value that replaced both
  // "Northstar" and "NStar" quietly began replacing only "NStar" while the call
  // reported success.
  resetState();
  addValues([{ category: "entity_names", mainText: "Northstar" }]);
  setValueSpellings("entity_names", "Northstar", ["Northstar", "NStar"]);

  const before = new Set([...spellingsOf(getState().values[0]).keys()]);
  assert.equal(renameValue("entity_names", "Northstar", "NStar"), "",
    "the promotion succeeds; refusing it would be defensible, losing a form is not");

  const row = getState().values[0];
  assert.equal(row.mainText, "NStar", "the promoted spelling becomes the main text");
  assert.equal(row.spellingPolicy, "curated",
    "the row curates, so nothing can re-derive the pair back apart");
  assert.deepEqual([...spellingsOf(row).keys()].sort(), [...before].sort(),
    "the set of forms this Value replaces is unchanged: a promotion moves the "
    + "main text, it does not drop one");

  const text = cardText();
  assert.ok(text.includes("Northstar"), `the old name is still on the card:\n${text}`);
  assert.ok(!text.includes(WORKSPACE.spellingsPending),
    `a promoted row is settled, so it must not read as still working:\n${text}`);
});

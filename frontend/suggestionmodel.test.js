// suggestionmodel.test.js, tests for the Suggestions-tab view-model.
//
// The table has a search box, a type selector, a discovery-method selector and a
// sort toggle. All four run through visibleSuggestions, and the bulk accept and
// reject buttons act on exactly what it returns, so a bug here would either hide
// rows a bulk action then swept up, or act on rows the user could not see. That
// makes this the highest-value pure function on the Identify step.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  visibleSuggestions,
  toggleCountSort, toggleValueSort, DEFAULT_SUGGESTION_FILTER,
} from "./suggestionmodel.js";

/** fixture() is the shared Suggestion list: mixed categories, counts and
 *  discovery methods, including one row two methods found. */
function fixture() {
  return [
    { mainText: "Marie Duval", category: "person_names", count: 7, discoveryMethods: ["heuristic", "signal"] },
    { mainText: "Alpine Trust", category: "entity_names", count: 3, discoveryMethods: ["heuristic"] },
    { mainText: "Anouk Berger", category: "person_names", count: 3, discoveryMethods: ["local_llm"] },
    { mainText: "Project Borealis", category: "project_names", count: 1, discoveryMethods: ["heuristic"] },
    { mainText: "Helpdesk", category: "entity_names", count: 12, discoveryMethods: ["heuristic"] },
  ];
}

const texts = (rows) => rows.map((r) => r.mainText);

// --- The neutral filter --------------------------------------------------

test("the default filter shows everything, most frequent first", () => {
  const rows = visibleSuggestions(fixture(), DEFAULT_SUGGESTION_FILTER);
  assert.deepEqual(texts(rows), [
    "Helpdesk", "Marie Duval", "Alpine Trust", "Anouk Berger", "Project Borealis",
  ]);
});

test("an omitted filter behaves as the default", () => {
  assert.deepEqual(texts(visibleSuggestions(fixture())), texts(visibleSuggestions(fixture(), DEFAULT_SUGGESTION_FILTER)));
});

test("the input list is never mutated", () => {
  const input = fixture();
  const before = texts(input);
  visibleSuggestions(input, { ...DEFAULT_SUGGESTION_FILTER, sort: "value-asc" });
  assert.deepEqual(texts(input), before,
    "the store's suggestion order must survive a view sort");
});

test("an empty or missing list is handled", () => {
  assert.deepEqual(visibleSuggestions([], DEFAULT_SUGGESTION_FILTER), []);
  assert.deepEqual(visibleSuggestions(undefined, DEFAULT_SUGGESTION_FILTER), []);
});

// --- the value search ----------------------------------------------------

test("the search matches anywhere in the value, case-insensitively", () => {
  const search = (q) => texts(visibleSuggestions(fixture(), { ...DEFAULT_SUGGESTION_FILTER, search: q }));
  assert.deepEqual(search("duv"), ["Marie Duval"]);
  assert.deepEqual(search("DUVAL"), ["Marie Duval"]);
  assert.deepEqual(search("berger"), ["Anouk Berger"]);
  // A substring shared by several values returns them all, still in the
  // active sort order (descending count here).
  assert.deepEqual(search("r"), [
    "Marie Duval", "Alpine Trust", "Anouk Berger", "Project Borealis",
  ]);
});

test("the search ignores accidental surrounding spaces", () => {
  const rows = visibleSuggestions(fixture(), { ...DEFAULT_SUGGESTION_FILTER, search: "  alpine  " });
  assert.deepEqual(texts(rows), ["Alpine Trust"]);
});

test("a search matching nothing returns an empty list, not everything", () => {
  const rows = visibleSuggestions(fixture(), { ...DEFAULT_SUGGESTION_FILTER, search: "zzzz" });
  assert.deepEqual(rows, []);
});

// --- the type selector ---------------------------------------------------

test("the category filter keeps only that type", () => {
  const rows = visibleSuggestions(fixture(), { ...DEFAULT_SUGGESTION_FILTER, category: "person_names" });
  assert.deepEqual(texts(rows), ["Marie Duval", "Anouk Berger"]);
});

test("an empty category means all types", () => {
  assert.equal(visibleSuggestions(fixture(), { ...DEFAULT_SUGGESTION_FILTER, category: "" }).length, 5);
});

test("search and category compose", () => {
  const rows = visibleSuggestions(fixture(), {
    ...DEFAULT_SUGGESTION_FILTER, category: "person_names", search: "anouk",
  });
  assert.deepEqual(texts(rows), ["Anouk Berger"]);
});

// --- the sorts -----------------------------------------------------------

test("occurrences sort both ways", () => {
  const desc = visibleSuggestions(fixture(), { ...DEFAULT_SUGGESTION_FILTER, sort: "count-desc" });
  assert.deepEqual(desc.map((r) => r.count), [12, 7, 3, 3, 1]);
  const asc = visibleSuggestions(fixture(), { ...DEFAULT_SUGGESTION_FILTER, sort: "count-asc" });
  assert.deepEqual(asc.map((r) => r.count), [1, 3, 3, 7, 12]);
});

test("equal counts tie-break by value, so rows never jump between renders", () => {
  const rows = visibleSuggestions(fixture(), { ...DEFAULT_SUGGESTION_FILTER, sort: "count-desc" });
  const tied = rows.filter((r) => r.count === 3).map((r) => r.mainText);
  assert.deepEqual(tied, ["Alpine Trust", "Anouk Berger"]);
  // Same input, same output, every time.
  for (let i = 0; i < 5; i++) {
    assert.deepEqual(texts(visibleSuggestions(fixture(), { ...DEFAULT_SUGGESTION_FILTER, sort: "count-desc" })), texts(rows));
  }
});

test("value sort both ways", () => {
  const asc = visibleSuggestions(fixture(), { ...DEFAULT_SUGGESTION_FILTER, sort: "value-asc" });
  assert.deepEqual(texts(asc), [
    "Alpine Trust", "Anouk Berger", "Helpdesk", "Marie Duval", "Project Borealis",
  ]);
  const desc = visibleSuggestions(fixture(), { ...DEFAULT_SUGGESTION_FILTER, sort: "value-desc" });
  assert.deepEqual(texts(desc), texts(asc).reverse());
});

test("an unknown sort key falls back to the default instead of throwing", () => {
  const rows = visibleSuggestions(fixture(), { ...DEFAULT_SUGGESTION_FILTER, sort: "colour-desc" });
  assert.deepEqual(texts(rows), texts(visibleSuggestions(fixture(), DEFAULT_SUGGESTION_FILTER)));
});

test("the sort toggles flip and adopt", () => {
  assert.equal(toggleCountSort("count-desc"), "count-asc");
  assert.equal(toggleCountSort("count-asc"), "count-desc");
  assert.equal(toggleCountSort("value-asc"), "count-desc", "adopts descending from another column");
  assert.equal(toggleValueSort("value-asc"), "value-desc");
  assert.equal(toggleValueSort("value-desc"), "value-asc");
  assert.equal(toggleValueSort("count-desc"), "value-asc");
});

// --- the bulk-action counts ----------------------------------------------




// --- The across-category bulk actions ------------------------------------
//
// The suggestions table sorts and filters across EVERY category at once, so
// "shown" is not a category: it is whatever survived the search, the type filter
// and the method filter. A per-category button cannot express that, which is why
// acceptAllShown / rejectAllShown exist beside it rather than replacing it.

import {
  resetState, getState, subscribe, addSuggestions, acceptAllShown, rejectAllShown,
} from "./state.js";

/** threeSuggestions() seeds a list spanning two categories and two methods. */
function threeSuggestions() {
  resetState();
  addSuggestions([
    { discoveryMethods: ["heuristic"], mainText: "Marie Duval", category: "person_names", count: 14 },
    { discoveryMethods: ["heuristic"], mainText: "Thomas Berger", category: "person_names", count: 9 },
  ]);
  addSuggestions([
    { discoveryMethods: ["local_llm"], mainText: "Meridian Consulting", category: "entity_names", count: 6 },
  ]);
}

test("acceptAllShown promotes rows ACROSS categories in one action", () => {
  threeSuggestions();
  const added = acceptAllShown(["Marie Duval", "Meridian Consulting"]);
  assert.equal(added, 2);
  const s = getState();
  // Each keeps its OWN category: the bulk action is about which rows, not about
  // what they are.
  assert.deepEqual(
    s.values.map((e) => `${e.category}:${e.mainText}`).sort(),
    ["entity_names:Meridian Consulting", "person_names:Marie Duval"]);
  // The row that was not shown is still waiting.
  assert.deepEqual(s.suggestions.map((r) => r.mainText), ["Thomas Berger"]);
});

test("acceptAllShown ignores values that are not suggestions", () => {
  // The view passes the filtered list; a stale entry in it must not invent a
  // Value out of nothing.
  threeSuggestions();
  const added = acceptAllShown(["Marie Duval", "Never Proposed"]);
  assert.equal(added, 1);
  assert.equal(getState().values.length, 1);
});

test("acceptAllShown on an empty selection changes nothing", () => {
  threeSuggestions();
  for (const empty of [[], undefined, null]) {
    assert.equal(acceptAllShown(empty), 0, JSON.stringify(empty));
    assert.equal(getState().suggestions.length, 3);
    assert.equal(getState().values.length, 0);
  }
});

test("acceptAllShown matches case-insensitively, like every suggestion lookup", () => {
  threeSuggestions();
  assert.equal(acceptAllShown(["marie duval"]), 1);
  assert.equal(getState().values[0].mainText, "Marie Duval",
    "the stored value keeps the spelling that was proposed");
});

test("rejectAllShown drops rows without promoting any", () => {
  threeSuggestions();
  const removed = rejectAllShown(["Marie Duval", "Meridian Consulting"]);
  assert.equal(removed, 2);
  const s = getState();
  assert.deepEqual(s.values, [], "rejecting must add nothing");
  assert.deepEqual(s.suggestions.map((r) => r.mainText), ["Thomas Berger"]);
});

test("rejectAllShown remembers nothing, so a later run may propose it again", () => {
  // A rejection is "stop taking up review space", not a permanent veto: the
  // alternative would be a hidden deny-list nobody can see or edit.
  threeSuggestions();
  rejectAllShown(["Marie Duval"]);
  const added = addSuggestions([{ discoveryMethods: ["heuristic"], mainText: "Marie Duval", category: "person_names" }]);
  assert.equal(added, 1, "a rejected value must be proposable again");
});

test("the bulk actions repaint once, not once per row", () => {
  threeSuggestions();
  let paints = 0;
  const off = subscribe(() => paints++);
  acceptAllShown(["Marie Duval", "Thomas Berger", "Meridian Consulting"]);
  // addValues and the suggestion removal are two setState calls; three rows
  // must not mean six repaints.
  assert.ok(paints <= 2, `${paints} repaints for a three-row bulk accept`);
  off();
});

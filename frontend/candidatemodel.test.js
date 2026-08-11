// candidatemodel.test.js, tests for the suggestions-table view-model
// (BUILD-04 CR14/CR15).
//
// The table gained a search box, a type selector and a sort toggle. All
// three run through visibleCandidates, and the bulk Accept all / Deny all
// buttons act on exactly what it returns, so a bug here would either hide
// rows a bulk action then swept up, or act on rows the user could not see.
// That makes this the highest-value pure function in the Values step.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  visibleCandidates,
  toggleCountSort, toggleValueSort, DEFAULT_CANDIDATE_FILTER,
} from "./candidatemodel.js";

/** fixture() is the shared candidate list: mixed categories and counts. */
function fixture() {
  return [
    { text: "Marie Duval", category: "person_names", count: 7, source: "smart" },
    { text: "Alpine Trust", category: "entity_names", count: 3, source: "smart" },
    { text: "Anouk Berger", category: "person_names", count: 3, source: "local-ai" },
    { text: "Project Borealis", category: "project_names", count: 1, source: "smart" },
    { text: "Helpdesk", category: "entity_names", count: 12, source: "smart" },
  ];
}

const texts = (rows) => rows.map((r) => r.text);

// --- The neutral filter ---------------------------------------------------

test("the default filter shows everything, most frequent first", () => {
  const rows = visibleCandidates(fixture(), DEFAULT_CANDIDATE_FILTER);
  assert.deepEqual(texts(rows), [
    "Helpdesk", "Marie Duval", "Alpine Trust", "Anouk Berger", "Project Borealis",
  ]);
});

test("an omitted filter behaves as the default", () => {
  assert.deepEqual(texts(visibleCandidates(fixture())), texts(visibleCandidates(fixture(), DEFAULT_CANDIDATE_FILTER)));
});

test("the input list is never mutated", () => {
  const input = fixture();
  const before = texts(input);
  visibleCandidates(input, { ...DEFAULT_CANDIDATE_FILTER, sort: "value-asc" });
  assert.deepEqual(texts(input), before,
    "the store's candidate order must survive a view sort");
});

test("an empty or missing list is handled", () => {
  assert.deepEqual(visibleCandidates([], DEFAULT_CANDIDATE_FILTER), []);
  assert.deepEqual(visibleCandidates(undefined, DEFAULT_CANDIDATE_FILTER), []);
});

// --- CR14: the value search ----------------------------------------------

test("the search matches anywhere in the value, case-insensitively", () => {
  const search = (q) => texts(visibleCandidates(fixture(), { ...DEFAULT_CANDIDATE_FILTER, search: q }));
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
  const rows = visibleCandidates(fixture(), { ...DEFAULT_CANDIDATE_FILTER, search: "  alpine  " });
  assert.deepEqual(texts(rows), ["Alpine Trust"]);
});

test("a search matching nothing returns an empty list, not everything", () => {
  const rows = visibleCandidates(fixture(), { ...DEFAULT_CANDIDATE_FILTER, search: "zzzz" });
  assert.deepEqual(rows, []);
});

// --- CR14: the type selector ---------------------------------------------

test("the category filter keeps only that type", () => {
  const rows = visibleCandidates(fixture(), { ...DEFAULT_CANDIDATE_FILTER, category: "person_names" });
  assert.deepEqual(texts(rows), ["Marie Duval", "Anouk Berger"]);
});

test("an empty category means all types", () => {
  assert.equal(visibleCandidates(fixture(), { ...DEFAULT_CANDIDATE_FILTER, category: "" }).length, 5);
});

test("search and category compose", () => {
  const rows = visibleCandidates(fixture(), {
    ...DEFAULT_CANDIDATE_FILTER, category: "person_names", search: "anouk",
  });
  assert.deepEqual(texts(rows), ["Anouk Berger"]);
});

// --- CR14: the sorts ------------------------------------------------------

test("occurrences sort both ways", () => {
  const desc = visibleCandidates(fixture(), { ...DEFAULT_CANDIDATE_FILTER, sort: "count-desc" });
  assert.deepEqual(desc.map((r) => r.count), [12, 7, 3, 3, 1]);
  const asc = visibleCandidates(fixture(), { ...DEFAULT_CANDIDATE_FILTER, sort: "count-asc" });
  assert.deepEqual(asc.map((r) => r.count), [1, 3, 3, 7, 12]);
});

test("equal counts tie-break by value, so rows never jump between renders", () => {
  const rows = visibleCandidates(fixture(), { ...DEFAULT_CANDIDATE_FILTER, sort: "count-desc" });
  const tied = rows.filter((r) => r.count === 3).map((r) => r.text);
  assert.deepEqual(tied, ["Alpine Trust", "Anouk Berger"]);
  // Same input, same output, every time.
  for (let i = 0; i < 5; i++) {
    assert.deepEqual(texts(visibleCandidates(fixture(), { ...DEFAULT_CANDIDATE_FILTER, sort: "count-desc" })), texts(rows));
  }
});

test("value sort both ways", () => {
  const asc = visibleCandidates(fixture(), { ...DEFAULT_CANDIDATE_FILTER, sort: "value-asc" });
  assert.deepEqual(texts(asc), [
    "Alpine Trust", "Anouk Berger", "Helpdesk", "Marie Duval", "Project Borealis",
  ]);
  const desc = visibleCandidates(fixture(), { ...DEFAULT_CANDIDATE_FILTER, sort: "value-desc" });
  assert.deepEqual(texts(desc), texts(asc).reverse());
});

test("an unknown sort key falls back to the default instead of throwing", () => {
  const rows = visibleCandidates(fixture(), { ...DEFAULT_CANDIDATE_FILTER, sort: "colour-desc" });
  assert.deepEqual(texts(rows), texts(visibleCandidates(fixture(), DEFAULT_CANDIDATE_FILTER)));
});

test("the sort toggles flip and adopt", () => {
  assert.equal(toggleCountSort("count-desc"), "count-asc");
  assert.equal(toggleCountSort("count-asc"), "count-desc");
  assert.equal(toggleCountSort("value-asc"), "count-desc", "adopts descending from another column");
  assert.equal(toggleValueSort("value-asc"), "value-desc");
  assert.equal(toggleValueSort("value-desc"), "value-asc");
  assert.equal(toggleValueSort("count-desc"), "value-asc");
});

// --- CR15: the bulk-action counts ----------------------------------------




// --- The across-category bulk actions (BUILD-05 Phase 6) ------------------
//
// The suggestions table sorts and filters across EVERY category at once, so
// "shown" is not a category: it is whatever survived the search, the type filter
// and the source filter. acceptAllInCategory cannot express that, which is why
// acceptAllShown / rejectAllShown exist beside it rather than replacing it.

import {
  resetState, getState, subscribe, addCandidates, acceptAllShown, rejectAllShown,
} from "./state.js";

/** threeCandidates() seeds a list spanning two categories and two sources. */
function threeCandidates() {
  resetState();
  addCandidates([
    { text: "Marie Duval", category: "person_names", count: 14 },
    { text: "Thomas Berger", category: "person_names", count: 9 },
  ], "smart");
  addCandidates([
    { text: "Meridian Consulting", category: "organisation_names", count: 6 },
  ], "local-ai");
}

test("acceptAllShown promotes rows ACROSS categories in one action", () => {
  threeCandidates();
  const added = acceptAllShown(["Marie Duval", "Meridian Consulting"]);
  assert.equal(added, 2);
  const s = getState();
  // Each keeps its OWN category: the bulk action is about which rows, not about
  // what they are.
  assert.deepEqual(
    s.entities.map((e) => `${e.category}:${e.canonical}`).sort(),
    ["organisation_names:Meridian Consulting", "person_names:Marie Duval"]);
  // The row that was not shown is still waiting.
  assert.deepEqual(s.candidates.map((c) => c.text), ["Thomas Berger"]);
});

test("acceptAllShown ignores values that are not candidates", () => {
  // The view passes the filtered list; a stale entry in it must not invent an
  // entity out of nothing.
  threeCandidates();
  const added = acceptAllShown(["Marie Duval", "Never Proposed"]);
  assert.equal(added, 1);
  assert.equal(getState().entities.length, 1);
});

test("acceptAllShown on an empty selection changes nothing", () => {
  threeCandidates();
  for (const empty of [[], undefined, null]) {
    assert.equal(acceptAllShown(empty), 0, JSON.stringify(empty));
    assert.equal(getState().candidates.length, 3);
    assert.equal(getState().entities.length, 0);
  }
});

test("acceptAllShown matches case-insensitively, like every candidate lookup", () => {
  threeCandidates();
  assert.equal(acceptAllShown(["marie duval"]), 1);
  assert.equal(getState().entities[0].canonical, "Marie Duval",
    "the stored value keeps the spelling that was proposed");
});

test("rejectAllShown drops rows without promoting any", () => {
  threeCandidates();
  const removed = rejectAllShown(["Marie Duval", "Meridian Consulting"]);
  assert.equal(removed, 2);
  const s = getState();
  assert.deepEqual(s.entities, [], "rejecting must add nothing");
  assert.deepEqual(s.candidates.map((c) => c.text), ["Thomas Berger"]);
});

test("rejectAllShown remembers nothing, so a later run may propose it again", () => {
  // A rejection is "stop taking up review space", not a permanent veto: the
  // alternative would be a hidden deny-list nobody can see or edit.
  threeCandidates();
  rejectAllShown(["Marie Duval"]);
  const added = addCandidates([{ text: "Marie Duval", category: "person_names" }], "smart");
  assert.equal(added, 1, "a rejected value must be proposable again");
});

test("the bulk actions repaint once, not once per row", () => {
  threeCandidates();
  let paints = 0;
  const off = subscribe(() => paints++);
  acceptAllShown(["Marie Duval", "Thomas Berger", "Meridian Consulting"]);
  // addEntities and the candidate removal are two setState calls; three rows
  // must not mean six repaints.
  assert.ok(paints <= 2, `${paints} repaints for a three-row bulk accept`);
  off();
});

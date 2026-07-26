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
  visibleCandidates, candidateCategoryCounts,
  toggleCountSort, toggleValueSort, DEFAULT_CANDIDATE_FILTER,
} from "./candidatemodel.js";

/** fixture() is the shared candidate list: mixed categories and counts. */
function fixture() {
  return [
    { text: "Marie Duval", category: "person_names", count: 7, source: "smart" },
    { text: "Alpine Trust", category: "client_names", count: 3, source: "smart" },
    { text: "Anouk Berger", category: "person_names", count: 3, source: "local-ai" },
    { text: "Project Borealis", category: "project_names", count: 1, source: "smart" },
    { text: "Helpdesk", category: "internal_names", count: 12, source: "smart" },
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

test("category counts describe exactly the rows given", () => {
  assert.deepEqual(candidateCategoryCounts(fixture()), {
    person_names: 2, client_names: 1, project_names: 1, internal_names: 1,
  });
});

test("counts follow the filter, which is what the bulk buttons promise", () => {
  // The button says "Accept all 1 person" when a search leaves one row,
  // and the reducer is handed exactly that row (CR15).
  const rows = visibleCandidates(fixture(), { ...DEFAULT_CANDIDATE_FILTER, search: "marie" });
  assert.deepEqual(candidateCategoryCounts(rows), { person_names: 1 });
});

test("counts of an empty list are an empty object", () => {
  assert.deepEqual(candidateCategoryCounts([]), {});
  assert.deepEqual(candidateCategoryCounts(undefined), {});
});

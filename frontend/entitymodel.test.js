// entitymodel.test.js, the variants regression suite (BUILD-02 Phase 7b).
// These tests pin the data-flow contract the Phase 9 entities rework must
// preserve: pending vs empty vs error states, single-entity re-expansion,
// and independence between expanded rows.

import { test } from "node:test";
import assert from "node:assert/strict";

import { variantRows, pendingExpansions } from "./entitymodel.js";
import {
  resetState, getState, entityKey,
  addEntities, editEntity, addManualVariant, setEntityVariants,
  setEntityVariantError,
} from "./state.js";

function keyOf(e) {
  return entityKey(e.category, e.canonical);
}

test("new entities are pending until expanded; toggling shows the right state", () => {
  resetState();
  addEntities([{ category: "person_names", canonical: "Marie Duval" }]);
  const e = getState().entities[0];
  assert.equal(e.variants, null, "fresh entity must be pending, not empty");

  const expanded = new Set([keyOf(e)]);
  let rows = variantRows(getState().entities, expanded);
  assert.equal(rows.length, 1);
  assert.equal(rows[0].state, "pending");

  // Expansion answers: list state.
  setEntityVariants("person_names", "Marie Duval", ["Marie Duval", "M. Duval"]);
  rows = variantRows(getState().entities, expanded);
  assert.equal(rows[0].state, "list");
  assert.deepEqual(rows[0].variants, ["Marie Duval", "M. Duval"]);

  // Collapse: no rows rendered; re-open: same settled state, NO re-pend.
  rows = variantRows(getState().entities, new Set());
  assert.equal(rows.length, 0);
  rows = variantRows(getState().entities, expanded);
  assert.equal(rows[0].state, "list");
});

test("zero variants is an explicit empty state, never a stuck pending placeholder", () => {
  resetState();
  addEntities([{ category: "client_names", canonical: "X1" }]);
  setEntityVariants("client_names", "X1", []);
  const rows = variantRows(getState().entities, new Set([entityKey("client_names", "X1")]));
  assert.equal(rows[0].state, "empty");
  // And it is NOT queued for re-expansion on the next render cycle.
  assert.equal(pendingExpansions(getState().entities).length, 0);
});

test("add variant re-pends ONLY the amended entity", () => {
  resetState();
  addEntities([
    { category: "person_names", canonical: "Marie Duval" },
    { category: "person_names", canonical: "Paul Stone" },
  ]);
  setEntityVariants("person_names", "Marie Duval", ["Marie Duval"]);
  setEntityVariants("person_names", "Paul Stone", ["Paul Stone"]);
  assert.equal(pendingExpansions(getState().entities).length, 0);

  addManualVariant("person_names", "Marie Duval", "Mimi");
  const pending = pendingExpansions(getState().entities);
  assert.equal(pending.length, 1, "exactly one entity re-expands");
  assert.equal(pending[0].canonical, "Marie Duval");
  // The other row keeps its settled variants untouched.
  const stone = getState().entities.find((e) => e.canonical === "Paul Stone");
  assert.deepEqual(stone.variants, ["Paul Stone"]);
});

test("editing a canonical resets only its own variants", () => {
  resetState();
  addEntities([
    { category: "person_names", canonical: "Marie Duval" },
    { category: "person_names", canonical: "Paul Stone" },
  ]);
  setEntityVariants("person_names", "Marie Duval", ["Marie Duval"]);
  setEntityVariants("person_names", "Paul Stone", ["Paul Stone"]);

  assert.equal(editEntity("person_names", "Marie Duval", "Maria Duval"), true);
  const pending = pendingExpansions(getState().entities);
  assert.equal(pending.length, 1);
  assert.equal(pending[0].canonical, "Maria Duval");
  const stone = getState().entities.find((e) => e.canonical === "Paul Stone");
  assert.deepEqual(stone.variants, ["Paul Stone"]);
});

test("two entities expanded simultaneously keep independent state", () => {
  resetState();
  addEntities([
    { category: "person_names", canonical: "A One" },
    { category: "person_names", canonical: "B Two" },
  ]);
  setEntityVariants("person_names", "A One", ["A One", "One"]);
  // B Two stays pending.
  const expanded = new Set([
    entityKey("person_names", "A One"),
    entityKey("person_names", "B Two"),
  ]);
  const rows = variantRows(getState().entities, expanded);
  assert.equal(rows.length, 2);
  assert.equal(rows[0].state, "list");
  assert.equal(rows[1].state, "pending");
});

test("a failed expansion surfaces the error and stops re-pending", () => {
  resetState();
  addEntities([{ category: "person_names", canonical: "Err Case" }]);
  setEntityVariantError("person_names", "Err Case", "the bridge exploded");
  const rows = variantRows(getState().entities, new Set([entityKey("person_names", "Err Case")]));
  assert.equal(rows[0].state, "error");
  assert.equal(rows[0].error, "the bridge exploded");
  // Errored rows are not silently retried forever.
  assert.equal(pendingExpansions(getState().entities).length, 0);
});

test("expand/collapse cycles never mutate entity data", () => {
  resetState();
  addEntities([{ category: "person_names", canonical: "Cycle Test" }]);
  setEntityVariants("person_names", "Cycle Test", ["Cycle Test"]);
  const before = JSON.stringify(getState().entities);
  const expanded = new Set();
  const key = entityKey("person_names", "Cycle Test");
  for (let i = 0; i < 5; i++) {
    expanded.add(key);
    variantRows(getState().entities, expanded);
    expanded.delete(key);
    variantRows(getState().entities, expanded);
  }
  assert.equal(JSON.stringify(getState().entities), before);
});

// --- BUILD-04 CR17: the variant drill-down regressions ---------------------
//
// Each case below was a REPORTED symptom, reproduced here before the fix
// so it cannot come back quietly. They run against the pure view-model and
// the reducers, which is where the state actually lives; the wiring that
// consumes them is in views/values.js.

import { moveVariant } from "./state.js";

/** expandAll(entities) is the expanded-key set for every row. */
function expandAll(entities) {
  return new Set(entities.map((e) => entityKey(e.category, e.canonical)));
}

test("CR17: adding a variant to a SECOND value works like the first", () => {
  resetState();
  addEntities([
    { category: "person_names", canonical: "Marie Duval", variants: ["Marie Duval"] },
    { category: "person_names", canonical: "Anouk Berger", variants: ["Anouk Berger"] },
  ]);

  // First value: add, expand, settle.
  addManualVariant("person_names", "Marie Duval", "Mimi");
  let pending = pendingExpansions(getState().entities);
  assert.deepEqual(pending.map((e) => e.canonical), ["Marie Duval"],
    "only the touched row may re-expand");
  setEntityVariants("person_names", "Marie Duval", ["Marie Duval", "Duval", "Mimi"]);

  // Second value: the exact same sequence must behave identically. The
  // reported symptom was that this one silently did nothing.
  addManualVariant("person_names", "Anouk Berger", "Nouk");
  pending = pendingExpansions(getState().entities);
  assert.deepEqual(pending.map((e) => e.canonical), ["Anouk Berger"],
    "the second value must become pending too, and alone");
  setEntityVariants("person_names", "Anouk Berger", ["Anouk Berger", "Berger", "Nouk"]);

  const rows = variantRows(getState().entities, expandAll(getState().entities));
  assert.equal(rows.length, 2);
  for (const row of rows) {
    assert.equal(row.state, "list", `${row.canonical} must show its variants`);
  }
  assert.ok(rows[0].variants.includes("Mimi"));
  assert.ok(rows[1].variants.includes("Nouk"));
});

test("CR17: an expanded row stays expanded across an add and its expansion", () => {
  resetState();
  addEntities([{ category: "client_names", canonical: "Alpine Trust", variants: ["Alpine Trust"] }]);
  const key = entityKey("client_names", "Alpine Trust");
  const expanded = new Set([key]);

  // Open, then add: the row must be PENDING and still open, never gone.
  addManualVariant("client_names", "Alpine Trust", "Alpine");
  let rows = variantRows(getState().entities, expanded);
  assert.equal(rows.length, 1, "the row must not disappear while expanding");
  assert.equal(rows[0].state, "pending");

  setEntityVariants("client_names", "Alpine Trust", ["Alpine Trust", "Alpine"]);
  rows = variantRows(getState().entities, expanded);
  assert.equal(rows.length, 1, "the row must still be open once expanded");
  assert.equal(rows[0].state, "list");
  assert.deepEqual(rows[0].variants, ["Alpine Trust", "Alpine"]);
});

test("CR17: a failed expansion surfaces as an error, never a stuck spinner", () => {
  resetState();
  addEntities([{ category: "client_names", canonical: "Alpine Trust" }]);
  const expanded = expandAll(getState().entities);

  assert.equal(variantRows(getState().entities, expanded)[0].state, "pending");
  setEntityVariantError("client_names", "Alpine Trust", "the bridge went away");

  const row = variantRows(getState().entities, expanded)[0];
  assert.equal(row.state, "error");
  assert.match(row.error, /bridge went away/);
  // And it must no longer be retried on every repaint, which is what
  // turned a single failure into an endless loop of bridge calls.
  assert.deepEqual(pendingExpansions(getState().entities), []);
});

test("CR17: moving a variant between two freshly added values works", () => {
  resetState();
  addEntities([
    { category: "person_names", canonical: "Marie Duval" },
    { category: "client_names", canonical: "Alpine Trust" },
  ]);
  setEntityVariants("person_names", "Marie Duval", ["Marie Duval", "Duval", "Marie"]);
  setEntityVariants("client_names", "Alpine Trust", ["Alpine Trust"]);

  assert.equal(
    moveVariant("person_names", "Marie Duval", "client_names", "Alpine Trust", "Duval"),
    true);

  const [from, to] = getState().entities;
  assert.ok(from.excludedVariants.includes("Duval"), "the source must stop matching it");
  assert.ok(to.manualVariants.includes("Duval"), "the target must gain it");
  // BOTH rows re-expand, and nothing else does.
  assert.deepEqual(
    pendingExpansions(getState().entities).map((e) => e.canonical),
    ["Marie Duval", "Alpine Trust"]);
});

test("CR17: renaming a value moves its expanded key with it", () => {
  // The expanded set is keyed by (category, canonical), so a rename used
  // to orphan the key and silently collapse the row the user was working
  // in. The view migrates the key; this pins the contract it relies on.
  resetState();
  addEntities([{ category: "client_names", canonical: "Alpine", variants: ["Alpine"] }]);
  const oldKey = entityKey("client_names", "Alpine");

  assert.equal(editEntity("client_names", "Alpine", "Alpine Trust"), true);
  const newKey = entityKey("client_names", "Alpine Trust");
  assert.notEqual(oldKey, newKey, "the key must change, which is why it needs migrating");

  // Migrated by hand, as the view does it.
  const expanded = new Set([oldKey]);
  expanded.delete(oldKey);
  expanded.add(newKey);

  const rows = variantRows(getState().entities, expanded);
  assert.equal(rows.length, 1, "the renamed row must still be open");
  assert.equal(rows[0].state, "pending", "and re-expanding under its new name");
});

test("CR17: a settled row is never re-expanded, an empty result included", () => {
  resetState();
  addEntities([{ category: "client_names", canonical: "ACME" }]);
  setEntityVariants("client_names", "ACME", []); // expanded, none found
  assert.deepEqual(pendingExpansions(getState().entities), [],
    "an explicit empty result is settled, not pending");
  const row = variantRows(getState().entities, expandAll(getState().entities))[0];
  assert.equal(row.state, "empty");
});

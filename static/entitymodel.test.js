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

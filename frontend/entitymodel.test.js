// entitymodel.test.js, the variants regression suite (BUILD-02 Phase 7b,
// extended by BUILD-04 CR17, trimmed for BUILD-05 Phase 9).
//
// These pin the DATA-FLOW contract, which the BUILD-05 relayout deliberately did
// not change: pending vs empty vs error are three distinct states, only the rows
// a user just touched re-expand, and two values never interfere with each other.
//
// What went with Phase 9 is variantRows() and everything about EXPANDED rows. The
// value cards that replaced the review table always show their variants, so there
// is no expanded set left to model. The states it encoded are still tested here,
// read straight off the entity, which is exactly what the cards do.

import { test } from "node:test";
import assert from "node:assert/strict";

import { pendingExpansions } from "./entitymodel.js";
import {
  resetState, getState,
  addEntities, addManualVariant, setEntityVariants, setEntityVariantError,
  removeEntity, moveVariant,
} from "./state.js";

/** entityFor(canonical) finds one value in the store. */
function entityFor(canonical) {
  return getState().entities.find((e) => e.canonical === canonical);
}

/**
 * variantState(e) is the state a value card renders, read straight off the
 * entity. It is the classification variantRows() used to return, spelled out
 * here because it is the contract these tests exist to pin.
 */
function variantState(e) {
  if (e.variantError) return "error";
  if (e.variants === null || e.variants === undefined) return "pending";
  if (e.variants.length === 0) return "empty";
  return "list";
}

test("a new value is PENDING, not empty", () => {
  // The distinction is the heart of BUILD-02 Phase 7a: "not expanded yet" and
  // "expanded and found nothing" look identical if both are [].
  resetState();
  addEntities([{ category: "person_names", canonical: "Marie Duval" }]);
  const e = entityFor("Marie Duval");
  assert.equal(e.variants, null, "a fresh value must be pending, not empty");
  assert.equal(variantState(e), "pending");
  assert.deepEqual(pendingExpansions(getState().entities).map((x) => x.canonical),
    ["Marie Duval"]);
});

test("zero variants is an explicit EMPTY state, never a stuck pending placeholder", () => {
  resetState();
  addEntities([{ category: "entity_names", canonical: "Acme" }]);
  setEntityVariants("entity_names", "Acme", []);
  const e = entityFor("Acme");
  assert.deepEqual(e.variants, []);
  assert.equal(variantState(e), "empty");
  assert.deepEqual(pendingExpansions(getState().entities), [],
    "an empty result is SETTLED: it must never be expanded again");
});

test("a list of variants settles the row", () => {
  resetState();
  addEntities([{ category: "person_names", canonical: "Marie Duval" }]);
  setEntityVariants("person_names", "Marie Duval", ["M. Duval", "Duval"]);
  assert.equal(variantState(entityFor("Marie Duval")), "list");
  assert.deepEqual(pendingExpansions(getState().entities), []);
});

test("adding a variant re-pends ONLY the amended value", () => {
  resetState();
  addEntities([
    { category: "person_names", canonical: "Marie Duval" },
    { category: "person_names", canonical: "Thomas Berger" },
  ]);
  setEntityVariants("person_names", "Marie Duval", ["Duval"]);
  setEntityVariants("person_names", "Thomas Berger", ["Berger"]);
  assert.deepEqual(pendingExpansions(getState().entities), []);

  addManualVariant("person_names", "Marie Duval", "Mimi");
  assert.deepEqual(pendingExpansions(getState().entities).map((e) => e.canonical),
    ["Marie Duval"], "the untouched value must NOT be re-expanded");
  assert.equal(variantState(entityFor("Thomas Berger")), "list");
});

test("two values keep independent state", () => {
  resetState();
  addEntities([
    { category: "entity_names", canonical: "Alpha" },
    { category: "entity_names", canonical: "Beta" },
  ]);
  setEntityVariants("entity_names", "Alpha", ["A"]);
  setEntityVariantError("entity_names", "Beta", "expansion failed");

  assert.equal(variantState(entityFor("Alpha")), "list");
  assert.equal(variantState(entityFor("Beta")), "error");
});

test("a failed expansion surfaces the error and stops re-pending", () => {
  // A row that keeps re-pending spins forever and retries on every repaint.
  resetState();
  addEntities([{ category: "person_names", canonical: "Marie Duval" }]);
  setEntityVariantError("person_names", "Marie Duval", "Go said no");
  const e = entityFor("Marie Duval");
  assert.equal(variantState(e), "error");
  assert.equal(e.variantError, "Go said no");
  assert.deepEqual(pendingExpansions(getState().entities), [],
    "an errored row is SETTLED: it must not be retried on every repaint");
});

test("setEntityVariants clears a previous error", () => {
  // A retry that succeeds has to clear the message, or the card shows an error
  // beside the variants it just found.
  resetState();
  addEntities([{ category: "person_names", canonical: "Marie Duval" }]);
  setEntityVariantError("person_names", "Marie Duval", "Go said no");
  setEntityVariants("person_names", "Marie Duval", ["Duval"]);
  const e = entityFor("Marie Duval");
  assert.equal(e.variantError, null);
  assert.equal(variantState(e), "list");
});

test("CR17: adding a variant to a SECOND value works like the first", () => {
  // The reported bug: the second value's add did nothing, because the wiring
  // addressed rows by position rather than by key.
  resetState();
  addEntities([
    { category: "entity_names", canonical: "First Client" },
    { category: "entity_names", canonical: "Second Client" },
  ]);
  setEntityVariants("entity_names", "First Client", []);
  setEntityVariants("entity_names", "Second Client", []);

  addManualVariant("entity_names", "Second Client", "SecondCo");
  const second = entityFor("Second Client");
  assert.deepEqual(second.manualVariants, ["SecondCo"]);
  assert.equal(variantState(second), "pending", "the amended row re-expands");
  assert.deepEqual(entityFor("First Client").manualVariants, [],
    "the first value must be untouched");
});

test("CR17: a duplicate variant changes nothing, case-insensitively", () => {
  resetState();
  addEntities([{ category: "entity_names", canonical: "Acme" }]);
  addManualVariant("entity_names", "Acme", "ACME Ltd");
  setEntityVariants("entity_names", "Acme", []);
  addManualVariant("entity_names", "Acme", "acme ltd");
  const e = entityFor("Acme");
  assert.deepEqual(e.manualVariants, ["ACME Ltd"], "one spelling, kept as typed");
  assert.equal(variantState(e), "empty", "an add that changed nothing must not re-pend");
});

test("CR17: moving a variant between two values re-pends BOTH", () => {
  resetState();
  addEntities([
    { category: "entity_names", canonical: "Alpha" },
    { category: "entity_names", canonical: "Beta" },
  ]);
  setEntityVariants("entity_names", "Alpha", ["Alph"]);
  setEntityVariants("entity_names", "Beta", []);

  assert.equal(moveVariant("entity_names", "Alpha", "entity_names", "Beta", "Alph"), true);
  const alpha = entityFor("Alpha");
  const beta = entityFor("Beta");
  // The source EXCLUDES it, so automatic expansion stops matching it; the target
  // gains it as a manual variant. Both re-expand.
  assert.deepEqual(alpha.excludedVariants, ["Alph"]);
  assert.deepEqual(beta.manualVariants, ["Alph"]);
  assert.equal(variantState(alpha), "pending");
  assert.equal(variantState(beta), "pending");
  assert.equal(pendingExpansions(getState().entities).length, 2);
});

test("CR17: a settled row is never re-expanded, an empty result included", () => {
  resetState();
  addEntities([{ category: "entity_names", canonical: "Acme" }]);
  setEntityVariants("entity_names", "Acme", []);
  for (let i = 0; i < 5; i++) {
    assert.deepEqual(pendingExpansions(getState().entities), [],
      "repeated renders must not queue the same expansion again");
  }
});

test("removing a value takes its variant state with it", () => {
  resetState();
  addEntities([{ category: "entity_names", canonical: "Acme" }]);
  removeEntity("entity_names", "Acme");
  assert.deepEqual(getState().entities, []);
  assert.deepEqual(pendingExpansions(getState().entities), []);
});

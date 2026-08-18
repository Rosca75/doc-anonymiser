// valuemodel.test.js, the spelling-expansion data-flow suite.
//
// Three states are DISTINCT and must stay distinct: derivedSpellings null means an
// expansion is in flight, [] means it finished and found none, and an error means
// it failed. Collapsing any two of them shows a forever-pending placeholder for a
// list that is already final, or the reverse.
//
// The other two contracts here: only the rows a user just touched re-expand, so a
// settled Value is never asked again; and two Values never interfere with each
// other's expansion state. A Value card reads these states straight off the Value,
// which is what these tests read too.

import { test } from "node:test";
import assert from "node:assert/strict";

import { pendingExpansions } from "./valuemodel.js";
import {
  resetState, getState,
  addValues, addSpelling, setValueSpellings, setValueSpellingError,
  deleteValue, moveSpelling,
} from "./state.js";

/** valueFor(mainText) finds one value in the store. */
function valueFor(mainText) {
  return getState().values.find((e) => e.mainText === mainText);
}

/**
 * spellingState(v) is the state a Value card renders, read straight off the
 * Value. It is spelled out here because it is the contract these tests pin.
 */
function spellingState(e) {
  if (e.spellingsError) return "error";
  if (e.derivedSpellings === null || e.derivedSpellings === undefined) return "pending";
  if (e.derivedSpellings.length === 0) return "empty";
  return "list";
}

test("a new value is PENDING, not empty", () => {
  // The distinction is the heart of: "not expanded yet" and
  // "expanded and found nothing" look identical if both are [].
  resetState();
  addValues([{ category: "person_names", mainText: "Marie Duval" }]);
  const e = valueFor("Marie Duval");
  assert.equal(e.derivedSpellings, null, "a fresh value must be pending, not empty");
  assert.equal(spellingState(e), "pending");
  assert.deepEqual(pendingExpansions(getState().values).map((x) => x.mainText),
    ["Marie Duval"]);
});

test("zero derivedSpellings is an explicit EMPTY state, never a stuck pending placeholder", () => {
  resetState();
  addValues([{ category: "entity_names", mainText: "Acme" }]);
  setValueSpellings("entity_names", "Acme", []);
  const e = valueFor("Acme");
  assert.deepEqual(e.derivedSpellings, []);
  assert.equal(spellingState(e), "empty");
  assert.deepEqual(pendingExpansions(getState().values), [],
    "an empty result is SETTLED: it must never be expanded again");
});

test("a list of derivedSpellings settles the row", () => {
  resetState();
  addValues([{ category: "person_names", mainText: "Marie Duval" }]);
  setValueSpellings("person_names", "Marie Duval", ["M. Duval", "Duval"]);
  assert.equal(spellingState(valueFor("Marie Duval")), "list");
  assert.deepEqual(pendingExpansions(getState().values), []);
});

test("adding a spelling re-pends ONLY the amended value", () => {
  resetState();
  addValues([
    { category: "person_names", mainText: "Marie Duval" },
    { category: "person_names", mainText: "Thomas Berger" },
  ]);
  setValueSpellings("person_names", "Marie Duval", ["Duval"]);
  setValueSpellings("person_names", "Thomas Berger", ["Berger"]);
  assert.deepEqual(pendingExpansions(getState().values), []);

  addSpelling("person_names", "Marie Duval", "Mimi");
  assert.deepEqual(pendingExpansions(getState().values).map((e) => e.mainText),
    ["Marie Duval"], "the untouched value must NOT be re-expanded");
  assert.equal(spellingState(valueFor("Thomas Berger")), "list");
});

test("two values keep independent state", () => {
  resetState();
  addValues([
    { category: "entity_names", mainText: "Alpha" },
    { category: "entity_names", mainText: "Beta" },
  ]);
  setValueSpellings("entity_names", "Alpha", ["A"]);
  setValueSpellingError("entity_names", "Beta", "expansion failed");

  assert.equal(spellingState(valueFor("Alpha")), "list");
  assert.equal(spellingState(valueFor("Beta")), "error");
});

test("a failed expansion surfaces the error and stops re-pending", () => {
  // A row that keeps re-pending spins forever and retries on every repaint.
  resetState();
  addValues([{ category: "person_names", mainText: "Marie Duval" }]);
  setValueSpellingError("person_names", "Marie Duval", "Go said no");
  const e = valueFor("Marie Duval");
  assert.equal(spellingState(e), "error");
  assert.equal(e.spellingsError, "Go said no");
  assert.deepEqual(pendingExpansions(getState().values), [],
    "an errored row is SETTLED: it must not be retried on every repaint");
});

test("setValueSpellings clears a previous error", () => {
  // A retry that succeeds has to clear the message, or the card shows an error
  // beside the derivedSpellings it just found.
  resetState();
  addValues([{ category: "person_names", mainText: "Marie Duval" }]);
  setValueSpellingError("person_names", "Marie Duval", "Go said no");
  setValueSpellings("person_names", "Marie Duval", ["Duval"]);
  const e = valueFor("Marie Duval");
  assert.equal(e.spellingsError, null);
  assert.equal(spellingState(e), "list");
});

test("CR17: adding a spelling to a SECOND value works like the first", () => {
  // The reported bug: the second value's add did nothing, because the wiring
  // addressed rows by position rather than by key.
  resetState();
  addValues([
    { category: "entity_names", mainText: "First Client" },
    { category: "entity_names", mainText: "Second Client" },
  ]);
  setValueSpellings("entity_names", "First Client", []);
  setValueSpellings("entity_names", "Second Client", []);

  addSpelling("entity_names", "Second Client", "SecondCo");
  const second = valueFor("Second Client");
  assert.deepEqual(second.spellings, ["SecondCo"]);
  assert.equal(spellingState(second), "pending", "the amended row re-expands");
  assert.deepEqual(valueFor("First Client").spellings, [],
    "the first value must be untouched");
});

test("CR17: a duplicate spelling changes nothing, case-insensitively", () => {
  resetState();
  addValues([{ category: "entity_names", mainText: "Acme" }]);
  addSpelling("entity_names", "Acme", "ACME Ltd");
  setValueSpellings("entity_names", "Acme", []);
  addSpelling("entity_names", "Acme", "acme ltd");
  const e = valueFor("Acme");
  assert.deepEqual(e.spellings, ["ACME Ltd"], "one spelling, kept as typed");
  assert.equal(spellingState(e), "empty", "an add that changed nothing must not re-pend");
});

test("moving a spelling curates the source and re-pends the target", () => {
  resetState();
  addValues([
    { category: "entity_names", mainText: "Alpha" },
    { category: "entity_names", mainText: "Beta" },
  ]);
  setValueSpellings("entity_names", "Alpha", ["Alph"]);
  setValueSpellings("entity_names", "Beta", []);

  assert.equal(moveSpelling("entity_names", "Alpha", "entity_names", "Beta", "Alph"), true);
  const alpha = valueFor("Alpha");
  const beta = valueFor("Beta");
  // The source CURATES without it, so its automatic expansion cannot derive it
  // again and the two values stop both claiming the spelling. The target gains
  // it as a manual spelling and re-expands around it.
  assert.equal(alpha.spellingPolicy, "curated");
  assert.deepEqual(alpha.spellings, ["Alpha"],
    "the moved spelling is gone; the value's own name stays a spelling");
  assert.deepEqual(beta.spellings, ["Alph"]);
  // A curated row is settled: nothing is left for Go to derive, so only the
  // target is asked to expand.
  assert.equal(spellingState(alpha), "empty");
  assert.equal(spellingState(beta), "pending");
  assert.deepEqual(pendingExpansions(getState().values).map((e) => e.mainText), ["Beta"]);
});

test("CR17: a settled row is never re-expanded, an empty result included", () => {
  resetState();
  addValues([{ category: "entity_names", mainText: "Acme" }]);
  setValueSpellings("entity_names", "Acme", []);
  for (let i = 0; i < 5; i++) {
    assert.deepEqual(pendingExpansions(getState().values), [],
      "repeated renders must not queue the same expansion again");
  }
});

test("removing a value takes its spelling state with it", () => {
  resetState();
  addValues([{ category: "entity_names", mainText: "Acme" }]);
  deleteValue("entity_names", "Acme");
  assert.deepEqual(getState().values, []);
  assert.deepEqual(pendingExpansions(getState().values), []);
});

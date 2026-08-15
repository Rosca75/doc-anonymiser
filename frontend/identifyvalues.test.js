// identifyvalues.test.js, render tests for the My values tab and the
// suggestion-row retype dropdown.
//
// These assert what a pane SHOWS, not that a string appears somewhere: a value
// card carries an editable name and a type dropdown; a value that would refuse
// the run is tinted, on the exact name or spelling at fault; the filters narrow
// the list; and a suggestion can be retyped before it is accepted.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  resetState, getState, setState, toggleCategory,
  addEntities, setEntityVariants, addAllowTerm, addCandidates, entityKey,
} from "./state.js";
import { valuesTab, suggestionsTab, visibleValues } from "./views/identifyworkspace.js";
import { all, one, exists, textOf } from "./testhtml.js";

/** seed(category, canonical, variants) adds one accepted value with a settled
 *  variant list, the shape the tab renders. */
function seed(category, canonical, variants = [canonical]) {
  addEntities([{ category, canonical }]);
  setEntityVariants(category, canonical, variants);
}

test("a value card shows the name and a type dropdown set to its category", () => {
  resetState();
  seed("person_names", "Marie Duval", ["Marie Duval", "Marie"]);
  const html = valuesTab(getState());

  assert.equal(one(html, "button.value-name").inner, "Marie Duval");
  const type = one(html, "select.value-type");
  assert.match(type.inner, /value="person_names" selected/);
});

test("every value card offers Group with and Remove", () => {
  resetState();
  seed("entity_names", "Acme");
  const html = valuesTab(getState());
  assert.ok(exists(html, "button.value-group"), "Group with is offered");
  assert.ok(exists(html, "button.value-remove"), "Remove is offered");
});

test("Solve conflicts appears only on a conflicting card", () => {
  resetState();
  seed("entity_names", "Acme", ["Acme"]);
  assert.ok(!exists(valuesTab(getState()), "button.value-solve"),
    "a clean value has nothing to solve");

  // The same name under a second type is a blocking ambiguity.
  seed("person_names", "Acme", ["Acme"]);
  assert.ok(exists(valuesTab(getState()), "button.value-solve"),
    "a conflicting value offers Solve conflicts");
});

test("a conflicting value tints the card and the name", () => {
  resetState();
  seed("entity_names", "Acme", ["Acme"]);
  seed("person_names", "Acme", ["Acme"]);
  const html = valuesTab(getState());
  const cards = all(html, ".value-card");
  assert.ok(cards.every((c) => c.attrs.class.includes("conflicted")),
    "both cards holding the ambiguous name are tinted");
  assert.ok(all(html, "button.value-name").every((n) => n.attrs.class.includes("bad")),
    "the name at fault is marked");
});

test("a shared spelling tints the chip, not the whole name", () => {
  resetState();
  seed("person_names", "Marie Duval", ["Marie Duval", "Marie"]);
  seed("person_names", "Marie Dupont", ["Marie Dupont", "Marie"]);
  const html = valuesTab(getState());
  // The "Marie" chip is the one at fault on each card.
  const badChips = all(html, "span.variant-chip")
    .filter((c) => c.attrs.class.includes("bad"));
  assert.ok(badChips.length >= 2, "the shared spelling is flagged on both cards");
  assert.ok(badChips.every((c) => c.attrs["data-variant"] === "Marie"));
  // The distinct names are not themselves in conflict.
  assert.ok(all(html, "button.value-name").every((n) => !n.attrs.class.includes("bad")));
});

test("Clear all is disabled only when the list is empty", () => {
  resetState();
  assert.ok("disabled" in one(valuesTab(getState()), "button#btn-clear-values").attrs,
    "nothing to clear, so the button is disabled");
  seed("entity_names", "Acme");
  assert.ok(!("disabled" in one(valuesTab(getState()), "button#btn-clear-values").attrs),
    "with values present, Clear all is live");
});

test("visibleValues matches a value by its name or any of its spellings", () => {
  resetState();
  seed("entity_names", "Meridian Consulting", ["Meridian Consulting", "Meridian", "Merid"]);
  seed("person_names", "Marie Duval", ["Marie Duval"]);
  const es = getState().entities;
  assert.equal(visibleValues(es, { search: "meridian" }).length, 1);
  // A spelling the name does not contain still finds the card.
  assert.equal(visibleValues(es, { search: "merid" }).length, 1);
  assert.equal(visibleValues(es, { search: "duval" }).length, 1);
  assert.equal(visibleValues(es, { search: "zzz" }).length, 0);
});

test("visibleValues narrows to one type", () => {
  resetState();
  seed("entity_names", "Acme");
  seed("person_names", "Marie Duval");
  const es = getState().entities;
  assert.equal(visibleValues(es, { type: "person_names" }).length, 1);
  assert.equal(visibleValues(es, { type: "person_names" })[0].canonical, "Marie Duval");
});

test("spellings show by default, and the toggle offers to hide them", () => {
  resetState();
  seed("person_names", "Marie Duval", ["Marie Duval", "Marie"]);
  const html = valuesTab(getState());
  assert.ok(exists(html, "span.variant-chip"), "spellings are shown by default");
  // The toggle is a live control that names the action it would take next.
  assert.match(textOf(html, "button#btn-toggle-variants"), /Hide spellings/);
});

test("a suggestion row carries a type dropdown set to its guessed category", () => {
  resetState();
  addCandidates([{ text: "Meridian", category: "person_names", count: 2 }], "smart");
  const shown = getState().candidates;
  const html = suggestionsTab(getState(), shown);
  const type = one(html, "select.cand-type");
  assert.match(type.inner, /value="person_names" selected/);
  assert.equal(type.attrs["data-text"], "Meridian");
});

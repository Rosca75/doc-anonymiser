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
  addValues, setValueSpellings, addAllowTerm, addSuggestions, valueKey,
  groupValues, curate, acceptSuggestion, setIntersections, canGoTo,
} from "./state.js";
import { valuesTab, suggestionsTab, visibleValues } from "./views/identifyworkspace.js";
import { all, one, exists, textOf } from "./testhtml.js";
import { WORKSPACE } from "./copy.js";

/** seed(category, mainText, derivedSpellings) adds one accepted value with a settled
 *  spelling list, the shape the tab renders. */
function seed(category, mainText, derivedSpellings = [mainText]) {
  addValues([{ category, mainText }]);
  setValueSpellings(category, mainText, derivedSpellings);
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
  const badChips = all(html, "span.spelling-chip")
    .filter((c) => c.attrs.class.includes("bad"));
  assert.ok(badChips.length >= 2, "the shared spelling is flagged on both cards");
  assert.ok(badChips.every((c) => c.attrs["data-spelling"] === "Marie"));
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
  const es = getState().values;
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
  const es = getState().values;
  assert.equal(visibleValues(es, { type: "person_names" }).length, 1);
  assert.equal(visibleValues(es, { type: "person_names" })[0].mainText, "Marie Duval");
});

test("spellings show by default, and the toggle offers to hide them", () => {
  resetState();
  seed("person_names", "Marie Duval", ["Marie Duval", "Marie"]);
  const html = valuesTab(getState());
  assert.ok(exists(html, "span.spelling-chip"), "spellings are shown by default");
  // The toggle is a live control that names the action it would take next.
  assert.match(textOf(html, "button#btn-toggle-derivedSpellings"), /Hide spellings/);
});

test("a suggestion row carries a type dropdown set to its guessed category", () => {
  resetState();
  addSuggestions([{ discoveryMethods: ["heuristic"], mainText: "Meridian", category: "person_names", count: 2 }]);
  const shown = getState().suggestions;
  const html = suggestionsTab(getState(), shown);
  const type = one(html, "select.sugg-type");
  assert.match(type.inner, /value="person_names" selected/);
  assert.equal(type.attrs["data-text"], "Meridian");
});

// CR1: Group with asks which participating value survives. The picker feeds the
// CHOSEN value to groupValues as the target and the rest as sources, so a user
// can fold the card's value INTO a source rather than the other way round. This
// pins the reducer path the wiring takes when a source is chosen as the main.
test("grouping three values and choosing a source as the main keeps that source as survivor", () => {
  resetState();
  seed("entity_names", "Acme");                                   // the card the picker opened from
  seed("person_names", "Marie Duval", ["Marie Duval", "Marie"]);  // a source, chosen as the main
  seed("entity_names", "Acme Corp");                              // another source

  const main = { category: "person_names", mainText: "Marie Duval" };
  const rest = [
    { category: "entity_names", mainText: "Acme" },
    { category: "entity_names", mainText: "Acme Corp" },
  ];
  assert.equal(groupValues(main, rest), 2, "both other values were folded");

  const es = getState().values;
  assert.equal(es.length, 1, "only the chosen survivor remains");
  assert.equal(es[0].mainText, "Marie Duval");
  assert.equal(es[0].category, "person_names");
  const folded = es[0].spellings ?? [];
  assert.ok(folded.includes("Acme"), "the card value folded in as a spelling");
  assert.ok(folded.includes("Acme Corp"), "the other source folded in as a spelling");
});

test("a curated value shows its chips and no pending placeholder", () => {
  // A curated row's list is settled by definition: showing "working out the
  // other spellings..." on it would promise a round-trip that never comes,
  // because nothing is left for Go to derive.
  resetState();
  seed("entity_names", "Delta Industries", ["Delta Industries", "Delta"]);
  const before = getState().values[0];
  setState({ values: [curate(before, ["Delta Industries"])] });

  const html = valuesTab(getState());
  assert.deepEqual(all(html, "span.spelling-chip").map((c) => c.attrs["data-spelling"]),
    ["Delta Industries"], "only the curated spellings are chips");
  assert.ok(!html.includes(WORKSPACE.spellingsPending),
    "a curated row is settled, so it never shows the pending placeholder");
});

test("a Value card names EVERY method that found it", () => {
  // Provenance is DISPLAYED, not merely stored: a precedence rule whose inputs
  // the user cannot see is indistinguishable from randomness. It is a SET,
  // because two methods agreeing is a different position from either alone.
  resetState();
  addSuggestions([{
    discoveryMethods: ["signal", "heuristic"], mainText: "Meridian", category: "entity_names",
  }]);
  acceptSuggestion("Meridian");
  setValueSpellings("entity_names", "Meridian", ["Meridian"]);

  const chips = all(valuesTab(getState()), "span.method-chip");
  assert.deepEqual(chips.map((c) => c.inner),
    [WORKSPACE.methodLabel.signal, WORKSPACE.methodLabel.heuristic]);
  assert.match(chips[0].attrs.class, /method-signal/);
});

test("a Value card explains the evidence behind it", () => {
  // A Suggestion the user accepted because an email address pointed at it should
  // still say so afterwards: the explanation is why they said yes.
  resetState();
  addSuggestions([{
    discoveryMethods: ["signal"], mainText: "Pierre Dupont", category: "person_names",
    evidence: [{
      kind: "email_local_part", signalCategory: "email",
      signalText: "pierre.dupont@tpps.com", documents: ["engagement.md"],
    }],
  }]);
  acceptSuggestion("Pierre Dupont");
  setValueSpellings("person_names", "Pierre Dupont", ["Pierre Dupont"]);

  const note = textOf(valuesTab(getState()), "span.evidence-note");
  assert.match(note, /pierre\.dupont@tpps\.com/, "the evidence names what can be checked");
  assert.match(note, /engagement\.md/, "and where it was found");
});

test("a Value the user typed is labelled as theirs", () => {
  resetState();
  seed("entity_names", "Alpine");
  const chip = one(valuesTab(getState()), "span.method-chip");
  assert.equal(chip.inner, WORKSPACE.methodLabel.manual);
});

// --- Intersections on the value card -------------------------------------

/** overlap(patch) is an intersection row as Go sends it. */
function overlap(patch = {}) {
  return {
    value: "marie.duval@example.com", category: "person_names", matchClass: "user_defined",
    winnerValue: "marie.duval@example.com", winnerCategory: "email",
    winnerMatchClass: "built_in_pattern",
    occurrences: 2, totalOccurrences: 2,
    ...patch,
  };
}

test("a fully covered value says it is never replaced under its own type", () => {
  resetState();
  seed("person_names", "marie.duval@example.com");
  setIntersections([overlap()]);
  const html = valuesTab(getState());

  const note = one(html, "div.intersection-note");
  assert.ok(note.inner.includes("Every occurrence"),
    "a value nothing leaves alone is the case worth shouting about");
  // The route is named in the same words the origin chip uses.
  assert.ok(note.inner.includes(WORKSPACE.matchClassLabel.built_in_pattern),
    "the warning names the winning method, never an internal rank");
  assert.ok(note.inner.includes("Priority order"), "the rule that decided is stated");
});

test("a partly covered value gets the milder count sentence", () => {
  resetState();
  seed("entity_names", "Meridian");
  setIntersections([overlap({
    value: "Meridian", category: "entity_names",
    winnerValue: "Meridian-4471", winnerCategory: "custom_patterns",
    winnerMatchClass: "user_defined",
    occurrences: 1, totalOccurrences: 3,
  })]);
  const note = one(valuesTab(getState()), "div.intersection-note");
  assert.ok(note.inner.includes("1 of 3"), "the counts are the difference between a note and an alarm");
  assert.ok(!note.inner.includes("Every occurrence"));
});

test("an intersection warns, it does not look like a blocking conflict", () => {
  resetState();
  seed("person_names", "marie.duval@example.com");
  setIntersections([overlap()]);
  const card = one(valuesTab(getState()), "div.value-card");

  assert.match(card.attrs.class, /intersects/);
  assert.ok(!/conflicted/.test(card.attrs.class),
    "the precedence rule has an answer, so the run is not refused");
  assert.ok(!exists(valuesTab(getState()), "button.value-solve"),
    "Solve conflicts is for the three blocking kinds, not for a warning");
});

test("an intersection does not close the step 2 to 3 gate", () => {
  // The gate exists for unreviewed SUGGESTIONS. An intersection is a decision
  // the engine can make on its own, so it must never block navigation.
  resetState();
  setState({ documents: [{ name: "a.txt" }] });
  seed("person_names", "marie.duval@example.com");
  setIntersections([overlap()]);
  assert.equal(canGoTo("anonymise"), true,
    "an intersection is a warning, so it must not stand between the steps");
});

test("a card with no intersection renders no note", () => {
  resetState();
  seed("entity_names", "Alpine");
  setIntersections([overlap()]); // belongs to a different value
  const html = valuesTab(getState());
  assert.ok(!exists(html, "div.intersection-note"));
  assert.ok(!/intersects/.test(one(html, "div.value-card").attrs.class));
});

test("a folded family is ONE Suggestion row that names its spellings", () => {
  // Three rows for "Alpine Trust", "Alpine Trust S.A." and "Alpine Trust Ltd."
  // invite three separate accept decisions for one company. Accepting the one
  // row accepts the spellings too, so the row has to say which.
  resetState();
  addSuggestions([{
    discoveryMethods: ["heuristic"], mainText: "Alpine Trust", category: "entity_names",
    count: 5, spellings: ["Alpine Trust S.A."],
  }]);
  const html = suggestionsTab(getState(), getState().suggestions);

  assert.equal(all(html, "div.grid-row").length, 1, "one family, one row");
  assert.ok(textOf(html, "span.sugg-spellings").includes("Alpine Trust S.A."),
    "the row names what accepting it will also replace");
});

test("a suggestion with no folded spellings says nothing extra", () => {
  resetState();
  addSuggestions([{ discoveryMethods: ["heuristic"], mainText: "Meridian", category: "entity_names", count: 2 }]);
  const html = suggestionsTab(getState(), getState().suggestions);
  assert.ok(!exists(html, "span.sugg-spellings"));
});

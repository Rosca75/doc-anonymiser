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
  relatedTo,
} from "./state.js";
import {
  valuesTab, suggestionsTab, visibleValues, previewSpellings,
} from "./views/identifyworkspace.js";
import { all, one, exists, textOf, stripTags, attr } from "./testhtml.js";
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

test("spellings are always shown, and there is no toggle to hide them", () => {
  // The card shows a bounded one-line preview and the popup owns the full list,
  // so a toggle would change every card's height for no information the popup
  // does not already give. Height is the whole point: a list of cards that
  // resizes under the pointer loses the reader's place.
  resetState();
  seed("person_names", "Marie Duval", ["Marie Duval", "Marie"]);
  const html = valuesTab(getState());
  assert.ok(exists(html, "span.spelling-chip"), "spellings are shown");
  assert.ok(!exists(html, "button#btn-toggle-derivedSpellings"),
    "a control that changes every card's height is not offered");
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

  // It is behind the card's info tooltip rather than on a row of its own: it is
  // informational, and a row that appears when evidence arrives is one more thing
  // making the card's height depend on its data.
  const note = attr(valuesTab(getState()), "span.help-bubble", "id");
  assert.ok(note, "the card carries an info tooltip");
  const text = textOf(valuesTab(getState()), "span.help-bubble");
  assert.match(text, /pierre\.dupont@tpps\.com/, "the evidence names what can be checked");
  assert.match(text, /engagement\.md/, "and where it was found");
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

  // The warning is an ICON whose surface holds the text and the actions. It is
  // not a row, because a row that appears when a warning arrives and vanishes
  // when it clears makes the card's height depend on its data.
  const pop = one(html, "span.warnpop");
  assert.match(pop.attrs.class, /warnpop-warn/,
    "an intersection is a warning, so it wears the warning tone");
  const text = textOf(html, "span.warnpop-bubble");
  assert.ok(text.includes("Every occurrence"),
    "a value nothing leaves alone is the case worth shouting about");
  // The winning method is named in the same words the method chip uses.
  assert.ok(text.includes(WORKSPACE.matchClassLabel.built_in_pattern),
    "the warning names the winning method, never an internal rank");
  assert.ok(text.includes("Priority order"), "the rule that decided is stated");
  // The two gestures that resolve it come with the sentence that explains it.
  const bubble = one(html, "span.warnpop-bubble").inner;
  assert.match(bubble, /class="[^"]*intersection-allow/,
    "the covering term can be allowlisted from where the warning is read");
  assert.match(bubble, /class="[^"]*value-group/,
    "and the two values can be grouped from there too");
});

test("a blocking conflict wears the error tone, an intersection does not", () => {
  // One icon and never two: a card with something to FIX before the run has that
  // as its whole message until it is fixed.
  resetState();
  seed("entity_names", "Acme", ["Acme"]);
  seed("person_names", "Acme", ["Acme"]); // the same name under two types
  const html = valuesTab(getState());
  const pops = all(html, "span.warnpop");
  assert.equal(pops.length, 2, "both sides of the ambiguity are warned");
  for (const pop of pops) {
    assert.match(pop.attrs.class, /warnpop-bad/, "a conflict is an error, not a warning");
  }
  for (const bubble of all(html, "span.warnpop-bubble")) {
    assert.match(bubble.inner, /class="[^"]*value-solve/,
      "Solve conflicts is reached from the warning that explains it");
  }
});

test("a clean card carries no status icon at all", () => {
  resetState();
  seed("entity_names", "Alpine", ["Alpine"]);
  assert.ok(!exists(valuesTab(getState()), "span.warnpop"),
    "nothing is wrong, so nothing is drawn");
});

test("a fully covered value with no separate literal names itself once", () => {
  // The winner sat on the value's own text, so Go sends no matchedTexts and the
  // sentence must not read as though the value were a spelling of itself.
  resetState();
  seed("person_names", "marie.duval@example.com");
  setIntersections([overlap()]);
  // The visible TEXT, not the HTML: the quotation marks the sentence puts round
  // the value are escaped entities in the markup and only decode on the way to
  // the user, which is who the sentence is for.
  const sentence = textOf(valuesTab(getState()), "span.warnpop-bubble");
  assert.ok(sentence.includes('"marie.duval@example.com"'), "the value is quoted");
  assert.ok(!sentence.includes("a spelling of"),
    "there is no other literal to name, so no spelling clause");
});

test("a covered value whose literal differs names the literal as a spelling", () => {
  // "Coca" occurs only inside lowercase email domains, so quoting the declared
  // form would claim the document holds "Coca" where it holds "coca".
  resetState();
  seed("entity_names", "Coca");
  setIntersections([overlap({
    value: "Coca", category: "entity_names",
    winnerValue: "sales@coca.us", winnerCategory: "email",
    winnerMatchClass: "built_in_pattern",
    matchedTexts: ["coca"],
  })]);
  const sentence = textOf(valuesTab(getState()), "span.warnpop-bubble");
  assert.ok(sentence.includes('"coca" (a spelling of "Coca")'),
    `the literal covered text is named, got ${sentence}`);
});

test("two covered fragments are both named, in the plural", () => {
  // A person's derived spellings match separately inside an address; the full
  // name occurs nowhere in it, so the sentence has to name both fragments.
  resetState();
  seed("person_names", "Pierre Dupont");
  setIntersections([overlap({
    value: "Pierre Dupont", category: "person_names",
    winnerValue: "pierre.dupont@coca.us", winnerCategory: "email",
    winnerMatchClass: "built_in_pattern",
    matchedTexts: ["pierre", "dupont"],
  })]);
  const sentence = textOf(valuesTab(getState()), "span.warnpop-bubble");
  assert.ok(sentence.includes('"pierre", "dupont" (spellings of "Pierre Dupont")'),
    `both fragments are named, got ${sentence}`);
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

// --- Relatedness: shared evidence, never automatic grouping ---------------
//
// Two organisations reached through one email domain may genuinely be two legal
// entities or two country branches. Folding them automatically would give one
// placeholder to two companies, and the mapping CSV would then state that two
// different organisations were the same one. So shared evidence produces a NOTE
// and the grouping stays the user's decision.

/** domainEvidence(text) is one piece of domain evidence, as Go sends it. */
function domainEvidence() {
  return [{
    kind: "email_domain", signalCategory: "email",
    signalText: "pierre.dupont@tpps.com", documents: ["mail.md"],
  }];
}

test("two Suggestions sharing evidence are named as related, not merged", () => {
  resetState();
  addSuggestions([
    { mainText: "Tpps France", category: "entity_names", count: 2, discoveryMethods: ["signal"], evidence: domainEvidence() },
    { mainText: "Tpps Holdings", category: "entity_names", count: 1, discoveryMethods: ["signal"], evidence: domainEvidence() },
  ]);
  const shown = getState().suggestions;
  assert.equal(shown.length, 2, "shared evidence must NOT collapse two rows into one");

  const html = suggestionsTab(getState(), shown);
  const notes = all(html, "span.related-note").map((n) => stripTags(n.inner));
  assert.equal(notes.length, 2, "each row names the other");
  assert.match(notes[0], /Tpps Holdings/);
  assert.match(notes[1], /Tpps France/);
  // Neither has quietly taken the other on as a spelling, which is what an
  // automatic fold would look like from the outside.
  for (const row of shown) {
    assert.deepEqual(row.spellings, [], `${row.mainText} must carry no folded rival`);
  }
});

test("relatedness is by the RELATIONSHIP, not by the document it was found in", () => {
  // The same email domain seen in two files is one relationship. Keying on the
  // document list would make two rows from two files look unrelated.
  resetState();
  addSuggestions([
    {
      mainText: "Tpps France", category: "entity_names", discoveryMethods: ["signal"],
      evidence: [{ kind: "email_domain", signalCategory: "email", signalText: "a@tpps.com", documents: ["one.md"] }],
    },
    {
      mainText: "Tpps Holdings", category: "entity_names", discoveryMethods: ["signal"],
      evidence: [{ kind: "email_domain", signalCategory: "email", signalText: "a@tpps.com", documents: ["two.md"] }],
    },
  ]);
  assert.deepEqual(relatedTo(getState().suggestions[0], getState().suggestions), ["Tpps Holdings"]);
});

test("rows with different evidence are not related", () => {
  resetState();
  addSuggestions([
    {
      mainText: "Tpps France", category: "entity_names", discoveryMethods: ["signal"],
      evidence: [{ kind: "email_domain", signalCategory: "email", signalText: "a@tpps.com" }],
    },
    {
      mainText: "Meridian", category: "entity_names", discoveryMethods: ["signal"],
      evidence: [{ kind: "email_domain", signalCategory: "email", signalText: "b@meridian.com" }],
    },
  ]);
  assert.deepEqual(relatedTo(getState().suggestions[0], getState().suggestions), []);
  assert.equal(all(suggestionsTab(getState(), getState().suggestions), "span.related-note").length, 0);
});

test("a row with no evidence is related to nothing", () => {
  // A heuristic finding carries no evidence, so it must not be swept into every
  // other evidence-free row's related list.
  resetState();
  addSuggestions([
    { mainText: "Alpha", category: "entity_names", discoveryMethods: ["heuristic"] },
    { mainText: "Beta", category: "entity_names", discoveryMethods: ["heuristic"] },
  ]);
  assert.deepEqual(relatedTo(getState().suggestions[0], getState().suggestions), []);
});

test("an accepted Value keeps naming the Values that share its evidence", () => {
  // The note survives the accept, because the question it answers ("are these the
  // same company?") is still open afterwards.
  resetState();
  addSuggestions([
    { mainText: "Tpps France", category: "entity_names", discoveryMethods: ["signal"], evidence: domainEvidence() },
    { mainText: "Tpps Holdings", category: "entity_names", discoveryMethods: ["signal"], evidence: domainEvidence() },
  ]);
  acceptSuggestion("Tpps France");
  acceptSuggestion("Tpps Holdings");
  for (const v of getState().values) {
    setValueSpellings("entity_names", v.mainText, [v.mainText]);
  }

  const notes = all(valuesTab(getState()), "span.help-bubble").map((n) => stripTags(n.inner));
  assert.equal(notes.length, 2, "each card carries its own info tooltip");
  assert.match(notes[0], /Tpps Holdings/);
});

test("a value card names itself through data-main-text, all lower case", () => {
  // A browser lower-cases attribute NAMES while parsing, so a camel-case data
  // attribute reaches the DOM under a key no handler reads: every action on the
  // card then resolves against `undefined` and silently does nothing. The
  // dash form is the only spelling that survives the parser as dataset.mainText.
  resetState();
  seed("person_names", "Marie Duval", ["Marie Duval"]);
  const html = valuesTab(getState());

  assert.equal(attr(html, ".value-card", "data-main-text"), "Marie Duval",
    "the card carries its main text under the dash-cased name");
  assert.equal(attr(html, ".value-card", "data-mainText"), undefined,
    "the camel-case spelling must not be rendered at all");
});

test("no attribute name in the values tab carries an upper-case letter", () => {
  // The local half of dataset_parity_test.go: this tab is the one that broke, so
  // it asserts the rule on its own output too, where a failure names the tab.
  resetState();
  seed("entity_names", "Acme", ["Acme"]);
  seed("person_names", "Marie Duval", ["Marie Duval"]);
  const html = valuesTab(getState());

  const offenders = [];
  for (const el of all(html, "div")) {
    for (const name of Object.keys(el.attrs)) {
      if (/[A-Z]/.test(name)) offenders.push(`${el.tag}[${name}]`);
    }
  }
  for (const el of all(html, "input")) {
    for (const name of Object.keys(el.attrs)) {
      if (/[A-Z]/.test(name)) offenders.push(`${el.tag}[${name}]`);
    }
  }
  assert.deepEqual(offenders, [],
    `these rendered attribute names carry an upper-case letter and are unreachable through dataset: ${offenders.join(", ")}`);
});

// --- The bounded chip row -------------------------------------------------
//
// The card shows the spellings that fit ONE LINE and puts the rest behind
// "+N more". That is a geometry contract: a chip row that grows with the data
// makes the card's height a function of its data, and a list of cards that
// resize under the pointer loses the reader's place.

test("previewSpellings spends a character budget, not a chip count", () => {
  // Three long spellings overflow a line that ten initials sit inside, which is
  // why the budget is characters rather than a fixed number of chips.
  const { shown, hidden } = previewSpellings(
    ["Alpine Trust Holdings SA", "Alpine Trust Holdings", "Alpine Trust", "Alpine", "AT"], 30);
  assert.deepEqual(shown, ["Alpine Trust Holdings SA"], "24 characters fit, the next 21 do not");
  assert.deepEqual(hidden, ["Alpine Trust Holdings", "Alpine Trust", "Alpine", "AT"]);
});

test("previewSpellings always shows one chip, however long it is", () => {
  // Otherwise a value whose only spelling is long renders a row of nothing but
  // "+1 more", which reads as a card with no spellings at all.
  const long = "an extremely long single spelling that alone exceeds any sane budget";
  const { shown, hidden } = previewSpellings([long], 10);
  assert.deepEqual(shown, [long]);
  assert.deepEqual(hidden, []);
});

test("previewSpellings stops at the first chip that does not fit", () => {
  // It does not skip past it to pick up a shorter one further down: the list is
  // ordered, and a preview that reorders it is a preview of a different list.
  const { shown, hidden } = previewSpellings(["abcde", "fghijklmno", "pq"], 10);
  assert.deepEqual(shown, ["abcde"]);
  assert.deepEqual(hidden, ["fghijklmno", "pq"]);
});

test("previewSpellings keeps everything when it all fits", () => {
  const { shown, hidden } = previewSpellings(["Marie Duval", "Marie", "M. Duval"], 65);
  assert.deepEqual(shown, ["Marie Duval", "Marie", "M. Duval"]);
  assert.deepEqual(hidden, []);
});

test("a card with few spellings shows them all and offers no overflow", () => {
  resetState();
  seed("person_names", "Marie Duval", ["Marie Duval", "Marie"]);
  const html = valuesTab(getState());
  assert.equal(all(html, "span.spelling-chip").length, 2);
  assert.ok(!exists(html, "button.spelling-more"), "nothing overflows, so nothing is offered");
  assert.ok(exists(html, "button.spelling-add"), "adding is always offered");
});

test("a card with many spellings shows a preview and counts the rest", () => {
  resetState();
  const many = [
    "Meridian Consulting Group SA", "Meridian Consulting Group",
    "Meridian Consulting", "Meridian", "MCG", "MC",
  ];
  seed("entity_names", "Meridian", many);
  const html = valuesTab(getState());

  const chips = all(html, "span.spelling-chip").map((c) => c.attrs["data-spelling"]);
  const { shown, hidden } = previewSpellings(many);
  assert.deepEqual(chips, shown, "the card shows exactly what the budget allows");
  assert.ok(hidden.length > 0, "this fixture must actually overflow, or it proves nothing");
  assert.equal(textOf(html, "button.spelling-more"), WORKSPACE.moreSpellings(hidden.length));
});

test("the visible chips are drag sources and carry no delete of their own", () => {
  // Dragging a chip onto another card still regroups it: that is the quick
  // gesture. The per-chip delete grew the row, so it lives in the popup, which is
  // also where a spelling in the overflow is reached.
  resetState();
  seed("person_names", "Marie Duval", ["Marie Duval", "Marie"]);
  const html = valuesTab(getState());
  for (const chip of all(html, "span.spelling-chip")) {
    assert.equal(chip.attrs.draggable, "true");
    assert.ok(chip.attrs["data-spelling"], "the drag payload is on the chip");
  }
  assert.ok(!exists(html, "button.spelling-del"), "no per-chip delete on the compact card");
});

test("a pending expansion replaces the chips inside the row, never below it", () => {
  // This is the height change the owner reported as a jumping scrollbar: editing
  // a spelling resets the row to pending, the chips vanish, the card shrinks, and
  // the browser clamps the list's scroll offset to the shorter content.
  resetState();
  addValues([{ category: "person_names", mainText: "Marie Duval" }]);
  const html = valuesTab(getState());
  const row = one(html, "div.spelling-row");
  assert.ok(stripTags(row.inner).includes(WORKSPACE.spellingsPending),
    "the pending line is inside the one row");
  assert.equal(all(html, "div.value-note").length, 0,
    "and it adds no row of its own");
});

// anonymise.test.js, tests for the Anonymise screen's pure helpers
//
// views/anonymise.js imports api.js, which only touches `window` inside its
// functions, so the module imports cleanly here. Only the PURE exports are
// exercised; the rest of the view is wiring and belongs to the manual pass.
//
// The occurrence count is the one worth testing hardest. The report Go sends
// carries per-CATEGORY totals; the per-VALUE counts in the drill-down are
// computed here, in JS, from the anonymised text of the documents in scope. That
// is what makes the scope selector mean anything, and it is easy to get subtly
// wrong.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  countOccurrences, valuesInCategory, formatDuration, continueHint,
  compareCard, reportCard, valuesCard, filterValues, blockedPanel, selectedCard,
  runCard, missedCard, renderAnonymise, paneWalk, paneCaption, paneSearchControls,
  selectionPanel, applySelection, wireSelectionPanel, missingDeclaredTexts,
} from "./views/anonymise.js";
import {
  resetState, setState, getState, subscribe, addValues, foldIntoFamily, buildRunRequest,
  NAME_CATEGORIES, addAllowTerm, valueKey,
} from "./state.js";
import { readFileSync } from "node:fs";
import { ANONYMISE, WORKSPACE } from "./copy.js";
import { textOf, all, attr, exists } from "./testhtml.js";
import { container, fire } from "./testdom.js";

// --- countOccurrences ----------------------------------------------------

test("countOccurrences counts non-overlapping occurrences", () => {
  assert.equal(countOccurrences("[A_1] and [A_1] again", "[A_1]"), 2);
  assert.equal(countOccurrences("nothing here", "[A_1]"), 0);
  assert.equal(countOccurrences("", "[A_1]"), 0);
});

test("countOccurrences treats a placeholder as LITERAL text, not a regex", () => {
  // A placeholder contains [ and ], which are regex metacharacters. Read as a
  // character class, "[A_1]" would match any of A, _ or 1, and every count on
  // the report would be nonsense.
  assert.equal(countOccurrences("A_1 A_1 A_1", "[A_1]"), 0,
    "the brackets are part of the string, not a character class");
  assert.equal(countOccurrences("[A_1]", "[A_1]"), 1);
});

test("countOccurrences returns 0 for an empty needle rather than a huge number", () => {
  // "".split("") would report one per character, which as an occurrence count is
  // both wrong and alarming.
  assert.equal(countOccurrences("anything at all", ""), 0);
  assert.equal(countOccurrences("anything", undefined), 0);
});

// --- The report's value list ---------------------------------------------
//
// The per-value counts come from GO now (report.values, computed once per run)
// rather than from recounting placeholders in the anonymised text on every
// repaint. These tests pin the two things that matters to the user: the flat
// list is there without clicking anything, and the scope selector picks the
// list Go computed for that document.

const RUN_VALUES = [
  { original: "Marie Duval", placeholder: "[PERSON_1]", category: "person_names", count: 3 },
  { original: "Meridian Consulting", placeholder: "[ENTITY_1]", category: "entity_names", count: 2 },
  { original: "Thomas Berger", placeholder: "[PERSON_2]", category: "person_names", count: 1 },
];

/** reportState(patch) is a state carrying one finished run. */
function reportState(patch = {}) {
  return {
    documents: [{ name: "a.docx", markdown: "source", previewTruncated: false, isGrid: false }],
    sourceCache: {},
    results: {
      documents: [
        { name: "a.docx", anonymised: "[PERSON_1] met [ENTITY_1].", byCategory: { person_names: 3, entity_names: 2 } },
        { name: "b.pptx", anonymised: "[PERSON_2] chaired.", byCategory: { person_names: 1 } },
      ],
      report: {
        level: "medium",
        totalReplacements: 6, byCategory: { person_names: 4, entity_names: 2 },
        values: RUN_VALUES,
        documents: [
          { name: "a.docx", values: RUN_VALUES.slice(0, 2) },
          { name: "b.pptx", values: RUN_VALUES.slice(2) },
        ],
      },
    },
    mapping: {},
    replacedValues: RUN_VALUES,
    removedValues: [],
    values: [],
    dismissedWarnings: [],
    ...patch,
  };
}

test("the Replaced values table lists every value without any clicking", () => {
  const html = valuesCard(reportState());
  const rows = all(html, "div.value-row");
  assert.equal(rows.length, 3);
  assert.deepEqual(rows.map((r) => textOf(r.outer, "span.report-original")),
    ["Marie Duval", "Meridian Consulting", "Thomas Berger"]);
  assert.deepEqual(rows.map((r) => textOf(r.outer, "span.report-count")), ["3", "2", "1"]);
});

test("every row offers the two things a value can have done to it", () => {
  // The replacement string is editable and the value is removable, for EVERY
  // trigger: the rows come from the registry, which does not record what found
  // a value, so a regex match and a name typed by hand get the same row.
  const rows = all(valuesCard(reportState()), "div.value-row");
  for (const row of rows) {
    assert.ok(all(row.outer, "input.ph-input").length === 1,
      "the placeholder has to be editable here, the only screen where it exists");
    assert.ok(all(row.outer, "button.value-remove").length === 1,
      "any value can be removed after the run");
  }
});

test("the table reads the registry, not the report", () => {
  // A row built from report text could offer an edit with no registry entry
  // behind it, and the edit would fail on a value the screen just showed.
  const s = reportState({ replacedValues: [] });
  assert.equal(all(valuesCard(s), "div.value-row").length, 0,
    "no registry rows means no table rows, whatever the report says");
});

test("removed values are a collapsed list with a way back", () => {
  const s = reportState({
    removedValues: [{ original: "Thomas Berger", category: "person_names", placeholder: "[PERSON_2]" }],
  });
  const html = valuesCard(s);
  const removed = all(html, "div.removed-row");
  assert.equal(removed.length, 1);
  assert.equal(textOf(removed[0].outer, "span.report-original"), "Thomas Berger");
  assert.ok(all(removed[0].outer, "button.value-restore").length === 1);
  assert.match(html, /NEW placeholder/,
    "the restore has to say the number changes, because an export may carry the old one");
});

test("with nothing removed the list is absent, not empty", () => {
  assert.equal(all(valuesCard(reportState()), "div.removed-row").length, 0);
  assert.ok(!valuesCard(reportState()).includes("Removed values"),
    "a heading over an empty list invites a search for something that is not there");
});

// --- The Selected placeholder card ---------------------------------------

test("the Selected placeholder card edits the replacement value, like the table", () => {
  // Clicking a mark opens this card. It must let the placeholder be changed, not
  // only turned into a spelling of another value: the same registry entry sits
  // behind the mark and behind the Replaced values row, so both edit it.
  const html = selectedCard(reportState(), { placeholder: "[PERSON_1]", original: "Marie Duval" });
  assert.equal(all(html, "input#selected-ph-input").length, 1,
    "the replacement value has to be editable here too, not just in the table");
  assert.equal(all(html, "input#reassign-input").length, 1,
    "the 'make it a spelling of' field stays: this adds an action, it does not remove one");
  assert.match(html, /Marie Duval/, "the card still names what the placeholder replaces");
});

test("the report note says the preset the run used, and nothing about an AI pass", () => {
  const s = reportState();
  const note = textOf(reportCard(s), "#report-run-note");
  assert.match(note, /medium/);
  assert.doesNotMatch(note, /deep scan|AI/i,
    "Anonymise runs no discovery method, so the run note must not mention an AI pass");
});

test("the Report card renders the overlap warnings the run computes, and dismisses them", () => {
  // Validation.Warnings is computed on every run (a declared Value losing
  // text to a built-in pattern) and nothing was rendering it: it shows here
  // beside Report.Warnings, and the same dismiss affordance works on it.
  resetState();
  const s = reportState({
    results: {
      ...reportState().results,
      validation: { warnings: [{ message: "Alpine Trust lost some text to a stronger match." }] },
    },
  });
  const html = reportCard(s);
  assert.match(html, /Alpine Trust lost some text to a stronger match\./);

  const dismissed = { ...s, dismissedWarnings: ["Alpine Trust lost some text to a stronger match."] };
  assert.doesNotMatch(reportCard(dismissed), /Alpine Trust lost some text/,
    "a dismissed validation warning must not keep rendering");
});

test("filterValues searches the value and the placeholder, because users use both", () => {
  assert.deepEqual(filterValues(RUN_VALUES, "meridian").map((v) => v.placeholder), ["[ENTITY_1]"]);
  assert.deepEqual(filterValues(RUN_VALUES, "person_2").map((v) => v.placeholder), ["[PERSON_2]"]);
  assert.equal(filterValues(RUN_VALUES, "").length, 3);
  assert.equal(filterValues(RUN_VALUES, "nothing here").length, 0);
  assert.deepEqual(filterValues(undefined, "x"), []);
});

test("an empty run says so rather than rendering an empty list", () => {
  const s = reportState({ replacedValues: [] });
  s.results.report.values = [];
  s.results.documents = [];
  assert.match(reportCard(s), /Nothing was replaced/);
  assert.match(valuesCard(s), /Nothing was replaced/);
});

test("valuesInCategory filters the list Go computed", () => {
  const rows = valuesInCategory(reportState(), [], "person_names");
  assert.deepEqual(rows.map((r) => [r.placeholder, r.count]),
    [["[PERSON_1]", 3], ["[PERSON_2]", 1]]);
});

test("valuesInCategory copes with a state that has no run in it", () => {
  assert.deepEqual(valuesInCategory({}, [], "person_names"), []);
  assert.deepEqual(valuesInCategory({ results: null }, [], "person_names"), []);
});

// --- formatDuration ------------------------------------------------------

test("formatDuration reads as a duration, not as a raw count", () => {
  assert.equal(formatDuration(0), "0 ms");
  assert.equal(formatDuration(340), "340 ms");
  assert.equal(formatDuration(999), "999 ms");
  assert.equal(formatDuration(1000), "1.0 s");
  assert.equal(formatDuration(1840), "1.8 s");
  assert.equal(formatDuration(62000), "62.0 s");
});

test("formatDuration handles a missing or negative figure", () => {
  for (const bad of [undefined, null, -1, NaN]) {
    assert.equal(formatDuration(bad), "0 ms", JSON.stringify(bad));
  }
});

// --- The refused run -----------------------------------------------------
//
// A blocking validation conflict aborts the engine before pass 1: the results
// carry empty documents and an empty report, so the summary reads 0/0/0 while an
// earlier run's registry still fills the value table. These tests pin that the
// screen now explains the refusal instead of showing that silent mismatch.

/** blockedState() is a state whose last run was refused by a spelling collision. */
function blockedState(patch = {}) {
  return {
    running: false,
    documents: [{ name: "a.pdf", markdown: "source" }],
    results: {
      documents: [],
      report: { totalReplacements: 0, byCategory: {} },
      validation: {
        blocking: [{
          kind: "collision", severity: "block", value: "Mendonça",
          message: "Two person values both claim the spelling \"Mendonça\".",
          fix: "Remove one of the two values on the Identify step.",
        }],
      },
    },
    ...patch,
  };
}

test("blockedPanel names the conflict and its fix on a refused run", () => {
  const html = blockedPanel(blockedState());
  assert.match(html, /The run was refused/);
  assert.match(html, /Mendon/);
  assert.match(html, /Remove one of the two values/);
  assert.match(html, /How to fix it/);
});

test("blockedPanel is empty when there is no blocking conflict", () => {
  assert.equal(blockedPanel({ results: { validation: { blocking: [] } } }), "");
  assert.equal(blockedPanel({ results: null }), "");
  assert.equal(blockedPanel({}), "");
});

test("continueHint refuses to leave the step while a run is blocked", () => {
  // A refused run has results but changed nothing, so the plain "0 replacements
  // ready" hint would invite the user to export a run that never happened.
  assert.match(continueHint(blockedState()), /Resolve the conflict/);
});

// --- continueHint --------------------------------------------------------

test("continueHint says what has to happen before the step can be left", () => {
  assert.match(continueHint({ running: false, results: null }), /Run the anonymisation/);
  assert.match(continueHint({ running: true, results: null }), /Running/);
});

test("continueHint counts what is ready once there is a result", () => {
  assert.equal(
    continueHint({ running: false, results: { report: { totalReplacements: 7 } } }),
    "7 replacements ready to export");
  assert.equal(
    continueHint({ running: false, results: { report: { totalReplacements: 1 } } }),
    "1 replacement ready to export");
  // A run that replaced nothing is still a run: the step is done and the user may
  // legitimately want to export the (unchanged) copies.
  assert.equal(
    continueHint({ running: false, results: { report: {} } }),
    "0 replacements ready to export");
});

// --- compareCard: the ORIGINAL pane shows SOURCE text --------------------
//
// Reported issue 4: "the preview area ORIGINAL must show the original text,
// not anonymised values such as [DATE_1], [PERSON_2]". The pane now reads the
// IMPORT LIST (one producer of original text in the whole application), so
// these tests pin both the content and where it comes from.

const SOURCE = "Marie Duval met Meridian Consulting on 12 March 2024.";
const ANONYMISED = "[PERSON_1] met [ENTITY_1] on [DATE_1].";

/** compareState() is a state with one imported document and one result. */
function compareState(overrides = {}) {
  return {
    documents: [{ name: "a.txt", markdown: SOURCE, previewTruncated: false, isGrid: false }],
    sourceCache: {},
    results: { documents: [{ name: "a.txt", anonymised: ANONYMISED, byCategory: { person_names: 1 } }] },
    mapping: {
      "[PERSON_1]": { original: "Marie Duval", category: "person_names" },
      "[ENTITY_1]": { original: "Meridian Consulting", category: "entity_names" },
      "[DATE_1]": { original: "12 March 2024", category: "date" },
    },
    ...overrides,
  };
}

test("compareCard renders the IMPORTED source in the ORIGINAL pane", () => {
  const s = compareState();
  const html = compareCard(s, s.results.documents[0]);
  assert.equal(textOf(html, "#original-pane"), SOURCE);
});

test("compareCard never puts a placeholder in the ORIGINAL pane", () => {
  const s = compareState();
  const shown = textOf(compareCard(s, s.results.documents[0]), "#original-pane");
  assert.ok(!/\[[A-Z][A-Z0-9_]*_\d+\]/.test(shown),
    "ORIGINAL must show the source text; placeholders belong to the other pane");
});

test("compareCard puts the anonymised text, marked up, in the ANONYMISED pane", () => {
  const s = compareState();
  const html = compareCard(s, s.results.documents[0]);
  assert.equal(textOf(html, "#anonymised-pane"), ANONYMISED);
  // Every known placeholder is a mark carrying its original, which is what
  // the hover tooltip reads.
  const marks = all(html, "mark[data-ph]");
  assert.deepEqual(marks.map((m) => m.attrs["data-original"]),
    ["Marie Duval", "Meridian Consulting", "12 March 2024"]);
});

test("compareCard ignores any stale copy of the source on the result document", () => {
  // The result no longer carries an `original` field. If one is ever added
  // back, the ORIGINAL pane must still read the import list, because the
  // import list is the only thing that cannot go stale.
  const s = compareState();
  const doc = { ...s.results.documents[0], original: "[PERSON_1] met [ENTITY_1]." };
  assert.equal(textOf(compareCard(s, doc), "#original-pane"), SOURCE);
});

test("compareCard falls back to the fetched source cache when the document left the import list", () => {
  const s = compareState({
    documents: [],
    sourceCache: { "a.txt": { markdown: SOURCE, truncated: false, isGrid: false, found: true } },
  });
  assert.equal(textOf(compareCard(s, s.results.documents[0]), "#original-pane"), SOURCE);
});

test("compareCard says so, rather than showing the anonymised text, when no source is known", () => {
  const s = compareState({
    documents: [],
    sourceCache: { "a.txt": { markdown: "", truncated: false, isGrid: false, found: false } },
  });
  const shown = textOf(compareCard(s, s.results.documents[0]), "#original-pane");
  assert.match(shown, /source text is not available/);
  assert.ok(!shown.includes("[PERSON_1]"));
});

test("compareCard carries the truncation notice through to the ORIGINAL pane", () => {
  const s = compareState({
    documents: [{ name: "a.txt", markdown: SOURCE, previewTruncated: true, isGrid: false }],
  });
  const html = compareCard(s, s.results.documents[0]);
  assert.match(textOf(html, "#original-pane"), /Preview truncated/);
  assert.ok(textOf(html, "#original-pane").includes(SOURCE));
});

// --- The run card: no discovery control, explanation on hover ------------
//
// Anonymise runs the deterministic passes and nothing else, so the card offers
// no discovery switch at all. Its explanation is a hover tooltip on the heading
// so the card stays a compact control strip.

test("the run card offers no discovery control", () => {
  assert.equal(all(runCard(reportState()), "input#deep-scan").length, 0,
    "Anonymise runs the deterministic passes only");
  assert.doesNotMatch(runCard(reportState()), /Deep scan/i,
    "no leftover copy naming a discovery pass on this step");
});

test("the run card carries its explanation on the heading, not as a subtitle", () => {
  const html = runCard(reportState());
  assert.equal(all(html, "span.card-sub").length, 0,
    "the status line is a hover tooltip now, not a visible subtitle");
  // reportState() is a finished, unblocked run, so the tooltip is subtitleDone.
  assert.equal(attr(html, "h2", "title"), ANONYMISE.subtitleDone,
    "the heading spells out the run's state on hover");
});

// --- Result sections start collapsed, and are gated on a run -------------
//
// renderAnonymise reads the module state, so these tests seed it through the
// state API and capture the HTML the view writes. wire() only touches a real
// DOM, and it runs after innerHTML is set, so the captured markup is complete.

/** renderColumn() returns the HTML renderAnonymise writes for the current
 *  module state. The fake container swallows the wiring calls. */
function renderColumn() {
  let html = "";
  const container = {
    set innerHTML(v) { html = v; },
    get innerHTML() { return html; },
    querySelector() { return null; },
    querySelectorAll() { return []; },
  };
  try { renderAnonymise(container); } catch { /* wiring needs a real DOM */ }
  return html;
}

const CR9_VALUES = [
  { original: "Marie Duval", placeholder: "[PERSON_1]", category: "person_names", count: 1 },
];

/** seedRun() puts one finished, unblocked run into the module state. */
function seedRun() {
  setState({
    running: false,
    results: {
      documents: [{ name: "a.txt", anonymised: "[PERSON_1].", byCategory: { person_names: 1 } }],
      report: {
        level: "medium", totalReplacements: 1, byCategory: { person_names: 1 },
        values: CR9_VALUES, documents: [{ name: "a.txt", values: CR9_VALUES }],
      },
    },
    replacedValues: CR9_VALUES,
  });
}

test("the result cards are absent before the first run", () => {
  resetState();
  setState({ documents: [{ name: "a.txt", markdown: "x", previewTruncated: false, isGrid: false }] });
  const html = renderColumn();
  assert.doesNotMatch(html, new RegExp(ANONYMISE.missedTitle),
    "with no run yet there is nothing to add a missed Value against");
});

test("after a run every result card renders, and each starts collapsed", () => {
  resetState();
  setState({ documents: [{ name: "a.txt", markdown: "x", previewTruncated: false, isGrid: false }] });
  seedRun();
  const html = renderColumn();
  assert.match(html, new RegExp(ANONYMISE.missedTitle),
    "Add missed Value appears once a run has happened");
  // The three foldable result cards (values, report, missed) all fold shut.
  assert.equal((html.match(/data-open="false"/g) ?? []).length, 3,
    "values, report and Add missed Value all start collapsed");
  assert.equal((html.match(/data-open="true"/g) ?? []).length, 0,
    "nothing in the result column starts open");
});

test("a refused run still shows Add missed Value, the one exit from a blocked screen", () => {
  resetState();
  setState({ documents: [{ name: "a.txt", markdown: "x", previewTruncated: false, isGrid: false }] });
  setState({
    running: false,
    results: {
      documents: [],
      report: { level: "medium", totalReplacements: 0, byCategory: {} },
      validation: { blocking: [{ kind: "ambiguity", message: "conflict", fix: "fix it" }] },
    },
    replacedValues: [],
  });
  const html = renderColumn();
  assert.match(html, new RegExp(ANONYMISE.missedTitle),
    "Add missed Value is the card that can clear the conflict it may have caused");
  assert.doesNotMatch(html, new RegExp(ANONYMISE.reportTitle),
    "the Report card stays hidden: a refused run has nothing to report");
});

// --- A refused run can be CLEARED from this screen ---------------------------
//
// Keeping the "Add missed Value" card visible makes the blocked screen a way back
// in. These are the way OUT: the conflict the run refused over has no row anywhere
// on step 3, and the only other route to a fix is Identify, which calls ResetRun on
// the way and discards the registry, so a mistyped declaration would cost every
// placeholder number the session had assigned.

/** blockedFor(conflict) is a state carrying one refused run. */
function blockedFor(conflict) {
  return {
    ...getState(),
    results: {
      documents: [],
      report: { level: "medium", totalReplacements: 0, byCategory: {} },
      validation: { blocking: [conflict] },
    },
  };
}

test("a blocking conflict over a declared value offers to delete it, by name", () => {
  resetState();
  addValues([{ category: "entity_names", mainText: "Meridian" }]);
  const html = blockedPanel(blockedFor({
    kind: "ambiguity", message: "conflict", fix: "fix it", value: "Meridian",
    refs: [{ kind: "value", category: "entity_names", mainText: "meridian" }],
  }));
  const actions = all(html, "button.blocked-delete-value");
  assert.equal(actions.length, 1);
  assert.equal(textOf(actions[0].outer, "button"), ANONYMISE.blockedDeleteValue("Meridian"),
    "named from the store, because the engine lower-cases mainText in a ref");
  assert.equal(actions[0].attrs["data-main-text"], "meridian");
  assert.equal(actions[0].attrs["data-category"], "entity_names");
});

test("an allowlist collision offers to take the term off the never-anonymise list", () => {
  resetState();
  addValues([{ category: "entity_names", mainText: "Meridian" }]);
  addAllowTerm("Meridian");
  const html = blockedPanel(blockedFor({
    kind: "collision", message: "conflict", fix: "fix it", value: "Meridian",
    refs: [
      { kind: "value", category: "entity_names", mainText: "meridian" },
      { kind: "allowlist", mainText: "meridian" },
    ],
  }));
  assert.equal(all(html, "button.blocked-allow-remove").length, 1);
  assert.equal(all(html, "button.blocked-delete-value").length, 0,
    "the never-anonymise term is what has to go, not the value the user declared");
  assert.equal(attr(html, "button.blocked-allow-remove", "data-term"), "Meridian");
});

test("a conflict naming no declared value offers nothing rather than a dead button", () => {
  resetState();
  const html = blockedPanel(blockedFor({
    kind: "ambiguity", message: "conflict", fix: "fix it", value: "Ghost",
    refs: [{ kind: "value", category: "entity_names", mainText: "ghost" }],
  }));
  assert.equal(all(html, "button.blocked-delete-value").length, 0);
});

test("the blocked panel's actions clear the conflict, through the real wiring", async () => {
  resetState();
  addValues([{ category: "entity_names", mainText: "Meridian" }]);
  addAllowTerm("Meridian");
  setState({
    documents: [{ name: "a.txt", markdown: "x", previewTruncated: false, isGrid: false }],
    running: false,
    results: {
      documents: [],
      report: { level: "medium", totalReplacements: 0, byCategory: {} },
      validation: {
        blocking: [{
          kind: "collision", message: "conflict", fix: "fix it", value: "Meridian",
          refs: [
            { kind: "value", category: "entity_names", mainText: "meridian" },
            { kind: "allowlist", mainText: "meridian" },
          ],
        }],
      },
    },
    replacedValues: [],
  });

  const root = container();
  renderAnonymise(root);
  const action = root.querySelector(".blocked-allow-remove");
  assert.ok(action, "the refused run offers its resolve action");
  await fire(action, "click");
  assert.deepEqual(getState().allowlist, [], "the term is off the never-anonymise list");
  assert.equal(getState().notice?.text, ANONYMISE.blockedAllowTermRemoved("Meridian"));
});

test("deleting the named value from the blocked panel removes exactly that value", async () => {
  resetState();
  addValues([{ category: "entity_names", mainText: "Meridian" }]);
  addValues([{ category: "person_names", mainText: "Marie Duval" }]);
  setState({
    documents: [{ name: "a.txt", markdown: "x", previewTruncated: false, isGrid: false }],
    running: false,
    results: {
      documents: [],
      report: { level: "medium", totalReplacements: 0, byCategory: {} },
      validation: {
        blocking: [{
          kind: "ambiguity", message: "conflict", fix: "fix it", value: "Meridian",
          refs: [{ kind: "value", category: "entity_names", mainText: "meridian" }],
        }],
      },
    },
    replacedValues: [],
  });

  const root = container();
  renderAnonymise(root);
  await fire(root.querySelector(".blocked-delete-value"), "click");
  assert.deepEqual(getState().values.map((v) => v.mainText), ["Marie Duval"]);
  assert.equal(getState().notice?.text, ANONYMISE.blockedValueDeleted("Meridian"));
});

test("each result card, rendered on its own, starts collapsed", () => {
  // The collapsed set is the single source of truth for all of them, so checking
  // each helper directly guards that membership.
  const s = reportState();
  for (const [name, fn] of [["values", valuesCard], ["report", reportCard], ["missed", missedCard]]) {
    assert.equal(attr(fn(s), ".cgroup", "data-open"), "false",
      `${name} card must start collapsed`);
  }
});

// --- The Compare search, split per pane ----------------------------------
//
// Each pane carries its OWN search bar in its caption, so there is one walk per
// pane over that pane's own needle, and the readout does not name a pane because
// the bar is already on it.

/** paneState(needle, index) is one pane's view-local search state. */
function paneState(needle, index = 0) {
  return { needle, index };
}

test("a pane walk finds hits only in its own text", () => {
  // "met" occurs once in the original ("... Duval met Meridian ...") and once in
  // the anonymised ("[PERSON_1] met [ENTITY_1] ..."), and each walk counts only
  // the text it was given.
  const inOriginal = paneWalk(SOURCE, paneState("met"));
  assert.equal(inOriginal.total, 1);
  assert.equal(inOriginal.active, 0);

  const inAnon = paneWalk(ANONYMISED, paneState("met"));
  assert.equal(inAnon.total, 1);
  assert.equal(inAnon.active, 0);
});

test("a pane walk wraps at both ends", () => {
  // "ar" occurs twice in the source ("Marie", "March").
  assert.equal(paneWalk(SOURCE, paneState("ar", 2)).index, 0, "past the last hit wraps to the first");
  assert.equal(paneWalk(SOURCE, paneState("ar", -1)).index, 1, "before the first wraps to the last");
});

test("a needle that matches nothing leaves the walk empty", () => {
  const walk = paneWalk(SOURCE, paneState("Borealis"));
  assert.equal(walk.total, 0);
  assert.equal(walk.active, -1);
});

test("the readout names the count and the total, without a pane", () => {
  const walk = paneWalk(ANONYMISED, paneState("met"));
  const html = paneSearchControls("anonymised", walk, paneState("met"));
  assert.equal(textOf(html, "span.search-readout"), ANONYMISE.searchCount(1, 1));
});

test("each pane's search box carries its own id and both nav buttons render", () => {
  const walk = paneWalk(SOURCE, paneState("met"));
  const html = paneSearchControls("original", walk, paneState("met"));
  assert.equal(attr(html, "input#compare-search-original", "value"), "met");
  assert.ok(all(html, "button.search-prev").length === 1);
  assert.ok(all(html, "button.search-next").length === 1);
});

test("with no hits both buttons are disabled AND say why", () => {
  // A greyed control that says nothing is a dead end.
  const walk = paneWalk(SOURCE, paneState("Borealis"));
  const html = paneSearchControls("original", walk, paneState("Borealis"));
  for (const cls of ["search-prev", "search-next"]) {
    assert.equal(attr(html, `button.${cls}`, "disabled"), "");
    assert.equal(attr(html, `button.${cls}`, "title"), ANONYMISE.searchNone);
  }
  assert.equal(textOf(html, "span.search-readout"), ANONYMISE.searchNone);
});

test("an empty needle disables the buttons without claiming there is no match", () => {
  const walk = paneWalk(SOURCE, paneState(""));
  const html = paneSearchControls("original", walk, paneState(""));
  assert.equal(attr(html, "button.search-next", "disabled"), "");
  assert.equal(textOf(html, "span.search-readout"), "",
    "before anything is typed there is nothing to report");
});

test("a pane caption carries its name and its own search bar", () => {
  const walk = paneWalk(SOURCE, paneState(""));
  const html = paneCaption(ANONYMISE.paneOriginal, "original", walk, paneState(""));
  assert.equal(textOf(html, "span.pane-caption-label"), ANONYMISE.paneOriginal);
  assert.ok(exists(html, "input#compare-search-original"),
    "the search bar sits inside the caption");
});

test("compareCard renders one search bar per pane, in the pane's caption", () => {
  const s = compareState();
  const html = compareCard(s, s.results.documents[0]);
  assert.ok(exists(html, "input#compare-search-original"));
  assert.ok(exists(html, "input#compare-search-anonymised"));
  // With no needle typed, neither pane highlights anything: the panes render
  // exactly as they did before a search.
  assert.equal(all(html, "span.find-hit").length, 0);
});

// --- The selection panel is copy or replace, in three stages -------------
//
// The panel asks what to DO with the selection, and the two replace modes
// differ in what ends up in the re-identification key, which is why each one
// carries a hint: that difference is not guessable from the labels. Both modes
// go through the Value model, so nothing here can rewrite text without the key
// recording it.

/** view(patch) is the panel's view-local state, as the card reads it. */
function view(patch = {}) {
  return {
    selection: { text: "Meridian", x: 100, y: 40 },
    stage: null, mode: null, target: "", category: "person_names", error: "",
    ...patch,
  };
}

test("stage 1 offers exactly two things: copy, or replace", () => {
  const html = selectionPanel(compareState(), view());
  assert.ok(exists(html, "button#btn-selection-copy"));
  assert.ok(exists(html, "button#btn-selection-replace"));
  assert.ok(!exists(html, "input.selection-mode"), "the modes come after Replace");
  assert.ok(!exists(html, "button#btn-apply-selection"), "there is nothing to apply yet");
});

test("stage 2 offers the two replace modes, each with its hint", () => {
  const html = selectionPanel(compareState(), view({ stage: "replace" }));
  assert.deepEqual(all(html, "input.selection-mode").map((r) => r.attrs.value),
    ["spelling", "value"]);

  // The hints are the safety-relevant copy: they say what lands in the
  // re-identification key. Compared as rendered TEXT, because copy containing
  // an apostrophe is escaped in the markup.
  assert.deepEqual(
    all(html, "span.hint").map((h) => textOf(h.outer, "span.hint")),
    [
      ANONYMISE.selectionModeVariantHint,
      ANONYMISE.selectionModeValueHint,
    ]);
});

test("each mode's stage 3 shows its own field", () => {
  const s = compareState();

  const spelling = selectionPanel(s, view({ stage: "replace", mode: "spelling" }));
  assert.ok(exists(spelling, "input#selection-target"), "the spelling mode asks which Value");
  assert.ok(!exists(spelling, "select#selection-category"));

  const value = selectionPanel(s, view({ stage: "replace", mode: "value" }));
  assert.ok(exists(value, "select#selection-category"), "the new-Value mode asks which type");
  assert.ok(!exists(value, "input#selection-target"));
});

test("the spelling target's suggestions are real buttons, prefix matches first, no datalist", () => {
  resetState();
  addValues([{ category: "person_names", mainText: "Marie Duval" }]);
  addValues([{ category: "entity_names", mainText: "A Marseille Corp" }]);

  const html = selectionPanel(getState(), view({ stage: "replace", mode: "spelling", target: "mar" }));
  assert.ok(!exists(html, "datalist"), "a native popup rebuilt mid-keystroke cannot be relied on");
  const list = all(html, "div#selection-target-list")[0];
  const picks = all(list.inner, "button.reassign-pick");
  assert.deepEqual(picks.map((p) => p.attrs["data-main-text"]),
    ["Marie Duval", "A Marseille Corp"], "the prefix match comes before the substring match");
});

test("typing in the spelling target patches the suggestion list in place, without repainting", async () => {
  // The one-letter symptom, expressed as an assertion: a repaint on every
  // keystroke destroyed and recreated the input, so the second keystroke
  // landed on an element that no longer existed by the time it fired.
  resetState();
  addValues([{ category: "person_names", mainText: "Marie Duval" }]);

  const c = container();
  c.innerHTML = selectionPanel(getState(), view({ stage: "replace", mode: "spelling", target: "" }));
  wireSelectionPanel(c);

  let notified = false;
  const unsubscribe = subscribe(() => { notified = true; });
  try {
    const input = c.querySelector("#selection-target");
    input.value = "m";
    await fire(input, "input");
    input.value = "ma";
    await fire(input, "input");
  } finally {
    unsubscribe();
  }

  assert.equal(notified, false, "a keystroke must not trigger a full repaint");
  const list = c.querySelector("#selection-target-list");
  const picks = list.querySelectorAll(".reassign-pick");
  assert.equal(picks.length, 1, "both keystrokes landed, and the list reflects the final text");
  assert.equal(picks[0].dataset.mainText, "Marie Duval");
});

test("clicking a suggestion fills the field with its main text", async () => {
  resetState();
  addValues([{ category: "person_names", mainText: "Marie Duval" }]);

  const c = container();
  c.innerHTML = selectionPanel(getState(), view({ stage: "replace", mode: "spelling", target: "" }));
  wireSelectionPanel(c);

  const input = c.querySelector("#selection-target");
  input.value = "mar";
  await fire(input, "input");

  const pick = c.querySelector("#selection-target-list").querySelector(".reassign-pick");
  await fire(pick, "click");
  assert.equal(input.value, "Marie Duval", "clicking a pick fills the field with its main text");
});

test("Apply uses a clicked pick directly, rather than re-resolving the text", async () => {
  // A click already named the exact Value, so this is not a second search: it
  // is what makes the mode reachable at all when two Values share a prefix.
  resetState();
  addValues([{ category: "person_names", mainText: "Marie Duval" }]);
  await applySelection(stubContainer(), view({
    selection: { text: "M. Duval", x: 0, y: 0 },
    stage: "replace", mode: "spelling", target: "Marie Duval",
    picked: { category: "person_names", mainText: "Marie Duval" },
  }));
  const v = getState().values.find((x) => x.mainText === "Marie Duval");
  assert.ok(v.spellings.includes("M. Duval"), "the selected text became a spelling of the picked Value");
});

test("a stale pick from before a further keystroke is not reused", async () => {
  // selectionPicked must be cleared on every edit: otherwise a click, then more
  // typing that no longer matches, would still apply the old pick.
  resetState();
  addValues([{ category: "person_names", mainText: "Marie Duval" }]);
  await applySelection(stubContainer(), view({
    selection: { text: "Someone Else", x: 0, y: 0 },
    stage: "replace", mode: "spelling", target: "Nobody",
    picked: { category: "person_names", mainText: "Marie Duval" },
  }));
  assert.equal(getState().values[0].spellings.length, 0,
    "a picked Value whose text no longer matches the field must not be reused");
});

// This is the guard that kills the bug class where CATEGORIES (a list of
// [key, label] pairs) was iterated as if it were a list of keys: every
// option's value has to be a real engine category, and the one matching the
// current type has to carry `selected`, or the type list quietly writes a
// Value nothing can apply.
test("the new-Value type list offers real categories, with the current one selected", () => {
  const html = selectionPanel(compareState(), view({
    stage: "replace", mode: "value", category: "entity_names",
  }));
  const select = all(html, "select#selection-category")[0];
  const options = all(select.inner, "option");
  assert.ok(options.length > 0, "the type list is not empty");
  for (const o of options) {
    assert.ok(NAME_CATEGORIES.includes(o.attrs.value),
      `"${o.attrs.value}" is not a declarable category`);
  }
  const selected = options.filter((o) => "selected" in o.attrs);
  assert.equal(selected.length, 1, "exactly one option is pre-selected");
  assert.equal(selected[0].attrs.value, "entity_names");
});

test("the Add missed Value type list offers real categories, with the default one selected", () => {
  const html = missedCard(reportState());
  const select = all(html, "select#missed-category")[0];
  const options = all(select.inner, "option");
  assert.ok(options.length > 0, "the type list is not empty");
  for (const o of options) {
    assert.ok(NAME_CATEGORIES.includes(o.attrs.value),
      `"${o.attrs.value}" is not a declarable category`);
  }
  const selected = options.filter((o) => "selected" in o.attrs);
  assert.equal(selected.length, 1, "exactly one option is pre-selected");
});

test("stage 3 offers Apply and a Cancel that steps back rather than closing", () => {
  const html = selectionPanel(compareState(), view({ stage: "replace", mode: "value" }));
  assert.ok(exists(html, "button#btn-apply-selection"));
  assert.ok(exists(html, "button#btn-cancel-selection"));
});

test("a refusal is shown ON the panel, next to the field the fix goes into", () => {
  const html = selectionPanel(compareState(), view({
    stage: "replace", mode: "spelling", error: ANONYMISE.selectionUnknownTarget,
  }));
  assert.equal(textOf(html, "p.hint"), ANONYMISE.selectionUnknownTarget);
});

test("no selection renders no panel at all", () => {
  assert.equal(selectionPanel(compareState(), view({ selection: null })), "");
});

test("the panel shows the selected text, escaped", () => {
  const html = selectionPanel(compareState(), view({
    selection: { text: '<b>Alpine & Co</b>', x: 0, y: 0 },
  }));
  assert.equal(textOf(html, "span.selection-text"), "<b>Alpine & Co</b>");
  assert.ok(!html.includes("<b>Alpine"), "the selection is text, never markup");
});

// Each mode routes to a DIFFERENT reducer, which is the whole point: they
// differ in what ends up in the re-identification key. Asserted on the store
// rather than on the DOM. runFastRerun runs afterwards and rejects without a
// bridge, which is caught and shown in the error strip, so the store effect is
// what these read.

/** stubContainer() swallows the wiring lookups applySelection makes. */
function stubContainer() {
  return { querySelector: () => null, querySelectorAll: () => [] };
}

test("mode 2 adds a new value of its own, in the chosen type", async () => {
  resetState();
  await applySelection(stubContainer(), view({
    selection: { text: "Meridian", x: 0, y: 0 },
    stage: "replace", mode: "value", category: "entity_names",
  }));

  const v = getState().values.find((x) => x.mainText === "Meridian");
  assert.ok(v, "the selection became a Value");
  assert.equal(v.category, "entity_names");
  assert.deepEqual(v.discoveryMethods, ["manual"], "the user declared it");
  assert.equal(getState().settings.categories.entity_names, true,
    "adding a value switches its type on, or the pipeline drops it");
});

test("mode 2 folds into an existing family instead of creating a rival", async () => {
  // A new value that is a spelling of one already listed must not become a
  // rival: the shorter would fire inside the longer and leave the rest behind.
  resetState();
  addValues([{ category: "brand_names", mainText: "Coca-Cola" }]);
  await applySelection(stubContainer(), view({
    selection: { text: "Coca-Cola company", x: 0, y: 0 },
    stage: "replace", mode: "value", category: "brand_names",
  }));

  assert.equal(getState().values.length, 1, "one value, not two");
  assert.ok(getState().values[0].spellings.includes("Coca-Cola company"));
});

test("mode 2 is refused AT THE DECLARATION when the name is already allowlisted", async () => {
  // The conflict is met on the panel the user is typing into, not as a
  // refused run they would have to leave the screen to fix.
  resetState();
  addAllowTerm("Meridian");
  await applySelection(stubContainer(), view({
    selection: { text: "Meridian", x: 0, y: 0 },
    stage: "replace", mode: "value", category: "entity_names",
  }));
  assert.equal(getState().values.length, 0, "the declaration must not go through");
});

test("mode 2 is refused when the same name is already declared under another type", async () => {
  resetState();
  addValues([{ category: "person_names", mainText: "Meridian" }]);
  await applySelection(stubContainer(), view({
    selection: { text: "Meridian", x: 0, y: 0 },
    stage: "replace", mode: "value", category: "entity_names",
  }));
  assert.equal(getState().values.filter((v) => v.mainText === "Meridian").length, 1,
    "the second declaration under a rival type must not go through");
});

test("mode 1 refuses a target that is not a value, on the panel", async () => {
  resetState();
  await applySelection(stubContainer(), view({
    selection: { text: "Meridian", x: 0, y: 0 },
    stage: "replace", mode: "spelling", target: "Nobody",
  }));
  assert.equal(getState().values.length, 0,
    "a refused spelling target must not fall through to creating a Value");
});

test("the Compare panel exposes only the two Value actions", () => {
  // Everything the panel can do goes through the Value model. A literal
  // replacement path would rewrite text with nothing in the re-identification
  // key saying what happened, so there is deliberately no third mode and no
  // free-text replacement field anywhere in the view.
  const source = readFileSync(new URL("./views/anonymise.js", import.meta.url), "utf8");
  assert.ok(!source.includes("selection-draft"),
    "no free-text replacement field: a literal rewrite leaves no re-identification entry");
  assert.ok(!/addSimpleRule|nextRulePlaceholder/.test(source),
    "no find-and-replace path remains in the view");
});

// --- Add missed Value ------------------------------------------------------
//
// The card is a MANUAL VALUE DECLARATION followed by a fast deterministic rerun.
// It creates a normal Value: a category, a placeholder, an entry in the
// re-identification key, spellings and grouping like any other. It is not a
// literal rewrite with a different name, which is what the facility it replaced
// was, and the difference is the whole point.

test("Add missed Value declares a real Value, with its category switched on", () => {
  resetState();
  const before = getState().values.length;
  assert.equal(addValues([{ category: "person_names", mainText: "P. Stone" }]), 1);

  const value = getState().values.find((v) => v.mainText === "P. Stone");
  assert.ok(value, "the declaration became a Value");
  assert.equal(value.category, "person_names", "it carries the category the user chose");
  assert.deepEqual(value.discoveryMethods, ["manual"], "declared by the user");
  assert.equal(value.spellingPolicy, "automatic",
    "a fresh Value is uncurated, so Go still derives its spellings");
  assert.equal(getState().values.length, before + 1);
  // The category is switched on, or the pipeline would drop the Value the user
  // just declared and the fast rerun would appear to do nothing.
  assert.equal(getState().settings.categories.person_names, true);
});

test("Add missed Value reaches Go as a Value, so it earns a placeholder", () => {
  // A Value in the run request is what gets a placeholder and a mapping row. A
  // path that rewrote text without going through the request would leave the
  // re-identification key unable to say what happened.
  resetState();
  addValues([{ category: "person_names", mainText: "P. Stone" }]);
  const sent = buildRunRequest().values.find((v) => v.mainText === "P. Stone");
  assert.ok(sent, "the declared Value travels in the run request");
  assert.deepEqual(sent.discoveryMethods, ["manual"]);
  assert.equal(sent.spellingPolicy, "automatic");
});

test("a missed Value that belongs to an existing family folds into it", () => {
  // One Value, one placeholder: adding "Coca-Cola company" beside "Coca-Cola"
  // must not create a rival the shorter form fires inside.
  resetState();
  addValues([{ category: "brand_names", mainText: "Coca-Cola" }]);
  const family = foldIntoFamily("brand_names", "Coca-Cola company");
  assert.ok(family, "the longer form joins the existing family");
  assert.equal(family.main, "Coca-Cola", "the shorter form stays the main text");
  assert.equal(getState().values.length, 1, "one Value, not two");
  assert.ok(getState().values[0].spellings.includes("Coca-Cola company"));
});

// --- The Add missed Value card gets the same safeguards the step 2 add row has --

test("the Add missed Value card folds a longer form into an existing family, through the real wiring", async () => {
  resetState();
  addValues([{ category: "brand_names", mainText: "Coca-Cola" }]);
  setState(reportState({ values: getState().values }));

  const c = container();
  renderAnonymise(c);

  const category = c.querySelector("#missed-category");
  category.value = "brand_names";
  await fire(category, "change");

  const input = c.querySelector("#missed-value");
  input.value = "Coca-Cola company";
  await fire(input, "input");

  await fire(c.querySelector("#btn-add-missed"), "click");

  assert.equal(getState().values.length, 1, "one Value, not two, through the actual card wiring");
  assert.ok(getState().values[0].spellings.includes("Coca-Cola company"));
});

test("the Add missed Value card refuses a conflicting declaration, and keeps the draft text", async () => {
  resetState();
  addAllowTerm("Meridian");
  setState(reportState({ values: [] }));

  const c = container();
  renderAnonymise(c);

  const category = c.querySelector("#missed-category");
  category.value = "entity_names";
  await fire(category, "change");

  const input = c.querySelector("#missed-value");
  input.value = "Meridian";
  await fire(input, "input");

  await fire(c.querySelector("#btn-add-missed"), "click");

  assert.equal(getState().values.length, 0, "the conflicting declaration must not go through");
  assert.equal(input.value, "Meridian", "the draft text survives the refusal, so the fix is right there");
});

test("the missed-value match readout degrades to silence rather than a crash with no bridge", async () => {
  resetState();
  setState(reportState());

  const c = container();
  renderAnonymise(c);

  const input = c.querySelector("#missed-value");
  input.value = "Some Person";
  await fire(input, "input");

  // The real debounce delay: this test process has no Wails bridge, so
  // countTermMatches rejects and the read-out must stay empty rather than
  // show a stale or wrong count.
  await new Promise((resolve) => setTimeout(resolve, 300));
  assert.equal(c.querySelector("#missed-matches").textContent, "");
});

// --- A declared Value that matched nothing, or that could not apply, must say so --

test("missingDeclaredTexts finds a declared text absent from the refreshed table", () => {
  const replaced = [{ original: "Alpine Trust", placeholder: "[ENTITY_1]", category: "entity_names", count: 3 }];
  assert.deepEqual(missingDeclaredTexts(replaced, ["Nonexistent Corp"]), ["Nonexistent Corp"]);
});

test("missingDeclaredTexts matches case-insensitively and reports nothing when found", () => {
  const replaced = [{ original: "Alpine Trust", placeholder: "[ENTITY_1]", category: "entity_names", count: 1 }];
  assert.deepEqual(missingDeclaredTexts(replaced, ["alpine trust"]), []);
});

test("missingDeclaredTexts treats an empty expectation as nothing to check", () => {
  assert.deepEqual(missingDeclaredTexts([], []), []);
  assert.deepEqual(missingDeclaredTexts(undefined, undefined), []);
});

// --- Nothing that bypasses the Value model remains -------------------------

// --- A declared Value another route covers entirely is warned about ----------
//
// The full-coverage intersection, which CHANGE-06 kept because it usually means a
// mis-declaration: the Value is never replaced under its own type, so a user who
// declared it and then cannot find it in the report has no other explanation.
// This screen has no value card to paint it on, so the Report card's warning list
// is its home, and it is said ONCE.

/**
 * withBridge(app, fn) runs fn with a stubbed Wails bridge in place, the same
 * namespace api.js reads. Only the methods a fast re-run touches need stubbing;
 * anything else still rejects, which every caller in the view already handles.
 */
async function withBridge(app, fn) {
  const previous = globalThis.window;
  globalThis.window = { go: { backend: { App: app } } };
  try {
    return await fn();
  } finally {
    if (previous === undefined) delete globalThis.window;
    else globalThis.window = previous;
  }
}

/** rerunBridge(placeholders, intersections) answers the calls a declaring fast
 *  re-run makes: the re-run itself, the mapping, and the two registry tables. */
function rerunBridge(placeholders, intersections = []) {
  return {
    FastRerun: async () => ({
      documents: [{ name: "a.txt", anonymised: "[ENTITY_1].", byCategory: {} }],
      report: { level: "medium", totalReplacements: 1, byCategory: {}, values: [], documents: [] },
      validation: { blocking: [], warnings: [] },
    }),
    GetMapping: async () => ({ rows: [] }),
    ValuePlaceholders: async () => placeholders,
    ListRemovedValues: async () => [],
    CheckIntersections: async () => ({ intersections }),
  };
}

/** declaredState() is one finished run with a document to declare against. */
function declaredState() {
  resetState();
  setState({
    documents: [{ name: "a.txt", markdown: "Meridian", previewTruncated: false, isGrid: false }],
    running: false,
    results: {
      documents: [{ name: "a.txt", anonymised: "[ENTITY_1]", byCategory: {} }],
      report: {
        level: "medium", totalReplacements: 1, byCategory: {},
        values: [], documents: [{ name: "a.txt", values: [] }],
      },
      validation: { blocking: [], warnings: [] },
    },
  });
}

const COVERED = {
  value: "Meridian", category: "entity_names",
  winnerValue: "Meridian Consulting", winnerMatchClass: "built_in_pattern",
  matchedTexts: ["Meridian"],
};

/** coveredSentence() is the wording the card must show for COVERED. */
function coveredSentence() {
  return [
    WORKSPACE.intersectionAll(COVERED.value, COVERED.winnerValue,
      WORKSPACE.matchClassLabel.built_in_pattern, COVERED.matchedTexts),
    WORKSPACE.intersectionFix,
  ].join(" ");
}

/** cautionLines(s) is the Report card's warning strip, as rendered text. */
function cautionLines(s) {
  return all(reportCard(s), "div.caution")
    .map((c) => textOf(c.outer, "span.caution-text"));
}

test("a Value declared here that another route covers entirely is warned about, once", async () => {
  declaredState();
  await withBridge(
    rerunBridge([{ placeholder: "[ENTITY_1]", original: "Meridian", category: "entity_names" }], [COVERED]),
    async () => {
      await applySelection(stubContainer(), view({
        selection: { text: "Meridian", x: 0, y: 0 },
        stage: "replace", mode: "value", category: "entity_names",
      }));
    });

  assert.deepEqual(cautionLines(getState()), [coveredSentence()],
    "the sentence names the winning METHOD, never the internal rank");
});

test("the intersection warning dismisses like any other, by its text", async () => {
  declaredState();
  await withBridge(
    rerunBridge([{ placeholder: "[ENTITY_1]", original: "Meridian", category: "entity_names" }], [COVERED]),
    async () => {
      await applySelection(stubContainer(), view({
        selection: { text: "Meridian", x: 0, y: 0 },
        stage: "replace", mode: "value", category: "entity_names",
      }));
    });
  const dismissed = { ...getState(), dismissedWarnings: [coveredSentence()] };
  assert.deepEqual(cautionLines(dismissed), [],
    "one dismissal is enough, however often the Value is re-declared");
});

test("a Value no other route claims produces no intersection warning", async () => {
  declaredState();
  await withBridge(
    rerunBridge([{ placeholder: "[ENTITY_1]", original: "Borealis", category: "entity_names" }], []),
    async () => {
      await applySelection(stubContainer(), view({
        selection: { text: "Borealis", x: 0, y: 0 },
        stage: "replace", mode: "value", category: "entity_names",
      }));
    });
  assert.deepEqual(cautionLines(getState()), []);
});

test("an intersection over a Value this screen did not declare is left to step 2", async () => {
  // Only declarations made HERE are listed: a Value accepted on Identify already
  // met this warning on its own card, and repeating it here would be two
  // warnings for one fact.
  declaredState();
  setState({ intersections: [COVERED] });
  addValues([{ category: "entity_names", mainText: "Meridian" }]);
  assert.deepEqual(cautionLines(getState()), []);
});

test("no bridge leaves the warning list empty rather than throwing into the repaint", async () => {
  // The render harness and this suite both run without a bridge. An unhandled
  // rejection here would blank the screen.
  declaredState();
  await applySelection(stubContainer(), view({
    selection: { text: "Borealis", x: 0, y: 0 },
    stage: "replace", mode: "value", category: "entity_names",
  }));
  assert.deepEqual(cautionLines(getState()), []);
});

test("no Find and replace surface remains anywhere on Anonymise", () => {
  // Every retired affordance in one assertion, because the point is not that the
  // card is gone but that no path rewrites text without the re-identification key
  // recording it.
  const source = readFileSync(new URL("./views/anonymise.js", import.meta.url), "utf8");
  for (const retired of [
    "addSimpleRule", "removeSimpleRule", "moveSimpleRule", "simpleRules",
    "nextRulePlaceholder", "rulesCard", "rule-find", "rule-replace",
  ]) {
    assert.ok(!source.includes(retired), `views/anonymise.js still mentions ${retired}`);
  }
  const copySource = readFileSync(new URL("./copy.js", import.meta.url), "utf8");
  assert.ok(!/Find and replace/.test(copySource), "no copy names the retired facility");
});

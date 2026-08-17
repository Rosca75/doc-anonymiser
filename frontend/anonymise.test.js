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
  runCard, rulesCard, missedCard, renderAnonymise, searchWalk, searchControls,
} from "./views/anonymise.js";
import { resetState, setState } from "./state.js";
import { ANONYMISE } from "./copy.js";
import { textOf, all, attr } from "./testhtml.js";

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
    entities: [],
    simpleRules: [],
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
  // only turned into a variant of another value: the same registry entry sits
  // behind the mark and behind the Replaced values row, so both edit it.
  const html = selectedCard(reportState(), { placeholder: "[PERSON_1]", original: "Marie Duval" });
  assert.equal(all(html, "input#selected-ph-input").length, 1,
    "the replacement value has to be editable here too, not just in the table");
  assert.equal(all(html, "input#reassign-input").length, 1,
    "the 'make it a variant of' field stays: this adds an action, it does not remove one");
  assert.match(html, /Marie Duval/, "the card still names what the placeholder replaces");
});

test("the report note says the preset the run used, and nothing about an AI pass", () => {
  const s = reportState();
  const note = textOf(reportCard(s), "#report-run-note");
  assert.match(note, /medium/);
  assert.doesNotMatch(note, /deep scan|AI/i,
    "the deep-scan feature is gone, so the run note must not mention an AI pass");
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

/** blockedState() is a state whose last run was refused by a variant collision. */
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

// --- The run card: no deep-scan control, explanation on hover ------------
//
// The "Deep scan (AI)" checkbox was removed from step 3: the run offers only
// the deterministic passes now. Its explanatory subtitle moved onto the
// heading as a hover tooltip so the card stays a compact control strip.

test("the run card offers no deep-scan control", () => {
  assert.equal(all(runCard(reportState()), "input#deep-scan").length, 0,
    "the Deep scan (AI) checkbox is gone: the run runs the deterministic passes only");
  assert.doesNotMatch(runCard(reportState()), /Deep scan/i,
    "no leftover copy naming the removed feature");
});

test("the run card carries its explanation on the heading, not as a subtitle", () => {
  const html = runCard(reportState());
  assert.equal(all(html, "span.card-sub").length, 0,
    "the status line is a hover tooltip now, not a visible subtitle");
  // reportState() is a finished, unblocked run, so the tooltip is subtitleDone.
  assert.equal(attr(html, "h2", "title"), ANONYMISE.subtitleDone,
    "the heading spells out the run's state on hover");
});

// --- CR9: result sections start collapsed, rules gated on a run ----------
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

test("the Find and replace card is absent before the first run", () => {
  resetState();
  setState({ documents: [{ name: "a.txt", markdown: "x", previewTruncated: false, isGrid: false }] });
  const html = renderColumn();
  assert.doesNotMatch(html, /Find and replace/,
    "with no run yet there is nothing to find-and-replace against");
});

test("after a run all four result cards render, and each starts collapsed", () => {
  resetState();
  setState({ documents: [{ name: "a.txt", markdown: "x", previewTruncated: false, isGrid: false }] });
  seedRun();
  const html = renderColumn();
  assert.match(html, /Find and replace/, "the rules card appears once a run has happened");
  // The four foldable result cards (values, report, missed, rules) all fold shut.
  assert.equal((html.match(/data-open="false"/g) ?? []).length, 4,
    "values, report, something-missed and find-and-replace all start collapsed");
  assert.equal((html.match(/data-open="true"/g) ?? []).length, 0,
    "nothing in the result column starts open");
});

test("each result card, rendered on its own, starts collapsed", () => {
  // The collapsed set is the single source of truth for all four, so checking
  // each helper directly guards the set membership CR9 relies on.
  const s = reportState();
  for (const [name, fn] of [["values", valuesCard], ["report", reportCard], ["missed", missedCard], ["rules", rulesCard]]) {
    assert.equal(attr(fn(s), ".cgroup", "data-open"), "false",
      `${name} card must start collapsed`);
  }
});

// --- CR3: the Compare search --------------------------------------------
//
// The walk is ONE ordered list, every original-pane hit then every
// anonymised-pane hit, with the active hit's PANE named in the readout. Two
// cursors, or one index over lists of different lengths, would drift; naming
// the pane makes crossing the boundary visible rather than silent.

/** searchState(needle, index) is the view-local search as the card reads it. */
function searchState(needle, index = 0) {
  return { needle, index };
}

/** walkFor(needle, index) is the walk over the standard compare fixture. */
function walkFor(needle, index = 0) {
  const s = compareState();
  const doc = s.results.documents[0];
  const source = { found: true, markdown: SOURCE, truncated: false };
  return searchWalk(s, doc, source, searchState(needle, index));
}

test("the walk finds hits in both panes and counts them together", () => {
  // "e" appears in both, but a 1-character needle is refused, so use a word
  // that is genuinely in each: "M" is in "Marie"/"Meridian", "ME" in neither
  // pane's placeholder. "et" occurs in "met" in both panes.
  const walk = walkFor("met");
  assert.equal(walk.original.length, 1, "ORIGINAL says 'met'");
  assert.equal(walk.anonymised.length, 1, "so does ANONYMISED");
  assert.equal(walk.total, 2);
});

test("the walk crosses from the original pane into the anonymised one", () => {
  const first = walkFor("met", 0);
  assert.equal(first.pane, "original");
  assert.equal(first.activeIn("original"), 0);
  assert.equal(first.activeIn("anonymised"), -1, "only one pane holds the active hit");

  const second = walkFor("met", 1);
  assert.equal(second.pane, "anonymised");
  assert.equal(second.activeIn("original"), -1);
  assert.equal(second.activeIn("anonymised"), 0);
});

test("the walk wraps at both ends", () => {
  assert.equal(walkFor("met", 2).index, 0, "past the last hit wraps to the first");
  assert.equal(walkFor("met", -1).index, 1, "before the first wraps to the last");
});

test("a needle that matches nothing leaves the walk empty", () => {
  const walk = walkFor("Borealis");
  assert.equal(walk.total, 0);
  assert.equal(walk.activeIn("original"), -1);
  assert.equal(walk.activeIn("anonymised"), -1);
});

test("the readout names the count, the total and the active hit's pane", () => {
  const html = searchControls(walkFor("met", 1), searchState("met", 1));
  assert.equal(textOf(html, "span.search-readout"),
    ANONYMISE.searchCount(2, 2, ANONYMISE.searchPaneAnonymised));
});

test("the search box and both navigation buttons render", () => {
  const html = searchControls(walkFor("met"), searchState("met"));
  assert.equal(attr(html, "input#compare-search", "value"), "met");
  assert.ok(all(html, "button.search-prev").length === 1);
  assert.ok(all(html, "button.search-next").length === 1);
});

test("with no hits both buttons are disabled AND say why", () => {
  // A greyed control that says nothing is a dead end.
  const html = searchControls(walkFor("Borealis"), searchState("Borealis"));
  for (const cls of ["search-prev", "search-next"]) {
    assert.equal(attr(html, `button.${cls}`, "disabled"), "");
    assert.equal(attr(html, `button.${cls}`, "title"), ANONYMISE.searchNone);
  }
  assert.equal(textOf(html, "span.search-readout"), ANONYMISE.searchNone);
});

test("an empty needle disables the buttons without claiming there is no match", () => {
  const html = searchControls(walkFor(""), searchState(""));
  assert.equal(attr(html, "button.search-next", "disabled"), "");
  assert.equal(textOf(html, "span.search-readout"), "",
    "before anything is typed there is nothing to report");
});

test("compareCard highlights the active hit in the pane that holds it", () => {
  // Rendered through the real card, so the panes and the readout are proven to
  // agree about which hit is active.
  const s = compareState();
  const html = compareCard(s, s.results.documents[0]);
  // The module-level needle is empty in a fresh test process, so no hit spans.
  assert.equal(all(html, "span.find-hit").length, 0,
    "with no needle the panes render exactly as they did before");
});

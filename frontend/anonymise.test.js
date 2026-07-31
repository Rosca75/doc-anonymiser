// anonymise.test.js, tests for the Anonymise screen's pure helpers
// (BUILD-05 Phase 7).
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
  countOccurrences, valuesInCategory, formatDuration, nextCustomNumber, continueHint,
} from "./views/anonymise.js";

// --- countOccurrences ---------------------------------------------------

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

// --- valuesInCategory ---------------------------------------------------

const MAPPING = {
  "[PERSON_1]": { original: "Marie Duval", category: "person_names" },
  "[PERSON_2]": { original: "Thomas Berger", category: "person_names" },
  "[CLIENT_1]": { original: "Meridian Consulting", category: "client_names" },
};

const DOCS = [
  { name: "a.docx", anonymised: "[PERSON_1] met [PERSON_1] and [CLIENT_1]." },
  { name: "b.pptx", anonymised: "[PERSON_2] chaired. [PERSON_1] apologised." },
];

test("valuesInCategory counts occurrences across the documents in scope", () => {
  const rows = valuesInCategory({ mapping: MAPPING }, DOCS, "person_names");
  assert.deepEqual(rows.map((r) => [r.placeholder, r.occurrences]),
    [["[PERSON_1]", 3], ["[PERSON_2]", 1]]);
  assert.equal(rows[0].original, "Marie Duval");
});

test("valuesInCategory honours the scope: one document counts only its own", () => {
  // This is why the counts are computed here rather than read off the registry,
  // which counts per SESSION and cannot answer a per-document question.
  const rows = valuesInCategory({ mapping: MAPPING }, [DOCS[1]], "person_names");
  assert.deepEqual(rows.map((r) => [r.placeholder, r.occurrences]),
    [["[PERSON_1]", 1], ["[PERSON_2]", 1]]);
});

test("valuesInCategory omits values that do not appear in scope at all", () => {
  // A value replaced in another document must not appear with a count of 0: the
  // drill-down is about what is in the files the user selected.
  const rows = valuesInCategory({ mapping: MAPPING }, [DOCS[1]], "client_names");
  assert.deepEqual(rows, [], "[CLIENT_1] is only in a.docx");
});

test("valuesInCategory sorts by occurrences, then by placeholder for a tie", () => {
  const mapping = {
    "[PERSON_2]": { original: "B", category: "person_names" },
    "[PERSON_1]": { original: "A", category: "person_names" },
  };
  const docs = [{ anonymised: "[PERSON_1] [PERSON_2]" }];
  const rows = valuesInCategory({ mapping }, docs, "person_names");
  // Equal counts, so the tie-break decides, and it has to be stable or the rows
  // would swap places between renders.
  assert.deepEqual(rows.map((r) => r.placeholder), ["[PERSON_1]", "[PERSON_2]"]);
});

test("valuesInCategory copes with no mapping at all", () => {
  assert.deepEqual(valuesInCategory({}, DOCS, "person_names"), []);
  assert.deepEqual(valuesInCategory({ mapping: null }, DOCS, "person_names"), []);
});

// --- formatDuration -----------------------------------------------------

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

// --- nextCustomNumber ---------------------------------------------------

test("nextCustomNumber continues from the existing custom rules", () => {
  // Two selections in a row must not both propose [CUSTOM_1].
  assert.equal(nextCustomNumber({ simpleRules: [] }), 1);
  assert.equal(nextCustomNumber({ simpleRules: [{ replace: "[CUSTOM_1]" }] }), 2);
  assert.equal(nextCustomNumber({
    simpleRules: [{ replace: "[CUSTOM_1]" }, { replace: "[CUSTOM_4]" }],
  }), 5, "it continues past the HIGHEST, so a gap does not cause a collision");
});

test("nextCustomNumber ignores rules that are not custom placeholders", () => {
  assert.equal(nextCustomNumber({
    simpleRules: [{ replace: "redacted" }, { replace: "[PERSON_9]" }, { replace: "" }],
  }), 1);
  assert.equal(nextCustomNumber({}), 1);
});

// --- continueHint -------------------------------------------------------

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

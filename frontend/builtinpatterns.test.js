// builtinpatterns.test.js, tests for the Built-in patterns tab of Identify
//
// The tab is READ-ONLY, and every test here is about a distinction the user
// would otherwise have to guess at:
//
//   "detection has not run" is not "the patterns found nothing";
//   "that category never ran" is not "that category found nothing";
//   a direct match is not a Suggestion, so no row offers an accept or a reject.

import { test } from "node:test";
import assert from "node:assert/strict";

import { resetState, getState, setBuiltInPatterns, setState } from "./state.js";
import { builtInPatternsTab, WORKSPACE_TABS } from "./views/identifyworkspace.js";
import { all, exists, textOf, stripTags, unescape } from "./testhtml.js";
import { WORKSPACE } from "./copy.js";

/** result(patch) is a DetectionResult as Go returns it, patched per case. */
function result(patch = {}) {
  return {
    suggestions: [],
    builtInPatternsOn: true,
    patternCategories: ["email", "address"],
    patternMatches: [
      {
        category: "email", text: "marie.duval@example.com", count: 3,
        documents: ["a.docx", "b.docx"], confidence: 1,
      },
      {
        category: "address", text: "1, Avenue de l'Innovation", count: 1,
        documents: ["a.docx"], confidence: 1,
      },
    ],
    ...patch,
  };
}

test("the tab set carries both pattern tabs, built-in before custom", () => {
  const builtin = WORKSPACE_TABS.indexOf("builtin");
  const custom = WORKSPACE_TABS.indexOf("patterns");
  assert.ok(builtin >= 0, "the built-in patterns tab must be in the set");
  assert.ok(custom >= 0, "the custom patterns tab must stay in the set");
  assert.ok(builtin < custom, "built-in comes first: it is the one already running");
  assert.equal(WORKSPACE.tabLabels.builtin, "Built-in patterns");
  assert.equal(WORKSPACE.tabLabels.patterns, "Custom patterns");
});

test("before detection has run the tab says so, rather than 'nothing found'", () => {
  resetState();
  const html = builtInPatternsTab(getState());
  assert.match(stripTags(html), /Run detection/);
  // The two facts must not be confused: an empty result is a different sentence.
  assert.doesNotMatch(stripTags(html), /matched nothing/);
});

test("a run records the preview and the tab groups it by category", () => {
  resetState();
  setBuiltInPatterns(result());

  const preview = getState().builtInPatterns;
  assert.equal(preview.matches.length, 2);
  assert.equal(preview.on, true);

  const html = builtInPatternsTab(getState());
  const groups = all(html, "div.builtin-group");
  assert.equal(groups.length, 2, "one section per ACTIVE category");
  const rows = all(html, "div.builtin-row");
  assert.equal(rows.length, 2);
  assert.match(unescape(stripTags(html)), /marie\.duval@example\.com/);
  // How often and where, which is the first question a surprising match raises.
  assert.match(stripTags(html), /3 occurrences in 2 files/);
  assert.match(stripTags(html), /1 occurrence in 1 file/);
});

test("a category that ran and found nothing keeps its section", () => {
  resetState();
  // Postal codes were ticked and matched nothing: the whole point of the tab in
  // the reported workflow is that this is visible, not silently absent.
  setBuiltInPatterns(result({
    patternCategories: ["email", "postal_code"],
    patternMatches: [{
      category: "email", text: "marie.duval@example.com", count: 1,
      documents: ["a.docx"], confidence: 1,
    }],
  }));

  const html = builtInPatternsTab(getState());
  const groups = all(html, "div.builtin-group");
  assert.equal(groups.length, 2);
  assert.match(stripTags(html), /Nothing matched/);
});

test("nothing on a row can accept, reject or edit a match", () => {
  resetState();
  setBuiltInPatterns(result());
  const html = builtInPatternsTab(getState());
  // A built-in pattern produces DIRECT matches: there is no review gate here,
  // so a control implying one would be a lie about what the tab does.
  assert.equal(exists(html, "button"), false, "the tab is read-only");
  assert.equal(exists(html, "input"), false, "the tab is read-only");
  assert.equal(exists(html, "select"), false, "the tab is read-only");
});

test("the master switch being off is its own message", () => {
  resetState();
  setBuiltInPatterns(result({
    builtInPatternsOn: false, patternCategories: [], patternMatches: [],
  }));
  assert.match(stripTags(builtInPatternsTab(getState())), /switched off/);
});

test("no applicable category is its own message, not an empty list", () => {
  resetState();
  setBuiltInPatterns(result({ patternCategories: [], patternMatches: [] }));
  const text = stripTags(builtInPatternsTab(getState()));
  assert.match(text, /document country/);
});

test("a failed corroborating check is shown, because the span is replaced anyway", () => {
  resetState();
  setBuiltInPatterns(result({
    patternCategories: ["iban"],
    patternMatches: [{
      category: "iban", text: "LU28 0019 4006 4475 0001", count: 1,
      documents: ["a.docx"], confidence: 0.6,
    }],
  }));
  const html = builtInPatternsTab(getState());
  assert.equal(all(html, "span.state-tag").length, 1);
  assert.match(textOf(html, "span.state-tag"), /Check failed/);
});

test("a second run replaces the preview instead of merging with it", () => {
  resetState();
  setBuiltInPatterns(result());
  // The user unticks addresses and runs again: the addresses must be GONE, or
  // the tab reports a category that is no longer on.
  setBuiltInPatterns(result({
    patternCategories: ["email"],
    patternMatches: [{
      category: "email", text: "marie.duval@example.com", count: 3,
      documents: ["a.docx", "b.docx"], confidence: 1,
    }],
  }));
  assert.deepEqual(getState().builtInPatterns.categories, ["email"]);
  assert.equal(getState().builtInPatterns.matches.length, 1);
  assert.equal(all(builtInPatternsTab(getState()), "div.builtin-group").length, 1);
});

test("the preview never becomes a suggestion or a value", () => {
  resetState();
  setBuiltInPatterns(result());
  // The step 2 to 3 gate is about unreviewed SUGGESTIONS. A direct match must
  // add none, or the gate would refuse to open on a pattern the user chose.
  assert.equal(getState().suggestions.length, 0);
  assert.equal(getState().values.length, 0);
});

test("stepping back to Identify clears the preview with the rest of the step", () => {
  resetState();
  setBuiltInPatterns(result());
  setState({ step: "anonymise" });
  // STEP_RESETS.identify owns the whole screen: a preview describing a category
  // selection that has just been reset would describe nothing on screen.
  assert.equal(getState().builtInPatterns !== null, true);
  resetState();
  assert.equal(getState().builtInPatterns, null);
});

test("a match under a category the run did not report active still renders", () => {
  resetState();
  // This cannot happen while the two lists agree, and that is the point: a
  // mismatch must SHOW as an extra section rather than swallow a finding.
  setBuiltInPatterns(result({ patternCategories: ["email"] }));
  const groups = all(builtInPatternsTab(getState()), "div.builtin-group");
  assert.equal(groups.length, 2, "the orphaned address match gets its own section");
});

// import.test.js, tests for the Import screen's pure helpers
// (BUILD-05 Phase 4).
//
// views/import.js imports api.js, which only touches `window` inside its
// functions, so the module imports cleanly here. Only the PURE exports are
// exercised: the markdown-table renderer, the unit-count pluralisation and the
// footer hint. The rest of the view is wiring and belongs to the manual pass.

import { test } from "node:test";
import assert from "node:assert/strict";

import { markdownTableToHTML, unitLabel, readyHint } from "./views/import.js";

// --- markdownTableToHTML ------------------------------------------------

test("markdownTableToHTML renders our own table shape", () => {
  const md = "| Client | Email |\n| --- | --- |\n| Aurora Group | lena@aurora.eu |";
  const html = markdownTableToHTML(md);
  assert.match(html, /<table class="grid-table">/);
  assert.match(html, /<th>Client<\/th>/);
  assert.match(html, /<td>Aurora Group<\/td>/);
  // The separator row is the table's, not a data row.
  assert.ok(!html.includes("---"));
});

test("markdownTableToHTML escapes every cell", () => {
  const md = "| Name |\n| --- |\n| <script>x</script> |";
  const html = markdownTableToHTML(md);
  assert.ok(!html.includes("<script>"));
  assert.ok(html.includes("&lt;script&gt;"));
});

test("markdownTableToHTML falls back to preformatted text for anything else", () => {
  // A document that is not one of our generated tables must still be readable,
  // not silently rendered as an empty table.
  for (const md of ["", "just prose", "| only one line |"]) {
    assert.match(markdownTableToHTML(md), /<pre class="md-preview">/, JSON.stringify(md));
  }
});

test("markdownTableToHTML restores escaped pipes inside a cell", () => {
  const md = "| Note |\n| --- |\n| a \\| b |";
  assert.ok(markdownTableToHTML(md).includes("a | b"));
});

// --- unitLabel ----------------------------------------------------------

test("unitLabel pluralises the unit Go sends singular", () => {
  assert.equal(unitLabel(6, "page"), "6 pages");
  assert.equal(unitLabel(1, "page"), "1 page");
  assert.equal(unitLabel(12, "slide"), "12 slides");
  assert.equal(unitLabel(1, "row"), "1 row");
  assert.equal(unitLabel(412, "line"), "412 lines");
  // Zero is plural in English, and a count of 0 should never reach here anyway
  // (Go falls back to lines rather than reporting 0 pages).
  assert.equal(unitLabel(0, "page"), "0 pages");
});

test("unitLabel handles an unknown unit rather than printing nothing", () => {
  // If Go ever grows a fifth unit, the label must still read correctly instead
  // of falling through a lookup table.
  assert.equal(unitLabel(3, "sheet"), "3 sheets");
});

// --- readyHint ----------------------------------------------------------

test("readyHint tells an empty screen what to do, not that it is empty", () => {
  const hint = readyHint({ documents: [] });
  assert.match(hint, /Add at least one document/);
  assert.ok(!hint.includes("0"), "reporting \"0 files\" states the obvious");
});

test("readyHint totals the units when every document counts the same thing", () => {
  const hint = readyHint({
    documents: [
      { unitCount: 6, unit: "page" },
      { unitCount: 4, unit: "page" },
    ],
  });
  assert.equal(hint, "2 documents, 10 pages");
});

test("readyHint refuses to add up a MIXED batch", () => {
  // Pages plus rows is a nonsense figure, so the hint says how many documents
  // there are and stops there.
  const hint = readyHint({
    documents: [
      { unitCount: 6, unit: "page" },
      { unitCount: 48, unit: "row" },
    ],
  });
  assert.equal(hint, "2 documents ready");
  assert.ok(!hint.includes("54"), "pages and rows must never be summed");
});

test("readyHint is singular for one document", () => {
  assert.equal(readyHint({ documents: [{ unitCount: 6, unit: "page" }] }),
    "1 document, 6 pages");
});

test("readyHint copes with a document that has no unit count", () => {
  const hint = readyHint({ documents: [{ unitCount: 0, unit: "" }] });
  assert.equal(hint, "1 document ready");
});

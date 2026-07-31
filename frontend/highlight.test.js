// highlight.test.js — Phase 8 JS test: the highlight-rendering function
// (placeholder → <mark> HTML, with escaping).

import test from "node:test";
import assert from "node:assert/strict";

import { renderHighlighted, markClass } from "./highlight.js";
import { tooltipMeta } from "./views/anonymise.js";

test("placeholders become category-coloured marks", () => {
  const html = renderHighlighted("mail [EMAIL_1] met [PERSON_2] on [CUSTOM_1]");
  assert.ok(html.includes('<mark class="pii" title="email">[EMAIL_1]</mark>'));
  assert.ok(html.includes('<mark class="entity" title="person">[PERSON_2]</mark>'));
  assert.ok(html.includes('<mark class="custom" title="custom">[CUSTOM_1]</mark>'));
});

test("surrounding document text is HTML-escaped", () => {
  const html = renderHighlighted(`<script>alert("x")</script> [EMAIL_1] & more`);
  assert.ok(!html.includes("<script>"), "raw tags must not survive");
  assert.ok(html.includes("&lt;script&gt;"));
  assert.ok(html.includes("&amp; more"));
  assert.ok(html.includes("[EMAIL_1]</mark>"));
});

test("text without placeholders is escaped verbatim", () => {
  assert.equal(renderHighlighted("a < b"), "a &lt; b");
});

test("bracket text that is not a placeholder is left unmarked", () => {
  const html = renderHighlighted("[not a placeholder] and [lower_1]");
  assert.ok(!html.includes("<mark"), html);
});

test("markClass covers the three families", () => {
  assert.equal(markClass("IBAN"), "pii");
  assert.equal(markClass("ENTITY"), "entity");
  assert.equal(markClass("CUSTOM"), "custom");
  assert.equal(markClass("FUTURE_LABEL"), "custom");
});

// --- BUILD-02 Phase 10b: mapping-aware marks ---------------------------------

test("mapping adds data attributes and the original in the title", () => {
  const html = renderHighlighted("see [ENTITY_1] here",
    { "[ENTITY_1]": { original: "Acme S.A.", category: "entity_names" } });
  assert.ok(html.includes('data-ph="[ENTITY_1]"'));
  assert.ok(html.includes('data-original="Acme S.A."'));
  assert.ok(html.includes('title="Original: Acme S.A."'));
});

test("mapping miss falls back to the label-only title", () => {
  const html = renderHighlighted("see [ENTITY_9] here", { "[ENTITY_1]": { original: "x" } });
  assert.ok(html.includes('title="entity"'));
  assert.ok(!html.includes("data-ph"));
});

test("hostile originals are inert in the output", () => {
  const html = renderHighlighted("x [PERSON_1] y",
    { "[PERSON_1]": { original: `"><script>alert(1)</script>`, category: "person_names" } });
  assert.ok(!html.includes("<script>"));
  assert.ok(html.includes("&quot;&gt;&lt;script&gt;"));
});

// --- BUILD-06: the hover tooltip has to be reachable --------------------

test("a known mark is focusable, so the tooltip is not mouse-only", () => {
  const html = renderHighlighted("see [ENTITY_1] here",
    { "[ENTITY_1]": { original: "Acme S.A.", category: "entity_names" } });
  assert.match(html, /tabindex="0"/);
  assert.match(html, /data-category="entity_names"/);
});

test("a mark with no known original stays unfocusable: there is nothing to show", () => {
  const html = renderHighlighted("see [ENTITY_9] here", {});
  assert.ok(!html.includes("tabindex"), html);
});

test("the tooltip's second line names the category and the count", () => {
  assert.equal(tooltipMeta("person_names", 3), "Person names, replaced 3 times in this document");
  assert.equal(tooltipMeta("person_names", 1), "Person names, replaced 1 time in this document");
  // No count yet (a document with no occurrences in view): no dangling comma.
  assert.equal(tooltipMeta("person_names", 0), "Person names");
  // An unknown category degrades to its identifier rather than to "undefined".
  assert.equal(tooltipMeta("mystery_names", 0), "mystery_names");
});

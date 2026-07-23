// highlight.test.js — Phase 8 JS test: the highlight-rendering function
// (placeholder → <mark> HTML, with escaping).

import test from "node:test";
import assert from "node:assert/strict";

import { renderHighlighted, markClass } from "./highlight.js";

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
  assert.equal(markClass("CLIENT"), "entity");
  assert.equal(markClass("CUSTOM"), "custom");
  assert.equal(markClass("FUTURE_LABEL"), "custom");
});

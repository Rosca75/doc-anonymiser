// highlight.test.js — tests the highlight-rendering function
// (placeholder → <mark> HTML, with escaping).

import test from "node:test";
import assert from "node:assert/strict";

import { renderHighlighted, markClass } from "./highlight.js";
import { findHits } from "./panesearch.js";
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

test("every entity placeholder label gets the entity tint", () => {
  // An unknown label falls through to "custom", so a label left out of
  // ENTITY_LABELS renders in the wrong tint with nothing failing. The list here
  // is the placeholderLabels table in backend/engine/registry.go, entity half.
  for (const label of ["ENTITY", "PROJECT", "PRODUCT", "BRAND", "PERSON", "ID", "OTHER"]) {
    assert.equal(markClass(label), "entity", `${label} must read as an entity`);
  }
});

// --- mapping-aware marks -------------------------------------------------

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

// --- per-occurrence variant spelling -------------------------------------

test("a variant occurrence carries the spelling it replaced and the value in brackets", () => {
  const html = renderHighlighted("[PERSON_1] and [PERSON_1]",
    { "[PERSON_1]": { original: "Johannes Borch", category: "person_names" } },
    { "[PERSON_1]": ["", "Borch"] });
  // First occurrence matched the canonical value: no data-variant, plain title.
  // Second replaced "Borch": data-variant present, title shows both.
  assert.match(html, /data-variant="Borch"/);
  assert.match(html, /title="Original: Borch \(Johannes Borch\)"/);
  // The canonical occurrence stays a plain value with no bracketed original.
  assert.match(html, /title="Original: Johannes Borch"/);
});

test("a variant equal to the value case aside adds no brackets", () => {
  const html = renderHighlighted("[PERSON_1]",
    { "[PERSON_1]": { original: "Johannes Borch", category: "person_names" } },
    { "[PERSON_1]": ["johannes borch"] });
  assert.ok(!html.includes("data-variant"));
  assert.match(html, /title="Original: Johannes Borch"/);
});

test("variants absent (the common case) render exactly as before", () => {
  const html = renderHighlighted("see [ENTITY_1] here",
    { "[ENTITY_1]": { original: "Acme S.A.", category: "entity_names" } });
  assert.ok(!html.includes("data-variant"));
  assert.ok(html.includes('title="Original: Acme S.A."'));
});



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

// --- The Compare search's hits inside the anonymised pane ----------------

test("a search hit in the plain text is wrapped without disturbing the marks", () => {
  const text = "Alpine wrote to [EMAIL_1] about Alpine.";
  const mapping = { "[EMAIL_1]": { original: "a@b.com", category: "email" } };
  const html = renderHighlighted(text, mapping, null, {
    hits: findHits(text, "Alpine"), activeIndex: 0,
  });

  assert.equal((html.match(/class="find-hit/g) ?? []).length, 2);
  assert.equal((html.match(/find-hit active/g) ?? []).length, 1);
  assert.ok(html.includes('data-original="a@b.com"'), "the mark is untouched");
});

test("a search hit inside a mark's own text is wrapped", () => {
  // Searching for a placeholder is how a user checks where one value landed,
  // so the hit has to appear inside the mark, not be skipped.
  const text = "see [EMAIL_1] here";
  const mapping = { "[EMAIL_1]": { original: "a@b.com", category: "email" } };
  const html = renderHighlighted(text, mapping, null, {
    hits: findHits(text, "EMAIL_1"), activeIndex: 0,
  });

  assert.match(html, /<mark[^>]*>\[<span class="find-hit active">EMAIL_1<\/span>\]<\/mark>/);
});

test("a hit straddling a mark boundary is not highlighted, and the mark survives", () => {
  // Splitting the mark would break the click-to-select and the tooltip, which
  // are how the anonymisation is actually checked. A needle spanning the edge
  // of a placeholder is rare; a broken mark is not recoverable.
  const text = "see [EMAIL_1] here";
  const mapping = { "[EMAIL_1]": { original: "a@b.com", category: "email" } };
  const html = renderHighlighted(text, mapping, null, {
    hits: findHits(text, "e [EMAIL"), activeIndex: 0,
  });

  assert.ok(!html.includes("find-hit"), "the straddling hit is left alone");
  assert.ok(html.includes('data-ph="[EMAIL_1]"'), "the mark keeps its attributes");
  assert.ok(html.includes('data-original="a@b.com"'));
  assert.ok(html.includes('tabindex="0"'), "and stays reachable from the keyboard");
});

test("rendering without a search is byte-identical to rendering with none", () => {
  // Every existing caller passes three arguments. The fourth must be free.
  const text = "Alpine wrote to [EMAIL_1] and [PERSON_2].";
  const mapping = {
    "[EMAIL_1]": { original: "a@b.com", category: "email" },
    "[PERSON_2]": { original: "Marie Duval", category: "person_names" },
  };
  assert.equal(renderHighlighted(text, mapping),
    renderHighlighted(text, mapping, null, { hits: [], activeIndex: -1 }));
  assert.equal(renderHighlighted(text, mapping),
    renderHighlighted(text, mapping, null, undefined));
});

test("a hit in an unmapped mark's text is wrapped too", () => {
  // The mapping-miss branch renders a different mark, and it walks the same
  // text, so it must treat hits the same way.
  const text = "see [GHOST_9] here";
  const html = renderHighlighted(text, {}, null, {
    hits: findHits(text, "GHOST"), activeIndex: 0,
  });
  assert.ok(html.includes('class="find-hit active">GHOST</span>'));
});

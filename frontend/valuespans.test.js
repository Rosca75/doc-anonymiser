// valuespans.test.js, tests for the Compare panes' origin spans.
//
// The feature this covers is the hover link between the two panes: pointing at
// "[PERSON_1]" tints, in the ORIGINAL pane, every stretch that placeholder
// replaced. So the tests are about which stretches those are, and they pin the
// three things that make the tint trustworthy rather than decorative: it covers
// the mainText value AND its spellings, it never claims a fragment of a longer
// word, and it never claims a character twice.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  replacedTexts, valueSpans, renderOriginWithSpans, MAX_SPANS,
} from "./valuespans.js";
import { findHits } from "./panesearch.js";
import { all, textOf } from "./testhtml.js";

const MAPPING = {
  "[PERSON_1]": { original: "Johannes Borch", category: "person_names" },
  "[ENTITY_1]": { original: "Delta", category: "entity_names" },
};

// --- replacedTexts -------------------------------------------------------

test("replacedTexts is the mainText value plus the spellings the run recorded", () => {
  const spellings = { "[PERSON_1]": ["Borch", "J. Borch", ""] };
  assert.deepEqual(replacedTexts("[PERSON_1]", MAPPING, spellings),
    ["Johannes Borch", "Borch", "J. Borch"],
    "the mainText leads, the empty slots (mainText matches) add nothing");
});

test("replacedTexts folds repeats and case, because the matching is case-insensitive", () => {
  const spellings = { "[PERSON_1]": ["Borch", "borch", "BORCH", "Johannes Borch"] };
  assert.deepEqual(replacedTexts("[PERSON_1]", MAPPING, spellings),
    ["Johannes Borch", "Borch"]);
});

test("replacedTexts is empty for a placeholder the mapping does not know", () => {
  assert.deepEqual(replacedTexts("[PERSON_9]", MAPPING, {}), []);
  assert.deepEqual(replacedTexts("[PERSON_1]", undefined, undefined), []);
});

// --- valueSpans ----------------------------------------------------------

test("valueSpans finds the mainText value AND each spelling", () => {
  const text = "Johannes Borch signed. Borch countersigned.";
  const spans = valueSpans(text, MAPPING, { "[PERSON_1]": ["Borch"] });
  assert.deepEqual(spans.map((s) => [text.slice(s.start, s.end), s.ph]), [
    ["Johannes Borch", "[PERSON_1]"],
    ["Borch", "[PERSON_1]"],
  ]);
});

test("valueSpans never claims a fragment of a longer word", () => {
  // The engine's value pass matches on word boundaries, so a tint that lit up
  // "Delta" inside "Deltaco" would claim a replacement the run never made.
  const text = "Delta and Deltaco and delta-force";
  const spans = valueSpans(text, MAPPING, {});
  assert.deepEqual(spans.map((s) => s.start), [0, 22],
    "the standalone Delta and the hyphenated one, never the one inside Deltaco");
});

test("valueSpans gives an overlapped stretch to the LONGEST claim", () => {
  // "Delta" is inside "Delta Industries", and one character can only be tinted
  // once. The longer value wins, which is the tie-break the engine's own
  // overlap resolution starts from.
  const text = "Delta Industries invoiced Delta.";
  const mapping = {
    "[ENTITY_1]": { original: "Delta", category: "entity_names" },
    "[ENTITY_2]": { original: "Delta Industries", category: "entity_names" },
  };
  const spans = valueSpans(text, mapping, {});
  assert.deepEqual(spans.map((s) => [text.slice(s.start, s.end), s.ph]), [
    ["Delta Industries", "[ENTITY_2]"],
    ["Delta", "[ENTITY_1]"],
  ]);
});

test("valueSpans is in document order, so the renderer can walk it once", () => {
  const text = "Borch met Delta, then Johannes Borch met Delta again.";
  const spans = valueSpans(text, MAPPING, { "[PERSON_1]": ["Borch"] });
  const starts = spans.map((s) => s.start);
  assert.deepEqual(starts, [...starts].sort((a, b) => a - b));
});

test("valueSpans matches the document's own casing and accents", () => {
  const text = "JOHANNES BORCH and Jérôme Petit";
  const mapping = { ...MAPPING, "[PERSON_2]": { original: "Jérôme Petit" } };
  const spans = valueSpans(text, mapping, {});
  assert.deepEqual(spans.map((s) => text.slice(s.start, s.end)),
    ["JOHANNES BORCH", "Jérôme Petit"]);
});

test("valueSpans returns nothing without a text or a mapping", () => {
  assert.deepEqual(valueSpans("", MAPPING, {}), []);
  assert.deepEqual(valueSpans("Johannes Borch", null, {}), []);
  assert.deepEqual(valueSpans(undefined, undefined, undefined), []);
});

test("valueSpans stops at MAX_SPANS rather than let one pane grow unbounded", () => {
  // Three values, each repeated past its own findHits cap, so the total the
  // pane would otherwise carry is well over the limit.
  const mapping = {
    "[ENTITY_1]": { original: "Alpha" },
    "[ENTITY_2]": { original: "Bravo" },
    "[ENTITY_3]": { original: "Charlie" },
  };
  const text = "Alpha Bravo Charlie ".repeat(1500);
  assert.equal(valueSpans(text, mapping, {}).length, MAX_SPANS);
});

// --- renderOriginWithSpans ----------------------------------------------

test("renderOriginWithSpans wraps each span, carrying its placeholder", () => {
  const text = "Johannes Borch signed for Delta.";
  const html = renderOriginWithSpans(text, valueSpans(text, MAPPING, {}));
  const spans = all(html, ".value-origin");
  assert.deepEqual(spans.map((s) => s.attrs["data-ph"]), ["[PERSON_1]", "[ENTITY_1]"]);
  assert.deepEqual(spans.map((s) => s.inner), ["Johannes Borch", "Delta"]);
});

test("renderOriginWithSpans reproduces the text exactly, span or no span", () => {
  // The pane must READ the same with the feature as without it: the spans are
  // invisible until a mark is hovered, so a character lost here is a silent
  // corruption of the one pane the user checks the anonymisation against.
  const text = 'Johannes Borch <chief> & "Delta" cost 5 < 6.';
  const html = renderOriginWithSpans(text, valueSpans(text, MAPPING, {}));
  assert.equal(textOf(`<pre>${html}</pre>`, "pre"), text);
});

test("renderOriginWithSpans escapes a value carrying markup characters", () => {
  const mapping = { "[OTHER_1]": { original: '<b>&"x"' } };
  const text = 'the token <b>&"x" appears once';
  const html = renderOriginWithSpans(text, valueSpans(text, mapping, {}));
  assert.ok(!html.includes("<b>"), "the value's own angle brackets stay escaped");
  assert.equal(all(html, ".value-origin")[0].attrs["data-ph"], "[OTHER_1]");
});

test("renderOriginWithSpans emits the search hits inside and outside a span", () => {
  const text = "Johannes Borch met Borch";
  const hits = findHits(text, "Borch");
  const html = renderOriginWithSpans(text, valueSpans(text, MAPPING, { "[PERSON_1]": ["Borch"] }), hits, 0);
  const found = all(html, ".find-hit");
  assert.equal(found.length, 2, "one hit nested in each origin span");
  assert.ok(found[0].attrs.class.includes("active"));
});

test("renderOriginWithSpans with no spans is the plain escaped text plus hits", () => {
  const text = "nothing was replaced here";
  assert.equal(renderOriginWithSpans(text, []), text);
  assert.equal(all(renderOriginWithSpans(text, [], findHits(text, "here"), 0), ".find-hit").length, 1);
});

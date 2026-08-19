// panesearch.test.js, the Compare search's pure half.
//
// These pin the two things the rest of the feature is built on: where the hits
// ARE (offsets into the plain text, so both renderers can emit them during
// their own escaping pass) and that rendering them never produces markup the
// escaping was supposed to prevent.

import { test } from "node:test";
import assert from "node:assert/strict";

import { findHits, escapeWithHits, MAX_HITS, MIN_NEEDLE } from "./panesearch.js";

/** whole(text, hits, activeIndex) is escapeWithHits over the whole string, the
 *  shape a pane with nothing over its text renders in. */
const whole = (text, hits, activeIndex = -1) =>
  escapeWithHits(text, hits, activeIndex, 0, String(text ?? "").length);

test("a needle shorter than the minimum finds nothing", () => {
  // One character matches most of any document, which tells the reader as
  // little as matching nothing does.
  assert.deepEqual(findHits("aaaa", ""), []);
  assert.deepEqual(findHits("aaaa", "a"), []);
  assert.equal(MIN_NEEDLE, 2);
  assert.deepEqual(findHits("aaaa", "   "), [], "whitespace is not a needle");
});

test("matching is case-insensitive", () => {
  assert.deepEqual(findHits("Alpine TRUST alpine", "alpine"),
    [{ start: 0, end: 6 }, { start: 13, end: 19 }]);
});

test("hits are non-overlapping, left to right", () => {
  // "aa" in "aaaa" is two hits, not three: a reader stepping through
  // occurrences expects each one to start after the last one ended.
  assert.deepEqual(findHits("aaaa", "aa"), [{ start: 0, end: 2 }, { start: 2, end: 4 }]);
});

test("hits at the very start and the very end are found", () => {
  assert.deepEqual(findHits("abcabc", "abc"),
    [{ start: 0, end: 3 }, { start: 3, end: 6 }]);
});

test("a needle that is not there finds nothing", () => {
  assert.deepEqual(findHits("Alpine Trust", "Borealis"), []);
});

test("the hit count is capped", () => {
  const text = "x".repeat(MAX_HITS * 2 + 50);
  assert.equal(findHits(text, "xx").length, MAX_HITS,
    "past the cap the whole pane is highlighted, which shows nothing");
});

test("escapeWithHits escapes the text and wraps each hit", () => {
  const text = 'a <b> & "c" a';
  const hits = findHits(text, "a");
  assert.deepEqual(hits, [], "guard: a 1-character needle is refused");

  const html = whole(text, findHits(text, '"c"'), 0);
  assert.ok(html.includes("&lt;b&gt;"), "the surrounding text is still escaped");
  assert.ok(html.includes('<span class="find-hit active">&quot;c&quot;</span>'),
    "the hit itself is escaped inside its span");
  assert.ok(!html.includes('<b>'), "no raw markup survives");
});

test("only the active hit carries the active class", () => {
  const html = whole("one two one", findHits("one two one", "one"), 1);
  const active = html.match(/find-hit active/g) ?? [];
  const all = html.match(/class="find-hit/g) ?? [];
  assert.equal(active.length, 1);
  assert.equal(all.length, 2);
});

test("no hits renders exactly the escaped text", () => {
  const text = 'a <b> & "c"';
  assert.equal(whole(text, [], 0), whole(text, null, 0));
  assert.ok(!whole(text, [], 0).includes("find-hit"));
});

test("a needle that could corrupt the markup is just text", () => {
  // The reason hits are computed over the PLAIN text: searching the rendered
  // HTML for "span" or "&" would rewrite the markup around it.
  const text = "the span of & the mark";
  const html = whole(text, findHits(text, "span"), 0);
  assert.ok(html.includes('<span class="find-hit active">span</span>'));
  assert.ok(html.includes("&amp;"), "the ampersand is still escaped, not matched as markup");
});

test("escapeWithHits emits only the hits ENTIRELY inside the stretch", () => {
  // This is the rule the panes depend on: a renderer walks its own elements
  // (a category mark, a value-origin span) and hands the stretches between and
  // inside them here. A hit straddling one of those edges is left alone rather
  // than split, because splitting would cut the element in two and take its
  // data attributes, its tooltip and its click target with it.
  const text = "alpha bravo charlie";
  const hits = findHits(text, "bravo"); // [6, 11)
  assert.equal(escapeWithHits(text, hits, 0, 6, 11),
    '<span class="find-hit active">bravo</span>', "exactly the stretch: wrapped");
  assert.equal(escapeWithHits(text, hits, 0, 0, 8), "alpha br",
    "straddling the far edge: emitted as plain text");
  assert.equal(escapeWithHits(text, hits, 0, 8, 19), "avo charlie",
    "straddling the near edge: emitted as plain text");
});

// panesearch.test.js, the Compare search's pure half.
//
// These pin the two things the rest of the feature is built on: where the hits
// ARE (offsets into the plain text, so both renderers can emit them during
// their own escaping pass) and that rendering them never produces markup the
// escaping was supposed to prevent.

import { test } from "node:test";
import assert from "node:assert/strict";

import { findHits, renderPlainWithHits, MAX_HITS, MIN_NEEDLE } from "./panesearch.js";

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

test("renderPlainWithHits escapes the text and wraps each hit", () => {
  const text = 'a <b> & "c" a';
  const hits = findHits(text, "a");
  assert.deepEqual(hits, [], "guard: a 1-character needle is refused");

  const html = renderPlainWithHits(text, findHits(text, '"c"'), 0);
  assert.ok(html.includes("&lt;b&gt;"), "the surrounding text is still escaped");
  assert.ok(html.includes('<span class="find-hit active">&quot;c&quot;</span>'),
    "the hit itself is escaped inside its span");
  assert.ok(!html.includes('<b>'), "no raw markup survives");
});

test("only the active hit carries the active class", () => {
  const html = renderPlainWithHits("one two one", findHits("one two one", "one"), 1);
  const active = html.match(/find-hit active/g) ?? [];
  const all = html.match(/class="find-hit/g) ?? [];
  assert.equal(active.length, 1);
  assert.equal(all.length, 2);
});

test("no hits renders exactly the escaped text", () => {
  const text = 'a <b> & "c"';
  assert.equal(renderPlainWithHits(text, [], 0), renderPlainWithHits(text, null, 0));
  assert.ok(!renderPlainWithHits(text, [], 0).includes("find-hit"));
});

test("a needle that could corrupt the markup is just text", () => {
  // The reason hits are computed over the PLAIN text: searching the rendered
  // HTML for "span" or "&" would rewrite the markup around it.
  const text = "the span of & the mark";
  const html = renderPlainWithHits(text, findHits(text, "span"), 0);
  assert.ok(html.includes('<span class="find-hit active">span</span>'));
  assert.ok(html.includes("&amp;"), "the ampersand is still escaped, not matched as markup");
});

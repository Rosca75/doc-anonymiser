// panesearch.js, finding a needle in the Compare panes.
//
// Search must NOT be done by rewriting the already-rendered HTML. The
// anonymised pane is full of <mark> elements and escaped values, so a needle
// like "mark" or "&" would corrupt the markup and take the tooltips and the
// click-to-select with it. So hits are computed over the PLAIN text and
// rendered during the same pass that escapes it: highlight.js does that for the
// anonymised pane, and renderPlainWithHits below for the original one.
//
// Pure JavaScript, no DOM required, unit-tested with `node --test`
// (panesearch.test.js).

import { escapeHTML } from "./html.js";

/**
 * MAX_HITS bounds one pane's highlights. Past a couple of thousand the
 * highlight tells the reader nothing (the whole pane is yellow) and the DOM
 * cost is real, so the search says it stopped rather than freezing the pane.
 */
export const MAX_HITS = 2000;

/**
 * MIN_NEEDLE is the shortest needle worth searching. One character matches
 * most of any document, which is the same as matching nothing.
 */
export const MIN_NEEDLE = 2;

/**
 * findHits(text, needle) locates every occurrence of needle in text.
 *
 * Case-insensitive, non-overlapping, left to right: "aa" in "aaaa" is two hits
 * at 0 and 2, not three. Offsets are into the PLAIN text, which is what both
 * renderers walk, so a hit can be emitted in the same pass that escapes the
 * character it starts at.
 *
 * @param {string} text the plain pane text
 * @param {string} needle what to find
 * @returns {Array<{start:number,end:number}>} up to MAX_HITS hits, in order
 */
export function findHits(text, needle) {
  const hay = String(text ?? "");
  const pin = String(needle ?? "").trim();
  if (pin.length < MIN_NEEDLE) return [];

  const lowerHay = hay.toLowerCase();
  const lowerPin = pin.toLowerCase();
  // Lowercasing can change a string's length for some runes, and an offset
  // taken from a differently-sized copy would land mid-character. When that
  // happens the search simply finds nothing rather than corrupting the render.
  if (lowerHay.length !== hay.length || lowerPin.length !== pin.length) return [];

  const hits = [];
  let from = 0;
  while (hits.length < MAX_HITS) {
    const i = lowerHay.indexOf(lowerPin, from);
    if (i < 0) break;
    hits.push({ start: i, end: i + lowerPin.length });
    from = i + lowerPin.length; // non-overlapping
  }
  return hits;
}

/**
 * renderPlainWithHits(text, hits, activeIndex) escapes text and wraps each hit
 * in a span, the active one marked so the pane can scroll to it.
 *
 * @param {string} text the plain pane text
 * @param {Array<{start:number,end:number}>} hits from findHits, in order
 * @param {number} [activeIndex] which hit is the current one, -1 for none
 * @returns {string} safe HTML
 */
export function renderPlainWithHits(text, hits, activeIndex = -1) {
  const source = String(text ?? "");
  if (!hits || hits.length === 0) return escapeHTML(source);

  let out = "";
  let last = 0;
  hits.forEach((hit, i) => {
    if (hit.start < last) return; // overlapping input; keep the earlier hit
    out += escapeHTML(source.slice(last, hit.start));
    out += `<span class="${hitClass(i === activeIndex)}">` +
      `${escapeHTML(source.slice(hit.start, hit.end))}</span>`;
    last = hit.end;
  });
  out += escapeHTML(source.slice(last));
  return out;
}

/**
 * hitClass(active) is the class one hit span carries. Exported so highlight.js
 * emits exactly the same markup for hits inside the anonymised pane: two
 * spellings of the same class would mean the navigation could find a hit the
 * stylesheet does not tint, or the other way round.
 */
export function hitClass(active) {
  return active ? "find-hit active" : "find-hit";
}

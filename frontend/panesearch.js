// panesearch.js, finding a needle in the Compare panes.
//
// Search must NOT be done by rewriting the already-rendered HTML. The
// anonymised pane is full of <mark> elements and escaped values, so a needle
// like "mark" or "&" would corrupt the markup and take the tooltips and the
// click-to-select with it. So hits are computed over the PLAIN text and
// rendered during the same pass that escapes it: highlight.js does that for the
// escapeWithHits below is the one function that does it, and every pane
// renderer hands its stretches to it.
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
 * escapeWithHits(text, hits, activeIndex, from, to) escapes ONE stretch of the
 * pane text and wraps whatever hits fall entirely inside it.
 *
 * This is THE definition of "escaped text with the search highlighted", and
 * every pane renderer goes through it. Both panes nest other elements over the
 * text (category marks in the anonymised pane, value-origin spans in the
 * original one), so each renderer walks its own elements and hands the stretches
 * BETWEEN and INSIDE them here. Two spellings of this loop would mean the
 * navigation could step to a hit one pane does not tint.
 *
 * A hit STRADDLING the stretch's edge is deliberately left alone rather than
 * split: splitting would cut the enclosing element in two and take its data
 * attributes, its tooltip and its click target with it, and a needle spanning
 * the edge of a placeholder is a rare thing to search for.
 *
 * @param {string} text the WHOLE plain pane text (hit offsets index into it)
 * @param {Array<{start:number,end:number}>} hits from findHits, in order
 * @param {number} activeIndex which hit is the current one, -1 for none
 * @param {number} from first character of the stretch to emit
 * @param {number} to one past its last character
 * @returns {string} safe HTML
 */
export function escapeWithHits(text, hits, activeIndex, from, to) {
  const source = String(text ?? "");
  const list = hits ?? [];
  if (list.length === 0) return escapeHTML(source.slice(from, to));

  let out = "";
  let cursor = from;
  for (let i = 0; i < list.length; i++) {
    const hit = list[i];
    // Already emitted, or an overlapping input hit: keep the earlier one rather
    // than emit the same characters twice.
    if (hit.start < cursor) continue;
    if (hit.start >= to) break; // hits are ordered, so nothing later fits either
    if (hit.end > to) continue; // straddles the far edge
    out += escapeHTML(source.slice(cursor, hit.start));
    out += `<span class="${hitClass(i === activeIndex)}">` +
      `${escapeHTML(source.slice(hit.start, hit.end))}</span>`;
    cursor = hit.end;
  }
  return out + escapeHTML(source.slice(cursor, to));
}


/**
 * hitClass(active) is the class one hit span carries. Module-private, because
 * escapeWithHits is the only thing that emits a hit: two spellings of the class
 * would mean the navigation could step to a hit the stylesheet does not tint, or
 * the other way round.
 */
function hitClass(active) {
  return active ? "find-hit active" : "find-hit";
}

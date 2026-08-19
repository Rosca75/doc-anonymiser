// valuespans.js, the link between the two Compare panes: WHICH STRETCHES OF THE
// ORIGINAL TEXT each placeholder stands for.
//
// The anonymised pane shows "[PERSON_1]". The original pane shows the words it
// replaced, and those words are not one string: a Value is a mainText plus its
// spellings, so one placeholder can stand for "Johannes Borch" in one paragraph
// and "Borch" in the next. Hovering the placeholder tints every one of them, so
// the reader can see what left the document without searching for it by hand.
//
// The spans are computed over the PLAIN pane text and emitted during the same
// pass that escapes it, exactly as the search hits are (panesearch.js), and for
// the same reason: rewriting already-rendered HTML would corrupt the escaped
// entities and the elements around them.
//
// Pure JavaScript, no DOM required, unit-tested with `node --test`
// (valuespans.test.js).

import { escapeHTML } from "./html.js";
import { findHits, escapeWithHits } from "./panesearch.js";

/**
 * MAX_SPANS bounds how many origin spans one pane carries. The hover tint is an
 * aid to reading one placeholder, not a survey of the document, and past a few
 * thousand elements the pane's repaint cost is real. A document with more
 * matches than this keeps the ones nearest the top of the text and the rest stay
 * untinted; the search bar is the tool for finding those.
 */
export const MAX_SPANS = 4000;

/**
 * WORD_CHAR decides what counts as "inside a word" for the boundary rule below.
 * Unicode-aware, because the documents are French as often as English and
 * "Dupont" must not match inside "Dupontel" any more than "Alten" matches
 * inside "Altenberg".
 */
const WORD_CHAR = /[\p{L}\p{N}_]/u;

/**
 * replacedTexts(placeholder, mapping, occurrenceSpellings) is every string that
 * placeholder is known to have replaced: its mainText value, plus each distinct
 * spelling the run recorded for it in THIS document.
 *
 * The spellings come from the run itself rather than from re-deriving the
 * Value's expansion here. A derivation done twice can disagree with itself
 * (spelling policy, folding, the allowlist), and the recorded list is the one
 * thing that cannot: it is what the pipeline actually matched.
 *
 * Comparison is case-insensitive throughout, because the matching below is too:
 * "borch" and "Borch" are one needle, not two.
 *
 * @param {string} placeholder e.g. "[PERSON_1]"
 * @param {object} [mapping] placeholder → {original, category}
 * @param {object} [occurrenceSpellings] placeholder → ordered spellings, "" for mainText
 * @returns {string[]} the distinct strings, mainText first
 */
export function replacedTexts(placeholder, mapping, occurrenceSpellings) {
  const out = [];
  const seen = new Set();
  const add = (text) => {
    const value = String(text ?? "");
    const key = value.toLowerCase();
    if (!value || seen.has(key)) return;
    seen.add(key);
    out.push(value);
  };
  add(mapping?.[placeholder]?.original);
  for (const spelling of occurrenceSpellings?.[placeholder] ?? []) add(spelling);
  return out;
}

/**
 * valueSpans(text, mapping, occurrenceSpellings) locates every stretch of the
 * original text that a placeholder replaced.
 *
 * Two rules keep the tint honest:
 *
 * WORD BOUNDARIES. A span must not start or end inside a word, so "Alten" is
 * not tinted inside "Altenberg". This is the rule the engine's own value pass
 * follows, and a highlight claiming a match the pipeline would never make would
 * be worse than no highlight at all.
 *
 * LONGEST WINS. Two placeholders can both claim a stretch ("Delta" inside
 * "Delta Industries"), and one character can only be tinted once. The longest
 * claim takes it, which is the same tie-break the engine's own overlap
 * resolution starts from, so the tint agrees with what the run replaced.
 *
 * @param {string} text the plain ORIGINAL pane text
 * @param {object} [mapping] placeholder → {original, category}
 * @param {object} [occurrenceSpellings] placeholder → ordered spellings
 * @returns {Array<{start:number,end:number,ph:string}>} non-overlapping, in
 *   document order
 */
export function valueSpans(text, mapping, occurrenceSpellings) {
  const source = String(text ?? "");
  if (!source || !mapping) return [];

  // Every claim any placeholder makes on any stretch, before the overlaps are
  // settled. findHits caps each needle on its own, so one value repeated
  // thousands of times cannot crowd out the others.
  const claims = [];
  for (const placeholder of Object.keys(mapping)) {
    for (const needle of replacedTexts(placeholder, mapping, occurrenceSpellings)) {
      for (const hit of findHits(source, needle)) {
        if (!onWordBoundaries(source, hit.start, hit.end)) continue;
        claims.push({ start: hit.start, end: hit.end, ph: placeholder });
      }
    }
  }
  if (claims.length === 0) return [];

  // Longest first, then earliest, then by placeholder so the result does not
  // depend on the order Object.keys happened to return.
  claims.sort((a, b) =>
    (b.end - b.start) - (a.end - a.start) ||
    a.start - b.start ||
    (a.ph < b.ph ? -1 : a.ph > b.ph ? 1 : 0));

  // `taken` is one byte per character rather than a scan of the kept spans:
  // a document with thousands of matches would otherwise cost a comparison
  // against every span already kept, for every claim.
  const taken = new Uint8Array(source.length);
  const kept = [];
  for (const claim of claims) {
    if (kept.length >= MAX_SPANS) break;
    if (overlaps(taken, claim.start, claim.end)) continue;
    taken.fill(1, claim.start, claim.end);
    kept.push(claim);
  }
  kept.sort((a, b) => a.start - b.start);
  return kept;
}

/** onWordBoundaries(text, start, end) reports whether the stretch is a whole
 *  word rather than a fragment of a longer one. */
function onWordBoundaries(text, start, end) {
  const before = start > 0 ? text[start - 1] : "";
  const after = end < text.length ? text[end] : "";
  // Only guard an edge that is itself a word character: a value ending in ")"
  // or "@" has no word edge to protect, and demanding one would drop it.
  if (before && WORD_CHAR.test(before) && WORD_CHAR.test(text[start])) return false;
  if (after && WORD_CHAR.test(after) && WORD_CHAR.test(text[end - 1])) return false;
  return true;
}

/** overlaps(taken, start, end) reports whether any character of the stretch is
 *  already claimed by a longer span. */
function overlaps(taken, start, end) {
  for (let i = start; i < end; i++) {
    if (taken[i]) return true;
  }
  return false;
}

/**
 * renderOriginWithSpans(text, spans, hits, activeIndex) escapes the original
 * pane's text, wraps each origin span so the hover tint has something to
 * address, and emits the search hits in the same pass.
 *
 * The span carries data-ph and NOTHING visible: it is invisible until a mark in
 * the other pane is hovered, so the pane reads exactly as it did before. A
 * search hit straddling a span's edge is left untinted for the same reason it is
 * inside a mark (panesearch.js escapeWithHits).
 *
 * @param {string} text the plain ORIGINAL pane text
 * @param {Array<{start:number,end:number,ph:string}>} spans from valueSpans
 * @param {Array<{start:number,end:number}>} [hits] from findHits, in order
 * @param {number} [activeIndex] which hit is the current one, -1 for none
 * @returns {string} safe HTML
 */
export function renderOriginWithSpans(text, spans, hits, activeIndex = -1) {
  const source = String(text ?? "");
  const list = spans ?? [];
  if (list.length === 0) return escapeWithHits(source, hits, activeIndex, 0, source.length);

  let out = "";
  let last = 0;
  for (const span of list) {
    if (span.start < last) continue; // overlapping input; keep the earlier span
    out += escapeWithHits(source, hits, activeIndex, last, span.start);
    out += `<span class="value-origin" data-ph="${escapeHTML(span.ph)}">` +
      `${escapeWithHits(source, hits, activeIndex, span.start, span.end)}</span>`;
    last = span.end;
  }
  return out + escapeWithHits(source, hits, activeIndex, last, source.length);
}

// highlight.js, pure rendering helper for the results view: wraps the
// placeholders in anonymised text with category-coloured <mark> elements,
// and carries the original term for hover tooltips
// and click-to-reassign.
//
// Pure JavaScript, no DOM required, unit-tested with `node --test`
// (highlight.test.js), which is why it is not inlined in views/run.js.
//
// Everything that is NOT a placeholder is escaped, and so are the data
// attributes: originals come from user documents (they can contain
// quotes and angle brackets) and must never reach innerHTML raw.

import { escapeHTML } from "./html.js";
import { hitClass } from "./panesearch.js";

// Placeholder shape produced by the Go registry: [LABEL_N].
const PLACEHOLDER_RE = /\[([A-Z][A-Z0-9_]*)_(\d+)\]/g;

// Label → colour family. PII labels share one tint, entity labels another,
// custom patterns a third (see style.css mark.* classes).
const PII_LABELS = new Set(["EMAIL", "PHONE", "IBAN", "VAT", "NATIONAL_ID", "URL", "AMOUNT", "DATE"]);
const ENTITY_LABELS = new Set([
  "ENTITY", "PROJECT", "PRODUCT", "BRAND", "PERSON", "ID", "OTHER",
]);

/**
 * markClass(label) picks the CSS class for one placeholder label.
 *
 * A label missing from both sets falls through to "custom", so an entity label
 * left out of ENTITY_LABELS renders in the wrong tint with nothing failing:
 * highlight.test.js asserts every label the registry can produce.
 */
export function markClass(label) {
  if (PII_LABELS.has(label)) return "pii";
  if (ENTITY_LABELS.has(label)) return "entity";
  return "custom"; // CUSTOM and any future label default to the third tint
}

/**
 * renderHighlighted(text, mapping) → HTML string with placeholders
 * wrapped in <mark> and all other content escaped.
 *
 * When the mapping knows a placeholder ("[ENTITY_1]" → {original,
 * category}), the mark carries data-ph, data-original, data-category and a
 * title="Original: <value>". When that occurrence replaced a non-mainText
 * spelling it also carries data-spelling, and the title shows
 * "spelling (original)". The title is the accessibility fallback; the styled
 * tooltip is positioned in JS against the Compare card (,
 * views/anonymise.js), because a CSS ::after inside the pane was CLIPPED by
 * the pane's own overflow and never appeared near the right-hand edge.
 *
 * A known mark is also focusable (tabindex="0"): the tooltip and the
 * click-to-select were mouse-only, which made the one place the anonymisation
 * is actually checked unreachable from the keyboard. A mapping miss falls back
 * to the label-only title exactly like v1, and stays unfocusable: there is
 * nothing to show.
 *
 * @param {string} text anonymised document text
 * @param {object} [mapping] placeholder → {original, category} lookup
 * @param {object} [derivedSpellings] placeholder → ordered array of the spellings each
 *   occurrence replaced (Go's ResultDocument.occurrenceVariants). Slot i is the
 *   text the i-th occurrence of that placeholder replaced, "" or absent when it
 *   was the mainText value. The marks are walked in the same left-to-right
 *   order Go recorded them, so slot i lines up with occurrence i.
 * @param {object} [search] {hits, activeIndex} from panesearch.js, with hits
 *   offset into the SAME text string this walks. Hits are emitted in the plain
 *   stretches and inside a mark's own text. A hit STRADDLING a mark boundary is
 *   deliberately not highlighted: the alternative is splitting the mark, which
 *   breaks the click-to-select and the tooltip, and a needle spanning the edge
 *   of a placeholder is a rare thing to search for. Three-argument callers are
 *   unaffected.
 * @returns {string} safe HTML
 */
export function renderHighlighted(text, mapping, derivedSpellings, search) {
  let out = "";
  let last = 0;
  const hits = search?.hits ?? [];
  const activeIndex = search?.activeIndex ?? -1;
  // withHits escapes a stretch of the text and wraps whatever hits fall
  // ENTIRELY inside it. Passing every stretch through one function is what
  // keeps the plain text and a mark's inner text behaving the same way.
  const withHits = (from, to) => {
    if (hits.length === 0) return escapeHTML(text.slice(from, to));
    let piece = "";
    let cursor = from;
    for (let i = 0; i < hits.length; i++) {
      const hit = hits[i];
      if (hit.end <= cursor) continue;
      if (hit.start >= to) break;
      // Straddles this stretch's edge: leave it alone rather than split a mark.
      if (hit.start < from || hit.end > to) continue;
      piece += escapeHTML(text.slice(cursor, hit.start));
      piece += `<span class="${hitClass(i === activeIndex)}">` +
        `${escapeHTML(text.slice(hit.start, hit.end))}</span>`;
      cursor = hit.end;
    }
    return piece + escapeHTML(text.slice(cursor, to));
  };

  const seen = Object.create(null); // placeholder → occurrences emitted so far
  for (const m of String(text).matchAll(PLACEHOLDER_RE)) {
    out += withHits(last, m.index);
    const label = m[1];
    const info = mapping?.[m[0]];
    const occ = seen[m[0]] ?? 0;
    seen[m[0]] = occ + 1;
    if (info?.original) {
      // The spelling this occurrence replaced, shown only when it differs from
      // the mainText value (case aside): "Borch" for [PERSON_1] whose value is
      // "Johannes Borch", nothing when the match WAS "Johannes Borch".
      const spelling = derivedSpellings?.[m[0]]?.[occ];
      const showVariant =
        spelling && spelling.toLowerCase() !== info.original.toLowerCase();
      const tip = showVariant ? `${spelling} (${info.original})` : info.original;
      out += `<mark class="${markClass(label)}" data-ph="${escapeHTML(m[0])}"` +
        ` data-original="${escapeHTML(info.original)}"` +
        (showVariant ? ` data-spelling="${escapeHTML(spelling)}"` : "") +
        (info.category ? ` data-category="${escapeHTML(info.category)}"` : "") +
        ` tabindex="0"` +
        ` title="Original: ${escapeHTML(tip)}">` +
        `${withHits(m.index, m.index + m[0].length)}</mark>`;
    } else {
      out += `<mark class="${markClass(label)}" title="${escapeHTML(label.toLowerCase())}">` +
        `${withHits(m.index, m.index + m[0].length)}</mark>`;
    }
    last = m.index + m[0].length;
  }
  out += withHits(last, text.length);
  return out;
}

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
 * title="Original: <value>". The title is the accessibility fallback; the
 * styled tooltip is positioned in JS against the Compare card (,
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
 * @returns {string} safe HTML
 */
export function renderHighlighted(text, mapping) {
  let out = "";
  let last = 0;
  for (const m of String(text).matchAll(PLACEHOLDER_RE)) {
    out += escapeHTML(text.slice(last, m.index));
    const label = m[1];
    const info = mapping?.[m[0]];
    if (info?.original) {
      out += `<mark class="${markClass(label)}" data-ph="${escapeHTML(m[0])}"` +
        ` data-original="${escapeHTML(info.original)}"` +
        (info.category ? ` data-category="${escapeHTML(info.category)}"` : "") +
        ` tabindex="0"` +
        ` title="Original: ${escapeHTML(info.original)}">${escapeHTML(m[0])}</mark>`;
    } else {
      out += `<mark class="${markClass(label)}" title="${escapeHTML(label.toLowerCase())}">${escapeHTML(m[0])}</mark>`;
    }
    last = m.index + m[0].length;
  }
  out += escapeHTML(text.slice(last));
  return out;
}

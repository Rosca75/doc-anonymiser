// highlight.js, pure rendering helper for the results view: wraps the
// placeholders in anonymised text with category-coloured <mark> elements,
// and (BUILD-02 Phase 10b) carries the original term for hover tooltips
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
const ENTITY_LABELS = new Set(["CLIENT", "PROJECT", "INTERNAL", "PERSON", "ORG", "LOCATION"]);

/** markClass(label) picks the CSS class for one placeholder label. */
export function markClass(label) {
  if (PII_LABELS.has(label)) return "pii";
  if (ENTITY_LABELS.has(label)) return "entity";
  return "custom"; // CUSTOM and any future label default to the third tint
}

/**
 * renderHighlighted(text, mapping) → HTML string with placeholders
 * wrapped in <mark> and all other content escaped.
 *
 * When the mapping knows a placeholder ("[CLIENT_1]" → {original,
 * category}), the mark carries data-ph, data-original and a
 * title="Original: <value>" (native tooltip as accessibility fallback;
 * the styled tooltip reads data-original in CSS). A mapping miss falls
 * back to the label-only title exactly like v1.
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
        ` title="Original: ${escapeHTML(info.original)}">${escapeHTML(m[0])}</mark>`;
    } else {
      out += `<mark class="${markClass(label)}" title="${escapeHTML(label.toLowerCase())}">${escapeHTML(m[0])}</mark>`;
    }
    last = m.index + m[0].length;
  }
  out += escapeHTML(text.slice(last));
  return out;
}

// html.js — tiny shared HTML helpers for the views.
//
// escapeHTML() lives in its own module (not in a view) because EVERY view
// injects document-derived strings into innerHTML — escaping everything
// from day one prevents an entire class of injection bugs, and a shared
// module keeps it testable with `node --test`.

/** escapeHTML(text) neutralises HTML-special characters. */
export function escapeHTML(text) {
  return String(text)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

/** fmtSize(bytes) renders a human-readable file size for the import list. */
export function fmtSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

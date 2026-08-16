// scroll.js, scroll-position preservation across re-renders
//
// The problem it solves: every reducer call runs setState, every setState
// repaints the WHOLE shell (main.js paint), and the repaint rewrites
// root.innerHTML. A rewritten element starts at scrollTop 0, so ticking a
// checkbox two thirds of the way down the Identify rail, drilling into a value,
// or adding an allowlist term threw the user back to the top of that panel.
//
// The fix is deliberately GENERIC and lives at the ONE place the reset happens,
// the shell repaint, rather than at each call site. An earlier version made
// every mutating handler wrap itself in a "keep this scroller" helper, which
// was easy to get wrong two ways at once: the default helper restored main#view
// (the page body), but in the fixed-height wizard main#view does not scroll at
// all, the card bodies do, so most handlers preserved nothing; and any handler
// that forgot to wrap reset the panel with no warning. Preserving scroll around
// the repaint itself means a handler cannot get it wrong, because it does not
// take part.
//
// The mechanism: snapshot the scroll offset of every currently-scrolled element
// just before the rewrite, keyed by a selector that survives the rewrite, then
// restore each after the new DOM (including each view's own render) is in place.
// Because the DOM is regenerated deterministically from state, the selector
// re-resolves to the same element. This also handles the async double-repaint
// for free: a handler that awaits a bridge reply and repaints again snapshots
// the intermediate DOM, which the previous paint already restored, so the offset
// is carried across the second paint too.
//
// This module is DOM-only and has no state of its own, which is why it is not
// part of state.js.

/**
 * stableSelector(el) returns a CSS selector that re-resolves to `el` after the
 * shell rewrites root.innerHTML.
 *
 * The rewrite regenerates an identical DOM from the same state, so identity is
 * preserved by STRUCTURE: an id when the element (or an ancestor) has one, else
 * a `tag:nth-of-type(n)` step up to the nearest id or the document root. Every
 * scroll owner in the app is anchored under an id (#identify-rail,
 * #identify-workspace, #view) or is a single stable instance, so the selector is
 * short and unambiguous.
 *
 * @param {Element} el
 * @returns {string} a selector, or "" when one cannot be built
 */
export function stableSelector(el) {
  if (!el || el.nodeType !== 1) return "";
  // An id is unique in the document, so it anchors the path and ends the walk.
  if (el.id) return `#${cssEscape(el.id)}`;
  const parent = el.parentElement;
  if (!parent) {
    // The element is detached or is the root: a bare tag is the best we can do.
    return el.tagName ? el.tagName.toLowerCase() : "";
  }
  const tag = el.tagName.toLowerCase();
  // Index among same-tag siblings (nth-of-type is 1-based).
  let index = 1;
  for (let sib = el.previousElementSibling; sib; sib = sib.previousElementSibling) {
    if (sib.tagName === el.tagName) index += 1;
  }
  const step = `${tag}:nth-of-type(${index})`;
  const prefix = stableSelector(parent);
  return prefix ? `${prefix} > ${step}` : step;
}

/**
 * snapshotScrollPositions() records the scroll offset of every element that is
 * currently scrolled away from its origin.
 *
 * Only scrolled elements are recorded: an element still at 0/0 has nothing to
 * restore, and skipping them keeps both the snapshot and the restore cheap.
 * Called BEFORE the repaint, while the current DOM (and its scroll offsets)
 * still exists.
 *
 * @returns {Array<{selector: string, top: number, left: number}>}
 */
export function snapshotScrollPositions() {
  if (typeof document === "undefined") return [];
  const snapshot = [];
  for (const el of document.querySelectorAll("*")) {
    const top = el.scrollTop;
    const left = el.scrollLeft;
    if (!top && !left) continue;
    const selector = stableSelector(el);
    if (selector) snapshot.push({ selector, top, left });
  }
  return snapshot;
}

/**
 * restoreScrollPositions(snapshot) puts each recorded offset back.
 *
 * Called AFTER the repaint, once the new DOM is in place. Re-queries each
 * selector so it addresses the freshly-created element; a selector that no
 * longer resolves (a panel the new state does not render) is skipped rather
 * than treated as an error.
 *
 * @param {Array<{selector: string, top: number, left: number}>} snapshot
 */
export function restoreScrollPositions(snapshot) {
  if (typeof document === "undefined" || !snapshot?.length) return;
  for (const { selector, top, left } of snapshot) {
    const el = document.querySelector(selector);
    if (!el) continue;
    el.scrollTop = top;
    el.scrollLeft = left;
  }
}

/**
 * cssEscape(id) escapes an id for use in a selector. Ids in this app are simple
 * slugs, but CSS.escape is used when present so an id that ever grows a special
 * character cannot silently produce a broken selector; the manual fallback
 * covers the non-browser test environment.
 */
function cssEscape(id) {
  if (typeof CSS !== "undefined" && typeof CSS.escape === "function") {
    return CSS.escape(id);
  }
  return String(id).replace(/[^a-zA-Z0-9_-]/g, (ch) => `\\${ch}`);
}

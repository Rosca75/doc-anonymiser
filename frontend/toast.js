// toast.js, the notice strip (BUILD-05 Phase 1).
//
// A notice is how the application makes a STATEMENT the user only has to
// notice: "Saved report.md to the destination folder", "3 documents
// anonymised, nothing written to disk yet". It is deliberately not a dialog:
// nothing is waiting on an answer, so nothing should block.
//
// A QUESTION goes through modal.js instead. The split matters, and it is the
// whole point of decision 10: the application used to reach for the native
// confirm() for both, which stole window focus for things that were only ever
// informational.
//
// This module is the thin wiring layer over two things that already exist:
// state.notice (state.js, the single source of truth) and ui.js toastHTML
// (the markup). It holds no state of its own.
//
// PLACEMENT is the caller's job. Each screen renders noticeHTML() where its
// mock-up puts it, which for the Export screen is the foot of the documents
// card and elsewhere is above the step footer. There is deliberately no
// shell-level strip: two notice channels would let one screen show two
// notices at once, and the store only ever holds one.

import { getState, setNotice, clearNotice } from "./state.js";
import { toastHTML } from "./ui.js";

/**
 * noticeHTML(s) renders the current notice, or nothing at all.
 * @param {object} [s] state (defaults to the live state; injectable for tests)
 * @returns {string} safe HTML ("" when there is no notice)
 */
export function noticeHTML(s = getState()) {
  return toastHTML(s.notice);
}

/**
 * wireNotice(container) attaches the dismiss button of a notice rendered
 * inside `container`.
 *
 * It is a no-op when there is no notice on screen, so a view can call it
 * unconditionally after every re-render rather than guarding first.
 *
 * @param {HTMLElement} container the element the view just filled
 */
export function wireNotice(container) {
  container.querySelector("#notice-dismiss")?.addEventListener("click", clearNotice);
}

/**
 * notify(text, tone) is the shorthand views use to say something.
 *
 * It exists so a view imports its notice API from one module (this one)
 * rather than reaching into state.js for the reducer and ui.js for the
 * markup. Re-exported rather than reimplemented: state.js stays the only
 * place state changes.
 *
 * @param {string} text the sentence to show
 * @param {string} [tone] "ok" (something was written) | "info" (a fact) |
 *   "warn" (it worked and the result needs care)
 * @returns {object|null} the stored notice
 */
export function notify(text, tone = "info") {
  return setNotice(text, tone);
}

/** dismiss() clears the strip, for a view that wants to clear it explicitly
 *  (opening a review panel replaces the notice it would sit under). */
export function dismiss() {
  clearNotice();
}

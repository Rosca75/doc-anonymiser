// modal.js, the in-app confirm (BUILD-05 decision 10).
//
// This module exists to delete every native confirm() from the application.
// A native dialog in a WebView is unstyled and unbranded, it cannot say
// anything longer than a line comfortably, and on Windows it steals focus from
// the window it belongs to. The replacement is an ordinary overlay: same
// typeface, same buttons, same copy rules, and it can carry the key-bearing
// tint when the question is about the re-identification key.
//
// The API is deliberately the same SHAPE as the thing it replaces, so a call
// site changes by one keyword:
//
//     if (!confirm("...")) return;            // before
//     if (!await askConfirm({ ... })) return; // after
//
// Two halves, in the usual split: state.js owns the pending question and the
// promise (askConfirm / answerConfirm), this module owns the markup and the
// DOM wiring. The overlay is rendered ONCE, by main.js at shell level, because
// a question can be asked from any screen and it must survive the re-render
// its own answer triggers.

import { getState, askConfirm as askConfirmState, answerConfirm } from "./state.js";
import { modalHTML } from "./ui.js";

/**
 * askConfirm(question) asks the user a yes/no question and resolves to their
 * answer. This is THE replacement for confirm().
 *
 * @param {object} question
 * @param {string} question.title the short heading, e.g. "Export the value
 *   mapping as CSV"
 * @param {string} question.body what will happen, in full sentences. This is
 *   where the native dialog's one cramped line becomes readable copy.
 * @param {string} [question.confirmLabel] the affirmative button; name the
 *   ACTION ("Export CSV"), not "OK", so the button says what it does
 * @param {string} [question.cancelLabel] default "Cancel"
 * @param {boolean} [question.keyBearing] the action exposes the
 *   re-identification key, which tints the dialog with the key surface
 * @returns {Promise<boolean>} true when confirmed, false when cancelled
 */
export function askConfirm(question) {
  return askConfirmState(question);
}

/**
 * modalLayerHTML(s) renders the overlay for the pending question, or nothing.
 * @param {object} [s] state (defaults to the live state; injectable for tests)
 * @returns {string} safe HTML ("" when nothing is being asked)
 */
export function modalLayerHTML(s = getState()) {
  return modalHTML(s.confirm);
}

// escapeListener is the currently-attached document-level Escape handler, kept
// so a re-render can remove the previous one. Without this the listeners would
// accumulate one per paint, and after twenty repaints a single Escape would
// answer twenty (already-settled) questions.
let escapeListener = null;

/**
 * wireModal(container) attaches the dialog's behaviour: the two buttons, a
 * click on the backdrop, and the Escape key. Backdrop and Escape both answer
 * NO, matching every dialog convention: the safe answer is the one you get by
 * dismissing.
 *
 * Safe to call after every paint. When no dialog is on screen it only detaches
 * the stale Escape handler and returns.
 *
 * @param {HTMLElement} container the element main.js just filled
 */
export function wireModal(container) {
  if (escapeListener) {
    document.removeEventListener("keydown", escapeListener);
    escapeListener = null;
  }

  const layer = container.querySelector(".modal-layer");
  if (!layer) return;

  container.querySelector("#modal-confirm")?.addEventListener("click", () => answerConfirm(true));
  container.querySelector("#modal-cancel")?.addEventListener("click", () => answerConfirm(false));

  // A click on the backdrop dismisses; a click INSIDE the dialog must not, so
  // the target has to be the layer itself rather than any descendant.
  layer.addEventListener("click", (ev) => {
    if (ev.target === layer) answerConfirm(false);
  });

  escapeListener = (ev) => {
    if (ev.key !== "Escape") return;
    ev.preventDefault();
    answerConfirm(false);
  };
  document.addEventListener("keydown", escapeListener);

  // Move focus into the dialog so the keyboard is not still driving the screen
  // behind it. The affirmative button is NOT focused: for a question about
  // exporting the key, a stray Enter must not be the answer.
  container.querySelector("#modal-cancel")?.focus?.();
}

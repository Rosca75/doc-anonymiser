// nav.js, the ONE place the wizard moves (BUILD-05 Phase 2).
//
// This module exists because BUILD-05 gave every screen its own footer. Before
// that, the shell owned the only Back/Next buttons, so the movement rule could
// live in main.js next to them. Now the step bar (main.js) and four screen
// footers (views/*.js) all move the wizard, and if each did it its own way the
// backward-reset rule would hold in some of them and not others.
//
// It is a separate file rather than an export of main.js purely to keep the
// module graph acyclic: main.js imports the views, so a view importing main.js
// would close a cycle. A cycle would probably work, and it would be a trap.
//
// The rule, unchanged from BUILD-04 CR16:
//
//   forward   the guard decides, nothing is cleared, nothing is asked
//   backward  ask first; on yes, reset the step being LEFT and move; on no,
//             do nothing at all, not even the move
//
// Cancelling deliberately does NOT navigate. "Go back but keep everything"
// sounds convenient, but it leaves half-finished state behind that the user
// then walks forward into, which is the confusion the reset exists to prevent.
// Imported documents and the never-anonymise list are never touched either way
// (state.js STEP_RESETS).

import {
  getState, WIZARD_STEPS, canGoTo, goTo, nextStep, isBackward, resetStep,
} from "./state.js";
import { askConfirm } from "./modal.js";
import { stepFooter } from "./ui.js";
import { NAV } from "./copy.js";

/**
 * navigateTo(step) moves the wizard to a step, asking first when that means
 * going back.
 *
 * The question is the in-app modal, not the native confirm() this used to call
 * (BUILD-05 decision 10), which is why this is async: the answer arrives on a
 * promise instead of blocking the thread. A caller that ignores the returned
 * promise behaves exactly as it did before.
 *
 * @param {string} step the requested step token
 * @returns {Promise<boolean>} whether the wizard moved
 */
export async function navigateTo(step) {
  const current = getState().step;
  if (step === current) return false;
  if (!canGoTo(step)) return false;

  if (isBackward(current, step)) {
    const confirmed = await askConfirm({
      title: NAV.backConfirmTitle(current),
      body: NAV.backConfirmBody(current),
      confirmLabel: NAV.backConfirmLabel,
    });
    if (!confirmed) return false;
    // Re-check the guard: the user had time to think, and in that time an
    // event from Go (a drop that emptied the document list) could have made
    // the target unreachable.
    if (!canGoTo(step)) return false;
    resetStep(current);
  }
  return goTo(step);
}

/**
 * advance() moves one step forward, for a screen footer's CONTINUE button.
 * Forward moves never ask anything, so this is the plain linear move and it
 * needs no promise.
 * @returns {boolean} whether the wizard moved
 */
export function advance() {
  return nextStep();
}

/**
 * goBack() moves one step back, for a screen footer's "Back to X" link. It
 * goes through navigateTo, so the reset question is asked exactly as it is for
 * the step bar: a footer link is not a back door around the rule.
 * @returns {Promise<boolean>} whether the wizard moved
 */
export function goBack() {
  const index = WIZARD_STEPS.indexOf(getState().step);
  if (index <= 0) return Promise.resolve(false);
  return navigateTo(WIZARD_STEPS[index - 1]);
}

/**
 * previousStep(s) names the step a screen's footer goes back to, or null on
 * step 1 (which has no back link). Pure, so a view can ask it while building
 * its footer.
 * @param {object} [s] state (defaults to the live state)
 * @returns {string|null}
 */
export function previousStep(s = getState()) {
  const index = WIZARD_STEPS.indexOf(s.step);
  return index > 0 ? WIZARD_STEPS[index - 1] : null;
}

/**
 * followingStep(s) names the step a screen's footer continues to, or null on
 * the last step.
 * @param {object} [s] state (defaults to the live state)
 * @returns {string|null}
 */
export function followingStep(s = getState()) {
  const index = WIZARD_STEPS.indexOf(s.step);
  return index >= 0 && index < WIZARD_STEPS.length - 1 ? WIZARD_STEPS[index + 1] : null;
}

// --- The per-screen footer -------------------------------------------------
//
// All four screens end in the same bar, so the labels, the ids and the wiring
// live here once. What each screen supplies is the only thing that differs:
// the readiness hint, which is the sentence a shell-level footer could never
// have written.

/**
 * stepFooterHTML(opts, s) builds a screen's footer bar.
 *
 * The labels come from the step tokens either side of the current one, so a
 * screen never spells "Back to Import" or "CONTINUE TO ANONYMISE" itself: a
 * step rename reaches all four footers through copy.js NAV.
 *
 * @param {object} [opts]
 * @param {string} [opts.hint] what is ready, or what is still missing. This is
 *   the screen's own sentence and the reason the footer moved into the screens.
 * @param {boolean} [opts.nextDisabled] gate the move (defaults to asking the
 *   navigation guard, which is right for every screen so far)
 * @param {string} [opts.nextTitle] tooltip, normally why it is disabled
 * @param {boolean} [opts.standalone] render as a card of its own rather than as
 *   the foot of the screen's workspace card
 * @param {string} [opts.rightHTML] trusted markup replacing the primary button.
 *   The LAST step uses it: there is nothing to continue to, and Export's
 *   right-hand action is "start a new batch", which must not look like the
 *   CONTINUE button of the other three screens.
 * @param {object} [s] state (defaults to the live state)
 * @returns {string} safe HTML
 */
export function stepFooterHTML(opts = {}, s = getState()) {
  const previous = previousStep(s);
  const following = followingStep(s);
  const nextDisabled = opts.nextDisabled ?? (following ? !canGoTo(following, s) : true);
  return stepFooter({
    backLabel: previous ? NAV.back(previous) : "",
    backId: "step-back",
    hint: opts.hint,
    nextLabel: following ? NAV.next(following) : "",
    nextId: "step-next",
    nextDisabled,
    nextTitle: opts.nextTitle,
    standalone: opts.standalone,
    rightHTML: opts.rightHTML,
  });
}

/**
 * wireStepFooter(container) attaches the footer's two buttons. Safe to call
 * when the screen rendered no footer.
 * @param {HTMLElement} container the element the view just filled
 */
export function wireStepFooter(container) {
  container.querySelector("#step-back")?.addEventListener("click", goBack);
  container.querySelector("#step-next")?.addEventListener("click", advance);
}

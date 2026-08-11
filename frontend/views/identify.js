// views/identify.js, wizard step 2: the two-column Identify screen
// (BUILD-05 Phase 2; the rail lands in Phase 5, the workspace in Phase 6).
//
// Identify is one screen with two halves:
//
//   the rail        a third of the width: WHAT to look for (country, preset, the
//                   22 categories, the confidence floor) and HOW (smart
//                   detection, local AI). This is what used to be a Configure
//                   step of its own; BUILD-05 folded it in, because choosing
//                   what to detect and reviewing what was detected is one task,
//                   not two screens. It is big enough to deserve its own file,
//                   views/identifyrail.js.
//   the workspace   the other two thirds, owned by this module: the suggestions
//                   to review, the values accepted so far, the never-anonymise
//                   list (views/allowlist.js) and the custom patterns.
//
// This module owns the layout, the workspace, and the screen's footer, which is
// written here because this is the only place that knows about both halves.

import { getState } from "../state.js";
import { WORKSPACE } from "../copy.js";
import { stepFooterHTML, wireStepFooter } from "../nav.js";
import { renderIdentifyRail } from "./identifyrail.js";
import { renderIdentifyWorkspace } from "./identifyworkspace.js";

export function renderIdentify(container) {
  const s = getState();

  // Two hosts, then each half fills its own. Both halves are rendered by a
  // function rather than composed as strings here, because both wire their own
  // handlers and neither should have to hand its markup through a parent.
  container.innerHTML = `
    <div class="workspace">
      <section class="card rail" id="identify-rail"></section>
      <section class="card" id="identify-workspace"></section>
    </div>
  `;

  renderIdentifyRail(container.querySelector("#identify-rail"));
  renderIdentifyWorkspace(container.querySelector("#identify-workspace"), {
    // The hint doubles as the disabled button's tooltip while the review gate
    // is shut: a control that refuses to work must say why in the place the
    // user is already looking, and again where they hover.
    footerHTML: stepFooterHTML({ hint: readyHint(s), nextTitle: gateReason(s) }, s),
  });

  // The footer is the workspace card's foot, so it is wired after that half has
  // rendered its markup.
  wireStepFooter(container);
}

/**
 * readyHint(s) is the footer's sentence, and since BUILD-06 Phase 7 it is the
 * REASON the disabled CONTINUE gives rather than a neutral progress read-out.
 *
 * Two states, because there are two things worth saying:
 *
 *   review outstanding   the move to Anonymise is refused (state.js canGoTo
 *                        rule 2), so the sentence is the refusal: how many are
 *                        waiting and the one action that clears them. It used
 *                        to append the count to a "0 values ready" sentence,
 *                        which narrated the blockage without ever naming it as
 *                        one, and the button beside it simply looked broken.
 *   review done          how much the next step will act on. It counts
 *                        ACCEPTED values rather than suggestions, and "0 values
 *                        ready to replace" is an honest answer, not an empty
 *                        one.
 *
 * @param {object} s state
 * @returns {string}
 */
export function readyHint(s) {
  const gate = gateReason(s);
  if (gate) return gate;
  const accepted = s.entities.filter((e) => e.status === "accepted").length;
  return WORKSPACE.readyToReplace(accepted);
}

/**
 * gateReason(s) is why the move to Anonymise is refused, or "" when it is not.
 *
 * It is derived from the same field the guard reads (s.candidates), rather than
 * re-deriving the guard's answer, so the sentence and the disabled button can
 * never disagree about whether the gate is shut.
 *
 * @param {object} s state
 * @returns {string} the refusal sentence, or "" when nothing is blocking
 */
export function gateReason(s) {
  const waiting = s.candidates.length;
  return waiting > 0 ? WORKSPACE.reviewGate(waiting) : "";
}

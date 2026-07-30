// views/identify.js, wizard step 2: the two-column Identify screen
// (BUILD-05 Phase 2; the rail lands in Phase 5, the workspace in Phase 6).
//
// Identify is one screen with two halves:
//
//   the rail        a third of the width: WHAT to look for (country, preset, the
//                   23 categories, the confidence floor) and HOW (smart
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
    footerHTML: stepFooterHTML({ hint: readyHint(s) }, s),
  });

  // The footer is the workspace card's foot, so it is wired after that half has
  // rendered its markup.
  wireStepFooter(container);
}

/**
 * readyHint(s) is the footer's sentence: how much is waiting to be replaced.
 *
 * It counts ACCEPTED values rather than suggestions, because that is what the
 * next step will act on. A screen full of unreviewed suggestions and no
 * accepted values is not ready, and saying "0 values ready to replace" is the
 * honest way to show it.
 *
 * @param {object} s state
 * @returns {string}
 */
export function readyHint(s) {
  const accepted = s.entities.filter((e) => e.status === "accepted").length;
  const waiting = s.candidates.length;
  const ready = `${accepted} value${accepted === 1 ? "" : "s"} ready to replace`;
  if (!waiting) return ready;
  return `${ready}, ${waiting} suggestion${waiting === 1 ? "" : "s"} still waiting`;
}

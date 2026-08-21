// views/identify.js, wizard step 2: the two-column Identify screen.
//
// Identify is one screen with two halves:
//
//   the rail       a third of the width: WHAT to look for (the document country,
//                  the preset rows, the category groups) and HOW (Built-in
//                  patterns, Heuristic discovery, Local LLM discovery), plus the
//                  Profile panel and the head's Run detection button. It is big
//                  enough to deserve its own file, views/identifyrail.js.
//   the workspace  the other two thirds: the suggestions to review, the values
//                  accepted so far, the never-anonymise list
//                  (views/allowlist.js), the built-in pattern preview and the
//                  custom patterns. views/identifyworkspace.js.
//
// The second half appears only once a detection run has happened, so a fresh
// step 2 is the rail alone: choosing what to look for is a complete task on its
// own, and there is nothing to review until something has looked.
//
// This module owns the layout, the screen's footer and the wiring of the run
// control, because it is the only place that knows about both halves.

import { getState } from "../state.js";
import { WORKSPACE } from "../copy.js";
import { stepFooterHTML, wireStepFooter } from "../nav.js";
import { renderIdentifyRail } from "./identifyrail.js";
import { renderIdentifyWorkspace, landOnResultTab } from "./identifyworkspace.js";
import { wireDetection } from "./detectionrun.js";

export function renderIdentify(container) {
  const s = getState();

  // The REVIEW panel is not rendered until a detection run has happened. Before
  // that it holds four empty tabs and a footer refusing to continue, which reads
  // as a broken screen rather than as "there is nothing here yet"; the rail's Run
  // detection button is the one thing to do, and it is what reveals this half.
  const reviewed = s.detectionRan === true;

  // The grid keeps BOTH its columns while the panel is hidden, so the rail sits in
  // exactly the place it will still occupy once the panel appears: nothing on
  // screen moves when the run finishes. That is also why the hidden state gets no
  // class of its own, since there is no styling to hang on it.
  //
  // Two hosts, then each half fills its own. Both halves are rendered by a
  // function rather than composed as strings here, because both wire their own
  // handlers and neither should have to hand its markup through a parent.
  container.innerHTML = `
    <div class="identify-view">
      <div class="workspace">
        <section class="card rail" id="identify-rail"></section>
        ${reviewed ? `<section class="card" id="identify-workspace"></section>` : ""}
      </div>
      ${reviewed ? "" : stepFooterHTML({ hint: gateReason(s), standalone: true }, s)}
    </div>
  `;

  renderIdentifyRail(container.querySelector("#identify-rail"));
  if (reviewed) {
    renderIdentifyWorkspace(container.querySelector("#identify-workspace"), {
      // The hint doubles as the disabled button's tooltip while the review gate
      // is shut: a control that refuses to work must say why in the place the
      // user is already looking, and again where they hover.
      footerHTML: stepFooterHTML({ hint: gateReason(s), nextTitle: gateReason(s) }, s),
    });
  }

  // The footer is the workspace card's foot once there is a workspace, and a
  // standalone bar under the rail before that, so it is wired after both halves
  // have rendered their markup. The run control is wired from here for the same
  // reason: it is drawn in the rail and it fills the workspace, so this is the
  // only module that can hand one to the other.
  wireStepFooter(container);
  wireDetection(container, { onSettled: landOnResultTab });
}

/**
 * gateReason(s) is why the move to Anonymise is refused, or "" when it is not.
 *
 * It is derived from the same field the guard reads (s.suggestions), rather than
 * re-deriving the guard's answer, so the sentence and the disabled button can
 * never disagree about whether the gate is shut.
 *
 * It is the footer's WHOLE sentence. There is no progress read-out beside it: a
 * count of accepted values narrated the state of the list the user is already
 * looking at, and it was the only thing the footer said whenever nothing was
 * blocking, which made the empty case ("0 values ready to replace") read as a
 * problem. Silence is the honest answer when nothing is in the way.
 *
 * @param {object} s state
 * @returns {string} the refusal sentence, or "" when nothing is blocking
 */
export function gateReason(s) {
  const waiting = s.suggestions.length;
  return waiting > 0 ? WORKSPACE.reviewGate(waiting) : "";
}

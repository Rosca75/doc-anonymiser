// shell.js, the PURE markup builders for the application shell
// (BUILD-04 Phase 2).
//
// main.js used to assemble the header inline, which made two shell rules
// impossible to test and easy to break:
//
//   CR4: the top menu must be IDENTICAL on every screen. The button set
//        never changes; only a quiet highlight moves.
//   CR7: the wizard step indicators must NOT live in the header. They belong
//        to a separate bar rendered under the menu, and only while the wizard
//        is on screen. BUILD-05 Phase 2 relaid that bar out as numbered
//        circles with a back link (stepbarHTML below); the rule is unchanged.
//
// Both are string-rendering functions with no DOM access, in the same
// spirit as ui.js and html.js, so shell.test.js can assert them under
// `node --test`. main.js keeps the wiring (event listeners), these
// functions keep the markup.
//
// The file also owns the one SHELL-LEVEL action, showDocumentation
// (CR6). It lives here rather than in main.js because both the top menu
// and the Home page button need it, and home.js importing main.js would
// make the module graph circular.

import { escapeHTML } from "./html.js";
import { button } from "./ui.js";
import { ICONS } from "./icons.js";
import { WORKFLOW, FOOTER } from "./copy.js";
import { openDocumentation } from "./api.js";
import { setState } from "./state.js";

/**
 * TOPNAV_ITEMS is THE definition of the permanent top menu. It is a
 * constant, not a function of state, which is exactly what CR4 asks for:
 * there is no code path that can add, remove or reorder an entry for a
 * particular screen.
 *
 * `screen` is the value goToScreen() receives for the first two entries.
 * Documentation has no screen: it opens a separate application window
 * (CR6, wired in main.js), so its screen is null and it is never marked
 * as the current location.
 */
export const TOPNAV_ITEMS = [
  { id: "nav-home", label: "Home", screen: "home" },
  { id: "nav-wizard", label: "Anonymise documents", screen: "wizard" },
  { id: "nav-docs", label: "Documentation", screen: null },
];

/**
 * topnavHTML(screen) renders the permanent top menu.
 *
 * Every entry is a plain text link on every screen (BUILD-05 CR3, from the
 * Claude Design mockup): no icon, no button chrome, just the label. The
 * active one gets the quiet `topnav-active` underline and aria-current
 * ="page". No orange: the one loud element per view belongs to the view,
 * not to the chrome (brand rule).
 *
 * @param {string} screen the current top-level screen ("home" | "wizard")
 * @returns {string} safe HTML
 */
export function topnavHTML(screen) {
  const items = TOPNAV_ITEMS.map((item) => {
    const active = item.screen !== null && item.screen === screen;
    return button(item.label, {
      kind: "ghost",
      id: item.id,
      cls: active ? "topnav-active" : "",
      current: active,
    });
  }).join("");
  return `<nav class="topnav" aria-label="Main menu">${items}</nav>`;
}

/**
 * headerActionsHTML(badgesHTML) renders the right-hand cluster of the
 * header: the bridge/Ollama status badges (caller-supplied, since they
 * come from live state main.js owns) followed by the Help and Settings
 * icon buttons from the Claude Design mockup.
 *
 * Settings has no screen to open yet, so its button renders with no click
 * behaviour wired in main.js; it is here for layout only until a settings
 * surface exists. Help reuses showDocumentation, the same action as the
 * Documentation menu entry.
 *
 * @param {string} badgesHTML trusted markup for the status badges
 * @returns {string} safe HTML
 */
export function headerActionsHTML(badgesHTML) {
  return `<div class="header-actions">` +
    `<div class="badges">${badgesHTML}</div>` +
    button("", { kind: "ghost", id: "header-help", icon: "help", cls: "icon-btn", ariaLabel: "Help", title: "Help" }) +
    button("", { kind: "ghost", id: "header-settings", icon: "settings", cls: "icon-btn", ariaLabel: "Settings", title: "Settings" }) +
    `</div>`;
}

/**
 * appFooterHTML() renders the permanent footer strip shown under every
 * screen (BUILD-05 CR2): the app version and the local-processing
 * reassurance with its status dot. Pure markup, no state.
 * @returns {string} safe HTML
 */
export function appFooterHTML() {
  return `<footer class="appfooter">` +
    `<span>${escapeHTML(FOOTER.version)}</span>` +
    `<span class="local-processing">${escapeHTML(FOOTER.localProcessing)}` +
    `<span class="status-dot" aria-hidden="true"></span></span>` +
    `</footer>`;
}

/**
 * stepbarHTML(steps, activeStep, labels, isEnabled) renders the wizard step
 * bar: a "back to the flow" link, a divider, then the numbered step circles
 * (BUILD-04 CR7, relaid out for BUILD-05 Phase 2 to match the mock-ups).
 *
 * It replaced workflowBannerHTML. Three things changed, and each one is
 * carrying its weight:
 *
 *   - The visible "Anonymisation workflow" title is gone. It named the bar
 *     it was already obviously part of; the slot now holds the back link,
 *     which does something. The string survives as the bar's accessible
 *     name, where naming the region is the point.
 *   - A step is a numbered CIRCLE plus its label, not a chip. The number
 *     says where the user is in a sequence, which a row of equal pills
 *     cannot.
 *   - A step BEHIND the active one shows a check mark instead of its number.
 *     That is the only progress indicator in the application, and it is the
 *     reason `activeStep` is compared by index here rather than by identity.
 *
 * The caller passes the guard predicate rather than this function importing
 * state.js, which keeps it pure and lets the test drive every combination.
 *
 * @param {string[]} steps ordered step tokens (state.js WIZARD_STEPS)
 * @param {string} activeStep the step currently on screen
 * @param {Record<string,string>} labels token to visible label map
 * @param {(step: string) => boolean} isEnabled navigation guard
 * @returns {string} safe HTML
 */
export function stepbarHTML(steps, activeStep, labels, isEnabled) {
  const activeIndex = steps.indexOf(activeStep);
  const items = steps.map((step, index) => {
    const active = step === activeStep;
    // "done" is positional, not a record of what the user actually finished:
    // being on step 3 means steps 1 and 2 were passed through. Tracking real
    // completion would need a fifth piece of state that says nothing extra,
    // since the guards already refuse a step whose prerequisites are missing.
    const done = activeIndex >= 0 && index < activeIndex;
    // A disabled step is rendered disabled AND carries the class, so the
    // greyed styling does not depend on the :disabled selector alone.
    const enabled = isEnabled(step);
    const classes = `step-item${active ? " active" : ""}${done ? " done" : ""}` +
      `${enabled ? "" : " disabled"}`;
    // The check mark is decorative: the label beside it already says which
    // step this is, and "check mark Import" read aloud is noise.
    const mark = done
      ? `<span class="icon" aria-hidden="true">${ICONS.check}</span>`
      : escapeHTML(String(index + 1));
    return `<button class="${classes}" data-step="${escapeHTML(step)}"` +
      `${enabled ? "" : " disabled"}${active ? ` aria-current="step"` : ""}>` +
      `<span class="step-circle">${mark}</span>` +
      `<span class="step-label">${escapeHTML(labels[step] ?? step)}</span>` +
      `</button>`;
  }).join("");
  return `<section class="stepbar" aria-label="${escapeHTML(WORKFLOW.title)}">` +
    button(WORKFLOW.backToFlow, {
      kind: "ghost", id: "stepbar-back", icon: "arrow_back",
      cls: "stepbar-home", title: WORKFLOW.backToFlowTitle,
    }) +
    `<span class="stepbar-divider" aria-hidden="true"></span>` +
    `<nav class="steps">${items}</nav>` +
    `</section>`;
}

// --- Shell-level actions ---------------------------------------------------

/**
 * showDocumentation() opens the bundled documentation in its own window
 * (CR6) and records a refusal in state.shellError, which main.js renders
 * as a dismissible banner above the active view.
 *
 * Failing to open the documentation must never break the screen the user
 * is on, hence the state field rather than a thrown error: this is the
 * chrome reporting a chrome problem.
 *
 * @returns {Promise<void>} always resolves; failures land in shellError.
 */
export async function showDocumentation() {
  try {
    await openDocumentation();
    setState({ shellError: null });
  } catch (err) {
    setState({ shellError: String(err?.message ?? err) });
  }
}

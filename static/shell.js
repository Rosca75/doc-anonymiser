// shell.js, the PURE markup builders for the application shell
// (BUILD-04 Phase 2).
//
// main.js used to assemble the header inline, which made two shell rules
// impossible to test and easy to break:
//
//   CR4: the top menu must be IDENTICAL on every screen. The button set
//        never changes; only a quiet highlight moves.
//   CR7: the five wizard step chips must NOT live in the header. They
//        belong to a separate "Anonymisation workflow" banner rendered
//        under the menu, and only while the wizard is on screen.
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
import { WORKFLOW } from "./copy.js";
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
  { id: "nav-home", label: "Home", icon: "home", screen: "home" },
  { id: "nav-wizard", label: "Anonymise documents", icon: "description", screen: "wizard" },
  { id: "nav-docs", label: "Documentation", icon: "menu_book", screen: null },
];

/**
 * topnavHTML(screen) renders the permanent top menu.
 *
 * Every entry is a ghost button on every screen; the active one gets the
 * quiet `topnav-active` class and aria-current="page". No orange: the one
 * loud element per view belongs to the view, not to the chrome
 * (brand rule).
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
      icon: item.icon,
      cls: active ? "topnav-active" : "",
      current: active,
    });
  }).join("");
  return `<nav class="topnav" aria-label="Main menu">${items}</nav>`;
}

/**
 * workflowBannerHTML(steps, activeStep, labels, isEnabled) renders the
 * "Anonymisation workflow" banner: a title plus the ordered step chips
 * (CR7). The caller passes the guard predicate rather than the banner
 * importing state.js, which keeps this function pure and lets the test
 * drive every enabled/disabled combination.
 *
 * @param {string[]} steps ordered step tokens (state.js WIZARD_STEPS)
 * @param {string} activeStep the step currently on screen
 * @param {Record<string,string>} labels token to visible label map
 * @param {(step: string) => boolean} isEnabled navigation guard
 * @returns {string} safe HTML
 */
export function workflowBannerHTML(steps, activeStep, labels, isEnabled) {
  const chips = steps.map((step) => {
    const active = step === activeStep;
    // A disabled chip is rendered disabled AND carries the class, so the
    // greyed styling does not depend on the :disabled selector alone.
    const enabled = isEnabled(step);
    const classes = `chip${active ? " active" : ""}${enabled ? "" : " disabled"}`;
    return `<button class="${classes}" data-step="${escapeHTML(step)}"` +
      `${enabled ? "" : " disabled"}${active ? ` aria-current="step"` : ""}>` +
      `${escapeHTML(labels[step] ?? step)}</button>`;
  }).join("");
  return `<section class="workflow-banner" aria-label="${escapeHTML(WORKFLOW.title)}">` +
    `<span class="workflow-title">${escapeHTML(WORKFLOW.title)}</span>` +
    `<nav class="workflow-steps">${chips}</nav>` +
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

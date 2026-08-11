// views/home.js, the Welcome/Home landing page (,
// rewritten for, two-column layout added in).
//
// Helvetica headline, the three-paragraph body from copy.js, and exactly
// ONE orange hero element (the primary "Anonymise documents" button, per
// the brand one-loud-element rule). A sidebar next to the hero walks
// through the five wizard steps in plain language, for a first-time user
// deciding whether to start. Renders from copy.js strings; the only
// actions it dispatches are the screen change and the documentation window.

import { goToScreen } from "../state.js";
import { showDocumentation } from "../shell.js";
import { escapeHTML } from "../html.js";
import { button, icon } from "../ui.js";
import { HOME } from "../copy.js";

export function renderHome(container) {
  // HOME.body is an array of paragraphs. Mapping over it, rather
  // than rendering one fixed lede, means copy.js alone decides how many
  // paragraphs the landing page has.
  const body = HOME.body
    .map((paragraph) => `<p class="home-lede">${escapeHTML(paragraph)}</p>`)
    .join("");

  const steps = HOME.steps
    .map((step, i) => `
      <li class="home-step">
        <span class="home-step-check" aria-hidden="true"></span>
        <div>
          <div class="home-step-label"><strong>${i + 1}.</strong> ${escapeHTML(step.label)}</div>
          <div class="home-step-body">${escapeHTML(step.body)}</div>
        </div>
      </li>`)
    .join("");

  container.innerHTML = `
    <div class="home-view">
      <div class="home-main">
        <section class="home-hero">
          <h1>${escapeHTML(HOME.headline)}</h1>
          ${body}
          <div class="home-actions">
            ${button("Anonymise documents", { kind: "primary", id: "home-start", icon: "play_arrow" })}
            ${button(HOME.docsLink, { kind: "ghost", id: "home-docs", cls: "home-docs-link" })}
          </div>
        </section>
        <div class="home-badges" aria-hidden="true">
          ${icon("security")}${icon("visibility_off")}${icon("verified_user")}
        </div>
      </div>
      <aside class="home-steps" aria-label="${escapeHTML(HOME.stepsTitle)}">
        <div class="home-steps-title">${escapeHTML(HOME.stepsTitle)}</div>
        <ol class="home-steps-list">${steps}</ol>
      </aside>
    </div>
  `;

  container.querySelector("#home-start").addEventListener("click", () => goToScreen("wizard"));
  // Same behaviour as the menu entry: the documentation opens in its own
  // window, and a refusal shows in the shell banner.
  container.querySelector("#home-docs").addEventListener("click", showDocumentation);
}

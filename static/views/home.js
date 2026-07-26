// views/home.js, the Welcome/Home landing page (BUILD-02 Phase 2b,
// rewritten for BUILD-04 CR1).
//
// Helvetica headline, the three-paragraph body from copy.js, and exactly
// ONE orange hero element (the primary "Anonymise documents" button, per
// the brand one-loud-element rule). Renders from copy.js strings; the only
// actions it dispatches are the screen change and the documentation window.

import { goToScreen } from "../state.js";
import { escapeHTML } from "../html.js";
import { button } from "../ui.js";
import { HOME } from "../copy.js";

export function renderHome(container) {
  // HOME.body is an array of paragraphs (CR1). Mapping over it, rather
  // than rendering one fixed lede, means copy.js alone decides how many
  // paragraphs the landing page has.
  const body = HOME.body
    .map((paragraph) => `<p class="home-lede">${escapeHTML(paragraph)}</p>`)
    .join("");

  container.innerHTML = `
    <div class="home-view">
      <h1>${escapeHTML(HOME.headline)}</h1>
      ${body}
      <div class="home-actions">
        ${button("Anonymise documents", { kind: "primary", id: "home-start", icon: "play_arrow" })}
        ${button("Documentation", { kind: "secondary", id: "home-docs", icon: "menu_book" })}
      </div>
    </div>
  `;

  container.querySelector("#home-start").addEventListener("click", () => goToScreen("wizard"));
  container.querySelector("#home-docs").addEventListener("click", () => goToScreen("docs"));
}

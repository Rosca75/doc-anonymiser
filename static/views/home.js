// views/home.js, the Welcome/Home landing page (BUILD-02 Phase 2b).
//
// Georgia headline, three short feature panels, and exactly ONE orange
// hero element (the primary "Anonymise documents" button, per the brand
// one-loud-element rule). Renders from copy.js strings; dispatches only
// goToScreen.

import { goToScreen } from "../state.js";
import { escapeHTML } from "../html.js";
import { button, icon } from "../ui.js";
import { HOME } from "../copy.js";

export function renderHome(container) {
  const panels = HOME.panels.map((p) => `
    <section class="panel">
      <div class="panel-head"><h2>${icon(p.icon)} ${escapeHTML(p.title)}</h2></div>
      <p>${escapeHTML(p.body)}</p>
    </section>`).join("");

  container.innerHTML = `
    <div class="home-view">
      <h1>${escapeHTML(HOME.headline)}</h1>
      <p class="home-lede">${escapeHTML(HOME.lede)}</p>
      <div class="home-panels">${panels}</div>
      <div class="home-actions">
        ${button("Anonymise documents", { kind: "primary", id: "home-start", icon: "play_arrow" })}
        ${button("Documentation", { kind: "secondary", id: "home-docs", icon: "menu_book" })}
      </div>
    </div>
  `;

  container.querySelector("#home-start").addEventListener("click", () => goToScreen("wizard"));
  container.querySelector("#home-docs").addEventListener("click", () => goToScreen("docs"));
}

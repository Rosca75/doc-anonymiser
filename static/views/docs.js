// views/docs.js, the documentation placeholder page (BUILD-02 Phase 2c).
// Deliberately content-free for now, per the improvement plan; it exists
// so the navigation shell is complete.

import { goToScreen } from "../state.js";
import { escapeHTML } from "../html.js";
import { button } from "../ui.js";
import { DOCS_PLACEHOLDER } from "../copy.js";

export function renderDocs(container) {
  container.innerHTML = `
    <div class="home-view">
      <h1>Documentation</h1>
      <p class="home-lede">${escapeHTML(DOCS_PLACEHOLDER)}</p>
      <div class="home-actions">
        ${button("Back to home", { kind: "secondary", id: "docs-back", icon: "arrow_back" })}
      </div>
    </div>
  `;
  container.querySelector("#docs-back").addEventListener("click", () => goToScreen("home"));
}

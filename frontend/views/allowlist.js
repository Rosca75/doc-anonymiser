// views/allowlist.js, the SHARED allowlist editor panel (BUILD-02
// Phase 6c). Both the Configure step and the Values step render this
// one component over the one state.allowlist list, so the user always
// sees a single consistent allowlist.
//
// renderAllowlistPanel returns the panel HTML; wireAllowlistPanel
// attaches the handlers after the view sets innerHTML.

import { importAllowlistCSV, saveAllowlistTemplate } from "../api.js";
import { getState, addAllowTerm, removeAllowTerm, clearAllowlist } from "../state.js";
import { escapeHTML } from "../html.js";
import { panel, button } from "../ui.js";
import { CONFIGURE } from "../copy.js";
import { keepScrollPosition } from "../scroll.js";

/**
 * renderAllowlistPanel(s, collapsedSet) returns the allowlist panel HTML.
 * @param {object} s current state
 * @param {Set<string>} collapsedSet the view's collapsed-panel set
 */
export function renderAllowlistPanel(s, collapsedSet) {
  const pills = s.allowlist.map((t) => `
    <span class="pill">${escapeHTML(t)}<button class="allow-del" data-term="${escapeHTML(t)}" title="Remove">✕</button></span>`).join("");
  const content = `
      <p class="hint">${escapeHTML(CONFIGURE.allowHint)}</p>
      <div class="pill-list">${pills || `<span class="hint">empty</span>`}</div>
      <div class="form-row">
        <input id="allow-input" placeholder="add a term, e.g. CSSF"/>
        <button id="allow-add">Add</button>
        ${button("Import CSV", { id: "allow-import", icon: "upload_file" })}
        ${button("Download template", { id: "allow-template", icon: "download" })}
      </div>
      <div id="allow-feedback" class="hint"></div>`;
  // "Clear all" sits in the panel header next to the collapse chevron
  // (BUILD-04 CR11). It is disabled on an empty list so the button never
  // looks like it did nothing.
  const clearButton = button(CONFIGURE.clearAll, {
    kind: "ghost", id: "allow-clear", icon: "delete",
    disabled: s.allowlist.length === 0,
    title: s.allowlist.length === 0
      ? "The list is already empty."
      : `Remove all ${s.allowlist.length} terms from this list.`,
  });
  return panel("allow-panel", CONFIGURE.allowTitle, content, {
    collapsible: true, collapsedSet, headExtraHTML: clearButton,
  });
}

/**
 * wireAllowlistPanel(container) attaches add/remove/import/template
 * handlers. Errors surface inline in the panel, never silently.
 */
export function wireAllowlistPanel(container) {
  const root = container.querySelector("#allow-panel");
  if (!root) return;
  const feedback = root.querySelector("#allow-feedback");
  const input = root.querySelector("#allow-input");

  // Every mutation here re-renders the whole view, so each one is wrapped
  // to carry the scroll position across (BUILD-04 CR12): this panel is
  // rendered near the BOTTOM of both the Configure and the Values screen,
  // which is exactly where a jump to the top hurts most.
  const add = () => keepScrollPosition(() => addAllowTerm(input.value));
  root.querySelector("#allow-add").addEventListener("click", add);
  input.addEventListener("keydown", (ev) => { if (ev.key === "Enter") add(); });

  for (const btn of root.querySelectorAll(".allow-del")) {
    btn.addEventListener("click", () => keepScrollPosition(() => removeAllowTerm(btn.dataset.term)));
  }

  // Clear all (BUILD-04 CR11). Emptying a list the user has curated, and
  // which protects terms from every pass, is worth one confirmation.
  root.querySelector("#allow-clear")?.addEventListener("click", (ev) => {
    ev.stopPropagation(); // the header row is also the collapse toggle
    if (!confirm(CONFIGURE.clearAllConfirm)) return;
    keepScrollPosition(() => {
      const removed = clearAllowlist();
      feedback.textContent = `${removed} term(s) removed.`;
    });
  });

  root.querySelector("#allow-import").addEventListener("click", async () => {
    try {
      const terms = await importAllowlistCSV();
      if (!terms) return; // dialog cancelled
      const before = getState().allowlist.length;
      keepScrollPosition(() => { for (const t of terms) addAllowTerm(t); });
      const added = getState().allowlist.length - before;
      feedback.textContent = `${terms.length} term(s) read, ${added} new added.`;
    } catch (err) {
      feedback.textContent = String(err?.message ?? err);
    }
  });

  root.querySelector("#allow-template").addEventListener("click", async () => {
    try {
      await saveAllowlistTemplate();
    } catch (err) {
      feedback.textContent = String(err?.message ?? err);
    }
  });
}

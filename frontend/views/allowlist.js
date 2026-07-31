// views/allowlist.js, the never-anonymise editor (BUILD-02 Phase 6c; reflowed
// into the mock-up's chip layout by BUILD-05 Phase 6).
//
// It is one component over the one state.allowlist list, so the user always sees
// a single consistent allowlist. Since BUILD-05 it has exactly ONE home, the
// "Never anonymise" tab of the Identify workspace: it was rendered on two
// screens before, which is why it was written as a shared panel, and it stays a
// separate module because it is a self-contained editor with its own bridge
// calls (CSV import, template download).
//
// The layout is now an add row plus a box of removable chips, rather than a
// collapsible panel with a pill list. Two consequences worth knowing:
//
//   The "Clear all" bulk button is gone. It lived in the panel header, which no
//   longer exists, and it was the last native confirm() on this screen
//   (decision 10). Removing terms one chip at a time is the ordinary case, and
//   a list of ten seeded terms is not a list anyone needs to empty in one go.
//   The engine defaults are still seeded at startup and still removable
//   individually.
//
//   renderAllowlistChips takes the draft text rather than reading an input,
//   because the tab it lives in repaints on every keystroke elsewhere and a
//   half-typed term must survive that.

import { importAllowlistCSV, saveAllowlistTemplate } from "../api.js";
import { getState, addAllowTerm, removeAllowTerm } from "../state.js";
import { escapeHTML } from "../html.js";
import { button } from "../ui.js";
import { notify } from "../toast.js";
import { CONFIGURE, ALLOWLIST } from "../copy.js";
import { keepScrollPosition } from "../scroll.js";

/**
 * renderAllowlistChips(s, draft) returns the "Never anonymise" tab's markup.
 * @param {object} s current state
 * @param {string} draft the half-typed term, kept by the caller across repaints
 * @returns {string} safe HTML
 */
export function renderAllowlistChips(s, draft = "") {
  const chips = s.allowlist.map((term) =>
    `<span class="chip-tag">${escapeHTML(term)}` +
    button("", {
      kind: "ghost", cls: "chip-remove allow-del", icon: "close",
      ariaLabel: `Remove ${term}`, title: ALLOWLIST.remove,
      data: { term },
    }) +
    `</span>`).join("");

  return `<p class="hint">${escapeHTML(CONFIGURE.allowHint)}</p>` +
    `<div class="add-row">` +
    `<input id="allow-draft" class="grow" value="${escapeHTML(draft)}"` +
    ` placeholder="${escapeHTML(ALLOWLIST.placeholder)}"` +
    ` aria-label="${escapeHTML(ALLOWLIST.label)}"/>` +
    button(ALLOWLIST.add, { kind: "secondary", id: "allow-add" }) +
    button(ALLOWLIST.importCSV, { kind: "secondary", id: "allow-import", icon: "upload_file" }) +
    button(ALLOWLIST.template, { kind: "secondary", id: "allow-template", icon: "download" }) +
    `</div>` +
    `<div class="chip-box">` +
    (chips || `<span class="hint">${escapeHTML(ALLOWLIST.empty)}</span>`) +
    `</div>`;
}

/**
 * wireAllowlistChips(container, drafts) attaches add / remove / import /
 * template. Failures surface as a notice, never silently.
 *
 * @param {HTMLElement} container the element the view just filled
 * @param {object} drafts the caller's draft store; `drafts.allow` is updated as
 *   the user types so a repaint does not empty the field
 */
export function wireAllowlistChips(container, drafts = {}) {
  const input = container.querySelector("#allow-draft");
  if (!input) return;

  input.addEventListener("input", () => { drafts.allow = input.value; });

  // Every mutation re-renders the whole screen, so each one carries the scroll
  // position across (BUILD-04 CR12): this tab can be a long list of chips, and a
  // jump to the top after removing one is exactly where it hurts.
  const add = () => {
    const term = (input.value ?? "").trim();
    if (!term) return;
    const before = getState().allowlist.length;
    drafts.allow = "";
    keepScrollPosition(() => addAllowTerm(term));
    if (getState().allowlist.length === before) notify(ALLOWLIST.alreadyThere(term), "info");
  };
  container.querySelector("#allow-add")?.addEventListener("click", add);
  input.addEventListener("keydown", (ev) => { if (ev.key === "Enter") add(); });

  for (const btn of container.querySelectorAll(".allow-del")) {
    btn.addEventListener("click", () => {
      keepScrollPosition(() => removeAllowTerm(btn.dataset.term));
    });
  }

  container.querySelector("#allow-import")?.addEventListener("click", async () => {
    try {
      const terms = await importAllowlistCSV();
      if (!terms) return; // dialog cancelled: nothing happened, say nothing
      const before = getState().allowlist.length;
      keepScrollPosition(() => { for (const term of terms) addAllowTerm(term); });
      const added = getState().allowlist.length - before;
      // Both numbers, because "12 read, 3 added" is the answer to the question a
      // user actually has after importing a list they already partly had.
      notify(ALLOWLIST.imported(terms.length, added), added ? "ok" : "info");
    } catch (err) {
      notify(String(err?.message ?? err), "warn");
    }
  });

  container.querySelector("#allow-template")?.addEventListener("click", async () => {
    try {
      await saveAllowlistTemplate();
      notify(ALLOWLIST.templateSaved, "ok");
    } catch (err) {
      notify(String(err?.message ?? err), "warn");
    }
  });
}

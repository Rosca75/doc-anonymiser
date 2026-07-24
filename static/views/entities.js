// views/entities.js, wizard step 3: entity discovery, review, allowlist
// and custom patterns (Phase 7). This screen is the heart of the UX and is
// FULLY usable without Ollama: the add-item rows are the whole flow in
// manual mode; only the discovery trigger is LLM-gated.
//
// Render-from-state discipline: every mutation goes through a state.js
// reducer; every Go call goes through api.js.

import {
  runDiscovery, cancelDiscovery, estimateDiscovery,
  expandVariants, validatePattern, patternMatches,
} from "../api.js";
import {
  getState, setState, llmEnabled,
  addEntities, setEntityStatus, editEntity, removeEntity,
  setEntityVariants, setEntityVariantError, addManualVariant, entityKey,
  addPattern, removePattern,
} from "../state.js";
import { variantRows, pendingExpansions } from "../entitymodel.js";
import { escapeHTML } from "../html.js";
import { panel, wirePanels } from "../ui.js";
import { llmGateTooltip } from "./configure.js";
import { renderAllowlistPanel, wireAllowlistPanel } from "./allowlist.js";

// The reviewable entity categories (CLAUDE.md §5) with display labels.
const CATEGORIES = [
  ["client_names", "Clients"],
  ["project_names", "Projects"],
  ["internal_names", "Internal"],
  ["person_names", "Persons"],
];

// Rows whose variant list is expanded, keyed by entityKey. View-local UI
// state (not business state), so it lives here, not in the store.
const expanded = new Set();

// Panels the user toggled away from their default open/collapsed state
// (BUILD-02 Phase 2f). View-local, same pattern as `expanded`.
const collapsedPanels = new Set();

export function renderEntities(container) {
  const s = getState();
  // Discovery gates on the master AI toggle AND live availability
  // (BUILD-02 Phase 6d).
  const aiOK = llmEnabled(s);

  container.innerHTML = `
    <div class="entities-view">
      ${discoveryPanel(s, aiOK)}
      ${CATEGORIES.map(([cat, label]) => categoryPanel(s, cat, label)).join("")}
      ${renderAllowlistPanel(s, collapsedPanels)}
      ${patternsPanel(s)}
      <div id="entities-error"></div>
    </div>
  `;

  wirePanels(container, collapsedPanels, () => setState({}));
  wireDiscovery(container, s);
  wireCategoryPanels(container);
  wireAllowlistPanel(container);
  wirePatterns(container);
}

// --- Discovery ---------------------------------------------------------

function discoveryPanel(s, aiOK) {
  const fileOptions = s.documents.map((d) => `
    <label class="radio-row">
      <input type="checkbox" class="disc-file" value="${escapeHTML(d.name)}"/>
      <span>${escapeHTML(d.name)}</span>
    </label>`).join("");
  const gate = aiOK ? "" : `disabled title="${escapeHTML(llmGateTooltip(s))}"`;

  // Determinate per-file progress + Cancel while a run is live
  // (BUILD-02 Phase 7c/7d); the bar reuses the run step's component.
  const d = s.discovery;
  const pct = d?.running && d.total ? Math.round(((d.current + 1) / d.total) * 100) : 0;
  const progressHTML = d?.running ? `
      <div class="progress-bar"><div style="width:${pct}%"></div></div>
      <p class="hint">Scanning ${escapeHTML(d.file ?? "")} (${d.current + 1}/${d.total})
        <button id="btn-disc-cancel">Cancel</button></p>` : "";

  const content = `
      <p class="hint">Pick one or more representative files; the local model proposes entities for review below.
        Without Ollama, add entities manually in the tables; that is a fully supported flow.</p>
      ${fileOptions || `<p class="hint">No documents imported.</p>`}
      ${progressHTML}
      <div id="disc-progress" class="hint"></div>`;
  return panel("panel-discovery", "Entity discovery (AI)", content, {
    collapsible: true, collapsedSet: collapsedPanels,
    headExtraHTML: `<button id="btn-discover" class="primary" ${gate} ${d?.running ? "disabled" : ""}>Run discovery</button>`,
  });
}

function wireDiscovery(container, s) {
  container.querySelector("#btn-disc-cancel")?.addEventListener("click", () => cancelDiscovery());

  const btn = container.querySelector("#btn-discover");
  if (!btn || btn.disabled) return;
  btn.addEventListener("click", async () => {
    let files = [...container.querySelectorAll(".disc-file:checked")].map((c) => c.value);
    const progress = container.querySelector("#disc-progress");
    if (files.length === 0) {
      progress.textContent = "Select at least one file first.";
      return;
    }

    // Pre-run size safeguard (BUILD-02 Phase 7e): oversized files are
    // excluded with the actionable message, never a mid-run hard error.
    try {
      const estimates = await estimateDiscovery(files);
      const tooLarge = (estimates ?? []).filter((e) => e.tooLarge);
      if (tooLarge.length) {
        progress.textContent = tooLarge.map((e) => e.message).join(" ");
        const excluded = new Set(tooLarge.map((e) => e.name));
        files = files.filter((f) => !excluded.has(f));
        if (files.length === 0) return;
      }
    } catch { /* estimation is advisory; the run still guards itself */ }

    setState({ discovery: { running: true, current: 0, total: files.length, file: files[0] } });
    try {
      const res = await runDiscovery(files, s.allowlist);
      const proposals = res?.proposals ?? [];
      const added = addEntities(proposals.map((p) => ({ category: p.category, canonical: p.text })));
      const status = res?.status ? `${res.status}. ` : "";
      setState({ discovery: null });
      getState(); // repaint happened; write the summary into the fresh DOM
      const slot = document.querySelector("#disc-progress");
      if (slot) slot.textContent = `${status}${proposals.length} proposal(s), ${added} new. Review them below.`;
      await refreshVariants();
    } catch (err) {
      setState({ discovery: null });
      showError(container, err);
    }
  });
}

// --- Review tables ------------------------------------------------------

function categoryPanel(s, category, label) {
  // Variant row content comes from the TESTED pure view-model
  // (entitymodel.js): pending / error / empty / list are explicit states,
  // so the "expanding" placeholder can never stick forever (Phase 7a).
  const vrowsByKey = new Map(
    variantRows(s.entities, expanded).map((r) => [r.key, r]));

  const rows = s.entities.filter((e) => e.category === category).map((e) => {
    const key = entityKey(e.category, e.canonical);
    const vrow = vrowsByKey.get(key);
    const isOpen = !!vrow;
    const variantCount = e.variants?.length;
    const countLabel = e.variants === null || e.variants === undefined
      ? "…" : `${variantCount} variant${variantCount === 1 ? "" : "s"}`;

    let variantBody = "";
    if (vrow) {
      const inner =
        vrow.state === "pending" ? `<span class="hint">expanding…</span>` :
        vrow.state === "error" ? `<span class="banner error">Variant expansion failed: ${escapeHTML(vrow.error)}</span>` :
        vrow.state === "empty" ? `<span class="hint">No variants</span>` :
        vrow.variants.map(escapeHTML).join(" · ");
      // The variant row carries ITS OWN data attributes; the old
      // previousElementSibling coupling broke silently on any markup
      // change between the rows (the Phase 7a bug).
      variantBody = `
      <tr class="variant-row" data-key="${escapeHTML(key)}"
          data-category="${escapeHTML(e.category)}" data-canonical="${escapeHTML(e.canonical)}">
        <td colspan="3">
        <div class="variant-list">${inner}</div>
        <div class="form-row">
          <input class="variant-input" placeholder="add a manual variant (e.g. a nickname)"/>
          <button class="variant-add">Add variant</button>
        </div>
      </td></tr>`;
    }

    return `
      <tr class="${e.status === "denied" ? "denied" : ""}" data-key="${escapeHTML(key)}"
          data-category="${escapeHTML(e.category)}" data-canonical="${escapeHTML(e.canonical)}">
        <td class="ent-name">${escapeHTML(e.canonical)}</td>
        <td>
          <button class="ent-variants" title="Show the name variants that will be replaced">
            ${countLabel} ${isOpen ? "▾" : "▸"}
          </button>
        </td>
        <td class="ent-actions">
          <button class="ent-accept" ${e.status === "accepted" ? "disabled" : ""}>accept</button>
          <button class="ent-deny" ${e.status === "denied" ? "disabled" : ""}>deny</button>
          <button class="ent-edit">edit</button>
          <button class="ent-delete">✕</button>
        </td>
      </tr>${variantBody}`;
  }).join("");

  const content = `
    <div data-panel="${category}">
      <table class="entity-table">
        <tbody>${rows}</tbody>
        <tfoot><tr>
          <td colspan="3" class="form-row">
            <input class="ent-add-input" placeholder="add a ${label.toLowerCase()} entry"/>
            <button class="ent-add">Add</button>
          </td>
        </tr></tfoot>
      </table>
    </div>`;
  return panel(`panel-cat-${category}`, label, content, {
    collapsible: true, collapsedSet: collapsedPanels,
  });
}

function wireCategoryPanels(container) {
  for (const panel of container.querySelectorAll("[data-panel]")) {
    const category = panel.dataset.panel;

    // Manual add row, the whole flow in no-Ollama mode.
    const addBtn = panel.querySelector(".ent-add");
    const addInput = panel.querySelector(".ent-add-input");
    const add = async () => {
      if (addEntities([{ category, canonical: addInput.value }]) > 0) await refreshVariants();
    };
    addBtn.addEventListener("click", add);
    addInput.addEventListener("keydown", (ev) => { if (ev.key === "Enter") add(); });

    for (const row of panel.querySelectorAll("tr[data-key]")) {
      const { category: cat, canonical } = row.dataset;
      row.querySelector(".ent-accept").addEventListener("click", () => setEntityStatus(cat, canonical, "accepted"));
      row.querySelector(".ent-deny").addEventListener("click", () => setEntityStatus(cat, canonical, "denied"));
      row.querySelector(".ent-delete").addEventListener("click", () => removeEntity(cat, canonical));
      row.querySelector(".ent-edit").addEventListener("click", async () => {
        const next = prompt("Edit entity name:", canonical);
        if (next !== null && editEntity(cat, canonical, next)) await refreshVariants();
      });
      row.querySelector(".ent-variants").addEventListener("click", async () => {
        const key = row.dataset.key;
        if (expanded.has(key)) expanded.delete(key); else expanded.add(key);
        await refreshVariants(); // ensures lists exist, re-renders via state
      });
    }

    for (const vrow of panel.querySelectorAll(".variant-row")) {
      // Explicit data attributes ON the variant row itself (Phase 7a);
      // no positional coupling to the entity row above.
      const { category: cat, canonical } = vrow.dataset;
      const input = vrow.querySelector(".variant-input");
      vrow.querySelector(".variant-add").addEventListener("click", async () => {
        addManualVariant(cat, canonical, input.value);
        await refreshVariants();
      });
    }
  }
}

/**
 * refreshVariants() asks Go to expand every entity whose variants are
 * PENDING (variants === null: just added, edited, or variant-amended).
 * Settled rows, including "expanded, none found" ([]), are never
 * re-expanded (Phase 7a). Failures surface as a visible inline message
 * with the Go error text instead of being swallowed. Sequential on
 * purpose: the lists are tiny and ordering keeps the UI deterministic.
 */
async function refreshVariants() {
  for (const e of pendingExpansions(getState().entities)) {
    try {
      const variants = await expandVariants({
        category: e.category, canonical: e.canonical, manualVariants: e.manualVariants,
      });
      setEntityVariants(e.category, e.canonical, variants ?? []);
    } catch (err) {
      setEntityVariantError(e.category, e.canonical, String(err?.message ?? err));
    }
  }
}

// --- Custom patterns -------------------------------------------------------

function patternsPanel(s) {
  const rows = s.patterns.map((p) => `
    <span class="pill ${p.error ? "warn" : ""}" title="${escapeHTML(p.error ?? "valid")}">
      <code>${escapeHTML(p.expr)}</code>
      <button class="pattern-del" data-expr="${escapeHTML(p.expr)}" title="Remove">✕</button>
    </span>`).join("");
  const content = `
      <p class="hint">User-defined regular expressions, replaced as [CUSTOM_N]. Validated as you type;
        the tester shows sample matches from the loaded documents.</p>
      <div class="pill-list">${rows || `<span class="hint">none</span>`}</div>
      <div class="form-row">
        <input id="pattern-input" placeholder="e.g. PRJ-[0-9]+" spellcheck="false"/>
        <button id="pattern-test">test</button>
        <button id="pattern-add">Add</button>
      </div>
      <div id="pattern-feedback" class="hint"></div>`;
  // Custom patterns are an advanced feature: collapsed by default
  // (BUILD-02 Phase 2f).
  return panel("pattern-panel", "Custom patterns (regex)", content, {
    collapsible: true, startOpen: false, collapsedSet: collapsedPanels,
  });
}

function wirePatterns(container) {
  const input = container.querySelector("#pattern-input");
  const feedback = container.querySelector("#pattern-feedback");

  // Live compile validation on every keystroke (cheap round-trip).
  input.addEventListener("input", async () => {
    const expr = input.value.trim();
    feedback.textContent = expr ? (await validatePattern(expr)) || "✓ pattern compiles" : "";
  });

  container.querySelector("#pattern-test").addEventListener("click", async () => {
    try {
      const samples = await patternMatches(input.value.trim());
      feedback.textContent = samples?.length
        ? `matches: ${samples.join(", ")}`
        : "no matches in the loaded documents";
    } catch (err) {
      feedback.textContent = String(err?.message ?? err);
    }
  });

  container.querySelector("#pattern-add").addEventListener("click", async () => {
    const expr = input.value.trim();
    if (!expr) return;
    const error = await validatePattern(expr);
    if (error) {
      feedback.textContent = error; // invalid patterns are not added
      return;
    }
    addPattern(expr, null);
  });

  for (const btn of container.querySelectorAll(".pattern-del")) {
    btn.addEventListener("click", () => removePattern(btn.dataset.expr));
  }
}

function showError(container, err) {
  container.querySelector("#entities-error").innerHTML =
    `<div class="banner error">${escapeHTML(String(err?.message ?? err))}</div>`;
}

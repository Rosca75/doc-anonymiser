// views/entities.js, wizard step 3: entity discovery, review, allowlist
// and custom patterns (Phase 7). This screen is the heart of the UX and is
// FULLY usable without Ollama: the add-item rows are the whole flow in
// manual mode; only the discovery trigger is LLM-gated.
//
// Render-from-state discipline: every mutation goes through a state.js
// reducer; every Go call goes through api.js.

import {
  runDiscovery, expandVariants, validatePattern, patternMatches,
} from "../api.js";
import {
  getState,
  addEntities, setEntityStatus, editEntity, removeEntity,
  setEntityVariants, addManualVariant, entityKey,
  addAllowTerm, removeAllowTerm, addPattern, removePattern,
} from "../state.js";
import { escapeHTML } from "../html.js";
import { LLM_DISABLED_TOOLTIP } from "./configure.js";

// The reviewable entity categories (CLAUDE.md §5) with display labels.
const CATEGORIES = [
  ["client_names", "Clients"],
  ["project_names", "Projects"],
  ["pwc_internal_names", "PwC internal"],
  ["person_names", "Persons"],
];

// Rows whose variant list is expanded, keyed by entityKey. View-local UI
// state (not business state), so it lives here, not in the store.
const expanded = new Set();

export function renderEntities(container) {
  const s = getState();
  const ollamaOK = !!s.ollama?.available;

  container.innerHTML = `
    <div class="entities-view">
      ${discoveryPanel(s, ollamaOK)}
      ${CATEGORIES.map(([cat, label]) => categoryPanel(s, cat, label)).join("")}
      ${allowlistPanel(s)}
      ${patternsPanel(s)}
      <div id="entities-error"></div>
    </div>
  `;

  wireDiscovery(container, s);
  wireCategoryPanels(container);
  wireAllowlist(container);
  wirePatterns(container);
}

// --- Discovery ---------------------------------------------------------

function discoveryPanel(s, ollamaOK) {
  const fileOptions = s.documents.map((d) => `
    <label class="radio-row">
      <input type="checkbox" class="disc-file" value="${escapeHTML(d.name)}"/>
      <span>${escapeHTML(d.name)}</span>
    </label>`).join("");
  const gate = ollamaOK ? "" : `disabled title="${LLM_DISABLED_TOOLTIP}"`;
  return `
    <section class="panel">
      <div class="panel-head">
        <h2>Entity discovery (AI)</h2>
        <button id="btn-discover" class="primary" ${gate}>Run discovery</button>
      </div>
      <p class="hint">Pick one or more representative files; the local model proposes entities for review below.
        Without Ollama, add entities manually in the tables; that is a fully supported flow.</p>
      ${fileOptions || `<p class="hint">No documents imported.</p>`}
      <div id="disc-progress" class="hint"></div>
    </section>`;
}

function wireDiscovery(container, s) {
  const btn = container.querySelector("#btn-discover");
  if (!btn || btn.disabled) return;
  btn.addEventListener("click", async () => {
    const files = [...container.querySelectorAll(".disc-file:checked")].map((c) => c.value);
    const progress = container.querySelector("#disc-progress");
    if (files.length === 0) {
      progress.textContent = "Select at least one file first.";
      return;
    }
    btn.disabled = true;
    progress.textContent = "Scanning…";
    try {
      const proposals = await runDiscovery(files, s.allowlist);
      const added = addEntities((proposals ?? []).map((p) => ({ category: p.category, canonical: p.text })));
      progress.textContent = `${proposals?.length ?? 0} proposal(s), ${added} new. Review them below.`;
      await refreshVariants();
    } catch (err) {
      showError(container, err);
      progress.textContent = "";
    } finally {
      btn.disabled = false;
    }
  });
}

// --- Review tables ------------------------------------------------------

function categoryPanel(s, category, label) {
  const rows = s.entities.filter((e) => e.category === category).map((e) => {
    const key = entityKey(e.category, e.canonical);
    const isOpen = expanded.has(key);
    const variantCount = e.variants?.length ?? 0;
    return `
      <tr class="${e.status === "denied" ? "denied" : ""}" data-key="${escapeHTML(key)}"
          data-category="${escapeHTML(e.category)}" data-canonical="${escapeHTML(e.canonical)}">
        <td class="ent-name">${escapeHTML(e.canonical)}</td>
        <td>
          <button class="ent-variants" title="Show the name variants that will be replaced">
            ${variantCount || "…"} variant${variantCount === 1 ? "" : "s"} ${isOpen ? "▾" : "▸"}
          </button>
        </td>
        <td class="ent-actions">
          <button class="ent-accept" ${e.status === "accepted" ? "disabled" : ""}>accept</button>
          <button class="ent-deny" ${e.status === "denied" ? "disabled" : ""}>deny</button>
          <button class="ent-edit">edit</button>
          <button class="ent-delete">✕</button>
        </td>
      </tr>
      ${isOpen ? `
      <tr class="variant-row"><td colspan="3">
        <div class="variant-list">${(e.variants ?? []).map(escapeHTML).join(" · ") || "expanding…"}</div>
        <div class="form-row">
          <input class="variant-input" placeholder="add a manual variant (e.g. a nickname)"/>
          <button class="variant-add">add variant</button>
        </div>
      </td></tr>` : ""}`;
  }).join("");

  return `
    <section class="panel" data-panel="${category}">
      <div class="panel-head"><h2>${label}</h2></div>
      <table class="entity-table">
        <tbody>${rows}</tbody>
        <tfoot><tr>
          <td colspan="3" class="form-row">
            <input class="ent-add-input" placeholder="add a ${label.toLowerCase()} entry…"/>
            <button class="ent-add">+ add</button>
          </td>
        </tr></tfoot>
      </table>
    </section>`;
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
      // The variant row belongs to the entity row right above it.
      const entRow = vrow.previousElementSibling;
      const { category: cat, canonical } = entRow.dataset;
      const input = vrow.querySelector(".variant-input");
      vrow.querySelector(".variant-add").addEventListener("click", async () => {
        addManualVariant(cat, canonical, input.value);
        await refreshVariants();
      });
    }
  }
}

/**
 * refreshVariants() asks Go to expand every entity whose variant list is
 * missing (new, edited, or variant-added rows). Sequential on purpose:
 * the lists are tiny and ordering keeps the UI deterministic. Each
 * setEntityVariants triggers a state notification, so the view re-renders
 * with the counts filled in.
 */
async function refreshVariants() {
  for (const e of getState().entities) {
    if ((e.variants?.length ?? 0) > 0) continue;
    try {
      const variants = await expandVariants({
        category: e.category, canonical: e.canonical, manualVariants: e.manualVariants,
      });
      setEntityVariants(e.category, e.canonical, variants ?? []);
    } catch {
      // Expansion is display sugar; the pipeline expands server-side
      // anyway. Leave the list empty rather than surfacing an error.
    }
  }
}

// --- Allowlist -----------------------------------------------------------

function allowlistPanel(s) {
  const pills = s.allowlist.map((t) => `
    <span class="pill">${escapeHTML(t)}<button class="allow-del" data-term="${escapeHTML(t)}" title="Remove">✕</button></span>`).join("");
  return `
    <section class="panel" id="allow-panel">
      <div class="panel-head"><h2>Allowlist (never anonymised)</h2></div>
      <p class="hint">Terms here survive every pass, even when they are also listed as entities.</p>
      <div class="pill-list">${pills || `<span class="hint">empty</span>`}</div>
      <div class="form-row">
        <input id="allow-input" placeholder="add a term, e.g. CSSF"/>
        <button id="allow-add">+ add</button>
      </div>
    </section>`;
}

function wireAllowlist(container) {
  const input = container.querySelector("#allow-input");
  const add = () => addAllowTerm(input.value);
  container.querySelector("#allow-add").addEventListener("click", add);
  input.addEventListener("keydown", (ev) => { if (ev.key === "Enter") add(); });
  for (const btn of container.querySelectorAll(".allow-del")) {
    btn.addEventListener("click", () => removeAllowTerm(btn.dataset.term));
  }
}

// --- Custom patterns -------------------------------------------------------

function patternsPanel(s) {
  const rows = s.patterns.map((p) => `
    <span class="pill ${p.error ? "warn" : ""}" title="${escapeHTML(p.error ?? "valid")}">
      <code>${escapeHTML(p.expr)}</code>
      <button class="pattern-del" data-expr="${escapeHTML(p.expr)}" title="Remove">✕</button>
    </span>`).join("");
  return `
    <section class="panel" id="pattern-panel">
      <div class="panel-head"><h2>Custom patterns (regex)</h2></div>
      <p class="hint">User-defined regular expressions, replaced as [CUSTOM_N]. Validated as you type;
        the tester shows sample matches from the loaded documents.</p>
      <div class="pill-list">${rows || `<span class="hint">none</span>`}</div>
      <div class="form-row">
        <input id="pattern-input" placeholder="e.g. PRJ-[0-9]+" spellcheck="false"/>
        <button id="pattern-test">test</button>
        <button id="pattern-add">+ add</button>
      </div>
      <div id="pattern-feedback" class="hint"></div>
    </section>`;
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

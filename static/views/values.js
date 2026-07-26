// views/values.js, wizard step 3 (BUILD-02 Phase 9): three discovery
// methods (cloud placeholder / local AI / Smart detection), ONE unified
// candidate review list (nothing reaches the value tables without an
// explicit Accept), live match preview for manual entries, and variant
// drag-and-drop regrouping. FULLY usable without Ollama: Smart detection
// and the manual add rows carry the whole flow.
//
// Naming note (BUILD-04 CR3): the step is called "Values" everywhere the
// user can see it. The ENGINE identifiers it manipulates (the category
// keys client_names, person_names, ... and the state.entities array) keep
// their original names on purpose, so a label change never ripples into
// the pipeline or into saved sessions.
//
// Render-from-state discipline: every mutation goes through a state.js
// reducer; every Go call goes through api.js.

import {
  runDiscovery, cancelDiscovery, estimateDiscovery, runSmartDetection,
  countTermMatches, expandVariants, validatePattern, patternMatches,
} from "../api.js";
import {
  getState, setState, llmEnabled,
  addEntities, setEntityStatus, editEntity, removeEntity,
  setEntityVariants, setEntityVariantError, addManualVariant, entityKey,
  addCandidates, acceptCandidate, rejectCandidate, updateCandidate,
  acceptAllInCategory, moveVariant,
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

export function renderValues(container) {
  const s = getState();
  // Discovery gates on the master AI toggle AND live availability
  // (BUILD-02 Phase 6d).
  const aiOK = llmEnabled(s);

  container.innerHTML = `
    <div class="values-view">
      ${discoveryPanel(s, aiOK)}
      ${candidatesPanel(s)}
      ${CATEGORIES.map(([cat, label]) => categoryPanel(s, cat, label)).join("")}
      ${renderAllowlistPanel(s, collapsedPanels)}
      ${patternsPanel(s)}
      <div id="values-error"></div>
    </div>
  `;

  wirePanels(container, collapsedPanels, () => setState({}));
  wireDiscovery(container, s);
  wireCandidates(container);
  wireCategoryPanels(container);
  wireAllowlistPanel(container);
  wirePatterns(container);
}

// --- Discovery methods (BUILD-02 Phase 9a) -------------------------------

function discoveryPanel(s, aiOK) {
  const fileOptions = s.documents.map((d) => `
    <label class="radio-row">
      <input type="checkbox" class="disc-file" value="${escapeHTML(d.name)}"/>
      <span>${escapeHTML(d.name)}</span>
    </label>`).join("");

  // Determinate per-file progress + Cancel while a run is live
  // (BUILD-02 Phase 7c/7d).
  const d = s.discovery;
  const pct = d?.running && d.total ? Math.round(((d.current + 1) / d.total) * 100) : 0;
  const progressHTML = d?.running ? `
      <div class="progress-bar"><div style="width:${pct}%"></div></div>
      <p class="hint">Scanning ${escapeHTML(d.file ?? "")} (${d.current + 1}/${d.total})
        <button id="btn-disc-cancel">Cancel</button></p>` : "";

  // Three methods (Phase 9a). The local-AI method is only VISIBLE when
  // the master toggle is on; the cloud method is a disabled placeholder.
  const localAIButton = s.settings.useAI ? `
      <div class="form-row">
        <button id="btn-discover" class="primary" ${aiOK && !d?.running ? "" : `disabled title="${escapeHTML(llmGateTooltip(s))}"`}>
          Auto-discovery with local AI</button>
        <span class="hint">Reads the selected files with the local Ollama model and suggests names.</span>
      </div>` : "";

  const content = `
      <p class="hint">Pick one or more files below, then choose a method. Every suggestion goes
        to the review list first; nothing is replaced without your approval.</p>
      ${fileOptions || `<p class="hint">No documents imported.</p>`}
      <div class="form-row">
        <button class="btn btn-ghost" disabled title="Not available yet.">Auto-discovery with cloud AI</button>
        <span class="hint">Not available yet.</span>
      </div>
      ${localAIButton}
      <div class="form-row">
        <button id="btn-smart" ${d?.running ? "disabled" : ""}>Smart detection</button>
        <span class="hint">Works without any AI. Finds likely names by how they are written.</span>
      </div>
      ${progressHTML}
      <div id="disc-progress" class="hint"></div>`;
  return panel("panel-discovery", "Find names to replace", content, {
    collapsible: true, collapsedSet: collapsedPanels,
  });
}

/** selectedFiles(container) reads the shared file-checkbox list. */
function selectedFiles(container) {
  return [...container.querySelectorAll(".disc-file:checked")].map((c) => c.value);
}

function wireDiscovery(container, s) {
  container.querySelector("#btn-disc-cancel")?.addEventListener("click", () => cancelDiscovery());

  const feedback = (msg) => {
    const slot = document.querySelector("#disc-progress");
    if (slot) slot.textContent = msg;
  };

  // Local AI discovery: proposals land in the REVIEW list, never
  // directly in the entity tables (Phase 9b behaviour change).
  const aiBtn = container.querySelector("#btn-discover");
  if (aiBtn && !aiBtn.disabled) {
    aiBtn.addEventListener("click", async () => {
      let files = selectedFiles(container);
      const progress = container.querySelector("#disc-progress");
      if (files.length === 0) {
        progress.textContent = "Select at least one file first.";
        return;
      }
      // Pre-run size safeguard (Phase 7e): oversized files are excluded
      // with the actionable message, never a mid-run hard error.
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
        const added = addCandidates(
          (res?.proposals ?? []).map((p) => ({ text: p.text, category: p.category })), "local-ai");
        setState({ discovery: null });
        feedback(`${res?.status ?? "done"}. ${added} new candidate(s) for review below.`);
      } catch (err) {
        setState({ discovery: null });
        showError(container, err);
      }
    });
  }

  // Smart detection: always available, no AI required. Categories are
  // refined by the local AI only when it is enabled AND reachable.
  container.querySelector("#btn-smart")?.addEventListener("click", async () => {
    const files = selectedFiles(container);
    const progress = container.querySelector("#disc-progress");
    if (files.length === 0) {
      progress.textContent = "Select at least one file first.";
      return;
    }
    setState({ discovery: { running: true, current: 0, total: files.length, file: files[0] } });
    try {
      const res = await runSmartDetection(files, s.allowlist, llmEnabled(s));
      const added = addCandidates(res?.candidates ?? [], "smart");
      setState({ discovery: null });
      feedback(`${res?.status ?? "done"}. ${added} new candidate(s) for review below.`);
    } catch (err) {
      setState({ discovery: null });
      showError(container, err);
    }
  });
}

// --- Candidate review (BUILD-02 Phase 9b) ---------------------------------

const CANDIDATE_CATEGORIES = [
  ["client_names", "Client"],
  ["project_names", "Project"],
  ["internal_names", "Internal"],
  ["person_names", "Person"],
];

function candidatesPanel(s) {
  if (!s.candidates.length) return "";
  const options = (selected) => CANDIDATE_CATEGORIES.map(([v, l]) =>
    `<option value="${v}" ${v === selected ? "selected" : ""}>${l}</option>`).join("");

  const rows = s.candidates.map((c) => `
    <tr data-ctext="${escapeHTML(c.text)}">
      <td class="ent-name" title="${escapeHTML((c.contexts ?? []).join("\n"))}">
        <input class="cand-text" value="${escapeHTML(c.text)}"/>
      </td>
      <td><select class="cand-category">${options(c.category)}</select></td>
      <td class="hint">${c.count ? `${c.count}×` : ""} ${escapeHTML(c.source)}</td>
      <td class="ent-actions">
        <button class="cand-accept">accept</button>
        <button class="cand-reject">reject</button>
      </td>
    </tr>`).join("");

  const bulk = CANDIDATE_CATEGORIES
    .filter(([v]) => s.candidates.some((c) => c.category === v))
    .map(([v, l]) => `<button class="cand-accept-all" data-category="${v}">Accept all ${l.toLowerCase()}s</button>`)
    .join(" ");

  const content = `
      <p class="hint">Review each suggestion: fix the name or category if needed, then accept or
        reject. Only accepted names are ever replaced.</p>
      <table class="entity-table"><tbody>${rows}</tbody></table>
      <div class="form-row">${bulk}</div>`;
  return panel("panel-candidates", `Suggestions to review (${s.candidates.length})`, content, {
    collapsible: true, collapsedSet: collapsedPanels,
  });
}

function wireCandidates(container) {
  for (const row of container.querySelectorAll("tr[data-ctext]")) {
    const original = row.dataset.ctext;
    const textInput = row.querySelector(".cand-text");
    const catSelect = row.querySelector(".cand-category");
    textInput.addEventListener("change", () => {
      if (!updateCandidate(original, { text: textInput.value })) {
        textInput.value = original; // collision or blank: revert visibly
      }
    });
    catSelect.addEventListener("change", () => updateCandidate(original, { category: catSelect.value }));
    row.querySelector(".cand-accept").addEventListener("click", async () => {
      if (acceptCandidate(row.dataset.ctext)) await refreshVariants();
    });
    row.querySelector(".cand-reject").addEventListener("click", () => rejectCandidate(row.dataset.ctext));
  }
  for (const btn of container.querySelectorAll(".cand-accept-all")) {
    btn.addEventListener("click", async () => {
      if (acceptAllInCategory(btn.dataset.category) > 0) await refreshVariants();
    });
  }
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
      const chips = vrow.variants.map((v) => `
        <span class="pill variant-chip" draggable="true" data-variant="${escapeHTML(v)}"
              data-category="${escapeHTML(e.category)}" data-canonical="${escapeHTML(e.canonical)}"
              title="Drag onto another value to move this variant there.">
          <span class="icon drag-handle" aria-hidden="true">⠿</span>${escapeHTML(v)}
          <button class="variant-move" title="Move this variant to another value">move</button>
        </span>`).join(" ");
      const inner =
        vrow.state === "pending" ? `<span class="hint">expanding…</span>` :
        vrow.state === "error" ? `<span class="banner error">Variant expansion failed: ${escapeHTML(vrow.error)}</span>` :
        vrow.state === "empty" ? `<span class="hint">No variants</span>` :
        `<span class="pill-list">${chips}</span>`;
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
          <td colspan="3">
            <div class="form-row">
              <input class="ent-add-input" placeholder="add a ${label.toLowerCase()} entry"/>
              <button class="ent-add">Add</button>
            </div>
            <div class="ent-add-preview hint"></div>
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

    // Live match preview (BUILD-02 Phase 9c): debounced count of the
    // typed term across the loaded documents, shown before committing.
    const preview = panel.querySelector(".ent-add-preview");
    let previewTimer = null;
    addInput.addEventListener("input", () => {
      clearTimeout(previewTimer);
      const term = addInput.value.trim();
      if (!term) { preview.textContent = ""; return; }
      previewTimer = setTimeout(async () => {
        try {
          const info = await countTermMatches(term);
          preview.textContent = info?.count
            ? `Found ${info.count} time(s) in ${info.documents} document(s).`
            : "Not found in the loaded documents.";
        } catch { preview.textContent = ""; }
      }, 300);
    });

    for (const row of panel.querySelectorAll("tr[data-key]")) {
      const { category: cat, canonical } = row.dataset;
      row.querySelector(".ent-accept").addEventListener("click", () => setEntityStatus(cat, canonical, "accepted"));
      row.querySelector(".ent-deny").addEventListener("click", () => setEntityStatus(cat, canonical, "denied"));
      row.querySelector(".ent-delete").addEventListener("click", () => removeEntity(cat, canonical));
      row.querySelector(".ent-edit").addEventListener("click", async () => {
        const next = prompt("Edit the value:", canonical);
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

    // Variant drag-and-drop regrouping (BUILD-02 Phase 9d): chips are
    // draggable; value rows are drop targets. The tested moveVariant
    // reducer does all the work; this is wiring only.
    for (const chip of panel.querySelectorAll(".variant-chip")) {
      chip.addEventListener("dragstart", (ev) => {
        ev.dataTransfer.setData("application/x-variant", JSON.stringify({
          variant: chip.dataset.variant,
          fromCategory: chip.dataset.category,
          fromCanonical: chip.dataset.canonical,
        }));
        ev.dataTransfer.effectAllowed = "move";
      });
      // Keyboard/mouse fallback so the feature is not drag-only: a
      // "move" button prompting for the target entity name.
      chip.querySelector(".variant-move").addEventListener("click", async () => {
        const target = prompt(
          "Move this variant to which value? Type its exact name (any category):");
        if (!target) return;
        const to = getState().entities.find(
          (e) => e.canonical.toLowerCase() === target.trim().toLowerCase());
        if (!to) {
          showError(container, new Error(`No value named "${target.trim()}" exists. Add it first, then move the variant.`));
          return;
        }
        if (moveVariant(chip.dataset.category, chip.dataset.canonical, to.category, to.canonical, chip.dataset.variant)) {
          await refreshVariants();
        }
      });
    }
  }

  // Drop targets: every value row accepts a dragged variant chip.
  for (const row of container.querySelectorAll("tr[data-key]:not(.variant-row)")) {
    row.addEventListener("dragover", (ev) => {
      if (ev.dataTransfer.types.includes("application/x-variant")) {
        ev.preventDefault();
        ev.dataTransfer.dropEffect = "move";
      }
    });
    row.addEventListener("drop", async (ev) => {
      const raw = ev.dataTransfer.getData("application/x-variant");
      if (!raw) return;
      ev.preventDefault();
      const { variant, fromCategory, fromCanonical } = JSON.parse(raw);
      const { category: toCategory, canonical: toCanonical } = row.dataset;
      if (moveVariant(fromCategory, fromCanonical, toCategory, toCanonical, variant)) {
        await refreshVariants();
      }
    });
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
  container.querySelector("#values-error").innerHTML =
    `<div class="banner error">${escapeHTML(String(err?.message ?? err))}</div>`;
}

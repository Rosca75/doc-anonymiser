// views/anonymise.js, wizard step 3: pipeline execution, live progress, and
// results review (Phase 8; renamed from views/run.js by BUILD-05 Phase 2,
// when the step token became "anonymise" so module, token and visible label
// finally agree).
//
//   - Run button (+ optional deep-scan checkbox, LLM-gated) and Cancel,
//   - progress bar fed by "pipeline:progress" events (state.progress),
//   - side-by-side original/anonymised preview with placeholder highlights
//     (highlight.js), per-document counts, aggregate report panel,
//   - "something missed?" loop: add an entity or simple-replace rule and
//     fast-rerun (passes 2+4, no LLM) to refresh the outputs,
//   - ordered simple-replace rules editor (mirrors notebook Cell 8).

import { runPipeline, cancelPipeline, fastRerun, getMapping } from "../api.js";
import {
  getState, setState, llmEnabled,
  addEntities, buildRunRequest,
  addSimpleRule, removeSimpleRule, moveSimpleRule,
  entityAutocomplete, reassignOriginal,
} from "../state.js";
import { escapeHTML } from "../html.js";
import { renderHighlighted } from "../highlight.js";
import { panel, wirePanels } from "../ui.js";
import { llmGateTooltip } from "./identifyrail.js";

// Panels the user toggled away from their default state (BUILD-02 Phase 2f).
const collapsedPanels = new Set();

export function renderAnonymise(container) {
  const s = getState();
  // Deep-scan gates on the master AI toggle AND live availability
  // (BUILD-02 Phase 6d).
  const aiOK = llmEnabled(s);

  container.innerHTML = `
    <div class="run-view">
      ${controlPanel(s, aiOK)}
      ${rulesPanel(s)}
      ${s.results ? resultsPanel(s) : ""}
      <div id="run-error"></div>
    </div>
  `;

  wirePanels(container, collapsedPanels, () => setState({}));
  wireControls(container, s);
  wireRules(container);
  if (s.results) wireResults(container, s);
}

// --- Run controls + progress ------------------------------------------------

function controlPanel(s, aiOK) {
  const gate = aiOK ? "" : `disabled title="${escapeHTML(llmGateTooltip(s))}"`;
  const pct = s.progress && s.progress.docCount
    ? Math.round(((s.progress.docIndex + 1) / s.progress.docCount) * 100)
    : 0;
  return `
    <section class="panel">
      <div class="panel-head">
        <h2>Run anonymisation</h2>
        <div>
          <label title="${aiOK ? "Extra AI pass to catch residual values" : escapeHTML(llmGateTooltip(s))}">
            <input type="checkbox" id="deep-scan" ${gate}/> deep-scan (AI)
          </label>
          <button id="btn-run" class="primary" ${s.running ? "disabled" : ""}>Run</button>
          <button id="btn-cancel" ${s.running ? "" : "disabled"}>Cancel</button>
        </div>
      </div>
      ${s.running ? `
        <div class="progress-bar"><div style="width:${pct}%"></div></div>
        <p class="hint">${s.progress
          ? `${escapeHTML(s.progress.stage)}: ${escapeHTML(s.progress.docName)} (${s.progress.docIndex + 1}/${s.progress.docCount})`
          : "starting…"}</p>` : ""}
    </section>`;
}

function wireControls(container, s) {
  container.querySelector("#btn-run").addEventListener("click", async () => {
    try {
      setState({ running: true, progress: null, results: null });
      await runPipeline(buildRunRequest(container.querySelector("#deep-scan").checked));
      // Results arrive via the "pipeline:done" event (see main.js boot).
    } catch (err) {
      setState({ running: false });
      showError(container, err);
    }
  });
  container.querySelector("#btn-cancel").addEventListener("click", () => cancelPipeline());
}

// --- Simple-replace rules editor ---------------------------------------------

function rulesPanel(s) {
  const rows = s.simpleRules.map((r, i) => `
    <tr data-index="${i}">
      <td><code>${escapeHTML(r.find)}</code></td>
      <td>→ <code>${escapeHTML(r.replace)}</code></td>
      <td>${r.caseSensitive ? "case-sensitive" : "any case"}</td>
      <td class="ent-actions">
        <button class="rule-up" ${i === 0 ? "disabled" : ""}>↑</button>
        <button class="rule-down" ${i === s.simpleRules.length - 1 ? "disabled" : ""}>↓</button>
        <button class="rule-del">✕</button>
      </td>
    </tr>`).join("");
  const content = `
      <table class="entity-table"><tbody>${rows}</tbody></table>
      <div class="form-row">
        <input id="rule-find" placeholder="find (literal text)"/>
        <input id="rule-replace" placeholder="replace with"/>
        <label><input type="checkbox" id="rule-case"/> case-sensitive</label>
        <button id="rule-add">Add rule</button>
      </div>`;
  // Rules are an advanced feature: collapsed by default (BUILD-02 Phase 2f).
  return panel("rules-panel", "Manual find and replace (runs last, in order)", content, {
    collapsible: true, startOpen: false, collapsedSet: collapsedPanels,
  });
}

function wireRules(container) {
  container.querySelector("#rule-add").addEventListener("click", () => {
    addSimpleRule({
      find: container.querySelector("#rule-find").value,
      replace: container.querySelector("#rule-replace").value,
      caseSensitive: container.querySelector("#rule-case").checked,
    });
  });
  for (const row of container.querySelectorAll("#rules-panel tr[data-index]")) {
    const i = parseInt(row.dataset.index, 10);
    row.querySelector(".rule-up").addEventListener("click", () => moveSimpleRule(i, -1));
    row.querySelector(".rule-down").addEventListener("click", () => moveSimpleRule(i, +1));
    row.querySelector(".rule-del").addEventListener("click", () => removeSimpleRule(i));
  }
}

// --- Results review -----------------------------------------------------------

function resultsPanel(s) {
  const r = s.results;
  const selected = s.resultDoc ?? r.documents?.[0]?.name;
  const doc = (r.documents ?? []).find((d) => d.name === selected);

  const docOptions = (r.documents ?? []).map((d) =>
    `<option value="${escapeHTML(d.name)}" ${d.name === selected ? "selected" : ""}>${escapeHTML(d.name)}</option>`).join("");

  const counts = doc ? Object.entries(doc.byCategory ?? {}).map(([cat, n]) =>
    `<span class="pill">${escapeHTML(cat)}: ${n}</span>`).join(" ") : "";

  const categories = Object.entries(r.report?.byCategory ?? {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([cat, n]) => `<tr><td>${escapeHTML(cat)}</td><td>${n}</td></tr>`).join("");

  const resultsContent = `
      <div class="pill-list">${counts || `<span class="hint">no replacements in this document</span>`}</div>
      <div class="compare">
        <div>
          <h3 class="hint">Original</h3>
          <pre class="md-preview">${escapeHTML(doc?.original ?? "")}</pre>
        </div>
        <div>
          <h3 class="hint">Anonymised</h3>
          <pre class="md-preview" id="anonymised-pane">${doc ? renderHighlighted(doc.anonymised, s.mapping) : ""}</pre>
        </div>
      </div>`;

  const missedContent = `
      <p class="hint">Add it as a value (or a rule above), then re-run the fast deterministic passes.
        There is no AI re-scan, and existing placeholders keep their numbers.</p>
      <div class="form-row">
        <select id="missed-category">
          <option value="person_names">person</option>
          <option value="client_names">client</option>
          <option value="project_names">project</option>
          <option value="internal_names">Internal</option>
        </select>
        <input id="missed-name" placeholder="missed name, e.g. P. Stone"/>
        <button id="missed-add">Add value</button>
        <button id="btn-fast-rerun" class="primary">Fast re-run</button>
      </div>`;

  const reportContent = `
      <p class="hint">
        Level: <strong>${escapeHTML(r.report?.level ?? "")}</strong> ·
        LLM pass: ${escapeHTML(r.report?.llmPass ?? "")} ·
        ${r.report?.totalReplacements ?? 0} replacements in ${r.documents?.length ?? 0} document(s) ·
        ${r.report?.durationMs ?? 0} ms
      </p>
      <table class="entity-table"><tbody>${categories}</tbody></table>
      ${(r.report?.warnings ?? []).map((w) => `<div class="banner warn">${escapeHTML(w)}</div>`).join("")}`;

  return panel("results-panel", "Results", resultsContent, {
    collapsible: true, collapsedSet: collapsedPanels,
    headExtraHTML: `<select id="result-doc">${docOptions}</select>`,
  }) + panel("missed-panel", "Something missed?", missedContent, {
    collapsible: true, collapsedSet: collapsedPanels,
  }) + panel("report-panel", "Report", reportContent, {
    collapsible: true, startOpen: false, collapsedSet: collapsedPanels,
  });
}

function wireResults(container, s) {
  container.querySelector("#result-doc").addEventListener("change", (ev) => {
    setState({ resultDoc: ev.target.value });
  });
  container.querySelector("#missed-add").addEventListener("click", () => {
    addEntities([{
      category: container.querySelector("#missed-category").value,
      canonical: container.querySelector("#missed-name").value,
    }]);
  });
  container.querySelector("#btn-fast-rerun").addEventListener("click", async () => {
    try {
      const results = await fastRerun(buildRunRequest(false));
      // The mapping may have grown (new entities earned placeholders).
      setState({ results, mapping: await getMapping() });
    } catch (err) {
      showError(container, err);
    }
  });

  wireReassignPopover(container);
}

// --- Click-to-reassign popover (BUILD-02 Phase 10d) --------------------------

/**
 * wireReassignPopover attaches one click handler to the anonymised pane:
 * clicking a mark with a known original opens a small popover anchored to
 * it, offering to make the original a variant of another entity. The
 * autocomplete uses the tested entityAutocomplete filter; confirming
 * calls reassignOriginal + fastRerun (deterministic passes only). Escape
 * or click-away closes; only one popover exists at a time.
 */
function wireReassignPopover(container) {
  const pane = container.querySelector("#anonymised-pane");
  if (!pane) return;

  const closePopover = () => container.querySelector("#reassign-popover")?.remove();

  pane.addEventListener("click", (ev) => {
    const mark = ev.target.closest("mark[data-ph]");
    if (!mark) return;
    closePopover();

    const original = mark.dataset.original;
    const placeholder = mark.dataset.ph;
    const pop = document.createElement("div");
    pop.id = "reassign-popover";
    pop.className = "reassign-popover";
    pop.innerHTML = `
      <p><strong>${escapeHTML(placeholder)}</strong> replaces <strong>${escapeHTML(original)}</strong></p>
      <div class="form-row">
        <label>variant of</label>
        <input id="reassign-input" placeholder="type a value" autocomplete="off"/>
      </div>
      <ul class="reassign-suggestions" id="reassign-suggestions"></ul>`;

    // Anchor next to the clicked mark inside the scrollable pane.
    const rect = mark.getBoundingClientRect();
    const paneRect = pane.getBoundingClientRect();
    pop.style.left = `${rect.left - paneRect.left}px`;
    pop.style.top = `${rect.bottom - paneRect.top + 4}px`;
    pane.style.position = "relative";
    pane.appendChild(pop);

    const input = pop.querySelector("#reassign-input");
    const list = pop.querySelector("#reassign-suggestions");

    const confirm = async (category, canonical) => {
      closePopover();
      if (!reassignOriginal(original, category, canonical)) return;
      try {
        const results = await fastRerun(buildRunRequest(false));
        setState({ results, mapping: await getMapping() });
      } catch (err) {
        showError(container, err);
      }
    };

    input.addEventListener("input", () => {
      const matches = entityAutocomplete(input.value).slice(0, 8);
      list.innerHTML = matches.map((m) => `
        <li><button class="btn btn-ghost reassign-pick" data-category="${escapeHTML(m.category)}"
             data-canonical="${escapeHTML(m.canonical)}">${escapeHTML(m.canonical)}
             <span class="hint">${escapeHTML(m.category)}</span></button></li>`).join("");
      for (const btn of list.querySelectorAll(".reassign-pick")) {
        btn.addEventListener("click", () => confirm(btn.dataset.category, btn.dataset.canonical));
      }
    });
    input.addEventListener("keydown", (ev2) => {
      if (ev2.key === "Escape") closePopover();
      if (ev2.key === "Enter") {
        const first = list.querySelector(".reassign-pick");
        if (first) confirm(first.dataset.category, first.dataset.canonical);
      }
    });
    input.focus();
    ev.stopPropagation();
  });

  // Click-away and Escape close the popover.
  document.addEventListener("click", (ev) => {
    if (!ev.target.closest("#reassign-popover") && !ev.target.closest("mark[data-ph]")) closePopover();
  }, { once: false });
  document.addEventListener("keydown", (ev) => {
    if (ev.key === "Escape") closePopover();
  });
}

function showError(container, err) {
  container.querySelector("#run-error").innerHTML =
    `<div class="banner error">${escapeHTML(String(err?.message ?? err))}</div>`;
}

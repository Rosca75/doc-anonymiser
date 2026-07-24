// views/run.js, wizard step 4: pipeline execution, live progress, and
// results review (Phase 8).
//
//   - Run button (+ optional deep-scan checkbox, LLM-gated) and Cancel,
//   - progress bar fed by "pipeline:progress" events (state.progress),
//   - side-by-side original/anonymised preview with placeholder highlights
//     (highlight.js), per-document counts, aggregate report panel,
//   - "something missed?" loop: add an entity or simple-replace rule and
//     fast-rerun (passes 2+4, no LLM) to refresh the outputs,
//   - ordered simple-replace rules editor (mirrors notebook Cell 8).

import { runPipeline, cancelPipeline, fastRerun } from "../api.js";
import {
  getState, setState,
  addEntities, buildRunRequest,
  addSimpleRule, removeSimpleRule, moveSimpleRule,
} from "../state.js";
import { escapeHTML } from "../html.js";
import { renderHighlighted } from "../highlight.js";
import { LLM_DISABLED_TOOLTIP } from "./configure.js";

export function renderRun(container) {
  const s = getState();
  const ollamaOK = !!s.ollama?.available;

  container.innerHTML = `
    <div class="run-view">
      ${controlPanel(s, ollamaOK)}
      ${rulesPanel(s)}
      ${s.results ? resultsPanel(s) : ""}
      <div id="run-error"></div>
    </div>
  `;

  wireControls(container, s);
  wireRules(container);
  if (s.results) wireResults(container, s);
}

// --- Run controls + progress ------------------------------------------------

function controlPanel(s, ollamaOK) {
  const gate = ollamaOK ? "" : `disabled title="${LLM_DISABLED_TOOLTIP}"`;
  const pct = s.progress && s.progress.docCount
    ? Math.round(((s.progress.docIndex + 1) / s.progress.docCount) * 100)
    : 0;
  return `
    <section class="panel">
      <div class="panel-head">
        <h2>Run anonymisation</h2>
        <div>
          <label title="${ollamaOK ? "Extra AI pass to catch residual entities" : LLM_DISABLED_TOOLTIP}">
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
  return `
    <section class="panel" id="rules-panel">
      <div class="panel-head"><h2>Manual find → replace (runs last, in order)</h2></div>
      <table class="entity-table"><tbody>${rows}</tbody></table>
      <div class="form-row">
        <input id="rule-find" placeholder="find (literal text)"/>
        <input id="rule-replace" placeholder="replace with"/>
        <label><input type="checkbox" id="rule-case"/> case-sensitive</label>
        <button id="rule-add">+ add rule</button>
      </div>
    </section>`;
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

  return `
    <section class="panel">
      <div class="panel-head">
        <h2>Results</h2>
        <select id="result-doc">${docOptions}</select>
      </div>
      <div class="pill-list">${counts || `<span class="hint">no replacements in this document</span>`}</div>
      <div class="compare">
        <div>
          <h3 class="hint">Original</h3>
          <pre class="md-preview">${escapeHTML(doc?.original ?? "")}</pre>
        </div>
        <div>
          <h3 class="hint">Anonymised</h3>
          <pre class="md-preview">${doc ? renderHighlighted(doc.anonymised) : ""}</pre>
        </div>
      </div>
    </section>
    <section class="panel">
      <div class="panel-head"><h2>Something missed?</h2></div>
      <p class="hint">Add it as an entity (or a rule above), then re-run the fast deterministic passes.
        There is no AI re-scan, and existing placeholders keep their numbers.</p>
      <div class="form-row">
        <select id="missed-category">
          <option value="person_names">person</option>
          <option value="client_names">client</option>
          <option value="project_names">project</option>
          <option value="pwc_internal_names">PwC internal</option>
        </select>
        <input id="missed-name" placeholder="missed name, e.g. P. Stone"/>
        <button id="missed-add">add entity</button>
        <button id="btn-fast-rerun" class="primary">Fast re-run</button>
      </div>
    </section>
    <section class="panel">
      <div class="panel-head"><h2>Report</h2></div>
      <p class="hint">
        Level: <strong>${escapeHTML(r.report?.level ?? "")}</strong> ·
        LLM pass: ${escapeHTML(r.report?.llmPass ?? "")} ·
        ${r.report?.totalReplacements ?? 0} replacements in ${r.documents?.length ?? 0} document(s) ·
        ${r.report?.durationMs ?? 0} ms
      </p>
      <table class="entity-table"><tbody>${categories}</tbody></table>
      ${(r.report?.warnings ?? []).map((w) => `<div class="banner warn">${escapeHTML(w)}</div>`).join("")}
    </section>`;
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
      setState({ results });
    } catch (err) {
      showError(container, err);
    }
  });
}

function showError(container, err) {
  container.querySelector("#run-error").innerHTML =
    `<div class="banner error">${escapeHTML(String(err?.message ?? err))}</div>`;
}

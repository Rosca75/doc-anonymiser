// views/export.js, wizard step 5: every egress path (Phase 9), all behind
// explicit user actions:
//   - per-document save with per-format buttons (CSV round-trip for
//     grid-origin documents, .md/.txt/.json per the format rules),
//   - export-all zip,
//   - copy to clipboard per document,
//   - entity-mapping export behind a confirmation warning (it is the
//     re-identification key),
//   - report export (JSON + human-readable markdown),
//   - session save/load with the same sensitivity warning.

import {
  exportDocumentFormats, saveDocument, exportAllZip, copyDocument,
  exportMapping, exportReport, saveSession, loadSession,
} from "../api.js";
import { getState, setState, buildRunRequest, addEntities, presetCategories } from "../state.js";
import { escapeHTML } from "../html.js";
import { panel, wirePanels, button } from "../ui.js";

// Panels the user toggled away from their default state (BUILD-02 Phase 2f).
const collapsedPanels = new Set();

// The wording of the sensitivity warning, shared by mapping export and
// session save (CLAUDE.md §5).
const KEY_WARNING =
  "This file contains the RE-IDENTIFICATION KEY: anyone holding it can map " +
  "every placeholder back to the real name. Store it as carefully as the " +
  "original documents. Continue?";

// Per-document format lists, fetched once per render (name → [ext...]).
const formatCache = new Map();

export function renderExport(container) {
  const s = getState();
  const docs = s.results?.documents ?? [];

  // Native Office extensions are the same-format copies (BUILD-02
  // Phase 11): keep the layout, never touch the source file.
  const NATIVE_EXTS = new Set(["docx", "pptx", "xlsx"]);
  const NATIVE_CAPTION = "Keeps the original layout. Your source file is not changed.";

  const rows = docs.map((d) => {
    const formats = formatCache.get(d.name) ?? [];
    const buttons = formats.map((ext) => NATIVE_EXTS.has(ext)
      ? `<button class="doc-save" data-name="${escapeHTML(d.name)}" data-ext="${ext}" title="${NATIVE_CAPTION}">.${ext} (same format)</button>`
      : `<button class="doc-save" data-name="${escapeHTML(d.name)}" data-ext="${ext}">.${ext}</button>`).join(" ");
    return `
      <tr>
        <td class="ent-name">${escapeHTML(d.name)}</td>
        <td>${buttons || "…"}</td>
        <td><button class="doc-copy" data-name="${escapeHTML(d.name)}">copy</button></td>
      </tr>`;
  }).join("");

  const docsContent = `
        <p class="hint">Each file saves with an _anon suffix. Nothing is ever written back to the original files.</p>
        <table class="entity-table"><tbody>${rows || `<tr><td class="hint">No results yet. Run the pipeline first.</td></tr>`}</tbody></table>`;

  const mappingContent = `
        <p class="hint">Maps every placeholder back to the original value. Handle it like the originals themselves.</p>
        <div class="form-row">
          <button id="map-csv">Export as CSV</button>
          <button id="map-json">Export as JSON</button>
        </div>`;

  const reportContent = `
        <div class="form-row">
          <button id="rep-json">Export report as JSON</button>
          <button id="rep-md">Export report as Markdown</button>
        </div>`;

  const sessionContent = `
        <p class="hint">Saves entities, allowlist, patterns, rules, settings and the placeholder registry, so a
          follow-up batch reuses the same placeholders. The file contains the re-identification key.</p>
        <div class="form-row">
          <button id="ses-save">Save session</button>
          <button id="ses-load">Load session</button>
        </div>`;

  container.innerHTML = `
    <div class="export-view">
      ${panel("export-docs-panel", "Export documents", docsContent, {
        collapsible: true, collapsedSet: collapsedPanels,
        headExtraHTML: button("Export all as zip", { kind: "primary", id: "btn-zip", icon: "download" }),
      })}
      ${panel("export-mapping-panel", "Entity mapping (re-identification key)", mappingContent, {
        collapsible: true, collapsedSet: collapsedPanels,
      })}
      ${panel("export-report-panel", "Report", reportContent, {
        collapsible: true, startOpen: false, collapsedSet: collapsedPanels,
      })}
      ${panel("export-session-panel", "Session", sessionContent, {
        collapsible: true, collapsedSet: collapsedPanels,
      })}
      <div id="export-msg"></div>
    </div>
  `;

  wirePanels(container, collapsedPanels, () => setState({}));
  ensureFormats(docs);
  wire(container);
}

/** ensureFormats() lazily fetches each document's offered extensions and
 *  re-renders (via state notify) once known. */
function ensureFormats(docs) {
  for (const d of docs) {
    if (formatCache.has(d.name)) continue;
    formatCache.set(d.name, []); // mark as pending to avoid refetch loops
    exportDocumentFormats(d.name)
      .then((formats) => {
        formatCache.set(d.name, formats ?? []);
        setState({}); // trigger a re-render with the buttons filled in
      })
      .catch(() => formatCache.delete(d.name));
  }
}

function wire(container) {
  const feedback = (msg, isError) => {
    container.querySelector("#export-msg").innerHTML =
      `<div class="banner ${isError ? "error" : "warn"}">${escapeHTML(msg)}</div>`;
  };
  const guard = (promise, okMsg) =>
    promise.then(() => okMsg && feedback(okMsg, false))
      .catch((err) => feedback(String(err?.message ?? err), true));

  for (const btn of container.querySelectorAll(".doc-save")) {
    btn.addEventListener("click", () => guard(saveDocument(btn.dataset.name, btn.dataset.ext)));
  }
  for (const btn of container.querySelectorAll(".doc-copy")) {
    btn.addEventListener("click", () =>
      guard(copyDocument(btn.dataset.name), `Copied ${btn.dataset.name} to the clipboard.`));
  }
  container.querySelector("#btn-zip").addEventListener("click", () => guard(exportAllZip()));

  // Mapping + session exports sit behind the explicit key warning.
  container.querySelector("#map-csv").addEventListener("click", () => {
    if (confirm(KEY_WARNING)) guard(exportMapping("csv"));
  });
  container.querySelector("#map-json").addEventListener("click", () => {
    if (confirm(KEY_WARNING)) guard(exportMapping("json"));
  });

  container.querySelector("#rep-json").addEventListener("click", () => guard(exportReport("json")));
  container.querySelector("#rep-md").addEventListener("click", () => guard(exportReport("md")));

  container.querySelector("#ses-save").addEventListener("click", () => {
    if (confirm(KEY_WARNING)) guard(saveSession(buildRunRequest(false)));
  });
  container.querySelector("#ses-load").addEventListener("click", async () => {
    try {
      const session = await loadSession();
      if (!session) return; // user cancelled
      // Restore the frontend state from the session (Go already restored
      // registry + settings). Denied/accepted review states are not part
      // of a session, loaded entities arrive accepted.
      setState({
        entities: [],
        allowlist: session.allowTerms ?? [],
        patterns: (session.patterns ?? []).map((p) => ({ expr: p.expr, error: null })),
        simpleRules: session.simpleRules ?? [],
        settings: {
          level: session.settings?.level ?? "medium",
          // v1 sessions predate these fields; fall back to the preset /
          // current defaults rather than dropping them to zero values.
          categories: session.settings?.categories ?? presetCategories(session.settings?.level ?? "medium"),
          ollamaPort: session.settings?.ollamaPort ?? 11434,
          model: session.settings?.model ?? "",
          contextSize: session.settings?.contextSize || getState().settings.contextSize,
          useAI: session.settings?.useAI ?? getState().settings.useAI,
        },
      });
      addEntities((session.entities ?? []).map((e) => ({
        category: e.category, canonical: e.canonical, manualVariants: e.manualVariants ?? [],
      })));
      feedback("Session loaded. Entities, allowlist, patterns, rules and settings were restored.", false);
    } catch (err) {
      feedback(String(err?.message ?? err), true);
    }
  });
}

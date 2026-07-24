// views/import.js, wizard step 1: import documents.
//
// Responsibilities (render from state, dispatch via api.js, CLAUDE.md §4):
//   - "Add files" button → native multi-file dialog (all 7 formats),
//   - drag-drop hint (the drop itself is handled natively by Wails and
//     arrives via the "documents:changed" event, see main.js),
//   - imported list with format badge, size, warnings, remove button,
//     EXPERIMENTAL badge for PDFs, one entry per xlsx sheet,
//   - markdown preview pane for the selected document (CSV/xlsx grids are
//     markdown tables already, rendered as a simple table).

import { importFiles, removeDocument } from "../api.js";
import { getState, setState, applyImportResult, setImportSplit } from "../state.js";
import { escapeHTML, fmtSize } from "../html.js";
import { button } from "../ui.js";

export function renderImport(container) {
  const s = getState();

  const rows = s.documents.map((d) => `
    <li class="doc-row ${s.previewDoc === d.name ? "selected" : ""}" data-name="${escapeHTML(d.name)}">
      <span class="doc-name" title="${escapeHTML(d.name)}">${escapeHTML(d.name)}</span>
      <span class="tag">${escapeHTML(d.format)}</span>
      ${d.experimental ? `<span class="tag warn" title="PDF text extraction is experimental. Review the preview carefully.">EXPERIMENTAL</span>` : ""}
      <span class="doc-size">${fmtSize(d.sizeBytes)}</span>
      ${d.warnings?.length ? `<span class="tag warn" title="${escapeHTML(d.warnings.join("\n"))}">⚠ ${d.warnings.length}</span>` : ""}
      <button class="doc-remove" title="Remove from session">✕</button>
    </li>`).join("");

  const errors = (s.importErrors ?? []).map((e) => `
    <div class="banner error">${escapeHTML(e)} <button class="dismiss">✕</button></div>`).join("");

  const preview = s.documents.find((d) => d.name === s.previewDoc);

  // Resizable two-pane layout (BUILD-02 Phase 2g): the grid template comes
  // from state.importSplit so the ratio survives re-renders.
  const split = s.importSplit ?? 0.5;
  const columns = `${(split * 100).toFixed(1)}fr 6px ${((1 - split) * 100).toFixed(1)}fr`;

  container.innerHTML = `
    <div class="import-view">
      ${errors}
      <div class="import-columns" style="grid-template-columns: ${columns}">
        <section class="panel import-list">
          <div class="panel-head">
            <h2>Documents</h2>
            ${button("Add files", { kind: "primary", id: "btn-add", icon: "add" })}
          </div>
          <p class="hint">or drop .txt / .csv / .md / .docx / .pptx / .xlsx / .pdf files anywhere in this window</p>
          <ul class="doc-list">${rows || `<li class="empty">Nothing imported yet.</li>`}</ul>
        </section>
        <div class="import-divider" id="import-divider"
             title="Drag to resize. Double-click to reset."></div>
        <section class="panel preview">
          <div class="panel-head"><h2>Preview${preview ? `: ${escapeHTML(preview.name)}` : ""}</h2></div>
          ${preview ? renderPreview(preview) : `<p class="hint">Select a document to preview its working form.</p>`}
        </section>
      </div>
    </div>
  `;

  container.querySelector("#btn-add").addEventListener("click", async () => {
    try {
      applyImportResult(await importFiles());
    } catch (err) {
      setState({ importErrors: [String(err?.message ?? err)] });
    }
  });

  for (const row of container.querySelectorAll(".doc-row")) {
    const name = row.dataset.name;
    row.addEventListener("click", () => setState({ previewDoc: name }));
    row.querySelector(".doc-remove").addEventListener("click", async (ev) => {
      ev.stopPropagation();
      applyImportResult(await removeDocument(name));
    });
  }
  for (const btn of container.querySelectorAll(".banner .dismiss")) {
    btn.addEventListener("click", () => setState({ importErrors: [] }));
  }

  wireDivider(container);
}

/**
 * wireDivider(container) makes the 6px divider draggable with pointer
 * events: dragging updates the column template live (cheap) and commits
 * the clamped ratio to state on release (setImportSplit) so it persists
 * across re-renders. Double-click resets to 50/50.
 */
function wireDivider(container) {
  const divider = container.querySelector("#import-divider");
  const grid = container.querySelector(".import-columns");
  if (!divider || !grid) return;

  divider.addEventListener("pointerdown", (down) => {
    down.preventDefault();
    divider.setPointerCapture(down.pointerId);
    divider.classList.add("dragging");
    const rect = grid.getBoundingClientRect();

    const onMove = (move) => {
      const raw = (move.clientX - rect.left) / rect.width;
      const ratio = Math.min(0.8, Math.max(0.2, raw));
      grid.style.gridTemplateColumns = `${(ratio * 100).toFixed(1)}fr 6px ${((1 - ratio) * 100).toFixed(1)}fr`;
    };
    const onUp = (up) => {
      divider.classList.remove("dragging");
      divider.removeEventListener("pointermove", onMove);
      divider.removeEventListener("pointerup", onUp);
      // Commit the final ratio through the tested reducer (clamped there).
      setImportSplit((up.clientX - rect.left) / rect.width);
    };
    divider.addEventListener("pointermove", onMove);
    divider.addEventListener("pointerup", onUp);
  });

  divider.addEventListener("dblclick", () => setImportSplit(0.5));
}

/**
 * renderPreview(doc) shows the markdown working form. Grid documents
 * (CSV / flat xlsx) get their markdown table rendered as an HTML table for
 * readability; everything else is shown as preformatted markdown.
 * All content is escaped, it comes from user documents.
 */
function renderPreview(doc) {
  // Very large documents preview truncated (first 5 000 lines), the
  // pipeline still processes the FULL content (Phase 10 hardening).
  const notice = doc.previewTruncated
    ? `<div class="banner warn">Preview truncated to the first 5 000 lines. The full document is still processed and exported.</div>`
    : "";
  if (doc.isGrid) {
    return `${notice}<div class="table-scroll">${markdownTableToHTML(doc.markdown)}</div>`;
  }
  return `${notice}<pre class="md-preview">${escapeHTML(doc.markdown)}</pre>`;
}

/** markdownTableToHTML renders our own generated markdown table (and only
 *  that shape) as an HTML table; every cell is escaped. */
export function markdownTableToHTML(md) {
  const lines = md.trim().split("\n").filter((l) => l.startsWith("|"));
  if (lines.length < 2) return `<pre class="md-preview">${escapeHTML(md)}</pre>`;
  const cells = (line) => line.slice(1, -1).split(" | ").map((c) => c.trim().replaceAll("\\|", "|"));
  const header = cells(lines[0]);
  const body = lines.slice(2).map(cells); // line 1 is the --- separator
  return `<table class="grid-table"><thead><tr>${
    header.map((h) => `<th>${escapeHTML(h)}</th>`).join("")
  }</tr></thead><tbody>${
    body.map((row) => `<tr>${row.map((c) => `<td>${escapeHTML(c)}</td>`).join("")}</tr>`).join("")
  }</tbody></table>`;
}

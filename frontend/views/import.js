// views/import.js, wizard step 1: import documents (relaid out to match
// Import.dc.html by).
//
// Two cards side by side, both fixed-height:
//
//   Documents the file count, an ADD FILES primary, the drop zone, and one
//              row per imported document carrying its format badge, size, unit
//              count, EXPERIMENTAL badge, warning count and remove button. The
//              selected row wears an orange bar down its left edge.
//   Preview the selected document's WORKING FORM, which is the markdown
//              every pipeline pass actually operates on. Grid documents (CSV,
//              flat xlsx sheet) render as a table; everything else as
//              monospaced markdown.
//
// Two things this layout deliberately does not have: the
// draggable pane divider and the importSplit state behind it. The mock-up uses
// a fixed two-column grid, and a divider that only ever moved between 20% and
// 80% was three state transitions and a pointer-capture dance to save the user
// a decision they did not have.
//
// The drop itself is handled natively by Wails and arrives as the
// "documents:changed" event (see main.js), so the drop zone is a target for the
// eye and a click shortcut to the dialog, not a dragover handler.

import { importFiles, removeDocument, resetSession, probeOllama } from "../api.js";
import { getState, setState, applyImportResult, resetState, goToScreen } from "../state.js";
import { escapeHTML, fmtSize } from "../html.js";
import { button, card, icon, sectionLabel } from "../ui.js";
import { stepFooterHTML, wireStepFooter } from "../nav.js";
import { askConfirm } from "../modal.js";
import { notify } from "../toast.js";
import { CARDS, IMPORT } from "../copy.js";

export function renderImport(container) {
  const s = getState();
  const preview = s.documents.find((d) => d.name === s.previewDoc);

  container.innerHTML = `
    <div class="workspace workspace-import">
      ${documentsCard(s)}
      ${previewCard(preview)}
    </div>
  `;

  wire(container);
}

// --- The Documents card ----------------------------------------------------

/** documentsCard(s) is the left card: the count, ADD FILES, the drop zone and
 *  the document rows. */
function documentsCard(s) {
  const count = s.documents.length;
  const countLabel = count === 1 ? "1 file" : `${count} files`;

  // Import errors sit above the drop zone rather than in a shell banner: they
  // are about the files on THIS screen, and the user's next action is to try a
  // different file.
  const errors = (s.importErrors ?? []).map((message) => `
    <div class="banner error">${escapeHTML(message)}` +
    button("", { kind: "ghost", cls: "dismiss import-error-dismiss", icon: "close", ariaLabel: "Dismiss" }) +
    `</div>`).join("");

  const rows = s.documents.map((d) => documentRow(d, d.name === s.previewDoc)).join("");

  return card({
    id: "import-documents",
    title: CARDS.documents.title,
    subtitle: countLabel,
    headRightHTML:
      button(IMPORT.startOver, {
        kind: "ghost", id: "btn-start-over", icon: "refresh", title: IMPORT.startOverTooltip,
      }) +
      button(IMPORT.addFiles, { kind: "primary", id: "btn-add", icon: "add" }),
    bodyCls: "stack",
    bodyHTML: errors + dropZone() +
      (count ? `<ul class="doc-list">${rows}</ul>` : ""),
    footHTML: stepFooterHTML({ hint: readyHint(s) }, s),
  });
}

/** dropZone() is the click-to-browse target that also names every accepted
 *  format, so the answer to "can it take a .pptx?" is on the screen. */
function dropZone() {
  return `<div class="drop-zone" id="drop-zone" role="button" tabindex="0">` +
    icon("upload_file") +
    `<div class="drop-title">${escapeHTML(IMPORT.dropTitle)}</div>` +
    `<div class="drop-hint">${escapeHTML(IMPORT.dropHint)}</div>` +
    `</div>`;
}

/**
 * documentRow(d, selected) renders one imported document.
 *
 * The metadata line is format, size, unit count, then the two things that need
 * attention: the EXPERIMENTAL badge and the warning count. Ordering the calm
 * facts first and the caution last means the eye lands on the caution.
 *
 * @param {object} d a DocumentInfo from Go
 * @param {boolean} selected whether this row drives the preview
 * @returns {string} safe HTML
 */
function documentRow(d, selected) {
  const meta = [
    `<span class="fmt-badge">${escapeHTML(d.format.toUpperCase())}</span>`,
    `<span class="doc-size">${escapeHTML(fmtSize(d.sizeBytes))}</span>`,
  ];

  // The unit count: "6 pages", "12 slides", "48 rows".
  // Go sends the unit SINGULAR, because only the side printing the number knows
  // which form it needs.
  if (d.unitCount > 0 && d.unit) {
    meta.push(`<span class="doc-dot" aria-hidden="true"></span>`);
    meta.push(`<span class="doc-units">${escapeHTML(unitLabel(d.unitCount, d.unit))}</span>`);
  }

  if (d.experimental) {
    meta.push(`<span class="badge-experimental" title="${escapeHTML(IMPORT.experimentalTooltip)}">` +
      `EXPERIMENTAL</span>`);
  }

  // The warning COUNT with the warnings themselves in the tooltip. Printing all
  // of them inline would push the row to three lines for something that is
  // advice, not an error.
  const warnings = d.warnings ?? [];
  if (warnings.length) {
    meta.push(`<span class="doc-warn" title="${escapeHTML(warnings.join("\n"))}">` +
      icon("warning") + escapeHTML(String(warnings.length)) + `</span>`);
  }

  return `<li class="doc-row${selected ? " selected" : ""}" data-name="${escapeHTML(d.name)}">` +
    `<span class="doc-bar" aria-hidden="true"></span>` +
    `<span class="doc-icon" aria-hidden="true">${icon("description").replace(' aria-hidden="true"', "")}</span>` +
    `<span class="doc-main">` +
    `<span class="doc-name" title="${escapeHTML(d.name)}">${escapeHTML(d.name)}</span>` +
    `<span class="doc-meta">${meta.join("")}</span>` +
    `</span>` +
    button("", {
      kind: "ghost", cls: "doc-remove icon-action danger", icon: "close",
      ariaLabel: `Remove ${d.name}`, title: IMPORT.removeTooltip,
    }) +
    `</li>`;
}

/**
 * unitLabel(count, unit) pluralises a unit count. Go sends the singular; the
 * four units it can send all pluralise with a plain "s", so this is a rule
 * rather than a table, and an unknown unit still reads correctly.
 * @param {number} count
 * @param {string} unit singular ("page", "slide", "row", "line")
 * @returns {string} e.g. "6 pages"
 */
export function unitLabel(count, unit) {
  return `${count} ${unit}${count === 1 ? "" : "s"}`;
}

/**
 * readyHint(s) is the footer sentence: what the user has, or what they need.
 *
 * On an empty screen it says what to do rather than reporting "0 files", which
 * is the difference between a footer that helps and one that states the
 * obvious.
 *
 * @param {object} s state
 * @returns {string}
 */
export function readyHint(s) {
  const count = s.documents.length;
  if (count === 0) return IMPORT.hintEmpty;
  const units = s.documents.reduce((total, d) => total + (d.unitCount > 0 ? d.unitCount : 0), 0);
  const files = count === 1 ? "1 document" : `${count} documents`;
  // The unit total is only meaningful when every document counts the same
  // thing; a mixed batch (pages and rows) would add up to a nonsense figure.
  const units0 = s.documents[0]?.unit;
  const uniform = units0 && s.documents.every((d) => d.unit === units0) && units > 0;
  return uniform ? `${files}, ${unitLabel(units, units0)}` : `${files} ready`;
}

// --- The Preview card ------------------------------------------------------

/**
 * previewCard(doc) is the right card: the selected document's working form.
 *
 * The "WORKING FORM" caption is doing real work: what the user sees here is the
 * markdown the pipeline operates on, not the original layout, and for a docx or
 * a pdf those differ enough to surprise someone who has not been told.
 */
function previewCard(doc) {
  return card({
    id: "import-preview",
    title: CARDS.preview.title,
    subtitle: doc ? doc.name : IMPORT.noSelection,
    caption: CARDS.preview.caption,
    bodyCls: "pane",
    bodyHTML: doc ? previewBody(doc) : `<p class="hint">${escapeHTML(IMPORT.previewEmpty)}</p>`,
  });
}

/**
 * previewBody(doc) shows the markdown working form. Grid documents (CSV, flat
 * xlsx sheet) get their markdown table rendered as an HTML table for
 * readability; everything else is shown as preformatted markdown.
 * All content is escaped: it comes from user documents.
 */
export function previewBody(doc) {
  // Very large documents preview truncated (first 5 000 lines); the pipeline
  // still processes the FULL content.
  const notice = doc.previewTruncated
    ? `<div class="banner warn">${escapeHTML(IMPORT.previewTruncated)}</div>`
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

// --- Wiring ---------------------------------------------------------------

function wire(container) {
  const addFiles = async () => {
    try {
      applyImportResult(await importFiles());
    } catch (err) {
      setState({ importErrors: [String(err?.message ?? err)] });
    }
  };

  container.querySelector("#btn-add").addEventListener("click", addFiles);

  // Start over: the clean-sheet escape hatch. It resets Go (documents, registry,
  // removed values, settings) AND the frontend store, so a separate
  // anonymisation on new files inherits nothing from the previous one.
  container.querySelector("#btn-start-over")?.addEventListener("click", async () => {
    const ok = await askConfirm({
      title: IMPORT.startOverTitle,
      body: IMPORT.startOverBody,
      confirmLabel: IMPORT.startOverConfirm,
    });
    if (!ok) return;
    try {
      await resetSession();
    } catch (err) {
      // A run or detection is still in progress, or the bridge is absent: the
      // Go side refused, so leave the frontend as it is and say why.
      setState({ importErrors: [String(err?.message ?? err)] });
      return;
    }
    // Go is clean; now the frontend. resetState lands on Home, so step back into
    // the wizard on Import (its default step) ready for new files.
    resetState();
    goToScreen("wizard");
    notify(IMPORT.startOverDone, "info");
    // A clean sheet must match a FRESH BOOT: re-probe Ollama (resetState
    // cleared the last probe) so the Local AI switch is not silently lost by
    // starting over. The allowlist is NOT re-seeded: it starts empty on a
    // fresh boot too, so the reset already leaves it in the right state.
    // Best-effort and after the notice: a probe failure must not undo the reset.
    probeOllama()
      .then((status) => setState({ ollama: status }))
      .catch(() => { /* leave ollama unknown; the badge reads "not detected" */ });
  });

  // The drop zone doubles as a click target for the same dialog, and it is a
  // role="button" span, so Enter and Space have to be wired by hand.
  const zone = container.querySelector("#drop-zone");
  zone.addEventListener("click", addFiles);
  zone.addEventListener("keydown", (ev) => {
    if (ev.key !== "Enter" && ev.key !== " ") return;
    ev.preventDefault();
    addFiles();
  });

  for (const row of container.querySelectorAll(".doc-row")) {
    const name = row.dataset.name;
    row.addEventListener("click", () => setState({ previewDoc: name }));
    row.querySelector(".doc-remove").addEventListener("click", async (ev) => {
      // Without this the click also selects the row being removed.
      ev.stopPropagation();
      applyImportResult(await removeDocument(name));
    });
  }

  for (const btn of container.querySelectorAll(".import-error-dismiss")) {
    btn.addEventListener("click", () => setState({ importErrors: [] }));
  }

  wireStepFooter(container);
}

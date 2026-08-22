// views/export.js, wizard step 4: every egress path.
//
// A column of cards on the left, the document list on the right:
//
//   Export the destination folder, Browse, and EXPORT ALL AS ZIP. The
//                  folder drives the ZIP AND NOTHING ELSE: every
//                  other export keeps its own save dialog, so nothing
//                  key-bearing can land on disk from a single click.
//   Value mapping the re-identification key, as CSV or JSON, behind the
//                  key-bearing modal. Wears the key tint.
//   Report counts per category and the settings used. Contains no
//                  original values, so no warning and no tint.
//   Session save and load, so a follow-up batch reuses the same
//                  placeholders. Contains the key, so it wears the same gate.
//   Documents one row per anonymised document with its _anon output name,
//                  its counts, its per-format buttons and a copy button. The
//                  properties review for a same-format save opens inline here,
//                  because that is where the button that opened it lives.
//
// There is no native confirm(): the key warning is the in-app
// modal, and so is the confirmation on START A NEW BATCH.

import {
  exportDocumentFormats, saveDocument, exportAllZipTo, chooseExportFolder,
  copyDocument, exportMapping, exportReport,
  getSameFormatMetadata, saveSameFormat,
} from "../api.js";
import {
  getState, setState, addValues, defaultCategories, adoptCategories,
  setMetaReview, setExportDir, startNewBatch, setDocumentCountry,
  HEURISTIC_DISCOVERY_DEFAULTS, SIGNAL_SOURCES, SIGNAL_DERIVATIONS,
} from "../state.js";
import { escapeHTML } from "../html.js";
import { button, card, collapsibleGroup, wireGroups, sectionLabel, toastHTML } from "../ui.js";
import { askConfirm } from "../modal.js";
import { notify, wireNotice } from "../toast.js";
import { stepFooterHTML, wireStepFooter } from "../nav.js";
import { CARDS, EXPORT } from "../copy.js";
import { DEFAULT_COUNTRY } from "../countries.js";

// Native Office extensions are the same-format copies: they
// keep the original layout and never touch the source file.
const NATIVE_EXTS = new Set(["docx", "pptx", "xlsx", "pdf"]);

// Which of the foldable cards are shut. Value mapping starts OPEN because it
// is the one a user is most likely to be looking for and the one that needs
// its warning read; the report starts shut.
const collapsed = new Set(["report"]);

// Per-document format lists, fetched once per render (name → [ext...]).
const formatCache = new Map();

export function renderExport(container) {
  const s = getState();
  const docs = s.results?.documents ?? [];

  container.innerHTML = `
    <div class="export-view">
      <div class="workspace workspace-side">
        <div class="card-column">
          ${exportCard(s, docs)}
          ${mappingCard()}
          ${reportCard(s)}
        </div>
        ${documentsCard(s, docs)}
      </div>
      ${footerBar(s)}
      <div id="export-msg"></div>
    </div>
  `;

  ensureFormats(docs);
  wire(container, s);
}

// --- The Export card ------------------------------------------------------

function exportCard(s, docs) {
  const total = docs.reduce(
    (sum, d) => sum + Object.values(d.byCategory ?? {}).reduce((a, b) => a + b, 0), 0);

  const destination =
    sectionLabel(EXPORT.destination) +
    `<div class="add-row">` +
    `<input id="export-dir" class="grow mono" value="${escapeHTML(s.exportDir)}"` +
    ` placeholder="${escapeHTML(EXPORT.destinationPlaceholder)}"` +
    ` aria-label="${escapeHTML(EXPORT.destination)}"/>` +
    button(EXPORT.browse, { kind: "secondary", id: "btn-browse", icon: "folder_open" }) +
    `</div>`;

  return card({
    id: "export-card",
    title: CARDS.export.title,
    subtitle: EXPORT.subtitle(total, docs.length),
    bodyCls: "stack",
    bodyHTML: destination +
      button(EXPORT.zip, {
        kind: "primary", id: "btn-zip", icon: "download",
        disabled: docs.length === 0,
        title: docs.length === 0 ? EXPORT.needsRun : EXPORT.zipTooltip,
      }) +
      `<p class="hint">${escapeHTML(EXPORT.anonSuffixHint)}</p>`,
  });
}

// --- The three foldable cards --------------------------------------------

/** mappingCard() is the re-identification key. It wears the key tint, which is
 *  the only place in the application the orange family is used as a fill. */
function mappingCard() {
  const body =
    `<p class="hint">${escapeHTML(EXPORT.mappingHint)}</p>` +
    `<div class="button-pair">` +
    button("CSV", { kind: "secondary", id: "map-csv" }) +
    button("JSON", { kind: "secondary", id: "map-json" }) +
    `</div>`;
  return foldCard("mapping", EXPORT.mappingTitle, EXPORT.keyTag, body, true);
}

/** reportCard(s) contains NO original values, which is why it has no warning and
 *  no tint: it is the one artefact here that is safe to circulate. */
function reportCard(s) {
  const categories = Object.keys(s.results?.report?.byCategory ?? {}).length;
  const body =
    `<p class="hint">${escapeHTML(EXPORT.reportHint)}</p>` +
    `<div class="button-pair">` +
    button("JSON", { kind: "secondary", id: "rep-json" }) +
    button(EXPORT.markdown, { kind: "secondary", id: "rep-md" }) +
    `</div>`;
  return foldCard("report", EXPORT.reportTitle, EXPORT.reportSummary(categories), body);
}

/**
 * foldCard(id, title, summary, bodyHTML, keyBearing) is the shape the three
 * foldable cards share. Like the Anonymise screen's, they do NOT scroll
 * individually: the left column is one scroller.
 */
function foldCard(id, title, summary, bodyHTML, keyBearing = false) {
  return `<section class="card fold-card${keyBearing ? " key-bearing" : ""}">` +
    collapsibleGroup(id, title, `<div class="fold-body">${bodyHTML}</div>`, {
      open: !collapsed.has(id),
      countLabel: summary,
      cls: keyBearing ? "key-group" : "",
    }) +
    `</section>`;
}

// --- The Documents card ---------------------------------------------------

function documentsCard(s, docs) {
  const total = docs.reduce(
    (sum, d) => sum + Object.values(d.byCategory ?? {}).reduce((a, b) => a + b, 0), 0);

  const rows = docs.map((d) => documentRow(s, d)).join("") ||
    `<div class="grid-empty">${escapeHTML(EXPORT.noResults)}</div>`;

  // The properties review opens INSIDE this card, under the rows, because the
  // button that opened it is in one of those rows.
  const reviews = Object.entries(s.metaReview ?? {})
    .map(([docName, review]) => metaReviewPanel(docName, review)).join("");

  return `<section class="card" id="export-documents">` +
    `<div class="card-head">` +
    `<div class="card-head-left"><h2>${escapeHTML(EXPORT.documentsTitle)}</h2>` +
    `<span class="card-sub">${escapeHTML(EXPORT.documentsSummary(total))}</span></div>` +
    `<span class="card-caption">${escapeHTML(EXPORT.oneAtATime)}</span>` +
    `</div>` +
    `<div class="card-body">${rows}${reviews}</div>` +
    toastHTML(s.notice) +
    `</section>`;
}

function documentRow(s, d) {
  const formats = formatCache.get(d.name) ?? [];
  const replacements = Object.values(d.byCategory ?? {}).reduce((a, b) => a + b, 0);
  // The property count is only known once a review has been loaded for this
  // document. Fetching the metadata of every file up front would mean one bridge
  // round-trip per document on entering the screen, to show a number the user
  // has not asked for yet.
  const properties = s.metaReview?.[d.name]?.fields?.length;

  const buttons = formats.map((ext) => {
    const native = NATIVE_EXTS.has(ext);
    const label = native ? EXPORT.sameFormatLabel(ext) : `.${ext}`;
    const title = ext === "pdf" ? EXPORT.pdfCaption
      : native ? EXPORT.nativeCaption : EXPORT.plainCaption;
    return button(label, {
      kind: "secondary", cls: `fmt-btn${native ? " same-format" : ""}`,
      title, data: { name: d.name, ext },
    });
  }).join("");

  return `<div class="export-row">` +
    `<span class="export-name">` +
    `<span class="export-out" title="${escapeHTML(outputName(d.name))}">` +
    `${escapeHTML(outputName(d.name))}</span>` +
    `<span class="hint">${escapeHTML(EXPORT.rowMeta(replacements, properties))}</span>` +
    `</span>` +
    `<span class="export-formats">${buttons || `<span class="hint">${escapeHTML(EXPORT.loadingFormats)}</span>`}</span>` +
    button("", {
      kind: "ghost", cls: "doc-copy icon-action boxed", icon: "content_copy",
      ariaLabel: `Copy ${d.name}`, title: EXPORT.copyTooltip,
      data: { name: d.name },
    }) +
    `</div>`;
}

/**
 * outputName(name) is the file name an export suggests: the original with an
 * _anon suffix before its extension.
 *
 * It mirrors Go's engine.ExportFileName for DISPLAY only. The name that is
 * actually written still comes from Go, so this is a preview rather than a second
 * implementation of the rule: showing the user "services-agreement_anon.docx"
 * before they press anything is worth the duplication.
 */
export function outputName(name) {
  const dot = name.lastIndexOf(".");
  if (dot <= 0) return `${name}_anon`;
  return `${name.slice(0, dot)}_anon${name.slice(dot)}`;
}

// --- The properties review --------------------------

/**
 * metaReviewPanel(docName, review) renders one document's properties review:
 * the current value, the editable proposed value, keep-original per field, the
 * filename proposal, and the export button.
 *
 * Nothing is rewritten without this review. These properties travel INSIDE the
 * file, so a document whose body is anonymised but whose Author field still names
 * the person is not anonymised at all, and that is a failure the user cannot see
 * by reading the document.
 */
function metaReviewPanel(docName, review) {
  const rows = review.fields.map((f, i) => {
    const changed = f.finalValue !== f.value;
    return `<span class="meta-name">` +
      `<span>${escapeHTML(f.name)}</span>` +
      `<span class="hint">${escapeHTML(f.part)}</span>` +
      `</span>` +
      `<span class="meta-current" title="${escapeHTML(f.value)}">${escapeHTML(f.value)}</span>` +
      `<input class="meta-value mono" data-idx="${i}" value="${escapeHTML(f.finalValue)}"` +
      ` aria-label="${escapeHTML(EXPORT.willBecome)}: ${escapeHTML(f.name)}"/>` +
      (changed
        ? button(EXPORT.keepOriginal, {
          kind: "secondary", cls: "meta-keep", data: { idx: String(i) },
          title: EXPORT.keepOriginalTooltip,
        })
        : `<span class="hint meta-unchanged">${escapeHTML(EXPORT.unchanged)}</span>`);
  }).join("");

  const grid = rows
    ? `<div class="meta-grid">` +
      sectionLabel(EXPORT.property, { mini: true }) +
      sectionLabel(EXPORT.current, { mini: true }) +
      sectionLabel(EXPORT.willBecome, { mini: true }) +
      `<span></span>` + rows +
      `</div>`
    : `<p class="hint">${escapeHTML(EXPORT.noProperties)}</p>`;

  return `<section class="card key-bearing meta-review" data-doc="${escapeHTML(docName)}">` +
    `<div class="card-head">` +
    `<div class="card-head-left">` +
    `<h2>${escapeHTML(EXPORT.reviewTitle(docName))}</h2></div>` +
    `<span class="key-tag">${escapeHTML(review.ext.toUpperCase())}</span>` +
    `</div>` +
    `<div class="card-body stack">` +
    `<p class="hint">${escapeHTML(EXPORT.reviewHint)}</p>` +
    grid +
    pdfTextNote(review.pdfText) +
    imageNote(review.images) +
    `<div class="add-row">` +
    `<span class="hint">${escapeHTML(EXPORT.fileName)}</span>` +
    `<input class="meta-filename grow mono" value="${escapeHTML(review.filename)}"` +
    ` aria-label="${escapeHTML(EXPORT.fileName)}"/>` +
    button(EXPORT.close, { kind: "secondary", cls: "meta-cancel" }) +
    button(EXPORT.exportCopy(review.ext), { kind: "primary", cls: "meta-export" }) +
    `</div></div></section>`;
}

/**
 * imageNote(summary) is the picture line above the review's export button.
 *
 * Go answers with the counts, or with nothing at all for a format that has no
 * image review and for a document with no pictures, and nothing at all is what
 * this renders then: a line reading "0 images" on a .txt-derived copy is noise,
 * and one on a PDF would contradict the IMAGE tab, which says a PDF export has
 * already dropped every picture.
 *
 * The all-kept sentence is the one that earns this line. A user who never opened
 * the IMAGE tab has decided nothing, and this is the last surface before the file
 * is written, so it is the only place they can be told that the client logo and
 * the screenshot of the client's own system are going out untouched.
 *
 * @param {{kept:number, boxed:number, blurred:number, removed:number}|null|undefined} summary
 * @returns {string} the line's HTML, or "" when there is nothing to say
 */
function imageNote(summary) {
  if (!summary) return "";
  const changed = (summary.boxed ?? 0) + (summary.blurred ?? 0) + (summary.removed ?? 0);
  const total = changed + (summary.kept ?? 0);
  if (total === 0) return "";
  const text = changed > 0 ? EXPORT.imagesChanged(summary) : EXPORT.imagesAllKept(total);
  return `<p class="hint image-note">${escapeHTML(text)}</p>`;
}

/**
 * pdfTextNote(plan) is the in-place PDF export's line above the review's
 * export button: what the location ladder will do to the text, and, when
 * something cannot be located at all, that the export WILL refuse and the
 * .md export is the way out.
 *
 * Go answers with the plan for a .pdf review only, and nothing at all for the
 * other formats, and nothing at all is what this renders then: the OOXML
 * exports splice by offset and have no ladder to describe.
 *
 * @param {{counts: {literal:number, tolerant:number, fragment:number, wrapped:number},
 *          unlocated?: Array<{placeholder:string, page:number}>}|null|undefined} plan
 * @returns {string} the line's HTML, or "" when there is nothing to say
 */
function pdfTextNote(plan) {
  if (!plan) return "";
  if (plan.unlocated?.length) {
    return `<p class="hint pdf-text-note pdf-text-refusal">${escapeHTML(EXPORT.pdfUnlocated(plan.unlocated.length))}</p>`;
  }
  return `<p class="hint pdf-text-note">${escapeHTML(EXPORT.pdfLadder(plan.counts))}</p>`;
}

// --- The footer -----------------------------------------------------------

/**
 * footerBar(s) is Export's footer. It has no CONTINUE: this is the last step, so
 * the right-hand action is START A NEW BATCH, and it deliberately does NOT look
 * like the primary of the other three screens. Nothing about finishing should
 * invite a reflex click.
 */
function footerBar(s) {
  const finished = (s.results?.documents?.length ?? 0) > 0;
  return stepFooterHTML({
    hint: EXPORT.finishHint,
    standalone: true,
    // NOT the shared primary: nothing about finishing should invite the same
    // reflex click as CONTINUE on the other three screens.
    rightHTML: button(EXPORT.newBatch, {
      kind: "secondary", id: "btn-new-batch", icon: "refresh", cls: "step-next new-batch",
      disabled: !finished, title: finished ? EXPORT.newBatchTooltip : EXPORT.needsRun,
    }),
  }, s);
}

// --- Formats --------------------------------------------------------------

/** ensureFormats(docs) lazily fetches each document's offered extensions and
 *  re-renders (via state notify) once known. */
function ensureFormats(docs) {
  for (const d of docs) {
    if (formatCache.has(d.name)) continue;
    formatCache.set(d.name, []); // mark as pending to avoid a refetch loop
    exportDocumentFormats(d.name)
      .then((formats) => {
        formatCache.set(d.name, formats ?? []);
        setState({}); // re-render with the buttons filled in
      })
      .catch(() => formatCache.delete(d.name));
  }
}

// --- Wiring ---------------------------------------------------------------

function wire(container, s) {
  wireGroups(container, (id) => {
    if (collapsed.has(id)) collapsed.delete(id); else collapsed.add(id);
    setState({});
  });

  wireDestination(container);
  wireKeyBearing(container);
  wireReport(container);
  wireDocuments(container);
  wireMetaReview(container, s);
  wireNewBatch(container);
  wireNotice(container);
  wireStepFooter(container);
}

function wireDestination(container) {
  const input = container.querySelector("#export-dir");
  input?.addEventListener("change", () => setExportDir(input.value));

  container.querySelector("#btn-browse")?.addEventListener("click", async () => {
    try {
      const dir = await chooseExportFolder();
      if (!dir) return; // cancelled: nothing happened, so say nothing
      setExportDir(dir);
    } catch (err) {
      notify(String(err?.message ?? err), "warn");
    }
  });

  container.querySelector("#btn-zip")?.addEventListener("click", async () => {
    // The input is read from the store rather than the element, so a path typed
    // and not blurred is still committed by the change handler above first.
    const dir = getState().exportDir || (input?.value ?? "").trim();
    if (!dir) {
      notify(EXPORT.zipNeedsFolder, "info");
      return;
    }
    setExportDir(dir);
    try {
      const path = await exportAllZipTo(dir);
      notify(EXPORT.zipDone(path), "ok");
    } catch (err) {
      notify(String(err?.message ?? err), "warn");
    }
  });
}

/**
 * wireKeyBearing(container) gates the two key-bearing exports behind the in-app
 * modal).
 *
 * The confirm button names the ACTION ("Export CSV") rather than saying "OK", so
 * the last thing the user reads before the key leaves the application is what is
 * about to happen.
 */
function wireKeyBearing(container) {
  const gate = async (title, confirmLabel, run, done) => {
    const ok = await askConfirm({
      title, body: EXPORT.keyWarningBody, confirmLabel, keyBearing: true,
    });
    if (!ok) return;
    try {
      await run();
      notify(done, "warn"); // "it worked AND the result needs care"
    } catch (err) {
      notify(String(err?.message ?? err), "warn");
    }
  };

  container.querySelector("#map-csv")?.addEventListener("click", () =>
    gate(EXPORT.mappingCsvTitle, EXPORT.mappingCsvConfirm,
      () => exportMapping("csv"), EXPORT.mappingCsvDone));
  container.querySelector("#map-json")?.addEventListener("click", () =>
    gate(EXPORT.mappingJsonTitle, EXPORT.mappingJsonConfirm,
      () => exportMapping("json"), EXPORT.mappingJsonDone));
}

function wireReport(container) {
  const save = async (format, done) => {
    try {
      await exportReport(format);
      notify(done, "ok");
    } catch (err) {
      notify(String(err?.message ?? err), "warn");
    }
  };
  container.querySelector("#rep-json")?.addEventListener("click", () =>
    save("json", EXPORT.reportJsonDone));
  container.querySelector("#rep-md")?.addEventListener("click", () =>
    save("md", EXPORT.reportMdDone));
}

/**
 * applySession(session) restores the frontend state from a loaded session.
 *
 * Go has already restored the registry and the settings; this is the other half.
 *
 * There are no per-field defaults for a missing value: the loader refuses a
 * file written by any other version, so a session that reaches here has every
 * field. The two `??` below are the genuine cases: a field legitimately absent
 * because the user never set it, and the settings that describe the MACHINE
 * rather than the batch.
 *
 * Exported so identity of the restore (which flags an absent field turns back
 * on) can be unit-tested without a bridge.
 */
export function applySession(session) {
  const settings = session.settings ?? {};
  const current = getState().settings;
  setState({
    values: [],
    allowlist: session.allowTerms ?? [],
    patterns: (session.patterns ?? []).map((p) => ({ expr: p.expr, error: null })),
    settings: {
      // `categories` is omitted from the file when it equals nothing at all,
      // which cannot happen for a session this build wrote; the default
      // selection is the safe reading either way and beats an empty selection
      // that silently anonymises nothing.
      //
      // The file's `presets` is deliberately NOT restored into the store: which
      // preset each row is on is DERIVED from the categories (activePreset), so
      // restoring the selection restores the chips with it and the two cannot
      // come back disagreeing.
      categories: adoptCategories(settings.categories ?? defaultCategories(settings.country)),
      ollamaPort: settings.ollamaPort,
      model: settings.model,
      contextSize: settings.contextSize,
      useLocalLLM: settings.useLocalLLM,
      // Absent means ON for both offline routes: they are the shipped defaults,
      // and a file that says nothing about one must not restore it switched off.
      // Each of the three route switches is restored from its OWN key; there is
      // no fourth section flag to restore.
      useBuiltInPatterns: settings.useBuiltInPatterns !== false,
      useHeuristicDiscovery: settings.useHeuristicDiscovery !== false,
      // Same rule per READING: a key the file omits falls back to the default
      // rather than to off, at either level, so a reading cannot be silently
      // disabled by silence.
      signalSuggestionSources: Object.fromEntries(SIGNAL_SOURCES.map((source) =>
        [source, Object.fromEntries((SIGNAL_DERIVATIONS[source] ?? []).map((d) =>
          [d, settings.signalSuggestionSources?.[source]?.[d] !== false]))])),
      // Absent means OFF, which is both the shipped default and what the file
      // was written under: a session file that says nothing about the checksum
      // switch was saved with it off.
      requireChecksum: settings.requireChecksum === true,
      // A session that deliberately turned every heuristic filter off writes
      // zeroes, which must be obeyed; one that says nothing about them gets the
      // shipped defaults. The pointer on the Go side is what keeps those two
      // apart, and the spread preserves the distinction here.
      heuristicDiscovery: { ...HEURISTIC_DISCOVERY_DEFAULTS, ...(settings.heuristicDiscovery ?? {}) },
    },
  });
  // The document country is not part of a session file: it is a frontend-only
  // display choice. Re-applying the default keeps the rail's
  // country and its identifier switches agreeing after a load, which is the one
  // way they could otherwise drift.
  setDocumentCountry(DEFAULT_COUNTRY);
  // Loaded values arrive ACCEPTED: a session records what the user decided to
  // replace, not what was once proposed to them.
  addValues((session.values ?? []).map((e) => ({
    category: e.category, mainText: e.mainText, spellings: e.spellings ?? [],
  })));
  if (current.contextSize && !settings.contextSize) {
    // Go treats 0 as "keep the current setting"; mirror that here rather than
    // dropping the connection setting to zero.
    setState({ settings: { ...getState().settings, contextSize: current.contextSize } });
  }
}

function wireDocuments(container) {
  for (const btn of container.querySelectorAll(".fmt-btn")) {
    btn.addEventListener("click", async () => {
      const { name, ext } = btn.dataset;
      if (NATIVE_EXTS.has(ext)) {
        // A same-format save goes through the properties review FIRST
        // Decisions persist per document for the session,
        // so the review only happens before the first such save of a file.
        if (getState().metaReview?.[name]?.ext === ext) {
          notify(EXPORT.reviewAlreadyOpen(name), "info");
          return;
        }
        try {
          const meta = await getSameFormatMetadata(name, ext);
          setMetaReview(name, {
            ext,
            filename: meta?.filename ?? "",
            fields: (meta?.fields ?? []).map((f) => ({ ...f, finalValue: f.proposed })),
            // Absent for a format with no image review, which is what makes the
            // panel say nothing rather than "0 images".
            images: meta?.images ?? null,
            // Absent for every format but .pdf: only the in-place PDF export
            // has a location ladder to describe.
            pdfText: meta?.pdfText ?? null,
          });
        } catch (err) {
          notify(String(err?.message ?? err), "warn");
        }
        return;
      }
      try {
        await saveDocument(name, ext);
        notify(EXPORT.savedPlain(name, ext), "ok");
      } catch (err) {
        notify(String(err?.message ?? err), "warn");
      }
    });
  }

  for (const btn of container.querySelectorAll(".doc-copy")) {
    btn.addEventListener("click", async () => {
      try {
        await copyDocument(btn.dataset.name);
        notify(EXPORT.copied(btn.dataset.name), "ok");
      } catch (err) {
        notify(String(err?.message ?? err), "warn");
      }
    });
  }
}

function wireMetaReview(container, s) {
  for (const panel of container.querySelectorAll(".meta-review")) {
    const docName = panel.dataset.doc;
    const review = s.metaReview?.[docName];
    if (!review) continue;

    for (const input of panel.querySelectorAll(".meta-value")) {
      input.addEventListener("change", () => {
        const i = parseInt(input.dataset.idx, 10);
        review.fields[i].finalValue = input.value;
        setMetaReview(docName, review);
      });
    }
    for (const keep of panel.querySelectorAll(".meta-keep")) {
      keep.addEventListener("click", () => {
        const i = parseInt(keep.dataset.idx, 10);
        review.fields[i].finalValue = review.fields[i].value;
        setMetaReview(docName, review);
      });
    }
    panel.querySelector(".meta-filename")?.addEventListener("change", (ev) => {
      review.filename = ev.target.value;
      setMetaReview(docName, review);
    });
    panel.querySelector(".meta-cancel")?.addEventListener("click", () =>
      setMetaReview(docName, null));

    panel.querySelector(".meta-export")?.addEventListener("click", async () => {
      const reviewed = review.fields.map((f) => ({
        part: f.part, name: f.name, value: f.finalValue,
      }));
      const filename = review.filename;
      try {
        await saveSameFormat(docName, review.ext, reviewed, filename);
        setMetaReview(docName, null);
        // "warn" rather than "ok": it worked AND the result needs care, because a
        // same-format copy looks exactly like the original.
        notify(EXPORT.sameFormatDone(filename), "warn");
      } catch (err) {
        notify(String(err?.message ?? err), "warn");
      }
    });
  }
}

function wireNewBatch(container) {
  container.querySelector("#btn-new-batch")?.addEventListener("click", async () => {
    const ok = await askConfirm({
      title: EXPORT.newBatchTitle,
      body: EXPORT.newBatchBody,
      confirmLabel: EXPORT.newBatchConfirm,
    });
    if (!ok) return;
    startNewBatch();
    notify(EXPORT.newBatchDone, "info");
  });
}

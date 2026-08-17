// views/anonymise.js, wizard step 3: run the pipeline and check the result
//
// A column of cards on the left, one big Compare card on the right:
//
//   Run                  RUN / RUN AGAIN, Cancel, the progress bar and the
//                        four stat tiles.
//   Selected placeholder appears when a mark in the anonymised pane is clicked.
//                        It REPLACES the floating reassign popover: a popover
//                        anchored inside a scrolling pane drifted away from the
//                        mark it belonged to, and it could not be reached by
//                        keyboard at all.
//   Replaced values one row per value the run replaced, with an editable
//                        placeholder and a remove action, plus the collapsed
//                        list of removed values with restore. This is THE
//                        surface for both rules: a value can be renamed and a
//                        value can be removed, whatever trigger found it.
//   Report the per-category drill-down, scoped to all files or
//                        one, plus the run's dismissible warnings.
//   Something missed?    add a value, then re-run the fast passes.
//   Find and replace the ordered rules that run last.
//
// The Compare card's two panes are the one place the anonymisation is actually
// checked, which is why they get two thirds of the screen: hovering a mark shows
// its original, clicking one selects it, and selecting free text offers to
// replace it with a [CUSTOM_n] rule.

import {
  runPipeline, cancelPipeline, fastRerun, getMapping, getDocumentSource,
  valuePlaceholders, listRemovedValues, setValuePlaceholder, removeValue,
  restoreValue, nextRulePlaceholder,
} from "../api.js";
import {
  getState, setState,
  buildRunRequest, documentSource, cacheDocumentSource,
  addSimpleRule, removeSimpleRule, moveSimpleRule,
  entityAutocomplete, reassignOriginal, addEntities,
  setValueTables, dismissWarning, visibleWarnings, blockingConflicts,
} from "../state.js";
import { escapeHTML } from "../html.js";
import { renderHighlighted } from "../highlight.js";
import { findHits, renderPlainWithHits, MAX_HITS } from "../panesearch.js";
import { button, card, statTile, collapsibleGroup, wireGroups, icon, sectionLabel } from "../ui.js";
import { CATEGORIES } from "./identifyworkspace.js";
import { stepFooterHTML, wireStepFooter } from "../nav.js";
import { notify, wireNotice } from "../toast.js";
import { CARDS, ANONYMISE, CATEGORY_LABELS, IMPORT } from "../copy.js";
import { toastHTML } from "../ui.js";

// --- View-local state -----------------------------------------------------

// Which of the four collapsible cards are folded shut. All four start closed:
// the result cards open on demand, so the screen after a run is a compact
// column of headings rather than a wall of tables.
const collapsed = new Set(["missed", "rules", "selected", "removed", "values", "report"]);
// Which report categories are expanded, by category key.
const expandedCategories = new Set();
// The report's scope: "__all" or one document name.
let reportScope = "__all";
// The Replaced values filter text. View state: a way of looking at the result,
// not part of it.
let valueFilter = "";
// Per-row feedback from a refused rename, keyed by placeholder. A refusal
// belongs ON the row, because it is about that value and the fix is in that
// field.
const rowErrors = new Map();
// The results object the value tables were loaded for, so the load happens once
// per run rather than on every repaint.
let tablesLoadedFor = null;
// The clicked placeholder, or null: {placeholder, original, category}.
let selectedMark = null;
// The reassign autocomplete's draft text.
let reassignDraft = "";
// Feedback from a refused placeholder rename in the Selected placeholder card.
// It belongs ON the card, next to the field the fix goes into, for the same
// reason a refused rename in the Replaced values table lands on its own row.
let selectedError = "";
// A live text selection in either pane, or null: {text, x, y}. Coordinates are
// relative to the Compare card, so the floating panel follows the selection.
let selection = null;
let selectionDraft = "";
// The Compare search: a way of LOOKING at the result, not part of it, so it
// lives here and not in the store. `index` walks ONE combined list, every
// original-pane hit then every anonymised-pane hit, because the two panes have
// different match counts and a single shared index over lists of different
// lengths would drift.
//
// Hits are recomputed during render from the current needle rather than cached,
// which keeps them in step with the text with nothing to invalidate. It is
// reset when the compared document changes or a new run lands: offsets belong
// to one text.
let search = { needle: "", index: 0 };
// The hit the pane was last scrolled to, so scrolling happens only when the
// active hit CHANGED. scroll.js restores each pane's offset on every repaint,
// and scrolling unconditionally would fight it and drag the pane back on every
// keystroke.
let lastScrolledTo = null;
// The three add rows' draft text, kept across repaints.
const drafts = { missedCategory: "person_names", missed: "", find: "", replace: "", caseSensitive: false };

export function renderAnonymise(container) {
  const s = getState();
  const doc = currentDocument(s);
  // A refused run carries empty documents and an empty report, so the value,
  // report and "something missed" cards would show a zero run beside a stale
  // registry table: the exact mismatch a refused run produces. They are hidden
  // until the conflict is fixed, and the run card explains why.
  const blocked = blockingConflicts(s).length > 0;

  container.innerHTML = `
    <div class="anonymise-view">
      <div class="workspace workspace-side">
        <div class="card-column">
          ${runCard(s)}
          ${selectedMark ? selectedCard(s) : ""}
          ${s.results && !blocked ? valuesCard(s) : ""}
          ${s.results && !blocked ? reportCard(s) : ""}
          ${s.results && !blocked ? missedCard(s) : ""}
          ${s.results ? rulesCard(s) : ""}
        </div>
        ${compareCard(s, doc)}
      </div>
      ${stepFooterHTML({
        hint: continueHint(s),
        nextDisabled: !s.results || blocked,
        nextTitle: continueBlockedTitle(s),
        standalone: true,
      }, s)}
      <div id="run-error"></div>
    </div>
  `;

  wire(container, s, doc);
}

/** continueBlockedTitle(s) is the disabled CONTINUE button's tooltip: it names
 *  the reason the step cannot be left, so a greyed-out button is never mute. */
function continueBlockedTitle(s) {
  if (blockingConflicts(s).length > 0) return ANONYMISE.continueBlocked;
  if (!s.results) return ANONYMISE.continueNeedsRun;
  return "";
}

/** currentDocument(s) is the document the Compare card shows. */
function currentDocument(s) {
  const documents = s.results?.documents ?? [];
  return documents.find((d) => d.name === s.resultDoc) ?? documents[0] ?? null;
}

// --- Run card -------------------------------------------------------------

export function runCard(s) {
  const done = !!s.results && !s.running;

  const actions =
    `<div class="run-actions">` +
    // RUN is the primary until there is a result; after that RUN AGAIN steps
    // back to a neutral button, because the loud element on a finished screen
    // belongs to CONTINUE TO EXPORT in the footer.
    button(done ? ANONYMISE.runAgain : ANONYMISE.run, {
      kind: done ? "secondary" : "primary", id: "btn-run", icon: "play_arrow",
      disabled: s.running || s.documents.length === 0,
      title: s.documents.length === 0 ? ANONYMISE.runNeedsDocuments : "",
    }) +
    button(ANONYMISE.cancel, {
      kind: "secondary", id: "btn-cancel", disabled: !s.running,
      title: s.running ? ANONYMISE.cancelTooltip : ANONYMISE.cancelIdleTooltip,
    }) +
    `</div>`;

  // The explanatory sentence rides on the heading as a hover tooltip rather
  // than a visible subtitle: the run card is a compact control strip, and the
  // status line was repeating what the progress bar and stat tiles already say.
  return card({
    id: "run-card", title: CARDS.run.title, titleTooltip: runSubtitle(s),
    bodyCls: "stack",
    bodyHTML: actions + progressStrip(s) + blockedPanel(s) + statsRow(s),
  });
}

/**
 * blockedPanel(s) explains a refused run.
 *
 * A blocking conflict aborts inside the engine before pass 1, so the results
 * carry no documents and an empty report: the summary reads 0/0/0 while an
 * earlier run's registry still fills the value table. Without this panel that
 * looks like a run that silently did nothing. It lists every conflict's message
 * and its fix, and it is not dismissible: the conflict, not the user, decides
 * when it goes away.
 */
export function blockedPanel(s) {
  const conflicts = blockingConflicts(s);
  if (conflicts.length === 0) return "";
  const items = conflicts.map((c) => {
    const fix = c.fix
      ? `<div class="hint"><strong>${escapeHTML(ANONYMISE.blockedFixLabel)}:</strong> ${escapeHTML(c.fix)}</div>`
      : "";
    return `<li>${escapeHTML(c.message)}${fix}</li>`;
  }).join("");
  return `<div class="banner error blocked-banner" role="alert">` +
    `<span class="banner-icon">${icon("warning")}</span>` +
    `<div class="banner-body">` +
    `<strong>${escapeHTML(ANONYMISE.blockedTitle)}</strong>` +
    `<p class="hint">${escapeHTML(ANONYMISE.blockedIntro)}</p>` +
    `<ul class="blocked-list">${items}</ul>` +
    `</div></div>`;
}

function runSubtitle(s) {
  if (s.running) return ANONYMISE.subtitleRunning;
  if (blockingConflicts(s).length > 0) return ANONYMISE.subtitleBlocked;
  if (s.results) return ANONYMISE.subtitleDone;
  return ANONYMISE.subtitleIdle(s.documents.length);
}

function progressStrip(s) {
  if (!s.running) return "";
  const p = s.progress;
  const pct = p?.docCount ? Math.round(((p.docIndex + 1) / p.docCount) * 100) : 0;
  const label = p
    ? ANONYMISE.progress(p.stage, p.docName, p.docIndex + 1, p.docCount)
    : ANONYMISE.progressStarting;
  return `<div class="run-progress">` +
    `<div class="progress-bar"><div style="width:${pct}%"></div></div>` +
    `<span class="hint mono">${escapeHTML(label)}</span></div>`;
}

/** statsRow(s) is the four figures the run produced. Nothing before a run: four
 *  zeroes would look like a finished run that found nothing. */
function statsRow(s) {
  if (!s.results || s.running) return "";
  // A refused run has results but changed nothing: the blocked panel above says
  // so. Four zeroes here would contradict it by reading as a finished run.
  if (blockingConflicts(s).length > 0) return "";
  const report = s.results.report ?? {};
  const categories = Object.keys(report.byCategory ?? {}).length;
  const duration = report.durationMs ?? 0;
  return `<div class="stat-row">` +
    statTile(report.totalReplacements ?? 0, ANONYMISE.statReplacements) +
    statTile(s.results.documents?.length ?? 0, ANONYMISE.statDocuments) +
    statTile(categories, ANONYMISE.statCategories) +
    statTile(formatDuration(duration), ANONYMISE.statDuration) +
    `</div>`;
}

/** formatDuration(ms) reads as a duration rather than as a raw count: "1.8 s"
 *  beats "1840 ms" for a figure nobody is going to do arithmetic on. */
export function formatDuration(ms) {
  if (!ms || ms < 0) return "0 ms";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}

// --- Selected placeholder card --------------------------------------------

/**
 * selectedCard(s) is what replaced the floating reassign popover.
 *
 * The popover was anchored to a mark inside a scrolling pane, so it drifted away
 * from its own mark the moment the pane scrolled, and it could not be reached
 * from the keyboard. A card in the left column stays put, has room for the
 * explanation, and is the same shape as everything else on the screen.
 *
 * The placeholder is editable here for the same reason it is editable in the
 * Replaced values table: both read and write the one registry entry behind the
 * mark, and a user looking at a single value should be able to change what it
 * becomes without hunting for it in the full list. Like the table, the rename
 * takes effect on the NEXT run and not on the text already shown.
 */
export function selectedCard(s, mark = selectedMark) {
  const matches = reassignDraft.trim()
    ? entityAutocomplete(reassignDraft, s).slice(0, 6)
    : [];
  const suggestions = matches.map((m) =>
    `<button class="btn btn-secondary reassign-pick"` +
    ` data-category="${escapeHTML(m.category)}" data-canonical="${escapeHTML(m.canonical)}">` +
    `${escapeHTML(m.canonical)}` +
    `<span class="hint">${escapeHTML(CATEGORY_LABELS[m.category]?.[0] ?? m.category)}</span>` +
    `</button>`).join("");

  const body =
    `<div class="rail-field-stack">` +
    `<span class="hint">${escapeHTML(ANONYMISE.placeholderLabel)}</span>` +
    `<div class="selected-pair">` +
    `<input id="selected-ph-input" class="ph-input mono" value="${escapeHTML(mark.placeholder)}"` +
    ` title="${escapeHTML(ANONYMISE.placeholderTooltip)}"` +
    ` aria-label="${escapeHTML(ANONYMISE.placeholderLabel)}"/>` +
    `<span class="hint">${escapeHTML(ANONYMISE.replaces)}</span>` +
    `<span class="selected-original">${escapeHTML(mark.original)}</span>` +
    `</div></div>` +
    (selectedError ? `<span class="hint bad value-error">${escapeHTML(selectedError)}</span>` : "") +
    `<label class="rail-field-stack" for="reassign-input">` +
    `<span class="hint">${escapeHTML(ANONYMISE.makeVariantOf)}</span>` +
    `<input id="reassign-input" value="${escapeHTML(reassignDraft)}" autocomplete="off"` +
    ` placeholder="${escapeHTML(ANONYMISE.reassignPlaceholder)}"/></label>` +
    (suggestions ? `<div class="reassign-list">${suggestions}</div>` : "") +
    `<p class="hint">${escapeHTML(ANONYMISE.reassignHint)}</p>`;

  return card({
    id: "selected-card", keyBearing: true,
    title: ANONYMISE.selectedTitle,
    headRightHTML: button("", {
      kind: "ghost", cls: "icon-action", icon: "close", id: "btn-clear-selection",
      ariaLabel: ANONYMISE.closeSelection, title: ANONYMISE.closeSelection,
    }),
    bodyCls: "stack", bodyHTML: body,
  });
}

// --- Report card ----------------------------------------------------------

/**
 * reportCard(s) answers "what did you replace?".
 *
 * It used to answer "how much", and only that: a list of category totals, each
 * of which had to be clicked open to see the values behind it. A user who had
 * just watched dozens of values disappear from their document could not find a
 * list of them anywhere, which is reported issue 7.
 *
 * So the VALUES come first now, as a flat, filterable list that is on screen
 * without clicking anything, and the per-category breakdown follows it. Both
 * read report.values from Go, which is also what the exported report
 * contains: the screen and the file can no longer disagree, and the counts are
 * computed once per run instead of on every repaint.
 */
export function reportCard(s) {
  const scopeDocs = scopedDocuments(s);
  const byCategory = aggregateCategories(scopeDocs);
  const total = Object.values(byCategory).reduce((a, b) => a + b, 0);

  const options = [{ value: "__all", label: ANONYMISE.scopeAll }]
    .concat((s.results.documents ?? []).map((d) => ({ value: d.name, label: d.name })))
    .map((o) =>
      `<option value="${escapeHTML(o.value)}"${o.value === reportScope ? " selected" : ""}>` +
      `${escapeHTML(o.label)}</option>`).join("");

  const rows = Object.entries(byCategory)
    .sort((a, b) => b[1] - a[1])
    .map(([category, count]) => categoryRow(s, scopeDocs, category, count))
    .join("") || `<div class="grid-empty">${escapeHTML(ANONYMISE.reportEmpty)}</div>`;

  const warnings = visibleWarnings(s).map((w) =>
    `<div class="caution" data-warning="${escapeHTML(w)}">${icon("warning")}` +
    `<span class="caution-text">${escapeHTML(w)}</span>` +
    button("", {
      kind: "ghost", cls: "warning-dismiss icon-action", icon: "close",
      ariaLabel: ANONYMISE.dismissWarning, title: ANONYMISE.dismissWarning,
    }) +
    `</div>`).join("");

  const body =
    `<select id="report-scope" class="rail-select" aria-label="${escapeHTML(ANONYMISE.scopeLabel)}">` +
    `${options}</select>` +
    runNote(s) +
    sectionLabel(ANONYMISE.byCategoryTitle) +
    `<div class="report-rows">${rows}</div>` +
    warnings;

  return collapsibleCard("report", ANONYMISE.reportTitle,
    ANONYMISE.reportSummary(total, scopedValues(s).length), body);
}

/**
 * runNote(s) surfaces what the run itself did, which the card used to ignore
 * entirely: the preset it ran at.
 */
function runNote(s) {
  const report = s.results?.report ?? {};
  const parts = [];
  if (report.level) parts.push(ANONYMISE.reportLevel(report.level));
  if (!parts.length) return "";
  return `<p class="hint" id="report-run-note">${escapeHTML(parts.join(". "))}.</p>`;
}

/**
 * valuesCard(s) is the Replaced values table: THE surface for the two rules
 * about a value once a run has produced it.
 *
 *   the replacement string is editable, for every trigger. A regex match, a
 *   detected value and a value typed by hand all appear here, because all three
 *   end up as one registry entry and the registry is what this reads.
 *   any value can be removed, and a removed value can be restored. Removal
 *   prunes the registry entry AND records a session exclusion, so it survives a
 *   re-run; restoring brings the value back with a NEW number, because the old
 *   one was retired the moment an export could have carried it.
 *
 * It carries the re-identification warning: unlike the exported report, this
 * list shows originals in clear.
 */
export function valuesCard(s) {
  const shown = filterValues(s.replacedValues, valueFilter);
  const rows = shown.map(valueRow).join("") ||
    `<span class="hint">${escapeHTML(
      s.replacedValues.length ? ANONYMISE.valuesFilterEmpty : ANONYMISE.reportEmpty)}</span>`;

  const body =
    `<span class="hint">${escapeHTML(ANONYMISE.valuesKeyWarning)}</span>` +
    `<input id="report-value-filter" class="report-filter" value="${escapeHTML(valueFilter)}"` +
    ` placeholder="${escapeHTML(ANONYMISE.valuesFilterPlaceholder)}"` +
    ` aria-label="${escapeHTML(ANONYMISE.valuesFilterPlaceholder)}"/>` +
    `<div class="report-detail-head">` +
    sectionLabel(ANONYMISE.valuePlaceholder, { mini: true }) +
    sectionLabel(ANONYMISE.occurrences, { mini: true }) +
    `</div>` +
    `<div class="report-value-rows" id="report-value-rows">${rows}</div>` +
    removedList(s);

  return collapsibleCard("values", ANONYMISE.valuesTitle,
    ANONYMISE.valuesSummary(s.replacedValues.length, s.removedValues.length), body);
}

/** valueRow(v) is one replaced value: what it was, what it becomes, how often,
 *  and the action that removes it. */
function valueRow(v) {
  const error = rowErrors.get(v.placeholder);
  return `<div class="report-item value-row" data-placeholder="${escapeHTML(v.placeholder)}">` +
    `<span class="report-item-value">` +
    `<span class="report-original">${escapeHTML(v.original)}</span>` +
    `<span class="hint">${escapeHTML(CATEGORY_LABELS[v.category]?.[0] ?? v.category)}</span>` +
    `</span>` +
    `<input class="ph-input mono" value="${escapeHTML(v.placeholder)}"` +
    ` title="${escapeHTML(ANONYMISE.placeholderTooltip)}"` +
    ` aria-label="${escapeHTML(ANONYMISE.placeholderLabel)}"/>` +
    `<span class="mono report-count">${escapeHTML(String(v.count))}</span>` +
    button("", {
      kind: "ghost", cls: "value-remove icon-action danger", icon: "close",
      ariaLabel: `${ANONYMISE.removeValue}: ${v.original}`, title: ANONYMISE.removeValue,
    }) +
    (error ? `<span class="hint bad value-error">${escapeHTML(error)}</span>` : "") +
    `</div>`;
}

/** removedList(s) is the collapsed half: what was removed, and the way back. */
function removedList(s) {
  if (!s.removedValues.length) return "";
  const rows = s.removedValues.map((v) =>
    `<div class="report-item removed-row" data-placeholder="${escapeHTML(v.placeholder)}">` +
    `<span class="report-item-value">` +
    `<span class="report-original">${escapeHTML(v.original)}</span>` +
    `<span class="hint">${escapeHTML(CATEGORY_LABELS[v.category]?.[0] ?? v.category)}</span>` +
    `</span>` +
    button(ANONYMISE.restoreValue, { kind: "ghost", cls: "value-restore" }) +
    `</div>`).join("");

  return collapsibleGroup("removed",
    ANONYMISE.removedTitle,
    `<p class="hint">${escapeHTML(ANONYMISE.removedHint)}</p>${rows}`,
    { open: !collapsed.has("removed"), countLabel: String(s.removedValues.length) });
}

/**
 * scopedValues(s) is the values Go reported for the documents in scope.
 *
 * Go computes both the whole-run list and a per-document one, so the scope
 * selector picks a list rather than recounting anything.
 */
export function scopedValues(s) {
  const report = s.results?.report ?? {};
  if (reportScope === "__all") return report.values ?? [];
  const doc = (report.documents ?? []).find((d) => d.name === reportScope);
  return doc?.values ?? [];
}

/** filterValues(values, needle) is the search box, over both the original and
 *  the placeholder: a user looks for either. */
export function filterValues(values, needle) {
  const q = (needle ?? "").trim().toLowerCase();
  if (!q) return values ?? [];
  return (values ?? []).filter((v) =>
    String(v.original ?? "").toLowerCase().includes(q) ||
    String(v.placeholder ?? "").toLowerCase().includes(q));
}

/** scopedDocuments(s) is the documents the report covers. */
function scopedDocuments(s) {
  const documents = s.results?.documents ?? [];
  return reportScope === "__all" ? documents : documents.filter((d) => d.name === reportScope);
}

/** aggregateCategories(docs) sums the per-category counts across documents. */
function aggregateCategories(docs) {
  const out = {};
  for (const d of docs) {
    for (const [category, n] of Object.entries(d.byCategory ?? {})) {
      out[category] = (out[category] ?? 0) + n;
    }
  }
  return out;
}

/** categoryRow(s, docs, category, count) is one expandable report row. */
function categoryRow(s, docs, category, count) {
  const open = expandedCategories.has(category);
  const label = CATEGORY_LABELS[category]?.[0] ?? category;

  let detail = "";
  if (open) {
    const items = valuesInCategory(s, docs, category);
    // Same data as the flat list above, filtered to this category.
    detail = `<div class="report-detail">` +
      `<div class="report-detail-head">` +
      sectionLabel(ANONYMISE.valuePlaceholder, { mini: true }) +
      sectionLabel(ANONYMISE.occurrences, { mini: true }) +
      `</div>` +
      (items.map((it) =>
        `<div class="report-item">` +
        `<span class="report-item-value">` +
        `<span class="report-original">${escapeHTML(it.original)}</span>` +
        `<span class="report-placeholder mono">${escapeHTML(it.placeholder)}</span>` +
        `</span>` +
        `<span class="mono report-count">${escapeHTML(String(it.count))}</span>` +
        `</div>`).join("") ||
        `<span class="hint">${escapeHTML(ANONYMISE.noValuesInScope)}</span>`) +
      `</div>`;
  }

  return `<div class="report-row" data-category="${escapeHTML(category)}">` +
    `<button class="report-toggle" aria-expanded="${open}">` +
    `<span class="icon chevron${open ? "" : " closed"}" aria-hidden="true">` +
    icon("expand_more").replace(/^<span class="icon" aria-hidden="true">|<\/span>$/g, "") +
    `</span>${escapeHTML(label)}</button>` +
    `<span class="mono report-count">${escapeHTML(String(count))}</span>` +
    detail +
    `</div>`;
}

/**
 * valuesInCategory(s, docs, category) is the values of ONE category, in scope.
 *
 * It now filters the list GO computed (report.values) instead of recounting
 * placeholder occurrences in the anonymised text of every document on every
 * repaint. Same numbers, computed once per run rather than once per render,
 * and identical to what the exported report contains, which the recount could
 * not promise.
 *
 * `docs` is no longer used and is kept in the signature only so the call sites
 * read the same as the rest of the report code; the scope lives in the module's
 * reportScope, which scopedValues() applies.
 */
export function valuesInCategory(s, docs, category) {
  return scopedValues(s).filter((v) => v.category === category);
}

/** countOccurrences(text, needle) counts non-overlapping occurrences. split() is
 *  used rather than a regex because a placeholder contains regex metacharacters
 *  ([ and ]) and escaping them per call would be the slower, longer way to do
 *  exactly this. */
export function countOccurrences(text, needle) {
  if (!needle) return 0;
  return text.split(needle).length - 1;
}

// --- Something missed? card ----------------------------------------------

export function missedCard(s) {
  const body =
    `<p class="hint">${escapeHTML(ANONYMISE.missedHint)}</p>` +
    `<div class="add-row">` +
    `<select id="missed-category" aria-label="${escapeHTML(ANONYMISE.missedCategoryLabel)}">` +
    CATEGORIES.map(([key, label]) =>
      `<option value="${escapeHTML(key)}"${key === drafts.missedCategory ? " selected" : ""}>` +
      `${escapeHTML(label)}</option>`).join("") +
    `</select>` +
    `<input id="missed-value" class="grow" value="${escapeHTML(drafts.missed)}"` +
    ` placeholder="${escapeHTML(ANONYMISE.missedPlaceholder)}"` +
    ` aria-label="${escapeHTML(ANONYMISE.missedLabel)}"/>` +
    `</div>` +
    `<div class="run-actions">` +
    button(ANONYMISE.addValue, { kind: "secondary", id: "btn-add-missed" }) +
    button(ANONYMISE.fastRerun, { kind: "secondary", id: "btn-fast-rerun", icon: "refresh" }) +
    `</div>`;

  return collapsibleCard("missed", ANONYMISE.missedTitle,
    ANONYMISE.missedSummary(s.entities.length), body);
}

// --- Find and replace card ----------------------------------------------

export function rulesCard(s) {
  const rows = s.simpleRules.map((r, i) =>
    `<div class="rule-row" data-index="${i}">` +
    `<span class="rule-text mono">` +
    `<span class="rule-find">${escapeHTML(r.find)}</span>` +
    `<span class="hint">${escapeHTML(ANONYMISE.ruleTo)}</span>` +
    `<span class="rule-replace">${escapeHTML(r.replace)}</span>` +
    `</span>` +
    `<span class="hint rule-case">${escapeHTML(r.caseSensitive ? ANONYMISE.exactCase : ANONYMISE.anyCase)}</span>` +
    `<span class="rule-actions">` +
    button("", {
      kind: "ghost", cls: "rule-up icon-action", icon: "expand_less",
      disabled: i === 0, ariaLabel: ANONYMISE.moveUp, title: ANONYMISE.moveUp,
    }) +
    button("", {
      kind: "ghost", cls: "rule-down icon-action", icon: "expand_more",
      disabled: i === s.simpleRules.length - 1,
      ariaLabel: ANONYMISE.moveDown, title: ANONYMISE.moveDown,
    }) +
    button("", {
      kind: "ghost", cls: "rule-del icon-action danger", icon: "close",
      ariaLabel: ANONYMISE.removeRule, title: ANONYMISE.removeRule,
    }) +
    `</span></div>`).join("");

  const body =
    `<p class="hint">${escapeHTML(ANONYMISE.rulesHint)}</p>` +
    (rows ? `<div class="rule-list">${rows}</div>` : "") +
    `<div class="add-row">` +
    `<input id="rule-find" class="grow" value="${escapeHTML(drafts.find)}"` +
    ` placeholder="${escapeHTML(ANONYMISE.ruleFind)}" aria-label="${escapeHTML(ANONYMISE.ruleFind)}"/>` +
    `<input id="rule-replace" class="grow" value="${escapeHTML(drafts.replace)}"` +
    ` placeholder="${escapeHTML(ANONYMISE.ruleReplace)}" aria-label="${escapeHTML(ANONYMISE.ruleReplace)}"/>` +
    `</div>` +
    `<div class="row-between">` +
    `<label class="cat-row"><input type="checkbox" id="rule-case"${drafts.caseSensitive ? " checked" : ""}/>` +
    `<span class="cat-label">${escapeHTML(ANONYMISE.caseSensitive)}</span></label>` +
    button(ANONYMISE.addRule, { kind: "secondary", id: "btn-add-rule" }) +
    `</div>`;

  return collapsibleCard("rules", ANONYMISE.rulesTitle,
    ANONYMISE.rulesSummary(s.simpleRules.length), body);
}

/**
 * collapsibleCard(id, title, summary, bodyHTML) is the shape the left column's
 * three foldable cards share: a header that toggles, a right-hand summary that
 * stays readable while folded, and a body that hides.
 *
 * It reuses collapsibleGroup rather than card(), because these do NOT scroll
 * individually: the whole left column is one scroller, and nesting a second
 * scroller inside it is how a user loses their place.
 */
function collapsibleCard(id, title, summary, bodyHTML) {
  return `<section class="card fold-card">` +
    collapsibleGroup(id, title, `<div class="fold-body">${bodyHTML}</div>`, {
      open: !collapsed.has(id),
      countLabel: summary,
    }) +
    `</section>`;
}

// --- Compare card --------------------------------------------------------

/**
 * compareCard(s, doc) is the two-pane comparison. Exported for the render
 * tests: the ORIGINAL pane's content is the thing reported issues 1 and 4 were
 * about, so it is asserted rather than eyeballed (see anonymise.test.js).
 */
export function compareCard(s, doc) {
  const options = (s.results?.documents ?? []).map((d) =>
    `<option value="${escapeHTML(d.name)}"${d === doc ? " selected" : ""}>` +
    `${escapeHTML(d.name)}</option>`).join("");

  const head = options
    ? `<select id="compare-doc" class="compare-select" aria-label="${escapeHTML(ANONYMISE.compareDoc)}">${options}</select>`
    : "";

  // The ORIGINAL pane reads the IMPORTED source, never anything the pipeline
  // produced or copied. That is the whole fix for the "ORIGINAL shows
  // [PERSON_2]" report: there is now one producer of original text in the
  // application (App.GetDocumentSource, mirrored in the import list) and this
  // pane is a reader of it. `source` is null only while the fetch for a
  // document that left the import list is in flight (see wireCompare).
  const source = doc ? documentSource(s, doc.name) : null;
  // The combined walk, computed once so the head's readout and both panes agree
  // about which hit is active.
  const walk = searchWalk(s, doc, source, search);

  const originalBody = source?.found
    ? (source.truncated
      ? `<div class="banner warn">${escapeHTML(IMPORT.previewTruncated)}</div>` : "") +
      renderPlainWithHits(source.markdown, walk.original, walk.activeIn("original"))
    : `<span class="hint">${escapeHTML(ANONYMISE.originalUnavailable)}</span>`;

  const panes = doc
    ? `<div class="compare-panes">` +
      `<div class="compare-pane">` +
      `<div class="pane-caption">${escapeHTML(ANONYMISE.paneOriginal)}</div>` +
      `<pre class="pane-body" id="original-pane">${originalBody}</pre>` +
      `</div>` +
      `<div class="compare-pane">` +
      `<div class="pane-caption">${escapeHTML(ANONYMISE.paneAnonymised)}</div>` +
      `<pre class="pane-body" id="anonymised-pane">${renderHighlighted(
        doc.anonymised ?? "", s.mapping, doc.occurrenceVariants,
        { hits: walk.anonymised, activeIndex: walk.activeIn("anonymised") },
      )}</pre>` +
      `</div></div>`
    : `<div class="card-body"><p class="hint">${escapeHTML(
        blockingConflicts(s).length > 0 ? ANONYMISE.compareBlocked : ANONYMISE.compareEmpty,
      )}</p></div>`;

  return `<section class="card compare-card" id="compare-card">` +
    `<div class="card-head with-controls">` +
    `<div class="card-head-left"><h2>${escapeHTML(CARDS.compare.title)}</h2>` +
    `<span class="card-sub">${escapeHTML(compareSubtitle(doc))}</span></div>` +
    `<div class="card-head-right">${searchControls(walk, search)}${head}</div>` +
    `</div>` +
    selectionPanel() +
    `<div class="mark-tooltip" id="mark-tooltip" role="tooltip" hidden></div>` +
    panes +
    toastHTML(getState().notice) +
    `</section>`;
}

/**
 * searchWalk(s, doc, source) computes the current needle's hits in both panes
 * and resolves which one is active.
 *
 * The walk is ONE ordered list: every original-pane hit, then every
 * anonymised-pane hit. The two panes have different match counts, so a single
 * shared index over two separate lists would drift, and two cursors would leave
 * the user unsure which one the buttons move. Crossing the boundary is made
 * visible instead, by naming the active hit's pane in the readout.
 *
 * @returns {object} {original, anonymised, total, index, pane, capped,
 *   activeIn(pane)} where activeIn returns the active hit's index WITHIN that
 *   pane, or -1 when the active hit is in the other one
 */
export function searchWalk(s, doc, source, state = search) {
  const needle = state.needle;
  const originalText = source?.found ? source.markdown : "";
  const original = doc ? findHits(originalText, needle) : [];
  const anonymised = doc ? findHits(doc.anonymised ?? "", needle) : [];
  const total = original.length + anonymised.length;

  // The index is clamped rather than corrected in place: the text can change
  // under a stale index (a re-run, a new document) and a clamp here means every
  // reader sees the same answer without anyone having to reset it first.
  const index = total === 0 ? 0 : ((state.index % total) + total) % total;
  const inOriginal = index < original.length;

  return {
    original,
    anonymised,
    total,
    index,
    pane: inOriginal ? "original" : "anonymised",
    // Both panes cap independently, so either hitting the cap means the
    // highlight is showing a prefix and should say so.
    capped: original.length >= MAX_HITS || anonymised.length >= MAX_HITS,
    activeIn(pane) {
      if (total === 0) return -1;
      if (pane === "original") return inOriginal ? index : -1;
      return inOriginal ? -1 : index - original.length;
    },
  };
}

/**
 * searchControls(walk) is the search box, the two navigation buttons and the
 * readout, in the Compare card head beside the document selector.
 *
 * Both buttons are disabled with a title when there is nothing to step through,
 * because a greyed control that says nothing is a dead end.
 */
export function searchControls(walk, state = search) {
  const none = walk.total === 0;
  const hasNeedle = state.needle.trim().length > 0;
  const readout = none
    ? (hasNeedle ? ANONYMISE.searchNone : "")
    : ANONYMISE.searchCount(walk.index + 1, walk.total, paneLabel(walk.pane));

  return `<div class="compare-search">` +
    `<label class="search-box">${icon("search")}` +
    `<input id="compare-search" value="${escapeHTML(state.needle)}"` +
    ` placeholder="${escapeHTML(ANONYMISE.searchPlaceholder)}"` +
    ` aria-label="${escapeHTML(ANONYMISE.searchLabel)}"/></label>` +
    button("", {
      kind: "ghost", cls: "search-prev icon-action", icon: "chevron_left",
      ariaLabel: ANONYMISE.searchPrev, title: none ? ANONYMISE.searchNone : ANONYMISE.searchPrev,
      disabled: none,
    }) +
    button("", {
      kind: "ghost", cls: "search-next icon-action", icon: "chevron_right",
      ariaLabel: ANONYMISE.searchNext, title: none ? ANONYMISE.searchNone : ANONYMISE.searchNext,
      disabled: none,
    }) +
    `<span class="search-readout hint">${escapeHTML(readout)}</span>` +
    (walk.capped ? `<span class="search-readout hint">${escapeHTML(ANONYMISE.searchCapped(MAX_HITS))}</span>` : "") +
    `</div>`;
}

/** paneLabel(pane) is the pane name as the readout says it. */
function paneLabel(pane) {
  return pane === "original" ? ANONYMISE.searchPaneOriginal : ANONYMISE.searchPaneAnonymised;
}

function compareSubtitle(doc) {
  if (!doc) return "";
  const n = Object.values(doc.byCategory ?? {}).reduce((a, b) => a + b, 0);
  return ANONYMISE.replacementsInDocument(n);
}

/**
 * selectionPanel() is the floating REPLACE SELECTION card.
 *
 * It appears where the user selected text, because it is about that exact
 * selection and anchoring it anywhere else would make them look for it. Unlike
 * the reassign popover it replaced, it does NOT live inside a scrolling pane: it
 * is positioned against the Compare card, so scrolling the pane moves the text
 * out from under it and the panel stays where the user's eye is.
 */
function selectionPanel() {
  if (!selection) return "";
  return `<div class="selection-card" id="selection-card"` +
    ` style="left:${selection.x}px;top:${selection.y}px">` +
    sectionLabel(ANONYMISE.replaceSelection) +
    `<span class="selection-text mono">${escapeHTML(selection.text)}</span>` +
    `<input id="selection-draft" class="mono" value="${escapeHTML(selectionDraft)}"` +
    ` aria-label="${escapeHTML(ANONYMISE.replaceWith)}"/>` +
    `<div class="run-actions">` +
    button(ANONYMISE.cancelSelection, { kind: "secondary", id: "btn-cancel-selection" }) +
    button(ANONYMISE.applySelection, { kind: "primary", id: "btn-apply-selection" }) +
    `</div></div>`;
}

// --- The footer hint -----------------------------------------------------

/** continueHint(s) says what is ready, or what has to happen first. */
export function continueHint(s) {
  if (s.running) return ANONYMISE.hintRunning;
  if (blockingConflicts(s).length > 0) return ANONYMISE.continueBlocked;
  if (!s.results) return ANONYMISE.hintNotRun;
  const n = s.results.report?.totalReplacements ?? 0;
  return ANONYMISE.hintReady(n);
}

// --- Wiring --------------------------------------------------------------

function wire(container, s, doc) {
  wireRun(container);
  wireGroups(container, (id) => {
    if (collapsed.has(id)) collapsed.delete(id); else collapsed.add(id);
    setState({});
  });
  if (selectedMark) wireSelected(container);
  if (s.results) {
    wireValues(container, s);
    wireReport(container);
    wireMissed(container);
  }
  wireRules(container);
  wireCompare(container, doc);
  wireNotice(container);
  wireStepFooter(container);
}

function wireRun(container) {
  container.querySelector("#btn-run")?.addEventListener("click", async () => {
    try {
      // Clearing the results before the run is deliberate: a stale Compare pane
      // beside a live progress bar reads as the new result, and it is not.
      selectedMark = null;
      selection = null;
      setState({ running: true, progress: null, results: null, mapping: null });
      await runPipeline(buildRunRequest());
      // Results arrive via the "pipeline:done" event (see main.js boot).
    } catch (err) {
      setState({ running: false });
      showError(container, err);
    }
  });
  container.querySelector("#btn-cancel")?.addEventListener("click", () => cancelPipeline());
}

function wireSelected(container) {
  container.querySelector("#btn-clear-selection")?.addEventListener("click", () => {
    selectedMark = null;
    reassignDraft = "";
    selectedError = "";
    setState({});
  });

  // Editing the placeholder here is the same act as editing it in the Replaced
  // values table: rename the one registry entry, refresh the tables, and stop.
  // It does NOT re-run, so the mark in the pane keeps its old number until the
  // next run, which is what the field's tooltip promises.
  const phInput = container.querySelector("#selected-ph-input");
  phInput?.addEventListener("change", async (ev) => {
    const current = selectedMark?.placeholder;
    if (!current) return;
    const next = ev.target.value.trim();
    try {
      await setValuePlaceholder(current, next);
      // Keep the card addressing the value under its NEW placeholder, so a
      // second edit renames from where the registry now is, not from the old
      // number the registry no longer knows.
      selectedMark.placeholder = next;
      selectedError = "";
      await refreshValueTables();
      setState({});
    } catch (err) {
      selectedError = String(err?.message ?? err);
      setState({});
    }
  });

  const input = container.querySelector("#reassign-input");
  input?.addEventListener("input", () => {
    // Repaint to refresh the suggestion list, then put the caret back: the input
    // is re-created by the repaint.
    const caret = input.selectionStart;
    reassignDraft = input.value;
    setState({});
    const again = container.querySelector("#reassign-input");
    if (again) {
      again.focus();
      again.setSelectionRange(caret, caret);
    }
  });

  for (const pick of container.querySelectorAll(".reassign-pick")) {
    pick.addEventListener("click", async () => {
      const { category, canonical } = pick.dataset;
      const original = selectedMark?.original;
      if (!original) return;
      if (!reassignOriginal(original, category, canonical)) {
        notify(ANONYMISE.reassignRefused(original, canonical), "warn");
        return;
      }
      selectedMark = null;
      reassignDraft = "";
      await runFastRerun(container, ANONYMISE.reassignDone(original, canonical));
    });
  }
}

function wireReport(container) {
  container.querySelector("#report-scope")?.addEventListener("change", (ev) => {
    reportScope = ev.target.value;
    setState({});
  });
  for (const row of container.querySelectorAll(".report-row")) {
    row.querySelector(".report-toggle")?.addEventListener("click", () => {
      // Expanding a category repaints the whole shell; the report's scroll
      // position is preserved centrally by that repaint (scroll.js), so the row
      // the user clicked stays in view.
      const key = row.dataset.category;
      if (expandedCategories.has(key)) expandedCategories.delete(key);
      else expandedCategories.add(key);
      setState({});
    });
  }
  for (const strip of container.querySelectorAll(".caution[data-warning]")) {
    strip.querySelector(".warning-dismiss")?.addEventListener("click", () => {
      dismissWarning(strip.dataset.warning);
    });
  }
}

/**
 * wireValues(container, s) attaches the Replaced values table.
 *
 * Neither a rename nor a removal re-runs from inside Go: RunPipeline holds an
 * in-progress guard and FastRerun is synchronous, so re-running from a bound
 * method would be a deadlock. The caller re-runs, and this is the caller.
 *
 * A rename takes effect on the NEXT run and not retroactively, so it refreshes
 * the table and stops there; a removal changes what the text should say, so it
 * re-runs.
 */
function wireValues(container, s) {
  // Load once per run. The tables come from the registry, which only Go has.
  if (s.results && tablesLoadedFor !== s.results) {
    tablesLoadedFor = s.results;
    refreshValueTables();
  }

  // The filter repaints the rows in place rather than through the store:
  // routing every keystroke through a full re-render moves the caret.
  const filter = container.querySelector("#report-value-filter");
  filter?.addEventListener("input", () => {
    valueFilter = filter.value;
    const rows = container.querySelector("#report-value-rows");
    if (!rows) return;
    rows.innerHTML = filterValues(getState().replacedValues, valueFilter)
      .map(valueRow).join("") ||
      `<span class="hint">${escapeHTML(ANONYMISE.valuesFilterEmpty)}</span>`;
    wireValueRows(container);
  });

  wireValueRows(container);

  for (const row of container.querySelectorAll(".removed-row")) {
    row.querySelector(".value-restore")?.addEventListener("click", async () => {
      try {
        await restoreValue(row.dataset.placeholder);
        await refreshValueTables();
        await runFastRerun(container, ANONYMISE.valueRestored);
      } catch (err) {
        notify(String(err?.message ?? err), "warn");
      }
    });
  }
}

/** wireValueRows(container) binds the rename field and the remove action. It is
 *  separate because the filter rebuilds those rows without a repaint. */
function wireValueRows(container) {
  for (const row of container.querySelectorAll(".value-row")) {
    const placeholder = row.dataset.placeholder;

    row.querySelector(".ph-input")?.addEventListener("change", async (ev) => {
      try {
        await setValuePlaceholder(placeholder, ev.target.value);
        rowErrors.delete(placeholder);
        await refreshValueTables();
      } catch (err) {
        // A refusal is shown on the row, not as a notice: it is about this
        // value and the fix is in this field.
        rowErrors.set(placeholder, String(err?.message ?? err));
        setState({});
      }
    });

    row.querySelector(".value-remove")?.addEventListener("click", async () => {
      try {
        const info = await removeValue(placeholder);
        rowErrors.delete(placeholder);
        await refreshValueTables();
        await runFastRerun(container, ANONYMISE.valueRemoved(info?.original ?? placeholder));
      } catch (err) {
        notify(String(err?.message ?? err), "warn");
      }
    });
  }
}

/**
 * refreshValueTables() mirrors the Go registry into the store.
 *
 * A bridge failure leaves the tables as they were rather than emptying them: an
 * empty Replaced values table reads as "nothing was replaced", which is a
 * different and much worse statement than "the table could not be refreshed".
 */
async function refreshValueTables() {
  try {
    const [replaced, removed] = await Promise.all([valuePlaceholders(), listRemovedValues()]);
    setValueTables(replaced, removed);
  } catch { /* no bridge (plain browser): keep what is on screen */ }
}

function wireMissed(container) {
  const category = container.querySelector("#missed-category");
  const value = container.querySelector("#missed-value");
  category?.addEventListener("change", () => { drafts.missedCategory = category.value; });
  value?.addEventListener("input", () => { drafts.missed = value.value; });

  // Adding a value here adds it to the VALUE LIST, and nothing happens to the
  // text until the fast re-run applies it. The two are separate buttons because
  // they are separate decisions: several values are usually added at once.
  const add = () => {
    const text = (drafts.missed ?? "").trim();
    if (!text) return;
    if (!addEntities([{ category: drafts.missedCategory, canonical: text }])) {
      notify(ANONYMISE.missedAlreadyThere(text), "info");
      return;
    }
    drafts.missed = "";
    setState({});
  };
  container.querySelector("#btn-add-missed")?.addEventListener("click", add);
  value?.addEventListener("keydown", (ev) => { if (ev.key === "Enter") add(); });

  container.querySelector("#btn-fast-rerun")?.addEventListener("click", async () => {
    await runFastRerun(container, ANONYMISE.fastRerunDone(getState().entities.length));
  });
}

function wireRules(container) {
  const find = container.querySelector("#rule-find");
  const replace = container.querySelector("#rule-replace");
  const caseBox = container.querySelector("#rule-case");
  find?.addEventListener("input", () => { drafts.find = find.value; });
  replace?.addEventListener("input", () => { drafts.replace = replace.value; });
  caseBox?.addEventListener("change", () => { drafts.caseSensitive = caseBox.checked; });

  container.querySelector("#btn-add-rule")?.addEventListener("click", () => {
    if (!addSimpleRule({
      find: drafts.find, replace: drafts.replace, caseSensitive: drafts.caseSensitive,
    })) {
      // An empty needle is a no-op rule, and adding one silently would leave the
      // user waiting for a row that never appears.
      notify(ANONYMISE.ruleNeedsFind, "info");
      return;
    }
    drafts.find = "";
    drafts.replace = "";
    setState({});
  });

  for (const row of container.querySelectorAll(".rule-row")) {
    const i = parseInt(row.dataset.index, 10);
    row.querySelector(".rule-up")?.addEventListener("click", () => moveSimpleRule(i, -1));
    row.querySelector(".rule-down")?.addEventListener("click", () => moveSimpleRule(i, +1));
    row.querySelector(".rule-del")?.addEventListener("click", () => removeSimpleRule(i));
  }
}

/**
 * wireCompareSearch(container) wires the search box, its two buttons and the
 * keyboard, then scrolls to the active hit.
 *
 * The input keeps focus and caret across the repaint each keystroke causes, the
 * same pattern the values search bar uses: a search box that loses focus
 * mid-word cannot be typed into.
 */
function wireCompareSearch(container) {
  const input = container.querySelector("#compare-search");
  input?.addEventListener("input", () => {
    const caret = input.selectionStart;
    // A new needle starts at its first hit: keeping the old position would land
    // the user somewhere unrelated to what they just typed.
    search = { needle: input.value, index: 0 };
    lastScrolledTo = null;
    if (compareSearchTimer) clearTimeout(compareSearchTimer);
    compareSearchTimer = setTimeout(() => {
      compareSearchTimer = null;
      setState({});
      const again = container.querySelector("#compare-search");
      if (again) {
        again.focus();
        again.setSelectionRange(caret, caret);
      }
    }, 150);
  });

  input?.addEventListener("keydown", (ev) => {
    // Enter and Shift+Enter step through the hits without leaving the field,
    // which is how every search box behaves. Escape clears.
    if (ev.key === "Enter") {
      ev.preventDefault();
      stepSearch(ev.shiftKey ? -1 : 1);
    } else if (ev.key === "Escape") {
      ev.preventDefault();
      search = { needle: "", index: 0 };
      lastScrolledTo = null;
      setState({});
    }
  });

  container.querySelector(".search-prev")?.addEventListener("click", () => stepSearch(-1));
  container.querySelector(".search-next")?.addEventListener("click", () => stepSearch(1));

  scrollToActiveHit(container);
}

// The pending repaint from a keystroke in the search box. Debounced so the
// input survives a burst of typing.
let compareSearchTimer = null;

/**
 * stepSearch(delta) moves through the ONE combined list and wraps at both ends.
 * The index is normalised at read time (searchWalk), so this only has to add.
 */
function stepSearch(delta) {
  search = { ...search, index: search.index + delta };
  setState({});
}

/**
 * scrollToActiveHit(container) brings the active hit into view, but ONLY when
 * it changed since the last paint.
 *
 * scroll.js restores each pane's previous offset on every repaint. Scrolling
 * unconditionally would fight that and drag the pane back to the active hit on
 * every keystroke, including the ones that were meant to scroll somewhere else.
 */
function scrollToActiveHit(container) {
  const active = container.querySelector(".find-hit.active");
  if (!active) {
    lastScrolledTo = null;
    return;
  }
  const key = `${search.needle}|${search.index}`;
  if (key === lastScrolledTo) return;
  lastScrolledTo = key;
  active.scrollIntoView?.({ block: "center" });
}

function wireCompare(container, doc) {
  // If the ORIGINAL pane has no source to show, ask Go for it ONCE. This is
  // the fallback path for a document that left the import list; the common
  // case is served straight from the store with no bridge call at all.
  if (doc && documentSource(getState(), doc.name) === null) {
    getDocumentSource(doc.name)
      .then((source) => cacheDocumentSource(doc.name, source))
      // A missing bridge (plain browser) leaves the pane on its hint, which is
      // the honest answer: no backend, no source text.
      .catch(() => cacheDocumentSource(doc.name, { found: false }));
  }

  container.querySelector("#compare-doc")?.addEventListener("change", (ev) => {
    // Changing document clears the selected mark: it belonged to the old one.
    selectedMark = null;
    selection = null;
    // The search offsets belong to ONE text, so the position resets. The needle
    // is kept: the user is looking for the same thing in the next document.
    search = { ...search, index: 0 };
    lastScrolledTo = null;
    setState({ resultDoc: ev.target.value });
  });

  wireCompareSearch(container);

  // A results view restored by navigation can have results but no mapping (the
  // fast re-run path fetches it separately). Without it every mark falls back
  // to its label and the hover shows nothing, which is indistinguishable from
  // a broken tooltip. Fetch it once, quietly.
  if (doc && !getState().mapping) {
    getMapping()
      .then((mapping) => { if (mapping) setState({ mapping }); })
      .catch(() => { /* no bridge: the marks keep their label-only titles */ });
  }

  wireMarkTooltip(container, doc);

  // Clicking a mark fills the Selected placeholder card. highlight.js already
  // emits data-ph and data-original, so this needs no parsing.
  container.querySelector("#anonymised-pane")?.addEventListener("click", (ev) => {
    const mark = ev.target.closest("mark[data-ph]");
    if (!mark || !mark.dataset.original) return;
    selectedMark = {
      placeholder: mark.dataset.ph,
      original: mark.dataset.original,
    };
    reassignDraft = "";
    selectedError = "";
    collapsed.delete("selected");
    setState({});
  });

  wireTextSelection(container);
  wireSelectionPanel(container);
}

/**
 * wireMarkTooltip(container, doc) shows the original value under the mark the
 * pointer (or the keyboard focus) is on.
 *
 * The markup for this has been right for a long time; the PLACEMENT was not.
 * The tooltip was a CSS ::after on the mark itself, and the mark lives inside
 * `.pane-body { overflow: auto }`, so the pane clipped it: near the right-hand
 * edge or the last line of the pane it was simply not visible, which is why it
 * was reported as missing. `white-space: nowrap` made a long value run off the
 * side even when it did appear.
 *
 * The fix is the one the reassign popover already had to make (style.css, the
 * note where that popover used to be): position it against the COMPARE CARD,
 * not inside the scrolling pane. Scrolling then moves the text out from under
 * it, so the tooltip hides on scroll rather than drifting away from its mark.
 */
function wireMarkTooltip(container, doc) {
  const host = container.querySelector("#compare-card");
  const tip = container.querySelector("#mark-tooltip");
  const pane = container.querySelector("#anonymised-pane");
  if (!host || !tip || !pane) return;

  const hide = () => { tip.hidden = true; };

  const show = (mark) => {
    const original = mark.dataset.original;
    if (!original) return; // a mapping miss has nothing to show
    const category = mark.dataset.category;
    const count = doc ? countOccurrences(doc.anonymised ?? "", mark.dataset.ph ?? "") : 0;
    // When this occurrence replaced a variant spelling, lead with what was
    // actually on the page and keep the canonical value in brackets:
    // "Borch (Johannes Borch)". A canonical match shows the value alone.
    const variant = mark.dataset.variant;
    const originalDisplay = variant ? `${variant} (${original})` : original;

    tip.innerHTML =
      `<span class="tooltip-original">${escapeHTML(originalDisplay)}</span>` +
      `<span class="tooltip-meta">${escapeHTML(tooltipMeta(category, count))}</span>`;
    tip.hidden = false;

    // Positioned against the card, and flipped above the mark when there is no
    // room below it: a tooltip that opens off the bottom of the window is the
    // same as no tooltip.
    const markBox = mark.getBoundingClientRect();
    const hostBox = host.getBoundingClientRect();
    const tipBox = tip.getBoundingClientRect();
    const below = markBox.bottom - hostBox.top + 6;
    const above = markBox.top - hostBox.top - tipBox.height - 6;
    const fitsBelow = markBox.bottom + tipBox.height + 12 <= hostBox.bottom;
    let left = markBox.left - hostBox.left;
    left = Math.max(6, Math.min(left, hostBox.width - tipBox.width - 6));
    tip.style.left = `${left}px`;
    tip.style.top = `${fitsBelow || above < 0 ? below : above}px`;
  };

  for (const mark of pane.querySelectorAll("mark[data-original]")) {
    mark.addEventListener("mouseenter", () => show(mark));
    mark.addEventListener("mouseleave", hide);
    // Keyboard parity: the marks are focusable now (highlight.js), so the same
    // information is reachable without a pointer.
    mark.addEventListener("focus", () => show(mark));
    mark.addEventListener("blur", hide);
    mark.addEventListener("keydown", (ev) => {
      if (ev.key !== "Enter" && ev.key !== " ") return;
      ev.preventDefault();
      mark.click();
    });
  }
  // Scrolling moves the text, not the tooltip: hide it rather than let it
  // point at whatever has scrolled under it.
  pane.addEventListener("scroll", hide);
}

/** tooltipMeta(category, count) is the second line: what kind of value it is
 *  and how often it was replaced in this document. */
export function tooltipMeta(category, count) {
  const label = CATEGORY_LABELS[category]?.[0] ?? category ?? "";
  const times = count > 0 ? ANONYMISE.tooltipTimes(count) : "";
  return [label, times].filter(Boolean).join(", ");
}

/**
 * wireTextSelection(container) offers to replace whatever text the user
 * selected in either pane.
 *
 * The guards matter more than the feature. A selection is only offered when it
 * is non-empty, short enough to be a value rather than a paragraph, and inside
 * one of the two panes: a stray drag while scrolling must not put a panel over
 * the text the user is reading.
 */
function wireTextSelection(container) {
  const host = container.querySelector("#compare-card");
  if (!host) return;

  for (const pane of container.querySelectorAll(".pane-body")) {
    pane.addEventListener("mouseup", () => {
      const sel = container.ownerDocument.defaultView?.getSelection?.();
      if (!sel || sel.isCollapsed) {
        if (selection) { selection = null; setState({}); }
        return;
      }
      const text = sel.toString().trim();
      // A paragraph-length "value" is a mis-drag, not a value. 120 characters is
      // generously long for a name, an address or an account number.
      if (!text || text.length > 120 || !pane.contains(sel.anchorNode)) {
        if (selection) { selection = null; setState({}); }
        return;
      }
      const rect = sel.getRangeAt(0).getBoundingClientRect();
      const hostRect = host.getBoundingClientRect();
      selection = {
        text,
        x: rect.left + rect.width / 2 - hostRect.left,
        y: rect.top - hostRect.top,
      };
      // GO mints the placeholder and reserves it, because only the registry
      // knows every number already spent. CUSTOM is also the automatic label
      // for custom_patterns matches, so numbering from the rules alone hands
      // out one the registry has already given to a pattern match.
      selectionDraft = "";
      setState({});
      nextRulePlaceholder()
        .then((placeholder) => {
          selectionDraft = placeholder;
          setState({});
        })
        .catch(() => { /* no bridge: the user types their own replacement */ });
    });
  }
}

function wireSelectionPanel(container) {
  const draft = container.querySelector("#selection-draft");
  draft?.addEventListener("input", () => { selectionDraft = draft.value; });

  container.querySelector("#btn-cancel-selection")?.addEventListener("click", () => {
    selection = null;
    setState({});
  });

  container.querySelector("#btn-apply-selection")?.addEventListener("click", async () => {
    const find = selection?.text;
    const replace = (selectionDraft ?? "").trim();
    if (!find || !replace) {
      notify(ANONYMISE.selectionNeedsReplacement, "info");
      return;
    }
    // Case-sensitive, because the user selected an exact string: matching it
    // case-insensitively would replace things they did not point at.
    addSimpleRule({ find, replace, caseSensitive: true });
    selection = null;
    collapsed.delete("rules"); // show the rule that was just created
    container.ownerDocument.defaultView?.getSelection?.()?.removeAllRanges();
    await runFastRerun(container, ANONYMISE.selectionApplied(find, replace));
  });
}

/**
 * runFastRerun(container, message) re-runs the DETERMINISTIC passes only and
 * refreshes the mapping.
 *
 * "Fast" means no LLM: the values and rules on screen are re-applied, and
 * existing placeholders keep their numbers because the session registry is
 * unchanged. That last part is the whole point, and it is why every editing
 * action on this screen ends here rather than in a full re-run.
 *
 * @param {HTMLElement} container the view container, for the error strip
 * @param {string} message the notice to show on success
 */
async function runFastRerun(container, message) {
  try {
    const results = await fastRerun(buildRunRequest());
    // The mapping and the value table may both have grown: new values earned
    // placeholders, and removed ones lost theirs.
    tablesLoadedFor = results;
    setState({ results, mapping: await getMapping() });
    await refreshValueTables();
    notify(message, "ok");
  } catch (err) {
    showError(container, err);
  }
}

function showError(container, err) {
  const slot = container.querySelector("#run-error");
  if (slot) {
    slot.innerHTML = `<div class="banner error">${escapeHTML(String(err?.message ?? err))}</div>`;
  }
}

// views/anonymise.js, wizard step 3: run the pipeline and check the result
//
// The step has TWO halves behind one tab bar, because a document is words and
// pictures and the pipeline only ever touches the words. This module owns the tab
// bar, the shared document selection and the footer, plus the whole TEXT half
// below. The IMAGE half is views/anonymiseimages.js, a sibling for the reason
// identifyrail.js is a sibling of identify.js: one screen, halves big enough to
// own a file each.
//
// The TEXT half: a column of cards on the left, one big Compare card on the
// right:
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
//                        value can be removed, whatever discovery method found it.
//   Report the per-category drill-down, scoped to all files or
//                        one, plus the run's dismissible warnings.
//   Add missed Value     declare a Value, then re-run the fast passes.
//
// The Compare card's two panes are the one place the anonymisation is actually
// checked, which is why they get two thirds of the screen: hovering a mark shows
// its original, clicking one selects it, and selecting free text offers to make
// it a spelling of an existing Value or a new Value of its own. Both go through
// the Value model, so everything the run replaced is in the re-identification
// key.

import {
  runPipeline, cancelPipeline, fastRerun, getMapping, getDocumentSource,
  valuePlaceholders, listRemovedValues, setValuePlaceholder, removeValue,
  restoreValue, copyText, countTermMatches, checkIntersections,
} from "../api.js";
import {
  getState, setState,
  buildRunRequest, documentSource, cacheDocumentSource,
  valueAutocomplete, reassignOriginal, addValues,
  setValueTables, dismissWarning, visibleWarnings, visibleValidationWarnings,
  blockingConflicts, foldIntoFamily, checkValueConflict,
  valueKey, deleteValue, removeAllowTerm,
  setIntersections, intersectionsFor, buildIntersectionRequest,
  setAnonymiseTab,
  PRESET_SCOPES, presetFamilies, presetKey, findPreset,
} from "../state.js";
import { escapeHTML } from "../html.js";
import { renderHighlighted } from "../highlight.js";
import { findHits, MAX_HITS } from "../panesearch.js";
import { valueSpans, renderOriginWithSpans } from "../valuespans.js";
import {
  button, card, statTile, collapsibleGroup, wireGroups, icon, sectionLabel,
  searchBox, wireSearchBox, tabbar,
} from "../ui.js";
import { imageCount, imageTabHTML, wireImageTab } from "./anonymiseimages.js";
import { categorySelect, conflictMessage } from "./identifyworkspace.js";
import { stepFooterHTML, wireStepFooter } from "../nav.js";
import { notify, wireNotice } from "../toast.js";
import { CARDS, ANONYMISE, CATEGORY_LABELS, IMPORT, WORKSPACE, VALUES, IMAGES } from "../copy.js";
import { toastHTML } from "../ui.js";

// --- View-local state -----------------------------------------------------

// Which of the collapsible cards are folded shut. All of them start closed:
// the result cards open on demand, so the screen after a run is a compact
// column of headings rather than a wall of tables.
const collapsed = new Set(["missed", "selected", "removed", "values", "report"]);
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
// The clicked placeholder, or null: {placeholder, original}.
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
// The selection panel's stage: null (choose copy or replace), "replace" (choose
// how), then a mode. All view state: it is about THIS selection and dies with
// it, so it resets whenever `selection` is cleared or the document changes.
let selectionStage = null;    // null | "replace"
let selectionMode = null;     // null | "spelling" | "value"
let selectionTarget = "";     // the spelling autocomplete's draft
// The Value a suggestion pick chose, or null when the target was typed by
// hand without clicking one. Set alongside selectionTarget so Apply does not
// have to re-resolve a text search for a click that already named the Value;
// cleared on every further keystroke, because the field may no longer match
// what was picked.
let selectionPicked = null;    // null | {category, mainText}
let selectionCategory = "person_names";
// A refusal from Apply. It belongs ON the panel, next to the field the fix goes
// into, for the same reason a refused rename lands on its own row.
let selectionError = "";
// The panel's width in pixels, matching .selection-card in style.css. The clamp
// that keeps it inside the Compare card needs a number, and reading it off the
// element is not possible before the element exists.
const SELECTION_PANEL_WIDTH = 304; // 19rem at the 16px root
// The Compare search: a way of LOOKING at the result, not part of it, so it
// lives here and not in the store. There is ONE search per pane, because each
// pane carries its OWN search bar in its caption. Each keeps its own needle and
// its own cursor: the two panes have different match counts, and a bar that sits
// on the pane it searches has no reason to share an index with the other one.
//
// Hits are recomputed during render from each pane's needle rather than cached,
// which keeps them in step with the text with nothing to invalidate. An index is
// reset when the compared document changes or a new run lands: offsets belong to
// one text.
let search = {
  original: { needle: "", index: 0 },
  anonymised: { needle: "", index: 0 },
};
// The hit each pane was last scrolled to, per pane, so scrolling happens only
// when that pane's active hit CHANGED. scroll.js restores each pane's offset on
// every repaint, and scrolling unconditionally would fight it and drag the pane
// back on every keystroke.
let lastScrolledTo = { original: null, anonymised: null };
// The Add missed Value row's draft text, kept across repaints.
const drafts = { missedCategory: "person_names", missed: "", missedMatches: "" };
// Texts declared from the missed-value card since the last fast re-run, so the
// re-run can say when one of them matched nothing. Several values are usually
// declared before the user re-runs once, so this is a list, not one string.
let pendingMissedTexts = [];
// A declaration refused before it ever reached addValues (an ambiguity, an
// allowlist collision, a spelling another Value claims), shown on the card
// next to the field the fix goes into, exactly like every other per-surface
// error on this screen.
let missedError = "";
// The Values DECLARED FROM THIS SCREEN, by valueKey. A Value that a
// higher-priority route covers ENTIRELY is the full-case intersection, which
// usually means a mis-declaration, and this screen has no value card to paint it
// on: the Report card's warning list is its home. Only declarations made here are
// listed, because a Value accepted on Identify already met that warning on its
// own card.
const declaredHere = new Set();

export function renderAnonymise(container) {
  const s = getState();
  const doc = currentDocument(s);
  const images = s.anonymiseTab === "images";

  container.innerHTML = `
    <div class="anonymise-view">
      ${anonymiseTabbar(s)}
      ${images ? imageTabHTML(s) : textWorkspaceHTML(s, doc)}
      ${stepFooterHTML({
        hint: continueHint(s),
        nextDisabled: !s.results || blockingConflicts(s).length > 0,
        nextTitle: continueBlockedTitle(s),
        standalone: true,
      }, s)}
      <div id="run-error"></div>
    </div>
  `;

  wireAnonymiseTabbar(container);
  if (images) {
    // The IMAGE half wires itself, and it wires the footer too, because the
    // footer is shared: the step 3 to 4 gate is about the TEXT run, and picture
    // decisions deliberately do not gate the move to Export.
    wireImageTab(container, s);
    wireStepFooter(container);
    return;
  }
  wire(container, s, doc);
}

/**
 * anonymiseTabbar(s) is the two halves of step 3, above both columns.
 *
 * TEXT is the screen this step has always been; IMAGE is the picture review. The
 * count is the number of pictures in the SELECTED document and is absent for a
 * format that has no image review, so the badge never claims a .txt file was
 * reviewed and found to have none.
 *
 * @param {object} s state
 * @returns {string} safe HTML
 */
function anonymiseTabbar(s) {
  return tabbar([
    { id: "text", label: IMAGES.tabText, active: s.anonymiseTab !== "images" },
    {
      id: "images", label: IMAGES.tabImage, count: imageCount(s),
      active: s.anonymiseTab === "images", title: IMAGES.tabImageTitle,
    },
  ], { attr: "anontab", ariaLabel: IMAGES.tabImageTitle });
}

/** wireAnonymiseTabbar(container) switches the half. It is wired for BOTH
 *  halves, because the tab bar is the one control each has to offer the other. */
function wireAnonymiseTabbar(container) {
  for (const tab of container.querySelectorAll("[data-anontab]")) {
    tab.addEventListener("click", () => setAnonymiseTab(tab.dataset.anontab));
  }
}

/**
 * textWorkspaceHTML(s, doc) is the TEXT half: the card column and the Compare
 * card, exactly as this step has always rendered them.
 *
 * @param {object} s state
 * @param {object|null} doc the result document the Compare card shows
 * @returns {string} safe HTML
 */
function textWorkspaceHTML(s, doc) {
  // A refused run carries empty documents and an empty report, so the Replaced
  // values and Report cards would show a zero run beside a stale registry
  // table: the exact mismatch a refused run produces. They stay hidden until
  // the conflict is fixed, and the run card explains why. "Add missed Value"
  // is the one exception: the screen that can create a blocking conflict
  // (a mistyped declaration) must also offer a way to clear it, or the only
  // way out is Identify, which discards the whole registry over a typo.
  const blocked = blockingConflicts(s).length > 0;

  return `
      <div class="workspace workspace-side">
        <div class="card-column">
          ${runCard(s)}
          ${selectedMark ? selectedCard(s) : ""}
          ${s.results && !blocked ? valuesCard(s) : ""}
          ${s.results && !blocked ? reportCard(s) : ""}
          ${s.results ? missedCard(s) : ""}
        </div>
        ${compareCard(s, doc)}
      </div>`;
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
    return `<li>${escapeHTML(c.message)}${fix}${blockedActions(s, c)}</li>`;
  }).join("");
  return `<div class="banner error blocked-banner" role="alert">` +
    `<span class="banner-icon">${icon("warning")}</span>` +
    `<div class="banner-body">` +
    `<strong>${escapeHTML(ANONYMISE.blockedTitle)}</strong>` +
    `<p class="hint">${escapeHTML(ANONYMISE.blockedIntro)}</p>` +
    `<ul class="blocked-list">${items}</ul>` +
    `</div></div>`;
}

/**
 * blockedActions(s, c) is the resolve actions for ONE blocking conflict.
 *
 * Keeping the "Add missed Value" card on a refused run makes the screen a way
 * back in; this makes it a way OUT. The conflict the run refused over has no row
 * anywhere on this screen, and the only other route to a fix is the Identify
 * step, which calls ResetRun on the way and discards the registry: a mistyped
 * declaration would cost every placeholder number the session had assigned.
 *
 * The action follows the conflict's own shape, which the engine states in its
 * refs: an allowlist collision is cleared by taking the term off the
 * never-anonymise list, and every other blocking conflict by deleting one of the
 * two Values that fight over the text.
 */
function blockedActions(s, c) {
  const refs = c.refs ?? [];
  // The engine STATES the one-gesture fix (engine/conflicts.go
  // ConflictResolution). Reading it, rather than inferring it from a ref kind, is
  // what keeps this panel and the value card on Identify offering the same way
  // out: two inferences can disagree, and then a button offers a fix the engine
  // never described.
  if (c.resolution?.action === "drop_allow_term") {
    return `<div class="run-actions">` +
      button(ANONYMISE.blockedRemoveAllowTerm, {
        kind: "secondary", cls: "blocked-allow-remove",
        data: { term: c.resolution.term || c.value },
      }) + `</div>`;
  }

  // Named from the store rather than from the ref, because the engine
  // lower-cases mainText in a ref and the user typed a capital letter. A ref
  // naming no declared Value gets no button: the fix is not here.
  const declared = (ref) => s.values
    .find((v) => valueKey(v.category, v.mainText) === valueKey(ref.category, ref.mainText));
  const actions = refs
    .filter((r) => r.kind === "value" && r.category && declared(r))
    .map((r) => button(ANONYMISE.blockedDeleteValue(declared(r).mainText), {
      kind: "secondary", cls: "blocked-delete-value",
      data: { category: r.category, "main-text": r.mainText },
    }));
  if (actions.length === 0) return "";
  return `<div class="run-actions">${actions.join("")}</div>`;
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

/**
 * valuePickButton(m) is the one rendering for "here is a Value you might
 * mean": a real button carrying the category and main text as data, not a
 * native <option>. Both the Selected placeholder card's reassign search and
 * the selection panel's spelling search ask the same question ("which Value
 * is this"), so both render its answers the same way.
 */
function valuePickButton(m) {
  return `<button class="btn btn-secondary reassign-pick"` +
    ` data-category="${escapeHTML(m.category)}" data-main-text="${escapeHTML(m.mainText)}">` +
    `${escapeHTML(m.mainText)}` +
    `<span class="hint">${escapeHTML(CATEGORY_LABELS[m.category]?.[0] ?? m.category)}</span>` +
    `</button>`;
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
    ? valueAutocomplete(reassignDraft, s).slice(0, 6)
    : [];
  const suggestions = matches.map(valuePickButton).join("");

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

  // Three sources, one rendered list: the run's own notes (Report.Warnings), the
  // overlap warnings the engine computes on every run (Validation.Warnings, e.g.
  // "this Value lost text to a built-in pattern"), and the full-coverage
  // intersection for a Value declared on this screen. All dismiss the same way,
  // keyed on the message text, so a third source needed no new mechanism.
  const warnings = reportWarnings(s).map((w) =>
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
 * reportWarnings(s) is every warning the card shows, deduplicated, in one list.
 *
 * One list because they all answer the same question, "why does the result not
 * look like what I asked for", and because dismissWarning keys on the warning
 * TEXT: a further source needs no new mechanism, and the same warning arriving
 * twice cannot stack.
 */
function reportWarnings(s) {
  const dismissed = new Set(s.dismissedWarnings ?? []);
  const out = [];
  for (const w of [
    ...visibleWarnings(s),
    ...visibleValidationWarnings(s),
    ...declaredIntersectionWarnings(s),
  ]) {
    if (!w || dismissed.has(w) || out.includes(w)) continue;
    out.push(w);
  }
  return out;
}

/**
 * declaredIntersectionWarnings(s) is the intersection sentence for each Value
 * declared on this screen that a higher-priority route covers entirely.
 *
 * CHANGE-06 kept the full-coverage case because it usually means a
 * mis-declaration: the Value is never replaced under its own type, so a user who
 * declared it and then cannot find it in the report has no other explanation. It
 * names the winning METHOD and never the internal rank, exactly as the step 2
 * value card does: the rank is an engine input, and "rank 1" teaches nobody
 * anything.
 */
function declaredIntersectionWarnings(s) {
  const overlaps = intersectionsFor(s);
  const out = [];
  for (const key of declaredHere) {
    const row = overlaps.get(key);
    if (!row) continue;
    const route = WORKSPACE.matchClassLabel[row.winnerMatchClass] ?? row.winnerMatchClass;
    out.push([
      WORKSPACE.intersectionAll(row.value, row.winnerValue, route, row.matchedTexts),
      WORKSPACE.intersectionFix,
    ].join(" "));
  }
  return out;
}

/**
 * runNote(s) surfaces what the run itself did, which the card used to ignore
 * entirely: the presets its selection matched.
 *
 * report.presets is keyed "<scope>.<family>" and Go derives it from the selection
 * the run OBEYED, so this note can never name a preset the run did not use. A row
 * that matched none contributes no key, and therefore nothing to say.
 */
function runNote(s) {
  const presets = s.results?.report?.presets ?? {};
  const rows = [];
  for (const scope of PRESET_SCOPES) {
    for (const family of presetFamilies(scope)) {
      const id = presets[presetKey(scope, family)];
      if (!id) continue;
      rows.push({
        scope: ANONYMISE.presetScopeLabel[scope] ?? scope,
        preset: findPreset(scope, family, id)?.label ?? id,
      });
    }
  }
  const sentence = ANONYMISE.reportPresets(rows);
  if (!sentence) return "";
  return `<p class="hint" id="report-run-note">${escapeHTML(sentence)}.</p>`;
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

// --- Add missed Value card -----------------------------------------------

export function missedCard(s) {
  const body =
    `<p class="hint">${escapeHTML(ANONYMISE.missedHint)}</p>` +
    `<div class="add-row">` +
    categorySelect(drafts.missedCategory, { id: "missed-category", ariaLabel: ANONYMISE.missedCategoryLabel }) +
    `<input id="missed-value" class="grow" value="${escapeHTML(drafts.missed)}"` +
    ` placeholder="${escapeHTML(ANONYMISE.missedPlaceholder)}"` +
    ` aria-label="${escapeHTML(ANONYMISE.missedLabel)}"/>` +
    `</div>` +
    // The live match count, the same read-out the step 2 add row shows: a
    // value that matches nothing is almost always a typo, and saying so
    // before the fast re-run is the cheapest correction there is.
    `<p class="hint" id="missed-matches">${escapeHTML(drafts.missedMatches)}</p>` +
    // A refused declaration lands here, next to the field the fix goes into,
    // the same reasoning behind every other per-surface error on this screen.
    (missedError ? `<p class="hint bad" id="missed-error">${escapeHTML(missedError)}</p>` : "") +
    `<div class="run-actions">` +
    button(ANONYMISE.addValue, { kind: "secondary", id: "btn-add-missed" }) +
    button(ANONYMISE.fastRerun, { kind: "secondary", id: "btn-fast-rerun", icon: "refresh" }) +
    `</div>`;

  return collapsibleCard("missed", ANONYMISE.missedTitle,
    ANONYMISE.missedSummary(s.values.length), body);
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
  // One walk per pane: each pane searches on its own needle, so its hits and its
  // active index are computed from its own search state and read only by its own
  // caption bar and body.
  const originalText = source?.found ? source.markdown : "";
  const originalWalk = paneWalk(doc ? originalText : "", search.original);
  const anonWalk = paneWalk(doc ? (doc.anonymised ?? "") : "", search.anonymised);

  // The ORIGINAL pane's text also carries the ORIGIN SPANS: one invisible span
  // per stretch this run replaced, carrying the placeholder that replaced it.
  // Hovering a mark in the other pane tints the spans that share its
  // placeholder (wireOriginLink), which is how the reader sees the mainText
  // value AND every spelling one placeholder stands for.
  const originalBody = source?.found
    ? (source.truncated
      ? `<div class="banner warn">${escapeHTML(IMPORT.previewTruncated)}</div>` : "") +
      renderOriginWithSpans(
        source.markdown,
        originSpans(doc?.name, source.markdown, s.mapping, doc?.occurrenceSpellings),
        originalWalk.hits, originalWalk.active,
      )
    : `<span class="hint">${escapeHTML(ANONYMISE.originalUnavailable)}</span>`;

  const panes = doc
    ? `<div class="compare-panes">` +
      `<div class="compare-pane">` +
      paneCaption(ANONYMISE.paneOriginal, "original", originalWalk, search.original) +
      `<pre class="pane-body" id="original-pane">${originalBody}</pre>` +
      `</div>` +
      `<div class="compare-pane">` +
      paneCaption(ANONYMISE.paneAnonymised, "anonymised", anonWalk, search.anonymised) +
      `<pre class="pane-body" id="anonymised-pane">${renderHighlighted(
        doc.anonymised ?? "", s.mapping, doc.occurrenceSpellings,
        { hits: anonWalk.hits, activeIndex: anonWalk.active },
      )}</pre>` +
      `</div></div>`
    : `<div class="card-body"><p class="hint">${escapeHTML(
        blockingConflicts(s).length > 0 ? ANONYMISE.compareBlocked : ANONYMISE.compareEmpty,
      )}</p></div>`;

  return `<section class="card compare-card" id="compare-card">` +
    `<div class="card-head with-controls">` +
    `<div class="card-head-left"><h2>${escapeHTML(CARDS.compare.title)}</h2>` +
    `<span class="card-sub">${escapeHTML(compareSubtitle(doc))}</span></div>` +
    `<div class="card-head-right">${head}</div>` +
    `</div>` +
    selectionPanel(s) +
    `<div class="mark-tooltip" id="mark-tooltip" role="tooltip" hidden></div>` +
    panes +
    toastHTML(getState().notice) +
    `</section>`;
}

// The ORIGINAL pane's origin spans, cached for the exact inputs they were
// computed from. Finding every value in the source is one scan per spelling, and
// the Compare card repaints on every keystroke in a search field, so recomputing
// them per repaint would make typing in the search box lurch on a long document.
// The keys are compared by IDENTITY: the store hands out the same text and the
// same mapping object until something actually replaces them, so an identity
// miss means the inputs really are new and a hit cannot be stale.
let originCache = { name: null, text: null, mapping: null, spellings: null, spans: [] };

/** originSpans(name, text, mapping, spellings) is the memoised valueSpans call.
 *  The Compare card asks for the spans on every repaint and gets the same array
 *  back until one of the four inputs is genuinely a different object. */
function originSpans(name, text, mapping, spellings) {
  if (originCache.name === name && originCache.text === text &&
      originCache.mapping === mapping && originCache.spellings === spellings) {
    return originCache.spans;
  }
  const spans = valueSpans(text, mapping, spellings);
  originCache = { name, text, mapping, spellings, spans };
  return spans;
}

/**
 * paneWalk(text, state) computes one pane's hits for its OWN needle and resolves
 * which hit is active.
 *
 * Each pane searches on its own: the search bar lives in the pane's caption, so
 * there is no shared cursor to keep two lists of different lengths in step, and
 * the readout does not name a pane because the bar is already on it.
 *
 * @param {string} text the pane's plain text
 * @param {{needle:string,index:number}} state that pane's search state
 * @returns {object} {hits, total, index, active, capped} where active is the
 *   active hit's index in this pane, or -1 when there is none
 */
export function paneWalk(text, state) {
  const needle = state?.needle ?? "";
  const hits = findHits(String(text ?? ""), needle);
  const total = hits.length;
  // The index is clamped rather than corrected in place: the text can change
  // under a stale index (a re-run, a new document) and a clamp here means every
  // reader sees the same answer without anyone having to reset it first.
  const index = total === 0 ? 0 : ((state.index % total) + total) % total;
  return {
    hits,
    total,
    index,
    active: total === 0 ? -1 : index,
    // A pane caps independently, so hitting the cap means the highlight is
    // showing a prefix and should say so.
    capped: total >= MAX_HITS,
  };
}

/**
 * paneCaption(label, pane, walk, state) is one pane's caption: its name on the
 * left and its OWN search bar aligned right. The search bar belongs here, on the
 * pane it searches, rather than in the shared card head.
 */
export function paneCaption(label, pane, walk, state) {
  return `<div class="pane-caption">` +
    `<span class="pane-caption-label">${escapeHTML(label)}</span>` +
    paneSearchControls(pane, walk, state) +
    `</div>`;
}

/**
 * paneSearchControls(pane, walk, state) is one pane's search bar: the field, its
 * two navigation buttons and the readout. `pane` is "original" or "anonymised"
 * and namespaces every id and data attribute so the two bars never address each
 * other.
 *
 * Both buttons are disabled with a title when there is nothing to step through,
 * because a greyed control that says nothing is a dead end.
 */
export function paneSearchControls(pane, walk, state) {
  const none = walk.total === 0;
  const hasNeedle = (state?.needle ?? "").trim().length > 0;
  const readout = none
    ? (hasNeedle ? ANONYMISE.searchNone : "")
    : ANONYMISE.searchCount(walk.index + 1, walk.total);

  return `<div class="compare-search" data-pane="${escapeHTML(pane)}">` +
    searchBox({
      id: `compare-search-${pane}`, value: state?.needle ?? "",
      placeholder: ANONYMISE.searchPlaceholder, label: searchLabelFor(pane),
      clearLabel: VALUES.clearSearch,
    }) +
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

/** searchLabelFor(pane) is the accessible name of one pane's search field: the
 *  two bars look alike, so each says which pane it searches. */
function searchLabelFor(pane) {
  return pane === "original" ? ANONYMISE.searchLabelOriginal : ANONYMISE.searchLabelAnonymised;
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
export function selectionPanel(s = getState(), view = selectionViewState()) {
  if (!view.selection) return "";
  return `<div class="selection-card" id="selection-card"` +
    ` style="left:${view.selection.x}px;top:${view.selection.y}px">` +
    sectionLabel(ANONYMISE.selectionTitle) +
    `<span class="selection-text mono">${escapeHTML(view.selection.text)}</span>` +
    selectionBody(s, view) +
    (view.error ? `<p class="hint bad">${escapeHTML(view.error)}</p>` : "") +
    `</div>`;
}

/**
 * selectionViewState() bundles the panel's view-local state. The panel is
 * rendered from it rather than from the module variables directly, so the
 * stages can be exercised without a live selection.
 */
function selectionViewState() {
  return {
    selection, stage: selectionStage, mode: selectionMode,
    target: selectionTarget, category: selectionCategory,
    picked: selectionPicked, error: selectionError,
  };
}

/** selectionBody(s, view) is whichever of the three stages is current. */
function selectionBody(s, view) {
  if (view.stage === null) return selectionStageChoose();
  if (view.mode === null) return selectionStageMode();
  return selectionStageFields(s, view);
}

/** Stage 1: copy the text out, or go on to replace it. */
function selectionStageChoose() {
  return `<div class="run-actions">` +
    button(ANONYMISE.selectionCopy, { kind: "secondary", id: "btn-selection-copy", icon: "content_copy" }) +
    button(ANONYMISE.selectionReplace, { kind: "primary", id: "btn-selection-replace" }) +
    `</div>`;
}

/**
 * Stage 2: WHICH KIND of replacement. Both hints are load-bearing: the modes
 * differ in what ends up in the re-identification key, and that is not guessable
 * from the labels.
 */
function selectionStageMode() {
  const option = (mode, label, hint) =>
    `<label class="selection-option">` +
    `<input type="radio" name="selection-mode" class="selection-mode" value="${escapeHTML(mode)}"/>` +
    `<span class="selection-option-body">` +
    `<span class="selection-option-name">${escapeHTML(label)}</span>` +
    `<span class="hint">${escapeHTML(hint)}</span>` +
    `</span></label>`;

  return `<div class="selection-options">` +
    option("spelling", ANONYMISE.selectionModeVariant, ANONYMISE.selectionModeVariantHint) +
    option("value", ANONYMISE.selectionModeValue, ANONYMISE.selectionModeValueHint) +
    `</div>` +
    `<div class="run-actions">` +
    button(ANONYMISE.selectionBack, { kind: "ghost", id: "btn-selection-back" }) +
    `</div>`;
}

/** Stage 3: the fields the chosen mode needs, plus Apply and Cancel. */
function selectionStageFields(s, view) {
  let fields = "";
  if (view.mode === "spelling") {
    // The same autocomplete the Selected placeholder card uses, so "which value
    // is this a spelling of" is answered the same way in both places, and the
    // same REAL buttons rather than a native <datalist>: a popup rebuilt on
    // every repaint closes itself mid-keystroke, which is why the datalist
    // version of this field could never be typed into.
    const picks = view.target.trim() ? valueAutocomplete(view.target, s).slice(0, 6) : [];
    fields =
      `<label class="field-label">${escapeHTML(ANONYMISE.selectionTargetLabel)}` +
      `<input id="selection-target" autocomplete="off"` +
      ` value="${escapeHTML(view.target)}"` +
      ` placeholder="${escapeHTML(ANONYMISE.selectionTargetPlaceholder)}"` +
      ` aria-label="${escapeHTML(ANONYMISE.selectionTargetLabel)}"/></label>` +
      `<div class="reassign-list" id="selection-target-list">` +
      picks.map(valuePickButton).join("") +
      `</div>`;
  } else {
    fields =
      `<label class="field-label">${escapeHTML(ANONYMISE.selectionTypeLabel)}` +
      categorySelect(view.category, { id: "selection-category", ariaLabel: ANONYMISE.selectionTypeLabel }) +
      `</label>`;
  }

  return fields +
    `<div class="run-actions">` +
    // Cancel steps BACK to stage 1 rather than closing: a mis-click on a mode
    // must not throw away the selection the user made.
    button(ANONYMISE.cancelSelection, { kind: "secondary", id: "btn-cancel-selection" }) +
    button(ANONYMISE.applySelection, { kind: "primary", id: "btn-apply-selection" }) +
    `</div>`;
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
  wireBlocked(container, s);
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
      // A full run applies the declarations too, so every Value on the list is a
      // Value OF the run now rather than one this screen just added.
      declaredHere.clear();
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

/**
 * wireBlocked(container, s) binds the refused-run panel's resolve actions.
 *
 * Neither re-runs. Clearing a conflict and deciding to run again are two
 * decisions, and re-running on the first click would replace text the user has
 * not looked at since the refusal.
 */
function wireBlocked(container, s) {
  for (const btn of container.querySelectorAll(".blocked-allow-remove")) {
    btn.addEventListener("click", () => {
      const term = btn.dataset.term;
      if (!term) return;
      removeAllowTerm(term);
      notify(ANONYMISE.blockedAllowTermRemoved(term), "ok");
    });
  }
  for (const btn of container.querySelectorAll(".blocked-delete-value")) {
    btn.addEventListener("click", () => {
      const { category, mainText } = btn.dataset;
      if (!category || !mainText) return;
      const value = s.values
        .find((v) => valueKey(v.category, v.mainText) === valueKey(category, mainText));
      deleteValue(category, mainText);
      notify(ANONYMISE.blockedValueDeleted(value?.mainText ?? mainText), "ok");
    });
  }
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

  // Scoped to this card: the selection panel renders picks of its own with the
  // same styling class, and they mean a different thing (which Value the
  // selected TEXT is a spelling of, not which Value THIS mark reassigns to).
  const selectedCardEl = container.querySelector("#selected-card");
  for (const pick of selectedCardEl?.querySelectorAll(".reassign-pick") ?? []) {
    pick.addEventListener("click", async () => {
      const { category, mainText } = pick.dataset;
      const original = selectedMark?.original;
      if (!original) return;
      if (!reassignOriginal(original, category, mainText)) {
        notify(ANONYMISE.reassignRefused(original, mainText), "warn");
        return;
      }
      selectedMark = null;
      reassignDraft = "";
      await runFastRerun(container, ANONYMISE.reassignDone(original, mainText));
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
  const matches = container.querySelector("#missed-matches");
  category?.addEventListener("change", () => { drafts.missedCategory = category.value; });

  // Debounced, the same as the step 2 add row: one bridge round-trip per
  // keystroke over a whole batch of documents would be a count nobody waits
  // for.
  let matchTimer = null;
  value?.addEventListener("input", () => {
    drafts.missed = value.value;
    // A further edit is the user acting on the refusal; showing the same
    // sentence over a text that changed would be describing a fixed problem.
    if (missedError) {
      missedError = "";
      const errorEl = container.querySelector("#missed-error");
      if (errorEl) errorEl.remove();
    }
    if (matchTimer) clearTimeout(matchTimer);
    const term = value.value.trim();
    if (!term) {
      drafts.missedMatches = "";
      if (matches) matches.textContent = "";
      return;
    }
    matchTimer = setTimeout(async () => {
      try {
        const info = await countTermMatches(term);
        // The field may have moved on while the count was in flight; a stale
        // answer under a different word is worse than no answer.
        if (value.value.trim() !== term) return;
        drafts.missedMatches = WORKSPACE.valueMatches(info?.count ?? 0, info?.documents ?? 0);
      } catch {
        drafts.missedMatches = ""; // no bridge: say nothing rather than guess
      }
      if (matches) matches.textContent = drafts.missedMatches;
    }, 250);
  });

  // Adding a value here adds it to the VALUE LIST, and nothing happens to the
  // text until the fast re-run applies it. The two are separate buttons because
  // they are separate decisions: several values are usually added at once.
  const add = async () => {
    const text = (drafts.missed ?? "").trim();
    if (!text) return;
    missedError = "";

    // A value that is a spelling of one already listed joins it instead of
    // becoming a rival, exactly as the step 2 add row and the selection panel
    // already do: without this, "Coca-Cola company" beside "Coca-Cola" leaves
    // the text reading "[BRAND_1] company". A folded spelling is not a new
    // declaration and cannot conflict with itself, so the conflict check below
    // only runs when this did NOT fold.
    const family = foldIntoFamily(drafts.missedCategory, text);
    if (family) {
      drafts.missed = "";
      drafts.missedMatches = "";
      notify(WORKSPACE.foldedIntoValue(text, family.main), "info");
      setState({});
      return;
    }

    // Refused HERE, before addValues, so the conflict is met on the field the
    // user is typing into rather than as a refused run on another screen: the
    // draft text stays put so the fix is right there to make.
    const conflicts = checkValueConflict(drafts.missedCategory, text, getState());
    if (conflicts.length > 0) {
      missedError = conflictMessage(conflicts[0]);
      setState({});
      return;
    }

    if (addValues([{ category: drafts.missedCategory, mainText: text }])) {
      declaredHere.add(valueKey(drafts.missedCategory, text));
    } else {
      notify(ANONYMISE.missedAlreadyThere(text), "info");
      return;
    }
    pendingMissedTexts.push(text);
    drafts.missed = "";
    drafts.missedMatches = "";
    setState({});
  };
  container.querySelector("#btn-add-missed")?.addEventListener("click", add);
  value?.addEventListener("keydown", (ev) => { if (ev.key === "Enter") add(); });

  container.querySelector("#btn-fast-rerun")?.addEventListener("click", async () => {
    const expected = pendingMissedTexts;
    pendingMissedTexts = [];
    await runFastRerun(container, ANONYMISE.fastRerunDone(getState().values.length), expected);
  });
}

/**
 * wireCompareSearch(container) wires BOTH panes' search bars, then scrolls each
 * pane to its own active hit. The two bars are independent, so each is wired the
 * same way against its own pane.
 */
function wireCompareSearch(container) {
  wirePaneSearch(container, "original", "#original-pane");
  wirePaneSearch(container, "anonymised", "#anonymised-pane");
}

/**
 * wirePaneSearch(container, pane, paneSelector) wires one pane's search box, its
 * two buttons and the keyboard, then scrolls that pane to its active hit.
 *
 * The input keeps focus and caret across the repaint each keystroke causes, the
 * same pattern the values search bar uses: a search box that loses focus
 * mid-word cannot be typed into.
 */
function wirePaneSearch(container, pane, paneSelector) {
  const id = `compare-search-${pane}`;
  // Typing and the field's own ✕ both arrive here, so the two cannot drift.
  const input = wireSearchBox(container, id, (needle, field) => {
    const caret = field.selectionStart;
    // A new needle starts at its first hit: keeping the old position would land
    // the user somewhere unrelated to what they just typed.
    search = { ...search, [pane]: { needle, index: 0 } };
    lastScrolledTo = { ...lastScrolledTo, [pane]: null };
    if (compareSearchTimer[pane]) clearTimeout(compareSearchTimer[pane]);
    compareSearchTimer[pane] = setTimeout(() => {
      compareSearchTimer[pane] = null;
      setState({});
      // The repaint rewrote the whole shell (main.js paint), so the field this
      // closure captured through `container` is now a DETACHED node: focusing it
      // does nothing and the caret lands nowhere, which is why the box took
      // exactly one character before the user had to click back into it. Re-query
      // the LIVE document for the freshly-rendered field instead.
      const again = (container.ownerDocument ?? globalThis.document)?.getElementById(id);
      if (again) {
        again.focus();
        again.setSelectionRange?.(caret, caret);
      }
    }, 150);
  });

  input?.addEventListener("keydown", (ev) => {
    // Enter and Shift+Enter step through this pane's hits without leaving the
    // field, which is how every search box behaves. Escape clears this pane's.
    if (ev.key === "Enter") {
      ev.preventDefault();
      stepSearch(pane, ev.shiftKey ? -1 : 1);
    } else if (ev.key === "Escape") {
      ev.preventDefault();
      search = { ...search, [pane]: { needle: "", index: 0 } };
      lastScrolledTo = { ...lastScrolledTo, [pane]: null };
      setState({});
    }
  });

  const bar = container.querySelector(`.compare-search[data-pane="${pane}"]`);
  bar?.querySelector(".search-prev")?.addEventListener("click", () => stepSearch(pane, -1));
  bar?.querySelector(".search-next")?.addEventListener("click", () => stepSearch(pane, 1));

  scrollToActiveHit(container, pane, paneSelector);
}

// The pending repaint from a keystroke in each pane's search box, per pane.
// Debounced so the input survives a burst of typing.
let compareSearchTimer = { original: null, anonymised: null };

/**
 * stepSearch(pane, delta) moves through ONE pane's hit list and wraps at both
 * ends. The index is normalised at read time (paneWalk), so this only has to
 * add.
 */
function stepSearch(pane, delta) {
  const cur = search[pane];
  search = { ...search, [pane]: { ...cur, index: cur.index + delta } };
  setState({});
}

/**
 * scrollToActiveHit(container, pane, paneSelector) brings ONE pane's active hit
 * into view, but ONLY when it changed since the last paint.
 *
 * The active hit is looked up INSIDE that pane's body, so one pane's search
 * never scrolls the other. scroll.js restores each pane's previous offset on
 * every repaint; scrolling unconditionally would fight that and drag the pane
 * back to the active hit on every keystroke, including the ones meant to scroll
 * somewhere else.
 */
function scrollToActiveHit(container, pane, paneSelector) {
  const paneEl = container.querySelector(paneSelector);
  const active = paneEl?.querySelector(".find-hit.active");
  if (!active) {
    lastScrolledTo = { ...lastScrolledTo, [pane]: null };
    return;
  }
  const st = search[pane];
  const key = `${st.needle}|${st.index}`;
  if (key === lastScrolledTo[pane]) return;
  lastScrolledTo = { ...lastScrolledTo, [pane]: key };

  // The scroll is deferred one frame ON PURPOSE. This runs DURING the view
  // render, but main.js paint() calls scroll.js restoreScrollPositions AFTER the
  // view renders, which puts every pane back to the offset it had BEFORE this
  // paint. A scrollIntoView done now would be overwritten a moment later, so
  // moving to the next match never repositioned the preview. Running it after
  // the paint completes lets the move win, which is exactly right: the active
  // hit changed, so this pane SHOULD travel to it, the way a document search in
  // any editor does. The element is re-queried live at that point in case a
  // further paint has replaced it.
  const doc = container.ownerDocument ?? globalThis.document;
  const win = doc?.defaultView;
  const bringIntoView = () => {
    const liveActive = doc?.querySelector(`${paneSelector} .find-hit.active`);
    liveActive?.scrollIntoView?.({ block: "center" });
  };
  if (win?.requestAnimationFrame) win.requestAnimationFrame(bringIntoView);
  else bringIntoView();
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
    resetSelectionPanel();
    // The search offsets belong to ONE text, so both panes' positions reset. The
    // needles are kept: the user is looking for the same thing in the next
    // document.
    search = {
      original: { ...search.original, index: 0 },
      anonymised: { ...search.anonymised, index: 0 },
    };
    lastScrolledTo = { original: null, anonymised: null };
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
  wireOriginLink(container);

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
    // When this occurrence replaced a spelling spelling, lead with what was
    // actually on the page and keep the mainText value in brackets:
    // "Borch (Johannes Borch)". A mainText match shows the value alone.
    const spelling = mark.dataset.spelling;
    const originalDisplay = spelling ? `${spelling} (${original})` : original;

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

/**
 * wireOriginLink(container) tints, in the ORIGINAL pane, everything one
 * placeholder replaced, while the pointer (or the keyboard focus) is on that
 * placeholder in the ANONYMISED pane.
 *
 * This is the one gesture that answers "what did [PERSON_1] used to be" for a
 * value with several spellings. The tooltip names ONE of them, the one under
 * the pointer; the tint shows the whole family in place, so a reader checking
 * the anonymisation can see that "Johannes Borch" and "Borch" both went and
 * that nothing else did.
 *
 * It is deliberately its own wiring rather than a branch inside
 * wireMarkTooltip: the tooltip is about what the mark MEANS and has to measure
 * and place a floating element, this is about where the mark CAME FROM and only
 * toggles a class. Folding them together would make the tint depend on the
 * tooltip's layout maths succeeding.
 *
 * Exported for the wiring tests.
 */
export function wireOriginLink(container) {
  const originPane = container.querySelector("#original-pane");
  const anonPane = container.querySelector("#anonymised-pane");
  if (!originPane || !anonPane) return;

  // Collected once: the pane is rebuilt on every repaint, so this list lives
  // exactly as long as the elements in it do.
  const spans = originPane.querySelectorAll(".value-origin");

  const clear = () => {
    for (const span of spans) span.classList.remove("is-linked");
  };
  const link = (placeholder) => {
    for (const span of spans) {
      span.classList.toggle("is-linked", span.dataset.ph === placeholder);
    }
  };

  for (const mark of anonPane.querySelectorAll("mark[data-ph]")) {
    mark.addEventListener("mouseenter", () => link(mark.dataset.ph));
    mark.addEventListener("mouseleave", clear);
    // Keyboard parity: the marks are focusable, so the tint is reachable
    // without a pointer, exactly like the tooltip.
    mark.addEventListener("focus", () => link(mark.dataset.ph));
    mark.addEventListener("blur", clear);
  }
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
      const sel = container.ownerDocument?.defaultView?.getSelection?.();
      if (!sel || sel.isCollapsed) {
        if (selection) { resetSelectionPanel(); setState({}); }
        return;
      }
      const text = sel.toString().trim();
      // A paragraph-length "value" is a mis-drag, not a value. 120 characters is
      // generously long for a name, an address or an account number.
      if (!text || text.length > 120 || !pane.contains(sel.anchorNode)) {
        if (selection) { resetSelectionPanel(); setState({}); }
        return;
      }
      const rect = sel.getRangeAt(0).getBoundingClientRect();
      const hostRect = host.getBoundingClientRect();
      resetSelectionPanel();
      // Clamped to the card's bounds, the same clamp the mark tooltip makes:
      // a selection near the right-hand edge would otherwise push the panel off
      // screen, and the panel is transform: translate(-50%) about this point.
      const half = SELECTION_PANEL_WIDTH / 2;
      const centre = rect.left + rect.width / 2 - hostRect.left;
      selection = {
        text,
        x: Math.max(half + 6, Math.min(centre, hostRect.width - half - 6)),
        y: rect.top - hostRect.top,
      };
      // NO placeholder is reserved here. Reserving on selection spent a
      // [CUSTOM_N] on every stray drag, and numbers are never freed by design:
      // an export or a mapping CSV in which [CUSTOM_4] means one thing may
      // already have left the machine. The reservation now happens when the
      // user chooses the mode that actually needs one.
      setState({});
    });
  }
}

export function wireSelectionPanel(container) {
  // Stage 1.
  container.querySelector("#btn-selection-copy")?.addEventListener("click", async () => {
    const text = selection?.text ?? "";
    try {
      await copyText(text);
      resetSelectionPanel();
      container.ownerDocument?.defaultView?.getSelection?.()?.removeAllRanges();
      setState({});
      notify(ANONYMISE.selectionCopied, "ok");
    } catch (err) {
      // The cap and the empty case both come back as an actionable sentence
      // from Go, and it belongs on the panel the user is looking at.
      selectionError = String(err?.message ?? err);
      setState({});
    }
  });
  container.querySelector("#btn-selection-replace")?.addEventListener("click", () => {
    selectionStage = "replace";
    selectionError = "";
    setState({});
  });

  // Stage 2.
  container.querySelector("#btn-selection-back")?.addEventListener("click", () => {
    selectionStage = null;
    selectionMode = null;
    selectionError = "";
    setState({});
  });
  for (const radio of container.querySelectorAll(".selection-mode")) {
    radio.addEventListener("change", () => {
      selectionMode = radio.value;
      selectionError = "";
      setState({});
    });
  }

  // Stage 3. The target field patches its own suggestion list IN PLACE rather
  // than going through setState: a full repaint on every keystroke destroys
  // and recreates the input (moving the caret to the end) and the two Compare
  // panes are re-highlighted for no reason. This is the same pattern the
  // Replaced values filter uses (wireValues).
  const target = container.querySelector("#selection-target");
  target?.addEventListener("input", () => {
    selectionTarget = target.value;
    selectionPicked = null;
    selectionError = "";
    const list = container.querySelector("#selection-target-list");
    if (list) {
      const picks = selectionTarget.trim() ? valueAutocomplete(selectionTarget, getState()).slice(0, 6) : [];
      list.innerHTML = picks.map(valuePickButton).join("");
      wireSelectionPicks(container);
    }
  });
  wireSelectionPicks(container);
  container.querySelector("#selection-category")?.addEventListener("change", (ev) => {
    selectionCategory = ev.target.value;
  });
  // Cancel steps BACK to stage 1 rather than closing, so a mis-click on a mode
  // does not lose the selection.
  container.querySelector("#btn-cancel-selection")?.addEventListener("click", () => {
    selectionStage = null;
    selectionMode = null;
    selectionError = "";
    setState({});
  });

  container.querySelector("#btn-apply-selection")?.addEventListener("click", () => {
    applySelection(container);
  });
}

/**
 * wireSelectionPicks(container) binds the spelling target's suggestion
 * buttons. Separate from wireSelectionPanel because the target field's input
 * handler rebuilds those buttons without a repaint, and the fresh elements
 * need their own listeners each time.
 */
function wireSelectionPicks(container) {
  const list = container.querySelector("#selection-target-list");
  for (const pick of list?.querySelectorAll(".reassign-pick") ?? []) {
    pick.addEventListener("click", () => {
      const { category, mainText } = pick.dataset;
      selectionTarget = mainText;
      selectionPicked = { category, mainText };
      selectionError = "";
      // Filled directly rather than waiting for the setState repaint: the
      // field is what the user is looking at, and the pick should read back
      // immediately.
      const input = container.querySelector("#selection-target");
      if (input) input.value = mainText;
      setState({});
    });
  }
}

/**
 * applySelection(container) carries out the chosen replace mode.
 *
 * The two modes differ in what ends up in the re-identification key, which is
 * why they are two modes and not one field: a spelling of an existing Value
 * shares that Value's placeholder, and a new Value earns its own. Both go
 * through the Value model, so neither can rewrite text without the key saying
 * what happened.
 */
export async function applySelection(container, view = selectionViewState()) {
  const text = view.selection?.text;
  if (!text) return;
  const clearSelection = () =>
    container.ownerDocument?.defaultView?.getSelection?.()?.removeAllRanges();

  if (view.mode === "spelling") {
    const main = (view.target ?? "").trim();
    if (!main) {
      selectionError = ANONYMISE.selectionNeedsTarget;
      setState({});
      return;
    }
    // A clicked pick already named the Value, so Apply does not have to
    // re-resolve it by text, provided a further keystroke has not changed the
    // field since (selectionPicked is cleared on every edit, so a stale pick
    // cannot outlive the text it was chosen for).
    const picked = view.picked && view.picked.mainText.toLowerCase() === main.toLowerCase()
      ? view.picked : null;
    // reassignOriginal refuses an unknown target, or one that IS the text. The
    // reason goes on the panel rather than into a toast: the fix is in the
    // field the user is looking at.
    const value = picked ?? valueAutocomplete(main, getState())
      .find((v) => v.mainText.toLowerCase() === main.toLowerCase());
    if (!value || !reassignOriginal(text, value.category, value.mainText)) {
      selectionError = ANONYMISE.selectionUnknownTarget;
      setState({});
      return;
    }
    resetSelectionPanel();
    clearSelection();
    await runFastRerun(container, ANONYMISE.selectionBecameVariant(text, value.mainText));
    return;
  }

  // The remaining mode is "value": a new Value of its own.
  {
    // Through foldIntoFamily first, so a new value that belongs to an existing
    // family becomes a spelling of it instead of a rival that would fire inside
    // it. addValues switches the category on, which is what makes the value
    // actually apply.
    const family = foldIntoFamily(view.category, text);
    // A value folded into an existing family is not a new declaration and
    // cannot conflict with itself, so the conflict check only applies to the
    // genuinely new branch below. Refusing HERE, before addValues, means the
    // conflict is met on the panel the user is typing into rather than as a
    // refused run they would have to undo from a different screen.
    if (!family) {
      const conflicts = checkValueConflict(view.category, text, getState());
      if (conflicts.length > 0) {
        selectionError = conflictMessage(conflicts[0]);
        setState({});
        return;
      }
    }
    let message = family ? WORKSPACE.foldedIntoValue(text, family.main) : "";
    // The registry records a match under the Value's MAIN TEXT, never the
    // spelling that matched (pipeline.go assigns mainText), so the "matched
    // nothing" check below only makes sense for a genuinely new Value: text
    // folded into an existing family would never equal that family's main
    // text and would misreport a successful fold as "not found".
    let expect = [];
    if (!family) {
      // addValues reports how many rows it actually added: a Value already
      // declared under this exact category and text adds nothing, and the
      // success notice must say that rather than repeat "became a Value" for
      // a click that changed nothing.
      const added = addValues([{ category: view.category, mainText: text, discoveryMethods: ["manual"] }]);
      message = added > 0 ? ANONYMISE.selectionBecameValue(text) : ANONYMISE.selectionAlreadyThere(text);
      if (added > 0) {
        expect = [text];
        declaredHere.add(valueKey(view.category, text));
      }
    }
    resetSelectionPanel();
    clearSelection();
    await runFastRerun(container, message, expect);
  }
}

/**
 * resetSelectionPanel() clears the panel and everything about the selection it
 * was showing. One function, so a path that closes the panel cannot leave a
 * stage or a draft behind for the next selection to inherit.
 */
function resetSelectionPanel() {
  selection = null;
  selectionStage = null;
  selectionMode = null;
  selectionTarget = "";
  selectionPicked = null;
  selectionError = "";
}

/**
 * runFastRerun(container, message, expectedTexts) re-runs the DETERMINISTIC
 * passes only and refreshes the mapping.
 *
 * "Fast" means no discovery: the Values on screen are re-applied, and
 * existing placeholders keep their numbers because the session registry is
 * unchanged. That last part is the whole point, and it is why every editing
 * action on this screen ends here rather than in a full re-run.
 *
 * @param {HTMLElement} container the view container, for the error strip
 * @param {string} message the notice to show on success
 * @param {string[]} [expectedTexts] text just declared as a Value. A Value
 *   that IS applied but matches no text in the batch earns no registry entry,
 *   so it would otherwise show up nowhere while this notice still claimed
 *   success. Checked case-insensitively against the refreshed Replaced values
 *   table; when one is missing the success notice becomes a warning naming it.
 */
async function runFastRerun(container, message, expectedTexts = []) {
  try {
    const results = await fastRerun(buildRunRequest());
    // The mapping and the value table may both have grown: new values earned
    // placeholders, and removed ones lost theirs.
    tablesLoadedFor = results;
    setState({ results, mapping: await getMapping() });
    await refreshValueTables();
    // A declaration is the only thing that can create a NEW intersection, so the
    // check runs only when one was made.
    if (expectedTexts.length > 0) await refreshIntersections();
    const missing = missingDeclaredTexts(getState().replacedValues, expectedTexts);
    if (missing.length > 0) {
      notify(ANONYMISE.declaredValueNotFound(missing.join(", ")), "warn");
    } else {
      notify(message, "ok");
    }
  } catch (err) {
    showError(container, err);
  }
}

/**
 * refreshIntersections() asks Go which Values another route also claims, so a
 * declaration this screen just made can be told when a higher-priority match
 * covers every occurrence of it.
 *
 * It uses the same request builder and the same comparator the run uses, so the
 * warning cannot describe a decision the run did not make. Silent when there is
 * no bridge: this is a warning the user never asked for, so its absence is not a
 * failure, and it must never throw into a repaint.
 */
async function refreshIntersections() {
  try {
    const res = await checkIntersections(buildIntersectionRequest());
    setIntersections(res?.intersections ?? []);
  } catch {
    setIntersections([]);
  }
}

/**
 * missingDeclaredTexts(replacedValues, expectedTexts) is the pure half of the
 * "matched nothing" check: which of the just-declared texts have no row in
 * the refreshed Replaced values table, matched case-insensitively because the
 * table shows the document's own casing and the user may have typed either.
 */
export function missingDeclaredTexts(replacedValues, expectedTexts) {
  const replaced = replacedValues ?? [];
  return (expectedTexts ?? []).filter((text) =>
    !replaced.some((r) => r.original.toLowerCase() === text.toLowerCase()));
}

function showError(container, err) {
  const slot = container.querySelector("#run-error");
  if (slot) {
    slot.innerHTML = `<div class="banner error">${escapeHTML(String(err?.message ?? err))}</div>`;
  }
}

// views/identifyworkspace.js, the WORKSPACE of wizard step 2, Identify
// (BUILD-02 Phase 9 as views/values.js; renamed by BUILD-05 Phase 2 and relaid
// out into the mock-up's four tabs by BUILD-05 Phase 6).
//
// Four tabs, each with its item count in the tab itself:
//
//   Suggestions      the review gate. Everything any detection method proposes
//                    waits here until the user accepts it: NOTHING reaches the
//                    value list without an explicit accept. Sortable by value
//                    and by count, filterable by type and by source.
//   My values        the values that WILL be replaced, one card each, with the
//                    editable placeholder, the type badge and the variant chips.
//   Never anonymise  the allowlist, which wins over every pass.
//   Patterns         user regular expressions, with a valid / error badge.
//
// One structural change beyond the layout (BUILD-05 Phase 6): "Run detection" is
// ONE button in the card header. It used to be a panel with a per-file checkbox
// list and a separate button per method. Now it runs the offline smart pass
// always, and the AI pass as well when Local AI is on, over every imported
// document. The estimateDiscovery pre-check survives; an oversized file becomes
// a notice rather than a picker the user has to operate before anything happens.
//
// Naming note (BUILD-04 CR3, restated by BUILD-05 Phase 0): the visible labels
// have changed twice now, from "Entities" to "Values" to this half of
// "Identify". The ENGINE identifiers this module manipulates (the category keys
// entity_names, person_names, ... and the state.entities array) have not changed
// once, on purpose: a label is a display string, an identifier is a contract.

import {
  runDiscovery, cancelDiscovery, estimateDiscovery, runSmartDetection,
  expandVariants, validatePattern, setEntityPlaceholder, entityPlaceholder,
} from "../api.js";
import {
  getState, setState, llmEnabled,
  addEntities, removeEntity, entityKey,
  setEntityVariants, setEntityVariantError, addManualVariant,
  addCandidates, acceptCandidate, rejectCandidate,
  acceptAllShown, rejectAllShown, moveVariant,
  addPattern, removePattern,
  smartDetectOptions,
} from "../state.js";
import { pendingExpansions } from "../entitymodel.js";
import {
  visibleCandidates, toggleCountSort, toggleValueSort, DEFAULT_CANDIDATE_FILTER,
} from "../candidatemodel.js";
import { escapeHTML } from "../html.js";
import { button, tabbar, icon, toastHTML } from "../ui.js";
import { llmGateTooltip } from "./identifyrail.js";
import { renderAllowlistChips, wireAllowlistChips } from "./allowlist.js";
import { notify, wireNotice } from "../toast.js";
import { keepScrollPosition } from "../scroll.js";
import { CARDS, WORKSPACE, VALUES, CATEGORY_LABELS } from "../copy.js";

// The reviewable entity categories (CLAUDE.md §5) with display labels. These
// are the categories a user may ADD a value to by hand; the PII categories are
// detected, not listed.
export const CATEGORIES = [
  ["entity_names", "Entities"],
  ["project_names", "Projects"],
  ["person_names", "Persons"],
];

// WORKSPACE_TABS is the tab set, in order.
export const WORKSPACE_TABS = ["suggestions", "values", "allow", "patterns"];

// --- View-local state -----------------------------------------------------
//
// None of this belongs in the store: nothing downstream reads it, it must not
// travel in a session file, and routing a sort click through a reducer would
// make a view preference part of the application's business state.

let activeTab = "suggestions";
let candidateFilter = { ...DEFAULT_CANDIDATE_FILTER, source: "" };
// The draft text of the three add rows, kept across repaints so a state change
// elsewhere does not empty a half-typed value.
const drafts = { value: "", valueCategory: "entity_names", allow: "", pattern: "" };
// Per-entity inline feedback (a refused placeholder, a duplicate variant),
// keyed by entityKey. Cleared for a row as soon as it succeeds at anything.
const rowFeedback = new Map();
// The variant chip currently being dragged, or null. It is held here rather than
// only in the DataTransfer because a dragover handler is not allowed to read
// DataTransfer contents, and the drop target has to know where the drag started
// to refuse a drop onto its own card.
let dragging = null;

/**
 * renderIdentifyWorkspace(container, opts) fills the workspace card.
 * @param {HTMLElement} container the card element views/identify.js created
 * @param {object} [opts]
 * @param {string} [opts.footerHTML] the screen's step footer, rendered as the
 *   card's foot. It is passed in rather than built here because only
 *   views/identify.js knows about both halves of the screen.
 */
export function renderIdentifyWorkspace(container, opts = {}) {
  const s = getState();
  const shown = visibleCandidates(s.candidates, candidateFilter);
  const busy = s.discovery?.running === true;

  container.innerHTML =
    head(s, busy) +
    `<div class="workspace-tabs">${tabs(s)}</div>` +
    `<div class="card-body stack">${tabBody(s, shown)}</div>` +
    toastHTML(s.notice) +
    (opts.footerHTML ?? "");

  wire(container, s, shown);
}

/** head(s, busy) is the card header: the title, the live counts, the search box
 *  and the one Run detection button. */
function head(s, busy) {
  const search =
    `<label class="search-box">${icon("search")}` +
    `<input id="workspace-search" value="${escapeHTML(candidateFilter.search)}"` +
    ` placeholder="${escapeHTML(VALUES.searchPlaceholder)}"` +
    ` aria-label="${escapeHTML(VALUES.searchPlaceholder)}"/></label>`;

  // The run button says what it will DO, which depends on whether the local AI
  // is on: the offline pass always runs, the AI pass only when it can.
  const aiOK = llmEnabled(s);
  const runTitle = aiOK ? WORKSPACE.runWithAI : WORKSPACE.runOffline;
  const run = busy
    ? button(WORKSPACE.cancel, { kind: "secondary", id: "btn-detect-cancel", icon: "cancel" })
    : button(WORKSPACE.runDetection, {
      kind: "secondary", id: "btn-detect", icon: "smart_toy",
      disabled: s.documents.length === 0,
      title: s.documents.length === 0 ? WORKSPACE.runNeedsDocuments : runTitle,
    });

  return `<div class="card-head with-controls">` +
    `<div class="card-head-left"><h2>${escapeHTML(CARDS.identify.title)}</h2>` +
    `<span class="card-sub">${escapeHTML(subtitle(s))}</span></div>` +
    `<div class="card-head-right">${search}${run}</div>` +
    `</div>` +
    progressStrip(s);
}

/** subtitle(s) is the live count beside the heading. */
function subtitle(s) {
  const waiting = s.candidates.length;
  const accepted = s.entities.filter((e) => e.status === "accepted").length;
  return WORKSPACE.subtitle(waiting, accepted);
}

/** progressStrip(s) is the determinate per-file bar shown while a detection run
 *  is in flight (BUILD-02 Phase 7c/7d). */
function progressStrip(s) {
  const d = s.discovery;
  // The gate is deliberately `=== true` and nothing else (BUILD-04 CR17): the
  // bar must depend on a run being in flight, never on a leftover object.
  if (d?.running !== true) return "";
  const pct = d.total ? Math.round(((d.current + 1) / d.total) * 100) : 0;
  return `<div class="detect-progress">` +
    `<div class="progress-bar"><div style="width:${pct}%"></div></div>` +
    `<span class="hint mono">${escapeHTML(WORKSPACE.scanning(d.file ?? "", d.current + 1, d.total))}</span>` +
    `</div>`;
}

function tabs(s) {
  const counts = {
    suggestions: s.candidates.length,
    values: s.entities.length,
    allow: s.allowlist.length,
    patterns: s.patterns.length,
  };
  return tabbar(WORKSPACE_TABS.map((id) => ({
    id, label: WORKSPACE.tabLabels[id], count: counts[id], active: activeTab === id,
  })), { attr: "wstab", ariaLabel: WORKSPACE.tabsLabel });
}

function tabBody(s, shown) {
  switch (activeTab) {
    case "values": return valuesTab(s);
    case "allow": return allowTab(s);
    case "patterns": return patternsTab(s);
    default: return suggestionsTab(s, shown);
  }
}

// --- Suggestions ----------------------------------------------------------

// The suggestions grid's column template, shared by the header and the rows so
// the two cannot drift apart.
const SUGGESTION_COLUMNS = "minmax(0,1fr) minmax(6.5rem,9.5rem) 5.5rem minmax(5.75rem,8rem) 5.25rem";

/** suggestionsTab(s, shown) is the review gate. */
function suggestionsTab(s, shown) {
  const bulk =
    `<div class="bulk-actions">` +
    button(WORKSPACE.acceptAllShown, {
      kind: "secondary", id: "btn-accept-shown",
      disabled: shown.length === 0, title: WORKSPACE.bulkScopeHint,
    }) +
    button(WORKSPACE.rejectAllShown, {
      kind: "secondary", id: "btn-reject-shown",
      disabled: shown.length === 0, title: WORKSPACE.bulkScopeHint,
    }) +
    `</div>`;

  const rows = shown.map((c) => suggestionRow(c)).join("");
  const body = rows ||
    `<div class="grid-empty">${escapeHTML(VALUES.noMatchingSuggestions)}</div>`;

  return `<div class="row-between">` +
    `<p class="hint">${escapeHTML(WORKSPACE.reviewHint)}</p>${bulk}</div>` +
    `<div class="grid-box">${suggestionHeader(s)}${body}</div>`;
}

/**
 * suggestionHeader(s) is the sortable header row: VALUE and COUNT are sort
 * buttons, TYPE and SOURCE are filter selects living IN the header.
 *
 * Putting the filters in the header rather than in a row above it is the
 * mock-up's choice and a good one: a filter belongs to its column, and a
 * separate filter row costs a whole line of a fixed-height card.
 */
function suggestionHeader(s) {
  const sortState = (column) => {
    const active = candidateFilter.sort.startsWith(column);
    const asc = candidateFilter.sort.endsWith("-asc");
    return { active, asc };
  };
  const sortButton = (column, label, id, title) => {
    const { active, asc } = sortState(column);
    return `<button class="sort-btn${active ? " active" : ""}" id="${id}" title="${escapeHTML(title)}">` +
      escapeHTML(label) +
      `<span class="icon sort-arrow${active && asc ? " asc" : ""}" aria-hidden="true">` +
      icon("expand_more").replace(/^<span class="icon" aria-hidden="true">|<\/span>$/g, "") +
      `</span></button>`;
  };

  // The filter options are whatever is ACTUALLY present, not every category the
  // engine has: a filter listing twenty types when the run found two is a list
  // the user has to read past.
  const typeOptions = distinct(s.candidates, "category")
    .map((key) => ({ value: key, label: (CATEGORY_LABELS[key]?.[0] ?? key).toUpperCase() }));
  // Sources render whatever is present (decision 9). "Pattern" is deliberately
  // absent: deterministic PII matches are applied without review by design and
  // never enter state.candidates, so offering it as a filter would promise rows
  // that cannot appear.
  const sourceOptions = distinct(s.candidates, "source")
    .map((key) => ({ value: key, label: (WORKSPACE.sourceLabels[key] ?? key).toUpperCase() }));

  const select = (id, label, value, options, title) =>
    `<select class="head-select${value ? " filtered" : ""}" id="${id}" title="${escapeHTML(title)}"` +
    ` aria-label="${escapeHTML(title)}">` +
    `<option value="">${escapeHTML(label)}</option>` +
    options.map((o) =>
      `<option value="${escapeHTML(o.value)}"${o.value === value ? " selected" : ""}>` +
      `${escapeHTML(o.label)}</option>`).join("") +
    `</select>`;

  return `<div class="grid-head" style="grid-template-columns:${SUGGESTION_COLUMNS}">` +
    sortButton("value", WORKSPACE.colValue, "sort-value", VALUES.sortValueHint) +
    select("filter-type", WORKSPACE.allTypes, candidateFilter.category, typeOptions,
      WORKSPACE.filterTypeTitle) +
    sortButton("count", WORKSPACE.colCount, "sort-count", VALUES.sortCountHint) +
    select("filter-source", WORKSPACE.allSources, candidateFilter.source, sourceOptions,
      WORKSPACE.filterSourceTitle) +
    `<span class="col-actions">${escapeHTML(WORKSPACE.colActions)}</span>` +
    `</div>`;
}

function suggestionRow(c) {
  const type = CATEGORY_LABELS[c.category]?.[0] ?? c.category;
  const source = WORKSPACE.sourceLabels[c.source] ?? c.source;
  return `<div class="grid-row" style="grid-template-columns:${SUGGESTION_COLUMNS}"` +
    ` data-text="${escapeHTML(c.text)}">` +
    `<span class="cell-value" title="${escapeHTML(c.text)}">${escapeHTML(c.text)}</span>` +
    `<span class="cell-type" title="${escapeHTML(type)}">${escapeHTML(type)}</span>` +
    `<span class="cell-count mono">${escapeHTML(String(c.count ?? 0))}</span>` +
    `<span class="src-badge src-${escapeHTML(c.source)}">${escapeHTML(source)}</span>` +
    `<span class="cell-actions">` +
    button("", {
      kind: "ghost", cls: "cand-accept icon-action boxed ok", icon: "check_circle",
      ariaLabel: `Accept ${c.text}`, title: WORKSPACE.accept,
    }) +
    button("", {
      kind: "ghost", cls: "cand-reject icon-action boxed danger", icon: "close",
      ariaLabel: `Reject ${c.text}`, title: WORKSPACE.reject,
    }) +
    `</span></div>`;
}

/** distinct(rows, field) lists the values of one field, sorted, once each. */
function distinct(rows, field) {
  return [...new Set((rows ?? []).map((r) => r[field]).filter(Boolean))].sort();
}

// --- My values ------------------------------------------------------------

/** valuesTab(s) is one card per value: name, type, editable placeholder,
 *  variant chips and an add-variant control. */
function valuesTab(s) {
  const addRow =
    `<div class="add-row">` +
    `<input id="value-draft" class="grow" value="${escapeHTML(drafts.value)}"` +
    ` placeholder="${escapeHTML(WORKSPACE.addValuePlaceholder)}"` +
    ` aria-label="${escapeHTML(WORKSPACE.addValueLabel)}"/>` +
    `<select id="value-category" aria-label="${escapeHTML(WORKSPACE.addValueCategory)}">` +
    CATEGORIES.map(([key, label]) =>
      `<option value="${escapeHTML(key)}"${key === drafts.valueCategory ? " selected" : ""}>` +
      `${escapeHTML(label)}</option>`).join("") +
    `</select>` +
    button(WORKSPACE.addValue, { kind: "secondary", id: "btn-add-value" }) +
    `</div>`;

  const cards = s.entities.map((e) => valueCard(e)).join("") ||
    `<div class="grid-empty">${escapeHTML(WORKSPACE.noValues)}</div>`;

  return addRow + cards;
}

/**
 * valueCard(e) renders one value.
 *
 * The placeholder field is editable (BUILD-05 Phase 3). Before the first run
 * there is nothing to rename, so it renders empty and disabled with a tooltip
 * saying so rather than showing a guess the engine has not made.
 */
function valueCard(e) {
  const key = entityKey(e.category, e.canonical);
  const type = CATEGORY_LABELS[e.category]?.[0] ?? e.category;
  const placeholder = e.placeholder ?? "";
  const feedback = rowFeedback.get(key);

  const variants = [...(e.variants ?? []), ...(e.manualVariants ?? [])];
  // The chips are DRAGGABLE onto another value card, which is how a variant is
  // regrouped when the expansion attached it to the wrong value (BUILD-02
  // Phase 9d, kept through the BUILD-05 relayout). The mock-up does not show
  // this, but the capability is real, tested (state.js moveVariant) and has no
  // other home: dropping it would mean a mis-grouped spelling could only be
  // fixed by excluding it here and re-typing it there.
  const chips = variants.map((v) =>
    `<span class="chip-tag variant-chip" draggable="true" data-variant="${escapeHTML(v)}"` +
    ` title="${escapeHTML(WORKSPACE.variantDragHint)}">${escapeHTML(v)}` +
    button("", {
      kind: "ghost", cls: "chip-remove variant-del", icon: "close",
      ariaLabel: `Remove variant ${v}`, title: WORKSPACE.removeVariant,
      data: { variant: v },
    }) +
    `</span>`).join("");

  // "pending" is a real state, not an absence: null variants mean an expansion
  // is in flight, [] means it finished and found none (BUILD-02 Phase 7a).
  const variantNote = e.variantError
    ? `<span class="hint bad">${escapeHTML(e.variantError)}</span>`
    : (e.variants === null || e.variants === undefined)
      ? `<span class="hint">${escapeHTML(WORKSPACE.variantsPending)}</span>`
      : (variants.length === 0 ? `<span class="hint">${escapeHTML(WORKSPACE.noVariants)}</span>` : "");

  return `<div class="value-card" data-key="${escapeHTML(key)}"` +
    ` data-category="${escapeHTML(e.category)}" data-canonical="${escapeHTML(e.canonical)}">` +
    `<div class="row-between">` +
    `<div class="value-head">` +
    `<span class="value-name">${escapeHTML(e.canonical)}</span>` +
    `<span class="fmt-badge">${escapeHTML(type)}</span>` +
    `<input class="ph-input mono" value="${escapeHTML(placeholder)}"` +
    (placeholder ? "" : ` disabled placeholder="${escapeHTML(WORKSPACE.placeholderPending)}"`) +
    ` title="${escapeHTML(placeholder ? WORKSPACE.placeholderTooltip : WORKSPACE.placeholderPendingTooltip)}"` +
    ` aria-label="${escapeHTML(WORKSPACE.placeholderLabel)}"/>` +
    `</div>` +
    button("", {
      kind: "ghost", cls: "value-remove icon-action danger", icon: "close",
      ariaLabel: `Remove ${e.canonical}`, title: WORKSPACE.removeValue,
    }) +
    `</div>` +
    `<div class="chip-row variant-row">` +
    `<span class="hint">${escapeHTML(WORKSPACE.variants)}</span>${chips}` +
    button(WORKSPACE.addVariant, { kind: "ghost", cls: "chip-add variant-add", icon: "add" }) +
    `</div>` +
    (variantNote ? `<div class="value-note">${variantNote}</div>` : "") +
    (feedback ? `<div class="value-note"><span class="hint bad">${escapeHTML(feedback)}</span></div>` : "") +
    `</div>`;
}

// --- Never anonymise ------------------------------------------------------

function allowTab(s) {
  return renderAllowlistChips(s, drafts.allow);
}

// --- Patterns -------------------------------------------------------------

function patternsTab(s) {
  const addRow =
    `<div class="add-row">` +
    `<input id="pattern-draft" class="grow mono" value="${escapeHTML(drafts.pattern)}"` +
    ` spellcheck="false" placeholder="${escapeHTML(WORKSPACE.addPatternPlaceholder)}"` +
    ` aria-label="${escapeHTML(WORKSPACE.addPattern)}"/>` +
    button(WORKSPACE.addPattern, { kind: "secondary", id: "btn-add-pattern" }) +
    `</div>`;

  const rows = s.patterns.map((p) =>
    `<div class="pattern-row" data-expr="${escapeHTML(p.expr)}">` +
    `<span class="mono">${escapeHTML(p.expr)}</span>` +
    `<span class="state-tag${p.error ? " bad" : ""}">` +
    `${escapeHTML(p.error ? p.error : WORKSPACE.patternValid)}</span>` +
    `<span class="spacer"></span>` +
    button("", {
      kind: "ghost", cls: "pattern-del icon-action danger", icon: "close",
      ariaLabel: `Remove pattern ${p.expr}`, title: WORKSPACE.removePattern,
    }) +
    `</div>`).join("");

  return `<p class="hint">${escapeHTML(WORKSPACE.patternsHint)}</p>` + addRow +
    (rows ? `<div class="grid-box">${rows}</div>` : "") +
    `<div id="pattern-feedback" class="hint"></div>`;
}

// --- Wiring ---------------------------------------------------------------

function wire(container, s, shown) {
  for (const tab of container.querySelectorAll("[data-wstab]")) {
    tab.addEventListener("click", () => {
      activeTab = tab.dataset.wstab;
      setState({}); // repaint; the tab is view state
    });
  }

  const search = container.querySelector("#workspace-search");
  search?.addEventListener("input", () => {
    // Repaint on every keystroke, but keep the caret: the input is re-created,
    // so its value and selection have to be restored by hand.
    const caret = search.selectionStart;
    candidateFilter = { ...candidateFilter, search: search.value };
    setState({});
    const again = container.querySelector("#workspace-search");
    if (again) {
      again.focus();
      again.setSelectionRange(caret, caret);
    }
  });

  wireDetection(container, s);
  wireNotice(container);

  if (activeTab === "suggestions") wireSuggestions(container, shown);
  else if (activeTab === "values") wireValues(container);
  else if (activeTab === "allow") wireAllowlistChips(container, drafts);
  else if (activeTab === "patterns") wirePatterns(container);
}

// --- Run detection --------------------------------------------------------

/**
 * wireDetection(container, s) wires the ONE Run detection button.
 *
 * It folds what used to be two panels and a per-file picker into a single
 * action over every imported document (BUILD-05 Phase 6):
 *
 *   the offline smart pass  always, so the button works with Ollama absent
 *   the local AI pass       as well, when Local AI is on AND reachable
 *
 * The estimateDiscovery pre-check survives, because an oversized file failing
 * mid-run is a much worse experience than being told up front. What changed is
 * the shape of the answer: an oversized file becomes a NOTICE naming it, and
 * the run continues over the rest, instead of a picker the user has to operate
 * before anything happens at all.
 */
function wireDetection(container, s) {
  container.querySelector("#btn-detect-cancel")?.addEventListener("click", () => cancelDiscovery());

  const btn = container.querySelector("#btn-detect");
  if (!btn || btn.disabled) return;

  btn.addEventListener("click", async () => {
    const all = getState().documents.map((d) => d.name);
    if (all.length === 0) return;

    let files = all;
    const aiOK = llmEnabled(getState());

    // The size pre-check only matters for the AI pass: the offline pass streams
    // and has no context window to overflow.
    if (aiOK) {
      try {
        const estimates = await estimateDiscovery(files);
        const tooLarge = (estimates ?? []).filter((e) => e.tooLarge);
        if (tooLarge.length) {
          const excluded = new Set(tooLarge.map((e) => e.name));
          files = files.filter((f) => !excluded.has(f));
          notify(WORKSPACE.tooLargeNotice(tooLarge.map((e) => e.name)), "warn");
        }
      } catch { /* estimation is advisory; the run still guards itself */ }
    }

    setState({ discovery: { running: true, current: 0, total: all.length, file: all[0] } });
    let added = 0;
    try {
      // The offline pass first, and unconditionally. Its tuning travels with the
      // call (BUILD-04 CR13), read from the store so the options in force are
      // exactly the ones the rail shows.
      const smart = await runSmartDetection(
        all, getState().allowlist, false, smartDetectOptions(getState()));
      added += addCandidates(smart?.candidates ?? [], "smart");

      // Then the AI pass, if it can run at all and there is anything left to
      // read after the size check.
      if (aiOK && files.length) {
        const ai = await runDiscovery(files, getState().allowlist);
        added += addCandidates(
          (ai?.proposals ?? []).map((p) => ({ text: p.text, category: p.category })), "local-ai");
      }
      notify(WORKSPACE.detectionDone(added), added ? "ok" : "info");
    } catch (err) {
      notify(String(err?.message ?? err), "warn");
    } finally {
      // ALWAYS clear the running flag (BUILD-04 CR17). Clearing it only on the
      // happy paths meant any escape in between left the button disabled forever
      // with no way back except leaving and re-entering the step.
      setState({ discovery: null });
      // Newly accepted nothing yet, but a fresh candidate list is worth looking
      // at, so land the user on it.
      activeTab = "suggestions";
      setState({});
    }
  });
}

// --- Suggestions wiring ---------------------------------------------------

function wireSuggestions(container, shown) {
  container.querySelector("#sort-value")?.addEventListener("click", () => {
    candidateFilter = { ...candidateFilter, sort: toggleValueSort(candidateFilter.sort) };
    setState({});
  });
  container.querySelector("#sort-count")?.addEventListener("click", () => {
    candidateFilter = { ...candidateFilter, sort: toggleCountSort(candidateFilter.sort) };
    setState({});
  });
  container.querySelector("#filter-type")?.addEventListener("change", (ev) => {
    candidateFilter = { ...candidateFilter, category: ev.target.value };
    setState({});
  });
  container.querySelector("#filter-source")?.addEventListener("change", (ev) => {
    candidateFilter = { ...candidateFilter, source: ev.target.value };
    setState({});
  });

  for (const row of container.querySelectorAll(".grid-row[data-text]")) {
    const text = row.dataset.text;
    row.querySelector(".cand-accept")?.addEventListener("click", async () => {
      keepScrollPosition(() => acceptCandidate(text));
      await refreshVariants();
    });
    row.querySelector(".cand-reject")?.addEventListener("click", () => {
      keepScrollPosition(() => rejectCandidate(text));
    });
  }

  // The two bulk buttons act on exactly the rows on screen, which is why they
  // are passed the FILTERED list rather than re-deriving it: a bulk action must
  // never be a surprise.
  const texts = shown.map((c) => c.text);
  container.querySelector("#btn-accept-shown")?.addEventListener("click", async () => {
    const n = acceptAllShown(texts);
    notify(WORKSPACE.acceptedN(n), n ? "ok" : "info");
    await refreshVariants();
  });
  container.querySelector("#btn-reject-shown")?.addEventListener("click", () => {
    const n = rejectAllShown(texts);
    notify(WORKSPACE.rejectedN(n), "info");
  });
}

// --- My values wiring -----------------------------------------------------

function wireValues(container) {
  const draft = container.querySelector("#value-draft");
  const category = container.querySelector("#value-category");
  draft?.addEventListener("input", () => { drafts.value = draft.value; });
  category?.addEventListener("change", () => { drafts.valueCategory = category.value; });

  const add = async () => {
    const value = (drafts.value ?? "").trim();
    if (!value) return;
    const n = addEntities([{ category: drafts.valueCategory, canonical: value }]);
    drafts.value = "";
    if (n === 0) notify(WORKSPACE.valueAlreadyThere(value), "info");
    setState({});
    await refreshVariants();
  };
  container.querySelector("#btn-add-value")?.addEventListener("click", add);
  draft?.addEventListener("keydown", (ev) => { if (ev.key === "Enter") add(); });

  for (const cardEl of container.querySelectorAll(".value-card")) {
    const { category: cat, canonical, key } = cardEl.dataset;

    cardEl.querySelector(".value-remove")?.addEventListener("click", () => {
      rowFeedback.delete(key);
      keepScrollPosition(() => removeEntity(cat, canonical));
    });

    // The editable placeholder (BUILD-05 Phase 3). Go validates the shape and
    // refuses a collision; a refusal is shown ON THE ROW rather than as a
    // notice, because it is about this value and the fix is in this field.
    const ph = cardEl.querySelector(".ph-input");
    if (ph && !ph.disabled) {
      ph.addEventListener("change", async () => {
        try {
          await setEntityPlaceholder(cat, canonical, ph.value);
          rowFeedback.delete(key);
          await refreshPlaceholders();
        } catch (err) {
          rowFeedback.set(key, String(err?.message ?? err));
          setState({});
        }
      });
    }

    // The add-variant control reveals an INLINE input rather than opening a
    // dialog: native dialogs are banned (decision 10), and an inline field is
    // fewer steps than a dialog anyway.
    cardEl.querySelector(".variant-add")?.addEventListener("click", () => {
      revealVariantInput(cardEl, cat, canonical, key);
    });

    for (const del of cardEl.querySelectorAll(".variant-del")) {
      del.addEventListener("click", (ev) => {
        // The chip itself is a drag handle, so the remove button has to stop the
        // click reaching it.
        ev.stopPropagation();
        // Removing a variant is an EXCLUSION, not a deletion: automatic expansion
        // would put it straight back otherwise. moveVariant is for regrouping;
        // here the variant simply stops applying.
        keepScrollPosition(() => excludeVariant(cat, canonical, del.dataset.variant));
      });
    }
  }

  wireVariantDrag(container);
}

/**
 * wireVariantDrag(container) makes a variant chip draggable onto another value
 * card, which regroups it (BUILD-02 Phase 9d).
 *
 * The payload is carried in a module-local variable rather than only in the
 * DataTransfer, because a WebView's dragover handler cannot read DataTransfer
 * contents (the spec hides them until the drop), and the drop target needs to
 * know which card the drag STARTED from so it can refuse a drop onto itself.
 */
function wireVariantDrag(container) {
  for (const chip of container.querySelectorAll(".variant-chip")) {
    chip.addEventListener("dragstart", (ev) => {
      const card = chip.closest(".value-card");
      dragging = {
        variant: chip.dataset.variant,
        fromCategory: card?.dataset.category,
        fromCanonical: card?.dataset.canonical,
      };
      // Setting the data is still required, or some WebViews cancel the drag.
      ev.dataTransfer?.setData("text/plain", chip.dataset.variant ?? "");
      if (ev.dataTransfer) ev.dataTransfer.effectAllowed = "move";
    });
    chip.addEventListener("dragend", () => { dragging = null; });
  }

  for (const card of container.querySelectorAll(".value-card")) {
    card.addEventListener("dragover", (ev) => {
      if (!dragging) return;
      if (card.dataset.canonical === dragging.fromCanonical &&
          card.dataset.category === dragging.fromCategory) {
        return; // a drop onto its own card would be a no-op
      }
      ev.preventDefault(); // this is what marks the element as a valid target
      card.classList.add("drop-target");
    });
    card.addEventListener("dragleave", () => card.classList.remove("drop-target"));
    card.addEventListener("drop", async (ev) => {
      ev.preventDefault();
      card.classList.remove("drop-target");
      if (!dragging) return;
      const moved = moveVariant(
        dragging.fromCategory, dragging.fromCanonical,
        card.dataset.category, card.dataset.canonical, dragging.variant);
      const { variant } = dragging;
      dragging = null;
      if (!moved) return; // a stale drop: the reducer refused it, say nothing
      notify(WORKSPACE.variantMoved(variant, card.dataset.canonical), "ok");
      await refreshVariants();
    });
  }
}

/**
 * revealVariantInput(cardEl, category, canonical, key) swaps the "add" chip for
 * an inline text input, because decision 10 bans prompt() and an inline field is
 * fewer steps than a dialog anyway.
 *
 * It does NOT go through a state change: the input is transient, and a repaint
 * would destroy it. It commits through addManualVariant, which does repaint.
 */
function revealVariantInput(cardEl, category, canonical, key) {
  const row = cardEl.querySelector(".variant-row");
  const addChip = row?.querySelector(".variant-add");
  if (!row || !addChip) return;

  const input = cardEl.ownerDocument.createElement("input");
  input.className = "variant-input";
  input.placeholder = WORKSPACE.addVariantPlaceholder;
  input.setAttribute("aria-label", WORKSPACE.addVariantPlaceholder);
  addChip.replaceWith(input);
  input.focus();

  const commit = async () => {
    const value = input.value.trim();
    if (!value) {
      setState({}); // repaint puts the add chip back
      return;
    }
    const before = variantCount(category, canonical);
    addManualVariant(category, canonical, value);
    if (variantCount(category, canonical) === before) {
      // A duplicate: say so, rather than looking like nothing happened.
      rowFeedback.set(key, WORKSPACE.variantAlreadyThere(value));
      setState({});
      return;
    }
    rowFeedback.delete(key);
    await refreshVariants();
  };

  input.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") commit();
    else if (ev.key === "Escape") setState({});
  });
  input.addEventListener("blur", commit);
}

/**
 * excludeVariant(category, canonical, variant) stops one spelling applying to a
 * value.
 *
 * It has to be an EXCLUSION rather than a removal: the automatic expansion would
 * regenerate an automatic variant on the next expansion, so deleting it would
 * look like the button did nothing. Setting variants back to pending re-expands
 * this row only.
 */
function excludeVariant(category, canonical, variant) {
  const s = getState();
  setState({
    entities: s.entities.map((e) => {
      if (entityKey(e.category, e.canonical) !== entityKey(category, canonical)) return e;
      const lower = variant.toLowerCase();
      return {
        ...e,
        manualVariants: (e.manualVariants ?? []).filter((x) => x.toLowerCase() !== lower),
        excludedVariants: [...(e.excludedVariants ?? []), variant],
        variants: null, // re-expand ONLY this row
        variantError: null,
      };
    }),
  });
  void refreshVariants();
}

/** variantCount(category, canonical) is how many spellings a value carries right
 *  now, automatic and manual together. The add flow compares it before and after
 *  so a duplicate gets an explanation instead of silence (BUILD-04 CR17). */
function variantCount(category, canonical) {
  const e = getState().entities.find(
    (x) => entityKey(x.category, x.canonical) === entityKey(category, canonical));
  if (!e) return 0;
  return (e.variants?.length ?? 0) + (e.manualVariants?.length ?? 0);
}

/**
 * refreshVariants() asks Go to expand every value whose variants are PENDING
 * (variants === null: just added, edited, or variant-amended). Settled rows,
 * including "expanded, none found" ([]), are never re-expanded (Phase 7a).
 *
 * Sequential on purpose: the lists are tiny and ordering keeps the UI
 * deterministic.
 */
async function refreshVariants() {
  // The snapshot is taken ONCE, before any await. Every expansion below writes
  // to the store and therefore repaints, and re-reading the list mid-loop would
  // mean expanding rows a repaint had already settled (BUILD-04 CR17).
  const pending = pendingExpansions(getState().entities);
  for (const e of pending) {
    try {
      const variants = await expandVariants({
        category: e.category, canonical: e.canonical,
        manualVariants: e.manualVariants, excludedVariants: e.excludedVariants ?? [],
      });
      setEntityVariants(e.category, e.canonical, variants ?? []);
    } catch (err) {
      // A failure becomes a VISIBLE error on the row, and settles it, so the
      // placeholder cannot spin forever and the row is not retried on every
      // repaint (BUILD-02 Phase 7a, re-pinned by CR17).
      setEntityVariantError(e.category, e.canonical, String(err?.message ?? err));
    }
  }
  await refreshPlaceholders();
  return pending.length;
}

/**
 * refreshPlaceholders() reads the placeholder Go currently has for each value,
 * so the editable field shows the truth rather than a guess.
 *
 * Before the first run there are none, and every field renders disabled with a
 * tooltip saying why. This is a read, so a bridge failure costs the field its
 * value and nothing else.
 */
async function refreshPlaceholders() {
  const entities = getState().entities;
  let changed = false;
  const next = [];
  for (const e of entities) {
    let placeholder = e.placeholder ?? "";
    try {
      placeholder = (await entityPlaceholder(e.category, e.canonical)) ?? "";
    } catch { /* no bridge (plain browser): leave the field empty */ }
    if (placeholder !== (e.placeholder ?? "")) changed = true;
    next.push({ ...e, placeholder });
  }
  if (changed) setState({ entities: next });
}

// --- Patterns wiring ------------------------------------------------------

function wirePatterns(container) {
  const input = container.querySelector("#pattern-draft");
  const feedback = container.querySelector("#pattern-feedback");
  input?.addEventListener("input", async () => {
    drafts.pattern = input.value;
    const expr = input.value.trim();
    // Live compile validation (a cheap round-trip), so a broken expression is
    // visible before it is added rather than after.
    feedback.textContent = expr
      ? (await validatePattern(expr)) || WORKSPACE.patternCompiles
      : "";
  });

  const add = async () => {
    const expr = (drafts.pattern ?? "").trim();
    if (!expr) return;
    const error = await validatePattern(expr);
    // A pattern that does not compile IS added, carrying its error: the mock-up
    // keeps it visible and never uses it, which beats silently discarding what
    // the user typed. validPatterns() filters it out of every run.
    addPattern(expr, error || null);
    drafts.pattern = "";
    setState({});
  };
  container.querySelector("#btn-add-pattern")?.addEventListener("click", add);
  input?.addEventListener("keydown", (ev) => { if (ev.key === "Enter") add(); });

  for (const row of container.querySelectorAll(".pattern-row")) {
    row.querySelector(".pattern-del")?.addEventListener("click", () => {
      keepScrollPosition(() => removePattern(row.dataset.expr));
    });
  }
}

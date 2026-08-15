// views/identifyworkspace.js, the WORKSPACE of wizard step 2, Identify
//
// Four tabs, each with its item count in the tab itself:
//
//   Suggestions the review gate. Everything any detection method proposes
//                    waits here until the user accepts it: NOTHING reaches the
//                    value list without an explicit accept. Sortable by value
//                    and by count, filterable by type and by source, and each
//                    row's type is a dropdown so a mis-guessed suggestion is
//                    retyped before it is accepted.
//   My values the values that WILL be replaced, one card each. The card
//                    is the correction surface: the name and each spelling are
//                    editable in place, the type is a dropdown, "Group with"
//                    merges values that are the same real-world thing, and a
//                    value that would BLOCK the run (the same name under two
//                    types, a spelling two values both claim, a value that is
//                    also allowlisted) is tinted light red on the exact name or
//                    chip at fault, with "Solve conflicts" offering the fixes.
//                    Renaming what a value BECOMES (its placeholder) is still a
//                    step 3 action: there is nothing to rename until a run has
//                    assigned one.
//   Never anonymise the allowlist, which wins over every pass.
//   Patterns user regular expressions, with a valid / error badge.
//
// One structural change beyond the layout: "Run detection" is
// ONE button in the card header. It used to be a panel with a per-file checkbox
// list and a separate button per method.
//
//  made it ONE bridge call as well (api.js runDetection): Go runs every
// switched-on route under one cancellation context, reports one monotonic
// progress fraction across the whole run, and always ends with a terminal
// event. This module no longer sequences the passes, no longer computes a
// percentage, and no longer decides which routes run.
//
// Naming note: the visible labels
// have changed twice now, from "Entities" to "Values" to this half of
// "Identify". The ENGINE identifiers this module manipulates (the category keys
// entity_names, person_names, ... and the state.entities array) have not changed
// once, on purpose: a label is a display string, an identifier is a contract.

import {
  runDetection, cancelDetection, countTermMatches, patternMatches,
  expandVariants, validatePattern,
} from "../api.js";
import {
  getState, setState, llmEnabled, detectionRoutesOn,
  addEntities, removeEntity, entityKey,
  setEntityVariants, setEntityVariantError, addManualVariant,
  addCandidates, acceptCandidate, rejectCandidate,
  acceptAllShown, rejectAllShown, moveVariant,
  addPattern, removePattern, NAME_CATEGORIES,
  renameEntity, renameVariant, changeEntityCategory, changeCandidateCategory,
  groupEntities, clearAllEntities, entityConflicts, spellingsOf, removeAllowTerm,
} from "../state.js";
import { pendingExpansions } from "../entitymodel.js";
import {
  visibleCandidates, toggleCountSort, toggleValueSort, DEFAULT_CANDIDATE_FILTER,
} from "../candidatemodel.js";
import { escapeHTML } from "../html.js";
import { button, tabbar, icon, toastHTML } from "../ui.js";
import { askConfirm } from "../modal.js";
import { llmGateTooltip } from "./identifyrail.js";
import { renderAllowlistChips, wireAllowlistChips } from "./allowlist.js";
import { notify, wireNotice } from "../toast.js";
import { keepScrollPosition, keepScrollPositionIn } from "../scroll.js";
import { CARDS, WORKSPACE, VALUES, CATEGORY_LABELS } from "../copy.js";

// The categories a user may ADD a value to by hand, with their display labels.
// It is ALL of them, derived from the store rather than listed here: every
// category gates manually typed values as well as detected ones, so a category
// missing from this dropdown is a value the user cannot declare at all, even
// though the switch for it is right there in the rail.
//
// The PII categories are absent because they are patterns, not values: there is
// nothing to type into "email addresses" that a regex does not already find.
export const CATEGORIES = NAME_CATEGORIES.map((c) => [c, CATEGORY_LABELS[c][0]]);

/** categoryLabel(key) is a category's display label, falling back to the key. */
function categoryLabel(key) {
  return CATEGORY_LABELS[key]?.[0] ?? key;
}

/**
 * categorySelect(selected, opts) is the one dropdown used everywhere a value's
 * type is chosen or changed: the add row, the suggestion row (retype before
 * accepting) and the value card (change type). It lists exactly CATEGORIES, so
 * every place that assigns a type offers the same set.
 *
 * @param {string} selected the currently selected category key
 * @param {object} [opts] {id, cls, title, ariaLabel, data}
 * @returns {string} safe HTML
 */
function categorySelect(selected, opts = {}) {
  const attrs = [
    opts.id ? `id="${escapeHTML(opts.id)}"` : "",
    opts.cls ? `class="${escapeHTML(opts.cls)}"` : "",
    opts.title ? `title="${escapeHTML(opts.title)}"` : "",
    opts.ariaLabel ? `aria-label="${escapeHTML(opts.ariaLabel)}"` : "",
  ];
  for (const [k, v] of Object.entries(opts.data ?? {})) {
    attrs.push(`data-${escapeHTML(k)}="${escapeHTML(v)}"`);
  }
  return `<select ${attrs.filter(Boolean).join(" ")}>` +
    CATEGORIES.map(([key, label]) =>
      `<option value="${escapeHTML(key)}"${key === selected ? " selected" : ""}>` +
      `${escapeHTML(label)}</option>`).join("") +
    `</select>`;
}

// WORKSPACE_TABS is the tab set, in order.
export const WORKSPACE_TABS = ["suggestions", "values", "allow", "patterns"];

// --- View-local state -----------------------------------------------------
//
// None of this belongs in the store: nothing downstream reads it, it must not
// travel in a session file, and routing a sort click through a reducer would
// make a view preference part of the application's business state.

let activeTab = "suggestions";
let candidateFilter = { ...DEFAULT_CANDIDATE_FILTER, source: "" };
// The My values tab's own filters, kept out of the store for the same reason
// candidateFilter is: nothing downstream reads them and they must not travel in
// a session file. `showVariants` folds the spelling rows away so a long list is
// scannable; `type` narrows to one category; `search` matches a name OR any of
// its spellings.
let valuesFilter = { search: "", type: "", showVariants: true };
// Which value card has an inline panel open, and which panel. Only one is open
// at a time: a stack of open "group with" pickers down the list would be noise.
// key is an entityKey; kind is "group" | "solve" | null.
let openValuePanel = { key: null, kind: null };
// The draft text of the three add rows, kept across repaints so a state change
// elsewhere does not empty a half-typed value.
const drafts = {
  value: "", valueCategory: "entity_names", allow: "", pattern: "",
  // The live "found N times in M documents" read-out under the add row.
  valueMatches: "",
};
// Per-entity inline feedback (a refused placeholder, a duplicate variant),
// keyed by entityKey. Cleared for a row as soon as it succeeds at anything.
const rowFeedback = new Map();
// The variant chip currently being dragged, or null. It is held here rather than
// only in the DataTransfer because a dragover handler is not allowed to read
// DataTransfer contents, and the drop target has to know where the drag started
// to refuse a drop onto its own card.
let dragging = null;

// The value list scrolls inside the workspace card body, not the page, so an
// action that repaints (removing a spelling, renaming a value) has to restore
// THAT element's scroll or the list jumps to the first card. keepScrollPosition
// alone restores main#view, which is not what scrolls here.
const VALUES_SCROLLER = "#identify-workspace .card-body";
/** keepValuesScroll(mutate) preserves the value list's scroll across a repaint. */
function keepValuesScroll(mutate) {
  return keepScrollPositionIn(VALUES_SCROLLER, mutate);
}

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

  // The run button says what it will DO, which depends on which detection
  // ROUTES are switched on in the rail. With every route off there
  // is nothing to run, and the button says so rather than running an empty
  // pass and reporting "0 suggestions" as if it had looked.
  const aiOK = llmEnabled(s);
  const routes = detectionRoutesOn(s);
  const blocked = s.documents.length === 0 ? WORKSPACE.runNeedsDocuments
    : (routes === 0 ? WORKSPACE.runNeedsRoute : "");
  const runTitle = aiOK ? WORKSPACE.runWithAI : WORKSPACE.runOffline;
  const run = busy
    ? button(WORKSPACE.cancel, { kind: "secondary", id: "btn-detect-cancel", icon: "cancel" })
    : button(WORKSPACE.runDetection, {
      kind: "secondary", id: "btn-detect", icon: "smart_toy",
      disabled: !!blocked,
      title: blocked || runTitle,
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

/**
 * progressStrip(s) is the bar shown while a detection run is in flight.
 *
 * The percentage comes from GO: it covers the whole run across
 * every route, so it cannot rewind when the second route starts over with a
 * smaller file count. Recomputing it here from (current+1)/total per route is
 * exactly the bug that made the bar jump backwards mid-run.
 */
export function progressStrip(s) {
  const d = s.discovery;
  // The gate is deliberately `=== true` and nothing else: the
  // bar must depend on a run being in flight, never on a leftover object.
  if (d?.running !== true) return "";
  const pct = Math.max(0, Math.min(100, Math.round((d.fraction ?? 0) * 100)));
  return `<div class="detect-progress">` +
    `<div class="progress-bar"><div style="width:${pct}%"></div></div>` +
    `<span class="hint mono" id="detect-caption">${escapeHTML(detectionCaption(d))}</span>` +
    `</div>`;
}

/**
 * detectionCaption(d) says which route is running, on which file, how far
 * into it, and for how long.
 *
 * Every part of that answers a question the old one-line caption left open
 * when a run felt stuck: WHICH pass is this (two routes read the same files
 * twice), where inside a long file has it got to (a chunked AI scan sat on
 * one caption for minutes), and has anything happened at all recently.
 */
export function detectionCaption(d) {
  const route = d.phaseCount > 1
    ? `${WORKSPACE.phaseName(d.phase)} (${(d.phaseIndex ?? 0) + 1}/${d.phaseCount})`
    : WORKSPACE.phaseName(d.phase);
  const parts = [route];
  if (d.total) parts.push(WORKSPACE.fileOf(d.file ?? "", (d.current ?? 0) + 1, d.total));
  if (d.chunkCount > 1) parts.push(WORKSPACE.chunkOf((d.chunk ?? 0) + 1, d.chunkCount));
  if (d.startedAt) {
    const seconds = Math.max(0, Math.round((Date.now() - d.startedAt) / 1000));
    parts.push(WORKSPACE.elapsed(seconds));
  }
  return parts.join(", ");
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
export function suggestionsTab(s, shown) {
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
  // Sources render whatever is present. "Pattern" is deliberately
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
  const source = WORKSPACE.sourceLabels[c.source] ?? c.source;
  // The type is a dropdown, not a label: Smart detection and the local AI guess
  // which KIND of name a value is from its shape, and are often wrong about it.
  // Retyping here means the value lands in the right type the moment it is
  // accepted, rather than being accepted wrong and moved on the next tab.
  const type = categorySelect(c.category, {
    cls: "cell-type-select cand-type", title: WORKSPACE.retypeSuggestionTitle,
    ariaLabel: WORKSPACE.retypeSuggestionTitle, data: { text: c.text },
  });
  return `<div class="grid-row" style="grid-template-columns:${SUGGESTION_COLUMNS}"` +
    ` data-text="${escapeHTML(c.text)}">` +
    `<span class="cell-value" title="${escapeHTML(c.text)}">${escapeHTML(c.text)}</span>` +
    type +
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

/**
 * visibleValues(entities, filter) applies the My values tab's search and type
 * filter. A search matches a value's NAME or any of its spellings, so a user
 * who only remembers a variant still finds the card. Pure, and exported for the
 * render test.
 */
export function visibleValues(entities, filter) {
  const q = (filter.search ?? "").trim().toLowerCase();
  const type = filter.type ?? "";
  return (entities ?? []).filter((e) => {
    if (type && e.category !== type) return false;
    if (!q) return true;
    if (e.canonical.toLowerCase().includes(q)) return true;
    for (const [lower] of spellingsOf(e)) if (lower.includes(q)) return true;
    return false;
  });
}

/** conflictMessage(c) is the user-visible wording for one conflict, built from
 *  copy.js so no sentence lives in a view. */
function conflictMessage(c) {
  switch (c.kind) {
    case "ambiguity": return WORKSPACE.conflictAmbiguity(c.value, categoryLabel(c.withCategory));
    case "collision": return WORKSPACE.conflictCollision(c.spelling, c.withValue);
    case "allowlist": return WORKSPACE.conflictAllowlist(c.value);
    default: return "";
  }
}

/** valuesFilterBar(s) is the toolbar above the value cards: search, a type
 *  filter, the show/hide spellings toggle and Clear all. */
function valuesFilterBar(s) {
  const search =
    `<label class="search-box values-search">${icon("search")}` +
    `<input id="values-search" value="${escapeHTML(valuesFilter.search)}"` +
    ` placeholder="${escapeHTML(WORKSPACE.valuesSearchPlaceholder)}"` +
    ` aria-label="${escapeHTML(WORKSPACE.valuesSearchLabel)}"/></label>`;

  // The type filter lists only the types actually present, so it never offers a
  // category the current list cannot show.
  const typeOptions = distinct(s.entities, "category")
    .map((key) =>
      `<option value="${escapeHTML(key)}"${key === valuesFilter.type ? " selected" : ""}>` +
      `${escapeHTML(categoryLabel(key).toUpperCase())}</option>`).join("");
  const typeFilter =
    `<select id="values-type" class="head-select${valuesFilter.type ? " filtered" : ""}"` +
    ` title="${escapeHTML(WORKSPACE.valuesFilterTypeTitle)}"` +
    ` aria-label="${escapeHTML(WORKSPACE.valuesFilterTypeTitle)}">` +
    `<option value="">${escapeHTML(WORKSPACE.valuesAllTypes)}</option>${typeOptions}</select>`;

  const toggle = button(valuesFilter.showVariants ? WORKSPACE.hideVariants : WORKSPACE.showVariants, {
    kind: "secondary", id: "btn-toggle-variants",
    icon: valuesFilter.showVariants ? "expand_less" : "expand_more",
    title: valuesFilter.showVariants ? WORKSPACE.hideVariantsTitle : WORKSPACE.showVariantsTitle,
  });

  const clear = button(WORKSPACE.clearAll, {
    kind: "secondary", id: "btn-clear-values", icon: "delete",
    disabled: s.entities.length === 0, title: WORKSPACE.clearAllTitle,
  });

  return `<div class="values-toolbar">${search}${typeFilter}${toggle}${clear}</div>`;
}

/** valuesTab(s) is the filter toolbar, the add row, then one card per value,
 *  each highlighting the conflicts that would block the run. */
export function valuesTab(s) {
  const addRow =
    `<div class="add-row">` +
    `<input id="value-draft" class="grow" value="${escapeHTML(drafts.value)}"` +
    ` placeholder="${escapeHTML(WORKSPACE.addValuePlaceholder)}"` +
    ` aria-label="${escapeHTML(WORKSPACE.addValueLabel)}"/>` +
    categorySelect(drafts.valueCategory, {
      id: "value-category", ariaLabel: WORKSPACE.addValueCategory,
    }) +
    button(WORKSPACE.addValue, { kind: "secondary", id: "btn-add-value" }) +
    `</div>` +
    // The live match count. A value that matches nothing is almost always a
    // typo, and saying so before the run is the cheapest correction there is.
    `<p class="hint" id="value-matches">${escapeHTML(drafts.valueMatches)}</p>`;

  // Conflicts are computed ONCE for the whole list, because a collision is a
  // relationship BETWEEN two values: each card needs to know about the other.
  const conflicts = entityConflicts(s);
  const shown = visibleValues(s.entities, valuesFilter);
  const cards = s.entities.length === 0
    ? `<div class="grid-empty">${escapeHTML(WORKSPACE.noValues)}</div>`
    : (shown.length === 0
      ? `<div class="grid-empty">${escapeHTML(WORKSPACE.noValuesMatch)}</div>`
      : shown.map((e) => valueCard(e, conflicts.get(entityKey(e.category, e.canonical)), s)).join(""));

  return valuesFilterBar(s) + addRow + cards;
}

/**
 * valueCard(e, conflict, s) renders one value: its editable name, its type
 * dropdown, the group/solve/remove actions, and its variant chips.
 *
 * `conflict` is this value's entry from entityConflicts (or undefined when it is
 * clean). A conflict tints the card, and the exact name or chip at fault, so a
 * user can see BEFORE the Anonymise step which values would refuse the run.
 */
function valueCard(e, conflict, s) {
  const key = entityKey(e.category, e.canonical);
  const feedback = rowFeedback.get(key);
  const nameBad = !!(conflict && conflict.nameConflicts.length);
  const variantConflicts = conflict ? conflict.variantConflicts : new Map();

  // The name is click-to-edit: a button that reads as a heading until clicked,
  // then reveals an input (revealNameInput). Renaming what a value BECOMES is a
  // step 3 action; this renames the value ITSELF.
  const nameBtn =
    `<button type="button" class="value-name${nameBad ? " bad" : ""}"` +
    ` title="${escapeHTML(WORKSPACE.editValueTitle)}">${escapeHTML(e.canonical)}</button>`;

  const typeSelect = categorySelect(e.category, {
    cls: "value-type", title: WORKSPACE.changeTypeLabel, ariaLabel: WORKSPACE.changeTypeLabel,
  });

  const actions =
    button(WORKSPACE.groupWith, {
      kind: "ghost", cls: "value-group", icon: "content_copy", title: WORKSPACE.groupWithTitle,
    }) +
    (conflict ? button(WORKSPACE.solveConflicts, {
      kind: "ghost", cls: "value-solve", icon: "warning", title: WORKSPACE.solveConflictsTitle,
    }) : "") +
    button("", {
      kind: "ghost", cls: "value-remove icon-action danger", icon: "close",
      ariaLabel: `Remove ${e.canonical}`, title: WORKSPACE.removeValue,
    });

  // The chips are DRAGGABLE onto another value card, which is how a variant is
  // regrouped when the expansion attached it to the wrong value. The text is a
  // separate span so a double-click edits only the spelling, not the remove
  // button beside it.
  const variants = [...(e.variants ?? []), ...(e.manualVariants ?? [])];
  const chips = variants.map((v) => {
    const bad = variantConflicts.has(v.trim().toLowerCase());
    return `<span class="chip-tag variant-chip${bad ? " bad" : ""}" draggable="true" data-variant="${escapeHTML(v)}"` +
      ` title="${escapeHTML(WORKSPACE.variantDragHint)}">` +
      `<span class="variant-text" title="${escapeHTML(WORKSPACE.editVariantTitle)}">${escapeHTML(v)}</span>` +
      button("", {
        kind: "ghost", cls: "chip-remove variant-del", icon: "close",
        ariaLabel: `Remove variant ${v}`, title: WORKSPACE.removeVariant,
        data: { variant: v },
      }) +
      `</span>`;
  }).join("");

  // "pending" is a real state, not an absence: null variants mean an expansion
  // is in flight, [] means it finished and found none.
  const variantNote = e.variantError
    ? `<span class="hint bad">${escapeHTML(e.variantError)}</span>`
    : (e.variants === null || e.variants === undefined)
      ? `<span class="hint">${escapeHTML(WORKSPACE.variantsPending)}</span>`
      : (variants.length === 0 ? `<span class="hint">${escapeHTML(WORKSPACE.noVariants)}</span>` : "");

  const variantRow = valuesFilter.showVariants
    ? `<div class="chip-row variant-row">` +
      `<span class="hint">${escapeHTML(WORKSPACE.variants)}</span>${chips}` +
      button(WORKSPACE.addVariant, { kind: "ghost", cls: "chip-add variant-add", icon: "add" }) +
      `</div>` +
      (variantNote ? `<div class="value-note">${variantNote}</div>` : "")
    : "";

  const conflictNote = conflict
    ? `<div class="value-note conflict-note">` +
      [...new Set(conflict.list.map(conflictMessage))]
        .map((m) => `<span class="hint bad">${icon("warning")}${escapeHTML(m)}</span>`).join("") +
      `</div>`
    : "";

  const panel = openValuePanel.key === key
    ? (openValuePanel.kind === "group" ? groupPanel(e, s)
      : (openValuePanel.kind === "solve" && conflict ? solvePanel(e, conflict) : ""))
    : "";

  return `<div class="value-card${conflict ? " conflicted" : ""}" data-key="${escapeHTML(key)}"` +
    ` data-category="${escapeHTML(e.category)}" data-canonical="${escapeHTML(e.canonical)}">` +
    `<div class="row-between">` +
    `<div class="value-head">${nameBtn}${typeSelect}</div>` +
    `<div class="value-actions">${actions}</div>` +
    `</div>` +
    conflictNote +
    variantRow +
    (feedback ? `<div class="value-note"><span class="hint bad">${escapeHTML(feedback)}</span></div>` : "") +
    panel +
    `</div>`;
}

/** groupPanel(e, s) is the inline picker for "Group with": the other values,
 *  each a checkbox, folded into this one on Apply. */
function groupPanel(e, s) {
  const selfKey = entityKey(e.category, e.canonical);
  const others = s.entities.filter((o) => entityKey(o.category, o.canonical) !== selfKey);
  if (others.length === 0) {
    return `<div class="value-panel"><p class="hint">${escapeHTML(WORKSPACE.groupNone)}</p>` +
      `<div class="panel-actions">` +
      button(WORKSPACE.groupCancel, { kind: "ghost", cls: "panel-cancel" }) +
      `</div></div>`;
  }
  const rows = others.map((o) =>
    `<label class="group-option">` +
    `<input type="checkbox" class="group-pick"` +
    ` data-category="${escapeHTML(o.category)}" data-canonical="${escapeHTML(o.canonical)}"/>` +
    `<span class="group-option-name">${escapeHTML(o.canonical)}</span>` +
    `<span class="fmt-badge">${escapeHTML(categoryLabel(o.category))}</span>` +
    `</label>`).join("");
  return `<div class="value-panel group-panel">` +
    `<p class="section-label">${escapeHTML(WORKSPACE.groupWithHeading)}</p>` +
    `<p class="hint">${escapeHTML(WORKSPACE.groupWithHint)}</p>` +
    `<div class="group-options">${rows}</div>` +
    `<div class="panel-actions">` +
    button(WORKSPACE.groupApply, { kind: "secondary", cls: "group-apply" }) +
    button(WORKSPACE.groupCancel, { kind: "ghost", cls: "panel-cancel" }) +
    `</div></div>`;
}

/** solvePanel(e, conflict) lists each conflict on the card with the concrete
 *  actions that resolve it. */
function solvePanel(e, conflict) {
  const items = conflict.list.map((c) => {
    let acts = "";
    if (c.kind === "collision") {
      acts =
        button(WORKSPACE.solveDropVariant, {
          kind: "ghost", cls: "solve-action", data: { act: "drop-variant", spelling: c.spelling },
        }) +
        button(WORKSPACE.solveGroupOtherLabel(c.withValue), {
          kind: "ghost", cls: "solve-action",
          data: { act: "merge", withcategory: c.withCategory, withvalue: c.withValue },
        });
    } else if (c.kind === "ambiguity") {
      acts = button(WORKSPACE.solveRemoveThis, {
        kind: "ghost", cls: "solve-action", data: { act: "remove-value" },
      });
    } else if (c.kind === "allowlist") {
      acts =
        button(WORKSPACE.solveRemoveFromAllowlist, {
          kind: "ghost", cls: "solve-action", data: { act: "remove-allow" },
        }) +
        button(WORKSPACE.solveRemoveThis, {
          kind: "ghost", cls: "solve-action", data: { act: "remove-value" },
        });
    }
    return `<div class="solve-item">` +
      `<p class="hint bad">${escapeHTML(conflictMessage(c))}</p>` +
      `<div class="panel-actions">${acts}</div></div>`;
  }).join("");
  return `<div class="value-panel solve-panel">` +
    `<p class="section-label">${escapeHTML(WORKSPACE.solveHeading)}</p>${items}` +
    `<div class="panel-actions">` +
    button(WORKSPACE.solveClose, { kind: "ghost", cls: "panel-cancel" }) +
    `</div></div>`;
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

  // The worked examples: eight clickable starter expressions. Each is a button
  // so it is reachable by keyboard and announces itself; clicking it fills the
  // add box above (wirePatterns), where the live feedback then explains what it
  // matches. This is the whole "you do not have to be a regex expert" feature.
  const examples = WORKSPACE.patternExamples.map((ex) =>
    `<button type="button" class="pattern-example" data-expr="${escapeHTML(ex.expr)}"` +
    ` aria-label="${escapeHTML(WORKSPACE.useExample(ex.expr))}">` +
    `<span class="mono">${escapeHTML(ex.expr)}</span>` +
    `<span class="pattern-example-label">${escapeHTML(ex.label)}</span>` +
    `<span class="pattern-example-sample mono" title="${escapeHTML(WORKSPACE.matchesSample(ex.sample))}">` +
    `${escapeHTML(WORKSPACE.matchesSample(ex.sample))}</span>` +
    `</button>`).join("");
  const examplesBlock =
    `<div class="pattern-examples">` +
    `<p class="section-label">${escapeHTML(WORKSPACE.patternExamplesLabel)}</p>` +
    `<p class="hint">${escapeHTML(WORKSPACE.patternExamplesHint)}</p>` +
    `<div class="pattern-example-list">${examples}</div>` +
    `</div>`;

  return `<p class="hint">${escapeHTML(WORKSPACE.patternsHint)}</p>` + addRow +
    `<div id="pattern-feedback" class="hint"></div>` +
    (rows ? `<div class="grid-box">${rows}</div>` : "") +
    examplesBlock;
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

  wireDetection(container);
  wireNotice(container);

  if (activeTab === "suggestions") wireSuggestions(container, shown);
  else if (activeTab === "values") wireValues(container);
  else if (activeTab === "allow") wireAllowlistChips(container, drafts);
  else if (activeTab === "patterns") wirePatterns(container);
}

// --- Run detection --------------------------------------------------------

/**
 * wireDetection(container) wires the ONE Run detection button to the ONE
 * detection call.
 *
 * Everything this function used to decide now belongs to Go: which routes run,
 * which files the local AI can read, what happens when one file fails, and
 * when the run is over. What is left here is what a view should do: start it,
 * fold the findings into the store, and report what came back, INCLUDING the
 * cancelled flag and the per-file problems the old code discarded.
 */
function wireDetection(container) {
  container.querySelector("#btn-detect-cancel")?.addEventListener("click", () => cancelDetection());

  const btn = container.querySelector("#btn-detect");
  if (!btn || btn.disabled) return;

  btn.addEventListener("click", async () => {
    const all = getState().documents.map((d) => d.name);
    if (all.length === 0) return;

    // ONE call for the whole run. Go decides which routes are on,
    // skips what the local AI cannot read and says so, keeps going past a
    // file that failed, and always ends the run with a terminal event that
    // clears the progress bar. The old two-call sequence could not do any of
    // that: it had two cancellation slots with a dead zone between them, and
    // it dropped the cancelled flag and the status both passes returned.
    setState({
      discovery: {
        running: true, phase: "", phaseIndex: 0, phaseCount: 1,
        current: 0, total: all.length, file: all[0],
        chunk: 0, chunkCount: 0, fraction: 0, startedAt: Date.now(),
      },
    });

    try {
      const result = await runDetection(all, getState().allowlist);
      const added =
        addCandidates(result?.candidates ?? [], "smart") +
        addCandidates(
          (result?.proposals ?? []).map((p) => ({ text: p.text, category: p.category })), "local-ai");

      // A file the AI could not read is reported, not silently dropped.
      for (const skip of result?.skipped ?? []) {
        notify(WORKSPACE.skippedNotice(skip.name, skip.reason), "warn");
      }
      // So is a route that failed on one file while the others succeeded.
      for (const message of result?.errors ?? []) {
        notify(message, "warn");
      }
      if (result?.cancelled) {
        notify(WORKSPACE.detectionCancelled(added), "info");
      } else {
        notify(WORKSPACE.detectionDone(added), added ? "ok" : "info");
      }
    } catch (err) {
      notify(String(err?.message ?? err), "warn");
    } finally {
      // Belt and braces: the terminal event already clears this (main.js), so
      // a lost event cannot strand the bar, and a lost promise cannot either.
      setState({ discovery: null });
      // Land the user on the fresh candidate list, which is what they ran for.
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
    // Retyping a suggestion before it is accepted. keepScrollPosition holds the
    // list in place so retyping a row two thirds down does not jump the view.
    row.querySelector(".cand-type")?.addEventListener("change", (ev) => {
      keepScrollPosition(() => changeCandidateCategory(text, ev.target.value));
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
  const matches = container.querySelector("#value-matches");
  // Debounced: one bridge round-trip per keystroke over a batch of documents
  // would be a count nobody waits for.
  let matchTimer = null;
  draft?.addEventListener("input", () => {
    drafts.value = draft.value;
    if (matchTimer) clearTimeout(matchTimer);
    const term = draft.value.trim();
    if (!term) {
      drafts.valueMatches = "";
      if (matches) matches.textContent = "";
      return;
    }
    matchTimer = setTimeout(async () => {
      try {
        const info = await countTermMatches(term);
        // The field may have moved on while the count was in flight; a stale
        // answer under a different word is worse than no answer.
        if (draft.value.trim() !== term) return;
        drafts.valueMatches = WORKSPACE.valueMatches(info?.count ?? 0, info?.documents ?? 0);
      } catch {
        drafts.valueMatches = ""; // no bridge: say nothing rather than guess
      }
      if (matches) matches.textContent = drafts.valueMatches;
    }, 250);
  });
  category?.addEventListener("change", () => { drafts.valueCategory = category.value; });

  const add = async () => {
    const value = (drafts.value ?? "").trim();
    if (!value) return;
    const n = addEntities([{ category: drafts.valueCategory, canonical: value }]);
    drafts.value = "";
    drafts.valueMatches = "";
    if (n === 0) notify(WORKSPACE.valueAlreadyThere(value), "info");
    setState({});
    await refreshVariants();
  };
  container.querySelector("#btn-add-value")?.addEventListener("click", add);
  draft?.addEventListener("keydown", (ev) => { if (ev.key === "Enter") add(); });

  wireValuesToolbar(container);

  for (const cardEl of container.querySelectorAll(".value-card")) {
    const { category: cat, canonical, key } = cardEl.dataset;

    // Renaming the value: click the name to reveal an inline input.
    cardEl.querySelector(".value-name")?.addEventListener("click", () => {
      revealNameInput(cardEl, cat, canonical, key);
    });

    // Changing the type re-expands the row (a person and an organisation expand
    // differently). The change AND the re-expansion are one keepValuesScroll so
    // the list stays put across both repaints.
    cardEl.querySelector(".value-type")?.addEventListener("change", (ev) => {
      keepValuesScroll(async () => {
        const reason = changeEntityCategory(cat, canonical, ev.target.value);
        if (reason === "duplicate") notify(WORKSPACE.typeChangeDuplicate(canonical), "warn");
        else await refreshVariants();
      });
    });

    cardEl.querySelector(".value-group")?.addEventListener("click", () => {
      togglePanel(key, "group");
    });
    cardEl.querySelector(".value-solve")?.addEventListener("click", () => {
      togglePanel(key, "solve");
    });

    cardEl.querySelector(".value-remove")?.addEventListener("click", () => {
      rowFeedback.delete(key);
      if (openValuePanel.key === key) openValuePanel = { key: null, kind: null };
      keepValuesScroll(() => removeEntity(cat, canonical));
    });

    // The add-variant control reveals an INLINE input rather than opening a
    // dialog: native dialogs are banned, and an inline field is
    // fewer steps than a dialog anyway.
    cardEl.querySelector(".variant-add")?.addEventListener("click", () => {
      revealVariantInput(cardEl, cat, canonical, key);
    });

    // Editing a spelling: double-click its text (a single click would fight the
    // drag handle the chip is).
    for (const text of cardEl.querySelectorAll(".variant-text")) {
      text.addEventListener("dblclick", () => {
        revealVariantEditInput(text, cat, canonical, key);
      });
    }

    for (const del of cardEl.querySelectorAll(".variant-del")) {
      del.addEventListener("click", (ev) => {
        // The chip itself is a drag handle, so the remove button has to stop the
        // click reaching it.
        ev.stopPropagation();
        // Removing a variant is an EXCLUSION, not a deletion: automatic expansion
        // would put it straight back otherwise. moveVariant is for regrouping;
        // here the variant simply stops applying. The exclusion AND the
        // re-expansion it triggers are wrapped in one keepValuesScroll, so the
        // value list stays put across both repaints instead of jumping to the
        // top when the async re-expansion lands.
        keepValuesScroll(async () => {
          excludeVariant(cat, canonical, del.dataset.variant);
          await refreshVariants();
        });
      });
    }

    wireGroupPanel(cardEl, cat, canonical);
    wireSolvePanel(cardEl, cat, canonical);
  }

  wireVariantDrag(container);
}

/** togglePanel(key, kind) opens the group/solve panel on one card, or closes it
 *  if it was already the open one, then repaints. */
function togglePanel(key, kind) {
  openValuePanel = (openValuePanel.key === key && openValuePanel.kind === kind)
    ? { key: null, kind: null }
    : { key, kind };
  keepValuesScroll(() => setState({}));
}

/** wireValuesToolbar(container) wires the search, type filter, show/hide
 *  spellings toggle and Clear all. */
function wireValuesToolbar(container) {
  const search = container.querySelector("#values-search");
  search?.addEventListener("input", () => {
    const caret = search.selectionStart;
    valuesFilter = { ...valuesFilter, search: search.value };
    setState({});
    const again = container.querySelector("#values-search");
    if (again) {
      again.focus();
      again.setSelectionRange(caret, caret);
    }
  });

  container.querySelector("#values-type")?.addEventListener("change", (ev) => {
    valuesFilter = { ...valuesFilter, type: ev.target.value };
    setState({});
  });

  container.querySelector("#btn-toggle-variants")?.addEventListener("click", () => {
    valuesFilter = { ...valuesFilter, showVariants: !valuesFilter.showVariants };
    keepValuesScroll(() => setState({}));
  });

  container.querySelector("#btn-clear-values")?.addEventListener("click", async () => {
    const n = getState().entities.length;
    if (n === 0) return;
    if (!await askConfirm({ title: WORKSPACE.clearAll, body: WORKSPACE.clearAllConfirm(n) })) return;
    openValuePanel = { key: null, kind: null };
    const cleared = clearAllEntities();
    rowFeedback.clear();
    notify(WORKSPACE.clearedN(cleared), cleared ? "ok" : "info");
  });
}

/** wireGroupPanel(cardEl, cat, canonical) wires the Group with picker's Apply
 *  and Cancel. */
function wireGroupPanel(cardEl, cat, canonical) {
  cardEl.querySelector(".group-apply")?.addEventListener("click", () => {
    const sources = [...cardEl.querySelectorAll(".group-pick:checked")].map((cb) => ({
      category: cb.dataset.category, canonical: cb.dataset.canonical,
    }));
    if (sources.length === 0) return;
    openValuePanel = { key: null, kind: null };
    keepValuesScroll(async () => {
      const n = groupEntities({ category: cat, canonical }, sources);
      if (n) notify(WORKSPACE.groupedN(n, canonical), "ok");
      await refreshVariants();
    });
  });
  cardEl.querySelector(".panel-cancel")?.addEventListener("click", () => {
    openValuePanel = { key: null, kind: null };
    keepValuesScroll(() => setState({}));
  });
}

/** wireSolvePanel(cardEl, cat, canonical) wires each resolve action inside the
 *  Solve conflicts panel. Each action, plus any re-expansion it triggers, is one
 *  keepValuesScroll so the list does not jump. */
function wireSolvePanel(cardEl, cat, canonical) {
  for (const action of cardEl.querySelectorAll(".solve-action")) {
    action.addEventListener("click", () => {
      const { act } = action.dataset;
      openValuePanel = { key: null, kind: null };
      if (act === "remove-value") {
        keepValuesScroll(() => removeEntity(cat, canonical));
      } else if (act === "remove-allow") {
        keepValuesScroll(() => removeAllowTerm(canonical));
      } else if (act === "drop-variant") {
        keepValuesScroll(async () => {
          excludeVariant(cat, canonical, action.dataset.spelling);
          await refreshVariants();
        });
      } else if (act === "merge") {
        keepValuesScroll(async () => {
          groupEntities(
            { category: cat, canonical },
            [{ category: action.dataset.withcategory, canonical: action.dataset.withvalue }]);
          await refreshVariants();
        });
      }
    });
  }
}

/**
 * wireVariantDrag(container) makes a variant chip draggable onto another value
 * card, which regroups it.
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
 * an inline text input, because bans prompt() and an inline field is
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
 * revealNameInput(cardEl, category, canonical, key) swaps the value name for an
 * inline input, so a mis-detected value can be corrected in place. It commits
 * through renameEntity, which re-expands the row.
 *
 * Like the variant input, it is transient DOM: no state change reveals it, and
 * a repaint puts the read-only name back.
 */
function revealNameInput(cardEl, category, canonical, key) {
  const nameEl = cardEl.querySelector(".value-name");
  if (!nameEl) return;

  const input = cardEl.ownerDocument.createElement("input");
  input.className = "value-name-input";
  input.value = canonical;
  input.placeholder = WORKSPACE.editValuePlaceholder;
  input.setAttribute("aria-label", WORKSPACE.editValueTitle);
  nameEl.replaceWith(input);
  input.focus();
  input.select();

  let done = false;
  const commit = async () => {
    if (done) return;
    done = true;
    const value = input.value.trim();
    if (!value || value === canonical) {
      setState({}); // repaint puts the name back unchanged
      return;
    }
    await keepValuesScroll(async () => {
      const reason = renameEntity(category, canonical, value);
      if (reason === "duplicate") {
        rowFeedback.set(key, WORKSPACE.valueRenamedDuplicate(value));
        setState({});
        return;
      }
      rowFeedback.delete(key);
      await refreshVariants();
    });
  };

  input.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") commit();
    else if (ev.key === "Escape") { done = true; setState({}); }
  });
  input.addEventListener("blur", commit);
}

/**
 * revealVariantEditInput(textEl, category, canonical, key) swaps one spelling's
 * text for an inline input. It commits through renameVariant, which excludes the
 * old spelling and adds the new one, then re-expands.
 */
function revealVariantEditInput(textEl, category, canonical, key) {
  const old = textEl.textContent;

  const input = textEl.ownerDocument.createElement("input");
  input.className = "variant-input";
  input.value = old;
  input.placeholder = WORKSPACE.editVariantPlaceholder;
  input.setAttribute("aria-label", WORKSPACE.editVariantTitle);
  // Stop the chip's drag machinery from reacting while the field is focused.
  input.setAttribute("draggable", "false");
  textEl.replaceWith(input);
  input.focus();
  input.select();

  let done = false;
  const commit = async () => {
    if (done) return;
    done = true;
    const value = input.value.trim();
    if (!value || value === old) {
      setState({}); // repaint puts the chip back unchanged
      return;
    }
    rowFeedback.delete(key);
    await keepValuesScroll(async () => {
      renameVariant(category, canonical, old, value);
      await refreshVariants();
    });
  };

  input.addEventListener("keydown", (ev) => {
    ev.stopPropagation();
    if (ev.key === "Enter") commit();
    else if (ev.key === "Escape") { done = true; setState({}); }
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
 *
 * It does NOT trigger the re-expansion itself: the caller wraps this and the
 * refreshVariants that follows in ONE keepValuesScroll, so the value list's
 * scroll survives BOTH repaints (the exclusion and the re-expansion). Calling
 * refreshVariants here, outside that wrapper, is exactly what let the async
 * re-expansion repaint jump the list back to the top.
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
}

/** variantCount(category, canonical) is how many spellings a value carries right
 *  now, automatic and manual together. The add flow compares it before and after
 *  so a duplicate gets an explanation instead of silence. */
function variantCount(category, canonical) {
  const e = getState().entities.find(
    (x) => entityKey(x.category, x.canonical) === entityKey(category, canonical));
  if (!e) return 0;
  return (e.variants?.length ?? 0) + (e.manualVariants?.length ?? 0);
}

/**
 * refreshVariants() asks Go to expand every value whose variants are PENDING
 * (variants === null: just added, edited, or variant-amended). Settled rows,
 * including "expanded, none found" ([]), are never re-expanded.
 *
 * Sequential on purpose: the lists are tiny and ordering keeps the UI
 * deterministic.
 */
async function refreshVariants() {
  // The snapshot is taken ONCE, before any await. Every expansion below writes
  // to the store and therefore repaints, and re-reading the list mid-loop would
  // mean expanding rows a repaint had already settled.
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
      // repaint.
      setEntityVariantError(e.category, e.canonical, String(err?.message ?? err));
    }
  }
  return pending.length;
}

// --- Patterns wiring ------------------------------------------------------

function wirePatterns(container) {
  const input = container.querySelector("#pattern-draft");
  const feedback = container.querySelector("#pattern-feedback");
  input?.addEventListener("input", async () => {
    drafts.pattern = input.value;
    const expr = input.value.trim();
    if (!expr) {
      feedback.textContent = "";
      return;
    }
    // Live compile validation (a cheap round-trip), so a broken expression is
    // visible before it is added rather than after.
    const error = await validatePattern(expr);
    if (error) {
      feedback.textContent = error;
      return;
    }
    // It compiles: now say what it actually MATCHES. The sample
    // matches existed behind a bridge method nothing called, which left
    // "this expression compiles" as the only feedback a regex ever got, and
    // a regex that compiles and matches nothing is the common mistake.
    try {
      const samples = await patternMatches(expr);
      if (input.value.trim() !== expr) return; // the field moved on
      feedback.textContent = samples?.length
        ? WORKSPACE.patternSamples(samples)
        : WORKSPACE.patternNoMatches;
    } catch {
      feedback.textContent = WORKSPACE.patternCompiles;
    }
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

  // Clicking an example drops it into the add box and immediately runs the same
  // live feedback the user would get by typing it, so they see what it matches
  // before deciding to add it. It deliberately does NOT add the pattern: the
  // examples are a starting point to tweak, not a one-click commit.
  for (const ex of container.querySelectorAll(".pattern-example")) {
    ex.addEventListener("click", () => {
      drafts.pattern = ex.dataset.expr;
      setState({}); // repaint so the input shows the chosen expression
      const draft = container.querySelector("#pattern-draft");
      if (draft) {
        draft.focus();
        // Re-run the input listener wired above, which validates and reports
        // the sample matches for the newly-filled expression.
        draft.dispatchEvent(new Event("input"));
      }
    });
  }
}

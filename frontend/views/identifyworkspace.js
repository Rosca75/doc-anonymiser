// views/identifyworkspace.js, the WORKSPACE of wizard step 2, Identify
//
// Five tabs, each with its item count in the tab itself:
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
//   Built-in patterns READ-ONLY: what the application's own patterns matched
//                    the last time detection ran, grouped by signal category,
//                    including the categories that ran and matched nothing.
//                    There is nothing to accept here, because a built-in
//                    pattern produces DIRECT matches rather than Suggestions;
//                    the tab exists so the one decision the user does make
//                    about them, which categories are on, is checkable before
//                    the whole batch is anonymised.
//   Custom patterns the user's own regular expressions, with a valid / error
//                    badge. Named for its author, which is the only difference
//                    between it and the tab above.
//
// The RUN is not here. The Run detection button, the progress bar and the
// bridge call live in views/detectionrun.js, and the button itself is rendered
// in the Configure rail's head: this panel is not on screen at all until a run
// has happened, so the control that starts one cannot sit inside it. All this
// module exports towards the run is landOnResultTab(), because the tab state is
// its own.
//
// Naming note: the ENGINE identifiers this module manipulates (the category keys
// entity_names, person_names, ... and the state.values array) have not changed
// once, on purpose: a label is a display string, an identifier is a contract.

import {
  countTermMatches, patternMatches,
  expandSpellings, validatePattern, checkIntersections,
} from "../api.js";
import {
  getState, setState,
  addValues, deleteValue, deleteValues, valueKey,
  setValueSpellings, setValueSpellingError, addSpelling,
  acceptSuggestion, rejectSuggestion,
  acceptAllShown, rejectAllShown, moveSpelling,
  addPattern, removePattern, NAME_CATEGORIES,
  renameValue, renameSpelling, changeValueCategory, changeSuggestionCategory,
  groupValues, clearAllValues, valueConflicts, spellingsOf, removeAllowTerm, addAllowTerm,
  curate, setIntersections, intersectionsFor, buildIntersectionRequest,
  foldIntoFamily, DISCOVERY_METHODS, relatedTo,
} from "../state.js";
import { pendingExpansions } from "../valuemodel.js";
import {
  visibleSuggestions, toggleCountSort, toggleValueSort, DEFAULT_SUGGESTION_FILTER,
} from "../suggestionmodel.js";
import { escapeHTML } from "../html.js";
import {
  button, tabbar, icon, toastHTML, searchBox, wireSearchBox, sectionLabel,
  helpTooltip, wireHelpTooltips, warningPopover, wireWarningPopovers,
} from "../ui.js";
import { askConfirm, askChoice } from "../modal.js";
import { llmGateTooltip } from "./identifyrail.js";
import { renderAllowlistChips, wireAllowlistChips } from "./allowlist.js";
import { notify, wireNotice } from "../toast.js";
import { CARDS, WORKSPACE, VALUES, CATEGORY_LABELS } from "../copy.js";

// The categories a user may ADD a value to by hand, with their display labels.
// It is ALL of them, derived from the store rather than listed here: every
// category gates manually typed values as well as detected ones, so a category
// missing from this dropdown is a value the user cannot declare at all, even
// though the switch for it is right there in the rail.
//
// The PII categories are absent because they are patterns, not values: there is
// nothing to type into "email addresses" that a regex does not already find.
const CATEGORIES = NAME_CATEGORIES.map((c) => [c, CATEGORY_LABELS[c][0]]);

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
export function categorySelect(selected, opts = {}) {
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

// WORKSPACE_TABS is the tab set, in order. The two pattern tabs sit together at
// the end and are split by AUTHOR: "builtin" is read-only and shows what the
// application's own patterns matched, "patterns" is where the user writes their
// own. Built-in comes first because it is the one that is already running.
export const WORKSPACE_TABS = ["suggestions", "values", "allow", "builtin", "patterns"];

// --- View-local state -----------------------------------------------------
//
// None of this belongs in the store: nothing downstream reads it, it must not
// travel in a session file, and routing a sort click through a reducer would
// make a view preference part of the application's business state.

let activeTab = "suggestions";
let suggestionFilter = { ...DEFAULT_SUGGESTION_FILTER };
// The My values tab's own filters, kept out of the store for the same reason
// suggestionFilter is: nothing downstream reads them and they must not travel in
// a session file. `type` narrows to one category; `search` matches a name OR any
// of its spellings.
//
// There is no show/hide-spellings switch. The card shows a bounded ONE-LINE
// preview and the popup owns the full list, so a toggle would change every card's
// height for no information the popup does not already give.
let valuesFilter = { search: "", type: "" };
// Which value card has an inline panel open, and which panel. Only one is open
// at a time: a stack of open "group with" pickers down the list would be noise.
// key is an valueKey; kind is "group" | "solve" | null.
let openValuePanel = { key: null, kind: null };
// The value cards the user picked with Ctrl+click, as a Set of valueKeys. It is
// view state for the same reason the filters are: nothing downstream reads it and
// it must never travel in a session file. It exists so a bulk action can act on
// SOME of the list instead of all of it, which is why the clear button reads the
// set rather than the value count.
//
// Keys, not indexes or object references: the cards are re-rendered from state on
// every repaint, so an index moves when a value is added above it and a reference
// points at an object the next repaint replaced. A key survives both. It does not
// survive a RENAME, which changes the key, and dropping the selection there is
// the honest answer: the card the user picked is not the card that is now shown.
let selectedValueKeys = new Set();
// The draft text of the add rows, kept across repaints so a state change
// elsewhere does not empty a half-typed value.
const drafts = {
  value: "", valueCategory: "entity_names", allow: "", pattern: "",
  // The live "found N times in M documents" read-out under the add row.
  valueMatches: "",
  // The spellings popup's own add field.
  spelling: "",
};

// The open spellings popup: which Value it belongs to, or null when none is
// open. It is view state for the same reason the open card panel is, and it
// carries `focusAdd` because "+ add" and "+N more" open the SAME surface and
// only differ in where the caret lands.
let spellingsPopup = null;
// The popup's search text. It filters the listed rows in place, exactly as the
// values search filters the cards, so the input node is never replaced and the
// caret survives.
let spellingsPopupSearch = "";
// The spelling chip currently being dragged, or null. It is held here rather than
// only in the DataTransfer because a dragover handler is not allowed to read
// DataTransfer contents, and the drop target has to know where the drag started
// to refuse a drop onto its own card.
let dragging = null;

// The value list scrolls inside the workspace card body, not the page. An
// action that repaints (removing a spelling, renaming a value) keeps its place
// because scroll offsets are preserved centrally by the shell repaint
// (scroll.js), so nothing here has to carry them.

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
  const shown = visibleSuggestions(s.suggestions, suggestionFilter);

  container.innerHTML =
    head() +
    `<div class="workspace-tabs">${tabs(s)}</div>` +
    `<div class="card-body stack">${tabBody(s, shown)}</div>` +
    toastHTML(s.notice) +
    (opts.footerHTML ?? "") +
    // Outside the scrolling body on purpose: the popup is a full-viewport
    // overlay, and an overlay rendered inside the element it covers inherits
    // that element's scroll offset and its clipping.
    spellingsPopupHTML(s);

  wire(container, s, shown);

  // Ask Go which values another route also claims, whenever the answer could
  // have changed. Reading the CONFIGURATION off each repaint is what makes this
  // one place instead of a call at every reducer that touches the value list: a
  // reducer added later cannot forget it, and the debounce absorbs the burst of
  // repaints one edit produces.
  const signature = intersectionSignature(s);
  if (signature !== lastIntersectionSignature) {
    lastIntersectionSignature = signature;
    scheduleIntersectionCheck();
  }
}

// The configuration the last intersection check was asked about. Compared as a
// string so a repaint that changed nothing relevant (a filter, a panel opening)
// does not re-ask.
let lastIntersectionSignature = "";

/**
 * intersectionSignature(s) is everything an intersection depends on: the
 * values, the patterns and the detection scope. Deliberately NOT the whole
 * state, so opening a panel or typing in the search box does not send Go over
 * every document again.
 */
function intersectionSignature(s) {
  const values = s.values
    .map((e) => `${e.category}|${e.mainText}|${(e.discoveryMethods ?? []).join(",")}`)
    .join("\n");
  const patterns = (s.patterns ?? []).map((p) => p.expr).join("\n");
  const categories = Object.entries(s.settings?.categories ?? {})
    .filter(([, on]) => on).map(([c]) => c).sort().join(",");
  // JSON rather than a joined string: a value or a pattern may contain any
  // separator character, and a collision here reads as "nothing changed".
  return JSON.stringify([
    values, patterns, categories, s.allowlist, s.settings?.useBuiltInPatterns === true,
  ]);
}

/** head(s) is the card header: the panel's title, and nothing else.
 *
 *  The Run detection button is NOT here. It lives in the Configure rail's head
 *  (views/detectionrun.js runControlHTML), because this panel is not on screen
 *  until a run has happened: a button that reveals the card it sits in cannot
 *  sit in that card. */
function head() {
  return `<div class="card-head">` +
    `<div class="card-head-left"><h2>${escapeHTML(CARDS.identify.title)}</h2></div>` +
    `</div>`;
}

function tabs(s) {
  const counts = {
    suggestions: s.suggestions.length,
    values: s.values.length,
    allow: s.allowlist.length,
    // Null rather than 0 before the first run: a "0" badge states that the
    // patterns found nothing, which is not what "detection has not run" means.
    builtin: s.builtInPatterns ? s.builtInPatterns.matches.length : null,
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
    case "builtin": return builtInPatternsTab(s);
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

  // Relatedness is computed against every SHOWN row, not the whole store: the
  // note points at rows the user can see and act on, not at ones a filter hides.
  const rows = shown.map((row) => suggestionRow(row, shown)).join("");
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
    const active = suggestionFilter.sort.startsWith(column);
    const asc = suggestionFilter.sort.endsWith("-asc");
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
  const typeOptions = distinct(s.suggestions, "category")
    .map((key) => ({ value: key, label: (CATEGORY_LABELS[key]?.[0] ?? key).toUpperCase() }));
  // Discovery methods render whatever is actually present, for the same reason.
  // Built-in and custom pattern matching are deliberately absent: they produce
  // DIRECT MATCHES applied without review, never Suggestions, so offering them as
  // a filter would promise rows that cannot appear.
  const methodOptions = DISCOVERY_METHODS
    .filter((m) => (s.suggestions ?? []).some((r) => (r.discoveryMethods ?? []).includes(m)))
    .map((key) => ({ value: key, label: (WORKSPACE.methodLabel[key] ?? key).toUpperCase() }));

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
    select("filter-type", WORKSPACE.allTypes, suggestionFilter.category, typeOptions,
      WORKSPACE.filterTypeTitle) +
    sortButton("count", WORKSPACE.colCount, "sort-count", VALUES.sortCountHint) +
    select("filter-method", WORKSPACE.allMethods, suggestionFilter.method, methodOptions,
      WORKSPACE.filterMethodTitle) +
    `<span class="col-actions">${escapeHTML(WORKSPACE.colActions)}</span>` +
    `</div>`;
}

/**
 * spellingsOfSuggestion(row) names the longer forms folded into a Suggestion.
 *
 * A family arrives as ONE row, which is the point: three rows for "Coca-Cola",
 * "Coca-Cola company" and "Coca-Cola Ltd." invite three separate accept decisions
 * for one company. But accepting the row also accepts the spellings, so the row
 * has to say which ones, or the user is agreeing to something they cannot see.
 */
function spellingsOfSuggestion(row) {
  const spellings = row.spellings ?? [];
  if (spellings.length === 0) return "";
  return ` <span class="sugg-spellings hint">${escapeHTML(WORKSPACE.alsoSpelled(spellings))}</span>`;
}

/**
 * methodChips(methods) names every method that found a Suggestion or Value.
 *
 * A SET, not one badge, because two routes agreeing is worth seeing: the user
 * judging a Suggestion is deciding how much to trust it, and "the heuristic and
 * the local model both found this" is a different position from either alone.
 */
function methodChips(methods) {
  const list = (methods ?? []).filter((m) => DISCOVERY_METHODS.includes(m));
  if (list.length === 0) return "";
  return list.map((m) =>
    `<span class="method-chip method-${escapeHTML(m)}" title="${escapeHTML(WORKSPACE.methodTitle)}">` +
    `${escapeHTML(WORKSPACE.methodLabel[m] ?? m)}</span>`).join("");
}

/**
 * relatedNote(row, rows) names the other rows that share evidence with this one.
 *
 * It is a NOTE, never a fold: two organisations reached through one email domain
 * may genuinely be two legal entities, and giving them one placeholder would make
 * the mapping CSV say they were the same company. The user confirms grouping, with
 * the action the card already carries.
 */
function relatedNote(row, rows) {
  const sentence = relatedSentence(row, rows);
  if (!sentence) return "";
  return `<span class="related-note hint">${escapeHTML(sentence)}</span>`;
}

/**
 * relatedSentence(row, rows) is the same statement as plain text, for the places
 * that need the sentence rather than the element: a value card folds it into a
 * tooltip, where markup would be escaped and shown.
 */
function relatedSentence(row, rows) {
  const others = relatedTo(row, rows);
  return others.length === 0 ? "" : WORKSPACE.relatedValues(others);
}

/**
 * evidenceNote(evidence) explains WHY a discovery method produced a row.
 *
 * The engine returns evidence STRUCTURED, and the sentence is built here from
 * copy.js, because an engine returning prose makes the copy a contract nobody can
 * check and puts the explanation out of reach of the copy guards.
 */
function evidenceNote(evidence) {
  const list = evidence ?? [];
  if (list.length === 0) return "";
  const lines = list.map((e) => WORKSPACE.evidenceSentence(e)).filter(Boolean);
  if (lines.length === 0) return "";
  return `<span class="evidence-note hint" title="${escapeHTML(WORKSPACE.evidenceTitle)}">` +
    `${escapeHTML(lines.join(" "))}</span>`;
}

function suggestionRow(row, rows) {
  // The type is a dropdown, not a label: discovery guesses which KIND of name a
  // value is from its shape, and is often wrong about it. Retyping here means the
  // Value lands in the right type the moment it is accepted, rather than being
  // accepted wrong and moved on the next tab.
  const type = categorySelect(row.category, {
    cls: "cell-type-select sugg-type", title: WORKSPACE.retypeSuggestionTitle,
    ariaLabel: WORKSPACE.retypeSuggestionTitle, data: { text: row.mainText },
  });
  return `<div class="grid-row" style="grid-template-columns:${SUGGESTION_COLUMNS}"` +
    ` data-text="${escapeHTML(row.mainText)}">` +
    `<span class="cell-value" title="${escapeHTML(row.mainText)}">` +
    `${escapeHTML(row.mainText)}${spellingsOfSuggestion(row)}` +
    `${evidenceNote(row.evidence)}${relatedNote(row, rows)}</span>` +
    type +
    `<span class="cell-count mono">${escapeHTML(String(row.count ?? 0))}</span>` +
    `<span class="cell-methods">${methodChips(row.discoveryMethods)}</span>` +
    `<span class="cell-actions">` +
    button("", {
      kind: "ghost", cls: "sugg-accept icon-action boxed ok", icon: "check_circle",
      ariaLabel: `Accept ${row.mainText}`, title: WORKSPACE.accept,
    }) +
    button("", {
      kind: "ghost", cls: "sugg-reject icon-action boxed danger", icon: "close",
      ariaLabel: `Reject ${row.mainText}`, title: WORKSPACE.reject,
    }) +
    `</span></div>`;
}

/** distinct(rows, field) lists the values of one field, sorted, once each. */
function distinct(rows, field) {
  return [...new Set((rows ?? []).map((r) => r[field]).filter(Boolean))].sort();
}

// --- My values ------------------------------------------------------------

/**
 * visibleValues(values, filter) applies the My values tab's search and type
 * filter. A search matches a value's NAME or any of its spellings, so a user
 * who only remembers a spelling still finds the card. Pure, and exported for the
 * render test.
 */
export function visibleValues(values, filter) {
  const q = (filter.search ?? "").trim().toLowerCase();
  const type = filter.type ?? "";
  return (values ?? []).filter((e) => {
    if (type && e.category !== type) return false;
    if (!q) return true;
    if (e.mainText.toLowerCase().includes(q)) return true;
    for (const [lower] of spellingsOf(e)) if (lower.includes(q)) return true;
    return false;
  });
}

/** conflictMessage(c) is the user-visible wording for one conflict, built from
 *  copy.js so no sentence lives in a view. */
export function conflictMessage(c) {
  switch (c.kind) {
    case "ambiguity": return WORKSPACE.conflictAmbiguity(c.value, categoryLabel(c.withCategory));
    case "collision": return WORKSPACE.conflictCollision(c.spelling, c.withValue);
    case "allowlist": return WORKSPACE.conflictAllowlist(c.value);
    default: return "";
  }
}

/**
 * pruneValueSelection(s) drops picked keys whose value is no longer in the list.
 *
 * Called from the render rather than from every reducer that can remove a value,
 * for the reason the intersection check is: a reducer added later cannot forget
 * a rule that lives in the one place all of them repaint through.
 *
 * @param {object} s the state snapshot being rendered
 */
function pruneValueSelection(s) {
  if (selectedValueKeys.size === 0) return;
  const live = new Set((s.values ?? []).map((e) => valueKey(e.category, e.mainText)));
  for (const key of selectedValueKeys) if (!live.has(key)) selectedValueKeys.delete(key);
}

/**
 * SELECTION_SAFE_EXCLUDES are the elements a selecting click must NOT be read
 * from: everything on a card that already does something when clicked.
 *
 * Ctrl+click selects a card only where there is nothing else to press. A modifier
 * does not stop a button firing, so without this guard one gesture would both
 * select the card and remove a spelling, and the user cannot tell which of the
 * two they asked for.
 *
 * Listed one selector per entry rather than as one comma-separated selector
 * because closest() is asked for each in turn, which is also what the test DOM
 * supports.
 */
const SELECTION_SAFE_EXCLUDES = ["button", "input", "select", "textarea", "a", "label"];

/**
 * isInteractiveTarget(node) is true when the click landed on, or inside, one of
 * the card's own controls.
 *
 * @param {object} node the event target
 * @returns {boolean}
 */
function isInteractiveTarget(node) {
  for (const selector of SELECTION_SAFE_EXCLUDES) {
    if (node?.closest?.(selector)) return true;
  }
  return false;
}

/**
 * toggleValueSelection(key) adds or removes one card from the selection.
 *
 * It repaints, because the selection is not only a tint on one card: the clear
 * button's LABEL depends on whether anything is picked, and a tint applied in
 * place beside a button still reading "Clear all" would be a lie about what the
 * next press does.
 *
 * @param {string} key the card's valueKey
 */
function toggleValueSelection(key) {
  if (selectedValueKeys.has(key)) selectedValueKeys.delete(key);
  else selectedValueKeys.add(key);
  setState({});
}

/**
 * valuesFilterBar(s) is the FILTERS block: the caption, then the search and the
 * type filter on ONE row.
 *
 * It holds nothing that CHANGES the list. Narrowing what is shown and adding to
 * what exists are two different jobs, and one row carrying both let a user reach
 * for the search and press Clear all: the two captions are what keep the reading
 * unambiguous, so the bulk clear lives in the VALUES block beside Add value.
 *
 * The type filter is drawn as an ordinary select, the same as the add row's own
 * category dropdown, because that is what it is: one control that picks a
 * category. It must NOT wear .head-select, which is the borderless bold caption
 * style a filter takes when it sits inside a table's HEADER ROW: standing on its
 * own under a caption, that style reads as a second heading rather than as
 * something to press.
 */
function valuesFilterBar(s) {
  const search = searchBox({
    id: "values-search", value: valuesFilter.search, cls: "values-search",
    placeholder: WORKSPACE.valuesSearchPlaceholder, label: WORKSPACE.valuesSearchLabel,
    clearLabel: VALUES.clearSearch,
  });

  // The type filter lists only the types actually present, so it never offers a
  // category the current list cannot show.
  const typeOptions = distinct(s.values, "category")
    .map((key) =>
      `<option value="${escapeHTML(key)}"${key === valuesFilter.type ? " selected" : ""}>` +
      `${escapeHTML(categoryLabel(key))}</option>`).join("");
  const typeFilter =
    `<select id="values-type" class="values-type-filter${valuesFilter.type ? " filtered" : ""}"` +
    ` title="${escapeHTML(WORKSPACE.valuesFilterTypeTitle)}"` +
    ` aria-label="${escapeHTML(WORKSPACE.valuesFilterTypeTitle)}">` +
    `<option value="">${escapeHTML(WORKSPACE.valuesAllTypes)}</option>${typeOptions}</select>`;

  return `<div class="values-section">` +
    sectionLabel(WORKSPACE.valuesFiltersHeading) +
    `<div class="values-toolbar">${search}${typeFilter}</div>` +
    `</div>`;
}

/**
 * clearValuesButton(s) is the ONE bulk-removal control, and it states which of
 * its two jobs it will do.
 *
 * With nothing picked it empties the list ("Clear all"). With cards picked by
 * Ctrl+click it removes only those ("Clear selected"). One button rather than
 * two, because two would put a live "Clear all" next to a selection the user
 * just made, and pressing the wrong one destroys work no undo brings back. Its
 * id does not change with its label: the handler reads the selection, so both
 * jobs are the same control.
 */
function clearValuesButton(s) {
  const picked = selectedValueKeys.size;
  return button(picked ? WORKSPACE.clearSelected : WORKSPACE.clearAll, {
    kind: "secondary", id: "btn-clear-values", icon: "delete",
    disabled: s.values.length === 0,
    title: picked ? WORKSPACE.clearSelectedTitle : WORKSPACE.clearAllTitle,
  });
}

/** valuesTab(s) is the filter toolbar, the add row, then one card per value,
 *  each highlighting the conflicts that would block the run. */
export function valuesTab(s) {
  // A selection outlives the repaint that follows every edit, so keys pointing at
  // values that are no longer there have to go: a stale key would leave the clear
  // button saying "Clear selected" with nothing selected, which is a button that
  // then removes nothing and explains nothing.
  pruneValueSelection(s);

  const addRow =
    `<div class="values-section">` +
    sectionLabel(WORKSPACE.valuesHeading) +
    `<div class="add-row">` +
    `<input id="value-draft" class="grow" value="${escapeHTML(drafts.value)}"` +
    ` placeholder="${escapeHTML(WORKSPACE.addValuePlaceholder)}"` +
    ` aria-label="${escapeHTML(WORKSPACE.addValueLabel)}"/>` +
    categorySelect(drafts.valueCategory, {
      id: "value-category", ariaLabel: WORKSPACE.addValueCategory,
    }) +
    button(WORKSPACE.addValue, { kind: "secondary", id: "btn-add-value" }) +
    // The bulk clear sits on the SAME row as Add value, at its far end: both act
    // on the list as a whole, so they belong to the same block, and the growing
    // input between them is what keeps "add one" and "remove many" apart.
    clearValuesButton(s) +
    `</div>` +
    // The live match count. A value that matches nothing is almost always a
    // typo, and saying so before the run is the cheapest correction there is.
    `<p class="hint" id="value-matches">${escapeHTML(drafts.valueMatches)}</p>` +
    `</div>`;

  // Conflicts are computed ONCE for the whole list, because a collision is a
  // relationship BETWEEN two values: each card needs to know about the other.
  const conflicts = valueConflicts(s);
  // Intersections come from Go and are keyed the same way, so a card attaches
  // its own with no searching.
  const overlaps = intersectionsFor(s);
  // Only the TYPE filter narrows what is RENDERED. The search is applied IN
  // PLACE after the render (applyValuesSearchFilter), never here, and the reason
  // is a bug that returns the moment the search is folded back in: if a render
  // pruned the cards a search hides, then a render triggered WHILE a search is
  // active (a type change, an add, an accept) would drop those cards from the
  // DOM, and the in-place filter cannot reveal a card that is not there. Clearing
  // the search with the field's own clear then fails to bring the hidden cards
  // back, because there is nothing left to unhide. Rendering every type match and
  // hiding by search in place, exactly as the spellings popup does, keeps the
  // full set in the DOM so clearing always restores it.
  const shown = visibleValues(s.values, { type: valuesFilter.type });
  const cards = s.values.length === 0
    ? `<div class="grid-empty">${escapeHTML(WORKSPACE.noValues)}</div>`
    : (shown.length === 0
      ? `<div class="grid-empty">${escapeHTML(WORKSPACE.noValuesMatch)}</div>`
      : shown.map((e) => {
        const key = valueKey(e.category, e.mainText);
        return valueCard(e, conflicts.get(key), s, overlaps.get(key));
      }).join("") +
        // Shown only by the live search when it hides every card: the toolbar
        // filters imperatively without a re-render, so it needs a "no match"
        // line that is already in the DOM to reveal.
        `<div class="grid-empty values-search-empty" style="display:none">${escapeHTML(WORKSPACE.noValuesMatch)}</div>`);

  return valuesFilterBar(s) + addRow + cards;
}

/**
 * SPELLING_PREVIEW_BUDGET is how many CHARACTERS of spelling text the compact
 * card shows before the rest goes behind "+N more".
 *
 * A character budget rather than a chip count, because the thing that has to fit
 * is one LINE of the card, and three long spellings overflow a line that ten
 * initials sit inside comfortably. 65 is what fits at the card's width with the
 * label, the overflow control and the add control beside it.
 */
const SPELLING_PREVIEW_BUDGET = 65;

/**
 * previewSpellings(spellings, budget) splits an ordered spelling list into the
 * chips the card shows and the ones the popup owns.
 *
 * The first chip is ALWAYS shown, even when it alone exceeds the budget: a value
 * whose only spelling is long would otherwise render a row of nothing but "+1
 * more", which reads as a card with no spellings at all. It stops at the first
 * chip that does not fit rather than skipping it to try a shorter one, because
 * the list is ordered and a preview that reorders it is a preview of a different
 * list.
 *
 * Pure, and exported for the render test.
 *
 * @param {Array<string>} spellings the ordered list, derived then manual
 * @param {number} [budget] characters of chip text to spend
 * @returns {{shown: Array<string>, hidden: Array<string>}}
 */
export function previewSpellings(spellings, budget = SPELLING_PREVIEW_BUDGET) {
  const list = spellings ?? [];
  let used = 0;
  let count = 0;
  for (const spelling of list) {
    const length = (spelling ?? "").length;
    if (count > 0 && used + length > budget) break;
    used += length;
    count += 1;
  }
  return { shown: list.slice(0, count), hidden: list.slice(count) };
}

/**
 * cardStatus(e, conflict, overlap) is the card's ONE warning affordance: an icon
 * whose surface holds the warning text and the actions that resolve it.
 *
 * There is one icon and never two, because the two states are not independent
 * questions: a card with a blocking conflict has something to FIX before the run,
 * and whatever else is true about it is secondary until that is done. Red for the
 * conflict, amber for the intersection, nothing at all for a clean card.
 *
 * The actions inside it are the ones that used to sit under the inline notes, so
 * nothing is lost by folding them away: Solve conflicts for a conflict, and for
 * an intersection the two gestures that resolve it, never anonymise the covering
 * term or group the two values into one.
 */
function cardStatus(e, conflict, overlap) {
  if (conflict) {
    return warningPopover({
      tone: "bad",
      label: WORKSPACE.cardConflictLabel,
      // Deduplicated: the same conflict is reported from both sides of the
      // relationship, and reading it twice teaches nothing.
      lines: [...new Set(conflict.list.map(conflictMessage))].filter(Boolean),
      actionsHTML: button(WORKSPACE.solveConflicts, {
        kind: "ghost", cls: "value-solve", title: WORKSPACE.solveConflictsTitle,
      }),
      id: `warn-${valueKey(e.category, e.mainText)}`,
    });
  }
  if (overlap) {
    // The warning names the winning METHOD, never the internal rank: the rank is
    // an engine input, and a user reading "rank 1" learns nothing.
    const route = WORKSPACE.matchClassLabel[overlap.winnerMatchClass] ?? overlap.winnerMatchClass;
    return warningPopover({
      tone: "warn",
      label: WORKSPACE.cardWarningLabel,
      lines: [
        WORKSPACE.intersectionAll(
          overlap.value, overlap.winnerValue, route, overlap.matchedTexts),
        WORKSPACE.intersectionOrder,
        WORKSPACE.intersectionFix,
      ],
      actionsHTML:
        button(WORKSPACE.intersectionAllowWinner, {
          kind: "ghost", cls: "intersection-allow", data: { term: overlap.winnerValue },
        }) +
        button(WORKSPACE.groupWith, { kind: "ghost", cls: "value-group" }),
      id: `warn-${valueKey(e.category, e.mainText)}`,
    });
  }
  return "";
}

/**
 * cardInfo(e, s) folds the evidence and the related-values note behind an info
 * tooltip.
 *
 * Neither is a warning: they explain where a Value came from and which other
 * Values share its evidence. They are worth keeping and not worth a permanent row
 * each, and a row that appears when evidence arrives is one more thing changing
 * the card's height under the reader.
 */
function cardInfo(e, s) {
  const lines = [
    ...(e.evidence ?? []).map((piece) => WORKSPACE.evidenceSentence(piece)),
    relatedSentence(e, s.values),
  ].filter(Boolean);
  if (lines.length === 0) return "";
  return helpTooltip(lines.join(" "), {
    label: WORKSPACE.cardInfoLabel,
    id: `info-${valueKey(e.category, e.mainText)}`,
  });
}

/**
 * valueCard(e, conflict, s, overlap) renders one value: its editable name, its
 * type dropdown, its status icon, the group/remove actions, and a bounded
 * preview of its spellings.
 *
 * The card is a FIXED-HEIGHT surface, and that is a behaviour rather than a
 * style. Its height must not depend on how many warnings it carries or how many
 * spellings it has, because every card below one that resizes moves under the
 * reader: re-deriving a spelling briefly empties the chip row, the browser clamps
 * the list's scroll offset to the shorter content, and the position is lost
 * upward. So the warnings are one hover icon, the explanations are one info
 * tooltip, and the chips are one line with the overflow behind "+N more".
 *
 * `conflict` is this value's entry from valueConflicts (or undefined when it is
 * clean). A conflict tints the card and the exact name at fault, so a user can
 * see BEFORE the Anonymise step which values would refuse the run.
 *
 * `overlap` is its entry from intersectionsFor: another route claims the same
 * text. It is a WARNING and must not look like a blocking conflict, because the
 * precedence rule always has an answer and the run will go ahead. It gets its
 * own quieter tint, and it does not touch the step 2 to 3 gate.
 */
function valueCard(e, conflict, s, overlap) {
  const key = valueKey(e.category, e.mainText);
  const nameBad = !!(conflict && conflict.nameConflicts.length);
  const spellingConflicts = conflict ? conflict.spellingConflicts : new Map();

  // Every lower-cased spelling this value would match, joined for the live
  // search: the toolbar filters cards imperatively (see applyValuesSearchFilter)
  // rather than re-rendering per keystroke, so it reads this instead of the
  // card's text. It mirrors visibleValues' matching, and it holds the spellings
  // that are in the popup's overflow rather than on the card.
  const searchText = [...spellingsOf(e).keys()].join(" ");

  // The name is click-to-edit: a button that reads as a heading until clicked,
  // then reveals an input (revealNameInput). Renaming what a value BECOMES is a
  // step 3 action; this renames the value ITSELF.
  const nameBtn =
    `<button type="button" class="value-name${nameBad ? " bad" : ""}"` +
    ` title="${escapeHTML(WORKSPACE.editValueTitle)}">${escapeHTML(e.mainText)}</button>`;

  const typeSelect = categorySelect(e.category, {
    cls: "value-type", title: WORKSPACE.changeTypeLabel, ariaLabel: WORKSPACE.changeTypeLabel,
  });

  // Which methods found this Value. It is SHOWN, not merely stored: the
  // precedence rule between routes is only meaningful to a user who can see which
  // route owns a Value, and an unexplained decision reads as randomness.
  const methods = methodChips(e.discoveryMethods?.length ? e.discoveryMethods : ["manual"]);

  const actions =
    button(WORKSPACE.groupWith, {
      kind: "ghost", cls: "value-group", icon: "content_copy", title: WORKSPACE.groupWithTitle,
    }) +
    button("", {
      kind: "ghost", cls: "value-remove icon-action danger", icon: "close",
      ariaLabel: `Remove ${e.mainText}`, title: WORKSPACE.removeValue,
    });

  // The visible chips stay DRAG SOURCES, which is the quick way to regroup a
  // spelling the expansion attached to the wrong value. Each also carries a small
  // delete "x", the quick way to drop a spelling the expansion should not have
  // attached. The full list and every gesture that manages it still live in the
  // popup, which is also where a spelling in the overflow is reached.
  const all = [...(e.derivedSpellings ?? []), ...(e.spellings ?? [])];
  const { shown, hidden } = previewSpellings(all);
  const chips = shown.map((v) => {
    const bad = spellingConflicts.has(v.trim().toLowerCase());
    return `<span class="chip-tag spelling-chip${bad ? " bad" : ""}" draggable="true"` +
      ` data-spelling="${escapeHTML(v)}"` +
      ` title="${escapeHTML(WORKSPACE.spellingDragHint)}">` +
      `<span class="spelling-text">${escapeHTML(v)}</span>` +
      button("", {
        kind: "ghost", cls: "chip-remove spelling-del", icon: "close",
        ariaLabel: `Remove spelling ${v}`, title: WORKSPACE.removeSpelling,
        data: { spelling: v },
      }) +
      `</span>`;
  }).join("");

  // "pending" is a real state, not an absence: null derivedSpellings mean an
  // expansion is in flight, [] means it finished and found none. All three
  // possibilities render INSIDE the one row, never as a row of their own, so the
  // card is the same height whichever is true.
  const spellingNote = e.spellingsError
    ? `<span class="hint bad">${escapeHTML(e.spellingsError)}</span>`
    : (e.derivedSpellings === null || e.derivedSpellings === undefined)
      ? `<span class="hint">${escapeHTML(WORKSPACE.spellingsPending)}</span>`
      : (all.length === 0 ? `<span class="hint">${escapeHTML(WORKSPACE.noSpellings)}</span>` : "");

  const more = hidden.length
    ? button(WORKSPACE.moreSpellings(hidden.length), {
      kind: "ghost", cls: "chip-add spelling-more", title: WORKSPACE.moreSpellingsTitle,
    })
    : "";

  const spellingRow =
    `<div class="chip-row spelling-row">` +
    `<span class="hint">${escapeHTML(WORKSPACE.derivedSpellings)}</span>` +
    `${chips}${spellingNote}${more}` +
    button(WORKSPACE.addSpelling, {
      kind: "ghost", cls: "chip-add spelling-add",
      title: WORKSPACE.moreSpellingsTitle,
    }) +
    `</div>`;

  const panel = openValuePanel.key === key
    ? (openValuePanel.kind === "group" ? groupPanel(e, s)
      : (openValuePanel.kind === "solve" && conflict ? solvePanel(e, conflict) : ""))
    : "";

  // Picked with Ctrl+click. The title teaches the gesture where the gesture is
  // performed: the controls inside the card carry their own titles, so this one
  // only surfaces over the card's own surface, which is exactly the area a
  // selecting click is allowed to land on.
  const selected = selectedValueKeys.has(key);

  return `<div class="value-card${conflict ? " conflicted" : ""}${overlap ? " intersects" : ""}` +
    `${selected ? " selected" : ""}" data-key="${escapeHTML(key)}"` +
    ` data-category="${escapeHTML(e.category)}" data-main-text="${escapeHTML(e.mainText)}"` +
    ` title="${escapeHTML(WORKSPACE.selectCardHint)}"` +
    ` data-search="${escapeHTML(searchText)}">` +
    `<div class="row-between">` +
    `<div class="value-head">${nameBtn}${typeSelect}${methods}` +
    `${cardStatus(e, conflict, overlap)}${cardInfo(e, s)}</div>` +
    `<div class="value-actions">${actions}</div>` +
    `</div>` +
    spellingRow +
    panel +
    `</div>`;
}

/* GROUP_COLUMNS is the picker grid's shape: the value takes the room left over
 * (it carries the checkbox and can be long) and the category is a fixed column,
 * so the badges line up down the list. */
const GROUP_COLUMNS = "minmax(0,1fr) 9rem";

/* The picker's sort key, module-level so it survives the repaint a merge or a
 * type change causes. It is view state for the same reason valuesFilter is:
 * nothing downstream reads it and it must never travel in a session file.
 *
 * There is no filter text beside it, deliberately: the filter hides rows IN
 * PLACE (see wireGroupPanel), so the live query is the input's own value and
 * cannot disagree with what is on screen. */
let groupSort = "value-asc";

/** groupRowsFor(e, s) is the picker's row data: every value except this card's
 *  own, in the order the current sort asks for. */
function groupRowsFor(e, s) {
  const selfKey = valueKey(e.category, e.mainText);
  const rows = s.values
    .filter((o) => valueKey(o.category, o.mainText) !== selfKey)
    .map((o) => ({ category: o.category, mainText: o.mainText, label: categoryLabel(o.category) }));
  return sortGroupRows(rows, groupSort);
}

/** sortGroupRows(rows, sort) orders the picker by one of its two columns.
 *
 *  The other column is always the tie-breaker, so two values of one category
 *  keep a stable, readable order instead of the order the store happens to
 *  hold them in. */
function sortGroupRows(rows, sort) {
  const dir = sort.endsWith("-desc") ? -1 : 1;
  const byValue = (a, b) => a.mainText.localeCompare(b.mainText, undefined, { sensitivity: "base" });
  const byCategory = (a, b) => a.label.localeCompare(b.label, undefined, { sensitivity: "base" });
  return [...rows].sort(sort.startsWith("category")
    ? (a, b) => (byCategory(a, b) || byValue(a, b)) * dir
    : (a, b) => (byValue(a, b) || byCategory(a, b)) * dir);
}

/** groupSortButton(column, label, title) draws one of the picker's two column
 *  headings as a sort control, with the arrow showing the live direction. */
function groupSortButton(column, label, title) {
  const active = groupSort.startsWith(column);
  const asc = groupSort.endsWith("-asc");
  return `<button class="sort-btn${active ? " active" : ""}" data-sort="${escapeHTML(column)}"` +
    ` title="${escapeHTML(title)}">${escapeHTML(label)}` +
    `<span class="icon sort-arrow${active && asc ? " asc" : ""}" aria-hidden="true">` +
    icon("expand_more").replace(/^<span class="icon" aria-hidden="true">|<\/span>$/g, "") +
    `</span></button>`;
}

/** groupPanel(e, s) is the inline picker for "Group with": the other values as a
 *  two-column grid (the value and its category), each row a checkbox, folded
 *  into this one on Apply.
 *
 *  It is a GRID rather than a plain list because the list is as long as the
 *  session's value count: sorting by either column and filtering by text are
 *  what make picking one row out of eighty possible. Both act on the rendered
 *  rows in place (wireGroupPanel), never through a repaint, so a tick already
 *  made survives a re-sort or a change of filter. */
function groupPanel(e, s) {
  const rows = groupRowsFor(e, s);
  if (rows.length === 0) {
    return `<div class="value-panel"><p class="hint">${escapeHTML(WORKSPACE.groupNone)}</p>` +
      `<div class="panel-actions">` +
      button(WORKSPACE.groupCancel, { kind: "ghost", cls: "panel-cancel" }) +
      `</div></div>`;
  }
  const body = rows.map((o) =>
    `<label class="grid-row group-row" style="grid-template-columns:${GROUP_COLUMNS}"` +
    ` data-search="${escapeHTML(`${o.mainText} ${o.label}`.toLowerCase())}">` +
    `<span class="group-cell">` +
    `<input type="checkbox" class="group-pick"` +
    ` data-category="${escapeHTML(o.category)}" data-main-text="${escapeHTML(o.mainText)}"/>` +
    `<span class="group-option-name">${escapeHTML(o.mainText)}</span>` +
    `</span>` +
    `<span class="group-cat">${escapeHTML(o.label)}</span>` +
    `</label>`).join("");
  // The heading keeps its explanation one hover (or one Tab) away instead of in
  // a paragraph above the grid: the panel opens INSIDE a card in a scrolling
  // list, and a sentence read once then costs a line of the picker forever.
  return `<div class="value-panel group-panel">` +
    `<div class="rail-label-row">` +
    `<p class="section-label">${escapeHTML(WORKSPACE.groupWithHeading)}</p>` +
    helpTooltip(WORKSPACE.groupWithHint, { label: WORKSPACE.groupWithHeading }) +
    `</div>` +
    searchBox({
      id: "group-filter", value: "",
      placeholder: WORKSPACE.groupFilterPlaceholder,
      label: WORKSPACE.groupFilterLabel,
      clearLabel: VALUES.clearSearch,
      cls: "group-filter",
    }) +
    `<div class="grid-box group-grid">` +
    `<div class="grid-head" style="grid-template-columns:${GROUP_COLUMNS}">` +
    groupSortButton("value", WORKSPACE.groupColValue, WORKSPACE.groupSortValueHint) +
    groupSortButton("category", WORKSPACE.groupColCategory, WORKSPACE.groupSortCategoryHint) +
    `</div>` +
    `<div class="group-options">${body}</div>` +
    `</div>` +
    `<p class="hint group-no-match" hidden>${escapeHTML(WORKSPACE.groupNoMatch)}</p>` +
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
          kind: "ghost", cls: "solve-action", data: { act: "drop-spelling", spelling: c.spelling },
        }) +
        button(WORKSPACE.solveGroupOtherLabel(c.withValue), {
          kind: "ghost", cls: "solve-action",
          data: { act: "merge", withcategory: c.withCategory, withvalue: c.withValue },
        });
    } else if (c.kind === "ambiguity") {
      acts = button(WORKSPACE.solveRemoveThis, {
        kind: "ghost", cls: "solve-action", data: { act: "remove-value" },
      });
    } else if (c.resolution?.action === "drop_allow_term") {
      // The action comes from the conflict's own stated resolution, the same one
      // the refused-run panel on Anonymise reads, so the two screens cannot
      // offer different ways out of one refusal.
      acts =
        button(WORKSPACE.solveRemoveFromAllowlist, {
          kind: "ghost", cls: "solve-action",
          data: { act: "remove-allow", term: c.resolution.term || c.value },
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

// --- The spellings popup ---------------------------------------------------
//
// One Value's whole spelling list, and every gesture that manages it: add,
// edit, delete, and move to another Value. It exists because the compact card
// deliberately shows only what fits on one line, and the affordances it used to
// carry (a delete per chip, an inline add) are what made its height a function
// of its data.
//
// It is a SELF-CONTAINED overlay rather than a kind of the shell's confirm
// dialog, for a reason that only shows up in the interaction: "Move to" opens
// the shared value picker (askChoice) WHILE this is open, and the shell has one
// question slot and one pending promise. Sharing it would mean the popup
// destroying itself to ask where a spelling should go. The two surfaces stack
// instead, which is also what the user is doing: picking a target for something
// in the list behind.
//
// It edits state LIVE. Every row action calls the same reducer the card used to,
// so the compact card behind it updates on the same repaint; there is no OK to
// press and nothing to cancel, which the footer says out loud.

/** popupValue(s) is the Value the open popup belongs to, or undefined. */
function popupValue(s) {
  if (!spellingsPopup) return undefined;
  return s.values.find((e) =>
    valueKey(e.category, e.mainText) === valueKey(spellingsPopup.category, spellingsPopup.mainText));
}

/**
 * spellingsPopupHTML(s) renders the popup, or nothing when none is open.
 *
 * The list holds the SPELLINGS and nothing else. The main text is the head of
 * the family rather than one of its spellings, and neither row action applies to
 * it, so a row for it carried nothing but the reason it could not be used. The
 * popup's title is where the family is named.
 *
 * Exported for the render test.
 */
export function spellingsPopupHTML(s) {
  const e = popupValue(s);
  // The Value can disappear under an open popup (removed from another surface,
  // or folded into a group). Rendering nothing is the honest answer; the wiring
  // closes the popup on the same repaint.
  if (!e) return "";

  const spellings = [...(e.derivedSpellings ?? []), ...(e.spellings ?? [])];
  const query = spellingsPopupSearch.trim().toLowerCase();
  const matches = (text) => !query || text.toLowerCase().includes(query);

  const rows = spellings.map((v) =>
    `<div class="spelling-list-row" data-spelling-row="${escapeHTML(v)}"` +
    ` data-search="${escapeHTML(v.toLowerCase())}"` +
    `${matches(v) ? "" : ` style="display:none"`}>` +
    `<button type="button" class="spelling-list-text spelling-list-edit"` +
    ` title="${escapeHTML(WORKSPACE.spellingsPopupEditTitle)}">${escapeHTML(v)}</button>` +
    `<span class="spelling-list-actions">` +
    button(WORKSPACE.spellingsPopupMove, {
      kind: "ghost", cls: "spelling-move", title: WORKSPACE.spellingsPopupMoveTitle,
      data: { spelling: v },
    }) +
    button(WORKSPACE.spellingsPopupDelete, {
      kind: "ghost", cls: "spelling-delete danger", title: WORKSPACE.spellingsPopupDeleteTitle,
      data: { spelling: v },
    }) +
    `</span></div>`).join("");

  const empty = spellings.length === 0
    ? `<p class="hint" id="spellings-popup-empty">${escapeHTML(WORKSPACE.spellingsPopupEmpty)}</p>`
    : "";
  // Rendered up front and revealed by the in-place filter, exactly as the values
  // search's "no match" line is: the filter never re-renders, so the line it
  // needs has to be in the DOM already.
  const noMatch =
    `<p class="hint" id="spellings-popup-nomatch"` +
    `${query && !spellings.some(matches) ? "" : ` style="display:none"`}>` +
    `${escapeHTML(WORKSPACE.spellingsPopupNoMatch)}</p>`;

  return `<div class="modal-layer spellings-layer" role="presentation">` +
    `<div class="modal spellings-popup" role="dialog" aria-modal="true"` +
    ` aria-label="${escapeHTML(WORKSPACE.spellingsPopupTitle(e.mainText))}">` +
    `<div class="modal-head">` +
    `<span>${escapeHTML(WORKSPACE.spellingsPopupTitle(e.mainText))}</span>` +
    button("", {
      kind: "ghost", cls: "spellings-close icon-action", icon: "close",
      ariaLabel: WORKSPACE.spellingsPopupClose, title: WORKSPACE.spellingsPopupClose,
    }) +
    `</div>` +
    `<div class="modal-body">` +
    `<div class="add-row">` +
    `<input id="spelling-draft" class="grow" value="${escapeHTML(drafts.spelling)}"` +
    ` placeholder="${escapeHTML(WORKSPACE.spellingsPopupAddPlaceholder)}"` +
    ` aria-label="${escapeHTML(WORKSPACE.spellingsPopupAddLabel)}"/>` +
    button(WORKSPACE.spellingsPopupAdd, { kind: "secondary", id: "btn-add-spelling" }) +
    `</div>` +
    searchBox({
      id: "spellings-search", value: spellingsPopupSearch,
      placeholder: WORKSPACE.spellingsPopupSearchPlaceholder,
      label: WORKSPACE.spellingsPopupSearchLabel,
      clearLabel: VALUES.clearSearch,
    }) +
    `<div class="spelling-list" id="spellings-popup-list">` +
    `<div class="spelling-list-head">` +
    `<span>${escapeHTML(WORKSPACE.colSpelling)}</span>` +
    `<span>${escapeHTML(WORKSPACE.colActions)}</span>` +
    `</div>${rows}</div>` +
    empty + noMatch +
    `<p class="hint">${escapeHTML(WORKSPACE.spellingsPopupLive)}</p>` +
    `</div></div></div>`;
}

/** openSpellingsPopup(category, mainText, focusAdd) opens the popup on one
 *  Value, with the caret in the add field when that is what was clicked. */
function openSpellingsPopup(category, mainText, focusAdd) {
  spellingsPopup = { category, mainText, focusAdd: !!focusAdd };
  spellingsPopupSearch = "";
  drafts.spelling = "";
  setState({});
}

/** closeSpellingsPopup() dismisses it and repaints. */
function closeSpellingsPopup() {
  if (!spellingsPopup) return;
  spellingsPopup = null;
  spellingsPopupSearch = "";
  drafts.spelling = "";
  setState({});
}

/**
 * applySpellingsSearchFilter(container, query) shows or hides the already
 * rendered rows, WITHOUT re-rendering.
 *
 * The same reason the values search filters in place: a re-render rewrites the
 * container with innerHTML and destroys the input mid-type, so focus and the
 * caret are lost after every character. Exported for the render test.
 *
 * @returns {number} how many rows remain visible
 */
export function applySpellingsSearchFilter(container, query = spellingsPopupSearch) {
  const q = (query ?? "").trim().toLowerCase();
  let visible = 0;
  for (const row of container.querySelectorAll(".spelling-list-row")) {
    const hay = row.dataset?.search ?? "";
    const match = !q || hay.includes(q);
    row.style.display = match ? "" : "none";
    if (match) visible++;
  }
  const none = container.querySelector("#spellings-popup-nomatch");
  if (none) none.style.display = visible === 0 ? "" : "none";
  return visible;
}

/**
 * wireSpellingsPopup(container) attaches the popup's behaviour.
 *
 * Every action calls the reducer the card used to call and then repaints, which
 * is what makes the compact card behind the popup update live: both read the same
 * store.
 */
function wireSpellingsPopup(container) {
  const layer = container.querySelector(".spellings-layer");
  if (!layer) {
    // The popup is open on a Value that no longer exists: nothing rendered, so
    // clear the view state rather than leaving a slot pointing at nothing.
    if (spellingsPopup && !popupValue(getState())) spellingsPopup = null;
    return;
  }
  const { category: cat, mainText } = spellingsPopup;

  layer.querySelector(".spellings-close")?.addEventListener("click", closeSpellingsPopup);
  // A click on the backdrop dismisses; a click INSIDE must not, so the target has
  // to be the layer itself rather than any descendant.
  layer.addEventListener("click", (ev) => {
    if (ev.target === layer) closeSpellingsPopup();
  });
  layer.addEventListener("keydown", (ev) => {
    if (ev.key !== "Escape") return;
    ev.stopPropagation();
    closeSpellingsPopup();
  });

  const draft = layer.querySelector("#spelling-draft");
  draft?.addEventListener("input", () => { drafts.spelling = draft.value; });

  const add = async () => {
    const value = (drafts.spelling ?? "").trim();
    if (!value) return;
    const before = spellingCount(cat, mainText);
    addSpelling(cat, mainText, value);
    if (spellingCount(cat, mainText) === before) {
      // A duplicate: say so, rather than looking like nothing happened. It goes
      // to the notice strip and not to a line on the card, because a line that
      // appears and clears is one more thing changing the card's height.
      notify(WORKSPACE.spellingAlreadyThere(value), "info");
      return;
    }
    drafts.spelling = "";
    setState({});
    await refreshVariants();
  };
  layer.querySelector("#btn-add-spelling")?.addEventListener("click", add);
  draft?.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") add();
    else if (ev.key === "Escape") { ev.stopPropagation(); closeSpellingsPopup(); }
  });

  const search = wireSearchBox(layer, "spellings-search", (value) => {
    // No setState: re-rendering would destroy this very input and lose the caret.
    spellingsPopupSearch = value;
    applySpellingsSearchFilter(layer, value);
  });

  for (const row of layer.querySelectorAll(".spelling-list-row")) {
    const spelling = row.dataset.spellingRow;
    if (!spelling) continue; // no spelling behind the row, so no action to wire

    row.querySelector(".spelling-list-edit")?.addEventListener("click", (ev) => {
      revealSpellingRowInput(ev.currentTarget, cat, mainText);
    });

    row.querySelector(".spelling-delete")?.addEventListener("click", async () => {
      deleteVariant(cat, mainText, spelling);
      await refreshVariants();
      // The nudge towards the tab that IS for negative rules: this deletion
      // stops the spelling belonging to THIS value, not to every value.
      notify(WORKSPACE.spellingDeleted(spelling), "info");
    });

    row.querySelector(".spelling-move")?.addEventListener("click", async () => {
      await moveSpellingElsewhere(cat, mainText, spelling);
    });
  }

  // "+ add" and "+N more" open the same surface; only the caret's home differs.
  if (spellingsPopup.focusAdd) draft?.focus?.();
  else search?.focus?.();
}

/**
 * moveSpellingElsewhere(cat, mainText, spelling) asks which Value a spelling
 * should belong to and regroups it.
 *
 * This is how a spelling in the overflow is regrouped: dragging a chip only
 * works for the ones the card shows, and the popup is where the rest live. It
 * reuses the SAME picker "Group with" builds, so there is one way to name a
 * target Value.
 */
async function moveSpellingElsewhere(cat, mainText, spelling) {
  const selfKey = valueKey(cat, mainText);
  const choices = getState().values
    .filter((o) => valueKey(o.category, o.mainText) !== selfKey)
    .map((o) => ({
      id: valueKey(o.category, o.mainText),
      label: `${o.mainText} (${categoryLabel(o.category)})`,
    }));
  if (choices.length === 0) {
    notify(WORKSPACE.spellingsPopupMoveNone, "info");
    return;
  }
  const target = await askChoice({
    title: WORKSPACE.spellingsPopupMoveHeading,
    body: WORKSPACE.spellingsPopupMoveBody(spelling),
    choices,
  });
  if (!target) { setState({}); return; } // cancelled: the popup stays as it was

  const to = getState().values.find((o) => valueKey(o.category, o.mainText) === target);
  if (!to) { setState({}); return; }
  // "" is success; anything else is a reason the move did not happen (a stale
  // drop, an unknown row), and the popup simply stays as it was.
  if (moveSpelling(cat, mainText, to.category, to.mainText, spelling) !== "") {
    setState({});
    return;
  }
  notify(WORKSPACE.spellingMoved(spelling, to.mainText), "ok");
  await refreshVariants();
}

/**
 * revealSpellingRowInput(textEl, category, mainText) swaps one row's text for an
 * inline input, so a typo is fixed without the row losing its place.
 *
 * Editing rather than delete-then-add, because delete-then-add moves the row to
 * the end of the list and curates twice for one correction. It commits through
 * renameSpelling, which excludes the old spelling and adds the new one.
 *
 * Transient DOM: no state change reveals it, and a repaint puts the row back.
 */
function revealSpellingRowInput(textEl, category, mainText) {
  const old = textEl.textContent;

  const input = textEl.ownerDocument.createElement("input");
  input.className = "spelling-input";
  input.value = old;
  input.placeholder = WORKSPACE.editVariantPlaceholder;
  input.setAttribute("aria-label", WORKSPACE.spellingsPopupEditTitle);
  textEl.replaceWith(input);
  input.focus();
  input.select();

  let done = false;
  const commit = async () => {
    if (done) return;
    done = true;
    const value = input.value.trim();
    if (!value || value === old) {
      setState({}); // repaint puts the row back unchanged
      return;
    }
    renameSpelling(category, mainText, old, value);
    await refreshVariants();
  };

  input.addEventListener("keydown", (ev) => {
    ev.stopPropagation();
    if (ev.key === "Enter") commit();
    else if (ev.key === "Escape") { done = true; setState({}); }
  });
  input.addEventListener("blur", commit);
}

// --- Never anonymise ------------------------------------------------------

function allowTab(s) {
  return renderAllowlistChips(s, drafts.allow);
}

// --- Built-in patterns (read-only) ----------------------------------------

/**
 * builtInPatternsTab(s) shows what the built-in patterns matched the last time
 * detection ran, grouped by signal category.
 *
 * It is READ-ONLY, and that is the design rather than an omission. A built-in
 * pattern produces DIRECT matches: the pattern is a rule the user chose, so its
 * findings are applied without review and there is nothing on a row to accept or
 * reject. What the user does decide is which categories are on, and this tab is
 * the only place that decision can be CHECKED before the batch is anonymised. So
 * the copy points at the rail's category switches instead of offering per-row
 * actions this tab must not have.
 *
 * Every category that RAN gets a section, including the ones that matched
 * nothing. An empty section is the point of the tab in the workflow it was asked
 * for: tick "street addresses", run detection, and see either the addresses or
 * an explicit "nothing matched" under that heading. Dropping empty sections
 * would make a category that ran and found nothing indistinguishable from one
 * that never ran at all.
 */
export function builtInPatternsTab(s) {
  const preview = s.builtInPatterns;
  // Never run in this batch. Not the same as "found nothing", so it says so.
  if (!preview) {
    return `<div class="grid-empty">${escapeHTML(WORKSPACE.builtInNeverRan)}</div>`;
  }
  if (!preview.on) {
    return `<p class="hint">${escapeHTML(WORKSPACE.builtInSwitchedOff)}</p>`;
  }
  if (preview.categories.length === 0) {
    return `<p class="hint">${escapeHTML(WORKSPACE.builtInNoCategories)}</p>`;
  }

  // Grouped by category, in the order Go reported the ACTIVE categories, which
  // is the engine's own stable order (engine.AllPIICategories). Rendering from
  // that list rather than from the matches is what puts the empty sections in.
  const byCategory = new Map(preview.categories.map((key) => [key, []]));
  for (const match of preview.matches) {
    // A match under a category the run did not report as active cannot happen,
    // but rendering it in its own section rather than dropping it means a
    // mismatch between the two lists shows up instead of hiding a finding.
    if (!byCategory.has(match.category)) byCategory.set(match.category, []);
    byCategory.get(match.category).push(match);
  }

  const summary =
    `<p class="hint">${escapeHTML(
      WORKSPACE.builtInSummary(preview.matches.length, preview.categories.length))}</p>`;

  const sections = [...byCategory.entries()].map(([key, matches]) => {
    const body = matches.length
      ? matches.map(builtInRow).join("")
      : `<div class="grid-empty">${escapeHTML(WORKSPACE.builtInNoneInCategory)}</div>`;
    return `<div class="builtin-group" data-builtin-category="${escapeHTML(key)}">` +
      sectionLabel(`${categoryLabel(key)} (${matches.length})`) +
      `<div class="grid-box">${body}</div>` +
      `</div>`;
  }).join("");

  return `<p class="hint">${escapeHTML(WORKSPACE.builtInHint)}</p>` +
    (preview.matches.length === 0
      ? `<p class="hint">${escapeHTML(WORKSPACE.builtInNoMatchesAtAll)}</p>`
      : summary) +
    sections;
}

/**
 * builtInRow(match) is one matched text: what it says, how often and where, and
 * a badge when a corroborating checksum did not pass.
 *
 * The failed check is SHOWN rather than hidden, because by default the span is
 * replaced anyway (CLAUDE.md §5: a checksum failure lowers confidence, it never
 * vetoes on its own) and a mistyped, partly-redacted or synthetic bank
 * identifier is exactly what a template document holds. The user's lever over it
 * is "Only replace when the checksum matches", inside Built-in patterns in the
 * rail, not this row: the row REPORTS, and switching the rule is a setting.
 */
function builtInRow(match) {
  const files = match.documents ?? [];
  const weak = match.confidence > 0 && match.confidence < 1;
  return `<div class="builtin-row" data-builtin-text="${escapeHTML(match.text)}">` +
    `<span class="mono builtin-text">${escapeHTML(match.text)}</span>` +
    (weak
      ? `<span class="state-tag bad" title="${escapeHTML(WORKSPACE.builtInLowConfidence(match.confidence))}">` +
        `${escapeHTML(WORKSPACE.builtInLowConfidenceBadge)}</span>`
      : "") +
    `<span class="spacer"></span>` +
    `<span class="hint builtin-where" title="${escapeHTML(WORKSPACE.builtInInFiles(files))}">` +
    `${escapeHTML(WORKSPACE.builtInOccurrences(match.count, files.length))}</span>` +
    `</div>`;
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

  wireNotice(container);
  // Both hover surfaces are wired for the WHOLE workspace, not per tab: a card
  // carries one of each and re-wiring them per tab would mean four call sites
  // for one behaviour.
  wireHelpTooltips(container);
  wireWarningPopovers(container);
  wireSpellingsPopup(container);

  if (activeTab === "suggestions") wireSuggestions(container, shown);
  else if (activeTab === "values") wireValues(container);
  else if (activeTab === "allow") wireAllowlistChips(container, drafts);
  else if (activeTab === "patterns") wirePatterns(container);
}

// --- Where a run lands ----------------------------------------------------

/**
 * landOnResultTab() selects the tab the finished detection run filled.
 *
 * views/detectionrun.js calls it once the run has settled. It is exported
 * rather than inlined there because `activeTab` is this module's state: the
 * panel that owns the tabs is the only place that may choose between them.
 */
export function landOnResultTab() {
  // The fresh suggestion list is what the user ran for, unless no discovery
  // route ran at all: then there IS no suggestion list and what the run produced
  // is the built-in pattern preview, so that is the tab to land on rather than
  // an empty one.
  activeTab = builtInOnlyRun(getState()) ? "builtin" : "suggestions";
}

/**
 * builtInOnlyRun(s) reports whether the last run's only visible product was the
 * built-in pattern preview: no suggestion is waiting, and the patterns matched
 * something.
 *
 * It reads the STORE rather than the result, because "no suggestion is waiting"
 * is a fact about the review list the user is looking at (a run can find nothing
 * new while rows from an earlier run still wait), and landing on an empty tab is
 * exactly what this avoids.
 */
function builtInOnlyRun(s) {
  return s.suggestions.length === 0 && (s.builtInPatterns?.matches.length ?? 0) > 0;
}

// --- Suggestions wiring ---------------------------------------------------

function wireSuggestions(container, shown) {
  container.querySelector("#sort-value")?.addEventListener("click", () => {
    suggestionFilter = { ...suggestionFilter, sort: toggleValueSort(suggestionFilter.sort) };
    setState({});
  });
  container.querySelector("#sort-count")?.addEventListener("click", () => {
    suggestionFilter = { ...suggestionFilter, sort: toggleCountSort(suggestionFilter.sort) };
    setState({});
  });
  container.querySelector("#filter-type")?.addEventListener("change", (ev) => {
    suggestionFilter = { ...suggestionFilter, category: ev.target.value };
    setState({});
  });
  container.querySelector("#filter-method")?.addEventListener("change", (ev) => {
    suggestionFilter = { ...suggestionFilter, method: ev.target.value };
    setState({});
  });

  for (const row of container.querySelectorAll(".grid-row[data-text]")) {
    const text = row.dataset.text;
    row.querySelector(".sugg-accept")?.addEventListener("click", async () => {
      acceptSuggestion(text);
      await refreshVariants();
    });
    row.querySelector(".sugg-reject")?.addEventListener("click", () => {
      rejectSuggestion(text);
    });
    // Retyping a suggestion before it is accepted.
    row.querySelector(".sugg-type")?.addEventListener("change", (ev) => {
      changeSuggestionCategory(text, ev.target.value);
    });
  }

  // The two bulk buttons act on exactly the rows on screen, which is why they
  // are passed the FILTERED list rather than re-deriving it: a bulk action must
  // never be a surprise.
  const texts = shown.map((r) => r.mainText);
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
    drafts.value = "";
    drafts.valueMatches = "";

    // A value that is a spelling of one already listed joins it instead of
    // becoming a rival. Detection folds families over its whole output; values
    // added one at a time need the same treatment, or "Coca-Cola company"
    // beside "Coca-Cola" leaves the text reading "[BRAND_1] company".
    const family = foldIntoFamily(drafts.valueCategory, value);
    if (family) {
      // Said out loud, because a silent "your value became a spelling of
      // another one" is indistinguishable from the button not working.
      notify(WORKSPACE.foldedIntoValue(value, family.main), "info");
      setState({});
      await refreshVariants();
      return;
    }

    const n = addValues([{ category: drafts.valueCategory, mainText: value }]);
    if (n === 0) notify(WORKSPACE.valueAlreadyThere(value), "info");
    setState({});
    await refreshVariants();
  };
  container.querySelector("#btn-add-value")?.addEventListener("click", add);
  draft?.addEventListener("keydown", (ev) => { if (ev.key === "Enter") add(); });

  wireValuesToolbar(container);

  // Reflect the live search on the freshly rendered cards. The render puts every
  // type-matching card in the DOM (see valuesTab); this hides the ones the
  // current search does not match, so a re-render that happened while a search
  // was active still shows the right subset without pruning the DOM.
  applyValuesSearchFilter(container);

  // A card with no identity cannot act on a Value, and it has to SAY so: a
  // handler that silently returns is indistinguishable from a button that is not
  // wired, which is the failure this guard exists to prevent. The saying happens
  // ONCE, after the loop, and only when the strip is not already showing it: a
  // notice inside the loop repaints, the repaint re-wires, and the message would
  // renew itself forever.
  let identityLost = false;

  for (const cardEl of container.querySelectorAll(".value-card")) {
    const { category: cat, mainText, key } = cardEl.dataset;
    if (!cat || !mainText) {
      identityLost = true;
      continue;
    }

    // Ctrl+click (Cmd+click too, so the gesture is the platform's own) picks the
    // card, so a bulk action can name what it will act on. It is bound to the
    // CARD and reads the event's target, rather than being bound to a bare
    // "surface" element, because the card has no such element: its whole area is
    // either a control or a gap between controls, and a gap cannot carry a
    // listener. A plain click is left alone, so every existing gesture on the
    // card is untouched.
    cardEl.addEventListener("click", (ev) => {
      if (!(ev.ctrlKey || ev.metaKey)) return;
      if (isInteractiveTarget(ev.target)) return;
      if (!key) return;
      // The browser's own Ctrl+click meanings (opening a link, extending a text
      // selection) have nothing to do here, and letting one through would leave
      // the card selected AND a stray text selection across it.
      ev.preventDefault?.();
      toggleValueSelection(key);
    });

    // Renaming the value: click the name to reveal an inline input.
    cardEl.querySelector(".value-name")?.addEventListener("click", () => {
      revealNameInput(cardEl, cat, mainText);
    });

    // Changing the type re-expands the row (a person and an organisation expand
    // differently).
    cardEl.querySelector(".value-type")?.addEventListener("change", async (ev) => {
      const reason = changeValueCategory(cat, mainText, ev.target.value);
      if (reason === "duplicate") notify(WORKSPACE.typeChangeDuplicate(mainText), "warn");
      else await refreshVariants();
    });

    // querySelectorAll, not querySelector: the intersection note carries a
    // second "Group with" button, and both must open the same panel.
    for (const groupBtn of cardEl.querySelectorAll(".value-group")) {
      groupBtn.addEventListener("click", () => togglePanel(key, "group"));
    }

    // The intersection note's own action: the covering term goes on the
    // never-anonymise list, so nothing replaces it and this value wins its text
    // back. Existing mechanism, reached from the card that explains why.
    cardEl.querySelector(".intersection-allow")?.addEventListener("click", (ev) => {
      const term = ev.currentTarget.dataset.term;
      if (!term) return;
      addAllowTerm(term);
      notify(WORKSPACE.intersectionAllowed(term), "ok");
    });

    cardEl.querySelector(".value-solve")?.addEventListener("click", () => {
      togglePanel(key, "solve");
    });

    cardEl.querySelector(".value-remove")?.addEventListener("click", () => {
      if (openValuePanel.key === key) openValuePanel = { key: null, kind: null };
      deleteValue(cat, mainText);
    });

    // Both spelling controls open the SAME popup, which owns the full list and
    // every gesture that manages it. They differ only in where the caret lands:
    // "+ add" is a request to type, "+N more" is a request to look.
    cardEl.querySelector(".spelling-add")?.addEventListener("click", () => {
      openSpellingsPopup(cat, mainText, true);
    });
    cardEl.querySelector(".spelling-more")?.addEventListener("click", () => {
      openSpellingsPopup(cat, mainText, false);
    });

    // The small delete "x" on a visible chip drops that spelling without opening
    // the popup. Deleting one CURATES the value: the remaining chips become its
    // whole list, so the deletion sticks without a negative rule.
    for (const del of cardEl.querySelectorAll(".spelling-del")) {
      del.addEventListener("click", async (ev) => {
        // The chip itself is a drag handle, so the remove button has to stop the
        // click reaching it.
        ev.stopPropagation();
        const gone = del.dataset.spelling;
        deleteVariant(cat, mainText, gone);
        await refreshVariants();
        // Point the user at the ONE place a negative rule lives: this deletion
        // stops the spelling belonging to THIS value, not to every value.
        notify(WORKSPACE.spellingDeleted(gone), "info");
      });
    }

    wireGroupPanel(cardEl, cat, mainText);
    wireSolvePanel(cardEl, cat, mainText);
  }

  if (identityLost && getState().notice?.text !== WORKSPACE.cardIdentityLost) {
    notify(WORKSPACE.cardIdentityLost, "warn");
  }

  wireVariantDrag(container);
}

/** togglePanel(key, kind) opens the group/solve panel on one card, or closes it
 *  if it was already the open one, then repaints. */
function togglePanel(key, kind) {
  openValuePanel = (openValuePanel.key === key && openValuePanel.kind === kind)
    ? { key: null, kind: null }
    : { key, kind };
  setState({});
}

/**
 * applyValuesSearchFilter(container, query) shows or hides the already-rendered
 * value cards to match the search, WITHOUT re-rendering.
 *
 * The bug this fixes: the toolbar used to call setState on every keystroke,
 * which rewrites the whole workspace via innerHTML and destroys the search
 * input mid-type, so focus and the caret were lost after each character. Here
 * the input node is never replaced, so focus is preserved for free. Only
 * structural changes (add/remove/group/type/toggle) still re-render, through
 * their own reducers, and those regenerate the cards from visibleValues.
 *
 * Matching mirrors visibleValues: a card matches when the query is a substring
 * of any of its spellings, read from data-search. Cards are hidden with an
 * inline display:none (an author `.value-card{display:flex}` would override a
 * `[hidden]` attribute or a utility class, and this module cannot touch the
 * CSS). Exported for the render test.
 *
 * @param {HTMLElement} container the workspace element
 * @param {string} [query] the search text (defaults to the live filter)
 * @returns {number} how many cards remain visible
 */
export function applyValuesSearchFilter(container, query = valuesFilter.search) {
  const q = (query ?? "").trim().toLowerCase();
  let visible = 0;
  for (const card of container.querySelectorAll(".value-card")) {
    const hay = card.dataset?.search ?? "";
    const match = !q || hay.includes(q);
    card.style.display = match ? "" : "none";
    if (match) visible++;
  }
  const empty = container.querySelector(".values-search-empty");
  if (empty) empty.style.display = visible === 0 ? "" : "none";
  return visible;
}

/** wireValuesToolbar(container) wires the search, type filter, show/hide
 *  spellings toggle and Clear all. Exported for the focus-preservation test. */
export function wireValuesToolbar(container) {
  wireSearchBox(container, "values-search", (value) => {
    // No setState here: re-rendering would destroy this very input and lose the
    // caret. Update the module-level filter and toggle the rendered rows in
    // place, so the input node survives and keeps focus. That is also why the ✕
    // is hidden by CSS rather than rendered conditionally: there is no repaint
    // here for a conditional render to happen on.
    valuesFilter = { ...valuesFilter, search: value };
    applyValuesSearchFilter(container, value);
  });

  container.querySelector("#values-type")?.addEventListener("change", (ev) => {
    valuesFilter = { ...valuesFilter, type: ev.target.value };
    setState({});
  });

  // One button, two scopes: the picked cards when there are any, otherwise the
  // whole list. It reads the selection at PRESS time rather than trusting its own
  // rendered label, so a repaint between the two cannot make it act on a scope
  // the user was never shown.
  container.querySelector("#btn-clear-values")?.addEventListener("click", async () => {
    if (getState().values.length === 0) return;
    const picked = [...selectedValueKeys];

    if (picked.length) {
      if (!await askConfirm({
        title: WORKSPACE.clearSelected,
        body: WORKSPACE.clearSelectedConfirm(picked.length),
      })) return;
      openValuePanel = { key: null, kind: null };
      spellingsPopup = null;
      selectedValueKeys = new Set();
      const cleared = deleteValues(picked);
      notify(WORKSPACE.clearedN(cleared), cleared ? "ok" : "info");
      return;
    }

    const n = getState().values.length;
    if (!await askConfirm({ title: WORKSPACE.clearAll, body: WORKSPACE.clearAllConfirm(n) })) return;
    openValuePanel = { key: null, kind: null };
    spellingsPopup = null;
    const cleared = clearAllValues();
    notify(WORKSPACE.clearedN(cleared), cleared ? "ok" : "info");
  });
}

/** wireGroupPanel(cardEl, cat, mainText) wires the Group with picker's Apply
 *  and Cancel.
 *
 *  Apply does NOT assume the card's value is the survivor: the merge folds
 *  several values into one, and which one keeps its placeholder is the user's
 *  decision, not a side effect of which card they opened the picker from. So it
 *  asks (askChoice) which participating value becomes the main one, then folds
 *  the rest into it. Cancelling the pick abandons the merge. */
function wireGroupPanel(cardEl, cat, mainText) {
  wireGroupPickerGrid(cardEl);
  cardEl.querySelector(".group-apply")?.addEventListener("click", async () => {
    const sources = [...cardEl.querySelectorAll(".group-pick:checked")].map((cb) => ({
      category: cb.dataset.category, mainText: cb.dataset.mainText,
    }));
    if (sources.length === 0) return;

    // The card's own value participates too: it is one of the suggestions to
    // become the survivor, not automatically the survivor.
    const participants = [{ category: cat, mainText }, ...sources];
    const choices = participants.map((p) => ({
      id: valueKey(p.category, p.mainText),
      label: `${p.mainText} (${categoryLabel(p.category)})`,
    }));
    const mainKey = await askChoice({
      title: WORKSPACE.groupMainTitle, body: WORKSPACE.groupMainBody, choices,
    });
    // Cancelled: repaint (the modal's own close already cleared it) and leave
    // the values untouched.
    if (!mainKey) { setState({}); return; }

    const main = participants.find((p) => valueKey(p.category, p.mainText) === mainKey);
    if (!main) { setState({}); return; }
    const rest = participants.filter((p) => valueKey(p.category, p.mainText) !== mainKey);

    openValuePanel = { key: null, kind: null };
    // groupValues shares the family's ""-or-reason convention, so the COUNT is
    // read from the store: how many rows the merge removed.
    const before = getState().values.length;
    if (groupValues(main, rest) === "") {
      const merged = before - getState().values.length;
      if (merged) notify(WORKSPACE.groupedN(merged, main.mainText), "ok");
    }
    await refreshVariants();
  });
  cardEl.querySelector(".panel-cancel")?.addEventListener("click", () => {
    openValuePanel = { key: null, kind: null };
    setState({});
  });
}

/** wireGroupPickerGrid(cardEl) wires the picker grid's two sort buttons and its
 *  filter field.
 *
 *  Both work on the rendered rows IN PLACE, with no setState: a repaint rebuilds
 *  the checkboxes and would silently drop the ticks the user has already made,
 *  which is the one thing a merge picker must not do. It also keeps the filter
 *  input alive mid-type, so there is no caret to restore. */
function wireGroupPickerGrid(cardEl) {
  const list = cardEl.querySelector(".group-options");
  if (!list) return;

  // Re-sorting is a reorder of the SAME row nodes (appendChild moves a node
  // rather than copying it), which is what preserves their checked state.
  const reorder = () => {
    const rows = [...list.querySelectorAll(".group-row")];
    const keyed = rows.map((row) => ({
      row,
      mainText: row.querySelector(".group-pick")?.dataset.mainText ?? "",
      label: row.querySelector(".fmt-badge")?.textContent ?? "",
    }));
    for (const item of sortGroupRows(keyed, groupSort)) list.appendChild(item.row);
    for (const btn of cardEl.querySelectorAll(".sort-btn")) {
      const active = groupSort.startsWith(btn.dataset.sort);
      btn.classList.toggle("active", active);
      btn.querySelector(".sort-arrow")?.classList.toggle("asc", active && groupSort.endsWith("-asc"));
    }
  };

  for (const btn of cardEl.querySelectorAll(".sort-btn")) {
    btn.addEventListener("click", (ev) => {
      // The label is inside a <label> wrapping nothing clickable here, but the
      // button still sits in a panel whose card selects on click: stop both.
      ev.preventDefault();
      ev.stopPropagation();
      const column = btn.dataset.sort;
      groupSort = groupSort === `${column}-asc` ? `${column}-desc` : `${column}-asc`;
      reorder();
    });
  }

  wireSearchBox(cardEl, "group-filter", (value) => {
    const q = (value ?? "").trim().toLowerCase();
    let visible = 0;
    for (const row of list.querySelectorAll(".group-row")) {
      const match = !q || (row.dataset?.search ?? "").includes(q);
      // An inline display, not [hidden]: .grid-row declares display:grid, which
      // would win over the hidden attribute's UA rule.
      row.style.display = match ? "" : "none";
      if (match) visible++;
    }
    const empty = cardEl.querySelector(".group-no-match");
    if (empty) empty.hidden = visible !== 0;
  });
}

/** wireSolvePanel(cardEl, cat, mainText) wires each resolve action inside the
 *  Solve conflicts panel. */
function wireSolvePanel(cardEl, cat, mainText) {
  for (const action of cardEl.querySelectorAll(".solve-action")) {
    action.addEventListener("click", async () => {
      const { act } = action.dataset;
      openValuePanel = { key: null, kind: null };
      if (act === "remove-value") {
        deleteValue(cat, mainText);
      } else if (act === "remove-allow") {
        // The term comes from the resolution rather than from the card's own
        // main text: they agree today, and reading the stated one is what keeps
        // them agreeing.
        removeAllowTerm(action.dataset.term || mainText);
      } else if (act === "drop-spelling") {
        deleteVariant(cat, mainText, action.dataset.spelling);
        await refreshVariants();
      } else if (act === "merge") {
        groupValues(
          { category: cat, mainText },
          [{ category: action.dataset.withcategory, mainText: action.dataset.withvalue }]);
        await refreshVariants();
      }
    });
  }
}

/**
 * wireVariantDrag(container) makes a spelling chip draggable onto another value
 * card, which regroups it.
 *
 * The payload is carried in a module-local variable rather than only in the
 * DataTransfer, because a WebView's dragover handler cannot read DataTransfer
 * contents (the spec hides them until the drop), and the drop target needs to
 * know which card the drag STARTED from so it can refuse a drop onto itself.
 */
function wireVariantDrag(container) {
  for (const chip of container.querySelectorAll(".spelling-chip")) {
    chip.addEventListener("dragstart", (ev) => {
      const card = chip.closest(".value-card");
      dragging = {
        spelling: chip.dataset.spelling,
        fromCategory: card?.dataset.category,
        fromMainText: card?.dataset.mainText,
      };
      // Setting the data is still required, or some WebViews cancel the drag.
      ev.dataTransfer?.setData("text/plain", chip.dataset.spelling ?? "");
      if (ev.dataTransfer) ev.dataTransfer.effectAllowed = "move";
    });
    chip.addEventListener("dragend", () => { dragging = null; });
  }

  for (const card of container.querySelectorAll(".value-card")) {
    card.addEventListener("dragover", (ev) => {
      if (!dragging) return;
      if (card.dataset.mainText === dragging.fromMainText &&
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
      const moved = moveSpelling(
        dragging.fromCategory, dragging.fromMainText,
        card.dataset.category, card.dataset.mainText, dragging.spelling);
      const { spelling } = dragging;
      dragging = null;
      if (!moved) return; // a stale drop: the reducer refused it, say nothing
      notify(WORKSPACE.spellingMoved(spelling, card.dataset.mainText), "ok");
      await refreshVariants();
    });
  }
}

/**
 * revealNameInput(cardEl, category, mainText, key) swaps the value name for an
 * inline input, so a mis-detected value can be corrected in place. It commits
 * through renameValue, which re-expands the row.
 *
 * Like the spelling input, it is transient DOM: no state change reveals it, and
 * a repaint puts the read-only name back.
 */
function revealNameInput(cardEl, category, mainText) {
  const nameEl = cardEl.querySelector(".value-name");
  if (!nameEl) return;

  const input = cardEl.ownerDocument.createElement("input");
  input.className = "value-name-input";
  input.value = mainText;
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
    if (!value || value === mainText) {
      setState({}); // repaint puts the name back unchanged
      return;
    }
    const reason = renameValue(category, mainText, value);
    if (reason === "duplicate") {
      // The notice strip, not a line on the card: a line that appears and clears
      // changes the card's height, which is exactly what the fixed-height card
      // exists to stop.
      notify(WORKSPACE.valueRenamedDuplicate(value), "warn");
      setState({});
      return;
    }
    await refreshVariants();
  };

  input.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") commit();
    else if (ev.key === "Escape") { done = true; setState({}); }
  });
  input.addEventListener("blur", commit);
}

/**
 * deleteVariant(category, mainText, spelling) removes one spelling from a
 * value.
 *
 * It CURATES the row: the remaining chips become the value's whole list and the
 * automatic expansion stops applying. That is what makes the deletion stick.
 * Deleting from an auto-expanding list would look like the button did nothing,
 * because the next expansion would derive the same spelling again.
 *
 * It does NOT trigger a re-expansion, and there is nothing left to expand: a
 * curated row's list is settled. The value list's scroll survives the repaint
 * because scroll offsets are preserved centrally (scroll.js).
 */
function deleteVariant(category, mainText, spelling) {
  const s = getState();
  const lower = spelling.toLowerCase();
  setState({
    values: s.values.map((e) => {
      if (valueKey(e.category, e.mainText) !== valueKey(category, mainText)) return e;
      return curate(e, [...spellingsOf(e).values()].filter((x) => x.toLowerCase() !== lower));
    }),
  });
}

/** spellingCount(category, mainText) is how many spellings a value carries right
 *  now, automatic and manual together. The add flow compares it before and after
 *  so a duplicate gets an explanation instead of silence. */
function spellingCount(category, mainText) {
  const e = getState().values.find(
    (x) => valueKey(x.category, x.mainText) === valueKey(category, mainText));
  if (!e) return 0;
  return (e.derivedSpellings?.length ?? 0) + (e.spellings?.length ?? 0);
}

/**
 * refreshVariants() asks Go to expand every value whose derivedSpellings are PENDING
 * (derivedSpellings === null: just added, edited, or spelling-amended). Settled rows,
 * including "expanded, none found" ([]), are never re-expanded.
 *
 * Sequential on purpose: the lists are tiny and ordering keeps the UI
 * deterministic.
 */
async function refreshVariants() {
  // The snapshot is taken ONCE, before any await. Every expansion below writes
  // to the store and therefore repaints, and re-reading the list mid-loop would
  // mean expanding rows a repaint had already settled.
  const pending = pendingExpansions(getState().values);
  for (const e of pending) {
    try {
      const derivedSpellings = await expandSpellings({
        category: e.category, mainText: e.mainText,
        spellings: e.spellings,
        spellingPolicy: e.spellingPolicy === "curated" ? "curated" : "automatic",
      });
      setValueSpellings(e.category, e.mainText, derivedSpellings ?? []);
    } catch (err) {
      // A failure becomes a VISIBLE error on the row, and settles it, so the
      // placeholder cannot spin forever and the row is not retried on every
      // repaint.
      setValueSpellingError(e.category, e.mainText, String(err?.message ?? err));
    }
  }
  return pending.length;
}

// --- The intersection recheck ---------------------------------------------

// The pending recheck timer. One at a time, last call wins: a user editing a
// value list produces a burst of changes, and asking Go to re-scan every
// document on each keystroke would make the screen stutter for an answer that
// is about to be superseded.
let intersectionTimer = null;

// How long to wait for the edits to stop. Long enough to swallow a burst of
// typing, short enough that the warning arrives while the user is still looking
// at the card it belongs to.
const INTERSECTION_DEBOUNCE_MS = 400;

/**
 * scheduleIntersectionCheck() asks Go, once the edits settle, which values
 * another detection route also claims.
 *
 * The debounce lives here rather than in state.js because it is about how this
 * screen talks to Go, not about what the store holds: the store simply clears
 * the warnings whenever the value list changes, and this puts the fresh answer
 * back when there is one.
 *
 * A missing bridge (a plain browser, the render tests) leaves the list empty
 * and the screen unchanged. It must never throw: an unhandled rejection here
 * would take the whole repaint down and blank the screen.
 */
function scheduleIntersectionCheck() {
  if (intersectionTimer) clearTimeout(intersectionTimer);
  intersectionTimer = setTimeout(async () => {
    intersectionTimer = null;
    try {
      const res = await checkIntersections(buildIntersectionRequest());
      setIntersections(res?.intersections ?? []);
    } catch {
      // No bridge, or Go refused. There is nothing to tell the user: this is a
      // warning they never asked for, so its absence is not a failure.
      setIntersections([]);
    }
  }, INTERSECTION_DEBOUNCE_MS);
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
      removePattern(row.dataset.expr);
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

// views/identifyworkspace.js, the WORKSPACE of wizard step 2, Identify
// (BUILD-02 Phase 9 as views/values.js; renamed by BUILD-05 Phase 2 and
// relaid out into the mock-up's four tabs by BUILD-05 Phase 6).
//
// Three discovery
// methods (cloud placeholder / local AI / Smart detection), ONE unified
// candidate review list (nothing reaches the value tables without an
// explicit Accept), live match preview for manual entries, and variant
// drag-and-drop regrouping. FULLY usable without Ollama: Smart detection
// and the manual add rows carry the whole flow.
//
// Naming note (BUILD-04 CR3, restated by BUILD-05 Phase 0): the visible
// labels have changed twice now, from "Entities" to "Values" to this half of
// "Identify". The ENGINE identifiers this module manipulates (the category
// keys client_names, person_names, ... and the state.entities array) have not
// changed once, on purpose: a label is a display string, an identifier is a
// contract.
//
// Render-from-state discipline: every mutation goes through a state.js
// reducer; every Go call goes through api.js.

import {
  runDiscovery, cancelDiscovery, estimateDiscovery, runSmartDetection,
  countTermMatches, expandVariants, validatePattern, patternMatches,
} from "../api.js";
import {
  getState, setState, llmEnabled,
  addEntities, setEntityStatus, editEntity, removeEntity,
  setEntityVariants, setEntityVariantError, addManualVariant, entityKey,
  addCandidates, acceptCandidate, rejectCandidate, updateCandidate,
  acceptAllInCategory, denyAllInCategory, moveVariant,
  addPattern, removePattern,
  setSmartDetectOptions, smartDetectOptions,
} from "../state.js";
import { variantRows, pendingExpansions } from "../entitymodel.js";
import {
  visibleCandidates, candidateCategoryCounts,
  toggleCountSort, toggleValueSort, DEFAULT_CANDIDATE_FILTER,
} from "../candidatemodel.js";
import { escapeHTML } from "../html.js";
import { panel, wirePanels, button } from "../ui.js";
import { llmGateTooltip } from "./identifyrail.js";
import { renderAllowlistPanel, wireAllowlistPanel } from "./allowlist.js";
import { keepScrollPosition } from "../scroll.js";
import { VALUES } from "../copy.js";

// The reviewable entity categories (CLAUDE.md §5) with display labels.
const CATEGORIES = [
  ["client_names", "Clients"],
  ["project_names", "Projects"],
  ["internal_names", "Internal"],
  ["person_names", "Persons"],
];

// Rows whose variant list is expanded, keyed by entityKey. View-local UI
// state (not business state), so it lives here, not in the store.
//
// The key is (category, canonical), so ANY change to a value's name
// changes its key. Renaming used to orphan the key and silently collapse
// the row the user was working in (BUILD-04 CR17), which is why every
// rename now goes through renameExpanded below.
const expanded = new Set();

/**
 * renameExpanded(category, oldCanonical, newCanonical) carries a row's
 * expanded state across a rename, so editing a value does not fold away
 * the variant list the user just opened (BUILD-04 CR17).
 */
function renameExpanded(category, oldCanonical, newCanonical) {
  const oldKey = entityKey(category, oldCanonical);
  if (!expanded.has(oldKey)) return;
  expanded.delete(oldKey);
  expanded.add(entityKey(category, newCanonical));
}

/**
 * openExpanded(category, canonical) makes sure a row is expanded. Called
 * whenever the user does something to a row's variants, because acting on
 * a list and having it fold shut is the opposite of what they asked for.
 */
function openExpanded(category, canonical) {
  expanded.add(entityKey(category, canonical));
}

// Panels the user toggled away from their default open/collapsed state
// (BUILD-02 Phase 2f). View-local, same pattern as `expanded`.
const collapsedPanels = new Set();

// The suggestions table's search / category / sort selection
// (BUILD-04 CR14). Which column a user is sorting by is a VIEW
// preference, not application state, so it lives here next to `expanded`
// rather than in the store. candidatemodel.js turns it into rows.
let candidateFilter = { ...DEFAULT_CANDIDATE_FILTER };

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
  // Discovery gates on the master AI toggle AND live availability
  // (BUILD-02 Phase 6d).
  const aiOK = llmEnabled(s);

  container.innerHTML = `
    <div class="values-view">
      ${discoveryPanel(s, aiOK)}
      ${candidatesPanel(s)}
      ${CATEGORIES.map(([cat, label]) => categoryPanel(s, cat, label)).join("")}
      ${renderAllowlistPanel(s, collapsedPanels)}
      ${patternsPanel(s)}
      <div id="values-error"></div>
    </div>
    ${opts.footerHTML ?? ""}
  `;

  wirePanels(container, collapsedPanels, () => setState({}));
  wireDiscovery(container, s);
  wireCandidates(container);
  wireCategoryPanels(container);
  wireAllowlistPanel(container);
  wirePatterns(container);
}

// --- Discovery methods (BUILD-02 Phase 9a) -------------------------------

function discoveryPanel(s, aiOK) {
  const fileOptions = s.documents.map((d) => `
    <label class="radio-row">
      <input type="checkbox" class="disc-file" value="${escapeHTML(d.name)}"/>
      <span>${escapeHTML(d.name)}</span>
    </label>`).join("");

  // Determinate per-file progress + Cancel while a run is live
  // (BUILD-02 Phase 7c/7d).
  // The discovery gate is deliberately `=== true` and nothing else
  // (BUILD-04 CR17): the Smart button must depend on a run being in
  // flight, never on a pending variant expansion or a leftover object.
  const d = s.discovery;
  const pct = d?.running === true && d.total ? Math.round(((d.current + 1) / d.total) * 100) : 0;
  const progressHTML = d?.running === true ? `
      <div class="progress-bar"><div style="width:${pct}%"></div></div>
      <p class="hint">Scanning ${escapeHTML(d.file ?? "")} (${d.current + 1}/${d.total})
        <button id="btn-disc-cancel">Cancel</button></p>` : "";

  // Three methods (Phase 9a). The local-AI method is only VISIBLE when
  // the master toggle is on; the cloud method is a disabled placeholder.
  const localAIButton = s.settings.useAI ? `
      <div class="form-row">
        <button id="btn-discover" class="primary" ${aiOK && !d?.running ? "" : `disabled title="${escapeHTML(llmGateTooltip(s))}"`}>
          Auto-discovery with local AI</button>
        <span class="hint">Reads the selected files with the local Ollama model and suggests names.</span>
      </div>` : "";

  const content = `
      <p class="hint">Pick one or more files below, then choose a method. Every suggestion goes
        to the review list first; nothing is replaced without your approval.</p>
      ${fileOptions || `<p class="hint">No documents imported.</p>`}
      <div class="form-row">
        <button class="btn btn-ghost" disabled title="Not available yet.">Auto-discovery with cloud AI</button>
        <span class="hint">Not available yet.</span>
      </div>
      ${localAIButton}
      <div class="form-row">
        <button id="btn-smart" ${d?.running === true ? "disabled" : ""}>Smart detection</button>
        <span class="hint">Works without any AI. Finds likely names by how they are written.</span>
      </div>
      ${smartSettingsHTML(s)}
      ${progressHTML}
      <div id="disc-progress" class="hint"></div>`;
  return panel("panel-discovery", "Find names to replace", content, {
    collapsible: true, collapsedSet: collapsedPanels,
  });
}

/**
 * smartSettingsHTML(s) renders the Smart-detection tuning controls
 * (BUILD-04 CR13). Smart detection was surfacing far more values than
 * anyone could review, so these four controls each remove a distinct kind
 * of noise. They ship at the stricter defaults; every one of them can be
 * turned off to get the old, exhaustive behaviour back.
 */
function smartSettingsHTML(s) {
  const o = smartDetectOptions(s);
  return `
      <details class="smart-settings" id="smart-settings">
        <summary>${escapeHTML(VALUES.smartSettingsTitle)}</summary>
        <p class="hint">${escapeHTML(VALUES.smartSettingsHint)}</p>
        <div class="form-row">
          <label for="smart-min-length">${escapeHTML(VALUES.smartMinLength)}</label>
          <input id="smart-min-length" type="number" min="0" max="40" step="1" value="${o.minLength}"/>
          <span class="hint">${escapeHTML(VALUES.smartMinLengthHint)}</span>
        </div>
        <div class="form-row">
          <label for="smart-min-occurrences">${escapeHTML(VALUES.smartMinOccurrences)}</label>
          <input id="smart-min-occurrences" type="number" min="0" max="100" step="1" value="${o.minOccurrences}"/>
          <span class="hint">${escapeHTML(VALUES.smartMinOccurrencesHint)}</span>
        </div>
        <label class="radio-row">
          <input type="checkbox" id="smart-common-words" ${o.excludeCommonWords ? "checked" : ""}/>
          <span>${escapeHTML(VALUES.smartCommonWords)}
            <span class="hint">${escapeHTML(VALUES.smartCommonWordsHint)}</span></span>
        </label>
        <div class="form-row">
          <label for="smart-min-confidence">${escapeHTML(VALUES.smartMinConfidence)}</label>
          <input id="smart-min-confidence" type="range" min="0" max="100" step="5"
                 value="${Math.round(o.minConfidence * 100)}"/>
          <output id="smart-min-confidence-value">${Math.round(o.minConfidence * 100)}</output>
          <span class="hint">${escapeHTML(VALUES.smartMinConfidenceHint)}</span>
        </div>
      </details>`;
}

/** selectedFiles(container) reads the shared file-checkbox list. */
function selectedFiles(container) {
  return [...container.querySelectorAll(".disc-file:checked")].map((c) => c.value);
}

function wireDiscovery(container, s) {
  container.querySelector("#btn-disc-cancel")?.addEventListener("click", () => cancelDiscovery());
  wireSmartSettings(container);

  const feedback = (msg) => {
    const slot = document.querySelector("#disc-progress");
    if (slot) slot.textContent = msg;
  };

  // Local AI discovery: proposals land in the REVIEW list, never
  // directly in the entity tables (Phase 9b behaviour change).
  const aiBtn = container.querySelector("#btn-discover");
  if (aiBtn && !aiBtn.disabled) {
    aiBtn.addEventListener("click", async () => {
      let files = selectedFiles(container);
      const progress = container.querySelector("#disc-progress");
      if (files.length === 0) {
        progress.textContent = "Select at least one file first.";
        return;
      }
      // Pre-run size safeguard (Phase 7e): oversized files are excluded
      // with the actionable message, never a mid-run hard error.
      try {
        const estimates = await estimateDiscovery(files);
        const tooLarge = (estimates ?? []).filter((e) => e.tooLarge);
        if (tooLarge.length) {
          progress.textContent = tooLarge.map((e) => e.message).join(" ");
          const excluded = new Set(tooLarge.map((e) => e.name));
          files = files.filter((f) => !excluded.has(f));
          if (files.length === 0) return;
        }
      } catch { /* estimation is advisory; the run still guards itself */ }

      setState({ discovery: { running: true, current: 0, total: files.length, file: files[0] } });
      try {
        const res = await runDiscovery(files, s.allowlist);
        const added = addCandidates(
          (res?.proposals ?? []).map((p) => ({ text: p.text, category: p.category })), "local-ai");
        feedback(`${res?.status ?? "done"}. ${added} new candidate(s) for review below.`);
      } catch (err) {
        showError(container, err);
      } finally {
        setState({ discovery: null }); // see the Smart path (BUILD-04 CR17)
      }
    });
  }

  // Smart detection: always available, no AI required. Categories are
  // refined by the local AI only when it is enabled AND reachable.
  container.querySelector("#btn-smart")?.addEventListener("click", async () => {
    const files = selectedFiles(container);
    const progress = container.querySelector("#disc-progress");
    if (files.length === 0) {
      progress.textContent = "Select at least one file first.";
      return;
    }
    setState({ discovery: { running: true, current: 0, total: files.length, file: files[0] } });
    try {
      // The tuning travels with the call (BUILD-04 CR13); it is read from
      // the store, so the options in force are exactly the ones shown.
      const res = await runSmartDetection(
        files, s.allowlist, llmEnabled(s), smartDetectOptions(getState()));
      const added = addCandidates(res?.candidates ?? [], "smart");
      feedback(`${res?.status ?? "done"}. ${added} new candidate(s) for review below.`);
    } catch (err) {
      showError(container, err);
    } finally {
      // ALWAYS clear the running flag (BUILD-04 CR17). Clearing it only on
      // the two happy-ish paths meant any escape in between, an exception
      // from a reducer for instance, left the button disabled forever with
      // no way back except leaving and re-entering the step.
      setState({ discovery: null });
    }
  });
}

/**
 * wireSmartSettings(container) commits each tuning control to the store
 * (BUILD-04 CR13). Every commit is wrapped in keepScrollPosition, because
 * the settings block sits mid-screen and the re-render would otherwise
 * throw the user back to the top (CR12).
 *
 * The <details> block keeps its open state across re-renders by hand: the
 * store deliberately knows nothing about it, and losing it on every
 * keystroke would make the controls unusable.
 */
function wireSmartSettings(container) {
  const details = container.querySelector("#smart-settings");
  if (!details) return;
  details.open = smartSettingsOpen;
  details.addEventListener("toggle", () => { smartSettingsOpen = details.open; });

  const commit = (patch) => keepScrollPosition(() => setSmartDetectOptions(patch));

  container.querySelector("#smart-min-length")?.addEventListener("change", (ev) => {
    commit({ minLength: parseInt(ev.target.value, 10) });
  });
  container.querySelector("#smart-min-occurrences")?.addEventListener("change", (ev) => {
    commit({ minOccurrences: parseInt(ev.target.value, 10) });
  });
  container.querySelector("#smart-common-words")?.addEventListener("change", (ev) => {
    commit({ excludeCommonWords: ev.target.checked });
  });

  const confidence = container.querySelector("#smart-min-confidence");
  if (confidence) {
    const readout = container.querySelector("#smart-min-confidence-value");
    // Live read-out while dragging, one commit on release.
    confidence.addEventListener("input", () => {
      if (readout) readout.textContent = confidence.value;
    });
    confidence.addEventListener("change", () => {
      commit({ minConfidence: Number(confidence.value) / 100 });
    });
  }
}

// Whether the smart-detection settings block is unfolded. View-local, in
// the same family as `expanded` and `collapsedPanels`.
let smartSettingsOpen = false;

// --- Candidate review (BUILD-02 Phase 9b) ---------------------------------

const CANDIDATE_CATEGORIES = [
  ["client_names", "Client"],
  ["project_names", "Project"],
  ["internal_names", "Internal"],
  ["person_names", "Person"],
];

function candidatesPanel(s) {
  if (!s.candidates.length) return "";

  // The rows the user can currently SEE, from the tested pure model.
  // Everything below (the bulk buttons, the empty state, the counts) is
  // derived from this list, so what a button does always matches what the
  // table shows (BUILD-04 CR14/CR15).
  const rows = visibleCandidates(s.candidates, candidateFilter);
  const counts = candidateCategoryCounts(rows);

  const options = (selected) => CANDIDATE_CATEGORIES.map(([v, l]) =>
    `<option value="${v}" ${v === selected ? "selected" : ""}>${l}</option>`).join("");

  // Category selector: only the NAME categories the user actually
  // enabled in Configure (CR14), so the filter reflects their choices
  // instead of offering categories that cannot appear.
  const enabled = CANDIDATE_CATEGORIES.filter(([v]) => s.settings.categories?.[v]);
  const filterCategories = enabled.length ? enabled : CANDIDATE_CATEGORIES;
  const categoryOptions = [`<option value="">${escapeHTML(VALUES.filterAllTypes)}</option>`]
    .concat(filterCategories.map(([v, l]) =>
      `<option value="${v}" ${v === candidateFilter.category ? "selected" : ""}>${escapeHTML(l)}</option>`))
    .join("");

  // Bulk actions ABOVE the table (CR15), one pair per category present in
  // the visible rows, each naming exactly how many rows it will touch.
  const bulk = CANDIDATE_CATEGORIES
    .filter(([v]) => counts[v] > 0)
    .map(([v, l]) => `
      <span class="bulk-pair">
        <button class="cand-accept-all" data-category="${v}"
                title="${escapeHTML(VALUES.bulkScopeHint)}">Accept all ${counts[v]} ${escapeHTML(l.toLowerCase())}${counts[v] === 1 ? "" : "s"}</button>
        <button class="cand-deny-all" data-category="${v}"
                title="${escapeHTML(VALUES.bulkScopeHint)}">Deny all ${counts[v]} ${escapeHTML(l.toLowerCase())}${counts[v] === 1 ? "" : "s"}</button>
      </span>`).join(" ");

  const countArrow = candidateFilter.sort === "count-asc" ? "\u25B2"
    : candidateFilter.sort === "count-desc" ? "\u25BC" : "";
  const valueArrow = candidateFilter.sort === "value-asc" ? "\u25B2"
    : candidateFilter.sort === "value-desc" ? "\u25BC" : "";

  const body = rows.length ? rows.map((c) => `
    <tr data-ctext="${escapeHTML(c.text)}">
      <td class="ent-name" title="${escapeHTML((c.contexts ?? []).join("\n"))}">
        <input class="cand-text" value="${escapeHTML(c.text)}"/>
      </td>
      <td><select class="cand-category">${options(c.category)}</select></td>
      <td class="hint">${c.count ? `${c.count}` : ""}</td>
      <td class="hint">${escapeHTML(c.source)}</td>
      <td class="ent-actions">
        <button class="cand-accept">accept</button>
        <button class="cand-reject">reject</button>
      </td>
    </tr>`).join("")
    : `<tr><td colspan="5" class="hint">${escapeHTML(VALUES.noMatchingSuggestions)}</td></tr>`;

  const content = `
      <p class="hint">Review each suggestion: fix the value or its type if needed, then accept or
        reject. Only accepted values are ever replaced.</p>
      <div class="form-row bulk-row">${bulk}</div>
      <table class="entity-table candidate-table">
        <thead>
          <tr>
            <th>
              <button class="cand-sort" data-sort="value" title="${escapeHTML(VALUES.sortValueHint)}">
                ${escapeHTML(VALUES.colValue)} ${valueArrow}</button>
            </th>
            <th>${escapeHTML(VALUES.colType)}</th>
            <th>
              <button class="cand-sort" data-sort="count" title="${escapeHTML(VALUES.sortCountHint)}">
                ${escapeHTML(VALUES.colOccurrences)} ${countArrow}</button>
            </th>
            <th>${escapeHTML(VALUES.colFoundBy)}</th>
            <th>${escapeHTML(VALUES.colActions)}</th>
          </tr>
          <tr class="filter-row">
            <th><input id="cand-search" type="search" placeholder="${escapeHTML(VALUES.searchPlaceholder)}"
                       value="${escapeHTML(candidateFilter.search)}"/></th>
            <th><select id="cand-filter-category">${categoryOptions}</select></th>
            <th colspan="3" class="hint">${escapeHTML(showingLabel(rows.length, s.candidates.length))}</th>
          </tr>
        </thead>
        <tbody>${body}</tbody>
      </table>`;
  return panel("panel-candidates", `Suggestions to review (${s.candidates.length})`, content, {
    collapsible: true, collapsedSet: collapsedPanels,
  });
}

/** showingLabel(visible, total) states how much of the list is on screen,
 *  so a filter can never silently hide rows the user forgets about. */
export function showingLabel(visible, total) {
  if (visible === total) return `Showing all ${total}.`;
  return `Showing ${visible} of ${total}.`;
}

function wireCandidates(container) {
  for (const row of container.querySelectorAll("tr[data-ctext]")) {
    const original = row.dataset.ctext;
    const textInput = row.querySelector(".cand-text");
    const catSelect = row.querySelector(".cand-category");
    textInput.addEventListener("change", () => keepScrollPosition(() => {
      if (!updateCandidate(original, { text: textInput.value })) {
        textInput.value = original; // collision or blank: revert visibly
      }
    }));
    catSelect.addEventListener("change", () => keepScrollPosition(
      () => updateCandidate(original, { category: catSelect.value })));
    row.querySelector(".cand-accept").addEventListener("click", async () => {
      if (!acceptCandidate(row.dataset.ctext)) return;
      await refreshVariants();
      repaintValues();
    });
    row.querySelector(".cand-reject").addEventListener("click", () => rejectCandidate(row.dataset.ctext));
  }

  // Per-column filters (CR14). They change VIEW state only, then ask for
  // a repaint; the scroll position is carried across it.
  const search = container.querySelector("#cand-search");
  if (search) {
    let timer = null;
    search.addEventListener("input", () => {
      // Debounced: retyping a search must not repaint on every keystroke.
      clearTimeout(timer);
      timer = setTimeout(() => {
        candidateFilter = { ...candidateFilter, search: search.value };
        keepScrollPosition(() => setState({}));
        // The re-render replaced the input, so put the caret back.
        const next = document.querySelector("#cand-search");
        if (next) {
          next.focus();
          next.setSelectionRange(next.value.length, next.value.length);
        }
      }, 200);
    });
  }
  container.querySelector("#cand-filter-category")?.addEventListener("change", (ev) => {
    candidateFilter = { ...candidateFilter, category: ev.target.value };
    keepScrollPosition(() => setState({}));
  });
  for (const btn of container.querySelectorAll(".cand-sort")) {
    btn.addEventListener("click", () => {
      candidateFilter = {
        ...candidateFilter,
        sort: btn.dataset.sort === "count"
          ? toggleCountSort(candidateFilter.sort)
          : toggleValueSort(candidateFilter.sort),
      };
      keepScrollPosition(() => setState({}));
    });
  }

  // Bulk accept / deny (CR15). Both act on the VISIBLE rows only, which
  // is what the button labels count, so a filtered list can never hide
  // rows a bulk action silently swept up.
  const visibleTexts = (category) => visibleCandidates(getState().candidates, candidateFilter)
    .filter((c) => c.category === category)
    .map((c) => c.text);

  for (const btn of container.querySelectorAll(".cand-accept-all")) {
    btn.addEventListener("click", async () => {
      const category = btn.dataset.category;
      if (acceptAllInCategory(category, visibleTexts(category)) === 0) return;
      await refreshVariants();
      repaintValues();
    });
  }
  for (const btn of container.querySelectorAll(".cand-deny-all")) {
    btn.addEventListener("click", () => {
      const category = btn.dataset.category;
      const texts = visibleTexts(category);
      if (!texts.length) return;
      if (!confirm(VALUES.denyAllConfirm(texts.length))) return;
      keepScrollPosition(() => denyAllInCategory(category, texts));
    });
  }
}

// --- Review tables ------------------------------------------------------

function categoryPanel(s, category, label) {
  // Variant row content comes from the TESTED pure view-model
  // (entitymodel.js): pending / error / empty / list are explicit states,
  // so the "expanding" placeholder can never stick forever (Phase 7a).
  const vrowsByKey = new Map(
    variantRows(s.entities, expanded).map((r) => [r.key, r]));

  const rows = s.entities.filter((e) => e.category === category).map((e) => {
    const key = entityKey(e.category, e.canonical);
    const vrow = vrowsByKey.get(key);
    const isOpen = !!vrow;
    const variantCount = e.variants?.length;
    const countLabel = e.variants === null || e.variants === undefined
      ? "…" : `${variantCount} variant${variantCount === 1 ? "" : "s"}`;

    let variantBody = "";
    if (vrow) {
      // draggable="true" on the chip, and draggable="false" on the button
      // inside it: a draggable child would otherwise swallow the drag that
      // starts on the button, which is most of the chip's width
      // (BUILD-04 CR17, "chips undraggable").
      const chips = vrow.variants.map((v) => `
        <span class="pill variant-chip" draggable="true" data-variant="${escapeHTML(v)}"
              data-category="${escapeHTML(e.category)}" data-canonical="${escapeHTML(e.canonical)}"
              title="Drag onto another value to move this variant there.">
          <span class="icon drag-handle" aria-hidden="true">\u283F</span>${escapeHTML(v)}
          <button class="variant-move" draggable="false"
                  title="Move this variant to another value">move</button>
        </span>`).join(" ");
      const inner =
        vrow.state === "pending" ? `<span class="hint">expanding…</span>` :
        vrow.state === "error" ? `<span class="banner error">Variant expansion failed: ${escapeHTML(vrow.error)}</span>` :
        vrow.state === "empty" ? `<span class="hint">No variants</span>` :
        `<span class="pill-list">${chips}</span>`;
      // The variant row carries ITS OWN data attributes; the old
      // previousElementSibling coupling broke silently on any markup
      // change between the rows (the Phase 7a bug).
      variantBody = `
      <tr class="variant-row" data-key="${escapeHTML(key)}"
          data-category="${escapeHTML(e.category)}" data-canonical="${escapeHTML(e.canonical)}">
        <td colspan="3">
        <div class="variant-list">${inner}</div>
        <div class="form-row">
          <input class="variant-input" placeholder="add a spelling, for example a nickname"/>
          <button class="variant-add">Add variant</button>
        </div>
        <div class="variant-note hint"></div>
      </td></tr>`;
    }

    return `
      <tr class="${e.status === "denied" ? "denied" : ""}" data-key="${escapeHTML(key)}"
          data-category="${escapeHTML(e.category)}" data-canonical="${escapeHTML(e.canonical)}">
        <td class="ent-name">${escapeHTML(e.canonical)}</td>
        <td>
          <button class="ent-variants" title="Show the name variants that will be replaced">
            ${countLabel} ${isOpen ? "▾" : "▸"}
          </button>
        </td>
        <td class="ent-actions">
          <button class="ent-accept" ${e.status === "accepted" ? "disabled" : ""}>accept</button>
          <button class="ent-deny" ${e.status === "denied" ? "disabled" : ""}>deny</button>
          <button class="ent-edit">edit</button>
          <button class="ent-delete">✕</button>
        </td>
      </tr>${variantBody}`;
  }).join("");

  const content = `
    <div data-panel="${category}">
      <table class="entity-table">
        <tbody>${rows}</tbody>
        <tfoot><tr>
          <td colspan="3">
            <div class="form-row">
              <input class="ent-add-input" placeholder="add a ${label.toLowerCase()} entry"/>
              <button class="ent-add">Add</button>
            </div>
            <div class="ent-add-preview hint"></div>
          </td>
        </tr></tfoot>
      </table>
    </div>`;
  return panel(`panel-cat-${category}`, label, content, {
    collapsible: true, collapsedSet: collapsedPanels,
  });
}

function wireCategoryPanels(container) {
  for (const panel of container.querySelectorAll("[data-panel]")) {
    const category = panel.dataset.panel;

    // Manual add row, the whole flow in no-Ollama mode.
    const addBtn = panel.querySelector(".ent-add");
    const addInput = panel.querySelector(".ent-add-input");
    const add = async () => {
      const typed = addInput.value.trim();
      if (!typed) return;
      if (addEntities([{ category, canonical: typed }]) === 0) return;
      // Open the new row straight away: its variants are what the user
      // needs to check, and hiding them behind a second click was half of
      // the "add works only once" report (BUILD-04 CR17).
      openExpanded(category, typed);
      await refreshVariants();
      repaintValues();
    };
    addBtn.addEventListener("click", add);
    addInput.addEventListener("keydown", (ev) => { if (ev.key === "Enter") add(); });

    // Live match preview (BUILD-02 Phase 9c): debounced count of the
    // typed term across the loaded documents, shown before committing.
    const preview = panel.querySelector(".ent-add-preview");
    let previewTimer = null;
    addInput.addEventListener("input", () => {
      clearTimeout(previewTimer);
      const term = addInput.value.trim();
      if (!term) { preview.textContent = ""; return; }
      previewTimer = setTimeout(async () => {
        try {
          const info = await countTermMatches(term);
          preview.textContent = info?.count
            ? `Found ${info.count} time(s) in ${info.documents} document(s).`
            : "Not found in the loaded documents.";
        } catch { preview.textContent = ""; }
      }, 300);
    });

    for (const row of panel.querySelectorAll("tr[data-key]")) {
      const { category: cat, canonical } = row.dataset;
      row.querySelector(".ent-accept").addEventListener("click", () => setEntityStatus(cat, canonical, "accepted"));
      row.querySelector(".ent-deny").addEventListener("click", () => setEntityStatus(cat, canonical, "denied"));
      row.querySelector(".ent-delete").addEventListener("click", () => {
        expanded.delete(entityKey(cat, canonical)); // no orphan keys behind a deleted row
        removeEntity(cat, canonical);
      });
      row.querySelector(".ent-edit").addEventListener("click", async () => {
        const next = prompt("Edit the value:", canonical);
        if (next === null) return;
        if (!editEntity(cat, canonical, next)) {
          showError(container, new Error(
            `"${next.trim()}" cannot be used: it is empty, or another value in this list already has that name.`));
          return;
        }
        // The key changed, so the open row has to follow it (CR17).
        renameExpanded(cat, canonical, next.trim());
        await refreshVariants();
        repaintValues();
      });
      row.querySelector(".ent-variants").addEventListener("click", async () => {
        const key = row.dataset.key;
        if (expanded.has(key)) expanded.delete(key); else expanded.add(key);
        // Repaint UNCONDITIONALLY. `expanded` is view-local, so opening a
        // row whose variants are already known changes nothing the store
        // would notify about, and the row used to open invisibly until
        // some unrelated action repainted the step (BUILD-04 CR17).
        repaintValues();
        // Then fill in anything still missing; each expansion repaints
        // again as it settles.
        await refreshVariants();
      });
    }

    for (const vrow of panel.querySelectorAll(".variant-row")) {
      // Explicit data attributes ON the variant row itself (Phase 7a);
      // no positional coupling to the entity row above.
      const { category: cat, canonical } = vrow.dataset;
      const input = vrow.querySelector(".variant-input");
      const note = vrow.querySelector(".variant-note");
      const addVariant = async () => {
        const typed = input.value.trim();
        // Say what happened. Adding a blank or a duplicate used to change
        // no state, so nothing repainted and the button looked broken
        // (BUILD-04 CR17).
        if (!typed) {
          if (note) note.textContent = "Type a spelling first, for example a nickname or an abbreviation.";
          return;
        }
        const before = currentVariantCount(cat, canonical);
        addManualVariant(cat, canonical, typed);
        if (currentVariantCount(cat, canonical) === before) {
          if (note) note.textContent = `"${typed}" is already one of this value's variants.`;
          return;
        }
        // Keep the row open across the re-expansion (CR17).
        openExpanded(cat, canonical);
        await refreshVariants();
        repaintValues();
      };
      vrow.querySelector(".variant-add").addEventListener("click", addVariant);
      input.addEventListener("keydown", (ev) => { if (ev.key === "Enter") addVariant(); });
    }

    // Variant drag-and-drop regrouping (BUILD-02 Phase 9d): chips are
    // draggable; value rows are drop targets. The tested moveVariant
    // reducer does all the work; this is wiring only.
    for (const chip of panel.querySelectorAll(".variant-chip")) {
      chip.addEventListener("dragstart", (ev) => {
        ev.dataTransfer.setData("application/x-variant", JSON.stringify({
          variant: chip.dataset.variant,
          fromCategory: chip.dataset.category,
          fromCanonical: chip.dataset.canonical,
        }));
        ev.dataTransfer.effectAllowed = "move";
      });
      // Keyboard/mouse fallback so the feature is not drag-only: a
      // "move" button prompting for the target entity name.
      chip.querySelector(".variant-move").addEventListener("click", async () => {
        const target = prompt(
          "Move this variant to which value? Type its exact name (any category):");
        if (!target) return;
        const to = getState().entities.find(
          (e) => e.canonical.toLowerCase() === target.trim().toLowerCase());
        if (!to) {
          showError(container, new Error(`No value named "${target.trim()}" exists. Add it first, then move the variant.`));
          return;
        }
        if (moveVariant(chip.dataset.category, chip.dataset.canonical, to.category, to.canonical, chip.dataset.variant)) {
          // Both rows re-expand, so keep both open and show the result at
          // once rather than after the next unrelated repaint (CR17).
          openExpanded(chip.dataset.category, chip.dataset.canonical);
          openExpanded(to.category, to.canonical);
          await refreshVariants();
          repaintValues();
        }
      });
    }
  }

  // Drop targets: every value row accepts a dragged variant chip.
  for (const row of container.querySelectorAll("tr[data-key]:not(.variant-row)")) {
    row.addEventListener("dragover", (ev) => {
      if (ev.dataTransfer.types.includes("application/x-variant")) {
        ev.preventDefault();
        ev.dataTransfer.dropEffect = "move";
        // Visible drop feedback: without it the user cannot tell which row
        // will receive the chip (BUILD-04 CR17).
        row.classList.add("drop-target");
      }
    });
    row.addEventListener("dragleave", () => row.classList.remove("drop-target"));
    row.addEventListener("drop", async (ev) => {
      row.classList.remove("drop-target");
      const raw = ev.dataTransfer.getData("application/x-variant");
      if (!raw) return;
      ev.preventDefault();
      const { variant, fromCategory, fromCanonical } = JSON.parse(raw);
      const { category: toCategory, canonical: toCanonical } = row.dataset;
      if (moveVariant(fromCategory, fromCanonical, toCategory, toCanonical, variant)) {
        openExpanded(fromCategory, fromCanonical);
        openExpanded(toCategory, toCanonical);
        await refreshVariants();
        repaintValues();
      }
    });
  }
}

/**
 * refreshVariants() asks Go to expand every entity whose variants are
 * PENDING (variants === null: just added, edited, or variant-amended).
 * Settled rows, including "expanded, none found" ([]), are never
 * re-expanded (Phase 7a). Failures surface as a visible inline message
 * with the Go error text instead of being swallowed. Sequential on
 * purpose: the lists are tiny and ordering keeps the UI deterministic.
 */
async function refreshVariants() {
  // The snapshot is taken ONCE, before any await. Every expansion below
  // writes to the store and therefore repaints, and re-reading the list
  // mid-loop would mean expanding rows that a repaint had already
  // settled (BUILD-04 CR17).
  const pending = pendingExpansions(getState().entities);
  for (const e of pending) {
    try {
      const variants = await expandVariants({
        category: e.category, canonical: e.canonical,
        manualVariants: e.manualVariants, excludedVariants: e.excludedVariants ?? [],
      });
      setEntityVariants(e.category, e.canonical, variants ?? []);
    } catch (err) {
      // A failure becomes a VISIBLE error on the row, and settles it, so
      // the placeholder cannot spin forever and the row is not retried on
      // every repaint (BUILD-02 Phase 7a, re-pinned by CR17).
      setEntityVariantError(e.category, e.canonical, String(err?.message ?? err));
    }
  }
  return pending.length;
}

/**
 * currentVariantCount(category, canonical) is how many spellings a value
 * carries right now, automatic and manual together. The variant-add flow
 * compares it before and after to tell "added" from "already there", so
 * a duplicate gets an explanation instead of silence (BUILD-04 CR17).
 */
function currentVariantCount(category, canonical) {
  const e = getState().entities.find(
    (x) => entityKey(x.category, x.canonical) === entityKey(category, canonical));
  if (!e) return 0;
  return (e.variants?.length ?? 0) + (e.manualVariants?.length ?? 0);
}

/**
 * repaintValues() forces one re-render of the Values step.
 *
 * This is the fix for the headline CR17 symptom, "variants only appear
 * after leaving and coming back to the step". Expanding a row changes the
 * view-local `expanded` set, which the store knows nothing about, so
 * nothing repainted unless the click ALSO happened to make an expansion
 * pending. A row whose variants were already known therefore opened
 * invisibly, and only showed up on the next repaint from an unrelated
 * action, which usually meant switching steps.
 */
function repaintValues() {
  setState({});
}

// --- Custom patterns -------------------------------------------------------

function patternsPanel(s) {
  const rows = s.patterns.map((p) => `
    <span class="pill ${p.error ? "warn" : ""}" title="${escapeHTML(p.error ?? "valid")}">
      <code>${escapeHTML(p.expr)}</code>
      <button class="pattern-del" data-expr="${escapeHTML(p.expr)}" title="Remove">✕</button>
    </span>`).join("");
  const content = `
      <p class="hint">User-defined regular expressions, replaced as [CUSTOM_N]. Validated as you type;
        the tester shows sample matches from the loaded documents.</p>
      <div class="pill-list">${rows || `<span class="hint">none</span>`}</div>
      <div class="form-row">
        <input id="pattern-input" placeholder="e.g. PRJ-[0-9]+" spellcheck="false"/>
        <button id="pattern-test">test</button>
        <button id="pattern-add">Add</button>
      </div>
      <div id="pattern-feedback" class="hint"></div>`;
  // Custom patterns are an advanced feature: collapsed by default
  // (BUILD-02 Phase 2f).
  return panel("pattern-panel", "Custom patterns (regex)", content, {
    collapsible: true, startOpen: false, collapsedSet: collapsedPanels,
  });
}

function wirePatterns(container) {
  const input = container.querySelector("#pattern-input");
  const feedback = container.querySelector("#pattern-feedback");

  // Live compile validation on every keystroke (cheap round-trip).
  input.addEventListener("input", async () => {
    const expr = input.value.trim();
    feedback.textContent = expr ? (await validatePattern(expr)) || "✓ pattern compiles" : "";
  });

  container.querySelector("#pattern-test").addEventListener("click", async () => {
    try {
      const samples = await patternMatches(input.value.trim());
      feedback.textContent = samples?.length
        ? `matches: ${samples.join(", ")}`
        : "no matches in the loaded documents";
    } catch (err) {
      feedback.textContent = String(err?.message ?? err);
    }
  });

  container.querySelector("#pattern-add").addEventListener("click", async () => {
    const expr = input.value.trim();
    if (!expr) return;
    const error = await validatePattern(expr);
    if (error) {
      feedback.textContent = error; // invalid patterns are not added
      return;
    }
    addPattern(expr, null);
  });

  for (const btn of container.querySelectorAll(".pattern-del")) {
    btn.addEventListener("click", () => removePattern(btn.dataset.expr));
  }
}

function showError(container, err) {
  container.querySelector("#values-error").innerHTML =
    `<div class="banner error">${escapeHTML(String(err?.message ?? err))}</div>`;
}

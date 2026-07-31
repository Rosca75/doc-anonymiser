// views/identifyrail.js, the LEFT RAIL of wizard step 2, Identify
// (BUILD-02 Phase 6 as views/configure.js; renamed by BUILD-05 Phase 2 when
// Configure stopped being a step of its own, and relaid out into the mock-up's
// four tabs by BUILD-05 Phase 5).
//
// The rail answers WHAT to look for and HOW, in four tabs:
//
//   Scope           the document country, the preset, the 23 detection
//                   categories in four collapsible groups, and the confidence
//                   floor. Open by default, and the only tab most users will
//                   ever touch.
//   Smart detection the offline heuristic pass's strictness. It guesses which
//                   words are names from how they are written, so it always
//                   proposes some things that are not names, and these settings
//                   decide how much of that the user sees.
//   Local AI        the master toggle, the Ollama port (host locked to
//                   loopback, CLAUDE.md §8), the model and the context size.
//   Cloud AI        a placeholder panel and nothing else (decision 8).
//
// The allowlist editor moved OUT of here: it belongs to the workspace half of
// Identify now (views/identifyworkspace.js, the "Never anonymise" tab), because
// it is a list the user curates rather than a setting.
//
// Every AI-dependent control everywhere in the application gates on
// llmEnabled(state) = useAI AND ollama.available, so the deterministic pipeline
// stays fully usable with Ollama absent.

import { applySettings, listOllamaModels, probeOllama } from "../api.js";
import {
  getState, setState,
  applyPreset, toggleCategory, selectionPresetName, setUseAI,
  setCategoryGroup, setMinConfidence, setDocumentCountry,
  setSmartDetectOptions, smartDetectOptions,
  ALL_CATEGORIES,
  HARD_PII_CATEGORIES, EXTENDED_PII_CATEGORIES, NAME_CATEGORIES,
  ADVANCED_PII_CATEGORIES, ADVANCED_ENTITY_CATEGORIES,
} from "../state.js";
import { escapeHTML } from "../html.js";
import { button, chipRow, sectionLabel, collapsibleGroup, wireGroups } from "../ui.js";
import { CARDS, CONFIGURE, RAIL, VALUES, categoryLabels } from "../copy.js";
import { examplesFor, countryOptions } from "../countries.js";
import { keepScrollPosition } from "../scroll.js";

// The tooltip used by every disabled LLM control when Ollama is missing,
// verbatim from CLAUDE.md §4.
export const LLM_DISABLED_TOOLTIP = "Requires Ollama, which was not detected on 127.0.0.1:11434";

/**
 * llmGateTooltip(s) picks the right explanation for a disabled AI control:
 * Ollama missing vs the master toggle being off. Two different problems with two
 * different fixes, so they must not share one message.
 */
export function llmGateTooltip(s) {
  if (!s.ollama?.available) return LLM_DISABLED_TOOLTIP;
  return CONFIGURE.aiOffTooltip;
}

// RAIL_TABS is the tab set, in order. `id` is the token stored in activeTab.
export const RAIL_TABS = [
  ["scope", RAIL.tabScope],
  ["smart", RAIL.tabSmart],
  ["local", RAIL.tabLocalAI],
  ["cloud", RAIL.tabCloudAI],
];

// Which tab is open, and which category groups the user folded shut. Both are
// VIEW preferences rather than application state: nothing downstream reads
// them, they must not travel in a session file, and putting them in the store
// would mean every fold went through a reducer.
let activeTab = "scope";
const collapsedGroups = new Set();

// PRESETS: engine level → user-facing label. "soft/medium/advanced" reads too
// technical; Standard and Thorough say what they mean.
export const PRESETS = [
  ["soft", "Soft"],
  ["medium", "Standard"],
  ["advanced", "Thorough"],
];

// CATEGORY_GROUPS is the rail's grouping of the engine categories:
// [visible title, category keys]. It is a module constant so the select-all
// buttons can address a group by INDEX rather than re-deriving the key list, and
// so identifyrail.test.js can assert that every engine category the store knows
// about is reachable from some group (BUILD-04 CR9: the BUILD-03 recognizers
// were unreachable precisely because they belonged to no group).
export const CATEGORY_GROUPS = [
  [CONFIGURE.groupContact, HARD_PII_CATEGORIES],
  [CONFIGURE.groupTechnical, EXTENDED_PII_CATEGORIES],
  [CONFIGURE.groupNames, [...NAME_CATEGORIES, "custom_patterns"]],
  [CONFIGURE.groupThorough, [...ADVANCED_PII_CATEGORIES, ...ADVANCED_ENTITY_CATEGORIES]],
];

/**
 * renderIdentifyRail(container) fills the rail card.
 * @param {HTMLElement} container the card element views/identify.js created
 */
export function renderIdentifyRail(container) {
  const s = getState();
  const active = ALL_CATEGORIES.filter((c) => s.settings.categories?.[c]).length;

  container.innerHTML =
    `<div class="card-head">` +
    `<div class="card-head-left"><h2>${escapeHTML(CARDS.configure.title)}</h2></div>` +
    `<span class="card-sub">${escapeHTML(RAIL.activeCount(active, ALL_CATEGORIES.length))}</span>` +
    `</div>` +
    `<div class="rail-tabs">` +
    chipRow(RAIL_TABS.map(([id, label]) => ({ id, label, active: activeTab === id })),
      { attr: "railtab", square: true, ariaLabel: RAIL.tabsLabel }) +
    `</div>` +
    `<div class="card-body">${tabBody(s)}</div>` +
    `<div id="settings-error"></div>`;

  for (const chip of container.querySelectorAll("[data-railtab]")) {
    chip.addEventListener("click", () => {
      activeTab = chip.dataset.railtab;
      setState({}); // repaint; the tab is view state, so nothing else changes
    });
  }

  if (activeTab === "scope") wireScope(container);
  else if (activeTab === "smart") wireSmart(container);
  else if (activeTab === "local") wireLocalAI(container);
  // The Cloud AI tab is a placeholder panel with no controls (decision 8), so
  // there is deliberately nothing to wire.
}

/** tabBody(s) renders whichever tab is open. */
function tabBody(s) {
  switch (activeTab) {
    case "smart": return smartTab(s);
    case "local": return localAITab(s);
    case "cloud": return cloudAITab();
    default: return scopeTab(s);
  }
}

// --- Scope ----------------------------------------------------------------

/** scopeTab(s) is the country, the preset, the category groups and the
 *  confidence floor, in that order: broadest choice first. */
function scopeTab(s) {
  return `<div class="rail-block">` +
    sectionLabel(RAIL.country) +
    countrySelect(s) +
    `<p class="hint">${escapeHTML(RAIL.countryHint)}</p>` +
    `</div>` +
    `<div class="rail-block">` +
    sectionLabel(RAIL.preset) +
    presetChips(s) +
    `<p class="hint">${escapeHTML(CONFIGURE.presetHint)}</p>` +
    `</div>` +
    `<div class="rail-block">` +
    sectionLabel(RAIL.whatToAnonymise) +
    categoryGroups(s) +
    `</div>` +
    `<div class="rail-block">` +
    sectionLabel(CONFIGURE.confidenceTitle) +
    confidenceControl(s) +
    `</div>`;
}

function countrySelect(s) {
  const options = countryOptions().map((c) =>
    `<option value="${escapeHTML(c.code)}"${c.code === s.documentCountry ? " selected" : ""}>` +
    `${escapeHTML(c.name)}</option>`).join("");
  return `<select id="document-country" class="rail-select"` +
    ` aria-label="${escapeHTML(RAIL.country)}">${options}</select>`;
}

function presetChips(s) {
  const current = selectionPresetName(s.settings.categories);
  const chips = PRESETS.map(([id, label]) => ({ id, label, active: current === id }));
  // "Custom" is a READ-OUT, not a choice: there is no preset to apply, it is
  // what the selection reads as once it matches none of the three. Rendering it
  // as a chip that does nothing when pressed would be a lie, so it is disabled
  // and carries the explanation as its tooltip.
  chips.push({
    id: "custom", label: "Custom", active: current === "custom",
    disabled: true, title: CONFIGURE.presetHint,
  });
  return chipRow(chips, { attr: "preset", ariaLabel: RAIL.preset });
}

/**
 * categoryGroups(s) renders the four collapsible groups, each with its n/m count
 * and its select-all / deselect-all pair.
 *
 * The examples beside three of the labels come from the country (decision 2),
 * overlaid on copy.js CATEGORY_LABELS at render time rather than stored five
 * times over.
 */
function categoryGroups(s) {
  const labels = categoryLabels(examplesFor(s.documentCountry));

  return CATEGORY_GROUPS.map(([title, keys], index) => {
    const on = keys.filter((k) => s.settings.categories?.[k]).length;
    const rows = keys.map((key) => {
      const [label, example] = labels[key] ?? [key, ""];
      return `<label class="cat-row">` +
        `<input type="checkbox" class="cat-toggle" data-category="${escapeHTML(key)}"` +
        `${s.settings.categories?.[key] ? " checked" : ""}/>` +
        `<span class="cat-label">${escapeHTML(label)}</span>` +
        `<span class="cat-example" title="${escapeHTML(example)}">${escapeHTML(example)}</span>` +
        `</label>`;
    }).join("");

    // Icon-only bulk switches: the group header is narrow, and "Select all"
    // plus "Deselect all" across four groups would be sixteen words of chrome.
    const bulk =
      button("", {
        kind: "ghost", cls: "cat-group-all icon-action ok", icon: "check",
        ariaLabel: `${CONFIGURE.selectAll}: ${title}`, title: CONFIGURE.selectAll,
        data: { group: String(index), on: "1" },
      }) +
      button("", {
        kind: "ghost", cls: "cat-group-all icon-action", icon: "remove",
        ariaLabel: `${CONFIGURE.deselectAll}: ${title}`, title: CONFIGURE.deselectAll,
        data: { group: String(index), on: "0" },
      });

    return collapsibleGroup(`cat-group-${index}`, title, rows, {
      open: !collapsedGroups.has(`cat-group-${index}`),
      countLabel: `${on}/${keys.length}`,
      headRightHTML: bulk,
    });
  }).join("");
}

/**
 * confidenceControl(s) renders the detection-confidence floor (BUILD-04 CR9).
 *
 * It is deliberately NOT gated on the local AI: every detection carries a score
 * whether or not Ollama is running. A slider in whole percent rather than a
 * number field, because the meaningful settings are ranges; the live read-out
 * names what the current position actually excludes.
 */
function confidenceControl(s) {
  const percent = Math.round((s.settings.minConfidence ?? 0) * 100);
  return `<p class="hint">${escapeHTML(CONFIGURE.confidenceHint)}</p>` +
    `<div class="rail-slider">` +
    `<input id="min-confidence" type="range" min="0" max="100" step="5" value="${percent}"` +
    ` aria-label="${escapeHTML(CONFIGURE.confidenceLabel)}"/>` +
    `<output id="min-confidence-value" for="min-confidence">${percent}</output>` +
    `</div>` +
    `<p class="hint" id="min-confidence-effect">${escapeHTML(confidenceEffect(percent))}</p>`;
}

/**
 * confidenceEffect(percent) puts the current slider position into words, so the
 * user reads what the setting DOES rather than a bare number.
 *
 * BUILD-05 decision 3 rewrote this copy. The mock-up's version described a
 * source-tiered rule ("values that only the local AI suggested are skipped"),
 * which the engine does not implement: what the setting actually is is a FLOOR
 * on the confidence score every detection carries. The thresholds below mirror
 * where the engine's own scores sit (engine/pii.go: local-AI proposals score
 * 0.8, values the user listed 0.95, pattern matches 1.0), and the sentences name
 * the effect of the floor rather than inventing a rule about sources.
 *
 * @param {number} percent slider position, 0 to 100
 * @returns {string} a plain-language sentence
 */
export function confidenceEffect(percent) {
  if (percent <= 0) return "Nothing is skipped: every detection is replaced.";
  if (percent <= 80) {
    return "Nothing is skipped at this setting: every detection the application makes " +
      "scores at least 80, so this floor is not yet reached.";
  }
  if (percent <= 95) {
    return "Detections scoring below this floor are left alone. In practice that is the " +
      "weaker ones, proposed rather than matched outright.";
  }
  return "Only the strongest detections are replaced, which in practice means the " +
    "pattern matches. Everything scored lower is left alone.";
}

function wireScope(container) {
  container.querySelector("#document-country")?.addEventListener("change", (ev) => {
    keepScrollPosition(() => {
      setDocumentCountry(ev.target.value);
      pushSettings(container);
    });
  });

  for (const chip of container.querySelectorAll("[data-preset]")) {
    if (chip.disabled) continue; // the Custom read-out
    chip.addEventListener("click", () => {
      applyPreset(chip.dataset.preset);
      pushSettings(container);
    });
  }

  for (const box of container.querySelectorAll(".cat-toggle")) {
    // keepScrollPosition wraps the state change so the full re-render it
    // triggers lands with the rail where the user left it (BUILD-04 CR12:
    // ticking a box in a long list used to jump back to the top).
    box.addEventListener("change", () => keepScrollPosition(() => {
      toggleCategory(box.dataset.category, box.checked);
      pushSettings(container);
    }));
  }

  for (const btn of container.querySelectorAll(".cat-group-all")) {
    btn.addEventListener("click", (ev) => {
      // The button lives in the group header, which is itself a toggle; stop
      // the click before wireGroups reads it as a request to fold the group.
      ev.stopPropagation();
      const group = CATEGORY_GROUPS[Number(btn.dataset.group)];
      if (!group) return;
      keepScrollPosition(() => {
        // ONE reducer call flips the whole group, so there is exactly one
        // repaint rather than one per category.
        setCategoryGroup(group[1], btn.dataset.on === "1");
        pushSettings(container);
      });
    });
  }

  wireGroups(container, (id) => {
    if (collapsedGroups.has(id)) collapsedGroups.delete(id);
    else collapsedGroups.add(id);
    setState({}); // repaint; folding is view state
  });

  // "input" updates the read-out live while dragging without touching the
  // store; "change" (on release) commits it, so a drag does not fire one bridge
  // round-trip per pixel.
  const confidence = container.querySelector("#min-confidence");
  if (confidence) {
    const readout = container.querySelector("#min-confidence-value");
    const effect = container.querySelector("#min-confidence-effect");
    confidence.addEventListener("input", () => {
      const percent = Number(confidence.value);
      if (readout) readout.textContent = String(percent);
      if (effect) effect.textContent = confidenceEffect(percent);
    });
    confidence.addEventListener("change", () => keepScrollPosition(() => {
      setMinConfidence(Number(confidence.value) / 100);
      return pushSettings(container);
    }));
  }
}

// --- Smart detection ------------------------------------------------------

/** smartTab(s) is the offline heuristic pass's strictness (BUILD-04 CR13). */
function smartTab(s) {
  const opts = smartDetectOptions(s);
  const numberRow = (id, label, hint, value, attrs) =>
    `<label class="rail-field" for="${id}">` +
    `<span class="rail-field-label">${escapeHTML(label)}</span>` +
    `<input id="${id}" type="number" ${attrs} value="${escapeHTML(String(value))}"/>` +
    `</label><p class="hint">${escapeHTML(hint)}</p>`;

  return `<div class="rail-block">` +
    sectionLabel(RAIL.tabSmart) +
    `<p class="hint">${escapeHTML(VALUES.smartSettingsHint)}</p>` +
    numberRow("smart-min-length", VALUES.smartMinLength, VALUES.smartMinLengthHint,
      opts.minLength, `min="0" max="40" step="1"`) +
    numberRow("smart-min-occurrences", VALUES.smartMinOccurrences, VALUES.smartMinOccurrencesHint,
      opts.minOccurrences, `min="0" max="100" step="1"`) +
    numberRow("smart-min-confidence", VALUES.smartMinConfidence, VALUES.smartMinConfidenceHint,
      opts.minConfidence, `min="0" max="1" step="0.05"`) +
    `<label class="cat-row">` +
    `<input type="checkbox" id="smart-common-words"${opts.excludeCommonWords ? " checked" : ""}/>` +
    `<span class="cat-label">${escapeHTML(VALUES.smartCommonWords)}</span>` +
    `</label>` +
    `<p class="hint">${escapeHTML(VALUES.smartCommonWordsHint)}</p>` +
    `</div>`;
}

function wireSmart(container) {
  const numbers = [
    ["#smart-min-length", (v) => ({ minLength: Math.round(v) })],
    ["#smart-min-occurrences", (v) => ({ minOccurrences: Math.round(v) })],
    ["#smart-min-confidence", (v) => ({ minConfidence: v })],
  ];
  for (const [selector, toPatch] of numbers) {
    container.querySelector(selector)?.addEventListener("change", (ev) => {
      const value = Number(ev.target.value);
      if (Number.isNaN(value)) return;
      // setSmartDetectOptions validates and IGNORES a bad value rather than
      // storing it, so a typo shows as the field snapping back rather than as
      // smart detection quietly finding nothing.
      keepScrollPosition(() => setSmartDetectOptions(toPatch(value)));
    });
  }
  container.querySelector("#smart-common-words")?.addEventListener("change", (ev) => {
    setSmartDetectOptions({ excludeCommonWords: ev.target.checked });
  });
}

// --- Local AI -------------------------------------------------------------

function localAITab(s) {
  const ollamaOK = !!s.ollama?.available;
  const aiOn = !!s.settings.useAI;
  // The model and the context size are gated, the PORT and Re-probe are not:
  // those two are how a user CONNECTS, so gating them would lock someone out of
  // fixing the very connection the gate is complaining about.
  const gated = aiOn && ollamaOK ? "" : ` disabled title="${escapeHTML(llmGateTooltip(s))}"`;
  const models = (s.ollama?.models ?? []).map((m) =>
    `<option value="${escapeHTML(m)}"${s.settings.model === m ? " selected" : ""}>` +
    `${escapeHTML(m)}</option>`).join("");

  return `<div class="rail-block">` +
    sectionLabel(RAIL.tabLocalAI) +
    `<label class="cat-row">` +
    `<input type="checkbox" id="use-ai"${aiOn ? " checked" : ""}` +
    `${ollamaOK ? "" : ` title="${escapeHTML(LLM_DISABLED_TOOLTIP)}"`}/>` +
    `<span class="cat-label">${escapeHTML(CONFIGURE.useAILabel)}</span>` +
    `</label>` +
    `<p class="hint">${escapeHTML(CONFIGURE.useAIHint)}</p>` +
    `<div class="rail-status">` +
    `<span class="state-tag${ollamaOK ? "" : " bad"}" title="${escapeHTML(s.ollama?.detail ?? "")}">` +
    `${escapeHTML(ollamaOK ? RAIL.ollamaDetected : RAIL.ollamaMissing)}</span>` +
    `<span class="hint">${escapeHTML(RAIL.hostLocked)}</span>` +
    `</div>` +
    `<label class="rail-field" for="ollama-port">` +
    `<span class="rail-field-label">${escapeHTML(RAIL.port)}</span>` +
    `<input id="ollama-port" type="number" min="1" max="65535" value="${escapeHTML(String(s.settings.ollamaPort))}"/>` +
    `</label>` +
    `<label class="rail-field" for="ollama-model">` +
    `<span class="rail-field-label">${escapeHTML(RAIL.model)}</span>` +
    `<select id="ollama-model"${gated}>${models || `<option value="">${escapeHTML(RAIL.noModels)}</option>`}</select>` +
    `</label>` +
    `<label class="rail-field" for="context-size">` +
    `<span class="rail-field-label">${escapeHTML(RAIL.contextSize)}</span>` +
    `<input id="context-size" type="number" min="0" step="1024" value="${escapeHTML(String(s.settings.contextSize ?? 8192))}"${gated}/>` +
    `</label>` +
    `<p class="hint">${escapeHTML(CONFIGURE.contextSizeHint)}</p>` +
    button(RAIL.reprobe, { kind: "secondary", id: "btn-reprobe", icon: "refresh" }) +
    `</div>`;
}

function wireLocalAI(container) {
  container.querySelector("#use-ai")?.addEventListener("change", (ev) => {
    setUseAI(ev.target.checked);
    pushSettings(container);
  });
  for (const id of ["#ollama-port", "#ollama-model", "#context-size"]) {
    container.querySelector(id)?.addEventListener("change", () => pushSettings(container));
  }
  container.querySelector("#btn-reprobe")?.addEventListener("click", async () => {
    await pushSettings(container);
    setState({ ollama: await probeOllama() });
  });
}

// --- Cloud AI -------------------------------------------------------------

/**
 * cloudAITab() is a placeholder panel and NOTHING else (BUILD-05 decision 8).
 *
 * Deliberately absent, because a separate improvement plan owns this feature and
 * must not have to trip over half-built scaffolding: no provider list, no
 * endpoint field, no settings shape in state.js, no BRIDGE.md row, no Go client.
 * The tab exists so the shape of the eventual feature is visible, and the copy
 * commits only to the thing that will not change about it, which is that nothing
 * leaves the machine before the user has said in writing what may.
 */
function cloudAITab() {
  return `<div class="rail-block">` +
    sectionLabel(RAIL.tabCloudAI) +
    `<div class="placeholder-panel">` +
    `<span class="fmt-badge">${escapeHTML(RAIL.cloudNotYet)}</span>` +
    `<p class="hint">${escapeHTML(RAIL.cloudBody)}</p>` +
    `</div></div>`;
}

// --- Shared: push settings to Go -----------------------------------------

/**
 * pushSettings assembles the full settings payload from state plus the Local AI
 * tab's inputs (when that tab is rendered) and applies it in one round-trip. The
 * store mirror updates from what Go accepted; errors surface as a banner.
 */
async function pushSettings(container) {
  const s = getState();
  const port = container.querySelector("#ollama-port");
  const model = container.querySelector("#ollama-model");
  const ctxSize = container.querySelector("#context-size");
  const settings = {
    level: s.settings.level,
    categories: s.settings.categories,
    // A tab that is not on screen contributes nothing: read the store instead of
    // a missing element, so switching tabs never resets a setting.
    ollamaPort: port ? (parseInt(port.value, 10) || 0) : s.settings.ollamaPort,
    model: model?.value || s.settings.model,
    contextSize: ctxSize ? (parseInt(ctxSize.value, 10) || 0) : (s.settings.contextSize ?? 8192),
    useAI: !!s.settings.useAI,
    // Read from the store, not the input: setMinConfidence already validated and
    // stored it, and the Scope tab may not be rendered at all.
    minConfidence: s.settings.minConfidence ?? 0,
    smartDetect: smartDetectOptions(s),
  };
  try {
    const status = await applySettings(settings);
    setState({ settings: { ...getState().settings, ...settings }, ollama: status });
    if (status.available) {
      try {
        const models = await listOllamaModels();
        setState({ ollama: { ...status, models } });
      } catch { /* keep the probe result; the dropdown shows nothing */ }
    }
  } catch (err) {
    const slot = container.querySelector("#settings-error");
    if (slot) {
      slot.innerHTML = `<div class="banner error">${escapeHTML(String(err?.message ?? err))}</div>`;
    }
  }
}

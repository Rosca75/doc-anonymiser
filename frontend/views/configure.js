// views/configure.js, wizard step 2 (BUILD-02 Phase 6): two focused
// sub-screens with granular controls and plain language.
//
//   - "What to anonymise": preset chips (Soft / Standard / Thorough) over
//     grouped per-category checkboxes; touching any checkbox switches the
//     preset display to Custom. Plus the shared allowlist editor.
//   - "AI and advanced settings": the master "Use local AI" toggle, the
//     Ollama port (host locked to loopback, CLAUDE.md §8), model select,
//     context size and re-probe.
//
// Every AI-dependent control everywhere in the app gates on
// llmEnabled(state) = UseAI AND ollama.available.

import { applySettings, listOllamaModels, probeOllama } from "../api.js";
import {
  getState, setState,
  applyPreset, toggleCategory, selectionPresetName, setUseAI, llmEnabled,
  setCategoryGroup, setMinConfidence,
  HARD_PII_CATEGORIES, EXTENDED_PII_CATEGORIES, NAME_CATEGORIES,
  ADVANCED_PII_CATEGORIES, ADVANCED_ENTITY_CATEGORIES,
} from "../state.js";
import { escapeHTML } from "../html.js";
import { panel, wirePanels, button } from "../ui.js";
import { CONFIGURE, CATEGORY_LABELS } from "../copy.js";
import { renderAllowlistPanel, wireAllowlistPanel } from "./allowlist.js";
import { keepScrollPosition } from "../scroll.js";

// The tooltip used by every disabled LLM control when Ollama is missing,
// verbatim from CLAUDE.md §4.
export const LLM_DISABLED_TOOLTIP = "Requires Ollama, which was not detected on 127.0.0.1:11434";

/**
 * llmGateTooltip(s) picks the right explanation for a disabled AI
 * control: Ollama missing vs the master toggle being off.
 */
export function llmGateTooltip(s) {
  if (!s.ollama?.available) return LLM_DISABLED_TOOLTIP;
  return CONFIGURE.aiOffTooltip;
}

// Which sub-screen tab is open. View-local UI state (same pattern as the
// collapsed-panel sets).
let activeTab = "what";

// Panels toggled away from their defaults (BUILD-02 Phase 2f).
const collapsedPanels = new Set();

export function renderConfigure(container) {
  const s = getState();

  container.innerHTML = `
    <div class="configure-view">
      <div class="subtabs">
        ${button(CONFIGURE.tabWhat, { kind: activeTab === "what" ? "secondary" : "ghost", id: "tab-what" })}
        ${button(CONFIGURE.tabAI, { kind: activeTab === "ai" ? "secondary" : "ghost", id: "tab-ai" })}
      </div>
      ${activeTab === "what" ? whatTab(s) : aiTab(s)}
      <div id="settings-error"></div>
    </div>
  `;

  container.querySelector("#tab-what").addEventListener("click", () => { activeTab = "what"; setState({}); });
  container.querySelector("#tab-ai").addEventListener("click", () => { activeTab = "ai"; setState({}); });

  wirePanels(container, collapsedPanels, () => setState({}));
  if (activeTab === "what") wireWhatTab(container, s);
  else wireAITab(container, s);
}

// --- "What to anonymise" -------------------------------------------------

// Preset chips: value → user-facing label (soft/medium/advanced read too
// technical; Standard and Thorough say what they mean).
const PRESETS = [
  ["soft", "Soft"],
  ["medium", "Standard"],
  ["advanced", "Thorough"],
];

// CATEGORY_GROUPS is the Configure screen's grouping of the engine
// categories: [visible title, category keys]. It is a module constant so
// the select-all buttons can address a group by INDEX rather than by
// re-deriving the key list, and so a test can assert that every engine
// category the store knows about is reachable from some group (BUILD-04
// CR9: the BUILD-03 recognizers were unreachable precisely because they
// belonged to no group).
export const CATEGORY_GROUPS = [
  [CONFIGURE.groupContact, HARD_PII_CATEGORIES],
  [CONFIGURE.groupTechnical, EXTENDED_PII_CATEGORIES],
  [CONFIGURE.groupNames, [...NAME_CATEGORIES, "custom_patterns"]],
  [CONFIGURE.groupThorough, [...ADVANCED_PII_CATEGORIES, ...ADVANCED_ENTITY_CATEGORIES]],
];

function whatTab(s) {
  const current = selectionPresetName(s.settings.categories);
  const chips = PRESETS.map(([value, label]) =>
    `<button class="chip preset-chip ${current === value ? "active" : ""}" data-preset="${value}">${label}</button>`).join("");
  const customChip = `<span class="chip preset-chip ${current === "custom" ? "active" : ""}" title="${escapeHTML(CONFIGURE.presetHint)}">Custom</span>`;

  const groupsHTML = CATEGORY_GROUPS.map(([title, keys], i) => {
    const rows = keys.map((key) => {
      const [label, example] = CATEGORY_LABELS[key] ?? [key, ""];
      return `
        <label class="radio-row">
          <input type="checkbox" class="cat-toggle" data-category="${key}" ${s.settings.categories?.[key] ? "checked" : ""}/>
          <span>${escapeHTML(label)} <span class="hint">${escapeHTML(example)}</span></span>
        </label>`;
    }).join("");
    // Bulk switches for the whole group (BUILD-04 CR10). They sit in the
    // panel header, which wirePanels already treats as "not a collapse
    // click", so pressing one does not fold the panel shut.
    const bulk =
      button(CONFIGURE.selectAll, { kind: "ghost", cls: "cat-group-all", data: { group: String(i), on: "1" } }) +
      button(CONFIGURE.deselectAll, { kind: "ghost", cls: "cat-group-all", data: { group: String(i), on: "0" } });
    return panel(`cfg-group-${i}`, title, rows, {
      collapsible: true, collapsedSet: collapsedPanels, headExtraHTML: bulk,
    });
  }).join("");

  const presetPanel = panel("cfg-presets", "Preset", `
      <p class="hint">${escapeHTML(CONFIGURE.presetHint)}</p>
      <div class="pill-list">${chips}${customChip}</div>`,
  { collapsible: false });

  return presetPanel + groupsHTML + renderAllowlistPanel(s, collapsedPanels);
}

function wireWhatTab(container, s) {
  for (const chip of container.querySelectorAll("[data-preset]")) {
    chip.addEventListener("click", () => {
      applyPreset(chip.dataset.preset);
      pushSettings(container);
    });
  }
  for (const box of container.querySelectorAll(".cat-toggle")) {
    // keepScrollPosition wraps the state change so the full re-render it
    // triggers lands with the page where the user left it (BUILD-04 CR12:
    // ticking a box in a long list used to jump back to the top).
    box.addEventListener("change", () => keepScrollPosition(() => {
      toggleCategory(box.dataset.category, box.checked);
      pushSettings(container);
    }));
  }
  // Select all / Deselect all per group (BUILD-04 CR10). One reducer call
  // flips the whole group, so there is exactly one re-render.
  for (const btn of container.querySelectorAll(".cat-group-all")) {
    btn.addEventListener("click", (ev) => {
      // The button lives in a collapsible panel header; stop the click
      // before wirePanels reads it as a request to fold the panel.
      ev.stopPropagation();
      const group = CATEGORY_GROUPS[Number(btn.dataset.group)];
      if (!group) return;
      keepScrollPosition(() => {
        setCategoryGroup(group[1], btn.dataset.on === "1");
        pushSettings(container);
      });
    });
  }
  wireAllowlistPanel(container);
}

// --- "AI and advanced settings" -------------------------------------------

function aiTab(s) {
  const ollamaOK = !!s.ollama?.available;
  const aiOn = !!s.settings.useAI;
  const controlsGate = aiOn && ollamaOK ? "" : `disabled title="${escapeHTML(llmGateTooltip(s))}"`;
  const modelOptions = (s.ollama?.models ?? []).map((m) =>
    `<option value="${escapeHTML(m)}" ${s.settings.model === m ? "selected" : ""}>${escapeHTML(m)}</option>`).join("");

  const content = `
      <label class="radio-row">
        <input type="checkbox" id="use-ai" ${aiOn ? "checked" : ""} ${ollamaOK ? "" : `title="${LLM_DISABLED_TOOLTIP}"`}/>
        <span><strong>${escapeHTML(CONFIGURE.useAILabel)}</strong>
          <span class="hint">${escapeHTML(CONFIGURE.useAIHint)}</span></span>
      </label>
      <p class="hint">
        <span class="badge ${ollamaOK ? "ok" : "off"}" title="${escapeHTML(s.ollama?.detail ?? "")}">
          ${ollamaOK ? "Ollama detected" : "Ollama not detected"}
        </span>
        The host is locked to 127.0.0.1; only the port can be changed.
      </p>
      <div class="form-row">
        <label for="ollama-port">Port</label>
        <input id="ollama-port" type="number" min="1" max="65535" value="${s.settings.ollamaPort}"/>
        <button id="btn-reprobe">Re-probe</button>
      </div>
      <div class="form-row">
        <label for="ollama-model">Model</label>
        <select id="ollama-model" ${controlsGate}>
          ${modelOptions || `<option value="">(no models found)</option>`}
        </select>
      </div>
      <div class="form-row">
        <label for="context-size">Context size</label>
        <input id="context-size" type="number" min="0" step="1024" value="${s.settings.contextSize ?? 8192}" ${controlsGate}/>
        <span class="hint">${escapeHTML(CONFIGURE.contextSizeHint)}</span>
      </div>`;

  return panel("cfg-ai", CONFIGURE.tabAI, content, { collapsible: false }) +
    confidencePanel(s);
}

/**
 * confidencePanel(s) renders the detection-confidence control
 * (BUILD-04 CR9), which surfaces the BUILD-03 confidence scoring.
 *
 * The control is deliberately NOT gated on the local AI: the score exists
 * for every detection, whether or not Ollama is running. It is a slider in
 * whole percent rather than a free number field because the meaningful
 * settings are ranges, not exact values, and the live read-out names what
 * the current position actually excludes.
 */
function confidencePanel(s) {
  const percent = Math.round((s.settings.minConfidence ?? 0) * 100);
  const content = `
      <p class="hint">${escapeHTML(CONFIGURE.confidenceHint)}</p>
      <div class="form-row">
        <label for="min-confidence">${escapeHTML(CONFIGURE.confidenceLabel)}</label>
        <input id="min-confidence" type="range" min="0" max="100" step="5" value="${percent}"/>
        <output id="min-confidence-value" for="min-confidence">${percent}</output>
      </div>
      <p class="hint">${escapeHTML(CONFIGURE.confidenceScale)}</p>
      <p class="hint" id="min-confidence-effect">${escapeHTML(confidenceEffect(percent))}</p>`;
  return panel("cfg-confidence", CONFIGURE.confidenceTitle, content, { collapsible: false });
}

/**
 * confidenceEffect(percent) puts the current slider position into words,
 * so the user reads what the setting DOES rather than a bare number. The
 * thresholds mirror the engine's confidence scale (engine/pii.go: local AI
 * proposals score 0.8, values the user listed 0.95, pattern matches 1.0).
 * @param {number} percent slider position, 0 to 100
 * @returns {string} a plain-language sentence
 */
export function confidenceEffect(percent) {
  if (percent <= 0) return "Nothing is skipped: every detection is replaced.";
  if (percent <= 80) return "Nothing is skipped yet at this setting: every detection scores at least 80.";
  if (percent <= 95) return "Values that only the local AI suggested are skipped. The values you listed and the pattern matches are still replaced.";
  if (percent <= 100 && percent > 95) return "Only pattern matches are replaced. The values you listed yourself and the AI suggestions are both skipped.";
  return "Nothing is skipped: every detection is replaced.";
}

function wireAITab(container, s) {
  container.querySelector("#use-ai").addEventListener("change", (ev) => {
    setUseAI(ev.target.checked);
    pushSettings(container);
  });
  for (const id of ["#ollama-port", "#ollama-model", "#context-size"]) {
    container.querySelector(id)?.addEventListener("change", () => pushSettings(container));
  }

  // Detection confidence (BUILD-04 CR9). "input" updates the read-out
  // live while dragging without touching the store; "change" (on release)
  // commits it, so a drag does not fire one bridge round-trip per pixel.
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
  container.querySelector("#btn-reprobe").addEventListener("click", async () => {
    await pushSettings(container);
    setState({ ollama: await probeOllama() });
  });
}

// --- Shared: push settings to Go -------------------------------------------

/**
 * pushSettings assembles the full settings payload from state + the AI
 * tab inputs (when rendered) and applies it in one round-trip. The store
 * mirror updates from what Go accepted; errors surface as a banner.
 */
async function pushSettings(container) {
  const s = getState();
  const port = container.querySelector("#ollama-port");
  const model = container.querySelector("#ollama-model");
  const ctxSize = container.querySelector("#context-size");
  const settings = {
    level: s.settings.level,
    categories: s.settings.categories,
    ollamaPort: port ? (parseInt(port.value, 10) || 0) : s.settings.ollamaPort,
    model: model?.value || s.settings.model,
    contextSize: ctxSize ? (parseInt(ctxSize.value, 10) || 0) : (s.settings.contextSize ?? 8192),
    useAI: !!s.settings.useAI,
    // Read from the store, not the input: setMinConfidence already
    // validated and stored it, and the AI tab may not be rendered at all.
    minConfidence: s.settings.minConfidence ?? 0,
  };
  try {
    const status = await applySettings(settings);
    setState({ settings, ollama: status });
    if (status.available) {
      try {
        const models = await listOllamaModels();
        setState({ ollama: { ...status, models } });
      } catch { /* keep the probe result; the dropdown shows nothing */ }
    }
  } catch (err) {
    const slot = container.querySelector("#settings-error");
    if (slot) slot.innerHTML = `<div class="banner error">${escapeHTML(String(err?.message ?? err))}</div>`;
  }
}

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
  HARD_PII_CATEGORIES, NAME_CATEGORIES,
  ADVANCED_PII_CATEGORIES, ADVANCED_ENTITY_CATEGORIES,
} from "../state.js";
import { escapeHTML } from "../html.js";
import { panel, wirePanels, button } from "../ui.js";
import { CONFIGURE, CATEGORY_LABELS } from "../copy.js";
import { renderAllowlistPanel, wireAllowlistPanel } from "./allowlist.js";

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

function whatTab(s) {
  const current = selectionPresetName(s.settings.categories);
  const chips = PRESETS.map(([value, label]) =>
    `<button class="chip preset-chip ${current === value ? "active" : ""}" data-preset="${value}">${label}</button>`).join("");
  const customChip = `<span class="chip preset-chip ${current === "custom" ? "active" : ""}" title="${escapeHTML(CONFIGURE.presetHint)}">Custom</span>`;

  const groups = [
    [CONFIGURE.groupContact, HARD_PII_CATEGORIES],
    [CONFIGURE.groupNames, [...NAME_CATEGORIES, "custom_patterns"]],
    [CONFIGURE.groupThorough, [...ADVANCED_PII_CATEGORIES, ...ADVANCED_ENTITY_CATEGORIES]],
  ];
  const groupsHTML = groups.map(([title, keys], i) => {
    const rows = keys.map((key) => {
      const [label, example] = CATEGORY_LABELS[key] ?? [key, ""];
      return `
        <label class="radio-row">
          <input type="checkbox" class="cat-toggle" data-category="${key}" ${s.settings.categories?.[key] ? "checked" : ""}/>
          <span>${escapeHTML(label)} <span class="hint">${escapeHTML(example)}</span></span>
        </label>`;
    }).join("");
    return panel(`cfg-group-${i}`, title, rows, { collapsible: true, collapsedSet: collapsedPanels });
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
    box.addEventListener("change", () => {
      toggleCategory(box.dataset.category, box.checked);
      pushSettings(container);
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

  return panel("cfg-ai", CONFIGURE.tabAI, content, { collapsible: false });
}

function wireAITab(container, s) {
  container.querySelector("#use-ai").addEventListener("change", (ev) => {
    setUseAI(ev.target.checked);
    pushSettings(container);
  });
  for (const id of ["#ollama-port", "#ollama-model", "#context-size"]) {
    container.querySelector(id)?.addEventListener("change", () => pushSettings(container));
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

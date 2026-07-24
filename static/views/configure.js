// views/configure.js, wizard step 2: settings.
//
//   - anonymisation level radio (default medium),
//   - Ollama port override (host locked to loopback, the input is the
//     PORT only, by design; see CLAUDE.md §8),
//   - model dropdown populated from ListModels(),
//   - "re-probe" button,
//   - greyed-state mechanics: state.ollama.available drives disabled +
//     tooltip on every LLM control (CLAUDE.md §4 graceful degradation).

import { applySettings, listOllamaModels, probeOllama } from "../api.js";
import { getState, setState } from "../state.js";
import { escapeHTML } from "../html.js";

// The tooltip used by every disabled LLM control, verbatim from CLAUDE.md §4.
export const LLM_DISABLED_TOOLTIP = "Requires Ollama, which was not detected on 127.0.0.1:11434";

export function renderConfigure(container) {
  const s = getState();
  const ollamaOK = !!s.ollama?.available;
  const llmDisabled = ollamaOK ? "" : `disabled title="${LLM_DISABLED_TOOLTIP}"`;

  const levels = [
    ["soft", "Soft: hard personal details (emails, phones, bank accounts, IDs, VAT numbers, web addresses) and engagement names"],
    ["medium", "Medium (default): soft plus person names"],
    ["advanced", "Advanced: medium plus dates, locations, organisations and amounts"],
  ];
  const radios = levels.map(([value, label]) => `
    <label class="radio-row">
      <input type="radio" name="level" value="${value}" ${s.settings.level === value ? "checked" : ""}/>
      <span>${escapeHTML(label)}</span>
    </label>`).join("");

  const modelOptions = (s.ollama?.models ?? []).map((m) =>
    `<option value="${escapeHTML(m)}" ${s.settings.model === m ? "selected" : ""}>${escapeHTML(m)}</option>`).join("");

  container.innerHTML = `
    <div class="configure-view">
      <section class="panel">
        <div class="panel-head"><h2>Anonymisation level</h2></div>
        ${radios}
      </section>
      <section class="panel">
        <div class="panel-head"><h2>Ollama (optional AI)</h2>
          <span class="badge ${ollamaOK ? "ok" : "off"}" title="${escapeHTML(s.ollama?.detail ?? "")}">
            ${ollamaOK ? "detected" : "not detected"}
          </span>
        </div>
        <p class="hint">The AI features are optional. The deterministic pipeline works without them.
        Host is locked to 127.0.0.1; only the port can be changed.</p>
        <div class="form-row">
          <label for="ollama-port">Port</label>
          <input id="ollama-port" type="number" min="1" max="65535" value="${s.settings.ollamaPort}"/>
          <button id="btn-reprobe">Re-probe</button>
        </div>
        <div class="form-row">
          <label for="ollama-model">Model</label>
          <select id="ollama-model" ${llmDisabled}>
            ${modelOptions || `<option value="">(no models found)</option>`}
          </select>
        </div>
      </section>
      <div id="settings-error"></div>
    </div>
  `;

  // Applying settings is one round-trip: store + re-probe together.
  const apply = async () => {
    const settings = {
      level: container.querySelector('input[name="level"]:checked').value,
      ollamaPort: parseInt(container.querySelector("#ollama-port").value, 10) || 0,
      model: container.querySelector("#ollama-model").value || s.settings.model,
    };
    try {
      const status = await applySettings(settings);
      setState({ settings, ollama: status });
      // Refresh the model list on a successful probe so the dropdown
      // reflects the (possibly different) server on the new port.
      if (status.available) {
        try {
          const models = await listOllamaModels();
          setState({ ollama: { ...status, models } });
        } catch { /* keep the probe result; the dropdown shows nothing */ }
      }
    } catch (err) {
      container.querySelector("#settings-error").innerHTML =
        `<div class="banner error">${escapeHTML(String(err?.message ?? err))}</div>`;
    }
  };

  for (const input of container.querySelectorAll('input[name="level"], #ollama-model')) {
    input.addEventListener("change", apply);
  }
  container.querySelector("#ollama-port").addEventListener("change", apply);
  container.querySelector("#btn-reprobe").addEventListener("click", async () => {
    await apply();
    // A manual re-probe also refreshes the badge when settings are unchanged.
    setState({ ollama: await probeOllama() });
  });
}

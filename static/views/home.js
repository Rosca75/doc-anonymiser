// views/home.js — the placeholder home screen for the bootstrap phase.
//
// Responsibilities (frontend discipline, CLAUDE.md §4):
//   - render FROM state only (state.js), never keep private copies,
//   - dispatch actions that call api.js (never window.go directly),
//   - show two things: the JS↔Go bridge status ("pong") and the Ollama
//     availability badge (green "detected: N models" / grey "not detected").
//
// Later phases replace this screen with the import → anonymise → export
// wizard; the render-from-state pattern established here carries over.

import { ping, probeOllama } from "../api.js";
import { getState, setState, subscribe } from "../state.js";

/**
 * renderHome(root) mounts the home view into the given container and kicks
 * off the two startup checks (bridge ping, Ollama probe).
 * @param {HTMLElement} root the #app container from index.html.
 */
export function renderHome(root) {
  // Re-render on every state change. The view is tiny, so a full
  // re-render is simpler and safer than DOM diffing.
  subscribe(() => paint(root));
  paint(root);

  // --- Action: prove the JS↔Go bridge. -------------------------------
  ping()
    .then((answer) => setState({ bridge: answer }))
    .catch((err) => setState({ bridge: `bridge error: ${err.message ?? err}` }));

  // --- Action: probe the local Ollama server. ------------------------
  // "Not available" is NOT an error — it arrives inside the status
  // object and renders as the grey badge (graceful degradation).
  probeOllama()
    .then((status) => setState({ ollama: status }))
    .catch((err) =>
      setState({
        ollama: {
          available: false,
          models: [],
          detail: `Probe failed unexpectedly: ${err.message ?? err}`,
        },
      })
    );
}

/**
 * paint(root) renders the whole view from the current state. Pure
 * function of state → DOM; no data is stored in the DOM itself.
 */
function paint(root) {
  const s = getState();

  // Bridge badge: green "pong" when the round-trip worked.
  let bridgeBadge;
  if (s.bridge === null) {
    bridgeBadge = `<span class="badge off">checking…</span>`;
  } else if (s.bridge === "pong") {
    bridgeBadge = `<span class="badge ok">${escapeHTML(s.bridge)}</span>`;
  } else {
    bridgeBadge = `<span class="badge error">${escapeHTML(s.bridge)}</span>`;
  }

  // Ollama badge: green "detected: N models" or grey "not detected".
  // The tooltip (title attribute) carries the actionable detail string.
  let ollamaBadge;
  if (s.ollama === null) {
    ollamaBadge = `<span class="badge off">checking…</span>`;
  } else if (s.ollama.available) {
    const n = s.ollama.models?.length ?? 0;
    ollamaBadge = `<span class="badge ok" title="${escapeHTML(s.ollama.detail)}">detected: ${n} model${n === 1 ? "" : "s"}</span>`;
  } else {
    ollamaBadge = `<span class="badge off" title="${escapeHTML(s.ollama.detail)}">not detected — LLM features disabled</span>`;
  }

  root.innerHTML = `
    <div class="home">
      <h1>doc-anonymiser</h1>
      <p class="tagline">Local, offline anonymisation of .txt / .csv / .md documents.</p>
      <div class="status-card">
        <div class="status-row">
          <span class="status-label">Go bridge</span>
          ${bridgeBadge}
        </div>
        <div class="status-row">
          <span class="status-label">Ollama (optional AI)</span>
          ${ollamaBadge}
        </div>
      </div>
    </div>
  `;
}

/**
 * escapeHTML(text) neutralises HTML-special characters before injecting
 * strings into innerHTML. Status strings come from our own Go code today,
 * but future strings will contain document content — escaping everything
 * from day one prevents an entire class of injection bugs.
 */
function escapeHTML(text) {
  return String(text)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

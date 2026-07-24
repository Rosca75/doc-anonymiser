// main.js, the application shell: top navigation (Home / wizard / docs),
// the wizard step header, per-step explainer banner, active view,
// navigation footer, and the startup checks (bridge ping, Ollama probe,
// event subscriptions).
//
// The shell owns NO business state, it renders from state.js and defers
// every screen to its view module (one per screen, CLAUDE.md §3).

import { ping, probeOllama, onEvent, defaultAllowlist } from "./api.js";
import {
  getState, setState, subscribe,
  WIZARD_STEPS, canGoTo, goTo, goToScreen, nextStep, prevStep,
  applyImportResult, defaultUseAIFromProbe,
} from "./state.js";
import { escapeHTML } from "./html.js";
import { button, banner } from "./ui.js";
import { STEP_BANNERS } from "./copy.js";
import { renderHome } from "./views/home.js";
import { renderDocs } from "./views/docs.js";
import { renderImport } from "./views/import.js";
import { renderConfigure } from "./views/configure.js";
import { renderEntities } from "./views/entities.js";
import { renderRun } from "./views/run.js";
import { renderExport } from "./views/export.js";

// Step metadata for the header chips.
const STEP_LABELS = {
  import: "1 · Import",
  configure: "2 · Configure",
  entities: "3 · Entities",
  run: "4 · Run",
  export: "5 · Export",
};

// One renderer per wizard step. Each receives the container element.
const VIEWS = {
  import: renderImport,
  configure: renderConfigure,
  entities: renderEntities,
  run: renderRun,
  export: renderExport,
};

/**
 * boot(root) mounts the shell and kicks off the startup checks.
 * @param {HTMLElement} root the #app container from index.html.
 */
export function boot(root) {
  subscribe(() => paint(root));
  paint(root);

  // Bridge self-test: if "pong" never arrives the header shows the error.
  ping()
    .then((answer) => setState({ bridge: answer }))
    .catch((err) => setState({ bridge: `bridge error: ${err.message ?? err}` }));

  // Ollama probe: "not available" is a normal state (grey badge +
  // tooltip), never an error.
  probeOllama()
    .then((status) => {
      setState({ ollama: status });
      // First probe fills the "Use local AI" default: on when detected,
      // off otherwise. An explicit user choice is never overwritten
      // (BUILD-02 Phase 6d).
      defaultUseAIFromProbe(status?.available);
    })
    .catch((err) => setState({
      ollama: { available: false, models: [], detail: `Probe failed unexpectedly: ${err.message ?? err}` },
    }));

  // Seed the allowlist with the engine defaults so the user SEES them and
  // can remove any (BUILD-02 Phase 4b: nothing silent). Only on a fresh
  // state; a loaded session's list is never overwritten.
  defaultAllowlist()
    .then((terms) => {
      if (getState().allowlist.length === 0 && terms?.length) {
        setState({ allowlist: terms });
      }
    })
    .catch(() => { /* bridge missing (plain browser): keep the empty list */ });

  // Drag-and-drop imports arrive as events from Go (app.go OnFileDrop).
  onEvent("documents:changed", (result) => applyImportResult(result));

  // Pipeline progress/done events are consumed by the Run view via state.
  onEvent("pipeline:progress", (ev) => setState({ progress: ev }));
  onEvent("pipeline:done", (payload) => setState({
    running: false, progress: null,
    results: payload,
    // The placeholder → original mapping rides in the same payload
    // (BUILD-02 Phase 10a).
    mapping: payload?.mapping ?? null,
  }));

  // Discovery progress (BUILD-02 Phase 7c): the Go side emits one event
  // per scanned file; the entities view renders a determinate bar.
  onEvent("discovery:progress", (ev) => setState({
    discovery: {
      running: true,
      current: ev?.docIndex ?? 0,
      total: ev?.docCount ?? 0,
      file: ev?.docName ?? "",
    },
  }));

  // Keyboard shortcuts (Phase 10): Ctrl+O jumps to Import, Ctrl+E to
  // Export (guards still apply, Export needs results). The browser's own
  // Ctrl+O/Ctrl+E defaults are suppressed inside the app window.
  document.addEventListener("keydown", (ev) => {
    if (!(ev.ctrlKey || ev.metaKey)) return;
    if (ev.key === "o" || ev.key === "O") {
      ev.preventDefault();
      goToScreen("wizard");
      goTo("import");
    } else if (ev.key === "e" || ev.key === "E") {
      ev.preventDefault();
      goToScreen("wizard");
      goTo("export");
    }
  });
}

/** paint(root) renders the whole shell from the current state. */
function paint(root) {
  const s = getState();

  // Persistent top navigation: Home, Anonymise documents (the wizard),
  // Documentation (BUILD-02 Phase 2d). Ghost buttons; the active screen's
  // entry is visually quiet, never a second orange element.
  const topnav = `
    <nav class="topnav">
      ${button("Home", { kind: "ghost", id: "nav-home", icon: "home" })}
      ${button("Anonymise documents", { kind: "ghost", id: "nav-wizard", icon: "description" })}
      ${button("Documentation", { kind: "ghost", id: "nav-docs", icon: "menu_book" })}
    </nav>`;

  // Wizard chrome (step chips + footer) only appears on the wizard screen.
  const isWizard = s.screen === "wizard";
  const chips = isWizard ? WIZARD_STEPS.map((step) => {
    const active = s.step === step ? " active" : "";
    const enabled = canGoTo(step) ? "" : " disabled";
    return `<button class="chip${active}${enabled}" data-step="${step}" ${enabled ? "disabled" : ""}>${STEP_LABELS[step]}</button>`;
  }).join("") : "";

  root.innerHTML = `
    <header class="topbar">
      <div class="brand">doc-anonymiser</div>
      ${topnav}
      <nav class="steps">${chips}</nav>
      <div class="badges">
        ${bridgeBadge(s)}
        ${ollamaBadge(s)}
      </div>
    </header>
    <main id="view"></main>
    ${isWizard ? `
    <footer class="navbar">
      ${button("Back", { kind: "secondary", id: "nav-back", icon: "arrow_back", disabled: s.step === "import" })}
      ${button("Next", { kind: "primary", id: "nav-next", icon: "arrow_forward", disabled: !canAdvance(s) })}
    </footer>` : ""}
  `;

  root.querySelector("#nav-home").addEventListener("click", () => goToScreen("home"));
  root.querySelector("#nav-wizard").addEventListener("click", () => goToScreen("wizard"));
  root.querySelector("#nav-docs").addEventListener("click", () => goToScreen("docs"));

  const view = root.querySelector("#view");
  if (s.screen === "home") {
    renderHome(view);
    return;
  }
  if (s.screen === "docs") {
    renderDocs(view);
    return;
  }

  // Wizard: step chips navigate directly (guards enforced in goTo).
  for (const btn of root.querySelectorAll(".chip")) {
    btn.addEventListener("click", () => goTo(btn.dataset.step));
  }
  root.querySelector("#nav-back").addEventListener("click", prevStep);
  root.querySelector("#nav-next").addEventListener("click", nextStep);

  // Per-step explainer banner (BUILD-02 Phase 2e), then the active view
  // below it in its own container.
  const b = STEP_BANNERS[s.step];
  view.innerHTML = `${b ? banner(b.title, b.body, { icon: b.icon }) : ""}<div id="step-view"></div>`;
  VIEWS[s.step](view.querySelector("#step-view"));
}

/** canAdvance(s), is the linear "Next" allowed from the current step? */
function canAdvance(s) {
  const idx = WIZARD_STEPS.indexOf(s.step);
  return idx < WIZARD_STEPS.length - 1 && canGoTo(WIZARD_STEPS[idx + 1], s);
}

function bridgeBadge(s) {
  if (s.bridge === null) return `<span class="badge off">bridge…</span>`;
  if (s.bridge === "pong") return `<span class="badge ok" title="JS↔Go bridge OK">bridge</span>`;
  return `<span class="badge error" title="${escapeHTML(s.bridge)}">bridge</span>`;
}

function ollamaBadge(s) {
  if (s.ollama === null) return `<span class="badge off">Ollama…</span>`;
  if (s.ollama.available) {
    const n = s.ollama.models?.length ?? 0;
    return `<span class="badge ok" title="${escapeHTML(s.ollama.detail)}">Ollama · ${n} model${n === 1 ? "" : "s"}</span>`;
  }
  return `<span class="badge off" title="${escapeHTML(s.ollama.detail)}">Ollama off</span>`;
}

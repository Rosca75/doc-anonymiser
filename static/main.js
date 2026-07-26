// main.js, the application shell: the permanent top menu, the
// "Anonymisation workflow" banner, the per-step explainer banner, the
// active view, the navigation footer, and the startup checks (bridge
// ping, Ollama probe, event subscriptions).
//
// The shell owns NO business state, it renders from state.js and defers
// every screen to its view module (one per screen, CLAUDE.md §3). The
// header and banner MARKUP lives in shell.js so it can be unit-tested;
// this file only wires the handlers onto it (BUILD-04 Phase 2).

import { ping, probeOllama, onEvent, defaultAllowlist } from "./api.js";
import {
  getState, setState, subscribe,
  WIZARD_STEPS, canGoTo, goTo, goToScreen, nextStep, prevStep,
  applyImportResult, defaultUseAIFromProbe,
  isBackward, resetStep,
} from "./state.js";
import { escapeHTML } from "./html.js";
import { button, banner } from "./ui.js";
import { topnavHTML, workflowBannerHTML, showDocumentation } from "./shell.js";
import { STEP_BANNERS, NAV } from "./copy.js";
import { renderHome } from "./views/home.js";
import { renderImport } from "./views/import.js";
import { renderConfigure } from "./views/configure.js";
import { renderValues } from "./views/values.js";
import { renderRun } from "./views/run.js";
import { renderExport } from "./views/export.js";

// Step metadata for the header chips.
const STEP_LABELS = {
  import: "1 · Import",
  configure: "2 · Configure",
  values: "3 · Values",
  run: "4 · Run",
  export: "5 · Export",
};

// One renderer per wizard step. Each receives the container element.
const VIEWS = {
  import: renderImport,
  configure: renderConfigure,
  values: renderValues,
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

  // The top menu is IDENTICAL on every screen (CR4): same three buttons,
  // in the same order, with only a quiet highlight moving. Its markup
  // comes from shell.js, which is what shell.test.js asserts.
  const topnav = topnavHTML(s.screen);

  // The five step chips live in their own banner UNDER the menu (CR7),
  // and only while the wizard is on screen, so the header itself never
  // changes shape. The wizard footer follows the same rule.
  const isWizard = s.screen === "wizard";
  const workflow = isWizard
    ? workflowBannerHTML(WIZARD_STEPS, s.step, STEP_LABELS, (step) => canGoTo(step))
    : "";

  root.innerHTML = `
    <header class="topbar">
      <div class="brand">doc-anonymiser</div>
      ${topnav}
      <div class="badges">
        ${bridgeBadge(s)}
        ${ollamaBadge(s)}
      </div>
    </header>
    ${workflow}
    ${shellErrorBanner(s)}
    <main id="view"></main>
    ${isWizard ? `
    <footer class="navbar">
      ${button("Back", { kind: "secondary", id: "nav-back", icon: "arrow_back", disabled: s.step === "import" })}
      ${button("Next", { kind: "primary", id: "nav-next", icon: "arrow_forward", disabled: !canAdvance(s) })}
    </footer>` : ""}
  `;

  root.querySelector("#nav-home").addEventListener("click", () => goToScreen("home"));
  root.querySelector("#nav-wizard").addEventListener("click", () => goToScreen("wizard"));
  // Documentation is NOT a screen any more (BUILD-04 CR6): it opens in a
  // separate window served by the embedded asset server. A failure to
  // open it is a chrome-level problem, so it surfaces in the shell
  // banner rather than inside whichever view happens to be visible.
  root.querySelector("#nav-docs").addEventListener("click", showDocumentation);
  root.querySelector("#shell-error-dismiss")?.addEventListener("click", () => setState({ shellError: null }));

  const view = root.querySelector("#view");
  if (s.screen === "home") {
    renderHome(view);
    return;
  }

  // Wizard: step chips navigate directly (guards enforced in goTo). The
  // selector is scoped to the workflow banner so it can never pick up the
  // preset chips a view renders inside #view.
  for (const btn of root.querySelectorAll(".workflow-steps .chip")) {
    btn.addEventListener("click", () => navigateTo(btn.dataset.step));
  }
  root.querySelector("#nav-back").addEventListener("click", () => {
    const index = WIZARD_STEPS.indexOf(getState().step);
    if (index > 0) navigateTo(WIZARD_STEPS[index - 1]);
  });
  root.querySelector("#nav-next").addEventListener("click", nextStep);

  // Per-step explainer banner (BUILD-02 Phase 2e), then the active view
  // below it in its own container.
  // The per-step class on #step-view is a LAYOUT contract, not decoration
  // (BUILD-04 CR8): the import step needs its container to fill the
  // remaining height so the pane divider spans top to bottom even with an
  // empty preview. Every other step keeps content height. Doing it with a
  // class beats :has(), which needs a newer WebView than the pin assumes.
  const b = STEP_BANNERS[s.step];
  view.innerHTML = `${b ? banner(b.title, b.body, { icon: b.icon }) : ""}` +
    `<div id="step-view" class="step-view step-view-${escapeHTML(s.step)}"></div>`;
  VIEWS[s.step](view.querySelector("#step-view"));
}

/**
 * navigateTo(step) is the ONE way the shell moves the wizard, and the
 * place the backward-reset rule lives (BUILD-04 CR16).
 *
 * Forward moves are unchanged: the guard decides, nothing is cleared.
 * A BACKWARD move asks first, because the owner's report was that going
 * back could wipe the current step's work without warning. Now it is a
 * choice with two clear outcomes:
 *
 *   confirm  the step being LEFT is reset, then the wizard moves back
 *   cancel   nothing at all happens, not even the move
 *
 * Cancelling deliberately does NOT navigate. "Go back but keep
 * everything" sounds convenient, but it leaves half-finished state
 * behind that the user then walks forward into, which is the confusion
 * the reset exists to prevent. Imported documents are never touched
 * either way (state.js STEP_RESETS).
 *
 * @param {string} step the requested step token
 * @returns {boolean} whether the wizard moved
 */
export function navigateTo(step) {
  const current = getState().step;
  if (step === current) return false;
  if (!canGoTo(step)) return false;

  if (isBackward(current, step)) {
    if (!confirm(NAV.backConfirm(current))) return false;
    resetStep(current);
  }
  return goTo(step);
}

/** shellErrorBanner(s) renders the dismissible chrome-level error strip. */
function shellErrorBanner(s) {
  if (!s.shellError) return "";
  return `<div class="shell-error"><div class="banner error">` +
    `${escapeHTML(s.shellError)}` +
    `<button class="dismiss" id="shell-error-dismiss" title="Dismiss">\u2715</button>` +
    `</div></div>`;
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

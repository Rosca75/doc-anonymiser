// main.js, the application shell: the permanent top menu, the wizard step
// bar, the active view, the in-app confirm layer, and the startup checks
// (bridge ping, Ollama probe, event subscriptions).
//
// The shell owns NO business state, it renders from state.js and defers
// every screen to its view module (one per step, CLAUDE.md §3). The header
// and step-bar MARKUP lives in shell.js so it can be unit-tested; this file
// only wires the handlers onto it (BUILD-04 Phase 2).
//
// Two things the shell used to render are gone (BUILD-05 Phase 2):
//
//   the global Back/Next footer   Each screen owns its own footer now
//                                 (ui.js stepFooter). A shell-level bar
//                                 cannot say what the CURRENT screen still
//                                 needs before the user moves on, and that
//                                 sentence is the whole point of the footer.
//   the per-step explainer strip  Its copy became each card's subtitle, next
//                                 to the heading it explains, instead of a
//                                 band of prose above every screen eating the
//                                 vertical space the fixed-height workspace
//                                 needs.
//
// What the shell DOES still render globally is the confirm layer, because a
// question can be asked from any screen and has to survive the re-render its
// own answer triggers.

import { ping, probeOllama, onEvent, defaultAllowlist } from "./api.js";
import {
  getState, setState, subscribe,
  WIZARD_STEPS, canGoTo, goTo, goToScreen, knownStep,
  applyImportResult,
} from "./state.js";
import { escapeHTML } from "./html.js";
import { topnavHTML, stepbarHTML, headerActionsHTML, appFooterHTML, showDocumentation } from "./shell.js";
import { modalLayerHTML, wireModal } from "./modal.js";
import { navigateTo } from "./nav.js";
import { renderHome } from "./views/home.js";
import { renderImport } from "./views/import.js";
import { renderIdentify } from "./views/identify.js";
import { renderAnonymise } from "./views/anonymise.js";
import { renderExport } from "./views/export.js";

// STEP_LABELS: the visible label on each step-bar circle. The number itself
// comes from the step's position (shell.js stepbarHTML), so it is not repeated
// here; a hardcoded "2 ·" prefix was one more place a reorder could go stale.
//
// Cross-checked against WIZARD_STEPS by ../step_parity_test.go.
const STEP_LABELS = {
  import: "Import",
  identify: "Identify",
  anonymise: "Anonymise",
  export: "Export",
};

// One renderer per wizard step. Each receives the container element.
// Cross-checked against WIZARD_STEPS by ../step_parity_test.go: a step with
// no entry here renders a blank screen.
const VIEWS = {
  import: renderImport,
  identify: renderIdentify,
  anonymise: renderAnonymise,
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
      // Detecting Ollama ENABLES the Local AI switch, it never flips it
       // (BUILD-06): sending the document to a model, however local, is a
       // decision the user makes, not one made for them by an installation
       // they may have done for something else entirely.
      setState({ ollama: status });
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

  // Pipeline progress/done events are consumed by the Anonymise view via state.
  onEvent("pipeline:progress", (ev) => setState({ progress: ev }));
  onEvent("pipeline:done", (payload) => setState({
    running: false, progress: null,
    results: payload,
    // The placeholder → original mapping rides in the same payload
    // (BUILD-02 Phase 10a).
    mapping: payload?.mapping ?? null,
  }));

  // Discovery progress (BUILD-02 Phase 7c): the Go side emits one event
  // per scanned file; the Identify view renders a determinate bar.
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

  // The step bar lives in its own strip UNDER the menu (CR7), and only while
  // the wizard is on screen, so the header itself never changes shape.
  const isWizard = s.screen === "wizard";
  const stepbar = isWizard
    ? stepbarHTML(WIZARD_STEPS, s.step, STEP_LABELS, (step) => canGoTo(step))
    : "";

  root.innerHTML = `
    <header class="topbar">
      <div class="brand">Doc Anonymiser</div>
      ${topnav}
      ${headerActionsHTML(`${bridgeBadge(s)}${ollamaBadge(s)}`)}
    </header>
    ${stepbar}
    ${shellErrorBanner(s)}
    <main id="view"${isWizard ? "" : ` class="page"`}></main>
    ${appFooterHTML()}
    ${modalLayerHTML(s)}
  `;

  root.querySelector("#nav-home").addEventListener("click", () => goToScreen("home"));
  root.querySelector("#nav-wizard").addEventListener("click", () => goToScreen("wizard"));
  // Documentation is NOT a screen any more (BUILD-04 CR6): it opens in a
  // separate window served by the embedded asset server. A failure to
  // open it is a chrome-level problem, so it surfaces in the shell
  // banner rather than inside whichever view happens to be visible.
  root.querySelector("#nav-docs").addEventListener("click", showDocumentation);
  // The header's Help icon is a second entry point to the same action
  // (BUILD-05 CR2, from the Claude Design mockup). Settings has no
  // click handler yet: there is no settings surface to open, so the
  // button renders for layout only until that screen exists.
  root.querySelector("#header-help").addEventListener("click", showDocumentation);
  root.querySelector("#shell-error-dismiss")?.addEventListener("click", () => setState({ shellError: null }));

  // The confirm layer is wired on every paint: it is rendered at shell level
  // precisely so a question survives the re-render its own answer causes.
  wireModal(root);

  const view = root.querySelector("#view");
  if (s.screen === "home") {
    renderHome(view);
    return;
  }

  // Wizard: the step-bar items navigate directly (guards enforced in goTo).
  // The selector is scoped to the bar so it can never pick up the preset chips
  // a view renders inside #view.
  for (const item of root.querySelectorAll(".steps .step-item")) {
    item.addEventListener("click", () => navigateTo(item.dataset.step));
  }
  // The bar's back link leaves the flow for the landing page, which is what
  // "Anonymise Flow" points back at. Wizard state is untouched (goToScreen).
  root.querySelector("#stepbar-back").addEventListener("click", () => goToScreen("home"));

  // The active view fills the workspace. Each screen renders its own footer, so
  // there is nothing between the step bar and the screen itself.
  //
  // knownStep() is THE one call site of the unknown-token fallback (BUILD-05
  // decision 1). VIEWS is looked up unconditionally, so a token the store should
  // never hold (a corrupted persisted value, a typo in a caller) would otherwise
  // throw here and leave a blank screen with an exception behind it. Falling back
  // to Import costs nothing and is always a usable answer.
  VIEWS[knownStep(s.step)](view);
}

/** shellErrorBanner(s) renders the dismissible chrome-level error strip. */
function shellErrorBanner(s) {
  if (!s.shellError) return "";
  return `<div class="shell-error"><div class="banner error">` +
    `${escapeHTML(s.shellError)}` +
    `<button class="dismiss" id="shell-error-dismiss" title="Dismiss">✕</button>` +
    `</div></div>`;
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

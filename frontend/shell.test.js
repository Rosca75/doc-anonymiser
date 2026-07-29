// shell.test.js, markup tests for the application shell (BUILD-04 Phase 2).
//
// These lock the two shell rules that used to live as inline template
// strings in main.js and had no coverage at all:
//
//   CR4: the top menu is the SAME on every screen, only the highlight
//        moves. A regression here (a screen quietly dropping or adding an
//        entry) is exactly what the owner reported.
//   CR7: the step chips render inside .workflow-banner, never inside the
//        header, and only for the wizard.
//
// Run with `node --test frontend/*.test.js`.

import { test } from "node:test";
import assert from "node:assert/strict";

import { topnavHTML, workflowBannerHTML, TOPNAV_ITEMS } from "./shell.js";
import { WORKFLOW } from "./copy.js";

const STEPS = ["import", "configure", "values", "run", "export"];
const LABELS = {
  import: "1 · Import",
  configure: "2 · Configure",
  values: "3 · Values",
  run: "4 · Run",
  export: "5 · Export",
};

/** labelsOf(html) extracts the visible text of every top-menu button. */
function labelsOf(html) {
  return [...html.matchAll(/<button[^>]*>(.*?)<\/button>/gs)]
    .map((m) => m[1].replace(/<[^>]*>/g, "").trim());
}

// --- CR4: the permanent top menu -----------------------------------------

test("the top menu holds exactly Home, Anonymise documents, Documentation", () => {
  assert.deepEqual(
    TOPNAV_ITEMS.map((i) => i.label),
    ["Home", "Anonymise documents", "Documentation"]);
});

test("the top menu is identical on every screen, only the highlight moves", () => {
  // "docs" is included on purpose: it is a legacy screen value, and even
  // an unknown screen name must not change the button set.
  const screens = ["home", "wizard", "docs", "somethingUnexpected"];
  const rendered = screens.map((screen) => topnavHTML(screen));
  const labels = rendered.map(labelsOf);
  for (const set of labels) {
    assert.deepEqual(set, ["Home", "Anonymise documents", "Documentation"]);
  }
  // Ids and order are stable too, so main.js can always wire all three.
  for (const html of rendered) {
    for (const item of TOPNAV_ITEMS) {
      assert.ok(html.includes(`id="${item.id}"`), `${item.id} missing`);
    }
  }
});

test("the active screen's menu entry is marked quietly, never orange", () => {
  const home = topnavHTML("home");
  assert.match(home, /id="nav-home"[^>]*aria-current="page"/);
  assert.ok(home.includes("topnav-active"));
  // Quiet means ghost, not primary: no orange hero button in the chrome.
  assert.ok(!home.includes("btn-primary"));

  const wizard = topnavHTML("wizard");
  assert.match(wizard, /id="nav-wizard"[^>]*aria-current="page"/);
  assert.equal((wizard.match(/topnav-active/g) ?? []).length, 1,
    "exactly one entry may be highlighted");
});

test("Documentation is never marked current, it opens its own window", () => {
  for (const screen of ["home", "wizard", "docs"]) {
    const html = topnavHTML(screen);
    const docsButton = html.slice(html.indexOf(`id="nav-docs"`));
    assert.ok(!docsButton.startsWith(`id="nav-docs" `) || !docsButton.includes("aria-current"),
      "the documentation entry must not claim to be the current page");
  }
});

test("top-menu entries render as plain text, no icon (BUILD-05 CR3)", () => {
  const html = topnavHTML("home");
  assert.equal((html.match(/<svg/g) ?? []).length, 0);
});

// --- CR7: the Anonymisation workflow banner -------------------------------

test("the workflow banner is titled and carries all five step chips", () => {
  const html = workflowBannerHTML(STEPS, "import", LABELS, () => true);
  assert.ok(html.includes(`class="workflow-banner"`));
  assert.ok(html.includes(WORKFLOW.title));
  assert.equal(WORKFLOW.title, "Anonymisation workflow");
  for (const step of STEPS) {
    assert.ok(html.includes(`data-step="${step}"`), `${step} chip missing`);
    assert.ok(html.includes(LABELS[step]), `${step} label missing`);
  }
});

test("the chips live inside .workflow-steps, not in the header", () => {
  const html = workflowBannerHTML(STEPS, "import", LABELS, () => true);
  const chipArea = html.slice(html.indexOf(`class="workflow-steps"`));
  for (const step of STEPS) {
    assert.ok(chipArea.includes(`data-step="${step}"`));
  }
  assert.ok(!html.includes("topbar"), "the banner must not render header markup");
});

test("the active chip is marked and the guarded chips are disabled", () => {
  // Only import is reachable: the guard shape of a fresh session.
  const html = workflowBannerHTML(STEPS, "import", LABELS, (s) => s === "import");
  assert.match(html, /class="chip active"[^>]*data-step="import"/);
  assert.match(html, /data-step="import"[^>]*aria-current="step"/);
  for (const step of STEPS.slice(1)) {
    const chip = html.slice(html.indexOf(`data-step="${step}"`));
    assert.ok(chip.startsWith(`data-step="${step}" disabled`), `${step} must render disabled`);
  }
  assert.equal((html.match(/chip disabled/g) ?? []).length, STEPS.length - 1);
});

test("the workflow banner escapes labels and tokens", () => {
  const html = workflowBannerHTML(["a<b"], "a<b", { "a<b": `<img src=x>` }, () => true);
  assert.ok(!html.includes("<img"));
  assert.ok(html.includes("&lt;img"));
  assert.ok(html.includes(`data-step="a&lt;b"`));
});

// --- CR6: the documentation window ----------------------------------------

test("Documentation is a menu entry with no screen of its own", () => {
  // The in-app docs screen was retired: the entry opens a separate
  // window instead, so it must not carry a screen to navigate to.
  const docs = TOPNAV_ITEMS.find((i) => i.id === "nav-docs");
  assert.equal(docs.screen, null);
});

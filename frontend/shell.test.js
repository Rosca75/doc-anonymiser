// shell.test.js, markup tests for the application shell (BUILD-04 Phase 2).
//
// These lock the two shell rules that used to live as inline template
// strings in main.js and had no coverage at all:
//
//   CR4: the top menu is the SAME on every screen, only the highlight
//        moves. A regression here (a screen quietly dropping or adding an
//        entry) is exactly what the owner reported.
//   CR7: the step indicators render inside .stepbar, never inside the header,
//        and only for the wizard. BUILD-05 Phase 2 relaid that bar out as
//        numbered circles with a check mark for completed steps.
//
// Run with `node --test "frontend/**/*.test.js"`.

import { test } from "node:test";
import assert from "node:assert/strict";

import { topnavHTML, stepbarHTML, TOPNAV_ITEMS } from "./shell.js";
import { WORKFLOW } from "./copy.js";

const STEPS = ["import", "identify", "anonymise", "export"];
const LABELS = {
  import: "Import",
  identify: "Identify",
  anonymise: "Anonymise",
  export: "Export",
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

// --- CR7: the wizard step bar --------------------------------------------

test("the step bar carries every step, numbered, with a way out of the flow", () => {
  const html = stepbarHTML(STEPS, "import", LABELS, () => true);
  assert.ok(html.includes(`class="stepbar"`));
  // The bar's accessible name survives even though the visible title is gone.
  assert.ok(html.includes(`aria-label="${WORKFLOW.title}"`));
  assert.equal(WORKFLOW.title, "Anonymisation workflow");
  // The back link replaced the visible title.
  assert.ok(html.includes(`id="stepbar-back"`));
  assert.ok(html.includes(WORKFLOW.backToFlow));
  for (const step of STEPS) {
    assert.ok(html.includes(`data-step="${step}"`), `${step} missing`);
    assert.ok(html.includes(LABELS[step]), `${step} label missing`);
  }
});

test("the step number comes from the position, not from the label", () => {
  const html = stepbarHTML(STEPS, "import", LABELS, () => true);
  // On step 1 nothing is done yet, so every circle shows its own number.
  for (let i = 0; i < STEPS.length; i++) {
    assert.ok(html.includes(`<span class="step-circle">${i + 1}</span>`),
      `circle ${i + 1} missing`);
  }
});

test("steps behind the active one show a check mark instead of their number", () => {
  const html = stepbarHTML(STEPS, "anonymise", LABELS, () => true);
  // import and identify are behind anonymise: two done steps, so two check
  // marks and no "1" or "2" circle left.
  assert.equal((html.match(/step-item [^"]*done/g) ?? []).length, 2);
  assert.ok(!html.includes(`<span class="step-circle">1</span>`));
  assert.ok(!html.includes(`<span class="step-circle">2</span>`));
  // The check mark is decorative: the label beside it already names the step.
  assert.ok(html.includes(`class="icon" aria-hidden="true"`));
  // export is still ahead, so it keeps its number.
  assert.ok(html.includes(`<span class="step-circle">4</span>`));
});

test("the indicators live inside .steps, not in the header", () => {
  const html = stepbarHTML(STEPS, "import", LABELS, () => true);
  const stepArea = html.slice(html.indexOf(`class="steps"`));
  for (const step of STEPS) {
    assert.ok(stepArea.includes(`data-step="${step}"`));
  }
  assert.ok(!html.includes("topbar"), "the bar must not render header markup");
});

test("the active step is marked and the guarded steps are disabled", () => {
  // Only import is reachable: the guard shape of a fresh session.
  const html = stepbarHTML(STEPS, "import", LABELS, (s) => s === "import");
  assert.match(html, /class="step-item active"[^>]*data-step="import"/);
  assert.match(html, /data-step="import"[^>]*aria-current="step"/);
  for (const step of STEPS.slice(1)) {
    const item = html.slice(html.indexOf(`data-step="${step}"`));
    assert.ok(item.startsWith(`data-step="${step}" disabled`), `${step} must render disabled`);
  }
  assert.equal((html.match(/step-item disabled/g) ?? []).length, STEPS.length - 1);
});

test("an unknown active step marks nothing done rather than marking everything", () => {
  // A step token the bar does not recognise must not read as "all complete".
  const html = stepbarHTML(STEPS, "nonesuch", LABELS, () => true);
  assert.ok(!html.includes("done"));
  assert.ok(!html.includes("active"));
});

test("the step bar escapes labels and tokens", () => {
  const html = stepbarHTML(["a<b"], "a<b", { "a<b": `<img src=x>` }, () => true);
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

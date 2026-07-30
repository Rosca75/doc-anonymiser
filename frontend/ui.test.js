// ui.test.js, unit tests for the shared UI toolkit (BUILD-02 Phase 1).
// Run with `node --test frontend/` (dev-time check, zero npm dependencies).

import { test } from "node:test";
import assert from "node:assert/strict";
import {
  button, panel, icon,
  card, countBadge, tabbar, chipRow, sectionLabel, statTile,
  collapsibleGroup, stepFooter, toastHTML, modalHTML,
} from "./ui.js";

// --- button -----------------------------------------------------------

test("button renders each kind with the right classes", () => {
  const cases = [
    ["primary", 'class="btn btn-primary primary"'],
    ["secondary", 'class="btn btn-secondary"'],
    ["ghost", 'class="btn btn-ghost"'],
  ];
  for (const [kind, want] of cases) {
    const html = button("Go", { kind });
    assert.ok(html.includes(want), `${kind}: ${html}`);
  }
});

test("button defaults to secondary", () => {
  assert.ok(button("Go").includes("btn-secondary"));
});

test("button renders disabled and title attributes", () => {
  const html = button("Go", { disabled: true, title: "why not" });
  assert.ok(html.includes("disabled"));
  assert.ok(html.includes('title="why not"'));
  assert.ok(!button("Go").includes("disabled"));
});

test("button escapes label and title", () => {
  const html = button("<script>x</script>", { title: `a"b<c>` });
  assert.ok(!html.includes("<script>"));
  assert.ok(html.includes("&lt;script&gt;"));
  assert.ok(html.includes("a&quot;b&lt;c&gt;"));
});

test("button includes icon markup and id", () => {
  const html = button("Next", { icon: "arrow_forward", id: "nav-next" });
  assert.ok(html.includes('id="nav-next"'));
  assert.ok(html.includes("<svg"));
});

test("button renders data attributes escaped", () => {
  const html = button("x", { data: { name: `a"b` } });
  assert.ok(html.includes('data-name="a&quot;b"'));
});

// --- icon --------------------------------------------------------------

test("icon returns svg for known names and empty string for unknown", () => {
  assert.ok(icon("home").includes("<svg"));
  assert.equal(icon("no_such_icon"), "");
});

// --- panel --------------------------------------------------------------

test("panel renders title, body and id", () => {
  const html = panel("p1", "Allowlist", "<p>body</p>");
  assert.ok(html.includes('id="p1"'));
  assert.ok(html.includes("Allowlist"));
  assert.ok(html.includes("<p>body</p>"));
  // Non-collapsible panels carry no data-collapsed attribute.
  assert.ok(!html.includes("data-collapsed"));
});

test("collapsible panel renders data-collapsed per default and toggle set", () => {
  const none = new Set();
  // startOpen default: open until toggled.
  assert.ok(panel("p1", "T", "b", { collapsible: true, collapsedSet: none })
    .includes('data-collapsed="false"'));
  // toggled away from open default: collapsed.
  assert.ok(panel("p1", "T", "b", { collapsible: true, collapsedSet: new Set(["p1"]) })
    .includes('data-collapsed="true"'));
  // startOpen false: collapsed until toggled.
  assert.ok(panel("p2", "T", "b", { collapsible: true, startOpen: false, collapsedSet: none })
    .includes('data-collapsed="true"'));
  assert.ok(panel("p2", "T", "b", { collapsible: true, startOpen: false, collapsedSet: new Set(["p2"]) })
    .includes('data-collapsed="false"'));
});

test("panel escapes id and title", () => {
  const html = panel(`x"y`, "<b>t</b>", "");
  assert.ok(html.includes('id="x&quot;y"'));
  assert.ok(html.includes("&lt;b&gt;t&lt;/b&gt;"));
});

// --- Icon alignment contract (BUILD-04 CR5) --------------------------------
//
// Alignment itself is visual and cannot be asserted without a browser, so
// these lock the CONTRACT the fix relies on: the helper emits a .icon span,
// and style.css centres both that span and its button parent on the cross
// axis instead of nudging the icon below the baseline with a magic offset.

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const staticDir = path.dirname(fileURLToPath(import.meta.url));
const styleCSS = fs.readFileSync(path.join(staticDir, "style.css"), "utf8");

test("icon() emits a span carrying the .icon class", () => {
  assert.match(icon("home"), /^<span class="icon" aria-hidden="true">/);
  assert.match(button("Home", { icon: "home" }), /<span class="icon"/);
});

test("buttons centre their icon and label on the cross axis", () => {
  const rule = styleCSS.match(/\nbutton, \.btn \{[^}]*\}/);
  assert.ok(rule, "the shared button rule must exist");
  assert.match(rule[0], /display:\s*inline-flex/);
  assert.match(rule[0], /align-items:\s*center/);
});

test("the icon rule centres rather than nudging below the baseline", () => {
  const rule = styleCSS.match(/\n\.icon \{[^}]*\}/);
  assert.ok(rule, "the shared .icon rule must exist");
  assert.match(rule[0], /align-items:\s*center/);
  assert.match(rule[0], /vertical-align:\s*middle/);
  assert.ok(!/vertical-align:\s*-/.test(styleCSS),
    "no negative vertical-align nudge may remain (BUILD-04 CR5)");
});

test("the step bar has its own styling, separate from the header", () => {
  assert.match(styleCSS, /\.stepbar \{/);
  assert.match(styleCSS, /\.steps \{/);
  // The bar is fixed-height chrome: it must not wrap or grow (BUILD-05).
  const bar = styleCSS.match(/\.stepbar \{[^}]*\}/);
  assert.match(bar[0], /height:\s*var\(--chrome-stepbar\)/);
  assert.match(bar[0], /flex:\s*none/);
  assert.ok(!bar[0].includes("flex-wrap"), "the step bar must never wrap");
  // The active top-menu entry stays quiet: no accent colour in its rule.
  const active = styleCSS.match(/\.topnav \.topnav-active \{[^}]*\}/);
  assert.ok(active, "the quiet active-entry rule must exist");
  assert.ok(!active[0].includes("--accent"),
    "the active menu entry must not use the accent orange");
});

test("the active step is marked with an outline, never an orange flood fill", () => {
  // One loud element per view, and it is the view's primary button, not the
  // chrome (brand rule). So the accent may colour the underline, the circle's
  // border and the circle's number, but never a background.
  const rules = [...styleCSS.matchAll(/\.step-item\.active[^{]*\{([^}]*)\}/g)].map((m) => m[1]);
  assert.ok(rules.length > 0, "the active-step rules must exist");
  for (const body of rules) {
    assert.ok(!/background:[^;]*--accent/.test(body),
      `an active step must not be filled with the accent: ${body}`);
  }
  assert.ok(rules.some((b) => /border-bottom-color:\s*var\(--accent\)/.test(b)));
});

test("the workflow banner is gone, replaced by the step bar", () => {
  // Phase 2 renamed it. A leftover rule would style markup nothing emits.
  assert.ok(!styleCSS.includes(".workflow-banner"));
  assert.ok(!styleCSS.includes(".workflow-steps"));
  // The global navigation footer is gone too: each screen owns its own.
  assert.ok(!/^\.navbar \{/m.test(styleCSS));
  // So is the per-step explainer strip; its copy moved into card subtitles.
  assert.ok(!styleCSS.includes(".step-banner"));
});

test("button renders aria-current only when asked", () => {
  assert.match(button("Home", { current: true }), /aria-current="page"/);
  assert.ok(!button("Home").includes("aria-current"));
});

// --- Import screen layout (BUILD-05 Phase 4) -----------------------------
//
// BUILD-04 CR8 fixed a pane divider that collapsed to its content height. The
// divider is GONE (BUILD-05 decision 6): the mock-up uses a fixed two-column
// grid, so the height chain that used to need pinning is now the shared card
// contract, asserted above. What is left to pin here is that the divider and
// its state did not survive as dead CSS.

test("the import pane divider is gone, not merely unused", () => {
  assert.ok(!styleCSS.includes(".import-divider"),
    "the draggable divider was dropped by decision 6");
  assert.ok(!styleCSS.includes(".import-columns"),
    "the resizable grid was replaced by .workspace-import");
  assert.ok(!styleCSS.includes(".step-view-import"),
    "the per-step layout class went with the per-step container");
});

test("the import screen uses the shared fixed two-column workspace", () => {
  const grid = styleCSS.match(/\.workspace-import \{[^}]*\}/);
  assert.ok(grid, "the .workspace-import rule must exist");
  assert.match(grid[0], /grid-template-columns/);
});

test("the selected document row is marked with a bar, not an accent flood fill", () => {
  // One loud element per view, and on the import screen it is ADD FILES.
  const selected = styleCSS.match(/\.doc-row\.selected \{[^}]*\}/);
  assert.ok(selected, "the selected-row rule must exist");
  assert.ok(!/background:\s*var\(--accent\)/.test(selected[0]),
    "a selected row must not be filled with the accent orange");
  assert.match(styleCSS, /\.doc-row\.selected \.doc-bar \{[^}]*background:\s*var\(--accent\)/);
});

test("a wide grid preview scrolls sideways instead of widening the page", () => {
  assert.match(styleCSS, /\.table-scroll \{[^}]*overflow-x:\s*auto/);
  // The preview must NOT cap its own height: the card body is the scroll owner,
  // so a max-height here would leave the card scrolling something that never
  // overflows.
  const preview = styleCSS.match(/\.md-preview \{[^}]*\}/);
  assert.ok(preview, "the .md-preview rule must exist");
  assert.ok(!preview[0].includes("max-height"),
    "the card body owns the scrolling, not the preview element");
});

test("the documentation page styles itself from the brand tokens only", () => {
  const docsCSS = fs.readFileSync(path.join(staticDir, "docs", "docs.css"), "utf8");
  // No literal colour or font value: brand.css stays the single source.
  assert.ok(!/#[0-9a-fA-F]{3,8}\b/.test(docsCSS), "no literal colours in docs.css");
  assert.match(docsCSS, /var\(--font-body\)/);
  const docsHTML = fs.readFileSync(path.join(staticDir, "docs", "index.html"), "utf8");
  // Local-only guarantee: no remote stylesheet, script or font.
  assert.ok(!/https?:\/\//.test(docsHTML.replace(/127\.0\.0\.1/g, "")),
    "the documentation page must reference no remote URL");
  assert.match(docsHTML, /href="\.\.\/brand\.css"/);
});

// --- Variant chip contract (BUILD-04 CR17) --------------------------------
//
// Reported symptom: the variant chips looked disabled and could not be
// dragged. Both were CSS, not markup: the chips inherited the muted
// colour of the variant area, and nothing said they were draggable. The
// markup side is asserted in views/values.js by reading the source, since
// the chip builder is not exported and there is no DOM here.

const workspaceJS = fs.readFileSync(path.join(staticDir, "views", "identifyworkspace.js"), "utf8");

test("variant chips are rendered draggable", () => {
  assert.match(workspaceJS, /class="pill variant-chip" draggable="true"/);
});

test("the move button inside a chip does not swallow the drag", () => {
  // A draggable child would start its own (non) drag over most of the
  // chip's width, which is why the button opts out explicitly.
  assert.match(workspaceJS, /class="variant-move" draggable="false"/);
});

test("variant chips are not styled as disabled", () => {
  const chip = styleCSS.match(/\.variant-list \.variant-chip \{[^}]*\}/);
  assert.ok(chip, "the chip rule must exist");
  // The chip must override the muted colour its container carries.
  assert.match(chip[0], /color:\s*var\(--text\)/);
  assert.ok(!/opacity/.test(chip[0]), "a chip must never be dimmed, it is a live control");
  // And it must say it can be dragged before the user tries.
  assert.match(chip[0], /cursor:\s*grab/);
});

test("a drop target is visibly marked", () => {
  assert.match(styleCSS, /tr\.drop-target > td \{/);
});

// =====================================================================
// The BUILD-05 card kit.
// =====================================================================

// --- card -------------------------------------------------------------

test("card renders head, body and foot in order", () => {
  const html = card({
    title: "Documents", subtitle: "3 files",
    bodyHTML: "<p>BODY</p>", footHTML: "<b>FOOT</b>",
  });
  assert.ok(html.startsWith('<section class="card"'), html);
  assert.ok(html.includes("<h2>Documents</h2>"));
  assert.ok(html.includes('<span class="card-sub">3 files</span>'));
  // Order matters: the head and foot must not scroll, the body must.
  assert.ok(html.indexOf("card-head") < html.indexOf("card-body"));
  assert.ok(html.indexOf("card-body") < html.indexOf("card-foot"));
});

test("card omits the head entirely when there is nothing to put in it", () => {
  const html = card({ bodyHTML: "x" });
  assert.ok(!html.includes("card-head"), html);
  assert.ok(html.includes('<div class="card-body">x</div>'));
});

test("card escapes the title, subtitle and caption but trusts the body markup", () => {
  const html = card({
    title: "<b>t</b>", subtitle: `a"b`, caption: "<i>c</i>",
    bodyHTML: "<p>trusted</p>",
  });
  assert.ok(!html.includes("<b>t</b>"));
  assert.ok(html.includes("&lt;b&gt;t&lt;/b&gt;"));
  assert.ok(html.includes("a&quot;b"));
  assert.ok(html.includes("&lt;i&gt;c&lt;/i&gt;"));
  assert.ok(html.includes("<p>trusted</p>"));
});

test("card headRightHTML replaces the caption and switches the head to controls", () => {
  const html = card({ title: "t", caption: "IGNORED", headRightHTML: "<button>x</button>" });
  assert.ok(html.includes("with-controls"));
  assert.ok(html.includes("<button>x</button>"));
  assert.ok(!html.includes("IGNORED"));
});

test("card marks a key-bearing card and places non-scrolling slots correctly", () => {
  const html = card({
    title: "t", keyBearing: true, bodyCls: "stack",
    beforeBodyHTML: "<nav>TABS</nav>", bodyHTML: "B", afterBodyHTML: "<div>NOTICE</div>",
  });
  assert.ok(html.includes('class="card key-bearing"'));
  assert.ok(html.includes('class="card-body stack"'));
  // A tab bar goes above the body, a notice strip below it: neither scrolls.
  assert.ok(html.indexOf("<nav>TABS</nav>") < html.indexOf("card-body"));
  assert.ok(html.indexOf("card-body") < html.indexOf("<div>NOTICE</div>"));
});

// --- countBadge -------------------------------------------------------

test("countBadge renders zero but renders nothing for a missing count", () => {
  assert.ok(countBadge(0).includes(">0<"));
  assert.equal(countBadge(null), "");
  assert.equal(countBadge(undefined), "");
});

test("countBadge marks the active variant", () => {
  assert.ok(countBadge(4, { active: true }).includes('class="count-badge active"'));
  assert.ok(countBadge(4).includes('class="count-badge"'));
  assert.ok(!countBadge(4).includes("active"));
});

// --- tabbar -----------------------------------------------------------

test("tabbar marks the active tab and carries the data attribute", () => {
  const html = tabbar([
    { id: "suggestions", label: "Suggestions", count: 8, active: true },
    { id: "values", label: "My values", count: 3 },
  ], { ariaLabel: "Identify sections" });
  assert.ok(html.includes('aria-label="Identify sections"'));
  assert.ok(html.includes('data-tab="suggestions"'));
  assert.ok(html.includes('class="tab active"'));
  assert.ok(html.includes('aria-current="true"'));
  // The active tab's badge is the active badge, the inactive one's is not.
  assert.ok(html.includes('<span class="count-badge active">8</span>'));
  assert.ok(html.includes('<span class="count-badge">3</span>'));
});

test("tabbar renders a disabled tab as disabled and escapes labels", () => {
  const html = tabbar([{ id: "a", label: "<x>", disabled: true, title: "why" }]);
  assert.ok(html.includes("disabled"));
  assert.ok(html.includes('title="why"'));
  assert.ok(!html.includes("<x>"));
});

test("tabbar honours a custom data attribute so two rows can coexist", () => {
  assert.ok(tabbar([{ id: "scope", label: "Scope" }], { attr: "railtab" })
    .includes('data-railtab="scope"'));
});

test("tabbar renders an empty row for no tabs rather than throwing", () => {
  assert.equal(tabbar([]), '<nav class="tabbar"></nav>');
  assert.equal(tabbar(undefined), '<nav class="tabbar"></nav>');
});

// --- chipRow ----------------------------------------------------------

test("chipRow marks the active chip and reports it to assistive technology", () => {
  const html = chipRow([
    { id: "soft", label: "Soft" },
    { id: "medium", label: "Standard", active: true },
  ], { ariaLabel: "Preset" });
  assert.ok(html.includes('data-chip="medium"'));
  assert.ok(html.includes('class="tint-chip active"'));
  assert.ok(html.includes('aria-pressed="true"'));
  assert.ok(html.includes('role="group" aria-label="Preset"'));
});

test("chipRow square variant and disabled chips", () => {
  const html = chipRow([{ id: "cloud", label: "Cloud AI", disabled: true }], { square: true });
  assert.ok(html.includes("tint-chip square"));
  assert.ok(html.includes("disabled"));
});

// --- sectionLabel / statTile -------------------------------------------

test("sectionLabel escapes and supports the mini variant", () => {
  assert.ok(sectionLabel("Document country").includes('class="section-label">Document country<'));
  assert.ok(sectionLabel("x", { mini: true }).includes("section-label mini"));
  assert.ok(sectionLabel("<b>").includes("&lt;b&gt;"));
});

test("statTile puts the value before its label and escapes both", () => {
  const html = statTile(12, "REPLACEMENTS");
  assert.ok(html.indexOf("stat-value") < html.indexOf("stat-label"));
  assert.ok(html.includes(">12<"));
  assert.ok(statTile("<b>", "<i>").includes("&lt;b&gt;"));
});

// --- collapsibleGroup --------------------------------------------------

test("collapsibleGroup defaults to open and reports its state twice", () => {
  const html = collapsibleGroup("names", "Names", "<p>rows</p>");
  // data-open drives the CSS, aria-expanded drives the screen reader; both
  // have to agree or the two audiences see different things.
  assert.ok(html.includes('data-open="true"'));
  assert.ok(html.includes('aria-expanded="true"'));
  assert.ok(!html.includes("chevron closed"));
  assert.ok(html.includes("<p>rows</p>"));
});

test("collapsibleGroup closed state turns the chevron and hides nothing itself", () => {
  const html = collapsibleGroup("names", "Names", "<p>rows</p>", { open: false });
  assert.ok(html.includes('data-open="false"'));
  assert.ok(html.includes('aria-expanded="false"'));
  assert.ok(html.includes("chevron closed"));
  // The body is still IN the markup: CSS hides it, so toggling costs no
  // re-render of the rows themselves.
  assert.ok(html.includes("<p>rows</p>"));
});

test("collapsibleGroup renders the count and header actions, and escapes the title", () => {
  const html = collapsibleGroup("g", "<b>t</b>", "", {
    countLabel: "3/6", headRightHTML: "<button>all</button>",
  });
  assert.ok(html.includes('<span class="cgroup-count">3/6</span>'));
  assert.ok(html.includes("<button>all</button>"));
  assert.ok(!html.includes("<b>t</b>"));
  assert.ok(html.includes('data-group-toggle="g"'));
});

// --- stepFooter -------------------------------------------------------

test("stepFooter renders the back link, the hint and the primary action", () => {
  const html = stepFooter({
    backLabel: "Back to Import", backId: "back",
    hint: "6 values ready to replace",
    nextLabel: "CONTINUE TO ANONYMISE", nextId: "next",
  });
  assert.ok(html.includes("Back to Import"));
  assert.ok(html.includes('id="back"'));
  assert.ok(html.includes("6 values ready to replace"));
  // The primary button is the one loud element on the screen.
  assert.ok(html.includes("btn-primary"));
  assert.ok(html.includes('id="next"'));
  assert.ok(html.indexOf("Back to Import") < html.indexOf("CONTINUE"));
});

test("stepFooter keeps the primary right-justified on step one", () => {
  const html = stepFooter({ nextLabel: "CONTINUE TO IDENTIFY", nextId: "n" });
  // No back link on step 1, but the empty span has to stay so the flex
  // space-between still pushes the primary to the right.
  assert.ok(html.includes("<span></span>"));
  assert.ok(!html.includes("step-back"));
});

test("stepFooter gates the primary with a reason", () => {
  const html = stepFooter({
    nextLabel: "CONTINUE TO EXPORT", nextId: "n",
    nextDisabled: true, nextTitle: "Run the anonymisation first",
  });
  assert.ok(html.includes("disabled"));
  assert.ok(html.includes('title="Run the anonymisation first"'));
});

test("stepFooter standalone renders as its own card", () => {
  assert.ok(stepFooter({ nextLabel: "x", nextId: "n", standalone: true })
    .includes("step-footer standalone"));
});

// --- toastHTML --------------------------------------------------------

test("toastHTML renders nothing without a notice", () => {
  assert.equal(toastHTML(null), "");
  assert.equal(toastHTML({ text: "" }), "");
  assert.equal(toastHTML(undefined), "");
});

test("toastHTML maps each tone onto its own class", () => {
  assert.ok(toastHTML({ text: "saved", tone: "ok" }).includes("notice notice-ok"));
  assert.ok(toastHTML({ text: "fyi", tone: "info" }).includes("notice notice-info"));
  assert.ok(toastHTML({ text: "careful", tone: "warn" }).includes("notice notice-warn"));
});

test("toastHTML degrades an unknown tone to info instead of dropping the sentence", () => {
  const html = toastHTML({ text: "still shown", tone: "purple" });
  assert.ok(html.includes("notice-info"), html);
  assert.ok(html.includes("still shown"));
});

test("toastHTML escapes the text and wires a dismiss button", () => {
  const html = toastHTML({ text: "<script>x</script>", tone: "ok" });
  assert.ok(!html.includes("<script>"));
  assert.ok(html.includes('id="notice-dismiss"'));
  assert.ok(html.includes('role="status"'));
});

// --- modalHTML --------------------------------------------------------

test("modalHTML renders nothing when nothing is being asked", () => {
  assert.equal(modalHTML(null), "");
  assert.equal(modalHTML(undefined), "");
});

test("modalHTML renders a dialog with both fixed button ids", () => {
  const html = modalHTML({
    title: "Export the value mapping as CSV",
    body: "This file contains the re-identification key.",
    confirmLabel: "Export CSV",
  });
  assert.ok(html.includes('role="dialog" aria-modal="true"'));
  assert.ok(html.includes('aria-label="Export the value mapping as CSV"'));
  assert.ok(html.includes("This file contains the re-identification key."));
  assert.ok(html.includes('id="modal-confirm"'));
  assert.ok(html.includes('id="modal-cancel"'));
  assert.ok(html.includes("Export CSV"));
  assert.ok(html.includes("Cancel"), "cancel label defaults");
});

test("modalHTML tints a key-bearing question and escapes its copy", () => {
  const plain = modalHTML({ title: "t", body: "b" });
  assert.ok(!plain.includes("key-bearing"));
  const key = modalHTML({ title: "<b>t</b>", body: `a"b`, keyBearing: true });
  assert.ok(key.includes('class="modal key-bearing"'));
  assert.ok(!key.includes("<b>t</b>"));
  assert.ok(key.includes("a&quot;b"));
});

// --- The fixed-height layout contract (BUILD-05 Phase 1) ----------------
//
// These read style.css because the contract IS the CSS: markup alone cannot
// express "the body is the only scroll owner". Each assertion pins one link of
// the chain, because the symptom of a missing link is subtle (the page body
// scrolls instead of the card) and easy to reintroduce.

test("the card body is the only scroll owner in a card", () => {
  assert.match(styleCSS, /\.card-body \{[^}]*min-height:\s*0/,
    "min-height: 0 is what lets the body shrink and scroll");
  assert.match(styleCSS, /\.card-body \{[^}]*overflow-y:\s*auto/);
  // The head and the foot must never scroll away.
  assert.match(styleCSS, /\.card-head \{[^}]*flex:\s*none/);
  assert.match(styleCSS, /\.card-foot \{[^}]*flex:\s*none/);
});

test("the workspace and the card both refuse to grow past the window", () => {
  assert.match(styleCSS, /\.workspace \{[^}]*min-height:\s*0/);
  assert.match(styleCSS, /^\.card \{[^}]*min-height:\s*0/m);
});

test("a preview pane scrolls in both directions so wide content never widens the page", () => {
  assert.match(styleCSS, /\.card-body\.pane \{[^}]*overflow:\s*auto/);
});

test("the chrome heights match the mock-ups", () => {
  // 80px header, 68px step bar, 60px footer, expressed in rem against the
  // 16px root so a user's font-size setting scales the whole shell together.
  assert.match(styleCSS, /--chrome-header:\s*5rem/);
  assert.match(styleCSS, /--chrome-stepbar:\s*4\.25rem/);
  assert.match(styleCSS, /--chrome-footer:\s*3\.75rem/);
});

test("brand.css declares the functional tint pairs the kit consumes", () => {
  const brandCSS = fs.readFileSync(path.join(staticDir, "brand.css"), "utf8");
  for (const token of [
    "--ink-2", "--ink-3", "--surface-subtle",
    "--src-smart-fg", "--src-ai-fg", "--src-pattern-fg",
    "--valid-fg", "--invalid-fg",
    "--notice-ok-fg", "--notice-info-fg", "--notice-warn-fg",
    "--caution-fg", "--experimental-fg", "--key-fg",
  ]) {
    assert.ok(brandCSS.includes(token + ":"), `brand.css must declare ${token}`);
  }
  // The two greys BUILD-05 adds come straight out of the brand palette, so
  // pin the exact values: a "close enough" grey is off-brand.
  assert.match(brandCSS, /--ink-2:\s*#54616C/);
  assert.match(brandCSS, /--ink-3:\s*#717C8D/);
});

test("every functional tint the kit references is declared in brand.css", () => {
  // Catches the other half of the same mistake: a style.css rule reaching for
  // a var() nobody declared renders as "no colour at all", silently.
  const brandCSS = fs.readFileSync(path.join(staticDir, "brand.css"), "utf8");
  const used = new Set();
  for (const m of styleCSS.matchAll(/var\((--[a-z0-9-]+)\)/g)) used.add(m[1]);
  const missing = [...used].filter((token) => !brandCSS.includes(token + ":") &&
    !styleCSS.includes(token + ":")).sort();
  assert.deepEqual(missing, [], `style.css uses undeclared tokens: ${missing.join(", ")}`);
});

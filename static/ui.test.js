// ui.test.js, unit tests for the shared UI toolkit (BUILD-02 Phase 1).
// Run with `node --test static/` (dev-time check, zero npm dependencies).

import { test } from "node:test";
import assert from "node:assert/strict";
import { button, banner, panel, icon } from "./ui.js";

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

// --- banner -------------------------------------------------------------

test("banner renders title, body and escapes content", () => {
  const html = banner("Import", "Add <files> here.", { icon: "upload_file" });
  assert.ok(html.includes("step-banner"));
  assert.ok(html.includes("Import"));
  assert.ok(html.includes("Add &lt;files&gt; here."));
  assert.ok(html.includes("<svg"));
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

test("the workflow banner has its own styling, separate from the header", () => {
  assert.match(styleCSS, /\.workflow-banner \{/);
  assert.match(styleCSS, /\.workflow-steps \{/);
  // The active top-menu entry stays quiet: no accent colour in its rule.
  const active = styleCSS.match(/\.topnav \.topnav-active \{[^}]*\}/);
  assert.ok(active, "the quiet active-entry rule must exist");
  assert.ok(!active[0].includes("--accent"),
    "the active menu entry must not use the accent orange");
});

test("button renders aria-current only when asked", () => {
  assert.match(button("Home", { current: true }), /aria-current="page"/);
  assert.ok(!button("Home").includes("aria-current"));
});

// --- Import pane height contract (BUILD-04 CR8) ----------------------------
//
// The bug: .import-columns collapsed to its content height, so the pane
// divider only reached full height once a preview was loaded. The fix is a
// height chain from #view down to the grid. Layout cannot be measured
// without a browser, so this asserts the chain is declared.

test("the import step view fills the available height", () => {
  const view = styleCSS.match(/main#view \{[^}]*\}/);
  assert.ok(view, "the #view rule must exist");
  assert.match(view[0], /display:\s*flex/);
  assert.match(view[0], /flex-direction:\s*column/);

  const fill = styleCSS.match(/\.step-view-import \{[^}]*\}/);
  assert.ok(fill, "the import step view must claim the leftover height");
  assert.match(fill[0], /flex:\s*1/);
  assert.match(fill[0], /min-height:\s*0/);

  assert.match(styleCSS, /\.step-view-import \.import-columns \{[^}]*flex:\s*1/);
});

test("the import grid stretches its row so the divider spans it", () => {
  const columns = styleCSS.match(/\n\.import-columns \{[^}]*\}/);
  assert.ok(columns, "the .import-columns rule must exist");
  assert.match(columns[0], /align-items:\s*stretch/);
  assert.ok(!columns[0].includes("align-items: start"),
    "align-items: start is what collapsed the row (BUILD-04 CR8)");
  assert.match(styleCSS, /\.import-divider \{[^}]*align-self:\s*stretch/);
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

const valuesJS = fs.readFileSync(path.join(staticDir, "views", "values.js"), "utf8");

test("variant chips are rendered draggable", () => {
  assert.match(valuesJS, /class="pill variant-chip" draggable="true"/);
});

test("the move button inside a chip does not swallow the drag", () => {
  // A draggable child would start its own (non) drag over most of the
  // chip's width, which is why the button opts out explicitly.
  assert.match(valuesJS, /class="variant-move" draggable="false"/);
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

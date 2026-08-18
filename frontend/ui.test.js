// ui.test.js, unit tests for the shared UI toolkit.
// Run with `node --test "frontend/**/*.test.js"` (dev-time check, zero npm dependencies).

import { test } from "node:test";
import assert from "node:assert/strict";
import {
  button, icon,
  card, countBadge, tabbar, chipRow, sectionLabel, statTile,
  collapsibleGroup, stepFooter, toastHTML, modalHTML,
  helpTooltip, wireHelpTooltips, expandableChecklist,
} from "./ui.js";
import { ICONS } from "./icons.js";

// --- button --------------------------------------------------------------

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

// --- icon ----------------------------------------------------------------

test("icon returns svg for known names and empty string for unknown", () => {
  assert.ok(icon("info").includes("<svg"));
  assert.equal(icon("no_such_icon"), "");
});

test("every icon name in ICONS renders an svg coloured by CSS", () => {
  // The empty string for an unknown name is deliberate (a missing glyph must not
  // break a button render) and it is also what hid a missing icon for a whole
  // release: every help trigger rendered as an empty circle. ../icon_parity_test.go
  // is the guard that no name reaching icon() is unknown; this pins the other
  // half, that a name it DOES know produces markup CSS can colour.
  const names = Object.keys(ICONS);
  assert.ok(names.length > 0, "the vendored icon map is not empty");
  for (const name of names) {
    const html = icon(name);
    assert.match(html, /<svg/, `icon(${name}) renders an svg`);
    assert.match(html, /fill="currentColor"/,
      `icon(${name}) must be coloured by CSS, not by a baked-in fill`);
  }
});

test("ICONS carries the info glyph the help tooltip renders", () => {
  // helpTooltip() renders icon("info"). With no "info" entry, icon() returned the
  // empty string and every help trigger in the application was an invisible
  // 1.15rem hit area: the tooltip worked and there was nothing to hover.
  assert.ok(ICONS.info, "ICONS must hold an info glyph");
  assert.match(ICONS.info, /fill="currentColor"/);
});

// --- Icon alignment contract ---------------------------------------------
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
  assert.match(icon("info"), /^<span class="icon" aria-hidden="true">/);
  assert.match(button("Home", { icon: "info" }), /<span class="icon"/);
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
  // The bar is fixed-height chrome: it must not wrap or grow.
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
  // A leftover rule would style markup nothing emits.
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

// --- Import screen layout ------------------------------------------------
//
// The import screen is a fixed two-column grid, so its height chain is the
// shared card contract asserted above. What is pinned here is that the
// draggable divider it once had left no dead CSS behind.

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

// --- Spelling chip contract -----------------------------------------------
//
// Reported symptom: the spelling chips looked disabled and could not be dragged.
// Both were CSS, not markup: the chips inherited the muted colour of the spelling
// area, and nothing said they were draggable. The markup side is asserted by
// reading views/identifyworkspace.js, since the chip builder is not exported and
// there is no DOM here.

const workspaceJS = fs.readFileSync(path.join(staticDir, "views", "identifyworkspace.js"), "utf8");

test("spelling chips are rendered draggable", () => {
  // the chips looked disabled and could not be dragged.
  // They are .chip-tag now, and the drag survived the relayout:
  // regrouping a mis-attached spelling has no other home. A conflicting spelling
  // adds a " bad" class between the base class and the closing quote, so this
  // asserts the two facts (the chip class, and draggable) rather than one exact
  // run of characters the interpolation now splits.
  assert.match(workspaceJS, /class="chip-tag spelling-chip/);
  assert.match(workspaceJS, /draggable="true"/);
});

test("the remove button inside a chip does not swallow the drag", () => {
  // The chip IS the drag handle, so its remove button has to stop the click
  // reaching it; otherwise removing a spelling would sometimes start a drag
  // instead.
  assert.match(workspaceJS, /ev\.stopPropagation\(\);/);
});

test("a drop onto a value's own card is refused rather than being a no-op move", () => {
  // Dragging a spelling onto the card it already belongs to must not clear and
  // re-add it: the reducer would exclude it from the source and add it back to
  // the same target, which reads as the chip vanishing.
  assert.match(workspaceJS, /a drop onto its own card would be a no-op/);
});

test("spelling chips are not styled as disabled", () => {
  const chip = styleCSS.match(/\n\.spelling-chip \{[^}]*\}/);
  assert.ok(chip, "the chip rule must exist");
  // The chip must carry the ordinary text colour, not the muted one its
  // surroundings use: a greyed-out chip reads as disabled.
  assert.match(chip[0], /color:\s*var\(--text\)/);
  assert.ok(!/opacity/.test(chip[0]), "a chip must never be dimmed, it is a live control");
  // And it must say it can be dragged before the user tries.
  assert.match(chip[0], /cursor:\s*grab/);
  // A text selection mid-gesture cancels the drag, so it is suppressed.
  assert.match(chip[0], /user-select:\s*none/);
});

test("a drop target is visibly marked", () => {
  // The target is a value CARD now, not a table row: the spelling chips moved
  // from a table into cards.
  assert.match(styleCSS, /\.value-card\.drop-target \{/);
});

// =====================================================================
// The card kit.
// =====================================================================

// --- card ----------------------------------------------------------------

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

test("card renders a titleTooltip on the heading, and escapes it", () => {
  const plain = card({ title: "Run anonymisation", bodyHTML: "x" });
  assert.ok(plain.includes("<h2>Run anonymisation</h2>"),
    "with no tooltip the heading stays a bare h2");

  const tipped = card({ title: "Run anonymisation", titleTooltip: `on "hover"`, bodyHTML: "x" });
  assert.ok(tipped.includes('<h2 title="on &quot;hover&quot;">Run anonymisation</h2>'),
    "the tooltip lands on the h2's title attribute, escaped");
  assert.ok(!tipped.includes('class="card-sub"'),
    "a titleTooltip is not a visible subtitle");
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

// --- countBadge ----------------------------------------------------------

test("countBadge renders zero but renders nothing for a missing count", () => {
  assert.ok(countBadge(0).includes(">0<"));
  assert.equal(countBadge(null), "");
  assert.equal(countBadge(undefined), "");
});

test("countBadge marks the active spelling", () => {
  assert.ok(countBadge(4, { active: true }).includes('class="count-badge active"'));
  assert.ok(countBadge(4).includes('class="count-badge"'));
  assert.ok(!countBadge(4).includes("active"));
});

// --- tabbar --------------------------------------------------------------

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

// --- chipRow -------------------------------------------------------------

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

test("chipRow square spelling and disabled chips", () => {
  const html = chipRow([{ id: "later", label: "Not yet", disabled: true }], { square: true });
  assert.ok(html.includes("tint-chip square"));
  assert.ok(html.includes("disabled"));
});

// --- sectionLabel / statTile ---------------------------------------------

test("sectionLabel escapes and supports the mini spelling", () => {
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

// --- collapsibleGroup ----------------------------------------------------

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

// --- stepFooter ----------------------------------------------------------

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

// --- toastHTML -----------------------------------------------------------

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

// --- modalHTML -----------------------------------------------------------

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

test("modalHTML renders one button per choice instead of a confirm button", () => {
  const html = modalHTML({
    title: "Choose the main value", body: "Pick one.",
    choices: [{ id: "person_names|marie", label: "Marie (Person)" },
      { id: "entity_names|acme", label: "Acme (Organisation)" }],
  });
  // A choice dialog has no affirmative modal-confirm; it keeps the cancel.
  assert.ok(!html.includes('id="modal-confirm"'), "no yes/no confirm button");
  assert.ok(html.includes('id="modal-cancel"'), "cancel is still offered");
  // One button per choice, tagged with the choice id.
  assert.ok(html.includes('data-choice="person_names|marie"'));
  assert.ok(html.includes('data-choice="entity_names|acme"'));
  assert.ok(html.includes("Marie (Person)"));
  assert.ok(html.includes("Acme (Organisation)"));
});

test("modalHTML with an empty choices list falls back to the yes/no buttons", () => {
  const html = modalHTML({ title: "t", body: "b", choices: [] });
  assert.ok(html.includes('id="modal-confirm"'));
  assert.ok(html.includes('id="modal-cancel"'));
});

// --- The fixed-height layout contract ------------------------------------
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
  // The two greys come straight out of the brand palette, so
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

// --- helpTooltip ---------------------------------------------------------
//
// The tooltip is what lets the Configure panel explain itself without spending
// permanent vertical space. Two things are load-bearing and both are asserted
// here: the KEYBOARD path (the icon is a real focusable button and the bubble is
// its accessible description) and the OPEN state living in one data attribute so
// CSS owns appearance and JavaScript owns only when.

/** boxNode(rect) is an element that only has to be measurable: a trigger or a
 *  bubble as placeHelpBubble sees it. `style` records the two custom properties
 *  the placement writes, which is what the .help-bubble rule reads back. */
function boxNode(rect) {
  const props = new Map();
  return {
    props,
    getBoundingClientRect: () => ({ width: 0, height: 0, top: 0, left: 0, right: 0, bottom: 0, ...rect }),
    style: { setProperty: (k, v) => props.set(k, v) },
    blurred: false,
    blur() { this.blurred = true; },
  };
}

/** fakeNode(tag) is a minimal element stand-in with the four APIs
 *  wireHelpTooltips touches: listeners, attributes and one query. */
function fakeNode(children = {}) {
  const listeners = new Map();
  const attrs = new Map();
  return {
    listeners, attrs,
    addEventListener: (name, fn) => listeners.set(name, fn),
    setAttribute: (k, v) => attrs.set(k, v),
    removeAttribute: (k) => attrs.delete(k),
    querySelector: (sel) => children[sel] ?? null,
    fire: (name, ev = {}) => listeners.get(name)?.(ev),
    isOpen: () => attrs.get("data-open") === "true",
  };
}

/** helpWithBoxes(triggerRect, bubbleSize) is a wrapper whose trigger and bubble
 *  are measurable, plus a window big enough to place them in. */
function helpWithBoxes(triggerRect, bubbleSize) {
  const trigger = boxNode(triggerRect);
  const bubble = boxNode(bubbleSize);
  const help = fakeNode({ ".help-icon": trigger, ".help-bubble": bubble });
  return { help, trigger, bubble };
}

/** withWindow(size, fn) lends the module a viewport. The frontend suite has no
 *  DOM, and placeHelpBubble reads window.innerWidth/innerHeight, so the test has
 *  to supply one and put it back afterwards. */
function withWindow(size, fn) {
  const previous = globalThis.window;
  globalThis.window = { innerWidth: size.width, innerHeight: size.height };
  try {
    fn();
  } finally {
    if (previous === undefined) delete globalThis.window;
    else globalThis.window = previous;
  }
}

/** wireOne(help) runs wireHelpTooltips over a container holding just `help`. */
function wireOne(help) {
  wireHelpTooltips({ querySelectorAll: () => [help] });
}

test("helpTooltip renders a focusable icon that describes its bubble", () => {
  const html = helpTooltip("Why this matters", { id: "help-x", label: "About the floor" });
  assert.match(html, /<button type="button" class="help-icon"/,
    "a real button, so Tab reaches it: a span with a hover style is invisible to the keyboard");
  assert.match(html, /aria-label="About the floor"/);
  assert.match(html, /aria-describedby="help-x"/);
  assert.match(html, /<span class="help-bubble" id="help-x" role="tooltip">Why this matters<\/span>/);
});

test("helpTooltip renders a VISIBLE glyph, not an empty hit area", () => {
  // This is the assertion that was missing. The suite asserted the wrapper
  // (`<button class="help-icon"`) and passed on an empty button: icon("info")
  // returned "" because ICONS had no "info" entry, so every help trigger in the
  // application was 1.15rem of invisible hit area with nothing to hover.
  const html = helpTooltip("Why this matters");
  assert.match(html, /<button type="button" class="help-icon"[^>]*><span class="icon"/,
    "the trigger's first child is the icon span, so the button is not empty");
  assert.match(html, /<svg/, "and the span holds real vendored markup");
});

test("helpTooltip escapes its text and derives a stable id", () => {
  const html = helpTooltip('a <b>bold</b> "claim"');
  assert.ok(!html.includes("<b>"), "copy is escaped, never trusted as markup");
  assert.equal(html, helpTooltip('a <b>bold</b> "claim"'),
    "the same text must derive the same id, or aria-describedby drifts between renders");
  assert.notEqual(helpTooltip("one"), helpTooltip("two"),
    "different text must derive different ids, or two bubbles collide");
});

test("helpTooltip renders nothing for empty copy", () => {
  // A tooltip with no text is an icon that opens an empty bubble.
  assert.equal(helpTooltip(""), "");
  assert.equal(helpTooltip(undefined), "");
});

test("the tooltip opens on hover AND on keyboard focus", () => {
  const help = fakeNode();
  wireOne(help);
  assert.ok(!help.isOpen(), "closed until asked");

  help.fire("pointerenter");
  assert.ok(help.isOpen(), "hover opens it");
  help.fire("pointerleave");
  assert.ok(!help.isOpen(), "and leaving closes it");

  help.fire("focusin");
  assert.ok(help.isOpen(), "Tab opens it too, or half the users never see it");
  help.fire("focusout");
  assert.ok(!help.isOpen());
});

test("the tooltip stays open while EITHER hover or focus holds", () => {
  // With one flag, moving the pointer from the icon into the bubble closes the
  // bubble the pointer is moving towards, so the text cannot be selected.
  const help = fakeNode();
  wireOne(help);
  help.fire("focusin");
  help.fire("pointerenter");
  help.fire("pointerleave");
  assert.ok(help.isOpen(), "still focused, so still open");
  help.fire("focusout");
  assert.ok(!help.isOpen());
});

test("Escape closes the tooltip and gives up the hover with it", () => {
  const iconBtn = { blurred: false, blur() { this.blurred = true; } };
  const help = fakeNode({ ".help-icon": iconBtn });
  wireOne(help);
  help.fire("pointerenter");
  assert.ok(help.isOpen());

  let stopped = false;
  help.fire("keydown", { key: "Escape", stopPropagation: () => { stopped = true; } });
  assert.ok(!help.isOpen(), "Escape closes it");
  assert.ok(stopped, "and does not also reach whatever else listens for Escape");
  assert.ok(iconBtn.blurred, "focus is released, or the bubble reopens on the next repaint");

  // The hover flag is cleared too, so a twitch that does not leave the icon does
  // not bring the bubble straight back.
  help.fire("pointerleave");
  assert.ok(!help.isOpen());
});

// --- The bubble's placement ----------------------------------------------
//
// The bubble is `position: fixed` so no clipping ancestor can cut it off: the
// rail's body is `overflow-y: auto` and a .cgroup is `overflow: hidden`, so an
// absolutely positioned bubble is trimmed at the group's edge. Fixed coordinates
// are measured from the viewport, which CSS cannot know from inside the trigger,
// so wireHelpTooltips computes them. These pin that it does, and that the CSS
// keeps exactly one positioning model.

test("opening a tooltip writes the bubble's viewport coordinates", () => {
  withWindow({ width: 1440, height: 900 }, () => {
    const { help, bubble } = helpWithBoxes(
      { top: 300, bottom: 318, left: 400, right: 418 },
      { width: 320, height: 120 });
    wireOne(help);

    help.fire("pointerenter");
    assert.ok(help.isOpen(), "the wrapper opens");
    // Below the trigger, right-aligned to it: 418 - 320 = 98, 318 + 6 = 324.
    assert.equal(bubble.props.get("--help-x"), "98px");
    assert.equal(bubble.props.get("--help-y"), "324px");
  });
});

test("a bubble with no room below flips above the trigger", () => {
  withWindow({ width: 1440, height: 900 }, () => {
    const { help, bubble } = helpWithBoxes(
      { top: 840, bottom: 858, left: 400, right: 418 },
      { width: 320, height: 120 });
    wireOne(help);

    help.fire("focusin");
    // 858 + 6 + 120 would end at 984, past the 900 viewport, so it flips:
    // 840 - 120 - 6 = 714.
    assert.equal(bubble.props.get("--help-y"), "714px");
  });
});

test("a bubble is clamped into the viewport rather than hanging off its edge", () => {
  withWindow({ width: 1440, height: 900 }, () => {
    const { help, bubble } = helpWithBoxes(
      { top: 300, bottom: 318, left: 20, right: 38 },
      { width: 320, height: 120 });
    wireOne(help);

    help.fire("pointerenter");
    // Right-aligning would put it at 38 - 320 = -282, off screen: clamped to the
    // 8px margin instead.
    assert.equal(bubble.props.get("--help-x"), "8px");
  });
});

test("the open rule only reveals the bubble, it does not reposition it", () => {
  // Two positioning models is how the bubble got clipped: .help-bubble declares
  // `position: fixed` precisely so the rail cannot trim it, and a data-open rule
  // that set `position: absolute` put it straight back inside the clipping
  // ancestor. The measurement above is the only thing that may place it.
  const base = styleCSS.match(/\n\.help-bubble \{[^}]*\}/);
  assert.ok(base, "the .help-bubble rule must exist");
  assert.match(base[0], /position:\s*fixed/);
  assert.match(base[0], /left:\s*var\(--help-x/);
  assert.match(base[0], /top:\s*var\(--help-y/);

  const open = styleCSS.match(/\n\.help\[data-open\] \.help-bubble \{[^}]*\}/);
  assert.ok(open, "the open rule must exist");
  assert.ok(!/position:/.test(open[0]),
    `the open rule must not restate position: ${open[0]}`);
  for (const prop of ["top:", "left:", "right:", "bottom:", "inset:"]) {
    assert.ok(!open[0].includes(prop),
      `the open rule must not set ${prop} it would override the measured placement`);
  }
});

test("the help trigger has a resting shape, so it is discoverable", () => {
  // A borderless glyph in the muted colour reads as decoration beside a label.
  const rule = styleCSS.match(/\n\.help-icon \{[^}]*\}/);
  assert.ok(rule, "the .help-icon rule must exist");
  assert.match(rule[0], /border:\s*1px solid/);
  assert.match(rule[0], /background:\s*var\(--bg\)/);
});

// --- expandableChecklist -------------------------------------------------
//
// The control answers a NESTED question: not "which signals may derive
// Suggestions" but "what would this signal derive, and do I want each of those?".
// So the shape is a list of groups, each a master over its rows, and these pin the
// three things that shape has to get right: collapsed costs one row, the master and
// the chevron are separate controls, and every string is escaped.

/** oneGroup(over) is a single-group checklist, with the fields under test
 *  overridable per case. */
function oneGroup(over = {}) {
  return expandableChecklist({
    id: "sources",
    label: "Signal-based suggestions",
    groups: [{
      id: "email",
      label: "Email addresses",
      summary: "Person and organisation names",
      checked: true,
      open: false,
      rows: [
        { id: "email.person", label: "Person names", detail: "from the part before the @", checked: true },
        { id: "email.organisation", label: "Organisation names", detail: "from the domain", checked: false },
      ],
      ...over,
    }],
  });
}

test("a collapsed group is one row, and its rows cost no vertical space", () => {
  const closed = oneGroup({ open: false });
  assert.match(closed, /aria-expanded="false"/);
  assert.match(closed, /class="checklist-rows" id="sources-email-rows" hidden/,
    "the readings are hidden while the group is collapsed");
  assert.match(closed, /<span class="checklist-summary">Person and organisation names<\/span>/,
    "and the collapsed row says what is on, so it is readable without expanding");

  const open = oneGroup({ open: true });
  assert.match(open, /data-open="true"/);
  assert.match(open, /aria-expanded="true"/);
  assert.ok(!/checklist-rows" id="sources-email-rows" hidden/.test(open),
    "expanding reveals them");
});

test("a group carries a master checkbox and one checkbox per row", () => {
  const html = oneGroup({ open: true });
  assert.match(html, /class="checklist-master" data-source="email"[^>]* checked/,
    "the master is keyed by SOURCE and reflects the caller's derived value");
  assert.match(html, /data-derivation="email\.person"[^>]* checked/);
  assert.match(html, /data-derivation="email\.organisation"(?![^>]*checked)/,
    "a reading that is off renders unchecked, so the two are independently switchable");
});

test("the chevron is a separate control from the master", () => {
  // One control per intent: ticking the master must not fold the group, and folding
  // it must not tick the master. Two jobs on one element is how a master ends up
  // being switched by accident.
  const html = oneGroup({ open: true });
  const toggle = html.match(/<button[^>]*class="checklist-group-toggle"[^>]*>/);
  assert.ok(toggle, "the group head has its own toggle button");
  assert.ok(!toggle[0].includes("checkbox"), "and it is not the checkbox");
  assert.match(toggle[0], /aria-controls="sources-email-rows"/,
    "it names the region it expands, so a screen reader can follow it");
});

test("a collapsed group's rows are hidden by CSS, not only by the attribute", () => {
  // An author `display` beats the `hidden` attribute's own display:none, so
  // .checklist-rows needs an explicit rule or a collapsed group still spends its
  // rows' height: hidden in the markup and visible on screen. A string test sees
  // the attribute and stops there, which is why the rule itself is asserted.
  const rule = styleCSS.match(/\n\.checklist-rows\[hidden\] \{[^}]*\}/);
  assert.ok(rule, "the .checklist-rows[hidden] rule must exist");
  assert.match(rule[0], /display:\s*none/);

  const base = styleCSS.match(/\n\.checklist-rows \{[^}]*\}/);
  assert.ok(base, "and the base rule that makes it necessary");
  assert.match(base[0], /display:\s*flex/);
});

test("a row's help tooltip travels with it", () => {
  // Each reading explains itself, because "Person names" alone does not say that
  // clearing it leaves the address itself anonymised.
  const html = expandableChecklist({
    id: "sources", label: "L",
    groups: [{
      id: "email", label: "Email addresses", summary: "S", checked: true, open: true,
      rows: [{
        id: "email.person", label: "Person names", checked: true,
        helpHTML: '<span class="help" data-help>marker</span>',
      }],
    }],
  });
  assert.match(html, /class="help" data-help/);
});

test("a checklist with no groups renders the label and nothing else", () => {
  const html = expandableChecklist({ id: "sources", label: "Signal-based suggestions" });
  assert.match(html, /Signal-based suggestions/);
  assert.ok(!html.includes("checklist-group\""), "no empty group is invented");
});

test("expandableChecklist escapes every string it is given", () => {
  const html = expandableChecklist({
    id: "x", label: "<b>L</b>",
    groups: [{
      id: "a", label: "<b>A</b>", summary: "<b>S</b>", checked: false, open: true,
      rows: [{ id: "r", label: "<b>R</b>", detail: "<b>D</b>", checked: false }],
    }],
  });
  assert.ok(!html.includes("<b>"), "copy is escaped, never trusted as markup");
});

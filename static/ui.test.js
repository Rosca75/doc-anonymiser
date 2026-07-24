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

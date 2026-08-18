// export.test.js, tests for the Export screen.
//
// views/export.js imports api.js, which only touches `window` inside its
// functions, so the module imports cleanly here. Only the PURE export is
// exercised directly; the rest is wiring, and the state half of the screen is
// covered by the startNewBatch tests in state.test.js.

import { test } from "node:test";
import assert from "node:assert/strict";

import { outputName, applySession, profileCard } from "./views/export.js";
import { resetState, getState } from "./state.js";
import { one, exists, textOf } from "./testhtml.js";

test("outputName puts _anon before the extension", () => {
  assert.equal(outputName("services-agreement.docx"), "services-agreement_anon.docx");
  assert.equal(outputName("notes.txt"), "notes_anon.txt");
});

test("outputName handles a name with no extension", () => {
  assert.equal(outputName("README"), "README_anon");
});

test("outputName uses the LAST dot, so a dotted name keeps its extension", () => {
  // "workbook.xlsx#Clients" and "q3.final.pdf" both occur in practice.
  assert.equal(outputName("q3.final.pdf"), "q3.final_anon.pdf");
});

test("outputName leaves a dotfile alone rather than treating it as an extension", () => {
  // ".gitignore" has no name to suffix, so suffixing before the dot would
  // produce "_anon.gitignore", which is a different hidden file.
  assert.equal(outputName(".gitignore"), ".gitignore_anon");
});

test("applySession restores Native and Auto detection, an absent flag meaning on", () => {
  resetState();
  applySession({ settings: { level: "medium", useBuiltInPatterns: false } });
  const s = getState().settings;
  assert.equal(s.useBuiltInPatterns, false, "an explicit false is restored");
  assert.equal(s.useHeuristicDiscovery, true, "an absent flag restores ON, like useSmartDetect");
  assert.equal(s.useSmartDetect, true, "derived: either half on means the route is on");
});

test("applySession with both detection halves absent restores both on", () => {
  resetState();
  applySession({ settings: { level: "medium" } });
  const s = getState().settings;
  assert.equal(s.useBuiltInPatterns, true);
  assert.equal(s.useHeuristicDiscovery, true);
  assert.equal(s.useSmartDetect, true);
});

test("applySession with both detection halves off derives useSmartDetect off", () => {
  resetState();
  applySession({ settings: { level: "medium", useBuiltInPatterns: false, useHeuristicDiscovery: false } });
  assert.equal(getState().settings.useSmartDetect, false, "both off means the route reads off");
});

test("profileCard renders as Profile with Save and NO Load button", () => {
  const html = profileCard();
  // Renamed section: the Export step keeps the SAVE half of the profile only.
  assert.ok(textOf(html, ".cgroup-title").includes("Profile"), "section titled Profile");
  assert.ok(exists(html, "#ses-save"), "Save button present");
  assert.equal(exists(html, "#ses-load"), false, "Load button removed (it lives on the rail)");
});

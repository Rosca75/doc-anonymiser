// export.test.js, tests for the Export screen.
//
// views/export.js imports api.js, which only touches `window` inside its
// functions, so the module imports cleanly here. Only the PURE export is
// exercised directly; the rest is wiring, and the state half of the screen is
// covered by the startNewBatch tests in state.test.js.

import { test } from "node:test";
import assert from "node:assert/strict";

import { outputName, applySession, profileCard } from "./views/export.js";
import {
  resetState, getState, smartDetectionOn, enabledSignalSources, SIGNAL_SOURCES,
} from "./state.js";
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

test("applySession restores each Smart detection method, an absent flag meaning on", () => {
  // The safe reading of silence is the shipped default. Reading it as "off" would
  // silently stop detecting for anyone whose file predates the flag.
  resetState();
  applySession({ settings: { level: "medium", useBuiltInPatterns: false } });
  const s = getState().settings;
  assert.equal(s.useBuiltInPatterns, false, "an explicit false is restored");
  assert.equal(s.useHeuristicDiscovery, true, "an absent flag restores ON");
  assert.equal(smartDetectionOn(getState()), true, "one method on means the section reads on");
});

test("applySession with every method absent restores them all on", () => {
  resetState();
  applySession({ settings: { level: "medium" } });
  const s = getState().settings;
  assert.equal(s.useBuiltInPatterns, true);
  assert.equal(s.useHeuristicDiscovery, true);
  assert.deepEqual(enabledSignalSources(getState()), SIGNAL_SOURCES,
    "an absent source key restores its default, never off");
});

test("applySession with every method off leaves the section reading off", () => {
  // The section has no flag of its own to restore, so it can only read off when
  // every method it summarises actually is.
  resetState();
  applySession({
    settings: {
      level: "medium", useBuiltInPatterns: false, useHeuristicDiscovery: false,
      signalSuggestionSources: { email: false },
    },
  });
  assert.equal(smartDetectionOn(getState()), false);
  assert.deepEqual(enabledSignalSources(getState()), []);
});

test("applySession restores a signal source the user switched off", () => {
  resetState();
  applySession({ settings: { level: "medium", signalSuggestionSources: { email: false } } });
  assert.deepEqual(enabledSignalSources(getState()), [],
    "a source switched off must come back off, or the file did not save the decision");
});

test("profileCard renders as Profile with Save and NO Load button", () => {
  const html = profileCard();
  // Renamed section: the Export step keeps the SAVE half of the profile only.
  assert.ok(textOf(html, ".cgroup-title").includes("Profile"), "section titled Profile");
  assert.ok(exists(html, "#ses-save"), "Save button present");
  assert.equal(exists(html, "#ses-load"), false, "Load button removed (it lives on the rail)");
});

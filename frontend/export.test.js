// export.test.js, tests for the Export screen.
//
// views/export.js imports api.js, which only touches `window` inside its
// functions, so the module imports cleanly here. Only the PURE export is
// exercised directly; the rest is wiring, and the state half of the screen is
// covered by the startNewBatch tests in state.test.js.

import { test } from "node:test";
import assert from "node:assert/strict";

import { outputName, applySession, renderExport } from "./views/export.js";
import {
  resetState, getState, smartDetectionOn, enabledSignalSources, signalDerivationOn, SIGNAL_SOURCES,
} from "./state.js";
import { exists } from "./testhtml.js";

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
      signalSuggestionSources: {
        email: { "email.person": false, "email.organisation": false },
      },
    },
  });
  assert.equal(smartDetectionOn(getState()), false);
  assert.deepEqual(enabledSignalSources(getState()), []);
});

test("applySession restores each signal READING the user switched off", () => {
  resetState();
  applySession({
    settings: {
      level: "medium",
      signalSuggestionSources: {
        email: { "email.person": false, "email.organisation": false },
      },
    },
  });
  assert.deepEqual(enabledSignalSources(getState()), [],
    "readings switched off must come back off, or the file did not save the decision");
});

test("applySession restores ONE reading off and leaves the other on", () => {
  // The reason the shape is nested: a v7 file's one boolean per source could not
  // say this, and a reader guessing "both on" would produce Suggestions the user
  // had switched off.
  resetState();
  applySession({
    settings: {
      level: "medium",
      signalSuggestionSources: { email: { "email.person": false } },
    },
  });
  assert.equal(signalDerivationOn(getState(), "email", "email.person"), false,
    "the reading the file cleared comes back cleared");
  assert.equal(signalDerivationOn(getState(), "email", "email.organisation"), true,
    "and the one it says nothing about lands on its default, not on off");
  assert.deepEqual(enabledSignalSources(getState()), ["email"],
    "so the signal still derives something, and its master still reads on");
});

test("the Export step has no profile control of its own: Save moved to Identify", () => {
  // The profile has exactly one home now, the Identify rail's Load/Save
  // section. A second Save control here duplicated the same file under a
  // second name, which is what the owner asked to stop.
  resetState();
  let html = "";
  const container = {
    set innerHTML(v) { html = v; },
    get innerHTML() { return html; },
    querySelector() { return null; },
    querySelectorAll() { return []; },
  };
  renderExport(container);
  assert.equal(exists(html, "#ses-save"), false, "no Save profile control remains on Export");
  assert.equal(exists(html, "#ses-load"), false, "no Load profile control remains on Export");
  assert.ok(!/\bsave profile\b/i.test(html), "no leftover 'Save profile' copy renders on Export");
  // The mapping and report exports are untouched: they are a different
  // artefact (the re-identification key exports), not the profile.
  assert.ok(exists(html, "#map-csv"));
  assert.ok(exists(html, "#map-json"));
  assert.ok(exists(html, "#rep-json"));
  assert.ok(exists(html, "#rep-md"));
});

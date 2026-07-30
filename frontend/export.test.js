// export.test.js, tests for the Export screen (BUILD-05 Phase 8).
//
// views/export.js imports api.js, which only touches `window` inside its
// functions, so the module imports cleanly here. Only the PURE export is
// exercised directly; the rest is wiring, and the state half of the screen is
// covered by the startNewBatch tests in state.test.js.

import { test } from "node:test";
import assert from "node:assert/strict";

import { outputName } from "./views/export.js";

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

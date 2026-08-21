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
  resetState, getState, setMetaReview,
  enabledSignalSources, signalDerivationOn, SIGNAL_SOURCES,
} from "./state.js";
import { exists, textOf } from "./testhtml.js";

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

test("applySession restores each route flag on its own, an absent flag meaning on", () => {
  // The safe reading of silence is the shipped default. Reading it as "off" would
  // silently stop detecting for anyone whose file predates the flag. Each of the
  // three route switches is restored from its OWN key: there is no fourth
  // section flag, so a per-flag assertion is the whole check.
  resetState();
  applySession({ settings: { level: "medium", useBuiltInPatterns: false } });
  const s = getState().settings;
  assert.equal(s.useBuiltInPatterns, false, "an explicit false is restored");
  assert.equal(s.useHeuristicDiscovery, true, "an absent flag restores ON");
  assert.equal(s.useLocalLLM, undefined,
    "the local route is not switched on by a file that says nothing about it");
});

test("applySession restores the checksum switch, an absent flag meaning off", () => {
  // The safe reading of silence here is the OPPOSITE of a route flag's, and for
  // the same reason: absence must read as what the file was written under. Off is
  // the shipped default, so a file that says nothing about the switch was saved
  // with it off, and reading absence as "on" would leave a checksum-failed bank
  // identifier in clear in a document the user expects anonymised.
  resetState();
  applySession({ settings: { requireChecksum: true } });
  assert.equal(getState().settings.requireChecksum, true, "an explicit true is restored");
  resetState();
  applySession({ settings: {} });
  assert.equal(getState().settings.requireChecksum, false, "an absent flag restores OFF");
});

test("applySession with every offline route absent restores them all on", () => {
  resetState();
  applySession({ settings: { level: "medium" } });
  const s = getState().settings;
  assert.equal(s.useBuiltInPatterns, true);
  assert.equal(s.useHeuristicDiscovery, true);
  assert.deepEqual(enabledSignalSources(getState()), SIGNAL_SOURCES,
    "an absent source key restores its default, never off");
});

test("applySession restores every offline route switched off", () => {
  // Each flag comes back exactly as the file wrote it, per route and per signal
  // reading, so a decision the user made cannot be quietly undone by a default.
  resetState();
  applySession({
    settings: {
      level: "medium", useBuiltInPatterns: false, useHeuristicDiscovery: false,
      signalSuggestionSources: {
        email: { "email.person": false, "email.organisation": false },
        url: { "url.organisation": false },
      },
    },
  });
  const off = getState().settings;
  assert.equal(off.useBuiltInPatterns, false);
  assert.equal(off.useHeuristicDiscovery, false);
  assert.deepEqual(enabledSignalSources(getState()), []);
});

test("applySession restores each signal READING the user switched off", () => {
  resetState();
  applySession({
    settings: {
      level: "medium",
      signalSuggestionSources: {
        email: { "email.person": false, "email.organisation": false },
        url: { "url.organisation": false },
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
  assert.deepEqual(enabledSignalSources(getState()), ["email", "url"],
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

/**
 * renderToString() renders the Export screen into a container that only records
 * the HTML, so a render assertion needs no DOM. The wiring reads back null and
 * empty lists, which is what the screen's own guards are written for.
 */
function renderToString() {
  let html = "";
  const container = {
    set innerHTML(v) { html = v; },
    get innerHTML() { return html; },
    querySelector() { return null; },
    querySelectorAll() { return []; },
  };
  renderExport(container);
  return html;
}

/** openReview(images) puts one properties review on screen carrying that
 *  picture summary, which is the shape Go answers GetSameFormatMetadata with. */
function openReview(images) {
  resetState();
  setMetaReview("deck.pptx", {
    ext: "pptx", filename: "deck_anon.pptx", fields: [], images,
  });
  return renderToString();
}

test("the properties review counts the pictures this copy will change", () => {
  // The last surface before the file is written, so it is where the count has to
  // be: after the save there is nothing left to do about it.
  const html = openReview({ kept: 4, boxed: 1, blurred: 1, removed: 1 });
  assert.equal(textOf(html, ".image-note"),
    "3 images will be changed in this copy (1 boxed, 1 blurred, 1 removed).");
});

test("the properties review says so when the copy keeps every picture", () => {
  // The important sentence: a user who never opened the IMAGE tab has decided
  // nothing, and this is the only place they are told that the client logo and
  // the screenshot of the client's own system are going out untouched.
  const html = openReview({ kept: 7, boxed: 0, blurred: 0, removed: 0 });
  assert.equal(textOf(html, ".image-note"),
    "This copy keeps all 7 of the document's images, exactly as they are.");
});

test("the properties review names one changed picture in the singular", () => {
  const html = openReview({ kept: 0, boxed: 0, blurred: 0, removed: 1 });
  assert.equal(textOf(html, ".image-note"),
    "1 image will be changed in this copy (1 removed).");
});

test("the properties review says nothing about pictures for a format with none", () => {
  // Go answers with no summary at all for a PDF and for an .xlsx, and a line
  // reading "0 images" on a PDF would contradict the IMAGE tab, which says a PDF
  // export has already dropped every picture.
  for (const images of [null, undefined, { kept: 0, boxed: 0, blurred: 0, removed: 0 }]) {
    const html = openReview(images);
    assert.equal(exists(html, ".image-note"), false,
      `a summary of ${JSON.stringify(images)} must render no picture line at all`);
  }
});

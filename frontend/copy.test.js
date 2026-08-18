// copy.test.js, the automated copy-style guard.
//
// UI copy rules: no em dashes (U+2014) and no en
// dashes used as dashes (U+2013) anywhere in the frontend source. This test
// walks every .js and .html file under frontend/ (excluding *.test.js and
// assets/) and fails listing file:line for each offending character, so a
// dash can never quietly return. Use commas, periods, or parentheses.
//
// The matching Go-side guard is copy_guard_test.go at the repository root.

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { HOME, CARDS, NAV, WORKFLOW, CATEGORY_LABELS, WORKSPACE } from "./copy.js";

const staticDir = path.dirname(fileURLToPath(import.meta.url));

/** walk(dir) yields every .js/.html file under dir, skipping tests+assets. */
function* walk(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name === "assets" || entry.name === "node_modules") continue;
      yield* walk(full);
      continue;
    }
    if (entry.name.endsWith(".test.js")) continue;
    if (entry.name.endsWith(".js") || entry.name.endsWith(".html")) yield full;
  }
}

test("no em or en dashes in frontend source", () => {
  const hits = [];
  for (const file of walk(staticDir)) {
    const lines = fs.readFileSync(file, "utf8").split("\n");
    lines.forEach((line, i) => {
      if (line.includes("—") || line.includes("–")) {
        hits.push(`${path.relative(staticDir, file)}:${i + 1}: ${line.trim()}`);
      }
    });
  }
  assert.deepEqual(hits, [],
    "em/en dashes found in UI source; replace them with commas, periods or parentheses:\n" +
    hits.join("\n"));
});

// --- Home page copy ------------------------------------------------------

// The home headline and body wording are an editorial decision the owner
// makes by hand, not a behavioural contract. We deliberately do NOT assert on
// the exact headline, the paragraph count, or the presence of particular
// words: those checks broke the build every time the copy was reworded. The
// only guard kept here is structural, so `main.js` can render `HOME.body`
// without a runtime surprise: it must be a non-empty list of non-empty
// strings.
test("home body is a non-empty list of non-empty paragraphs", () => {
  assert.ok(Array.isArray(HOME.body), "HOME.body must be an array of paragraphs");
  assert.ok(HOME.body.length > 0, "HOME.body must have at least one paragraph");
  for (const paragraph of HOME.body) {
    assert.equal(typeof paragraph, "string");
    assert.ok(paragraph.trim().length > 0, `empty paragraph: ${JSON.stringify(paragraph)}`);
  }
});

// --- The four-step vocabulary --------------------------------------------

test("the home sidebar teaches the wizard's own step names", () => {
  // The sidebar used to say "Configure / Identify" for steps the wizard called
  // "Values / Run", so the landing page taught a vocabulary the application
  // then did not use. Every sidebar label must now be a real step name.
  const stepNames = new Set(Object.values(NAV.stepNames));
  for (const step of HOME.steps) {
    assert.ok(stepNames.has(step.label),
      `the home sidebar names a step the wizard does not have: ${step.label}`);
  }
  assert.equal(HOME.steps.length, stepNames.size,
    "the sidebar must cover every step exactly once");
});

test("no user-visible copy still calls a step Configure, Values or Run", () => {
  // Those three labels were retired by. Configure survives as
  // the name of the Identify RAIL, which is a card heading rather than a step,
  // so CARDS.configure is expected and only the step vocabulary is checked.
  for (const [token, name] of Object.entries(NAV.stepNames)) {
    assert.ok(!["Configure", "Values", "Run"].includes(name),
      `${token} is still labelled ${name}`);
  }
  assert.deepEqual(Object.values(NAV.stepNames),
    ["Import", "Identify", "Anonymise", "Export"]);
});

test("the navigation labels are built from the step names", () => {
  assert.equal(NAV.back("import"), "Back to Import");
  assert.equal(NAV.next("anonymise"), "CONTINUE TO ANONYMISE");
  // An unknown token degrades to itself rather than rendering "undefined".
  assert.equal(NAV.back("nonesuch"), "Back to nonesuch");
});

test("the reset question names the step and says what survives", () => {
  const body = NAV.backConfirmBody("identify");
  assert.match(NAV.backConfirmTitle("identify"), /Identify/);
  assert.match(body, /Identify/);
  assert.match(body, /imported documents/i, "the user must be told what is kept");
  assert.match(body, /never anonymise/i);
});

test("the step bar keeps an accessible name even though it shows no title", () => {
  assert.equal(WORKFLOW.title, "Anonymisation workflow");
  assert.ok(WORKFLOW.backToFlow.length > 0);
});

test("every card that carries a screen's content has a heading", () => {
  for (const [key, card] of Object.entries(CARDS)) {
    assert.equal(typeof card.title, "string", `${key} needs a title`);
    assert.ok(card.title.trim().length > 0, `${key} has an empty title`);
  }
});

test("the per-step explainer banner is gone and its copy moved into subtitles", () => {
  // STEP_BANNERS was a band of prose above every screen. Its useful sentences
  // are card subtitles now, next to the heading they explain.
  assert.ok(!("STEP_BANNERS" in CARDS));
  for (const key of ["documents", "identify", "run", "export"]) {
    assert.ok(CARDS[key].subtitle?.length > 0,
      `${key} must carry the explaining sentence the banner used to`);
  }
});

// --- Brand guards --------------------------------------------------------

/** walkAll(dir) yields every shipped source file under dir, skipping assets,
 *  node_modules and Markdown documentation. The .md skip exists because the
 *  frontend charters (CLAUDE.md, BRIDGE.md) restate the brand rule by naming
 *  the forbidden font to forbid it, exactly like this guard names it on
 *  purpose below; they are developer docs, not shipped font stacks. */
function* walkAll(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name === "assets" || entry.name === "node_modules") continue;
      yield* walkAll(full);
      continue;
    }
    if (entry.name.endsWith(".md")) continue;
    yield full;
  }
}

test("no Georgia reference remains anywhere under frontend/", () => {
  // Georgia is a PowerPoint-only brand guideline. This walks EVERY
  // shipped source file (css and js and html, tests included) so neither a
  // font stack nor a stale comment can quietly bring the name back.
  const hits = [];
  for (const file of walkAll(staticDir)) {
    if (file === fileURLToPath(import.meta.url)) continue; // this guard names it on purpose
    const lines = fs.readFileSync(file, "utf8").split("\n");
    lines.forEach((line, i) => {
      if (line.includes("Georgia")) {
        hits.push(`${path.relative(staticDir, file)}:${i + 1}: ${line.trim()}`);
      }
    });
  }
  assert.deepEqual(hits, [],
    "Georgia found under frontend/; the web application uses Helvetica with an Arial fallback:\n" +
    hits.join("\n"));
});

test("brand.css declares Helvetica first for headings and body", () => {
  const css = fs.readFileSync(path.join(staticDir, "brand.css"), "utf8");
  assert.match(css, /--font-heading:\s*Helvetica,\s*Arial,\s*sans-serif;/);
  assert.match(css, /--font-body:\s*Helvetica,\s*Arial,\s*sans-serif;/);
});

// --- the merged entity_names category ------------------------------------

test("the retired category labels are gone from the copy", () => {
  // client_names and internal_names merged into entity_names. A label left
  // behind would offer the user a category the engine no longer has.
  assert.ok(!("client_names" in CATEGORY_LABELS), "client_names must be gone");
  assert.ok(!("internal_names" in CATEGORY_LABELS), "internal_names must be gone");
  assert.equal(CATEGORY_LABELS.entity_names[0], "Entity names");
});

test("the Patterns tab ships eight worked examples that each compile and match their sample", () => {
  // The feature is "a non-expert can start from an example rather than a blank
  // box". That only works if there are enough examples to cover the common
  // shapes, each one actually compiles, each is labelled in plain words, and
  // each ships one concrete value that really matches, so the example is proven
  // to work rather than merely asserted to.
  const ex = WORKSPACE.patternExamples;
  assert.equal(ex.length, 8, "the enhancement asks for exactly eight examples");
  const seen = new Set();
  for (const e of ex) {
    assert.ok(e.expr && e.label && e.sample, "every example needs an expression, a label and a sample");
    assert.ok(!seen.has(e.expr), `duplicate example expression: ${e.expr}`);
    seen.add(e.expr);
    // Compile the expression. The examples target Go's RE2 engine (the backend
    // validates them), which supports a leading inline flag like (?i) that JS
    // RegExp does not, so translate that one construct before compiling here.
    const m = e.expr.match(/^\(\?([a-z]+)\)(.*)$/s);
    const [body, flags] = m ? [m[2], m[1]] : [e.expr, ""];
    let re;
    assert.doesNotThrow(() => { re = new RegExp(body, flags); }, `example must compile: ${e.expr}`);
    // The sample must actually be caught by the pattern: an example that does
    // not match its own sample teaches the wrong thing.
    assert.match(e.sample, re, `sample ${e.sample} must match ${e.expr}`);
  }
});

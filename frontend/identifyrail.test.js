// identifyrail.test.js, tests for the Identify rail's category grouping and
// confidence read-out (BUILD-04 Phase 4 as configure.test.js; renamed with the
// module by BUILD-05 Phase 2).
//
// views/identifyrail.js imports api.js, which only touches `window` inside
// its functions, so the module imports cleanly here. Only the PURE exports are
// exercised: the group table (CR9/CR10), the tab set and preset table
// (BUILD-05 Phase 5), and the sentence that explains the confidence slider
// (CR9, rewritten by decision 3). Everything else in the view is wiring and
// belongs to the manual pass.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  CATEGORY_GROUPS, RAIL_SECTIONS, PRESETS, confidenceEffect, llmGateTooltip,
  llmDisabledTooltip, railBody,
} from "./views/identifyrail.js";
import { CONFIGURE, RAIL } from "./copy.js";
import { ALL_CATEGORIES, resetState, getState, setState, setUseAI } from "./state.js";
import { textOf, all, one, exists } from "./testhtml.js";

// --- CR9: every category is reachable from some group ---------------------

test("every category the store knows belongs to exactly one group", () => {
  const seen = new Map();
  for (const [title, keys] of CATEGORY_GROUPS) {
    for (const key of keys) {
      assert.ok(!seen.has(key), `${key} is in both "${seen.get(key)}" and "${title}"`);
      seen.set(key, title);
    }
  }
  const missing = ALL_CATEGORIES.filter((key) => !seen.has(key));
  assert.deepEqual(missing, [],
    `these categories have no group, so the rail cannot show them: ${missing.join(", ")}`);
});

test("the group table invents no category", () => {
  for (const [title, keys] of CATEGORY_GROUPS) {
    for (const key of keys) {
      assert.ok(ALL_CATEGORIES.includes(key), `${title} lists the unknown category ${key}`);
    }
  }
});

test("the BUILD-03 recognizers have their own named group (CR9)", () => {
  const group = CATEGORY_GROUPS.find(([, keys]) => keys.includes("credit_card"));
  assert.ok(group, "the payment card recognizer must belong to a group");
  const [title, keys] = group;
  assert.ok(title.length > 5, "the group needs a readable title");
  for (const key of ["uk_nhs", "ip_address", "mac_address", "crypto",
    "database_uri", "de_steuer_id", "es_nif"]) {
    assert.ok(keys.includes(key), `${key} belongs in the same group as credit_card`);
  }
});

test("every group has a title and at least one category (CR10)", () => {
  // A select-all button on an empty group would do nothing at all.
  for (const [title, keys] of CATEGORY_GROUPS) {
    assert.equal(typeof title, "string");
    assert.ok(title.trim().length > 0);
    assert.ok(keys.length > 0, `"${title}" has no categories`);
  }
});

// --- The rail's route sections (BUILD-06) --------------------------------
//
// The four peer tabs are gone. They said Scope was a thing of its own rather
// than the scope OF smart detection, and they made Cloud AI, which is not
// built, look like a peer of two routes that are.

test("the rail is three route sections, in the order the routes run", () => {
  assert.deepEqual(RAIL_SECTIONS.map(([id]) => id),
    ["rail-smart", "rail-local", "rail-cloud"]);
  for (const [id, label] of RAIL_SECTIONS) {
    assert.ok(label.trim().length > 0, `${id} has no label`);
  }
});

test("only the two built routes have an operable switch", () => {
  const keys = Object.fromEntries(RAIL_SECTIONS.map(([id, , key]) => [id, key]));
  assert.equal(keys["rail-smart"], "useSmartDetect");
  assert.equal(keys["rail-local"], "useAI");
  assert.equal(keys["rail-cloud"], null, "Cloud AI is not built, so its switch cannot be operated");
});

test("Scope is no longer a section: it is nested in the route it scopes", () => {
  assert.ok(!RAIL_SECTIONS.some(([, label]) => /scope/i.test(label)));
});

test("the presets are the three engine levels, and Custom is not among them", () => {
  assert.deepEqual(PRESETS.map(([level]) => level), ["soft", "medium", "advanced"]);
  assert.ok(!PRESETS.some(([level]) => level === "custom"),
    "Custom is a read-out of the current selection, not a preset that can be applied");
  // The labels say what they mean: soft/medium/advanced read too technical.
  assert.deepEqual(PRESETS.map(([, label]) => label), ["Soft", "Standard", "Thorough"]);
});

// --- The AI gate tooltip --------------------------------------------------

test("the gate tooltip tells the two reasons apart", () => {
  // Ollama missing and the toggle being off are different problems with
  // different fixes, so they must not share one message.
  const missing = llmGateTooltip({ ollama: { available: false }, settings: {} });
  assert.match(missing, /127\.0\.0\.1:11434/, "it must name the address that was probed");

  // The port is a SETTING, so the sentence has to follow it. A fixed 11434
  // lies to the one user most likely to be reading it.
  const moved = llmGateTooltip({ ollama: { available: false }, settings: { ollamaPort: 11500 } });
  assert.match(moved, /127\.0\.0\.1:11500/);
  assert.equal(llmDisabledTooltip(0), llmDisabledTooltip(11434), "an unset port falls back to the default");

  const off = llmGateTooltip({ ollama: { available: true }, settings: { useAI: false } });
  assert.equal(off, CONFIGURE.aiOffTooltip);
  assert.notEqual(off, missing);
});

// --- CR9: the confidence read-out, rewritten by decision 3 ---------------
//
// The mock-up's copy described a SOURCE-TIERED rule ("values that only the local
// AI suggested are skipped"), which the engine does not implement. What the
// setting actually is is a FLOOR on the confidence score every detection
// carries, so the copy describes the floor. These tests pin that it does not
// drift back into promising the rule the engine does not have.

test("the confidence read-out says nothing is skipped at the default", () => {
  assert.match(confidenceEffect(0), /Nothing is skipped/);
});

test("the confidence read-out describes a floor, not a rule about sources", () => {
  for (let percent = 0; percent <= 100; percent += 5) {
    const sentence = confidenceEffect(percent);
    assert.ok(!/only the local AI suggested/i.test(sentence),
      `${percent}: the engine has no source-tiered rule (decision 3): ${sentence}`);
    assert.ok(!/you listed yourself/i.test(sentence),
      `${percent}: the floor does not know who listed a value: ${sentence}`);
  }
});

test("the confidence read-out changes as the meaningful thresholds are crossed", () => {
  // Below where any score sits, the floor does nothing.
  assert.match(confidenceEffect(50), /Nothing is skipped/);
  // Past it, weaker detections start being left alone...
  assert.match(confidenceEffect(90), /left alone/);
  // ...and at the top only the strongest survive.
  assert.match(confidenceEffect(100), /strongest/);
  // Each band must actually say something different, or the slider looks dead.
  const bands = [0, 50, 90, 100].map(confidenceEffect);
  assert.equal(new Set(bands).size, 4, "every band must read differently");
});

test("the confidence read-out is a full sentence at every slider stop", () => {
  for (let percent = 0; percent <= 100; percent += 5) {
    const sentence = confidenceEffect(percent);
    assert.ok(sentence.length > 20, `${percent}: too terse`);
    assert.ok(sentence.endsWith("."), `${percent}: not a sentence`);
    assert.ok(!sentence.includes("—"), `${percent}: em dash`);
  }
});

// --- What the rail actually renders (BUILD-06) ---------------------------

/** railHTML() renders the rail from a fresh store, optionally patched. */
function railHTML(patch = {}) {
  resetState();
  if (patch.ollama) setState({ ollama: patch.ollama });
  if (patch.useAI) setUseAI(true);
  return railBody(getState());
}

test("the rail renders three sections and nothing else at the top level", () => {
  const html = railHTML();
  const sections = all(html, "section.rail-section");
  assert.equal(sections.length, 3);
  // The first title in a section is its own; the rest belong to the groups
  // nested inside it (the category groups, the strictness block).
  const titles = sections.map((sec) =>
    all(sec.outer, "span.cgroup-title")[0].inner.replace(/<[^>]*>/g, "").trim());
  assert.deepEqual(titles, [RAIL.tabSmart, RAIL.tabLocalAI, RAIL.tabCloudAI]);
});

test("Smart detection is ON by default, and switchable", () => {
  const html = railHTML();
  const smart = all(html, "input.route-toggle")[0];
  assert.equal(smart.attrs["data-route"], "rail-smart");
  assert.ok("checked" in smart.attrs, "Smart detection runs unless the user says otherwise");
  assert.ok(!("disabled" in smart.attrs), "and the user can say otherwise");
});

test("Local AI is OFF by default even when Ollama is detected", () => {
  const html = railHTML({ ollama: { available: true, models: [], detail: "" } });
  const local = all(html, "input.route-toggle")[1];
  assert.equal(local.attrs["data-route"], "rail-local");
  assert.ok(!("checked" in local.attrs),
    "detecting Ollama enables the switch, it does not flip it");
  assert.ok(!("disabled" in local.attrs));
});

test("Cloud AI is off, disabled, and says why", () => {
  const html = railHTML();
  const cloud = all(html, "input.route-toggle")[2];
  assert.ok(!("checked" in cloud.attrs));
  assert.ok("disabled" in cloud.attrs, "a switch for a route that does not exist must not move");
  assert.match(html, new RegExp(RAIL.cloudNotYet));
});

test("the scope controls live inside the Smart detection section", () => {
  // This is the whole point of the restructure: scope is the scope OF smart
  // detection, not a peer of it.
  const smart = all(railHTML(), "section.rail-section")[0].outer;
  assert.ok(exists(smart, "#document-country"), "the document country");
  assert.ok(exists(smart, "[data-preset]"), "the presets");
  assert.ok(exists(smart, ".cat-toggle"), "the category checkboxes");
  assert.ok(exists(smart, "#min-confidence"), "the confidence floor");
  assert.ok(exists(smart, "#smart-min-length"), "and the strictness fields");
});

test("every category checkbox is reachable without switching anything", () => {
  // With tabs, three quarters of the rail was one click away and invisible.
  const boxes = all(railHTML(), "input.cat-toggle").map((b) => b.attrs["data-category"]);
  assert.deepEqual(boxes.slice().sort(), ALL_CATEGORIES.slice().sort());
});

test("the Local AI fields are disabled while the route is off", () => {
  const off = railHTML({ ollama: { available: true, models: ["m"], detail: "" } });
  assert.ok("disabled" in one(off, "#ollama-model").attrs);
  const on = railHTML({ ollama: { available: true, models: ["m"], detail: "" }, useAI: true });
  assert.ok(!("disabled" in one(on, "#ollama-model").attrs));
  // The port is never gated: it is how a user FIXES a connection, so locking
  // it would lock them out of fixing the thing the gate complains about.
  assert.ok(!("disabled" in one(off, "#ollama-port").attrs));
});

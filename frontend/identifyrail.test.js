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
  CATEGORY_GROUPS, RAIL_TABS, PRESETS, confidenceEffect, llmGateTooltip,
  LLM_DISABLED_TOOLTIP,
} from "./views/identifyrail.js";
import { CONFIGURE } from "./copy.js";
import { ALL_CATEGORIES } from "./state.js";

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

// --- The rail's tabs and presets (BUILD-05 Phase 5) ----------------------

test("the rail has exactly the four tabs the mock-up shows", () => {
  assert.deepEqual(RAIL_TABS.map(([id]) => id), ["scope", "smart", "local", "cloud"]);
  for (const [id, label] of RAIL_TABS) {
    assert.ok(label.trim().length > 0, `${id} has no label`);
  }
});

test("Scope is the first tab, so it is the one that opens", () => {
  // It holds the country, the preset and the categories: the controls a user
  // came here for. Opening on Smart detection would put a tuning panel in front
  // of the choice it tunes.
  assert.equal(RAIL_TABS[0][0], "scope");
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
  assert.equal(missing, LLM_DISABLED_TOOLTIP);
  assert.match(missing, /127\.0\.0\.1:11434/, "it must name the address that was probed");

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

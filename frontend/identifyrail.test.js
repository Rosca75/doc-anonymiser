// identifyrail.test.js, tests for the Identify rail's category grouping and
// confidence read-out (BUILD-04 Phase 4 as configure.test.js; renamed with the
// module by BUILD-05 Phase 2).
//
// views/identifyrail.js imports api.js, which only touches `window` inside
// its functions, so the module imports cleanly here. Only the two PURE
// exports are exercised: the group table (CR9/CR10) and the sentence that
// explains the confidence slider (CR9). Everything else in the view is
// wiring and belongs to the manual pass.

import { test } from "node:test";
import assert from "node:assert/strict";

import { CATEGORY_GROUPS, confidenceEffect } from "./views/identifyrail.js";
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
    `these categories have no group, so the Configure screen cannot show them: ${missing.join(", ")}`);
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

// --- CR9: the confidence read-out ----------------------------------------

test("the confidence read-out says nothing is skipped at the default", () => {
  assert.match(confidenceEffect(0), /Nothing is skipped/);
});

test("the confidence read-out names each tier as it is crossed", () => {
  // Below the AI tier (0.8) nothing is excluded yet.
  assert.match(confidenceEffect(50), /Nothing is skipped yet/);
  // Between the AI tier and the user tier (0.95): AI suggestions go.
  assert.match(confidenceEffect(90), /local AI/);
  assert.doesNotMatch(confidenceEffect(90), /^Only pattern matches/);
  // Above the user tier: only pattern matches remain.
  assert.match(confidenceEffect(100), /Only pattern matches/);
});

test("the confidence read-out is a full sentence at every slider stop", () => {
  for (let percent = 0; percent <= 100; percent += 5) {
    const sentence = confidenceEffect(percent);
    assert.ok(sentence.length > 20, `${percent}: too terse`);
    assert.ok(sentence.endsWith("."), `${percent}: not a sentence`);
    assert.ok(!sentence.includes("—"), `${percent}: em dash`);
  }
});

// identifyrail.test.js, tests for the Identify rail's category grouping and
// confidence read-out.
//
// views/identifyrail.js imports api.js, which only touches `window` inside
// its functions, so the module imports cleanly here. Only the PURE exports are
// exercised: the group table, the tab set and preset table
// and the sentence that explains the confidence slider
// Everything else in the view is wiring and
// belongs to the manual pass.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  CATEGORY_GROUPS, RAIL_SECTIONS, PRESETS, confidenceEffect, llmGateTooltip,
  llmDisabledTooltip, railBody,
} from "./views/identifyrail.js";
import { CONFIGURE, RAIL, CATEGORY_LABELS } from "./copy.js";
import {
  ALL_CATEGORIES, NAME_CATEGORIES, resetState, getState, setState, setUseAI,
  setAIScope, setCategoryGroup,
} from "./state.js";
import { textOf, stripTags, all, one, exists } from "./testhtml.js";

// --- every category is reachable from some group -------------------------

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

// --- The rail's route sections -------------------------------------------
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

// --- The AI gate tooltip -------------------------------------------------

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

// --- the confidence read-out, rewritten by -------------------------------
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

// --- What the rail actually renders --------------------------------------

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
    stripTags(all(sec.outer, "span.cgroup-title")[0].inner).trim());
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

test("the strictness lever is a select of the three levels, balanced by default", () => {
  const smart = all(railHTML(), "section.rail-section")[0].outer;
  const select = one(smart, "#smart-strictness");
  const values = all(select.outer, "option").map((o) => o.attrs.value);
  assert.deepEqual(values, ["lenient", "balanced", "strict"],
    "the three engine strictness levels, in order");
  const selected = all(select.outer, "option").find((o) => "selected" in o.attrs);
  assert.equal(selected.attrs.value, "balanced", "balanced is the default selection");
});

test("the strictness select reflects a non-default stored value", () => {
  resetState();
  setState({ settings: { ...getState().settings,
    smartDetect: { ...getState().settings.smartDetect, strictness: "strict" } } });
  const smart = all(railBody(getState()), "section.rail-section")[0].outer;
  const selected = all(one(smart, "#smart-strictness").outer, "option")
    .find((o) => "selected" in o.attrs);
  assert.equal(selected.attrs.value, "strict");
});

test("every category checkbox is reachable without switching anything", () => {
  // With tabs, three quarters of the rail was one click away and invisible.
  // Every category is now rendered ONCE, in the Smart detection section: one
  // setting, one control. A second copy inside Local AI would be folded shut by
  // default and therefore not reachable at all, which is what this test is for.
  const boxes = all(railHTML(), "input.cat-toggle").map((b) => b.attrs["data-category"]);
  assert.deepEqual(boxes.slice().sort(), ALL_CATEGORIES.slice().sort(),
    "every category exactly once, with no duplicate checkbox for the same setting");
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

// --- The Local-AI scan scope ---------------------------------------------

/** localAIHTML renders the Local AI section with documents and an optional
 *  scope, the route switched on and Ollama present so the fields are live. */
function localAIHTML({ documents = [], scope = null, useAI = true } = {}) {
  resetState();
  setState({ ollama: { available: true, models: ["m"], detail: "" }, documents });
  if (useAI) setUseAI(true);
  if (scope) setAIScope(scope);
  return all(railBody(getState()), "section.rail-section")[1].outer;
}

test("the scan-scope picker defaults to all documents with no range", () => {
  const html = localAIHTML({
    documents: [{ name: "a.pdf", unit: "page", pageCount: 6 }],
  });
  const select = one(html, "#ai-scope-doc");
  assert.ok(select, "the Local AI section offers a document picker");
  const selected = all(select.outer, "option").find((o) => "selected" in o.attrs);
  assert.equal(selected.attrs.value, "", "all documents is the default");
  assert.ok(!exists(html, "#ai-scope-from"), "no range until a document is chosen");
});

test("choosing a multi-unit document reveals a bounded range", () => {
  const html = localAIHTML({
    documents: [{ name: "a.pdf", unit: "page", pageCount: 6 }],
    scope: { docName: "a.pdf", fromPage: 1, toPage: 1 },
  });
  const from = one(html, "#ai-scope-from");
  const to = one(html, "#ai-scope-to");
  assert.ok(from && to, "From and To inputs appear for a multi-unit document");
  assert.equal(from.attrs.max, "6", "the range is bounded by the page count");
  assert.equal(to.attrs.max, "6");
});

test("a single-unit document offers no range at all", () => {
  const html = localAIHTML({
    documents: [{ name: "note.txt", unit: "line", pageCount: 1 }],
    scope: { docName: "note.txt", fromPage: 1, toPage: 1 },
  });
  assert.ok(!exists(html, "#ai-scope-from"),
    "a document with one addressable unit has nothing to range over");
});

test("a multi-unit range warns, a single unit does not", () => {
  const range = localAIHTML({
    documents: [{ name: "a.pdf", unit: "page", pageCount: 6 }],
    scope: { docName: "a.pdf", fromPage: 2, toPage: 4 },
  });
  assert.ok(exists(range, "#ai-scope-warn"),
    "spanning several pages is the 'too much' the feature warns about");
  const single = localAIHTML({
    documents: [{ name: "a.pdf", unit: "page", pageCount: 6 }],
    scope: { docName: "a.pdf", fromPage: 3, toPage: 3 },
  });
  assert.ok(!exists(single, "#ai-scope-warn"),
    "one page is not a range, so it does not nag");
});

// --- The trigger grouping ------------------------------------------------

test("the detected group holds exactly what a detector can emit", () => {
  // The group is named "Auto detected values", so its membership is a claim.
  // custom_patterns is a regex the user WROTE, which is declarative: leaving it
  // here is the mislabelling the rename exists to fix.
  const detected = CATEGORY_GROUPS.find(([title]) => title === CONFIGURE.groupDetected);
  assert.ok(detected, "there must be a detected-values group");
  assert.deepEqual(detected[1], NAME_CATEGORIES);
  assert.ok(!detected[1].includes("custom_patterns"),
    "a regex the user wrote is declared, not detected");
});

test("the user's own patterns are a group of their own", () => {
  const declared = CATEGORY_GROUPS.find(([title]) => title === CONFIGURE.groupDeclared);
  assert.ok(declared, "custom_patterns needs a group that says what it is");
  assert.deepEqual(declared[1], ["custom_patterns"]);
});

test("a category only a detection route or manual entry can produce says so", () => {
  // The switch is NEVER disabled for these: a category gates manually typed
  // values too, so disabling brand_names with Ollama absent would silently stop
  // replacing a brand the user typed by hand. The honest signal is the label's
  // second element, and this turns that comment into something enforced.
  for (const category of ["brand_names", "other_names"]) {
    const [, description] = CATEGORY_LABELS[category];
    assert.match(description, /AI|add(ed)? by you/i,
      `${category} cannot be found offline, so its description must say where it comes from`);
  }
});

// --- The group select-all buttons address the RIGHT group (CR11) ----------
//
// The rail's select-all/deselect-all buttons carry their target as data-*
// attributes, and wireScope resolves the group from btn.dataset.groupType. A
// browser lowercases attribute NAMES, so a camelCase data key (data-groupType)
// is read back as data-grouptype, and dataset.groupType comes out undefined ->
// the handler falls back to "regex" and the "Auto detected values" (entity)
// group's buttons silently drive the Contact regex group instead. The fix is a
// hyphenated key (data-group-type) that survives the dataset round-trip. These
// tests drive the handler's exact resolution FROM the rendered attributes, so a
// regression to the camelCase key fails them.

/** datasetOf(attrs) reproduces the browser's data-* -> element.dataset mapping:
 *  drop the "data-" prefix, then camel-case each "-x" boundary. This is the
 *  round-trip the bug broke, so the test must model it rather than read the raw
 *  attribute by hand. */
function datasetOf(attrs) {
  const ds = {};
  for (const [name, value] of Object.entries(attrs)) {
    if (!name.startsWith("data-")) continue;
    const key = name.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase());
    ds[key] = value;
  }
  return ds;
}

/** bulkButton(html, title, on) finds one group's select-all ("1") or
 *  deselect-all ("0") button by its aria-label and returns its parsed attrs. */
function bulkButton(html, title, on) {
  const label = `${on === "1" ? CONFIGURE.selectAll : CONFIGURE.deselectAll}: ${title}`;
  const btn = all(html, "button.cat-group-all")
    .find((b) => b.attrs["aria-label"] === label && b.attrs["data-on"] === on);
  assert.ok(btn, `the "${title}" group is missing its ${on === "1" ? "select-all" : "deselect-all"} button`);
  return btn;
}

/** clickBulk replays wireScope's click handler verbatim, but resolves the group
 *  from the button's RENDERED data-* attributes via datasetOf, which is exactly
 *  the path the bug corrupted. ENTITY_GROUPS/REGEX_GROUPS are private to the
 *  view, so they are re-derived here the same way the view slices them. */
function clickBulk(btn) {
  const REGEX_GROUPS = CATEGORY_GROUPS.slice(0, 3);
  const ENTITY_GROUPS = CATEGORY_GROUPS.slice(3);
  const ds = datasetOf(btn.attrs);
  const type = ds.groupType || "regex";
  const groupArray = type === "entity" ? ENTITY_GROUPS : REGEX_GROUPS;
  const group = groupArray[Number(ds.group)];
  assert.ok(group, "the button's data-group index must resolve to a real group");
  setCategoryGroup(group[1], ds.on === "1");
}

test("the entity group's select-all toggles the NAME categories, not the regex ones (CR11)", () => {
  resetState();
  // A clean baseline: every category off, so "became selected" is unambiguous.
  const off = {};
  for (const c of ALL_CATEGORIES) off[c] = false;
  setState({ settings: { ...getState().settings, categories: off } });

  clickBulk(bulkButton(railBody(getState()), CONFIGURE.groupDetected, "1"));

  const cats = getState().settings.categories;
  for (const name of NAME_CATEGORIES) {
    assert.equal(cats[name], true,
      `${name} is a NAME category, so the Auto detected values select-all must switch it on`);
  }
  // The Contact regex group is REGEX_GROUPS[0], which the bug's "regex" fallback
  // would have toggled by mistake. These must stay exactly as they were.
  for (const regex of ["email", "url", "iban", "vat"]) {
    assert.equal(cats[regex], false,
      `${regex} belongs to the Contact regex group and must be left untouched`);
  }
});

test("the entity select-all carries the hyphenated data key so dataset reads it back (CR11)", () => {
  resetState();
  const btn = bulkButton(railBody(getState()), CONFIGURE.groupDetected, "1");
  // The literal attribute must be hyphenated: a camelCase key is lowercased by
  // the browser and dataset.groupType comes back undefined.
  assert.ok("data-group-type" in btn.attrs,
    "the bulk button must emit data-group-type, not a camelCase data-groupType");
  assert.equal(datasetOf(btn.attrs).groupType, "entity",
    "the entity group's button must resolve to the entity route");
});

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
  ALL_CATEGORIES, NAME_CATEGORIES, resetState, getState, setState, setUseLocalAI,
  setAIScope, setCategoryGroup, setUseBuiltInPatterns, setUseHeuristicDiscovery,
  setSmartDetection, setSignalSource, SIGNAL_SOURCES,
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
// The rail is the two user-operable detection routes, in the order they run.
// Scope is not a peer of them: it is the scope OF Smart detection.

test("the rail is two route sections, in the order the routes run", () => {
  assert.deepEqual(RAIL_SECTIONS.map(([id]) => id), ["rail-smart", "rail-local"]);
  for (const [id, label] of RAIL_SECTIONS) {
    assert.ok(label.trim().length > 0, `${id} has no label`);
  }
});

test("each route section names what switches it on", () => {
  const keys = Object.fromEntries(RAIL_SECTIONS.map(([id, , key]) => [id, key]));
  // Smart detection's state is DERIVED from its three methods and not stored, so
  // it names the sentinel rather than a settings key that does not exist.
  assert.equal(keys["rail-smart"], "derived");
  assert.equal(keys["rail-local"], "useLocalAI");
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

  const off = llmGateTooltip({ ollama: { available: true }, settings: { useLocalAI: false } });
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
  if (patch.useLocalAI) setUseLocalAI(true);
  return railBody(getState());
}

test("the rail renders the three routes plus the Load profile section", () => {
  const html = railHTML();
  // Exactly two DETECTION ROUTE sections carry .rail-section; the render
  // harness (scripts/uitest/probes.js) counts the same class. Load profile is a
  // switch-less panel with its own .rail-panel class, so it must NOT be counted.
  const sections = all(html, "section.rail-section");
  assert.equal(sections.length, 2);
  const titles = sections.map((sec) =>
    stripTags(all(sec.outer, "span.cgroup-title")[0].inner).trim());
  assert.deepEqual(titles, [RAIL.tabSmart, RAIL.tabLocalAI]);
  // The Load profile panel sits after the routes as its own .rail-panel section.
  const panel = all(html, "section.rail-panel");
  assert.equal(panel.length, 1);
  assert.equal(
    stripTags(all(panel[0].outer, "span.cgroup-title")[0].inner).trim(), RAIL.profileTitle);
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

test("Smart detection's three methods lead the section", () => {
  const smart = all(railHTML(), "section.rail-section")[0].outer;
  const builtIn = one(smart, "#smart-built-in");
  const heuristic = one(smart, "#smart-heuristic");
  assert.ok(builtIn, "the built-in pattern switch renders");
  assert.ok(heuristic, "the heuristic discovery switch renders");
  assert.ok(exists(smart, "#signal-sources"), "the signal-source checklist renders");
  assert.ok("checked" in builtIn.attrs, "built-in patterns default on");
  assert.ok("checked" in heuristic.attrs, "heuristic discovery defaults on");
  // All three lead the section: each appears before the first category checkbox.
  const firstCat = smart.indexOf('class="cat-toggle"');
  for (const marker of ['id="smart-built-in"', 'id="smart-heuristic"', 'id="signal-sources"']) {
    const at = smart.indexOf(marker);
    assert.ok(at >= 0, `${marker} renders`);
    assert.ok(at < firstCat, `${marker} comes before the category block it governs`);
  }
  assert.ok(smart.includes(RAIL.builtInPatterns), "the Built-in patterns label renders");
  assert.ok(smart.includes(RAIL.heuristicDiscovery), "the Heuristic discovery label renders");
  assert.ok(smart.includes(RAIL.signalSuggestions), "the Signal-based suggestions label renders");
});

test("the signal-source checklist is closed by default and summarises what is on", () => {
  // Closed it is ONE row: that is what keeps the panel short as sources are
  // added. The summary is the read-out, not a list of names.
  resetState();
  const smart = all(railBody(getState()), "section.rail-section")[0].outer;
  const control = one(smart, "#signal-sources");
  assert.ok(!("data-open" in control.attrs), "it starts closed");
  assert.equal(one(smart, ".checklist-toggle").attrs["aria-expanded"], "false");
  assert.equal(textOf(smart, "span.checklist-summary"), RAIL.signalSourceLabel.email,
    "one enabled source reads as its own name");
  assert.ok("hidden" in one(smart, ".checklist-list").attrs, "the list is hidden while closed");
});

test("the checklist lists only the sources that implement discovery", () => {
  // A row with nothing behind it is a control that appears to do something and
  // does not, so the rows come from SIGNAL_SOURCES, which the Go parity guard
  // holds to the engine's own list.
  resetState();
  const smart = all(railBody(getState()), "section.rail-section")[0].outer;
  const boxes = all(smart, "input.checklist-box");
  assert.deepEqual(boxes.map((b) => b.attrs["data-checklist"]), SIGNAL_SOURCES);
  assert.ok("checked" in boxes[0].attrs, "email-derived Suggestions default on");
});

test("the closed summary reads Off once every source is cleared", () => {
  resetState();
  for (const source of SIGNAL_SOURCES) setSignalSource(source, false);
  const smart = all(railBody(getState()), "section.rail-section")[0].outer;
  assert.equal(textOf(smart, "span.checklist-summary"), RAIL.signalSourcesOff);
});

test("clearing a signal source does not disable the signal's own category", () => {
  // Acceptance criterion 4, at the view level: the two are different mechanisms,
  // and the control must not quietly do both.
  resetState();
  setSignalSource("email", false);
  const smart = all(railBody(getState()), "section.rail-section")[0].outer;
  const email = all(smart, "input.cat-toggle").find((b) => b.attrs["data-category"] === "email");
  assert.ok("checked" in email.attrs, "the email category stays on");
  assert.ok(!("disabled" in email.attrs), "and stays editable");
  assert.ok("checked" in one(smart, "#smart-built-in").attrs, "built-in patterns stay on");
});

test("turning built-in patterns off disables the pattern category block only", () => {
  resetState();
  setUseBuiltInPatterns(false);
  const smart = all(railBody(getState()), "section.rail-section")[0].outer;
  // The structured signal categories (email, vat, ...) go disabled; the name
  // categories (person_names, ...) stay editable, because heuristic discovery is
  // still on.
  const email = all(smart, "input.cat-toggle").find((b) => b.attrs["data-category"] === "email");
  const person = all(smart, "input.cat-toggle").find((b) => b.attrs["data-category"] === "person_names");
  assert.ok("disabled" in email.attrs,
    "the pattern category is disabled while Built-in patterns is off");
  assert.ok(!("disabled" in person.attrs), "name categories stay editable");
  // The selection is not cleared: the checkbox keeps its checked state.
  assert.ok("checked" in email.attrs, "the stored selection is preserved, not cleared");
});

test("the Smart detection header switch is a master over its three methods", () => {
  // Off only when EVERY method is off; on when any one is. The section state is
  // derived, so it cannot disagree with the methods it summarises.
  resetState();
  setSmartDetection(false);
  let smart = all(railBody(getState()), "input.route-toggle")[0];
  assert.ok(!("checked" in smart.attrs), "every method off means the section reads off");

  setUseBuiltInPatterns(true);
  smart = all(railBody(getState()), "input.route-toggle")[0];
  assert.ok("checked" in smart.attrs, "one method on means the section reads on");

  setUseBuiltInPatterns(false);
  setSignalSource("email", true);
  smart = all(railBody(getState()), "input.route-toggle")[0];
  assert.ok("checked" in smart.attrs, "a signal source alone also reads on");
});

// --- The Load profile section (CR7) --------------------------------------

test("the Load profile section renders AFTER the routes", () => {
  resetState();
  const html = railBody(getState());
  // Ordering by first appearance: the profile title must come after the last
  // route's title, so the section sits at the foot of the rail.
  assert.ok(html.includes(RAIL.profileTitle), "the Load profile section renders");
  assert.ok(html.indexOf(RAIL.profileTitle) > html.indexOf(RAIL.tabLocalAI),
    "Load profile is below the Local AI route");
});

test("the Load profile section has a Load and a Save button", () => {
  resetState();
  const html = railBody(getState());
  assert.ok(exists(html, "#profile-load"), "Load button present");
  assert.ok(exists(html, "#profile-save"), "Save button present");
});

test("the profile Save is disabled until detection has run once", () => {
  resetState();
  // Fresh session: no detection has run, so Save is disabled with the reason.
  let save = one(railBody(getState()), "#profile-save");
  assert.ok("disabled" in save.attrs, "Save is disabled before any detection");
  assert.ok((save.attrs.title || "").includes(RAIL.profileSaveDisabled),
    "the disabled Save says why in its tooltip");
  // After a detection run the gate opens.
  setState({ detectionRan: true });
  save = one(railBody(getState()), "#profile-save");
  assert.ok(!("disabled" in save.attrs), "Save is enabled once detection has run");
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
    heuristicDiscovery: { ...getState().settings.heuristicDiscovery, strictness: "strict" } } });
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
  const on = railHTML({ ollama: { available: true, models: ["m"], detail: "" }, useLocalAI: true });
  assert.ok(!("disabled" in one(on, "#ollama-model").attrs));
  // The port is never gated: it is how a user FIXES a connection, so locking
  // it would lock them out of fixing the thing the gate complains about.
  assert.ok(!("disabled" in one(off, "#ollama-port").attrs));
});

// --- The Local-AI scan scope ---------------------------------------------

/** localAIHTML renders the Local AI section with documents and an optional
 *  scope, the route switched on and Ollama present so the fields are live. */
function localAIHTML({ documents = [], scope = null, useLocalAI = true } = {}) {
  resetState();
  setState({ ollama: { available: true, models: ["m"], detail: "" }, documents });
  if (useLocalAI) setUseLocalAI(true);
  if (scope) setAIScope(scope);
  return all(railBody(getState()), "section.rail-section")[1].outer;
}

test("the scan-scope picker defaults to all documents with no page controls", () => {
  const html = localAIHTML({
    documents: [{ name: "a.pdf", unit: "page", pageCount: 6 }],
  });
  const select = one(html, "#ai-scope-doc");
  assert.ok(select, "the Local AI section offers a document picker");
  const selected = all(select.outer, "option").find((o) => "selected" in o.attrs);
  assert.equal(selected.attrs.value, "", "all documents is the default");
  assert.ok(!exists(html, "input.ai-scope-mode"),
    "no mode control until a document is chosen");
  assert.ok(!exists(html, "#ai-pages"), "no page field until a document is chosen");
});

test("choosing a multi-unit document reveals the Entire/Specific control", () => {
  const html = localAIHTML({
    documents: [{ name: "a.pdf", unit: "page", pageCount: 6 }],
    scope: { docName: "a.pdf", mode: "all" },
  });
  const modes = all(html, "input.ai-scope-mode");
  assert.equal(modes.length, 2, "an Entire document / Specific pages pair appears");
  const values = modes.map((m) => m.attrs.value).sort();
  assert.deepEqual(values, ["all", "pages"]);
  const checked = modes.find((m) => "checked" in m.attrs);
  assert.equal(checked.attrs.value, "all", "entire document is the default choice");
  assert.ok(!exists(html, "#ai-pages"), "no page field until Specific pages is chosen");
});

test("Specific pages mode reveals a page field and a live read-out", () => {
  const html = localAIHTML({
    documents: [{ name: "a.pdf", unit: "page", pageCount: 20 }],
    scope: { docName: "a.pdf", mode: "pages", pages: "12-15,18" },
  });
  const field = one(html, "#ai-pages");
  assert.ok(field, "a free-text page field appears in Specific pages mode");
  assert.equal(field.attrs.value, "12-15,18", "the field shows the stored spec");
  assert.ok(exists(html, "#ai-pages-readout"),
    "a read-out reports how many units the spec resolves to");
  // 12,13,14,15,18 = five pages.
  const readout = textOf(html, "#ai-pages-readout");
  assert.ok(readout.includes("5"),
    `the read-out counts the resolved units, got ${readout}`);
});

test("a malformed page spec shows an inline error", () => {
  const html = localAIHTML({
    documents: [{ name: "a.pdf", unit: "page", pageCount: 20 }],
    scope: { docName: "a.pdf", mode: "pages", pages: "12,oops" },
  });
  assert.ok(exists(html, "#ai-pages-error"), "a bad token is named inline");
  const err = textOf(html, "#ai-pages-error");
  assert.ok(err.includes("oops"),
    `the error names the offending token, got ${err}`);
});

test("a single-unit document offers no page controls at all", () => {
  const html = localAIHTML({
    documents: [{ name: "note.txt", unit: "line", pageCount: 1 }],
    scope: { docName: "note.txt", mode: "all" },
  });
  assert.ok(!exists(html, "input.ai-scope-mode"),
    "a document with one addressable unit has nothing to scope over");
  assert.ok(!exists(html, "#ai-pages"));
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
// the handler falls back to "regex" and the "Auto detected values" (name)
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

test("the name group's select-all toggles the NAME categories, not the pattern ones", () => {
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

// --- Help tooltips instead of paragraphs ---------------------------------
//
// The Configure panel spends no permanent vertical space on prose. Every
// explanation is a help tooltip beside the label it explains, reachable by hover
// AND by keyboard. What stays inline is only what CHANGES: a validation error,
// the live confidence value, Ollama's availability, an active count, run status.

test("the Configure panel carries no explanatory paragraphs", () => {
  // The guard is structural, not a list of retired sentences: deleting the eight
  // named paragraphs and adding a ninth would satisfy a literal check and undo
  // the decision. So it counts <p class="hint"> in the rail and demands none.
  resetState();
  const html = railBody(getState());
  const paragraphs = all(html, "p.hint");
  assert.deepEqual(paragraphs.map((p) => stripTags(p.inner).trim()), [],
    "an explanation belongs in a help tooltip, not in a paragraph that is read " +
    "once and then occupies the panel forever");
});

test("every explained control carries a help tooltip beside its label", () => {
  resetState();
  const html = railBody(getState());
  const tooltips = all(html, "span.help");
  assert.ok(tooltips.length >= 8,
    `only ${tooltips.length} help tooltips: the explanations the paragraphs used ` +
    "to carry must still be reachable, not deleted");
  for (const tip of tooltips) {
    const iconBtn = one(tip.outer, "button.help-icon");
    const bubble = one(tip.outer, "span.help-bubble");
    // The icon is FOCUSABLE (a real button, not a span), so the explanation is
    // reachable by Tab and not only by pointer.
    assert.equal(iconBtn.attrs["aria-describedby"], bubble.attrs.id,
      "the bubble must be the icon's accessible description");
    assert.equal(bubble.attrs.role, "tooltip");
    assert.ok((iconBtn.attrs["aria-label"] ?? "").length > 0,
      "an icon-only button needs an accessible name");
    assert.ok(stripTags(bubble.inner).trim().length > 0, "an empty bubble explains nothing");
  }
});

test("each help bubble has a unique id, or aria-describedby points at the wrong one", () => {
  resetState();
  const ids = all(railBody(getState()), "span.help-bubble").map((b) => b.attrs.id);
  assert.equal(new Set(ids).size, ids.length, `duplicate bubble ids: ${ids.join(", ")}`);
});

test("the dynamic read-outs stay inline, where the user is watching them", () => {
  // A value that changes as a control moves cannot live behind a hover.
  resetState();
  const html = railBody(getState());
  assert.ok(exists(html, "output#min-confidence-value"), "the live confidence value");
  assert.ok(exists(html, "#min-confidence-effect"), "and what it currently excludes");
  assert.ok(/\d+\/\d+/.test(html), "the per-group active counts");
});

// identifyrail.test.js, tests for the Identify rail's route sections, its
// category grouping and its read-outs.
//
// views/identifyrail.js imports api.js, which only touches `window` inside
// its functions, so the module imports cleanly here. Only the PURE exports are
// exercised: the group tables, the section table, the preset table and the
// sentence that explains the confidence slider. Everything else in the view is
// wiring and belongs to the manual pass.
//
// Sections are addressed BY ID through sectionById() below, never by position in
// the rendered list. An index makes a test silently assert about its neighbour
// the moment a section is added or reordered, which is how a passing suite can
// stop covering the thing it names.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  CATEGORY_GROUPS, PATTERN_GROUPS, NAME_GROUPS, RAIL_SECTIONS, PRESETS,
  confidenceEffect, llmGateTooltip,
  llmDisabledTooltip, railBody, settingsPayload,
} from "./views/identifyrail.js";
import { CONFIGURE, RAIL, CATEGORY_LABELS } from "./copy.js";
import {
  ALL_CATEGORIES, NAME_CATEGORIES, HARD_PII_CATEGORIES, EXTENDED_PII_CATEGORIES,
  ADVANCED_PII_CATEGORIES, ALWAYS_ON_CATEGORIES,
  resetState, getState, setState, setUseLocalLLM,
  setLLMScope, setCategoryGroup, setUseBuiltInPatterns, setUseHeuristicDiscovery,
  setSignalSource, setSignalDerivation, applyPreset,
  SIGNAL_SOURCES, SIGNAL_DERIVATIONS, LLM_DETAIL_LEVELS,
} from "./state.js";
import { renderIdentifyRail } from "./views/identifyrail.js";
import { textOf, stripTags, all, one, exists } from "./testhtml.js";
import { container, fire } from "./testdom.js";

// PATTERN_CATEGORIES is every built-in pattern category the store knows, which
// is what the eight groups of the Built-in patterns section must cover exactly.
const PATTERN_CATEGORIES = [
  ...HARD_PII_CATEGORIES, ...EXTENDED_PII_CATEGORIES, ...ADVANCED_PII_CATEGORIES,
];

/**
 * sectionById(html, id) returns one rail section's outer HTML, found by the id
 * its own header carries. A section's markup contains its nested subgroups'
 * headers too, so the match is on the FIRST title inside it, which is its own.
 *
 * Addressing a section by id rather than by index is the point: with an index, a
 * new section silently re-points every test after it.
 */
function sectionById(html, id) {
  const sec = all(html, "section.cgroup").find((candidate) =>
    all(candidate.outer, "span.cgroup-title")[0]?.attrs["data-group-toggle"] === id);
  assert.ok(sec, `no rail section with id ${id}`);
  return sec.outer;
}

/** sectionAttrs(html, id) is the same lookup, returning the parsed section. */
function sectionAttrs(html, id) {
  const sec = all(html, "section.cgroup").find((candidate) =>
    all(candidate.outer, "span.cgroup-title")[0]?.attrs["data-group-toggle"] === id);
  assert.ok(sec, `no rail section with id ${id}`);
  return sec;
}

/** routeToggle(html, id) is one route section's header switch. */
function routeToggle(html, id) {
  const box = all(html, "input.route-toggle").find((t) => t.attrs["data-route"] === id);
  assert.ok(box, `no route switch for ${id}`);
  return box;
}

// --- every category is reachable from some group -------------------------

test("every switchable category belongs to exactly one group", () => {
  const seen = new Map();
  for (const [title, keys] of CATEGORY_GROUPS) {
    for (const key of keys) {
      assert.ok(!seen.has(key), `${key} is in both "${seen.get(key)}" and "${title}"`);
      seen.set(key, title);
    }
  }
  // ALWAYS_ON_CATEGORIES are deliberately outside the groups: they have no switch
  // anywhere in the rail, so a group row for them would be a control that does
  // nothing.
  const missing = ALL_CATEGORIES
    .filter((key) => !ALWAYS_ON_CATEGORIES.includes(key))
    .filter((key) => !seen.has(key));
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

test("every pattern category appears exactly once, in one of the eight groups", () => {
  // The Built-in patterns section is the ONLY place a pattern category has a
  // switch, and PATTERN_GROUPS is the only way in. A category in no group is a
  // category the user cannot switch, which reads as "not detected" and is not; a
  // category in two groups is two controls for one setting.
  assert.equal(PATTERN_GROUPS.length, 8, "eight classes, one group each");
  const seen = new Map();
  for (const [title, keys] of PATTERN_GROUPS) {
    for (const key of keys) {
      assert.ok(!seen.has(key), `${key} is in both "${seen.get(key)}" and "${title}"`);
      seen.set(key, title);
    }
  }
  assert.deepEqual([...seen.keys()].sort(), PATTERN_CATEGORIES.slice().sort());
});

test("the pattern groups are grouped by class, not by preset tier", () => {
  // A group named after a UI shorthand tells nobody where a new recognizer
  // belongs. These are the classes the established PII tools converge on, so the
  // membership is the claim each title makes and is worth pinning.
  const byTitle = Object.fromEntries(PATTERN_GROUPS);
  assert.deepEqual(byTitle[CONFIGURE.groupFinancial],
    ["iban", "bic", "credit_card", "crypto"],
    "financial account numbers are their own class");
  assert.deepEqual(byTitle[CONFIGURE.groupGovernment],
    ["vat", "matricule", "de_steuer_id", "es_nif"],
    "government and tax identifiers are their own class, and are country-scoped");
  assert.deepEqual(byTitle[CONFIGURE.groupHealth], ["uk_nhs"],
    "health data is an Article 9 special category, so it is split on regulation "
    + "rather than on taxonomy, one member or not");
  assert.deepEqual(byTitle[CONFIGURE.groupCredentials], ["database_uri"],
    "credentials are separated from network identifiers even though both look technical");
  assert.deepEqual(byTitle[CONFIGURE.groupNetwork], ["ip_address", "mac_address"]);
  // The contextual group is last, matching how the presets escalate.
  assert.equal(PATTERN_GROUPS[PATTERN_GROUPS.length - 1][0], CONFIGURE.groupContextual);
});

test("the name groups hold what a discovery method can emit, and no declaration", () => {
  assert.equal(NAME_GROUPS.length, 1);
  const [title, keys] = NAME_GROUPS[0];
  assert.equal(title, CONFIGURE.groupDetected);
  assert.deepEqual(keys, NAME_CATEGORIES);
  for (const key of ALWAYS_ON_CATEGORIES) {
    assert.ok(!keys.includes(key),
      `${key} is declarative and has no switch: it must not sit under a detected group`);
  }
});

test("CATEGORY_GROUPS is the two halves in render order, and nothing else", () => {
  // Both halves are addressed BY NAME everywhere. Nothing may go back to slicing
  // this list by position: that arithmetic is what makes adding a group a
  // two-place edit and silently re-points the bulk buttons when a group moves.
  assert.deepEqual(CATEGORY_GROUPS, [...PATTERN_GROUPS, ...NAME_GROUPS]);
});

test("every category group renders folded by default", () => {
  // The rail opens on what a user changes most, the route switches and the scope
  // summary. A wall of expanded category lists buries those and makes the panel
  // scroll for a setting most sessions never touch, so each category group opens
  // only when its owner reaches for the categories inside it. The render harness
  // proves the folded body has no laid-out checkboxes; this proves the markup the
  // harness measures, and that every group is present to fold.
  resetState();
  const html = railBody(getState());
  const catTitles = CATEGORY_GROUPS.map(([title]) => title);
  const seen = new Set();
  for (const sec of all(html, "section.cgroup")) {
    const title = all(sec.outer, "span.cgroup-title")[0];
    if (!title) continue;
    const name = stripTags(title.inner).trim();
    if (!catTitles.includes(name)) continue;
    seen.add(name);
    assert.equal(sec.attrs["data-open"], "false", `the "${name}" group must be folded by default`);
  }
  assert.deepEqual([...seen].sort(), [...catTitles].sort(),
    "every category group renders and each is checked for its folded state");
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
// The rail is the three user-operable detection routes, in the order they run,
// each named after the MECHANISM it is. Scope is not a peer of them: each route
// holds the settings its own mechanism reads.

test("the rail is three route sections, in the order the routes run", () => {
  assert.deepEqual(RAIL_SECTIONS.map(([id]) => id),
    ["rail-patterns", "rail-heuristic", "rail-local"]);
  assert.deepEqual(RAIL_SECTIONS.map(([, label]) => label),
    [RAIL.tabPatterns, RAIL.tabHeuristic, RAIL.tabLocalLLM]);
  for (const [id, label] of RAIL_SECTIONS) {
    assert.ok(label.trim().length > 0, `${id} has no label`);
  }
});

test("every route section names a REAL settings flag, and none names a sentinel", () => {
  // A section switch must be the flag it claims to be: a derived section state
  // can read "On" while nothing the section names actually runs, and the user has
  // no way to tell.
  const keys = Object.fromEntries(RAIL_SECTIONS.map(([id, , key]) => [id, key]));
  assert.deepEqual(keys, {
    "rail-patterns": "useBuiltInPatterns",
    "rail-heuristic": "useHeuristicDiscovery",
    "rail-local": "useLocalLLM",
  });
  resetState();
  const stored = getState().settings;
  for (const [id, , key] of RAIL_SECTIONS) {
    assert.notEqual(key, "derived", `${id} must not name a sentinel`);
    assert.equal(typeof stored[key], "boolean",
      `${id} names ${key}, which must be a stored boolean`);
  }
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

// --- The local-model gate tooltip ----------------------------------------

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

  const off = llmGateTooltip({ ollama: { available: true }, settings: { useLocalLLM: false } });
  assert.equal(off, CONFIGURE.llmOffTooltip);
  assert.notEqual(off, missing);
});

// --- the confidence read-out, rewritten by -------------------------------
//
// The mock-up's copy described a SOURCE-TIERED rule ("values that only the local
// model suggested are skipped"), which the engine does not implement. What the
// setting actually is is a FLOOR on the confidence score every detection
// carries, so the copy describes the floor. These tests pin that it does not
// drift back into promising the rule the engine does not have.

test("the confidence read-out says nothing is skipped at the default", () => {
  assert.match(confidenceEffect(0), /Nothing is skipped/);
});

test("the confidence read-out describes a floor, not a rule about sources", () => {
  for (let percent = 0; percent <= 100; percent += 5) {
    const sentence = confidenceEffect(percent);
    assert.ok(!/only the local model suggested/i.test(sentence),
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
  if (patch.useLocalLLM) setUseLocalLLM(true);
  return railBody(getState());
}

test("the rail renders three routes plus the two switch-less panels", () => {
  const html = railHTML();
  // Exactly three DETECTION ROUTE sections carry .rail-section; the render
  // harness (scripts/uitest/probes.js) counts the same class. Detection quality
  // and Load profile are switch-less panels with their own .rail-panel class, so
  // they must NOT be counted as routes.
  const sections = all(html, "section.rail-section");
  assert.equal(sections.length, 3);
  const titles = sections.map((sec) =>
    stripTags(all(sec.outer, "span.cgroup-title")[0].inner).trim());
  assert.deepEqual(titles, [RAIL.tabPatterns, RAIL.tabHeuristic, RAIL.tabLocalLLM]);
  // The two panels sit after the routes, in this order.
  const panels = all(html, "section.rail-panel").map((sec) =>
    stripTags(all(sec.outer, "span.cgroup-title")[0].inner).trim());
  assert.deepEqual(panels, [RAIL.qualityTitle, RAIL.profileTitle]);
  assert.ok(html.indexOf(RAIL.qualityTitle) > html.indexOf(RAIL.tabLocalLLM),
    "the panels come after the last route");
});

test("each route section carries its own help beside its switch", () => {
  // The switch says whether the mechanism runs; the tooltip says what it is. A
  // section with a switch and no explanation is a control nobody can evaluate.
  const html = railHTML();
  for (const [id, label] of RAIL_SECTIONS) {
    // The section's OWN header is the first .cgroup-head inside it: the ones
    // after it belong to the category groups nested in its body.
    const head = all(sectionById(html, id), ".cgroup-head")[0].outer;
    assert.ok(exists(head, "span.help"), `${label} has no help tooltip on its header`);
    assert.ok(exists(head, "input.route-toggle"), `${label} has no switch on its header`);
    assert.ok(head.indexOf('class="help"') < head.indexOf("route-switch"),
      `${label}: the explanation comes before the switch it explains`);
  }
});

test("the two offline routes are ON by default, and switchable", () => {
  const html = railHTML();
  for (const id of ["rail-patterns", "rail-heuristic"]) {
    const box = routeToggle(html, id);
    assert.ok("checked" in box.attrs, `${id} runs unless the user says otherwise`);
    assert.ok(!("disabled" in box.attrs), "and the user can say otherwise");
  }
});

test("Local LLM discovery is OFF by default even when Ollama is detected", () => {
  const html = railHTML({ ollama: { available: true, models: [], detail: "" } });
  const local = routeToggle(html, "rail-local");
  assert.ok(!("checked" in local.attrs),
    "detecting Ollama enables the switch, it does not flip it");
  assert.ok(!("disabled" in local.attrs));
});

test("Built-in patterns owns the document country and the preset", () => {
  // They are the scope of the pattern pass, so they sit in the section whose
  // switch decides whether that pass runs at all.
  const patterns = sectionById(railHTML(), "rail-patterns");
  assert.ok(exists(patterns, "#document-country"), "the document country");
  assert.ok(exists(patterns, "[data-preset]"), "the presets");
  assert.ok(exists(patterns, "#preset-also-sets"),
    "and the read-out naming what else the level switched on");
  // No stray label row over the groups: under a section already titled "Built-in
  // patterns", a "Categories" heading says nothing and costs a row.
  assert.ok(!patterns.includes("Categories"), "the Categories label row is gone");
});

test("Built-in patterns owns every pattern category and no name category", () => {
  const patterns = sectionById(railHTML(), "rail-patterns");
  const keys = all(patterns, "input.cat-toggle").map((b) => b.attrs["data-category"]);
  assert.deepEqual(keys.slice().sort(), PATTERN_CATEGORIES.slice().sort());
});

test("Heuristic discovery owns the name categories and exactly one subgroup", () => {
  const heuristic = sectionById(railHTML(), "rail-heuristic");
  const keys = all(heuristic, "input.cat-toggle").map((b) => b.attrs["data-category"]);
  assert.deepEqual(keys.slice().sort(), NAME_CATEGORIES.slice().sort());
  const subgroups = all(heuristic, ".rail-subgroup");
  assert.equal(subgroups.length, 1, "the strictness block, and nothing else nested");
  assert.ok(exists(subgroups[0].outer, "#smart-strictness"),
    "and it is the one holding the strictness lever");
  assert.ok(exists(heuristic, "#smart-min-length"), "its strictness fields render");
});

test("Detection quality is a switch-less panel carrying the cross-route floor", () => {
  // The floor governs every route that is on, so placing it inside one of them
  // would mislabel it as that route's own knob.
  const html = railHTML();
  const quality = sectionAttrs(html, "rail-quality");
  assert.match(quality.attrs.class, /rail-panel/);
  assert.ok(!/rail-section/.test(quality.attrs.class),
    ".rail-section marks a detection ROUTE, which this is not");
  assert.ok(!exists(quality.outer, "input.route-toggle"), "and it has no switch");
  assert.ok(exists(quality.outer, "#min-confidence"), "the floor lives here");
  assert.ok(exists(quality.outer, "output#min-confidence-value"));
  // Folded by default, so its header has to state the value it is holding.
  assert.equal(quality.attrs["data-open"], "false");
  assert.equal(stripTags(one(quality.outer, "span.cgroup-count").inner).trim(), "0%");

  // And it is nowhere else: one setting, one control.
  assert.equal(all(html, "#min-confidence").length, 1);
  for (const id of ["rail-patterns", "rail-heuristic", "rail-local"]) {
    assert.ok(!exists(sectionById(html, id), "#min-confidence"),
      `the cross-route floor must not render inside ${id}`);
  }
});

test("the heuristic block's own minimum confidence is not the cross-route floor", () => {
  // Two different settings that are easy to read as one: this one is read by
  // heuristic discovery alone, the slider decides what a run replaces.
  const html = railHTML();
  const heuristic = sectionById(html, "rail-heuristic");
  assert.ok(exists(heuristic, "#smart-min-confidence"), "the route's own strictness field");
  assert.ok(!exists(heuristic, "#min-confidence"), "and not the cross-route floor");
});

test("every signal that derives Suggestions has a category row to hang off", () => {
  // The drill-down lives ON the row of the pattern that produces the evidence,
  // which only works because a signal source identifier IS a category key. A
  // source with no row would be a control with nowhere to render.
  const grouped = new Set(CATEGORY_GROUPS.flatMap(([, keys]) => keys));
  for (const source of SIGNAL_SOURCES) {
    assert.ok(grouped.has(source),
      `the signal ${source} is in no category group, so its readings have no row`);
  }
});

test("a signal's readings hang off its own category row, collapsed", () => {
  // Collapsed the readings cost NO row, which is what keeps the panel short as
  // signals and readings are added, and puts them beside the signal they read
  // rather than in a block of their own that has to name it again.
  resetState();
  const patterns = sectionById(railBody(getState()), "rail-patterns");
  const rows = all(patterns, ".signal-row");
  assert.deepEqual(rows.map((r) => r.attrs["data-signal-source"]), SIGNAL_SOURCES,
    "one drill-down per signal that implements discovery, and no other");

  const row = rows[0].outer;
  // The row it hangs off is the signal's own pattern category, checkbox and all.
  assert.equal(one(row, "input.cat-toggle").attrs["data-category"], SIGNAL_SOURCES[0],
    "the drill-down is on the row of the category that IS the signal");
  assert.ok(!("data-open" in rows[0].attrs), "a signal starts collapsed");
  assert.equal(one(row, ".signal-drill").attrs["aria-expanded"], "false");
  assert.ok("hidden" in one(row, ".signal-readings").attrs,
    "the readings are hidden while the drill-down is collapsed");
  assert.equal(textOf(row, "span.signal-count"), RAIL.signalDerivationCount(2),
    "and the panel says how many readings are on");
});

test("the help icon sits after the drill-down that opens what it explains", () => {
  resetState();
  const patterns = sectionById(railBody(getState()), "rail-patterns");
  const head = one(all(patterns, ".signal-row")[0].outer, ".signal-row-head").outer;
  assert.ok(head.indexOf('class="cat-label"') < head.indexOf('class="signal-drill"'),
    "the category label comes first");
  assert.ok(head.indexOf('class="signal-drill"') < head.indexOf('class="help"'),
    "then the drill-down, then the icon explaining it");
  assert.ok(head.indexOf('class="help"') < head.indexOf('class="cat-example"'),
    "and the example closes the row");
});

test("a signal's readings are its implemented ones, each individually switchable", () => {
  // A row with nothing behind it is a control that appears to do something and
  // does not, so the readings come from SIGNAL_DERIVATIONS, which the Go parity
  // guard holds to the engine's own producers.
  resetState();
  const patterns = sectionById(railBody(getState()), "rail-patterns");
  const boxes = all(patterns, "input.signal-box");
  assert.deepEqual(boxes.map((b) => b.attrs["data-derivation"]),
    SIGNAL_SOURCES.flatMap((source) => SIGNAL_DERIVATIONS[source]));
  assert.ok(boxes.every((b) => "checked" in b.attrs),
    "every reading defaults on: the evidence is deterministic and deriving from it "
    + "is why the feature exists");

  // And the master is keyed by SOURCE, so the two levels cannot be confused.
  const master = all(patterns, "input.signal-master");
  assert.deepEqual(master.map((b) => b.attrs["data-source"]), SIGNAL_SOURCES);
});

test("every reading explains itself through a tooltip", () => {
  // "Person names" alone does not say that clearing it leaves the address itself
  // anonymised, which is the distinction the whole setting exists for.
  resetState();
  const patterns = sectionById(railBody(getState()), "rail-patterns");
  const rows = all(patterns, ".signal-reading");
  assert.equal(rows.length,
    SIGNAL_SOURCES.reduce((n, source) => n + SIGNAL_DERIVATIONS[source].length, 0));
  for (const row of rows) {
    assert.ok(exists(row.outer, "span.help"),
      `a reading with no help tooltip: ${stripTags(row.inner).trim()}`);
  }
});

test("the drill-down's own checkbox is a DERIVED master over its readings", () => {
  // On when any reading is on, off only when all of them are. Never stored: a
  // fourth flag beside the readings it summarises could disagree with them, and a
  // master reading "on" over readings that are all off lies about what a run does.
  resetState();
  const railFor = () => sectionById(railBody(getState()), "rail-patterns");

  // Scoped to the email row: the rail carries one master per signal source, and
  // this is a question about ONE source's master over ITS readings.
  const emailMaster = () => all(railFor(), "input.signal-master")
    .find((b) => b.attrs["data-source"] === "email");

  setSignalDerivation("email", "email.person", false);
  assert.ok("checked" in emailMaster().attrs,
    "one reading off leaves the signal reading on, because it still derives something");

  setSignalDerivation("email", "email.organisation", false);
  assert.ok(!("checked" in emailMaster().attrs),
    "every reading off is the only thing that reads as off");
});

test("the panel's read-out reads Off once every reading is cleared", () => {
  resetState();
  for (const source of SIGNAL_SOURCES) setSignalSource(source, false);
  const patterns = sectionById(railBody(getState()), "rail-patterns");
  for (const count of all(patterns, "span.signal-count")) {
    assert.equal(stripTags(count.inner).trim(), RAIL.signalSourcesOff);
  }
});

test("clearing ONE reading leaves the other producing its own Suggestions", () => {
  // The whole point of the per-reading switches: they are independent, and the
  // engine honours each on its own (backend/engine/signaldiscovery_test.go).
  resetState();
  setSignalDerivation("email", "email.person", false);
  const patterns = sectionById(railBody(getState()), "rail-patterns");
  const boxes = all(patterns, "input.signal-box");
  const person = boxes.find((b) => b.attrs["data-derivation"] === "email.person");
  const org = boxes.find((b) => b.attrs["data-derivation"] === "email.organisation");
  assert.ok(!("checked" in person.attrs), "the cleared reading is off");
  assert.ok("checked" in org.attrs, "and the other one is untouched");
});

test("the drill-down stays usable while Built-in patterns is off", () => {
  // Which readings may derive Suggestions is its OWN setting: switching the
  // pattern pass off greys its categories out, and must not silently take the
  // readings away with them.
  resetState();
  setUseBuiltInPatterns(false);
  const patterns = sectionById(railBody(getState()), "rail-patterns");
  const row = all(patterns, ".signal-row")[0].outer;
  assert.ok("disabled" in one(row, "input.cat-toggle").attrs,
    "the pattern category itself is greyed out");
  for (const box of all(row, "input.signal-box")) {
    assert.ok(!("disabled" in box.attrs), "and its readings stay switchable");
  }
});

test("clearing a signal source does not disable the signal's own category", () => {
  // Acceptance criterion 4, at the view level: the two are different mechanisms,
  // and the control must not quietly do both.
  resetState();
  setSignalSource("email", false);
  const patterns = sectionById(railBody(getState()), "rail-patterns");
  const email = all(patterns, "input.cat-toggle").find((b) => b.attrs["data-category"] === "email");
  assert.ok("checked" in email.attrs, "the email category stays on");
  assert.ok(!("disabled" in email.attrs), "and stays editable");
  assert.ok("checked" in routeToggle(railBody(getState()), "rail-patterns").attrs,
    "and the Built-in patterns section stays on");
});

test("turning built-in patterns off disables the pattern category block only", () => {
  resetState();
  setUseBuiltInPatterns(false);
  const html = railBody(getState());
  // The structured signal categories (email, vat, ...) go disabled; the name
  // categories (person_names, ...) are in another section, governed by another
  // switch, and stay editable.
  const boxIn = (id, key) => all(sectionById(html, id), "input.cat-toggle")
    .find((b) => b.attrs["data-category"] === key);
  const email = boxIn("rail-patterns", "email");
  const person = boxIn("rail-heuristic", "person_names");
  assert.ok("disabled" in email.attrs,
    "the pattern category is disabled while Built-in patterns is off");
  assert.ok(!("disabled" in person.attrs),
    "a name category belongs to Heuristic discovery, which is still on");
  // The selection is not cleared: the checkbox keeps its checked state.
  assert.ok("checked" in email.attrs, "the stored selection is preserved, not cleared");
});

test("each header switch renders its OWN flag, and no other section's", () => {
  // One switch, one mechanism. This is the render half of that claim: the switch
  // shown for a section follows only the flag that section names, so a user
  // reading the rail sees three independent answers rather than one summary.
  resetState();
  setUseBuiltInPatterns(false);
  let html = railBody(getState());
  assert.ok(!("checked" in routeToggle(html, "rail-patterns").attrs),
    "the pattern section follows useBuiltInPatterns");
  assert.ok("checked" in routeToggle(html, "rail-heuristic").attrs,
    "and its neighbour is untouched");

  resetState();
  setUseHeuristicDiscovery(false);
  html = railBody(getState());
  assert.ok(!("checked" in routeToggle(html, "rail-heuristic").attrs));
  assert.ok("checked" in routeToggle(html, "rail-patterns").attrs);
});

test("a switched-off section is marked as off, and its section still renders", () => {
  // .route-off is the CSS hook that greys a section down. The section stays on
  // screen because its selection is still visible and still restored when the
  // switch comes back.
  resetState();
  setUseBuiltInPatterns(false);
  const patterns = sectionAttrs(railBody(getState()), "rail-patterns");
  assert.match(patterns.attrs.class, /route-off/);
  assert.ok(exists(patterns.outer, "input.cat-toggle"), "the scope is still shown");
});

// --- The Load profile section (CR7) --------------------------------------

test("the Load profile section renders AFTER the routes", () => {
  resetState();
  const html = railBody(getState());
  // Ordering by first appearance: the profile title must come after the last
  // route's title, so the section sits at the foot of the rail.
  assert.ok(html.includes(RAIL.profileTitle), "the Load profile section renders");
  const lastRoute = RAIL_SECTIONS[RAIL_SECTIONS.length - 1][1];
  assert.ok(html.includes(lastRoute), "the last route renders");
  assert.ok(html.indexOf(RAIL.profileTitle) > html.indexOf(lastRoute),
    "Load profile is below the last route");
});

test("the Load profile section has a Load and a Save button", () => {
  resetState();
  const html = railBody(getState());
  assert.ok(exists(html, "#profile-load"), "Load button present");
  assert.ok(exists(html, "#profile-save"), "Save button present");
});

test("the profile Save is disabled until Go actually holds a registry", () => {
  resetState();
  // Fresh session: no run yet, so Save is disabled with the reason.
  let save = one(railBody(getState()), "#profile-save");
  assert.ok("disabled" in save.attrs, "Save is disabled before any run");
  assert.ok((save.attrs.title || "").includes(RAIL.profileSaveDisabled),
    "the disabled Save says why in its tooltip");
  // A run producing a registry opens the gate.
  setState({ replacedValues: [{ original: "Alpine Trust", placeholder: "[ENTITY_1]", category: "entity_names", count: 1 }] });
  save = one(railBody(getState()), "#profile-save");
  assert.ok(!("disabled" in save.attrs), "Save is enabled once a run has produced a registry");
});

test("the profile Save gate closes again once the registry empties, e.g. after stepping back from Anonymise", () => {
  resetState();
  setState({ replacedValues: [{ original: "Alpine Trust", placeholder: "[ENTITY_1]", category: "entity_names", count: 1 }] });
  assert.ok(!("disabled" in one(railBody(getState()), "#profile-save").attrs));
  // STEP_RESETS.anonymise() clears replacedValues on a backward move; a
  // "detection ran" latch would stay on and silently offer to save nothing.
  setState({ replacedValues: [] });
  const save = one(railBody(getState()), "#profile-save");
  assert.ok("disabled" in save.attrs, "Save must close again once the registry is gone");
});

test("the strictness lever is a select of the three levels, balanced by default", () => {
  const heuristic = sectionById(railHTML(), "rail-heuristic");
  const select = one(heuristic, "#smart-strictness");
  const values = all(select.outer, "option").map((o) => o.attrs.value);
  assert.deepEqual(values, ["lenient", "balanced", "strict"],
    "the three engine strictness levels, in order");
  const selected = all(select.outer, "option").find((o) => "selected" in o.attrs);
  assert.equal(selected.attrs.value, "balanced", "balanced is the default selection");
});

test("the strictness block is a nested subgroup, so CSS can inset it", () => {
  // The inset is a CSS rule keyed on this class (.rail-subgroup > .cgroup-body).
  // .cgroup-body carries no padding of its own, so a strictness block that stopped
  // being a .rail-subgroup would silently sit flush against its border again,
  // hanging left of every label above it. The pixels are the harness's job; the
  // hook the rule needs is this one's.
  const heuristic = sectionById(railHTML(), "rail-heuristic");
  const subgroups = all(heuristic, ".rail-subgroup");
  assert.equal(subgroups.length, 1, "Heuristic discovery nests exactly one subgroup");
  assert.ok(exists(subgroups[0].outer, "#smart-strictness"),
    "and it is the one holding the strictness lever");
});

test("every strictness field explains itself through a tooltip", () => {
  // The Configure panel explains itself through tooltips, never prose: a field
  // with no tooltip is a control whose only explanation would have to be a
  // paragraph, and a paragraph is what put the controls at the foot of the panel
  // out of reach.
  const heuristic = sectionById(railHTML(), "rail-heuristic");
  const block = all(heuristic, ".rail-subgroup")[0].outer;
  const rows = all(block, ".rail-field-row");
  assert.ok(rows.length >= 4, `the block has its four fields, got ${rows.length}`);
  for (const row of rows) {
    assert.ok(exists(row.outer, "span.help"),
      `a strictness field with no help tooltip: ${stripTags(row.inner).trim()}`);
  }
  // The block's own switch is explained too, so nothing in it is unexplained.
  for (const toggle of all(block, ".rail-toggle")) {
    assert.ok(exists(toggle.outer, "span.help"),
      `a strictness switch with no help tooltip: ${stripTags(toggle.inner).trim()}`);
  }
});

test("the strictness select reflects a non-default stored value", () => {
  resetState();
  setState({ settings: { ...getState().settings,
    heuristicDiscovery: { ...getState().settings.heuristicDiscovery, strictness: "strict" } } });
  const heuristic = sectionById(railBody(getState()), "rail-heuristic");
  const selected = all(one(heuristic, "#smart-strictness").outer, "option")
    .find((o) => "selected" in o.attrs);
  assert.equal(selected.attrs.value, "strict");
});

test("every switchable category is reachable without switching anything", () => {
  // One setting, one control. A second copy of a category inside another section
  // would be folded shut by default and therefore not reachable at all, which is
  // what this test is for.
  const boxes = all(railHTML(), "input.cat-toggle").map((b) => b.attrs["data-category"]);
  const expected = ALL_CATEGORIES.filter((c) => !ALWAYS_ON_CATEGORIES.includes(c));
  assert.deepEqual(boxes.slice().sort(), expected.slice().sort(),
    "every switchable category exactly once, with no duplicate checkbox for one setting");
});

test("a category with no switch renders no checkbox anywhere in the rail", () => {
  // custom_patterns is declarative: its editor is the workspace's Custom patterns
  // tab. A switch here would be a second control for a setting that must stay on,
  // and clearing it would leave a pattern editor whose patterns never run.
  const html = railHTML();
  const rendered = all(html, "input.cat-toggle").map((b) => b.attrs["data-category"]);
  for (const key of ALWAYS_ON_CATEGORIES) {
    assert.ok(!rendered.includes(key), `${key} must have no checkbox in the rail`);
  }
});

test("the active count still counts a switch-less category as on", () => {
  // The heading's read-out is over ALL_CATEGORIES, so a category with no switch
  // must be counted rather than silently dropped from the denominator.
  resetState();
  for (const level of ["soft", "medium", "advanced"]) {
    applyPreset(level);
    for (const key of ALWAYS_ON_CATEGORIES) {
      assert.equal(getState().settings.categories[key], true,
        `${key} is on at ${level}, so the "N of M categories on" read-out counts it`);
    }
  }
});

test("the Local LLM fields are disabled while the route is off", () => {
  const off = railHTML({ ollama: { available: true, models: ["m"], detail: "" } });
  assert.ok("disabled" in one(off, "#ollama-model").attrs);
  const on = railHTML({ ollama: { available: true, models: ["m"], detail: "" }, useLocalLLM: true });
  assert.ok(!("disabled" in one(on, "#ollama-model").attrs));
  // The port is never gated: it is how a user FIXES a connection, so locking
  // it would lock them out of fixing the thing the gate complains about.
  assert.ok(!("disabled" in one(off, "#ollama-port").attrs));
});

// --- The local-model scan scope ------------------------------------------

/** localLLMHTML renders the Local LLM discovery section with documents and an optional
 *  scope, the route switched on and Ollama present so the fields are live. */
function localLLMHTML({ documents = [], scope = null, useLocalLLM = true } = {}) {
  resetState();
  setState({ ollama: { available: true, models: ["m"], detail: "" }, documents });
  if (useLocalLLM) setUseLocalLLM(true);
  if (scope) setLLMScope(scope);
  return sectionById(railBody(getState()), "rail-local");
}

test("the scan-scope picker defaults to all documents with no page controls", () => {
  const html = localLLMHTML({
    documents: [{ name: "a.pdf", unit: "page", pageCount: 6 }],
  });
  const select = one(html, "#ai-scope-doc");
  assert.ok(select, "the Local LLM discovery section offers a document picker");
  const selected = all(select.outer, "option").find((o) => "selected" in o.attrs);
  assert.equal(selected.attrs.value, "", "all documents is the default");
  assert.ok(!exists(html, "input.ai-scope-mode"),
    "no mode control until a document is chosen");
  assert.ok(!exists(html, "#ai-pages"), "no page field until a document is chosen");
});

test("choosing a multi-unit document reveals the Entire/Specific control", () => {
  const html = localLLMHTML({
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
  const html = localLLMHTML({
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
  const html = localLLMHTML({
    documents: [{ name: "a.pdf", unit: "page", pageCount: 20 }],
    scope: { docName: "a.pdf", mode: "pages", pages: "12,oops" },
  });
  assert.ok(exists(html, "#ai-pages-error"), "a bad token is named inline");
  const err = textOf(html, "#ai-pages-error");
  assert.ok(err.includes("oops"),
    `the error names the offending token, got ${err}`);
});

test("a single-unit document offers no page controls at all", () => {
  const html = localLLMHTML({
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

test("the user's own patterns have left the rail entirely", () => {
  // A regex the user wrote is declarative, permanently on, and edited on the
  // workspace's Custom patterns tab. A rail group for it would be a switch with
  // nothing to decide.
  for (const [title, keys] of CATEGORY_GROUPS) {
    for (const key of ALWAYS_ON_CATEGORIES) {
      assert.ok(!keys.includes(key), `${key} must not be in the "${title}" group`);
    }
  }
  assert.ok(!CATEGORY_GROUPS.some(([title]) => /your own pattern/i.test(title)),
    "and there is no group left over that used to hold them");
});

test("a category only a detection route or manual entry can produce says so", () => {
  // The switch is NEVER disabled for these: a category gates manually typed
  // values too, so disabling brand_names with Ollama absent would silently stop
  // replacing a brand the user typed by hand. The honest signal is the label's
  // second element, and this turns that comment into something enforced.
  for (const category of ["brand_names", "other_names"]) {
    const [, description] = CATEGORY_LABELS[category];
    assert.match(description, /Local LLM discovery/,
      `${category} cannot be found offline, so its description must NAME the route that ` +
      `can find it, beside the fact that you can type it yourself`);
    assert.match(description, /add(ed)? by you/i,
      `${category} gates manually typed values too, so its description must say so`);
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
  const groupsByType = { pattern: PATTERN_GROUPS, name: NAME_GROUPS };
  const ds = datasetOf(btn.attrs);
  const type = ds.groupType || "pattern";
  const group = (groupsByType[type] ?? [])[Number(ds.group)];
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
  // The Contact details group is PATTERN_GROUPS[0], which the bug's fallback
  // would have toggled by mistake. These must stay exactly as they were.
  for (const pattern of ["email", "url", "iban", "vat"]) {
    assert.equal(cats[pattern], false,
      `${pattern} belongs to a pattern group and must be left untouched`);
  }
});

test("the name select-all carries the hyphenated data key so dataset reads it back (CR11)", () => {
  resetState();
  const btn = bulkButton(railBody(getState()), CONFIGURE.groupDetected, "1");
  // The literal attribute must be hyphenated: a camelCase key is lowercased by
  // the browser and dataset.groupType comes back undefined.
  assert.ok("data-group-type" in btn.attrs,
    "the bulk button must emit data-group-type, not a camelCase data-groupType");
  assert.equal(datasetOf(btn.attrs).groupType, "name",
    "the name group's button must resolve to the name group list");
});

test("every group's bulk buttons resolve back to that group's own keys", () => {
  // The bug this covers pointed one group's buttons at another group's keys. The
  // guard is exhaustive rather than a spot check, because the arithmetic that
  // broke was per group.
  resetState();
  const html = railBody(getState());
  const groupsByType = { pattern: PATTERN_GROUPS, name: NAME_GROUPS };
  for (const [type, groups] of Object.entries(groupsByType)) {
    groups.forEach(([title, keys], index) => {
      for (const on of ["1", "0"]) {
        const ds = datasetOf(bulkButton(html, title, on).attrs);
        assert.equal(ds.groupType, type, `"${title}" names the wrong group type`);
        assert.equal(Number(ds.group), index, `"${title}" names the wrong group index`);
        assert.deepEqual(groupsByType[ds.groupType][Number(ds.group)][1], keys,
          `"${title}" resolves to another group's keys`);
      }
    });
  }
});

// --- WIRING: one switch writes one flag ----------------------------------
//
// "This switch changes only this" is exactly the property a reader of the code
// cannot verify by reading it, so it is driven through the rendered checkbox.

/** railRoot() renders the whole rail, wiring included, into a fake DOM. */
function railRoot() {
  const root = container();
  // pushSettings reaches api.js, which has no bridge here; the reducer has
  // already run by then and the rejection is what the view itself tolerates.
  try { renderIdentifyRail(root); } catch { /* no bridge: the wiring still ran */ }
  return root;
}

/** flipRoute(root, id, on) clicks one section's header switch. */
async function flipRoute(root, id, on) {
  const box = [...root.querySelectorAll(".route-toggle")]
    .find((t) => t.getAttribute("data-route") === id);
  assert.ok(box, `no route switch for ${id}`);
  box.checked = on;
  await fire(box, "change");
}

test("the Built-in patterns switch writes its own flag and no other", async () => {
  resetState();
  await flipRoute(railRoot(), "rail-patterns", false);
  const s = getState().settings;
  assert.equal(s.useBuiltInPatterns, false, "its own flag moved");
  assert.equal(s.useHeuristicDiscovery, true, "the heuristic route is untouched");
  assert.equal(s.useLocalLLM, false, "and the local route is untouched");
  // And the signal readings survive it: signal-based discovery matches its own
  // evidence, so the pattern pass being off must not silently clear them.
  for (const source of SIGNAL_SOURCES) {
    for (const derivation of SIGNAL_DERIVATIONS[source]) {
      assert.equal(getState().settings.signalSuggestionSources?.[source]?.[derivation] ?? true,
        true, `${derivation} must survive the pattern pass being switched off`);
    }
  }
});

test("the Heuristic discovery switch writes its own flag and no other", async () => {
  resetState();
  await flipRoute(railRoot(), "rail-heuristic", false);
  const s = getState().settings;
  assert.equal(s.useHeuristicDiscovery, false, "its own flag moved");
  assert.equal(s.useBuiltInPatterns, true, "the pattern route is untouched");
  assert.equal(s.useLocalLLM, false, "and the local route is untouched");
});

test("the Local LLM discovery switch writes its own flag and no other", async () => {
  resetState();
  setState({ ollama: { available: true, models: ["m"], detail: "" } });
  await flipRoute(railRoot(), "rail-local", true);
  const s = getState().settings;
  assert.equal(s.useLocalLLM, true, "its own flag moved");
  assert.equal(s.useBuiltInPatterns, true, "the pattern route is untouched");
  assert.equal(s.useHeuristicDiscovery, true, "and the heuristic route is untouched");
});

// --- What the local model actually did last time --------------------------
//
// "0 values found" means two different things and only one of them is about the
// document. The read-out is what separates them, and its numbers are measured on
// the user's own machine, which is the half no tooltip can supply.

test("the rail says nothing about a local model scan that has not happened", () => {
  resetState();
  setUseLocalLLM(true);
  const html = railBody(getState());
  assert.ok(!exists(html, "#last-ai-scan"),
    "a read-out with nothing to report only teaches the reader to ignore it");
});

test("the rail reports the request count and the measured seconds of the last scan", () => {
  resetState();
  setUseLocalLLM(true);
  setState({ lastLLMScan: { requests: 15, silent: 0, secondsPerRequest: 7.2 } });
  const readout = one(railBody(getState()), "#last-ai-scan");
  assert.ok(readout, "the last scan must be reported somewhere the user will read it");
  const text = stripTags(readout.inner);
  assert.match(text, /15 requests/,
    `the request count is what makes "found nothing" legible: ${text}`);
  assert.match(text, /7\.2s/, `the measured seconds must be shown: ${text}`);
  assert.ok(!/returned nothing/.test(text),
    `a scan where everything answered must not mention silence: ${text}`);
});

test("the rail names total silence, and mentions partial silence without alarm", () => {
  resetState();
  setUseLocalLLM(true);

  setState({ lastLLMScan: { requests: 15, silent: 15, secondsPerRequest: 7 } });
  const allSilent = stripTags(one(railBody(getState()), "#last-ai-scan").inner);
  assert.match(allSilent, /nothing for any of them/,
    `every request answering nothing is the case that reads as a clean document: ${allSilent}`);

  setState({ lastLLMScan: { requests: 15, silent: 4, secondsPerRequest: 7 } });
  const partial = stripTags(one(railBody(getState()), "#last-ai-scan").inner);
  assert.match(partial, /4 returned nothing/,
    `a partly silent scan says how many, plainly: ${partial}`);
  assert.ok(!/any of them/.test(partial),
    `a normal scan must not be described as a silent model: ${partial}`);
});

test("the rail reports cut-off requests beside the silent ones, never folded into them", () => {
  // The two counts say opposite things about the document: a silent request
  // found nothing, a cut-off one found more than it could finish listing. Only
  // the second means values may be missing from pages that DID return some.
  resetState();
  setUseLocalLLM(true);

  setState({ lastLLMScan: { requests: 10, silent: 0, truncated: 2, secondsPerRequest: 7 } });
  const cut = stripTags(one(railBody(getState()), "#last-ai-scan").inner);
  assert.match(cut, /2 ran out of room/,
    `a cut-off reply must be named as such: ${cut}`);
  assert.match(cut, /may be missing/,
    `the read-out must say what a cut-off reply costs the user: ${cut}`);
  assert.ok(!/returned nothing/.test(cut),
    `a request that ran out of room is not a silent one: ${cut}`);

  setState({ lastLLMScan: { requests: 10, silent: 3, truncated: 2, secondsPerRequest: 7 } });
  const both = stripTags(one(railBody(getState()), "#last-ai-scan").inner);
  assert.match(both, /3 returned nothing/, `both facts are reported: ${both}`);
  assert.match(both, /2 ran out of room/, `both facts are reported: ${both}`);

  setState({ lastLLMScan: { requests: 10, silent: 0, truncated: 0, secondsPerRequest: 7 } });
  const clean = stripTags(one(railBody(getState()), "#last-ai-scan").inner);
  assert.ok(!/ran out of room/.test(clean),
    `a scan where nothing was cut off must not mention it: ${clean}`);
});

// --- The model dropdown --------------------------------------------------

test("exactly one model option is marked selected whenever models exist", () => {
  // Marking nothing lets the browser select the first option by itself while the
  // store holds something else, so the control shows one model and the next
  // settings write sends whichever the server listed first. Which model a fresh
  // session runs on is then decided by Ollama's tag ordering.
  resetState();
  const marked = (html) => all(one(html, "#ollama-model").outer, "option")
    .filter((o) => "selected" in o.attrs).map((o) => o.attrs.value);

  setState({ ollama: { available: true, models: ["first:1b", "second:4b"], detail: "" } });
  assert.deepEqual(marked(railBody(getState())), ["first:1b"],
    "with nothing stored the drawn option is the first, marked explicitly rather than by the browser");

  setState({ settings: { ...getState().settings, model: "second:4b" } });
  assert.deepEqual(marked(railBody(getState())), ["second:4b"], "a stored choice is what is drawn");

  setState({ settings: { ...getState().settings, model: "a-model-nobody-has:latest" } });
  assert.deepEqual(marked(railBody(getState())), ["first:1b"],
    "a stored model the probe cannot see falls back to an installed one, never to nothing marked");
});

test("no models installed leaves the dropdown saying so, with nothing selected", () => {
  // There is nothing to mark, and the placeholder option is the message: a
  // marked option here would name a model that does not exist.
  resetState();
  setState({ ollama: { available: true, models: [], detail: "" } });
  const select = one(railBody(getState()), "#ollama-model");
  const options = all(select.outer, "option");
  assert.equal(options.length, 1, "one placeholder option, not an invented model name");
  assert.equal(options[0].attrs.value, "");
  assert.ok(!("selected" in options[0].attrs));
});

// --- The detail level: the speed-versus-recall dial -----------------------

test("the detail level is a select of exactly the two levels Go validates", () => {
  resetState();
  const rail = railBody(getState());
  const select = one(rail, "#ai-detail-level");
  assert.ok(select, "the dial must be in the Local LLM discovery section, or the trade-off has no control");
  assert.deepEqual(all(select.outer, "option").map((o) => o.attrs.value), LLM_DETAIL_LEVELS,
    "the rail must offer the levels the engine sizes and no third one it would refuse");
  assert.ok("disabled" in select.attrs,
    "with the route off it is gated, exactly as the model field is");
});

test("exactly one detail-level option is marked selected, always", () => {
  // Leaving nothing marked lets the browser pick the first by itself, which is
  // how a choice gets made by option ordering instead of by the user.
  resetState();
  const marked = (html) => all(one(html, "#ai-detail-level").outer, "option")
    .filter((o) => "selected" in o.attrs).map((o) => o.attrs.value);

  assert.deepEqual(marked(railBody(getState())), ["thorough"],
    "a fresh session shows the default it will actually run");

  setState({ settings: { ...getState().settings, llmDetailLevel: "faster" } });
  assert.deepEqual(marked(railBody(getState())), ["faster"], "a stored choice is what is drawn");

  setState({ settings: { ...getState().settings, llmDetailLevel: "exhaustive" } });
  assert.deepEqual(marked(railBody(getState())), ["thorough"],
    "a level the rail does not offer falls back to thorough, never to nothing marked");
});

test("the detail level explains itself through a tooltip, in outcome terms", () => {
  resetState();
  const row = all(railBody(getState()), ".rail-field-row")
    .find((r) => exists(r.outer, "#ai-detail-level"));
  assert.ok(exists(row.outer, "span.help"),
    "a control whose only explanation would be a paragraph must carry a tooltip");
  assert.ok(RAIL.detailLevel.split(" ").length <= 3,
    `a rail label stays short, got "${RAIL.detailLevel}"`);
  const bubble = stripTags(one(row.outer, "span.help-bubble").inner);
  assert.match(bubble, /slices/,
    `the tooltip must say what the setting achieves, in outcome terms: ${bubble}`);
  assert.ok(!/\d+ ?(bytes|B|requests)/.test(bubble),
    `byte sizes and request counts are dynamic and belong in the read-out: ${bubble}`);
});

test("the detail level reaches the settings payload", () => {
  resetState();
  setUseLocalLLM(true);
  setState({ ollama: { available: true, models: ["m:1b"], detail: "" } });

  const root = container();
  root.innerHTML = railBody(getState());
  assert.equal(settingsPayload(getState(), root).llmDetailLevel, "thorough",
    "the default travels explicitly, so Go stores what the rail is showing");

  root.querySelector("#ai-detail-level").value = "faster";
  assert.equal(settingsPayload(getState(), root).llmDetailLevel, "faster",
    "choosing a level must reach Go, or the control is decoration");

  // A rail rendered without the Local LLM body contributes no element, and the
  // store's value is what travels: switching tabs must not reset the choice.
  setState({ settings: { ...getState().settings, llmDetailLevel: "faster" } });
  assert.equal(settingsPayload(getState(), container()).llmDetailLevel, "faster",
    "with the control off screen the stored choice is what is sent");
});

test("the request estimate is a read-out beside the dial, never a paragraph", () => {
  // The rail carries no p.hint at all and the Local LLM section is in the DOM
  // even when folded, so a read-out added as a hint turns the structural guard
  // red for a reason that reads as unrelated to this control.
  resetState();
  setUseLocalLLM(true);
  assert.ok(!exists(railBody(getState()), "#ai-request-estimate"),
    "before Go has answered there is no number, and a guess the run can contradict is worse than none");

  setState({ llmRequestEstimate: 12 });
  const html = railBody(getState());
  const readout = one(html, "#ai-request-estimate");
  assert.ok(readout, "the cost of the choice must be visible before the user pays it");
  assert.match(stripTags(readout.inner), /12 requests/,
    "the read-out names the request count the run will send");
  assert.deepEqual(all(html, "p.hint").map((p) => stripTags(p.inner).trim()), [],
    "a live fact is a span in a .rail-status row; p.hint is static prose the panel does not carry");
});

// --- The reply-format switch ---------------------------------------------

test("the reply-format switch renders off, explained, and gated", () => {
  // Off by default, because it is the slow end of a trade-off with no single
  // winner: it finds a little more on some documents and takes about twice as
  // long. Gated with the model and the context size, because it changes what a
  // request asks the model for and means nothing without one running.
  resetState();
  const off = railBody(getState());
  const box = one(off, "#ai-strict-format");
  assert.ok(!("checked" in box.attrs),
    "asking for every category is opt-in, so it must render unchecked");
  assert.ok("disabled" in box.attrs,
    "with the route off it is gated, exactly as the model field is");

  const toggle = all(off, ".rail-toggle").find((t) => exists(t.outer, "#ai-strict-format"));
  assert.ok(exists(toggle.outer, "span.help"),
    "a control whose only explanation would be a paragraph must carry a tooltip");
  assert.equal(stripTags(one(toggle.outer, "span.cat-label").inner).trim(), RAIL.strictFormat);
  assert.ok(RAIL.strictFormat.split(" ").length <= 4,
    `a rail label stays short, got "${RAIL.strictFormat}"`);
  assert.ok(stripTags(one(toggle.outer, "span.help-bubble").inner).includes("every category"),
    "the tooltip must say what the setting achieves, in outcome terms");
});

test("the reply-format switch reflects a stored on", () => {
  resetState();
  setUseLocalLLM(true);
  setState({ ollama: { available: true, models: ["m:1b"], detail: "" } });
  setState({ settings: { ...getState().settings, llmStrictFormat: true } });
  const box = one(railBody(getState()), "#ai-strict-format");
  assert.ok("checked" in box.attrs, "a stored on must draw a ticked box");
  assert.ok(!("disabled" in box.attrs),
    "with the route on and Ollama there, the control is usable");
});

test("the reply-format choice reaches the settings payload", () => {
  // The payload is what Go acts on, and this boolean must travel EXPLICITLY:
  // Go reads an absent value as off, so an omitted key would make "on"
  // unsayable.
  resetState();
  setUseLocalLLM(true);
  setState({ ollama: { available: true, models: ["m:1b"], detail: "" } });

  const root = container();
  root.innerHTML = railBody(getState());
  assert.equal(settingsPayload(getState(), root).llmStrictFormat, false,
    "an unticked box sends false, not nothing");

  root.querySelector("#ai-strict-format").checked = true;
  assert.equal(settingsPayload(getState(), root).llmStrictFormat, true,
    "ticking the box must reach Go, or the control is decoration");

  // A rail rendered without the Local LLM body contributes no element, and the
  // store's value is what travels: switching tabs must not reset the choice.
  setState({ settings: { ...getState().settings, llmStrictFormat: true } });
  assert.equal(settingsPayload(getState(), container()).llmStrictFormat, true,
    "with the control off screen the stored choice is what is sent");
});

test("the last scan read-out is a read-out, never an explanatory paragraph", () => {
  // The rail carries no p.hint at all, and the Local LLM section is in the DOM
  // even when folded, so a read-out added as a hint turns the structural guard
  // above red for a reason that reads as unrelated.
  resetState();
  setUseLocalLLM(true);
  setState({ lastLLMScan: { requests: 3, silent: 0, secondsPerRequest: 2 } });
  const html = railBody(getState());
  assert.deepEqual(all(html, "p.hint").map((p) => stripTags(p.inner).trim()), [],
    "a live fact is a .rail-readout; p.hint is static prose the panel does not carry");
  assert.equal(one(html, "#last-ai-scan").attrs.class, "rail-readout",
    "the read-out must carry the class the panel's live facts use");
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

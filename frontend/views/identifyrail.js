// views/identifyrail.js, the LEFT RAIL of wizard step 2, Identify
//
// The rail lists the DETECTION ROUTES, one switchable section each, in the
// order they run. ONE SWITCH, ONE MECHANISM: every section switch here is a
// real stored settings flag, and each section holds the settings its own
// mechanism reads. A switch governing several unrelated mechanisms cannot tell
// the user which of them found what, and the review gate is exactly the
// difference between them: a direct match is applied without review, a
// suggestion is not.
//
//   Built-in patterns ON by default (useBuiltInPatterns). Application-provided
//                    patterns for structured signals, which MATCH AND REPLACE
//                    the signal itself. Its scope, the document country, the
//                    preset and the eight category groups, is nested inside it.
//   Heuristic discovery ON by default (useHeuristicDiscovery). Spelling,
//                    context, frequency and deterministic gazetteers, producing
//                    Suggestions. It owns the name categories and its own
//                    strictness block.
//   Local LLM discovery OFF by default (useLocalLLM). The Ollama port (host
//                    locked to loopback, CLAUDE.md §8), the model, the detail
//                    level and the context size. Detecting Ollama enables the
//                    switch; it never flips it.
//
// Below the routes sit two switch-less panels: Detection quality, holding the
// match-confidence floor, which governs EVERY route that is on and therefore
// belongs to none of them; and Load profile.
//
// Signal-based discovery has no section of its own. Its readings hang off the
// category row of the pattern that produces the evidence, inside Built-in
// patterns, because that is where the question belongs.
//
// Nothing here gates the deterministic PII pass: that is not a detection
// route, it is what the Anonymise step always does.
//
// The allowlist editor moved OUT of here: it belongs to the workspace half of
// Identify now (views/identifyworkspace.js, the "Never anonymise" tab), because
// it is a list the user curates rather than a setting.
//
// Every model-dependent control everywhere in the application gates on
// llmEnabled(state) = useLocalLLM AND ollama.available, so the deterministic
// pipeline stays fully usable with Ollama absent.

import {
  applySettings, listOllamaModels, probeOllama, loadSession, saveSession,
  valuePlaceholders, listRemovedValues, estimateLLMRequests,
} from "../api.js";
import {
  getState, setState,
  applyPreset, toggleCategory, selectionPresetName, setUseLocalLLM,
  setUseBuiltInPatterns, setUseHeuristicDiscovery,
  SIGNAL_SOURCES, SIGNAL_DERIVATIONS,
  signalSourceOn, signalDerivationOn, enabledSignalDerivations,
  setSignalSource, setSignalDerivation,
  setCategoryGroup, setMinConfidence, setDocumentCountry,
  setHeuristicDiscoveryOptions, heuristicDiscoveryOptions,
  setLLMScope,
  llmScopeArg,
  adoptProbe,
  LLM_DETAIL_LEVELS,
  parsePageSpec,
  buildRunRequest, setValueTables,
  ALL_CATEGORIES,
  NAME_CATEGORIES,
} from "../state.js";
import { escapeHTML } from "../html.js";
import {
  button, chipRow, sectionLabel, collapsibleGroup, wireGroups,
  helpTooltip, wireHelpTooltips, signalDrillDown,
} from "../ui.js";
import { CARDS, CONFIGURE, RAIL, VALUES, categoryLabels } from "../copy.js";
import { examplesFor, countryOptions } from "../countries.js";
import { categoryAppliesTo, CATEGORY_COUNTRIES } from "../countries.js";
import { notify } from "../toast.js";
// applySession lives in export.js (it is the load half of the same save/load
// pair the Export step owns). Importing it here is a one-way edge: export.js
// never imports the rail, so the module graph stays acyclic.
import { applySession } from "./export.js";

/**
 * llmDisabledTooltip(port) is what every disabled LLM control says when Ollama
 * is missing (CLAUDE.md §4). It names the ADDRESS ACTUALLY PROBED: the port is
 * a setting, so a fixed "11434" in this sentence lies to the one user most
 * likely to be reading it, the one who changed the port.
 */
export function llmDisabledTooltip(port) {
  return `Requires Ollama, which was not detected on 127.0.0.1:${port || 11434}`;
}

/**
 * llmGateTooltip(s) picks the right explanation for a disabled model control:
 * Ollama missing vs the route being switched off. Two different problems with
 * two different fixes, so they must not share one message.
 */
export function llmGateTooltip(s) {
  if (!s.ollama?.available) return llmDisabledTooltip(s.settings?.ollamaPort);
  return CONFIGURE.llmOffTooltip;
}

// RAIL_SECTIONS is the rail's shape: [id, title, settings key that switches
// the route on]. The order is the order the routes run in.
//
// Every third element is a REAL settings key. A section switch must be the flag
// it claims to be: a derived or computed section state can read "On" while
// nothing the section names actually runs, and the user has no way to tell.
export const RAIL_SECTIONS = [
  ["rail-patterns", RAIL.tabPatterns, "useBuiltInPatterns"],
  ["rail-heuristic", RAIL.tabHeuristic, "useHeuristicDiscovery"],
  ["rail-local", RAIL.tabLocalLLM, "useLocalLLM"],
];

// SECTION_HELP is the explanation on each section header, keyed by section id.
// It is separate from RAIL_SECTIONS so that list stays exactly what its name
// says: the shape of the rail and the flag behind each switch.
const SECTION_HELP = {
  "rail-patterns": RAIL.tabPatternsHelp,
  "rail-heuristic": RAIL.tabHeuristicHelp,
  "rail-local": RAIL.tabLocalLLMHelp,
};

// Which sections and which category groups the user folded shut. A VIEW
// preference rather than application state: nothing downstream reads it, it
// must not travel in a session file, and putting it in the store would mean
// every fold went through a reducer.
//
// Local LLM discovery starts folded: it is off, and an open panel of disabled
// fields is noise above the settings that ARE in use. Detection quality and Load
// profile start folded for the same reason: most sessions never touch either,
// and the quality panel's header states its value while it is shut.
const collapsedGroups = new Set(["rail-local", "rail-quality", "rail-profile"]);

// Which signal rows are EXPANDED to show their individual readings. A VIEW
// preference, like the folded sections: nothing downstream reads it, it must not
// travel in a session file, and putting it in the store would mean every open and
// close went through a reducer.
const openSignalSources = new Set();

// PRESETS: engine level → user-facing label. "soft/medium/advanced" reads too
// technical; Standard and Thorough say what they mean.
export const PRESETS = [
  ["soft", "Soft"],
  ["medium", "Standard"],
  ["advanced", "Thorough"],
];

// PATTERN_GROUPS is the built-in pattern categories, grouped by CLASS:
// [visible title, category keys]. It renders inside the Built-in patterns
// section, which is what matches them.
//
// The classes are the ones the established PII tools converge on: financial
// account numbers are their own class, government and tax identifiers are their
// own class and are country-scoped (which engine.CategoryCountries already
// models), and credentials are separated from network identifiers even though
// both look "technical". Two departures are deliberate. Health identifiers get
// their own group with one member today, because health data is an Article 9
// special category under the GDPR in this application's market, so the split is
// regulatory rather than taxonomic. Dates and monetary amounts get a group no
// benchmark has, because this application treats them as contextual identifiers
// rather than as PII.
//
// Order is broadest-first with the contextual group last, matching how the
// presets escalate. Grouping by class rather than by preset tier is what gives a
// new recognizer an obvious home: identifyrail.test.js asserts that every
// pattern category the store knows about appears in exactly one group, because a
// category in no group is a category the user cannot switch, which reads as "not
// detected" and is not.
export const PATTERN_GROUPS = [
  [CONFIGURE.groupContact, ["email", "phone", "url"]],
  [CONFIGURE.groupLocations, ["address", "postal_code"]],
  [CONFIGURE.groupFinancial, ["iban", "bic", "credit_card", "crypto"]],
  [CONFIGURE.groupGovernment, ["vat", "matricule", "de_steuer_id", "es_nif"]],
  [CONFIGURE.groupHealth, ["uk_nhs"]],
  [CONFIGURE.groupNetwork, ["ip_address", "mac_address"]],
  [CONFIGURE.groupCredentials, ["database_uri"]],
  [CONFIGURE.groupContextual, ["date", "amount"]],
];

// NAME_GROUPS is the name categories a DISCOVERY method can emit. They render
// inside the Heuristic discovery section, which is what discovers them offline,
// and Local LLM discovery reads the same one selection rather than a second copy
// of the boxes.
//
// custom_patterns is deliberately absent: a regex the user wrote is DECLARATIVE,
// its editor is the workspace's Custom patterns tab, and it has no switch here at
// all (state.js ALWAYS_ON_CATEGORIES keeps it on). What is left in this group is,
// by construction, exactly the set a discovery method can emit.
export const NAME_GROUPS = [
  [CONFIGURE.groupDetected, NAME_CATEGORIES],
];

// CATEGORY_GROUPS is every group the rail renders, in render order. Both halves
// are addressed BY NAME everywhere: nothing may go back to slicing this list by
// position, because that arithmetic is what makes adding a group a two-place
// edit and silently re-points the bulk buttons when a group moves.
export const CATEGORY_GROUPS = [...PATTERN_GROUPS, ...NAME_GROUPS];

// GROUPS_BY_TYPE is the lookup the bulk select-all buttons use: the data
// attribute carries the type and the index, and this resolves them back to the
// key list. Keyed by the same tokens the group ids are built from.
const GROUPS_BY_TYPE = { pattern: PATTERN_GROUPS, name: NAME_GROUPS };

// The category groups start FOLDED. The rail opens on what a user changes most:
// the route switches and the scope summary (country, preset). A wall of expanded
// category lists buries those above the fold and makes the panel scroll for a
// setting most sessions never touch, so each group opens only when its owner
// reaches for the categories inside it. The IDs match the ones categoryGroups()
// builds, `cat-group-<type>-<index>`, and are derived from the group lists so a
// group added or reordered folds by default too.
for (const [type, groups] of Object.entries(GROUPS_BY_TYPE)) {
  groups.forEach((_g, index) => collapsedGroups.add(`cat-group-${type}-${index}`));
}

/**
 * renderIdentifyRail(container) fills the rail card.
 * @param {HTMLElement} container the card element views/identify.js created
 */
export function renderIdentifyRail(container) {
  const s = getState();
  const active = ALL_CATEGORIES.filter((c) => s.settings.categories?.[c]).length;

  container.innerHTML =
    `<div class="card-head">` +
    `<div class="card-head-left"><h2>${escapeHTML(CARDS.configure.title)}</h2></div>` +
    `<span class="card-sub">${escapeHTML(RAIL.activeCount(active, ALL_CATEGORIES.length))}</span>` +
    `</div>` +
    `<div class="card-body">${railBody(s)}</div>` +
    `<div id="settings-error"></div>`;

  // Every section is on screen at once now, so every section is wired every
  // time. There is no tab-switching dance deciding which wiring runs
  // with the tabs.
  wireSectionSwitches(container);
  wireScope(container);
  wireSmart(container);
  wireSignalSources(container);
  wireLocalLLM(container);
  wireProfile(container);
  wireHelpTooltips(container);
  wireGroups(container, (id) => {
    if (collapsedGroups.has(id)) collapsedGroups.delete(id);
    else collapsedGroups.add(id);
    setState({}); // repaint; folding is view state
  });
}

/**
 * railBody(s) renders the route sections. Exported for the render tests: which
 * sections exist and which switch is on is load-bearing for what a run does.
 *
 * Each is a collapsibleGroup whose header carries the route's switch, so the
 * one control that decides whether a section matters sits on the section
 * itself rather than three fields down inside it.
 */
export function railBody(s) {
  const routes = RAIL_SECTIONS.map(([id, title, key]) => {
    const on = !!s.settings[key];
    return collapsibleGroup(id, title, sectionBody(s, id), {
      open: !collapsedGroups.has(id),
      cls: `rail-section${on ? "" : " route-off"}`,
      // The explanation first, then the switch, so the icon that says what this
      // section IS sits beside the control that turns it on.
      headRightHTML: helpTooltip(SECTION_HELP[id], { label: title }) +
        routeSwitch(s, id, key, on),
    });
  }).join("");

  // Two switch-less panels follow the routes. They take the parallel
  // "rail-panel" class rather than "rail-section": that class marks a detection
  // ROUTE, and the render harness counts it to assert how many routes the rail
  // has, so a utility panel wearing it would be counted as a route.
  const quality = collapsibleGroup("rail-quality", RAIL.qualityTitle, qualitySection(s), {
    open: !collapsedGroups.has("rail-quality"),
    cls: "rail-panel",
    // The live value on the header, so the folded panel still states what the
    // floor is set to: a shut panel hiding a setting that changes what a run
    // replaces is a setting nobody knows is there.
    countLabel: `${Math.round((s.settings.minConfidence ?? 0) * 100)}%`,
    headRightHTML: helpTooltip(RAIL.qualityHelp, { label: RAIL.qualityTitle }),
  });

  return routes + quality +
    collapsibleGroup("rail-profile", RAIL.profileTitle, profileSection(s), {
      open: !collapsedGroups.has("rail-profile"),
      cls: "rail-panel",
    });
}

/**
 * profileSection(s) is the Load/Save profile body. Load restores a saved setup;
 * Save writes one, but only once Go actually holds a placeholder registry
 * (s.replacedValues, the same mirror of App.ValuePlaceholders the Anonymise
 * step reads), because a profile without a registry behind it saves an empty
 * key. Detection completing is NOT that fact: detection mints no placeholders,
 * only a run does, so gating on a "detection ran" latch left Save enabled
 * right after a detection and, worse, after stepping back from Anonymise
 * (which discards the registry but left the old latch on). The disabled Save
 * says why in its tooltip rather than vanishing, so the control that will
 * become available is visible before it does.
 */
function profileSection(s) {
  const canSave = (s.replacedValues?.length ?? 0) > 0;
  return `<div class="rail-block">` +
    `<div class="rail-label-row">` +
    `<span class="rail-field-label">${escapeHTML(RAIL.profileTitle)}</span>` +
    helpTooltip(RAIL.profileHelp, { label: RAIL.profileTitle }) +
    `</div>` +
    `<div class="button-pair">` +
    button(RAIL.profileLoad, { kind: "secondary", id: "profile-load" }) +
    button(RAIL.profileSave, {
      kind: "secondary",
      id: "profile-save",
      disabled: !canSave,
      title: canSave ? "" : RAIL.profileSaveDisabled,
    }) +
    `</div></div>`;
}

/**
 * routeSwitch(s, id, key, on) is the on/off control in a section header.
 */
function routeSwitch(s, id, key, on) {
  const title = key === "useLocalLLM" && !s.ollama?.available
    ? llmDisabledTooltip(s.settings.ollamaPort) : "";
  return `<label class="route-switch"${title ? ` title="${escapeHTML(title)}"` : ""}>` +
    `<input type="checkbox" class="route-toggle" data-route="${escapeHTML(id)}"` +
    `${on ? " checked" : ""}` +
    ` aria-label="${escapeHTML(RAIL.routeSwitchLabel(RAIL_SECTIONS.find(([sid]) => sid === id)?.[1] ?? id))}"/>` +
    `<span class="route-state">${escapeHTML(on ? RAIL.routeOn : RAIL.routeOff)}</span>` +
    `</label>`;
}

/** sectionBody(s, id) is the content of one route section. One case per entry
 *  in RAIL_SECTIONS: a section with no body would render as a switch with
 *  nothing under it. */
function sectionBody(s, id) {
  switch (id) {
    case "rail-local": return localLLMSection(s);
    case "rail-heuristic": return heuristicSection(s);
    default: return patternsSection(s);
  }
}

/** wireSectionSwitches(container) wires the header switches. The click must
 *  NOT reach the header, which is itself the fold toggle: switching a route on
 *  and having the section fold shut under your cursor is nobody's intent. */
function wireSectionSwitches(container) {
  for (const box of container.querySelectorAll(".route-toggle")) {
    box.addEventListener("click", (ev) => ev.stopPropagation());
    box.addEventListener("change", (ev) => {
      ev.stopPropagation();
      const on = ev.target.checked;
      // One switch, one flag: each section header writes ITS OWN settings key and
      // touches nothing else. A wiring test asserts that, because "this switch
      // changes only this" is exactly the property a reader of the code cannot
      // verify by reading it.
      const route = ev.target.dataset.route;
      if (route === "rail-local") setUseLocalLLM(on);
      else if (route === "rail-patterns") setUseBuiltInPatterns(on);
      else if (route === "rail-heuristic") setUseHeuristicDiscovery(on);
      // Turning a route on opens its section: the settings it reads are the
      // next thing the user wants.
      if (on) collapsedGroups.delete(ev.target.dataset.route);
      pushSettings(container);
    });
  }
  for (const head of container.querySelectorAll(".route-switch")) {
    head.addEventListener("click", (ev) => ev.stopPropagation());
  }
}

// --- Built-in patterns: the scope of the pattern pass ---------------------

/**
 * patternsSection(s) is what built-in pattern matching looks for: the document
 * country, the preset, and the eight category groups.
 *
 * There is no "Categories" label row over the groups. Under a section already
 * titled "Built-in patterns" that label says nothing, and the panel's height is
 * its scarcest resource; the explanation it carried is on the section header's
 * own help tooltip instead.
 */
function patternsSection(s) {
  return countryBlock(s) + presetBlock(s) +
    categoryGroups(s, PATTERN_GROUPS, "pattern", !s.settings.useBuiltInPatterns);
}

/**
 * heuristicSection(s) is what heuristic discovery looks for and how strict it is
 * about it: the name categories, then the strictness block as its own folded
 * subgroup, because that is the part a user changes when the suggestions
 * themselves are wrong rather than when the scope is.
 *
 * The name categories live HERE, under the route that discovers them offline.
 * Local LLM discovery reads the same one selection (engine.CategorySelection is
 * one setting) and says so rather than rendering a second copy of the boxes.
 */
function heuristicSection(s) {
  return `<div class="rail-block">` +
    labelWithHelp(RAIL.valuesAuto, RAIL.valuesAutoHelp) +
    categoryGroups(s, NAME_GROUPS, "name") +
    `</div>` +
    collapsibleGroup("rail-smart-tuning", RAIL.smartTuning, smartTuning(s), {
      open: !collapsedGroups.has("rail-smart-tuning"),
      cls: "rail-subgroup",
      headRightHTML: helpTooltip(RAIL.smartTuningHelp, { label: RAIL.smartTuning }),
    });
}

/**
 * qualitySection(s) is the match-confidence floor, and nothing else.
 *
 * The floor is the one genuinely cross-route control: it governs pass-1 pattern
 * spans, declared Values, custom patterns and every discovery method's output
 * alike, and it decides what a run is allowed to REPLACE rather than what
 * discovery is allowed to suggest. Placing a control that governs three routes
 * inside one of them would mislabel it as that route's own knob, so it gets a
 * switch-less panel of its own.
 *
 * It is NOT the heuristic block's own minimum confidence, which is
 * settings.heuristicDiscovery and lives inside Heuristic discovery because
 * nothing else reads it.
 */
function qualitySection(s) {
  return `<div class="rail-block">` +
    labelWithHelp(CONFIGURE.confidenceTitle, CONFIGURE.confidenceHelp) +
    confidenceControl(s) +
    `</div>`;
}

/**
 * signalCategoryRow(s, source, headHTML, tailHTML) is the category row of a
 * signal that can also DERIVE Suggestions, with its readings folded into it.
 *
 * A signal source identifier IS a built-in pattern category key (engine
 * SignalSourceEmail is the "email" category), which is what lets the readings hang
 * off the row of the pattern that produces the evidence. The row keeps its own
 * checkbox untouched: that one decides whether the signal is matched and replaced,
 * the drill-down decides what the match may additionally be read as, and
 * conflating the two is the mistake the separate setting exists to prevent.
 *
 * The shape follows the question. In front of a signal a user does not ask "may
 * this derive Suggestions" but "what would this derive, and do I want each of
 * those?": an address's local part is evidence for a person and its domain is
 * evidence for an organisation, through two separate mechanisms, and wanting one
 * without the other is a reasonable thing to want. So each reading is its own
 * switch and the panel's checkbox is a MASTER over them, derived for display (on
 * when any reading is on) and never stored, for the same reason a route section
 * switch is a real flag: a summary that can disagree with what it summarises
 * lies about what a run does.
 *
 * Only sources and readings that ACTUALLY implement discovery appear
 * (SIGNAL_SOURCES and SIGNAL_DERIVATIONS mirror the engine's, guarded by
 * ../detection_parity_test.go): a row with nothing behind it is a control that
 * appears to do something and does not.
 *
 * Clearing a reading stops the Suggestions THAT reading produces and leaves the
 * signal's own anonymisation alone. That distinction is the whole reason the
 * control exists, and it is in the help tooltip beside the button rather than in a
 * paragraph.
 *
 * @param {object} s the store snapshot
 * @param {string} source one of SIGNAL_SOURCES, and the category key of the row
 * @param {string} headHTML the category row's own markup (checkbox and label)
 * @param {string} tailHTML the row's trailing example
 */
function signalCategoryRow(s, source, headHTML, tailHTML) {
  return signalDrillDown({
    source,
    label: RAIL.signalSuggestions,
    headHTML,
    tailHTML,
    // The bubble's id is scoped by SOURCE. helpTooltip derives an id from the
    // text, and every signal row carries the same explanation, so two sources
    // would render two bubbles with one id and aria-describedby would point at
    // whichever the browser saw first.
    helpHTML: helpTooltip(RAIL.signalSuggestionsHelp, {
      id: `help-signal-${source}`,
      label: RAIL.signalSuggestions,
    }),
    title: RAIL.signalDerivedFrom(RAIL.signalSourceLabel[source] ?? source),
    summary: RAIL.signalDerivationCount(enabledSignalDerivations(s, source).length),
    checked: signalSourceOn(s, source),
    open: openSignalSources.has(source),
    rows: (SIGNAL_DERIVATIONS[source] ?? []).map((d) => {
      const rowLabel = RAIL.signalDerivationLabel[d] ?? d;
      return {
        id: d,
        label: rowLabel,
        detail: RAIL.signalDerivationFinds[d] ?? "",
        helpHTML: helpTooltip(RAIL.signalDerivationHelp[d], { label: rowLabel }),
        checked: signalDerivationOn(s, source, d),
      };
    }),
  });
}

/**
 * labelWithHelp(text, help) keeps every block's heading to a SHORT visible label
 * with its explanation one hover or one Tab away. A paragraph under each of these
 * is what made the panel taller than the window.
 */
function labelWithHelp(text, help) {
  return `<div class="rail-label-row">` + sectionLabel(text) +
    helpTooltip(help, { label: text }) + `</div>`;
}

/** countryBlock(s) is the document country: the broadest choice, so it leads. */
function countryBlock(s) {
  return `<div class="rail-block">` +
    labelWithHelp(RAIL.country, RAIL.countryHelp) +
    countrySelect(s) +
    `</div>`;
}

/**
 * presetBlock(s) is the preset chips and the read-out under them.
 *
 * A preset fills BOTH the pattern categories here and the name categories under
 * Heuristic discovery (CLAUDE.md §5, the anonymisation levels), so a chip pressed
 * in this section reaches across into another one. That is a domain rule rather
 * than a UI one, so the read-out makes it VISIBLE instead of the chip changing a
 * selection the user cannot see from here.
 */
function presetBlock(s) {
  const namesOn = NAME_CATEGORIES.filter((c) => s.settings.categories?.[c]).length;
  return `<div class="rail-block">` +
    labelWithHelp(RAIL.preset, CONFIGURE.presetHelp) +
    presetChips(s) +
    // .rail-readout, not .hint: the count changes with the chip, so it is dynamic
    // information rather than the static prose the panel does not carry.
    `<p class="rail-readout" id="preset-also-sets">` +
    `${escapeHTML(RAIL.presetAlsoSets(namesOn))}</p>` +
    `</div>`;
}

function countrySelect(s) {
  const options = countryOptions().map((c) =>
    `<option value="${escapeHTML(c.code)}"${c.code === s.documentCountry ? " selected" : ""}>` +
    `${escapeHTML(c.name)}</option>`).join("");
  return `<select id="document-country" class="rail-select"` +
    ` aria-label="${escapeHTML(RAIL.country)}">${options}</select>`;
}

function presetChips(s) {
  const current = selectionPresetName(s.settings.categories);
  const chips = PRESETS.map(([id, label]) => ({ id, label, active: current === id }));
  // "Custom" is a READ-OUT, not a choice: there is no preset to apply, it is
  // what the selection reads as once it matches none of the three. Rendering it
  // as a chip that does nothing when pressed would be a lie, so it is disabled
  // and carries the explanation as its tooltip.
  chips.push({
    id: "custom", label: "Custom", active: current === "custom",
    disabled: true, title: CONFIGURE.presetHelp,
  });
  return chipRow(chips, { attr: "preset", ariaLabel: RAIL.preset });
}

/**
 * categoryGroups(s, groups, type) renders collapsible groups, each with its n/m
 * count and its select-all / deselect-all pair.
 *
 * The examples beside three of the labels come from the country,
 * overlaid on copy.js CATEGORY_LABELS at render time rather than stored five
 * times over.
 *
 * type is "pattern" (built-in pattern categories) or "name" (the name
 * categories a discovery method can emit). It is the group id's prefix and the
 * key GROUPS_BY_TYPE resolves the bulk buttons through, so nothing addresses a
 * group by its position in CATEGORY_GROUPS.
 *
 * blockDisabled greys the whole block out without clearing the stored selection:
 * when Built-in patterns is off, its categories still show (so the user sees the
 * scope) but cannot be edited, and the selection returns intact when the section
 * is switched back on.
 */
function categoryGroups(s, groups, type = "pattern", blockDisabled = false) {
  const labels = categoryLabels(examplesFor(s.documentCountry));

  return groups.map(([title, keys], index) => {
    const on = keys.filter((k) => s.settings.categories?.[k]).length;
    const rows = keys.map((key) => {
      const [label, example] = labels[key] ?? [key, ""];
      const applies = categoryAppliesTo(key, s.documentCountry);
      const disabled = blockDisabled || !applies;
      const countries = CATEGORY_COUNTRIES[key] ?? [];
      const disabledHint = `Only applies to ${countries.join(", ")}`;
      const hint = applies ? example : `${example}. ${disabledHint}`;
      const box =
        `<input type="checkbox" class="cat-toggle" data-category="${escapeHTML(key)}"` +
        `${s.settings.categories?.[key] ? " checked" : ""}${disabled ? " disabled" : ""}/>` +
        `<span class="cat-label">${escapeHTML(label)}</span>`;
      const exampleHTML =
        `<span class="cat-example" title="${escapeHTML(hint)}">${escapeHTML(example)}</span>`;
      const title = applies ? "" : ` title="${escapeHTML(disabledHint)}"`;

      // A signal that can also DERIVE Suggestions carries its readings on its own
      // row: the drill-down button, then the help icon that explains it, then the
      // example. The category checkbox in front of them is untouched, because it
      // answers a different question (is this signal replaced at all).
      //
      // The drill-down is deliberately NOT gated on blockDisabled, and this
      // asymmetry is the whole reason the separate setting exists. Signal-based
      // discovery is gated ONLY by signalSuggestionSources: it matches its own
      // evidence, so it keeps producing Suggestions with Built-in patterns off,
      // which governs only whether the signal ITSELF is replaced. Switching one
      // off must never silently take the other with it. A wiring test holds it.
      if (SIGNAL_SOURCES.includes(key)) {
        return signalCategoryRow(s, key,
          `<label class="cat-row"${title}>${box}</label>`, exampleHTML);
      }
      return `<label class="cat-row"${title}>${box}${exampleHTML}</label>`;
    }).join("");

    // Icon-only bulk switches: the group header is narrow, and "Select all"
    // plus "Deselect all" across six groups would be twelve words of chrome.
    // Data attributes encode both type and index so wireScope can look up the right group.
    const bulk =
      button("", {
        kind: "ghost", cls: "cat-group-all icon-action ok", icon: "check",
        ariaLabel: `${CONFIGURE.selectAll}: ${title}`, title: CONFIGURE.selectAll,
        data: { "group-type": type, group: String(index), on: "1" },
        disabled: blockDisabled,
      }) +
      button("", {
        kind: "ghost", cls: "cat-group-all icon-action", icon: "remove",
        ariaLabel: `${CONFIGURE.deselectAll}: ${title}`, title: CONFIGURE.deselectAll,
        data: { "group-type": type, group: String(index), on: "0" },
        disabled: blockDisabled,
      });

    return collapsibleGroup(`cat-group-${type}-${index}`, title, rows, {
      open: !collapsedGroups.has(`cat-group-${type}-${index}`),
      countLabel: `${on}/${keys.length}`,
      headRightHTML: bulk,
    });
  }).join("");
}

/**
 * confidenceControl(s) renders the match-confidence floor, inside the switch-less
 * Detection quality panel.
 *
 * It is deliberately NOT gated on any route: every detection carries a score
 * whether or not Ollama is running, and the floor applies to all of them. A
 * slider in whole percent rather than a number field, because the meaningful
 * settings are ranges; the live read-out names what the current position
 * actually excludes.
 */
function confidenceControl(s) {
  const percent = Math.round((s.settings.minConfidence ?? 0) * 100);
  // The value and what it excludes stay INLINE: they change as the slider moves,
  // and dynamic information the user is watching cannot live behind a hover.
  return `<div class="rail-slider">` +
    `<input id="min-confidence" type="range" min="0" max="100" step="5" value="${percent}"` +
    ` aria-label="${escapeHTML(CONFIGURE.confidenceLabel)}"/>` +
    `<output id="min-confidence-value" for="min-confidence">${percent}</output>` +
    `</div>` +
    // .rail-readout, not .hint: a hint is static prose, which the panel does not
    // carry any more, while this sentence changes as the slider moves. The two
    // need different classes so the guard against reintroducing prose can be
    // structural rather than a list of retired sentences.
    `<p class="rail-readout" id="min-confidence-effect">${escapeHTML(confidenceEffect(percent))}</p>`;
}

/**
 * confidenceEffect(percent) puts the current slider position into words, so the
 * user reads what the setting DOES rather than a bare number.
 *
 *  rewrote this copy. The mock-up's version described a
 * source-tiered rule ("values that only the local model suggested are skipped"),
 * which the engine does not implement: what the setting actually is is a FLOOR
 * on the confidence score every detection carries. The thresholds below mirror
 * where the engine's own scores sit (engine/pii.go: local-model proposals score
 * 0.8, values the user listed 0.95, pattern matches 1.0), and the sentences name
 * the effect of the floor rather than inventing a rule about sources.
 *
 * @param {number} percent slider position, 0 to 100
 * @returns {string} a plain-language sentence
 */
export function confidenceEffect(percent) {
  if (percent <= 0) return "Nothing is skipped: every detection is replaced.";
  if (percent <= 80) {
    return "Nothing is skipped at this setting: every detection the application makes " +
      "scores at least 80, so this floor is not yet reached.";
  }
  if (percent <= 95) {
    return "Detections scoring below this floor are left alone. In practice that is the " +
      "weaker ones, proposed rather than matched outright.";
  }
  return "Only the strongest detections are replaced, which in practice means the " +
    "pattern matches. Everything scored lower is left alone.";
}

function wireScope(container) {
  container.querySelector("#document-country")?.addEventListener("change", (ev) => {
    setDocumentCountry(ev.target.value);
    pushSettings(container);
  });

  for (const chip of container.querySelectorAll("[data-preset]")) {
    if (chip.disabled) continue; // the Custom read-out
    chip.addEventListener("click", () => {
      applyPreset(chip.dataset.preset);
      pushSettings(container);
    });
  }

  for (const box of container.querySelectorAll(".cat-toggle")) {
    // The full re-render this triggers keeps the rail where the user left it:
    // scroll is preserved centrally by the shell repaint (scroll.js), so ticking
    // a box in a long list no longer jumps back to the top.
    box.addEventListener("change", () => {
      toggleCategory(box.dataset.category, box.checked);
      pushSettings(container);
    });
  }

  for (const btn of container.querySelectorAll(".cat-group-all")) {
    btn.addEventListener("click", (ev) => {
      // The button lives in the group header, which is itself a toggle; stop
      // the click before wireGroups reads it as a request to fold the group.
      ev.stopPropagation();
      const type = btn.dataset.groupType || "pattern";
      const group = (GROUPS_BY_TYPE[type] ?? [])[Number(btn.dataset.group)];
      if (!group) return;
      // ONE reducer call flips the whole group, so there is exactly one repaint
      // rather than one per category.
      setCategoryGroup(group[1], btn.dataset.on === "1");
      pushSettings(container);
    });
  }

  // "input" updates the read-out live while dragging without touching the
  // store; "change" (on release) commits it, so a drag does not fire one bridge
  // round-trip per pixel.
  const confidence = container.querySelector("#min-confidence");
  if (confidence) {
    const readout = container.querySelector("#min-confidence-value");
    const effect = container.querySelector("#min-confidence-effect");
    confidence.addEventListener("input", () => {
      const percent = Number(confidence.value);
      if (readout) readout.textContent = String(percent);
      if (effect) effect.textContent = confidenceEffect(percent);
    });
    confidence.addEventListener("change", () => {
      setMinConfidence(Number(confidence.value) / 100);
      return pushSettings(container);
    });
  }
}

// --- Heuristic discovery: its own strictness -----------------------------

/** smartTuning(s) is heuristic discovery's strictness: the four fields a user
 *  changes when the suggestions themselves are wrong, rather than when the scope
 *  is. Its minimum confidence is this route's own, read by
 *  engine.HeuristicDiscoverContext and by nothing else; the cross-route floor is
 *  the Detection quality panel's slider. */
function smartTuning(s) {
  const opts = heuristicDiscoveryOptions(s);
  const fieldRow = (id, label, help, controlHTML) =>
    `<div class="rail-field-row">` +
    `<label class="rail-field" for="${id}">` +
    `<span class="rail-field-label">${escapeHTML(label)}</span>` +
    controlHTML +
    `</label>` +
    helpTooltip(help, { label }) +
    `</div>`;
  const numberRow = (id, label, help, value, attrs) =>
    fieldRow(id, label, help,
      `<input id="${id}" type="number" ${attrs} value="${escapeHTML(String(value))}"/>`);

  return `<div class="rail-block">` +
    fieldRow("smart-strictness", VALUES.smartStrictness, VALUES.smartStrictnessHelp,
      `<select id="smart-strictness">` +
      strictnessOption("lenient", VALUES.smartStrictnessLenient, opts.strictness) +
      strictnessOption("balanced", VALUES.smartStrictnessBalanced, opts.strictness) +
      strictnessOption("strict", VALUES.smartStrictnessStrict, opts.strictness) +
      `</select>`) +
    numberRow("smart-min-length", VALUES.smartMinLength, VALUES.smartMinLengthHelp,
      opts.minLength, `min="0" max="40" step="1"`) +
    numberRow("smart-min-occurrences", VALUES.smartMinOccurrences, VALUES.smartMinOccurrencesHelp,
      opts.minOccurrences, `min="0" max="100" step="1"`) +
    numberRow("smart-min-confidence", VALUES.smartMinConfidence, VALUES.smartMinConfidenceHelp,
      opts.minConfidence, `min="0" max="1" step="0.05"`) +
    `<div class="rail-toggle">` +
    `<label class="cat-row">` +
    `<input type="checkbox" id="smart-common-words"${opts.excludeCommonWords ? " checked" : ""}/>` +
    `<span class="cat-label">${escapeHTML(VALUES.smartCommonWords)}</span>` +
    `</label>` +
    helpTooltip(VALUES.smartCommonWordsHelp, { label: VALUES.smartCommonWords }) +
    `</div>` +
    `</div>`;
}

/**
 * wireSignalSources(container) wires each signal category row's drill-down: the
 * button that opens the readings, the master checkbox in the opened panel, each
 * reading's own checkbox, and Escape to fold whatever is open.
 *
 * Escape folds them for the same reason the help tooltip closes on Escape: a
 * control that can only be dismissed by clicking elsewhere is a keyboard trap.
 */
function wireSignalSources(container) {
  const signalRows = container.querySelectorAll(".signal-row");
  if (signalRows.length === 0) return;

  for (const row of signalRows) {
    const source = row.dataset.signalSource;
    if (!source) continue;

    row.querySelector(".signal-drill")?.addEventListener("click", (ev) => {
      // The button sits inside a category list row; stopPropagation keeps the
      // click off the row and off the group header above it, so opening the
      // readings neither ticks the category nor folds the group.
      ev.stopPropagation();
      if (openSignalSources.has(source)) openSignalSources.delete(source);
      else openSignalSources.add(source);
      setState({}); // repaint; the expanded state is view state
    });

    // The master writes every reading of this signal in one action, which is what
    // saves the user N clicks to switch a whole signal off.
    row.querySelector(".signal-master")?.addEventListener("click", (ev) => {
      ev.stopPropagation();
    });
    row.querySelector(".signal-master")?.addEventListener("change", (ev) => {
      setSignalSource(source, ev.target.checked);
      pushSettings(container);
    });

    for (const box of row.querySelectorAll(".signal-box")) {
      box.addEventListener("click", (ev) => { ev.stopPropagation(); });
      box.addEventListener("change", (ev) => {
        setSignalDerivation(source, box.dataset.derivation, ev.target.checked);
        // Pushed to Go like every other setting: which readings may derive
        // Suggestions is what the next detection run reads.
        pushSettings(container);
      });
    }

    row.addEventListener("keydown", (ev) => {
      if (ev.key !== "Escape" || !openSignalSources.has(source)) return;
      ev.stopPropagation();
      openSignalSources.delete(source);
      setState({});
    });
  }
}

/** strictnessOption renders one <option>, marked selected when it is current. */
function strictnessOption(value, label, current) {
  const selected = (current ?? "balanced") === value ? " selected" : "";
  return `<option value="${value}"${selected}>${escapeHTML(label)}</option>`;
}

function wireSmart(container) {
  const numbers = [
    ["#smart-min-length", (v) => ({ minLength: Math.round(v) })],
    ["#smart-min-occurrences", (v) => ({ minOccurrences: Math.round(v) })],
    ["#smart-min-confidence", (v) => ({ minConfidence: v })],
  ];
  for (const [selector, toPatch] of numbers) {
    container.querySelector(selector)?.addEventListener("change", (ev) => {
      const value = Number(ev.target.value);
      if (Number.isNaN(value)) return;
      // setHeuristicDiscoveryOptions validates and IGNORES a bad value rather than
      // storing it, so a typo shows as the field snapping back rather than as
      // heuristic discovery quietly finding nothing.
      setHeuristicDiscoveryOptions(toPatch(value));
    });
  }
  container.querySelector("#smart-strictness")?.addEventListener("change", (ev) => {
    setHeuristicDiscoveryOptions({ strictness: ev.target.value });
  });
  container.querySelector("#smart-common-words")?.addEventListener("change", (ev) => {
    setHeuristicDiscoveryOptions({ excludeCommonWords: ev.target.checked });
  });
}

// --- Local LLM discovery --------------------------------------------------

/**
 * scopeBlock(s, gated) is the "What to scan" control for Local LLM discovery: a
 * document picker plus, for a multi-unit document, a choice between scanning the
 * whole document or a set of its own pages/slides/rows/lines. It exists because
 * handing a whole document to a small local model is too much (the user's
 * words); aiming the scan keeps the pass quick and the model focused.
 *
 * The picker defaults to "All documents (whole)", the unchanged behaviour. When
 * one document with more than one addressable unit is chosen, an "Entire
 * document" / "Specific pages" control appears; choosing "Specific pages"
 * reveals a free-text field that accepts a single page, a range, or a
 * discontiguous mix ("14", "12-15", "12,13,18-20"). A live read-out names how
 * many units the current spec resolves to, and a malformed token is shown
 * inline, because a wide or mistyped scan is exactly the "too much" the feature
 * exists to avoid.
 */
function scopeBlock(s, gated) {
  const scope = s.llmScope ?? { docName: "", mode: "all", pages: "" };
  const docs = s.documents ?? [];
  const options = [
    `<option value=""${scope.docName ? "" : " selected"}>` +
      `${escapeHTML(RAIL.scopeAllDocs)}</option>`,
    ...docs.map((d) => {
      const count = Math.max(1, d.pageCount || 1);
      const label = RAIL.scopeDocOption(d.name, count, d.unit);
      return `<option value="${escapeHTML(d.name)}"` +
        `${scope.docName === d.name ? " selected" : ""}>${escapeHTML(label)}</option>`;
    }),
  ].join("");

  let range = "";
  const selected = docs.find((d) => d.name === scope.docName);
  const count = Math.max(1, selected?.pageCount || 1);
  if (selected && count > 1) {
    const unit = RAIL.scopeUnitWord(selected.unit);
    const pagesMode = scope.mode === "pages";
    // The radio pair: entire document (default) versus a specific page set.
    const modeControl =
      `<div class="rail-modes" role="radiogroup">` +
      `<label class="rail-radio">` +
      `<input class="ai-scope-mode" type="radio" name="ai-scope-mode" value="all"` +
        `${pagesMode ? "" : " checked"}${gated}/>` +
      `<span>${escapeHTML(RAIL.scopeEntireDoc)}</span></label>` +
      `<label class="rail-radio">` +
      `<input class="ai-scope-mode" type="radio" name="ai-scope-mode" value="pages"` +
        `${pagesMode ? " checked" : ""}${gated}/>` +
      `<span>${escapeHTML(RAIL.scopeSpecificPages)}</span></label>` +
      `</div>`;

    let pagesField = "";
    if (pagesMode) {
      const parsed = parsePageSpec(scope.pages, count);
      const readout = parsed.error
        ? `<p class="hint warn" id="ai-pages-error">${escapeHTML(RAIL.scopePagesError(parsed.error))}</p>`
        : `<p class="hint" id="ai-pages-readout">${escapeHTML(RAIL.scopeReadout(parsed.pages.length, unit))}</p>`;
      pagesField =
        `<label class="rail-field" for="ai-pages">` +
        `<span class="rail-field-label">${escapeHTML(RAIL.scopePagesLabel(unit))}</span>` +
        `<input id="ai-pages" type="text" ` +
          `placeholder="${escapeHTML(RAIL.scopePagesPlaceholder)}" ` +
          `value="${escapeHTML(scope.pages || "")}"${gated}/>` +
        `</label>` + readout;
    }
    range = modeControl + pagesField;
  }

  return `<div class="rail-block">` +
    `<div class="rail-label-row">` +
    `<span class="rail-field-label">${escapeHTML(RAIL.scopeHeading)}</span>` +
    helpTooltip(RAIL.scopeHelp, { label: RAIL.scopeHeading }) +
    `</div>` +
    `<label class="rail-field" for="ai-scope-doc">` +
    `<span class="rail-field-label">${escapeHTML(RAIL.scopeDoc)}</span>` +
    `<select id="ai-scope-doc"${gated}>${options}</select>` +
    `</label>` + range +
    `</div>`;
}

/**
 * lastScanReadout(s) reports what the local model actually did on the last run:
 * how many requests it sent, how long each took on THIS machine, how many came
 * back with nothing, and how many ran out of room before they finished
 * answering.
 *
 * It is a `.rail-readout` and not a hint, because it is a measured fact that
 * changes with every run rather than static prose. The seconds are the half no
 * tooltip could supply: how a scan feels depends on the model, the machine and
 * the document, and this is the only place the user sees all three combined.
 * Empty before the first local model run, because a read-out with nothing to
 * report is a line that only ever teaches the reader to ignore it.
 */
function lastScanReadout(s) {
  const scan = s.lastLLMScan;
  if (!scan || !(scan.requests > 0)) return "";
  return `<p class="rail-readout" id="last-ai-scan">` +
    `${escapeHTML(RAIL.lastScan(scan.requests, scan.secondsPerRequest, scan.silent, scan.truncated))}</p>`;
}

/**
 * modelOptions(s) is the model dropdown's options, one per model the probe saw.
 *
 * Exactly ONE option is marked selected whenever there are any: the stored model
 * when it is among them, otherwise the first. Leaving nothing marked lets the
 * browser select the first option by itself while the store still holds
 * something else, so the control shows one model, the store names another, and
 * the next settings write sends whichever the server happened to list first.
 * Which model a fresh session runs on is then decided by Ollama's tag ordering.
 *
 * The fallback here and Go's model resolution agree by construction: Go resolves
 * to an installed model and the store adopts it (state.js adoptProbe), so the
 * stored name is normally present and this fallback is the belt to that braces.
 */
function modelOptions(s) {
  const models = s.ollama?.models ?? [];
  if (models.length === 0) return "";
  const current = models.includes(s.settings.model) ? s.settings.model : models[0];
  return models.map((m) =>
    `<option value="${escapeHTML(m)}"${m === current ? " selected" : ""}>` +
    `${escapeHTML(m)}</option>`).join("");
}

/**
 * detailLevelOptions(s) is the dropdown's two options, built from the mirrored
 * LLM_DETAIL_LEVELS so the rail cannot offer a level Go would refuse.
 *
 * Exactly one option is marked selected, always: an unset or unrecognised stored
 * level falls back to the first, which is thorough. Leaving nothing marked lets
 * the browser pick the first by itself, and a control whose value nobody wrote
 * is a choice made by option ordering.
 */
function detailLevelOptions(s) {
  const current = LLM_DETAIL_LEVELS.includes(s.settings.llmDetailLevel)
    ? s.settings.llmDetailLevel : LLM_DETAIL_LEVELS[0];
  return LLM_DETAIL_LEVELS.map((level) =>
    `<option value="${escapeHTML(level)}"${level === current ? " selected" : ""}>` +
    `${escapeHTML(RAIL.detailLevelOptions[level] ?? level)}</option>`).join("");
}

/**
 * scanEstimateReadout(s) is what the current scope and detail level would COST,
 * shown before the user pays it. Go computes it with the same helper the run
 * uses, so the number equals the number of requests the run then makes.
 *
 * A `<span class="hint">` inside a `.rail-status` row, exactly as the Ollama
 * availability line is: the rail bans explanatory paragraphs, and this is a live
 * fact rather than prose. Empty until Go has answered, because a read-out
 * guessing while it waits is a number the run can contradict.
 */
function scanEstimateReadout(s) {
  const requests = s.llmRequestEstimate;
  if (!(requests > 0)) return "";
  return `<div class="rail-status">` +
    `<span class="hint" id="ai-request-estimate">${escapeHTML(RAIL.scanEstimate(requests))}</span>` +
    `</div>`;
}

function localLLMSection(s) {
  const ollamaOK = !!s.ollama?.available;
  const aiOn = !!s.settings.useLocalLLM;
  // The model and the context size are gated, the PORT and Re-probe are not:
  // those two are how a user CONNECTS, so gating them would lock someone out of
  // fixing the very connection the gate is complaining about.
  const gated = aiOn && ollamaOK ? "" : ` disabled title="${escapeHTML(llmGateTooltip(s))}"`;
  const models = modelOptions(s);

  // The route's own switch lives in the section header now, so the body opens
  // with what the switch cannot say: whether Ollama is actually there.
  // The body opens with what the header switch cannot say: whether Ollama is
  // actually there. That IS dynamic, so it stays inline.
  return `<div class="rail-block">` +
    `<div class="rail-label-row">` +
    `<span class="rail-field-label">${escapeHTML(RAIL.tabLocalLLM)}</span>` +
    helpTooltip(CONFIGURE.useLLMHelp, { label: RAIL.tabLocalLLM }) +
    `</div>` +
    `<div class="rail-status">` +
    `<span class="state-tag${ollamaOK ? "" : " bad"}" title="${escapeHTML(s.ollama?.detail ?? "")}">` +
    `${escapeHTML(ollamaOK ? RAIL.ollamaDetected : RAIL.ollamaMissing)}</span>` +
    `<span class="hint">${escapeHTML(RAIL.hostLocked)}</span>` +
    `</div>` +
    `<label class="rail-field" for="ollama-port">` +
    `<span class="rail-field-label">${escapeHTML(RAIL.port)}</span>` +
    `<input id="ollama-port" type="number" min="1" max="65535" value="${escapeHTML(String(s.settings.ollamaPort))}"/>` +
    `</label>` +
    `<label class="rail-field" for="ollama-model">` +
    `<span class="rail-field-label">${escapeHTML(RAIL.model)}</span>` +
    `<select id="ollama-model"${gated}>${models || `<option value="">${escapeHTML(RAIL.noModels)}</option>`}</select>` +
    `</label>` +
    // The speed-versus-recall dial sits with the two other settings about how
    // much the model reads, and before Context, because it is the one that
    // decides the size of a request; Context only bounds it.
    `<div class="rail-field-row">` +
    `<label class="rail-field" for="ai-detail-level">` +
    `<span class="rail-field-label">${escapeHTML(RAIL.detailLevel)}</span>` +
    `<select id="ai-detail-level"${gated}>${detailLevelOptions(s)}</select>` +
    `</label>` +
    helpTooltip(CONFIGURE.detailLevelHelp, { label: RAIL.detailLevel }) +
    `</div>` +
    scanEstimateReadout(s) +
    `<div class="rail-field-row">` +
    `<label class="rail-field" for="context-size">` +
    `<span class="rail-field-label">${escapeHTML(RAIL.contextSize)}</span>` +
    `<input id="context-size" type="number" min="0" step="1024" value="${escapeHTML(String(s.settings.contextSize ?? 8192))}"${gated}/>` +
    `</label>` +
    helpTooltip(CONFIGURE.contextSizeHelp, { label: RAIL.contextSize }) +
    `</div>` +
    // The reply format, gated like the model and the context size are: it changes
    // what a request asks the model for, so it means nothing without one running.
    // Off by default, which is the fast end of a trade-off with no single winner.
    `<div class="rail-toggle">` +
    `<label class="cat-row" for="ai-strict-format">` +
    `<input type="checkbox" id="ai-strict-format"${s.settings.llmStrictFormat ? " checked" : ""}${gated}/>` +
    `<span class="cat-label">${escapeHTML(RAIL.strictFormat)}</span>` +
    `</label>` +
    helpTooltip(CONFIGURE.strictFormatHelp, { label: RAIL.strictFormat }) +
    `</div>` +
    lastScanReadout(s) +
    button(RAIL.reprobe, { kind: "secondary", id: "btn-reprobe", icon: "refresh" }) +
    `</div>` +
    scopeBlock(s, gated) +
    // NO second copy of the category checkboxes here. There is ONE category
    // selection in the engine (engine.CategorySelection), so a second set of
    // boxes would be two controls for one setting; worse, this section is folded
    // shut by default, so those boxes render at zero height and the user cannot
    // tick them at all (the rendering harness fails on exactly that). The route
    // says which list it reads instead.
    `<div class="rail-block">` +
    `<div class="rail-label-row">` +
    `<span class="rail-field-label">${escapeHTML(RAIL.valuesAuto)}</span>` +
    helpTooltip(RAIL.localValuesHelp, { label: RAIL.valuesAuto }) +
    `</div>` +
    `</div>`;
}

// The inputs the last estimate was computed for. The rail re-renders on every
// state change, so without this the read-out would ask Go the same question
// dozens of times, and answering it would setState and re-render again.
let lastEstimateKey = null;

/**
 * refreshLLMEstimate() asks Go what the current scope and detail level would
 * cost, and stores the answer for the read-out.
 *
 * It is fired from the rail's wiring rather than from a list of the changes that
 * could affect it: importing a document, changing the scope and changing the
 * level all move the number, and enumerating them is how one gets forgotten. The
 * key guard is what makes that safe, both against the render loop and against
 * asking Go the same question on every repaint.
 *
 * A failure is silence, not a banner. The estimate is a convenience beside a
 * control that works without it, and the run itself reports anything genuinely
 * wrong with the scope.
 */
async function refreshLLMEstimate() {
  const s = getState();
  const names = s.documents.map((d) => d.name);
  const scope = llmScopeArg(s);
  const key = JSON.stringify([names, scope, s.settings.llmDetailLevel]);
  if (key === lastEstimateKey) return;
  lastEstimateKey = key;
  if (names.length === 0) {
    if (getState().llmRequestEstimate !== 0) setState({ llmRequestEstimate: 0 });
    return;
  }
  try {
    const requests = await estimateLLMRequests(names, scope);
    if (getState().llmRequestEstimate !== requests) setState({ llmRequestEstimate: requests });
  } catch {
    // No bridge, or nothing to estimate: the read-out simply stays away.
    lastEstimateKey = null;
    if (getState().llmRequestEstimate !== 0) setState({ llmRequestEstimate: 0 });
  }
}

function wireLocalLLM(container) {
  refreshLLMEstimate();
  for (const id of ["#ollama-port", "#ollama-model", "#context-size", "#ai-strict-format",
    "#ai-detail-level"]) {
    container.querySelector(id)?.addEventListener("change", () => pushSettings(container));
  }
  container.querySelector("#btn-reprobe")?.addEventListener("click", async () => {
    await pushSettings(container);
    adoptProbe(await probeOllama());
  });
  // The scan-scope controls write straight to state (no Go round-trip: scope is
  // a per-run choice, not a saved setting). setLLMScope re-renders the rail, so
  // switching to a multi-unit document reveals the mode control, and choosing
  // "Specific pages" reveals the page field. Switching document resets the scope
  // to the whole document.
  container.querySelector("#ai-scope-doc")?.addEventListener("change", (ev) => {
    setLLMScope({ docName: ev.target.value, mode: "all", pages: "" });
  });
  for (const radio of container.querySelectorAll('input[name="ai-scope-mode"]')) {
    radio.addEventListener("change", (ev) => {
      if (ev.target.checked) setLLMScope({ mode: ev.target.value });
    });
  }
  const pages = container.querySelector("#ai-pages");
  if (pages) {
    // Live read-out: update the parsed-count (or the error) as the user types,
    // without a full repaint that would steal focus from the field. A repaint
    // still happens on "change" (blur) so the stored spec and the rest of the
    // rail stay in step.
    const doc = getState().documents.find((d) => d.name === getState().llmScope.docName);
    const max = doc ? Math.max(1, doc.pageCount || 1) : 0;
    const unit = RAIL.scopeUnitWord(doc?.unit);
    const refresh = () => {
      const parsed = parsePageSpec(pages.value, max);
      const readout = container.querySelector("#ai-pages-readout");
      const errEl = container.querySelector("#ai-pages-error");
      const text = parsed.error
        ? RAIL.scopePagesError(parsed.error)
        : RAIL.scopeReadout(parsed.pages.length, unit);
      const target = parsed.error ? errEl : readout;
      // Reuse whichever line is on screen; class flips so an error reads as one.
      const line = target || readout || errEl;
      if (line) {
        line.textContent = text;
        line.classList.toggle("warn", !!parsed.error);
      }
    };
    pages.addEventListener("input", refresh);
    pages.addEventListener("change", () => setLLMScope({ pages: pages.value }));
  }
}

// --- Load profile ---------------------------------------------------------

/**
 * wireProfile(container) wires the Load/Save profile buttons. Load restores a
 * saved session with applySession (export.js owns the load half of the pair),
 * then refreshes the registry mirror: a loaded session can carry its own
 * placeholder registry in Go, ready to re-save immediately, and the mirror
 * would otherwise still show whatever it held before the load. Save is
 * guarded on the same fact as the disabled attribute (profileSection), here
 * too, so a click that slips through a stale DOM cannot save an empty
 * registry.
 */
function wireProfile(container) {
  container.querySelector("#profile-load")?.addEventListener("click", async () => {
    try {
      const session = await loadSession();
      if (!session) return; // cancelled
      applySession(session);
      try {
        const [replaced, removed] = await Promise.all([valuePlaceholders(), listRemovedValues()]);
        setValueTables(replaced, removed);
      } catch { /* no bridge: the gate stays as it was */ }
      notify(RAIL.profileLoadDone, "ok");
    } catch (err) {
      // A refused session file (a version this build does not read) lands here
      // with Go's actionable message, and the user needs the whole of it.
      notify(String(err?.message ?? err), "warn");
    }
  });
  container.querySelector("#profile-save")?.addEventListener("click", async () => {
    if ((getState().replacedValues?.length ?? 0) === 0) return; // guard: matches the disabled attribute
    try {
      await saveSession(buildRunRequest());
      notify(RAIL.profileSaveDone, "ok");
    } catch (err) {
      notify(String(err?.message ?? err), "warn");
    }
  });
}

// --- Shared: push settings to Go -----------------------------------------

/**
 * settingsPayload(s, container) is the ONE definition of what a settings write
 * sends to Go: the store plus whatever the Local LLM discovery section's inputs
 * say when that section is on screen. Pure, so a test can read the payload a given state and DOM
 * produce without a bridge to answer the call.
 *
 * A tab that is not rendered contributes nothing and its value comes from the
 * store, so switching tabs never resets a setting.
 */
export function settingsPayload(s, container) {
  const port = container.querySelector("#ollama-port");
  const model = container.querySelector("#ollama-model");
  const ctxSize = container.querySelector("#context-size");
  const strictFormat = container.querySelector("#ai-strict-format");
  const detailLevel = container.querySelector("#ai-detail-level");
  return {
    level: s.settings.level,
    categories: s.settings.categories,
    country: s.settings.country ?? s.documentCountry,
    // A tab that is not on screen contributes nothing: read the store instead of
    // a missing element, so switching tabs never resets a setting.
    ollamaPort: port ? (parseInt(port.value, 10) || 0) : s.settings.ollamaPort,
    model: model?.value || s.settings.model,
    contextSize: ctxSize ? (parseInt(ctxSize.value, 10) || 0) : (s.settings.contextSize ?? 8192),
    useLocalLLM: !!s.settings.useLocalLLM,
    // The reply format the local model's discovery call asks for. Sent EXPLICITLY as
    // a boolean, never left out: Go reads an absent value as off, so an omitted
    // key and a cleared checkbox would be the same thing on the wire and there
    // would be no way to say "on".
    llmStrictFormat: strictFormat ? strictFormat.checked : !!s.settings.llmStrictFormat,
    // How much text one local model request carries. Read from the element when the
    // tab is on screen and from the store otherwise, exactly as the model is, so
    // switching tabs never resets it.
    llmDetailLevel: detailLevel?.value || s.settings.llmDetailLevel || LLM_DETAIL_LEVELS[0],
    useBuiltInPatterns: s.settings.useBuiltInPatterns !== false,
    useHeuristicDiscovery: s.settings.useHeuristicDiscovery !== false,
    // Which READINGS of which built-in signals may DERIVE Suggestions. Sent as a
    // complete map of the known sources and their derivations rather than a patch,
    // so Go and the store cannot end up disagreeing about a key one of them has
    // never seen.
    signalSuggestionSources: Object.fromEntries(
      SIGNAL_SOURCES.map((source) => [source, Object.fromEntries(
        (SIGNAL_DERIVATIONS[source] ?? [])
          .map((d) => [d, signalDerivationOn(s, source, d)]))])),
    // Read from the store, not the input: setMinConfidence already validated and
    // stored it, and the block may not be rendered at all.
    minConfidence: s.settings.minConfidence ?? 0,
    heuristicDiscovery: heuristicDiscoveryOptions(s),
  };
}

/**
 * pushSettings applies the payload in one round-trip. The store mirror updates
 * from what Go accepted; errors surface as a banner.
 */
async function pushSettings(container) {
  const settings = settingsPayload(getState(), container);
  try {
    const status = await applySettings(settings);
    // What Go accepted first, then the model IT resolved: an uninstalled name in
    // the payload comes back as an installed one, and the store has to hold the
    // model a run will post to rather than the one that was asked for.
    setState({ settings: { ...getState().settings, ...settings } });
    adoptProbe(status);
    if (status.available) {
      try {
        const models = await listOllamaModels();
        setState({ ollama: { ...status, models } });
      } catch { /* keep the probe result; the dropdown shows nothing */ }
    }
  } catch (err) {
    const slot = container.querySelector("#settings-error");
    if (slot) {
      slot.innerHTML = `<div class="banner error">${escapeHTML(String(err?.message ?? err))}</div>`;
    }
  }
}

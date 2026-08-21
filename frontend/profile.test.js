// profile.test.js, the shared Profile controls.
//
// The two screens that offer profiles render the SAME block, and the gate on
// Save has one definition, so this is where both are asserted: the rail's own
// suite checks that Identify offers Load alone, and anonymise.test.js checks
// that step 3 offers both.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  canSaveProfile, profileControlsHTML, wireProfileControls,
} from "./views/profile.js";
import { resetState, setState, getState } from "./state.js";
import { RAIL } from "./copy.js";
import { exists, one } from "./testhtml.js";
import { container, fire } from "./testdom.js";

const REGISTRY = [
  { original: "Alpine Trust", placeholder: "[ENTITY_1]", category: "entity_names", count: 1 },
];

test("canSaveProfile is the registry, not a detection latch", () => {
  resetState();
  assert.equal(canSaveProfile(getState()), false,
    "a fresh session has no placeholders to preserve");
  setState({ replacedValues: REGISTRY });
  assert.equal(canSaveProfile(getState()), true, "a run that minted placeholders opens the gate");
  setState({ replacedValues: [] });
  assert.equal(canSaveProfile(getState()), false,
    "and the gate closes again when the registry goes, e.g. on a backward move");
});

test("Save is rendered only where it is asked for", () => {
  resetState();
  const loadOnly = profileControlsHTML(getState());
  assert.ok(exists(loadOnly, "#profile-load"), "Load is always offered");
  assert.ok(!exists(loadOnly, "#profile-save"),
    "the default is Load alone, which is what the rail renders");

  const both = profileControlsHTML(getState(), { withSave: true });
  assert.ok(exists(both, "#profile-save"), "step 3 asks for Save and gets it");
});

test("the disabled Save says why rather than vanishing", () => {
  // A control that disappears teaches nothing; one that is visible and explains
  // itself tells the user what to do to earn it.
  resetState();
  const save = one(profileControlsHTML(getState(), { withSave: true }), "#profile-save");
  assert.ok("disabled" in save.attrs);
  assert.ok((save.attrs.title || "").includes(RAIL.profileSaveDisabled));

  setState({ replacedValues: REGISTRY });
  const open = one(profileControlsHTML(getState(), { withSave: true }), "#profile-save");
  assert.ok(!("disabled" in open.attrs), "with a registry behind it, Save is live");
});

test("the block names itself and explains itself once", () => {
  resetState();
  const html = profileControlsHTML(getState(), { withSave: true });
  assert.ok(html.includes(RAIL.profileTitle), "the heading is the section name");
  assert.equal((html.match(/class="help"/g) ?? []).length, 1,
    "one tooltip for the block, not one per button");
});

test("a click on the disabled Save cannot save an empty registry", async () => {
  // The handler repeats the gate, so a click that slips through a stale DOM
  // (a repaint in flight, a browser ignoring the attribute) still cannot write
  // a profile whose re-identification key is empty.
  resetState();
  const root = container();
  root.innerHTML = profileControlsHTML(getState(), { withSave: true });
  wireProfileControls(root);
  await fire(root.querySelector("#profile-save"), "click");
  assert.equal(getState().notice, null,
    "nothing was attempted, so there is nothing to report either way");
});

// profile.js, the Profile controls: load a saved setup, and save the current one.
//
// It is ONE module because the two screens that offer profiles offer the same
// two buttons, and the gate on Save is a fact about Go's registry rather than
// about a screen. Where each screen puts them differs:
//
//   step 2 (Identify)  Load only. Nothing on that step can have produced a
//                      placeholder registry, so a Save there is a control that
//                      can never be used: the registry is minted by a RUN, and
//                      stepping back from Anonymise discards it.
//   step 3 (Anonymise) Load and Save. This is the step that HAS a registry, so
//                      it is the only step where saving a profile means anything.
//
// A profile is a session file: the Values, the never-anonymise list, the
// patterns and the placeholder registry, so a follow-up batch reuses the same
// placeholders. That is why Save is gated on the registry rather than on
// detection having run: detection mints no placeholders.

import { loadSession, saveSession, valuePlaceholders, listRemovedValues } from "../api.js";
import { getState, buildRunRequest, setValueTables } from "../state.js";
import { escapeHTML } from "../html.js";
import { button, helpTooltip } from "../ui.js";
import { notify } from "../toast.js";
import { RAIL } from "../copy.js";
// applySession lives in export.js: it is the load half of the same save/load
// pair, and export.js owns the session shape.
import { applySession } from "./export.js";

/** canSaveProfile(s) is the ONE definition of the Save gate: Go holds a
 *  placeholder registry, mirrored into the store by setValueTables. Both the
 *  disabled attribute and the click handler read it, so a click that slips
 *  through a stale DOM cannot save an empty key. */
export function canSaveProfile(s) {
  return (s.replacedValues?.length ?? 0) > 0;
}

/**
 * profileControlsHTML(s, opts) is the heading, its tooltip and the buttons.
 *
 * @param {object} s state
 * @param {object} [opts]
 * @param {boolean} [opts.withSave] render the Save button too (step 3 only)
 * @returns {string} safe HTML
 */
export function profileControlsHTML(s, opts = {}) {
  const save = opts.withSave
    ? button(RAIL.profileSave, {
      kind: "secondary",
      id: "profile-save",
      disabled: !canSaveProfile(s),
      title: canSaveProfile(s) ? "" : RAIL.profileSaveDisabled,
    })
    : "";
  return `<div class="rail-block profile-block">` +
    `<div class="rail-label-row">` +
    `<span class="rail-field-label">${escapeHTML(RAIL.profileTitle)}</span>` +
    helpTooltip(RAIL.profileHelp, { label: RAIL.profileTitle }) +
    `</div>` +
    `<div class="button-pair">` +
    button(RAIL.profileLoad, { kind: "secondary", id: "profile-load" }) +
    save +
    `</div></div>`;
}

/**
 * wireProfileControls(container) wires whichever of the two buttons is present,
 * so the same call serves a screen with one and a screen with two.
 *
 * Load refreshes the registry mirror after applying the session: a loaded
 * profile can carry its own registry in Go, ready to re-save immediately, and
 * the mirror would otherwise still show whatever it held before the load.
 */
export function wireProfileControls(container) {
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
    if (!canSaveProfile(getState())) return; // guard: matches the disabled attribute
    try {
      await saveSession(buildRunRequest());
      notify(RAIL.profileSaveDone, "ok");
    } catch (err) {
      notify(String(err?.message ?? err), "warn");
    }
  });
}

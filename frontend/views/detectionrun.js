// views/detectionrun.js, the ONE Run detection control and the ONE call behind it.
//
// The control lives in the Configure rail's head and the review list it fills
// lives in the Identify panel, so neither half of step 2 can own it: the rail
// renders the button, the workspace decides which tab the findings land on, and
// views/identify.js is the only module that knows about both. This module holds
// what is common to them, which is everything about the RUN itself: the button's
// markup and its refusals, the progress bar, and the single bridge call.
//
// It deliberately imports nothing from views/identifyrail.js or
// views/identifyworkspace.js. Those two already depend on each other, and a
// third edge into either of them would close a cycle; what this module needs
// from the workspace (where to land afterwards) arrives as a callback instead.

import { runDetection, cancelDetection } from "../api.js";
import {
  getState, setState, llmEnabled, detectionCanRun,
  addSuggestions, setBuiltInPatterns, llmScopeArg,
} from "../state.js";
import { escapeHTML } from "../html.js";
import { button } from "../ui.js";
import { notify } from "../toast.js";
import { WORKSPACE } from "../copy.js";

/**
 * runControlHTML(s, busy) is the Run detection button, or the Cancel button
 * while a run is in flight.
 *
 * The run button says what it will DO, which depends on which detection ROUTES
 * are switched on in the rail. With every route off, discovery and patterns
 * alike, there is nothing to run, and the button says so rather than running an
 * empty pass and reporting "0 suggestions" as if it had looked.
 *
 * It is the step's hero control, so it takes the primary (accent orange) kind
 * and the class that drops the border: the panel it now sits in shows nothing at
 * all until this button has been pressed once, so it must read as the one thing
 * to do on the screen.
 *
 * @param {object} s state
 * @param {boolean} busy whether a detection run is in flight
 * @returns {string} safe HTML
 */
export function runControlHTML(s, busy) {
  if (busy) {
    return button(WORKSPACE.cancel, {
      kind: "secondary", id: "btn-detect-cancel", icon: "cancel",
    });
  }
  const aiOK = llmEnabled(s);
  // detectionCanRun rather than detectionRoutesOn: a run with every discovery
  // route off still produces the built-in patterns' preview, which is a complete
  // answer, so the button is only dead when nothing at all would look.
  const blocked = s.documents.length === 0 ? WORKSPACE.runNeedsDocuments
    : (detectionCanRun(s) ? "" : WORKSPACE.runNeedsRoute);
  return button(WORKSPACE.runDetection, {
    kind: "primary", id: "btn-detect", icon: "find_in_page", cls: "btn-run-detect",
    disabled: !!blocked,
    title: blocked || (aiOK ? WORKSPACE.runWithLLM : WORKSPACE.runOffline),
  });
}

/**
 * progressStrip(s) is the bar shown while a detection run is in flight.
 *
 * The percentage comes from GO: it covers the whole run across every route, so
 * it cannot rewind when the second route starts over with a smaller file count.
 * Recomputing it here from (current+1)/total per route is exactly the bug that
 * made the bar jump backwards mid-run.
 */
export function progressStrip(s) {
  const d = s.discovery;
  // The gate is deliberately `=== true` and nothing else: the bar must depend on
  // a run being in flight, never on a leftover object.
  if (d?.running !== true) return "";
  const pct = Math.max(0, Math.min(100, Math.round((d.fraction ?? 0) * 100)));
  return `<div class="detect-progress">` +
    `<div class="progress-bar"><div style="width:${pct}%"></div></div>` +
    `<span class="hint mono" id="detect-caption">${escapeHTML(detectionCaption(d))}</span>` +
    `</div>`;
}

/**
 * detectionCaption(d) says which route is running, on which file, how far into
 * it, and for how long.
 *
 * Every part of that answers a question a one-line caption leaves open when a
 * run feels stuck: WHICH pass is this (two routes read the same files twice),
 * where inside a long file has it got to (a chunked model scan sits on one
 * caption for minutes), and has anything happened at all recently.
 */
export function detectionCaption(d) {
  const route = d.phaseCount > 1
    ? `${WORKSPACE.phaseName(d.phase)} (${(d.phaseIndex ?? 0) + 1}/${d.phaseCount})`
    : WORKSPACE.phaseName(d.phase);
  const parts = [route];
  if (d.total) parts.push(WORKSPACE.fileOf(d.file ?? "", (d.current ?? 0) + 1, d.total));
  if (d.chunkCount > 1) parts.push(WORKSPACE.chunkOf((d.chunk ?? 0) + 1, d.chunkCount));
  if (d.startedAt) {
    const seconds = Math.max(0, Math.round((Date.now() - d.startedAt) / 1000));
    parts.push(WORKSPACE.elapsed(seconds));
  }
  return parts.join(", ");
}

/**
 * wireDetection(container, opts) wires the ONE Run detection button to the ONE
 * detection call.
 *
 * Which routes run, which files the local model can read, what happens when one
 * file fails and when the run is over all belong to Go. What is left here is
 * what a view should do: start it, fold the findings into the store, and report
 * what came back, INCLUDING the cancelled flag and the per-file problems.
 *
 * @param {HTMLElement} container an element containing the run control. The
 *   button lives in the rail and the tabs it fills live in the workspace, so the
 *   caller passes the whole screen rather than either half.
 * @param {object} [opts]
 * @param {Function} [opts.onSettled] called once the run has ended, before the
 *   final repaint, so the workspace can land on the tab the run filled. It is a
 *   callback rather than an import because the workspace owns that tab state.
 */
export function wireDetection(container, opts = {}) {
  container.querySelector("#btn-detect-cancel")?.addEventListener("click", () => cancelDetection());

  const btn = container.querySelector("#btn-detect");
  if (!btn || btn.disabled) return;

  btn.addEventListener("click", async () => {
    const all = getState().documents.map((d) => d.name);
    if (all.length === 0) return;

    // ONE call for the whole run. Go decides which routes are on, skips what the
    // local model cannot read and says so, keeps going past a file that failed,
    // and always ends the run with a terminal event that clears the progress
    // bar. A per-route sequence here cannot do any of that: it has one
    // cancellation slot per call with a dead zone between them, and it drops the
    // cancelled flag and the status each pass returned.
    setState({
      discovery: {
        running: true, phase: "", phaseIndex: 0, phaseCount: 1,
        current: 0, total: all.length, file: all[0],
        chunk: 0, chunkCount: 0, fraction: 0, startedAt: Date.now(),
      },
    });

    try {
      const result = await runDetection(all, getState().allowlist, llmScopeArg());
      // ONE list, one call. Every row already says which methods found it, so
      // there is no per-route mapping step here for a field to fall out of.
      const added = addSuggestions(result?.suggestions ?? []);

      // The built-in patterns' read-only preview, replaced wholesale: it
      // describes the categories that were on for THIS run, so a merge with an
      // older run would show matches from a category since switched off.
      setBuiltInPatterns(result);

      // What the local model actually did, kept for the rail's read-out. A run
      // that found nothing is not the same fact as a document that holds
      // nothing, and the request count is what separates them.
      if ((result?.llmRequests ?? 0) > 0) {
        setState({
          lastLLMScan: {
            requests: result.llmRequests,
            silent: result.llmSilentRequests ?? 0,
            truncated: result.llmTruncatedRequests ?? 0,
            secondsPerRequest: result.llmSecondsPerRequest ?? 0,
          },
        });
      }

      // A file the model could not read is reported, not silently dropped.
      for (const skip of result?.skipped ?? []) {
        notify(WORKSPACE.skippedNotice(skip.name, skip.reason), "warn");
      }
      // So is a route that failed on one file while the others succeeded.
      for (const message of result?.errors ?? []) {
        notify(message, "warn");
      }
      if (result?.cancelled) {
        notify(WORKSPACE.detectionCancelled(added), "info");
      } else {
        notify(WORKSPACE.detectionDone(added), added ? "ok" : "info");
      }
    } catch (err) {
      notify(String(err?.message ?? err), "warn");
    } finally {
      // Belt and braces: the terminal event already clears this (main.js), so a
      // lost event cannot strand the bar, and a lost promise cannot either.
      setState({ discovery: null });
      // The review panel is hidden until a run has happened, so this flag is
      // what reveals it. It is set once the run has SETTLED, cancelled included:
      // a cancelled run still produced whatever it got through, and hiding the
      // panel that holds it would lose the user's findings.
      setState({ detectionRan: true });
      // Land the user on the tab the run filled, which only the workspace can
      // decide.
      opts.onSettled?.();
      setState({});
    }
  });
}

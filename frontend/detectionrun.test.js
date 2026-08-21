// detectionrun.test.js, tests for the detection run's progress read-out.
//
// views/detectionrun.js holds the ONE Run detection control and the ONE bridge
// call behind it. What is testable without a bridge is the read-out, and it is
// worth testing because a progress bar is how the user tells a slow run from a
// hung one: a bar that rewinds, clamps wrongly or captions the wrong route is
// a bug they can only report as "it looks stuck".
//
// Run with `node --test "frontend/**/*.test.js"`.

import { test } from "node:test";
import assert from "node:assert/strict";

import { progressStrip, detectionCaption } from "./views/detectionrun.js";
import { attr, textOf } from "./testhtml.js";

/** running(patch) is a detection state as main.js builds it from an event. */
function running(patch = {}) {
  return {
    discovery: {
      running: true, phase: "rules", phaseIndex: 0, phaseCount: 1,
      current: 0, total: 3, file: "a.docx",
      chunk: 0, chunkCount: 0, fraction: 0.25, startedAt: Date.now(),
      ...patch,
    },
  };
}

test("no bar unless a run is actually in flight", () => {
  // The gate is `=== true` on purpose: a leftover object must
  // not resurrect the bar.
  assert.equal(progressStrip({ discovery: null }), "");
  assert.equal(progressStrip({ discovery: { running: false, fraction: 0.5 } }), "");
  assert.equal(progressStrip({}), "");
});

test("the bar width is Go's fraction, not a recomputed one", () => {
  const html = progressStrip(running({ fraction: 0.42 }));
  assert.match(attr(html, "div.progress-bar", "class"), /progress-bar/);
  assert.match(html, /width:42%/);
});

test("an out-of-range fraction is clamped rather than rendered as nonsense", () => {
  assert.match(progressStrip(running({ fraction: 1.4 })), /width:100%/);
  assert.match(progressStrip(running({ fraction: -1 })), /width:0%/);
  assert.match(progressStrip(running({ fraction: undefined })), /width:0%/);
});

test("the caption names the route when more than one is running", () => {
  // The tokens are the ENGINE's own: app_detect.go PhaseRules and PhaseLocalLLM.
  // A caption keyed on a token no phase ever carries reads "Starting" for the
  // whole run, and nothing throws to say so.
  const caption = detectionCaption(running({
    phase: "local_llm", phaseIndex: 1, phaseCount: 2, startedAt: null,
  }).discovery);
  assert.match(caption, /Local LLM discovery \(2\/2\)/);
  // Two routes read the same files twice; without the route name the second
  // pass looks like the first one starting over.
  assert.match(caption, /a\.docx \(1 of 3\)/);
});

test("the caption reports the position inside a chunked file", () => {
  // A long model scan used to sit on one unchanging caption for minutes, which
  // is indistinguishable from a hung run.
  const caption = detectionCaption(running({
    phase: "local_llm", chunk: 6, chunkCount: 20, startedAt: null,
  }).discovery);
  assert.match(caption, /part 7 of 20/);
});

test("a single chunk is not reported: 'part 1 of 1' is noise", () => {
  const caption = detectionCaption(running({ chunkCount: 1, startedAt: null }).discovery);
  assert.ok(!caption.includes("part"), caption);
});

test("the caption carries elapsed time so a slow run does not read as a hung one", () => {
  const caption = detectionCaption(running({ startedAt: Date.now() - 95000 }).discovery);
  assert.match(caption, /1m 35s/);
});

test("the strip renders the caption it computed", () => {
  const state = running({ startedAt: null });
  assert.equal(textOf(progressStrip(state), "#detect-caption"),
    detectionCaption(state.discovery));
});

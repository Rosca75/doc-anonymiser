// identifyworkspace.test.js, tests for the detection progress strip
// (BUILD-06).
//
// The reported issue was "detection sometimes does not complete, the progress
// is difficult to follow". The Go side owns completion (one terminal event) and
// the progress arithmetic (one monotonic fraction across the whole run); what
// is left here is what the user reads, and these tests pin it:
//
//   the percentage comes from Go and is NEVER recomputed per route, which is
//   what used to make the bar jump backwards mid-run;
//   the caption names the route, the file, the part of the file and the
//   elapsed time, because those are the questions a run that feels stuck
//   raises.

import { test } from "node:test";
import assert from "node:assert/strict";

import { progressStrip, detectionCaption } from "./views/identifyworkspace.js";
import { attr, textOf } from "./testhtml.js";

/** running(patch) is a detection state as main.js builds it from an event. */
function running(patch = {}) {
  return {
    discovery: {
      running: true, phase: "smart", phaseIndex: 0, phaseCount: 1,
      current: 0, total: 3, file: "a.docx",
      chunk: 0, chunkCount: 0, fraction: 0.25, startedAt: Date.now(),
      ...patch,
    },
  };
}

test("no bar unless a run is actually in flight", () => {
  // The gate is `=== true` on purpose (BUILD-04 CR17): a leftover object must
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
  const caption = detectionCaption(running({
    phase: "ai", phaseIndex: 1, phaseCount: 2, startedAt: null,
  }).discovery);
  assert.match(caption, /Local AI \(2\/2\)/);
  // Two routes read the same files twice; without the route name the second
  // pass looks like the first one starting over.
  assert.match(caption, /a\.docx \(1 of 3\)/);
});

test("the caption reports the position inside a chunked file", () => {
  // A long AI scan used to sit on one unchanging caption for minutes, which
  // is indistinguishable from a hung run.
  const caption = detectionCaption(running({
    phase: "ai", chunk: 6, chunkCount: 20, startedAt: null,
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

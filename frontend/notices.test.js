// notices.test.js, tests for the two modules that carry the application's
// STATEMENTS and its QUESTIONS: toast.js (the notice strip) and modal.js (the
// in-app confirm).
//
// Why this file exists: neither module had a unit test, and
// ../frontend_tests_test.go said so out loud rather than letting the gap sit
// invisible. They are tested together because they are one decision seen from
// two sides (BUILD-05 decision 10): every native confirm() and alert() left the
// application, a question became modal.js and a statement became toast.js, and
// the thing worth pinning is that the two stayed separate.
//
// What is tested is the pure half of each: the builder that takes state and
// returns markup, plus the fact that the reducer shorthands delegate to state.js
// rather than keeping state of their own. The DOM wiring (wireModal's Escape
// handler, wireNotice's dismiss button) needs a document and belongs to the
// layer 3 rendering harness, which drives real events (docs/UITESTING.md).
//
// Run with `node --test "frontend/**/*.test.js"`.

import { test } from "node:test";
import assert from "node:assert/strict";

import { noticeHTML, notify, dismiss } from "./toast.js";
import { modalLayerHTML, askConfirm } from "./modal.js";
import { getState, resetState, answerConfirm } from "./state.js";
import { one, textOf, exists } from "./testhtml.js";

// The store is module-global, so every test starts from a known state. Without
// this a notice left behind by one test is a notice the next one did not set,
// which is the sort of order dependency that makes a suite mysteriously flaky.
function fresh() {
  resetState();
}

// --- The notice strip ------------------------------------------------------

test("no notice renders nothing at all", () => {
  fresh();
  // Empty string, not an empty container: a hidden strip would still take
  // vertical space in the fixed-height workspace (frontend/CLAUDE.md).
  assert.equal(noticeHTML(getState()), "");
  assert.equal(noticeHTML({ notice: null }), "");
});

test("a notice renders its sentence and its tone", () => {
  fresh();
  notify("Saved report.md to the destination folder", "ok");

  const html = noticeHTML(getState());
  assert.match(textOf(html, ".notice-text"), /Saved report\.md/);
  assert.match(one(html, ".notice").attrs.class, /\bnotice-ok\b/,
    "the tone drives the tint, so it has to reach the markup");
});

test("notify defaults to the info tone", () => {
  fresh();
  const stored = notify("3 documents anonymised, nothing written to disk yet");
  assert.equal(stored.tone, "info");
});

test("notify writes through the store rather than holding its own copy", () => {
  // toast.js is documented as holding no state of its own: state.notice is the
  // single source of truth. If it ever kept a private copy, the strip and the
  // store could disagree and only one of them would be what the user sees.
  fresh();
  notify("A fact", "info");
  assert.equal(getState().notice.text, "A fact");

  dismiss();
  assert.equal(getState().notice, null);
  assert.equal(noticeHTML(getState()), "");
});

test("only ONE notice is ever held, the newest", () => {
  // Deliberate: a stack of notices turns into a log nobody reads, and the newest
  // statement is always the one that matters (state.js).
  fresh();
  notify("First", "info");
  notify("Second", "warn");

  const html = noticeHTML(getState());
  assert.match(textOf(html, ".notice-text"), /Second/);
  assert.ok(!textOf(html, ".notice-text").includes("First"),
    "the earlier notice must be replaced, not queued behind the new one");
  assert.match(one(html, ".notice").attrs.class, /\bnotice-warn\b/,
    "and the newest notice's tone wins too");
});

test("a notice's text is escaped, because it can quote a document name", () => {
  fresh();
  notify(`Saved <script>alert(1)</script>.md`, "ok");
  const html = noticeHTML(getState());
  assert.ok(!html.includes("<script>"), "a document name is user data and must be escaped");
});

// --- The in-app confirm ----------------------------------------------------

test("nothing being asked renders no overlay", () => {
  fresh();
  assert.equal(modalLayerHTML(getState()), "");
});

test("a pending question renders its title, body and named action", () => {
  fresh();
  // askConfirm returns the promise that the answer settles. It is deliberately
  // not awaited yet: the point here is what is ON SCREEN while it is pending.
  const answer = askConfirm({
    title: "Export the value mapping as CSV",
    body: "The file contains the re-identification key, so it undoes the anonymisation.",
    confirmLabel: "Export CSV",
    keyBearing: true,
  });

  const html = modalLayerHTML(getState());
  assert.match(html, /Export the value mapping as CSV/);
  assert.match(html, /re-identification key/);

  // The affirmative button names the ACTION, never "OK": the button has to say
  // what it does (modal.js).
  assert.equal(textOf(html, "#modal-confirm").trim(), "Export CSV");
  assert.ok(exists(html, "#modal-cancel"), "there is always a way out");

  answerConfirm(false);
  return answer; // settle it so the test does not leave a pending promise behind
});

test("askConfirm resolves true on confirm and false on cancel", () => {
  // This is the whole contract that replaced confirm(): a Promise<boolean> whose
  // value is the user's answer. Both directions, because a dialog that always
  // resolves the same way is worse than no dialog.
  fresh();
  const confirmed = askConfirm({ title: "Go back and reset?", body: "Everything this step owns is cleared." });
  answerConfirm(true);

  return confirmed.then((yes) => {
    assert.equal(yes, true);

    const cancelled = askConfirm({ title: "Go back and reset?", body: "Everything this step owns is cleared." });
    answerConfirm(false);
    return cancelled.then((no) => assert.equal(no, false));
  });
});

test("answering clears the overlay, so it cannot survive its own answer", () => {
  // The overlay is rendered at shell level precisely so a question survives the
  // re-render its answer triggers. The flip side is that the answer must actually
  // clear it, or the dialog stays on screen over a screen that has moved on.
  fresh();
  const answer = askConfirm({ title: "Reset the Identify step?", body: "It starts fresh." });
  assert.notEqual(modalLayerHTML(getState()), "");

  answerConfirm(false);
  assert.equal(modalLayerHTML(getState()), "",
    "the pending question is gone, so the overlay renders nothing");
  return answer;
});

test("a question's copy is escaped, because it quotes document names", () => {
  fresh();
  const answer = askConfirm({
    title: `Overwrite <b>a.docx</b>?`,
    body: "The file on disk is replaced.",
  });
  const html = modalLayerHTML(getState());
  assert.ok(!html.includes("<b>a.docx</b>"), "the title is data, not markup");

  answerConfirm(false);
  return answer;
});

test("a statement and a question are separate channels", () => {
  // The split IS decision 10. If a notice ever rendered through the modal layer
  // (or the reverse) the application would be back to blocking on things that are
  // only informational.
  fresh();
  notify("Saved", "ok");
  assert.equal(modalLayerHTML(getState()), "",
    "a notice must not raise a dialog");

  fresh();
  const answer = askConfirm({ title: "Go back?", body: "This step is reset." });
  assert.equal(noticeHTML(getState()), "",
    "a question must not appear in the notice strip");

  answerConfirm(false);
  return answer;
});

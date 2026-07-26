// scroll.test.js, tests for the scroll-preservation helper
// (BUILD-04 CR12).
//
// The reported bug: ticking a checkbox or adding an allowlist term threw
// the page back to the top, because the state change re-renders the view
// and a rewritten element starts at scrollTop 0.
//
// There is no DOM here, so the test installs the smallest possible fake:
// a `document` whose querySelector returns a stub element with a mutable
// scrollTop. That is enough to prove the contract that matters, which is
// that the offset read before the mutation is written back after it, even
// when the mutation resets it (which is exactly what a re-render does).

import { test } from "node:test";
import assert from "node:assert/strict";

import { keepScrollPosition, scrollOwner } from "./scroll.js";

/**
 * withFakeView(fn) installs a stub `main#view` element and runs fn with
 * it, restoring whatever global document was there afterwards. Awaiting
 * fn matters: an async case must keep the fake document alive until the
 * late restore has run, which is the whole point of that case.
 */
async function withFakeView(fn) {
  const previous = globalThis.document;
  const view = { scrollTop: 0, scrollLeft: 0 };
  globalThis.document = {
    querySelector: (selector) => (selector === "main#view" ? view : null),
  };
  try {
    return await fn(view);
  } finally {
    if (previous === undefined) delete globalThis.document;
    else globalThis.document = previous;
  }
}

test("scrollOwner finds the shell's scrolling region", async () => {
  await withFakeView((view) => {
    assert.equal(scrollOwner(), view);
  });
});

test("scrollOwner is null when the shell has not painted", () => {
  const previous = globalThis.document;
  delete globalThis.document;
  try {
    assert.equal(scrollOwner(), null);
  } finally {
    if (previous !== undefined) globalThis.document = previous;
  }
});

test("a mutation that resets the scroll has it restored (CR12)", async () => {
  await withFakeView((view) => {
    view.scrollTop = 640;
    view.scrollLeft = 12;
    keepScrollPosition(() => {
      // This is what a full re-render does to the scroll container.
      view.scrollTop = 0;
      view.scrollLeft = 0;
    });
    assert.equal(view.scrollTop, 640, "the vertical offset must survive the re-render");
    assert.equal(view.scrollLeft, 12);
  });
});

test("the mutation's return value is passed through", async () => {
  await withFakeView(() => {
    assert.equal(keepScrollPosition(() => 42), 42);
  });
});

test("the scroll is restored even when the mutation throws", async () => {
  await withFakeView((view) => {
    view.scrollTop = 300;
    assert.throws(() => keepScrollPosition(() => {
      view.scrollTop = 0;
      throw new Error("reducer blew up");
    }), /reducer blew up/);
    assert.equal(view.scrollTop, 300);
  });
});

test("an async mutation has the scroll restored after it settles", async () => {
  await withFakeView(async (view) => {
    view.scrollTop = 500;
    const result = await keepScrollPosition(async () => {
      view.scrollTop = 0;      // the synchronous re-render
      await Promise.resolve();
      view.scrollTop = 0;      // a second re-render, after the bridge replied
      return "done";
    });
    assert.equal(result, "done");
    assert.equal(view.scrollTop, 500, "the late re-render must not win either");
  });
});

test("an async mutation that rejects still restores the scroll", async () => {
  await withFakeView(async (view) => {
    view.scrollTop = 220;
    await assert.rejects(
      () => keepScrollPosition(async () => {
        view.scrollTop = 0;
        throw new Error("bridge failed");
      }),
      /bridge failed/);
    assert.equal(view.scrollTop, 220);
  });
});

test("without a scroll owner the mutation still runs", () => {
  const previous = globalThis.document;
  delete globalThis.document;
  try {
    let ran = false;
    const out = keepScrollPosition(() => { ran = true; return 7; });
    assert.equal(ran, true);
    assert.equal(out, 7);
  } finally {
    if (previous !== undefined) globalThis.document = previous;
  }
});

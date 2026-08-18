// api.test.js, tests for the Go bridge wrappers.
//
// api.js touches `window` only INSIDE its functions, so the module can be
// imported here and driven against a stub bridge. That is enough to lock
// the one wrapper that carries real logic rather than a bare delegation:
// openDocumentation, which has to ask Go where the documentation
// lives, open a SEPARATE window on that path, and fail with an actionable
// message when the WebView refuses.
//
// Run with `node --test "frontend/**/*.test.js"`.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  openDocumentation, documentationURL, exportDocumentFormats, ping, runPipeline,
  valuePlaceholders, setValuePlaceholder, removeValue, restoreValue,
  listRemovedValues, validateValues, checkIntersections,
  copyText,
} from "./api.js";

/**
 * withStubBridge(app, openImpl, fn) installs a fake Wails bridge and
 * window.open for the duration of fn, then restores whatever was there.
 * Node has no window, so this also covers "the module never reads window
 * at import time".
 */
async function withStubBridge(app, openImpl, fn) {
  const previous = globalThis.window;
  const calls = [];
  globalThis.window = {
    // Namespace mirrors the real Wails binding: App lives in package
    // backend, so it is exposed as window.go.backend.App (see api.js).
    go: { backend: { App: app } },
    open: (...args) => {
      calls.push(args);
      return openImpl(...args);
    },
  };
  try {
    return await fn(calls);
  } finally {
    if (previous === undefined) delete globalThis.window;
    else globalThis.window = previous;
  }
}

test("documentationURL delegates to the Go bound method", async () => {
  await withStubBridge(
    { DocumentationURL: async () => "docs/index.html" },
    () => ({}),
    async () => {
      assert.equal(await documentationURL(), "docs/index.html");
    });
});

test("openDocumentation opens the Go-declared path in a named window", async () => {
  await withStubBridge(
    { DocumentationURL: async () => "docs/index.html" },
    () => ({ focus() { this.focused = true; } }),
    async (calls) => {
      await openDocumentation();
      assert.equal(calls.length, 1);
      const [url, name, features] = calls[0];
      // The path comes from Go, never hardcoded in the frontend.
      assert.equal(url, "docs/index.html");
      // A NAMED window, so a second click focuses the existing one
      // instead of stacking copies.
      assert.equal(name, "doc-anonymiser-docs");
      assert.match(features, /width=\d+/);
    });
});

test("openDocumentation never targets a remote URL", async () => {
  // The local-only guarantee: whatever Go returns is opened as a relative
  // asset-server path. A scheme here would mean the documentation left
  // the embedded assets.
  await withStubBridge(
    { DocumentationURL: async () => "docs/index.html" },
    () => ({}),
    async (calls) => {
      await openDocumentation();
      const [url] = calls[0];
      assert.ok(!/^[a-z]+:\/\//i.test(url), `documentation URL must stay relative: ${url}`);
    });
});

test("openDocumentation reports a blocked window with an actionable message", async () => {
  await withStubBridge(
    { DocumentationURL: async () => "docs/index.html" },
    () => null, // what a popup blocker returns
    async () => {
      await assert.rejects(
        () => openDocumentation(),
        (err) => {
          assert.match(err.message, /could not be opened/);
          assert.match(err.message, /popup blocker/, "the message must say what to do");
          return true;
        });
    });
});

test("the bridge wrappers explain a missing Wails bridge", async () => {
  const previous = globalThis.window;
  globalThis.window = {}; // a plain browser: no window.go
  try {
    await assert.rejects(async () => documentationURL(), /must run inside the/);
  } finally {
    if (previous === undefined) delete globalThis.window;
    else globalThis.window = previous;
  }
});

test("a missing bridge REJECTS, it never throws synchronously", () => {
  // This is the contract the whole frontend is written against, and it was not
  // true until the Linux rendering harness ran with no bridge at all
  // (scripts/uitest/renderharness). bridge() throws, so a non-async wrapper threw
  // synchronously: the caller got an exception instead of a promise, the
  // `.catch()` at the call site never ran, and views/export.js ensureFormats()
  // took the whole Export screen down on an uncaught error.
  //
  // Calling a wrapper WITHOUT awaiting must therefore hand back a promise, every
  // time. The three below are a render-path call, a startup call and a
  // long-running call, so all three shapes are covered.
  const previous = globalThis.window;
  globalThis.window = {}; // a plain browser: no window.go
  try {
    for (const [name, call] of [
      ["exportDocumentFormats", () => exportDocumentFormats("a.docx")],
      ["ping", () => ping()],
      ["runPipeline", () => runPipeline({})],
    ]) {
      let returned;
      assert.doesNotThrow(() => { returned = call(); },
        `${name} must not throw synchronously when the bridge is absent`);
      assert.ok(returned instanceof Promise, `${name} must return a promise`);
      // The rejection still has to be handled, or node reports it as unhandled.
      returned.catch((err) => assert.match(err.message, /Wails bridge not available/));
    }
  } finally {
    if (previous === undefined) delete globalThis.window;
    else globalThis.window = previous;
  }
});

// --- The step 3 value surface --------------------------------------------

test("the value wrappers name the Go methods BRIDGE.md documents", async () => {
  // A bare delegation still has one thing that can be wrong, and it is the one
  // thing nothing else catches: the METHOD NAME. A typo here is a rejection at
  // runtime on a button, and every other test in this folder passes.
  const called = [];
  const record = (name) => async (...args) => {
    called.push([name, ...args]);
    return name;
  };
  const app = {
    ValuePlaceholders: record("ValuePlaceholders"),
    SetValuePlaceholder: record("SetValuePlaceholder"),
    RemoveValue: record("RemoveValue"),
    RestoreValue: record("RestoreValue"),
    ListRemovedValues: record("ListRemovedValues"),
    ValidateValues: record("ValidateValues"),
    CheckIntersections: record("CheckIntersections"),
  };

  await withStubBridge(app, () => ({}), async () => {
    assert.equal(await valuePlaceholders(), "ValuePlaceholders");
    assert.equal(await setValuePlaceholder("[ENTITY_1]", "[CLIENT_1]"), "SetValuePlaceholder");
    assert.equal(await removeValue("[ENTITY_1]"), "RemoveValue");
    assert.equal(await restoreValue("[ENTITY_1]"), "RestoreValue");
    assert.equal(await listRemovedValues(), "ListRemovedValues");
    assert.equal(await validateValues({ values: [] }), "ValidateValues");
    assert.equal(await checkIntersections({ values: [] }), "CheckIntersections");
  });

  // The arguments reach Go in the documented order, which is the other half of
  // the contract: setValuePlaceholder(current, next), not (next, current).
  assert.deepEqual(called[1], ["SetValuePlaceholder", "[ENTITY_1]", "[CLIENT_1]"]);
});

test("the value wrappers reject rather than throw when the bridge is absent", async () => {
  // The rendering harness serves this folder as static files with no Go behind
  // it, so a view that calls these while rendering must get a rejection it can
  // catch, never a synchronous throw (frontend/CLAUDE.md, Testing).
  const previous = globalThis.window;
  globalThis.window = {}; // a plain browser: no window.go
  try {
    for (const wrapper of [valuePlaceholders, listRemovedValues]) {
      const promise = wrapper();
      assert.ok(promise instanceof Promise, `${wrapper.name} must be async`);
      await assert.rejects(promise, /must run inside the/, wrapper.name);
    }
  } finally {
    if (previous === undefined) delete globalThis.window;
    else globalThis.window = previous;
  }
});

test("copyText calls CopyText with the selected text", async () => {
  // Clipboard access goes through Go, as copyDocument does, so the wrapper's
  // job is to reach the right bound method with the right argument.
  const called = [];
  const app = {
    CopyText: (...args) => { called.push(args); return "CopyText"; },
  };
  await withStubBridge(app, () => ({}), async () => {
    assert.equal(await copyText("Marie Duval"), "CopyText");
  });
  assert.deepEqual(called, [["Marie Duval"]]);
});

test("copyText rejects rather than throws when the bridge is absent", async () => {
  const previous = globalThis.window;
  globalThis.window = {};
  try {
    const promise = copyText("Marie Duval");
    assert.ok(promise instanceof Promise);
    await assert.rejects(promise, /must run inside the/);
  } finally {
    if (previous === undefined) delete globalThis.window;
    else globalThis.window = previous;
  }
});

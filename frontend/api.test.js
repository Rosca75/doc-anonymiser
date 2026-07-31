// api.test.js, tests for the Go bridge wrappers (BUILD-04 Phase 3).
//
// api.js touches `window` only INSIDE its functions, so the module can be
// imported here and driven against a stub bridge. That is enough to lock
// the one wrapper that carries real logic rather than a bare delegation:
// openDocumentation (CR6), which has to ask Go where the documentation
// lives, open a SEPARATE window on that path, and fail with an actionable
// message when the WebView refuses.
//
// Run with `node --test "frontend/**/*.test.js"`.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  openDocumentation, documentationURL, exportDocumentFormats, ping, runPipeline,
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

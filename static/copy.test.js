// copy.test.js, the automated copy-style guard (BUILD-02 Phase 1f).
//
// UI copy rules (BUILD-02 ground rule 4): no em dashes (U+2014) and no en
// dashes used as dashes (U+2013) anywhere in the frontend source. This test
// walks every .js and .html file under static/ (excluding *.test.js and
// assets/) and fails listing file:line for each offending character, so a
// dash can never quietly return. Use commas, periods, or parentheses.
//
// The matching Go-side guard is copy_guard_test.go at the repository root.

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const staticDir = path.dirname(fileURLToPath(import.meta.url));

/** walk(dir) yields every .js/.html file under dir, skipping tests+assets. */
function* walk(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name === "assets" || entry.name === "node_modules") continue;
      yield* walk(full);
      continue;
    }
    if (entry.name.endsWith(".test.js")) continue;
    if (entry.name.endsWith(".js") || entry.name.endsWith(".html")) yield full;
  }
}

test("no em or en dashes in frontend source", () => {
  const hits = [];
  for (const file of walk(staticDir)) {
    const lines = fs.readFileSync(file, "utf8").split("\n");
    lines.forEach((line, i) => {
      if (line.includes("—") || line.includes("–")) {
        hits.push(`${path.relative(staticDir, file)}:${i + 1}: ${line.trim()}`);
      }
    });
  }
  assert.deepEqual(hits, [],
    "em/en dashes found in UI source; replace them with commas, periods or parentheses:\n" +
    hits.join("\n"));
});

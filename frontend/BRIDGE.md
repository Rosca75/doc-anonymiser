# BRIDGE.md — the Go ↔ JS contract

This is the **handoff surface** between the GUI and the backend. A design or
UI change reads this to know exactly what data the interface can request and
what shape it comes back in, **without opening any Go**.

- Every call goes through a wrapper in `api.js` (the only file allowed to
  touch the bridge).
- Under the hood each wrapper calls `window.go.backend.App.<Method>` and gets
  a `Promise`. The namespace is `backend` because the App struct lives in
  `../backend` (package `backend`).
- Source of truth: the wrappers in `frontend/api.js` and the method bodies in
  `../backend/app.go`, `app_entities.go`, `app_export.go`, `app_run.go`. If
  this doc and those disagree, the code wins — update this doc.
- Shapes below use `{...}` for objects and `[...]` for arrays. "resolves to"
  = the Promise's fulfilled value. Only genuine failures reject; documented
  "normal empty/absent" states resolve instead (graceful degradation).

## Connectivity / status

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `ping()` | — | `"pong"` (proves the JS↔Go bridge end to end) |
| `probeOllama()` | — | `{available, models, detail}`. Never rejects for "Ollama missing" — that is a normal state in the object. |
| `listOllamaModels()` | — | installed model names `[string]` |

## Documentation window

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `documentationURL()` | — | asset path of the bundled docs page (Go owns the path) |
| `openDocumentation()` | — | opens the docs in a SECOND window via `window.open`; rejects with an actionable message if the WebView refuses |

## Import

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `importFiles()` | — | `ImportResult {documents, errors}` (native multi-file dialog) |
| `removeDocument(name)` | `name` | `ImportResult` |
| `listDocuments()` | — | current `DocumentInfo[]` |
| `getDocumentSource(name)` | `name` | `{found, markdown, truncated, isGrid}`, the SOURCE text of one imported document. An unknown name resolves with `found: false`; it never rejects. |

**Original text has exactly one producer.** `DocumentInfo.markdown` and
`getDocumentSource()` are the same bytes from the same document, cut by the
same `engine.PreviewMarkdown`. The pipeline result carries NO copy of the
source (`ResultDocument` has no `original` field): the Anonymise screen's
ORIGINAL pane reads the import list, and falls back to `getDocumentSource()`
only for a document that has left it. Anything that reintroduces a second
"original" reintroduces the bug where a preview drifts from the imported file.

`DocumentInfo` carries `unitCount` and `unit` (BUILD-05 Phase 3): the document's
size in its OWN terms, so the import list can say "6 pages" or "12 slides"
rather than only a byte count. `unit` is SINGULAR (`"page"`, `"slide"`, `"row"`,
`"line"`) and the frontend pluralises, because only the side printing the number
knows which form it needs. `"line"` is the common fallback, not an error: a page
count can only come from what the writing application cached in
`docProps/app.xml`, and that part is optional.

## Settings

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `getSettings()` | — | `{level, categories, ollamaPort, model, contextSize, useAI, useSmartDetect, minConfidence, smartDetect}` (see app.go `Settings`) |
| `applySettings(settings)` | settings object | fresh `OllamaStatus`; rejects with an actionable message on bad input |

`useAI` and `useSmartDetect` are the DETECTION ROUTE switches: Smart detection
on by default, Local AI off by default and additionally gated on the live
Ollama probe. There is no cloud route and no `useCloudAI` on the Go side
(BUILD-05 decision 8).

## Allowlist (never-anonymise terms)

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `defaultAllowlist()` | — | seeded never-anonymise terms shown at startup (removable like any other) |
| `importAllowlistCSV()` | — | parsed terms, or `null` when the user cancels the dialog |
| `saveAllowlistTemplate()` | — | saves the downloadable CSV template |

## Values screen (discovery — UI label "Values", engine token `entities`)

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `runDetection(fileNames, allowTerms)` | names, allowlist | `DetectionResult {candidates, proposals, phases, skipped, errors, cancelled, status}`. THE detection entry point: Go runs every switched-on route under one cancellation context. A cancelled run resolves with the partial findings and `cancelled: true`; only a failure to START rejects (no matching documents, a run already in flight). |
| `cancelDetection()` | — | aborts the in-flight run, reaching whichever route is running, including mid-file |
| `runDiscovery(fileNames, allowTerms)` | names, allowlist | `DiscoveryResult {proposals:[{category,text}], status, cancelled}`; a cancelled run resolves with partial proposals, only real failures reject |
| `cancelDiscovery()` | — | aborts the in-flight discovery run (no-op if idle) |
| `estimateDiscovery(fileNames)` | names | `[{name, chunks, tooLarge, message}]` so oversized files can be excluded BEFORE the run |
| `expandVariants(entity)` | entity | `{category, canonical, manualVariants, excludedVariants}` |
| `runSmartDetection(fileNames, allowTerms, classify, options)` | names, allowlist, bool, `SmartDetectOptions {minLength, minOccurrences, excludeCommonWords, minConfidence}` | `SmartDetectionResult {candidates, status, cancelled}`; works fully offline, `classify=true` refines categories via local AI |
| `countTermMatches(term)` | term | `{count, documents}`, the live read-out under the manual add-value row (debounced) |
| `validatePattern(expr)` | regex | `""` (valid) or the error message |
| `patternMatches(expr)` | regex | up to 20 sample matches across the loaded documents, shown live under the pattern field: a regex that compiles and matches nothing is the common mistake |
| `entityPlaceholder(category, canonical)` | engine category id, value | the placeholder currently assigned to that value, or `""` before the first run |
| `setEntityPlaceholder(category, canonical, placeholder)` | engine category id, value, `[NAME_N]` | resolves on success; REJECTS with an actionable message when the shape is wrong or the placeholder already belongs to another value. Takes effect on the next run or fast re-run, never retroactively (BUILD-05 Phase 3) |

## Run screen (pipeline)

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `runPipeline(request)` | request | resolves immediately; results arrive on the `pipeline:done` event, progress on `pipeline:progress` |
| `cancelPipeline()` | — | aborts the in-flight run |
| `fastRerun(request)` | request | re-runs the deterministic passes only (no LLM); resolves directly to fresh `Results` |
| `getResults()` | — | latest `Results`, or `null` |
| `getMapping()` | — | placeholder → `{original, category}` lookup (empty before the first run) |

## Export screen

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `exportDocumentFormats(name)` | name | offered extensions (default first) for one result document |
| `saveDocument(name, ext)` | name, ext | opens a save dialog for one document |
| `getSameFormatMetadata(name, ext)` | name, ext | `{fields, filename}`: document properties with proposed replacements + proposed anonymised filename, for the review panel |
| `saveSameFormat(name, ext, fields, filename)` | name, ext, reviewed fields, filename | writes the same-format copy with the REVIEWED metadata and filename |
| `exportAllZip()` | — | saves every anonymised document into one zip, via the native save dialog |
| `chooseExportFolder()` | — | opens the native FOLDER picker and resolves to the chosen path, or `""` when cancelled. Picks only; writes nothing (BUILD-05 Phase 3) |
| `exportAllZipTo(dir)` | folder path | writes the batch zip into that folder with NO second dialog and resolves to the full path written. The only dialog-free write in the contract, allowed because the folder was chosen explicitly and the zip carries no re-identification key (decision 4). An existing archive is never overwritten, the new one is numbered |
| `copyDocument(name)` | name | puts the anonymised text on the clipboard |
| `exportMapping(format)` | `"csv"`/`"json"` | saves the re-identification key. Call ONLY after the user confirmed the sensitivity warning |
| `exportReport(format)` | `"json"`/`"md"` | saves the run report, INCLUDING the per-value table (BUILD-06). That table maps placeholders back to real values, so the exported report is a re-identification key: warn before writing it, as for `exportMapping`. |
| `saveSession(request)` | request | persists the session (entities, allowlist, patterns, rules, settings, registry). Warn the user first: the file contains the re-identification key |
| `loadSession()` | — | the `Session` object, or `null` when the user cancels |

## Runtime events (push, not request/reply)

Subscribe with `onEvent(name, handler)` (returns an unsubscribe function; a
missing runtime is a safe no-op).

| Event | Emitted when | Payload |
|---|---|---|
| `documents:changed` | after a drag-drop import (drops are push, not request/reply) | `ImportResult` |
| `pipeline:progress` | during a `runPipeline` run | progress info |
| `pipeline:done` | when a `runPipeline` run finishes | `Results` |
| `detection:progress` | during a `runDetection` run | `{phase, phaseIndex, phaseCount, docIndex, docCount, docName, chunkIndex, chunkCount, fraction}` |
| `detection:done` | when a `runDetection` run finishes, is cancelled, or has nothing to run | `DetectionResult` |
| `detection:error` | when a run stops unexpectedly | `{message}` |

**`detection:done` / `detection:error` are a guarantee, not a courtesy.** Exactly
one of them fires for every started run, so the progress bar can be cleared by
the event rather than by the caller's `finally`. `fraction` is the whole run's
progress, computed in Go and non-decreasing across routes: never recompute a
percentage per route in the frontend, that is what made the bar rewind when the
second route started with a smaller file count.

## Rules for changing the contract

- New backend data for the UI = a new exported method on `App` in
  `../backend/app*.go` + a new wrapper in `api.js` + a row here.
- Never call `window.go` / `window.runtime` outside `api.js`.
- Keep method names and shapes in sync across `api.js`, the Go methods and
  this table.

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
| `resetRun()` | — | nothing. Discards the Go-side run state (registry, results, last request, removed values); keeps documents and settings. Called by nav.js when a backward move leaves the Anonymise step, so a re-run restarts numbering from 1. |
| `resetSession()` | — | nothing; rejects with an actionable message while a run or detection is in progress. Returns the whole Go session to a freshly launched state (no documents, no registry, default settings). The Import step's "start over" action, paired with the frontend `resetState()`. |
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

`DocumentInfo` also carries `pageCount`: how many units the LOCAL AI scan scope
can address for this document (`PageRangeMarkdown` is 1-based inclusive over
them). For PDF and DOCX it counts the explicit per-page texts held at ingestion;
for a flat CSV/XLSX sheet it is the row count, for PPTX the slide count, for
TXT/MD the line count; a complex sheet or a single-unit document reports 1. The
Local AI section sizes its From/To range inputs from it.

## Settings

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `applySettings(settings)` | settings object | fresh `OllamaStatus`; rejects with an actionable message on bad input |

`useAI` and `useSmartDetect` are the DETECTION ROUTE switches: Smart detection
on by default, Local AI off by default and additionally gated on the live
Ollama probe. Smart detection split into two independently switchable halves,
both sent alongside `useSmartDetect`: `useNativeDetect` is the MASTER over the
regex signal categories (email, VAT, IBAN, amount, date, ...) applied at
anonymisation time, and `useAutoDetect` is the offline word-frequency detection
pass. Both default on. `useSmartDetect` is now DERIVED and sent for backward
compatibility, always `useNativeDetect || useAutoDetect`. There is no cloud
route and no `useCloudAI`, on either side. The frontend store carried one and
`pushSettings` sent it while Go discarded it, which made this line read as a
contradiction rather than a contract; BUILD-06 Phase 8 deleted the field instead
of documenting it, because a setting nothing reads and nothing can change is a
claim the next reader has to disprove.

`country` is the DOCUMENT COUNTRY (BUILD-06 Phase 1), one of the codes in
`engine.SupportedCountries`. It is a real engine setting, not a frontend
display choice: it decides which country-specific regex categories run.
`applySettings` rejects an unknown code.

## Allowlist (never-anonymise terms)

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `defaultAllowlist()` | — | the suggested never-anonymise terms. NOT added to the list automatically: the allowlist starts empty and the user chooses its terms. Kept as the source for the downloadable template. |
| `importAllowlistCSV()` | — | parsed terms, or `null` when the user cancels the dialog |
| `saveAllowlistTemplate()` | — | saves the downloadable CSV template |

## Values screen (discovery — UI label "Values", engine token `entities`)

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `runDetection(fileNames, allowTerms, aiScope)` | names, allowlist, optional `AIScope {docName, pages}` (null = every document whole; restricts the LOCAL AI route only; `pages` is a 1-based `number[]` over the document's own page/slide/row/line units, and an empty array means the whole selected document) | `DetectionResult {candidates, proposals, phases, skipped, errors, cancelled, status}`. THE detection entry point: Go runs every switched-on route under one cancellation context. A cancelled run resolves with the partial findings and `cancelled: true`; only a failure to START rejects (no matching documents, a run already in flight). An out-of-range or unknown-document scope is reported in `errors`, not rejected. |
| `cancelDetection()` | — | aborts the in-flight run, reaching whichever route is running, including mid-file |
| `expandVariants(entity)` | `{category, canonical, manualVariants, autoExpand}` | the spellings this value matches, longest first. `autoExpand: false` means the user curated the list: Go derives nothing and returns the canonical name plus exactly the spellings it was given, so the chips on the card are what the run replaces |
| `countTermMatches(term)` | term | `{count, documents}`, the live read-out under the manual add-value row (debounced) |
| `checkIntersections(request)` | `{entities, patterns, allowTerms, categories, suppressRegexPII}` | `{intersections: [{value, category, origin, winnerValue, winnerCategory, winnerOrigin, occurrences, totalOccurrences, documents}]}`. The values another detection route also claims, so a card can warn BEFORE the run rather than the user finding out on the results screen. `occurrences == totalOccurrences` means the value is never replaced under its own type. Mutates nothing (no placeholder minted, registry untouched), so it is safe to call on every edit. An empty list is the normal answer, not an error |
| `validatePattern(expr)` | regex | `""` (valid) or the error message |
| `patternMatches(expr)` | regex | up to 20 sample matches across the loaded documents, shown live under the pattern field: a regex that compiles and matches nothing is the common mistake |

## Values, placeholders and removals (step 3)

These are the surface behind the step 3 **Replaced values** table: one row per
value the session replaced, editable placeholder, remove action, and a collapsed
removed list with restore.

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `valuePlaceholders()` | — | `[{original, placeholder, category, count}]`, sorted by category then placeholder number. The source for the table, read from the REGISTRY rather than derived from report text, so a row cannot exist that the edit behind it has no entry for. Empty before the first run, which is an empty table, not an error |
| `setValuePlaceholder(current, next)` | `[NAME_N]`, `[NAME_N]` | resolves on success; REJECTS when the shape is wrong, when `current` is not a placeholder this session assigned, or when `next` already belongs to another value (two originals behind one placeholder makes the key ambiguous). Takes effect on the NEXT run, never retroactively |
| `removeValue(placeholder)` | `[NAME_N]` | resolves to `{original, category, placeholder, variants}`. Removal prunes the registry entry AND records a session exclusion, so the value stays gone across re-runs and same-format exports. The NUMBER is not freed. Does not re-run: the caller re-runs |
| `restoreValue(placeholder)` | the placeholder it USED to have | resolves on success; REJECTS when nothing by that placeholder was removed. The value returns on the next run with a NEW number, because the old one stays retired |
| `listRemovedValues()` | — | `[{original, category, placeholder, variants}]` for the collapsed removed list |
| `nextRulePlaceholder()` | — | the next free `[CUSTOM_N]`, RESERVED as it is handed over. Replaces the frontend's own numbering, which counted only the rules while `CUSTOM` is also the automatic label for `custom_patterns` matches, so a rule and an automatic assignment could collide. Works before the first run |
| `validateValues(request)` | `{entities, patterns, rules, allowTerms}` | `{blocking, warnings}`, each `[{kind, severity, message}]`. Blocking conflicts refuse the run, so this is what a screen calls to say so before the user presses it |

## Run screen (pipeline)

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `runPipeline(request)` | request | resolves immediately; results arrive on the `pipeline:done` event, progress on `pipeline:progress` |
| `cancelPipeline()` | — | aborts the in-flight run |
| `fastRerun(request)` | request | re-runs the deterministic passes only (no LLM); resolves directly to fresh `Results` |
| `getMapping()` | — | placeholder → `{original, category}` lookup (empty before the first run) |

The run `request` is `{entities, allowTerms, patterns, categories, simpleRules,
suppressRegexPII}`. Each entity is
`{category, canonical, manualVariants, origin, autoExpand}`. `autoExpand: false`
means the spellings are curated, so Go replaces exactly `manualVariants` plus
the canonical name and derives nothing. `origin` is the ROUTE that produced the
value, one of `native`, `declared`, `auto`, `ai` (precedence order, lower wins);
it is what decides which route owns a string when two claim the same text, and
it is deliberately separate from confidence, which feeds the `minConfidence`
floor. Absent reads as `declared`.
`suppressRegexPII` is the "Native detection" master switch
inverted (`!useNativeDetect`): when true, Go skips the deterministic regex PII
pass (pass 1) so NO signal category is replaced, whatever `categories` selects;
the entity, custom-pattern and code passes are unaffected.

## Export screen

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `exportDocumentFormats(name)` | name | offered extensions (default first) for one result document |
| `saveDocument(name, ext)` | name, ext | opens a save dialog for one document |
| `getSameFormatMetadata(name, ext)` | name, ext | `{fields, filename}`: document properties with proposed replacements + proposed anonymised filename, for the review panel |
| `saveSameFormat(name, ext, fields, filename)` | name, ext, reviewed fields, filename | writes the same-format copy with the REVIEWED metadata and filename |
| `chooseExportFolder()` | — | opens the native FOLDER picker and resolves to the chosen path, or `""` when cancelled. Picks only; writes nothing (BUILD-05 Phase 3) |
| `exportAllZipTo(dir)` | folder path | writes the batch zip into that folder with NO second dialog and resolves to the full path written. The only dialog-free write in the contract, allowed because the folder was chosen explicitly and the zip carries no re-identification key (decision 4). An existing archive is never overwritten, the new one is numbered |
| `copyDocument(name)` | name | puts the anonymised text on the clipboard |
| `copyText(text)` | the selected text | puts an arbitrary short string on the clipboard, for the Compare pane's selection panel. Rejects with an actionable message on an empty selection or one over 4096 bytes, which is a mis-drag guard: a drag that ran away down the pane must not push a whole document through the clipboard |
| `exportMapping(format)` | `"csv"`/`"json"` | saves the re-identification key. Call ONLY after the user confirmed the sensitivity warning |
| `exportReport(format)` | `"json"`/`"md"` | saves the run report, INCLUDING the per-value table (BUILD-06). That table maps placeholders back to real values, so the exported report is a re-identification key: warn before writing it, as for `exportMapping`. |
| `saveSession(request)` | request | persists the session (entities, allowlist, patterns, rules, settings, registry, the removal list and the spent placeholder numbers). Warn the user first: the file contains the re-identification key |
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

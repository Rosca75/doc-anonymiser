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
  `../backend/app.go`, `app_values.go`, `app_detect.go`, `app_export.go`,
  `app_run.go`. If this doc and those disagree, the code wins — update this doc.
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

There are TWO detection routes, and the settings say so directly.

**Local AI** is one switch, `useLocalAI`. Off by default and additionally gated
on the live Ollama probe, so a stale `true` can never start a model that is not
running.

**Smart detection** is THREE methods, each with its own setting, and no switch of
its own:

| Setting | What it does | Default |
|---|---|---|
| `useBuiltInPatterns` | MASTER over the structured signal categories (email, VAT, IBAN, amount, date, …). Off means pass 1 is skipped and no signal category is replaced, whatever `categories` selects; the selection is left intact. Produces DIRECT MATCHES, never Suggestions. | on |
| `useHeuristicDiscovery` | Heuristic discovery: spelling, context, frequency and deterministic gazetteers. Produces SUGGESTIONS. | on |
| `signalSuggestionSources` | `{email: {"email.person": bool, "email.organisation": bool}}`, keyed by `engine.AllSignalSources` and then by `engine.SignalDerivations[source]`. Which READINGS of which built-in signals may be used as EVIDENCE to derive Suggestions. Produces SUGGESTIONS. | every reading on |

The section's on/off state is DERIVED (`state.js smartDetectionOn`): it is on when
any of the three is on. There is deliberately no fourth persisted boolean, because
a stored section flag can disagree with the three methods it claims to summarise,
and a section reading "On" while every method is off lies about what a run does.
The rail's header switch is a master that changes all three in one action.

`signalSuggestionSources` is keyed by source AND by DERIVATION, because one signal
supports several readings through several mechanisms: an address's local part is
evidence for a person (`email.person`), its domain for an organisation
(`email.organisation`), and wanting one without the other is a reasonable thing to
want. Each reading is switched on its own; a source has no boolean of its own, and
the rail DERIVES the signal's master state from its readings (on when any is on) for
the same reason the Smart detection section derives its own.

It does NOT govern whether a signal is matched and replaced. Clearing a reading
stops the Suggestions THAT reading produces and leaves email anonymisation exactly
as it was, which is governed by `useBuiltInPatterns` and the `email` category.
Conflating the two is the mistake the separate setting exists to prevent. A key the
object omits reads as its DEFAULT, never as off, at either level and on both sides:
the safe reading of silence is the shipped behaviour.

Go REFUSES an unknown source or an unknown derivation rather than storing it, and
the refusal names the valid set: stored, it would be a switch nothing reads for the
rest of the session.

There is no cloud route and no `useCloudAI`, on either side.

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

## Identify: detection and the Value surface

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `runDetection(fileNames, allowTerms, aiScope)` | names, allowlist, optional `AIScope {docName, pages}` (null = every document whole; restricts the LOCAL AI route only; `pages` is a 1-based `number[]` over the document's own page/slide/row/line units, and an empty array means the whole selected document) | `DetectionResult {suggestions, phases, skipped, errors, cancelled, status}`. THE detection entry point: Go runs every switched-on route under one cancellation context. A cancelled run resolves with the partial findings and `cancelled: true`; only a failure to START rejects (no matching documents, a run already in flight). An out-of-range or unknown-document scope is reported in `errors`, not rejected. |
| `cancelDetection()` | — | aborts the in-flight run, reaching whichever route is running, including mid-file |
| `expandSpellings(value)` | `{category, mainText, spellings, spellingPolicy}` | the forms this Value matches, longest first. `spellingPolicy: "curated"` means the list is the user's: Go derives nothing and returns the main text plus exactly the spellings it was given, so the chips on the card are what the run replaces |
| `countTermMatches(term)` | term | `{count, documents}`, the live read-out under the manual declaration row (debounced) |
| `checkIntersections(request)` | `{values, patterns, allowTerms, categories, suppressRegexPII}` | `{intersections: [{value, category, matchClass, winnerValue, winnerCategory, winnerMatchClass, occurrences, totalOccurrences, documents, matchedTexts}]}`. The Values another method claims in EVERY place they occur, so a card can warn BEFORE the run rather than the user finding out on the results screen. Only FULL coverage is reported, so `occurrences == totalOccurrences` always holds: a value covered in some places and free in others still gets its own placeholder where nothing covers it, which is neither a leak nor an action. `matchedTexts` is the literal text the winner actually covered, in document order, and is ABSENT when that is the value's own text; it exists because `value` is the canonical main text, and a person covered inside `pierre.dupont@coca.us` is covered as the fragments `pierre` and `dupont`, which the full name's spelling never matches there. `matchClass` is the engine-internal precedence input; the frontend turns it into the NAME of the winning method (`copy.js WORKSPACE.matchClassLabel`) and never prints a rank. Mutates nothing (no placeholder minted, registry untouched), so it is safe to call on every edit. An empty list is the normal answer, not an error |
| `validatePattern(expr)` | regex | `""` (valid) or the error message |
| `patternMatches(expr)` | regex | up to 20 sample matches across the loaded documents, shown live under the pattern field: a regex that compiles and matches nothing is the common mistake |

### The unified Suggestion

`DetectionResult.suggestions` is ONE list for every method. Each row is:

```json
{
  "mainText": "Pierre Dupont",
  "category": "person_names",
  "spellings": ["Dupont"],
  "count": 3,
  "contexts": ["Contact Pierre Dupont for approval."],
  "confidence": 0.9,
  "discoveryMethods": ["signal", "heuristic"],
  "evidence": [
    {
      "kind": "email_local_part",
      "signalCategory": "email",
      "signalText": "pierre.dupont@tpps.com",
      "documents": ["engagement.md"]
    }
  ]
}
```

One list, not one per route, and that is a data-integrity decision rather than a
tidiness one: with a list per route the frontend had to map each into its own
shape, and the mapping for the Local AI route rebuilt the row as
`{text, category}` and dropped the folded spellings on the floor. A row says which
methods found it, so route membership is a property of the row.

- `discoveryMethods` is a SET drawn from `engine.AllDiscoveryMethods`
  (`manual`, `signal`, `heuristic`, `local_ai`), mirrored by `state.js
  DISCOVERY_METHODS` and guarded by `../detection_parity_test.go`. Several
  methods can find the same thing, and two routes agreeing is corroboration
  worth showing rather than a fact to overwrite. Built-in and custom pattern
  matching never appear: they produce direct matches, never Suggestions.
- `evidence` is STRUCTURED and bounded, one entry per relationship, with the
  document list capped. The engine never returns prose: the sentence is built in
  `copy.js` from `evidenceKindLabel`, so the copy stays checkable and the copy
  guards can reach it.
- `spellings` are the longer forms `engine.FoldValueFamilies` folded in.
  Accepting the row carries them across, so ONE Value with its spellings reaches
  the pipeline rather than two rivals, the shorter of which would fire inside the
  longer and leave the rest of the phrase in clear text.
- Merging is ONE rule, `engine.MergeSuggestions`, used by every producer:
  case-insensitive dedupe of `mainText` WITHIN a category, summed counts, and
  unioned spellings, contexts, methods and evidence. `state.js
  addSuggestions` mirrors it, so a second run that finds a row again updates it
  rather than being dropped as a duplicate.

Shared evidence makes two rows RELATED, never one Value: `state.js relatedTo`
computes it and the row carries a note. Two organisations reached through one
email domain may genuinely be two legal entities, and one placeholder for two
companies would make the mapping CSV state they were the same one.

## Values, placeholders and removals (the Anonymise step)

These are the surface behind the Anonymise step's **Replaced values** table: one
row per Value the session replaced, editable placeholder, remove action, and a
collapsed removed list with restore.

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `valuePlaceholders()` | — | `[{original, placeholder, category, count}]`, sorted by category then placeholder number. The source for the table, read from the REGISTRY rather than derived from report text, so a row cannot exist that the edit behind it has no entry for. Empty before the first run, which is an empty table, not an error |
| `setValuePlaceholder(current, next)` | `[NAME_N]`, `[NAME_N]` | resolves on success; REJECTS when the shape is wrong, when `current` is not a placeholder this session assigned, or when `next` already belongs to another value (two originals behind one placeholder makes the key ambiguous). Takes effect on the NEXT run, never retroactively |
| `removeValue(placeholder)` | `[NAME_N]` | resolves to `{mainText, category, placeholder, spellings}`. Removal prunes the registry entry AND records a session exclusion, so the Value stays gone across re-runs and same-format exports. The NUMBER is not freed. Does not re-run: the caller re-runs |
| `restoreValue(placeholder)` | the placeholder it USED to have | resolves on success; REJECTS when nothing by that placeholder was removed. The value returns on the next run with a NEW number, because the old one stays retired |
| `listRemovedValues()` | — | `[{mainText, category, placeholder, spellings}]` for the collapsed removed list |
| `validateValues(request)` | `{values, patterns, allowTerms}` | `{blocking, warnings}`, each `[{kind, severity, message}]`. Blocking conflicts refuse the run, so this is what a screen calls to say so before the user presses it |

## Run screen (pipeline)

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `runPipeline(request)` | request | resolves immediately; results arrive on the `pipeline:done` event, progress on `pipeline:progress` |
| `cancelPipeline()` | — | aborts the in-flight run |
| `fastRerun(request)` | request | re-runs the deterministic passes; resolves directly to fresh `Results`. "Fast" is not a reduced mode: Anonymise runs NO discovery method at all, so this differs from `runPipeline` only in being synchronous and reusing the session registry |
| `getMapping()` | — | placeholder → `{original, category}` lookup (empty before the first run) |

The run `request` is
`{values, allowTerms, patterns, categories, suppressRegexPII}`. Each Value is:

```json
{
  "category": "person_names",
  "mainText": "Pierre Dupont",
  "spellings": ["Dupont"],
  "spellingPolicy": "automatic",
  "discoveryMethods": ["signal", "heuristic"],
  "evidence": [{ "kind": "email_local_part", "signalCategory": "email",
                 "signalText": "pierre.dupont@tpps.com", "documents": ["mail.md"] }]
}
```

- `spellingPolicy` is `"automatic"` or `"curated"`. Curated means main text plus
  exactly the listed `spellings` IS the complete replacement set and Go derives
  nothing, so the chips on the card are what the run replaces. Absent reads as
  automatic, so a producer that never sets it cannot freeze a Value's spellings by
  accident.
- `discoveryMethods` is PROVENANCE and travels intact. Go reduces the set to ONE
  match class (`engine.MatchClassForMethods`, taking the strongest) when an
  overlap has to be decided, and reduces it nowhere else. Precedence order is
  `built_in_pattern`, `user_defined`, `smart_discovered`, `local_ai_discovered`;
  lower wins, and an unknown or empty set ranks with `user_defined`, so a producer
  that states nothing is trusted rather than silently demoted. `matchClass` is
  never user-editable state and the frontend never writes it onto a Value.
- `evidence` travels too, because it is what lets a session file explain a Value
  after a reload.
- Confidence is a THIRD, separate thing, feeding the `minConfidence` floor. With
  one field doing precedence as well, raising the floor silently reordered which
  route won.

`suppressRegexPII` is the Built-in patterns switch inverted
(`!useBuiltInPatterns`): when true, Go skips pass 1 so NO signal category is
replaced, whatever `categories` selects; the Value, custom-pattern and code
passes are unaffected.

**Anonymise creates no Value.** No discovery method runs during a pipeline run and
Ollama is never reached, so nothing can be replaced that the user did not accept
on Identify. A run that could mint a Value the user never saw would walk past the
review gate rather than enforce it.

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
| `saveSession(request)` | request | persists the session (Values, the never-anonymise list, patterns, settings, registry, the removal list and the spent placeholder numbers). Warn the user first: the file contains the re-identification key |
| `loadSession()` | — | the `Session` object, or `null` when the user cancels |

The session file is **schema version 8** and nothing else is accepted. Its shape
is `{version, values, allowTerms, patterns, settings, registry,
placeholderOverrides, removedValues, retiredPlaceholders}`. A file of any other
version is REFUSED with an actionable message naming which direction the mismatch
goes, and never migrated: a session file holds the re-identification key, and a
half-migrated one silently reassigns placeholders. There is no migration table and
no compatibility alias anywhere in the loader.

## Runtime events (push, not request/reply)

Subscribe with `onEvent(name, handler)` (returns an unsubscribe function; a
missing runtime is a safe no-op).

| Event | Emitted when | Payload |
|---|---|---|
| `documents:changed` | after a drag-drop import (drops are push, not request/reply) | `ImportResult` |
| `pipeline:progress` | during a `runPipeline` run | progress info |
| `pipeline:done` | when a `runPipeline` run finishes | `Results` |
| `detection:progress` | during a `runDetection` run | `{phase, phaseIndex, phaseCount, docIndex, docCount, docName, chunkIndex, chunkCount, fraction}` |
| `detection:done` | when a `runDetection` run finishes, is cancelled, or has nothing to run | `DetectionResult` (one `suggestions` list) |
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

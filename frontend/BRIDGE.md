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
| `probeOllama()` | — | `{available, models, detail, model}`. Never rejects for "Ollama missing" — that is a normal state in the object. |
| `listOllamaModels()` | — | installed model names `[string]` |

**`model` is the model a run will actually post to, and it is never a name the
probe did not just see.** Go answers `OllamaState {status, model}`; `api.js`
flattens the two into one object (`flatProbe`) so the split stops there, and
`state.js adoptProbe` is the one place a probe result reaches the store, taking
`model` into `settings.model`.

The resolution is an APP decision, not the client's, because it reads the stored
settings: the preference order is the user's stored choice, then
`ollama.DefaultModel`, then the first model installed. The pin is a documented
preference and not an installed fact, so it cannot outrank a choice the user made
and it cannot be posted to a server that does not have it. A model name that is
not installed fails at the very END of a run the user already waited for, and it
arrives as a per-file detection problem rather than as the configuration mistake
it is.

`model` is EMPTY only when there is nothing to run (no reachable server, or a
server with no models installed), and a probe that FAILED changes nothing: an
unreachable Ollama says nothing about which models exist, so it must not throw
away a choice. The rail's `<select>` marks exactly one option selected whenever
models exist, for the same reason: with nothing marked the browser picks the
first by itself and the effective model is decided by the server's tag ordering.

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

`DocumentInfo` also carries `pageCount`: how many units the LOCAL LLM scan scope
can address for this document (`PageRangeMarkdown` is 1-based inclusive over
them). For PDF and DOCX it counts the explicit per-page texts held at ingestion;
for a flat CSV/XLSX sheet it is the row count, for PPTX the slide count, for
TXT/MD the line count; a complex sheet or a single-unit document reports 1. The
Local LLM discovery section sizes its From/To range inputs from it.

## Settings

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `applySettings(settings)` | settings object | the fresh probe result, in the same flat `{available, models, detail, model}` shape `probeOllama()` gives; rejects with an actionable message on bad input. It re-resolves the model because a settings write can change the PORT, so which models exist afterwards is not what the last probe saw |

There are THREE detection routes, and the settings say so directly: the WIRE
CONTRACT is exactly three booleans plus the nested `signalSuggestionSources`, and
there is no derived section flag on either side of the bridge.

**Local LLM discovery** is one switch, `useLocalLLM`. Off by default and
additionally gated on the live Ollama probe, so a stale `true` can never start a
model that is not running.

`llmStrictFormat` is the same route's reply-format choice, off by default: on, the
DISCOVERY request asks the model to answer for every category (a JSON Schema in
`format`); off, it asks for loose JSON mode. It changes recall and time and
nothing else, and the two directions are both real: the schema found a little more
on a short dense page and on very small documents, cost about twice the wall clock
on a slide-heavy deck for no more values, and returned nothing at all on a 0.8B
model. It is a `*bool` in Go so a session file can tell "absent" from "switched
off"; absent reads as OFF, which is the default, so the rail sends the boolean
EXPLICITLY rather than omitting it. It does NOT reach the CLASSIFICATION call,
which is always schema-constrained: that call files a bounded list of names, where
"every category present" is what makes the re-filing complete.

`llmDetailLevel` is how much text one request of the same route carries. One of `engine.AllDetailLevels` (`"thorough"`, the default, or
`"faster"`), mirrored by `state.js LLM_DETAIL_LEVELS` and guarded by
`../detection_parity_test.go`. `applySettings` REFUSES a level Go cannot size and
names the two valid ones; the EMPTY string is accepted, because absence has a
documented meaning (thorough) and is what a session file written before the
setting existed carries. Go fills it out to `thorough` when storing, so
`getSettings` always answers with a level the rail's dropdown can mark selected.

There is deliberately no "whole document in one request" level: it measures zero
values on every model tried, and a choice whose outcome is "finds nothing" is a
broken switch rather than an option.

The two OFFLINE mechanisms are one setting each, and signal-based discovery is a
nested map of readings:

| Setting | What it does | Default |
|---|---|---|
| `useBuiltInPatterns` | MASTER over the structured signal categories (email, VAT, IBAN, amount, date, …). Off means pass 1 is skipped and no signal category is replaced, whatever `categories` selects; the selection is left intact. Produces DIRECT MATCHES, never Suggestions. The rail labels it **Built-in patterns**. | on |
| `useHeuristicDiscovery` | Heuristic discovery: spelling, context, frequency and deterministic gazetteers. Produces SUGGESTIONS. The rail labels it **Heuristic discovery**. | on |
| `signalSuggestionSources` | `{email: {"email.person": bool, "email.organisation": bool}}`, keyed by `engine.AllSignalSources` and then by `engine.SignalDerivations[source]`. Which READINGS of which built-in signals may be used as EVIDENCE to derive Suggestions. Produces SUGGESTIONS. | every reading on |

Each of the three route switches is its OWN persisted boolean, and the rail's
three header switches write one each. There is deliberately no fourth boolean
summarising them, because a stored section flag can disagree with the mechanisms
it claims to summarise, and a section reading "On" while nothing it names runs
lies about what a run does.

`signalSuggestionSources` is keyed by source AND by DERIVATION, because one signal
supports several readings through several mechanisms: an address's local part is
evidence for a person (`email.person`), its domain for an organisation
(`email.organisation`), and wanting one without the other is a reasonable thing to
want. Each reading is switched on its own; a source has no boolean of its own, and
the rail DERIVES the signal's master state from its readings (on when any is on),
for the reason a route switch is a real flag rather than a summary: a summary that
can disagree with what it summarises lies about what a run does.

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

`categories` is the per-category selection, and it is the ONLY thing the pipeline
reads about which categories are on. An absent or empty map means
`engine.DefaultSelection(country)`, the depth Standard presets in both scopes.

`presets` records which preset each chip ROW is on, keyed `"<scope>.<family>"`
(`engine.PresetKey`; the scopes are `patterns` and `names`, the only family today
is `depth`, and the depth IDs are `soft`, `standard` and `thorough`). Flat rather
than nested so a family added later needs no schema change.

It is a RECORD, never an instruction. Nothing in the pipeline reads it: the rail
DERIVES it from `categories` (`state.js activePresets`, mirroring
`engine.MatchingPresets`) and sends both in one payload, so Go can never hold a
preset that disagrees with the selection it was given. A row whose selection
matches no preset contributes NO KEY, which is how "Custom" is representable at
all; an empty map is therefore valid and means Custom on every row.

`applySettings` REFUSES an unknown scope, family or preset ID and names what is
valid, for the reason it refuses an unknown signal derivation: stored, the key
would be one nothing reads for the rest of the session.

The preset TABLE itself is `engine.AllPresets`, mirrored by `state.js PRESETS`
and guarded by `../preset_parity_test.go`: the same rows, in the same ORDER
(which is both the chips' display order and the first-match rule behind the
derivation), filling the same categories. A preset writes only the categories of
its own scope, which is what stops a chip in one rail section from changing a
checkbox in another.

## Allowlist (never-anonymise terms)

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `defaultAllowlist()` | — | the suggested never-anonymise terms. NOT added to the list automatically: the allowlist starts empty and the user chooses its terms. Kept as the source for the downloadable template. |
| `importAllowlistCSV()` | — | parsed terms, or `null` when the user cancels the dialog |
| `saveAllowlistTemplate()` | — | saves the downloadable CSV template |
| `definedTerms()` | — | `DefinedTerm[]`, rows of `{term, idiom, document}`: the vocabulary the imported documents DECLARE about themselves, read at detection time and enforced through the allowlist. `idiom` is `"means"` (the dictionary form, `"Work Order" means ...`) or `"parenthetical"` (the inline form, `(the "Dedicated Advisors")`). It is a SEPARATE list from the user's own terms, because deleting a term the user typed is not the same gesture as dropping a definition the application read out of a file, and it is SHOWN because a suppression the user cannot see is one they cannot lift |
| `forgetDefinedTerm(term)` | the term | the `DefinedTerm[]` that remains. Stops honouring ONE definition, so the value it was hiding can be suggested again. Matching is case-insensitive, which is how the allowlist matches it |

## Identify: detection and the Value surface

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `runDetection(fileNames, allowTerms, llmScope)` | names, allowlist, optional `LLMScope {docName, pages}` (null = every document whole; restricts the LOCAL LLM route only; `pages` is a 1-based `number[]` over the document's own page/slide/row/line units, and an empty array means the whole selected document) | `DetectionResult {suggestions, phases, skipped, errors, cancelled, status, llmRequests, llmSilentRequests, llmTruncatedRequests, llmSecondsPerRequest, patternMatches, patternCategories, builtInPatternsOn}`. THE detection entry point: Go runs every switched-on route under one cancellation context. A cancelled run resolves with the partial findings and `cancelled: true`; only a failure to START rejects (no matching documents, a run already in flight). An out-of-range or unknown-document scope is reported in `errors`, not rejected. |
| `estimateLLMRequests(fileNames, llmScope)` | names, optional `LLMScope` | how many model requests the current scope and DETAIL LEVEL imply, as a number. Reaches no model, probes nothing and mutates nothing, so it is safe to call on every edit. Go computes it with the SAME helper the run uses, which is what makes it equal to the request count the run then makes: a read-out predicting something else is worse than none. Rejects only when there is nothing to estimate (no matching documents); a scope naming pages that do not exist resolves to what the run would actually send, which for that document is zero |
| `cancelDetection()` | — | aborts the in-flight run, reaching whichever route is running, including mid-file |
| `expandSpellings(value)` | `{category, mainText, spellings, spellingPolicy}` | the forms this Value matches, longest first. `spellingPolicy: "curated"` means the list is the user's: Go derives nothing and returns the main text plus exactly the spellings it was given, so the chips on the card are what the run replaces |
| `countTermMatches(term)` | term | `{count, documents}`, the live read-out under the manual declaration row (debounced) |
| `checkIntersections(request)` | `{values, patterns, allowTerms, categories, suppressRegexPII}` | `{intersections: [{value, category, matchClass, winnerValue, winnerCategory, winnerMatchClass, occurrences, totalOccurrences, documents, matchedTexts}]}`. The Values another method claims in EVERY place they occur, so a card can warn BEFORE the run rather than the user finding out on the results screen. Only FULL coverage is reported, so `occurrences == totalOccurrences` always holds: a value covered in some places and free in others still gets its own placeholder where nothing covers it, which is neither a leak nor an action. `matchedTexts` is the literal text the winner actually covered, in document order, and is ABSENT when that is the value's own text; it exists because `value` is the canonical main text, and a person covered inside `pierre.dupont@coca.us` is covered as the fragments `pierre` and `dupont`, which the full name's spelling never matches there. `matchClass` is the engine-internal precedence input; the frontend turns it into the NAME of the winning method (`copy.js WORKSPACE.matchClassLabel`) and never prints a rank. Mutates nothing (no placeholder minted, registry untouched), so it is safe to call on every edit. An empty list is the normal answer, not an error |
| `validatePattern(expr)` | regex | `""` (valid) or the error message |
| `patternMatches(expr)` | regex | up to 20 sample matches across the loaded documents, shown live under the pattern field: a regex that compiles and matches nothing is the common mistake |

### What the built-in patterns matched (read-only)

`DetectionResult` also carries built-in pattern matching's **preview**, which is
the answer to a question the review list cannot answer: a built-in pattern
produces DIRECT matches, applied without review, so its findings never appear as
Suggestions and until this preview existed the only way to check which signal
categories were actually on was to anonymise the whole batch and read the result.

| Field | Meaning |
|---|---|
| `patternMatches` | `[{category, text, count, documents, confidence}]`, one entry per DISTINCT matched text, aggregated over the whole batch. `documents` names the files it occurs in, without repeats; `confidence` is the LOWEST any occurrence scored, so a failed corroborating checksum shows instead of being averaged away |
| `patternCategories` | the signal categories that actually ran: switched on AND applicable to the document country, in the engine's own order. It is reported beside the matches because "found nothing" and "never ran" are different facts and only the second is actionable |
| `builtInPatternsOn` | the Built-in patterns master switch as it stood when the run started. Off means every signal category was silent whatever the category switches said |

These are NOT Suggestions and must never be turned into any: nothing here enters
the review list, and nothing here affects the Identify to Anonymise gate, which
exists for unreviewed suggestions. The frontend keeps them in
`state.builtInPatterns` (null before the first run, which is a different sentence
for the tab to show than an empty list) and renders them on the read-only
**Built-in patterns** tab, with no accept, no reject and no edit on a row. The run
detects again for itself, so the preview is never an input to anonymisation.

The preview runs even when every DISCOVERY route is off, because ticking "street
addresses" and pressing Run detection is a complete question in itself; answering
it only when some unrelated switch happened to be on would make the answer
depend on that switch. In that case `phases` is empty and `status` says where the
matches are.

### What the local model did, and did not say

`DetectionResult` carries four numbers about the LOCAL LLM route, and they exist
because **"0 suggestions" means two different things and only one of them is about
the document**. A model that answered nothing fifteen times reads exactly like a
document with no names in it, and the user gets no hint that another model or a
smaller slice would change the answer.

| Field | Meaning |
|---|---|
| `llmRequests` | how many requests the route sent, across every document it read. Zero when the route did not run |
| `llmSilentRequests` | how many of those parsed cleanly and yielded NOTHING, counted after the hallucination filter, because a reply of three invented names told the user nothing |
| `llmTruncatedRequests` | how many of those were still answering when the model hit its generation cap. Counted APART from the silent ones, never folded into them: a silent request found nothing, a cut-off one found more than it was allowed to finish listing, and only the second means values may be missing from a page that did return some |
| `llmSecondsPerRequest` | MEASURED, not estimated: the phase's wall clock divided by its requests. It is what lets a user judge a scan on their own machine and their own document, which no fixed sentence in a tooltip can do |

Most requests returning nothing is NORMAL, so only an ALL-silent phase adds a
message to `errors`, and that message names the MODEL, which is the actionable
half. `status` names the request count whenever the route ran, so the one-line
summary distinguishes the two cases by itself. The frontend keeps the four
numbers in `state.lastLLMScan` and shows them as the Local LLM discovery section's
`.rail-readout`; the backward reset for Identify clears them, because they
describe a run that reset discards.

A reply that Ollama cut off at the generation cap (`done_reason: "length"`) is
reported as TRUNCATION rather than surfacing as "the model's reply was not the
expected JSON object". The user can act on one of those and not the other.

Truncation degrades ONE SLICE and ends nothing. What the model finished writing
before the cut is salvaged, the slice is counted in `llmTruncatedRequests`, and
the scan carries on to the next slice; the run then reports, per document, how
many of its requests ran out of room and what to do about it. Salvage is safe
because the hallucination filter drops anything that does not occur verbatim in
the source, so a half-written name cannot reach the review list. Aborting the
document instead is what made one dense page leave every page after it unread,
while the run presented a fraction of the document as though that were all of
it. The remedy the message names is scanning fewer pages at a time or trying
another model: it must never name the detail level, which is already the
default.

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
shape, and the mapping for the local model route rebuilt the row as
`{text, category}` and dropped the folded spellings on the floor. A row says which
methods found it, so route membership is a property of the row.

- `discoveryMethods` is a SET drawn from `engine.AllDiscoveryMethods`
  (`manual`, `signal`, `heuristic`, `local_llm`), mirrored by `state.js
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
- `confidence` is a THIRD thing beside provenance and precedence, and it is what
  the Configure rail's **Minimum confidence** acts on. A local model finding carries
  `engine.ConfidenceLLMDefault` (0.8), stamped at the Ollama boundary beside the
  `local_llm` method. `0` means NOT STATED, which the engine reads as a user
  declaration and scores at `ConfidenceManualDefault` (0.95). The number must
  survive the whole way across: `addSuggestions` keeps it on the row,
  `valueFromSuggestion` carries it into the Value, and `addValues` stores it.
  Dropped anywhere along that chain, an accepted model finding is scored as if
  the user had typed it, and raising the floor past 80 stops doing what the
  control says.
- Merging is ONE rule, `engine.MergeSuggestions`, used by every producer:
  case-insensitive dedupe of `mainText` WITHIN a category, summed counts,
  unioned spellings, contexts, methods and evidence, and the STRONGEST
  confidence. `state.js addSuggestions` mirrors it, so a second run that finds a
  row again updates it rather than being dropped as a duplicate, and a row two
  routes found is not demoted to the weaker route's score.

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

## Anonymise: images

The Anonymise step's IMAGE half reads these five. They answer about the
**imported** document and need no run: the pictures live in the bytes captured
at import, and the user reviews them before as well as after the text is
anonymised.

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `listDocumentImages(name)` | imported document name | `Inventory {applicable, reason, assets, warnings}`. Each asset carries its current `decision`. Rejects only for a document that is not imported, or a file that cannot be read as an archive |
| `imageThumbnail(docName, assetId, maxPx)` | asset ID from the inventory; `maxPx` 0 asks for the default | `{dataUrl, width, height}` |
| `setImageDecision(docName, assetId, decision)` | a `Decision` (below) | resolves on success; REJECTS a decision the picture cannot carry, naming the reason and the way out of it. A `keep` CLEARS the stored decision |
| `previewImageTreatment(docName, assetId, decision, maxPx)` | the decision to try, not to record | `{dataUrl, width, height}`: what the export WILL produce. Records nothing |
| `resetImageDecisions(docName)` | imported document name | resolves on success; the "keep them all" bulk action. Rejects only for a document that is not imported. **No view calls it yet**, deliberately: the review screen has no bulk control, because with one decision per picture and every picture starting on `keep`, "undo all of them" is a gesture with no failure to recover from, and a button that clears work the user did needs a confirm of its own. The wrapper and the bound method stay, because the alternative to a documented and tested wrapper is discovering at the point of need that the bound method was never reachable; `api.test.js` keeps it honest |

An **image asset** is one picture FILE inside the document archive, and it is
what a decision attaches to. An **image occurrence** is one PLACE that asset is
used. A logo on five slides is ONE asset with five occurrences, so it is one
row and one question.

```json
{
  "applicable": true,
  "assets": [
    {
      "id": "ppt/media/image1.png",
      "name": "Alpine Trust logo",
      "format": "png",
      "bytes": 26144,
      "width": 120, "height": 80,
      "companion": "",
      "linked": false,
      "occurrences": [
        { "part": "ppt/slides/slide1.xml", "ordinal": 0, "kind": "picture",
          "location": "Slide 1", "displayCX": 1828800, "displayCY": 1219200 }
      ],
      "decision": { "treatment": "keep" }
    }
  ],
  "warnings": ["unreadable_part"]
}
```

- `id` is the archive part path, and it is what identifies an asset across
  calls and across re-imports of the same file.
- `format` is `"png"`, `"jpeg"`, `"svg"` or `"other"`, sniffed from the BYTES
  rather than from the extension: a part named `.png` holding JPEG bytes is
  common enough in real documents to matter.
- `kind` is `"picture"`, `"fill"` or `"background"`: what ENCLOSES the picture,
  which is a separate question from what it is.
- `location` is ready to print, in the document's own words ("Slide 4",
  "Page 2", "Header", "Slide master", "Hidden slide 7", "Notes on slide 2").
  Go builds it because only the scan knows which part an occurrence came from.
- `companion` is the SVG part of an SVG picture, whose `id` stays the PNG
  fallback Office writes beside it, because that is what the relationship
  points at.
- `linked` marks a picture that lives OUTSIDE the file, so there are no bytes
  here to change.
- `decision` is what the user has decided about this picture, or
  `{"treatment": "keep"}` when they have decided nothing. It travels with the
  asset so the screen has ONE call and cannot draw a row whose decision it has
  not read.

### The `Decision` wire shape

```json
{ "treatment": "box", "boxText": "Client logo removed", "blurStrength": 5 }
```

- `treatment` is `"keep"`, `"box"`, `"blur"` or `"remove"`, in that order (the
  order the interface offers them in). Every picture starts at `keep`, so
  nothing is ever "undecided", and `keep` is stored as the ABSENCE of a
  decision: the Go side holds only what the user changed.
- `boxText` is drawn into the rectangle, centred and wrapped. Empty is allowed
  and gives a plain rectangle. At most **120 characters**. The raster box is
  drawn with a built-in bitmap font, so accents are simplified (`é` becomes `e`,
  `ß` becomes `ss`) and anything it cannot draw becomes `?`; the SVG box names a
  real font family and keeps the text as typed.
- `blurStrength` is **1 to 10**, relative to the picture's own size, so the same
  number means the same amount of destruction on a 60-pixel icon and on a
  4000-pixel screenshot. ABSENT reads as the default (5), never as "none".
  Strength 1 is deliberately weak.
- Both extra fields are omitted when they carry nothing, so a `remove` is
  `{"treatment": "remove"}`.

**Not every treatment is offered for every picture**, and `setImageDecision`
rejects the ones that are not, so the interface must disable rather than let the
refusal arrive at export time:

| Picture | Offered |
|---|---|
| PNG or JPEG | all four |
| SVG | `keep`, `box`, `remove`. **No blur:** a blur filter leaves every original shape and every original text string inside the file, so a control that did it would be labelled "anonymise" while anonymising nothing |
| any other format (emf, wmf, gif, tiff, bmp) | `keep`, `remove`. The application cannot redraw it |
| `linked` | `keep`, `remove`. There are no bytes here to change |

A decision is attached to the ASSET and applies to every place it appears, so a
logo on five slides is one question and one answer.

**`applicable: false` is an ANSWER, not a failure.** `reason` is a CODE the
frontend maps to its own copy, never a sentence: `"pdf_images_removed"` (the
PDF export regenerates the file from text, so a source PDF's pictures are
already absent from everything the application writes) and
`"format_not_supported"` (xlsx, csv, txt, md). `warnings` are codes too:
`"unreadable_part"` and `"linked_images"`.

**An SVG preview is rendered through `<img src="data:image/svg+xml;base64,...">`
and never inlined into the page as an `<svg>` element.** An `<img>` context
executes no script and an inlined element does. Raster previews are thumbnailed
in Go with a box filter and arrive as PNG whatever the source encoding was.

The inventory is cached per document on the Go side, because the screen asks on
every repaint. The previews are NOT: they are the largest thing this feature
holds, and keeping every one of them for a two-hundred-picture deck is how a
desktop application starts swapping.

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
  `built_in_pattern`, `user_defined`, `rules_discovered`, `local_llm_discovered`;
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

**Anonymise runs no discovery method and reaches no model.** No discovery
method runs during a pipeline run and Ollama is never reached: nothing can be
replaced that the user did not accept on Identify or DECLARE while reviewing
the result on Anonymise (the Compare pane selection, or the "Add missed Value"
card). A declaration is the user acting, so it passes the review gate by
definition; what the gate forbids is an unreviewed MACHINE finding reaching
the text. A Value declared on Anonymise is a first-class Value: it reaches
the registry, the report, the Replaced values table, the mapping and the
session file exactly like one accepted on Identify.

## Export screen

| `api.js` wrapper | Args | Resolves to |
|---|---|---|
| `exportDocumentFormats(name)` | name | offered extensions (default first) for one result document |
| `saveDocument(name, ext)` | name, ext | opens a save dialog for one document |
| `getSameFormatMetadata(name, ext)` | name, ext | `{fields, filename, images?}`: document properties with proposed replacements, the proposed anonymised filename, and what this save will do to the document's PICTURES, for the review panel |
| `saveSameFormat(name, ext, fields, filename)` | name, ext, reviewed fields, filename | writes the same-format copy with the REVIEWED metadata and filename |
| `chooseExportFolder()` | — | opens the native FOLDER picker and resolves to the chosen path, or `""` when cancelled. Picks only; writes nothing (BUILD-05 Phase 3) |
| `exportAllZipTo(dir)` | folder path | writes the batch zip into that folder with NO second dialog and resolves to the full path written. The only dialog-free write in the contract, allowed because the folder was chosen explicitly and the zip carries no re-identification key (decision 4). An existing archive is never overwritten, the new one is numbered |
| `copyDocument(name)` | name | puts the anonymised text on the clipboard |
| `copyText(text)` | the selected text | puts an arbitrary short string on the clipboard, for the Compare pane's selection panel. Rejects with an actionable message on an empty selection or one over 4096 bytes, which is a mis-drag guard: a drag that ran away down the pane must not push a whole document through the clipboard |
| `exportMapping(format)` | `"csv"`/`"json"` | saves the re-identification key. Call ONLY after the user confirmed the sensitivity warning |
| `exportReport(format)` | `"json"`/`"md"` | saves the run report, INCLUDING the per-value table (BUILD-06). That table maps placeholders back to real values, so the exported report is a re-identification key: warn before writing it, as for `exportMapping`. |
| `saveSession(request)` | request | persists the session (Values, the never-anonymise list, patterns, settings, registry, the removal list and the spent placeholder numbers). Warn the user first: the file contains the re-identification key |
| `loadSession()` | — | the `Session` object, or `null` when the user cancels |

`images` is ABSENT for a format with no image review (`.pdf`, `.xlsx`) and for a
document with no pictures, and the review panel then says nothing about pictures
at all: a line reading "0 images" on a PDF would contradict the IMAGE tab, which
says a PDF export has already dropped every picture. When present it is
`{kept, boxed, blurred, removed}`, counted per ASSET, and the panel states it one
line above the button that writes the file. The all-kept case is the one that
earns the line: a user who never opened the IMAGE tab has decided nothing, and
this is the last surface that can tell them the pictures are leaving exactly as
they arrived.

The exported REPORT carries the same answer in full. Its JSON gains an `images`
key, one entry per document that has pictures, shaped
`{document, kept, anonymised: [{asset, name, locations, treatment, boxText?}]}`,
and its markdown gains a "Pictures" section. The anonymised pictures are listed
and the kept ones counted, because a list of everything left alone is noise while
the count is what tells a reader "no pictures" from "pictures, all kept". It names
no original value, so it is not part of the re-identification key and needs no
warning of its own.

The session file is **schema version 9** and nothing else is accepted. Its shape
is `{version, values, allowTerms, patterns, settings, registry,
placeholderOverrides, removedValues, retiredPlaceholders, imageDecisions}`. A file
of any other
version is REFUSED with an actionable message naming which direction the mismatch
goes, and never migrated: a session file holds the re-identification key, and a
half-migrated one silently reassigns placeholders. There is no migration table and
no compatibility alias anywhere in the loader. Version 8 is refused like every
other: a v8 file carries no image treatments and a v8 READER ignores the field, so
either way round the file loads, nothing errors, and the exported document ships a
picture the user had redacted.

## Runtime events (push, not request/reply)

Subscribe with `onEvent(name, handler)` (returns an unsubscribe function; a
missing runtime is a safe no-op).

| Event | Emitted when | Payload |
|---|---|---|
| `documents:changed` | after a drag-drop import (drops are push, not request/reply) | `ImportResult` |
| `pipeline:progress` | during a `runPipeline` run | progress info |
| `pipeline:done` | when a `runPipeline` run finishes | `Results` |
| `detection:progress` | during a `runDetection` run | `{phase, phaseIndex, phaseCount, docIndex, docCount, docName, chunkIndex, chunkCount, unitFrom, unitTo, unitWord, fraction}` |
| `detection:done` | when a `runDetection` run finishes, is cancelled, or has nothing to run | `DetectionResult` (one `suggestions` list) |
| `detection:error` | when a run stops unexpectedly | `{message}` |

`phase` is one of `backend.AllDetectionPhases`: **`rules`**, the offline
discovery half (heuristic discovery and signal-based discovery run in one pass
over one document, so naming the phase after either half would describe half of
what it did), and **`local_llm`**. The frontend maps the token to the words the
user reads in `copy.js phaseName`, and `detection_parity_test.go` holds the two
lists together: an unhandled token falls through to "Starting", so a run in
flight would report itself as one that has not begun, and nothing errors to say
so.

**`detection:done` / `detection:error` are a guarantee, not a courtesy.** Exactly
one of them fires for every started run, so the progress bar can be cleared by
the event rather than by the caller's `finally`. `fraction` is the whole run's
progress, computed in Go and non-decreasing across routes: never recompute a
percentage per route in the frontend, that is what made the bar rewind when the
second route started with a smaller file count.

On the LOCAL LLM route the model reads a document in slices aligned to the
document's own units, one request each, so `chunkIndex` and `chunkCount` are the
request number and the request count for that document's scan. `unitFrom`,
`unitTo` and `unitWord` say which of the document's OWN units the current request
covers, so a caption can read "slides 4 to 6 of 15" in the same word the import
list uses; `unitWord` is SINGULAR and the frontend pluralises, exactly as
`DocumentInfo.unit` works. All three are zero and empty on the `rules` phase,
which sends no requests.

## Rules for changing the contract

- New backend data for the UI = a new exported method on `App` in
  `../backend/app*.go` + a new wrapper in `api.js` + a row here.
- Never call `window.go` / `window.runtime` outside `api.js`.
- Keep method names and shapes in sync across `api.js`, the Go methods and
  this table.

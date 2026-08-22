# CLAUDE.md — backend/ (Go engine + Wails bound-app layer)

Purpose of this file: it is the **backend charter**. Claude Code auto-loads
the nearest `CLAUDE.md` up the tree for the files it edits, so working in
`backend/` loads this and keeps backend sessions scoped. The repo-root
`CLAUDE.md` stays authoritative for cross-cutting product rules; this file
owns the backend detail.

## What this folder is

All Go business logic and the Wails bound-app layer, as **package `backend`**
(plus the sub-packages `engine`, `engine/convert`, `engine/exportfmt`,
`engine/imaging`, `engine/ooxml`, `ollama`). Pure Go, **no CGo, ever** (pattern
P0). Standard library first; no new dependency without adding it to the
pinned-versions table in the root `CLAUDE.md`.

The one thing that is NOT here: `main.go`, the `//go:embed all:frontend`
directive and `wails.json` stay at the repo root, because `go:embed` cannot
reference a parent directory, so the file that embeds `frontend/` must sit at
or above it. `main.go` imports this package and calls `backend.NewApp()`.

## The bound-app layer (`app*.go`)

- `app.go`, `app_values.go`, `app_detect.go`, `app_export.go`, `app_run.go`,
  `app_images.go` hold the `App` struct — the ONLY seam between the frontend
  and Go. Every method is a thin adapter that delegates straight to `engine/*`
  or `ollama/*`. **No business logic in these files.**
- `App` is the only place allowed to touch user-chosen filesystem paths
  (dialogs, drag-drop): it reads the bytes ONCE and hands `[]byte` + filename
  to the engine. The engine never sees a path. **Originals are immutable** —
  nothing is ever written back to a source file.
- **Binding namespace:** because `App` is in package `backend`, Wails exposes
  its methods to the frontend as `window.go.backend.App.<Method>` (not
  `main`). `Startup` and the `DocumentationAsset` const are exported so the
  root `main` package can wire them. The frontend contract for these methods
  is documented in `../frontend/BRIDGE.md`.

## Engine invariants

- **UI-agnostic:** nothing under `engine/` imports Wails or reads user paths.
  This keeps it headless-unit-testable and keeps the P4 fallback contained.
- **One external boundary:** only `ollama/client.go` constructs HTTP requests
  to Ollama, and only the App calls it, from the Identify-time discovery route.
  `engine/*` never sees the concrete client, so swapping the NER backend is a
  contained refactor. Ollama host is locked to loopback `127.0.0.1:11434` (port
  is settable); never make the host remote.
- **Anonymise runs no discovery method and reaches no model.** `engine.Run` has
  no LLM slot at all: every discovery method runs at Identify time and its
  findings are Suggestions the user accepts. The only Values a run can apply
  are the ones the user accepted on Identify or DECLARED while reviewing the
  result on Anonymise (the frontend's Compare pane selection and "Add missed
  Value" card). A declaration is the user acting, so it passes the review gate
  by definition: the gate exists to stop an unreviewed MACHINE finding
  reaching the text, not to stop the person reviewing the result from fixing
  what the machine missed. `TestAnonymiseNeverCallsOllama` asserts the model
  call count is zero.
- **Graceful degradation:** Ollama is probed at startup and on demand
  (`GET /api/tags`); the deterministic pipeline must be fully usable without
  it. LLM-dependent controls disable with a tooltip when it is absent.

## The Value model

A **Value** is one accepted replacement unit: one category, one `MainText`, its
`Spellings`, one placeholder for the whole family. A **Suggestion** is an
unreviewed potential Value. Nothing becomes a Value without the user accepting
it.

Provenance and precedence are SEPARATE fields, and that is the load-bearing
decision of the model:

- `DiscoveryMethods` is provenance, a SET drawn from `AllDiscoveryMethods`
  (`manual`, `signal`, `heuristic`, `local_llm`). Several methods can find the
  same thing, and accepting a Suggestion keeps all of them, because two routes
  agreeing is corroboration worth showing rather than a fact to overwrite.
- The **match class** is precedence, derived from the methods by
  `MatchClassForMethods` taking the strongest, and consumed only by overlap
  resolution, ownership unification and the pre-run intersection check.
- `Confidence` is a third thing, and it is DATA rather than a filter: it orders
  overlaps after the match class, it feeds heuristic discovery's own floor before
  a Suggestion is shown, and it is reported. Nothing a run replaces is decided by
  comparing it against a threshold, and an accepted Value is never dropped by it.

One field answering two of those questions is what made raising a confidence
floor silently reorder which route won. One field being read as a lever over what
a run replaces is what made a floor drop Values the user had already ACCEPTED, by
the score of whatever originally found them. The single score a user may ask to
have vetoed is a corroborating checksum that did not pass, and that question is a
checkbox (`Settings.RequireChecksum`, `engine.RejectFailedChecksums`), off by
default, applied to pass 1's spans alone.

`SpellingPolicy` (`"automatic"` or `"curated"`) is a string rather than a bool so
both states have a name on both sides of the bridge and in a session file. It is
what makes deleting a spelling STICK without a negative rule: a curated Value's
main text plus its listed spellings IS the complete replacement set. A per-Value
list of suppressed spellings would be a rule with no home in the interface,
invisible except as the absence of a chip and impossible to undo. `Evidence` is
structured and bounded, so the frontend can explain a Value from `copy.js`
instead of the engine returning prose nobody can check.

These files carry the model, and each exists because the rule it enforces cannot
be enforced anywhere else:

- `engine/country.go` — the document country and `CategoryCountries`, the table
  saying which regex categories apply where, plus `CategoryAppliesTo`. It is
  the SINGLE source `frontend/countries.js` mirrors. `piiPattern` carries a
  `countries` field and `DetectPIISelected(text, sel, country)` skips the
  patterns the country excludes, so a German document is not scanned for French
  VAT numbers.
- `engine/conflicts.go` — `ValidateValues`, pure set arithmetic over the
  declarations that reads no document text, so it is cheap enough to run on
  every run and every fast re-run. It runs inside `engine.Run` BEFORE pass 1,
  because the App has two entry points and the engine has one. Blocking
  conflicts abort before the registry is mutated. `Run`'s preamble is ordered
  removals, then validation over what is left, then reservations: a removed
  value stops being a declaration, so validating it as one would refuse every
  run after a removal. The overlap WARNINGS come from
  `ResolveOverlapsWithLosers`, the one place the decision is made, because a
  parallel check can disagree with the pipeline and then describe something
  that did not happen.
- `engine/presets.go` — presets as scoped DATA. A preset is a row in a table
  carrying an ID, a family, a SCOPE and the categories it switches on, and
  `ApplyPreset` is the ONLY writer: it rewrites its own scope and leaves every
  category outside it untouched, which is what keeps a chip under one rail section
  from moving a checkbox under another. `MatchingPreset` derives which preset a row
  reads as rather than storing it, for the reason `SignalSourceEnabled` is derived,
  and where several presets match it prefers the DEFAULT depth, then table order,
  so a row cannot flicker between two presets that fill the same set and a fresh
  session's row names the depth it started on. `DefaultSelection` is the pipeline's fallback and
  the only caller of the both-scopes builder in the application. Nothing here is
  consulted at run time: `CategorySelection` is the authority. Mirrored by
  `frontend/state.js PRESETS`, guarded by `../preset_parity_test.go`.
- `engine/matchclass.go` — the discovery methods, the match classes
  (`built_in_pattern`, `user_defined`, `rules_discovered`,
  `local_llm_discovered`), `MatchClassRank` (lower wins) and
  `MatchClassForMethods`. An unknown or empty class ranks with `user_defined`
  rather than last, so a producer that states none is trusted rather than
  silently demoted: ranking it last turns a forgotten stamp into a missing
  replacement instead of an error. `ResolveOverlaps` compares the class FIRST.
  Mirrored by `frontend/state.js DISCOVERY_METHODS` and `MATCH_CLASSES`, guarded
  by `../detection_parity_test.go`.
- `engine/conflicts.go`'s `ConflictResolution` — the action that CLEARS a
  blocking conflict, stated by the engine rather than inferred by each screen
  from the conflict's refs. The refusal reaches the user on two screens (the
  value's own card on Identify, the refused-run panel on Anonymise) and both have
  to offer the same way out; each inferring it is two places deciding one thing,
  and two answers can disagree. `Fix` stays the sentence a human reads, and a
  conflict with NO resolution is one no single gesture clears, which is honest:
  an ambiguity needs the user to say which of two Values to drop. Mirrored by
  `frontend/state.js CONFLICT_RESOLUTIONS`, guarded by
  `../detection_parity_test.go`.
- `engine/allowlist.go`'s DEFINED TERMS — `DiscoverDefinedTerms` and
  `ApplyDefinedTerms`. A contract declares its own vocabulary, and a phrase
  introduced as `"Work Order" means ...` or `(the "Dedicated Advisors")` is the
  document saying the phrase is part of its machinery. Two idioms and no more:
  the dictionary form alone caught six of nineteen on the measured fixture, and
  adding the parenthetical form caught all nineteen, while a looser shape would
  start suppressing the party names a document introduces with `referred to as
  "..."`. Enforced through the allowlist for the reason removals are, stored as
  its OWN list on the App and in the session file for the reason removals are,
  and SHOWN on the never-anonymise tab because a suppression the user cannot see
  is one they cannot lift. Two bounds are load-bearing: whole-term matching (a
  prefix rule removed "Services NStar") and the required article in the
  parenthetical idiom.
- `engine/signals.go` — `AllSignalSources`, `SignalDerivations` and
  `SignalSourceSelection`: which READINGS of which built-in signals may DERIVE
  Suggestions. The selection is `map[source]map[derivation]bool`, because the two
  questions are nested: a source is a signal the pattern pass matched, a
  derivation is one reading of it through one mechanism, and a flat map of dotted
  keys would let a reading exist with no source above it. Data-driven on purpose,
  so a new reading is one constant and one implementation rather than a new field,
  a new rail row and a new persisted flag. `SignalDerivationEnabled` is the leaf
  question the discovery pass asks; `SignalSourceEnabled` DERIVES the signal's own
  state from its readings (on when any is on) and is never stored, for the reason a
  rail route switch is a real settings flag rather than a summary: a summary that
  can disagree with what it summarises lies about what a run does. It does NOT govern
  whether a signal is matched and replaced, and conflating the two is the mistake
  the separate setting exists to prevent. A nil selection, or a key missing at
  either level, reads as the defaults, never as "none". Mirrored by
  `frontend/state.js SIGNAL_SOURCES` and `SIGNAL_DERIVATIONS`, guarded by
  `../detection_parity_test.go`.
- `engine/signaldiscovery.go` — signal-based discovery: an email's local part
  seeds a person and its domain seeds an organisation, and a WEBSITE's
  registrable domain label seeds an organisation too, each gated on its OWN
  derivation so clearing one leaves the others producing their rows, and each
  seed is searched for across the WHOLE BATCH, because the evidence is in one
  file and the text it points at is usually in another. The website source exists
  because a document need contain no address at all and still name its parties: a
  measured framework agreement carried none, while `www.nstar.lu` sat in it as
  evidence for an organisation whose spelling no derivation rule can produce from
  its own long form. Its URL shape deliberately mirrors `pii.go`'s, so a URL that
  is anonymised is a URL that can be read as evidence. Three rules shape every decision: whole
  batch; nothing suggested from text found only INSIDE the source signal; and
  the DOCUMENT's own casing and accents are what gets suggested. A domain seed
  extends over the capitalised name built on it, and the bare stem is then
  dropped when a longer name exists, because keeping it would let family folding
  collapse two legal entities into one Value.
- `engine/evidence.go` — `Evidence` and `MergeEvidence`. Deduplicated by
  RELATIONSHIP, not by document, so the same relationship found in five files is
  one piece of evidence naming several documents.
- `engine/intersections.go` — `DetectIntersections`, which answers "what covers
  what" BEFORE a run so the warning can sit on the value's own card instead of
  arriving on the results screen. It reuses `detectText` and the shared
  comparator, never a parallel check, because a parallel check can disagree
  with the pipeline and then describe something that did not happen.
- `engine/families.go` — `FoldValueFamilies`: "Coca-Cola" and "Coca-Cola
  company" are ONE Value, and the SHORTER form is the main text. Spellings match
  longest first, so that way the phrase collapses into one placeholder; the other
  way the shorter fires inside the longer and leaks the rest. It folds ONCE over
  the unified Suggestion list, across every method, so a family cannot be split
  by which route found which spelling. `MergeSuggestions` beside it is THE merge
  rule every producer funnels through, which is what makes "nothing is lost"
  checkable rather than hopeful.
- `engine/codes.go` — the offline detector for CODE-SHAPED values, a second
  scanner over the raw text. It is separate from `discover.go` because that
  file's tokenizer treats a digit as a word boundary, so no code shape can
  surface through it, and teaching it digits would change what every other
  detector sees.
- `engine/removals.go` — `RemovedValue`, `ApplyRemovals` and `FilterRemoved`.
  Removals are enforced through `Allowlist.Contains`, the single veto every
  span producer already consults; the App folds them in once, in
  `App.allowlistFor`, which run, detection and export all go through so a
  removal cannot be honoured by one and forgotten by another.

`Registry` owns the one-value-one-replacement invariant through its `byOriginal`
index, and tracks the numbers it will never hand out again: `retired`, where a
`Forget` freed the entry and deliberately not the number. It persists in the
session file, or a save-and-reload frees exactly the numbers the removal refused
to free.

## Pipeline passes (fixed order)

1. Deterministic PII regex pass (`engine/pii.go`). Regexes are compiled once
   at package init and documented with match / deliberately-no-match examples.
   A pattern has three optional gates and the field name says which is which:
   `validate` VETOES on the matched text (a shape check, or a checksum that IS
   the recognizer, as Luhn is over a bare digit run); `checksum` only SCORES
   (`ConfidenceChecksumFailed`), because a mistyped or synthetic bank identifier
   is exactly what must still be anonymised, and `RequireChecksum` is the user
   asking for the veto instead; and `reject` vetoes on the
   SURROUNDINGS, since RE2 has no lookarounds and some rules are about what sits
   in front of a match (a BIC needs its own cue, an IBAN's interior is not a
   payment card).
2. Value pass (`engine/values.go`): the accepted Values, expanded into their
   spellings, longest-match-first. Derivation stops the moment the spelling
   policy goes `curated`: from then on main text plus `Spellings` IS the list,
   which is what makes deleting a spelling stick without a negative rule.
   While it applies, derivation has three classes and a category belongs to
   exactly one: person-style (initials, surname-only, first-name-only,
   hyphen/space), organisation-style (a legal suffix stripped, never added),
   and literal, for the categories with no name structure to derive from.
3. Post-pass: registry re-application across ALL loaded documents so the same
   real-world subject maps to the same placeholder everywhere.

There is no discovery pass here, and that is the point. Discovery happens at
Identify time (`App.RunDetection` over `engine/discover.go`,
`engine/signaldiscovery.go` and `ollama`), every finding is a Suggestion, and
every local model finding passes a **hallucination filter** (dropped unless the
exact string occurs in the source text) and the allowlist before the user ever
sees it.

`Run` executes those passes in THREE phases rather than one loop: detect every
document, then UNIFY OWNERSHIP across the whole batch (`unifyOwnership` picks
each string's owner by `MatchClassRank`), then apply. The split exists because
`Registry.Assign`'s `byOriginal` index gives a string to its first claimant for
the whole session, and with detection and replacement in one step the first
claimant was decided by byte offset within DOCUMENT order: which category a
value ended up under, and therefore its placeholder text, depended on the order
the files were imported in.

Allowlisted terms are never replaced, by any pass. Placeholders are stable
per session, format `[CATEGORY_N]`; the registry maps original → placeholder
and is exportable as the re-identification key.

A user may RENAME one placeholder (`Registry.SetPlaceholder`, BUILD-05
Phase 3). The shape is enforced and a collision is refused, because two
originals sharing one placeholder makes the key ambiguous and silently ends the
ability to reverse the anonymisation. Automatic assignment then skips any number
an override took. The renames a user made are recorded rather than inferred
(`Registry.Overrides`) and persist in the session file; **session files are read
only by the version that wrote them**, so a file whose `SessionVersion` this
build does not know is refused with an actionable message instead of
half-migrated. The current version is **13**, and every older one is refused. The
reason for the move to 13 is that `minConfidence` LEFT the schema and
`requireChecksum` entered it: the two are not readable as each other in either
direction, because a saved floor says nothing about the checksum question, and a
reader finding no floor silently restores the replacement of every accepted Value
the saved floor had been suppressing. The move to 12 was that `level` LEFT the
schema and `presets` entered it: a single level string cannot say that the pattern
categories are at Soft while the name categories are at Thorough, which is a
selection the scoped chips make in two clicks. The move to 10 ran BACKWARDS from
the usual reason: `Registry.Assign`
PANICS on a category with no `placeholderLabels` row, so a v9 file written by a
later build carrying a `country_names` Value would be accepted by an older v9
binary and crash it on the next run, and the bump turns that crash into the clear
"written by a different version" refusal. The move to 9 was for the shape of
failure the rule exists for: a v8 file carries no image treatments and a v8 READER
ignores the field, so either way round the file loads, nothing errors, and the
exported document ships a picture the user had redacted. Version 7 is refused
because its `signalSuggestionSources` holds one boolean per source, which cannot
say which READING of a signal the user wanted, so reading it would mean guessing,
and the guess changes what the next run suggests. A corrupt
key (two entries claiming one value) is refused the same way, as an ERROR:
these functions run behind bound methods on a file the user picked, so
panicking would take the application down on a bad file.

## Pictures (`engine/imaging/`)

A package of its own, beside `convert/` and `exportfmt/`, because BOTH of them
need it and neither may own it: the scan reads the bytes captured at IMPORT and
the treatments write the bytes an EXPORT produces, and the two are deliberately
independent packages. Its whole dependency list is the standard library
(`archive/zip`, `encoding/xml`, `image`, `image/png`, `image/jpeg` and
arithmetic): the owner decided against a new Go module for pictures, so what
this package cannot do with the standard library is something the feature does
not do.

Two nouns carry the model, and keeping them apart is what makes the review one
question per picture:

- an **Asset** is one picture FILE inside the archive (`ppt/media/image3.png`).
  It is what carries bytes and what a decision attaches to.
- an **Occurrence** is one PLACE that asset is used. A logo on five slides is
  one Asset with five Occurrences, and it is `Part` plus `Ordinal` that
  identifies the element, never a byte offset: an export has to be able to
  re-scan its own rewritten bytes and still find the same picture.

`ScanDocx` and `ScanPptx` are one walker and two profiles. The formats differ
only in which parts are walked and what a place is CALLED; a picture is a
DrawingML blip (or the legacy VML `v:imagedata` Word still writes) in both, and
keeping the walk shared is what lets an export find exactly the element the scan
listed. `Format` is sniffed from the BYTES rather than the extension, and
`Measure` reads headers only: answering "what size is this" must not cost what
decoding a forty-megapixel screenshot costs. `Kind` (picture, fill, background)
is what ENCLOSES the blip, a separate question from what the picture is, because
it decides whether removing it can delete an element at all.

`Inventory.Reason` and `Inventory.Warnings` are CODES, never sentences: copy
lives in `frontend/copy.js`, and a sentence returned from Go is a user-visible
string outside the place the interface's copy is reviewed.

The bound layer (`app_images.go`) caches one inventory per document, because the
review screen asks on every repaint, and drops the cache wherever the bytes
behind it can change (import, re-import, removal, session reset). Previews are
NOT cached: they are the largest thing the feature holds.

## Converters (`engine/convert/`) and same-format export (`engine/exportfmt/`)

- Converters are **pure Go and one-way**: binary formats convert TO markdown
  on import (docx, pptx, xlsx via excelize, pdf via the vendored
  aspose-pdf-foss-for-go library — experimental). Only the standard library +
  the pinned libs. PDF extraction goes through the FRAGMENT LINE MODEL
  (`convert/pdflayout.go`): the library's layout extraction returns fragments
  with rectangles, the model splits a line where two fragments merely share a
  baseline and joins a wrapped continuation only when the geometry agrees, and
  the working markdown is DERIVED from the model, so the import's text and the
  export's locations can never disagree about what a line is. The
  ledongthuc-based extractor stays beside it with no production caller, as the
  deep tier's comparison baseline, until the owner's decommissioning gate
  (root `CLAUDE.md` §7).
- `engine/ooxml/` holds the plumbing docx, pptx and xlsx share: pulling a named
  `docProps/` part out of the archive, token-scanning named elements out of an
  XML part, and reading the cached counts (`<Pages>`, `<Slides>`). Both
  `engine/convert/unitcount.go` (the import list's "6 pages") and
  `engine/exportfmt/metadata.go` (the properties review) read through it, so
  the zip walk exists once. An ABSENT part is never an error there: every
  `docProps` part is optional and the repo's own fixtures omit them, so the
  callers degrade (a page count falls back to a line count) rather than
  reporting "0 pages".
- `exportfmt/` writes a NEW anonymised copy in the source format by rewriting
  a copy of the original bytes held in memory. The source file on disk is read
  once at import and never written, moved or modified. If pure-Go PDF quality
  is unacceptable, the recorded fallback is a wazero WASM extractor (P3), not
  CGo.
- **The PDF export is IN-PLACE replacement** (`exportfmt/pdfinplace.go`): the
  produced file is the original's bytes with the pipeline's replacements
  applied through the location ladder (`exportfmt/pdfladder.go`; the rungs,
  the fits-check, the redaction gesture and the refusal are specified in root
  `CLAUDE.md` §5's PDF rules). Three disciplines hold the leak-critical path:
  an occurrence the whole ladder cannot locate REFUSES the export naming the
  .md way out; the save is `RemoveUnusedObjects()` then `WriteTo`, never a
  naked `WriteTo`; and the whole-file leak scan (`exportfmt/pdfscan.go`) runs
  over the produced bytes as a BLOCKING self-check. The regenerated exporter
  (`exportfmt/pdf.go`, fpdf) stays compiled with no production caller until
  the owner's decommissioning gate, and is never a fallback behind the
  refusal.
- **The same-format export makes TWO passes over one part, text first and then
  pictures, deliberately sequential rather than merged.** A merged splice set
  would have to reconcile a text replacement that falls INSIDE a picture element
  being deleted (a Word text box lives inside `w:drawing`), and "apply the text,
  then re-scan the result and apply the pictures" has no such case: the second
  pass reads bytes the first pass has finished with. That is also why an
  occurrence is identified by PART plus ORDINAL and never by a byte offset: every
  offset the import-time scan recorded is stale once the text pass has run, so
  the picture pass asks `imaging.PicturePlaces` again against the bytes it now
  holds. The pass also writes REPLACEMENT BYTES for every anonymised asset, not
  only for a removal, because `rewriteZip` copies an entry it has no rewriter for
  byte for byte: leaving the media part alone is the leak the feature exists to
  close (`exportfmt/images.go`).

## Coding rules

- Heavy comments everywhere; each file opens with a purpose header. The owner
  is not a Go expert and orchestrates agents, so explain intent, not just
  mechanics. Comments never carry change history: no phase numbers, no "this
  used to", no tombstones for deleted functions (root `CLAUDE.md` §6). Error
  messages must be actionable: what failed, what was expected, how to fix it.
- **A change is not finished until its tests move with it** (root `CLAUDE.md`
  section 6). In the same change: update the tests that asserted the old
  behaviour, add a test for the new behaviour, delete the tests for behaviour
  that is gone, and never weaken an assertion to make it pass. A pass that
  asserts a retired contract is worse than a failure, because it is read as
  evidence. When a change here alters something the UI reads, the FRONTEND
  suite is part of the same change too: `node --test "frontend/**/*.test.js"`.
- **Testing: all conventions, tiers, fixture rules and commands are defined in
  `../docs/TESTING.md`. Read it before writing or running any test.** Engine
  logic is table-driven; fixtures live in `backend/testdata/` (English and
  French) and are reached by relative path (`../testdata`, `../../testdata`), so
  `testdata/` stays under `backend/`. `go test ./...` is the unit tier; the
  integration tier (format round-trips, the mock-Ollama detection flow) needs
  `-tags=integration`; the deep tier (wall-clock budgets) needs `-tags=deep`.
- No em dashes in user-visible Go string literals (error/report/prompt text);
  enforced by `../copy_guard_test.go`, which walks `backend/` + `.`.

## Pinned dependencies (backend-relevant subset)

Authoritative table is in the root `CLAUDE.md` §7. Key pins: Go 1.26.x,
Wails v2.13.x (v2 API only, never v3 idioms), `xuri/excelize/v2` v2.9.x,
`aspose-pdf-foss-for-go` v0.7.0 (vendored; the PDF import and in-place
export), `ledongthuc/pdf` (2025-05-11 commit) and `go-pdf/fpdf` v0.9.0 (both
without a production caller, awaiting the owner's decommissioning gate).
Default Ollama model `qwen3.5:0.8b` (a setting, never hardcoded outside
defaults).

## Where to look next

- The method surface the frontend calls: `../frontend/BRIDGE.md`.
- Product/domain rules, the preset model, full pinned-versions table:
  repo-root `CLAUDE.md`.
- Frontend rules: `../frontend/CLAUDE.md`.

# CLAUDE.md — backend/ (Go engine + Wails bound-app layer)

Purpose of this file: it is the **backend charter**. Claude Code auto-loads
the nearest `CLAUDE.md` up the tree for the files it edits, so working in
`backend/` loads this and keeps backend sessions scoped. The repo-root
`CLAUDE.md` stays authoritative for cross-cutting product rules; this file
owns the backend detail.

## What this folder is

All Go business logic and the Wails bound-app layer, as **package `backend`**
(plus the sub-packages `engine`, `engine/convert`, `engine/exportfmt`,
`engine/ooxml`, `ollama`). Pure Go, **no CGo, ever** (pattern P0). Standard library first; no
new dependency without adding it to the pinned-versions table in the root
`CLAUDE.md`.

The one thing that is NOT here: `main.go`, the `//go:embed all:frontend`
directive and `wails.json` stay at the repo root, because `go:embed` cannot
reference a parent directory, so the file that embeds `frontend/` must sit at
or above it. `main.go` imports this package and calls `backend.NewApp()`.

## The bound-app layer (`app*.go`)

- `app.go`, `app_values.go`, `app_detect.go`, `app_export.go`, `app_run.go`
  hold the `App` struct — the ONLY seam between the frontend and Go. Every method is a thin
  adapter that delegates straight to `engine/*` or `ollama/*`. **No business
  logic in these files.**
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
  (`manual`, `signal`, `heuristic`, `local_ai`). Several methods can find the
  same thing, and accepting a Suggestion keeps all of them, because two routes
  agreeing is corroboration worth showing rather than a fact to overwrite.
- The **match class** is precedence, derived from the methods by
  `MatchClassForMethods` taking the strongest, and consumed only by overlap
  resolution, ownership unification and the pre-run intersection check.
- `Confidence` is a third thing, feeding `MinConfidence`.

One field answering two of those questions is what made raising the confidence
floor silently reorder which route won.

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
- `engine/matchclass.go` — the discovery methods, the match classes
  (`built_in_pattern`, `user_defined`, `smart_discovered`,
  `local_ai_discovered`), `MatchClassRank` (lower wins) and
  `MatchClassForMethods`. An unknown or empty class ranks with `user_defined`
  rather than last, so a producer that states none is trusted rather than
  silently demoted: ranking it last turns a forgotten stamp into a missing
  replacement instead of an error. `ResolveOverlaps` compares the class FIRST.
  Mirrored by `frontend/state.js DISCOVERY_METHODS` and `MATCH_CLASSES`, guarded
  by `../detection_parity_test.go`.
- `engine/signals.go` — `AllSignalSources`, `SignalDerivations` and
  `SignalSourceSelection`: which READINGS of which built-in signals may DERIVE
  Suggestions. The selection is `map[source]map[derivation]bool`, because the two
  questions are nested: a source is a signal the pattern pass matched, a
  derivation is one reading of it through one mechanism, and a flat map of dotted
  keys would let a reading exist with no source above it. Data-driven on purpose,
  so a new reading is one constant and one implementation rather than a new field,
  a new rail row and a new persisted flag. `SignalDerivationEnabled` is the leaf
  question the discovery pass asks; `SignalSourceEnabled` DERIVES the signal's own
  state from its readings (on when any is on) and is never stored, for the reason
  the Smart detection section's state is never stored either. It does NOT govern
  whether a signal is matched and replaced, and conflating the two is the mistake
  the separate setting exists to prevent. A nil selection, or a key missing at
  either level, reads as the defaults, never as "none". Mirrored by
  `frontend/state.js SIGNAL_SOURCES` and `SIGNAL_DERIVATIONS`, guarded by
  `../detection_parity_test.go`.
- `engine/signaldiscovery.go` — signal-based discovery: an email's local part
  seeds a person and its domain seeds an organisation, each gated on its OWN
  derivation so clearing one leaves the other producing its rows, and each seed is
  searched for across the WHOLE BATCH, because the address is in one file and the
  text it points at is usually in another. Three rules shape every decision: whole
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
every Local AI finding passes a **hallucination filter** (dropped unless the
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
half-migrated. The current version is **8**, and version 7 is refused like any
other: a v7 file's `signalSuggestionSources` holds one boolean per source, which
cannot say which READING of a signal the user wanted, so reading it would mean
guessing, and the guess changes what the next run suggests. A corrupt
key (two entries claiming one value) is refused the same way, as an ERROR:
these functions run behind bound methods on a file the user picked, so
panicking would take the application down on a bad file.

## Converters (`engine/convert/`) and same-format export (`engine/exportfmt/`)

- Converters are **pure Go and one-way**: binary formats convert TO markdown
  on import (docx, pptx, xlsx via excelize, pdf via ledongthuc/pdf —
  experimental). Only the standard library + the pinned excelize / pdf libs.
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
`ledongthuc/pdf` (2025-05-11 commit), `go-pdf/fpdf` v0.9.0. Default Ollama
model `qwen3.5:0.8b` (a setting, never hardcoded outside defaults).

## Where to look next

- The method surface the frontend calls: `../frontend/BRIDGE.md`.
- Product/domain rules, anonymisation levels, full pinned-versions table:
  repo-root `CLAUDE.md`.
- Frontend rules: `../frontend/CLAUDE.md`.

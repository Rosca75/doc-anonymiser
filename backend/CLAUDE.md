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

- `app.go`, `app_entities.go`, `app_export.go`, `app_run.go` hold the `App`
  struct — the ONLY seam between the frontend and Go. Every method is a thin
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
  to Ollama. `engine/*` receives an interface, never the concrete client, so
  swapping the NER backend is a contained refactor. Ollama host is locked to
  loopback `127.0.0.1:11434` (port is settable); never make the host remote.
- **Graceful degradation:** Ollama is probed at startup and on demand
  (`GET /api/tags`); the deterministic pipeline must be fully usable without
  it. LLM-dependent controls disable with a tooltip when it is absent.

## The Value model (BUILD-06)

Three files carry it, and each exists because the rule it enforces cannot be
enforced anywhere else:

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

`Registry` owns the one-value-one-replacement invariant through its
`byOriginal` index, and tracks two sets of numbers it will never hand out
again: `retired` (a `Forget` freed the entry, deliberately not the number) and
`reserved` (a rule replacement minted outside the registry). Both persist in
the session file, or a save-and-reload frees exactly the numbers the removal
refused to free.

## Pipeline passes (fixed order)

1. Deterministic PII regex pass (`engine/pii.go`). Regexes are compiled once
   at package init and documented with match / deliberately-no-match examples.
2. Known-entity pass (`engine/entities.go`): discovery + manual entities,
   expanded into name variants, longest-match-first. Expansion has three
   classes and a category belongs to exactly one: person-style (initials,
   surname-only, first-name-only, hyphen/space), organisation-style (a legal
   suffix stripped, never added), and literal, for the categories with no name
   structure to expand.
3. Optional LLM deep-scan pass (`engine/discover.go` + `ollama`): every
   LLM-proposed entity passes a **hallucination filter** (dropped unless the
   exact string occurs in the source text) and respects the allowlist.
4. Post-pass: registry re-application across ALL loaded documents so the same
   real-world entity maps to the same placeholder everywhere.

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
half-migrated (BUILD-05 decision 1). The current version is **4**. A corrupt
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
- **Table-driven unit tests** for all engine logic; fixtures live in
  `backend/testdata/` in the supported formats, English and French. Tests
  reach fixtures by relative path (`../testdata`, `../../testdata`) — keep
  `testdata/` under `backend/` so those paths stay valid.
- No em dashes in user-visible Go string literals (error/report/prompt text);
  enforced by `../copy_guard_test.go`, which walks `backend/` + `.`.

## Pinned dependencies (backend-relevant subset)

Authoritative table is in the root `CLAUDE.md` §7. Key pins: Go 1.26.x,
Wails v2.13.x (v2 API only, never v3 idioms), `xuri/excelize/v2` v2.9.x,
`ledongthuc/pdf` (2025-05-11 commit), `go-pdf/fpdf` v0.9.0. Default Ollama
model `qwen2.5:3b-instruct` (a setting, never hardcoded outside defaults).

## Where to look next

- The method surface the frontend calls: `../frontend/BRIDGE.md`.
- Product/domain rules, anonymisation levels, full pinned-versions table:
  repo-root `CLAUDE.md`.
- Frontend rules: `../frontend/CLAUDE.md`.

# CLAUDE.md — doc-anonymiser

This file is the single source of truth for this repository. It overrides any
conflicting instruction found elsewhere. Re-read it before every work session.

## 1. Project overview

doc-anonymiser is a Windows-first desktop application (Go + Wails v2, pattern
P0 — pure Go, no CGo, no npm) that anonymises text-based client documents
(.txt, .csv, .md) entirely on the local machine. It replaces two internal
Python notebooks. The anonymisation pipeline is deterministic at its core
(regex PII pass + known-entity pass) and optionally augmented by a local LLM
served by Ollama over localhost HTTP (entity discovery, deep-scan pass).
Fallback decision recorded: if local-LLM quality proves insufficient for NER,
the fallback is pattern P4 (a small ONNX NER model running via ONNX Runtime
Web inside the WebView) — do NOT introduce CGo bindings under any circumstance.

## 2. Owner profile

The owner orchestrates LLM coding agents and is not an expert in Go. All code
must be heavily commented, explaining intent, not just mechanics. Never assume
the owner will debug at the language level. Error messages must be actionable:
what failed, what was expected, how to fix it.

## 3. Repository structure

The repo is split into two self-documenting top-level folders, `frontend/`
and `backend/`, so GUI-focused and engine-focused work can be prompted
independently. Each folder has its OWN `CLAUDE.md` charter that Claude Code
auto-loads when editing files inside it; this root file stays authoritative
for cross-cutting rules (§4-§8), and the charters own their subtree detail:

- `frontend/CLAUDE.md` — frontend charter (module map, discipline, typography,
  copy rules).
- `frontend/BRIDGE.md` — the Go ↔ JS method/event contract (the design → code
  handoff surface).
- `backend/CLAUDE.md` — backend charter (engine passes, converters, Ollama
  boundary, binding namespace).

The module anchor (`main.go`, the `//go:embed all:frontend` directive and
`wails.json`) stays at the ROOT because `go:embed` cannot reference a parent
directory: the embedding file must sit at or above `frontend/`.

```
doc-anonymiser/
├── CLAUDE.md                  # this file — authoritative for cross-cutting rules
├── README.md                  # user-facing documentation
├── LICENSE                    # MIT, Oscar Liber
├── .gitignore / .gitattributes
├── go.mod / go.sum            # module root (module path: doc-anonymiser)
├── wails.json                 # Wails config; assetdir: "frontend"
├── main.go                    # Wails bootstrap ONLY: //go:embed all:frontend; backend.NewApp()
├── embed_test.go              # asserts the frontend is embedded (package main)
├── backend/app_e2e_test.go    # headless end-to-end through the bound app layer
├── category_parity_test.go    # JS↔Go category parity guard (package main)
├── copy_guard_test.go         # no em dashes in Go user-facing strings (package main)
├── uitest_parity_test.go      # keeps the two UI harnesses on ONE probes.js (package main)
├── frontend/                  # THE GUI — vanilla ES modules, embedded via go:embed
│   ├── CLAUDE.md              # frontend charter (see above)
│   ├── BRIDGE.md              # Go↔JS contract (see above)
│   ├── index.html
│   ├── brand.css / style.css  # brand tokens (single source) + styling
│   ├── api.js                 # THE ONLY file that calls Go bound methods
│   ├── state.js               # single source of truth for frontend state
│   ├── main.js / shell.js / ui.js / html.js / icons.js / copy.js / scroll.js
│   ├── nav.js                 # THE one place the wizard moves (per-screen footers + step bar)
│   ├── toast.js / modal.js    # state-backed notice strip + in-app confirm (no native dialogs)
│   ├── highlight.js / entitymodel.js / candidatemodel.js / countries.js
│   ├── views/                 # one JS module per wizard step + shared panels:
│   │                          #   home.js, import.js, export.js, anonymise.js,
│   │                          #   identify.js (layout) + identifyrail.js (choices)
│   │                          #   + identifyworkspace.js (values), allowlist.js
│   ├── docs/                  # bundled offline user docs (SECOND window, embedded only)
│   ├── assets/icons/          # vendored Material Symbols SVGs + LICENSE
│   ├── testhtml.js            # dev-time HTML query helper for the render tests
│   └── *.test.js              # node --test "frontend/**/*.test.js" (zero npm deps)
├── backend/                   # ALL Go business logic + the Wails bound-app layer (package backend)
│   ├── CLAUDE.md              # backend charter (see above)
│   ├── app.go                 # Wails bound struct: thin adapters to engine/* and ollama/*
│   ├── app_entities.go / app_export.go / app_run.go   # App method groups
│   ├── engine/                # UI-agnostic anonymisation engine
│   │   ├── document.go        # Document model, txt/csv/md ingestion
│   │   ├── csvmd.go           # CSV ⇄ markdown-table conversion (round-trip)
│   │   ├── convert/           # binary-format → markdown converters (pure Go, one-way)
│   │   │   ├── docx.go / pptx.go / xlsx.go / pdf.go
│   │   ├── pii.go             # Pass 1: deterministic regex PII detection
│   │   ├── country.go         # Document-country model; which regex categories apply where
│   │   ├── conflicts.go       # ValidateValues: blocking conflicts + warnings, before pass 1
│   │   ├── removals.go        # Removed values: the session exclusion list
│   │   ├── entities.go        # Entity model, categories, variant expansion
│   │   ├── discover.go        # LLM discovery / deep-scan orchestration
│   │   ├── registry.go        # Placeholder registry (consistent pseudonyms)
│   │   ├── pipeline.go        # Pass orchestration per anonymisation level
│   │   ├── allowlist.go       # Terms never anonymised
│   │   ├── simplereplace.go   # Manual find-and-replace pass
│   │   ├── report.go          # Per-file / per-category / per-VALUE statistics
│   │   ├── session.go         # Save/load session state (JSON, schema migrations)
│   │   └── exportfmt/         # same-format export: rewrite of original bytes (docx/pptx/xlsx, pdf experimental)
│   ├── ollama/
│   │   └── client.go          # THE ONLY FILE that talks to Ollama (net/http)
│   └── testdata/              # fixture documents for unit tests (lives with the engine that uses it)
├── Taskfile.yml               # local entry point for the audit layer (no make: `task audit`)
├── .golangci.yml              # the machine-readable half of §6's coding rules
├── tools/                     # SEPARATE Go module holding the audit tool deps.
│                              #   Separate so tool dependencies cannot take part
│                              #   in the application module's version resolution
│                              #   and move what `wails build` compiles.
├── scripts/
│   ├── genicon.go             # standalone icon generator (//go:build ignore)
│   ├── to_sarif.py            # deadcode/deadexports JSON -> SARIF 2.1.0 (stdlib only)
│   ├── to_sarif_test.go       # its tests, in Go so `go test ./...` gates them
│   ├── audit_summary.py       # per-tool finding counts, read from the SARIF
│   ├── deadexports/           # frontend dead-export scanner (knip's job, no npm)
│   └── uitest/                # the real-rendering test layer (docs/UITESTING.md)
│       ├── probes.js          # THE ONE definition of the browser-side probes and
│       │                      #   the state they seed; BOTH harnesses read it
│       ├── renderharness/     # Linux, Chromium, Go + stdlib only (no new
│       │                      #   dependency: ws.go is a minimal RFC 6455
│       │                      #   client). Runs in CI as a BLOCKING step
│       └── Invoke-UITest.ps1  # Windows additional platform check (PowerShell +
│                              #   .NET, no packages): the real WebView2 engine
│                              #   plus a UI Automation smoke test of the
│                              #   packaged .exe. Never yet executed
├── .github/workflows/
│   ├── audit.yml              # deterministic static analysis -> code scanning
│   ├── ci.yml                 # build + test on push/PR
│   └── release.yml            # on tag: build, zip, attach to Release
└── docs/                      # phased build plans (BUILD.md, BUILD-02..04, CHANGE-01)
    ├── UITESTING.md           # the three test layers and how to run each
    ├── audit.md               # the audit layer: running it, dismissing, adding a tool
    ├── audit-baseline.md      # first full run: counts, genuine vs noise
    └── brand/color-palette.json  # vendored brand palette (source for frontend/brand.css)
```

## 4. Architecture rules

- **Local-only guarantee (non-negotiable):** the application performs no
  network I/O except HTTP to `127.0.0.1:11434` (Ollama). No telemetry, no
  update checks, no remote fonts/CDNs. All frontend assets are vendored in
  `frontend/` and embedded in the binary.
- **One-file external boundary:** only `backend/ollama/client.go` may
  construct HTTP requests to Ollama. `backend/engine/*` receives an interface,
  never a concrete client — this keeps the P4 fallback a contained refactor.
- **Engine is UI-agnostic:** nothing under `backend/engine/` imports Wails or
  reads the filesystem paths chosen by the user; documents arrive as `[]byte`
  + filename via `backend/app.go`. This keeps the engine unit-testable
  headless.
- **Frontend discipline** (detail in `frontend/CLAUDE.md`): `api.js` is the
  only bridge caller; `state.js` is the only state holder; view modules render
  from state and dispatch actions. The Wails binding namespace is
  `window.go.backend.App` (App lives in package `backend`); the full method
  contract is `frontend/BRIDGE.md`.
- **Documentation opens in a second window (BUILD-04 CR6):** the
  "Documentation" menu entry opens a SEPARATE window whose content comes from
  `frontend/docs/*`, embedded by the same `go:embed` directive. It may load
  NOTHING but embedded assets (no `http(s)://`, no CDN, no system-browser
  hand-off): Wails v2 drives one native window per process, so Go owns the path
  (`backend/app.go DocumentationURL`) and the frontend opens it with
  `window.open` (`api.js openDocumentation`). Do NOT convert it to
  `runtime.BrowserOpenURL`. Full mechanism in `frontend/CLAUDE.md`.
- **Originals are immutable:** imported files are read once and never written
  back to their source path. All output goes through explicit save dialogs.
- **Graceful degradation:** Ollama availability is probed at startup and on
  demand (`GET /api/tags`). Every LLM-dependent UI control renders in a
  disabled state with a tooltip ("Requires Ollama, which was not detected
  on 127.0.0.1:11434") when unavailable. The deterministic pipeline must be
  fully usable without Ollama. User-visible copy never contains em dashes
  (enforced by copy_guard_test.go and frontend/copy.test.js).
- **Converters are pure Go and one-way:** `backend/engine/convert/*` may use
  only the Go standard library, excelize, and ledongthuc/pdf (pinned in §7).
  No CGo, ever. Binary formats convert TO markdown on import for preview and
  processing. The app can additionally write a NEW anonymised copy in the
  source format (docx/pptx/xlsx, and experimentally pdf) at export time; this
  copy is produced by rewriting a copy of the original bytes held in memory
  (`backend/engine/exportfmt/`). The source file on disk is read once at import
  and never written, moved, or modified. If pure-Go PDF extraction quality
  proves unacceptable, the recorded fallback is a wazero-embedded WASM
  extractor (P3 pattern) — not a CGo binding.

## 5. Domain rules

- **Supported inputs:** `.txt`, `.csv`, `.md`, `.docx`, `.pptx`, `.xlsx`,
  `.pdf`. Reject anything else in the file dialog filter AND on drop, with a
  clear message. Conversion rules per format:
  - `.txt` → markdown as-is (line-ending normalisation).
  - `.md`  → passthrough.
  - `.csv` → Grid model + markdown-table preview; round-trips to CSV on export.
  - `.docx` → headings (paragraph styles Heading 1–6 → #..######), bold/italic
    runs, ordered/unordered lists (numPr), tables → markdown tables,
    hyperlinks → markdown links. Images dropped with an inline placeholder
    `*[image omitted]*`. Headers/footers/footnotes dropped (pagination noise).
  - `.pptx` → one `## Slide N: <title>` section per slide; body text with
    bullet indentation; tables → markdown tables; speaker notes under a
    `**Notes:**` sub-block. Slide-master/branding shapes skipped.
  - `.xlsx` → one Document per sheet, named `<workbook>.xlsx#<sheet>`. Smart
    routing per sheet (nb1 rules): FLAT (no merged cells, contiguous data
    bounds, header-like first row) → Grid model, same behaviour as a CSV
    import including CSV round-trip export; COMPLEX → structured JSON
    rendered in a fenced code block, anonymised as text. Trailing empty
    rows/columns trimmed via data-bounds detection.
  - `.pdf` → per-page text extraction with the spacing-repair heuristic
    (collapse runs of single uppercase characters split by kerning; collapse
    doubled spaces). PDF support is EXPERIMENTAL and labelled as such in the
    UI. A PDF yielding no extractable text is rejected with: "No text layer
    found, this PDF is likely scanned. OCR is not supported; convert it
    externally first."
- **Process order (fixed):** 1) import → convert to markdown working form,
  2) anonymise, 3) export. CSV imports are converted to a markdown table for
  preview/processing but retain their grid model so they can round-trip back
  to CSV on export.
- **The Value, the unit everything else is about (BUILD-06):** a Value is a
  string to be replaced. It has none, one or many VARIANTS (spellings of the
  same real-world thing), and one Value plus all its variants maps to exactly
  ONE replacement string, whatever found it. That invariant lives in the
  registry, not only in validation, because detection is what produces the
  violation: `Registry.Assign` keeps a `byOriginal` index, so a string already
  owned under one category is never given a second placeholder under another.
  Precedence is the fixed pass order (pass 1 before 2 before 3), which makes it
  deterministic with no extra tie-break rule and agrees with `ResolveOverlaps`
  preferring the higher-confidence span.
  A Value reaches the pipeline through one of three TRIGGERS: a regex (native,
  scoped by country), auto-detection (Smart detection and the local AI), or a
  declaration the user typed. Triggers must not conflict:
  `engine.ValidateValues` (`backend/engine/conflicts.go`) runs inside
  `engine.Run` BEFORE pass 1 and returns blocking conflicts (the same string in
  two active categories, a variant colliding with another value, a declared
  value that is also allowlisted, a simple-replace rule that would rewrite
  anonymised output) and warnings. A blocking conflict aborts before the
  registry is mutated: a half-run that assigned placeholders for a
  configuration the user was just told is invalid is unrecoverable without a
  new session.
- **Country association (BUILD-06 Phase 1):** the country-specific regex
  categories are scoped by the DOCUMENT COUNTRY, owned by the engine
  (`backend/engine/country.go`, `CategoryCountries`, `CategoryAppliesTo`) and
  mirrored by `frontend/countries.js` exactly as `presetCategories()` mirrors
  `PresetSelection`. `Settings.Country` and `PipelineInput.Country` carry it;
  a category outside the selected country renders DISABLED rather than hidden,
  because an absent switch reads as "unsupported" rather than "not applicable
  here".
- **Removing a Value (BUILD-06 Phase 4):** any value can be removed after the
  run, from the step 3 table. Removal is ONE action with three effects that
  cannot happen separately: the registry entry is forgotten, the value and its
  variants are recorded as a SESSION EXCLUSION, and the `Entity` behind it (if
  any) is dropped. The exclusion is the whole mechanism for a regex-detected
  value, which has no `Entity` at all. Exclusions live on the App
  (`App.removed`) and in the session file, deliberately SEPARATE from the
  allowlist in state, so "undo the removal" is not the same gesture as "delete
  an allowlist term"; they are ENFORCED through the allowlist
  (`App.allowlistFor`, `engine.ApplyRemovals`), because `Allowlist.Contains` is
  the single veto every span producer already consults. Removing a value does
  NOT free its number: an export, a mapping CSV or a session file in which
  `[PERSON_4]` means one person may already have left the machine. Restoring a
  value brings it back with a NEW number, for the same reason.
- **Anonymisation levels** (mirror the notebook semantics):
  - `soft` — hard PII (emails, phones, IBANs, national IDs, VAT numbers,
    URLs with credentials) + engagement entities (entity/project names) +
    custom patterns.
  - `medium` (default) — soft + person names. Dates, locations and amounts
    kept.
  - `advanced` — medium + dates, amounts, organisation names and location
    names.
  - Levels are PRESETS over granular per-category switches
    (`engine.CategorySelection`, BUILD-02 Phase 3): the pipeline obeys the
    per-category selection; a level is the UI shorthand that fills it.
    `medium` remains the default preset.
- **Pipeline passes (fixed order):**
  1. Deterministic PII regex pass (`backend/engine/pii.go`).
  2. Known-entity pass: discovery results + manual entities, expanded into
     name variants (initials, surname-only, first-name-only, hyphen/space
     variants), longest-match-first (`backend/engine/entities.go`).
  3. Optional LLM deep-scan pass (Ollama): finds residual entities. Every
     LLM-proposed entity passes a **hallucination filter** — it is dropped
     unless the exact string occurs in the source text — and respects the
     allowlist.
  4. Post-pass: registry re-application across ALL loaded documents so the
     same real-world entity maps to the same placeholder everywhere.
- **Placeholders:** stable per session, format `[CATEGORY_N]` (e.g.
  `[ENTITY_1]`, `[PERSON_3]`, `[EMAIL_2]`). The registry maps original →
  placeholder and is exportable as a re-identification key (CSV/JSON).
- **Allowlist wins:** an allowlisted term is never replaced, by any pass,
  including `ApplySimpleRules`, which runs last and used to be the one pass
  that could.
- **The step 2 to 3 gate (BUILD-06 Phase 7):** the wizard cannot reach
  Anonymise while a detection suggestion is still unreviewed. Detection
  produces suggestions, not decisions, and walking past one silently answers
  "reject" on the user's behalf. The rule lives in `state.js canGoTo` once, so
  the step bar and all four footers inherit it, and the Identify footer's hint
  is the refusal itself, naming the bulk "Reject all shown" so the gate is
  never a dead end.
- **Entity categories:** eight, listed in `engine.AllEntityCategories` and
  mirrored by `frontend/state.js`. Every one is reachable by manual entry and
  by the local AI; three are additionally reachable OFFLINE, by a heuristic
  detector, and the frontend label of the rest says where they come from,
  enforced by `identifyrail.test.js`.

  | Identifier | Placeholder | Also found offline by |
  |---|---|---|
  | `entity_names` | `ENTITY` | legal-suffix runs, country-scoped org keywords |
  | `project_names` | `PROJECT` | codes beside a project cue |
  | `product_names` | `PRODUCT` | a trademark mark, or a product head noun |
  | `brand_names` | `BRAND` | nothing: a brand is world knowledge |
  | `person_names` | `PERSON` | title cues, multi-word runs, email local-parts |
  | `identifier_names` | `ID` | reference and contract codes |
  | `other_names` | `OTHER` | nothing: it is defined by exclusion |
  | `custom_patterns` | `CUSTOM` | the user's own regexes |

  `entity_names` covers named organisations, companies, teams and internal
  systems. A human being is always `person_names`, which is why `entity_names`
  gets organisation-style variant expansion and NOT the person-style expansion
  (initials, surname-only): expanding "Delta Industries" to "Industries" would
  replace an ordinary noun everywhere. `identifier_names` and `other_names` are
  expanded LITERALLY (`engine.literalOnlyCategories`): a code has no name
  structure, and stripping a token that resembles a legal suffix off one would
  invent a variant matching a different code.
  The code detector (`backend/engine/codes.go`) requires a separator between
  the letters and the digits. That is what keeps it out of pass 1's territory,
  which owns tax and VAT numbers, and `TestCodeDetectorDoesNotOverlapPassOne`
  holds the boundary.
- **Engine identifiers are stable, user-visible labels are not (BUILD-05 Phase 0,
  superseding BUILD-04 CR3):** the wizard has **four** steps, and both their
  tokens and their visible labels are: 1 **Import**, 2 **Identify**,
  3 **Anonymise**, 4 **Export**. Step 2 owns what used to be a screen of its
  own: the configure choices are the left rail of Identify, and the values,
  suggestions, allowlist and custom patterns are its workspace. The rail lists
  the DETECTION ROUTES as switchable sections (BUILD-06): Smart detection, on
  by default and owning the scope controls (country, preset, the 24 detection
  categories, the confidence floor) because they are that route's scope; Local
  AI, off by default; Cloud AI, off and not built. Detecting Ollama ENABLES the
  Local AI switch, it never flips it. Within the scope controls the categories
  are grouped by TRIGGER, not by preset tier: contact details, technical
  identifiers, "Auto detected values" (what a detector can emit) and "Your own
  patterns" (`custom_patterns`, which is declarative and must never sit under
  the detected group).
  The engine category identifiers listed above, and the PII category constants
  in `backend/engine/pii.go`, are NEVER renamed to follow a label change: a
  label is a display string, an identifier is a contract. Session files are
  read only by the version that wrote them: a file whose schema version this
  build does not know is refused with an actionable message rather than
  half-migrated, so no step-token or field migration table exists
  (BUILD-05 decision 1).
- **Sensitive state stays in memory** by default. Saving a session (registry
  + entities + settings + the removal list + the spent placeholder numbers) to
  disk is an explicit user action with a warning that the file contains the
  re-identification key. `SessionVersion` is **4** (BUILD-06); a file of any
  other version is refused, never migrated, and the reasons for each bump are
  recorded beside the constant in `backend/engine/session.go`.

## 6. Coding rules

- Heavy comments everywhere; each file starts with a purpose header.
- **Comments explain intent, never change history.** Say what the code is for
  and why it is shaped that way. Do NOT write what it used to be, which phase
  changed it, or which change request asked for it: that is what `git log` and
  `docs/` are for, and in the code it decays into a story nobody can check. A
  comment naming a deleted function is a monument, not documentation; if the
  absence matters, assert it in a test. Where a past mistake explains a rule,
  state the RULE and the failure it prevents, in the present tense.
- Go standard library first. No new dependency without adding it to the
  BUILD.md dependency table AND the pinned-versions table below.
- **A change is not finished until its tests move with it.** This is a hard
  rule, not an aspiration, because the alternative is not "fewer tests", it is
  a suite that reports safety it no longer provides. Every one of the seven
  issues reported against the built application passed a green suite. In the
  SAME change that alters behaviour: update the tests that asserted the old
  behaviour, add a test for the new behaviour, and delete the tests for
  behaviour that no longer exists. A test left asserting a retired contract is
  worse than no test, and a test deleted without a replacement is a silent loss
  of coverage. Never make a test pass by weakening what it asserts.
- **Both suites are the deliverable, and both gate.** `go test ./...` for the
  engine and the bound app; `node --test "frontend/**/*.test.js"` for the
  frontend. The frontend suite is not optional or secondary: `frontend/` holds
  the whole user interface, so a change there is exactly as testable, and
  exactly as capable of regressing, as one in `backend/`. Layer detail and how
  to bring a new screen under test: `docs/UITESTING.md`.
- Table-driven unit tests for all engine logic; `backend/testdata/` fixtures
  in the supported formats, in English and French. Keep `testdata/` under
  `backend/` so the engine tests' relative fixture paths stay valid.
- **The parity guards are load-bearing.** `category_parity_test.go`,
  `step_parity_test.go`, `copy_guard_test.go`, `uitest_parity_test.go` and
  `frontend_tests_test.go` exist because each one is a mistake that already
  happened once and passed every other test. When one fails it is naming a real
  inconsistency; fix the inconsistency, not the guard.
- Frontend coding and typography rules live in `frontend/CLAUDE.md` (ES
  modules, no framework/build/CDN; Helvetica with Arial fallback, no Georgia,
  headings at regular weight; `--font-heading` in `brand.css` is the single
  heading-face declaration).
- All user-visible strings in English for v1 (UI i18n deferred to v2).
- Regexes are compiled once at package init and documented with examples of
  what they match and deliberately do not match.

## 7. Pinned versions

| Component | Version | Notes |
|---|---|---|
| Go | 1.26.x | toolchain in go.mod (pinned to 1.26.5); CI uses the floating 1.26.x. Moved off 1.23.x (now unsupported: Go only patches the two newest majors) to adopt Wails v2.13 and the current ledongthuc/pdf, which require Go >= 1.24/1.25 |
| Wails | v2.13.x | v2 API only — do NOT use Wails v3 idioms. v2.13.0 requires Go >= 1.25 (its go.mod says `go 1.25.0`) |
| wails CLI (CI) | v2.13.x | pinned in ci.yml and release.yml — same row as the library: the CLI and go.mod versions are a coupled pair; CI must fail with an actionable message if they diverge |
| Ollama HTTP API | as of 2026: `GET /api/tags`, `POST /api/chat` with `"format":"json"`, `"stream":false` | probed at startup; if `/api/tags` succeeds but `/api/chat` returns 404 without a model-not-found body, show "Ollama too old, please update" |
| Default Ollama model | `qwen2.5:3b-instruct` | user-selectable from `/api/tags` results; model name is a setting, never hardcoded outside settings defaults |
| Frontend | vanilla JS (ES2020), embedded via go:embed | no npm, no bundler |
| github.com/xuri/excelize/v2 | v2.9.x | XLSX reading; pure Go, MIT licence |
| github.com/ledongthuc/pdf | v0.0.0-20250511090121-5959a4027728 | pure-Go PDF text extraction (BSD-3); limited by design — see §5 PDF rules. Pinned to the 2025-05-11 commit (go.mod `go 1.24.1`), adopted with the Go 1.26 upgrade: the older 2024-02-01 commit crashes under Go 1.26 (`malformed PDF: cross-reference table not found`), which the 2025 commit fixes |
| github.com/pdfcpu/pdfcpu | NOT ADDED (evaluated at BUILD-02 Phase 13, 2026-07-24) | in-place PDF rewriting was rejected (subset-font glyph availability), so pdfcpu's metadata role is covered by fpdf (new file's Info dict) + ledongthuc/pdf (reading the original's Info dict). The earlier Go-version incompatibility no longer applies under the Go 1.26 pin, but pdfcpu stays out for the functional reason above |
| github.com/go-pdf/fpdf | v0.9.0 | pure-Go PDF writer for the regenerated-PDF same-format fallback (BUILD-02 Phase 13); MIT; go.mod requires Go 1.20 (compatible with the Go 1.26 pin) |
| golangci-lint (audit tool, `tools/go.mod`) | v2.12.2 | v2 config format (`version: "2"`) and v2 output flags (`--output.sarif.path`); the v1 flags do not exist |
| golang.org/x/tools (audit tool, `tools/go.mod`) | v0.49.0 | supplies `cmd/deadcode` |
| golang.org/x/vuln (audit tool, `tools/go.mod`) | v1.7.0 | supplies `cmd/govulncheck` |
| go-task (audit tool, `tools/go.mod`) | v3.52.0 | `Taskfile.yml` runner; `make` is unavailable on the target laptop |
| Material Symbols SVGs (assets, not a Go module) | snapshot at BUILD-02 Phase 1 | individual SVG files vendored into `frontend/assets/icons/`; Apache-2.0; licence text at `frontend/assets/icons/LICENSE` |

## 8. Validated constants

- Ollama base URL: `http://127.0.0.1:11434` (user-overridable port in
  settings; host is locked to loopback — do not "improve" this into a
  configurable remote host: it would break the local-only guarantee).
- Discovery and deep-scan prompts must request STRICT JSON with the exact
  category keys from §5 and set `"format":"json"` in the request body.

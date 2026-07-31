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
│   └── *.test.js              # node --test frontend/*.test.js (zero npm deps)
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
├── scripts/
│   ├── genicon.go             # standalone icon generator (//go:build ignore)
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
│   ├── ci.yml                 # build + test on push/PR
│   └── release.yml            # on tag: build, zip, attach to Release
└── docs/                      # phased build plans (BUILD.md, BUILD-02..04, CHANGE-01)
    ├── UITESTING.md           # the three test layers and how to run each
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
- **Anonymisation levels** (mirror the notebook semantics):
  - `soft` — hard PII (emails, phones, IBANs, national IDs, VAT numbers,
    URLs with credentials) + engagement entities (entity/project names).
  - `medium` (default) — soft + person names. Dates and locations kept.
  - `advanced` — medium + dates, locations, organisation names, monetary
    amounts.
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
- **Allowlist wins:** an allowlisted term is never replaced, by any pass.
- **Entity categories:** `entity_names`, `project_names`, `person_names`,
  `custom_patterns` (user regex), plus `organisation_names` and
  `location_names` (LLM proposals only) and the PII categories emitted by
  pass 1. The user-visible label for `entity_names` is "Entity names".
  `entity_names` is the BUILD-06 merge of the former `client_names` and
  `internal_names`: the pipeline treated the two identically, and the
  distinction cost the user a decision at every value they added. It covers
  named organisations, companies, teams and internal systems. A human being
  is always `person_names`, which is why `entity_names` gets
  organisation-style variant expansion and NOT the person-style expansion
  (initials, surname-only) `internal_names` used to get: expanding "Delta
  Industries" to "Industries" would replace an ordinary noun everywhere.
- **Engine identifiers are stable, user-visible labels are not (BUILD-05 Phase 0,
  superseding BUILD-04 CR3):** the wizard has **four** steps, and both their
  tokens and their visible labels are: 1 **Import**, 2 **Identify**,
  3 **Anonymise**, 4 **Export**. Step 2 owns what used to be a screen of its
  own: the configure choices are the left rail of Identify, and the values,
  suggestions, allowlist and custom patterns are its workspace. The rail lists
  the DETECTION ROUTES as switchable sections (BUILD-06): Smart detection, on
  by default and owning the scope controls (preset, the 22 detection
  categories, the confidence floor) because they are that route's scope; Local
  AI, off by default; Cloud AI, off and not built. Detecting Ollama ENABLES the
  Local AI switch, it never flips it.
  The engine category identifiers listed above, and the PII category constants
  in `backend/engine/pii.go`, are NEVER renamed to follow a label change: a
  label is a display string, an identifier is a contract. Session files are
  read only by the version that wrote them: a file whose schema version this
  build does not know is refused with an actionable message rather than
  half-migrated, so no step-token or field migration table exists
  (BUILD-05 decision 1).
- **Sensitive state stays in memory** by default. Saving a session (registry
  + entities + settings) to disk is an explicit user action with a warning
  that the file contains the re-identification key.

## 6. Coding rules

- Heavy comments everywhere; each file starts with a purpose header.
- Go standard library first. No new dependency without adding it to the
  BUILD.md dependency table AND the pinned-versions table below.
- Table-driven unit tests for all engine logic; `backend/testdata/` fixtures
  in the supported formats, in English and French. Keep `testdata/` under
  `backend/` so the engine tests' relative fixture paths stay valid.
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
| Material Symbols SVGs (assets, not a Go module) | snapshot at BUILD-02 Phase 1 | individual SVG files vendored into `frontend/assets/icons/`; Apache-2.0; licence text at `frontend/assets/icons/LICENSE` |

## 8. Validated constants

- Ollama base URL: `http://127.0.0.1:11434` (user-overridable port in
  settings; host is locked to loopback — do not "improve" this into a
  configurable remote host: it would break the local-only guarantee).
- Discovery and deep-scan prompts must request STRICT JSON with the exact
  category keys from §5 and set `"format":"json"` in the request body.

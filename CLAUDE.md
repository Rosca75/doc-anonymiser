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

```
doc-anonymiser/
├── CLAUDE.md                  # this file — authoritative
├── BUILD.md                   # phased implementation plan (v1, executed)
├── BUILD-02.md                # functional-improvements build plan (v2)
├── docs/BUILD-03.md           # Presidio-benchmarked detection layer (v3)
├── docs/BUILD-04.md           # UI correctness and feature-surfacing plan (v4);
│                              # AUTHORITATIVE for the CR1 to CR17 to phase mapping
├── README.md                  # user-facing documentation
├── LICENSE                    # MIT, Oscar Liber
├── .gitignore
├── go.mod / go.sum
├── main.go                    # Wails bootstrap only — no business logic
├── app.go                     # Wails bound struct: thin adapters to engine/*
├── engine/                    # ALL business logic lives here, UI-agnostic
│   ├── document.go            # Document model, txt/csv/md ingestion
│   ├── csvmd.go               # CSV ⇄ markdown-table conversion (round-trip)
│   ├── convert/               # binary-format → markdown converters (pure Go, one-way)
│   │   ├── docx.go            # zip + XML extraction of word/document.xml
│   │   ├── pptx.go            # one H2 per slide; body, tables, speaker notes
│   │   ├── xlsx.go            # excelize; smart per-sheet routing (flat → Grid, complex → JSON)
│   │   └── pdf.go             # text extraction + spacing repair (EXPERIMENTAL)
│   ├── pii.go                 # Pass 1: deterministic regex PII detection
│   ├── entities.go            # Entity model, categories, variant expansion
│   ├── registry.go            # Placeholder registry (consistent pseudonyms)
│   ├── pipeline.go            # Pass orchestration per anonymisation level
│   ├── allowlist.go           # Terms never anonymised
│   ├── simplereplace.go       # Manual find-and-replace pass
│   ├── report.go              # Per-file / per-category statistics
│   ├── session.go             # Save/load session state (JSON, schema migrations)
│   └── exportfmt/             # same-format export: rewrite of original bytes (docx/pptx/xlsx, pdf experimental)
├── ollama/
│   └── client.go              # THE ONLY FILE that talks to Ollama (net/http)
├── static/                    # vanilla-JS frontend, embedded via go:embed
│   ├── index.html
│   ├── brand.css              # brand tokens (single source of truth)
│   ├── style.css              # consumes brand.css variables only
│   ├── api.js                 # THE ONLY file that calls Go bound methods
│   ├── state.js               # single source of truth for frontend state
│   ├── shell.js               # pure header/workflow-banner markup + docs action
│   ├── ui.js                  # shared UI toolkit (button/banner/panel/icon)
│   ├── icons.js               # vendored Material Symbols SVG map
│   ├── copy.js                # user-visible strings (banners, step copy)
│   ├── scroll.js              # scroll preservation across re-renders
│   ├── docs/                  # bundled offline user documentation, opened in
│   │                          # a SECOND window (embedded assets only)
│   ├── entitymodel.js         # pure variant view-model (regression-tested)
│   ├── candidatemodel.js      # pure suggestions filter/sort view-model
│   ├── assets/icons/          # vendored Material Symbols SVGs + LICENSE
│   └── views/                 # one JS module per screen (home, wizard steps, shared allowlist panel)
├── .github/workflows/
│   ├── ci.yml                 # build + test on push/PR
│   └── release.yml            # on tag: build, zip, attach to Release
├── docs/brand/color-palette.json  # vendored brand palette (source for static/brand.css)
└── testdata/                  # fixture documents for unit tests
```

## 4. Architecture rules

- **Local-only guarantee (non-negotiable):** the application performs no
  network I/O except HTTP to `127.0.0.1:11434` (Ollama). No telemetry, no
  update checks, no remote fonts/CDNs. All frontend assets are vendored in
  `static/` and embedded in the binary.
- **One-file external boundary:** only `ollama/client.go` may construct HTTP
  requests to Ollama. `engine/*` receives an interface, never a concrete
  client — this keeps the P4 fallback a contained refactor.
- **Engine is UI-agnostic:** nothing under `engine/` imports Wails or reads
  the filesystem paths chosen by the user; documents arrive as `[]byte` +
  filename via `app.go`. This keeps the engine unit-testable headless.
- **Frontend discipline:** `api.js` is the only bridge caller; `state.js` is
  the only state holder; view modules render from state and dispatch actions.
- **Documentation opens in a second window (BUILD-04 CR6):** the "Documentation"
  menu entry opens a SEPARATE application window whose content comes from
  `static/docs/*`, embedded in the binary by the same `go:embed` directive as
  the rest of `static/`. It may load NOTHING but embedded assets: no
  `http(s)://` URL, no CDN, no system browser hand-off. There is no in-app
  documentation screen any more; the top menu keeps its entry.

  Mechanism, verified against the pin: **Wails v2 drives exactly one native
  window per process** and exposes no API to create a second one (multi-window
  is a v3 feature, and v3 idioms are forbidden). So Go owns the PATH
  (`app.go DocumentationURL`, since Go is what embeds the page) and the
  frontend opens it with a named `window.open` on the application's own asset
  server (`api.js openDocumentation`). Do NOT "fix" this into
  `runtime.BrowserOpenURL`: the system browser cannot reach the embedded
  assets, and handing it a real URL would break the local-only guarantee.
- **Originals are immutable:** imported files are read once and never written
  back to their source path. All output goes through explicit save dialogs.
- **Graceful degradation:** Ollama availability is probed at startup and on
  demand (`GET /api/tags`). Every LLM-dependent UI control renders in a
  disabled state with a tooltip ("Requires Ollama, which was not detected
  on 127.0.0.1:11434") when unavailable. The deterministic pipeline must be
  fully usable without Ollama. User-visible copy never contains em dashes
  (enforced by copy_guard_test.go and static/copy.test.js).
- **Converters are pure Go and one-way:** `engine/convert/*` may use only the
  Go standard library, excelize, and ledongthuc/pdf (pinned in §7). No CGo,
  ever. Binary formats convert TO markdown on import for preview and
  processing. The app can additionally write a NEW anonymised copy in the
  source format (docx/pptx/xlsx, and experimentally pdf) at export time; this
  copy is produced by rewriting a copy of the original bytes held in memory
  (`engine/exportfmt/`). The source file on disk is read once at import and
  never written, moved, or modified. If pure-Go PDF extraction quality proves
  unacceptable, the recorded fallback is a wazero-embedded WASM extractor
  (P3 pattern) — not a CGo binding.

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
    URLs with credentials) + engagement entities (client/project/internal
    names).
  - `medium` (default) — soft + person names. Dates and locations kept.
  - `advanced` — medium + dates, locations, organisation names, monetary
    amounts.
  - Levels are PRESETS over granular per-category switches
    (`engine.CategorySelection`, BUILD-02 Phase 3): the pipeline obeys the
    per-category selection; a level is the UI shorthand that fills it.
    `medium` remains the default preset.
- **Pipeline passes (fixed order):**
  1. Deterministic PII regex pass (`engine/pii.go`).
  2. Known-entity pass: discovery results + manual entities, expanded into
     name variants (initials, surname-only, first-name-only, hyphen/space
     variants), longest-match-first (`engine/entities.go`).
  3. Optional LLM deep-scan pass (Ollama): finds residual entities. Every
     LLM-proposed entity passes a **hallucination filter** — it is dropped
     unless the exact string occurs in the source text — and respects the
     allowlist.
  4. Post-pass: registry re-application across ALL loaded documents so the
     same real-world entity maps to the same placeholder everywhere.
- **Placeholders:** stable per session, format `[CATEGORY_N]` (e.g.
  `[CLIENT_1]`, `[PERSON_3]`, `[EMAIL_2]`). The registry maps original →
  placeholder and is exportable as a re-identification key (CSV/JSON).
- **Allowlist wins:** an allowlisted term is never replaced, by any pass.
- **Entity categories:** `client_names`, `project_names`,
  `internal_names`, `person_names`, `custom_patterns` (user regex),
  plus PII categories emitted by pass 1. The user-visible label for
  `internal_names` is "Internal".
- **Engine identifiers are stable, user-visible labels are not (BUILD-04 CR3):**
  wizard step 3 is called **"Values"** everywhere the user can see it (step
  chip, step banner, headings, help text), and its view module is
  `static/views/values.js`. The engine category identifiers listed above, and
  the PII category constants in `engine/pii.go`, are NEVER renamed to follow a
  label change. A saved session that stored the old step token `entities`
  still loads, mapped to `values` by an explicit migration.
- **Sensitive state stays in memory** by default. Saving a session (registry
  + entities + settings) to disk is an explicit user action with a warning
  that the file contains the re-identification key.

## 6. Coding rules

- Heavy comments everywhere; each file starts with a purpose header.
- Go standard library first. No new dependency without adding it to the
  BUILD.md dependency table AND the pinned-versions table below.
- Table-driven unit tests for all engine logic; `testdata/` fixtures in the
  three supported formats, in English and French.
- Frontend: ES modules, no framework, no build step, no external fonts/CDNs.
- **Typography (BUILD-04 CR2):** the web application uses **Helvetica, with
  Arial as the fallback**, for headings AND body text. Georgia is a
  PowerPoint-only brand guideline and must NOT appear anywhere under
  `static/`; `--font-heading` in `brand.css` is the single place the heading
  face is declared. Headings stay at regular weight: hierarchy comes from size
  and space, never bold. Helvetica and Arial are Windows system fonts, so this
  needs no font files and does not touch the local-only guarantee.
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
| Material Symbols SVGs (assets, not a Go module) | snapshot at BUILD-02 Phase 1 | individual SVG files vendored into `static/assets/icons/`; Apache-2.0; licence text at `static/assets/icons/LICENSE` |

## 8. Validated constants

- Ollama base URL: `http://127.0.0.1:11434` (user-overridable port in
  settings; host is locked to loopback — do not "improve" this into a
  configurable remote host: it would break the local-only guarantee).
- Discovery and deep-scan prompts must request STRICT JSON with the exact
  category keys from §5 and set `"format":"json"` in the request body.

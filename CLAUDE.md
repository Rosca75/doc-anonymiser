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
├── BUILD.md                   # phased implementation plan
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
│   └── session.go             # Save/load session state (JSON)
├── ollama/
│   └── client.go              # THE ONLY FILE that talks to Ollama (net/http)
├── static/                    # vanilla-JS frontend, embedded via go:embed
│   ├── index.html
│   ├── style.css
│   ├── api.js                 # THE ONLY file that calls Go bound methods
│   ├── state.js               # single source of truth for frontend state
│   └── views/                 # one JS module per wizard screen
├── .github/workflows/
│   ├── ci.yml                 # build + test on push/PR
│   └── release.yml            # on tag: build, zip, attach to Release
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
- **Originals are immutable:** imported files are read once and never written
  back to their source path. All output goes through explicit save dialogs.
- **Graceful degradation:** Ollama availability is probed at startup and on
  demand (`GET /api/tags`). Every LLM-dependent UI control renders in a
  disabled state with a tooltip ("Requires Ollama — not detected on
  127.0.0.1:11434") when unavailable. The deterministic pipeline must be
  fully usable without Ollama.
- **Converters are pure Go and one-way:** `engine/convert/*` may use only the
  Go standard library, excelize, and ledongthuc/pdf (pinned in §7). No CGo,
  ever. Binary formats convert TO markdown on import; the app never exports
  back to docx/pptx/xlsx/pdf. If pure-Go PDF extraction quality proves
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
  - `.pptx` → one `## Slide N — <title>` section per slide; body text with
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
    found — this PDF is likely scanned. OCR is not supported; convert it
    externally first."
- **Process order (fixed):** 1) import → convert to markdown working form,
  2) anonymise, 3) export. CSV imports are converted to a markdown table for
  preview/processing but retain their grid model so they can round-trip back
  to CSV on export.
- **Anonymisation levels** (mirror the notebook semantics):
  - `soft` — hard PII (emails, phones, IBANs, national IDs, VAT numbers,
    URLs with credentials) + engagement entities (client/project/PwC-internal
    names).
  - `medium` (default) — soft + person names. Dates and locations kept.
  - `advanced` — medium + dates, locations, organisation names, monetary
    amounts.
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
  `pwc_internal_names`, `person_names`, `custom_patterns` (user regex),
  plus PII categories emitted by pass 1.
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
- All user-visible strings in English for v1 (UI i18n deferred to v2).
- Regexes are compiled once at package init and documented with examples of
  what they match and deliberately do not match.

## 7. Pinned versions

| Component | Version | Notes |
|---|---|---|
| Go | 1.23.x | toolchain in go.mod; CI uses the same |
| Wails | v2.10.x | v2 API only — do NOT use Wails v3 idioms |
| wails CLI (CI) | v2.10.x | pinned in ci.yml and release.yml — same row as the library: the CLI and go.mod versions are a coupled pair; CI must fail with an actionable message if they diverge |
| Ollama HTTP API | as of 2026: `GET /api/tags`, `POST /api/chat` with `"format":"json"`, `"stream":false` | probed at startup; if `/api/tags` succeeds but `/api/chat` returns 404, show "Ollama too old — please update" |
| Default Ollama model | `qwen2.5:3b-instruct` | user-selectable from `/api/tags` results; model name is a setting, never hardcoded outside settings defaults |
| Frontend | vanilla JS (ES2020), embedded via go:embed | no npm, no bundler |
| github.com/xuri/excelize/v2 | v2.9.x | XLSX reading; pure Go, MIT licence |
| github.com/ledongthuc/pdf | pin the latest tagged/commit version at implementation time and record it here | pure-Go PDF text extraction (BSD-3); limited by design — see §5 PDF rules |

## 8. Validated constants

- Ollama base URL: `http://127.0.0.1:11434` (user-overridable port in
  settings; host is locked to loopback — do not "improve" this into a
  configurable remote host: it would break the local-only guarantee).
- Discovery and deep-scan prompts must request STRICT JSON with the exact
  category keys from §5 and set `"format":"json"` in the request body.

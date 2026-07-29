# CLAUDE.md — backend/ (Go engine + Wails bound-app layer)

Purpose of this file: it is the **backend charter**. Claude Code auto-loads
the nearest `CLAUDE.md` up the tree for the files it edits, so working in
`backend/` loads this and keeps backend sessions scoped. The repo-root
`CLAUDE.md` stays authoritative for cross-cutting product rules; this file
owns the backend detail.

## What this folder is

All Go business logic and the Wails bound-app layer, as **package `backend`**
(plus the sub-packages `engine`, `engine/convert`, `engine/exportfmt`,
`ollama`). Pure Go, **no CGo, ever** (pattern P0). Standard library first; no
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

## Pipeline passes (fixed order)

1. Deterministic PII regex pass (`engine/pii.go`). Regexes are compiled once
   at package init and documented with match / deliberately-no-match examples.
2. Known-entity pass (`engine/entities.go`): discovery + manual entities,
   expanded into name variants (initials, surname-only, first-name-only,
   hyphen/space), longest-match-first.
3. Optional LLM deep-scan pass (`engine/discover.go` + `ollama`): every
   LLM-proposed entity passes a **hallucination filter** (dropped unless the
   exact string occurs in the source text) and respects the allowlist.
4. Post-pass: registry re-application across ALL loaded documents so the same
   real-world entity maps to the same placeholder everywhere.

Allowlisted terms are never replaced, by any pass. Placeholders are stable
per session, format `[CATEGORY_N]`; the registry maps original → placeholder
and is exportable as the re-identification key.

## Converters (`engine/convert/`) and same-format export (`engine/exportfmt/`)

- Converters are **pure Go and one-way**: binary formats convert TO markdown
  on import (docx, pptx, xlsx via excelize, pdf via ledongthuc/pdf —
  experimental). Only the standard library + the pinned excelize / pdf libs.
- `exportfmt/` writes a NEW anonymised copy in the source format by rewriting
  a copy of the original bytes held in memory. The source file on disk is read
  once at import and never written, moved or modified. If pure-Go PDF quality
  is unacceptable, the recorded fallback is a wazero WASM extractor (P3), not
  CGo.

## Coding rules

- Heavy comments everywhere; each file opens with a purpose header. The owner
  is not a Go expert and orchestrates agents, so explain intent, not just
  mechanics. Error messages must be actionable: what failed, what was
  expected, how to fix it.
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

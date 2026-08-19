<!--
  doc-anonymiser pull request template.

  Fill in every section that applies and delete the ones that do not. The
  checklists are the point: they encode the invariants from CLAUDE.md and the
  gates in docs/TESTING.md that a reviewer would otherwise have to re-derive by
  hand. Tick a box only when it is actually true; strike through (~like this~)
  and add one line for any item that genuinely does not apply.

  Lines wrapped in these comment markers never render in the PR, so they cost
  the description nothing. Keep the prose short: the diff is the record.
-->

## Summary

<!-- One or two sentences: what this PR changes and the outcome, not the mechanics. -->

## Rationale

<!--
  Why this change, now. Link the issue, change order, or docs/ plan it comes
  from. If it implements a decision recorded in CLAUDE.md or docs/, name it.
-->

## Type of change

<!-- Tick all that apply. -->

- [ ] Feature (new user-facing capability)
- [ ] Bug fix (non-breaking change that fixes an issue)
- [ ] Refactor / internal (no behaviour change)
- [ ] Documentation only
- [ ] Build, CI, or tooling (`Taskfile.yml`, `.github/workflows/*`, `tools/`, `scripts/`)

## Changes

<!--
  The notable changes, grouped by area (frontend/, backend/engine/, backend/ollama/,
  docs/, ...). Bullet points, not a file list: the diff already lists files.
-->

-

## Testing

<!--
  What you ran and what it proved. Name the tiers (unit / integration / deep,
  see docs/TESTING.md) and both suites (Go `go test`, frontend `node --test`).
  A change is not finished until its tests move with it.
-->

- Go suite:
- Frontend suite:
- Manual check (if any):

## Screenshots or recording

<!-- Required for any visible UI change. Before / after helps. Delete this section otherwise. -->

## Architecture invariants (CLAUDE.md)

<!--
  These are the non-negotiable rules of this repository. Confirm the ones this
  PR could plausibly affect; leave the rest unticked rather than guessing.
-->

- [ ] **Local-only guarantee** holds: no network I/O added except HTTP to `127.0.0.1:11434` (Ollama). No telemetry, update checks, remote fonts, or CDNs. All frontend assets stay vendored and embedded.
- [ ] **No CGo** introduced anywhere, and converters under `backend/engine/convert/` still use only the standard library plus the pinned pure-Go dependencies.
- [ ] **External boundary intact:** only `backend/ollama/client.go` constructs HTTP requests to Ollama; the engine receives an interface, never a concrete client.
- [ ] **Engine is UI-agnostic:** nothing under `backend/engine/` imports Wails or reads user-chosen filesystem paths (documents still arrive as `[]byte` + filename).
- [ ] **Anonymisation stays deterministic and reaches no model:** `engine.Run` runs no discovery method; every discovery finding is still a reviewable Suggestion.
- [ ] **Frontend discipline:** `api.js` is still the only bridge caller and `state.js` the only state holder; the Wails namespace is still `window.go.backend.App`.
- [ ] **No new dependency**, or the new dependency is added to BOTH the BUILD dependency table and the pinned-versions table in `CLAUDE.md` §7, and is pure Go with a compatible licence.
- [ ] **Wails v2 API only** (no v3 idioms); Go / Wails version pins in `CLAUDE.md` §7 are respected.

## Contract and parity guards

<!--
  This repo keeps Go and JS in lockstep through parity tests. If you touched any
  of these, the mirror side and its guard test must move too, or CI fails.
-->

- [ ] Value categories changed → mirrored in `backend/engine` and `frontend/state.js` (`category_parity_test.go`).
- [ ] Discovery methods, match classes, or signal sources changed → mirrored both sides (`detection_parity_test.go`).
- [ ] The `Value` wire shape changed → no retired key returns and every current field is present (`value_shape_test.go`).
- [ ] Icons changed → every used name exists in `ICONS` and every entry is drawn (`icon_parity_test.go`).
- [ ] `SessionVersion` bumped only when the on-disk shape changed, with the reason recorded beside the constant in `backend/engine/session.go` (no migration alias added).
- [ ] Detection vocabulary (CLAUDE.md §5 table) used consistently in code, JSON, comments, and copy.

## Copy and comments

- [ ] No em dashes in any user-facing string (`copy_guard_test.go`, `frontend/copy.test.js`); all new copy is English.
- [ ] New/changed code carries intent-explaining comments; files keep their purpose header. Comments describe present behaviour, not change history.

## Final checklist

- [ ] Both suites pass locally (`go test ./...` and `node --test "frontend/**/*.test.js"`); integration/deep tiers run where the change warrants (`docs/TESTING.md`).
- [ ] `task audit` is clean, or every new finding is triaged/dismissed with a reason.
- [ ] Docs updated where behaviour changed (`README.md`, `docs/`, the relevant `CLAUDE.md`, `frontend/BRIDGE.md`).
- [ ] The diff is scoped to this PR's stated purpose; no unrelated changes rode along.

## Additional notes

<!-- Follow-ups, known limitations, anything a reviewer should watch for. Delete if none. -->

# Layer 1 — Deterministic Audit Pipeline

Instruction file for Claude Code. Run from the root of `doc-anonymiser`.

## Goal

Add a deterministic static analysis layer. Every finding is a fact from a tool, not a
judgement from a model. Results upload to GitHub code scanning alongside the existing
CodeQL / Code Quality alerts, so triage and dismissal state are handled by GitHub.

CodeQL (default setup), Code Quality and Dependabot are already enabled and a first
scan has been triaged. Do not duplicate them:
- Do NOT add `gosec` — CodeQL supersedes it and would produce duplicate alerts.
- Do NOT write a custom findings database or triage state file. Code scanning already
  provides stable alert identity and dismissal states.

## Environment constraints — read before installing anything

Corporate-managed Windows laptop: no admin rights, no global installers, TLS-inspecting
proxy that intermittently breaks `go mod download` and `npm install`.

- Prefer Go tools as module tool dependencies (`go get -tool`, Go 1.24+).
- Never assume a binary on PATH.
- No Makefile — `make` is unavailable. Use `Taskfile.yml` (go-task), itself a Go tool dep.
- CI has unrestricted network; marketplace actions are fine there.

## Phase 0 — Reconnaissance (do not skip)

1. `go version` — confirm 1.24+. If older, stop and report; tool directives won't work.
2. Read the Wails frontend `package.json`: package manager, TypeScript, existing ESLint.
3. Record current default-branch SHA — this is the audit baseline.
4. List `.github/workflows/` — identify anything that must not be disturbed.

Report before proceeding.

## Phase 1 — Go tooling

Add as tool dependencies:
- `github.com/golangci/golangci-lint/v2/cmd/golangci-lint`
- `golang.org/x/tools/cmd/deadcode`
- `golang.org/x/vuln/cmd/govulncheck`
- `github.com/go-task/task/v3/cmd/task`

**Verify SARIF flags via `--help`, do not assume them.** golangci-lint v2 changed its
output flags from v1. `deadcode` has no SARIF output — Phase 3 handles that.

### `.golangci.yml` (v2 format, `version: "2"`)

Be conservative — enabling everything on an existing codebase produces hundreds of
findings that get ignored wholesale. Enable: `errcheck`, `staticcheck`, `govet`,
`ineffassign`, `unused`, `revive`, `unconvert`, `bodyclose`, `contextcheck`,
`errorlint`; formatters `gofumpt`, `goimports`.

Exclude generated files, `vendor/`, and Wails bindings (`frontend/wailsjs/`).

Comment every non-default choice. This file is the machine-readable half of the
project's coding standards.

## Phase 2 — Frontend tooling

- Add `knip` for dead-export detection; configure Wails entry points so the app root
  isn't reported unused.
- Run `tsc --noEmit`; record current error count as baseline.
- Configure ESLint if absent — it's the one third-party tool Copilot Autofix supports.

## Phase 3 — SARIF conversion

Write `scripts/to_sarif.py` (Python 3, stdlib only — the proxy blocks `pip install`)
converting `deadcode` and `knip` JSON to SARIF 2.1.0.

- One SARIF file per tool. Do not merge — each needs its own code scanning category.
- Set `properties.security-severity` as a numeric string. Dead code and lint are not
  vulnerabilities: use 1.0–3.9 so they never masquerade as security alerts.
- `partialFingerprints` derived from rule ID + file + symbol, never line number.
- Tolerate empty input without crashing; emit a valid empty run instead.
- Comment heavily, including a short explanation of SARIF structure at the top.

## Phase 4 — Local entrypoint

`Taskfile.yml`:
- `task audit` — all tools, output to `.audit/`, never fail fast, per-tool summary.
- `task audit:go` / `task audit:web` — subsets.
- `task audit:new` — golangci-lint limited to changes since merge-base with default
  branch. This is the one that gets used daily; full audit is for CI and sweeps.

Add `.audit/` to `.gitignore`. Verify every task works in PowerShell, not just bash.

## Phase 5 — CI

New workflow `.github/workflows/audit.yml`. Do not modify existing workflows.

- Triggers: PR to default branch, push to default branch, `workflow_dispatch`.
- Permissions: `security-events: write`, `contents: read`.
- Upload each SARIF via `github/codeql-action/upload-sarif@v3` with a **distinct
  `category` per tool**. Without this, each upload overwrites the previous tool's
  alerts — the most common failure mode of multi-tool SARIF setups.
- `continue-on-error` on scan steps. The workflow must not block merges yet.

## Phase 6 — Baseline and verification

1. Run `task audit` locally. Record per-tool counts in `docs/audit-baseline.md` with
   the Phase 0 SHA.
2. Open a throwaway PR introducing one deliberate violation per tool (unchecked error,
   unused exported function, unused frontend export). Confirm each appears under the
   right category and severity. Delete the branch.
3. Report which baseline findings are genuine versus noise and propose `.golangci.yml`
   adjustments. Do NOT fix findings — this task builds the scanner, not uses it.

## Acceptance criteria

- `task audit` completes offline from a clean checkout.
- Alerts from all tools visible in the Security tab under distinct categories.
- No existing workflow or build behaviour changed.
- `docs/audit.md` explains how to run it, dismiss a false positive, and add a tool.

## Out of scope

Fixing findings. AI review agents. Touching CodeQL or Code Quality settings.
Anything requiring a paid licence or network access at scan time.

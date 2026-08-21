# Evaluation: distance to golang-standards/project-layout

An assessment of how far this repository's structure sits from the layout
published at <https://github.com/golang-standards/project-layout>, what a
migration would buy, and whether it is worth doing.

Measured against the tree as of this document's commit: 148 Go files, 25,290
lines of non-test Go, 24,640 lines of Go tests, 17,128 lines of frontend JS and
13,448 lines of frontend tests.

## 1. What the reference actually is

Two facts have to be established before any distance is meaningful, because
both change what the measurement means.

**It is not a Go standard.** Its own README opens with
`NOT an official standard defined by the core Go dev team`, and describes
itself as "a set of common historical and emerging project layout patterns".
The Go team has never adopted it, and the `pkg/` convention it popularised is
widely treated as a historical artifact rather than as guidance. The repository
name is the single most misleading thing about it: "golang-standards" is an
ordinary GitHub organisation name, not an endorsement.

**The official guidance is different and much smaller.** `go.dev`'s
[Organizing a Go module](https://go.dev/doc/modules/layout) is the real
reference. It recommends exactly three things relevant here:

1. A basic command may be a `go.mod` and a handful of `package main` files in
   the repository root. No subdirectories are required.
2. Supporting packages belong in `internal/`, because that "prevents other
   modules from depending on packages we don't necessarily want to expose and
   support for external uses."
3. `cmd/` is "very useful in a mixed repository that has both commands and
   importable packages", and "isn't strictly necessary in a repository that
   consists only of commands."

It does not mention `pkg/` at all.

The reference layout also carries its own exclusion, which applies directly to
a single-binary desktop application: "If you are trying to learn Go or if you
are building a PoC or a simple project for yourself this project layout is an
overkill."

This repository is not a PoC, so that sentence does not settle the question.
But it does mean the reference is offering a menu, not a specification, and the
correct reading of a menu is to take the items that answer a problem you have.

## 2. Directory-by-directory distance

| Reference directory | Purpose | This repository | Distance |
|---|---|---|---|
| `/cmd` | one subdirectory per binary | `main.go` at the root | **Blocked.** See §3 |
| `/internal` | private, non-importable code | `backend/` | **Rename only.** The one real gap |
| `/pkg` | code safe for external import | absent | **Correctly absent.** Nothing here is a library, and the convention is contested anyway |
| `/api` | OpenAPI specs, protobuf, JSON schema | `frontend/BRIDGE.md` | **Conceptual match.** The Go/JS bridge contract is prose because Wails generates the bindings; there is no schema file to house |
| `/web` | web application components | `frontend/` | **Cosmetic rename.** Zero functional gain, real legibility loss (§4) |
| `/configs` | config file templates | absent | **Correctly absent.** Settings live in memory and in the session file; there is no config file to template |
| `/init` | systemd, upstart, process managers | absent | **Correctly absent.** Desktop application, not a service |
| `/scripts` | build, install, analysis scripts | `scripts/` | **Exact match** |
| `/build` | packaging and CI configuration | `build/` | **Exact match.** Wails mandates this path for `appicon.png` and the Windows manifest, so alignment here is coincidental but complete |
| `/deployments` | Docker, Kubernetes, Terraform | absent | **Correctly absent** |
| `/test` | external test apps and test data | `backend/testdata/` plus 15 root parity guards | **Deliberate divergence.** See §5 |
| `/docs` | design and user documentation | `docs/` | **Exact match** |
| `/tools` | supporting tools | `tools/` (separate module) | **Exact match**, and arguably better: the separate `go.mod` keeps audit-tool dependencies out of the application's version resolution |
| `/examples` | examples for public libraries | absent | **Correctly absent.** No public API to exemplify |
| `/third_party` | forked or vendored external tools | absent | **Correctly absent** |
| `/githooks` | git hooks | absent | **Correctly absent** |
| `/assets` | repository assets | `frontend/assets/` | **Deliberate divergence.** Icons must sit under `frontend/` because `//go:embed all:frontend` is what puts them in the binary. A root `assets/` would need a second embed directive and would split the "everything the GUI ships" boundary in two |
| `/website` | project website | absent | **Correctly absent** |
| `/vendor` | vendored dependencies | absent | **Correctly absent.** Go modules |

Counting the nineteen reference directories: four are exact matches, eleven are
correctly absent because the project has no such concern, three diverge for a
stated reason (`/test`, `/assets`, `/web`), and one is a genuine gap
(`/internal`).

The distance is therefore **small**, and almost all of the remaining gap is one
rename.

## 3. Why `/cmd` is blocked, not merely declined

This is the constraint that matters most, because `/cmd` is the reference
layout's most visible feature and the one people notice is missing.

Two independent mechanisms pin `main.go` to the repository root:

**`go:embed` cannot reference a parent directory.** The embedding file must sit
at or above `frontend/`. This is already recorded in `CLAUDE.md` §3 as the
reason the module anchor stays at the root. Moving `main.go` to
`cmd/doc-anonymiser/` would require moving the entire frontend to
`cmd/doc-anonymiser/frontend/`, which buries the GUI three levels down to
satisfy a convention.

**The Wails v2 CLI has no option for the main package's location.** Its project
configuration struct (`internal/project.Project`, the schema behind
`wails.json`) carries `assetdir`, `wailsjsdir`, `build:dir`, `projectdir`,
`build:tags`, `outputfilename` and the frontend command hooks. It carries no
`maindir`, `mainpackage` or equivalent. `wails build` compiles the package in
the project directory, so the main package must be the directory holding
`wails.json`.

So `/cmd` is not available for the GUI binary at any price short of leaving
Wails. And per the official guidance quoted above, `cmd/` "isn't strictly
necessary in a repository that consists only of commands", which this one is.

The one scenario that changes this: **a second binary.** A companion CLI, a
headless batch runner or a server variant would each be a natural
`cmd/<name>/main.go` living beside the root `main.go`, and at that point the
repository becomes the "mixed repository" the official doc describes, where
`cmd/` earns itself. That is a trigger to watch for, not a reason to
pre-migrate.

## 4. The `backend/` to `internal/` question

This is the only change with a real argument behind it, so it deserves the
honest version of both sides.

### What it would buy

**Compiler-enforced privacy.** `internal/` is the one directory in the
reference layout that the Go toolchain itself understands. Nothing outside the
module can import it, and the failure is a compile error rather than a
convention nobody enforces.

**A declaration of intent.** `internal/` states "this is application code, not
a library". Today that fact lives only in `CLAUDE.md`. Encoding it in the tree
means a future promotion of the engine to importable status becomes a
deliberate move rather than something that happens by accident.

**Recognition.** A Go developer, or an agent reasoning about the repository
without reading the charters first, recognises `internal/` immediately.

**The Wails binding namespace survives.** This is the important de-risking
fact. `window.go.backend.App` is derived from the Go **package name**, not from
the directory path. Moving the directory to `internal/backend/` while keeping
`package backend` leaves the namespace, `frontend/api.js`, `frontend/BRIDGE.md`
and every bound-method call untouched. The migration only breaks if the package
is also renamed, which there is no reason to do.

### What it would cost

**The mechanical edit is not the expensive part.** 73 import lines across 56
Go files, resolved by one `gofmt -r`-style rewrite or a careful `sed`, then
`go build ./...` proves it.

**The documentation and tooling edits are where the risk sits**, because they
are the places a stale path fails silently rather than loudly:

- `.golangci.yml` carries path-based exclusions naming
  `backend/ollama/client.go` (the HTTP boundary), `backend/engine/convert/*`
  and `backend/engine/session.go`. A stale path here does not error; it
  silently stops excluding, or silently stops applying a rule.
- `.github/workflows/ci.yml` lists `backend/CLAUDE.md` in **two** `paths-ignore`
  blocks that the file's own comment says must stay identical.
- `Taskfile.yml`, `docs/TESTING.md`, `docs/UITESTING.md`, `docs/audit.md`,
  `frontend/BRIDGE.md`, `frontend/CLAUDE.md`, the root `CLAUDE.md`, and 22
  documents under `docs/` (mostly the archived change orders, which are a
  historical record and arguably should keep the paths that were true when they
  were written).
- Four frontend test files and three frontend source files mention `backend` as
  the binding namespace. Those must **not** change, which makes a blind
  find-and-replace across the repository actively dangerous.

**The legibility loss is the real objection.** `CLAUDE.md` §2 states the owner
orchestrates coding agents and is not a Go expert, and §3 states the two-folder
split exists so "GUI-focused and engine-focused work can be prompted
independently". `frontend/` and `backend/` is a symmetric pair that names
itself. `web/` and `internal/` is asymmetric and names neither half: `internal`
describes an import rule, not a subject, and a reader has to already know Go's
module semantics to see why the engine lives behind that word.

For a repository whose organising principle is "a non-Go-expert can point an
agent at one half", trading a self-describing name for a toolchain-semantic one
is a net loss in the dimension this repository optimises for.

The privacy benefit, meanwhile, is currently **theoretical**: there is no
external consumer to exclude, no published module path, and no plausible near
future in which someone imports `doc-anonymiser/backend/engine`. `internal/`
prevents a problem this repository cannot presently have.

## 5. Where the divergence is better than the reference

Two of the three deliberate divergences are not compromises.

**`/test` versus in-package tests and `testdata/`.** The reference layout's
`/test` is for "external test apps and additional external test data". Go's own
convention, which the toolchain enforces, is that test files live in the
package they test and fixtures live in a `testdata/` directory the tooling
ignores. This repository follows the toolchain: `backend/testdata/` sits beside
the engine that reads it, and the 15 root-level parity guards are
`package main` because that is the only package from which a Go test can
assert against both the embedded frontend and the engine constants at once.
Moving them to `/test` would either break them (a different package cannot see
the embed) or require exporting internals purely to let a test in another
directory reach them. The reference layout is simply weaker than the language
convention here.

**`tools/` as a separate module.** The reference layout says `/tools` holds
"supporting tools for the project" and stops there. This repository goes
further and gives it its own `go.mod`, so golangci-lint, deadcode, govulncheck
and go-task cannot participate in the application module's version resolution
and cannot change what `wails build` compiles. That is strictly better than the
reference and worth keeping explicitly.

## 6. Recommendation

**Optional, and on balance: do not migrate.**

Not "highly recommended" and not "recommended". The reasoning, in order of
weight:

1. **The measured distance is already small.** Four exact matches, eleven
   directories correctly absent, one genuine gap. There is no structural
   disorder here to fix. A repository that had business logic in the root,
   packages named after layers rather than subjects, or a `pkg/` full of
   application code would be a different assessment.
2. **The headline item is unavailable.** `/cmd` is blocked by `go:embed` and by
   the Wails CLI, so the most recognisable half of the reference layout cannot
   be adopted regardless of appetite. A partial migration to a layout whose
   most visible convention is missing signals less than either endpoint does.
3. **The one available item buys a benefit this project cannot yet use.**
   `internal/` enforces a boundary against consumers that do not exist.
4. **It costs the thing the repository is built around.** The
   `frontend/`/`backend/` symmetry is a stated design decision serving a stated
   owner constraint, and `web/`/`internal/` degrades it.
5. **The reference is not authoritative.** Migrating toward a layout its own
   README declines to call a standard, and away from a structure that already
   satisfies the official `go.dev` guidance, is motion rather than progress.

### Two triggers that would change the answer

Both are concrete and both are worth watching for, because in either case the
migration stops being cosmetic:

- **A second binary.** A CLI companion, a headless batch runner or a server
  variant makes this a mixed repository, which is exactly the case the official
  Go doc says `cmd/` is "very useful" for. Adopt `cmd/<name>/main.go` for the
  new binaries then, leaving the Wails GUI's `main.go` at the root where the
  CLI requires it.
- **A real external consumer of the engine.** If `backend/engine` is ever meant
  to be importable, the `internal/` decision becomes load-bearing rather than
  decorative, and you want it settled before the first import exists rather
  than after.

### If a migration is done anyway

Sequence it so nothing fails silently:

1. `git mv backend internal` and rewrite the 73 import lines. Keep
   `package backend` so `window.go.backend.App` and the whole bridge contract
   are untouched.
2. Fix `.golangci.yml` **first**, before running the suite, because a stale
   path-based exclusion there degrades quietly rather than failing.
3. Fix both `paths-ignore` blocks in `ci.yml`, keeping them identical as the
   file's own comment requires.
4. Update the live documents: root `CLAUDE.md` §3, `internal/CLAUDE.md`,
   `frontend/CLAUDE.md`, `frontend/BRIDGE.md`, `docs/TESTING.md`,
   `docs/UITESTING.md`, `docs/audit.md`, `Taskfile.yml`. Leave `docs/archive/`
   alone: those change orders record what was true when they were written.
5. Do **not** find-and-replace `backend` across `frontend/`. Seven files there
   mention it as the binding namespace and must keep it.
6. Gate on both suites plus the audit layer, per `docs/TESTING.md`: a rename is
   exactly the class of change where the parity guards and the dead-code
   scanners are the evidence that nothing was lost.

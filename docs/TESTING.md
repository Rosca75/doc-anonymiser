# Testing

This is the single source of truth for how tests are organised, run and scoped
in this repository. Read it before writing, moving or running any test. The
`CLAUDE.md` charters point here and hold no testing rules of their own, so that
guidance cannot drift between files.

There are two suites and three tiers. The suites are the Go suite
(`go test`) and the frontend suite (`node --test`); the tiers apply to the Go
suite and are selected with build tags. The frontend suite has no tiers: it is
fast, deterministic and browser-free, so it is always part of the unit tier.

For the UI-specific render layers (what a real browser SHOWS), see
`UITESTING.md`; this file owns the tier model and the Go suite.

## The three tiers

A test's tier is decided by **what it requires and what it costs**, never by how
sophisticated it is. A clever pure-logic test is unit; a trivial test that
spawns a browser is deep.

| Tier | Build tag | Requires | Cost budget | Runs |
|---|---|---|---|---|
| **unit** | none | pure functions, in-package logic, small committed `testdata/` fixtures | whole tier < 10s | every push |
| **integration** | `//go:build integration` | real file I/O, full pipeline wiring, format round-trips, spawned binaries (python for the SARIF converter). Deterministic and hermetic: **no network** | < 2 min | PRs to `main`, and per push when the change touches the package |
| **deep** | `//go:build deep` | non-deterministic, slow or dependency-heavy: wall-clock performance budgets, `-fuzz`, the real-Ollama pass, the CDP render harness | no runtime budget | on demand (`workflow_dispatch`, `task test:deep`) |

Rules:

- **The build tag is the only tier mechanism.** No env-var gating, no
  `testing.Short()` gating of whole tests.
- `testing.Short()` is orthogonal: use it only to shrink iteration counts or
  corpus size *within* a tier (a deep fuzz or golden-corpus test running fewer
  cases under `-short`), never to move a test between tiers.
- **Filenames carry the tier:** `foo_test.go` (unit), `foo_integration_test.go`,
  `foo_deep_test.go`. Every tagged file opens with its `//go:build` line and a
  header comment justifying the tier.
- **A missing dependency skips, it does not fail.** A deep test whose service is
  absent calls `t.Skip` with an actionable message
  (`"ollama not reachable at $OLLAMA_HOST; start it or drop -tags=deep"`), so a
  laptop without Ollama or a browser is never a red suite.

### Tier boundary discipline (never pay for the same assurance twice)

A higher tier tests only what the lower tier structurally cannot.

- **Integration** tests wiring, real-format behaviour and I/O edge cases. It
  does NOT re-verify a business rule a unit test already covers. If an
  integration test would fail for a reason a unit test already catches, narrow
  or delete it.
- **Deep** tests emergent, statistical or visual properties. It does NOT
  re-verify parsing or wiring. **Exactly one end-to-end happy path per supported
  input format**; variations of that path are unit table cases.
- When a test needs to move up a tier, first check whether it can be **split**:
  the logic half as a unit test, the I/O half as a thin integration test.
  Prefer that to promoting the whole test.

### Tag additivity (read before touching CI)

Go build tags are **additive**. `go test -tags=integration ./...` runs the
integration tests *and* every untagged unit test. `go test -tags=integration,deep ./...`
is therefore the **entire suite in one command** (which is what `task test:all`
uses).

The consequence CI must handle: a three-job pipeline that runs unit,
integration and deep separately executes the unit tier three times. The kept
overlap and its justification are in **CI triggers** below.

## Categories

The category is the package: `engine`, `convert`, `ooxml`, `exportfmt`,
`ollama`, `backend`, and the root `main` guards. There is **no category build
tag** — two tag axes multiply into an unmaintainable matrix.

Cross-cutting concerns are selected instead by a **subtest-name prefix**, so
`go test -run '/(layout|fonts)/'` picks exactly the categories a change touches.
The fixed vocabulary (lowercase, underscores, no spaces — Go turns a space into
an underscore and silently breaks a `-run` regex):

| Prefix | Covers |
|---|---|
| `detection` | pattern matching, heuristic/signal discovery, codes, confidence |
| `redaction` | pipeline replacement, registry, ownership unification, placeholders |
| `extraction` | binary-format import to markdown (docx/pptx/xlsx/pdf/csv) |
| `roundtrip` | same-format export, CSV round-trip, session save/load |
| `layout` | the render harness: screen fit, clipping, tooltip visibility |
| `fonts` | PDF and OOXML font / glyph handling |
| `config` | settings, category selection, presets, conflict validation |
| `errors` | error-message actionability and refusals |

Use a prefix with `t.Run`: `t.Run("layout/overflow_longer_replacement", ...)`.

## Scoping tests for a change

This is the operational rule for day-to-day work. Do NOT run the whole suite per
change; run the tiers and categories the change actually touches.

1. **Identify the categories (packages) the change touches**, directly and by
   dependency. A change to `engine/pii.go` touches `detection`; a change the
   frontend reads touches `layout` too.
2. **Decide, per tier, whether THIS change needs it, and say why:**
   - Pure logic change touching no I/O ⇒ **unit only**.
   - New format handler or changed file writing ⇒ **unit + integration**.
   - Change to a prompt, the model, font subsetting, or visual layout output ⇒
     **deep as well**, because nothing cheaper can observe the effect.
3. **Run only the selected tiers, scoped to the affected categories:**
   `go test ./backend/engine/... -run '/(detection|redaction)/'`.
4. **Before adding a test, check the overlap table** (`docs/audit.md` companion
   note, or the Phase 1 overlap audit): does a test at any tier already assert
   this invariant? If so, extend it as a table case rather than writing a new
   one.
5. **Full-suite runs (`task test:all`) are for pre-release and deliberate
   sweeps, not per change.**

## Test-code hygiene

- **Table-driven subtests.** Case names describe the scenario, never `case1`.
- `t.Helper()` on every helper. `t.Cleanup()` over `defer`. `t.TempDir()` over a
  hand-rolled temp dir.
- **Failure messages state got, want, and the input that produced them.** A bare
  `t.Fatal()` with no message is banned. Boolean-guard messages must at least
  name the invariant that failed.
- **Prefer external test packages** (`package engine_test`). Use a white-box
  `package engine` test only where unexported behaviour is genuinely the subject
  (checksum validators, tokenizers, the internal scanner), and say so in a
  comment.
- **Shared fixtures load once.** Expensive fixture reads go through a
  `sync.Once` accessor or `TestMain`, not per test.
- **Golden files** live under `testdata/golden/` and are regenerated by a single
  `-update` flag on the owning package's test, then committed.
- No `time.Sleep` for synchronisation, no global mutable state across tests, no
  ordering dependencies.
- **Do not disable the build/test cache.** Never put `-count=1` in a default
  target; add it only where a test is provably cache-unsafe, and fix the test
  first if you can.

## How to run

The runner is `go-task` (`Taskfile.yml`); see its header for bootstrap.

| Command | What it runs |
|---|---|
| `task test` | unit tier: Go unit tests + the frontend node suite |
| `task test:integration` | integration tier (additive: also the unit tier) |
| `task test:deep` | deep tier (additive: unit + integration) + the render harness |
| `task test:all` | every tier + frontend suite + render harness |
| `task cover` | coverage on the unit tier only, prints the total |

Without the runner, the same commands directly:

```
go test ./...                          # unit
go test -tags=integration ./...        # unit + integration
go test -tags=integration,deep ./...   # everything (compile-checks all tags)
node --test "frontend/**/*.test.js"    # frontend suite
go run ./scripts/uitest/renderharness  # the deep render harness (needs a browser)
```

Scope to categories with `-run` and the prefix vocabulary:

```
go test ./backend/engine/... -run '/(layout|fonts)/'
```

Regenerate a package's golden files (then commit them):

```
go test ./backend/engine/convert/ -run TestDocxGolden -update
```

## CI triggers

Triggers follow the same principle as the tiers: a tier runs when a change
touches what it observes, never on a fixed clock. There is **no nightly job**.

- **unit** — every push, with `-race`. It is the always-rational safety net.
- **compile-rot guard** — every push, once:
  `go vet -tags=integration,deep ./...` and
  `go build -tags=integration,deep ./...`. Build-tagged files are invisible to
  the compiler, `go vet` and the IDE unless the tag is set, so this compiles
  every tier once to catch a tagged file that no longer builds. One step, not
  one per tier.
- **integration** — on PRs to `main`. Because tags are additive, this job
  re-runs the unit tier too; that overlap is **kept on purpose**: it is under
  10s and gives a single-command safety net on the merge gate. The deep tier is
  excluded from it.
- **render harness (a deep check that still blocks)** — every push **that
  touches `frontend/**`, `scripts/uitest/**` or the stylesheets**. It stays a
  blocking gate (every one of the seven reported UI issues passed the cheaper
  layers), but it is path-filtered so a pure-backend change does not boot a
  browser. That is sound because the harness serves static frontend files with
  **no Go bridge**, so backend code cannot change its result (see
  `UITESTING.md`).
- **deep (the rest: perf budgets, `-fuzz`, real-Ollama)** — on demand
  (`workflow_dispatch`) and via `task test:deep` locally. These have no rational
  per-push trigger: they are either non-deterministic (wall-clock) or need a
  service that is not installed, in which case they `t.Skip`.

`-race` runs on the unit tier every push and on the integration tier when it
runs; not on every job, because race instrumentation costs roughly 5-10x and
re-running it adds no information.

**Coverage** gates on the unit tier only; the other tiers are reported without
gating.

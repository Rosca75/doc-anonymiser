# The audit layer

Deterministic static analysis for this repository. Every finding is a fact
produced by a tool, never a judgement produced by a model. Results are uploaded
to GitHub code scanning, so triage lives in the Security tab and not in a file
here.

This layer **reports**. It does not block a merge and it does not fix anything.

Companion documents:

- `docs/audit-baseline.md` — the first full run, with which findings are
  genuine and which are noise.
- `docs/UITESTING.md` — the three *test* layers, which are a different thing
  and gate separately.

## What runs

| Tool | What it answers | SARIF | Code scanning category |
|---|---|---|---|
| golangci-lint | Does any Go file break the enabled linters? | native | `golangci-lint` |
| deadcode | Which Go functions can no `main` package reach? | via `scripts/to_sarif.py` | `deadcode` |
| govulncheck | Does this code CALL a known-vulnerable symbol? | native | `govulncheck` |
| deadexports | Which frontend ES-module exports does nothing import? | via `scripts/to_sarif.py` | `deadexports` |

Deliberately absent:

- **gosec.** CodeQL (default setup) is enabled on this repository and
  supersedes it. Running both would file the same alerts twice.
- **knip, ESLint, tsc.** They need npm. `frontend/package.json` declares zero
  dependencies and exists only to mark the directory as an ES-module scope;
  CLAUDE.md §6 forbids a bundler and a `node_modules` tree, and `npm install`
  fails on the owner's proxy anyway. `scripts/deadexports` does knip's core job
  in stdlib Go instead. Its limits are documented in its package comment.
- **A findings database or a triage state file.** Code scanning already gives
  every alert a stable identity and a dismissal state. A second copy would
  disagree with the first within a month.

## Running it

### First time

The tools live in `tools/`, a Go module separate from the application's. Build
them once; after that everything runs from the build cache, offline.

```
go -C tools build -o ../.audit/bin/ github.com/go-task/task/v3/cmd/task
```

That line works verbatim in PowerShell and in bash. It builds `task` itself,
which then builds the rest:

```
.audit/bin/task tools
```

Add `.audit/bin` to `PATH` for the session if you would rather type `task`.

The first build needs network access to populate the Go module cache and takes
a few minutes. Every run after that is offline.

### Day to day

```
task audit:new
```

Lints only the changes since the merge-base with `origin/main`, as text in the
terminal. Run `git fetch` first: the comparison is against the remote-tracking
ref, and a stale one moves the merge-base backwards until half the repository
counts as changed.

This is the one that gets used. The full audit is for CI and for deliberate
sweeps.

### Full sweep

```
task audit          # everything, SARIF into .audit/, then a per-tool summary
task audit:go       # golangci-lint, deadcode, govulncheck
task audit:web      # deadexports
task audit:clean    # delete .audit/, tool binaries included
```

`task --list` shows the rest. `task <name> --summary` prints the long
explanation attached to a task.

Nothing in `task audit` fails on a finding, and no tool stops the ones after
it. A run that stops at the first tool tells you about one tool.

### Offline

Everything runs offline except `task vuln`, which fetches the Go vulnerability
database. On a disconnected laptop:

```
task lint deadcode deadexports audit:summary
```

CI runs `vuln` on every push, so nothing is lost by skipping it locally.

## Reading the output

`.audit/` holds one run's evidence and is gitignored:

| File | What it is |
|---|---|
| `<tool>.sarif` | what gets uploaded to code scanning |
| `<tool>.json` | the tool's raw output, before conversion |
| `<tool>.err` | the tool's stderr |
| `golangci-lint.txt` | the lint findings as readable text |

`task audit:summary` prints a per-tool and per-rule count read from the SARIF
files, so the number it prints is the number that will appear in the Security
tab. If those two disagree, the SARIF is wrong.

It also prints a warning when a tool reported **zero** findings and wrote
something to stderr. A tool that crashes writes nothing and converts to zero
findings, which looks exactly like a clean scan; that has already happened once
here, when `deadcode` was given a flag it does not have. When you see that
warning, read the `.err` file before believing the zero.

## Dismissing a false positive

Triage happens in GitHub, not in this repository.

1. **Security → Code scanning**, filter by the tool's category.
2. Open the alert, **Dismiss alert**, and pick a reason:
   - *Used in tests* — for a `test-only-export` that is genuinely the right
     seam for its test.
   - *False positive* — for a finding the tool got wrong.
   - *Won't fix* — for a finding that is correct and not worth acting on.
3. Write the reason in the comment box. The dismissal outlives everyone's
   memory of why.

The dismissal survives future runs because every result carries a
`partialFingerprint` derived from **rule id + file + symbol, never the line
number**. Moving a function down its file does not reopen its alert. Renaming
the function or moving it to another file does, correctly: that is a different
symbol.

Two things do lose triage state, so avoid both:

- **Renaming a code scanning category.** The old category's alerts are orphaned
  along with their dismissals.
- **Renaming a rule id** in `scripts/to_sarif.py` or `scripts/deadexports`.
  Same effect. Change the rule's `name` and description freely; the `id` is a
  contract.

If a whole class of finding is wrong rather than one instance, fix the
configuration instead of dismissing them one by one — see below.

## Adding a tool

Five steps, in this order.

**1. Add the dependency to `tools/go.mod`, never to the application's.**

```
go -C tools get -tool <module>/cmd/<tool>
go -C tools mod tidy
```

This boundary is load-bearing. Tool dependencies in the application's `go.mod`
take part in its version resolution: adding golangci-lint there pulled the
application's own `golang.org/x/text`, `x/sys` and `x/net` forward, which
changes what `wails build` compiles and ships. `.github/workflows/audit.yml`
fails the job if building the tools has modified `go.mod` or `go.sum`.

For a tool that is not a Go module, the equivalent rule is that it must run
with no install step. `scripts/to_sarif.py` and `scripts/audit_summary.py` are
Python with stdlib-only imports for that reason.

**2. Check whether it emits SARIF, with `--help`. Do not assume.**

If it does, use it: `golangci-lint` (`--output.sarif.path`, a v2 flag that did
not exist in v1) and `govulncheck` (`-format sarif`) both do. Hand-rolling a
format the tool already produces is how the two drift apart.

If it does not, add a converter to `scripts/to_sarif.py`:

- a rule catalogue entry per rule, with a `help_text` that answers "why is THIS
  reported and what do I do", not "what is dead code";
- `security-severity` as a numeric **string** in the range 1.0–3.9. A bare
  numeric literal is dropped silently by GitHub. The band is not negotiable:
  lint and dead code are maintenance facts, and an alert that presents itself
  as a security finding devalues the ones that are;
- a `convert_*` function that tolerates empty and absent input by returning no
  results;
- register it in `CONVERTERS`.

**3. Add a test.** `scripts/to_sarif_test.go` runs under `go test ./...`, which
is one of the two suites that gate this repository. A converter with no test is
a category that can silently go empty.

**4. Add a task to `Taskfile.yml`.** Redirect the tool's stderr to
`.audit/<tool>.err` so `audit:summary` can tell a crash from a clean scan. Mark
the scan step `ignore_error: true`: a finding is not a task failure. Keep the
command a single program with arguments — the Taskfile has to run under
PowerShell and bash both.

**5. Add an upload step to `.github/workflows/audit.yml` with a DISTINCT
`category`.** This is the step that is easy to get wrong and produces no error
when you do. Code scanning treats `(ref, category)` as the identity of a set of
alerts, so a second upload under an existing category **replaces** it. Two
tools sharing a category leaves you with whichever uploaded last, silently.
Give the upload `if: always()`, so that a scan step which failed still uploads
its empty SARIF — an empty run is what closes alerts a previous run opened.

Then run `task audit`, and add the new tool's counts to
`docs/audit-baseline.md` in the same change.

## Changing what is reported

`.golangci.yml` is the machine-readable half of the coding standards in
CLAUDE.md §6, and every non-default choice in it carries the reason it is
there. It is deliberately conservative: enabling every linter on an existing
codebase produces hundreds of findings that get ignored wholesale, and the
layer becomes decoration.

Adding or removing a linter is therefore a deliberate act:

1. Change `.golangci.yml`, with a comment saying what class of bug the change
   is about and what it costs.
2. Run `task audit`.
3. Update the counts in `docs/audit-baseline.md`.

Do the config change and the fixing of findings in **separate** commits. A
change that both adds a linter and tidies sixty findings is a change nobody can
review.

`docs/audit-baseline.md` ends with three proposed adjustments that would take
golangci-lint from 63 findings to 25 without losing anything real. None has
been applied.

## Why these choices

**A separate `tools/` module.** So the audit layer cannot move the shipped
binary. See `tools/go.mod`.

**go-task and not make.** `make` is not available on the owner's managed
Windows laptop and cannot be installed without admin rights. go-task is a
single Go binary built from `tools/`, so it arrives the way every other audit
tool does.

**Python for the converters.** No build step, no toolchain warm-up, and stdlib
only because the proxy breaks `pip install`. Their tests are in Go so they run
in a suite that already gates.

**Our own frontend scanner.** See `scripts/deadexports/main.go`. It does less
than knip and it does it without adding npm to a codebase whose charter forbids
it.

**Nothing blocks yet.** The baseline has not been worked through. A blocking
gate today would mean either a permanently red branch or a wholesale exclusion
list, and both end with the layer being ignored. Turning a category into a gate
is a later decision, taken once its baseline is at zero.

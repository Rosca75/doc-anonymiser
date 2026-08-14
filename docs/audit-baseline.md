# Audit baseline

The first full run of the deterministic audit layer, recorded so that later
runs can be compared against a known starting point rather than against an
impression. Regenerate the numbers with `task audit`; the analysis below is
written once and revised deliberately.

**Baseline commit:** `a481981ba110da7bf7a1e4868cbaf1c6d623e8ab` (`main`)
**Recorded:** 2026-08-14
**Tools:** golangci-lint v2.12.2, deadcode (x/tools v0.49.0), govulncheck
v1.7.0, deadexports (this repo)

Nothing in this document has been fixed. Building the scanner and using it are
separate pieces of work, deliberately: a change that both adds a linter and
"tidies" 60 findings is a change nobody can review.

## Counts

| Tool | Category | Findings |
|---|---|---|
| golangci-lint | `golangci-lint` | 63 |
| deadcode | `deadcode` | 7 |
| govulncheck | `govulncheck` | 10 |
| deadexports | `deadexports` | 43 |
| **Total** | | **123** |

### golangci-lint by linter

| Linter | Count |
|---|---|
| errcheck | 33 |
| revive | 12 |
| staticcheck | 8 |
| errorlint | 6 |
| gofumpt | 3 |
| bodyclose | 1 |

### golangci-lint by area

| Area | Count |
|---|---|
| `scripts/uitest/renderharness` | 20 |
| `backend/engine` | 16 |
| `backend/ollama` | 7 |
| `backend/engine/convert` | 7 |
| `backend/engine/exportfmt` | 6 |
| `backend` | 3 |
| root and `backend/engine/ooxml` | 4 |

## Genuine versus noise

### Genuine, and worth acting on

**errorlint, 6 findings.** The highest-value group in the baseline, because
each one is a latent silent-wrong-answer rather than a style point. CLAUDE.md
§2 requires actionable errors, which means wrapping with `%w`, which makes
every `==` comparison against a sentinel a comparison that will one day stop
matching:

- `backend/engine/convert/docx.go:171` and `pptx.go:189` compare errors with
  `==`. Both are `io.EOF`-shaped comparisons in the converters, which is
  exactly where a wrapped error from a future refactor would turn "end of
  document" into "unreadable document".
- `backend/engine/allowlist.go:73` type-asserts on an error instead of using
  `errors.As`.
- Three `fmt.Errorf` calls format an error with `%v` instead of `%w`
  (`allowlist.go:75`, `allowlist.go:77`, `entities.go:353`), which breaks the
  chain for every caller downstream.

**bodyclose, 1 finding.** `scripts/uitest/renderharness/ws.go:103` leaves an
HTTP response body unclosed. In the test harness rather than the application,
so no user-visible leak, but it is the only finding of its kind and the linter
exists for `backend/ollama/client.go`, which is clean.

**staticcheck, 8 findings.** Mixed but all real:

- `ST1005` twice in `backend/ollama/client.go` (capitalised error strings).
- `S1017` twice in `backend/engine/pii.go` (hand-rolled prefix trim).
- `QF1012` twice in `convert/pptx.go`, `QF1001` once in `engine/export.go`,
  `S1009` once in a test. Simplifications, no behaviour change.

**govulncheck, 10 findings — the most urgent item in this document.** Six are
`error` level, meaning the code actually calls the vulnerable symbol, and all
six are in the **Go standard library** as compiled by the pinned toolchain:
`net/http` (twice), `crypto/tls`, `encoding/asn1`, `encoding/xml`, `net/url`.
These are not fixed by editing this repository; they are fixed by moving the
Go patch version forward in `go.mod` and in the CI setup step. The remaining
four are `warning`/`note` level: a vulnerable package is imported but no
vulnerable symbol is reached (`golang.org/x/crypto`, `golang.org/x/text`,
`net`, stdlib).

This overlaps Dependabot deliberately and usefully: Dependabot reports that a
vulnerable version is required, govulncheck reports whether this code reaches
the vulnerable symbol. The six `error`-level results are the ones that would
otherwise sit in a Dependabot list looking the same as the four that cannot
affect this application.

**deadexports, 4 `unused-export` findings.** All four survive inspection:

- `frontend/nav.js:69 advance` and `nav.js:79 goBack`. The most interesting
  finding in the baseline. `nav.js` is documented as "THE one place the wizard
  moves", and these two are its forward and backward entry points for the
  screen footers, yet nothing imports either. Whatever the footers call, it is
  not these. That is a divergence between the module's stated contract and its
  actual use, which is worth resolving in one direction or the other.
- `frontend/views/anonymise.js:432 scopedValues`.
- `frontend/views/identifyworkspace.js:67 WORKSPACE_TABS`.

### Noise, or at least not defects

**errcheck, 33 findings — the largest group and the least valuable.** Two
things inflate it:

1. **18 of the 33 are in `scripts/uitest/renderharness`**, the test harness.
   Its `Close`/`RemoveAll`/`Kill` calls run during teardown of a browser
   process that is about to be discarded; there is no recovery to write.
2. **9 of the 33 are already-explicit `_ =` suppressions.** They are reported
   because `.golangci.yml` sets `check-blank: true`. An explicit `_ =` is a
   developer saying "I considered this error and it does not matter here",
   which is the behaviour the linter is meant to encourage, and reporting it
   anyway punishes the good form.

The residual ~6 findings in `backend/engine/exportfmt`, `backend/ollama` and
`backend/engine/convert` are the ones worth a look, and they are currently
buried.

**revive `unused-parameter`, 11 of 12 findings.** Nearly all are handler and
render functions with a fixed signature, where an unused parameter is the
signature holding rather than a mistake. The single `error-strings` finding is
genuine and duplicates a staticcheck `ST1005`.

**gofumpt, 3 findings.** Formatting only, and `--fix` resolves them. CI's
existing `gofmt` gate already passes, so these are the stricter-than-gofmt
subset.

**deadcode, all 7 findings.** Every one is reachable only from tests, and five
already carry a comment saying so:

| Symbol | Reached from |
|---|---|
| `App.documentInfos` | `app_hardening_test.go`, `app_e2e_test.go` |
| `App.latestResults` | `app_run_test.go` |
| `engine.NewAllowlist` | `backend/ollama/client_test.go` |
| `engine.DetectCodes` | `codes_test.go` |
| `engine.CategoryCountriesCopy` | `category_parity_test.go` |
| `engine.SmartDetectWithOptions` | `discover_test.go`, `entities_test.go` |
| `engine.ResolveOverlapsWithLosers` | `conflicts_test.go` |

None is deletable. `CategoryCountriesCopy` in particular exists to serve
`category_parity_test.go`, which CLAUDE.md §6 calls load-bearing. The tool is
still worth running: this is the check that would catch a genuinely orphaned
exported function, and a baseline of seven known entries is small enough to
notice an eighth.

**deadexports, 39 `test-only-export` findings.** Expected on a codebase with a
thorough frontend suite, and concentrated where the suite is thickest
(`views/anonymise.js` 10, `views/identifyrail.js` 6, `views/import.js` 4).
Each is the same design question — is this the right seam for the test, or is
the behaviour dead? — and the honest answer for most of them is "the right
seam". This is a list to read once, not a backlog.

## Proposed `.golangci.yml` adjustments

Ordered by how much noise each removes. None has been applied: changing the
config changes the baseline, and the two should not move in the same commit as
each other or as this document.

1. **Turn off `check-blank` for errcheck.** Removes 9 findings, all of them
   explicit `_ =` suppressions. The argument for keeping it is that a `_ =` can
   hide a real error; the argument against is that it is the idiom for
   deliberately discarding one, and reporting it makes the linter disagree with
   the language. Recommended: turn it off, and rely on code review for the
   `_ =` that should have been handled.

2. **Exclude `scripts/uitest/renderharness` from errcheck.** Removes 18
   findings, leaving errcheck at 6 and making that number readable. The harness
   is a test tool that drives and then discards a browser process; teardown
   errors there have no recovery path. Narrow exclusion (errcheck only, that
   directory only), not a blanket skip: the `bodyclose` finding in the same
   directory is real and should survive.

3. **Turn off revive's `unused-parameter`.** Removes 11 findings. The rule was
   enabled to catch a pipeline pass silently ignoring an input it claims to
   honour, which is a real risk in `backend/engine`. It has not caught one, and
   it fires on every fixed-signature render function instead. Alternative worth
   trying first: keep the rule but exclude `frontend`-facing view code and the
   render harness, so it still guards the engine.

Applying all three would take golangci-lint from 63 to 25, of which the
errorlint, bodyclose and staticcheck groups — the ones that describe actual
defects — would be 15. That is a list someone will read.

**Not proposed:** loosening `errorlint`, `bodyclose` or `staticcheck`. Every
finding in those three is real.

## What to do first

1. Move the Go patch version forward and re-run `task vuln`. Six of the ten
   findings are stdlib symbols this code actually calls, and no amount of work
   in this repository addresses them.
2. Fix the six `errorlint` findings. Small, mechanical, and each one closes a
   path where a wrapped error stops matching.
3. Decide what `nav.js`'s `advance` and `goBack` are for.

Everything else can wait for the config adjustments above to make it visible.

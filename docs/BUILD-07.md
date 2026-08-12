# BUILD-07: the value rules, as built

This file records the decisions taken while making the Value concept real, and
the deviations from the plan that produced it. It is the written record for a
change whose plan document was supplied to the agent and never checked in.

## A note on the series

**BUILD-06 has no plan document in this repository.** BUILD-02 through BUILD-05
each have one under `docs/`; BUILD-06 does not, and root `CLAUDE.md` §5 was its
only authority. The value-rules plan implemented here was supplied as
`BUILD-06.md` and never committed, so this file is the first written record of
that work. Anyone reading the series in order should know the gap is real and
not an oversight of filing.

## What the change delivers

**One value, one replacement.** The registry keeps a `byOriginal` index, so a
string already owned under one category is never given a second placeholder
under another. Precedence is the fixed pass order, which agrees with
`ResolveOverlaps` preferring the higher-confidence span, so the resolver and the
registry cannot disagree.

**Three triggers, and conflicts between them are refused or explained.**
`engine.ValidateValues` runs inside `Run` before pass 1 and blocks on four
shapes: one string in two active categories, a spelling two values would both
claim, a declared value that is also allowlisted, and a rule that would rewrite
anonymised output. Overlap warnings come from `ResolveOverlapsWithLosers`, the
one place the decision is actually made.

**Country scope.** `engine/country.go` owns which regex categories apply where,
and `frontend/countries.js` mirrors it as `presetCategories()` mirrors
`PresetSelection`. A category outside the selected country renders DISABLED,
not hidden: an absent switch reads as "unsupported" rather than "not applicable
here".

**Eight detected categories**, three of them reachable offline, all eight
reachable by manual entry and by the local AI. `organisation_names` and
`location_names` are deleted rather than deprecated: neither had a detector or
a prompt key, so both were switches that could never do anything.

**Values are editable and removable on step 3.** One Replaced values table,
sourced from the registry, with an editable placeholder and a remove action per
row, and a collapsed removed list with restore.

**The review gate.** The wizard cannot reach Anonymise while a suggestion is
unreviewed, because walking past one silently answers "reject" for the user.

## Decisions

1. **No retro compatibility, anywhere.** Session files of any other version are
   refused, never migrated. `SessionVersion` moved to 4 once, at the end, rather
   than three times along the way.

2. **Removals live on the App, not on `RunRequest`.** The prune and the
   exclusion are two halves of one action that must not be able to happen
   separately, and `sameFormatConfig` builds its allowlist from `a.lastReq`, so
   a removal carried only in the request would be honoured by the pipeline and
   forgotten by the export. `App.allowlistFor` is the single builder every
   caller goes through: run, all three detection routes, validation and export.

3. **Removals stay separate from the allowlist in state and in the session
   file, but are enforced by it.** A removed value must not appear as a term on
   the Allow tab, and "undo removal" must not be the same gesture as "delete an
   allowlist term". `Allowlist.Contains` is the single veto every span producer
   already consults, so a second veto would need threading through six callers,
   and the seventh somebody forgets is the bug.

4. **A removal does not free the number, and a restore does not reclaim it.**
   The user may already hold an export in which `[PERSON_4]` means one person.
   A restore is not evidence that the artefact never left the machine, so a
   restored value returns with a new number. `retired` and `reserved` therefore
   persist in the session file: without them, a save and a reload frees exactly
   the numbers the removal refused to free.

5. **The code detector requires a separator.** `PRJ-4471` is a code;
   `LU12345678` is a VAT number, and pass 1 owns it. The separator is the
   boundary between the two detectors, and `TestCodeDetectorDoesNotOverlapPassOne`
   is what found the collision and now holds the line. The cost is an
   unseparated in-house code, which the user can declare by hand.

6. **The nearest cue decides a code's category, not the strongest.** "Ref.
   INV-88213 covers the projet ATLAS-2024" has both cues in one window, and a
   rule preferring project cues would file every code in that sentence as a
   project.

7. **`brand_names` gets no gazetteer.** A brand and an organisation are
   indistinguishable by word shape; what separates them is world knowledge. A
   list of real brands is a maintenance liability and a privacy smell inside a
   local-only tool. The category is still switchable and still addable by hand,
   because a category also gates values the user TYPES: disabling it with Ollama
   absent would silently stop replacing a brand they entered themselves.

8. **`Run`'s preamble is ordered: removals, validation, reservations.** A
   removed value stops being a declaration, so validating it as one refused
   every run after a removal.

9. **A corrupt re-identification key is an error, not a panic.** These functions
   run behind bound methods on a file the user picked, so a panic takes the
   application down on a bad file, which is the opposite of the refuse-and-say-why
   policy every other load failure follows.

10. **Comments explain intent, never change history.** The comments had grown
    into a change log: 441 references to a build phase, 140 change-request
    numbers, and tombstone blocks for deleted functions. None of it says what
    the code is for, and none of it is checkable, because the plan documents it
    cites are not all in this repository. The rule is now in root `CLAUDE.md` §6
    and both sub-charters.

## Bugs fixed that no test would have caught

Every one of these passed a green `go test ./...` and a green frontend suite.

| Bug | Where | Why nothing caught it |
|---|---|---|
| `Registry.Rename` unlocked an already-unlocked mutex, a FATAL error that killed the process on every rename | `engine/registry.go` | nothing called it |
| `App.RestoreValue` returned "will be implemented in Phase 8" | `app_entities.go` | nothing called it |
| `App.ListRemovedValues` looked up owners `Forget` had just deleted, so it was always empty | `app_entities.go` | nothing called it |
| `App.NextRulePlaceholder` parsed with `%[^_]`, a scanf scanset Go's `fmt` does not support, so it always answered 1 | `app_entities.go` | nothing called it |
| Removals reached neither the pipeline nor the export: the App had no removal state | `app.go`, `app_run.go`, `app_export.go` | the engine half was implemented and unit-tested in isolation |
| `RemovedValues`, `RetiredPlaceholders`, `ReservedPlaceholders` declared on `Session` and never written or read | `app_export.go` | a field with no reader is invisible to a round-trip test that never sets it |
| Three of `ValidateValues`' four blocking rules were commented-out TODOs with dead helpers behind them | `engine/conflicts.go` | `conflicts_test.go` did not exist |
| `runAIPhase` threw away every proposal collected before a cancellation | `app_detect.go` | found by the rewritten cancellation test |
| `api.js cancelDetection()` called `CancelDiscovery`, a method that no longer existed | `api.js` | no test drove the wrapper's method name |
| `Ctrl+O` / `Ctrl+E` bypassed the backward-confirm and the step reset | `main.js` | `main.js` is an exempt module and the shortcuts were nowhere else |
| The code and product helpers made the offline pass quadratic again | `engine/codes.go` | caught by the existing scaling test, which is why it exists |

## Deleted, not deprecated

`organisation_names`, `location_names`, `SetEntityPlaceholder`,
`EntityPlaceholder`, `RunDiscovery`, `RunSmartDetection`, `EstimateDiscovery`,
`ListDocuments`, `GetResults`, `engine.SmartDetect`, `engine.extractRuns`,
`state.pendingValues` and its three reducers, `anonymise.js nextCustomNumber`,
`candidatemodel.js candidateCategoryCounts`, `state.settings.useCloudAI`,
`copy.js useAILabel` and `CONFIGURE.tabsLabel`.

The two placeholder methods are the ones worth naming: they were addressed by
(category, canonical) and lived on step 2, where the registry does not exist
yet, so the editor behind them failed before the first run by construction.
Moving it to step 3 deletes the bug rather than working around it.

## Deviations from the plan

1. **"address" is not a category.** The rules named address among the regex
   values; free-text address regexes are unreliable, and addresses are what
   smart detection is for. Saying the word makes it its own phase.

2. **The positional fallback in `discover.go` is unchanged.** Routing it to
   `other_names` would make the default review list mostly "Other", which reads
   as a broken detector and throws away the one useful split the heuristic makes.

3. **An FR INSEE recognizer is not built.** `matricule` is tagged LU, and the
   French copy no longer promises otherwise; a real French national-ID
   recognizer is new detection scope.

## Verification

- `go test ./...` and `node --test "frontend/**/*.test.js"`, both green.
- `go run ./scripts/uitest/renderharness` (layer 3, blocking in CI), 41 checks
  green, after every phase that changed the rail or added a surface.
- `go vet ./...` clean, `gofmt` clean, `GOOS=windows go build ./...` succeeds.
- Not performed: the end-to-end run in the real application on Windows, which
  needs a Windows desktop this work did not have.

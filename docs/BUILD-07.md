# BUILD-07: decisions and deviations from the BUILD-06 value-rules change

This file records the decisions taken while implementing the value-rules plan,
in the BUILD-05 style, and it records the parts of that plan that are **not**
implemented. It is written at the end of Phase 8, which is the phase the plan
gave the documentation to.

## A note on the series

**BUILD-06 has no plan document in this repository at all.** BUILD-02 through
BUILD-05 each have one under `docs/`; BUILD-06 does not, and root `CLAUDE.md`
§5 is its only authority. The value-rules plan implemented here is the document
the owner supplied as `BUILD-06.md`, and it was never checked in. This file
therefore names the phases as "BUILD-06 Phase N" to match the commit history
and the code comments, while being the first written record of that work.
Anyone reading the series in order should know that the gap is real and not an
oversight of filing.

## Decisions

1. **One session-version bump, at the end.** Phases 1, 2 and 4 each change what
   a session file holds. Policy is refuse-on-mismatch with no migrations
   (BUILD-05 decision 1), so intermediate phases left `SessionVersion` alone and
   Phase 8 moved 3 to 4 once. The reasons for the bump are written beside the
   constant in `backend/engine/session.go`, not here, so the next person to
   consider a migration reads them where the decision bites.

2. **Removals live on the App, not on `RunRequest`.** The prune and the
   exclusion are two halves of one action that must not be able to happen
   separately, and `sameFormatConfig` builds its allowlist from `a.lastReq`, so
   a removal carried only in the request would be honoured by the pipeline and
   forgotten by the export. `App.allowlistFor` is the single builder every
   caller now goes through: run, all three detection routes, validation and the
   same-format export. `Settings.UseAI` is the precedent.

3. **Removals stay separate from the allowlist in state and in the session
   file, but are enforced by it.** A removed value must not appear as a term on
   the Allow tab, and "undo the removal" must not be the same gesture as
   "delete an allowlist term". `Allowlist.Contains` is the single veto every
   span producer already consults, so a second veto would need threading
   through six callers and the seventh somebody forgets is the bug.

4. **A removal does not free the number, and a restore does not reclaim it.**
   The user may already hold an exported document, a mapping CSV or a session
   file in which `[PERSON_4]` means one person. A restore is not evidence that
   the artefact never left the machine, so a restored value returns with a new
   number. `retired` and `reserved` therefore had to persist in the session
   file: without them, a save-and-reload freed exactly the numbers the removal
   had refused to free.

5. **A corrupt re-identification key is refused, not survived, and refused as
   an ERROR.** `NewRegistryFromEntries` used to `panic` on a duplicated
   original. It runs behind a bound method on a file the user picked, so a
   panic takes the whole application down on a bad file, which is the opposite
   of the refuse-and-say-why policy every other load failure follows. It now
   returns an error naming the value and the two categories claiming it.

6. **The session loader validates before it mutates.** It installed the
   registry and the settings and validated afterwards, so a file the
   application refused still left the App holding that file's registry behind
   an error message the UI reads as "nothing was loaded", and the next export
   would have written the rejected file's key. `applyRestoredSession` now
   builds, validates, and only then installs.

7. **`a.lastReq` is stored after a successful run, not before it.** A run that
   validation refused used to leave the same-format export faithfully
   reproducing a request the application had just declared invalid.

8. **The review gate is one rule in `canGoTo`, and it does not apply to
   Export.** Reaching Export requires results, which means a run already
   happened with the review done; refusing to show the user the output of a
   finished run because a later pass added suggestions would strand them on a
   screen with nothing to do.

9. **The gate's refusal is the footer hint.** `readyHint` narrated the
   unreviewed count beside a button that simply looked broken. It is now the
   reason, and it names the one action that clears the gate ("Reject all
   shown"), including the fact that a filter limits that button, because a user
   who switched detection off after it ran has no other clue that suggestions
   are still sitting in the list.

10. **`useCloudAI` was deleted rather than documented.** The frontend store
    carried it and `pushSettings` sent it; Go discarded it and `BRIDGE.md` said
    it did not exist. The rail renders a static "not built yet" panel and never
    read the flag. A setting nothing reads and nothing can change is a claim the
    next reader has to disprove.

## Bugs fixed that no test would have caught

Each of these passed a green `go test ./...` and a green frontend suite.

| Bug | Where | Why nothing caught it |
|---|---|---|
| `Registry.Rename` unlocked an already-unlocked mutex, a FATAL error that killed the process on every rename | `engine/registry.go` | nothing called it |
| `App.RestoreValue` returned "will be implemented in Phase 8" | `app_entities.go` | nothing called it |
| `App.ListRemovedValues` read the registry's retired placeholders and looked up their owners, which `Forget` had just deleted, so it always returned empty | `app_entities.go` | nothing called it |
| `App.NextRulePlaceholder` parsed with `%[^_]`, a scanf scanset Go's `fmt` does not support, so it always answered 1 | `app_entities.go` | nothing called it |
| Removals reached neither the pipeline nor the export: the App had no removal state at all | `app.go`, `app_run.go`, `app_export.go` | the engine half (`removals.go`, `PipelineInput.Removed`) was implemented and unit-tested in isolation |
| `RemovedValues`, `RetiredPlaceholders` and `ReservedPlaceholders` were declared on `Session` and never written or read | `app_export.go` | a struct field with no reader is invisible to a round-trip test that never sets it |
| `Ctrl+O` / `Ctrl+E` bypassed the backward-confirm and the step reset | `main.js` | `main.js` is an exempt module and the shortcuts were nowhere else |

`backend/app_values_test.go`, the new cases in `backend/engine/registry_test.go`,
`frontend/identify.test.js` and the new `frontend/api.test.js` cases exist
specifically to call these.

## Deviations: what the plan asked for and this work did not deliver

Stated plainly, because a charter that describes work nobody did is worse than
no charter. Root `CLAUDE.md` and the two sub-charters describe the code **as it
is**, not as the plan wanted it.

1. **Phase 2 (the detected-category set) is not implemented.** The plan
   replaces the entity categories with seven (`entity_names`, `project_names`,
   `product_names`, `brand_names`, `person_names`, `identifier_names`,
   `other_names`) and retires `organisation_names` and `location_names`. The
   code still has the original six: `entity_names`, `project_names`,
   `person_names`, `custom_patterns`, `organisation_names`, `location_names`.
   `ALL_CATEGORIES` is still 22, not 24. Everything Phase 2 depended on
   (`PlaceholderLabel` coverage, the parity guards, the preset comment) is
   consistent with the SIX, so the tree is coherent; it is simply the old set.
   The plan's identifier and product detectors, the unified legal-suffix
   gazetteer and the `literalOnlyCategories` variant class do not exist.

2. **Phase 5's frontend half is not implemented.** The Go surface is complete
   and tested (`ValuePlaceholders`, `SetValuePlaceholder`, `RemoveValue`,
   `RestoreValue`, `ListRemovedValues`, `NextRulePlaceholder`,
   `ValidateValues`), and `api.js` now wraps all seven, but no view renders the
   step 3 **Replaced values** table. Consequently:
   - `SetEntityPlaceholder` and `EntityPlaceholder` are still alive rather than
     deleted, because `views/identifyworkspace.js` is still their only caller
     and deleting them would remove a working feature with nothing replacing
     it. `BRIDGE.md` marks them superseded and scheduled for deletion.
   - The step 2 placeholder editor still fails before the first run, which is
     the bug the move to step 3 was meant to delete.
   - `pendingValues`, `clearPendingValues()` and `anonymise.js nextCustomNumber`
     are still in place.
   Finishing Phase 5 is a frontend-only change: the bridge under it is done.

3. **Phase 6's grouping is close but not the plan's table.** The rail groups by
   trigger and `custom_patterns` has its own "Your own patterns" group, which
   is the substance of the rename. The membership of "Auto detected values" is
   the six categories that exist, not the plan's seven, and the plan's exact
   split of contact versus technical identifiers differs in two entries.

4. **Phase 3's `ResolveOverlapsWithLosers` warning path.** `ValidateValues` and
   the blocking rules are implemented and run inside `engine.Run` before pass 1;
   the overlap-derived WARNINGS, which the plan wanted sourced from the
   resolver itself so a parallel check cannot disagree with the pipeline, are
   not.

## Verification performed

- `go test ./...` and `node --test "frontend/**/*.test.js"`, both green.
- `go run ./scripts/uitest/renderharness` (layer 3, blocking in CI), 41 checks
  green, after Phase 7 and after the Phase 6 group rename.
- Not performed: the end-to-end run in the real application on Windows
  (verification step 3 of the plan), which needs a Windows desktop this work
  did not have. `go build` for the target platform is covered by CI.

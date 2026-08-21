# CHANGE-12c — Presets become scoped data, so a preset family can be added without a rewrite

You are executing a change order against the existing **doc-anonymiser**
repository (pattern P0, pure Go + Wails v2, no CGo, no npm). It is **ONE batch**.

It comes AFTER `docs/change-12.md` and `docs/change-12b.md`. It **changes
behaviour**: what a preset chip writes, and what the settings and the session file
carry. It is executed on its own.

## 1. Why

`docs/change-12.md` splits the rail into Built-in patterns and Heuristic
discovery. The preset chips (Soft / Standard / Thorough) survive that split
awkwardly: one chip fills BOTH the pattern categories and the name categories, so
a click under Built-in patterns reaches into Heuristic discovery. Change-12 makes
that honest with a read-out; this order makes it correct.

There is a second, larger reason. The owner intends preset FAMILIES: a
"regulatory" family beside the depth family, offering named presets such as
"GDPR". The current model cannot hold that. `Settings.Level` is ONE string, and
`engine.PresetSelection(level)` is a hand-written `if` ladder over three levels
that fills every category in one function. A second family, or a preset scoped to
one mechanism, has nowhere to go.

The load-bearing fact that makes this contained: **a preset is already only a
shortcut.** `PipelineInput.Level` is a FALLBACK, used solely when no explicit
selection is given (`engine/pipeline.go`), and the pipeline otherwise obeys
`CategorySelection`. So presets can be restructured without changing what a run
does.

Ground rules, in addition to the Ground rules block of `docs/change-12.md`:

- **The per-category selection stays the authority.** The pipeline keeps reading
  `CategorySelection` and nothing else. A preset writes that map; it is never
  consulted at run time.
- **`SessionVersion` bumps to 12** (11 having been taken by
  `docs/change-12b.md`), with the reason beside the constant: `level` leaves the
  schema and `presets` enters it. Refused, never migrated, as always.
- The `Level` type and `PresetSelection` may be DELETED only once nothing reads
  them. If a report or an export names the level, it names the depth preset
  instead, by the same label.

### The deviation rule

If a step is wrong, contradicted by the code, or cannot be done as written:
**stop, say so, and propose the alternative before writing it.**

---

## 2. The model

A preset is a ROW IN A TABLE, not a branch in a function:

```go
// engine/presets.go
type Preset struct {
    ID         string   // "thorough", "gdpr"
    Family     string   // "depth", "regulatory": one chip ROW per family
    Scope      string   // ScopePatterns or ScopeNames: which categories it may write
    Label      string   // the chip's visible text
    Categories []string // the category keys it switches ON, all within Scope
}
```

Four rules, and each one is why a piece of this is shaped the way it is.

**1. Scope is what makes the sections separate.** Applying a preset writes only
the categories in its own scope and leaves every category outside it exactly as
it was. That is the whole point of the order: the chips under Built-in patterns
can no longer touch a checkbox under Heuristic discovery. A `Categories` entry
outside its own scope is a BUILD-BREAKING error, asserted by a test, not a
silently ignored key.

**2. A family is a chip row.** Each section renders one row per family that has
entries in that section's scope. Adding the regulatory family adds a second row
under each section and introduces no new concept in the rail.

**3. A preset spanning both mechanisms is declared TWICE, once per scope.** GDPR
is one row for `ScopePatterns` and one for `ScopeNames`, sharing an ID and a
label. It then appears as a chip in both sections, each instance filling only its
own section's categories. Same name, two independent applications, no
cross-section reach. Sharing an ID across scopes is deliberate and is what lets
the two rows be recognised as the same regulation.

**4. No preset algebra.** A chip is a WRITE, not a layer. Each row derives
independently whether the current selection still matches one of its presets, and
shows "Custom" when it does not. Layering two families (a depth base plus a
regulatory overlay) would need conflict rules for a category two presets disagree
about, and would make the active chip unrecoverable from the selection: the rail
could no longer tell the user which preset they are on. If a future regulation
genuinely needs to say "off" as well as "on", that is a new order and it must
answer the reverse-derivation question first.

### The depth family, unchanged in meaning

The three depth presets keep exactly the sets `PresetSelection` fills today,
split by scope. Nothing about what Soft, Standard and Thorough switch on may
change in this batch: the framework-agreement numbers are the proof (§6).

| Family | Scope | Preset | Fills |
|---|---|---|---|
| depth | patterns | Soft | the hard and extended pattern categories |
| depth | patterns | Standard | the same (the medium level adds only name categories) |
| depth | patterns | Thorough | plus `amount`, `date`, `address`, `postal_code` |
| depth | names | Soft | `entity_names`, `project_names`, `identifier_names` |
| depth | names | Standard | plus `person_names`, `product_names`, `brand_names` |
| depth | names | Thorough | plus `other_names`, `country_names`, `nationality_names`, `business_sector_names` |

Two consequences to state in the code rather than discover later. **Soft and
Standard are identical in the patterns scope**, which is a fact about the levels
and not a bug: the medium level differs from soft only in name categories. The
patterns row must therefore be able to show two chips as "the current selection
matches", and the rule is that the FIRST match in table order wins, so the row
reads Soft rather than flickering. **`custom_patterns` is in no preset**: since
`docs/change-12.md` D5 it has no switch and is permanently on, so a preset must
neither set nor clear it.

### Storage

`Settings.Level string` becomes:

```go
// "<scope>.<family>" -> preset ID, e.g. {"patterns.depth": "standard",
// "names.regulatory": "gdpr"}. Flat rather than nested so a family added later
// needs no schema change, and absent rather than defaulted so "Custom" is
// representable: a selection matching no preset stores no key for that row.
Presets map[string]string `json:"presets,omitempty"`
```

The same field, the same JSON key, in `engine.SessionSettings`. A file carrying
the old `level` key is refused by the version gate, so no reader may look for it.

---

## 3. Engine changes

| File | Change |
|---|---|
| `engine/presets.go` (new) | the `Preset` type, `ScopePatterns` / `ScopeNames`, the preset TABLE, `PresetsFor(scope, family)`, `ApplyPreset(sel, preset)` writing within scope only, and `MatchingPreset(sel, scope, family)` returning the preset the selection matches or empty for Custom |
| `engine/pipeline.go` | `PresetSelection` and the `Level` fallback are replaced by `DefaultSelection()`, which is the two depth Standard presets applied to an empty selection plus `custom_patterns`. `PipelineInput.Level` goes; nothing else in the pipeline changes |
| `engine/session.go` | `Level` out, `Presets` in, `SessionVersion` to 12 with its reason |
| `backend/app.go` | `Settings.Level` out, `Settings.Presets` in; the defaults become the depth Standard pair; validation rejects an unknown scope, family or preset ID with a message naming what is valid |
| `engine/report.go` | if a report names the level, it names the depth presets instead |

`ApplyPreset` must be the ONLY writer. A view that fills a category map itself is
how the two sides drift.

## 4. Frontend changes

| File | Change |
|---|---|
| `state.js` | `applyPreset(level)` and `selectionPresetName(categories)` become `applyPreset(scope, family, id)` and `activePreset(s, scope, family)`, over a mirror of the engine table. The mirror is guarded by a parity test, like the categories |
| `views/identifyrail.js` `PRESETS` | the hand-written `[level, label]` list is DELETED: the labels come from the mirrored table, so a preset added to the engine appears without a second edit here |
| `views/identifyrail.js` | `presetChips(s)` takes a scope and renders one row per family in that scope. Built-in patterns renders the patterns rows; Heuristic discovery renders the names rows. The change-12 read-out under the chips ("Thorough also switched on 4 auto detected values below") is DELETED: it described the cross-section reach this order removes |
| `copy.js` | `RAIL.preset` becomes a label per FAMILY (`presetFamilyLabel`), so the regulatory row can be titled without a code change. The preset labels themselves come from the mirrored table, not from a second list |

## 5. Tests

| Test | Asserts |
|---|---|
| every preset's categories are inside its own scope | over the whole table, on both sides of the bridge |
| the JS mirror equals the engine table | a parity guard beside the category one |
| applying a patterns preset leaves every name category untouched, and the reverse | the point of the order, as a wiring test |
| the depth presets fill exactly what the levels filled | the three selections compared against the sets recorded in §2 |
| Soft and Standard are identical in the patterns scope, and the row shows Soft | the first-match rule, so it cannot regress into flicker |
| no preset touches `custom_patterns` | it has no switch and must stay on |
| a selection matching no preset reads as Custom, per row | independently for each row |
| Built-in patterns renders only patterns rows, Heuristic discovery only names rows | render |
| a session file of version 11 is refused | naming both versions |
| the framework-agreement suite | precision and recall UNCHANGED to the digit under the default settings |

## 6. Execution order

1. `engine/presets.go` and its tests, with the depth table transcribed from
   `PresetSelection` and the equality test proving the transcription.
2. `PresetSelection` and `PipelineInput.Level` deleted; `go build ./...` finds
   every reader.
3. Settings and session: the new field, the version bump, the refusal test.
4. Frontend: the mirror, the parity guard, the two scoped chip rows, the deleted
   read-out.
5. Harness and `docs/UITESTING.md` expectations.
6. `CLAUDE.md` §5 (the anonymisation-levels paragraph becomes the preset model,
   and it must keep saying that the pipeline obeys the per-category selection),
   `frontend/BRIDGE.md`, `frontend/CLAUDE.md`, `README.md`,
   `frontend/docs/index.html`.

## 7. Acceptance criteria

1. Both suites and the rendering harness green.
2. The framework-agreement numbers are identical to the digit: this order changes
   what a chip WRITES, never what a run does.
3. No chip in one section can change a checkbox in another, proven by a wiring
   test rather than by inspection.
4. Adding a preset is ONE table row per scope in the engine plus the mirror, with
   no change to any view. Demonstrate it in the commit message by writing out the
   two rows a "GDPR" preset would need. **Do not add the GDPR preset itself:**
   which categories a regulation requires is the owner's call and a separate
   order.
5. `SessionVersion` is 12, the reason is recorded, and a version 11 file is
   refused with a message naming both versions.

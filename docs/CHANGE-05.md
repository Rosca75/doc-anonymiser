# CHANGE-05 — The five PR#46 regressions on step 2, Identify

You are executing a change order against the existing **doc-anonymiser**
repository (pattern P0, pure Go + Wails v2, no CGo, no npm). It holds **one
self-contained implementation section per change request (CR1 to CR5)**,
followed by the **decisions taken**, a **conflict analysis**, the **recommended
execution sequence** and the **acceptance criteria**.

Every CR below is a REGRESSION reported against the built application after
PR#46 (`docs/CHANGE-04.md`). Each one passed a green suite, which is the second
half of what this change order has to fix: three of the five were invisible to
the tests because the tests assert HTML STRINGS while the browser assigns
meaning to that HTML. So each CR names both the fix and the guard that would
have caught it.

Ground rules for this change order (unchanged from `CLAUDE.md`):

- `CLAUDE.md` and the two subtree charters remain authoritative. Nothing here
  weakens the local-only guarantee, the one-file Ollama boundary, or the
  zero-CGo rule.
- **A change is not finished until its tests move with it** (`CLAUDE.md` §6).
  Each CR names the tests to add, update and delete. Both suites gate:
  `go test ./...` and `node --test "frontend/**/*.test.js"`.
- User-visible copy never contains em dashes (`copy_guard_test.go`,
  `frontend/copy.test.js`). Every proposed string below already obeys that.
- The parity guards are load-bearing. Two NEW ones are added here (CR5), for
  the same reason the existing ones exist: each is a mistake that already
  happened and passed every other test.
- Comments explain intent in the present tense. Do not write "used to be" or
  "CR3 changed this" into the code.
- **No retro-compatibility is required.** `SessionVersion` may be bumped and
  old session files refused, which is what §5 already mandates: refused, never
  migrated.

---

## 0. Why the suite did not catch any of this

Three distinct blind spots, and every CR closes the one it fell through:

| Blind spot | What it hides | Closed by |
|---|---|---|
| The view tests assert the HTML **string**, and `frontend/testhtml.js` preserves attribute case, while a real browser lower-cases attribute NAMES. | `data-mainText` renders, matches in tests, and is unreachable as `dataset.mainText` in the app. | CR4 + the camel-case data guard (CR5) |
| `ui.js icon(name)` returns the EMPTY STRING for a name absent from `ICONS`. Nothing fails; the control renders with no glyph. | Every help tooltip trigger is an invisible 1.15rem hit area. | CR3 + the icon-name guard (CR5) |
| The rendering harness measures the rail's SHAPE (counts, overflow, tooltip geometry) but never drives a value card's actions. | Rename, remove, group and solve all wired to `undefined`. | CR4 (new harness probe) |

---

## CR1 — Built-in patterns and Heuristic discovery sit side by side

### Current behaviour

`smartMethods(s)` (`frontend/views/identifyrail.js`, the `row()` helper) emits
three stacked `div.rail-toggle` rows inside one `.rail-block`:

```
[x] Built-in patterns            (i)
[x] Heuristic discovery          (i)
Signal-based suggestions   [2 sources v] (i)
```

`.rail-block` is `display: flex; flex-direction: column` (`style.css` ~773), so
the two plain method switches spend two full rows on two words each while the
rail is the panel the user already complained was too tall.

### Change

The two PLAIN switches become one horizontal pair; the signal control keeps its
own row, because it is not a plain switch (it expands, CR2).

1. In `smartMethods(s)`, wrap the two `row(...)` calls in a
   `<div class="rail-toggle-pair">` and leave `signalSourceControl(s)` as the
   following sibling. The `row()` helper itself is unchanged, so each half keeps
   its checkbox, its label and its help icon.
2. Add to `style.css`, beside the other `.rail-*` rules:

```css
/* The two plain Smart-detection methods are ONE row of two, not two rows of one.
   They are the shortest labels in the rail and the panel's height is its scarcest
   resource. The signal control stays on its own row because it expands. */
.rail-toggle-pair {
  display: grid; grid-template-columns: 1fr 1fr; gap: 0.6rem; align-items: center;
}
.rail-toggle-pair .rail-toggle { min-width: 0; }
.rail-toggle-pair .cat-label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
/* Below the rail's narrow measure the pair stacks rather than truncating both
   labels to nothing. */
@media (max-width: 1100px) {
  .rail-toggle-pair { grid-template-columns: 1fr; }
}
```

3. `.cat-row` carries a top border (`style.css` ~811) that reads as a list
   separator. Inside the pair that border draws a line between two side-by-side
   items, so suppress it: `.rail-toggle-pair .cat-row { border-top: none; }`.

No state, no reducer and no copy changes. The identifiers `#smart-built-in` and
`#smart-heuristic` stay, so `wireSmart` is untouched.

### Tests

- UPDATE `frontend/identifyrail.test.js`: assert that `#smart-built-in` and
  `#smart-heuristic` are BOTH inside one `.rail-toggle-pair`, and that
  `#signal-sources` is NOT.
- UPDATE `scripts/uitest/probes.js`: `configureRail()` gains
  `methodPairRow: { builtInTop, heuristicTop, sameRow }`, read from
  `getBoundingClientRect().top` of the two checkboxes (equal within 2px).
  Both harnesses read the one `probes.js` (`uitest_parity_test.go`), so this is
  one edit.

---

## CR2 — Signal-based suggestions becomes one expandable row per signal

### Current behaviour

`signalSourceControl(s)` renders `ui.js dropdownChecklist(...)`: a "Signal-based
suggestions" head with a summary button, opening a flat list of one checkbox per
SIGNAL (today exactly one, email). The state behind it is
`settings.signalSuggestionSources = { email: true }`
(`frontend/state.js`, `backend/engine/signals.go SignalSourceSelection`).

That control answers "which signals may derive suggestions" and nothing else. It
cannot answer the question the user actually has in front of a signal, which is
**what would this signal derive, and do I want each of those?** An email address
derives two different things through two different mechanisms
(`backend/engine/signaldiscovery.go`): `personSeeds(local, ...)` from the local
part, and `organisationSeeds(domain, ...)` from the domain. Today they are one
checkbox, so a user who wants organisations from domains but does not want
"pierre.dupont" read as a person has to switch both off.

### Change: a signal is a section, its derivations are its rows

The control becomes a list of **expandable signal rows**. Collapsed, one row per
signal with its master checkbox and a summary of what is on. Expanded, one row
per DERIVATION that signal supports, each individually switchable.

```
Signal-based suggestions                                    (i)
  v  [x] Email addresses            Person and organisation names
         [x] Person names      from the part before the @        (i)
         [x] Organisation names  from the domain                 (i)
```

Rules the shape has to obey:

- The signal's own checkbox is a **master over its derivations**, exactly as the
  Smart detection header is a master over its three methods: switching it off
  switches every derivation off, switching it on restores every derivation to
  its default. It is DERIVED for display (on when any derivation is on) and
  never stored as a fourth flag, for the reason `smartRouteOn` already states.
- Expanding is a VIEW preference, like `collapsedGroups` in the rail: module
  state, never in the store, never in a session file.
- Clearing a derivation stops the SUGGESTIONS and never stops the signal being
  anonymised. That invariant is `CLAUDE.md` §5 and it now has to hold per
  derivation. It is asserted at engine and bound-app level (below).
- Only derivations that ARE implemented appear. A row with nothing behind it is
  a control that appears to do something and does not.

#### The data model

`SignalSourceSelection` stops being `map[source]bool` and becomes a map of
source to derivation set. No retro-compatibility, so the old shape is deleted,
not aliased.

`backend/engine/signals.go`:

```go
// SignalDerivation identifiers: WHAT a signal derives, and by which mechanism.
// One per implemented mechanism in signaldiscovery.go: a row with no producer
// behind it is a control that appears to do something and does not.
const (
    // DerivationEmailPerson reads the local part of a matched address as a
    // person's name ("pierre.dupont@..." is evidence for Pierre Dupont).
    DerivationEmailPerson = "email.person"
    // DerivationEmailOrganisation reads the domain as an organisation's name
    // ("...@tpps.com" is evidence for Tpps).
    DerivationEmailOrganisation = "email.organisation"
)

// SignalDerivations lists, per signal source, the derivations it supports, in
// display order. It is the ONE definition of the tree the rail renders, mirrored
// by frontend/state.js SIGNAL_DERIVATIONS and guarded by
// ../../detection_parity_test.go.
var SignalDerivations = map[string][]string{
    SignalSourceEmail: {DerivationEmailPerson, DerivationEmailOrganisation},
}

// SignalSourceSelection is which DERIVATIONS may produce Suggestions, keyed by
// source and then by derivation.
//
// Nested rather than flat, because the two questions are nested: a source is a
// signal the pattern pass matched, a derivation is one reading of it. A flat
// map of dotted keys would let a derivation exist with no source above it.
type SignalSourceSelection map[string]map[string]bool
```

Functions to change in the same file, each keeping its current contract (a nil
or absent entry reads as the DEFAULT, never as "off"):

| Function | New signature / behaviour |
|---|---|
| `DefaultSignalSources()` | every known derivation on |
| `SignalSourceEnabled(sel, source)` | true when ANY of that source's derivations is on (the derived master) |
| `SignalDerivationEnabled(sel, source, derivation)` | NEW, the one the discovery pass consults |
| `ValidSignalSource(source)` | unchanged |
| `ValidSignalDerivation(source, derivation)` | NEW, refuses an unknown key rather than storing it |
| `NormaliseSignalSources(sel)` | fills every known source and derivation from the defaults, drops anything unknown |

`SignalDiscoveryInput.Sources` keeps its name and takes the new type.
`discoverFromEmails` gates each seed producer on its own derivation:

```go
if SignalDerivationEnabled(in.Sources, SignalSourceEmail, DerivationEmailPerson) {
    for _, seed := range personSeeds(local, address, doc.Name) { addSeed(seeds, seed) }
}
if SignalDerivationEnabled(in.Sources, SignalSourceEmail, DerivationEmailOrganisation) {
    for _, seed := range organisationSeeds(domain, address, doc.Name) { addSeed(seeds, seed) }
}
```

and `DiscoverFromSignals`'s early return becomes "no derivation of any source is
on".

`backend/app.go`: `Settings.SignalSuggestionSources` keeps its JSON key and
takes the new type; `ApplySettings`'s validation loop (app.go ~586) walks
source then derivation and refuses an unknown key with the same actionable
message shape it uses today, naming the valid set. `app_detect.go` ~298 stops
building a list of sources and passes the normalised selection straight through.

`backend/engine/session.go`: bump `SessionVersion` **7 to 8** and record the
reason beside the constant: *the signal-source selection is keyed by source and
derivation, so a version 7 file's booleans no longer describe what runs.* No
migration, per §5.

#### The frontend

`frontend/state.js`:

- `SIGNAL_SOURCES` stays (`["email"]`).
- NEW `SIGNAL_DERIVATIONS = { email: ["email.person", "email.organisation"] }`,
  mirroring `engine.SignalDerivations`, guarded by `detection_parity_test.go`.
- `signalSourceOn(s, source)` becomes DERIVED: any derivation on.
- NEW `signalDerivationOn(s, source, derivation)`, absent reads as on.
- NEW `setSignalDerivation(source, derivation, on)`, refusing an unknown pair.
- `setSignalSource(source, on)` stays as the MASTER: it writes every derivation
  of that source. Its comment says so, so the two are not confused.
- `smartDetectionOn` / `setSmartDetection` keep working through the master.
- `settings.signalSuggestionSources` default becomes
  `{ email: { "email.person": true, "email.organisation": true } }`.

`frontend/views/identifyrail.js`: `signalSourceControl(s)` stops calling
`dropdownChecklist` and calls a new `ui.js` builder, `expandableChecklist`:

```
expandableChecklist({
  id: "signal-sources",
  label: RAIL.signalSuggestions,
  helpHTML: helpTooltip(RAIL.signalSuggestionsHelp, { label: RAIL.signalSuggestions }),
  groups: SIGNAL_SOURCES.map((source) => ({
    id: source,
    label: RAIL.signalSourceLabel[source],
    summary: RAIL.signalDerivationSummary(onNames),   // "Person and organisation names" / "Off"
    checked: signalSourceOn(s, source),               // derived master
    open: openSignalSources.has(source),              // view state, module-local Set
    rows: SIGNAL_DERIVATIONS[source].map((d) => ({
      id: d,
      label: RAIL.signalDerivationLabel[d],
      detail: RAIL.signalDerivationFinds[d],
      helpHTML: helpTooltip(RAIL.signalDerivationHelp[d], { label: ... }),
      checked: signalDerivationOn(s, source, d),
    })),
  })),
})
```

`dropdownChecklist` and its CSS are DELETED once nothing calls it, together with
its tests: a builder kept "in case" is a second way to render the same control.

`wireSignalSources(container)` wires three things per group: the chevron
(`.checklist-group-toggle`, view state then repaint), the master checkbox
(`setSignalSource`, then `pushSettings`), and each derivation checkbox
(`setSignalDerivation`, then `pushSettings`). Escape closes the open group, as
today. Every checkbox `stopPropagation`s so ticking it does not fold the group,
the same guard `wireSectionSwitches` already carries.

#### Copy (`frontend/copy.js`, RAIL)

Delete `signalSourcesSummary`, `signalSourcesOff`, `signalSourceFinds` (the
checklist's). Add:

```js
signalSuggestions: "Signal-based suggestions",   // unchanged
signalSourceLabel: { email: "Email addresses" }, // unchanged
signalDerivationLabel: {
  "email.person": "Person names",
  "email.organisation": "Organisation names",
},
signalDerivationFinds: {
  "email.person": "from the part before the @",
  "email.organisation": "from the domain",
},
signalDerivationHelp: {
  "email.person": "Reads the part before the @ as a person's name, and suggests that name where it appears in prose elsewhere in the batch. Role mailboxes such as info@ and single-token handles derive nothing. Switching this off stops those suggestions and does not stop the address itself being anonymised.",
  "email.organisation": "Reads the domain as an organisation's name, and suggests that name where it appears in prose elsewhere in the batch. Public mail providers and public-suffix labels derive nothing. Switching this off stops those suggestions and does not stop the address itself being anonymised.",
},
/** signalDerivationSummary(names) is the collapsed row's read-out. */
signalDerivationSummary(names) { ... "Off" | "Person names" | "Person and organisation names" }
```

`frontend/BRIDGE.md`: update the `Settings.signalSuggestionSources` shape.
`frontend/docs/*`: update the sentence describing the control.

### Tests

Engine (`backend/engine/signals_test.go`, `signaldiscovery_test.go`):

- `TestSignalDerivationEnabledDefaultsOn` for nil, empty and partial selections.
- `TestPersonDerivationOffKeepsOrganisationSuggestions` and its mirror: with one
  derivation off, the other still produces its Suggestions.
- UPDATE `TestSessionRoundTripsSignalSources` to the nested shape.
- UPDATE the existing `Sources: SignalSourceSelection{SignalSourceEmail: true}`
  literals across `signaldiscovery_test.go`, `app_detect_test.go`,
  `app_e2e_test.go`.
- KEEP and re-point the invariant test: a derivation off must NOT stop the email
  category being anonymised, asserted at engine and at bound-app level.
- `TestApplySettingsRefusesAnUnknownSignalDerivation`, beside the existing
  unknown-source test.
- `TestSessionVersionIsEight` (or update the existing version assertion), plus
  the refusal of a version 7 file.

Frontend:

- `frontend/state.test.js`: derived master, per-derivation set, unknown pair
  refused, `setSignalSource` writing every derivation, `setSmartDetection`
  clearing them all, `smartDetectionOn` reading them.
- `frontend/ui.test.js`: `expandableChecklist` markup, one group per source, one
  row per derivation, open and closed states; DELETE the `dropdownChecklist`
  tests with the builder.
- `frontend/identifyrail.test.js`: the rail renders one group per
  `SIGNAL_SOURCES` entry and one row per `SIGNAL_DERIVATIONS` entry, and every
  derivation has a help tooltip.
- `detection_parity_test.go`: `SIGNAL_DERIVATIONS` versus
  `engine.SignalDerivations`, and a label present for every derivation
  (`assertLabelled`, as `signalSourceLabel` already is).
- `scripts/uitest/probes.js`: `configureRail()` reports
  `signalGroups`, `signalRowsWhenOpen`, and that expanding a group reveals rows
  with non-zero height (the geometry the string tests cannot see).

---

## CR3 — Discovery strictness: indentation, field width, and the missing help icon

Three defects in one panel, and one of them (the icon) is why NO tooltip
anywhere in the application appears.

### CR3a — The help icon has no glyph (this is regression 4)

`ui.js helpTooltip()` renders `${icon("info")}`. `ICONS` (`frontend/icons.js`)
has no `"info"` entry, and `icon(name)` returns `""` for an unknown name
(`ui.js` ~22). So every help trigger in the application is an empty circle,
1.15rem of invisible hit area: the tooltip works exactly as designed and there
is nothing on screen to hover. `identifyworkspace.js` ~780 loses the same glyph
in the intersection note.

Fix:

1. Vendor `frontend/assets/icons/info.svg` from Material Symbols Outlined
   (Apache-2.0, the licence already covers the folder) and add the `"info"`
   entry to `ICONS`, `fill="currentColor"`, `viewBox="0 -960 960 960"`, in
   alphabetical position. The map already holds `"help"`, unused; either use
   `"help"` at the call site or add `"info"`, not both. **Decision: add
   `"info"`** and keep `helpTooltip` unchanged, because "help" reads as
   documentation and "info" as an explanation of the control beside it.
2. Give the trigger a visible resting shape so it is discoverable, not only
   visible on hover:

```css
.help-icon {
  /* ... existing ... */
  border: 1px solid var(--gray-4); background: var(--bg);
}
.help-icon svg { width: 0.9rem; height: 0.9rem; }
```

3. CR5 adds the guard: every icon name any frontend module passes to `icon()` or
   to `button({icon})` exists in `ICONS`.

### CR3b — The bubble must not be clipped

`.help-bubble` documents itself as `position: fixed` precisely so the rail's
`overflow: auto` body cannot clip it, and then
`.help[data-open] .help-bubble` overrides it back to `position: absolute`
(`style.css` ~1553). `.cgroup` is `overflow: hidden` (~364) and the rail body is
`overflow-y: auto` (~231), so an open bubble is clipped at the group's edge, and
near the foot of a group nothing readable is left.

Fix: keep ONE positioning model, the fixed one the comment promises.
`wireHelpTooltips` computes the bubble's coordinates when it opens and writes
them to the `--help-x` / `--help-y` custom properties the base rule already
reads:

- measure the icon with `getBoundingClientRect()`;
- prefer below and left-aligned to the icon's right edge minus the bubble width,
  clamped into the viewport with an 8px margin;
- flip above when there is not enough room below.

The `[data-open]` rule then only sets `display: block`. That is the shape the
rendering harness's `helpTooltipVisibility()` probe already measures, so the
probe starts meaning what it says.

### CR3c — Indentation and the strictness dropdown's width

`.rail-section > .cgroup-body` gets `padding: 0.9rem 1.1rem` (`style.css` ~748),
but `.rail-subgroup` (the "Discovery strictness" block, nested inside the Smart
detection section) has no such rule, so its body sits flush at zero padding and
its four fields hang left of every label above them.

`.rail-field` is `grid-template-columns: 1fr 6rem` (~780), so the "How much to
trust" `<select>` is 6rem wide: narrower than its own longest option.

Fix:

```css
/* A subgroup's body takes the same inset as a section's, so a nested field lines
   up with the section labels above it rather than hanging left of them. */
.rail-subgroup > .cgroup-body { padding: 0.75rem 1.1rem; gap: 0.6rem; }

/* The control column is wide enough for the widest option of the widest select
   in it. A 6rem column truncated "How much to trust" to an unreadable stub. */
.rail-field { grid-template-columns: 1fr 18rem; }
/* The three number fields do not need the select's measure; they keep a compact
   box inside the same column, so the column stays one width and the boxes still
   read as numbers. */
.rail-field input[type="number"] { width: 6rem; }
```

18rem is the "3 times larger" the report asks for (6rem to 18rem). Where the
rail is narrower than the pair can hold, `.rail-field` falls back to stacked
label and control under the existing `@media (max-width: 1100px)` block.

Check the two other users of `.rail-field` when changing its grid: the Local AI
port/model fields and `.rail-range` (which overrides
`grid-template-columns: auto 1fr` already, so it is unaffected).

### Tests

- `frontend/ui.test.js`: `helpTooltip` renders a NON-EMPTY icon (assert the
  `<svg` is present, which is the assertion that would have failed);
  `wireHelpTooltips` sets `--help-x` / `--help-y` on open and clears
  `data-open` on Escape.
- `frontend/icons.test.js` (NEW, or in `ui.test.js`): `ICONS.info` exists and
  carries `fill="currentColor"`.
- `scripts/uitest/probes.js`: extend `helpTooltipVisibility()` to report the
  trigger's rendered `width`, `height` and `hasGlyph` (an `svg` child), and to
  assert the open bubble's rect is inside the viewport AND not clipped by the
  nearest scrolling ancestor. Add `strictnessFieldWidth`: the rendered width of
  `#smart-strictness` and the left offset of `.rail-subgroup .rail-field-label`
  against a section label above it (the indentation, in pixels).
- `frontend/identifyrail.test.js`: the strictness block is a `.rail-subgroup`
  and every field row carries a help tooltip.

---

## CR4 — The "My values" card actions do nothing (this is regression 5)

### Root cause

`valueCard` renders the card with

```
data-category="..." data-mainText="..."
```

(`frontend/views/identifyworkspace.js` ~713, and again on the group-pick
checkbox ~742, and in `frontend/views/anonymise.js` ~288).

An HTML parser lower-cases attribute NAMES, so the DOM holds
`data-maintext`, whose `dataset` key is `maintext`. Every handler reads
`dataset.mainText`, which is `undefined`. Before PR#46 the attribute was
`data-canonical`, all lower case, so the rename to `mainText` broke it.

Everything that reads it fails, which is exactly the reported set:

| Reported symptom | Handler | Reads |
|---|---|---|
| Changing the Value's main text does nothing | `revealNameInput(cardEl, cat, mainText, key)` | `mainText === undefined` |
| "Stop replacing this spelling here" does nothing | `wireSolvePanel` `drop-spelling` | `deleteVariant(cat, undefined, ...)` |
| "Merge with [...]" does nothing | `wireSolvePanel` `merge` | `groupValues({category, mainText: undefined}, ...)` |
| The card's top-right remove does nothing | `.value-remove` | `deleteValue(cat, undefined)` |
| (also broken, unreported) Group with Apply | `wireGroupPanel` reads `cb.dataset.mainText` | undefined |
| (also broken, unreported) dragging a spelling onto another card | `wireVariantDrag` reads `card.dataset.mainText` | undefined |
| (also broken, unreported) `changeValueCategory` | reads the same `mainText` | undefined |

`data-key` and `data-category` are lower case, which is why the panels still
OPEN (`togglePanel(key, ...)` works) and only the actions inside them fail. That
is precisely the "like if they were broken" the report describes.

### Change

1. Rename the attribute to `data-main-text` at all three emission sites and read
   it as `dataset.mainText` (the dash-to-camel mapping the DOM actually
   performs). Sites: `identifyworkspace.js` valueCard (~713), groupPanel option
   (~742), `anonymise.js` (~288).
2. Sweep for the same shape anywhere else: the audit in CR5 is the permanent
   version, but do the one-off `grep -n 'data-[a-z]*[A-Z]'` over
   `frontend/**/*.js` as part of this CR and fix every hit.
3. While in `wireValues`, make the failure LOUD rather than silent: if a card's
   dataset is missing `category` or `mainText`, `notify` an actionable message
   and return early. A handler that quietly does nothing is the bug being fixed;
   a handler that says "this card lost its identity, reopen the step" is
   debuggable by the owner.

```js
// A card with no identity cannot act on a Value. It must SAY so: a handler that
// silently returns is indistinguishable from a button that is not wired, which
// is the failure this guard exists to prevent.
const { category: cat, mainText, key } = cardEl.dataset;
if (!cat || !mainText) {
  notify(WORKSPACE.cardIdentityLost, "warn");
  continue;
}
```

with `WORKSPACE.cardIdentityLost = "This value card lost its identity, so its
actions are disabled. Leave the step and come back to rebuild the list."`

### Tests

- `frontend/identifyvalues.test.js`: assert the RENDERED attribute is
  `data-main-text` and that no rendered attribute name in the values tab
  contains an upper-case letter (the local half of CR5's guard).
- NEW `frontend/identifyactions.test.js`: drive the four reported actions
  through the handlers against a DOM-shaped fake whose `dataset` is built by
  LOWER-CASING attribute names, so the fake behaves like a parser. Assert state
  after each: rename changes `mainText`, remove drops the Value,
  `drop-spelling` removes only that spelling, `merge` folds the two Values into
  one family. These four tests are the ones that would have caught the
  regression.
- `scripts/uitest/probes.js`: NEW `valueCardActions()` probe, in the real
  browser: seed one Value, click `.value-name`, type, blur, and report the
  store's `mainText`; then click `.value-remove` and report `values.length`.
  This is the layer that sees attribute lower-casing, so it is the one that has
  to try.

---

## CR5 — The two guards that make CR3 and CR4 unrepeatable

Both are one-file Go tests beside the existing parity guards, reading the
frontend sources as text, exactly as `category_parity_test.go` does.

### CR5a — `dataset_parity_test.go`: no camel-case data attribute

Scan every `frontend/**/*.js` (excluding `*.test.js`) for
`data-[a-zA-Z-]*` inside a rendered string, and fail on any whose name carries
an upper-case letter. The message names the file, the attribute, and the fix:

> `frontend/views/identifyworkspace.js` renders `data-mainText`. A browser
> lower-cases attribute names, so `dataset.mainText` is undefined at runtime and
> the handler reading it does nothing. Write `data-main-text` and read
> `dataset.mainText`.

`frontend/identifyrail.test.js` already asserts this for ONE button
(`data-group-type`); that local assertion stays, and this guard generalises it.

### CR5b — `icon_parity_test.go`: every icon name exists

Collect every `icon("name")` argument and every `icon: "name"` option across
`frontend/**/*.js`, and every key of `ICONS` in `frontend/icons.js`. Fail when a
used name is absent (the CR3a bug: a silent empty string), and fail when an
`ICONS` key is used nowhere (dead vendored markup, which the audit layer's
dead-export scanner cannot see inside an object literal).

Both guards are listed in `CLAUDE.md` §6's load-bearing list and in
`docs/UITESTING.md`.

---

## Decisions taken

1. **Per-derivation switches, with the signal row as a derived master.** The
   report asks for "expand a signal, see which suggestions can be derived from
   it, activate or deactivate". That is only truthful if the engine honours the
   individual switches, so CR2 changes the engine, not only the rail. The
   signal's own checkbox stays as a master because a set with no "all" gesture
   makes switching a signal off an N-click job.
2. **`SessionVersion` 7 to 8, no migration.** §5 forbids a migration table for a
   file holding the re-identification key. No retro-compatibility was requested.
3. **`"info"` is added to `ICONS` rather than reusing `"help"`.** Two glyphs mean
   two things: "help" is the documentation window, "info" explains the control
   it sits beside.
4. **One positioning model for the help bubble, the fixed one.** The bubble
   escapes `overflow` containers by being fixed and positioned from JavaScript;
   the CSS override that reverted it to absolute is deleted rather than patched
   with a `z-index`.
5. **18rem for the `.rail-field` control column**, the "3 times larger" asked
   for, with the three number inputs kept at 6rem inside it so the column has
   one width and a number field still reads as a number.
6. **`dropdownChecklist` is deleted, not kept beside its replacement.** Two
   builders for one control is the next inconsistency.
7. **Silent handlers become loud.** Wherever a card action cannot identify its
   Value, it says so. Four of the five reported regressions were silent.

## Conflict analysis

### Files touched by more than one CR

| File | CRs | Note |
|---|---|---|
| `frontend/views/identifyrail.js` | CR1, CR2, CR3c | CR1 and CR2 both edit `smartMethods`; do CR1 first, it is three lines |
| `frontend/ui.js` | CR2, CR3a, CR3b | `expandableChecklist` added, `dropdownChecklist` deleted, `helpTooltip` and `wireHelpTooltips` changed |
| `frontend/style.css` | CR1, CR2, CR3a, CR3b, CR3c | four separate rule blocks, no overlap; the `.help` block is rewritten once |
| `frontend/copy.js` | CR2, CR4 | RAIL signal copy, WORKSPACE identity message |
| `frontend/state.js` | CR2 | the only structural state change in the whole order |
| `scripts/uitest/probes.js` | CR1, CR2, CR3, CR4 | ONE file, both harnesses (`uitest_parity_test.go`); batch the probe edits at the end of each CR |
| `backend/engine/signals.go` + callers | CR2 | the widest blast radius: app.go, app_detect.go, app_export.go, session.go and their tests |

### Hotspots

- **`SignalSourceSelection`'s type change** touches every test literal that
  writes it. Change the type first, let `go build ./...` list the call sites,
  then fix them; do not grep.
- **`.rail-field`'s grid** is shared with the Local AI fields. Check the Local AI
  section visually (or via the harness's `layout` probe) after CR3c.
- **`setSmartDetection`** writes `signalSuggestionSources` wholesale; it must
  write the nested shape or Smart detection's master silently stops working.

## Recommended order

1. **CR4** first, alone, and pushed on its own. It is the functional regression,
   it is three attribute renames plus a guard, and it depends on nothing else.
2. **CR5a and CR5b** immediately after: they are the guards for CR4 and CR3a,
   and running them proves the sweep in CR4 was complete.
3. **CR3a** (the `info` icon), which makes every tooltip in the application
   visible, then **CR3b** (positioning), then **CR3c** (indentation, width).
   Ordered so each step is verifiable by eye in the running app.
4. **CR1**, three lines and one CSS block.
5. **CR2** last, and largest: engine type, session bump, state, builder, copy,
   docs. It is the only CR that changes a contract, so it lands on a tree where
   everything else is already green.

## Acceptance criteria

- `go test ./...` and `node --test "frontend/**/*.test.js"` both green,
  including the two new guards.
- `task audit` reports no new finding.
- In the running application, on step 2:
  1. "Built-in patterns" and "Heuristic discovery" are on one line, side by
     side, each with a visible help icon.
  2. "Signal-based suggestions" lists "Email addresses" as an expandable row;
     expanding shows "Person names" and "Organisation names" with independent
     checkboxes; clearing one still anonymises email addresses in the run, and
     stops only that reading's Suggestions.
  3. The "Discovery strictness" fields align with the section labels above them
     and "How much to trust" shows its longest option in full.
  4. Hovering any help icon shows its text, fully readable, not clipped by the
     rail, and Escape dismisses it.
  5. On a "My values" card: editing the name renames the Value, the top-right
     remove deletes it, "Stop replacing this spelling here" removes that
     spelling, and "Merge with [...]" folds the two Values into one family.
- The rendering harness passes, with the new probes: the method pair on one row,
  the signal rows revealed by expanding, a help trigger with a glyph and an
  unclipped bubble, the strictness field's width and indentation, and a value
  card whose rename and remove change the store.

## First actions for the implementation coordinator

1. Read `CLAUDE.md`, `frontend/CLAUDE.md`, `backend/CLAUDE.md`,
   `frontend/BRIDGE.md` and `docs/UITESTING.md`.
2. `grep -rn 'data-[a-z]*[A-Z]' frontend/` and fix every hit (CR4).
3. Write `dataset_parity_test.go` and `icon_parity_test.go` and watch them fail
   before the fixes, then pass after (CR5).
4. Only then start CR3, CR1 and CR2, in that order.

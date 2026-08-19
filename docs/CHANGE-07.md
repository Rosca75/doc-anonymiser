# CHANGE-07 — Declaring Values while reviewing on step 3: the rule restated, and the six defects behind it

You are executing a change order against the existing **doc-anonymiser**
repository (pattern P0, pure Go + Wails v2, no CGo, no npm). It holds **one
self-contained implementation section per change request (CR1 to CR7)**, followed
by the **decisions taken**, a **conflict analysis**, the **recommended execution
sequence** and the **acceptance criteria**.

Every CR below comes from an owner review of the BUILT application on step 3,
Anonymise. They fall into three groups: two defects that break the
declare-a-Value-while-reviewing feature outright (CR1, CR2), three that make it
silent, unsafe or inconsistent with the same gesture on step 2 (CR3, CR4, CR5),
and a documentation correction that removes a claim the code does not make and
never made (CR6, CR7).

Ground rules for this change order (unchanged from `CLAUDE.md`):

- `CLAUDE.md` and the two subtree charters remain authoritative. Nothing here
  weakens the local-only guarantee, the one-file Ollama boundary, or the
  zero-CGo rule.
- **A change is not finished until its tests move with it** (`CLAUDE.md` §6).
  Each CR names the tests to add, update and delete. Both suites gate:
  `go test ./...` and `node --test "frontend/**/*.test.js"`.
- User-visible copy never contains em dashes (`copy_guard_test.go`,
  `frontend/copy.test.js`). Every proposed string below already obeys that.
- The parity guards are load-bearing (`category_parity_test.go`,
  `detection_parity_test.go`, `dataset_parity_test.go`, `icon_parity_test.go`,
  `value_shape_test.go`). Nothing here may render a camel-case `data-`
  attribute or call `icon()` with a name absent from `ICONS`.
- Comments explain intent in the present tense. Do not write "used to be" or
  "CR1 changed this" into the code.
- **No retro-compatibility is required.** No session-file shape changes in this
  order (see the decisions), so `SessionVersion` stays **8**.

---

## 0. Cold-start context for the implementing session

Read this section first if you are picking this document up with no
conversation history. It is everything the diagnosing session established.

### Where the work stands

| Fact | Value |
|---|---|
| Repository | `Rosca75/doc-anonymiser`, module path `doc-anonymiser` |
| Branch to develop and push on | `claude/anonymise-values-review-atszwo` |
| Its current head | this document, and nothing else. No code has changed yet. |
| Suites at this head | BOTH GREEN. `node --test "frontend/**/*.test.js"` 666 pass / 0 fail; `go test ./...` ok for every package |
| Audit | `task audit` (go-task, no make) |
| Real-rendering harness | `docs/UITESTING.md`; the Linux Chromium harness is a BLOCKING CI step |

### The rule this change order restates

A charter sentence is wrong, and it is wrong in a way that invites an agent to
delete a feature. Three files say some version of **"Anonymise creates no
Value"** (`CLAUDE.md` §1 line 16 and §5 line 362, `backend/CLAUDE.md` line 47,
`frontend/BRIDGE.md` line 257). Read literally, that forbids the step 3 review
surface the application ships and the owner relies on.

The true rule has two halves, and only the first was ever meant:

1. **No DISCOVERY method runs during a pipeline run, and no model is reached.**
   `engine.Run` has no LLM slot; every discovery method runs at Identify time
   and produces Suggestions the user accepts. Nothing in a run can mint a Value
   the user never saw. `TestAnonymiseNeverCallsOllama` asserts the call count is
   zero and stays.
2. **The user may still DECLARE a Value while reviewing on step 3**, and that is
   a strength of the application, not a leak in the review gate. A declaration
   is the user acting, so it passes the gate by definition: the gate exists to
   stop an unreviewed MACHINE finding reaching the text, not to stop the person
   reviewing the result from fixing what the machine missed.

So the accurate statement is: **anonymisation runs no discovery method and
reaches no model; the only Value it can apply is one the user accepted or
declared.** CR7 writes that into the four places that currently say otherwise.

### The three surfaces the rule lives on (all present in the built app)

| Surface | Where | What it does |
|---|---|---|
| Compare pane selection, "Replace" then "Make it a spelling of an existing Value" | `frontend/views/anonymise.js` `selectionStageFields` / `applySelection` (~835, ~1459) | the selected text becomes a spelling of an existing Value and shares its placeholder |
| Compare pane selection, "Replace" then "Add it as a new Value" | same | the selected text becomes a Value of its own with its own placeholder |
| "Add missed Value" card, left column | `frontend/views/anonymise.js` `missedCard` / `wireMissed` (~570, ~1093) | declare a Value by typing it, then "Re-run fast passes" applies it |

All three end in `runFastRerun` (~1532), which calls `FastRerun` on Go and then
refreshes the mapping and the two value tables.

### What is already correct (do NOT rewrite these paths)

The owner asked for three propagation guarantees. All three hold **once the
Value carries a real category and matches text in the batch**, and each was read
out of the code rather than assumed:

1. **Into the JSON.** `App.SaveSessionToFile` (`backend/app_export.go` ~487)
   writes `req.Values` straight from the frontend's `buildRunRequest()`
   (`frontend/state.js` ~2654), which reads `acceptedValues(state)`. A Value
   added on step 3 lands in `state.values` like any other, so it is in the
   session JSON, in the mapping JSON (`exportMapping("json")`, read from the
   registry) and in the report JSON (`Report.Values`).
2. **Into the Report section.** `engine.Run` assembles `Report.Values` and each
   `DocumentReport.Values` from the registry and the finished text once per run
   (`backend/engine/pipeline.go` ~388 to ~413). `FastRerun` goes through the
   same `runPipelineBlocking`, so a fast re-run rebuilds the report including
   the new Value, and `reportCard` reads `report.values` rather than recounting.
3. **Into the Replaced values section.** `App.ValuePlaceholders`
   (`backend/app_values.go` ~75) returns `registry.Export()`, and
   `refreshValueTables` (`anonymise.js` ~1086) mirrors it into
   `state.replacedValues` after every fast re-run.

Two more things are right and must survive this order:

- **Going back from Anonymise to Identify does not lose the Values.**
  `resetStepsForBackward` resets the steps AFTER the target, so a 3 to 2 move
  resets only `anonymise` (`state.js` STEP_RESETS ~1288). `values`,
  `suggestions`, `patterns` and the allowlist belong to Identify, which is the
  target, so they survive. A Value declared on step 3 is therefore waiting on
  step 2 as a normal value card carrying `discoveryMethods: ["manual"]`.
- **The registry is deliberately discarded by that move.** `nav.js` calls
  `resetRun()` (~84), and `App.ResetRun` (`backend/app.go` ~276) drops the
  registry, the results, the remembered request and the removal list so a
  re-run renumbers from 1 and stops hiding previously removed values. That is
  intended. CR5 and CR6 exist because the COPY and the Save-profile gate
  describe this move wrongly, not because the move is wrong.

### What was established by diagnosis (do not re-derive)

Each root cause below was read out of the code, and where it is observable it
was reproduced.

**Finding 1: the "Add it as a new Value" type dropdown emits a broken category
key, and the Value is then dropped in silence.** `selectionStageFields`
(`anonymise.js` ~853) iterates `CATEGORIES` as if it were a list of strings:

```js
CATEGORIES.map((c) =>
  `<option value="${escapeHTML(c)}"${c === view.category ? " selected" : ""}>` +
  `${escapeHTML(CATEGORY_LABELS[c]?.label ?? c)}</option>`)
```

`CATEGORIES` is a list of `[key, label]` PAIRS
(`frontend/views/identifyworkspace.js` line 80,
`NAME_CATEGORIES.map((c) => [c, CATEGORY_LABELS[c][0]])`), and `CATEGORY_LABELS`
values are `[label, example]` pairs, so `.label` is always `undefined`. Rendered
today (reproduced with node against the real modules):

```html
<option value="entity_names,Entity names">entity_names,Entity names</option>
<option value="person_names,Person names">person_names,Person names</option>
```

Three consequences, in order of severity:

- Nothing is ever pre-selected, because `c === view.category` compares an array
  with a string.
- Choosing a type writes `selectionCategory = "person_names,Person names"`
  (`anonymise.js` ~1433), and `applySelection` then calls
  `addValues([{category: "person_names,Person names", ...}])`.
- `addValues` (`state.js` ~1555) only guards the category SWITCH with
  `ALL_CATEGORIES.includes(item.category)` (~1567); the Value itself is added
  with the phantom category. On the next run `engine.Run` calls
  `filterValues(in.Values, sel)` (`pipeline.go` ~300, ~509), the phantom key is
  absent from the selection, and **the Value is dropped before validation**
  (`pipeline.go` ~302 validates the FILTERED list). No replacement, no registry
  entry, no report row, no Replaced values row, no warning. It does reach the
  session JSON, where it is a Value that can never apply.

  So the owner's three propagation guarantees fail together, and they fail for
  one reason: the type the user picked is not a type. The default path (never
  touching the dropdown) works, because the module default
  `selectionCategory = "person_names"` (~90) is a real key. That is why the
  feature looks half-working rather than broken.

  `missedCard` (~575) destructures the same list correctly, which is why "Add
  missed Value" is unaffected.

**Finding 2: the "Make it a spelling of" field accepts one letter and never
autocompletes.** Two independent causes in the same control:

- **Focus.** Its `input` handler (`anonymise.js` ~1427) does
  `selectionTarget = target.value; setState({})`. `setState` repaints the whole
  view, `compareCard` and the panel inside it are rebuilt from strings, and the
  input element the user is typing into is destroyed. Nothing restores focus or
  the caret, so the second keystroke goes nowhere. The two other search fields
  on this same screen both solve exactly this and are the precedent: the
  reassign field re-focuses and calls `setSelectionRange` after the repaint
  (~948), and the Compare search debounces 150 ms and then restores focus and
  caret (~1128).
- **Suggestions.** The suggestion source is a native `<datalist>` rebuilt from
  `valueAutocomplete(view.target, s)` on each repaint, and
  `valueAutocomplete("")` returns `[]` by contract (`state.js` ~2424). So the
  list is empty on the render the user starts typing into, and every repaint
  destroys and recreates the `<datalist>`, which closes the platform popup. Even
  with focus fixed, a native dropdown that is rebuilt mid-keystroke inside a
  floating panel is the wrong control here.

  The Selected placeholder card answers the identical question ("which Value is
  this a spelling of") and works, because it renders REAL buttons from
  `valueAutocomplete(...).slice(0, 6)` (`selectedCard` ~289, wired at ~962).
  That is the control to reuse.

  This matters more than a typing annoyance: `applySelection` requires an EXACT
  case-insensitive match on an existing Value's `mainText` (~1475), so a
  free-text field that cannot be typed into can only produce
  `ANONYMISE.selectionUnknownTarget`. The whole spelling mode is unreachable in
  the built app.

**Finding 3: a Value that cannot apply, or that matches nothing, says nothing.**
Three silent drops on one path:

- `addValues` accepts an unknown category (Finding 1).
- `filterValues` drops it with no warning, by design for a switched-OFF
  category (the user turned that switch off) but wrongly for a category that is
  not an engine category at all, because no switch can ever turn it on.
- A Value that IS applied but matches no text in the batch gets no registry
  entry, so it appears in neither the Report nor the Replaced values table,
  while `runFastRerun` still reports success
  (`ANONYMISE.selectionBecameValue(text)`, `ANONYMISE.fastRerunDone(n)`). The
  user is told the Value was added and then cannot find it anywhere.

  Note for CR3: `res.Validation.Warnings` is NOT rendered anywhere on step 3.
  `visibleWarnings` reads `results.report.warnings` (`state.js` ~2503) and
  `blockingConflicts` reads `results.validation.blocking` (~2524); the warnings
  half of validation is dropped on the floor at run time. So a new engine
  warning has to land in `Report.Warnings` to be seen.

**Finding 4: the backward-move confirmation names the wrong survivors.**
`NAV.backConfirmBody` (`copy.js` ~90) ends with "Your imported documents and
your never anonymise list are kept." Leaving Anonymise for Identify also keeps
the Values, the Suggestions, the patterns and every setting, because Identify is
the target and a target keeps its own data. Enumerating two survivors implies
the others are lost, which is precisely the fear that stops a user stepping back
to look at a Value they just declared on step 3. `frontend/copy.test.js` ~101
asserts the misleading sentence, so the test moves with the copy.

**Finding 5: "Save profile" is gated on a fact that does not imply a
registry.** `profileSection` disables Save unless `s.detectionRan`
(`identifyrail.js` ~224), and `wireProfile` repeats the guard with the comment
"so a click that slips through a stale DOM cannot save an empty registry"
(~957). The gate does not do that:

- Detection mints no placeholders. `backend/app_detect.go` contains no
  reference to the registry at all; only `engine.Run` assigns. So immediately
  after a detection and before any run, `detectionRan` is true and the registry
  is empty.
- Worse in the other direction: stepping back from Anonymise to Identify calls
  `ResetRun`, which sets `a.registry = nil`, while `detectionRan` stays true
  (the Identify reset is not applied when Identify is the TARGET). So the one
  route a user takes after declaring Values on step 3 leaves Save enabled and
  saving `registry: []`.

The owner's instruction stands: the Save option belongs on Identify and nowhere
else. Two things follow, and CR6 does both: gate it on what Go actually holds,
and stop the Export step calling a second control by the same name
(`export.js` ~413 fires `saveSession` under `EXPORT.sessionSaveConfirm`, which
is the string "Save profile", `copy.js` ~1165).

**Finding 6: minor, in the same code, worth fixing while it is open.**

- `applySelection` ignores the `addValues` return value (~1499), so
  re-declaring an existing Value reports "became a Value" having added nothing.
- `wireMissed` (~1102) declares the Value WITHOUT `foldIntoFamily`, while the
  step 2 add row folds first (`identifyworkspace.js` ~1534) precisely so
  "Coca-Cola company" beside "Coca-Cola" does not leave the text reading
  `[BRAND_1] company`. The step 3 selection path already folds (~1494); the card
  is the odd one out.
- `wireMissed` also has no match-count read-out, while the step 2 add row shows
  "Found N times in M documents" from `countTermMatches` (~1509).
- The module comment on `selectedMark` (~73) promises `{placeholder, original,
  category}`; the click handler stores only the first two (~1243). Nothing reads
  the third.
- `export.js` calls `buildRunRequest(false)` (~415); `buildRunRequest` takes no
  parameters (`state.js` ~2654). The argument is dead.

---

## CR1 — The new-Value type dropdown must emit a real category

### Symptom

On step 3, select text in a pane, "Replace", "Add it as a new Value": the type
dropdown reads `person_names,Person names`, nothing is pre-selected, and the
declared Value never appears in the anonymised text, the Report, the Replaced
values table or the mapping.

### Root cause

Finding 1. `CATEGORIES` is a pair list and `selectionStageFields` reads it as a
string list.

### The change

There are two dropdowns of this exact kind on step 3 (`missedCard` and the
selection panel) and a third builder for it already exists and is correct:
`categorySelect(selected, opts)` in `frontend/views/identifyworkspace.js` (~97),
used by the add row, the suggestion row and the value card. The frontend charter
requires exactly ONE way to draw each thing, so:

1. **Export `categorySelect`** from `identifyworkspace.js` (it already exports
   `CATEGORIES` next to it). Keep it there rather than moving it to `ui.js`:
   `ui.js` imports only `html.js` and `icons.js`, and `CATEGORIES` derives from
   `state.js` and `copy.js`, so moving the builder would widen the toolkit's
   import surface. If the owner prefers it in `ui.js`, `CATEGORIES` moves with
   it in one move, not two.
2. In `anonymise.js`, replace BOTH hand-rolled `<select>` bodies with it:
   - `selectionStageFields`:
     `categorySelect(view.category, { id: "selection-category", ariaLabel: ANONYMISE.selectionTypeLabel })`
   - `missedCard`:
     `categorySelect(drafts.missedCategory, { id: "missed-category", ariaLabel: ANONYMISE.missedCategoryLabel })`
3. Delete the local `CATEGORY_LABELS[c]?.label` lookup. It is dead the moment
   the builder is shared, and it was never a valid shape.

### Files

- `frontend/views/identifyworkspace.js` (export one function)
- `frontend/views/anonymise.js` (`selectionStageFields`, `missedCard`, imports)

### Tests

- ADD, `frontend/anonymise.test.js`: render the panel in `value` mode and assert
  that EVERY `option` value is a member of `NAME_CATEGORIES`, and that the
  option matching `view.category` carries `selected`. Assert the same for
  `missedCard`. This is the guard that kills the bug class: the existing
  assertions (~597 to ~600) only check that the `select` exists, which is why
  this shipped.
- ADD, `frontend/anonymise.test.js` (wiring, `testdom.js`): choose a category in
  the panel, Apply, and assert the Value that reaches the store carries that
  exact key and that `settings.categories[key]` is now true.
- OPTIONAL, `scripts/uitest/probes.js`: a probe that opens the panel in `value`
  mode in the real engine and returns the option values, since the harness is
  the only layer that renders a native `select`. Add it if it does not push the
  harness over its step budget.

---

## CR2 — The "Make it a spelling of" field must be typeable and must suggest

### Symptom

Selection, "Replace", "Make it a spelling of an existing Value": the search
field takes one letter and stops, and it never autocompletes from what was
typed. The mode cannot be completed.

### Root cause

Finding 2. The `input` handler repaints the whole view and nothing restores
focus or the caret; and the suggestion source is a `<datalist>` that is empty on
the first render and destroyed on every repaint.

### The change

Reuse the control that already works for the same question, and stop repainting
the panes on a keystroke:

1. **Render suggestions as buttons, not a datalist.** In
   `selectionStageFields`, replace the `<input list=...>` plus `<datalist>` with
   the shape `selectedCard` uses: the text input, then a
   `<div class="reassign-list">` of up to 6 `.selection-pick` buttons built from
   `valueAutocomplete(view.target, s)`, each carrying `data-category` and
   `data-main-text` and showing the category label as a hint. Reuse the existing
   `.reassign-list` / `.reassign-pick` styling; do not add a second look for the
   same list.
2. **Do not repaint on a keystroke.** Wire the field the way the Replaced values
   filter is wired (`wireValues` ~1021): on `input`, write `selectionTarget` and
   patch the suggestion list's `innerHTML` IN PLACE, then re-bind the picks. No
   `setState`, so the input is never destroyed, the caret never moves, and the
   two Compare panes are not re-highlighted on every letter. This is strictly
   better than the debounce-and-refocus dance, and it is an established pattern
   in this file.
3. **Clicking a pick selects the target**: it fills the field with that Value's
   `mainText` and stores the chosen `{category, mainText}` in view state, so
   Apply no longer has to re-resolve the text. Keep Apply and Cancel as they
   are: the panel's two-step shape is deliberate, and applying on a single click
   would make a mis-click a text change.
4. **Apply keeps its refusal** for a target typed by hand that matches nothing
   (`ANONYMISE.selectionUnknownTarget`), because a user may still type. When a
   pick was clicked, that path is unreachable.
5. While here, make `applySelection`'s new-Value branch honest: use the
   `addValues` return value and report `ANONYMISE.missedAlreadyThere(text)` (or a
   sibling string) when it added nothing, instead of claiming the text became a
   Value (Finding 6).

No new copy is required for the happy path. If the empty-query state needs a
line, use the existing `ANONYMISE.selectionTargetPlaceholder` on the input
rather than adding prose to the panel.

### Files

- `frontend/views/anonymise.js` (`selectionStageFields`, `wireSelectionPanel`,
  `applySelection`, the `selectionViewState` bundle)
- `frontend/style.css` only if `.selection-card` needs the list to scroll inside
  its fixed width. Do not let the panel grow past its clamp
  (`SELECTION_PANEL_WIDTH`, ~97), and do not introduce a second scroller.

### Tests

- ADD, `frontend/anonymise.test.js`: with two Values in the store, render the
  panel in `spelling` mode with `target: "mar"` and assert the picks are
  rendered, in `valueAutocomplete` order, prefix matches first.
- ADD, wiring test with `testdom.js`: fire `input` on the field and assert the
  suggestion list's contents changed WITHOUT a store notification (subscribe and
  assert the subscriber was not called), then click a pick and assert Apply adds
  the spelling to the right Value.
- ADD, wiring test: two consecutive `input` events both land, which is the
  one-letter symptom expressed as an assertion.
- UPDATE: any existing assertion that expects `#selection-target` to carry a
  `list` attribute or expects a `#selection-targets` datalist. Delete those,
  do not weaken them.

---

## CR3 — A Value that cannot apply, or that matches nothing, must say so

### Symptom

A declared Value silently does nothing, and the success notice says otherwise.

### Root cause

Finding 3.

### The change

Two halves, one in the engine and one in the view. Both are about telling the
truth after a run; neither changes what gets replaced.

1. **Engine, unknown type.** In `engine.Run`, when a declared Value's category
   is not a member of `AllValueCategories`, append a run warning to
   `res.Report.Warnings` naming the Value and the fact. Put it beside the
   existing preamble (`pipeline.go` ~299 to ~312), before `filterValues` drops
   it, and keep it a WARNING, never blocking: a run that refuses because one
   declaration is malformed punishes the user for a state the pipeline can
   resolve, which is the same reasoning `CLAUDE.md` §5 gives for intersections.
   `Report.Warnings` is the right home rather than `Validation.Warnings`,
   because the step 3 report card renders the former and nothing renders the
   latter (Finding 3, note).

   Proposed string (no em dash, actionable, states the fix):
   `the Value "X" has an unrecognised type and was not applied, remove it and declare it again from the type list`

   Distinguish this from a switched-OFF category, which stays silent by design:
   the user turned that switch off, and a warning per Value would fire on every
   run of a narrowed preset.

2. **View, matched nothing.** `runFastRerun` already refreshes the mapping and
   the value tables. Give it an optional expectation: the text just declared.
   After `refreshValueTables`, if no `state.replacedValues` row's `original`
   matches that text case-insensitively, replace the success notice with a
   warning notice saying the Value was added but no occurrence was found.

   Proposed string:
   `"X" was added as a Value, but no occurrence of it was found in the imported documents.`

   Apply it to both entry points that declare a Value (the selection panel's
   new-Value mode and "Add missed Value"). It is deliberately NOT applied to the
   spelling mode: a spelling has no row of its own, so proving it matched
   nothing means comparing the target placeholder's count across the re-run,
   which is a second mechanism for a smaller payoff. Leave the spelling mode's
   notice as it is and record the reason in the CR, not in the code.

### Files

- `backend/engine/pipeline.go` (the preamble warning)
- `frontend/views/anonymise.js` (`runFastRerun` and its two declaring callers)
- `frontend/copy.js` (one new string; the engine string lives in Go)

### Tests

- ADD, `backend/engine/pipeline_test.go` (or the nearest existing run test): a
  run with one Value carrying `Category: "person_names,Person names"` produces a
  report warning naming it, replaces nothing, and does NOT block. Assert the
  switched-off case produces no such warning, so the two stay distinguishable.
- ADD, `frontend/anonymise.test.js`: a fast re-run whose refreshed tables do not
  contain the declared text produces the warn notice, and one that does produces
  the ok notice.
- The new Go string is user-visible: `copy_guard_test.go` already walks
  `backend/`, so no new guard is needed, only a string with no em dash.

---

## CR4 — "Add missed Value" gets the two safeguards the step 2 add row has

### Symptom

Declaring "Coca-Cola company" on step 3 while "Coca-Cola" is already a Value
creates a rival Value, and the text reads `[BRAND_1] company`. The same gesture
on step 2 folds it into the family. The card also gives no indication whether the
text occurs in the documents at all.

### Root cause

Finding 6. `wireMissed` calls `addValues` directly; the step 2 add row calls
`foldIntoFamily` first and shows a `countTermMatches` read-out.

### The change

1. In `wireMissed`'s `add`, call `foldIntoFamily(drafts.missedCategory, text)`
   first, exactly as the step 2 add row does (`identifyworkspace.js` ~1534) and
   as the step 3 selection panel already does (`anonymise.js` ~1494). On a fold,
   notify with `WORKSPACE.foldedIntoValue(text, family.main)`, which already
   exists, and skip `addValues`. Saying it out loud is required: a silent fold is
   indistinguishable from the button not working.
2. Add the match read-out under the input, from `countTermMatches`, debounced,
   with the same stale-answer guard the step 2 row uses (~1509 to ~1521): if the
   field moved on while the count was in flight, drop the answer. Reuse
   `WORKSPACE.valueMatches(count, documents)`; do not add a second string for
   the same sentence.
3. Keep the two buttons separate. "Add Value" and "Re-run fast passes" are two
   decisions and several Values are usually added at once; that is already
   documented in the code and stays.

### Files

- `frontend/views/anonymise.js` (`missedCard`, `wireMissed`)

### Tests

- ADD, `frontend/anonymise.test.js` (wiring): with "Coca-Cola" in the store,
  declaring "Coca-Cola company" from the card produces ONE Value with a spelling
  and an info notice, not two Values.
- ADD: the read-out renders after the debounce and stays empty when the bridge
  rejects (the render harness and the unit suite both run without a bridge, so
  the no-bridge path must be silent rather than an error).

---

## CR5 — The backward-move question must name the right survivors

### Symptom

The reset question implies that stepping back from Anonymise to Identify loses
the Values, when it keeps them, including the ones just declared on step 3.

### Root cause

Finding 4.

### The change

Remove the second sentence from `NAV.backConfirmBody` (`copy.js` ~90). The first
sentence is already true and sufficient: it names the step being cleared and
says the step starts fresh.

Resulting string:

```
Going back clears everything the <name> step owns, so you start it fresh.
```

Rejected alternative, recorded here so it is not re-proposed: a per-step body
enumerating survivors. It would be correct but it is four sentences of copy to
maintain against `STEP_RESETS`, and the two can drift; the shorter claim cannot
be wrong. If the owner later wants the survivors named, the honest source is
`STEP_RESETS` itself, and the body should be GENERATED from it rather than
written.

### Files

- `frontend/copy.js`

### Tests

- UPDATE, `frontend/copy.test.js` ~101: the test currently asserts
  `/imported documents/i` and `/never anonymise/i`, which is the sentence being
  removed. Rewrite it to assert what the copy now claims: the title and body
  name the step, and the body does NOT enumerate survivors. Do not weaken it to
  a length check.

---

## CR6 — "Save profile" must be gated on the registry, and named once

### Symptom

Two problems behind one control:

- A profile saved on Identify right after a detection, or after stepping back
  from Anonymise, writes `registry: []`. The gate that claims to prevent this
  cannot.
- The Export step has a second control that fires the same bound method under
  the label "Save profile", so the same file has two homes while the owner wants
  one.

### Root cause

Finding 5.

### The change

1. **Gate on the fact, not the proxy.** Replace `s.detectionRan` as the Save
   gate with the number of placeholder rows Go actually holds. The frontend
   already mirrors them: `state.replacedValues` is filled by
   `refreshValueTables` from `App.ValuePlaceholders`. Two acceptable shapes,
   in preference order:
   - a) The Identify rail refreshes the mirror once when it renders (the same
     `valuePlaceholders()` call step 3 makes) and gates Save on
     `state.replacedValues.length > 0`. No new bound method, no new state.
   - b) A tiny bound `RegistrySize() int` on the App. Cheaper to read, but it is
     a new method on the bridge for a number the bridge already carries, so
     prefer (a).
2. **Keep `detectionRan` for what it does describe** (a detection has completed
   this session) if anything else reads it; if nothing does after this change,
   delete it and its resets, and delete the tests that assert the latch. Do not
   leave a latch nothing reads.
3. **Update the disabled tooltip** so it names the real precondition. Proposed:
   `Run the anonymisation once before saving a profile, a profile carries the placeholder registry.`
   Retire `RAIL.profileSaveDisabled`'s current text ("Run detection once before
   saving a profile."), which is the wrong instruction.
4. **Remove the Export step's session-save control** (`export.js` ~413 to ~415,
   its markup, and the `EXPORT.session*` strings that become unused), so the
   profile has exactly one home, on Identify, as the owner requires. Keep the
   mapping CSV and mapping JSON exports on step 4: those are the
   re-identification key exports and they are a different artefact from a
   profile.
5. Drop the dead argument in the remaining call site (`buildRunRequest(false)`).

Note for the implementer: this CR does NOT change `ResetRun`. Discarding the
registry on a backward move is correct and load-bearing (see section 0). What
changes is that Save then correctly reports itself unavailable until the next
run, instead of silently writing an empty key.

### Files

- `frontend/views/identifyrail.js` (`profileSection`, `wireProfile`)
- `frontend/views/export.js` (remove the control and its wiring)
- `frontend/copy.js` (`RAIL.profileSaveDisabled`, remove unused `EXPORT.session*`)
- `frontend/state.js` only if `detectionRan` is retired

### Tests

- UPDATE, `frontend/identifyrail.test.js` ~452: it seeds `detectionRan: true` to
  open the gate. Re-seed it with a non-empty `replacedValues` instead, and add
  the negative case: an empty registry keeps Save disabled and the tooltip names
  the reason.
- UPDATE, `frontend/state.test.js` ~1486 to ~1497: the `detectionRan` latch
  tests. If the latch is retired, delete them; if it survives for another
  reader, keep them and add the new gate's tests beside.
- UPDATE, `frontend/export.test.js`: delete the assertions for the removed
  control. Assert the mapping exports are untouched.

---

## CR7 — The charters, BRIDGE.md and the bundled docs must state the real rule

### Symptom

Four documents say Anonymise "creates no Value", which reads as a prohibition on
the step 3 declaration surface the application ships. An agent following the
charter literally would delete the feature, and this change order exists partly
because that has already happened once.

### Root cause

The sentence compresses two different claims into one, and the half it drops is
the half that permits the feature.

### The change

Restate the rule in all four places, in the present tense, with the reason
attached. The wording to use, adapted per file:

> **Anonymise runs no discovery method and reaches no model.** `engine.Run` has
> no LLM slot: every discovery method runs at Identify time and its findings are
> Suggestions the user accepts. The only Values a run can apply are the ones the
> user accepted on Identify or DECLARED while reviewing the result on Anonymise,
> from the Compare pane selection or the "Add missed Value" card. A declaration
> is the user acting, so it passes the review gate rather than walking past it;
> what the gate forbids is an unreviewed MACHINE finding reaching the text.
> `TestAnonymiseNeverCallsOllama` asserts the model call count is zero.

Apply to:

- `CLAUDE.md` §1 (line ~16, the overview sentence "anonymisation itself reaches
  no model and creates no Value") and §5 (line ~362, the bullet "Anonymise
  creates no Value"). §5's bullet should also name the three step 3 surfaces and
  state that a Value declared there is a first-class Value: it reaches the
  registry, the report, the Replaced values table, the mapping and the session
  file. That is the contract CR1 to CR3 restore.
- `backend/CLAUDE.md` (line ~47, "Anonymise reaches no model, and creates no
  Value").
- `frontend/BRIDGE.md` (line ~257, the same claim under the run contract).
- `frontend/docs/index.html` (the step 3 entry, ~132 to ~141): "This step never
  contacts the local AI and never invents a Value" becomes "This step never
  contacts the local AI and runs no detection of its own. Nothing is replaced
  that you did not accept or declare yourself." The surrounding paragraph
  already describes the selection and "Add missed Value" correctly and needs no
  change. Leave the Local AI section's "It never runs during the Anonymise step"
  (~225) as it is: that one is true and unambiguous.

Also correct the `selectedMark` comment in `anonymise.js` (~73) to the shape the
code stores, `{placeholder, original}` (Finding 6). A comment promising a field
nobody writes is the kind of drift `CLAUDE.md` §6 bans.

### Files

- `CLAUDE.md`, `backend/CLAUDE.md`, `frontend/BRIDGE.md`,
  `frontend/docs/index.html`, `frontend/views/anonymise.js` (one comment)

### Tests

- No new test. The claim is prose. The behaviour it describes is asserted by
  `TestAnonymiseNeverCallsOllama` (unchanged) and by CR1 to CR3's new tests,
  which is the right division: the charter explains, the tests hold.
- `copy_guard_test.go` and `frontend/copy.test.js` still gate the em dash rule
  over the docs string that changes in `index.html`.

---

## Decisions taken

1. **The step 3 declaration surfaces stay, and the charter changes.** The
   feature is correct and the sentence forbidding it is not. The review gate is
   about unreviewed machine findings, not about the user.
2. **A malformed declaration warns, it never blocks.** Same reasoning as
   intersections (`CLAUDE.md` §5): refusing a run for a state the pipeline can
   resolve punishes the user. It lands in `Report.Warnings`, because that is
   what the report card renders.
3. **A switched-off category stays silent.** The user turned that switch off; a
   per-Value warning would fire on every narrowed run.
4. **One dropdown builder, one suggestion list.** `categorySelect` is shared
   rather than re-implemented, and the spelling target reuses the pick-list the
   Selected placeholder card already uses. A second builder for the same control
   is the next inconsistency (`frontend/CLAUDE.md`).
5. **The datalist goes.** A native popup rebuilt mid-keystroke inside a floating
   panel cannot be made reliable, and the panel is already positioned against
   the Compare card for exactly this class of reason.
6. **Keystrokes do not repaint the panes.** The suggestion list is patched in
   place. Debounce-and-refocus is the fallback, not the target.
7. **`ResetRun` is not touched.** Discarding the registry on a backward move is
   the fix for a real leak and stays; only the copy and the Save gate change.
8. **The profile has one home, Identify.** The Export step keeps the mapping
   exports, which are a different artefact.
9. **No session-file change.** Nothing here alters what is persisted, so
   `SessionVersion` stays 8 and the loader keeps refusing every other version.
10. **The spelling mode gets no "matched nothing" notice.** The mechanism would
    be a per-placeholder count diff across the re-run, which is a second
    mechanism for a smaller payoff. Recorded, not implemented.

---

## Conflict analysis

- **CR1 and CR4 both touch `missedCard` / `wireMissed`.** CR1 changes the
  dropdown's builder, CR4 changes the add handler. Do CR1 first; CR4 then edits
  a function whose markup is already final.
- **CR2 and CR3 both touch `applySelection` and `runFastRerun`.** CR2 changes
  how the target is resolved, CR3 changes what is reported afterwards. Do CR2
  first: CR3's notice logic sits at the end of a `runFastRerun` whose signature
  CR2 leaves alone.
- **CR3's engine half and CR1 interact by design.** Once CR1 lands, the frontend
  can no longer produce an unknown category, so CR3's warning becomes a
  belt-and-braces guard against a session file or a future producer. That is the
  point: the guard belongs in the engine because the engine is what silently
  dropped the Value.
- **CR6 may retire `detectionRan`.** Check every reader first
  (`state.js`, `identifyrail.js`, `main.js` ~139 comment, both test files). If
  any reader remains, keep the field and add the new gate beside it; do not
  leave a latch nothing reads.
- **CR5 and CR7 both touch user-visible copy** in different files; no overlap.
- **No CR changes the engine's replacement behaviour**, the passes, the match
  classes, the signal derivations or the session shape. The parity guards should
  not move at all. If one of them fails, the change went further than this order
  asks.

---

## Recommended execution sequence

1. **CR1** (the broken dropdown). It is the defect that loses data, and it is
   small. Land it with its parity-style test first so the bug class cannot come
   back while the rest is in flight.
2. **CR2** (the unusable spelling field). Restores the second half of the
   feature. Largest single diff in this order.
3. **CR3** (make the silence loud), engine half then view half.
4. **CR4** (the add card's two safeguards).
5. **CR5** (the reset question) and **CR7** (the charters and docs). Copy and
   prose, cheap, no interaction with the above.
6. **CR6** (the Save gate and the duplicate control). Last, because it is the
   only one that removes a control and the only one that may retire a state
   field; it wants a clean tree behind it.

After each CR: `go test ./...` and `node --test "frontend/**/*.test.js"`. Run
`task audit` before the final push, and the Linux Chromium harness at least once
after CR2, since CR2 changes what the Compare card renders.

---

## Acceptance criteria

Functional, on the built application:

1. Selecting text on step 3, "Replace", "Add it as a new Value": the type list
   shows the eight declarable types with their labels, the current type is
   pre-selected, and after Apply the declared Value appears in the anonymised
   pane, in the Report card, in the Replaced values table and in the mapping
   export, under the type that was chosen.
2. Selecting text, "Replace", "Make it a spelling of an existing Value": the
   search field accepts a whole word, the suggestions narrow as the letters are
   typed, clicking one selects it, and Apply makes the selected text share that
   Value's placeholder.
3. "Add missed Value" folds a longer form into an existing family instead of
   creating a rival, says so when it does, and shows how often the typed text
   occurs in the imported documents.
4. A declared Value that matches no text in the batch produces a warning notice
   naming it, instead of a success notice.
5. A declaration the engine cannot apply produces a run warning in the Report
   card naming the Value, and the run still completes.
6. Stepping back from Anonymise to Identify shows a reset question that claims
   only what is true, and the Values declared on step 3 are on the Identify
   workspace afterwards.
7. "Save profile" exists on Identify and nowhere else. It is disabled, with a
   tooltip naming the reason, until a run has produced a registry, including
   after a backward move that discarded one.
8. A saved profile contains the Values declared on step 3 and a non-empty
   registry.

Mechanical:

9. `go test ./...` green, `node --test "frontend/**/*.test.js"` green, both from
   a clean tree.
10. `task audit` shows no new findings.
11. The Linux Chromium harness passes.
12. No parity guard changed: `category_parity_test.go`,
    `detection_parity_test.go`, `dataset_parity_test.go`,
    `icon_parity_test.go`, `value_shape_test.go` all pass untouched.
13. `TestAnonymiseNeverCallsOllama` still passes, unmodified. The rule it
    asserts is the half of the charter sentence that was always true.
14. No file under `frontend/` or `backend/` claims that Anonymise creates no
    Value, and no comment names a deleted function or a change request.

---

# Addendum A — CR8: warning parity between declaring a Value on step 3 and accepting one on step 2

This addendum extends the order above. It came from a second owner question:
does a Value declared on step 3 get the same error and warning handling that a
Suggestion accepted, or a Value reviewed, gets on step 2? It does not. CR1 to
CR7 restore the DATA path (the Value applies, and reaches the registry, the
report, the tables and the JSON). They leave the FEEDBACK path unequal, and one
of the gaps turns a typo into a lost registry.

CR8 is therefore part of this order, and the sections at the end of the document
(decisions, conflict analysis, sequence, acceptance criteria) are extended here
rather than rewritten.

## What step 2 gives a Value, and step 3 does not

Established by reading the code, not assumed. Step 2's workspace paints two
things on the card that owns a value, through the one `warningPopover`
affordance (`identifyworkspace.js` `cardStatus` ~695):

| Feedback on step 2 | Source | Reaches step 3? |
|---|---|---|
| Blocking conflicts on the card that causes them: the same name under two types, a spelling two values both claim, a value that is also allowlisted, each with a "Solve conflicts" action | `state.js valueConflicts` (~2339), a PURE function over state that mirrors the engine's three blocking checks | **No.** Only `identifyworkspace.js` imports it (~53). `anonymise.js` imports neither it nor `intersectionsFor` |
| The intersection warning, naming the winning method, with "Never anonymise the covering term" and "Group with" actions | `App.CheckIntersections` via `intersectionsFor` (~612, called at ~1989) | **No.** `checkIntersections` is called from the workspace only |
| A live match count under the add field, "Found N times in M documents" | `App.CountTermMatches` (~1509) | **No** on the "Add missed Value" card (CR4 fixes that half), and none on the selection panel |

Step 3 has real per-surface error handling, and it is the right pattern: a
refused rename lands on its own row (`rowErrors`, ~1053), a refused rename in the
Selected placeholder card lands on that card (`selectedError`, ~929), and a
refused Apply lands on the selection panel (`selectionError`, ~1468). All three
put the refusal where the fix is. What is missing is everything that would warn
BEFORE the run, plus one thing the run computes and nobody shows.

## Finding 7: the refused run is a dead end on step 3, and it costs the registry

When a declaration conflicts, `engine.Run` refuses before pass 1 and
`renderAnonymise` hides three cards at once (`anonymise.js` ~132 to ~134): the
Replaced values card, the Report card AND the "Add missed Value" card, all gated
on `s.results && !blocked`.

That leaves the user with no way to undo the declaration that caused the
refusal:

- The Replaced values table reads the REGISTRY, and a refused run assigned
  nothing, so the offending Value has no row there. It has no row anywhere on
  step 3: the step has no list of declared Values and no delete control for one.
- The card that would let them declare (and therefore re-declare) is hidden too.
- `ANONYMISE.blockedIntro` (`copy.js` ~941) says "Fix each conflict below on the
  Identify step, then run again", which is the copy admitting the fix lives on
  another screen.
- Going to that screen calls `resetRun()` (`nav.js` ~84), which nils the
  registry. So a mistyped declaration on step 3 costs the user every placeholder
  number the session had assigned.

`frontend/CLAUDE.md` requires the step 2 to 3 gate never to be a dead end: its
hint "is the refusal itself, naming the bulk Reject all shown". The step 3
refusal has no such exit.

## Finding 8: the overlap warnings the run computes are shown nowhere

`finishReport` appends `overlaps.conflicts()` to `res.Validation.Warnings`
(`pipeline.go` ~502 to ~507). Step 3 renders `results.report.warnings`
(`state.js visibleWarnings` ~2503) and `results.validation.blocking`
(`blockingConflicts` ~2524), and nothing renders `results.validation.warnings`.

So "the Value you declared lost this text to a built-in pattern" is computed on
every run, by the one place that decides it, and then dropped. This is the
warning that explains a declared Value replacing less than the user expected,
which is the most likely outcome of declaring something on step 3 that a pattern
already covers.

The main document mentions this field only as a reason to route CR3's new
warning to `Report.Warnings`. CR8 also surfaces the warnings already being
computed.

## CR8 — The change

Four parts. None of them changes what gets replaced.

### 8a. Warn on the declaration, before the run

`valueConflicts(s)` is pure and already exported. Call it from `anonymise.js` at
the two points where a Value is declared, and refuse the declaration with the
reason ON the surface the user is looking at, exactly as the existing per-row
errors do:

- The selection panel's new-Value mode: after resolving the category (CR1) and
  the family fold, run the check against the state the declaration WOULD produce.
  On a conflict, set `selectionError` to the conflict sentence and do not
  re-run. The panel already renders `selectionError` and the user is one click
  from a different category or Cancel.
- The "Add missed Value" card: on a conflict, keep the draft text in the field
  and show the sentence under the row, next to the input the fix goes into. Do
  NOT use a toast: a toast is gone before the user has re-read the field.

Reuse `conflictMessage`'s wording. It lives in `identifyworkspace.js` (~552) and
builds every sentence from `copy.js WORKSPACE.conflict*`; export it, or move it
to `copy.js` beside the strings it assembles, so both screens say the same thing
about the same conflict. Do not write a second set of sentences.

This is the load-bearing half of CR8: it converts a refused run into a refused
declaration, which is recoverable on the screen the user is on.

### 8b. Give the refused run an exit on step 3

Even with 8a, a refusal can still arrive: the allowlist can gain a term, a
category can be switched off and on, and a session can be loaded. So the blocked
state needs a way out that does not cost the registry:

- Keep the "Add missed Value" card VISIBLE when the run was refused, so the
  screen that caused the problem can also address it. Its summary already reads
  the declared-Value count, so the card is the natural home for the exit.
- Give the blocked panel, per conflict, the action that resolves it where the
  data allows: for an allowlist conflict, "Remove the term from Never
  anonymise"; for an ambiguity or a collision, a control that DELETES the
  declared Value named in the conflict. Both already exist as state actions
  (`removeAllowTerm`, `deleteValue`).
- Change `ANONYMISE.blockedIntro` so it stops sending the user to another
  screen for a fix that is now available here. Proposed:
  `Nothing was replaced. Two values would fight over the same text, which would make the re-identification key ambiguous. Fix each conflict below, then run again.`

If the owner prefers the smaller version of 8b, implement only the first bullet
(keep the card visible) plus the copy change, and leave the per-conflict actions
out. That alone removes the dead end, because the user can re-declare correctly.
Record which version was built.

### 8c. Surface the warnings the run already computes

Render `results.validation.warnings` on step 3, in the Report card beside
`visibleWarnings`, with the same dismiss affordance. Two acceptable shapes:

- a) The view reads both fields and renders one list. Cheapest, and keeps the
  engine's two fields meaning what they mean: blocking aborts, warnings inform.
- b) The engine copies overlap warnings into `Report.Warnings` too. Rejected:
  one warning in two fields is a shape where a later reader shows it twice.

Use (a). `dismissWarning` keys on the warning TEXT (`state.js` ~2490), so it
already works for warnings from a second source with no change.

### 8d. Run the intersection check for a Value declared on step 3

A Value declared on step 3 that another route entirely covers is the FULL-case
intersection, which CHANGE-06 kept because it usually means a mis-declaration.
After a fast re-run that declared a Value, call `checkIntersections` with
`buildIntersectionRequest()` and, if the newly declared Value is among the
results, show the warning. Two decisions to respect:

- Put it on the surface the declaration happened on (the selection panel is
  gone by then, so the Report card's warning list from 8c is the right home),
  NOT on a value card that does not exist on this screen.
- Say it once. The same Value re-declared must not stack warnings; the text-keyed
  dismissal handles repeats.

This is the lowest-value part of CR8 and the one to drop first if the change gets
long, because 8c already explains the same outcome after the fact for the
partial case. Drop it explicitly, in the commit message, rather than silently.

### Files

- `frontend/views/anonymise.js` (the two declaration paths, the blocked panel,
  the Report card's warning list, `runFastRerun`)
- `frontend/views/identifyworkspace.js` (export `conflictMessage`, or move it)
- `frontend/copy.js` (`blockedIntro`, and any new per-conflict action label)
- `frontend/state.js` only if the blocked-panel actions need a reducer that does
  not exist yet

### Tests

- ADD, `frontend/anonymise.test.js`: declaring a Value whose name is already
  allowlisted is REFUSED on the panel with the allowlist sentence, and no
  `fastRerun` is attempted. Same for an ambiguity and for a spelling collision.
- ADD: the same three from the "Add missed Value" card, asserting the draft text
  survives the refusal.
- ADD: with `results.validation.warnings` non-empty, the Report card renders
  them, and dismissing one hides it.
- ADD: when `blockingConflicts` is non-empty, the "Add missed Value" card is
  still rendered (the assertion that pins the exit), while the Replaced values
  and Report cards stay hidden.
- ADD, `backend/engine/pipeline_test.go`: a run in which a declared Value is
  fully covered by a built-in pattern produces an overlap warning in
  `Validation.Warnings`. If that is already asserted, extend it rather than
  duplicating it.
- UPDATE, `frontend/copy.test.js`: `blockedIntro` no longer names the Identify
  step.

## Extensions to the closing sections

### Decisions (added to the list above)

11. **A conflicting declaration is refused at the declaration, not at the run.**
    The engine's refusal stays exactly as it is, because it is the last line of
    defence and it protects the registry. What changes is that the user meets
    the conflict on the surface they are typing into, which is what step 2
    already does.
12. **Step 3 keeps its own exit from a refused run.** A screen that can create a
    blocking conflict must be able to clear it. Sending the user to Identify
    costs the registry, which makes the wizard punish a typo.
13. **One set of conflict sentences.** `conflictMessage` is shared, not
    reimplemented. Two screens describing one conflict in two ways is the
    inconsistency the copy rules exist to prevent.
14. **`Validation.Warnings` is rendered, not copied.** The engine's two fields
    keep their distinct meanings; the view reads both.

### Conflict analysis (added)

- **CR8a depends on CR1.** Until the category is real, the conflict check would
  run against a phantom category, and `valueConflicts` skips values whose
  category is not active (`categoryActive`, `state.js` ~2314), so it would
  report nothing. Do CR1 first or 8a is untestable.
- **CR8a overlaps CR4.** Both edit `wireMissed`'s add handler; CR4 adds the
  family fold, 8a adds the conflict refusal. The fold runs FIRST: a value folded
  into an existing family is not a new declaration and cannot conflict with
  itself.
- **CR8c overlaps CR3.** CR3 adds a warning to `Report.Warnings`, 8c adds a
  second source to the same rendered list. Do CR3 first, then 8c widens the
  reader.
- **CR8b touches the same render gate as CR1 to CR4** (`s.results && !blocked`).
  Land it after them so the card it keeps visible is already correct.

### Recommended sequence (revised)

CR1, CR2, CR3, CR4, then **CR8a, CR8c, CR8b, CR8d**, then CR5, CR7, CR6. CR8
sits with the functional work; the copy and charter CRs stay last.

### Acceptance criteria (added)

15. Declaring a Value on step 3 whose name is already a never-anonymise term, or
    already declared under another type, or whose spelling another Value claims,
    is refused AT THE DECLARATION with the same sentence step 2 would show, and
    no run is attempted.
16. A run refused by a blocking conflict still offers a way to clear it on
    step 3, and the user never has to go back to Identify (and lose the
    registry) to fix a declaration they made on step 3.
17. The overlap warnings a run computes are visible on step 3 and dismissible.
18. No conflict sentence exists in two places in the codebase.

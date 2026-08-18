# CHANGE-06 — Intersection warning cleanup, the compact value card, the spellings popup, and scroll stability on step 2

You are executing a change order against the existing **doc-anonymiser**
repository (pattern P0, pure Go + Wails v2, no CGo, no npm). It holds **one
self-contained implementation section per change request (CR1 to CR5)**,
followed by the **decisions taken**, a **conflict analysis**, the **recommended
execution sequence** and the **acceptance criteria**.

Every CR below comes from an owner review of the BUILT application on step 2,
Identify, "My values" tab. They fall into two groups: a correctness/clarity fix
to the intersection warning (CR1), and a redesign of the value card that makes
the card a fixed-height surface so managing a value no longer throws the scroll
position around (CR2 to CR4), plus a small search affordance shared across the
screen (CR5).

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
  `value_shape_test.go`). Nothing here may render a camel-case `data-` attribute
  or call `icon()` with a name absent from `ICONS`.
- Comments explain intent in the present tense. Do not write "used to be" or
  "CR2 changed this" into the code.
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
| Branch to develop and push on | `claude/coca-pattern-priority-text-m9g6ga` |
| Its current head | this document, and nothing else. No code has changed yet. |
| Suites | `go test ./...` and `node --test "frontend/**/*.test.js"`, both must be green |
| Audit | `task audit` (go-task, no make) |
| Real-rendering harness | `docs/UITESTING.md`; the Linux Chromium harness is a BLOCKING CI step |

### The owner's report, in substance

On step 2 Identify, "My values" tab:

1. The intersection warning is confusing and, in the partial-overlap case,
   pointless. Two examples the owner saw:
   - `7 of 38 occurrences of "Coca" are also matched by a built-in pattern as
     "pierre.dupont@coca.us", which takes priority there.`
   - `2 of 3 occurrences of "Pierre Dupont" are also matched by a built-in
     pattern as "pierre.dupont@coca.us", which takes priority there.`
2. The scrollbar of the "My values" list jumps upward after acting on a card
   (editing a spelling, editing the main text, deleting a card). It is
   disorienting when managing values.
3. The value card should be compact: warnings should collapse to a single icon
   (text on hover), and the spellings should be a short chip row with the
   overflow and full management moved into a popup.
4. A search box needs a way to clear its text.

### What was established by diagnosis (do not re-derive)

Each root cause below was read out of the code, not guessed.

- **The intersection message quotes the wrong string, and the partial case is
  noise.** `engine.DetectIntersections`
  (`backend/engine/intersections.go`) fills `Intersection.Value` with the losing
  span's `MainTextOrOriginal()` (`backend/engine/pii.go` ~61), which for a
  value-pass match is the Value's canonical `MainText`, NOT the literal text the
  winner actually covered. The literal covered text lives on the span as
  `Span.Original` and is discarded. Two consequences:
  - Casing: the entity "Coca" is covered inside the lowercase email domain
    "coca", but the message quotes "Coca".
  - Worse, for "Pierre Dupont" the covered occurrences are the DERIVED spellings
    "pierre" and "dupont" matching separately inside `pierre.dupont@coca.us`
    (the person expander emits bare first-name and surname forms,
    `expandPersonInto` in `backend/engine/values.go` ~199, and the value pass'
    boundary check treats `.` and `@` as boundaries, `isWordBoundary` ~328). The
    full name never appears in the address at all, yet the message reads as
    though it does.
  - Both examples are the PARTIAL case (`Occurrences < TotalOccurrences`). In the
    partial case the covered occurrences are redacted by the winner anyway, so
    nothing leaks and there is no action for the user to take. The message is
    noise. The FULL case (`Occurrences == TotalOccurrences`, "Every occurrence
    ... is not replaced under its own type") is the one worth keeping, because it
    tells the user a Value they declared will get NO placeholder of its own
    anywhere, which usually means a mis-declaration (e.g. an email address
    declared as a person).
- **The scroll jump is a height-thrash, not a broken scroll-preserver.**
  `scroll.js` snapshots each scrolled element's `scrollTop` before a repaint and
  writes it back after. It is sound, but it restores a RAW pixel offset. The
  affected actions transiently shrink the scroller's content
  (`.card-body.stack` inside `#identify-workspace`, `style.css` ~270):
  - Editing/deleting a spelling and renaming main text all reset that row's
    `derivedSpellings` to `null` (pending) so `refreshVariants()` re-derives via
    the async `expandSpellings` bridge. While pending, `valueCard` renders the
    chip-less "working out the other spellings..." placeholder
    (`identifyworkspace.js` ~668, ~686), so the card collapses. The repaint
    restores the old `scrollTop`, the browser CLAMPS it to the now-smaller
    `scrollHeight`, and the NEXT repaint (chips back) snapshots the clamped
    value, so the position is lost upward.
  - Warnings render INLINE in the card (`conflictNote`, `intersectionNote`,
    `evidence`, `identifyworkspace.js` ~698 to ~721), so a warning appearing or
    clearing changes card height too.
  - Deleting a whole card shortens the list, which clamps the offset to the new
    smaller maximum.
  The redesign in CR2 removes all three height-change sources; CR4 adds a small
  anchor for the one that legitimately remains (deleting a card).
- **The card is the wrong shape for the job.** `valueCard`
  (`identifyworkspace.js` ~620) renders the name row, then inline conflict and
  intersection notes, then the full editable/draggable chip row with a per-chip
  delete and an inline add. The chip row grows with the number of spellings and
  the notes grow with the number of warnings, so the card's height is a function
  of its data. Making it fixed-height means: warnings become one hover icon, and
  the chip row becomes a bounded preview plus a popup that owns full management.

### Decisions the owner has already approved

1. **Intersection: drop the partial warning, keep the full one.** Both on-screen
   examples were partial, so both disappear. The kept full-coverage message must
   also be corrected so it names the literal covered text, not the Value's
   canonical form (CR1).
2. **Compact card.** Warnings collapse to a red/yellow icon; hovering it shows
   the warning text AND the resolution actions beside it. The spelling chips
   become read-only pills, shown up to a **65-character budget** (the sum of the
   visible chips' characters, NOT a fixed count), with the remainder behind a
   "+N more" control and an "+ add" control. Drag-to-regroup is kept, but only
   for the VISIBLE chips.
3. **Spellings popup.** "+N more" and "+ add" open a modal listing every
   spelling, with an Add row, a Search box, and per-row **Delete** and **Move
   to...** actions. Editing in the popup is reflected immediately in the compact
   card (live, no OK/Cancel). "Move to..." reuses the existing Group-with value
   picker to send a spelling to another Value, which is how spellings in the
   "+N more" overflow are regrouped without needing them on screen.
4. **Card-level actions unchanged.** Group with and Remove stay where they are on
   the card (they were only cropped out of the mockup).
5. **The ✕ clear control goes in EVERY search box** on the screen: My Values
   search, the Suggestions search, and the new spellings-popup search.
6. **No engine session-shape change.** `SessionVersion` stays 8.

### Recommended session setup

- Model **Opus** (or the latest available), reasoning effort **high**. CR2 to
  CR4 are the bulk: one view module (`identifyworkspace.js`), the shared UI kit
  (`ui.js`), `style.css`, `copy.js`, and their tests. CR1 additionally touches
  the engine (`intersections.go`) and its tests.
- Do NOT split the CRs across parallel sessions: CR2 to CR4 all edit
  `identifyworkspace.js` and `style.css`, and `scripts/uitest/probes.js` is a
  single file both harnesses read by contract (`uitest_parity_test.go`).
- Commit per CR, with the CR number in the message body but never in a code
  comment (`CLAUDE.md` §6).

### Paste-ready opening prompt for the fresh session

> Read `CLAUDE.md`, `frontend/CLAUDE.md`, `backend/CLAUDE.md`,
> `frontend/BRIDGE.md`, `docs/UITESTING.md`, `docs/TESTING.md` and then
> `docs/CHANGE-06.md` in full. You are on branch
> `claude/coca-pattern-priority-text-m9g6ga`, which currently holds only that
> plan. Implement CHANGE-06 in the order its "Recommended order" section gives,
> one commit per CR. For each CR: write or update the tests named in the CR in
> the SAME commit as the behaviour, run `go test ./...` and
> `node --test "frontend/**/*.test.js"`, and run the Linux rendering harness
> (`docs/UITESTING.md`) for the CRs that add a probe. Push to
> `claude/coca-pattern-priority-text-m9g6ga`. If a decision in the plan turns out
> to be wrong once you are in the code, stop and tell me rather than inventing a
> third option.

---

## CR1 — The intersection warning: drop partial, keep and correct full

### Current behaviour

`engine.DetectIntersections` (`backend/engine/intersections.go`) reports every
Value whose text another route also claims, partial and full alike. The view
`intersectionNoteHTML` (`identifyworkspace.js` ~767) chooses between
`WORKSPACE.intersectionAll` (full, "Every occurrence ...") and
`WORKSPACE.intersectionSome` (partial, "N of M occurrences ...", `copy.js`
~616). Both quote `overlap.value`, which is the losing span's canonical
`MainText`, so the sentence can name a string that never literally appears
inside the winner (the "Coca" casing case and the "Pierre Dupont" fragment
case).

### Change

Two parts: stop emitting the partial rows, and correct the text the full rows
quote.

#### 1a. Engine: only report full coverage, and carry the literal covered text

`backend/engine/intersections.go`:

- In `DetectIntersections`, after the tallies are built, **keep only rows where
  `Occurrences == TotalOccurrences`** (full coverage). The cleanest place is in
  `sortedIntersections`: skip any tally whose row is not fully covered, so the
  partial rows never reach the frontend. Keep `TotalOccurrences`/`Occurrences`
  on the struct (the full-coverage rule is expressed with them and the test
  asserts them).
- Add `MatchedText string` to the `Intersection` struct, JSON `matchedText`,
  documented as: the literal text of one covered occurrence exactly as it
  appears in the document (`Span.Original`), which for a value-pass match keeps
  the document's own casing and can be a derived spelling rather than the Value's
  `MainText`. Populate it in `addIntersection` from `loser.Original`, and leave
  it empty when `loser.Original == loser.MainTextOrOriginal()` so the common case
  adds nothing to read.
- Because only full-coverage rows survive, `MatchedText` is the literal text of
  the covered occurrences; when a Value is covered by more than one distinct
  literal (the "Pierre Dupont" fragments "pierre" and "dupont"), record the SET
  and let the copy join them (see 1c). Represent it as `MatchedTexts []string`
  (deduped, document order, capped at a small number, say 3, like
  `Documents`) rather than a single string, so the message can say
  `"pierre", "dupont"`. Keep it empty when the only covered literal equals the
  Value's `MainText`.

> Implementer note: `resolveOverlaps(region.spans, true)` already returns the
> dropped losers with their `Original` intact; `addIntersection` currently
> ignores it. This is a field-plumbing change, not a new detection path, so the
> "check agrees with the run" guarantee is untouched.

#### 1b. Frontend view: delete the partial branch

`frontend/views/identifyworkspace.js`:

- In `intersectionNoteHTML`, remove the `covered >= total` conditional and the
  `intersectionSome` call. The engine now only sends full-coverage rows, so the
  note is always the "Every occurrence ..." form. Pass the new `matchedTexts`
  through.
- The two card actions on the note (Never anonymise the covering term, Group
  with) and the priority-order/fix lines stay.

#### 1c. Copy

`frontend/copy.js` (WORKSPACE):

- DELETE `intersectionSome` (no caller remains).
- Rework `intersectionAll` to name the literal covered text when it differs from
  the Value:

```js
/**
 * intersectedText(value, matchedTexts) names what actually sat inside the
 * winner. Usually that IS the value's own text and the sentence says so once;
 * when the covered occurrences are spellings with different casing or shape (a
 * lowercase spelling inside an email, or the "pierre"/"dupont" fragments of a
 * person name), naming them instead avoids implying the exact quoted string was
 * found verbatim where it was not.
 */
intersectedText(value, matchedTexts) {
  const list = (matchedTexts ?? []).filter((t) => t && t !== value);
  if (list.length === 0) return `"${value}"`;
  const quoted = list.map((t) => `"${t}"`).join(", ");
  return `${quoted} (${list.length === 1 ? "a spelling" : "spellings"} of "${value}")`;
},
/** intersectionAll(value, winner, route, matchedTexts): the value is never
 *  replaced under its own type, because a higher-priority match covers every
 *  occurrence. */
intersectionAll(value, winner, route, matchedTexts) {
  return `Every occurrence of ${this.intersectedText(value, matchedTexts)} is also matched by ${route} as "${winner}", which takes priority. This value is not replaced under its own type.`;
},
```

### Tests

Engine (`backend/engine/intersections_test.go`):

- UPDATE `TestIntersectionPartialCoverage`: it currently asserts a partial row is
  reported. Invert it: a partially covered value must now report NOTHING (the
  partial case is dropped). Rename to
  `TestIntersectionPartialCoverageIsNotReported`.
- KEEP `TestIntersectionEmailCoversADeclaredValue` (full coverage) and add an
  assertion that `MatchedTexts` is empty when the covered text equals the Value.
- ADD `TestIntersectionMatchedTextDiffersFromValue`: an entity "Coca" fully
  covered inside email domains reports `MatchedTexts == ["coca"]`. Use a document
  where "Coca" appears ONLY inside addresses so the row is full-coverage.
- ADD `TestIntersectionReportsFragmentSpellings`: a person "Pierre Dupont" whose
  only occurrences are inside `pierre.dupont@...` reports the fragment set
  (`["pierre", "dupont"]` in document order) and is full-coverage.
- KEEP `TestCheckAgreesWithTheRun` unchanged: it asserts winner category, which
  is unaffected.

Frontend:

- `frontend/identifyvalues.test.js`: UPDATE the intersection render tests. Delete
  the "partly covered value gets the milder count sentence" test. Keep the
  full-coverage test and add: a fully-covered value whose `matchedTexts` differ
  from `value` renders `"coca" (a spelling of "Coca")` and, for two fragments,
  `"pierre", "dupont" (spellings of "Pierre Dupont")`; a fully-covered value with
  empty `matchedTexts` names the value once with no "a spelling of".
- `frontend/copy.test.js`: the em-dash guard already covers the new strings; no
  new assertion needed beyond it running over the changed copy.

---

## CR2 — The compact value card: warning icon, bounded chip row

This CR is the one that fixes the scroll jump (CR item 2) at its source, by
making the card's height independent of how many warnings and spellings it
holds.

### Current behaviour

`valueCard` (`identifyworkspace.js` ~620) stacks, in order: the name/type/method
row, `conflictNote`, `intersectionNote`, an `evidence` note, then the full
`spellingRow` (every chip, each with a delete, plus an inline add), then a
feedback note and any open panel. Height grows with warnings and with the number
of spellings.

### Change

#### 2a. Warnings collapse to one hover icon with actions

Replace the three stacked inline notes (`conflictNote`, `intersectionNote`, and
the visible part of `evidence`) with a single **status icon** placed on the
name row, beside the method chip(s):

- **Red icon** (`icon("error")` or the existing warning glyph, red token) when
  the card has a BLOCKING conflict (`valueConflicts` returns an entry). Red is
  today's conflict colour.
- **Yellow/amber icon** (`icon("warning")`, warning token) when the card has a
  full-coverage intersection but no blocking conflict. Yellow is today's quieter
  warning colour.
- **No icon** when the card is clean.

Hovering (and keyboard-focusing) the icon opens a small **popover** that
contains, in this order:
1. the warning text (the conflict messages, or the intersection sentence from
   CR1), and
2. the resolution actions that used to live in the inline notes: for a conflict,
   the "Solve conflicts" affordance; for an intersection, "Never anonymise the
   covering term" and "Group with".

Reuse the existing hover/focus tooltip machinery (`helpTooltip` /
`wireHelpTooltips`, `ui.js`) as the pattern for open-on-hover-and-focus and
viewport-clamped positioning, but this popover carries interactive buttons, so
it must stay open while the pointer is inside it (do not close on mouseleave of
the icon alone; close on leaving the popover, on Escape, and on outside click).
Consider a dedicated `ui.js warningPopover(...)` builder next to `helpTooltip`
rather than overloading `helpTooltip`, because a tooltip that holds buttons is a
different contract from one that holds text; a second builder here is justified
(unlike the "two builders for one control" the charter warns against, because
these are two different controls).

The card no longer renders `conflictNote` / `intersectionNote` / the inline
`evidence` block as flowing rows. Keep the card-level `conflicted` / `intersects`
CSS classes if they only tint a border (fixed geometry); drop any margin that
reserved vertical space for the notes.

> Evidence: the "Why this was suggested" evidence and the "Shares evidence with
> ..." related-values note are informational, not warnings. Decide per the
> mockup: fold them behind an info affordance on the card (an `icon("info")`
> with `helpTooltip`) so they too stop contributing to height. This keeps the
> card fixed-height. `relatedNote` and `evidenceNote` become the content of that
> tooltip rather than inline blocks.

#### 2b. The bounded chip row

The spelling row becomes a fixed-height preview:

- Show spellings as **read-only pills** (no per-chip delete button, no inline
  add) up to a **65-character budget**: iterate the ordered spelling list
  (`derivedSpellings` then `spellings`, longest-first as today), summing
  `chip.length`; include chips while the running total is `<= 65`; the rest go to
  overflow. Always show at least one chip even if it alone exceeds 65, so a
  single long spelling still renders.
- After the visible chips, render a **"+N more"** control where `N` is the count
  of hidden spellings (omit it when nothing overflows), then an **"+ add"**
  control. Both open the spellings popup (CR3); "+ add" opens it focused on the
  Add input.
- The visible pills remain **drag sources** for regrouping (`wireVariantDrag`),
  exactly as today, so dragging a visible spelling onto another card still calls
  `moveSpelling`. Hidden spellings are moved from inside the popup (CR3).
- Put the whole chip row in a single-line, `overflow: hidden` container of fixed
  height, so the row can never wrap to a second line and change the card's
  height. The 65-char budget is chosen to fit one line at the card's width; the
  budget is a named constant with a comment.

Remove the "Show spellings / Hide spellings" toolbar toggle
(`#btn-toggle-derivedSpellings`, `valuesFilterBar`) and the `showSpellings`
branch: spellings are now always shown compactly, and their full list lives in
the popup, so a show/hide toggle that changes card height is both redundant and
a reintroduction of the height-thrash. (If the owner wants to keep a global
toggle, it must hide/show WITHOUT changing card height; default decision is to
remove it.)

#### 2c. What stays

The name (click-to-edit), the type `<select>`, the method chip(s), and the
card-level actions (Group with, Remove) stay exactly where they are (decision 4).
`revealNameInput` and the type-change handler are unchanged.

### Tests

- `frontend/identifyvalues.test.js`:
  - A clean card renders NO status icon; a conflicting card renders the red icon;
    an intersecting (full-coverage) card renders the yellow icon and no red one.
  - The chip row shows only chips within the 65-char budget, renders "+N more"
    with the correct N, and renders "+ add"; a value whose spellings all fit
    shows no "+N more".
  - The visible chips carry `draggable` and `data-spelling`; there is no per-chip
    delete button in the compact card.
  - Card height stability is a geometry property, so it is asserted in the
    harness (below), not in the string tests.
- `frontend/ui.test.js`: the new `warningPopover` builder renders its text and
  its action buttons; DELETE nothing (helpTooltip unchanged).
- `scripts/uitest/probes.js`: NEW `valueCardGeometry()` probe: seed a value with
  many spellings and a full-coverage intersection, record `.value-card` height;
  trigger a spelling edit (which re-derives) and a warning toggle; assert the
  card height is unchanged across both, and that the `.card-body` `scrollTop` is
  unchanged after the edit. This is the probe that proves CR item 2 is fixed.

---

## CR3 — The spellings popup

### Current behaviour

Spellings are added inline (`spelling-add` reveals an input), edited by
double-click (`revealVariantEditInput`), and deleted per chip (`spelling-del`
calls `deleteVariant`), all on the card. Cross-card regrouping is drag-only
(`wireVariantDrag` -> `moveSpelling`).

### Change

Add a **spellings popup**, opened by "+N more" and "+ add" on the compact card.
It owns full spelling management for one Value. Layout (from the owner's mockup):

```
Spellings for <MainText>                                        [x]
<count> spellings

[ Add a new spelling            ]  [ Add ]
[ Search spellings              ]

  <MainText>                                          Move to   Delete
  <spelling>                                          Move to   Delete
  ... (scrollable list)

Changes are reflected immediately in the compact card.
```

Rules:

- The popup is a NEW modal shape. `modal.js` today only renders `askConfirm` /
  `askChoice` (a title, a body, buttons). This popup is richer: an input + Add, a
  search, and a live editable list. Introduce it as a dedicated modal kind rather
  than bending `askConfirm`. Two clean options; pick one and note it:
  1. Extend the shell modal layer (`state.confirm` -> a more general
     `state.modal` with a `kind`), rendered by `modalLayerHTML`, so it inherits
     the backdrop, Escape-to-close and focus handling already in `wireModal`.
  2. A self-contained overlay component owned by the workspace. Given the
     "no native dialogs" rule and the existing shell-level overlay, **option 1 is
     preferred**: one overlay mechanism, one Escape handler, one backdrop.
- It edits state LIVE (no OK/Cancel): every action calls the existing reducers
  and repaints, and the compact card updates because it reads the same state.
  The footer line "Changes are reflected immediately in the compact card."
  states this.
- **Add**: the input + Add button call `addSpelling(cat, mainText, value)` (which
  already curates the value and re-derives). Enter in the input also adds.
- **Search**: filters the listed rows in place (substring, case-insensitive),
  the same interaction the values search uses; it never re-renders the input
  (preserve focus/caret, mirror `applyValuesSearchFilter`). This search box gets
  the ✕ clear control (CR5).
- **Delete** per row calls `deleteVariant(cat, mainText, spelling)` (curates).
  The MAIN TEXT row is shown (distinct styling, as in the mockup where it is
  darker while derived spellings are blue) but is NOT deletable: deleting the
  main text is meaningless (renaming it is a card action). Render its Delete as
  absent or disabled with a title explaining why.
- **Move to...** per row: opens `askChoice` with the OTHER values as choices
  (reuse the exact picker `wireGroupPanel` builds: `choices` of
  `{ id: valueKey(...), label: "<mainText> (<category>)" }`), then calls
  `moveSpelling(cat, mainText, toCategory, toMainText, spelling)`. This is the
  path for regrouping a spelling that lives in the "+N more" overflow. The main
  text row has no Move (moving the main text is not a spelling move).
- The count line ("<n> spellings") reflects the full list, not the filtered view.

The inline card affordances that the popup replaces are removed from the compact
card (per CR2: no per-chip delete, no inline add on the card). `deleteVariant`,
`addSpelling`, `moveSpelling`, `curate` and `revealVariantEditInput` remain the
reducers; only their entry points move into the popup. Editing a spelling's text
(today `revealVariantEditInput` on double-click) moves into the popup as an
inline edit of the row, or is dropped in favour of delete-then-add; **decision:
keep an inline edit of the row** so a typo fix does not lose the row's position,
reusing the same curate-on-edit path.

### Tests

- NEW `frontend/spellingspopup.test.js` (render + wiring, using `testhtml.js` and
  `testdom.js`): the popup lists every spelling with the count; Add appends and
  curates; Delete removes a spelling and curates; the main-text row has no
  Delete; search filters rows in place without replacing the input; Move calls
  `moveSpelling` with the chosen target. Drive the reducers through the wiring so
  the lower-casing DOM (`testdom.js`) is exercised, per the `dataset` guard.
- `frontend/identifyvalues.test.js`: "+N more" and "+ add" open the popup (assert
  the popup state is set); the popup reflects a live add (count increments).
- `scripts/uitest/probes.js`: NEW `spellingsPopup()` probe: open it, assert it is
  on screen and scrolls internally, add a spelling, assert the compact card's
  visible chip row and "+N more" update.

---

## CR4 — Scroll stability: keep the list position through card actions

CR2 removes the height changes from warnings and spelling edits, so the only
legitimate remaining height change is DELETING a whole card. Add a small anchor
so even that does not throw the position.

### Change

- The generic `scroll.js` restore stays as the baseline. For the value-list
  scroller specifically, when a card is deleted, keep the list anchored: after
  `deleteValue`, the `.card-body` should stay showing the same neighbourhood
  rather than clamping to a new maximum. The simplest robust approach that fits
  the existing snapshot/restore model: in `scroll.js restoreScrollPositions`,
  clamp intentionally to `min(saved, scrollHeight - clientHeight)` (which the
  browser already does) but ALSO, when the saved offset exceeds the new maximum,
  that is expected on delete; the disorientation the owner reported was the
  spelling/warning collapse (CR2), so verify in the harness whether CR2 alone
  resolves the complaint before adding more machinery here.
- If the harness still shows a jump on delete after CR2, add a targeted anchor:
  record, before the delete, the `data-key` of the card just above the deleted
  one; after the repaint, if the saved `scrollTop` was clamped, scroll that
  anchor card into view (`scrollIntoView({ block: "nearest" })`). Keep this in
  the delete handler, not in `scroll.js`, because it is specific to the values
  list.

> Implementer guidance: treat CR4 as "verify CR2 fixed it; add the anchor only if
> the probe still fails". Do not add the anchor speculatively.

### Tests

- `scripts/uitest/probes.js`: extend `valueCardGeometry()` (CR2) with a delete
  case: seed enough values to scroll, scroll down, delete a mid-list card, assert
  `scrollTop` moves by at most one card height (not to 0).
- `frontend/state.test.js`: unchanged reducer tests for `deleteValue`; the scroll
  behaviour is geometry and lives in the harness.

---

## CR5 — The ✕ clear control in every search box

### Current behaviour

Three search inputs exist: the Suggestions search (`#workspace-search`, `head()`
~219), the My Values search (`#values-search`, `valuesFilterBar` ~535), and (new
in CR3) the spellings-popup search. None has a clear button; the user must select
and delete the text.

### Change

Add a single reusable **clearable search** affordance in `ui.js` (a `searchBox`
builder, or extend the current `.search-box` label): the input plus an ✕ button
that shows only when the input is non-empty, positioned inside the box on the
right. Clicking ✕ clears the input and fires the same handler the input's own
`input` event fires (so the filtered rows update and focus returns to the input).

Apply it to all three search boxes:
- Suggestions search: clears `suggestionFilter.search` and re-renders (its
  handler already `setState`s).
- My Values search: clears `valuesFilter.search` and calls
  `applyValuesSearchFilter` in place (no re-render, preserve focus), matching the
  existing no-setState pattern.
- Spellings-popup search: clears the popup's in-place filter.

Do NOT confuse this with the existing destructive **"Clear all"** button in the
values toolbar (`#btn-clear-values`), which deletes every Value. The ✕ only
empties a search field; it carries no label text (an icon-only control inside the
field), so there is no naming collision.

### Tests

- `frontend/ui.test.js`: the `searchBox` builder renders the ✕ only when there is
  text (or renders it always but hidden by CSS when empty; assert whichever the
  implementation chooses), and the ✕ carries an accessible label.
- `frontend/identifyvalues.test.js`: clicking the My Values ✕ empties the search
  and reveals all cards (mirrors the existing search-filter test).
- `frontend/spellingspopup.test.js`: the popup search ✕ clears the filter.
- `scripts/uitest/probes.js`: optional, fold into existing search probes if any;
  otherwise the string/wiring tests suffice for a control this small.

---

## Decisions taken

1. **Drop the partial intersection warning; keep the full-coverage one.** The
   partial case warns about occurrences that the winning pattern redacts anyway,
   so it names no leak and no action. The full case tells the user a declared
   Value gets no placeholder of its own, which is actionable.
2. **The full-coverage message names the literal covered text, not the Value's
   canonical form.** `Span.Original` is plumbed through as
   `Intersection.MatchedTexts`, so "Coca" covered inside "coca" reads as
   `"coca" (a spelling of "Coca")`, and the "Pierre Dupont" fragments read as
   `"pierre", "dupont" (spellings of "Pierre Dupont")`.
3. **The value card becomes fixed-height.** Warnings collapse to one hover icon
   with actions; spellings become a bounded, single-line, read-only chip preview
   plus a popup. This removes the height-thrash that caused the scroll jump, so
   the redesign IS the scroll fix (CR4 only adds a delete anchor if the harness
   still shows a jump).
4. **65-character budget drives the visible chips, not a fixed count.** The
   overflow goes behind "+N more".
5. **Drag-to-regroup is kept for visible chips only; overflow chips are moved via
   "Move to..." in the popup**, reusing the Group-with picker. One mental model:
   visible chip = quick drag; popup = full management (add, edit, delete, move).
6. **The spellings popup is a shell-level modal kind**, not a bespoke overlay, so
   it inherits the one backdrop, Escape handler and focus trap; it edits live.
7. **The "Show/Hide spellings" toolbar toggle is removed.** Spellings are always
   shown compactly and fully managed in the popup, so a toggle that changes card
   height is redundant and reintroduces the thrash.
8. **A new `warningPopover` builder is justified** even though the charter warns
   against two builders for one control: a hover surface that holds interactive
   buttons is a different contract from `helpTooltip`'s text-only surface.
9. **No session-shape change.** `SessionVersion` stays 8; nothing here persists.

## Conflict analysis

### Files touched by more than one CR

| File | CRs | Note |
|---|---|---|
| `frontend/views/identifyworkspace.js` | CR1, CR2, CR3, CR4, CR5 | the workspace is the centre of gravity; do CR1 (small, self-contained) first, then CR2/CR3 together since the popup replaces the card's spelling affordances |
| `frontend/ui.js` | CR2, CR3, CR5 | `warningPopover` added (CR2), the modal kind or overlay (CR3), `searchBox` (CR5) |
| `frontend/style.css` | CR2, CR3, CR5 | fixed-height card + chip row, popup, clearable search; separate rule blocks |
| `frontend/copy.js` | CR1, CR3, CR5 | intersection copy (CR1), popup strings (CR3), clear label (CR5) |
| `frontend/state.js` | CR3, CR5 | the modal kind (CR3) if option 1; search filter state already exists |
| `scripts/uitest/probes.js` | CR2, CR3, CR4, CR5 | ONE file, both harnesses (`uitest_parity_test.go`); batch probe edits per CR |
| `backend/engine/intersections.go` + test | CR1 | isolated to the engine |

### Hotspots

- **`valueCard`'s structure changes shape** (notes removed, chip row bounded),
  and several handlers in `wireValues` bind to elements that move into the popup
  (`spelling-add`, `spelling-del`, `spelling-text` dblclick). Move those bindings
  to the popup's wiring in the SAME CR (CR3) so no handler is left binding a
  selector that no longer exists on the card.
- **`dataset_parity_test.go`**: any new `data-` attribute (e.g. on popup rows for
  the spelling and target) must be kebab-case. The popup rows will carry
  `data-spelling` (exists) and any `data-category` / `data-main-text` must follow
  the existing kebab convention.
- **The intersection note's two card actions** (`.intersection-allow`,
  `.value-group`) move into the warning popover (CR2); rewire them there and keep
  `wireGroupPanel` reachable.

## Recommended order

1. **CR1** first, alone: engine + copy + view, self-contained, and it is the
   correctness fix the whole discussion started from. Push on its own.
2. **CR2** next: the compact card (warning icon + bounded chip row). This is the
   scroll fix. Land it with the `valueCardGeometry` probe so the fix is proven.
3. **CR3**: the spellings popup, which absorbs the card's old spelling
   affordances. Do it right after CR2 so the card and popup land coherent.
4. **CR5**: the ✕ clear, small, touches all three search boxes (the popup one
   now exists).
5. **CR4**: verify the harness shows no scroll jump after CR2/CR3; add the delete
   anchor only if it still does.

## Acceptance criteria

- `go test ./...` and `node --test "frontend/**/*.test.js"` both green.
- `task audit` reports no new finding.
- In the running application, on step 2, "My values":
  1. No "N of M occurrences ..." warning appears. When a Value is fully covered
     by a higher-priority match, the card shows a yellow warning icon; hovering
     it reads "Every occurrence of ... is not replaced under its own type",
     naming the literal covered text (e.g. `"coca" (a spelling of "Coca")`), with
     the resolution actions beside it.
  2. Editing a spelling, editing the main text, and (within one card height)
     deleting a card no longer move the list's scroll position.
  3. The card is compact: warnings are a single red/yellow icon with the text and
     actions on hover; spellings are a single-line pill preview up to 65
     characters with "+N more" and "+ add".
  4. "+N more" and "+ add" open the spellings popup, which lists every spelling
     with Add, Search, per-row Delete and Move to..., and updates the card live.
  5. Dragging a visible chip onto another card still regroups it; a chip in the
     overflow is regrouped via Move to... in the popup.
  6. Every search box (Suggestions, My Values, spellings popup) has an ✕ that
     clears it.
- The rendering harness passes, including the new probes: card geometry stable
  across a spelling edit and a warning toggle, the popup opening and scrolling,
  and the compact chip row updating after a popup add.

## First actions for the implementation coordinator

1. Read `CLAUDE.md`, `frontend/CLAUDE.md`, `backend/CLAUDE.md`,
   `frontend/BRIDGE.md`, `docs/UITESTING.md` and `docs/TESTING.md`.
2. Implement CR1 (engine + copy + view), run both suites, push.
3. Implement CR2, add the `valueCardGeometry` probe, confirm the scroll jump is
   gone in the harness.
4. Implement CR3 (popup), CR5 (✕ clear), then CR4 (anchor only if still needed).

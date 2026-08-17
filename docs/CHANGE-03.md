# CHANGE-03 — Value intersections, the selection replace menu, Compare search

You are executing a change order against the existing **doc-anonymiser**
repository (pattern P0 — pure Go + Wails v2, no CGo, no npm). This document
holds **one self-contained implementation section per change request (CR1–CR4)**,
followed by the **decisions taken**, a **conflict analysis** and the
**recommended execution sequence** (they touch overlapping files, so order
matters).

Ground rules for this change order (unchanged from `CLAUDE.md`):

- `CLAUDE.md` and the two subtree charters remain authoritative. Nothing here
  weakens the local-only guarantee, the one-file Ollama boundary, or the
  zero-CGo rule.
- **A change is not finished until its tests move with it** (`CLAUDE.md` §6).
  Each CR below names the tests to add, update and delete. Both suites gate:
  `go test ./...` and `node --test "frontend/**/*.test.js"`.
- User-visible copy never contains em dashes (enforced by `copy_guard_test.go`
  and `frontend/copy.test.js`). Use commas or full stops. Every proposed string
  in this document already obeys that.
- The parity guards (`category_parity_test.go`, `step_parity_test.go`,
  `copy_guard_test.go`, `uitest_parity_test.go`, `frontend_tests_test.go`) are
  load-bearing. If one fails, fix the inconsistency, not the guard.
- Comments explain intent in the present tense. Do not write "used to be" or
  "CR1 changed this" into the code (`CLAUDE.md` §6).

---

## Why CR1 is the biggest of the four

The application already has four things that can claim the same characters:

| Route | Producer | Span confidence today |
|---|---|---|
| Native (signal) | `DetectPIISelected` (`backend/engine/pii.go`) | `ConfidenceDeterministic` = 1.0 |
| Custom patterns | `DetectCustomPatterns` (`backend/engine/entities.go`) | `ConfidenceDeterministic` = 1.0 |
| Auto detection (Smart) | accepted candidates, then `DetectEntities` | `ConfidenceManualDefault` = 0.95 |
| Local AI | accepted proposals, then `DetectEntities` | `ConfidenceLLMDefault` = 0.8 |

Precedence is currently decided by `resolveOverlaps` (`pii.go` ~line 555),
which sorts by **confidence, then length, then start, then category name**. That
has three defects the change request is reporting:

1. **Native and Custom patterns tie at 1.0.** The winner is then whichever match
   is *longer*, and if the lengths are equal, whichever category name sorts
   first alphabetically. That is the "random application behaviour": which route
   wins depends on the text, not on a rule anybody decided.
2. **Provenance is not stored anywhere.** Once the user accepts a candidate it
   becomes an `Entity`, and the only trace of which route found it is
   `Entity.Confidence` (0.95 vs 0.8). Confidence is simultaneously the input to
   the `MinConfidence` floor, so raising the floor also silently reorders
   precedence. Two different questions ("how much do I trust this?" and "who
   found it?") are riding on one number.
3. **The user is told after the fact, on the wrong screen.** Overlaps become
   `Conflict{Kind: "overlap", Severity: "warn"}` inside
   `overlapWarnings.add` (`conflicts.go` ~line 320) and surface in
   `Results.Validation` on **step 3**, after the run. Step 2's "My values" cards
   show only the three declaration conflicts computed by
   `entityConflicts` (`state.js` ~line 1780): ambiguity, collision, allowlist.
   There is no intersection warning on the card that owns the value.

CR1 therefore needs: an explicit precedence key, a place to store provenance,
a detection sweep that can answer "what intersects what" **before** a run, and a
warning on the value card.

---

## CR1 — Intersections between detected values

### CR1a — An explicit origin, and one total precedence order

#### Change: the engine

1. **`backend/engine/origin.go` (new file).** Origin is a first-class concept, so
   it gets its own file rather than being buried in `pii.go`:

   ```go
   // Origin names WHICH ROUTE produced a detection. It is deliberately
   // separate from Confidence: confidence answers "how much is this
   // trusted" and feeds MinConfidence, origin answers "who found it" and
   // feeds precedence. With one number doing both jobs, raising the
   // confidence floor silently reordered precedence.
   const (
       OriginNative   = "native"   // pass 1 regex signal
       OriginDeclared = "declared" // the user typed it: custom patterns and manual values
       OriginAuto     = "auto"     // offline Smart detection
       OriginAI       = "ai"       // the local model
   )

   // OriginRank is the superseding order. LOWER WINS. An unknown or empty
   // origin ranks with declared values rather than last, so a producer that
   // states none is trusted rather than silently demoted.
   func OriginRank(origin string) int
   ```

   Ranks: `native` 1, `declared` 2, `auto` 3, `ai` 4, unknown/empty 2.

2. **`Span.Origin string`** (`pii.go`, beside `Confidence`), set by every
   producer:
   - `DetectPIISelected` and the code detector: `OriginNative`.
   - `DetectCustomPatterns`: `OriginDeclared`.
   - `DetectEntities`: the entity's own origin (see CR1b).

3. **`resolveOverlaps` gains origin as the FIRST comparator**, before
   confidence:

   ```
   1. lower OriginRank wins          (native > declared > auto > ai)
   2. higher confidence wins         (unchanged, now only a tie-break inside one route)
   3. longer match wins              (the email-inside-a-URL case, unchanged)
   4. earlier start, then category   (determinism, unchanged)
   ```

   Update the doc comment above `ResolveOverlaps` to state the four-step order
   and why origin comes first. Keep `FilterByMinConfidence` running *before*
   resolution (unchanged): a value below the floor must not be able to suppress
   anything.

4. **`Registry.Assign` must agree.** `byOriginal` gives the first claimant
   ownership of a string for the whole session, so the string must be claimed by
   its highest-precedence owner. That is what CR1c's two-phase run guarantees.
   Add `Registry.Assign(category, original string)`'s doc comment note that
   ownership is decided upstream, by ownership unification, and that this
   function is deliberately not precedence-aware: changing an entry's category
   after the fact would change its placeholder text, and a placeholder that has
   already left the machine can never be re-numbered (`CLAUDE.md` §5).

#### Tests

- `backend/engine/origin_test.go`: `OriginRank` table, including unknown and
  empty.
- `backend/engine/pii_test.go`: table over `resolveOverlaps` with **equal
  confidence, equal length** spans of different origins, asserting the CR order
  in all six pairings. This is the case that has no defined answer today.
- `backend/engine/pii_test.go`: a native span and a declared span, native
  **shorter**, asserting native still wins (origin beats length).
- Keep and re-verify the existing "email inside a URL" length test: both spans
  are native, so rule 3 still decides it.

---

### CR1b — Provenance survives the accept

Today `addCandidates(items, source)` (`state.js` ~line 1330) records
`source` on the candidate row, and `acceptCandidate` / `acceptAllShown` drop it:
`addEntities([{category, canonical}])` writes no source at all.

#### Change

1. **`engine.Entity.Origin string`** (`entities.go`), documented as above. Empty
   reads as `OriginDeclared`, which is correct for every value the user typed and
   for any session file written before this field existed (such a file is refused
   by version anyway, see CR4).
2. **`DetectEntities`** stamps `Span.Origin = OriginRank`-relevant value from the
   entity: `e.Origin` when set, `OriginDeclared` when empty.
3. **`acceptProposals`** (`pipeline.go` ~line 601) already stamps
   `Confidence: ConfidenceLLMDefault` on LLM proposals; it now also stamps
   `Origin: OriginAI`. Auto-detected candidates accepted through the UI carry
   `OriginAuto`; values typed by the user carry `OriginDeclared`.
4. **`state.js`**:
   - `addEntities` accepts and stores `origin: item.origin ?? "declared"`.
   - `acceptCandidate` / `acceptAllShown` pass `origin: originOf(cand.source)`,
     where `originOf` maps `"smart"` to `"auto"` and `"ai"` to `"ai"` (one small
     exported pure function, so the mapping is testable and has one home).
   - `acceptedEntities` includes `origin` in the pipeline payload.
   - `ORIGINS` (`state.js`) mirrors the Go constants, guarded like the
     categories are (see the parity note below).
5. **`frontend/api.js`** doc comments and **`frontend/BRIDGE.md`**: the entity
   shape crossing the bridge is now
   `{category, canonical, manualVariants, origin}` (CR4 removes
   `excludedVariants`).
6. **Origin is shown, not just stored.** The value card gains a small origin
   chip, because the precedence rule is only meaningful to the user if they can
   see which route owns a value. Copy in `copy.js WORKSPACE`:
   `originLabel: {native: "Native", declared: "You", auto: "Smart detection", ai: "Local AI"}`.

#### Parity guard

`category_parity_test.go` exists because a Go/JS enum drifted once already. Add
the same guard for origins: extend it (or add `origin_parity_test.go`, same
package `main`) to assert `engine`'s four origin constants and `state.js`'s
`ORIGINS` list are identical, and that `copy.js` has a label for each.

#### Tests

- `backend/engine/entities_test.go`: an entity with `Origin: OriginAI` produces
  spans carrying that origin; an entity with no origin produces
  `OriginDeclared`.
- `backend/app_entities_test.go`: an AI proposal accepted through
  `acceptProposals` carries `OriginAI`.
- `frontend/state.test.js`: accepting a `"smart"` candidate yields an entity with
  `origin: "auto"`; accepting an `"ai"` candidate yields `"ai"`; a manually added
  value yields `"declared"`; `acceptedEntities` carries it.
- `frontend/identifyworkspace.test.js`: the card renders the origin chip with the
  right label.

---

### CR1c — Ownership is decided globally, before a single placeholder is minted

This is the structural half of CR1 and the reason the CR mentions bugs.

#### Current behaviour

`Run` (`pipeline.go` ~line 266) loops over documents and, per document, calls
`anonymiseDocument` → `anonymiseText`, which **detects and replaces in one
step**. Consequences:

- Resolution is per text region (per document, per grid cell, per JSON blob), so
  two occurrences of the same string in different regions can be won by
  different routes.
- `Registry.Assign`'s `byOriginal` index then freezes whichever claim happened to
  be assigned **first**, and assignment order is byte-offset order within
  document order. A value that a native regex claims in document 2 but Local AI
  claims in document 1 is owned by the AI category for the whole session, which
  is precisely the "random application behaviour" being reported.

#### Change: split `Run` into detect, unify, apply

1. **`anonymiseText` splits in two** (`pipeline.go`):
   - `detectText(text string, scope detectionScope) []Span` — everything up to
     and including `FilterByMinConfidence`, returning unresolved spans.
   - `applySpansToText(text string, spans []Span, assign func(Span) string, ...)`
     — resolution (`resolveOverlaps`), tracing, `ApplySpans`.
2. **`anonymiseDocument` splits the same way**, around a new type that keeps the
   per-region structure the grid and JSON formats need:

   ```go
   // documentPlan is one document's detections, held between the detect
   // phase and the apply phase. Regions mirror the document's shape: one
   // region for markdown, one per grid cell, one for the XLSX JSON blob.
   type documentPlan struct {
       doc     Document
       scope   detectionScope
       regions []planRegion // {key, text, spans}
   }
   ```

3. **`Run` becomes three phases** in place of one loop:
   - **Phase A, detect.** The existing loop body up to `anonymiseDocument`,
     including the per-document LLM deep scan and `acceptProposals` (unchanged),
     now ends at `detectDocument(doc, scope) documentPlan`. Progress events keep
     their current stages.
   - **Phase B, unify ownership.** `unifyOwnership(plans []documentPlan)` walks
     every span in every region, groups them by
     `strings.ToLower(span.CanonicalOrOriginal())`, picks the winning
     `(origin, category, canonical)` by `OriginRank` (then the existing
     tie-breaks), and rewrites every other claim on that string to the winner.
     One string, one owner, across the whole batch, decided by rule and not by
     file order. It returns the losing claims so they can become warnings.
   - **Phase C, apply.** Per region: `resolveOverlaps` (now with a globally
     consistent set) then `ApplySpans` with the registry `assign` closure. Pass 4
     (registry post-pass), the simple-replace pass and report assembly are
     unchanged and still run after.
4. **`Results.Validation.Warnings`** gains the unification losers, in the
   `"overlap"` shape `overlapWarnings` already produces, with a message that
   names **both** routes (see the copy in CR1e). Keep the existing
   `maxOverlapWarnings` / `maxOverlapSpansExamined` caps: they exist so a
   pathological document cannot generate megabytes of warnings.

#### Cost and risk

Phase A no longer discards spans as it goes, so a batch holds its spans in
memory until phase C. A span is 5 words plus two strings; a 10 MB batch with a
replacement every 200 bytes is ~50k spans, a few MB. Acceptable, and bounded by
the same import limits as the documents themselves. **No detection work is
duplicated:** phase C reuses phase A's spans.

If this refactor has to be de-scoped, the fallback is per-document unification
only (call `unifyOwnership` on one plan at a time). Per-document behaviour
becomes correct and deterministic, cross-document attribution stays
first-document-wins, and CR1d's report still tells the user when that matters.
Record the choice in the commit message if the fallback is taken.

#### Tests

- `backend/engine/pipeline_test.go`: two documents, the string claimed by Local
  AI in document 1 and by a native regex in document 2. Assert the registry
  entry's category is the **native** one, whichever order the documents are
  loaded in (run the table both ways). This test fails on today's code.
- Same file: a value claimed by a custom pattern and by an auto-detected value.
  Assert the custom pattern owns it, and that exactly one placeholder exists for
  the string.
- Same file: the grid and XLSX-JSON formats still round-trip through the split
  phases (extend the existing format tests rather than adding parallel ones).
- `backend/engine/tracing_test.go`: traces still describe the spans actually
  applied (`OnTrace` now fires in phase C).
- A **determinism** test: the same batch run twice, and once with the document
  slice reversed, yields identical mapping entries.

---

### CR1d — Intersections are detected before the run

CR1 asks for the warning in step 2 ("My values"), which means the answer must
exist without running the pipeline.

#### Change

1. **`engine.DetectIntersections(docs []Document, scope detectionScope) []Intersection`**
   (new, `backend/engine/intersections.go`). It runs the same `detectText`
   producers over each document, resolves with the same comparator, and reports
   what lost:

   ```go
   // Intersection is one value whose text is claimed by more than one
   // route. It is a WARNING, never blocking: the precedence rule always has
   // an answer, and refusing the run would punish the user for a
   // configuration the engine can resolve.
   type Intersection struct {
       Value       string `json:"value"`       // the value as the user sees it
       Category    string `json:"category"`    // the category it is filed under
       Origin      string `json:"origin"`      // its route
       WinnerValue    string `json:"winnerValue"`
       WinnerCategory string `json:"winnerCategory"`
       WinnerOrigin   string `json:"winnerOrigin"`
       Occurrences    int    `json:"occurrences"`      // how many of this value's hits are covered
       TotalOccurrences int  `json:"totalOccurrences"` // how many it has in total
       Documents      []string `json:"documents,omitempty"` // up to 3 names, for the message
   }
   ```

   `Occurrences == TotalOccurrences` means the value is **never** replaced under
   its own category, which is the case worth shouting about; a partial overlap is
   a milder note. The view distinguishes them.

2. **`App.CheckIntersections(request)`** (`backend/app_entities.go`), taking the
   same request shape `ValidateValues` takes plus the settings already stored on
   the App, resolving to `{intersections: [...]}`. It reads the loaded documents,
   detects, and returns; it never mutates the registry, so it is safe to call
   while the user is still editing values on step 2.

3. **`frontend/api.js checkIntersections(request)`** wrapper, plus its row in
   `BRIDGE.md` under "Values screen".

4. **`state.js`**:
   - `intersections: []` in the store, `setIntersections(list)` reducer, cleared
     by `resetState`, `startNewBatch` and any change to the value list (a stale
     intersection warning is worse than none).
   - `intersectionsFor(s)` returns a `Map` keyed by `entityKey(category, value)`
     so the view attaches each one to its card with no searching, exactly as
     `entityConflicts` is consumed today.
   - Debounced refresh: `identifyworkspace.js` asks for a recheck after a
     detection run completes, after a value is added, removed, renamed, grouped
     or re-categorised, and after a pattern is added or removed. One debounce
     (400 ms) with the last call winning, in the view's wiring, not in state.
   - A missing bridge (plain browser, render tests) leaves the list empty and the
     screen unchanged. It must never throw: the whole screen would go blank.

#### Tests

- `backend/engine/intersections_test.go`: a document where an email address is
  also a declared value; a document where a custom pattern covers a declared
  value; a document where an auto value covers an AI value. Assert winner,
  counts, and that nothing is reported when the two values do not actually
  co-occur in any text.
- `backend/app_entities_test.go`: `CheckIntersections` on a loaded batch returns
  the same verdicts a real `Run` produces for the same input (the guard against
  the check and the pipeline disagreeing, which is why the check reuses
  `detectText` instead of a parallel implementation).
- `frontend/state.test.js`: `setIntersections` / `intersectionsFor` keying;
  clearing on value edits.
- `frontend/identifyworkspace.test.js`: a card with an intersection renders the
  warning; a card without one does not.

---

### CR1e — The warning on the "My values" card

#### Current behaviour

`valueCard(e, conflict, s)` (`identifyworkspace.js` ~line 509) tints the card
`.conflicted`, marks the offending name or chip, renders `conflictNote` from
`conflictMessage(c)`, and offers "Solve conflicts" (`solvePanel`). Three kinds
are handled: `ambiguity`, `collision`, `allowlist`. All three are **blocking**.

#### Change

1. An intersection is a **warning**, so it must not look like a blocking
   conflict. Add a second, quieter note: class `value-note intersection-note`
   inside a `.value-card.intersects` (a warn tint in `style.css`, distinct from
   `.conflicted`). The step 2 to 3 gate (`state.js canGoTo`) is **not** extended:
   an intersection never blocks navigation, and the CR does not ask it to.
2. Copy, in `copy.js WORKSPACE` (no em dashes):

   ```js
   // Intersections: two routes claim the same text. The precedence rule
   // always decides, so these are warnings that explain the decision.
   intersectionTitle: "Overlaps another detection",
   intersectionAll(value, winner, route) {
     return `Every occurrence of "${value}" is also matched by ${route} as "${winner}", which takes priority. This value is not replaced under its own type.`;
   },
   intersectionSome(covered, total, value, winner, route) {
     return `${covered} of ${total} occurrences of "${value}" are also matched by ${route} as "${winner}", which takes priority there.`;
   },
   intersectionOrder: "Priority order: native detection, then your own values and patterns, then Smart detection, then Local AI.",
   intersectionFix: "If this value should win instead, switch off the type that covers it, narrow the pattern, or add the covering term to Never anonymise.",
   ```

   `route` comes from `WORKSPACE.originLabel` (CR1b), so the message names the
   route in the same words the origin chip uses.
3. The intersection note carries the same two actions the value already has where
   they apply: "Never anonymise this term" (existing `addAllowTerm` path) and
   "Group with" (existing `groupPanel`), because folding the two values into one
   is usually the right answer. No new mechanism.

#### Tests

- `frontend/copy.test.js`: the new keys exist and contain no em dashes (the guard
  already sweeps the file, so this is an existence assertion).
- `frontend/identifyworkspace.test.js`: a fully covered value renders
  `intersectionAll`, a partly covered one renders `intersectionSome`, the card
  gets `.intersects` and **not** `.conflicted`, and the "Continue" gate is
  unaffected.

---

### CR1f — Main value versus variant: the shorter form wins

The second half of CR1: when detection finds both "Coca-Cola" and "Coca-Cola
company", they are one value, and the shorter form must be the main value with
the longer forms as variants.

This is not cosmetic. `DetectEntities` matches variants **longest first**
(`entities.go`), so with "Coca-Cola" as the main value and "Coca-Cola Ltd." as
one of its variants, the whole phrase collapses into one placeholder. With them
as two separate values, the shorter one fires inside the longer one and the text
reads `[BRAND_1] Ltd.`, which leaks the legal form and spends two numbers on one
company.

#### Change

1. **`engine.FoldValueFamilies(cands []Candidate) []Candidate`** (new, in
   `backend/engine/discover.go` beside the other candidate post-processing, or
   its own `families.go` if `discover.go` grows past comfort). Rules, each one
   there to stop a specific wrong fold:
   - Group only **within one category**. A person "Delta" and an organisation
     "Delta Industries" are a cross-category *intersection* (CR1d), not a family:
     folding them would file a human being under an organisation.
   - A longer candidate joins a shorter one only when the shorter appears in it
     **at word boundaries** (the same unicode-aware boundary rule the entity pass
     uses, so the fold agrees with what will actually match).
   - The shortest member becomes the main value. Ties (equal length) break on
     occurrence count, then alphabetically, so the result is deterministic.
   - The shorter form must be at least `minVariantLen` runes long, otherwise the
     family is left alone: promoting a 2-character stem to a main value would
     shred ordinary text.
   - A member that is allowlisted is dropped from the family rather than folded.
   - Chains fold transitively into one family ("Coca", "Coca-Cola", "Coca-Cola
     Ltd.") with the shortest eligible member as main.
   The folded longer forms come back as the candidate's proposed variants, so the
   review row shows one value with its spellings rather than three rows.
2. **`Candidate.Variants []string`** so a folded family survives the bridge, and
   `state.js addCandidates` stores it; accepting a candidate passes them as
   `manualVariants` to `addEntities`.
3. **`RunDetection`** (`backend/app_detect.go`) folds **once, over the merged
   output of every route**, after the AI phase and before returning. Folding per
   route would leave a Smart "Coca-Cola" and an AI "Coca-Cola Ltd." unmerged,
   which is exactly the case the CR names.
4. **`state.js` folds on the late paths too**, because values also arrive one at
   a time (the "Something missed?" row, CR2's new-value option): a new
   `foldIntoFamily(category, canonical)` helper that, before adding, checks the
   existing values of that category for a family relationship and, if found,
   adds the new string as a **variant** of the shorter existing value (or renames
   the existing value to the new shorter one and keeps the old as a variant).
   Report the decision in the toast, because a silent "your value became a
   variant of another one" is indistinguishable from the button not working:
   `ANONYMISE.foldedIntoValue(added, main)` = `"Added as a spelling of \"<main>\", which is the shorter form."`.

#### Tests

- `backend/engine/families_test.go` (table): the Coca-Cola trio; a cross-category
  pair left unfolded; a substring that is not at a word boundary
  ("Alten"/"Altenberg") left unfolded; an allowlisted member dropped; a
  2-character stem left unfolded; ties broken deterministically; a transitive
  chain.
- `backend/app_detect_test.go`: a Smart candidate and an AI proposal in the same
  family come back as one candidate with the longer form as a variant.
- `frontend/state.test.js`: `foldIntoFamily` on manual add, both directions (new
  value is shorter, new value is longer), and the toast wording.
- `frontend/candidatemodel.test.js`: a candidate carrying variants renders one
  row.

---

## CR2 — The selection panel becomes copy or replace, with three replace modes

### Current behaviour

`wireTextSelection` (`anonymise.js` ~line 1131) watches `mouseup` in both panes,
keeps `selection = {text, x, y}` (rejecting anything over 120 characters), and
**immediately calls `nextRulePlaceholder()`**, which reserves a `[CUSTOM_N]` in
the Go registry. `selectionPanel()` (~line 694) renders the floating card with
one field, and `wireSelectionPanel` (~line 1172) turns Apply into
`addSimpleRule({find, replace, caseSensitive: true})` followed by
`runFastRerun`. So the only outcome is a find-and-replace rule.

Two things to fix while here:

- **Reserving on selection spends numbers the user never uses.** Every stray
  drag burns a `[CUSTOM_N]`, and numbers are never freed by design
  (`CLAUDE.md` §5). Reserve only when the user chooses mode 3.
- The panel is one field, so it has no room for a choice. It becomes a small
  two-stage panel.

### Change

1. **View-local state** in `anonymise.js`, beside `selection`:
   ```js
   // The selection panel's stage: null (choose copy or replace), "replace"
   // (choose how), or one of the three replace modes. View state: it is about
   // this selection and dies with it.
   let selectionStage = null;         // null | "replace"
   let selectionMode = null;          // "variant" | "value" | "text"
   let selectionTarget = "";          // the variant autocomplete draft
   let selectionCategory = "person_names";
   ```
   Every one resets when `selection` is cleared or the compare document changes.
2. **`selectionPanel()` renders three stages**:
   - **Stage 1**: the selected text, then `Copy` and `Replace`.
   - **Stage 2** (`Replace`): three radio options, in the CR's order, with the
     wording below.
   - **Stage 3**: the fields for the chosen mode, plus `Apply` and `Cancel`.
     `Cancel` steps back to stage 1 rather than closing, so a mis-click does not
     lose the selection.
3. **Copy** calls a new `copyText(text)` bridge wrapper. Clipboard access goes
   through Go, as `CopyDocument` already does (`app_export.go` ~line 377):

   ```go
   // CopyText puts an arbitrary short string on the system clipboard. It
   // exists for the Compare pane's selection panel, where the user copies a
   // value out of the preview. Length-capped so a mis-drag over a whole
   // document cannot push megabytes through the clipboard.
   func (a *App) CopyText(text string) error
   ```
   Cap at 4096 bytes with an actionable error ("that selection is too long to
   copy, select a single value"). Toast on success:
   `ANONYMISE.selectionCopied` = `"Copied to the clipboard."`.
4. **Mode 1, "Make it a spelling of an existing value"**: reuse
   `entityAutocomplete(query, s)` and `reassignOriginal(original, category,
   canonical)`, both of which already exist for the Selected placeholder card,
   then `runFastRerun`. On success:
   `ANONYMISE.selectionBecameVariant(text, main)`. When `reassignOriginal`
   returns false (unknown target, or the target is the text itself) show the
   reason on the panel rather than a toast: the fix is in the field the user is
   looking at.
5. **Mode 2, "Add as a new value"**: the `CATEGORIES` select (the same list
   `missedCard` uses) plus `addEntities([{category, canonical: text, origin:
   "declared"}])`, through CR1f's `foldIntoFamily` so a new value that belongs to
   an existing family becomes a spelling of it instead of a rival. Then
   `runFastRerun`. `addEntities` already switches the category on, which is what
   makes the value actually apply.
6. **Mode 3, "Replace the text"**: today's behaviour exactly, with the
   `[CUSTOM_N]` reservation moved here: request it when the mode is chosen, not
   when text is selected. Keep case-sensitive matching and keep opening the
   "Find and replace" card afterwards (`collapsed.delete("rules")`) so the rule
   the user just created is visible.
7. **Copy** (`copy.js ANONYMISE`):
   ```js
   selectionTitle: "Selected text",
   selectionCopy: "Copy",
   selectionReplace: "Replace",
   selectionModeVariant: "Make it a spelling of an existing value",
   selectionModeValue: "Add it as a new value",
   selectionModeText: "Replace the text only",
   selectionModeVariantHint: "The text is replaced with that value's placeholder, so both spellings share one number.",
   selectionModeValueHint: "The text becomes a value of its own, with its own placeholder.",
   selectionModeTextHint: "A find and replace rule. No value is created and nothing is added to the re-identification key.",
   ```
   The three hints matter: the difference between the modes is *what ends up in
   the re-identification key*, and that is not guessable from the labels.
8. **CSS** (`style.css`): the `.selection-card` grows a stacked body. It is
   positioned against the Compare card, so keep it inside the card's bounds
   (clamp `left` so a selection near the right edge does not push it off screen,
   the same clamp the mark tooltip already does).

### Tests

- `frontend/anonymise.test.js`: `selectionPanel` renders stage 1 with two
  buttons; stage 2 with the three options; each mode's stage 3 with its own
  fields. Assert the mode hints are present (they are the safety-relevant copy).
- `frontend/anonymise.test.js`: no `[CUSTOM_N]` is requested until mode 3 is
  chosen (spy on the api wrapper), which is the reservation-leak regression.
- `frontend/api.test.js`: `copyText` calls `CopyText` on the bridge.
- `backend/app_export_test.go` (new file; the export methods have no test file
  of their own yet): `CopyText` rejects an over-long string with an
  actionable message; accepts a normal one (the Wails runtime call is not
  exercised headless, so assert the guard, not the clipboard).
- `frontend/anonymise.test.js`: mode 1 routes through `reassignOriginal`, mode 2
  through `addEntities`, mode 3 through `addSimpleRule`. Three assertions, one
  per mode, on the store rather than on the DOM.
- Update the existing selection test that asserts "Apply creates a rule
  directly": that path now needs mode 3 chosen first. Do not delete it, retarget
  it (`CLAUDE.md` §6).

---

## CR3 — Search across both Compare panes

### Current behaviour

`compareCard(s, doc)` (`anonymise.js` ~line 630) renders the document selector in
the card head, then two `<pre class="pane-body">`: `#original-pane` holds
`escapeHTML(source.markdown)`, `#anonymised-pane` holds
`renderHighlighted(doc.anonymised, s.mapping, doc.occurrenceVariants)`. There is
no search.

### Change

1. **`frontend/panesearch.js` (new, pure, testable)**. Search must not be done
   with `innerHTML` string replacement over already-rendered HTML: the
   anonymised pane contains `<mark>` elements and escaped entities, and a
   needle like `mark` or `&` would corrupt them. So hits are computed over the
   **plain text** and rendered during the same pass that escapes it.

   ```js
   /** findHits(text, needle) → [{start, end}] over the PLAIN text,
    *  case-insensitive, non-overlapping, left to right. Capped at
    *  MAX_HITS (2000): past that the highlight is unreadable anyway and
    *  the DOM cost is real. Returns [] for an empty or 1-character needle. */
   export function findHits(text, needle)

   /** renderPlainWithHits(text, hits, activeIndex) → escaped HTML with each
    *  hit wrapped in <span class="find-hit">, the active one also
    *  .active. */
   export function renderPlainWithHits(text, hits, activeIndex)
   ```

2. **`highlight.js renderHighlighted` gains an optional fourth argument**
   `search = {hits, activeIndex}`, where hits are offsets **into the same text
   string** it is already walking. It emits hit spans in the plain segments and
   inside a mark's own text, and a hit that straddles a mark boundary is simply
   not highlighted (documented: the alternative is splitting marks, which breaks
   the click-to-select and tooltip contract for the sake of a rare case).
   Existing three-argument callers are unaffected.

3. **View-local search state** in `anonymise.js`:
   ```js
   // The Compare search: a way of LOOKING at the result, not part of it, so
   // it lives here and not in the store. Reset when the compared document
   // changes or a new run lands: offsets belong to one text.
   let search = { needle: "", pane: "original", index: 0 };
   ```
   Hits are recomputed during render from the current needle (both panes), which
   keeps them in step with the text with no cache to invalidate.

4. **Navigation over one combined list.** The two panes have different match
   counts, so a single shared index would drift. The walk is one ordered list:
   every original-pane hit, then every anonymised-pane hit. `>` advances and
   wraps, `<` reverses and wraps, and the readout names the pane of the active
   hit: `ANONYMISE.searchCount(i, total, paneLabel)` =
   `"3 of 8 in Original"`. Crossing the boundary between the panes is therefore
   visible rather than silent.

5. **UI in the card head**, beside the document selector:
   `<input id="compare-search">`, `<` (`chevron_left`) and `>`
   (`chevron_right`) icon buttons, and the readout. Both buttons disabled with a
   title when there are no hits, per the charter's rule that a greyed control is
   never mute. Keyboard: `Enter` next, `Shift+Enter` previous, `Escape` clears
   the needle. The needle survives repaints (module state) and the input keeps
   focus and caret across the repaint the way the values search bar already does
   (`identifyworkspace.js` has the pattern to copy).

6. **Scrolling to the active hit** happens in `wireCompare`, after the paint, and
   **only when the active index changed** since the last paint. `scroll.js`
   restores each pane's previous offset on every repaint; scrolling
   unconditionally would fight it and drag the pane back on every keystroke.
   Guard with a module-level `lastScrolledTo` and use
   `el.scrollIntoView({block: "center"})` on `.find-hit.active`.

7. **Copy** (`copy.js ANONYMISE`):
   ```js
   searchLabel: "Search",
   searchPlaceholder: "Find in both previews",
   searchNext: "Next occurrence",
   searchPrev: "Previous occurrence",
   searchNone: "No match in either preview.",
   searchCount(index, total, pane) { return `${index} of ${total} in ${pane}`; },
   searchCapped(max) { return `Showing the first ${max} matches. Narrow the search to see fewer.`; },
   ```

8. **CSS**: `.find-hit` is a neutral highlight, `.find-hit.active` a stronger
   one, and both must stay legible **on top of** a category `mark` (the
   anonymised pane nests them). Use an outline plus a background tint from the
   brand tokens rather than a hard-coded colour (`brand.css` is the single source
   for colour).

### Tests

- `frontend/panesearch.test.js` (new; `frontend_tests_test.go` requires every
  module to have one): empty and 1-character needles, case-insensitivity,
  overlapping candidates ("aa" in "aaaa" gives 2 non-overlapping hits), the cap,
  and hits at the very start and end of the text.
- `frontend/highlight.test.js`: hits inside plain text; hits inside a mark's
  text; a hit straddling a mark boundary is not highlighted and **the mark's
  attributes survive intact** (this is the regression that would break
  click-to-select); no-search calls render byte-identical HTML to before.
- `frontend/anonymise.test.js`: the search input and both nav buttons render;
  buttons are disabled with a title when there are no hits; the readout wording;
  `Escape` clears; the combined walk crosses from the original pane to the
  anonymised pane and wraps.
- `scripts/uitest/probes.js` + the render harness: a probe that types a needle
  present in both panes and asserts a `.find-hit.active` exists and is
  **visible inside** its pane (the real-rendering layer is the only one that can
  catch a highlight the pane's `overflow` clips, which is exactly how the mark
  tooltip bug was found). `uitest_parity_test.go` keeps both harnesses on the one
  `probes.js`, so add it there and nowhere else.

---

## CR4 — Deleting a variant deletes it, it does not create an exclusion

### Current behaviour

`Entity.ExcludedVariants` (`entities.go` ~line 33) is a per-value list of
spellings the automatic expansion must not produce. `ExpandVariants` filters
against it. Three frontend paths write it:

| Path | Where | Why it writes an exclusion today |
|---|---|---|
| Delete a variant chip | `identifyworkspace.js excludeVariant` ~line 1349 | the expansion would regenerate it |
| Rename a variant | `state.js renameVariant` ~line 1564 | same, for the old spelling |
| Drag a variant to another value | `state.js moveVariant` ~line 1465 | so exactly one value matches it |

It also travels in every run request (`acceptedEntities`) and is persisted in the
session file, which is where the owner saw it.

The reported problem is real and it is not only cosmetic. An exclusion is a
**negative rule with no home in the UI**: it is invisible except as the absence
of a chip, it is not listed anywhere, it cannot be undone except by re-adding the
spelling by hand, and it duplicates the job of the "Never anonymise" tab, which
is the one place negative rules are supposed to live and be visible.

### Change: the shown chips are the whole truth

Replace the exclusion mechanism with **curated variants**. A value's spellings
are automatic until the user edits them; from that moment the list is theirs and
the engine stops re-deriving it.

1. **`engine.Entity`**: delete `ExcludedVariants`. Add

   ```go
   // AutoExpand reports whether the automatic variant expansion still
   // applies. Nil and true both mean yes, which is the state of every value
   // detection or the user creates. It goes false the first time the user
   // edits the spellings: from then on ManualVariants IS the list, and the
   // chips on the card are exactly what will be replaced.
   //
   // A pointer so an absent field reads as "expand", and so the frontend
   // can send the field without inventing a default.
   AutoExpand *bool `json:"autoExpand,omitempty"`
   ```

2. **`ExpandVariants(e)`**: when `AutoExpand` is false, return canonical plus
   `ManualVariants` (deduplicated, longest first, `minVariantLen` still applied
   to keep a 2-character typo from shredding text) and expand nothing. Delete the
   exclusion filter.

3. **`state.js`**: one new helper, three rewritten reducers.
   ```js
   /** curate(e, variants) freezes a value's spellings: the list becomes
    *  exactly `variants`, and the automatic expansion stops applying. It is
    *  what makes a deletion stick without a negative rule: the deleted
    *  spelling is simply not in the list any more. */
   ```
   - **Delete a variant** (`identifyworkspace.js excludeVariant`, renamed
     `deleteVariant`): `curate(e, currentSpellings.filter(not the deleted one))`.
     No exclusion, no re-expansion, no bridge call.
   - **`renameVariant`**: curate with the old spelling swapped for the new one.
   - **`moveVariant`**: the source curates without the moved spelling; the target
     gains it as a manual variant (unchanged) and, if the target is already
     curated, keeps its curated list plus the new spelling.
   - **`groupEntities`**: the survivor's `manualVariants` already absorb the
     folded spellings; drop the un-exclude step. If any participant was curated,
     the survivor is curated (the user's explicit list must not be silently
     re-derived by a merge).
   - **`spellingsOf(e)`**, which mirrors `ExpandVariants` for the conflict
     highlight, loses its exclusion filter and gains the curated branch.
   - **`acceptedEntities`** and `buildRunRequest` send `autoExpand` instead of
     `excludedVariants`.
4. **`refreshVariants`** (`identifyworkspace.js`) skips curated rows: their list
   is settled by definition. `pendingExpansions` gets the same condition, so a
   curated row is never re-expanded and never shows the pending placeholder.
5. **The nudge the CR asks for.** After a delete, the toast offers the tab that
   *is* for negative rules:
   `WORKSPACE.variantDeleted(v)` = `"Removed the spelling \"<v>\" from this value. To stop it being replaced by anything at all, add it to Never anonymise."`
   No new mechanism, one sentence that points at the existing one.
6. **`SessionVersion` 5 → 6** (`session.go`), with the reason recorded beside the
   constant in the existing style: the entity shape lost `excludedVariants` and
   gained `autoExpand` and `origin` (CR1b), and a version 5 file's exclusions
   have no meaning under the curated model. Per `CLAUDE.md` §5 a file of another
   version is **refused with an actionable message, never migrated**: bump once
   for this whole change order, not once per CR.
7. **`BRIDGE.md`**: update the `expandVariants(entity)` row and the run-request
   shape.
8. **A guard against the field coming back.** Add to `copy_guard_test.go`'s
   neighbourhood (or a small `session_shape_test.go`, package `main`): marshal an
   `Entity` and assert the JSON contains no `excludedVariants` key, and grep
   `frontend/` for `excludedVariants` and fail on a hit. This is the mechanical
   version of "we decided not to have negative rules on a value".

### Tests

- `backend/engine/entities_test.go`: **delete** `TestExcludedVariants`-style
  assertions (the contract is retired) and add: a curated entity expands to
  exactly its list; an uncurated one expands as today; `minVariantLen` still
  applies to a curated list.
- `backend/app_entities_test.go`: retarget `TestExcludedVariants` to
  `TestCuratedVariants` on `ExpandEntityVariants`.
- `backend/engine/session_test.go`: a version 5 file is refused with the
  actionable message; a round-trip of a curated entity keeps `autoExpand: false`
  and writes no `excludedVariants`.
- `frontend/state.test.js`: update the three tests that currently assert
  `excludedVariants` contents (lines ~343, ~554, ~1627, ~1700) to assert the
  curated list instead. Add: deleting a variant then triggering a refresh does
  **not** bring it back (the regression the exclusion existed to prevent, now
  covered by curation).
- `frontend/entitymodel.test.js`: same retarget at line ~168.
- `frontend/identifyworkspace.test.js`: the delete toast wording; a curated row
  shows no pending placeholder.

---

## Decisions taken

Recorded here because each one is a judgement call the implementation should not
re-open on its own:

1. **A value the user typed ranks with custom patterns (rank 2).** CR1's order
   names four routes and does not say where a hand-typed value sits. Custom
   patterns and typed values are the same act (the user declaring something), so
   they share rank 2 as `OriginDeclared`, and native detection still wins over
   both, exactly as pass 1 already beats pass 2 today.
2. **Intersections warn, they never block.** The precedence rule always has an
   answer, so refusing the run would punish the user for a configuration the
   engine can resolve. The three existing blocking conflicts (ambiguity,
   collision, allowlist) keep blocking; the step 2 to 3 gate is untouched.
3. **The Compare search walks one combined list** (original hits, then
   anonymised hits) with the pane named in the readout, rather than two cursors
   or one shared index over lists of different lengths.
4. **`excludedVariants` is replaced, not merely hidden.** Removing it from the
   file means the suppression has to reach Go some other way, and the curated
   list is that way. Keeping the field and hiding it would leave the same
   invisible negative rule in the session file, which is what the CR objects to.
5. **One session version bump (5 to 6) for the whole change order.**
6. **Origin is displayed on the value card.** A precedence rule the user cannot
   see the inputs of is indistinguishable from randomness, which is how the
   current behaviour came to be reported.

---

## Conflict analysis

### Files touched by more than one CR

| File | CRs | Notes |
|---|---|---|
| `backend/engine/entities.go` | CR1a, CR1b, CR1f, CR4 | `Entity` changes shape twice (gains `Origin`, loses `ExcludedVariants`, gains `AutoExpand`) and `ExpandVariants` is rewritten by CR4. Do CR4 first so CR1 edits the final struct. |
| `backend/engine/pii.go` | CR1a, CR1c | `Span.Origin` and the comparator, then the detect/apply split. Same region. |
| `backend/engine/pipeline.go` | CR1b, CR1c | `acceptProposals` (small) and the three-phase `Run` (large). |
| `backend/engine/session.go` | CR1b, CR4 | One version bump, two reasons. Write both beside the constant. |
| `frontend/state.js` | CR1b, CR1d, CR1f, CR2, CR4 | `addEntities`, `acceptedEntities`, `buildRunRequest`, the variant reducers and `spellingsOf` are all in play. The heaviest overlap in the change order. |
| `frontend/views/identifyworkspace.js` | CR1b, CR1d, CR1e, CR4 | `valueCard` (origin chip + intersection note), the variant delete path, the debounced recheck. |
| `frontend/views/anonymise.js` | CR2, CR3 | Both live in `compareCard` and its wiring: CR3 in the card head and the panes, CR2 in `selectionPanel` and `wireSelectionPanel`. Adjacent, not overlapping. |
| `frontend/highlight.js` | CR3 | Only CR3, but every render test reads it. |
| `frontend/copy.js` | CR1e, CR1f, CR2, CR3, CR4 | Additive, distinct keys. Expect merge-adjacent edits. |
| `frontend/api.js` + `BRIDGE.md` | CR1d (`checkIntersections`), CR2 (`copyText`), CR4 (`expandVariants` shape) | Different rows; update `BRIDGE.md` in the same commit as each wrapper, never later. |
| `frontend/style.css` | CR1e, CR2, CR3 | `.intersects`, `.selection-card`, `.find-hit`. Independent blocks. |

### Hotspots

- **`state.js` (CR4 → CR1b → CR1f → CR1d → CR2).** Land the variant model
  cleanup first, then provenance, then folding (which needs both), then the
  intersection store, then CR2 (which consumes `addEntities` and
  `foldIntoFamily` as they finally are).
- **`entities.go` (CR4 → CR1a/CR1b).** `ExpandVariants` is rewritten by CR4;
  doing CR1 first means editing it twice.
- **`pipeline.go` (CR1a → CR1b → CR1c).** Comparator, then the origin stamp,
  then the phase split, so each step runs against a suite that already agrees
  about precedence.
- **`anonymise.js` (CR3 → CR2).** CR3 reshapes the card head and the pane
  rendering; CR2 reshapes the floating panel. Doing CR3 first means CR2's panel
  is written against the final `compareCard`.

## Recommended order

1. **CR4** — curated variants, `AutoExpand`, session bump to 6, guard against
   `excludedVariants` returning. Backend plus `state.js`, self-contained, and it
   settles the `Entity` shape everything else edits.
2. **CR1a** — `Origin`, `OriginRank`, the four-step comparator. Engine only, and
   the parity guard lands with it.
3. **CR1b** — provenance survives the accept: `Entity.Origin`, the accept paths,
   the origin chip, the origin parity guard.
4. **CR1c** — the three-phase `Run` and `unifyOwnership`. The largest single
   piece; it is also the one that makes the CR's rule true globally rather than
   per document.
5. **CR1d** — `DetectIntersections`, `App.CheckIntersections`, the store and the
   debounced recheck.
6. **CR1e** — the warning on the value card, with its copy.
7. **CR1f** — `FoldValueFamilies` in the engine, folding once over the merged
   detection output, plus `foldIntoFamily` for the one-at-a-time paths.
8. **CR3** — `panesearch.js`, the `renderHighlighted` search argument, the card
   head controls, the uitest probe.
9. **CR2** — the two-stage selection panel, `App.CopyText`, the three replace
   modes, and the reservation-leak fix.

Rationale: CR4 first because it settles a struct four other steps edit. CR1 then
runs bottom-up, engine before bridge before view, so no view is written against a
precedence rule that is still moving. CR3 precedes CR2 because both live in the
Compare card and CR3 owns its head and panes. CR2 last because its mode 2 wants
CR1f's folding to already exist.

## Cross-cutting reminders

- Bump `SessionVersion` **once** (in CR4) and record both reasons beside the
  constant.
- Update `CLAUDE.md` §5 in the same change order: the superseding order and the
  origin model, the shorter-form-is-the-main-value rule, the curated-variant
  model in place of the removal note's exclusion wording, and
  `SessionVersion` **6**. `CLAUDE.md` currently says version 4 while the code
  says 5; correct that drift while you are there.
- Update `frontend/BRIDGE.md` with `checkIntersections`, `copyText` and the new
  entity shape, and `frontend/CLAUDE.md` if the Compare card's module map
  changes (a new `panesearch.js` belongs in it).
- Run both suites after **each** numbered step, not once at the end:
  `go test ./...` and `node --test "frontend/**/*.test.js"`, plus the render
  harness for CR2 and CR3 (`docs/UITESTING.md`). Never weaken a guard to pass.

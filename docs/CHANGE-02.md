# CHANGE-02 — Eleven usability and correctness change requests

You are executing a change order against the existing **doc-anonymiser**
repository (pattern P0 — pure Go + Wails v2, no CGo, no npm). This document
holds **one self-contained implementation section per change request (CR1–CR11)**,
followed by a **conflict analysis and recommended execution sequence** (they
touch overlapping files, so order matters).

Ground rules for this change order (unchanged from CLAUDE.md):

- `CLAUDE.md` and the two subtree charters remain authoritative. Nothing here
  weakens the local-only guarantee, the one-file Ollama boundary, or the
  zero-CGo rule.
- **A change is not finished until its tests move with it** (CLAUDE.md §6). Each
  CR below names the tests to add or update. Both suites gate:
  `go test ./...` and `node --test "frontend/**/*.test.js"`.
- User-visible copy never contains em dashes (enforced by `copy_guard_test.go`
  and `frontend/copy.test.js`). Use commas or full stops.
- The parity guards (`category_parity_test.go`, `step_parity_test.go`,
  `copy_guard_test.go`, `uitest_parity_test.go`, `frontend_tests_test.go`) are
  load-bearing. If one fails, fix the inconsistency, not the guard.

Decisions confirmed with the owner before writing this plan:

- **CR2:** the new "Native detection" toggle is a **master switch over the regex
  signal categories**. When OFF, no signal category (email, VAT, IBAN, amount…)
  is replaced, regardless of the individual checkboxes.
- **CR1:** after the user clicks **Apply** in "Group with", a **popup** asks
  which participating value becomes the main one.
- **CR7:** the Export step keeps a renamed **"Profile"** section with **only the
  Save button**; the Load button lives **only** in the new Identify "Load
  profile" section.

---

## CR1 — "Group with" must prompt for the main value

### Current behaviour
`frontend/views/identifyworkspace.js`:
- `groupPanel(e, s)` (~line 589) renders the inline picker: for the card's value
  `e`, it lists every *other* value as a checkbox. There is no way to say which
  value survives.
- `wireGroupPanel(cardEl, cat, canonical)` (~line 1018) reads the checked
  sources and calls `groupEntities({category: cat, canonical}, sources)` — the
  **card's own value is always the target** (`keep`).

`frontend/state.js`:
- `groupEntities(target, sources)` (~line 1466) folds every source's spellings
  into `target` and removes the sources. `target` is the survivor.

The in-app modal (`frontend/modal.js` / `state.js askConfirm`) is **yes/no only**.

### Change
Prompt the user, after Apply, to pick the survivor among the *participating*
values (the card value plus the checked sources). Because grouping can involve
more than two values, add a small **choice** capability to the modal rather than
forcing a yes/no.

1. **state.js — add a choice modal** alongside the existing confirm:
   - The pending question lives in `state.confirm`. Extend its shape to accept an
     optional `choices: [{id, label}]`. When `choices` is present, the modal
     renders one button per choice instead of confirm/cancel.
   - Add `askChoice({title, body, choices})` returning `Promise<string|null>`
     (the chosen `id`, or `null` on cancel/Escape). Implement it exactly like
     `askConfirm`/`answerConfirm`, but resolve with the chosen id. Keep
     `answerConfirm(false)` (backdrop/Escape) resolving to `null`.
   - Keep `resetState()` settling any pending choice promise with `null` (mirror
     the existing `confirmResolve` handling), so a reset never hangs an awaiter.

2. **ui.js `modalHTML`** — when the question carries `choices`, render the choice
   buttons (each `data-choice="<id>"`) in place of the confirm/cancel pair. Keep
   a Cancel affordance (backdrop + Escape already answer `null`).

3. **modal.js `wireModal`** — wire `[data-choice]` buttons to
   `answerChoice(btn.dataset.choice)`. Keep backdrop/Escape → cancel.

4. **identifyworkspace.js `wireGroupPanel`** — replace the direct
   `groupEntities` call:
   ```js
   const participants = [{category: cat, canonical}, ...sources];
   const choices = participants.map((p) => ({
     id: entityKey(p.category, p.canonical),
     label: `${p.canonical} (${categoryLabel(p.category)})`,
   }));
   const mainKey = await askChoice({
     title: WORKSPACE.groupMainTitle,
     body: WORKSPACE.groupMainBody,
     choices,
   });
   if (!mainKey) { setState({}); return; }          // cancelled: no-op
   const main = participants.find((p) => entityKey(p.category, p.canonical) === mainKey);
   const rest = participants.filter((p) => entityKey(p.category, p.canonical) !== mainKey);
   const n = groupEntities(main, rest);
   ```
   `groupEntities` already handles an arbitrary target + sources, so no reducer
   change is required — only the *caller* now chooses the target.

5. **copy.js** — add `WORKSPACE.groupMainTitle` (e.g. "Choose the main value")
   and `WORKSPACE.groupMainBody` (e.g. "The other values become spellings of the
   one you pick. Its placeholder is the one that will appear in the output.").
   No em dashes.

### Tests
- `frontend/state.test.js`: add a test for `askChoice` resolving to the chosen id
  and to `null` on `answerConfirm(false)`; confirm `resetState` settles a pending
  choice with `null`.
- `frontend/identifyworkspace.test.js` (or `identifyvalues.test.js`): grouping
  three values and choosing a *source* as main leaves that value as the survivor
  with the others folded in as variants.
- `frontend/ui.test.js`: `modalHTML` renders one button per choice.

---

## CR2 — Split "Smart detection" into Native and Auto master toggles

### Current architecture (verified)
- `settings.useSmartDetect` (state.js, default `true`) is the **only** switch for
  the Smart detection route. In `backend/app_detect.go RunDetection`, it gates
  `PhaseSmart` → `runSmartPhase` → `engine.SmartDetectContext`, i.e. the offline
  **word-frequency ("auto") pass** that produces name candidates.
- The regex **"signals"** (email, url, iban, vat, matricule, phone, credit_card,
  …, amount, date) are **not** part of detection at all. They are applied at
  **anonymisation time** (pass 1), driven purely by `settings.categories`
  (`REGEX_GROUPS` in `identifyrail.js` = `CATEGORY_GROUPS.slice(0,3)`).
- The rail renders the Smart detection section via `smartSection(s)` →
  `scopeBlocks(s)` (identifyrail.js ~line 234). The section header switch is
  bound to `useSmartDetect` via `RAIL_SECTIONS`.

### Confirmed decision
"Native detection" is a **master switch over the regex signal categories**: OFF ⇒
no signal category is replaced, regardless of the per-category checkboxes. "Auto
detection" is the existing word-frequency pass.

### Change

1. **state.js `settings`** — add two flags, both default `true`:
   ```js
   useNativeDetect: true,   // master over the regex signal categories (pass 1)
   useAutoDetect: true,     // the offline word-frequency pass
   ```
   Deprecate `useSmartDetect` by making the **section** presence = `useNativeDetect || useAutoDetect`. To minimise churn, keep `useSmartDetect` as a *derived* value written on every settings push (`useSmartDetect = useAutoDetect`) so the existing session field and the AI/route wiring keep working; document that `useSmartDetect` now tracks `useAutoDetect`. Add setters `setUseNativeDetect(on)` and `setUseAutoDetect(on)`.

2. **identifyrail.js `smartSection(s)`** — render two toggles **at the very top**
   of the section body, before `scopeBlocks(s)`. Reuse the existing switch markup
   used by the section headers (search `RAIL_SECTIONS` rendering / the toggle
   component) so styling matches. Labels from copy: "Native detection (signals)"
   and "Auto detection (word frequency)". Each with a one-line `hint`.

3. **identifyrail.js `scopeBlocks` / `categoryGroups`** — when
   `!s.settings.useNativeDetect`, render the `REGEX_GROUPS` block **disabled**
   (greyed, like a country-inapplicable category) so the user sees the scope but
   cannot edit it while the master is off. The name-category block
   (`ENTITY_GROUPS`) is governed by `useAutoDetect` the same way.

4. **identifyrail.js `wireScope` / new `wireSmartToggles`** — wire the two
   toggles to `setUseNativeDetect` / `setUseAutoDetect` then `pushSettings`.

5. **pushSettings (identifyrail.js ~line 679)** — include `useNativeDetect` and
   `useAutoDetect` in the settings object pushed to Go; set
   `useSmartDetect: !!useAutoDetect`.

6. **Backend — `backend/app_detect.go RunDetection`:** gate `PhaseSmart` on
   `settings.UseAutoDetect` (add the field to the settings struct). The AI route
   is unchanged.

7. **Backend — the anonymisation pipeline (native master):** `useNativeDetect`
   must actually stop the regex signal categories. The cleanest, contract-safe
   way is to add a boolean to the run request / `PipelineInput` (e.g.
   `SuppressRegexPII bool`) and, when true, skip the regex PII pass (`pii.go`
   detectors) in `pipeline.go`. Trace `RunPipeline` in `backend/app_run.go` →
   `engine.Run` → `pipeline.go`. Do **not** mutate `categories`: the checkboxes
   remember the user's selection so toggling native back on restores it.
   - Frontend: `state.js buildRunRequest` adds
     `suppressRegexPII: !s.settings.useNativeDetect`.
   - Add the mirrored field to `PipelineInput` and honour it in `pipeline.go`.

8. **Settings struct (Go)** — add `UseNativeDetect` and `UseAutoDetect`
   (find the `Settings` struct in `backend/app.go` or `app_run.go`; it already
   carries `UseSmartDetect`, `UseAI`, etc.). Keep `UseSmartDetect` populated from
   `UseAutoDetect` for backward field compatibility, or retire it if no reader
   remains (grep first).

9. **export.js `applySession`** — restore both new flags (absent ⇒ `true`, like
   `useSmartDetect` today). **session.go `SessionVersion`** must bump (currently
   4 → 5) because the settings shape changed; record the reason beside the
   constant (CLAUDE.md §5: files are refused, never migrated).

10. **BRIDGE.md** — update the `applySettings` / `RunPipeline` / `RunDetection`
    payload descriptions to list the new fields.

### Tests
- `backend/engine/pipeline_test.go`: with `SuppressRegexPII: true`, an email/VAT
  in the text is **not** replaced even at `LevelAdvanced`; with it false, it is.
- `backend/app_detect_test.go`: `UseAutoDetect: false` runs no smart phase (the
  smart candidates are empty) while the AI phase still runs when enabled.
- `frontend/state.test.js`: `setUseNativeDetect` / `setUseAutoDetect` flip the
  flags; `buildRunRequest` carries `suppressRegexPII`.
- `frontend/identifyrail.test.js`: both toggles render at the top of Smart
  detection; turning Native off disables the signal category block.
- `frontend/export.test.js`: `applySession` restores the flags (absent ⇒ true).

---

## CR3 — Local AI scope: entire document, single page, range, or specific pages

### Current behaviour
- `state.js aiScope = {docName, fromPage, toPage}` (~line 134): one document and a
  single contiguous range.
- `identifyrail.js scopeBlock(s)` renders a document `<select>` plus **From/To**
  numeric spinners (shown only when a single multi-unit document is selected).
- `api.js runDetection(fileNames, allowTerms, aiScope)` passes it to Go.
- `backend/app_detect.go` `AIScope{DocName, FromPage, ToPage}`; `runAIPhase`
  calls `engine.Document.PageRangeMarkdown(from, to)`
  (`backend/engine/pagescope.go`) for one contiguous range.

### Change
Support **entire document**, a **single page**, a **contiguous range**, and a
**discontiguous set** (e.g. `12,13,18,19`, and mixed `1-3,7`).

1. **state.js** — replace the range fields with:
   ```js
   aiScope: { docName: "", mode: "all", pages: "" },
   // mode: "all" (entire selected document) | "pages" (parse the pages string)
   ```
   Add a pure parser `parsePageSpec(spec, maxPage)` that turns `"12-15,18,20"`
   into a sorted, de-duplicated `number[]` (1-based), ignoring out-of-range and
   malformed tokens, and returns `{pages, error}` where `error` names the first
   bad token so the UI can show it. Export it for tests.
   Add `aiScopeArg(s)` (update the existing helper) to emit the backend shape
   (see step 4). `docName === ""` still means "every document, whole".

2. **identifyrail.js `scopeBlock`** — after the document `<select>`, when a single
   document is selected render:
   - a radio/segmented control: **Entire document** vs **Specific pages**;
   - when "Specific pages": a single free-text input (`id="ai-pages"`,
     placeholder like `14, 12-15, 18-20`) plus a live read-out of how many
     units the current spec resolves to (reuse the existing unit word:
     page/slide/row/line from the document).
   Remove the From/To spinners. Keep the existing Ollama/AI gating.

3. **identifyrail.js wiring** — wire the radio and the text input to
   `setAiScope(...)` (state reducer) + repaint; validate live with
   `parsePageSpec` and surface the error inline.

4. **api.js + backend `AIScope`** — extend the wire contract to carry an explicit
   page set. Add `Pages []int` (1-based) to `AIScope`; keep `FromPage/ToPage`
   only if some other caller needs them (grep — otherwise drop them). When
   `Pages` is non-empty it defines the scan; when empty with an active `DocName`
   it means the whole document.
   - Frontend emits `{docName, pages: number[]}` (empty array = whole document).

5. **backend/engine/pagescope.go** — add `PagesMarkdown(pages []int) (string,
   error)` that concatenates the requested units (reusing the same
   unit-slicing logic as `PageRangeMarkdown`), validating each index against
   `PageCount()` with the existing actionable out-of-range message.
   `runAIPhase` (app_detect.go) calls `PagesMarkdown` when `Pages` is set, else
   the whole-document path.

6. **BRIDGE.md** — update the `runDetection` `AIScope` description to the new
   `{docName, pages}` shape (1-based, empty pages = whole selected document).

### Tests
- `frontend/state.test.js`: `parsePageSpec("12-15,18", 20)` → `[12,13,14,15,18]`;
  malformed and out-of-range tokens are reported/ignored; `aiScopeArg` shape.
- `backend/engine/pagescope_test.go`: `PagesMarkdown([]int{1,3})` returns those
  units concatenated; an out-of-range index returns the actionable error.
- `frontend/identifyrail.test.js`: the "Entire document / Specific pages" control
  renders and the text input drives the read-out.

---

## CR4 — Amount "25.150 €" not replaced (non-breaking spaces)

### Root cause (verified empirically)
`backend/engine/pii.go` `CatAmount` regex (~line 214):
```
(?:€|EUR|USD|GBP|CHF|\$|£)\s?[0-9]{1,3}(?:[.,' ][0-9]{3})*(?:[.,][0-9]{1,2})?|\b[0-9]{1,3}(?:[.,' ][0-9]{3})*(?:[.,][0-9]{1,2})?\s?(?:€|EUR|USD|GBP|CHF|\$|£)
```
Go's `\s` and the literal space in the `[.,' ]` separator class match **ASCII
space only**. A test run confirms:
- `"25.150 €"` with an ASCII space → **matches**.
- `"25.150\u00a0€"` (NO-BREAK SPACE) → **no match**.
- `"25.150\u202f€"` (NARROW NO-BREAK SPACE) → **no match**.

European/French documents routinely use U+00A0 / U+202F / U+2009 both as the
thousands separator and before the currency symbol, so the reported value almost
certainly used one of them.

### Change
Enrich the regex to treat Unicode spaces as whitespace **and** as thousands
separators. Define a small character class and reuse it:
- whitespace before/after the symbol: replace `\s?` with `[\s\x{00a0}\x{202f}\x{2009}]?`.
- thousands separator group: replace `[.,' ]` with `[.,'\x{00a0}\x{202f}\x{2009} ]`.

Keep the existing structure (prefix-currency and suffix-currency alternatives).
Add the CR's example to the header comment. While here, per the CR ("if the
pattern is short, enrich it"), also accept an optional magnitude suffix `k`/`M`
(case-insensitive) directly after the number, e.g. `1,5k €` / `€2.3M`, guarded so
a bare `2M` without a currency marker still does **not** match (the currency
marker requirement is what keeps amounts out of ordinary prose).

Compile the regex once at package init as today. Document what it now matches and
still deliberately does not (bare numbers).

### Tests
`backend/engine/pii_test.go` — add table cases (verify the currency character is
written as the actual `€`, and the spaces as explicit escapes in the fixture):
- `"25.150\u00a0€"` → `"25.150\u00a0€"` at `LevelAdvanced`.
- `"1.250,50 €"` (ASCII) still matches.
- `"25.150 €"` (ASCII) still matches.
- `"1,5k €"` matches (if the k/M enrichment is implemented).
- `"about 1500 items"` still does **not** match (no currency marker).

---

## CR5 — "My values" search input loses focus after each keystroke

### Root cause
`frontend/views/identifyworkspace.js`:
- `valuesFilterBar(s)` (~line 430) renders `<input id="values-search">` inside the
  workspace HTML.
- `wireValuesToolbar` (~line 984): the `input` handler calls `setState({})`, which
  triggers a **full `innerHTML` re-render** of the workspace (via the subscriber
  in `main.js`/`identify.js`). The handler then tries to re-focus and restore the
  caret, but the element it focuses is a *different* DOM node created by the
  repaint, and focus is lost between destroy and re-wire.

### Change
Do **not** call `setState({})` on every keystroke. The filter text is *view
state* (a module-level `valuesFilter`), so the input can filter the already-
rendered rows **without a repaint**:

1. Keep `valuesFilter` as the source of truth for the current search.
2. In the `input` handler, update `valuesFilter.search` and then **imperatively
   show/hide the value rows** already in the DOM (toggle a `hidden` class based on
   whether each row's text matches), instead of re-rendering. The input node is
   never replaced, so focus and caret are preserved with no restore hack.
3. Only re-render on structural changes (a value added/removed/grouped), which
   already happen through their own reducers.
4. Apply the **same fix** to the Suggestions tab search (`workspace-search`,
   ~line 719) if it shares the pattern, so the two behave identically and a
   future reader does not "fix" one and leave the other. (Confirm during
   implementation.)

If an imperative filter is judged too divergent from the codebase's render-from-
state discipline, the acceptable alternative is to **debounce** the `setState`
(≈150 ms) so a burst of keystrokes causes at most one repaint, and keep the
caret-restore — but the no-repaint approach is preferred because it removes the
focus race entirely rather than narrowing it.

### Tests
- `frontend/identifyworkspace.test.js` (jsdom): typing into `#values-search`
  filters the visible rows and the input retains focus / caret across multiple
  characters (assert `document.activeElement` stays the input and value grows to
  the full typed string).

---

## CR6 — "Never anonymise" has no default terms + a Clear all button

### Root cause / current behaviour
- The Go anonymisation-time allowlist does **not** seed defaults independently:
  `App.allowlistFor` (`backend/app.go` ~line 185) uses
  `engine.NewEmptyAllowlist()` and adds only the terms the frontend sends. So the
  defaults are purely a **frontend seeding** concern.
- The defaults appear because `frontend/main.js` (~line 97) and
  `frontend/views/import.js` (~line 273) call `defaultAllowlist()` and
  `setState({allowlist: terms})` on a fresh state.
- `frontend/views/allowlist.js` renders the chips; there is **no Clear all**
  button. `state.js` has `addAllowTerm` / `removeAllowTerm` but no bulk clear.

### Change
1. **Stop seeding defaults.** Remove the `defaultAllowlist()` seeding blocks in
   `main.js` and `import.js` (the whole `.then(...) setState({allowlist})`
   chains). The list starts empty and stays empty until the user adds terms.
   - The `defaultAllowlist()` API and the Go `DefaultAllowlist()` binding become
     unused for seeding. Keep the **template download** working: if
     `allow-template` uses `DefaultAllowlistTerms`, leave that path intact (a
     suggested-terms template the user can import is fine); only the automatic
     pre-population is removed. Grep `defaultAllowlist` to confirm no other
     reader breaks, and update `BRIDGE.md` line 90 (the "shown at startup" note)
     to reflect that the terms are no longer auto-added.
2. **Add a Clear all button.** In `allowlist.js renderAllowlistChips`, add a
   `button(ALLOWLIST.clearAll, {id: "allow-clear", kind: "ghost", icon: "delete"})`
   next to Add/Import/Template. Only enable it when `s.allowlist.length > 0`.
3. **state.js** — add `clearAllowlist()` reducer (`setState({allowlist: []})`,
   return the count cleared).
4. **allowlist.js `wireAllowlistChips`** — wire `#allow-clear` to confirm
   (`askConfirm`) then `clearAllowlist()` and a notice.
5. **copy.js** — add `ALLOWLIST.clearAll` ("Clear all") and a confirm body.

### Tests
- `frontend/state.test.js`: `clearAllowlist` empties the list and returns the
  count.
- `frontend/allowlist.test.js` (or `notices.test.js`): the Clear all button
  appears only with terms present and clears after confirm.
- Update any test that asserted the startup list is pre-seeded (grep
  `defaultAllowlist` / `CSSF` in `frontend/*.test.js`) — CLAUDE.md §6: the tests
  move with the behaviour.

---

## CR7 — Move Load to Identify; rename Export "Session" to "Profile"

### Current behaviour
`frontend/views/export.js`:
- `sessionCard()` (~line 134) renders **Save** (`#ses-save`) and **Load**
  (`#ses-load`) under `EXPORT.sessionTitle` ("Session"); `collapsed` set includes
  `"session"` (~line 48).
- `wireSession` (~line 429) wires Load → `loadSession()` + `applySession`.
  Save is wired in another `wire*` (~line 409) → `saveSession(buildRunRequest(false))`.

`frontend/views/identifyrail.js`:
- `RAIL_SECTIONS` (~line 70) lists Smart, Local AI, Cloud AI. Cloud AI is last.

### Confirmed decision
Export keeps a **"Profile"** section with **only Save**. Load lives **only** in a
new Identify "Load profile" section that also offers Save (Save enabled only once
detection has run).

### Change

1. **Detection-ran signal (state.js).** Add `detectionRan: false` to
   `initialState`; set it `true` in the `detection:done` event handler (find where
   `discovery`/`candidates` are set from the detection result — likely `main.js`
   or `api.js onEvent`). Reset it in the Identify/Import step reset table if one
   exists. This gates the Save button.

2. **export.js — rename and trim.**
   - Rename `EXPORT.sessionTitle`/`sessionSummary`/`sessionHint` copy to a
     "Profile" wording (keep the key names or rename to `profile*`; if you rename
     keys, update all references and any `export.test.js`).
   - Remove the **Load** button (`#ses-load`) and its `wireSession` handler from
     Export. Keep **Save** (`#ses-save`).
   - Keep the fold id; if you rename it, update the `collapsed` set.

3. **identifyrail.js — new "Load profile" section after Cloud AI.**
   - Add a section (e.g. `rail-profile`) rendered **after** the Cloud AI section.
     It contains a Load button and a Save button.
     - Load → `loadSession()` + `applySession` (move the logic; `applySession`
       currently lives in export.js — relocate it to a shared module or import it,
       so both are not duplicated. Simplest: move `applySession` into
       `frontend/state.js` or a small `session.js` and import from both places).
     - Save → `saveSession(buildRunRequest(false))`, **disabled** unless
       `s.detectionRan` (title explains why when disabled).
   - Because this section is not an on/off detection route, render it as a plain
     collapsible section, not a `RAIL_SECTIONS` toggle entry (or extend
     `RAIL_SECTIONS` to allow a switch-less section — check how the Cloud AI
     null-switch case is handled and follow it).

4. **copy.js** — add `RAIL.profileTitle` ("Load profile"), button labels, the
   Save-disabled tooltip ("Run detection once before saving a profile."), and
   adjust the Export "Profile" strings. No em dashes.

5. **BRIDGE.md** — no API change (`saveSession`/`loadSession` unchanged); update
   any prose that says the load lives on Export.

### Tests
- `frontend/export.test.js`: the Export section renders as "Profile" with Save
  only, no Load button.
- `frontend/identifyrail.test.js`: the "Load profile" section renders after Cloud
  AI; Save is disabled until `detectionRan` is true; Load is present.
- `frontend/state.test.js`: `detectionRan` flips on the detection-done path.

---

## CR8 — Remove "Deep scan (AI)" from step 3 Anonymise

### Current footprint (verified)
- `frontend/copy.js`: `ANONYMISE.deepScan` and `ANONYMISE.deepScanTooltip` (~line
  638), and `reportLLMPass` (~line 726) formatting "AI deep scan: …".
- `frontend/views/anonymise.js`: `runCard` builds a `deepScan` checkbox
  (`#deep-scan`, ~line 156) and prepends it to the body (~line 164). `wireRun`
  reads `#deep-scan` (`const deep = …`, ~line 749) and passes it to
  `buildRunRequest(deep)` (~line 756).
- `frontend/state.js`: `buildRunRequest(useDeepScan, s)` sets `useDeepScan`
  (~line 1932).
- Backend `backend/app_run.go`: honours `useDeepScan`, tracks `deepScanSkipped`,
  writes `results.Report.LLMPass` / warnings; `backend/ollama/client.go`
  `DeepScan(...)` performs the pass.

### Change
Remove the **UI feature and its request flag**. Decide with care whether to also
delete the backend `DeepScan` capability:
- **Frontend (remove):**
  - Delete the `deepScan` checkbox from `runCard` and drop it from the body
    concatenation.
  - In `wireRun`, remove the `deep` read and call `buildRunRequest(false)` (or
    drop the parameter — see below).
  - `buildRunRequest`: remove the `useDeepScan` parameter and the
    `useDeepScan` request field. Update **all** callers (anonymise.js, and
    export.js `saveSession(buildRunRequest(false))`, and any test).
  - Delete `ANONYMISE.deepScan` / `deepScanTooltip` copy and the
    `reportLLMPass` "AI deep scan" wording if it is now unreachable (grep).
- **Backend (recommended: retire the request field, keep or delete the pass):**
  Since the request no longer carries `useDeepScan`, the pass never runs. Remove
  the `useDeepScan` handling in `app_run.go` and the associated
  `deepScanSkipped` messaging so no dead branch remains. `ollama/client.go
  DeepScan` and its test (`TestDeepScanHallucinationFilterAndAllowlist`) may be
  left if other code references them; otherwise delete them **with** their tests
  (CLAUDE.md §6 — do not leave a test asserting a retired contract). Grep
  `DeepScan` / `useDeepScan` / `LLMPass` across `backend/` before deleting.
- **BRIDGE.md**: remove `useDeepScan` from the `RunPipeline` request description.

Note: the **local-AI detection** route (step 2) is unaffected — CR8 removes only
the step-3 deep-scan pass.

### Tests
- Update `frontend/anonymise.test.js` to assert the checkbox is gone and the run
  request has no `useDeepScan`.
- Update `frontend/state.test.js` `buildRunRequest` cases.
- Update/delete backend tests that drove `useDeepScan` / `DeepScan`.

---

## CR9 — Collapse result sections by default; show "Find and replace" only after a run

### Current behaviour
`frontend/views/anonymise.js`:
- `const collapsed = new Set(["missed", "rules", "selected", "removed"])` (~line
  54). "Replaced values" (`values`) and "Report" (`report`) are **not** in the set,
  so they start open.
- Render order (~line 98): `runCard`, optional `selectedCard`, then
  `valuesCard`/`reportCard`/`missedCard` gated on `s.results && !blocked`, then
  **`rulesCard(s)` rendered unconditionally** (~line 103).
- The four result cards use `collapsibleCard`; folds are toggled via `wireGroups`.

### Change
1. Add `"values"` and `"report"` to the initial `collapsed` set so **all four**
   ("Replaced values", "Report", "Something missed?", "Find and replace") start
   collapsed after a run. (`missed` and `rules` are already collapsed.)
2. Gate the "Find and replace" card on having run at least once:
   ```js
   ${s.results ? rulesCard(s) : ""}
   ```
   so it is absent before the first run and appears (collapsed) afterwards.
3. Verify the collapse state is per-session view state (module-level `collapsed`)
   and that re-running does not force them back open. If the reset table clears
   view state on a fresh run, ensure the defaults remain "all collapsed".

### Tests
- `frontend/anonymise.test.js`: before a run, no "Find and replace" card renders;
  after `s.results` is set, all four cards render **collapsed** by default
  (assert the collapsed/`data-open="false"` markup).

---

## CR10 — "Run anonymisation" subtitle becomes a hover tooltip

### Current behaviour
`frontend/views/anonymise.js` `runCard` passes `subtitle: runSubtitle(s)` to
`card()`. `runSubtitle` (~line 196) returns one of `ANONYMISE.subtitleRunning` /
`subtitleBlocked` / `subtitleDone` / `subtitleIdle(n)` (copy.js ~line 640). The
`card()` helper (ui.js ~line 110) renders the subtitle as
`<span class="card-sub">` and the title as a bare `<h2>` (no tooltip support).

### Change
1. **ui.js `card`** — add an optional `opts.titleTooltip`; when present render
   `<h2 title="${escapeHTML(opts.titleTooltip)}">`. This is a safe additive change
   to the shared helper (all other callers omit it).
2. **anonymise.js `runCard`** — pass `titleTooltip: runSubtitle(s)` and **remove**
   `subtitle`. Keep `runSubtitle` (now the tooltip source). The explanatory text
   no longer occupies a line under the heading; it appears on hover over "Run
   anonymisation".
3. Keep all four `runSubtitle` states — the tooltip should still reflect running /
   blocked / done / idle.

### Tests
- `frontend/anonymise.test.js`: the run card has no `.card-sub`; the `<h2>` carries
  a `title` equal to the expected `runSubtitle` for the current state.
- `frontend/ui.test.js`: `card({titleTooltip})` renders `<h2 title="...">`.

---

## CR11 — "Auto detected values" select-all / deselect-all does nothing useful

### Root cause (verified)
`frontend/views/identifyrail.js` `categoryGroups` (~line 326) builds the bulk
buttons with `data: { groupType: type, group: String(index), on: "1" }`. The
`button()` helper (`ui.js` ~line 60) emits `data-${key}` **verbatim**, producing
`data-groupType="entity"`. The browser lowercases attribute names to
`data-grouptype`, so `btn.dataset.groupType` in `wireScope` (~line 423) is
**`undefined`** and `type` falls back to `"regex"`. Consequently the entity-group
bulk buttons operate on `REGEX_GROUPS[index]` (Contact / Technical) instead of the
name categories — i.e. "Auto detected values" select-all silently toggles the
wrong group. (The regex-group buttons work by luck, because their fallback *is*
`"regex"`.)

### Change
Use a hyphenated data key so `dataset` reads it back correctly. In
`categoryGroups`:
```js
data: { "group-type": type, group: String(index), on: on ? "1" : "0" }
```
and in `wireScope` read `btn.dataset.groupType` (now populated from
`data-group-type`). No other logic changes — the index lookup is already correct
once `type` is right.

Alternatively (equivalent): keep `groupType` but read `btn.dataset.grouptype`.
The hyphenated key is preferred because `dataset.groupType` is the conventional,
self-documenting form.

### Tests
- Add a **DOM/jsdom** test in `frontend/identifyrail.test.js` that renders the
  rail, clicks the "Auto detected values" select-all button, and asserts the
  **name categories** (`entity_names`, `person_names`, …) become selected and the
  Contact/Technical categories are untouched — this is the regression the current
  unit tests miss because they call `setCategoryGroup` directly rather than
  through the rendered `data-*` attribute.

---

## Conflict analysis and recommended execution sequence

### Files touched by more than one CR

| File | CRs | Notes |
|---|---|---|
| `frontend/views/identifyrail.js` | CR2, CR3, CR7, CR11 | Heaviest overlap. CR2 & CR11 both edit `wireScope`/`categoryGroups`; CR2 & CR3 edit the Smart/Local-AI sections; CR7 adds a new section. |
| `frontend/views/identifyworkspace.js` | CR1, CR5 | Different functions (`wireGroupPanel` vs `valuesFilterBar`/`wireValuesToolbar`). Low risk. |
| `frontend/views/anonymise.js` | CR8, CR9, CR10 | CR8 & CR10 both edit `runCard`; CR9 edits the `collapsed` set and the render list. |
| `frontend/views/export.js` | CR7, CR8 | CR7 trims the Session/Profile card; CR8 changes `buildRunRequest` used by `saveSession`. |
| `frontend/state.js` | CR1, CR2, CR3, CR6, CR7, CR8 | Different reducers/areas, but `buildRunRequest` is edited by CR2 (add `suppressRegexPII`), CR3 (aiScope), CR8 (drop `useDeepScan`). Sequence these. |
| `frontend/copy.js` | CR1, CR2, CR3, CR6, CR7, CR8, CR10 | Additive/removal of distinct keys. Low risk; expect merge-adjacent edits. |
| `frontend/ui.js` | CR1 (modal choices), CR10 (`card` titleTooltip) | Different functions. |
| `frontend/main.js` | CR6 (remove seeding), CR7 (`detectionRan` on detection:done) | Different blocks. |
| `frontend/api.js` | CR3 (aiScope shape), CR6 (defaultAllowlist), CR8 (request) | Different functions. |
| `backend/app_detect.go` | CR2 (`UseAutoDetect` gating), CR3 (`AIScope`/`runAIPhase`) | Different regions of the same file. |
| `backend/app_run.go` + pipeline | CR2 (`suppressRegexPII`), CR8 (remove `useDeepScan`) | Both touch the run request struct. Sequence. |
| `backend/engine/session.go` | CR2 (`SessionVersion` bump) | Only CR2 bumps; if any other CR changes the session shape, coordinate a single bump. |

### The two hotspots

- **`identifyrail.js` (CR11 → CR2 → CR3 → CR7).** Do the small, isolated bug fix
  first, then the structural additions, so each later CR builds on a correct
  `wireScope`.
- **`anonymise.js runCard` (CR8 → CR10) and the card list (CR9).** Remove the
  deep-scan control first, then reshape the title/subtitle, then the collapse
  defaults.
- **`buildRunRequest` (CR8 → CR2).** CR8 removes the `useDeepScan` parameter; CR2
  adds `suppressRegexPII`. Do CR8 first so CR2 edits the already-trimmed
  signature.

### Recommended order

Grouped to minimise reopening the same file and to land isolated fixes first:

1. **CR4** — amount regex (backend-only, isolated). Quick, high-value.
2. **CR11** — `data-group-type` fix (isolated `identifyrail.js`/`wireScope`).
3. **CR5** — values search focus (isolated `identifyworkspace.js`).
4. **CR6** — allowlist defaults + Clear all (main.js/import.js/allowlist.js).
5. **CR8** — remove Deep scan (anonymise.js/state.js/backend; trims
   `buildRunRequest`).
6. **CR10** — Run anonymisation tooltip (anonymise.js `runCard`, after CR8).
7. **CR9** — collapse defaults + gate Find and replace (anonymise.js).
8. **CR1** — Group with main-value popup (identifyworkspace.js + modal/ui/state).
9. **CR2** — Native/Auto master toggles (identifyrail.js + state + backend +
   session bump; edits the CR8-trimmed `buildRunRequest`).
10. **CR3** — Local AI page-spec scope (identifyrail.js + state + api + backend).
11. **CR7** — move Load to Identify, rename Export to Profile (identifyrail.js
    last, after CR2/CR3 have finished reshaping the rail; needs `detectionRan`).

Rationale: CR4/CR11/CR5 are isolated and safe to land first. CR8 precedes CR2 so
`buildRunRequest` is trimmed once. All `anonymise.js` work (CR8, CR10, CR9) is
contiguous. All heavy `identifyrail.js` work (CR11, CR2, CR3, CR7) is ordered
bug-fix → toggles → scope → new section, with CR7 last because it depends on the
final rail shape and the new `detectionRan` flag.

### Cross-cutting reminders
- Bump `SessionVersion` **once** (CR2) and record the reason beside the constant;
  if CR3/CR7 end up changing the persisted session shape, fold their reasons into
  the same bump rather than bumping twice.
- Run both suites after each CR: `go test ./...` and
  `node --test "frontend/**/*.test.js"`, plus the parity guards. Never weaken a
  guard to pass.

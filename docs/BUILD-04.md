# BUILD-04.md — UI correctness and feature-surfacing build plan

> **Path note (repo restructure, 2026-07):** This is a historical plan.
> References to `static/...` and to root-level Go files now live under
> `frontend/...` and `backend/...` respectively. The authoritative current
> layout is `CLAUDE.md` §3.

You are executing the fourth build plan for doc-anonymiser. BUILD.md produced
v1. BUILD-02.md delivered the functional-improvements pass. BUILD-03.md added a
Presidio-benchmarked deterministic detection layer (extended recognizers,
confidence scoring, checksum validation, allow-list regex) to `engine/*`. This
document turns a set of 17 owner change requests (CR1 to CR17) into an ordered,
dependency-sequenced set of implementation phases.

It follows the same conventions as BUILD-02.md: each phase has a **Goal**,
**Activities**, **Unit tests**, and a **Definition of done** ending in a named
commit. CLAUDE.md remains authoritative. Where a change request touches a rule
in CLAUDE.md (the "never export a second window", the heading font), Phase 0
amends CLAUDE.md first so no later phase violates it.

Source inputs for this plan:

- The owner change requests CR1 to CR17 (verbatim below in §3).
- A full survey of the current codebase (file and symbol references are
  verified against the code as of commit `0d38942`).
- The brand palette (`docs/brand/color-palette.json`) and BUILD-03.md for the
  backend features that CR9 asks to surface.

---

## 1. Ground rules (in addition to everything in CLAUDE.md)

1. **Render-from-state discipline is preserved.** `api.js` stays the only
   bridge caller, `state.js` the only state holder, views render from state
   and dispatch reducers (CLAUDE.md §4). Every new behaviour lands as a tested
   pure reducer or a tested engine function first, then a view consumes it.
2. **One phase, one commit, CI green.** Do not start a phase until the previous
   phase's commit passes `go vet ./...`, `go test ./...`,
   `node --test static/*.test.js`, and the Wails build in CI.
3. **The local-only guarantee is untouched.** No CDN fonts, no remote icon
   fonts, no new network endpoints. The new documentation window (CR6) loads
   only bundled `go:embed` assets. Helvetica and Arial are Windows system
   fonts and require no font files.
4. **No em dashes in any user-visible string.** Enforced by `copy_guard_test.go`
   and `static/copy.test.js`. All new copy (the new home page body, new
   category labels, new buttons, new tooltips) must pass those guards. Use
   commas, periods, or parentheses. Also banned: "+" as a stand-in for "and",
   and unexplained jargon.
5. **Session compatibility.** Any change to category keys, settings shape, or
   entity/variant shape must load older `.anonsession.json` files via an
   explicit migration covered by a fixture test. CR9 (new categories), CR13
   (smart-detection options) and CR16/CR17 (step + variant state) each carry
   this obligation.
6. **Engine identifiers are stable.** CR3 renames the user-visible word
   "Entities" to "Values" but must NOT rename engine category identifiers
   (`client_names`, `project_names`, `internal_names`, `person_names`,
   `custom_patterns`, and the PII category constants in `engine/pii.go`).
7. **No new Go dependencies** unless unavoidable; if one is needed it goes in
   the pinned-versions tables of this file AND CLAUDE.md §7 before `go get`,
   must be pure Go (no CGo), and must compile under the Go 1.26.x pin. None of
   CR1 to CR17 is expected to need one.

---

## 2. Current-state findings (verified against the code)

These are the concrete anchor points every phase builds on.

| Area | Location | Finding |
|---|---|---|
| Home copy | `static/copy.js` `HOME` | headline/lede/panels; rendered by `static/views/home.js`; single-paragraph lede. (CR1) |
| Heading font | `static/brand.css` line 53 `--font-heading: Georgia,...` | consumed by `h1..h4`, `.brand`, `.step-banner .banner-title` in `static/style.css`; comments in both files assert Georgia. (CR2) |
| Step-3 token | `state.js` `WIZARD_STEPS`, `main.js` `STEP_LABELS`/`VIEWS`, `copy.js` `STEP_BANNERS.entities`, `static/views/entities.js` | token is `entities`, label "3 · Entities". (CR3) |
| Top menu vs steps | `main.js` `paint()` | persistent `.topnav` (Home / Anonymise documents / Documentation) AND the 5 step chips `.steps` are rendered inside the same `.topbar`, so the header changes between screens. (CR4, CR7) |
| Icon alignment | `static/style.css` line 92 `.btn .icon, button .icon { vertical-align: -0.32em; }` | nav icons sit below the text baseline. (CR5) |
| Documentation | `main.js` wires `nav-docs`/`home-docs` to `goToScreen("docs")`; `static/views/docs.js` renders `DOCS_PLACEHOLDER` | in-app placeholder screen, no separate window, no real content. (CR6) |
| Import divider | `static/style.css` `.import-columns { align-items: start }`, `.import-divider { align-self: stretch }` | grid row height collapses to content, so the divider only spans full height once a preview loads. (CR8) |
| New recognizers | `engine/pii.go` constants `CatCreditCard`, `CatNHS`, `CatIPAddress`, `CatMACAddress`, `CatCrypto`, `CatDatabaseURI`, `CatDESteuerID`, `CatESNIF`; `Span.Confidence` | built by BUILD-03 but NOT present in `CATEGORY_LABELS` (copy.js) nor the Configure groups/`state.js` category lists. No confidence control anywhere. (CR9) |
| Configure groups | `static/views/configure.js` `whatTab`/`wireWhatTab`; groups from `HARD_PII_CATEGORIES`, `NAME_CATEGORIES`, `ADVANCED_*` in `state.js` | no select-all/deselect-all. (CR10) |
| Allowlist panel | `static/views/allowlist.js` | no clear-all. (CR11) |
| Scroll reset | every checkbox/allowlist edit calls `setState({})` → full `paint()` → `innerHTML` rewrite | scroll position resets. (CR12) |
| Smart detection | `engine/discover.go` `SmartDetect(text, allow)`; bridge `RunSmartDetection(fileNames, allowTerms, classify)` (api.js) | no tuning options, over-detects. (CR13) |
| Suggestions table | `static/views/entities.js` `candidatesPanel`/`wireCandidates` | plain `<table><tbody>`, no headers, no filters; "Accept all X" at the bottom, no "Deny all". (CR14, CR15) |
| Backward nav | `state.js` `prevStep`/`goTo` | never resets step state; guards only block forward moves. (CR16) |
| Variants | `static/views/entities.js` `categoryPanel`/`wireCategoryPanels`/`refreshVariants`; `static/entitymodel.js`; reducers `addManualVariant`/`moveVariant`/`setEntityVariants` in `state.js`; module-local `expanded` Set keyed by `entityKey` | add works once; second entity variant fails; smart detection blocks after a variant add; variants only appear after a step switch; chips greyed and undraggable. (CR17) |

---

## 3. The change requests (verbatim intent)

- **CR1** New home page copy: title "Anonymise your documents safely" and the
  three-paragraph body (control, predefined patterns vs AI-powered discovery,
  local vs AI endpoint).
- **CR2** Do not use Georgia in the web app; use Helvetica, never Georgia.
- **CR3** Rename step 3 "Entities" to "Values" across the whole application.
- **CR4** The top menu must be static across screens: always Home, Anonymise
  documents, Documentation; it must not change when entering the wizard.
- **CR5** Align the Home/Anonymise/Documentation icons with the text (not
  slightly below the line).
- **CR6** The Documentation link should open a separate window.
- **CR7** The process steps 1-Import to 5-Export must live in a separate
  "Anonymisation workflow" banner under the menu.
- **CR8** Fix the buggy Documents/Preview divider (only spans full height once
  a preview loads).
- **CR9** Surface the BUILD-03 features in the application screens (they were
  built in the backend but never reached the UI).
- **CR10** Add "Select all" and "Deselect all" to the three category sections.
- **CR11** Add "Clear all" to the "Never anonymise these terms" section.
- **CR12** Do not reset the scroll bar each time a box is ticked or a term is
  added.
- **CR13** Smart detection displays too many values; add settings to make it
  more relevant.
- **CR14** In the "Suggestions to review" table, add headers and per-column
  filtering: text search on Value, a list-of-values selector on Name type
  (from the name types selected in Configure), sort asc/desc on Occurrence.
- **CR15** Move the "Accept all X" button to the TOP of the table and add
  "Deny all X".
- **CR16** Robust test procedures for the states between steps; going back to
  the previous step can reset the current step entirely.
- **CR17** The variant drill-down / add-variant feature is buggy (multiple
  symptoms; see §2 and Phase 6).

---

## 4. Open decisions, resolved

### 4.1 CR6 documentation window — RESOLVED
Open the bundled documentation in a **second native Wails v2 window / webview**
serving offline `go:embed` assets. Rationale: the local-only guarantee forbids
remote URLs, and Wails v2 (pinned, no v3 idioms) has no clean multi-window API,
so the window is created from Go with an embedded HTML asset. The in-app
`docs` screen is retired in favour of this window.

### 4.2 CR16 backward-navigation reset — RESOLVED
Going back shows a **confirmation dialog**; only on confirm is the abandoned
step's state reset. Imported documents are NEVER reset by navigation (they are
step 1 data and survive). A plain "keep everything" back is available by
cancelling the dialog.

### 4.3 CR3 internal token — RESOLVED
Rename the internal wizard token `entities` to `values` (view file
`views/entities.js` → `views/values.js`) for clarity, with a session migration
so older sessions that stored `step: "entities"` still load. Engine category
identifiers are untouched (Ground rule 6).

---

## 5. Phases

Ordering: low-risk brand/copy first (Phase 1), then the shell/navigation
layout (Phases 2 to 3), then Configure surfacing and controls (Phase 4), then
the higher-risk Values-step rework (Phases 5 to 6), then a cross-cutting test
and hardening pass (Phase 7). Each phase is independently shippable and CI
green.

---

### Phase 0 — CLAUDE.md amendments and guards

**Goal.** Amend CLAUDE.md so later phases do not violate it, before any code.

**Activities.**
1. §6/typography: record that the web app heading font is Helvetica/Arial, not
   Georgia (Georgia was a PowerPoint-only brand guideline). (CR2)
2. §3/§4: document the second offline documentation window and that it loads
   only embedded assets. (CR6)
3. §5: note the user-visible label for step 3 is "Values" while engine
   category identifiers are unchanged. (CR3)
4. Reserve the plan file `docs/BUILD-04.md` (this document) as authoritative
   for the CR-to-phase mapping.

**Unit tests.** None (documentation only).

**Definition of done.** CLAUDE.md updated; commit
`build-04: phase 0, amend CLAUDE.md for font, docs window, Values label`.

---

### Phase 1 — Brand and copy (CR1, CR2, CR3)

**Goal.** Correct the home copy, the heading font, and the step-3 wording.

**Activities.**
1. **CR1.** Replace in `static/copy.js`:
   - `HOME.headline` = `"Anonymise your documents safely"`.
   - `HOME.lede` (or a new `HOME.body` array) with the three paragraphs
     verbatim from the CR (control; predefined patterns vs AI-powered
     discovery and review; local vs AI endpoint). Keep the panels or fold them
     into the body as the copy dictates; ensure no em dashes.
   - Update `static/views/home.js` to render a multi-paragraph body (map over
     paragraphs into `<p class="home-lede">`).
2. **CR2.** In `static/brand.css` set
   `--font-heading: Helvetica, Arial, sans-serif;` and rewrite the Georgia
   references in the header comment and the usage-rules comment. Rewrite the
   matching comments in `static/style.css` (lines 8, 23) that mention Georgia.
   Grep the whole tree for `Georgia` and remove every remaining reference.
3. **CR3.** Rename step 3 user-visible wording to "Values":
   - `main.js` `STEP_LABELS.entities` → label `"3 · Values"`; rename the token
     to `values` in `WIZARD_STEPS` (`state.js`), `VIEWS`, and `STEP_LABELS`.
   - `copy.js` `STEP_BANNERS`: rename key to `values`, retitle to "Values",
     reword the body to talk about values rather than entities (still em-dash
     free).
   - Rename `static/views/entities.js` → `static/views/values.js`, export
     `renderValues` (keep a thin re-export or update the import in `main.js`).
   - Add a session migration: a stored `step: "entities"` loads as `"values"`.
   - Sweep remaining user-visible "Entit(y|ies)" strings to "Value(s)"
     (banner, headings) WITHOUT touching engine identifiers.

**Unit tests.**
- `copy.test.js`: assert the new headline text, that the body has three
  paragraphs, and the em-dash/jargon guards still pass.
- `copy_guard_test.go`: unchanged guard passes on the new strings.
- `state.test.js`: `WIZARD_STEPS` contains `values`; migration maps a legacy
  `entities` step to `values`; a grep-style assertion (or a small test) that
  no user-visible string equals "Entities".
- A brand test / grep assertion that `Georgia` no longer appears in `static/`.

**Definition of done.** Home shows the new copy in Helvetica; step 3 reads
"Values" everywhere; commit
`build-04: phase 1, new home copy, Helvetica heading font, Values step`.

---

### Phase 2 — Static top menu and workflow banner (CR4, CR5, CR7)

**Goal.** Make the top menu identical on every screen and move the step
progression into its own banner.

**Activities.**
1. **CR4/CR7.** In `main.js paint()`:
   - Keep `.topnav` (Home / Anonymise documents / Documentation) as the only
     interactive content of `.topbar` besides the brand and badges, on ALL
     screens.
   - Remove the `.steps` chips from `.topbar`.
   - On the wizard screen only, render a new section under the menu:
     `<section class="workflow-banner">` titled "Anonymisation workflow"
     containing the 5 step chips (1-Import to 5-Export, with the CR3 rename to
     Values on step 3). Reuse the existing `banner()`/`panel()` toolkit where
     it fits, or add a small dedicated block.
   - Mark the active top-menu item (Home vs wizard vs docs) with a quiet
     non-orange active state so the button SET never changes, only the
     highlight.
2. **CR5.** In `static/style.css`, fix inline icon alignment: replace the
   `.btn .icon, button .icon { vertical-align: -0.32em }` nudge with
   `display: inline-flex; align-items: center; vertical-align: middle;` (and
   verify `.icon svg` sizing) so icons align to the text centre in the top
   menu without dropping below the baseline. Check the primary button, step
   banner icons, and chips do not regress.
3. Add `.workflow-banner` styling in `style.css` (surface, title in the
   heading font, chip row) consistent with the brand (one loud element rule
   untouched: chips stay quiet, active chip keeps the orange underline).

**Unit tests.**
- `ui.test.js`: the button/icon markup helper emits an icon span that aligns
  (assert the class/inline-flex contract, since layout itself is visual).
- A shell/markup test (extend an existing DOM-level test or add one) that:
  the top navigation contains exactly Home, Anonymise documents, Documentation
  on the home, wizard, and docs screens; the step chips render inside
  `.workflow-banner` (not `.topbar`) and only on the wizard screen.

**Definition of done.** Menu is identical across screens; steps live in the
workflow banner; icons align; commit
`build-04: phase 2, static top menu, workflow banner, icon alignment`.

---

### Phase 3 — Documentation window and import divider (CR6, CR8)

**Goal.** Open documentation in a separate offline window and fix the import
divider height.

**Activities.**
1. **CR6.**
   - Add bundled documentation content as embedded static assets (e.g.
     `static/docs/index.html` plus any needed CSS, all `go:embed`-ed). Write
     real user documentation (what the app does, the 5 steps, local vs AI,
     the re-identification key warning), em-dash free.
   - Add a Go bound method (in `app.go`, thin adapter) that opens a second
     Wails v2 window/webview loading the embedded documentation asset. Use the
     Wails v2 runtime window API available under the pin; if a true second
     OS window is not supportable in v2, fall back to a dedicated maximised
     webview window created at startup and shown/hidden on demand. No remote
     URLs.
   - Wire `nav-docs` and `home-docs` (and the home "Documentation" button) to
     call the new bridge method instead of `goToScreen("docs")`.
   - Retire the in-app `docs` screen path (`views/docs.js`, the `docs` branch
     in `main.js`, `SCREENS`); keep `DOCS_PLACEHOLDER` only if still
     referenced, otherwise remove it and its test.
2. **CR8.** In `static/style.css`, make the import two-pane grid fill the
   available height so the divider spans top to bottom before any preview
   loads: give `.import-view`/`.import-columns` a definite height (e.g.
   `height: 100%` within the scrolling `main#view`, or `min-height` to the
   viewport-minus-chrome) and set the columns/divider to `align-items:
   stretch` so `.import-divider` reaches the bottom with an empty preview.

**Unit tests.**
- `api.test.js` (or extend an existing bridge test): the new `openDocs` bridge
  method is declared and callable (mock the Wails runtime).
- `state.test.js`: `SCREENS` no longer includes `docs` (or docs navigation is
  removed), and `nav-docs` handling does not change wizard state.
- Divider fix is visual; add a DOM assertion that `.import-columns` carries the
  stretch/height contract class so a regression is caught in markup.

**Definition of done.** Documentation opens in its own offline window; the
import divider spans the full height with no preview loaded; commit
`build-04: phase 3, offline documentation window, import divider height fix`.

---

### Phase 4 — Configure: surface BUILD-03 features and add controls (CR9, CR10, CR11, CR12)

**Goal.** Make the BUILD-03 recognizers and confidence scoring visible and
configurable, and fix the Configure UX papercuts.

**Activities.**
1. **CR9 (category surfacing).**
   - Add `CATEGORY_LABELS` entries (label + one plain example each, em-dash
     free) in `copy.js` for the 8 new categories: `credit_card`, `uk_nhs`,
     `ip_address`, `mac_address`, `crypto`, `database_uri`, `de_steuer_id`,
     `es_nif`.
   - Extend the category lists in `state.js` so the new recognizers appear in
     the right group and in `ALL_CATEGORIES`. Introduce a new Configure group
     "Financial and technical identifiers" (or extend the existing Contact and
     thorough groups) and place each recognizer sensibly.
   - Update `presetCategories(level)` in `state.js` to match the Go
     `engine.PresetSelection` for the new categories, and confirm parity with
     the Go side (extend/verify the preset-parity test). Soft/Standard/Thorough
     must stay consistent with the engine.
   - Render the new group(s) in `configure.js whatTab`.
   - Session migration: older sessions without the new category keys default
     them from their preset; add a fixture test.
2. **CR9 (confidence scoring).** Add a "Detection confidence" control in the
   Configure "AI and advanced settings" tab (or a new "Detection" panel): a
   slider/threshold that maps to the pipeline's minimum span confidence. Thread
   it through the settings shape (`state.js settings`), `applySettings`
   (app.go), and the pipeline so low-confidence spans below the threshold are
   dropped. Default keeps current behaviour (no accidental data loss). Explain
   it in plain language with an example.
3. **CR10.** Add "Select all" and "Deselect all" buttons to each of the three
   (now possibly four) category groups. Add a pure reducer
   `setCategoryGroup(keys, on)` in `state.js` that flips a set of keys in one
   `setState`, then switches the preset display to Custom as usual.
4. **CR11.** Add a "Clear all" button to the "Never anonymise these terms"
   panel in `allowlist.js`. Add a `clearAllowlist()` reducer in `state.js`
   (empties the list). Keep the ability to re-seed defaults available (the
   default-allowlist bridge call is unchanged).
5. **CR12.** Stop the scroll reset on every toggle/allowlist edit. Options, in
   order of preference:
   - Update only the changed control in place instead of re-rendering the
     whole view (e.g. toggle the checkbox and recompute the preset chip
     without `container.innerHTML = ...`), OR
   - Capture the scroll container's `scrollTop` before re-render and restore it
     after, in `configure.js`/`allowlist.js`.
   Apply the same fix to the allowlist add flow (also used inside the Values
   step). The `main#view` element is the scroll owner.

**Unit tests.**
- `state.test.js`: `ALL_CATEGORIES` includes the 8 new keys; `presetCategories`
  parity for soft/medium/advanced; `setCategoryGroup` flips exactly the given
  keys and marks Custom; `clearAllowlist` empties the list; a confidence
  setting round-trips through the settings reducer.
- Go: `pipeline` respects the confidence threshold (spans below it are not
  replaced); preset parity test includes the new categories; session migration
  fixture loads an older session and fills the new keys.
- `copy.test.js`: the 8 new labels and the confidence-control copy pass the
  em-dash/jargon guards.
- A DOM/markup test that ticking a checkbox preserves `main#view.scrollTop`
  (simulate scroll, dispatch change, assert unchanged) for CR12.

**Definition of done.** The new recognizers and a confidence control are
visible and functional in Configure; select-all/deselect-all and clear-all
work; toggling does not scroll the page; commit
`build-04: phase 4, surface BUILD-03 recognizers and confidence, configure controls`.

---

### Phase 5 — Values step: smart detection tuning and suggestions table (CR13, CR14, CR15)

**Goal.** Reduce Smart-detection noise and make the suggestions table
searchable, filterable, sortable, with top-of-table bulk accept/deny.

**Activities.**
1. **CR13 (engine).** Add `SmartDetectOptions` to `engine/discover.go`:
   - `MinLength` (drop very short tokens),
   - `MinOccurrences` (require N occurrences to surface),
   - `ExcludeCommonWords` (skip a curated common/dictionary stop-word set so
     ordinary capitalised sentence starts do not surface),
   - `MinConfidence` (if smart candidates carry a heuristic score).
   Thread options through `SmartDetect`, keeping the current signature via a
   defaulting wrapper so existing tests/callers still compile. Update `app.go`
   `RunSmartDetection` to accept the options and `static/api.js` accordingly.
2. **CR13 (UI).** Add a small "Smart detection settings" control block in the
   discovery panel of the Values step (`views/values.js`) feeding the options,
   with sensible stricter defaults to cut over-detection. Persist the options
   in `state.js settings` (session-migrated).
3. **CR14 (table).** Rebuild `candidatesPanel` as a real table with `<thead>`
   and per-column controls:
   - Value column: a text search input that filters rows client-side.
   - Name type column: a list-of-values `<select>` whose options come from the
     NAME categories currently selected in Configure (so it reflects the
     user's choices), filtering rows by category.
   - Occurrence column: an asc/desc sort toggle button on the count.
   Keep filter/sort as view-local state (same pattern as `expanded`), applied
   as a pure transform over `s.candidates` so it stays testable; extract the
   filter+sort into a pure helper (in `entitymodel.js` or a new
   `candidatemodel.js`) for unit testing.
4. **CR15 (bulk actions).** Move the "Accept all X" buttons to the TOP of the
   table (above `<table>`), and add "Deny all X" next to each. Add a
   `denyAllInCategory(category)` reducer in `state.js` (drops all candidates of
   that category, mirroring `acceptAllInCategory`). Bulk actions operate on the
   currently FILTERED set so they match what the user sees, or clearly state
   they apply to the whole category (choose filtered-set semantics and test
   it).

**Unit tests.**
- Go `discover_test.go`: table-driven tests for each option (min length, min
  occurrences, common-word exclusion, confidence) in English and French;
  defaulting wrapper keeps prior behaviour.
- `state.test.js`: `denyAllInCategory` removes exactly that category;
  smart-detection options round-trip through settings + migration.
- New `candidatemodel.test.js`: the pure filter+sort helper (text search,
  category filter, asc/desc) over a candidate fixture.
- `copy.test.js`: new labels ("Deny all ...", column headers, settings copy)
  pass the guards.

**Definition of done.** Smart detection surfaces fewer, more relevant values;
the suggestions table has headers, per-column filters, and top-of-table Accept
all / Deny all; commit
`build-04: phase 5, smart-detection tuning and filterable suggestions table`.

---

### Phase 6 — Values step: backward reset and variant fixes (CR16, CR17)

**Goal.** Add safe backward-navigation reset and fix the variant
drill-down/add feature end to end.

**Activities.**
1. **CR16 (backward reset).**
   - Add per-step reset reducers in `state.js`: `resetStep(step)` clearing only
     that step's state (`configure` → settings back to defaults/preset;
     `values` → entities, candidates, patterns, discovery; `run` → running,
     progress, results, mapping). Documents (step 1) are never cleared by
     navigation.
   - In `main.js`, when the user navigates BACKWARD (Back button or clicking an
     earlier chip), show a confirmation dialog ("Going back will reset the
     current step. Continue?"). On confirm, call `resetStep(currentStep)` then
     navigate; on cancel, stay. Forward navigation is unchanged.
   - Keep guards intact (`canGoTo`).
2. **CR17 (variants).** Root-cause and fix each reported symptom:
   - **Add works only once / second entity fails.** The module-local
     `expanded` Set is keyed by `entityKey` and can drift after renames or when
     a second entity is added; ensure `expanded` membership is preserved across
     re-renders and follows canonical changes, and that `refreshVariants`
     re-expands every pending row (not just the first). Verify
     `addManualVariant` → `variants: null` → `refreshVariants` →
     `setEntityVariants` reliably re-renders the open row.
   - **Variants only appear after leaving/returning to the step.** Ensure the
     accept/add path awaits `refreshVariants()` and that the resulting
     `setState` repaints the Values view immediately (no stale render). Confirm
     the expanded row is rendered on the same tick.
   - **Smart detection blocks after a variant add.** Fix the stuck state: a
     pending expansion or a non-null `discovery` object must not gate the Smart
     button; ensure `discovery` is always cleared and expansion errors surface
     instead of hanging. Make the Smart button depend only on `discovery?.running`.
   - **Chips greyed and undraggable.** Fix the variant-chip CSS so chips are
     not visually disabled and remain `draggable`; verify `dragstart`/`drop`
     wiring and the "move" button fallback both function, including moving a
     variant under another value.
   - Keep all variant logic in tested pure reducers (`state.js`) and the pure
     view-model (`entitymodel.js`); the view stays wiring-only.

**Unit tests.**
- `state.test.js`: `resetStep` clears exactly the target step and preserves
  documents; adding a variant to a first then a second entity both end in a
  pending-then-expanded state; `moveVariant` across two freshly added entities
  succeeds; smart-detection availability does not depend on pending expansions.
- `entitymodel.test.js`: extend the variant regression suite for the
  multi-entity add sequence and the "expanded stays open after add" contract.
- A DOM/markup test that variant chips render with `draggable="true"` and
  without a disabled/greyed class.

**Definition of done.** Backward navigation prompts and resets safely; all five
CR17 symptoms are fixed with regression tests; commit
`build-04: phase 6, backward-step reset and variant drill-down fixes`.

---

### Phase 7 — Cross-cutting tests and hardening (CR16 robustness)

**Goal.** Prove the inter-step state machine is correct across every reachable
combination and lock the whole plan behind green CI.

**Activities.**
1. Build a state-transition test matrix (`state.test.js` or a new
   `wizardflow.test.js`): for each adjacent step pair, assert forward guard
   behaviour and backward reset behaviour, including edge states (no documents,
   documents but no config, config but no candidates, candidates but no run,
   run with results). This is the "robust test procedures / different possible
   states between steps" CR16 asks for.
2. Run and fix the full suite: `go vet ./...`, `go test ./...`,
   `node --test static/*.test.js`, and the Wails build.
3. Manual smoke pass documented in the PR body: import → configure (toggle new
   recognizers, confidence) → values (smart detection tuned, filter/sort,
   accept/deny, variants add + drag) → run → export; plus opening the docs
   window and the Home/menu/back-nav flows.

**Unit tests.** The matrix above; no untested reducer ships.

**Definition of done.** Full CI green; commit
`build-04: phase 7, wizard state-transition test matrix and hardening`.

---

## 6. CR-to-phase traceability

| CR | Phase | Primary files |
|---|---|---|
| CR1 | 1 | `static/copy.js`, `static/views/home.js`, `static/copy.test.js` |
| CR2 | 1 | `static/brand.css`, `static/style.css` |
| CR3 | 1 | `static/state.js`, `static/main.js`, `static/copy.js`, `static/views/values.js` |
| CR4 | 2 | `static/main.js`, `static/style.css` |
| CR5 | 2 | `static/style.css` |
| CR6 | 3 | `app.go`, `static/docs/*`, `static/main.js`, `static/views/docs.js` |
| CR7 | 2 | `static/main.js`, `static/style.css` |
| CR8 | 3 | `static/style.css` |
| CR9 | 4 | `static/copy.js`, `static/state.js`, `static/views/configure.js`, `engine/pipeline.go`, `app.go` |
| CR10 | 4 | `static/views/configure.js`, `static/state.js` |
| CR11 | 4 | `static/views/allowlist.js`, `static/state.js` |
| CR12 | 4 | `static/views/configure.js`, `static/views/allowlist.js` |
| CR13 | 5 | `engine/discover.go`, `app.go`, `static/api.js`, `static/views/values.js`, `static/state.js` |
| CR14 | 5 | `static/views/values.js`, `static/entitymodel.js` (or `candidatemodel.js`) |
| CR15 | 5 | `static/views/values.js`, `static/state.js` |
| CR16 | 6, 7 | `static/state.js`, `static/main.js` |
| CR17 | 6 | `static/views/values.js`, `static/state.js`, `static/entitymodel.js` |

---

## 7. Risk register

| Risk | Likelihood | Mitigation |
|---|---|---|
| Wails v2 lacks a clean second-window API (CR6) | Medium | Fall back to a dedicated pre-created webview window shown/hidden on demand; keep all assets embedded. Confirm the exact v2 runtime call before Phase 3. |
| Preset parity drift between JS `presetCategories` and Go `PresetSelection` after adding 8 categories (CR9) | Medium | Single parity test asserted on both sides; add all 8 keys in one commit; session migration covers older files. |
| Confidence threshold accidentally drops legitimate PII (CR9) | Medium | Default threshold preserves current behaviour; document with an example; test that the default replaces exactly what v1 did. |
| CR17 root cause deeper than the view (a reducer bug) | Medium | Reproduce each symptom as a failing `state.test.js`/`entitymodel.test.js` case first, then fix, so regressions are locked. |
| CR3 token rename breaks a saved session | Low | Explicit migration `entities` → `values` with a fixture test. |
| Scroll-preserve fix (CR12) causes a flicker on in-place update | Low | Prefer targeted DOM update; if restoring scrollTop, do it synchronously after the render in the same frame. |

---

## 8. Definition of done (whole plan)

- All 17 CRs implemented and mapped in §6, each behind unit tests.
- `go vet ./...`, `go test ./...`, `node --test static/*.test.js` and the Wails
  build are green.
- No em dashes in any user-visible string; no `Georgia` reference remains in
  `static/`.
- The local-only guarantee is intact (documentation window loads embedded
  assets only; no new network endpoints).
- CLAUDE.md reflects the font, documentation-window, and Values-label changes.

---

## 9. Pull request creation (this machine only)

The corporate network on this machine blocks `git push` (HTTPS/SSH) and the
desktop app's built-in PR button. The only method that works is GitHub's REST
Git Data API over HTTPS to `api.github.com`. When a PR is requested, the owner
provides a fine-grained PAT (Contents: read+write, Pull requests: read+write,
Metadata: read) and the `gh-pr.ps1` script (blob → tree → commit → ref → PR).
Procedure: save the script to a temp `.ps1`, run it with
`-Token`/`-Repo`/`-Branch`/`-Title`/`-File` (and `-Body`), report the PR URL,
then delete the temp script. The token is used for that one run only and never
stored. Exit code 2 means the branch was pushed but the PR was refused
(permission); use the printed compare URL.

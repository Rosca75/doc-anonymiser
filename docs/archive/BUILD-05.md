# BUILD-05.md — GUI redesign from the mock-up set

You are executing the fifth build plan for doc-anonymiser. BUILD.md produced
v1. BUILD-02.md delivered the functional-improvements pass. BUILD-03.md added
the Presidio-benchmarked deterministic detection layer. BUILD-04.md fixed UI
correctness and surfaced features the GUI was hiding. This document turns a
complete GUI redesign, delivered by the owner as five HTML mock-ups, into an
ordered, dependency-sequenced set of phases.

It follows the BUILD-02/BUILD-04 conventions: each phase has a **Goal**,
**Activities**, **Tests**, and a **Definition of done** ending in a named
commit. `CLAUDE.md` remains authoritative; where this plan changes a rule
there, Phase 0 amends `CLAUDE.md` first so no later phase violates it.

Source inputs:

- **`docs/mockups/*.dc.html`** — the five mock-ups (Welcome, Import, Identify,
  Anonymise, Export), committed with this plan and the design source of truth
  for layout, copy, colour and behaviour. Read them; do not work from this
  summary alone.
- A full survey of the frontend and backend, verified against commit
  `42bc339`.
- The brand palette (`docs/brand/color-palette.json`).

### How to read the mock-ups

They are Claude Design components, not runnable pages. Each file is markup
plus a `<script type="text/x-dc">` block holding a `Component` class whose
`renderVals()` returns the values the markup interpolates. The templating tags
are `<sc-for list="{{ x }}" as="i">` and `<sc-if value="{{ flag }}">`. They
reference a `support.js` runtime that is deliberately **not** committed: the
files are specifications to read, not pages to render. The `renderVals()`
bodies are the most precise statement of intended behaviour, including exact
copy strings, sort and filter rules, and state transitions.

---

## 1. Ground rules (in addition to everything in CLAUDE.md)

1. **The mock-up is the design authority; CLAUDE.md is the rule authority.**
   Where a mock-up would break a CLAUDE.md rule (the local-only guarantee, one
   loud orange element per view, no em dashes in user-visible copy, headings at
   regular weight), the rule wins and the deviation is recorded in the phase.
2. **The backend already does almost all of this.** Before adding any bound
   method, check `frontend/BRIDGE.md`: zip export, same-format metadata review,
   clipboard copy, key export as CSV/JSON, report as JSON/Markdown, session
   save/load, `fastRerun`, `cancelPipeline`, smart-detection tuning, pattern
   validation, allowlist CSV import and template all already exist. So do
   `reassignOriginal`, `entityAutocomplete`, `setMetaReview`, ordered
   `simpleRules`, `minConfidence`, `smartDetect` and sourced `candidates` in
   `state.js`, and `confidenceEffect()` in `views/configure.js`. This plan is
   mostly re-layout and deletion, not new capability.
3. **Delete what the redesign supersedes.** Superseded chrome and state must
   go in the phase that replaces it, not linger. Phase 9 is a net, not the
   plan.
4. **Frontend discipline is unchanged** (`frontend/CLAUDE.md`): `api.js` is the
   only bridge caller, `state.js` is the only state holder, `ui.js`/`shell.js`
   stay pure string builders, `copy.js` owns every user-visible string.
5. **Every phase ends green**: `go build ./...`, `go test ./...`, and
   `node --test frontend/*.test.js`.

---

## 2. What the redesign changes

The mock-ups are not a reskin. Three structural changes drive the sequencing:

1. **Five wizard steps become four.** Import, Identify, Anonymise, Export.
   Configure stops being a screen and becomes the left rail of Identify.
2. **Every screen becomes a fixed-height two-column card workspace.** Today's
   screens are stacked collapsible panels on a scrolling page. The mock-ups
   never scroll the page body: the shell is `100vh`, and each card scrolls
   internally (`min-height: 0` plus `overflow`).
3. **The global Back/Next footer and the per-step explainer banner both go.**
   Each screen owns its own footer bar carrying "Back to X", a readiness hint,
   and a primary "CONTINUE TO Y".

The 23 detection categories in the mock-up match `state.js ALL_CATEGORIES` key
for key, and its `presetCategories()` matches ours, so the category model needs
no change at all.

---

## 3. Decisions

Decisions 1, 5 and 8 are the owner's, taken during planning. The rest were put
to the owner and not contested.

1. **Four steps, and no backward compatibility.** Nothing needs to keep
   working for state written by a previous version. This removes machinery
   rather than adding it:
   - `LEGACY_STEP_TOKENS` and `migrateStep()` are **deleted**, not extended.
     An unknown persisted step token falls back to `"import"`.
   - No session migration is written for the Phase 3 placeholder override.
   - `engine/session.go` refuses a session whose schema version this build
     does not know, with an actionable message, instead of half-migrating. The
     per-field `?? default` compatibility fallbacks in the session loader in
     `views/export.js` go with it.
   - Root `CLAUDE.md` §5 loses its promise that a session storing the old
     `entities` step token still loads.
2. **Document country is frontend-only.** The selector swaps the example text
   beside phone / VAT / national identification and toggles `de_steuer_id`,
   `es_nif`, `uk_nhs`. That is exactly what the mock-up script does. No new
   regexes, no locale-aware engine, no change to `engine/pii.go`.
3. **The confidence slider keeps the engine's semantics.** It maps 0-100 to
   `minConfidence` 0.0-1.0, the setting already wired end to end. The mock-up's
   helper copy ("values that only the local AI suggested are skipped")
   describes a source-tiered rule the engine does not have; that copy is
   rewritten to describe the real floor, reusing `confidenceEffect()`.
4. **The destination folder drives the zip only.** Single-document saves, the
   key, the report and the session keep their native save dialogs, so nothing
   key-bearing lands on disk from one click.
5. **Import unit counts, including docx pages.** A docx does carry a page
   count: OOXML's extended-properties part `docProps/app.xml` caches `<Pages>`
   (alongside `<Words>`, `<Lines>`, and `<Slides>` for pptx).
   `engine/exportfmt/metadata.go` already parses that exact part for Company
   and Manager, so the reader exists and needs a second consumer, not a
   rewrite. Two caveats belong in the code comments:
   - `<Pages>` is what the last writing application computed at save time, not
     something recomputed on import, so a file rewritten by a tool that does
     not relayout can report a stale figure.
   - The element is optional. Files written by minimal tooling omit it (the
     repo's own `backend/testdata/report.docx` and `deck.pptx` have no
     `app.xml` at all), so the absent case is the common one in tests and must
     degrade to a line count rather than showing "0 pages".
6. **The import divider is dropped.** The mock-up uses a fixed two-column
   grid. `importSplit` / `setImportSplit` and their tests go with it.
7. **The Welcome sidebar's trailing slot stays empty.** No resume-last-session
   card; it appears only in a superseded screenshot, not in the mock-up.
8. **Cloud AI is a placeholder panel and nothing else.** A separate improvement
   plan will cover it. The rail renders the tab with the mock-up's "Not
   available yet" copy. Deliberately out of scope here: no provider list, no
   endpoint field, no settings shape in `state.js`, no `BRIDGE.md` row, no Go
   client. This plan must not leave half-built scaffolding for that one to trip
   over.
9. **"Pattern" as a suggestion source is deferred.** The mock-up's Suggestions
   table shows rows sourced from "Pattern", but deterministic PII matches are
   applied without review by design and never enter `state.candidates`. The
   source filter renders whatever sources are actually present (`smart`,
   `local-ai`). Surfacing deterministic hits for review is a separate feature.
10. **Every native `confirm()` goes.** The mock-up replaces the key warning
    with an in-app modal; the same modal replaces the backward-navigation
    confirm in `main.js`.

Still open, and settleable when the phase starts:

- **Editable placeholder per value** is the one genuinely new engine feature
  (the registry override, Phase 3). If the registry should not be touched in
  this pass, the field renders read-only and Phase 3 shrinks to unit counts and
  the destination folder. Planned as a real override.
- **Run detection as one button** folds away today's per-file discovery picker.
  If that picker must be kept, it needs a home in the rail.

---

## 4. Phases

### Phase 0 — Charter amendments and the step guard

**Goal.** No later phase violates a written rule, and a step rename can never
again drift between the five places that define the wizard.

**Activities.**

1. Root `CLAUDE.md` §3: update the repository structure view-module list.
2. Root `CLAUDE.md` §5: replace the "Engine identifiers are stable, user-visible
   labels are not" bullet so it names the **four** steps and records that step 2
   is "Identify" and owns the configure choices, with engine category
   identifiers and the `engine/pii.go` constants still frozen. Keep the
   "labels are not identifiers" principle. **Drop** the sentence promising a
   session storing the old `entities` token still loads; state instead that
   session files are read only by the version that wrote them (decision 1).
3. `frontend/CLAUDE.md`: file map (new modules, `configure.js` removed), the
   fixed-height layout contract, and the no-native-dialog rule.

**Tests.** New root guard (`package main`, same spirit as
`category_parity_test.go`): `state.js WIZARD_STEPS`, `main.js STEP_LABELS`, the
`VIEWS` map, `STEP_RESETS` and `copy.js NAV.stepNames` must cover exactly the
same four tokens, failing with a message that names the offender.

**Done.** Guard passes; `go test ./...` green; commit
`BUILD-05 Phase 0: charter amendments and the wizard step guard`.

---

### Phase 1 — Brand tokens and the shared UI kit

**Goal.** One design system every screen draws from, so no screen invents its
own spacing, tint or card.

**Activities.**

1. `frontend/brand.css`: add the palette values the mock-up uses that are
   already in `docs/brand/color-palette.json` (`#717C8D`, `#54616C`) plus
   `#F9FAFB` as a subtle surface. Add **functional-only** status tint pairs,
   each commented as such: suggestion-source badges, pattern validity, toast
   tones (ok / info / warn), the warning strip. No decorative colour; the
   one-loud-orange rule still holds per view.
2. `frontend/style.css`: chrome heights from the mock-up (header 80px, step bar
   68px, footer 60px); the card system (`.card`, `.card-head`, `.card-body`,
   `.rail`, `.workspace`); the fixed-height contract (`min-height: 0` plus
   internal `overflow`; the page body never scrolls horizontally or
   vertically); tab bars with count badges; chip rows; sortable table headers;
   tint chips; stat tiles.
3. `frontend/ui.js` (pure builders only): `tabbar()`, `countBadge()`,
   `chipRow()`, `sectionLabel()` (the uppercase tracked labels), `statTile()`,
   `collapsibleGroup()`, `stepFooter()`, `toastHTML()`, `modalHTML()`.
4. New `frontend/toast.js` and `frontend/modal.js`: a state-backed notice, and
   an in-app confirm returning `Promise<boolean>`. Reducers `setNotice`,
   `clearNotice`, `askConfirm` in `state.js`.

**Tests.** `ui.test.js` for every new builder (escaping, active/disabled
states); `state.test.js` for the notice and confirm reducers.

**Done.** `node --test frontend/*.test.js` green; commit
`BUILD-05 Phase 1: brand tokens, card system and shared UI kit`.

---

### Phase 2 — Four steps, the new step bar, per-screen footers

**Goal.** The step model changes exactly once, before any screen is rewritten
against it.

**Activities.**

1. `state.js`: `WIZARD_STEPS = ["import","identify","anonymise","export"]`;
   **delete** `LEGACY_STEP_TOKENS` and `migrateStep()`, leaving an unknown
   token to fall back to `"import"` at its one call site; rekey `STEP_RESETS`
   so `identify` clears what Configure **and** Values owned and `anonymise`
   clears what Run owned; update `canGoTo`.
2. `shell.js`: re-render `workflowBannerHTML` as the mock-up's bar — a
   "← Anonymise Flow" back link, a divider, then numbered circles showing a
   check mark for completed steps. The separate "Anonymisation workflow" title
   goes; the back link replaces it.
3. `main.js`: `STEP_LABELS` and `VIEWS` for four steps; **delete the global
   `.navbar`** (each screen owns its footer now) and **delete the
   `STEP_BANNERS` explainer strip** (that copy becomes card subtitles);
   `navigateTo` uses the Phase 1 modal instead of `confirm()`.
4. `copy.js`: `NAV.stepNames`, the back-link label, `HOME.stepsTitle` and the
   four-entry `HOME.steps`; remove `STEP_BANNERS`, folding its useful sentences
   into the card subtitles each screen renders.
5. `views/home.js` renders from `copy.js`, so the four-step sidebar is nearly
   free; drop the empty trailing slot (decision 7).

**Tests.** Update `wizardflow.test.js`, `state.test.js`, `shell.test.js`,
`copy.test.js`. Add a case pinning that an unknown step token lands on
`"import"`.

**Done.** Phase 0 guard still green; commit
`BUILD-05 Phase 2: four-step wizard, new step bar, per-screen footers`.

---

### Phase 3 — Backend additions

**Goal.** The three pieces of new backend data the screens need, landed before
the screens that consume them.

**Activities.**

1. **Unit counts.** `DocumentInfo` gains `unitCount int` and `unit string`
   (`"page"`, `"slide"`, `"row"`, `"line"`), filled at import:
   - Extract the `docProps/app.xml` reader currently private to
     `engine/exportfmt/metadata.go` into something the converters can call (it
     already handles the part being absent). Do **not** duplicate the XML
     parsing.
   - docx reads `<Pages>`; pptx cross-checks `<Slides>` against its own
     slide-part count and prefers the part count, which is always correct.
   - `convert/pdf.go` already walks pages, so a pdf count is exact.
   - Grid documents (csv, flat xlsx sheet) report rows.
   - Anything else, and any docx whose `app.xml` is missing or reports 0,
     reports lines (decision 5).
2. **Placeholder override.** `engine.Registry` gains
   `SetPlaceholder(category, original, placeholder)`, validating the
   `[NAME_N]` shape and rejecting collisions with an actionable message. New
   bound `App.SetEntityPlaceholder(category, canonical, placeholder)`. Stored
   in the session as a plain additive field with **no migration path**
   (decision 1).
3. **Destination folder.** `App.ChooseExportFolder()` (native directory dialog)
   and `App.ExportAllZipTo(dir)`. The remembered folder is frontend state, not
   a Go setting.
4. **Session version refusal** (decision 1): an unknown schema version is
   refused with an actionable message.
5. `api.js` wrappers plus `BRIDGE.md` rows for each new method.

No report change: the per-value occurrence counts the Anonymise report
drill-down shows are counted in JS from the anonymised text, exactly as the
mock-up script does.

**Tests.** Table tests for the unit counts covering **both** branches, absent
and present `app.xml`; a new fixture that carries `app.xml` is needed for the
present branch, since `report.docx` and `deck.pptx` do not. Registry tests for
a valid override, a malformed placeholder and a collision. A session round trip
including overrides, plus a refusal test for an unknown version.

**Done.** `go test ./...` green; commit
`BUILD-05 Phase 3: unit counts, placeholder override, export destination`.

---

### Phase 4 — Import screen

**Goal.** `Import.dc.html`, exactly.

**Activities.** Rewrite `views/import.js`: two cards side by side; left card
with the file-count label, an ADD FILES primary, the drop zone, and document
rows carrying the format badge, size, unit count, EXPERIMENTAL badge, warning
count with tooltip, remove button and the selected-row orange bar; right card
the preview with its "WORKING FORM" label. Drop the divider and `importSplit`
(decision 6). Add the step footer with CONTINUE TO IDENTIFY. Reuse `fmtSize()`
and `markdownTableToHTML()` unchanged.

Note: `Import.dc.html` still draws the old five-chip step bar. It renders four
under decision 1.

**Tests.** Keep the `markdownTableToHTML` tests; drop the `setImportSplit`
tests.

**Done.** Commit `BUILD-05 Phase 4: import screen`.

---

### Phase 5 — Identify screen, the Configure rail

**Goal.** The left third of `Identify.dc.html`.

**Activities.**

1. New `views/identify.js` owning the two-column layout.
2. Rename `views/configure.js` to `views/identifyrail.js`, keeping its exported
   `llmGateTooltip` (the Anonymise view imports it) and reusing
   `confidenceEffect()`, `PRESETS` and the existing group definitions.
3. Rail tabs: Scope / Smart detection / Local AI / Cloud AI.
4. Scope tab: the country select, preset chips (Soft / Standard / Thorough /
   Custom), the four collapsible category groups with `n/m` counts and
   select-all / deselect-all (reuse `setCategoryGroup`), and the confidence
   slider with corrected copy (decision 3).
5. New pure `frontend/countries.js` (five countries, `examplesFor(code)`,
   `countryIDCategories`); `setDocumentCountry` reducer in `state.js` flipping
   the three country-specific ID categories (decision 2).
6. `copy.js CATEGORY_LABELS` examples for `phone`, `vat` and `matricule` become
   country-aware. `category_parity_test.go` matches on `\n  key: [`, so that
   declaration shape must survive.
7. Cloud AI tab: the placeholder panel only (decision 8).

**Tests.** New `countries.test.js`; `state.test.js` for
`setDocumentCountry`; `configure.test.js` renamed and updated with the rail;
`category_parity_test.go` must stay green.

**Done.** Commit `BUILD-05 Phase 5: identify screen configure rail`.

---

### Phase 6 — Identify screen, the workspace

**Goal.** The right two thirds of `Identify.dc.html`.

**Activities.**

1. Workspace tabs with count badges: Suggestions / My values / Never anonymise
   / Patterns; the header search box and the Run detection button.
2. Suggestions: reuse `candidatemodel.visibleCandidates`; sortable VALUE and
   COUNT headers, type and source filter selects in the header row, source
   badges, and Accept all shown / Reject all shown. The existing
   `acceptAllInCategory` / `denyAllInCategory` are per-category, so add
   `acceptAllShown(texts)` / `rejectAllShown(texts)` reducers for the mock-up's
   across-category bulk buttons.
3. Run detection folds today's separate discovery and smart-detection panels
   into one action: the offline smart pass always, the AI pass when Local AI is
   on. Keep the `estimateDiscovery` pre-check, surfacing oversized files as a
   notice rather than a per-file picker panel.
4. My values: entity cards with the editable placeholder (Phase 3), type badge,
   variant chips with remove, add-variant, and the add-value row. Reuse
   `entitymodel.js`.
5. Never anonymise: `views/allowlist.js` reflowed to the chip layout with
   Import CSV and Template.
6. Patterns: expression rows with the valid / error badge from
   `validatePattern`.
7. Step footer with the ready count and CONTINUE TO ANONYMISE.

**Tests.** `candidatemodel.test.js` for the bulk-shown reducers;
`entitymodel.test.js` unchanged; `state.test.js` for the placeholder override
path.

**Done.** Commit `BUILD-05 Phase 6: identify screen workspace`.

---

### Phase 7 — Anonymise screen

**Goal.** `Anonymise.dc.html`.

**Activities.** Rename `views/run.js` to `views/anonymise.js` and re-lay it out
as a left column of cards plus a right Compare card.

1. Run card: deep-scan checkbox (LLM-gated), RUN / RUN AGAIN, Cancel, the
   progress bar with its per-file label, and the stats row (replacements,
   documents, categories, duration from `report.durationMs`).
2. Selected placeholder card **replaces the floating reassign popover**:
   clicking a mark in the anonymised pane fills a card showing placeholder and
   original, offering "make it a variant of" (reuse `entityAutocomplete`,
   `reassignOriginal`, `fastRerun`). Delete `wireReassignPopover`.
3. Report card: scope select (all files / one file), per-category expandable
   rows listing value, placeholder and occurrences counted from the anonymised
   text, and dismissible warnings (new `dismissedWarnings` state).
4. Something missed? card: category select, value input, Add value producing
   pending chips, Fast re-run.
5. Find and replace card: the existing ordered rules editor reflowed.
6. Compare card: document select, ORIGINAL and ANONYMISED panes.
   `highlight.js` already emits `data-ph` and `data-original`, so hover
   tooltips and click selection reuse it. New: selecting text in either pane
   opens a floating REPLACE SELECTION card that adds a `[CUSTOM_n]`
   simple-replace rule and fast-reruns.
7. Step footer with CONTINUE TO EXPORT, gated on results.

**Tests.** `highlight.test.js` unchanged; `state.test.js` for
`dismissedWarnings` and the pending-values list.

**Done.** Commit `BUILD-05 Phase 7: anonymise screen`.

---

### Phase 8 — Export screen

**Goal.** `Export.dc.html`.

**Activities.** Rewrite `views/export.js`: left column with the Export card
(destination folder, Browse, EXPORT ALL AS ZIP), then collapsible Value
mapping, Report and Session cards; right card listing documents with their
`_anon` output name, replacement and property counts, per-format buttons
(same-format ones tinted, `.pdf` carrying the experimental caption) and the
copy button; the inline Properties review panel; and the toast strip at the
card's foot. The key warning becomes the Phase 1 modal (decision 10). START A
NEW BATCH is a new `startNewBatch()` reducer clearing documents, results,
entities, candidates, patterns, rules and mapping while keeping settings and
the allowlist, behind a confirmation.

**Tests.** `state.test.js` for `startNewBatch` (what it clears and, just as
importantly, what it keeps).

**Done.** Commit `BUILD-05 Phase 8: export screen`.

---

### Phase 9 — Cleanup, guards, tests

**Goal.** Nothing superseded survives, and the guards cover the new shape.

**Activities.**

1. Delete what the redesign supersedes: `STEP_BANNERS`, `.navbar`,
   `importSplit` / `setImportSplit`, `wireReassignPopover`, the old
   configure/values/run module names, and every CSS rule left unreferenced.
2. Delete what decision 1 supersedes: `LEGACY_STEP_TOKENS`, `migrateStep()`
   and their tests, and the per-field `?? default` compatibility fallbacks in
   the session loader in `views/export.js`.
3. Sweep `copy.js` for em dashes in new copy; `copy.test.js` and
   `copy_guard_test.go` must both be green.
4. Confirm `embed_test.go` still passes with any new asset.

**Done.** Full suite green; commit `BUILD-05 Phase 9: cleanup and guards`.

---

## 5. Critical files

| Area | Files |
|---|---|
| Step model | `frontend/state.js`, `frontend/main.js`, `frontend/shell.js`, `frontend/copy.js` |
| Design system | `frontend/brand.css`, `frontend/style.css`, `frontend/ui.js`, new `frontend/toast.js`, `frontend/modal.js` |
| Screens | `frontend/views/{home,import,identify,identifyrail,anonymise,export,allowlist}.js` |
| Reused logic | `frontend/candidatemodel.js`, `frontend/entitymodel.js`, `frontend/highlight.js`, `frontend/scroll.js` |
| Backend | `backend/app.go`, `backend/app_export.go`, `backend/engine/{registry,session}.go`, `backend/engine/convert/{docx,pptx,pdf,xlsx}.go`, `backend/engine/exportfmt/metadata.go` (its `app.xml` reader gets extracted for reuse) |
| Contract | `frontend/BRIDGE.md` |
| Charters | `CLAUDE.md`, `frontend/CLAUDE.md` |

## 6. Manual test matrix

On Windows with `wails dev`, using `backend/testdata` (docx, pptx, xlsx, pdf,
csv, md, txt, plus the French fixture):

1. Welcome shows four steps; ANONYMISE DOCUMENTS enters the flow.
2. Import: add files and drop files; check size, EXPERIMENTAL on the pdf, the
   warning count on a file with ingestion warnings, remove, and the preview's
   working form. For unit counts, import both a Word-authored docx (page count
   from `app.xml`, cross-checked against Word's status bar) and the repo's
   `report.docx`, which has no `app.xml` and must fall back to lines, never
   "0 pages".
3. Identify: switch country and confirm the phone / VAT / national-ID examples
   and the country ID switches change; run detection with Ollama up and down
   (every AI control disabled with its tooltip, the deterministic path fully
   usable); accept, reject and bulk-act on a filtered set; edit a placeholder
   and confirm a collision is refused readably; add an allowlist term and
   confirm it survives the run.
4. Anonymise: run, cancel mid-run, run again; click a placeholder and reassign
   it as a variant; select text and create a `[CUSTOM_n]` rule; add a missed
   value and fast re-run, confirming existing placeholders keep their numbers;
   expand a report category and check the occurrence counts against the panes.
5. Export: set a destination and export the zip; save one docx as same-format
   and confirm the properties review, the edited filename, and that the source
   file on disk is byte-identical afterwards; export the key and confirm the
   in-app modal gates it; save and reload a session and confirm placeholders
   are reused; START A NEW BATCH clears the batch but keeps settings.
6. Hand the app a session file written by the pre-BUILD-05 build: it must be
   refused with the actionable message, not partially loaded (decision 1).

## 6b. Implementation notes: deviations from this plan

Every deviation below is recorded here because §7 requires it. None changes a
numbered decision; each is a smaller judgement the plan did not reach.

1. **Identify is three modules, not two.** The plan's critical-files table names
   `views/identify.js` and `views/identifyrail.js`. The workspace half is ~700
   lines on its own, so it lives in `views/identifyworkspace.js`:
   `identify.js` owns the two-column layout and the footer, and each half wires
   its own handlers. Both charters name all three.
2. **`frontend/nav.js` is new.** Phase 2 gave every screen its own footer, so
   the step bar and four footers all move the wizard. The backward-reset rule
   lives in `nav.js` once rather than in five places, and the module is separate
   from `main.js` only to keep the graph acyclic (`main.js` imports the views).
   It also builds the footer, so a step rename reaches all four screens through
   `copy.js NAV`.
3. **The module renames happened in Phase 2, not 5 and 7.** `configure.js` to
   `identifyrail.js`, `values.js` to `identifyworkspace.js` and `run.js` to
   `anonymise.js` all landed with the step rename, so no phase shipped a step
   token whose module name contradicted it. Their contents were relaid out in
   Phases 5 to 7 as planned.
4. **Variant drag-to-regroup is kept.** `Identify.dc.html` shows variant chips
   with a remove button only. `state.js moveVariant` is a real, tested capability
   with no other home: without it a spelling attached to the wrong value could
   only be fixed by excluding it on one card and retyping it on another. The
   chips stay draggable and the value cards are drop targets.
5. **The allowlist "Clear all" button is gone.** It lived in the panel header
   that the chip layout does not have, and it was the last native `confirm()` on
   that screen (decision 10). Terms are removable one chip at a time and the
   engine defaults are still seeded at startup.
6. **The preset and the country interact, and the country wins.** Every preset
   switches all three country-specific identifiers on, because to the engine they
   are hard PII. `applyPreset` therefore re-applies the country, and
   `selectionPresetName` excludes those three from its comparison. Without the
   first, picking Soft on a German document would start looking for Spanish tax
   numbers; without the second, a Luxembourg document would read as "Custom" the
   instant the user picked Standard.
7. **Export's footer has no CONTINUE.** It is the last step, so `ui.js
   stepFooter` gained a `rightHTML` slot and START A NEW BATCH is a neutral
   button rather than the accent primary of the other three screens.
8. **`SessionVersion` is bumped to 2.** Decision 1 says a file this build does
   not know is refused. That only bites if the version changes, so it does, and
   the refusal message says which direction the mismatch goes because the fix
   differs.
9. **Phase 9 deleted more than the plan lists.** Beyond `STEP_BANNERS`,
   `.navbar`, `importSplit`, the reassign popover and `migrateStep`:
   `ui.js panel()` / `wirePanels()` (superseded by `card` and
   `collapsibleGroup`), `entitymodel.js variantRows()` (no expanded-row set
   exists any more), `state.js prevStep()` (it moved back without the reset
   question, which is a way around the rule `nav.js` enforces),
   `setEntityStatus` / `editEntity` / `updateCandidate` /
   `acceptAllInCategory` / `denyAllInCategory` / `clearAllowlist`, the
   `.panel*` / `.form-row` / `.radio-row` CSS, and the retired `copy.js` keys.

## 7. Definition of done (whole plan)

- All four screens match their mock-up in layout, copy and behaviour, with
  every deviation recorded against a CLAUDE.md rule or a numbered decision.
- The page body never scrolls; every card scrolls internally.
- No native `confirm()` remains in the frontend.
- No superseded module, reducer, CSS rule or copy string remains.
- `go build ./...`, `go test ./...` and `node --test frontend/*.test.js` are
  green, including the Phase 0 step guard, `category_parity_test.go`,
  `copy_guard_test.go` and `embed_test.go`.
- The deterministic pipeline is fully usable with Ollama absent.

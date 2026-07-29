# BUILD-02.md — Functional improvements build plan

> **Path note (repo restructure, 2026-07):** This is a historical plan.
> References to `static/...` and to root-level Go files now live under
> `frontend/...` and `backend/...` respectively. The authoritative current
> layout is `CLAUDE.md` §3.

You are executing the second build plan for doc-anonymiser. BUILD.md (fully
executed, merged as PR #3) produced the working v1 application. This document
turns the consolidated user-feedback improvement plan into an ordered,
dependency-sequenced set of implementation phases. It follows the same
conventions as BUILD.md: each phase has a Goal, Activities, Unit tests, and a
Definition of done ending in a named commit. CLAUDE.md remains authoritative;
Phase 0 amends it first so no later phase violates it.

Source inputs for this plan:

- The improvement plan (consolidated user feedback, sections 0 to 6).
- The brand palette, vendored at `docs/brand/color-palette.json`.
- The brand guidelines (typography and colour-usage rules).
- A full survey of the current codebase (file and symbol references below are
  verified against the code as of commit `6064461`).

---

## 1. Ground rules (in addition to everything in CLAUDE.md)

1. **Engine first, UI second.** Every behaviour change lands in `engine/*` or
   `ollama/*` with table-driven tests before any view consumes it.
2. **One phase, one commit, CI green.** Do not start a phase until the
   previous phase's commit passes `go vet ./...`, `go test ./...`,
   `node --test static/*.test.js`, and the Wails build in CI.
3. **The local-only guarantee is untouched.** No CDN fonts, no remote icon
   fonts, no new network endpoints. Material Symbols are vendored as SVG
   files; Georgia and Arial are Windows system fonts and require no font
   files at all.
4. **No em dashes in any user-visible string.** Enforced by an automated
   test from Phase 1 onward (see Phase 1). Use commas, periods, or
   parentheses. Also banned in UI copy: "+" as a stand-in for "and", and
   unexplained jargon such as "PII" without an example.
5. **Session compatibility.** Any change to category keys, settings shape, or
   registry labels must load v1 `.anonsession.json` files via an explicit
   migration, covered by a fixture test. Never silently drop user data.
6. **Same-format export never touches the source file.** It reads
   `Document.Raw` (already in memory) and writes a brand-new file through the
   existing save-dialog path. The original on disk is only ever read once, at
   import.
7. **New Go dependencies** must be added to the pinned-versions tables in
   this file AND CLAUDE.md §7 before `go get`, and must be pure Go (no CGo),
   and must compile under the Go 1.26.x pin (verify the dependency's own
   go.mod before adopting; this bit us once with ledongthuc/pdf).

---

## 2. Open decisions, status

### 2.1 Same-format export (improvement plan §0.1) — RESOLVED

Scope confirmed: Export gains a new option producing a fresh anonymised file
in the source format (docx/pptx/xlsx/pdf). The original is never modified.
Implemented in Phases 11 to 13. CLAUDE.md §4 ("the app never exports back to
docx/pptx/xlsx/pdf") is amended in Phase 0 before any code is written.

### 2.2 Entity type list (improvement plan §0.2) — BLOCKED, does not block the build

The feedback listing entity types cuts off ("clients, projects, Internal,
...").

**Decision recorded for this plan:** proceed with the four existing entity
categories, renamed per §4 of the improvement plan (clients, projects,
internal, persons) plus custom patterns, and implement all per-type behaviour
**data-driven** (a single `CATEGORIES` table in the frontend and a single
category registry in the engine) so that adding a fifth type later is a
one-line change per table, not a code change. When the owner supplies the
full list, add the missing types in a follow-up change order. No phase below
waits on this.

---

## 3. Sequencing and dependency graph

Phases are ordered so that every phase only consumes things built in earlier
phases. Three tracks exist; a single agent executes them in numeric order,
but tracks B and C can be parallelised across agents if desired.

```
Track A (UI foundation)      Track B (engine/AI)         Track C (export)
─────────────────────        ───────────────────         ────────────────
Phase 0  spec amendments  ─┬─────────────┬──────────────────┐
Phase 1  brand + toolkit   │             │                  │
Phase 2  shell + home      │             │                  │
                           ├─ Phase 3  category model       │
                           ├─ Phase 4  allowlist import     │
                           ├─ Phase 5  ollama robustness    │
Phase 6  configure UI  ←───┘ (needs 1,2,3,4,5)              │
Phase 7  entities fixes      (needs 5)                      │
                           ┌─ Phase 8  smart detection      │
Phase 9  entities UI   ←───┘ (needs 6,7,8)                  │
Phase 10 run UI              (needs 3, 10 is else-independent)
                                          Phase 11 OOXML export ←┘ (needs 3)
                                          Phase 12 metadata + filename (needs 11, reuses 9's review UI pattern)
                                          Phase 13 PDF export (needs 11's plumbing, 12's metadata model)
Phase 14 hardening + docs + release (needs all)
```

Rationale for the order:

- **Brand/toolkit (1) before any view work (2, 6, 9, 10):** every later UI
  phase consumes the button, banner, panel, and icon helpers; doing views
  first would mean restyling twice.
- **Engine category model (3) before configure UI (6):** the granular
  checkboxes need a settings shape and pipeline behaviour to bind to.
- **Ollama robustness (5) before discovery work (7, 8, 9):** the 400/404 fix,
  `num_ctx`, and chunking change the client's contract that discovery
  consumes; smart detection's span-classification mode is designed against
  the fixed client.
- **Targeted entities bug fixes (7) before the entities rework (9):** the
  variants bug fix and its regression tests define the data-flow contract
  the rework must preserve; fixing after a rewrite loses the regression
  anchor.
- **OOXML body export (11) before metadata (12) before PDF (13):** metadata
  reuses the zip-rewrite plumbing from 11; PDF is highest-risk and
  EXPERIMENTAL, so it ships last and cannot block the rest.

---

## 4. New pinned dependencies

Add rows to CLAUDE.md §7 in Phase 0; run `go get` only in the phase that
first uses each.

| Component | Version | First used | Notes |
|---|---|---|---|
| github.com/pdfcpu/pdfcpu | pin latest v0.x at Phase 13 start (v0.11.x as of writing) | Phase 13 | PDF Info dictionary + XMP metadata rewrite; Apache-2.0; pure Go. VERIFY its go.mod allows Go 1.23.x before adopting; if it requires 1.24, pin the newest release that does not (same precedent as ledongthuc/pdf) |
| github.com/go-pdf/fpdf | v0.9.x | Phase 13 (fallback path only) | Pure-Go PDF writer for the regenerated-PDF fallback; MIT. Only added if the fallback path is taken |
| Material Symbols SVGs (assets, not a Go module) | snapshot at Phase 1 | Phase 1 | Individual SVG files vendored into `static/assets/icons/`; Apache-2.0; add the licence text at `static/assets/icons/LICENSE` |

No other new dependencies. OOXML same-format export (Phases 11, 12) uses
only `archive/zip`, `encoding/xml`, and the already-pinned excelize.

---

## 5. Brand tokens (single source of truth for Phase 1)

Values come from `docs/brand/color-palette.json` and the brand guidelines.
Phase 1 creates `static/brand.css` containing exactly this block (plus
comments), and `style.css` consumes only these variables:

```css
:root {
  /* Core */
  --bg: #FFFFFF;              /* white background is the default */
  --bg-secondary: #EBEBEB;    /* quiet secondary surfaces, banding, dividers */
  --text: #000000;            /* black text on light surfaces, always */
  --muted: #A1A8B3;           /* slate gray, secondary text and followed links */

  /* Signature accent: ONE hero element per view (primary button, key count).
     Never body text, never a flood fill behind small text. */
  --accent: #FD5108;
  --accent-medium: #FE7C39;
  --accent-light: #FFAA72;

  /* Orange tint ramp (toward white) for highlight fills; black text on all */
  --tint-orange-1: #FFCDA8;
  --tint-orange-2: #FFE8D4;
  --tint-orange-3: #FFF5ED;

  /* Grey-blue ramp for neutral chrome */
  --gray-1: #A1A8B3;
  --gray-2: #B5BCC4;
  --gray-3: #CBD1D6;
  --gray-4: #DFE3E6;
  --gray-5: #EEEFF1;
  --gray-6: #F5F7F8;

  /* Status (functional only, never decorative) */
  --ok: #059669;
  --warn: #E9B01F;
  --err: #DC2626;

  /* Typography: Windows system fonts only, no font files, no CDNs */
  --font-heading: Georgia, 'Times New Roman', serif;   /* regular weight, never bold */
  --font-body: Arial, Helvetica, sans-serif;
  --font-mono: Consolas, 'Courier New', monospace;     /* previews, unchanged */
}
```

Usage rules (from the brand skill, enforce in review):

- Headings in Georgia at **regular weight**; hierarchy comes from size and
  space, not bold. Functional UI text (labels, buttons, tables) in Arial.
- Black text on all light surfaces. White text only on black, or as large
  bold type on `--accent`.
- One loud orange element per view. Greys carry everything contextual.
- Existing highlight-mark tints (`--mark-pii`, `--mark-entity`,
  `--mark-custom`) are remapped onto the orange/grey tint ramps:
  pii → `--tint-orange-1`, entity → `--gray-4`, custom → `--tint-orange-3`,
  each with black text.

---

## Phase 0 — Spec amendments and decision log

**Goal:** CLAUDE.md and this plan agree before code changes start; the two
resolved/blocked decisions are recorded.

**Activities:**

- 0a. Amend CLAUDE.md §4, bullet "Converters are pure Go and one-way".
  Replace its final two sentences' claim that the app "never exports back to
  docx/pptx/xlsx/pdf" with:

  > Binary formats convert TO markdown on import for preview and processing.
  > The app can additionally write a NEW anonymised copy in the source
  > format (docx/pptx/xlsx, and experimentally pdf) at export time; this
  > copy is produced by rewriting a copy of the original bytes held in
  > memory. The source file on disk is read once at import and never
  > written, moved, or modified.

- 0b. Amend CLAUDE.md §5 "Entity categories": name the internal-names
  category `internal_names`, and note that the user-visible label is
  "Internal".
- 0c. Amend CLAUDE.md §5 "Anonymisation levels": add that levels become
  presets over granular per-category switches (Phase 3), with `medium`
  remaining the default preset.
- 0d. Add the Phase-4 dependency-table rows from §4 above to CLAUDE.md §7.
- 0e. Amend CLAUDE.md §3 repository-structure tree: add
  `engine/exportfmt/` (same-format export package), `static/brand.css`,
  `static/ui.js`, `static/icons.js`, `static/assets/icons/`,
  `static/views/home.js`, `docs/brand/color-palette.json`, `BUILD-02.md`.
- 0f. Record in this file (§2.2 above) the entity-type decision: data-driven
  categories, four types now, extension later. Nothing else to do.

**Unit tests:** none (docs only). Run the full existing suite anyway to
confirm the tree is green at the starting point.

**Definition of done:** CLAUDE.md amendments committed as
`docs: amend CLAUDE.md for BUILD-02 scope (same-format export, internal rename, granular levels)`.

---

## Phase 1 — Brand foundation and UI toolkit

**Goal:** the app looks on-brand, and all later phases build UI from one
small set of shared, tested helpers instead of ad-hoc markup.

**Activities:**

- 1a. Create `static/brand.css` with exactly the token block from §5 above.
  Load it from `index.html` before `style.css`.
- 1b. Rework `static/style.css` to consume only brand tokens: replace
  `--accent: #b3542b` and the current palette; headings switch to
  `var(--font-heading)` regular weight, body/controls to `var(--font-body)`;
  remap the three `--mark-*` highlight tints as specified in §5. Keep the
  monospace preview styling.
- 1c. Create `static/ui.js`: pure string-rendering helpers (same pattern as
  `html.js`, fully testable under `node --test` with no DOM):
  - `button(label, {kind: "primary"|"secondary"|"ghost", id, icon, disabled, title})`
    renders a `<button class="btn btn-primary" ...>`. Primary = white text on
    `--accent` (large bold Arial passes contrast per brand rules); secondary
    = black on `--gray-5` with `--gray-3` border; ghost = borderless.
  - `banner(title, body)` renders the per-step explainer strip: `--gray-6`
    background, black text, an icon slot, no border radius extravagance.
  - `panel(id, title, contentHTML, {collapsible: true, startOpen})` renders a
    `<section class="panel">` with a header row that toggles a
    `data-collapsed` attribute; body max-height transition in CSS. Collapsed
    state is view-local (a `Set` of panel ids per view, same pattern as
    `expanded` in entities.js).
  - `icon(name)` looks up `static/icons.js`.
- 1d. Vendor Material Symbols: download (once, at build-agent time, from
  fonts.google.com/icons) the outlined SVGs needed across the plan, e.g.
  `home`, `upload_file`, `tune`, `badge`, `play_arrow`, `download`,
  `arrow_back`, `arrow_forward`, `add`, `close`, `drag_indicator`, `search`,
  `warning`, `check_circle`, `cancel`, `description`, `smart_toy`,
  `cloud_off`, `expand_more`, `expand_less`. Store each as
  `static/assets/icons/<name>.svg`, plus the Apache-2.0 licence file. Create
  `static/icons.js` exporting a `{name: svgString}` map (checked in, no
  runtime fetch; svgs use `fill="currentColor"` so CSS colours them).
- 1e. Replace the footer `← Back` / `Next →` text controls in
  `static/main.js` (`#nav-back`, `#nav-next`, currently main.js:104) with
  `ui.button` primary (Next, `arrow_forward` icon) and secondary (Back,
  `arrow_back` icon). Same ids, same handlers, so `state.js` nav guards are
  untouched.
- 1f. **Em-dash guard test.** New `static/copy.test.js`: reads every file in
  `static/` matching `*.js`, `*.html` (via `node:fs`, walking the embedded
  source tree, excluding `*.test.js` and `assets/`), and fails listing
  file:line for any occurrence of U+2014 (em dash) or U+2013 (en dash used as
  a dash) inside the file. Add a matching Go test `copy_guard_test.go` that
  scans user-facing string literals in `app*.go`, `engine/`, `ollama/` for
  U+2014. These tests are the permanent enforcement of the style rule.
  (First run will fail on existing copy such as the Ollama tooltip
  "Requires Ollama — not detected…"; fix every hit in this phase, e.g.
  "Requires Ollama, which was not detected on 127.0.0.1:11434".)
- 1g. Application icon. The improvement plan asks for the same generation
  pattern as `Rosca75/pptx-compressor`; that repo is not part of this
  session's sources. Two acceptable routes, in order of preference:
  1. The owner adds `Rosca75/pptx-compressor` to the session (or pastes its
     icon-generation script); copy the pattern verbatim.
  2. Fallback (recorded so the phase is never blocked): add
     `scripts/genicon.go` (a `//go:build ignore` program, run via
     `go run scripts/genicon.go`) that renders a 1024×1024 PNG with the
     stdlib `image` package: white rounded square, a black document glyph
     with three redaction bars, the middle bar in signature orange
     `#FD5108`. Write `build/appicon.png` (Wails v2 picks this up for the
     Windows executable icon). Commit both the script and the generated PNG.
- 1h. Restyle the header step chips and status badges (`bridgeBadge`,
  `ollamaBadge` in main.js) with the token palette: chips in `--gray-5` with
  the active chip's underline (not fill) in `--accent`; badges use status
  colours only for their semantic state.

**Unit tests:**

- `static/ui.test.js`: table-driven over `button`/`banner`/`panel`/`icon`:
  correct classes per kind, `disabled` and `title` attributes present,
  label and title escaped (feed `<script>` and quotes), icon lookup falls
  back to empty string for unknown names, panel renders `data-collapsed`
  per option.
- `static/copy.test.js` and `copy_guard_test.go` as described (these are
  themselves the tests; they must pass with zero hits).
- Existing `state.test.js` / `highlight.test.js` stay green (no logic
  changed).

**Manual checks:** visual pass over all five steps: Georgia headings, Arial
controls, one orange hero element per view, black text everywhere on light
surfaces, focus outlines visible.

**Definition of done:** commit
`feat(ui): brand foundation, shared UI toolkit, copy style guard`.

---

## Phase 2 — Shell: Home landing page, navigation, banners, panels, resizable import panes

**Goal:** the app opens on a Welcome/Home page with clear navigation; every
wizard step explains itself; long scrolls become focused collapsible panels;
the Import step's two panes are user-resizable.

**Activities:**

- 2a. `static/state.js`: add top-level `screen: "home" | "wizard" | "docs"`
  (default `"home"`), with `goToScreen(name)` reducer. The wizard's internal
  `step` state is unchanged. Guard: leaving `wizard` never clears wizard
  state (documents, entities survive navigation to Home and back).
- 2b. `static/views/home.js`: landing page. Georgia headline, short feature
  promotion (three panels: "Anonymise documents", "Everything stays on this
  machine", "Optional local AI"), a primary `ui.button` "Anonymise
  documents" dispatching `goToScreen("wizard")`, a secondary "Documentation"
  entry. One orange hero element (the primary button).
- 2c. `static/views/docs.js`: placeholder page ("Documentation is coming
  soon.") with a back-to-home button. No content, per the improvement plan.
- 2d. `static/main.js`: render a persistent top navigation bar (Home,
  Anonymise documents, Documentation) using `ui.button` ghost kind +
  Material icons; `paint()` branches on `s.screen` before the existing
  step-chip wizard shell. Keyboard shortcuts and Wails event wiring
  unchanged.
- 2e. Step banners: add a `STEP_BANNERS` map (module-level in `main.js` or a
  small `static/copy.js` strings module, preferred so the copy guard and
  future i18n have one home):
  - import: "Add the documents you want to anonymise. You can drag files
    here or browse for them. Your files are only read, never changed."
  - configure: "Choose what kinds of information to hide, and decide whether
    to use the optional local AI."
  - entities: "Tell the app the names it should replace. You can add them
    yourself or let the app suggest candidates for your review."
  - run: "Run the anonymisation and check the result side by side."
  - export: "Save the anonymised copies and, if you need it, the
    re-identification key."
  Render `banner()` at the top of every step view.
- 2f. Convert the long scrolling sections of each existing view into
  `ui.panel` collapsible sections (entities: discovery / per-category tables
  / allowlist / custom patterns; run: controls / rules / results / missed /
  report; export: per-document / bundle / mapping / report / session).
  Default-open the panels a first-time user needs; default-collapse the
  rest (rules, custom patterns, report JSON).
- 2g. Import step resizable panes: in `static/views/import.js`, wrap the
  Documents list and Preview in a two-column grid with a 6px divider
  element; pointer-event drag updates `grid-template-columns` and stores the
  ratio in state (`setImportSplit(ratio)`, clamped 0.2 to 0.8) so it
  survives re-renders. Double-click resets to 50/50.

**Unit tests:**

- `state.test.js` additions: `goToScreen` transitions, wizard state
  preserved across screen changes, `setImportSplit` clamping table
  (0, 0.1, 0.5, 0.95, NaN → clamped/rejected).
- `ui.test.js` additions if `panel` grows options here.
- Copy guard automatically covers all new strings.

**Manual checks:** app boots to Home; nav round-trips Home → wizard →
Documentation without losing imported documents; divider drags and resets;
every step shows its banner.

**Definition of done:** commit
`feat(ui): home landing, top navigation, step banners, collapsible panels, resizable import panes`.

---

## Phase 3 — Engine: granular category model, presets, "internal" rename

**Goal:** the pipeline is driven by an explicit per-category switch set
instead of only a 3-way level; the internal-names category is named
`internal_names` everywhere.

**Activities:**

- 3a. `engine/pipeline.go`: introduce
  `type CategorySelection map[string]bool` covering both PII categories
  (`email`, `url`, `iban`, `vat`, `matricule`, `phone`, `amount`, `date`)
  and entity categories (`client_names`, `project_names`, `internal_names`,
  `person_names`, `custom_patterns`). `Run` consumes the selection: PII
  detection filters `piiPattern` hits by selected category (replacing the
  pure level check), entity passes filter by selected entity categories
  (replacing `levelEntityCategories`).
- 3b. Presets: `PresetSelection(level Level) CategorySelection` reproduces
  today's exact semantics (soft = hard PII + client/project/internal +
  custom; medium = soft + persons; advanced = everything). `Level` stays in
  `Settings` as "last chosen preset" for UI display; the selection is what
  the pipeline obeys. `buildRunRequest` (state.js) sends the selection.
- 3c. Use the internal-names category key `internal_names` and placeholder
  label `INTERNAL` in every occurrence found in the survey:
  `engine/entities.go` (comments + `personCategories`), `engine/registry.go:29`
  (`placeholderLabels`), `engine/pipeline.go:113`, `ollama/client.go`
  (prompt text at lines 263 to 268, 293, 362: category key and the phrase
  "internal staff, teams or internal systems"),
  `static/highlight.js:18` (`ENTITY_LABELS`),
  `static/views/entities.js:25` and `static/views/run.js:168` (label
  "Internal"), plus README.md:36 wording. Update all affected tests.
- 3d. `engine/session.go`: entities and registry rows use the
  `internal_names` category key and `[INTERNAL_N]` placeholder labels
  directly (no legacy key to migrate).
- 3e. `app_run.go` / `app.go`: settings struct gains
  `Categories CategorySelection`; applying a preset from the UI fills it.
  Wails payload shape documented in comments on both sides of the bridge.

**Unit tests (table-driven, `engine/pipeline_test.go` + new
`engine/session_test.go` cases):**

- For each single category enabled alone, a fixture containing all category
  kinds anonymises only that kind (14 rows, one per category).
- Preset equivalence: `PresetSelection(soft|medium|advanced)` produces
  byte-identical pipeline output to the v1 level behaviour on the existing
  fixture corpus (regression anchor).
- Mixed selections: persons on + emails off leaves emails intact; allowlist
  still wins over any selection.
- `state.test.js`: `buildRunRequest` carries the selection; preset reducer
  fills expected switches.

**Definition of done:** commit
`feat(engine): granular category selection with level presets, internal rename, session migration`.

---

## Phase 4 — Engine: allowlist CSV import, template, seeded defaults surfaced

**Goal:** users can import a CSV of never-anonymise terms and download a
template; the engine's seeded allowlist stops being dead code.

**Activities:**

- 4a. `engine/allowlist.go`: `ParseAllowlistCSV(data []byte) ([]string, error)`.
  Tolerant: UTF-8 BOM stripped, CRLF ok, takes column "term" if a header row
  is present else column 1, trims whitespace, drops empties, deduplicates
  case-insensitively, returns an actionable error naming the first bad line
  on malformed CSV. `AllowlistTemplateCSV() []byte` returns a commented
  template (`term` header + three example rows such as `CSSF`, `IFRS 17`,
  `Luxembourg`).
- 4b. Surface the seeded defaults: today `defaultAllowlist` in
  `engine/allowlist.go` is never used at runtime (the pipeline builds
  `NewEmptyAllowlist()` plus UI terms only, `app_run.go:84`). Fix by
  exposing `DefaultAllowlistTerms() []string` and having `app.go` push them
  into frontend `state.allowlist` at startup, so the user sees them, can
  remove any, and the single UI-driven list remains the only runtime source
  (consistent with the review-everything philosophy; nothing silent).
- 4c. `app.go` bindings: `ImportAllowlistCSV()` (Wails open dialog, filter
  `*.csv`, returns parsed terms merged into state via existing
  `addAllowTerm` semantics) and `SaveAllowlistTemplate()` (save dialog,
  writes the template). Both follow the existing `saveWithDialog` /
  cancel-is-a-no-op pattern in `app_export.go`.

**Unit tests (`engine/allowlist_test.go`, table-driven):** header/no-header,
BOM, quoted terms with commas, semicolon-delimited rejected with actionable
message, duplicate case-insensitive collapse, empty file → empty list (not
an error), template parses through its own parser (round-trip).

**Definition of done:** commit
`feat(engine): allowlist CSV import and template, surface seeded defaults`.

---

## Phase 5 — Ollama client: correct error mapping, context size, chunking

**Goal:** an HTTP 400 (context overflow) is never again reported as "model
not installed"; context size is controllable; long documents are chunked
instead of silently truncated.

**Activities:**

- 5a. `ollama/client.go` `Chat` error mapping (currently lines 205 to 254).
  Replace the two-branch mapping with:
  - 404 **with a JSON error body mentioning the model** (Ollama returns
    `{"error":"model '<x>' not found"}`): report "Model %q is not installed;
    run 'ollama pull %s' or pick another model in settings."
  - 404 **without such a body**: keep `ErrTooOld` ("Ollama too old, please
    update").
  - 400: read the body's `error` field and report it verbatim, prefixed
    "Ollama rejected the request (HTTP 400): %s". If the text matches
    context-window phrasing (contains "context" or "length"), append "The
    document chunk was too large for the model's context window; lower the
    chunk size or raise the context size in Configure."
  - Other non-200: report status + body excerpt, no longer guessing about
    installation.
  The message "model may not be installed" must no longer be reachable from
  a 400 (this is the reported bug, `ollama/client.go:242`).
- 5b. Context size: `chatRequest` gains
  `Options struct{ NumCtx int `json:"num_ctx,omitempty"` }`. New client
  field `ContextSize` (default **8192**, replacing the implicit model
  default), plumbed from a new `Settings.ContextSize` through `app.go`.
  Setting 0 omits the option (model default).
- 5c. Chunking: replace the single 24 KiB `clipText` truncation with
  `chunkText(text string, budgetBytes int, overlapBytes int) []string`
  (rune-safe, prefers paragraph then line then space boundaries, overlap
  default 512 bytes). Budget derives from `ContextSize` (approximate 3 bytes
  per token, reserve 25% for the system prompt and reply). `Discover` and
  `DeepScan` loop chunks sequentially, honour `ctx` cancellation between
  chunks, and merge per-chunk proposals through the existing
  `MergeProposals`; the hallucination filter keeps running against the FULL
  document text, not the chunk.
- 5d. Size safeguard: exported `EstimateChunks(text string) int` so callers
  (Phase 7 UI) can warn before a discovery run, and a hard cap (e.g. 64
  chunks) that fails with "This document is very large (N chunks of M KB).
  Split it or run Smart detection instead." rather than running for an hour.

**Unit tests (`ollama/client_test.go`, httptest-based, table-driven):**

- 400 + `{"error":"...context length exceeded..."}` → message contains
  "context", does NOT contain "not installed".
- 404 + model-not-found body → "not installed" message naming the model.
- 404 empty body → `ErrTooOld`.
- `num_ctx` present in the outgoing request body when ContextSize set;
  absent when 0.
- Chunker: empty text → 1 empty chunk handled; text under budget → 1 chunk;
  boundary preference (paragraph split chosen over mid-word); overlap
  present; rune safety on multi-byte French text; chunk-count cap error.
- Discovery across 3 chunks merges and de-duplicates proposals; cancellation
  between chunks stops the loop.

**Definition of done:** commit
`fix(ollama): distinct 400/404 error surfaces, num_ctx setting, document chunking`.

---

## Phase 6 — Configure step rework: two sub-screens, checkboxes, AI toggle, plain copy

**Goal:** Configure becomes two focused sub-screens with granular controls
and plain, professional language; the "use local AI" decision moves here.

**Activities:**

- 6a. Sub-screen tabs inside the configure view (view-local state, same
  pattern as `expanded`): **"What to anonymise"** and **"AI and advanced
  settings"**. The step chip row and nav guards are untouched (still one
  wizard step).
- 6b. "What to anonymise": preset selector (three `ui.button` secondary
  chips: Soft, Standard, Thorough, mapping to soft/medium/advanced) above
  grouped checkboxes bound to `Settings.Categories` (Phase 3):
  - "Contact and account details": emails, phone numbers, bank accounts
    (IBAN), VAT numbers, national ID numbers, web addresses.
  - "Names": clients, projects, internal, persons.
  - "Only for thorough anonymisation": dates, places and organisations,
    money amounts.
  Touching any checkbox switches the preset chip to "Custom". Each checkbox
  carries a one-line example in muted text ("Phone numbers, for example
  +352 621 123 456").
- 6c. Allowlist controls move-in: import CSV button, download template
  button (Phase 4 bindings), and the pill list editor (shared state with
  the entities step; render the same `allowlistPanel` component extracted
  into `static/ui.js` or a shared module so both steps show one list).
- 6d. "AI and advanced settings": new master toggle **"Use local AI
  (Ollama)"** stored as `Settings.UseAI` (default: on when Ollama detected,
  off otherwise; user choice persists in session). Below it: port, model
  select, context size (number input, default 8192, help text "Higher
  values let the AI read longer documents at once but use more memory."),
  re-probe button. All AI-dependent controls elsewhere in the app now gate
  on `UseAI && ollama.available` (entities discovery, run deep-scan).
- 6e. Copy rewrite for the whole step: no "PII" (say "personal details such
  as emails and phone numbers"), no "+", no em dashes, sentences not
  fragments. All strings live in `static/copy.js`.

**Unit tests:** `state.test.js`: `UseAI` reducer and gating helper
(`llmEnabled(state)` used by entities/run views), preset chip → categories
mapping, custom detection (any divergence from preset → "custom"). Copy
guard covers the new strings. Go side: `applySettings` round-trips
`ContextSize`, `UseAI`, `Categories`.

**Manual checks:** with Ollama stopped: AI tab shows toggle off and
disabled-with-tooltip controls; deterministic pipeline fully usable. With
Ollama running but toggle off: discovery/deep-scan hidden (Phase 7/9
behaviour confirmed later).

**Definition of done:** commit
`feat(ui): configure step rework, granular checkboxes, AI master toggle, allowlist import, plain copy`.

---

## Phase 7 — Entities step targeted fixes: variants bug, discovery progress and cancel

**Goal:** the known variants expand/add bug is fixed with regression tests
pinning the contract, and discovery runs show progress and can be cancelled.
This lands BEFORE the big entities rework so the rework inherits tested
behaviour.

**Activities:**

- 7a. Variants data-flow fix in `static/views/entities.js`:
  - Replace the `vrow.previousElementSibling` coupling (entities.js:178,
    fragile: any markup change between the entity row and variant row breaks
    add-variant silently) with explicit `data-key`/`data-category`/
    `data-canonical` attributes ON the variant row itself, written at render
    time.
  - `refreshVariants()` (entities.js:198) currently loops ALL entities and
    re-expands any with empty variants on every render, and swallows
    `expandVariants` errors. Change to: expand only the entity that was
    toggled or edited; distinguish "not yet expanded" (`variants: null`)
    from "expanded, none found" (`variants: []`, render "No variants") so
    the "expanding…" placeholder can no longer stick forever; surface
    expansion errors into a visible inline message with the Go error text.
  - Extract the variant view-model into a pure function
    `variantRows(entities, expandedKeys)` in `static/state.js` (or a new
    `static/entitymodel.js`) returning plain data the view renders. This is
    what regression tests pin.
- 7b. Regression tests (`static/state.test.js` or `entitymodel.test.js`):
  toggle open/closed cycles; add variant appends and forces single-entity
  re-expansion (`variants: null` only for that key); entity with zero
  variants shows the explicit empty state, never a pending placeholder; two
  entities expanded simultaneously keep independent state; editing an entity
  canonical resets only its own variants.
- 7c. Discovery progress: `main.js` subscribes to the already-emitted
  `discovery:progress` event (`app_entities.go:50`, currently emitted with
  NO frontend listener) → store `state.discovery = {running, current, total,
  file}` → entities view renders a determinate progress bar (same component
  as the run step's pipeline bar) with the current filename.
- 7d. Discovery cancel: `app_entities.go` `RunDiscovery` currently uses
  `context.Background()` (app_entities.go:53). Hold a cancellable context on
  `App` exactly like the run pipeline does, add a `CancelDiscovery()`
  binding + `api.js` wrapper + Cancel button next to the progress bar.
  Cancellation between files AND between chunks (Phase 5c already honours
  ctx) returns partial results with a "cancelled after N of M files" status
  line rather than an error.
- 7e. Size safeguard in the UI: before running, call a new
  `EstimateDiscovery(files)` binding (wraps Phase 5d `EstimateChunks`);
  if any file exceeds the cap, show the actionable message and exclude it
  (checkbox turns into a warning row), never a mid-run hard error.

**Unit tests:** the 7b regression suite (this is the heart of the phase);
Go tests: `RunDiscovery` cancellation mid-run returns partial proposals and
no error; oversize estimate produces the documented message; progress events
emitted once per file (assert via the events mock used in existing app
tests).

**Definition of done:** commit
`fix(entities): variant expand/add data flow with regression tests, discovery progress and cancel`.

---

## Phase 8 — Engine: Smart detection tier (pure Go) and span classification

**Goal:** a fully offline heuristic discovery tier that always works without
Ollama, and a candidate-span classification mode that stops sending whole
documents to the model (structurally ending the context-overflow class of
bugs).

**Activities:**

- 8a. New `engine/discover.go` (UI-agnostic, no I/O):
  - `SmartDetect(text string, allow *Allowlist) []Candidate` with
    `Candidate{Text, Category, Count int, Contexts []string}` (up to 3
    context snippets of ±60 runes for the review UI and for LLM
    classification).
  - Detectors, in order: (1) capitalised-run extraction, unicode-aware,
    tolerating French/Dutch particles (de, du, des, van, von, le, la) and
    hyphenated names; runs at sentence starts require a second occurrence
    elsewhere to qualify (kills sentence-case noise). (2) Luxembourg-aware
    legal-suffix gazetteer: `S.A.`, `S.à r.l.`, `Sàrl`, `SARL`, `S.C.A.`,
    `S.C.S.`, `SCSp`, `ASBL`, `SE`, `GmbH`, `AG`, `N.V.`, `B.V.`, `Ltd`,
    `LLC`, `plc`, `SAS`, `S.p.A.` (table-driven, easy to extend); a
    capitalised run followed by a suffix is category `client_names` with
    high confidence. (3) Frequency analysis: runs occurring ≥2 times rank
    higher; single-occurrence runs without a suffix or title cue are
    dropped. (4) Title cues (`Mr`, `Mrs`, `Ms`, `Dr`, `Me`, `M.`, `Mme`)
    → `person_names`.
  - Allowlist veto applied last (allowlist wins, as everywhere).
- 8b. Classification mode: `ollama/client.go` gains
  `ClassifyCandidates(ctx, candidates []Candidate) ([]Proposal, error)`:
  sends ONLY candidate texts + their context snippets (batched to stay far
  under the context budget), strict-JSON prompt asking for one category per
  candidate from the exact category keys, `"format":"json"`. Hallucination
  filter unchanged (every returned text must exist verbatim in the source).
  When `UseAI` is on, discovery becomes: SmartDetect → ClassifyCandidates
  for category refinement; the whole-document `Discover` path remains
  available as the explicit "local GenAI" method (Phase 9) with Phase 5
  chunking.
- 8c. `app_entities.go` binding `RunSmartDetection(files []string)` mirroring
  `RunDiscovery` (progress events, cancellable, allowlist from UI state),
  returning candidates rather than auto-added entities.

**Unit tests (`engine/discover_test.go`, table-driven, English AND French
fixtures per CLAUDE.md §6):**

- Suffix gazetteer: each suffix form recognised; "Acme Solutions S.à r.l."
  → client candidate; suffix alone not a candidate.
- Capitalised runs: multi-word names, hyphenated ("Jean-Pierre Muller"),
  particle names ("Anouk van den Berg"), sentence-start single occurrence
  dropped, repeated sentence-start kept.
- Title cues route to persons ("Mme Weber" → person).
- Frequency: term appearing 3 times reports Count 3 with ≤3 contexts.
- Allowlisted term ("CSSF", "Luxembourg") never emitted.
- Ollama classification: httptest server returns categories; a candidate
  the server "invents" (text not in source) is dropped by the filter;
  batching stays under the byte budget for 200 candidates.

**Definition of done:** commit
`feat(engine): smart detection tier with Luxembourg gazetteer, candidate span classification`.

---

## Phase 9 — Entities step rework: three methods, unified review, live preview, drag-and-drop

**Goal:** the entities step presents three discovery methods, funnels ALL
candidates into one reviewable accept/reject/edit list, previews manual
entries live, and lets users regroup variants by drag-and-drop.

**Activities:**

- 9a. Discovery methods panel (replaces the current single discovery panel):
  1. **Auto-discovery with cloud AI**: rendered but disabled, ghost style,
     caption "Not available yet." Placeholder only, no wiring.
  2. **Auto-discovery with local AI**: the existing Ollama pass (now
     chunked). Visible ONLY when `Settings.UseAI` is true (improvement plan
     §4: hide AI options if AI was not enabled in Configure); additionally
     disabled with the standard tooltip when Ollama is unavailable.
  3. **Smart detection**: always visible, always enabled, caption "Works
     without any AI. Finds likely names by how they are written."
  Shared file-checkbox list, shared progress bar and Cancel (Phase 7).
- 9b. Unified candidate review: new `state.candidates` list (source-tagged:
  smart / local-ai / cloud-ai). Review table with per-row Accept (→
  `addEntities` in the chosen category), Reject (drop), Edit (inline text +
  category select before accepting), and bulk "accept all in category".
  Candidates NEVER flow into `state.entities` without an explicit accept
  (improvement plan: nothing is auto-anonymised without confirmation).
  Note this changes today's behaviour where `RunDiscovery` results are
  added directly as entities; `app_entities.go` now returns proposals to
  the store instead of committing them.
- 9c. Live manual-entry preview: as the user types in a category's "+ add"
  row (debounced 300 ms), call a new `CountTermMatches(term)` binding
  (engine: case-insensitive word-boundary count across loaded docs, reusing
  `isWordBoundary`) and render "Found 4 times in 2 documents" (or "Not
  found in the loaded documents") under the input before they commit.
- 9d. Drag-and-drop variant regrouping: variant chips inside expanded rows
  become draggable (HTML5 DnD, `drag_indicator` icon); dropping onto
  another entity's row moves the variant. The reducer
  `moveVariant(fromCategory, fromCanonical, toCategory, toCanonical,
  variant)` is pure in `state.js` and is the tested unit; the DnD wiring
  only calls it. Keyboard fallback: an "move to…" option in the variant's
  context so the feature is not mouse-only.
- 9e. Category labels now read Clients / Projects / **Internal** / Persons
  (key rename landed in Phase 3; this phase is display + the run.js:168
  dropdown option label).

**Unit tests:** `state.test.js`: candidate reducers (add from source,
accept moves to entities with correct category, reject removes, edit then
accept, bulk accept), `moveVariant` (happy path, no-op when variant absent,
cross-category move re-expands target only, cannot drop onto self);
Go: `CountTermMatches` table-driven (word boundary: "Lux" does not match
"Luxembourg"), candidates returned not committed.

**Manual checks:** UseAI off → only Smart detection and the disabled cloud
placeholder visible; full smart-detection round trip on `testdata/french.md`;
drag a variant between two entities and verify the pipeline uses the new
grouping (placeholder changes accordingly).

**Definition of done:** commit
`feat(entities): three discovery methods, unified candidate review, live match preview, variant drag-and-drop`.

---

## Phase 10 — Run step: hover originals, click-to-reassign

**Goal:** in the anonymised (right) pane, hovering a placeholder shows the
original term; clicking lets the user reassign it as a variant of another
entity on the fly.

**Activities:**

- 10a. Mapping to the frontend: extend the `pipeline:done` payload
  (`app_run.go`) with the registry export (original ↔ placeholder ↔
  category; the data already exists via `registry.Export()`). Store as
  `state.mapping` keyed by placeholder (e.g. `"[CLIENT_1]" → {original,
  category}`). Memory note in comments: this is the re-identification key;
  it stays in the app process exactly like the Go-side registry, and is
  cleared when documents are cleared.
- 10b. `static/highlight.js`: `renderHighlighted(text, mapping)` adds
  `data-ph="[CLIENT_1]"` to each `<mark>` and sets
  `title="Original: <value>"` when the mapping knows the placeholder
  (falling back to today's label-only title). Keep `escapeHTML` on every
  interpolation (originals can contain quotes and angle brackets; the
  existing test style covers this).
- 10c. Styled tooltip: pure CSS (`mark[data-ph]:hover::after` reading a
  `data-original` attribute) so no JS positioning library is needed; native
  `title` retained as accessibility fallback.
- 10d. Click-to-reassign: clicking a mark opens a small inline popover
  anchored to the mark: "**[CLIENT_1]** replaces **Acme S.A.** / variant
  of: [autocomplete input]". The autocomplete searches existing entity
  canonicals across categories (pure filter function in `state.js`,
  tested). Confirming calls `addManualVariant(cat, canonical, original)`
  and then the existing `fastRerun()` (deterministic passes only,
  app_run.go) so the document re-renders with the reassigned placeholder.
  Escape or click-away closes. Only one popover at a time.

**Unit tests:** `highlight.test.js` extensions: data attributes present,
mapping miss falls back cleanly, original containing `"><script>` is inert
in output; `state.test.js`: autocomplete filter (prefix beats substring
match ordering, category labels included), reassignment reducer sequence
leaves entities consistent. Go: `pipeline:done` payload includes mapping
(assert in `app_run_test.go`).

**Manual checks:** hover shows original; reassign "J. Muller" from PERSON_2
to variant-of "Jean Muller" and watch both placeholders unify after fast
re-run.

**Definition of done:** commit
`feat(run): original-term tooltips and inline variant reassignment`.

---

## Phase 11 — Same-format export: docx, pptx, xlsx body text

**Goal:** Export offers "Anonymised copy (.docx/.pptx/.xlsx)" producing a
new file with the source's formatting preserved and all body text passed
through the same pipeline mapping.

**Activities:**

- 11a. New package `engine/exportfmt/` (pure Go: `archive/zip`,
  `encoding/xml`, excelize; NO new deps). Shared plumbing first:
  `rewriteZip(raw []byte, rewriters map[string]func([]byte) ([]byte, error))
  ([]byte, error)` copies every archive entry byte-identically except
  entries with a registered rewriter (this guarantees styles, themes,
  images, and everything untouched survive bit-for-bit).
- 11b. Text-run replacement algorithm (docx `w:t`, pptx `a:t`), documented
  in code with the limitation spelled out:
  1. Parse the part with `encoding/xml` tokens, buffering per paragraph
     (`w:p` / `a:p`).
  2. Concatenate the paragraph's text nodes; record each node's offset
     range.
  3. Run the SAME span machinery as the body pipeline
     (`anonymiseText`-equivalent via a callback into `engine`: deterministic
     passes + registry mapping with `ResolveOverlaps`; the session registry
     is passed in so placeholders match the markdown export exactly).
  4. Splice replacements back per node by offset: a replacement contained in
     one node edits that node; a replacement spanning nodes is written
     wholly into the node where it starts and the covered tails of later
     nodes are emptied. Formatting outside replacements is preserved
     exactly; a spanning replacement adopts its first run's formatting
     (LIMITATION, documented in README and a code comment).
  5. Never touch anything except text-node contents; all other tokens are
     re-emitted verbatim.
- 11c. docx: rewrite `word/document.xml` plus `word/header*.xml`,
  `word/footer*.xml`, `word/footnotes.xml`, `word/endnotes.xml` if present.
  These parts were DROPPED at import (pagination noise), so their text never
  met entity discovery; they still go through the deterministic PII pass and
  the registry mapping, and any hit is counted into the report under a
  `document_extras` warning so the user knows text outside the preview was
  changed.
- 11d. pptx: rewrite every `ppt/slides/slide*.xml` and
  `ppt/notesSlides/notesSlide*.xml`.
- 11e. xlsx: via excelize (already pinned): open from `Document.Raw`, walk
  every sheet's cells (`GetRows` coordinates), apply the mapping/pipeline to
  string cells, `SetCellValue`, save to bytes. Shared strings, styles,
  merged cells survive because excelize preserves them.
- 11f. Wire-up: `engine.ExportExtensions` gains the native extension for
  docx/pptx/xlsx documents (listed after the existing defaults, so `.md`
  stays the first/default until the user picks); `app_export.go` adds
  `ExportSameFormat(filename)` using the existing save-dialog pattern; the
  export view renders the new button per document with a short caption
  "Keeps the original layout. Your source file is not changed."
- 11g. Sub-commits allowed per format (11c, 11d, 11e) if the diff grows
  large; each must be green.

**Unit tests (`engine/exportfmt/*_test.go`, using the existing
`testdata/report.docx`, `deck.pptx`, `workbook.xlsx` fixtures + new
fixtures with known entities):**

- Round-trip integrity: output opens with the EXISTING import converters
  (`convert.DocxToMarkdown` etc.); the re-converted markdown contains every
  expected placeholder and NO occurrence of any original term (assert
  against the registry).
- Byte-identical passthrough: every zip entry without a rewriter is
  bit-identical (hash comparison); zip entry count and names unchanged.
- Spanning replacement: fixture with an entity split across two runs
  (bold mid-name) replaces correctly, document still valid XML.
- xlsx: formulas untouched (value cells only), merged-cell sheet intact,
  numeric cells never stringified.
- Empty mapping → output re-converts to identical markdown as input
  (no-op safety).
- Report counts include `document_extras` hits for a fixture with a header
  containing an email.

**Manual checks (Windows):** open each exported file in Word / PowerPoint /
Excel: no repair prompt, formatting visually intact, placeholders present.

**Definition of done:** commit
`feat(export): same-format anonymised copies for docx, pptx, xlsx`
(or the three sub-commits, last one closing the phase).

---

## Phase 12 — Metadata and filename anonymisation, reviewable, plus README

**Goal:** document properties and the export filename go through the same
mapping, allowlist, and an explicit review step; the README privacy
statement is updated to describe same-format export precisely.

**Activities:**

- 12a. `engine/exportfmt/metadata.go`: extract-and-rewrite for OOXML
  `docProps/core.xml` (title, subject, creator, lastModifiedBy, keywords,
  description), `docProps/app.xml` (Company, Manager), and
  `docProps/custom.xml` string properties. Extraction returns
  `[]MetaField{Part, Name, Value}`; rewriting takes the reviewed
  replacement per field and reuses the Phase 11 `rewriteZip` plumbing.
- 12b. Proposed values: each field's text runs through the deterministic
  pipeline + registry mapping + allowlist (identical code path to body
  text; allowlist wins). Fields with no change are shown but marked
  "unchanged".
- 12c. Review UI in the export step (reuses the Phase 9 candidate-review
  pattern): before the first same-format export of a document, a panel
  lists every metadata field: current value → proposed value, with
  accept/edit/reject per field (reject keeps the original text). The user's
  decisions persist in state per document for the session. Nothing is
  rewritten silently (improvement plan §6.1).
- 12d. Filename: `engine.ExportFileName` gains a same-format variant that
  applies the registry mapping to the base name (case-insensitive, longest
  first, same code as the post-pass); if the result still contains any
  known original it falls back to `document_anon_N.<ext>`. The proposal is
  shown in the metadata review panel (editable) and used as the save
  dialog's default filename; the user can still change it in the dialog.
- 12e. README privacy statement: replace the relevant bullet with wording
  per improvement plan §0.1:

  > The app never modifies your original files. When you choose a
  > same-format export, it writes a new anonymised copy; your source file
  > is left exactly as it was.

  Also update the formats section to mention same-format export and its
  formatting-preservation limitation, and the §6.1 metadata behaviour.

**Unit tests:** metadata extraction on fixtures with authored core/app/custom
props (add `testdata/props.docx` authored via a test helper); rewrite
round-trip re-extracts the reviewed values; allowlisted term in a title
survives; filename mapping table (name with client term → placeholder name;
name that is entirely one entity → generic fallback; unicode names; `#`
sanitisation for xlsx sheet docs preserved); custom.xml with non-string
properties left untouched.

**Definition of done:** commit
`feat(export): metadata and filename anonymisation with review step, README privacy update`.

---

## Phase 13 — PDF same-format export (EXPERIMENTAL)

**Goal:** a same-format PDF option that is honest about its limits, plus PDF
metadata anonymisation. PDF in-place body rewriting is intrinsically
high-risk (content streams, font subsetting, encodings), so this phase
starts with a bounded evaluation and has a recorded fallback, mirroring the
CLAUDE.md precedent for PDF import.

**Activities:**

- 13a. Pin and add pdfcpu (per §4 table; verify Go 1.23 compatibility
  first). Implement metadata rewrite: Info dictionary (Title, Author,
  Subject, Keywords, Creator, Producer) and XMP packet when present, through
  the same MetaField review flow as Phase 12 (the review panel is
  format-agnostic by design).
- 13b. Evaluation spike (timeboxed, outcome recorded in this file as a
  dated note): attempt in-place body text replacement on
  `testdata/textlayer.pdf` via pdfcpu's content-stream APIs. Acceptance
  bar: replaced text renders correctly, non-replaced content pixel-stable,
  file opens in Edge and Acrobat without warnings. Expectation: this bar
  will NOT be met for general PDFs (placeholder strings need glyphs the
  subset font may not contain).
- 13c. Recorded fallback (implement unless 13b passes): **regenerated PDF**.
  Build a new PDF from the anonymised working text via `go-pdf/fpdf`
  (add pin), monospace-free simple layout: title from (anonymised)
  metadata, page-per-source-page where page breaks are known, plain
  paragraphs otherwise. UI caption: "Experimental. Produces a simplified
  layout, not a copy of the original design. Your source file is not
  changed." The export button for PDFs carries the EXPERIMENTAL badge just
  like PDF import does.
- 13d. Body coverage guarantee either way: re-extract text from the produced
  PDF with the existing ledongthuc reader and assert zero occurrences of
  any registry original (this is the test AND a runtime self-check that
  fails the export with an actionable message rather than shipping a leaky
  file).

**Unit tests:** metadata round-trip on `textlayer.pdf` (set fixture Info
fields via pdfcpu in a test helper); fallback generator output re-extracts
with no originals and all expected placeholders; scanned.pdf (no text
layer) rejected for same-format export with the existing scanned-PDF
message; runtime self-check failure path (inject a mapping the generator
ignores → export errors, no file written).

**Definition of done:** commit
`feat(export): experimental same-format PDF export with metadata anonymisation`
plus the dated evaluation note added to this section.

**Evaluation note (2026-07-24, Phase 13b outcome):** in-place PDF body
rewriting was NOT adopted. Placeholder strings need glyphs that subset
fonts frequently lack, and the acceptance bar (replaced text renders
correctly, non-replaced content pixel-stable, opens in Edge and Acrobat
without warnings) cannot be met for general PDFs nor verified headlessly.
The recorded fallback (13c, regenerated simplified-layout PDF) is what
shipped, via go-pdf/fpdf v0.9.0 (pure Go, MIT, go.mod requires only Go
1.20). Consequence for the §4 dependency table: **pdfcpu was evaluated
and NOT added.** Its current release v0.13.0 requires Go 1.25 (our pin
is 1.23.x; v0.11.0 would still fit), but with the in-place path
rejected, pdfcpu's only remaining role (Info/XMP rewrite of the original
bytes) is moot: the regenerated file's Info dictionary is written
directly by fpdf from the reviewed metadata, and the ORIGINAL file's
Info dictionary is extracted with the already-pinned ledongthuc/pdf
reader. One fewer heavy dependency, same reviewed-metadata behaviour.
The runtime leak self-check (13d) re-extracts the produced file with the
ledongthuc reader and fails the export if any registry original
survives, so a leaky file can never ship silently.

---

## Phase 14 — Hardening, manual test matrix, docs, release

**Goal:** everything above verified end-to-end on Windows, documentation
synced, release cut.

**Activities:**

- 14a. Run the FULL manual test matrix (BUILD.md matrix + the additions in
  §6 below) on a Windows machine with and without Ollama installed.
- 14b. Sweep all UI copy once more against the style rules (the automated
  guards catch dashes; a human pass catches jargon and tone).
- 14c. CLAUDE.md final sync: repository tree (§3), any dependency-table
  deltas from Phase 13's evaluation, category list.
- 14d. README: feature list, screenshots refresh (Home page, configure
  sub-screens, candidate review, same-format export), documentation of the
  spanning-replacement formatting limitation and PDF fallback behaviour.
- 14e. Tag and release per release.yml; release notes grouped by the six
  improvement-plan sections.

**Definition of done:** commit `chore: BUILD-02 hardening, docs, release prep`,
tag pushed, CI release artefacts attached.

---

## 6. Manual test matrix additions

Run on Windows. Each row states setup → action → expected.

| # | Scenario | Expected |
|---|---|---|
| M1 | Fresh start, no Ollama | Home page renders branded (Georgia headings, orange primary button); Anonymise → wizard; every step shows its banner |
| M2 | Configure, AI tab, Ollama stopped | UseAI toggle off and controls disabled with plain-language tooltip; deterministic run fully works |
| M3 | Configure, only "emails" checked | Run replaces emails only; names, IBANs untouched; report shows single category |
| M4 | Preset switch after custom | Touch a checkbox (chip shows Custom), click Standard → checkboxes reset to medium semantics |
| M5 | Allowlist CSV import | Template downloads, opens in Excel; re-import of edited template adds terms; allowlisted term never replaced in a run |
| M6 | Oversized document discovery | File over chunk cap shows warning row and is excluded; run proceeds on the rest |
| M7 | Discovery cancel | Cancel mid-run: partial candidates appear, status "cancelled after N of M files", UI responsive |
| M8 | Context overflow surfaced correctly | With a tiny ContextSize forced, discovery reports the context message, NOT "model not installed" |
| M9 | Smart detection, French fixture | `Sàrl`/`S.A.` companies and `Mme`-cued persons appear as candidates with counts; CSSF (allowlisted) absent |
| M10 | Candidate review gate | Nothing reaches the entity tables or the run without explicit Accept |
| M11 | Variants regression | Expand/collapse cycles, add variant while another entity is expanded, zero-variant entity shows "No variants"; drag a variant to another entity and re-run: placeholders unify |
| M12 | Run pane interactions | Hover placeholder → original tooltip; click → reassign as variant → fast re-run updates both occurrences |
| M13 | Same-format docx | Export copy; source file mtime/bytes unchanged; copy opens in Word without repair; formatting intact; no original terms present (search in Word) |
| M14 | Same-format pptx/xlsx | Same as M13 in PowerPoint (incl. speaker notes) and Excel (formulas intact, merged cells intact) |
| M15 | Metadata review | docx with author/company set: review panel lists fields with proposals; reject one → survives in output; accept rest → replaced; filename proposal shown and used as dialog default |
| M16 | PDF experimental path | textlayer.pdf exports (fallback layout), re-opens, contains no originals; scanned.pdf refused with the scanned-PDF message |
| M17 | v1 session load | A session saved before Phase 3 loads: internal entities present under Internal, placeholders continue numbering, re-run stable |
| M18 | Em-dash guard | `node --test` and `go test` fail if an em dash is introduced into UI copy (spot-check by temporarily adding one) |

---

## 7. Traceability: improvement-plan item → phase

| Improvement plan item | Phase |
|---|---|
| §0.1 same-format export decision | 0 (spec), 11-13 (build) |
| §0.2 entity type list | 2.2 decision above; data-driven tables in 3, 9 |
| §1 UX heuristics + branding | 1 (foundation), applied in 2, 6, 9, 10 |
| §1 Welcome/Home + navigation | 2 |
| §1 application icon → /build | 1g |
| §1 Material Symbols icons | 1d |
| §1 styled Next button | 1e |
| §1 no em dashes | 1f (automated guard), 6e, 14b |
| §1 per-step banner | 2e |
| §1 collapsible/resizable panels | 1c, 2f |
| §2 resizable import panes | 2g |
| §3 granular checkboxes | 3 (engine), 6b (UI) |
| §3 allowlist CSV import + template | 4 (engine), 6c (UI) |
| §3 plain-language copy | 2e, 6e, 14b |
| §3 move "use local AI" toggle here | 6d |
| §3 Ollama context bug (400 vs 404, context size, chunking) | 5 |
| §3 split configure into two sub-screens | 6a |
| §4 rename "internal" category → `internal_names` | 3c-3d, 9e |
| §4 AI options only if AI enabled | 6d (gate), 9a (visibility) |
| §4 three discovery methods (cloud placeholder / local / smart) | 8 (engine), 9a (UI) |
| §4 smart detection resolves context overflow via span classification | 8b |
| §4 unified reviewable candidate list | 9b |
| §4 variants bug + regression tests | 7a-7b |
| §4 live preview for manual entries | 9c |
| §4 drag-and-drop variant regrouping | 9d |
| §4 discovery progress + cancel + size safeguards | 7c-7e (uses 5d) |
| §5 hover tooltip original term | 10a-10c |
| §5 click inline reassign with autocomplete | 10d |
| §6 same-format export docx/pptx/xlsx/pdf | 11 (ooxml), 13 (pdf) |
| §6 README privacy statement | 12e |
| §6.1 filename anonymisation | 12d |
| §6.1 OOXML document properties | 12a-12c |
| §6.1 PDF Info + XMP | 13a |
| §6.1 allowlist + review flow for metadata | 12b-12c |

---

## 8. Risks and recorded fallbacks

1. **PDF in-place body rewrite likely infeasible** (font subsetting).
   Recorded fallback: regenerated simplified-layout PDF (Phase 13c), export
   labelled EXPERIMENTAL, runtime leak self-check (13d) so a bad file can
   never ship silently.
2. **Replacements spanning formatted runs collapse to the first run's
   formatting** (Phase 11b). Accepted, documented in README; run-splitting
   with format preservation is a v3 candidate.
3. **`pptx-compressor` icon pattern unavailable in-session.** Fallback
   generator specified (1g route 2) so Phase 1 never blocks; swap in the
   real pattern when provided.
4. **pdfcpu Go-version drift** (may require Go > 1.23). Mitigation: verify
   before adopting, pin the newest compatible release, note it in both
   dependency tables (precedent: ledongthuc/pdf).
5. **Smart-detection false positives** on Title Case prose. Mitigations
   built into 8a (sentence-start second-occurrence rule, suffix/title cues,
   frequency ranking) and, structurally, the Phase 9 review gate: nothing
   is replaced without explicit acceptance.
6. **Session compatibility across the category rename.** Covered by the
   schema-2 migration and fixture test (3d); the migration path is also
   exercised in manual M17.
7. **Behaviour change: discovery results no longer auto-commit to entities**
   (9b). Intentional per the improvement plan; called out in release notes.

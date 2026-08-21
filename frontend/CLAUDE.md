# CLAUDE.md — frontend/ (the doc-anonymiser GUI)

Purpose of this file: it is the **frontend charter**. Claude Code auto-loads
the nearest `CLAUDE.md` up the tree for the files it edits, so when you work
in `frontend/` this file loads and the whole backend spec does not. Keep GUI
prompts scoped to this folder and this charter.

This charter owns the frontend detail. The repo-root `CLAUDE.md` stays
authoritative for cross-cutting product rules (the local-only guarantee, the
anonymisation domain rules, pinned versions). When the two ever conflict on a
cross-cutting rule, the root wins; on frontend specifics, this file is the
detail.

## What this folder is

The entire user interface: vanilla ES2020 modules, **no build step, no npm
dependencies, no framework, no bundler**. Every asset is vendored here and
compiled into the Go binary by `//go:embed all:frontend` in the repo-root
`main.go`. `frontend/package.json` exists only to mark this folder as an
ES-module scope so `node --test` works; it installs nothing.

Because the frontend ships embedded, it must load **nothing** from the
network: no CDN, no remote font, no external stylesheet. Icons are vendored
SVGs under `assets/icons/`. This is the local-only guarantee at the frontend
layer, and `../embed_test.go` fails the build if any embedded asset references
a remote origin.

## The one seam to the backend

The frontend talks to Go through exactly one file, `api.js`, which calls the
Wails bridge at **`window.go.backend.App.<Method>`** (the namespace is
`backend`, not `main`, because the App struct lives in `../backend`). No other
module may touch `window.go` or `window.runtime`. The full method-by-method
contract, argument shapes, resolved return shapes and runtime events lives in
`BRIDGE.md` — read that, not the Go source, when a UI change needs backend
data.

`api.js` degrades gracefully: `bridge()` throws a readable error when the
Wails bridge is absent (page opened in a plain browser), and `onEvent` no-ops
without the runtime. Preserve that behaviour.

## Discipline rules (do not break these)

- **`api.js` is the only bridge caller.** Views and models import wrappers
  from `api.js`; they never reference `window.go` directly.
- **`state.js` is the single source of truth for state.** It holds the store
  and the subscribe/notify mechanism. Views render from state and dispatch
  actions; they do not keep their own parallel state.
- **One view module per screen** under `views/` (home, import, identify,
  anonymise, export, plus the shared allowlist panel). A screen's markup and
  wiring live in its module. Identify is the exception that proves the rule:
  it is one screen with two halves, each big enough to deserve its own file, so
  `identify.js` owns the layout and the footer, `identifyrail.js` the choices
  and `identifyworkspace.js` the Values, Suggestions and patterns.

  A **value card is a fixed-height surface**, and that is a behaviour rather than
  a style. Its height must not depend on how many warnings it carries or how many
  spellings it has: when one card resizes, every card below it moves, the browser
  clamps the list's scroll offset to the shorter content, and the next repaint
  snapshots the clamped value, so the reader's place is lost for good. So the
  warnings are ONE hover icon (`warningPopover`), the evidence and related-values
  notes are ONE info tooltip, and the spellings are ONE line of read-only pills up
  to a character budget with the rest behind "+N more". The full spelling list,
  and every gesture that manages it, lives in the spellings popup. There is no
  show/hide-spellings toggle, because a toggle that changes every card's height is
  the same failure wearing a switch.

  The rail is THREE switchable DETECTION ROUTE sections, not tabs, one per
  MECHANISM, each switch bound to a real settings flag: **Built-in patterns**
  (`useBuiltInPatterns`, on by default, owning the document country, its own
  preset rows and the eight pattern category groups), **Heuristic discovery**
  (`useHeuristicDiscovery`, on by default, owning the name categories, its own
  preset rows and its own strictness block) and **Local LLM discovery** (`useLocalLLM`, off by default).
  There is no cloud route.

  Two SWITCH-LESS panels follow them, carrying `.rail-panel` rather than
  `.rail-section` so a utility panel is never counted as a route: **Detection
  quality**, holding the cross-route match-confidence floor, and **Load profile**.
  The floor governs every route that is on, so it belongs to none of them; its
  header states the live percentage while the panel is folded.

  The workspace has FIVE tabs, and the last two are the same subject split by
  AUTHOR: **Built-in patterns** is read-only and shows what the application's own
  patterns matched the last time detection ran, grouped by signal category and
  keeping the sections of categories that ran and matched nothing; **Custom
  patterns** is where the user writes their own. The read-only one comes first
  because it is the one already running. It carries no accept, no reject and no
  edit on a row, because a built-in pattern produces DIRECT matches: there is
  nothing to review, and a control implying one would lie about what the tab does.
  It exists so the one decision the user does make about those patterns, which
  categories are on, is checkable before the whole batch is anonymised. Its state
  is `state.builtInPatterns`, NULL before the first run: "detection has not run"
  and "the patterns found nothing" are different sentences, and the tab's badge
  shows no count rather than a zero until a run has happened.

  ONE SWITCH, ONE MECHANISM. Each header switch writes ITS OWN settings flag and
  touches nothing else, verified by a wiring test rather than by reading the code.
  A section switch must be the flag it claims to be: a derived section state can
  read "On" while nothing the section names actually runs, and the user has no way
  to tell, so there is no fourth summarising boolean anywhere.

  Signal-based discovery has no section of its own, because it is a set of
  readings OF particular signals and a signal IS one of the pattern categories:
  its readings hang off that category's own row inside Built-in patterns, as a
  drill-down button beside the label with the help icon after it
  (`ui.js signalDrillDown`). Switching Built-in patterns off greys that section's
  checkboxes (`blockDisabled`) and leaves every `.signal-drill` and every reading
  inside it LIVE: signal-based discovery is gated only by
  `signalSuggestionSources` and matches its own evidence, while
  `useBuiltInPatterns` governs only whether the signal itself is replaced.

  `custom_patterns` has no rail switch at all: it is declarative, its editor is
  the workspace's Custom patterns tab, and `state.js ALWAYS_ON_CATEGORIES` keeps
  it on through `adoptCategories` (every adopted category map) and
  `toggleCategory` (which refuses to clear it).

  Anonymise is the SECOND such screen. It carries two tabs, **TEXT** and
  **IMAGE**, over one document selection, because the two answer different
  questions about the same file: what the pipeline replaced in the words, and
  what happens to the PICTURES, which the pipeline never touches. `anonymise.js`
  owns the tab bar (`ui.js tabbar()`), the shared document selector and the
  footer; `anonymiseimages.js` owns everything below the tab bar on the IMAGE
  side. The tab lives in `state.anonymiseTab`, and the IMAGE tab is never HIDDEN
  for a format with no image review: a tab that appears and disappears as the
  user changes file reads as a bug, and the sentence inside it is the answer to
  the question a missing tab would raise.

  A **picture decision is a draft until it is applied**, which is why the
  treatment panel is its own overlay (`.image-panel-layer`) rather than
  `modal.js`'s confirm. It borrows the confirm's mechanism (a backdrop, Escape, a
  click outside) and wires it itself, because `modal.js` is shell-level and
  answers a question, while this is a screen-level editing surface holding
  unsaved state. `wireModal` must keep looking only for `.modal-layer`.

  **An SVG preview is rendered through `<img src="data:image/svg+xml;base64,...">`
  and never inlined into the page as an `<svg>` element.** An `<img>` context
  executes no script; an inlined element does, and the SVG comes out of a client
  document. This is not a style preference, and it applies to the row
  thumbnails and to the treatment panel's preview alike.
- **`nav.js` is the only module that moves the wizard.** Every screen has its
  own footer now, so the step bar and four footers all navigate; the
  backward-reset rule lives in `nav.js` once rather than in five places. It
  also builds the footer itself (`stepFooterHTML`), so a step rename reaches
  all four screens through `copy.js NAV`.
- **`copy.js` is the single home for user-visible strings.** No user-facing
  text is hardcoded in a view. `CATEGORY_LABELS` in `copy.js` must have an
  entry for every engine category (enforced by `../category_parity_test.go`).
- **`ui.js` and `shell.js` are pure markup/string builders** (the shared UI
  toolkit and the application shell): no state, no bridge calls.
- **Pure view-models stay pure and tested**: `valuemodel.js` and
  `suggestionmodel.js` are logic-only and have regression tests.
- **The Configure panel explains itself through TOOLTIPS, never prose.** A
  paragraph under a control is read once and then occupies the panel forever,
  which is what put the controls at its foot out of reach. Every explanation
  goes in `ui.js helpTooltip` beside the label it explains, opening on hover AND
  on keyboard focus. What stays inline is only what CHANGES: a validation error,
  the live confidence value, the per-group active counts, Ollama availability,
  run status. Live read-outs carry `.rail-readout`; `p.hint` inside the rail is
  banned, and both the frontend suite and the rendering harness measure it.

## The fixed-height layout contract (BUILD-05)

Every wizard screen is a **fixed-height two-column card workspace**. This is a
layout contract, not a style preference, and breaking it is visible
immediately as a page that scrolls in two places at once.

- `body` and `#app` are `100vh`. The page body **never** scrolls, neither
  vertically nor horizontally.
- The chrome heights are fixed: header `80px`, step bar `68px`, app footer
  `60px`. What is left is the workspace, and it is the workspace that has to
  fit, not the window that has to grow.
- Scrolling happens **inside a card body** and nowhere else. Every link in the
  chain from `#view` down to the scrolling element needs `min-height: 0`,
  because a flex or grid item's default `min-height: auto` refuses to shrink
  below its content and pushes the whole column taller than the window
  instead.
- Wide content (a markdown table, a long preview line) scrolls inside its own
  `overflow-x: auto` container. It never widens the page.
- **Each screen owns its own footer bar** (`ui.js stepFooter()`): a "Back to X"
  link, a readiness hint, and the primary "CONTINUE TO Y". There is no global
  navigation footer and no per-step explainer banner; the explaining sentence
  is the card's own subtitle.

## No native dialogs (BUILD-05 decision 10)

`confirm()`, `alert()` and `prompt()` must not appear anywhere under
`frontend/`. A native dialog in a WebView is unstyled, unbranded, and on
Windows it steals focus from the window it belongs to.

- A question the user must answer goes through `modal.js askConfirm()`, which
  returns a `Promise<boolean>` and renders in-app.
- A statement the user only has to notice goes through `toast.js` and
  `state.notice`.

## File map

- `index.html` — the single page.
- `main.js` — application shell runtime: top menu, step bar, active view
  switch, startup checks. It renders NO wizard footer: each screen owns its
  own (see the fixed-height layout contract above).
- `shell.js` — pure header/step-bar markup builders.
- `nav.js` — wizard movement, the backward-reset rule, and the shared
  per-screen footer (markup plus wiring).
- `api.js` — THE ONLY bridge caller (see above and `BRIDGE.md`).
- `state.js` — the store; single source of truth for frontend state. It also
  holds the five vocabularies the two sides SHARE, each mirroring an engine
  list and each guarded by `../detection_parity_test.go`:
  `DISCOVERY_METHODS` (provenance: which methods found a Value, a SET),
  `MATCH_CLASSES` (precedence, in order; read only to NAME the winning method in
  an intersection warning, never written onto a Value), `SIGNAL_SOURCES`
  (which built-in signals may derive Suggestions), `SIGNAL_DERIVATIONS`
  (per signal, the READINGS it supports, in display order) and
  `CONFLICT_RESOLUTIONS` (the actions an interface can PERFORM, in one gesture, to
  clear a blocking conflict). A signal's own state
  is DERIVED from its readings by `signalSourceOn`, never stored, for the reason a
  route switch is a real flag: a summary that can disagree with what it summarises
  lies about what a run does. `setSignalSource` is the MASTER that writes them all,
  and `setSignalDerivation` writes one.

  A conflict's resolution is STATED by the engine and read here, never inferred
  from the conflict's refs: the refusal reaches the user on two screens (the
  value's own card on Identify, the refused-run panel on Anonymise) and each
  inferring the fix is two places deciding one thing.

  `state.definedTerms` is the vocabulary the imported DOCUMENTS declare about
  themselves, read by Go at detection time and enforced through the allowlist. It
  is a separate list from `state.allowlist` for the reason the removals are
  separate: deleting a term the user typed is not the same gesture as dropping a
  definition the application read out of a file. It is SHOWN on the
  never-anonymise tab, with the idiom that introduced each term and a per-entry
  remove, because a suppression the user cannot see is one they cannot lift.
- `copy.js` — all user-visible strings + `CATEGORY_LABELS`.
- `ui.js` — shared UI toolkit: the card kit (`card` with its optional `bodyId`,
  `tabbar`, `countBadge`,
  `chipRow`, `sectionLabel`, `statTile`, `collapsibleGroup`, `stepFooter`,
  `toastHTML`, `modalHTML`), the explanation kit (`helpTooltip`,
  `wireHelpTooltips`, `warningPopover`, `wireWarningPopovers`,
  `signalDrillDown`) plus `button` and `icon`. There is
  exactly ONE way to draw each thing: `card` is the fixed-height surface, and
  `bodyId` is an OPTION on it rather than a second builder, for the screens whose
  card body is the scroll owner: `scroll.js` keys a preserved offset on a selector
  that has to survive the repaint, so the body needs an id of its own.
  `collapsibleGroup` the foldable block, `helpTooltip` the explanation,
  `signalDrillDown` a nested set of switches hung on the row that raises the
  question, spending no permanent row of its own (a master over its readings, the
  master derived for display and never stored). A second builder for the same
  control is the next inconsistency, so a replaced one is DELETED, not kept.

  `warningPopover` is the ONE admitted exception, and it is an exception because
  it is not the same control: a hover surface holding BUTTONS has a different
  contract from one holding a sentence. A tooltip may vanish the instant the
  pointer leaves its trigger, because nothing in it is worth reaching; a surface
  with actions in it must survive the pointer travelling into it and must be
  dismissible three ways. Both share ONE positioning model (`placeBubble`, writing
  `--bubble-x` / `--bubble-y`), because the reason for `position: fixed` is the
  same for both: every panel they open inside is a clipping ancestor.
- `html.js` — tiny shared HTML helpers (`escapeHTML`).
- `icons.js` — vendored Material Symbols SVG map. It holds exactly the icons
  the interface draws, no more and no fewer: `ui.js icon(name)` returns the
  EMPTY STRING for a name it does not know, so a missing glyph is a control
  that renders with nothing in it and fails no test. `../icon_parity_test.go`
  holds the map, the call sites and `assets/icons/*.svg` to each other.
- `highlight.js` — renders placeholders as category-coloured `<mark>` with
  hover tooltips, and optionally the Compare search's hits (fourth argument).
  A hit STRADDLING a mark boundary is deliberately not highlighted: splitting
  the mark would break the click-to-select and the tooltip contract.
- `panesearch.js` — the Compare search's pure half: `findHits` over the PLAIN
  pane text, and `escapeWithHits`, THE one definition of "escaped text with the
  search highlighted": one stretch escaped, with whatever hits fall entirely
  inside it wrapped. Hits are
  never applied to already-rendered HTML, because the panes are full of elements
  and escaped entities and a needle like `mark` or `&` would corrupt them; they
  are emitted during the same pass that escapes the text, which is why
  `highlight.js` and `valuespans.js` both hand their stretches to
  `escapeWithHits` instead. Two spellings of that loop would mean the navigation
  could step to a hit one pane does not tint.
- `valuespans.js` — the hover link BETWEEN the two Compare panes: which
  stretches of the ORIGINAL text each placeholder replaced, and the renderer
  that wraps them. One placeholder stands for a whole family (a mainText value
  and its spellings), so the tint is what answers "what did `[PERSON_1]` used to
  be" when the tooltip can only name the one form under the pointer. The
  spellings come from the RUN (`ResultDocument.occurrenceSpellings`), never from
  re-deriving the Value's expansion: a derivation done twice can disagree with
  itself, and the recorded list is what the pipeline actually matched. Spans
  respect WORD BOUNDARIES and give an overlap to the LONGEST claim, because a
  tint claiming a replacement the engine would never make is worse than no
  tint.
- `valuemodel.js` — pure spelling-expansion view-model (regression-tested).
  Three states are distinct and must stay distinct: `derivedSpellings` null means
  an expansion is in flight, `[]` means it finished and found none, an error
  means it failed.

  It owns BOTH halves of the sentinel invariant, which is what makes the
  invariant checkable: `pendingExpansions` reads it and `repend` writes it. A
  CURATED row's amended sentinel is `[]`, never `null`. `pendingExpansions`
  deliberately skips curated rows (a curated row's chips ARE its list, so there
  is nothing to derive), so writing `null` onto one means no expansion is ever
  requested and nothing ever clears it: the card claims to be working forever
  over chips that are already correct. Every writer that changes what a Value
  MATCHES goes through `repend`; the fix is never to derive for a curated row,
  because that would let a deleted spelling come straight back.
- `suggestionmodel.js` — pure Suggestions filter/sort view-model (search,
  category, discovery method, count sort).
- `countries.js` — the document-country table, MIRRORING the engine's
  `backend/engine/country.go` exactly as `PRESETS` mirrors `engine.AllPresets`:
  the per-country example strings for the phone / VAT / national-identification
  labels and which categories each country switches on.
  Since BUILD-06 Phase 1 the country is a real ENGINE setting, not a display
  choice (superseding BUILD-05 decision 2): it decides which country-specific
  regexes run. It stays an ORTHOGONAL axis to the presets, and
  `COUNTRY_ID_CATEGORIES` is what makes that work: `applyPreset` masks the
  national identifiers by the document country and `activePreset` expects them
  masked, so picking Standard on a Luxembourg document does not read as
  "Custom". The engine applies the SAME mask over the same list
  (`engine.CountryIDCategories`), or the rail and the run report would name
  different presets for one selection.
- `toast.js` — the state-backed notice strip (`state.notice`).
- `modal.js` — the in-app confirm, returning `Promise<boolean>`.
- `scroll.js` — scroll-position preservation across re-renders.
- `brand.css` — brand tokens (colours, typography); the single source of
  truth for brand values.
- `style.css` — all layout/component styling; consumes `brand.css` variables
  only.
- `views/` — one module per wizard screen (Identify has three and Anonymise two,
  see the discipline rules) + the shared `allowlist.js` panel. `anonymise.js`
  holds step 3's tabs, its document selector, its footer and the TEXT half;
  `anonymiseimages.js` holds the IMAGE half: the banner, the picture list in its
  two views, and the treatment panel.
- `docs/` — the bundled offline user documentation, opened in a SECOND window
  (embedded assets only; see the documentation-window rule below).
- `assets/icons/` — vendored Material Symbols SVGs + their LICENSE.
- `*.test.js` — dev-time tests, run with `node --test "frontend/**/*.test.js"`
  (zero npm deps). They are self-relative and never shipped in the binary.
- `testhtml.js` — dev-time only: a tiny dependency-free HTML query helper
  (`one`, `all`, `textOf`, `attr`) so a test can assert what a pane SHOWS
  rather than that the output contains a substring. Views build HTML strings,
  so exporting a builder (`previewBody`, `compareCard`, …) is all it takes to
  test a whole screen without a browser.
- `testdom.js` — dev-time only: `testhtml.js`'s wiring counterpart, a minimal
  DOM (`container`, `fire`) whose parser LOWER-CASES attribute names exactly as
  a browser's does. A render test reads the string a view wrote; a wiring test
  has to read what a parser made of it, which is why a camel-case `data-`
  attribute is invisible to the first kind and fatal in the second. Use it
  whenever the assertion is about what a control DOES, not what it shows.

## Typography and brand (BUILD-04 CR2)

- **Helvetica, with Arial as the fallback**, for headings AND body text.
  Both are Windows system fonts, so no font files and no local-only impact.
- **Georgia is a PowerPoint-only brand guideline and must NOT appear anywhere
  under `frontend/`.**
- `--font-heading` in `brand.css` is the single place the heading face is
  declared.
- Headings stay at **regular weight**: hierarchy comes from size and space,
  never bold.

## Comment rules

Comments explain intent, never change history (root `CLAUDE.md` §6). No phase
numbers, no change-request numbers, no "this used to be a table", and no
tombstone blocks for deleted functions. Where a past mistake explains a rule,
state the rule and the failure it prevents, in the present tense.

## Copy rules

- **No em dashes (U+2014)** in any user-visible string. Enforced on the JS
  side by `copy.test.js` and on the Go side by `../copy_guard_test.go`.
- Use commas, periods or parentheses instead.
- All user-visible strings are English (i18n deferred).

## The documentation window (BUILD-04 CR6)

The "Documentation" menu entry opens a SEPARATE window whose content comes
from `frontend/docs/*`, embedded by the same `go:embed` directive. It may load
NOTHING but embedded assets: no `http(s)://` URL, no CDN, no system-browser
hand-off. Go owns the path (`../backend/app.go DocumentationURL`) and the
frontend opens it with a named `window.open` on the app's own asset server
(`api.js openDocumentation`). Do NOT convert this to
`runtime.BrowserOpenURL`: the system browser cannot reach embedded assets and
it would break the local-only guarantee. Wails v2 drives one native window per
process; this second window is one the WebView opens itself.

## Testing

Testing conventions, tiers and commands are defined in `../docs/TESTING.md`.
Read it before writing or running any test. It owns the frontend suite command,
the "a change is not finished until its tests move with it" discipline, the
`../frontend_tests_test.go` mechanical guard (inside `go test ./...`),
render-tests-over-substring, and the three UI layers.

One rule belongs here because it is about `api.js`, not testing: the CDP
rendering layer serves this folder as static files, so **there is no Go bridge**
in it. `api.js` must therefore degrade as a REJECTION, not a synchronous throw:
every wrapper is `async` for that reason, and `api.test.js` pins it. A view that
calls `api.js` while rendering must tolerate the rejection.

## Domain vocabulary

The frontend uses the same words as the engine, and the words are the contract.

| Term | In state | Meaning |
|---|---|---|
| **Suggestion** | `state.suggestions` | an UNREVIEWED potential Value. One list for every method: a row says which methods found it, so there is no per-route mapping step for a field to fall out of. |
| **Value** | `state.values` | an accepted replacement unit: one placeholder, one family of spellings. |
| **Main text** | `mainText` | the primary form naming a Value. Never duplicated in `spellings`. |
| **Spelling** | `spellings` | an alternative form of the same Value. `derivedSpellings` is the separate cache of what Go derived. |
| **Spelling policy** | `spellingPolicy` | `"automatic"` or `"curated"`. Curated means the chips ARE the list. |
| **Defined term** | `state.definedTerms` | a phrase the imported DOCUMENTS declare as their own vocabulary, suppressed through the allowlist and listed with its provenance. |
| **Discovery method** | `discoveryMethods` | provenance, a set. |
| **Evidence** | `evidence` | structured, bounded reasons a method produced a row. The SENTENCE is built in `copy.js`, never returned as prose by the engine. |

Retired names must not come back, and two guards say so mechanically:
`../value_shape_test.go` sweeps this folder for `excludedVariants`,
`manualVariants`, `autoExpand`, `canonical` and `origin` as object keys, and
`../detection_parity_test.go` holds the five shared lists to the engine's.

## Where to look next

- Backend data surface for the UI: `BRIDGE.md` (same folder).
- Product/domain rules, the preset model, pinned versions: repo-root
  `CLAUDE.md`.
- Backend internals (engine passes, converters, Ollama): `../backend/CLAUDE.md`.

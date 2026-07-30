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
  (country, preset, categories, confidence, local AI) and
  `identifyworkspace.js` the values, suggestions and patterns.
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
- **Pure view-models stay pure and tested**: `entitymodel.js`,
  `candidatemodel.js` are logic-only and have regression tests.

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
- `state.js` — the store; single source of truth for frontend state.
- `copy.js` — all user-visible strings + `CATEGORY_LABELS`.
- `ui.js` — shared UI toolkit: the card kit (`card`, `tabbar`, `countBadge`,
  `chipRow`, `sectionLabel`, `statTile`, `collapsibleGroup`, `stepFooter`,
  `toastHTML`, `modalHTML`) plus `button` and `icon`. There is exactly ONE way
  to draw each thing: `card` is the fixed-height surface, `collapsibleGroup` the
  foldable block. The BUILD-02 `panel()` did both at once and is gone.
- `html.js` — tiny shared HTML helpers (`escapeHTML`).
- `icons.js` — vendored Material Symbols SVG map.
- `highlight.js` — renders placeholders as category-coloured `<mark>` with
  hover tooltips.
- `entitymodel.js` — pure variant view-model (regression-tested).
- `candidatemodel.js` — pure suggestions filter/sort view-model.
- `countries.js` — pure document-country table: the per-country example
  strings for the phone / VAT / national-identification labels and the three
  country-specific ID categories. Frontend only; there is no locale-aware
  engine behind it (BUILD-05 decision 2). The country is an ORTHOGONAL axis to
  the preset: `applyPreset` re-applies it, and `selectionPresetName` excludes
  the three country-driven categories from its comparison, so picking Standard
  on a Luxembourg document does not read as "Custom".
- `toast.js` — the state-backed notice strip (`state.notice`).
- `modal.js` — the in-app confirm, returning `Promise<boolean>`.
- `scroll.js` — scroll-position preservation across re-renders.
- `brand.css` — brand tokens (colours, typography); the single source of
  truth for brand values.
- `style.css` — all layout/component styling; consumes `brand.css` variables
  only.
- `views/` — one module per wizard screen (Identify has three, see the
  discipline rules) + the shared `allowlist.js` panel.
- `docs/` — the bundled offline user documentation, opened in a SECOND window
  (embedded assets only; see the documentation-window rule below).
- `assets/icons/` — vendored Material Symbols SVGs + their LICENSE.
- `*.test.js` — dev-time tests, run with `node --test frontend/*.test.js`
  (zero npm deps). They are self-relative and never shipped in the binary.

## Typography and brand (BUILD-04 CR2)

- **Helvetica, with Arial as the fallback**, for headings AND body text.
  Both are Windows system fonts, so no font files and no local-only impact.
- **Georgia is a PowerPoint-only brand guideline and must NOT appear anywhere
  under `frontend/`.**
- `--font-heading` in `brand.css` is the single place the heading face is
  declared.
- Headings stay at **regular weight**: hierarchy comes from size and space,
  never bold.

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

- `node --test frontend/*.test.js` — store, view-model and copy-guard tests,
  zero npm dependencies (Node ships on the CI runner).
- Keep the pure view-models (`entitymodel.js`, `candidatemodel.js`) covered
  by table-style tests when you change their logic.

## Where to look next

- Backend data surface for the UI: `BRIDGE.md` (same folder).
- Product/domain rules, anonymisation levels, pinned versions: repo-root
  `CLAUDE.md`.
- Backend internals (engine passes, converters, Ollama): `../backend/CLAUDE.md`.

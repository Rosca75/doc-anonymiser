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
- **One view module per screen** under `views/` (home, import, configure,
  values, run, export, plus the shared allowlist panel). A screen's markup and
  wiring live in its module.
- **`copy.js` is the single home for user-visible strings.** No user-facing
  text is hardcoded in a view. `CATEGORY_LABELS` in `copy.js` must have an
  entry for every engine category (enforced by `../category_parity_test.go`).
- **`ui.js` and `shell.js` are pure markup/string builders** (the shared UI
  toolkit and the application shell): no state, no bridge calls.
- **Pure view-models stay pure and tested**: `entitymodel.js`,
  `candidatemodel.js` are logic-only and have regression tests.

## File map

- `index.html` — the single page.
- `main.js` — application shell runtime: top menu, workflow banner, active
  view switch, nav footer, startup checks.
- `shell.js` — pure header/workflow-banner markup builders.
- `api.js` — THE ONLY bridge caller (see above and `BRIDGE.md`).
- `state.js` — the store; single source of truth for frontend state.
- `copy.js` — all user-visible strings + `CATEGORY_LABELS`.
- `ui.js` — shared UI toolkit (button/banner/panel/icon string builders).
- `html.js` — tiny shared HTML helpers (`escapeHTML`).
- `icons.js` — vendored Material Symbols SVG map.
- `highlight.js` — renders placeholders as category-coloured `<mark>` with
  hover tooltips.
- `entitymodel.js` — pure variant view-model (regression-tested).
- `candidatemodel.js` — pure suggestions filter/sort view-model.
- `scroll.js` — scroll-position preservation across re-renders.
- `brand.css` — brand tokens (colours, typography); the single source of
  truth for brand values.
- `style.css` — all layout/component styling; consumes `brand.css` variables
  only.
- `views/` — one module per wizard screen + the shared `allowlist.js` panel.
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

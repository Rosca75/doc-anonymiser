# UI testing

Three layers, cheapest first. Layers 1 and 2 run in CI on every push; layer 3
(the render harness) runs on every push that TOUCHES THE UI (path-filtered,
because it serves the static frontend with no Go bridge, so a backend-only
change cannot alter what it renders). There is also an additional platform check
on Windows that is opt-in. Tiers, triggers and commands are defined in
`TESTING.md`; this file owns the UI-layer detail.

The reason there are three: every one of the seven issues reported against the
built application passed the existing tests. Unit tests proved each function;
nothing proved the assembled screen, and nothing at all proved what the user
could actually SEE.

| Layer | What it proves | Command | Gates |
|---|---|---|---|
| 1 | behaviour over a whole session, through the bound app | `go test ./...` | yes |
| 2 | the HTML each view builds, no browser | `node --test "frontend/**/*.test.js"` | yes |
| 3 | what a renderer shows: fit, visibility, clipping | `go run ./scripts/uitest/renderharness` | yes |
| + | the real WebView2 engine and the packaged `.exe` | `pwsh scripts/uitest/Invoke-UITest.ps1` | no, unverified |

## Layer 1: Go, end to end through the bound app

```
go test ./...
```

`backend/app_e2e_test.go` and `backend/app_detect_test.go` drive the same
methods the frontend calls (`App.emit` no-ops while `a.ctx` is nil, so no Wails
runtime is needed): import, detect, run, export, over the fixtures in
`backend/testdata/`.

This layer owns the assertions about behaviour over a whole session:

- the source text is byte-identical after two runs, a fast re-run and an export
  (reported issues 1 and 4);
- a detection run emits exactly one terminal event and a non-decreasing
  progress fraction, survives a file the model choked on, and reports a
  cancellation as a cancellation (issue 2);
- no retired category identifier is producible (issue 5);
- the report's value table sums to its category totals (issue 7);
- a session file round-trips every setting it claims to persist.

It also owns `../uitest_parity_test.go`, the guard that keeps the two layer-3
harnesses sharing one set of probes (see "One set of probes" below). That is a
source-inspection test, so it needs no browser and runs here rather than in
layer 3.

## Layer 2: frontend render tests, no browser

```
node --test "frontend/**/*.test.js"
```

The view modules build HTML strings, so a whole screen is testable without a
DOM. `frontend/testhtml.js` is a small dependency-free query helper (`one`,
`all`, `textOf`, `attr`, `exists`) so a test asserts **what a pane shows**
rather than that the output contains a substring somewhere:

```js
assert.equal(textOf(compareCard(state, doc), "#original-pane"), SOURCE);
```

To bring a new screen under test, export its builder (`previewBody`,
`compareCard`, `reportCard`, `railBody`, `progressStrip` are already exported)
and assert against it.

A string, though, is what the view WROTE, and the browser answers with what its
parser MADE of it: attribute names come back lower-cased, so a camel-case
`data-` attribute is unreachable as `dataset.x` while every string assertion
about it passes. When the question is what a control DOES rather than what it
shows, render into `frontend/testdom.js`'s `container()` and drive it with
`fire()`: its parser lower-cases attribute names too, so the handler fails here
for the reason it fails in the application. `frontend/identifyactions.test.js`
is the worked example, and `../dataset_parity_test.go` is the permanent guard.

A second silence has the same shape: `ui.js icon(name)` returns the EMPTY STRING
for a name absent from `frontend/icons.js ICONS`, so a control renders with no
glyph and every test asserting the wrapper element still passes. That is how
every help tooltip in the application became an invisible hit area.
`../icon_parity_test.go` holds the two lists to each other in both directions,
and the harness's `helpTooltipVisibility()` probe measures the trigger and its
glyph in pixels, because "an svg is in the DOM" and "the user can see it" are
different claims.

### These tests move with the code, in the same change

A frontend test that passes while asserting what a view USED to render is worse
than no test: it is read as evidence. So when anything under `frontend/` changes,
the same change updates the tests that pinned the old behaviour, adds a test for
the new behaviour, and deletes the tests for behaviour that is gone. Never
weaken an assertion to make it pass. The full rule is in `frontend/CLAUDE.md`
under Testing, and the cross-cutting version is root `CLAUDE.md` section 6.

`../frontend_tests_test.go` enforces the parts a machine can see, inside
`go test ./...`:

- **every test file is actually run** by the command in `ci.yml`. It expands that
  exact pattern and compares it against the files on disk. This is not
  hypothetical: the flat `frontend/*.test.js` pattern silently skipped everything
  under `frontend/views/`, so a deliberately failing `views/probe.test.js` was
  never picked up and the suite still reported 350 passing. The pattern is now
  recursive AND quoted, the quotes because bash's globstar is off by default and
  would collapse `**` back to `*`;
- **no test is `.skip`, `.todo` or `.only`.** A skipped test still exits 0, so
  the suite reports success for a check nobody is making;
- **every module with logic is imported by some test**, or listed in an exemption
  table with a reason. When this check first ran it named `nav.js`, `modal.js` and
  `toast.js`; all three got tests (`nav.test.js`, `notices.test.js`) rather than
  exemptions, because an exemption is for a module where a test would assert
  nothing, not for one nobody has got round to;
- **the command in the charters matches the one in `ci.yml`**, since a charter is
  what an agent follows and a stale command there outlives a correct workflow.

What no guard can check is whether a test asserts the RIGHT thing. That stays a
judgement, which is why the rule is written down where it will be read.

## Layer 3: real rendering, Linux, in CI

```
go run ./scripts/uitest/renderharness
go run ./scripts/uitest/renderharness -keep-artifacts
```

Layer 2 proves the HTML. It cannot prove that a tooltip is **visible** rather
than clipped by the pane it sits in, which is exactly what reported issue 6
turned out to be: the markup had been correct for months and
`.pane-body { overflow: auto }` was eating it. Nor can it prove that a screen
fits the window, which is the fixed-height layout contract in
`frontend/CLAUDE.md`.

### How it works

1. `frontend/` is served over loopback HTTP on an ephemeral port, in-process.
   `index.html` uses only relative paths and the modules import with absolute
   paths (`/state.js`), so a server rooted at `frontend/` serves the application
   exactly as the embedded asset server does.
2. A headless Chromium is started with `--remote-debugging-port=0`; the port it
   picks is read from `DevToolsActivePort` in a throwaway profile, so two
   parallel runs never collide.
3. The harness speaks the DevTools Protocol over a WebSocket and evaluates the
   shared probes in the page.

Nothing is installed and no test hook ships in the binary. It adds **no
dependency**: `scripts/uitest/renderharness/ws.go` is a deliberately minimal
RFC 6455 client (masked client text frames, reassembled server frames, ping and
close), because Go has no WebSocket in its standard library and a test harness is
a poor reason to put one in `go.mod` forever. It is not a general-purpose library
and must not grow into one; if it ever needs binary frames or compression, that is
the signal to add a real dependency to the pinned table in the root `CLAUDE.md`.

The viewport is pinned to 1440x900 with `Emulation.setDeviceMetricsOverride`.
`--window-size` is only a request (the headless window subtracts its own chrome,
and asking for 900 produced 708), and a layout contract measured against an
unknown height is not a contract.

### What it asserts

- **the fixed-height layout contract**, on all four wizard steps, in three parts,
  because the contract has three failure modes:
  - the page body does not scroll, in either direction;
  - `#view` does not **clip** what it holds. `main#view { overflow: hidden }` is
    load-bearing, and its cost is that a card which does not fit is silently cut
    off instead of scrolling the page, which the first check would never see;
  - every element that actually scrolls is a card body, a group body, the rail, a
    card column or a `.table-scroll`. That allow-list is the contract's real
    enumeration and lives in `probes.js`; add to it only with the `style.css` rule
    that justifies the entry.
- **the Import preview's rendered `innerText` contains no placeholder** matching
  `\[[A-Z][A-Z0-9_]*_\d+\]` (issues 1 and 4). The seeded state HAS a finished run
  in it, so there is real anonymised text for a view to reach for by mistake; a
  state with no placeholders anywhere would pass for the wrong reason. The pane is
  also asserted to be non-empty, for the same reason.
- **the Configure rail is the three route sections**, no `[data-railtab]` chips,
  Built-in patterns and Heuristic discovery on, Local LLM discovery off, the two
  switch-less panels below them counted as `.rail-panel` rather than as routes,
  each route's title and switch measured on ONE row with the title unclipped, and
  every switchable category checkbox present **and laid out with a non-zero
  height** (issue 3). Present in the DOM is not the same as reachable. The count
  is an EQUALITY against the fixture, not a floor: with a floor, adding a category
  and leaving the fixture behind keeps the harness green. `custom_patterns` is
  outside that count on purpose: it is declarative, permanently on, and has no
  switch in the rail at all.
- **the Configure panel spends 0px on prose, keeps its explanations, does not
  make the page scroll, and has a reachable foot.** Prose is measured in PIXELS
  rather than counted by class, because a paragraph given a different class would
  pass a class count and still occupy the panel. The foot is measured by scrolling
  the panel to its end and checking the last section is painted, which is the
  property that actually failed when every control carried a paragraph.
- **a help tooltip opens on hover AND on keyboard focus, is painted rather than
  clipped, and closes on leave and on Escape.** The bubble is positioned outside
  the rail's clipping scroll container, and the only way to see that it worked is
  a hit test at the bubble's own coordinates: the rect of a clipped element is
  still a full-size rect. The keyboard path is driven too, because an explanation
  only a pointer can reach is one half the users never get.
- **a value card keeps its HEIGHT through a spelling edit and a warning
  appearing, and the list keeps its scroll position.** This is the one the
  cheaper layers structurally cannot make: the markup was correct at every step.
  `scroll.js` restores a raw pixel offset, which is right only while the content
  is the same height; a card that shrank made the browser clamp the restored
  offset, and the next repaint snapshotted the clamped value. Deleting a card
  legitimately shortens the list, so there the assertion is that the offset moves
  by **at most one card height**, never to the top.
- **the spellings popup opens, is painted rather than clipped, scrolls inside
  itself, and updates the card behind it live.** The card shows only the
  spellings that fit one line, so the popup is the only way to reach the rest: a
  surface that is in the DOM but clipped, or one that grows past the window
  because its list does not scroll, makes them unreachable while every string
  test stays green. Painted is checked with `elementFromPoint` at the popup's own
  centre, for the reason the tooltip check gives: the rect of a clipped element
  is still a full-size rect.
- **a real `mouseenter` on a `mark[data-original]` produces a visible
  `#mark-tooltip`** (issue 6). Three marks are hovered: the first, and the two
  nearest the pane's right and bottom edges, because the middle of the pane was
  never where it failed. Each is checked for a rect inside `#compare-card` and
  inside the viewport, for a non-zero size, for **not being a descendant of
  `#anonymised-pane`** (that subtree is what `overflow: auto` clips, and moving the
  tooltip out of it is the whole fix), and for `elementFromPoint` at its centre
  returning the tooltip. That last one is the check with real teeth: the rect of a
  clipped element is still a full-size rect.
- **a real `mouseenter` on a `mark[data-ph]` tints, in `#original-pane`, every
  stretch that placeholder replaced.** The seed gives one Value two spellings
  precisely so this can be asserted: the check fails unless the whole family
  lights up and nothing belonging to another Value does. The teeth are in the
  colour, read with `getComputedStyle` and compared against an untinted span's:
  a class nothing styles, or one styled from a token `brand.css` does not
  define, leaves the markup, the wiring and the string suite all correct and the
  pane visibly unchanged. The span is also checked for a size, for sitting inside
  the pane's visible box after being scrolled to, and for `elementFromPoint` at
  its centre, because a tint painted under something else is not a tint.
- **the IMAGE half's list is the scroll owner, and a picture row keeps its
  height whatever it has to say.** `imageTabGeometry()` seeds a forty-picture
  inventory, one of which appears in five places and one in a single place, and
  measures both views: the tiles and the seven-column details grid. In each it
  checks that the list scrolls and the PAGE does not, in either direction, that
  the shared-picture card and the single-place card are exactly the same height,
  that the seven headings render in order, that the banner sits OUTSIDE the
  scrolling element so the filter stays reachable from the bottom of the list,
  that the filter chips carry their live counts, and that the card is inside the
  viewport. The pair is the check the cheaper layers structurally cannot make: a
  card that grows when it has more to say moves every card below it, the browser
  clamps the list's restored scroll offset to the shorter content, and the next
  repaint snapshots the clamped value. There is no bridge here, so every preview
  request rejects and the cells read "No preview": that is the point rather than a
  limitation, because a placeholder cell must reserve the space a picture would or
  the list reflows the moment a thumbnail lands.
- **no console error** during the run, collected from
  `Runtime.consoleAPICalled` and `Runtime.exceptionThrown` as they happen.

### What it does NOT cover: the bridge

The page is served as static files, so **`window.go` does not exist**. Every
`api.js` call rejects with the readable "Wails bridge not available" error
`api.js` is designed to throw, and application state is seeded straight into
`state.js` instead. This layer covers **rendering, not the bridge**.

That is a real boundary, not a caveat to wave away: the bridge belongs to layer 1
(`backend/app_e2e_test.go`, which drives the bound methods directly) and to the
Windows `wails dev` harness, where the bridge really is attached. The harness
filters exactly one message out of the console assertion, api.js's own bridge
wording, and nothing else, so a genuine error is never waved through.

Running with no bridge at all was worth doing for its own sake: it is what found
that the `api.js` wrappers threw **synchronously** when the bridge was missing, so
the `.catch()` every call site writes was dead code and `views/export.js`
`ensureFormats()` took the whole Export screen down on an uncaught error. The
wrappers are `async` now and `frontend/api.test.js` pins it.

## Additional platform check: Windows, WebView2 and the packaged .exe

```powershell
pwsh scripts/uitest/Invoke-UITest.ps1            # dev mode
pwsh scripts/uitest/Invoke-UITest.ps1 -Packaged  # the built .exe
```

Layer 3 already gates in CI, in Chromium. This script exists for the two things
Linux cannot do:

- **the real browser engine.** The application ships in WebView2 on Windows.
  Chromium is close, but it is not the engine the user gets.
- **the packaged binary.** `-Packaged` runs `wails build`, starts the `.exe` and
  uses UI Automation (`System.Windows.Automation`) to assert the window appears
  with a non-empty accessibility tree, which is what catches a white-screen boot
  that every other layer would miss. `System.Drawing` captures the window.

**Dev mode** additionally has something the Linux harness cannot offer: `wails dev`
serves the frontend on `http://localhost:34115` **with the Go bridge attached**.
It runs the same rendering assertions as layer 3, against a page where `api.js`
actually works.

Nothing here is installed either: WebSockets come from `System.Net.WebSockets`,
JSON from `System.Text.Json`, screenshots from `System.Drawing`, and the
accessibility tree from `UIAutomationClient`.

**This script has still never been executed.** That is why its CI job is
`continue-on-error`, and why it is documented as an additional check rather than a
layer. Two things are outstanding and neither is fixed by the Linux harness:

- the `ui-windows` job is `continue-on-error` and unverified;
- no manual pass has been done on real documents on Windows.

### Requirements

- Windows with PowerShell 7 (`pwsh`) or Windows PowerShell 5.1.
- Microsoft Edge (present on every supported Windows machine; pass `-Browser`
  for Chrome or another Chromium build).
- The pinned Wails CLI on `PATH` (see the version table in the root
  `CLAUDE.md`).

## One set of probes

`scripts/uitest/probes.js` is the **single** definition of what the rendering
checks look at and how application state is seeded. Both harnesses read that file,
evaluate it in the page, and call the same `__uiProbes.*` functions; they own only
the plumbing (Go plus a small WebSocket client on Linux, PowerShell plus
`ClientWebSocket` on Windows) and the reporting.

The split is deliberate: `probes.js` **measures** (it is the half that needs a
DOM) and the harness **judges** (it is the half that has to explain itself, with
what was expected, what was found and what to change).

Two copies of "which selector holds the Configure rail" is one copy nobody
updates, and since the Windows script has never run, its copy is the one that
would rot unnoticed. So the arrangement is enforced rather than requested, by
`../uitest_parity_test.go` in layer 1:

- neither harness may contain `document.querySelector`, `getBoundingClientRect`,
  `dispatchEvent`, `getComputedStyle`, an `import("/state.js")` or a `setState(`;
- every `__uiProbes.<name>` either harness calls must be defined in `probes.js`;
- every probe the Linux harness asserts on must also be called by the PowerShell
  script (not the reverse: `-Packaged` has checks Linux cannot make at all);
- `probes.js` is one immediately-invoked expression returning `"installed"`, and
  it lives under `scripts/` so `//go:embed all:frontend` can never ship it.

To add a check: write the measuring half as a new `__uiProbes` function, then add
the judging half to `scripts/uitest/renderharness/checks.go` **and** a `Test-*`
assertion to the PowerShell script. The parity test will tell you if you forget
the second one.

## Artefacts

Screenshots and the browser log land in `scripts/uitest/artifacts/` (gitignored).
Both harnesses delete them on a fully green run unless `-keep-artifacts` /
`-KeepArtifacts` is passed, because a stale screenshot from a green run is worse
than none: it is the one somebody reads next time. CI passes the flag and uploads
the folder either way.

Exit codes from the Linux harness are distinguishable on purpose: **1** means a UI
check failed, **2** means the harness itself could not run (no browser, no
checkout, a crashed page). Every failure prints what was expected, what was found,
and what to fix.

## In CI

The test tiers are split across workflows by trigger (see `TESTING.md`):

- `.github/workflows/ci.yml` runs layers 1 and 2 (the Go unit tier with `-race`
  and a coverage gate on every push and PR, and the node frontend suite), plus
  the integration tier on PRs to `main`.
- `.github/workflows/ui.yml` runs layer 3, the rendering harness, on every push
  and PR that touches the UI (`frontend/**`, `scripts/uitest/**`, css). The
  browser is the Google Chrome already on the runner image, so nothing is
  downloaded and no npm is involved.

Layer 3 blocks deliberately. Every one of the seven reported issues passed the
layers above it, and a check that catches them without gating is a comment. It
is path-filtered rather than dropped: the harness has no Go bridge, so a
backend-only change cannot change what it renders, and a pure-backend push
therefore does not need to boot a browser.

The `ui-windows` job (in `ci.yml`, whose push trigger also fires on tags) runs
the PowerShell script on `windows-latest`, on `workflow_dispatch` and on tags,
uploading `scripts/uitest/artifacts/` and marked `continue-on-error` until it
has passed a few times. Promote it by deleting that line.


# UI testing

Three layers, cheapest first. The first two run in CI on every push; the third
is Windows-only and opt-in.

The reason there are three: every one of the seven issues reported against the
built application passed the existing tests. Unit tests proved each function;
nothing proved the assembled screen, and nothing at all proved what the user
could actually SEE.

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

## Layer 2: frontend render tests, no browser

```
node --test frontend/*.test.js
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

## Layer 3: real rendering, Windows only

```powershell
pwsh scripts/uitest/Invoke-UITest.ps1            # dev mode
pwsh scripts/uitest/Invoke-UITest.ps1 -Packaged  # the built .exe
```

Layer 2 proves the HTML. It cannot prove that a tooltip is **visible** rather
than clipped by the pane it sits in, which is exactly what reported issue 6
turned out to be: the markup had been correct for months and
`.pane-body { overflow: auto }` was eating it. Nor can it prove that a screen
fits the window, which is the fixed-height layout contract in
`frontend/CLAUDE.md`.

**Dev mode** uses the fact that `wails dev` serves the frontend on
`http://localhost:34115` with the Go bridge attached. The script starts it,
launches Edge headless with `--remote-debugging-port`, and drives the DevTools
Protocol over `System.Net.WebSockets.ClientWebSocket`. Nothing is installed and
no test hook ships in the binary. It asserts:

- no wizard screen scrolls the page body, in either direction;
- the Import preview's rendered `innerText` contains no placeholder;
- the Configure rail is three route sections with the documented default
  switch positions, and every category checkbox is reachable without clicking;
- a real `mouseenter` on a mark produces a tooltip whose `getBoundingClientRect`
  is inside the Compare card and inside the viewport.

**`-Packaged`** builds the .exe, starts it and uses UI Automation
(`System.Windows.Automation`) to assert the window appears with a non-empty
accessibility tree, which is what catches a white-screen boot that every other
layer would miss. `System.Drawing` captures the window.

Screenshots and the log land in `scripts/uitest/artifacts/`. The script exits
non-zero on the first failing assertion set, and every failure prints what was
expected, what was found, and what to fix.

### Requirements

- Windows with PowerShell 7 (`pwsh`) or Windows PowerShell 5.1.
- Microsoft Edge (present on every supported Windows machine; pass `-Browser`
  for Chrome or another Chromium build).
- The pinned Wails CLI on `PATH` (see the version table in the root
  `CLAUDE.md`).

### In CI

`.github/workflows/ci.yml` runs Layers 1 and 2 on `ubuntu-latest` as the
blocking job. Layer 3 runs on `windows-latest` on `workflow_dispatch` and on
tags, uploading `scripts/uitest/artifacts/` and marked `continue-on-error`
until it has enough runs to show it is not flaky. Promote it to a gate by
deleting that line.

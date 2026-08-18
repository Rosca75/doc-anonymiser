<#
.SYNOPSIS
  The ADDITIONAL PLATFORM CHECK for the real doc-anonymiser UI: Windows, Edge,
  the real WebView2, and the packaged .exe. PowerShell + .NET, no packages.

.DESCRIPTION
  Layer 3 of the test strategy runs in CI on Linux
  (scripts/uitest/renderharness, see docs/UITESTING.md). This script is the
  Windows counterpart, and it owns the two things Linux cannot do:

    the REAL browser engine   The application ships in WebView2 on Windows.
                              The Linux harness renders in Chromium, which is
                              close but is not the engine the user gets.

    the PACKAGED .exe         -Packaged builds the binary, starts it, and
                              inspects the accessibility tree. No Linux check
                              can see a white-screen boot of a Windows build.

  It shares its ASSERTIONS with the Linux harness rather than restating them:
  both read scripts/uitest/probes.js, evaluate it in the page, and call the
  same __uiProbes.* functions. Two copies of "which selector holds the
  Configure rail" is two copies to forget to update, and the harness that runs
  less often is the copy that rots. The plumbing differs (PowerShell and
  ClientWebSocket here, Go and a small RFC 6455 client there); what is
  asserted does not.

  Note this script has still never been executed on a Windows machine. The
  Linux harness is the one that gates.

  Two checks, both optional and both opt-in:

    dev mode (default)  `wails dev` serves the frontend on
                        http://localhost:34115 WITH THE GO BRIDGE ATTACHED, so
                        the app can be driven by a headless browser over the
                        DevTools Protocol without shipping any test hook into
                        the binary. This is where the layout and visibility
                        assertions run, and the attached bridge is the one
                        thing the Linux harness genuinely cannot offer: it
                        serves the frontend statically, so window.go is absent
                        there.

    -Packaged           builds the .exe, starts it, and uses UI Automation
                        (System.Windows.Automation) to assert the window
                        appears with a non-empty accessibility tree, which is
                        what catches a white-screen boot. A screenshot is
                        captured for the artefact folder.

  Nothing here is installed: WebSockets come from System.Net.WebSockets,
  JSON from System.Text.Json, screenshots from System.Drawing, and the
  accessibility tree from UIAutomationClient. All ship with Windows/.NET.

.PARAMETER Packaged
  Run the packaged-binary smoke test instead of the dev-mode checks.

.PARAMETER Browser
  Path to a Chromium-based browser. Defaults to Edge, which is present on
  every supported Windows machine (it is the WebView2 runtime's browser).

.PARAMETER KeepArtifacts
  Keep the screenshots and the log even when everything passed.

.EXAMPLE
  pwsh scripts/uitest/Invoke-UITest.ps1
  pwsh scripts/uitest/Invoke-UITest.ps1 -Packaged
#>
[CmdletBinding()]
param(
    [switch]$Packaged,
    [string]$Browser,
    [switch]$KeepArtifacts
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..' '..')).Path
$artifactDir = Join-Path $PSScriptRoot 'artifacts'
New-Item -ItemType Directory -Force -Path $artifactDir | Out-Null

$script:Failures = @()
# The seed facts the shared probes report back (scripts/uitest/probes.js
# __uiProbes.fixture), so an expectation is never spelled twice across the two
# harnesses. Filled by Install-Probes.
$script:Fixture = $null

# --- Reporting --------------------------------------------------------------
#
# Every failure says what was expected, what was found, and where to look
# (CLAUDE.md section 2: error messages must be actionable). A UI failure that
# only says "assertion failed" costs more time than it saves.

function Write-Step($message) { Write-Host "==> $message" -ForegroundColor Cyan }

function Assert-That {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][bool]$Condition,
        [string]$Expected = '',
        [string]$Actual = '',
        [string]$Hint = ''
    )
    if ($Condition) {
        Write-Host "  PASS  $Name" -ForegroundColor Green
        return
    }
    Write-Host "  FAIL  $Name" -ForegroundColor Red
    if ($Expected) { Write-Host "        expected: $Expected" }
    if ($Actual)   { Write-Host "        actual:   $Actual" }
    if ($Hint)     { Write-Host "        fix:      $Hint" }
    $script:Failures += $Name
}

# --- DevTools Protocol over a raw WebSocket ---------------------------------
#
# One connection, one incrementing message id, synchronous request/reply. That
# is all this needs: no CDP library, no npm, nothing to install.

class CdpSession {
    [System.Net.WebSockets.ClientWebSocket]$Socket
    [int]$NextId = 1

    CdpSession([string]$url) {
        $this.Socket = [System.Net.WebSockets.ClientWebSocket]::new()
        $this.Socket.ConnectAsync([Uri]$url, [Threading.CancellationToken]::None).Wait(10000) | Out-Null
        if ($this.Socket.State -ne 'Open') {
            throw "Could not open a DevTools connection to $url. Is the browser running with --remote-debugging-port?"
        }
    }

    [object] Send([string]$method, [hashtable]$params) {
        $id = $this.NextId++
        $payload = @{ id = $id; method = $method; params = $params } | ConvertTo-Json -Depth 12 -Compress
        $bytes = [Text.Encoding]::UTF8.GetBytes($payload)
        $segment = [ArraySegment[byte]]::new($bytes)
        $this.Socket.SendAsync($segment, 'Text', $true, [Threading.CancellationToken]::None).Wait()

        # Read until the reply with OUR id arrives: the protocol interleaves
        # unsolicited events with replies.
        $deadline = (Get-Date).AddSeconds(30)
        while ((Get-Date) -lt $deadline) {
            $message = $this.Receive()
            if ($null -eq $message) { continue }
            if ($message.PSObject.Properties.Name -contains 'id' -and $message.id -eq $id) {
                if ($message.PSObject.Properties.Name -contains 'error') {
                    throw "DevTools rejected $method : $($message.error.message)"
                }
                return $message.result
            }
        }
        throw "DevTools did not answer $method within 30 s."
    }

    [object] Receive() {
        $buffer = [byte[]]::new(65536)
        $text = [Text.StringBuilder]::new()
        do {
            $segment = [ArraySegment[byte]]::new($buffer)
            $task = $this.Socket.ReceiveAsync($segment, [Threading.CancellationToken]::None)
            if (-not $task.Wait(30000)) { return $null }
            $result = $task.Result
            [void]$text.Append([Text.Encoding]::UTF8.GetString($buffer, 0, $result.Count))
        } while (-not $result.EndOfMessage)
        return $text.ToString() | ConvertFrom-Json
    }

    [object] Eval([string]$expression) {
        # awaitPromise + returnByValue so a test can await the application's
        # own promises and get plain data back.
        $result = $this.Send('Runtime.evaluate', @{
            expression    = $expression
            awaitPromise  = $true
            returnByValue = $true
        })
        if ($result.PSObject.Properties.Name -contains 'exceptionDetails') {
            throw "The page threw while evaluating: $($result.exceptionDetails.exception.description)"
        }
        return $result.result.value
    }

    [void] Close() {
        if ($this.Socket.State -eq 'Open') {
            $this.Socket.CloseAsync('NormalClosure', 'done', [Threading.CancellationToken]::None).Wait(5000) | Out-Null
        }
        $this.Socket.Dispose()
    }
}

function Wait-ForPort([int]$port, [int]$timeoutSeconds) {
    $deadline = (Get-Date).AddSeconds($timeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        try {
            $client = [Net.Sockets.TcpClient]::new()
            $client.Connect('127.0.0.1', $port)
            $client.Close()
            return $true
        } catch {
            Start-Sleep -Milliseconds 400
        }
    }
    return $false
}

function Find-Browser {
    if ($Browser) { return $Browser }
    $candidates = @(
        "${env:ProgramFiles(x86)}\Microsoft\Edge\Application\msedge.exe",
        "${env:ProgramFiles}\Microsoft\Edge\Application\msedge.exe",
        "${env:ProgramFiles}\Google\Chrome\Application\chrome.exe"
    )
    foreach ($path in $candidates) {
        if ($path -and (Test-Path $path)) { return $path }
    }
    throw "No Chromium-based browser found. Pass -Browser <path to msedge.exe or chrome.exe>."
}

# --- Dev-mode checks --------------------------------------------------------

function Invoke-DevChecks {
    Write-Step 'Starting wails dev'
    $wails = Start-Process -FilePath 'wails' -ArgumentList 'dev' -WorkingDirectory $repoRoot -PassThru -WindowStyle Minimized
    try {
        if (-not (Wait-ForPort 34115 180)) {
            throw "wails dev did not serve http://localhost:34115 within 3 minutes. Run it by hand to see why."
        }

        $profileDir = Join-Path $env:TEMP ("doc-anonymiser-uitest-" + [Guid]::NewGuid().ToString('N'))
        $browserPath = Find-Browser
        Write-Step "Launching $([IO.Path]::GetFileName($browserPath)) headless against localhost:34115"
        $browserArgs = @(
            '--headless=new', '--remote-debugging-port=9222', '--window-size=1440,900',
            "--user-data-dir=$profileDir", '--no-first-run', '--disable-gpu',
            'http://localhost:34115'
        )
        $browser = Start-Process -FilePath $browserPath -ArgumentList $browserArgs -PassThru
        try {
            if (-not (Wait-ForPort 9222 60)) { throw 'The browser did not open its DevTools port.' }
            Start-Sleep -Seconds 2

            $targets = Invoke-RestMethod 'http://127.0.0.1:9222/json' -TimeoutSec 20
            $page = $targets | Where-Object { $_.type -eq 'page' } | Select-Object -First 1
            if (-not $page) { throw 'The browser opened no page target.' }

            $cdp = [CdpSession]::new($page.webSocketDebuggerUrl)
            try {
                $cdp.Send('Runtime.enable', @{}) | Out-Null
                $cdp.Send('Page.enable', @{}) | Out-Null
                Start-Sleep -Seconds 2

                # Pin the viewport before measuring anything: --window-size is
                # only a request (the window subtracts its own chrome), and a
                # layout contract measured against an unknown height is not a
                # contract. Same 1440x900 as the Linux harness, so a failure
                # means the same thing on both.
                $cdp.Send('Emulation.setDeviceMetricsOverride', @{
                    width = 1440; height = 900; deviceScaleFactor = 1; mobile = $false
                }) | Out-Null

                Install-Probes $cdp
                Test-Layout $cdp
                Test-ImportPreview $cdp
                Test-ConfigureRail $cdp
                Test-ValueCardActions $cdp
                Test-ConfigurePanelFit $cdp
                Test-HelpTooltip $cdp
                Test-ScrollRetention $cdp
                Test-TooltipVisibility $cdp
                Test-CompareSearch $cdp
                Save-Screenshot $cdp 'wizard.png'
            } finally {
                $cdp.Close()
            }
        } finally {
            if (-not $browser.HasExited) { $browser.Kill() }
            Remove-Item -Recurse -Force $profileDir -ErrorAction SilentlyContinue
        }
    } finally {
        if ($wails -and -not $wails.HasExited) { $wails.Kill() }
    }
}

function Save-Screenshot([CdpSession]$cdp, [string]$name) {
    $shot = $cdp.Send('Page.captureScreenshot', @{ format = 'png' })
    $path = Join-Path $artifactDir $name
    [IO.File]::WriteAllBytes($path, [Convert]::FromBase64String($shot.data))
    Write-Host "  saved $path"
}

# --- The shared probes ------------------------------------------------------
#
# scripts/uitest/probes.js is the SINGLE definition of what these checks look at
# and how the application state is seeded. This script and the Linux harness both
# read that file, evaluate it in the page, and call the same __uiProbes.*
# functions; only the plumbing around it differs. See the .DESCRIPTION above.

function Install-Probes([CdpSession]$cdp) {
    Write-Step 'Installing the shared probes'
    $probesPath = Join-Path $PSScriptRoot 'probes.js'
    if (-not (Test-Path $probesPath)) {
        throw "The shared probes are missing from $probesPath. Both harnesses read that one file, so it is not optional; restore it from the repository."
    }
    $installed = $cdp.Eval((Get-Content -Raw -Encoding UTF8 $probesPath))
    if ($installed -ne 'installed') {
        throw "$probesPath returned '$installed' instead of 'installed', so it may not have run. The file must be ONE expression that ends by returning the string 'installed'."
    }
    $script:Fixture = $cdp.Eval('__uiProbes.fixture()')
}

# The fixed-height layout contract (frontend/CLAUDE.md), in the three parts the
# probe measures: the page body must not scroll, #view must not CLIP what it
# holds (overflow: hidden there turns a mis-sized card into cut-off content
# rather than a scrolling page), and the only things that scroll are card bodies.
# Only a real renderer can answer any of it.
function Test-Layout([CdpSession]$cdp) {
    Write-Step 'The fixed-height layout contract'
    foreach ($step in @('import', 'identify', 'anonymise', 'export')) {
        $m = $cdp.Eval("__uiProbes.layout('$step')")
        $where = "in a $($m.viewport.width)x$($m.viewport.height) viewport (tallest inside #view: $($m.tallest.selector) at $($m.tallest.height) px)"

        Assert-That -Name "$step does not scroll the page body" `
            -Condition ($m.down -le 1 -and $m.across -le 1) `
            -Expected 'the body scroll size equal to its client size, in both directions' `
            -Actual "$($m.down) px down, $($m.across) px across, $where" `
            -Hint 'body and #app are 100vh. Chrome past the fixed header/step-bar/footer heights, or wide content outside its own overflow-x: auto container, will do this.'

        Assert-That -Name "$step fits the workspace without being clipped" `
            -Condition ($m.viewClipsDown -le 1 -and $m.viewClipsAcross -le 1) `
            -Expected "#view's scroll size equal to its client size: whatever it holds has to fit" `
            -Actual "$($m.viewClipsDown) px clipped off the bottom, $($m.viewClipsAcross) px off the side, $where" `
            -Hint 'main#view is overflow: hidden, so a card that does not fit is CUT OFF. A link in the chain from #view down to the scrolling card body is missing min-height: 0.'

        $stray = @($m.scrollers | Where-Object { -not $_.allowed } |
            ForEach-Object { "$($_.selector) ($($_.down) px down, $($_.across) px across)" })
        Assert-That -Name "$step scrolls only inside a card body" `
            -Condition ($stray.Count -eq 0) `
            -Expected 'every scrolling element to be a card body, a group body, the rail, a card column or a .table-scroll' `
            -Actual ($stray -join '; ') `
            -Hint 'Move the overflow down onto the card body, or wrap wide content in its own overflow-x: auto container.'
    }
}

# Reported issues 1 and 4, seen through the pixels rather than the HTML: the
# source panes were showing pipeline output. The probe seeds a state that HAS a
# finished run in it, so there is real anonymised text for a view to reach for by
# mistake.
function Test-ImportPreview([CdpSession]$cdp) {
    Write-Step 'The Import preview shows source text, not pipeline output'
    $r = $cdp.Eval('__uiProbes.importPreview()')
    if ($r.PSObject.Properties.Name -contains 'error' -and $r.error) {
        Assert-That -Name 'the Import preview renders' -Condition $false `
            -Expected 'an .md-preview pane inside the Preview card' -Actual $r.error `
            -Hint 'views/import.js previewCard renders previewBody(doc) when a document is selected.'
        return
    }
    Assert-That -Name 'the Import preview contains no placeholder' `
        -Condition ([string]::IsNullOrEmpty($r.placeholder)) `
        -Expected 'no [CATEGORY_N] anywhere in the pane rendered text' `
        -Actual "found '$($r.placeholder)' around: $($r.excerpt)" `
        -Hint 'views/import.js must render the imported markdown (state.documentSource), never anything the pipeline produced.'
    Assert-That -Name 'the Import preview actually shows the document' `
        -Condition ($r.showsSource -and $r.chars -gt 200) `
        -Expected 'the seeded source text, several hundred characters of it' `
        -Actual "$($r.chars) characters, source marker present: $($r.showsSource)" `
        -Hint 'An empty pane would pass the placeholder check for the wrong reason, so it is asserted separately.'
}

# The Configure choices are switchable detection-route sections, not peer tabs.
function Test-ConfigureRail([CdpSession]$cdp) {
    Write-Step 'The Configure rail is the two detection routes'
    $r = $cdp.Eval('__uiProbes.configureRail()')
    if ($r.PSObject.Properties.Name -contains 'error' -and $r.error) {
        Assert-That -Name 'the Identify rail renders' -Condition $false `
            -Expected '#identify-rail on the Identify screen' -Actual $r.error `
            -Hint 'views/identify.js renders the rail section and hands it to renderIdentifyRail.'
        return
    }
    Assert-That -Name 'the rail is two route sections' -Condition ($r.sections -eq 2) `
        -Expected '2 .rail-section elements' -Actual "$($r.sections), routes: $($r.routes -join ', ')" `
        -Hint 'views/identifyrail.js RAIL_SECTIONS defines Smart detection and Local AI.'
    Assert-That -Name 'the old tab strip is gone' -Condition ($r.railTabs -eq 0) `
        -Expected '0 [data-railtab] chips anywhere in the document' -Actual "$($r.railTabs)" `
        -Hint 'The rail switches sections on and off; it does not tab between them.'
    Assert-That -Name 'Smart detection is on by default' -Condition ($r.smartOn -eq $true) `
        -Expected 'the rail-smart route switch checked' -Actual "$($r.smartOn)" `
        -Hint 'Every Smart detection method defaults on, so the derived section state reads on.'
    Assert-That -Name 'Local AI is off by default' -Condition ($r.localOn -eq $false) `
        -Expected 'the rail-local route switch unchecked' -Actual "$($r.localOn)" `
        -Hint 'state.js settings.useLocalAI defaults to false. Detecting Ollama ENABLES this switch, it never flips it.'
    Assert-That -Name 'every category checkbox is present' `
        -Condition ($r.categories -ge $script:Fixture.categoryCount) `
        -Expected "at least $($script:Fixture.categoryCount) .cat-toggle checkboxes" -Actual "$($r.categories)" `
        -Hint 'state.js ALL_CATEGORIES plus the country-specific ID categories must all reach the rail.'
    Assert-That -Name 'every category checkbox is reachable without clicking' `
        -Condition ($r.categories -gt 0 -and $r.categoriesWithSize -eq $r.categories) `
        -Expected 'all of them laid out with a non-zero height' `
        -Actual "$($r.categoriesWithSize) of $($r.categories) have a height" `
        -Hint 'A checkbox inside a folded group is in the DOM but not something the user can tick.'
}

# A value card's controls must CHANGE something. This is the layer that sees
# attribute lower-casing: a card names the Value its handlers act on through
# data- attributes, and a browser lower-cases attribute NAMES while a string test
# preserves them, so a camel-case data-mainText renders, matches every string
# assertion, and reaches the handler as an undefined dataset.mainText. Rename,
# remove, drop-a-spelling and merge then all silently do nothing.
function Test-ValueCardActions([CdpSession]$cdp) {
    Write-Step "A value card's actions reach the store"
    $r = $cdp.Eval('__uiProbes.valueCardActions()')
    if ($r.PSObject.Properties.Name -contains 'error' -and $r.error) {
        Assert-That -Name 'the value-card probe runs' -Condition $false `
            -Expected 'value cards on the My values tab' -Actual $r.error `
            -Hint 'views/identifyworkspace.js renders one .value-card per accepted Value.'
        return
    }
    Assert-That -Name 'the seeded values render as cards' -Condition ($r.cards -eq 2) `
        -Expected '2 .value-card elements' -Actual "$($r.cards)" `
        -Hint 'The probe seeds two accepted Values, one per card.'
    Assert-That -Name 'every card carries its own identity' `
        -Condition ($r.cards -gt 0 -and $r.cardsWithIdentity -eq $r.cards) `
        -Expected 'every card readable as dataset.category + dataset.mainText' `
        -Actual "$($r.cardsWithIdentity) of $($r.cards)" `
        -Hint 'The card renders data-category and data-main-text. A camel-case data-mainText is lower-cased by the parser, so dataset.mainText is undefined and every action on the card resolves against it.'
    Assert-That -Name 'clicking the name reveals an inline input' -Condition ($r.inlineInputAppeared -eq $true) `
        -Expected 'a .value-name-input in place of the name button' -Actual "$($r.inlineInputAppeared)" `
        -Hint 'revealNameInput replaces the name button; native dialogs are banned.'
    Assert-That -Name 'committing the input renames the Value' -Condition ($r.renamed -eq $true) `
        -Expected 'the new name in state.values' -Actual "$($r.renamed)" `
        -Hint 'revealNameInput commits through renameValue, which needs the card mainText.'
    Assert-That -Name "the card's remove control deletes the Value" -Condition ($r.removedOne -eq $true) `
        -Expected 'one fewer value in the store' -Actual "$($r.removedOne), values now: $($r.valuesAfter -join ', ')" `
        -Hint 'The .value-remove handler calls deleteValue(category, mainText) from the card dataset.'
}

# A scrolled panel keeps its position across a repaint. This is a visible-only
# regression: every state change rewrites the whole shell (main.js paint ->
# root.innerHTML), so a scrolled panel like the Identify rail snapped back to the
# top on every tick or drill-down. The fix preserves scroll centrally
# (frontend/scroll.js); this drives a real action through the rail and checks the
# offset survived.
function Test-ScrollRetention([CdpSession]$cdp) {
    Write-Step 'A scrolled panel keeps its position across a repaint'
    $r = $cdp.Eval('__uiProbes.scrollRetention()')
    if ($r.PSObject.Properties.Name -contains 'error' -and $r.error) {
        Assert-That -Name 'the scroll-retention probe runs' -Condition $false `
            -Expected '#identify-rail with a scroller and a category toggle' -Actual $r.error `
            -Hint 'views/identify.js renders the rail; identifyrail.js renders .cat-toggle checkboxes.'
        return
    }
    if (-not $r.scrollable) {
        # Not a failure: the rail fit the viewport, so the bug could not be
        # exercised. Say so rather than claim a green nobody earned.
        Assert-That -Name 'the rail overflows enough to exercise scrolling' -Condition $true `
            -Expected 'a scrollable rail' `
            -Actual 'the rail fit the viewport, so scroll retention was not exercised' -Hint ''
        return
    }
    Assert-That -Name 'the rail actually scrolled before the repaint' -Condition ($r.before -gt 0) `
        -Expected 'a non-zero scrollTop after scrolling the rail down' -Actual "$($r.before)" `
        -Hint 'If the rail refused to scroll the check measures nothing, so this is asserted separately.'
    Assert-That -Name 'the scroll position survives a repaint' -Condition ($r.after -eq $r.before) `
        -Expected "scrollTop still $($r.before) after ticking a category" -Actual "$($r.after)" `
        -Hint 'frontend/scroll.js snapshotScrollPositions/restoreScrollPositions must bracket main.js paint(), so a scrolled panel is not thrown back to the top by root.innerHTML.'
}

# The Configure panel must FIT, and its explanations must be tooltips rather than
# prose. The panel used to carry a paragraph under every control, and the sum of
# them put the controls at the foot out of reach.
function Test-ConfigurePanelFit([CdpSession]$cdp) {
    Write-Step 'The Configure panel fits, and explains itself on demand'
    $r = $cdp.Eval('__uiProbes.configurePanelFit()')
    if ($r.PSObject.Properties.Name -contains 'error' -and $r.error) {
        Assert-That -Name 'the panel-fit probe runs' -Condition $false `
            -Expected '#identify-rail on the Identify screen' -Actual $r.error `
            -Hint 'views/identify.js renders the rail and hands it to renderIdentifyRail.'
        return
    }
    Assert-That -Name 'the panel spends no vertical space on prose' `
        -Condition ($r.proseParagraphs -eq 0 -and $r.proseHeight -eq 0) `
        -Expected '0 static paragraphs, 0px of them' `
        -Actual "$($r.proseParagraphs) paragraph(s), $($r.proseHeight)px" `
        -Hint 'An explanation belongs in a help tooltip. Live read-outs carry .rail-readout and are excluded, so this measures prose only.'
    Assert-That -Name 'the explanations are still reachable' -Condition ($r.helpTooltips -ge 8) `
        -Expected 'at least 8 help tooltips in the rail' -Actual "$($r.helpTooltips)" `
        -Hint 'Removing the paragraphs must MOVE the explanations, not delete them: ui.js helpTooltip is where each one now lives.'
    Assert-That -Name 'the panel scrolls inside its own body, not the page' `
        -Condition ($r.pageOverflows -eq $false) `
        -Expected 'a document no taller than the window' -Actual "$($r.pageOverflows)" `
        -Hint 'The fixed-height layout contract: scrolling happens inside a card body and nowhere else.'
    Assert-That -Name 'the foot of the panel is reachable' -Condition ($r.footReachable -eq $true) `
        -Expected 'the last section painted after scrolling the panel to its end' `
        -Actual "$($r.footReachable)" `
        -Hint 'Prose under every control is what put the controls at the foot out of reach.'
}

# A help tooltip's bubble sits OUTSIDE the rail's clipping context on purpose: a
# bubble inside an overflow:auto ancestor is cut at the container's edge, and only
# a real renderer plus a hit test can see that. The keyboard path is driven too,
# because an explanation only a pointer can reach is one half the users never get.
function Test-HelpTooltip([CdpSession]$cdp) {
    Write-Step 'A Configure help tooltip opens, is painted, and closes'
    $r = $cdp.Eval('__uiProbes.helpTooltipVisibility()')
    if ($r.PSObject.Properties.Name -contains 'error' -and $r.error) {
        Assert-That -Name 'the help-tooltip probe runs' -Condition $false `
            -Expected 'a help tooltip in the Identify rail' -Actual $r.error `
            -Hint 'views/identifyrail.js renders ui.js helpTooltip beside each explained label.'
        return
    }
    Assert-That -Name 'the bubble is hidden until asked for' -Condition ($r.closedVisible -eq $false) `
        -Expected 'a zero-height bubble before any interaction' -Actual "$($r.closedVisible)" `
        -Hint 'An always-visible bubble is the paragraph the tooltip replaced, with extra steps.'
    Assert-That -Name 'hover opens it' -Condition ($r.openedOnHover -eq $true) `
        -Expected 'a painted bubble after pointerenter' -Actual "$($r.openedOnHover)" `
        -Hint 'ui.js wireHelpTooltips sets data-open on pointerenter; style.css reveals it.'
    Assert-That -Name 'the bubble is on screen' -Condition ($r.onScreen -eq $true) `
        -Expected 'the whole bubble inside the viewport' -Actual "$($r.onScreen)" `
        -Hint 'A bubble that opens off the edge of the window is a bubble nobody reads.'
    Assert-That -Name 'the bubble is painted, not clipped by the scrolling panel' `
        -Condition ($r.notClipped -eq $true) `
        -Expected 'the bubble itself under a hit test at its own coordinates' -Actual "$($r.notClipped)" `
        -Hint 'This is the reason the bubble sits outside the clipping context of the rail.'
    Assert-That -Name 'leaving closes it' -Condition ($r.closedOnLeave -eq $true) `
        -Expected 'a zero-height bubble after pointerleave' -Actual "$($r.closedOnLeave)" -Hint ''
    Assert-That -Name 'keyboard focus opens it too' -Condition ($r.openedOnFocus -eq $true) `
        -Expected 'a painted bubble after focusin' -Actual "$($r.openedOnFocus)" `
        -Hint 'An explanation only a pointer can reach is one half the users never get.'
    Assert-That -Name 'Escape closes it' -Condition ($r.closedOnEscape -eq $true) `
        -Expected 'a zero-height bubble after Escape' -Actual "$($r.closedOnEscape)" `
        -Hint 'A tooltip the keyboard can open and not dismiss is a keyboard trap.'
}

# Reported issue 6. This is the assertion that CANNOT be made without a real
# renderer: the tooltip was in the HTML all along, and the pane clipped it. Three
# marks are hovered, including the two nearest the pane right and bottom edges,
# because the middle of the pane was never where it failed.
function Test-TooltipVisibility([CdpSession]$cdp) {
    Write-Step 'The hover tooltip is visible, not clipped'
    $r = $cdp.Eval('__uiProbes.tooltipVisibility()')
    if ($r.PSObject.Properties.Name -contains 'error' -and $r.error) {
        Assert-That -Name 'the Compare card renders and a mark can be hovered' -Condition $false `
            -Expected 'marks in #anonymised-pane and a #mark-tooltip in #compare-card' -Actual $r.error `
            -Hint 'views/anonymise.js compareCard renders the tooltip node as a child of the CARD, not of the pane.'
        return
    }
    Assert-That -Name 'the anonymised pane has marks a user could hover' -Condition ($r.hoverable -gt 0) `
        -Expected 'at least one mark[data-original] visible inside the pane' `
        -Actual "$($r.marks) rendered, $($r.hoverable) visible" `
        -Hint 'highlight.js renders a mapped placeholder as a mark carrying data-original.'

    foreach ($sample in $r.samples) {
        $label = "the $($sample.edge) mark"
        if (-not $sample.appeared) {
            Assert-That -Name "$label shows a tooltip on mouseenter" -Condition $false `
                -Expected '#mark-tooltip with hidden cleared' -Actual 'the tooltip stayed hidden' `
                -Hint 'views/anonymise.js wireMarkTooltip binds mouseenter on every mark[data-original].'
            continue
        }
        $tip = "$($sample.tooltipRect.left),$($sample.tooltipRect.top) to $($sample.tooltipRect.right),$($sample.tooltipRect.bottom)"
        Assert-That -Name "$label shows the original value belonging to that mark" `
            -Condition ($sample.markOriginal -and $sample.text -like "*$($sample.markOriginal)*") `
            -Expected "the hovered mark own original, '$($sample.markOriginal)', in the tooltip" `
            -Actual "'$($sample.text)'" `
            -Hint 'The tooltip first line is mark.dataset.original, which comes from state.mapping.'
        Assert-That -Name "$label tooltip is inside the Compare card" -Condition ($sample.insideCard) `
            -Expected 'the tooltip rect within the card rect' -Actual "tooltip $tip" `
            -Hint 'Anchor it to #compare-card and clamp it to the card width, as wireMarkTooltip does.'
        Assert-That -Name "$label tooltip is on screen" -Condition ($sample.inViewport) `
            -Expected 'the tooltip rect within the viewport' -Actual "tooltip $tip" `
            -Hint 'Flip the tooltip above the mark when there is no room below it.'
        Assert-That -Name "$label tooltip has a size" -Condition ($sample.hasSize) `
            -Expected 'a tooltip more than 10 px wide and tall' `
            -Actual "$($sample.tooltipRect.width)x$($sample.tooltipRect.height)" `
            -Hint 'An empty tooltip means innerHTML was never filled in.'
        Assert-That -Name "$label tooltip is not inside the scrolling pane" `
            -Condition (-not $sample.insidePaneSubtree) `
            -Expected "#mark-tooltip outside #anonymised-pane, which is overflow: $($sample.paneOverflow)" `
            -Actual 'the tooltip is a descendant of the pane, so the pane clips it' `
            -Hint 'This is reported issue 6 exactly. Move the tooltip node up to #compare-card.'
        Assert-That -Name "$label tooltip is painted, not clipped away" -Condition ($sample.paintedOnTop) `
            -Expected 'elementFromPoint at the tooltip centre returning the tooltip' `
            -Actual "something else is at $tip" `
            -Hint 'The rect of a clipped element is still a full-size rect, so this is the check that catches it.'
    }
}

# The Compare search. Like the tooltip check, the interesting half cannot be
# asserted without a renderer: a string test proves the hit span was emitted,
# but only a real engine can prove the pane scrolled to it rather than leaving
# it clipped below the fold.
function Test-CompareSearch([CdpSession]$cdp) {
    Write-Step 'The Compare search finds text in both panes and shows the active hit'
    $r = $cdp.Eval('__uiProbes.compareSearch()')
    if ($r.PSObject.Properties.Name -contains 'error' -and $r.error) {
        Assert-That -Name 'the Compare search box renders and accepts a needle' -Condition $false `
            -Expected '#compare-search in the Compare card head' -Actual $r.error `
            -Hint 'views/anonymise.js compareCard puts searchControls in .card-head-right.'
        return
    }

    Assert-That -Name 'the needle is found in the ORIGINAL pane' -Condition ($r.perPane.original -gt 0) `
        -Expected "at least one .find-hit in #original-pane for '$($r.needle)'" `
        -Actual "$($r.perPane.original) hits" `
        -Hint 'views/anonymise.js renders the original pane through panesearch.js renderPlainWithHits.'

    Assert-That -Name 'the needle is found in the ANONYMISED pane' -Condition ($r.perPane.anonymised -gt 0) `
        -Expected "at least one .find-hit in #anonymised-pane for '$($r.needle)'" `
        -Actual "$($r.perPane.anonymised) hits" `
        -Hint 'highlight.js renderHighlighted takes a fourth search argument and emits hit spans in the plain stretches and inside a mark own text.'

    Assert-That -Name 'exactly one hit is the active one' -Condition ($r.hasActive) `
        -Expected 'a .find-hit.active somewhere on screen' -Actual 'no active hit was rendered' `
        -Hint 'searchWalk resolves the active index over the ONE combined list; searchControls and both panes read the same walk.'

    Assert-That -Name 'the readout says where the active hit is' -Condition ($r.readout) `
        -Expected 'a readout naming the count, the total and the active hit pane' -Actual 'an empty readout' `
        -Hint 'copy.js ANONYMISE.searchCount(index, total, pane).'

    Assert-That -Name 'both navigation buttons are live when there are hits' `
        -Condition ($r.nextEnabled -and $r.prevEnabled) `
        -Expected 'next and previous enabled' `
        -Actual "next enabled: $($r.nextEnabled), previous enabled: $($r.prevEnabled)" `
        -Hint 'searchControls disables them only when the walk is empty, and gives them a title saying why.'

    if (-not $r.visible) {
        Assert-That -Name 'the active hit sits inside a pane that can show it' -Condition $false `
            -Expected 'the active hit inside a .pane-body' -Actual 'the active hit has no .pane-body ancestor' `
            -Hint 'Both panes render their hits through the same two renderers.'
        return
    }

    $hit = "$($r.visible.activeRect.left),$($r.visible.activeRect.top) to $($r.visible.activeRect.right),$($r.visible.activeRect.bottom)"
    $pane = "$($r.visible.paneRect.left),$($r.visible.paneRect.top) to $($r.visible.paneRect.right),$($r.visible.paneRect.bottom)"

    Assert-That -Name 'the active hit has a size' -Condition ($r.visible.hasSize) `
        -Expected 'a rendered span with width and height' -Actual "hit $hit" `
        -Hint 'An empty hit span means the slice offsets and the text disagree.'

    Assert-That -Name 'the active hit is visible INSIDE its pane, not scrolled out of sight' `
        -Condition ($r.visible.insidePane) `
        -Expected 'the active hit rect within its pane rect' -Actual "hit $hit, pane $pane" `
        -Hint 'views/anonymise.js scrollToActiveHit runs after the paint and only when the active index changed, so it does not fight scroll.js restoring the pane offset.'

    Assert-That -Name 'the active hit is on screen' -Condition ($r.visible.inViewport) `
        -Expected 'the active hit rect within the viewport' -Actual "hit $hit" `
        -Hint 'scrollIntoView with block center on .find-hit.active.'
}

# --- Packaged-binary smoke test (UI Automation) -----------------------------

function Invoke-PackagedChecks {
    Write-Step 'wails build'
    Push-Location $repoRoot
    try {
        & wails build
        if ($LASTEXITCODE -ne 0) { throw "wails build failed with exit code $LASTEXITCODE." }
    } finally {
        Pop-Location
    }

    $exe = Get-ChildItem (Join-Path $repoRoot 'build\bin') -Filter '*.exe' | Select-Object -First 1
    if (-not $exe) { throw 'wails build produced no .exe in build\bin.' }

    Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes, System.Drawing, System.Windows.Forms

    Write-Step "Starting $($exe.Name)"
    $app = Start-Process -FilePath $exe.FullName -PassThru
    try {
        Start-Sleep -Seconds 6
        $root = [System.Windows.Automation.AutomationElement]::RootElement
        $condition = [System.Windows.Automation.PropertyCondition]::new(
            [System.Windows.Automation.AutomationElement]::ProcessIdProperty, $app.Id)
        $window = $root.FindFirst([System.Windows.Automation.TreeScope]::Children, $condition)

        Assert-That -Name 'the application window appears' -Condition ($null -ne $window) `
            -Expected 'a top-level window owned by the process' -Actual 'none found' `
            -Hint 'The binary started but never opened a window: check the Wails startup path.'
        if (-not $window) { return }

        # A WebView2 that failed to load renders a blank window that UI
        # Automation reports as an empty tree. That is the white-screen boot
        # this check exists to catch.
        $descendants = $window.FindAll([System.Windows.Automation.TreeScope]::Descendants,
            [System.Windows.Automation.Condition]::TrueCondition)
        Assert-That -Name 'the window exposes a non-empty accessibility tree' `
            -Condition ($descendants.Count -gt 0) `
            -Expected 'at least one accessible element inside the window' -Actual "$($descendants.Count)" `
            -Hint 'A blank tree means the WebView loaded nothing: check the embedded assets.'

        $rect = $window.Current.BoundingRectangle
        $bitmap = [Drawing.Bitmap]::new([int]$rect.Width, [int]$rect.Height)
        $graphics = [Drawing.Graphics]::FromImage($bitmap)
        $graphics.CopyFromScreen([int]$rect.X, [int]$rect.Y, 0, 0, $bitmap.Size)
        $path = Join-Path $artifactDir 'packaged-window.png'
        $bitmap.Save($path, [Drawing.Imaging.ImageFormat]::Png)
        $graphics.Dispose(); $bitmap.Dispose()
        Write-Host "  saved $path"
    } finally {
        if (-not $app.HasExited) { $app.CloseMainWindow() | Out-Null; Start-Sleep -Seconds 2 }
        if (-not $app.HasExited) { $app.Kill() }
    }
}

# --- Main -------------------------------------------------------------------

if ($Packaged) { Invoke-PackagedChecks } else { Invoke-DevChecks }

Write-Host ''
if ($script:Failures.Count -gt 0) {
    Write-Host "$($script:Failures.Count) UI check(s) failed:" -ForegroundColor Red
    $script:Failures | ForEach-Object { Write-Host "  - $_" }
    Write-Host "Screenshots and logs are in $artifactDir"
    exit 1
}

Write-Host 'All UI checks passed.' -ForegroundColor Green
if (-not $KeepArtifacts) {
    Remove-Item (Join-Path $artifactDir '*.png') -ErrorAction SilentlyContinue
}
exit 0

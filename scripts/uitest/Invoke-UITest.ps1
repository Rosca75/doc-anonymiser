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
                Test-ValuesTabLayout $cdp
                Test-BuiltInPatternsTab $cdp
                Test-ValueCardGeometry $cdp
                Test-SpellingsPopup $cdp
                Test-SignalDerivations $cdp
                Test-ConfigurePanelFit $cdp
                Test-StrictnessFields $cdp
                Test-HelpTooltip $cdp
                Test-ScrollRetention $cdp
                Test-TooltipVisibility $cdp
                Test-OriginLink $cdp
                Test-CompareSearch $cdp
                Test-SelectionPanel $cdp
                Test-ImageTab $cdp
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
    Assert-That -Name 'the rail is three route sections' -Condition ($r.sections -eq 3) `
        -Expected '3 .rail-section elements' -Actual "$($r.sections), routes: $($r.routes -join ', ')" `
        -Hint 'views/identifyrail.js RAIL_SECTIONS defines Built-in patterns, Heuristic discovery and Local LLM discovery, each bound to its own settings flag.'
    Assert-That -Name 'the one switch-less panel is a panel, not a route' -Condition ($r.panels -eq 1) `
        -Expected '1 .rail-panel element (Load profile)' -Actual "$($r.panels)" `
        -Hint 'Load profile is a utility rather than a detection route, so it may not wear .rail-section. It is the only panel: the confidence floor that used to sit beside it is one checkbox inside Built-in patterns now.'
    Assert-That -Name 'the checksum switch is inside Built-in patterns' -Condition ($r.checksumSwitch.section -eq 'rail-patterns') `
        -Expected '#require-checksum inside section#rail-patterns' -Actual "section $($r.checksumSwitch.section)" `
        -Hint 'It governs the built-in patterns own corroborating checksums and nothing else, so it belongs on the section that owns them.'
    Assert-That -Name 'the checksum switch is off by default' -Condition ($r.checksumSwitch.checked -eq $false) `
        -Expected '#require-checksum unchecked' -Actual "$($r.checksumSwitch.checked)" `
        -Hint 'state.js settings.requireChecksum defaults to false: keeping a match whose corroborating checksum failed is what the application has always done, because a mistyped or partly redacted bank identifier is still one.'
    Assert-That -Name 'the checksum switch is clickable, not merely present' -Condition (($r.checksumSwitch.laidOut -eq $true) -and ($r.checksumSwitch.disabled -eq $false)) `
        -Expected 'a laid-out, enabled checkbox' -Actual "laidOut=$($r.checksumSwitch.laidOut) disabled=$($r.checksumSwitch.disabled)" `
        -Hint 'It must be settable while the pattern pass itself is off, exactly as the category boxes are.'
    Assert-That -Name 'no confidence floor survives in the rail' -Condition ($r.confidenceSliders -eq 0) `
        -Expected '0 #min-confidence range inputs anywhere in the document' -Actual "$($r.confidenceSliders)" `
        -Hint 'The percentage was two unrelated questions wearing one control, and above roughly 0.8 it dropped Values the user had already accepted. A surviving slider would be a second answer.'
    Assert-That -Name 'the old tab strip is gone' -Condition ($r.railTabs -eq 0) `
        -Expected '0 [data-railtab] chips anywhere in the document' -Actual "$($r.railTabs)" `
        -Hint 'The rail switches sections on and off; it does not tab between them.'
    Assert-That -Name 'Built-in patterns is on by default' -Condition ($r.patternsOn -eq $true) `
        -Expected 'the rail-patterns route switch checked' -Actual "$($r.patternsOn)" `
        -Hint 'state.js settings.useBuiltInPatterns defaults to true: it needs nothing installed.'
    Assert-That -Name 'Heuristic discovery is on by default' -Condition ($r.heuristicOn -eq $true) `
        -Expected 'the rail-heuristic route switch checked' -Actual "$($r.heuristicOn)" `
        -Hint 'state.js settings.useHeuristicDiscovery defaults to true: it needs nothing installed.'
    Assert-That -Name 'Local LLM discovery is off by default' -Condition ($r.localOn -eq $false) `
        -Expected 'the rail-local route switch unchecked' -Actual "$($r.localOn)" `
        -Hint 'state.js settings.useLocalLLM defaults to false. Detecting Ollama ENABLES this switch, it never flips it.'
    Assert-That -Name 'every category checkbox is present' `
        -Condition ($r.categories -ge $script:Fixture.categoryCount) `
        -Expected "at least $($script:Fixture.categoryCount) .cat-toggle checkboxes" -Actual "$($r.categories)" `
        -Hint 'Every SWITCHABLE state.js category must reach the rail. custom_patterns is excluded on purpose: it is declarative, permanently on, and edited on the workspace Custom patterns tab.'
    Assert-That -Name 'the category groups are folded by default' `
        -Condition ($r.categories -gt 0 -and $r.categoriesWithSize -eq 0) `
        -Expected 'no category checkbox laid out until its group is opened' `
        -Actual "$($r.categoriesWithSize) of $($r.categories) have a height while nothing was clicked" `
        -Hint 'views/identifyrail.js seeds collapsedGroups with every cat-group id so the rail opens on the route switches and the scope summary, not a wall of category lists.'
    Assert-That -Name 'opening a category group lays out its checkboxes' `
        -Condition ($r.categories -gt 0 -and $r.categoriesWithSizeAfterExpand -eq $r.categories) `
        -Expected 'every category checkbox laid out once its group is opened' `
        -Actual "$($r.categoriesWithSizeAfterExpand) of $($r.categories) have a height after opening every group" `
        -Hint 'A folded group is only useful if it opens: collapsibleGroup + wireGroups reveal the checkboxes.'
    # The signal control is a tree hanging off the category row of the signal it
    # reads, built from the frontend lists the Go parity guard holds to the engine.
    # One drill-down per signal, one master per drill-down.
    $signalRows = @($r.signalRows)
    $signalMasters = @($r.signalMasters)
    $signalSources = @($r.signalSources)
    Assert-That -Name 'each signal has a drill-down on its own category row' `
        -Condition ($signalSources.Count -gt 0 -and ($signalRows -join ',') -eq ($signalSources -join ',')) `
        -Expected "one .signal-row per signal source: $($signalSources -join ', ')" `
        -Actual "$($signalRows.Count): $($signalRows -join ', ')" `
        -Hint 'views/identifyrail.js hangs a signalDrillDown off the category row of every state.js SIGNAL_SOURCES entry. The expectation is READ from the store, because a hardcoded count is left behind by the next source.'
    Assert-That -Name 'every drill-down has its own master switch' `
        -Condition ($signalMasters.Count -eq $signalRows.Count) `
        -Expected 'one .signal-master per drill-down' `
        -Actual "$($signalMasters.Count) masters for $($signalRows.Count) drill-downs" `
        -Hint 'The master is what saves switching a whole signal off one reading at a time.'
    # "On the row" is geometry: markup order proves nothing, since a row narrower
    # than its contents wraps the same markup onto two lines.
    $line = $r.signalRowLine
    if ($null -eq $line) {
        Assert-That -Name 'the signal row renders its drill-down and help icon' -Condition $false `
            -Expected '.cat-label, .signal-drill and span.help in one .signal-row-head' -Actual 'one of them is missing' `
            -Hint 'views/identifyrail.js signalCategoryRow passes the row, the button and the help tooltip to ui.js signalDrillDown.'
    }
    else {
        Assert-That -Name 'the drill-down and its help icon sit on the category row' -Condition ($line.sameRow -eq $true) `
            -Expected 'the label, the button and the icon at the same y (within 2px)' `
            -Actual "sameRow=$($line.sameRow) ($($line.widths))" `
            -Hint 'style.css .signal-row-head is one flex line. A drill-down that wraps below its own label no longer reads as belonging to it.'
        Assert-That -Name 'they are ordered label, drill-down, help icon' `
            -Condition ($line.drillIsAfterLabel -eq $true -and $line.helpIsAfterDrill -eq $true) `
            -Expected 'the button after the label, the icon after the button' `
            -Actual "drillAfterLabel=$($line.drillIsAfterLabel), helpAfterDrill=$($line.helpIsAfterDrill)" `
            -Hint 'The icon explains what the button opens, so it follows the button.'
        Assert-That -Name 'the row still fits the rail' -Condition ($line.fitsTheRail -eq $true) `
            -Expected 'no horizontal overflow in .signal-row-head' -Actual "$($line.widths)" `
            -Hint 'The rail is the narrowest column in the application and the page body never scrolls sideways.'
    }
    # Each route's switch is ON that route's own header, beside its title and its
    # help icon. That the three fit one line each is a claim about geometry, so
    # geometry answers it: a column-flex header stacks the same markup, and a title
    # clipped by the controls beside it is a route whose name cannot be read.
    Assert-That -Name 'every route section has a measurable header' `
        -Condition ($r.routeHeaders.Count -eq 3 -and -not ($r.routeHeaders -contains $null)) `
        -Expected '3 headers, each carrying a .cgroup-title, a .route-toggle and a .route-state' `
        -Actual "$($r.routeHeaders.Count) measured" `
        -Hint 'views/identifyrail.js railBody puts the help tooltip and routeSwitch in each section headRightHTML.'
    foreach ($head in $r.routeHeaders) {
        if ($null -eq $head) { continue }
        Assert-That -Name "$($head.route): title and switch share one row" `
            -Condition ($head.sameRow -eq $true -and $head.switchIsToTheRight -eq $true) `
            -Expected 'the switch centred on the title line and starting after it ends' `
            -Actual "sameRow=$($head.sameRow), toTheRight=$($head.switchIsToTheRight)" `
            -Hint 'style.css .cgroup-head is a flex row with the head-right group at its end.'
        Assert-That -Name "$($head.route): the route name is not clipped" `
            -Condition ($head.titleFullyShown -eq $true -and $head.titleLines -eq 1 -and $head.fitsTheRail -eq $true) `
            -Expected 'the whole title on one line, no horizontal overflow' `
            -Actual "$($head.titleFullyShown), lines=$($head.titleLines), fits=$($head.fitsTheRail) ($($head.widths))" `
            -Hint 'copy.js RAIL.tabPatterns, tabHeuristic and tabLocalLLM stay short precisely because the rail is the narrowest column and each title shares its row with a help icon and an On/Off switch.'
        Assert-That -Name "$($head.route): the switch is explained beside it" `
            -Condition ($head.hasHelp -eq $true) `
            -Expected 'a help tooltip on the section header' -Actual "$($head.hasHelp)" `
            -Hint 'The switch says whether the mechanism runs; the tooltip says what it is, and the panel carries no explanatory prose.'
    }
}

# Opening a signal's drill-down must REVEAL its readings, and each must be
# switchable on its own. Collapsed the readings are in the DOM at zero height:
# present to a string test and absent to the user, so only this layer can tell the
# two states apart.
function Test-SignalDerivations([CdpSession]$cdp) {
    Write-Step "Opening a signal's drill-down reveals its readings, each switchable on its own"
    $r = $cdp.Eval('__uiProbes.signalDerivations()')
    if ($r.PSObject.Properties.Name -contains 'error' -and $r.error) {
        Assert-That -Name 'the signal-derivation probe runs' -Condition $false `
            -Expected '.signal-row in the Identify rail' -Actual $r.error `
            -Hint 'views/identifyrail.js signalCategoryRow renders ui.js signalDrillDown.'
        return
    }
    Assert-That -Name 'a collapsed drill-down costs no vertical space' `
        -Condition ($r.collapsedRows -gt 0 -and $r.collapsedVisible -eq 0 -and $r.rowLaidOut -eq $true) `
        -Expected "the category row laid out, its $($r.collapsedRows) readings not" `
        -Actual "row laid out=$($r.rowLaidOut), $($r.collapsedRows) readings, $($r.collapsedVisible) with a height" `
        -Hint 'Collapsed the readings cost no row at all. That trade is what keeps the panel short as signals and readings are added.'
    Assert-That -Name 'opening it reveals every reading' -Condition ($r.openedVisible -eq $r.collapsedRows) `
        -Expected "all $($r.collapsedRows) readings laid out with a height after one click" `
        -Actual "$($r.openedVisible) of $($r.collapsedRows)" `
        -Hint 'A checkbox in the DOM at zero height is not something the user can tick.'
    Assert-That -Name 'ticking a reading reaches the store' -Condition ($r.readingWentOff -eq $true) `
        -Expected "$($r.derivation) stored as off" -Actual "$($r.readingWentOff)" `
        -Hint 'state.js setSignalDerivation writes the leaf, and that is what the next detection run reads.'
    Assert-That -Name 'the other readings are untouched' -Condition ($r.otherReadingsStillOn -eq $true) `
        -Expected 'every other reading of that signal still on' -Actual "$($r.otherReadingsStillOn)" `
        -Hint 'The independence is the whole point of the per-reading switches; the engine honours each on its own.'
    Assert-That -Name 'ticking a reading does not close the drill-down' -Condition ($r.groupStayedOpenAfterTicking -eq $true) `
        -Expected 'the readings still laid out after the tick' -Actual "$($r.groupStayedOpenAfterTicking)" `
        -Hint 'The checkbox stops the click reaching anything else. A drill-down that closes as you tick it makes switching two readings a four-click job.'
    Assert-That -Name 'the master stays on while any reading is on' -Condition ($r.masterAfterTick -eq $true) `
        -Expected "the drill-down master still checked with one of two readings off" -Actual "$($r.masterAfterTick)" `
        -Hint 'The master is DERIVED (state.js signalSourceOn): on when any reading is on.'
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

# The My values tab reads as two captioned blocks, and a Ctrl+click on a card is
# VISIBLE. Three pixel claims a markup test can only predict: a flex row that
# wrapped renders both controls in the right parent and still stacks them; a
# stray bold weight from a shared class survives every string assertion; and the
# .selected class is on the element whether or not anything paints, so only a
# computed background says the user can see what they picked.
function Test-ValuesTabLayout([CdpSession]$cdp) {
    Write-Step 'My values reads as two blocks, and a Ctrl+click picks a card visibly'
    $r = $cdp.Eval('__uiProbes.valuesTabLayout()')
    if ($r.PSObject.Properties.Name -contains 'error' -and $r.error) {
        Assert-That -Name 'the My values layout probe runs' -Condition $false `
            -Expected "the tab's filter and add controls" -Actual $r.error `
            -Hint 'views/identifyworkspace.js valuesTab renders the FILTERS and VALUES blocks.'
        return
    }
    Assert-That -Name 'the tab captions its two blocks, filters first' `
        -Condition ($r.captions.Count -eq 2 -and $r.captions[0] -eq 'Filters' -and $r.captions[1] -eq 'Values') `
        -Expected 'Filters then Values' -Actual "$($r.captions -join ', ')" `
        -Hint 'copy.js WORKSPACE.valuesFiltersHeading and valuesHeading, each through ui.js sectionLabel inside its own .values-section.'
    Assert-That -Name 'the search and the type filter sit on one row' `
        -Condition ($r.filterRowOffset -le 2) `
        -Expected 'centre lines within 2px' -Actual "$($r.filterRowOffset)px apart" `
        -Hint 'style.css .values-toolbar is one flex row. A control that wrapped no longer reads as part of the filter.'
    Assert-That -Name 'the bulk clear sits on the same row as Add value' `
        -Condition ($r.addRowOffset -le 2) `
        -Expected 'centre lines within 2px' -Actual "$($r.addRowOffset)px apart" `
        -Hint 'Both buttons live in .add-row, with the growing input between them.'
    Assert-That -Name 'the type filter is not bold' `
        -Condition ($r.filterWeight -gt 0 -and $r.filterWeight -lt 700) `
        -Expected 'a regular weight' -Actual "font-weight $($r.filterWeight)" `
        -Hint 'The filter must not take .head-select, the borderless BOLD spelling reserved for a filter inside a table header row.'
    Assert-That -Name "the type filter matches the add row's own category dropdown" `
        -Condition ($r.filterWeight -eq $r.categoryWeight -and $r.filterFontSize -eq $r.categoryFontSize) `
        -Expected "weight $($r.categoryWeight) at $($r.categoryFontSize)" `
        -Actual "weight $($r.filterWeight) at $($r.filterFontSize)" `
        -Hint 'style.css .values-type-filter carries the same size as .add-row select: both are one control picking a category.'
    Assert-That -Name 'a Ctrl+click tints the card it landed on' `
        -Condition ($r.selectedBg -eq 'rgb(238, 245, 255)') `
        -Expected 'rgb(238, 245, 255) (brand.css --selected-bg)' -Actual "$($r.selectedBg)" `
        -Hint 'style.css .value-card.selected sets the background. A selection the user cannot see is a bulk action aimed at nothing.'
    Assert-That -Name 'only that card is tinted' `
        -Condition ($r.selectedCount -eq 1 -and $r.othersTinted -eq $r.plainBg) `
        -Expected 'one .value-card.selected, the rest unchanged' `
        -Actual "$($r.selectedCount) selected, neighbour painted $($r.othersTinted)" `
        -Hint 'toggleValueSelection stores ONE key per Ctrl+click.'
    Assert-That -Name 'the bulk button says which of its two scopes the next press uses' `
        -Condition ($r.clearLabelPlain -eq 'Clear all' -and $r.clearLabelPicked -eq 'Clear selected') `
        -Expected '"Clear all" with nothing picked, "Clear selected" with a card picked' `
        -Actual "$($r.clearLabelPlain) then $($r.clearLabelPicked)" `
        -Hint 'clearValuesButton reads the selection, so the label cannot promise a scope the press does not use.'
    Assert-That -Name 'the same gesture lets the card go again' `
        -Condition ($r.selectedAfterUndo -eq 0 -and $r.clearLabelUndone -eq 'Clear all') `
        -Expected 'nothing selected, button back to "Clear all"' `
        -Actual "$($r.selectedAfterUndo) selected, button $($r.clearLabelUndone)" `
        -Hint 'Ctrl+click toggles. A selection with no way back turns a mis-click into a destroyed list.'
}

# The read-only Built-in patterns tab, with a match long enough to threaten the
# card's width. A URL with no spaces in it and a full street address are the
# NORMAL content of this tab, not an edge case, and both are what widens a card
# past the window. The grouping is covered by the frontend suite; what needs a
# renderer is whether the row fits and whether the occurrence note beside it is
# still on screen.
function Test-BuiltInPatternsTab([CdpSession]$cdp) {
    Write-Step 'Built-in patterns is read-only, and a long match still fits the card'
    $r = $cdp.Eval('__uiProbes.builtInPatternsTabLayout()')
    if ($r.PSObject.Properties.Name -contains 'error' -and $r.error) {
        Assert-That -Name 'the Built-in patterns probe runs' -Condition $false `
            -Expected 'a section per active category' -Actual $r.error `
            -Hint 'views/identifyworkspace.js builtInPatternsTab renders one .builtin-group per active category.'
        return
    }
    Assert-That -Name 'one section per ACTIVE category, empty ones included' `
        -Condition ($r.groups -eq 3 -and $r.emptyGroups -eq 1) `
        -Expected '3 sections, 1 of them empty' -Actual "$($r.groups) sections, $($r.emptyGroups) empty" `
        -Hint 'The sections come from the categories that RAN: one that ran and matched nothing must not look like one that never ran.'
    Assert-That -Name 'no row offers an accept, a reject or an edit' `
        -Condition ($r.actions -eq 0) `
        -Expected '0 controls on the rows' -Actual "$($r.actions) controls" `
        -Hint 'A built-in pattern produces DIRECT matches, applied without review: a control here promises a decision the tab cannot take.'
    Assert-That -Name 'a long match does not scroll the page sideways' `
        -Condition ($r.pageScrollsSideways -eq $false) `
        -Expected 'no horizontal page scroll' -Actual 'the page scrolls sideways' `
        -Hint 'style.css .builtin-text wraps with overflow-wrap: anywhere. A URL is one long unbreakable word until something says otherwise.'
    Assert-That -Name 'the row stays inside the card that holds it' `
        -Condition ($r.widestRowRight -le ($r.cardRight + 1)) `
        -Expected "every row within $($r.cardRight)px" -Actual "the widest row reaches $($r.widestRowRight)px" `
        -Hint 'The card body is the scroller; a row wider than it is content that escaped.'
    Assert-That -Name 'the occurrence note stays on its row' `
        -Condition ($r.noteInside -eq $true) `
        -Expected 'the note inside the row' -Actual 'the note pushed past the row right edge' `
        -Hint 'style.css .builtin-where is flex: none beside a wrapping text, so the note keeps its place.'
    Assert-That -Name 'the rows scroll inside the card body, not sideways' `
        -Condition ($r.bodyScrollsSideways -eq $false) `
        -Expected 'vertical scrolling only' -Actual 'the card body scrolls sideways' `
        -Hint 'The layout contract is that scrolling happens inside a card body, downwards.'
}

# A value card keeps its HEIGHT whatever its data, and the list keeps its place.
# The reported symptom was the My values scrollbar jumping upward after editing a
# spelling or deleting a card. The scroll preserver was not broken: it writes back
# a raw pixel offset, which is right only while the content is the same height.
# Editing a spelling sent the row back to pending, the chips were replaced by one
# line of text, the card shrank, the browser CLAMPED the restored offset to the
# shorter scrollHeight, and the next repaint snapshotted the clamped value.
function Test-ValueCardGeometry([CdpSession]$cdp) {
    Write-Step 'A value card keeps its height, and the list keeps its place'
    $r = $cdp.Eval('__uiProbes.valueCardGeometry()')
    if ($r.PSObject.Properties.Name -contains 'error' -and $r.error) {
        Assert-That -Name 'the card-geometry probe runs' -Condition $false `
            -Expected 'a My values list long enough to scroll' -Actual $r.error `
            -Hint 'The probe seeds fifteen Values inside #identify-workspace .card-body.'
        return
    }
    Assert-That -Name 'the measured card has more spellings than fit on its line' `
        -Condition ($r.overflowsChips -eq $true) `
        -Expected 'a "+N more" control on the card' -Actual "$($r.overflowsChips)" `
        -Hint 'The chip row spends a character budget (identifyworkspace.js SPELLING_PREVIEW_BUDGET). Without an overflow this check measures nothing.'
    Assert-That -Name "the popup's own list scrolls rather than growing the popup" `
        -Condition ($r.listScrolls -eq $true) `
        -Expected 'a .spelling-list taller than its box' -Actual "$($r.listScrolls)" `
        -Hint 'style.css .spelling-list caps its height and scrolls.'
    Assert-That -Name 'a spelling was actually deleted, so the row went back to pending' `
        -Condition ($r.deleted -eq $true) `
        -Expected 'one spelling removed through the popup' -Actual "$($r.deleted)" `
        -Hint "The popup's per-row Delete calls deleteVariant, which is the edit that used to collapse the chip row."
    Assert-That -Name 'the card is the same height after a spelling edit' `
        -Condition ($r.heightBefore -gt 0 -and $r.heightAfterEdit -eq $r.heightBefore) `
        -Expected "still $($r.heightBefore)px" -Actual "$($r.heightAfterEdit)px" `
        -Hint 'style.css .spelling-row is one line, overflow:hidden, with a min-height, so a pending expansion swaps the chips for a line of text INSIDE the row.'
    Assert-That -Name 'the list keeps its scroll position across a spelling edit' `
        -Condition ($r.scrollBefore -gt 0 -and $r.scrollAfterEdit -eq $r.scrollBefore) `
        -Expected "scrollTop still $($r.scrollBefore)" -Actual "$($r.scrollAfterEdit)" `
        -Hint 'scroll.js restores a raw pixel offset, which the browser clamps when the content got shorter.'
    # Renaming is two cases, because the sentinel a rename writes depends on the
    # row's spelling POLICY (valuemodel.js repend).
    Assert-That -Name 'renaming a CURATED value leaves it settled, never pending' `
        -Condition ($r.curatedSettled -eq $true) `
        -Expected 'the renamed Value still curated, with a settled spelling list' -Actual "$($r.curatedSettled)" `
        -Hint 'pendingExpansions skips curated rows, so sending one back to pending means no expansion is ever requested and nothing clears the sentinel: the card claims to be working forever over chips that are already correct.'
    Assert-That -Name 'the card is the same height after a rename' `
        -Condition ($r.heightRenamed -gt 0 -and $r.heightRenamed -eq $r.heightAfterEdit) `
        -Expected "still $($r.heightAfterEdit)px" -Actual "$($r.heightRenamed)px" `
        -Hint 'Whatever the row lands on, the chip row swaps its contents INSIDE itself, never as a row under it.'
    Assert-That -Name 'the list keeps its scroll position across a rename' `
        -Condition ($r.scrollRenamed -eq $r.scrollAfterEdit) `
        -Expected "scrollTop still $($r.scrollAfterEdit)" -Actual "$($r.scrollRenamed)" -Hint ''
    Assert-That -Name 'renaming an AUTOMATIC value sends its spellings back to pending' `
        -Condition ($r.pending -eq $true) `
        -Expected 'derivedSpellings null on the renamed automatic Value' -Actual "$($r.pending)" `
        -Hint 'This is where the pending state lives now, and it is the case with the most teeth for the layout: with nothing settled to draw, the chip row falls back to one line of text.'
    Assert-That -Name 'the card is the same height while its spellings are pending' `
        -Condition ($r.heightAutoBefore -gt 0 -and $r.heightAutoPending -eq $r.heightAutoBefore) `
        -Expected "still $($r.heightAutoBefore)px" -Actual "$($r.heightAutoPending)px" `
        -Hint 'The pending line renders INSIDE the chip row, in place of the chips, never as a row under it.'
    Assert-That -Name 'a warning renders as an icon on the card' `
        -Condition ($r.hasWarningIcon -eq $true) `
        -Expected 'a .warnpop on the card' -Actual "$($r.hasWarningIcon)" `
        -Hint 'ui.js warningPopover: the warning text and its actions live in a hover surface, not in a row.'
    Assert-That -Name 'the card is the same height once a warning appears' `
        -Condition ($r.heightWarned -gt 0 -and $r.heightWarned -eq $r.heightRenamed) `
        -Expected "still $($r.heightRenamed)px" -Actual "$($r.heightWarned)px" `
        -Hint 'A warning rendered as a row makes the card taller when it arrives and shorter when it clears.'
    Assert-That -Name 'the list keeps its scroll position when a warning appears' `
        -Condition ($r.scrollWarned -eq $r.scrollAutoPending) `
        -Expected "scrollTop still $($r.scrollAutoPending)" -Actual "$($r.scrollWarned)" -Hint ''
    $drift = [Math]::Abs($r.scrollBeforeDelete - $r.scrollAfterDelete)
    Assert-That -Name 'deleting a card moves the list by at most one card' `
        -Condition ($r.cardHeight -gt 0 -and $drift -le $r.cardHeight) `
        -Expected "scrollTop within $($r.cardHeight)px of $($r.scrollBeforeDelete)" `
        -Actual "$($r.scrollAfterDelete) (moved $($drift)px)" `
        -Hint 'The list really is shorter by one card, so a small clamp is expected. Jumping to the top is not.'
}

# The spellings popup opens, is genuinely on screen, scrolls inside itself, and
# updates the card behind it live. The card shows only the spellings that fit one
# line, so the popup is the only way to reach the rest: a surface that is in the
# DOM but clipped, or one that grows past the window, makes them unreachable while
# every string test stays green.
function Test-SpellingsPopup([CdpSession]$cdp) {
    Write-Step 'The spellings popup opens, scrolls, and updates the card live'
    $r = $cdp.Eval('__uiProbes.spellingsPopup()')
    if ($r.PSObject.Properties.Name -contains 'error' -and $r.error) {
        Assert-That -Name 'the spellings-popup probe runs' -Condition $false `
            -Expected 'a .spellings-popup opened from "+N more"' -Actual $r.error `
            -Hint 'The "+N more" handler calls openSpellingsPopup, which sets the view state the workspace renders spellingsPopupHTML from.'
        return
    }
    Assert-That -Name '"+N more" says how many are hidden' `
        -Condition ("$($r.moreLabel)".StartsWith('+')) `
        -Expected 'a label of the form "+N more"' -Actual "$($r.moreLabel)" `
        -Hint 'copy.js WORKSPACE.moreSpellings(n). A control that does not say how much is behind it is a control nobody opens.'
    Assert-That -Name 'the popup is painted, not clipped away' -Condition ($r.painted -eq $true) `
        -Expected "elementFromPoint at the popup's centre returning the popup itself" `
        -Actual "$($r.painted)" `
        -Hint 'The rect of a clipped element is still a full-size rect. The popup is rendered OUTSIDE the scrolling card body for exactly this reason.'
    Assert-That -Name 'the popup is on screen' -Condition ($r.onScreen -eq $true) `
        -Expected "a box inside the $($r.viewportHeight)px viewport" -Actual "$($r.onScreen)" -Hint ''
    Assert-That -Name 'the popup fits the window' `
        -Condition ($r.popupHeight -gt 0 -and $r.popupHeight -le $r.viewportHeight) `
        -Expected "a popup no taller than $($r.viewportHeight)px" -Actual "$($r.popupHeight)px" `
        -Hint 'style.css .spellings-popup caps its height and the list inside it scrolls.'
    Assert-That -Name 'the list scrolls INSIDE the popup' -Condition ($r.listScrolls -eq $true) `
        -Expected 'a .spelling-list taller than its box' -Actual "$($r.listScrolls)" -Hint ''
    Assert-That -Name 'and it really scrolls when scrolled' -Condition ($r.listScrolled -eq $true) `
        -Expected 'a non-zero scrollTop on the .spelling-list' -Actual "$($r.listScrolled)" `
        -Hint 'An overflowing element with overflow:hidden reports the same scrollHeight and moves nowhere, so the two checks are separate.'
    Assert-That -Name 'adding in the popup reaches the Value' -Condition ($r.onValueAfter -eq $true) `
        -Expected 'the new spelling in state.values' -Actual "$($r.onValueAfter)" `
        -Hint "The popup's Add calls addSpelling(category, mainText, value) from the open popup's own identity."
    Assert-That -Name 'and the compact card behind it updates on the same repaint' `
        -Condition ($r.chipsAfter -contains 'Zzz Popup Spelling') `
        -Expected "the new spelling among the card's chips" -Actual "$($r.chipsAfter -join ', ')" `
        -Hint 'The popup and the card read the same store, which is what makes the edit live.'
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

# The Discovery strictness block must be readable and aligned. .rail-field gave
# the control column 6rem, narrower than the strictness select's own longest
# option, and .cgroup-body carries no padding of its own, so a nested subgroup's
# fields sat flush against its border while every label above them was inset.
function Test-StrictnessFields([CdpSession]$cdp) {
    Write-Step 'The Discovery strictness fields are readable and aligned'
    $r = $cdp.Eval('__uiProbes.strictnessFields()')
    if ($r.PSObject.Properties.Name -contains 'error' -and $r.error) {
        Assert-That -Name 'the strictness probe runs' -Condition $false `
            -Expected 'the Discovery strictness block in the rail' -Actual $r.error `
            -Hint 'views/identifyrail.js heuristicSection nests it under Heuristic discovery.'
        return
    }
    Assert-That -Name 'the strictness select shows its longest option in full' `
        -Condition ($r.selectFitsWidestOption -eq $true) `
        -Expected "a box at least as wide as '$($r.widestText)' ($($r.widestOption)px)" `
        -Actual "$($r.selectWidth)px of box for $($r.widestOption)px of text" `
        -Hint 'style.css .rail-field sizes the control column. A column narrower than the widest option truncates it to an unreadable stub.'
    Assert-That -Name 'the nested fields are inset like the labels above them' `
        -Condition ($null -ne $r.nestedLabelLeft -and $null -ne $r.sectionLabelLeft -and $r.nestedLabelLeft -ge $r.sectionLabelLeft) `
        -Expected "a nested label at x >= $($r.sectionLabelLeft)px" -Actual "x = $($r.nestedLabelLeft)px" `
        -Hint 'style.css .rail-subgroup > .cgroup-body carries the inset; .cgroup-body has no padding of its own.'
    $lefts = @($r.fieldLabelLefts)
    Assert-That -Name 'every field in the block shares one inset' `
        -Condition ($lefts.Count -gt 0 -and (($lefts | Select-Object -Unique).Count -eq 1)) `
        -Expected 'all .rail-field-label left offsets equal' -Actual "$($lefts -join ', ')" `
        -Hint 'They are one form. A field at a different offset reads as a mistake.'
    $wrapped = @($r.labels | Where-Object { $_.lineHeight -gt 0 -and $_.height -gt ($_.lineHeight * 1.5) })
    Assert-That -Name 'no field label wraps to a second line' -Condition ($wrapped.Count -eq 0) `
        -Expected 'every .rail-field-label on one line' `
        -Actual "$($wrapped.Count) wrapped: $(($wrapped | ForEach-Object { $_.text }) -join ', ')" `
        -Hint 'style.css .rail-field splits the row between the label and the control. Widening the control column narrows the label, and the labels are where that trade shows first.'
    Assert-That -Name 'widening the control did not widen the rail' -Condition ($r.railOverflowsX -eq $false) `
        -Expected 'a rail no wider than its column' -Actual "$($r.railOverflowsX)" `
        -Hint 'The fixed-height layout contract: wide content scrolls inside its own container and never widens the page.'
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
    Assert-That -Name 'the help trigger has a glyph in it' -Condition ($r.trigger.hasGlyph -eq $true) `
        -Expected 'an <svg> inside button.help-icon' -Actual "$($r.trigger.hasGlyph)" `
        -Hint 'ui.js helpTooltip renders icon("info"), and icon() returns the EMPTY STRING for a name absent from frontend/icons.js ICONS, so the trigger is an invisible hit area. icon_parity_test.go is the cheap guard; this is the one that sees the result.'
    Assert-That -Name 'the trigger is big enough to aim at' `
        -Condition ($r.trigger.width -ge 14 -and $r.trigger.height -ge 14) `
        -Expected 'a trigger at least 14x14 CSS pixels' -Actual "$($r.trigger.width)x$($r.trigger.height)" `
        -Hint 'style.css .help-icon sizes it; a trigger smaller than this is a target nobody hits.'
    Assert-That -Name 'the glyph is painted at a readable size' `
        -Condition ($r.trigger.glyphWidth -ge 10 -and $r.trigger.glyphHeight -ge 10) `
        -Expected 'a glyph at least 10x10 CSS pixels' -Actual "$($r.trigger.glyphWidth)x$($r.trigger.glyphHeight)" `
        -Hint 'An svg present in the DOM at zero size is the same invisible control with extra markup.'
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

# The hover link between the two Compare panes. Same reason as the tooltip
# check: the string and wiring suites can prove the spans are emitted and the
# class is toggled, and only a real engine can prove the class resolves to a
# background the user can see rather than to nothing at all.
function Test-OriginLink([CdpSession]$cdp) {
    Write-Step 'Hovering a placeholder tints what it replaced in the original pane'
    $r = $cdp.Eval('__uiProbes.originLink()')
    if ($r.PSObject.Properties.Name -contains 'error' -and $r.error) {
        Assert-That -Name 'the two panes can be linked by a hover' -Condition $false `
            -Expected 'a .value-origin per stretch the run replaced' -Actual $r.error `
            -Hint 'valuespans.js valueSpans reads state.mapping and the document occurrenceSpellings.'
        return
    }
    Assert-That -Name 'the original pane marks every stretch the run replaced' -Condition ($r.spans -gt 0) `
        -Expected 'at least one .value-origin in #original-pane' -Actual "$($r.spans) spans" `
        -Hint 'valuespans.js renderOriginWithSpans wraps each span valueSpans found.'
    Assert-That -Name 'nothing is tinted before the pointer arrives' -Condition ($r.litBefore -eq 0) `
        -Expected 'no .is-linked span in the resting pane' -Actual "$($r.litBefore) already lit" `
        -Hint 'The tint is a hover state: the pane reads as plain text until a mark is hovered.'
    Assert-That -Name 'the hover lights the WHOLE family, main text and spellings alike' `
        -Condition (($r.family -gt 1) -and ($r.lit -eq $r.family)) `
        -Expected "all $($r.family) spans of $($r.placeholder) lit" -Actual "$($r.lit) of $($r.family) lit" `
        -Hint 'views/anonymise.js wireOriginLink matches on data-ph, which every spelling of one Value shares.'
    Assert-That -Name 'the hover lights nothing belonging to another Value' -Condition ($r.bled -eq 0) `
        -Expected 'no span of a different placeholder lit' -Actual "$($r.bled) lit" `
        -Hint 'Compare span.dataset.ph with the hovered mark own placeholder, not merely its category.'
    Assert-That -Name 'the tint is a real painted colour, not a class nothing styles' `
        -Condition ($r.background -and ($r.background -ne $r.plainBackground)) `
        -Expected "a resolved background differing from an untinted span's" `
        -Actual "lit '$($r.background)', untinted '$($r.plainBackground)'" `
        -Hint 'style.css .value-origin.is-linked must set a background from a token brand.css defines.'
    Assert-That -Name 'the tinted span has a size' -Condition ($r.hasSize) `
        -Expected 'a span with width and height' `
        -Actual "$($r.spanRect.width)x$($r.spanRect.height)" `
        -Hint 'A zero-sized span means the wrapper was emitted around nothing.'
    Assert-That -Name "the tinted span is inside the pane's visible box" -Condition ($r.insidePane) `
        -Expected 'the span rect within the pane rect' `
        -Actual "span top $($r.spanRect.top), pane top $($r.paneRect.top)" `
        -Hint 'The probe scrolls the span into view first, so a failure means the pane refused to scroll.'
    Assert-That -Name 'the tinted span is painted, not covered' -Condition ($r.paintedOnTop) `
        -Expected 'elementFromPoint at the span centre returning the span' -Actual 'something else is on top' `
        -Hint 'A search hit or another overlay is painting over the origin tint.'
    Assert-That -Name 'leaving the mark clears the tint' -Condition ($r.litAfter -eq 0) `
        -Expected 'no .is-linked span after mouseleave' -Actual "$($r.litAfter) still lit" `
        -Hint 'wireOriginLink binds mouseleave (and blur) to the same clear.'
}

# The Compare search. Like the tooltip check, the interesting half cannot be
# asserted without a renderer: a string test proves the hit span was emitted,
# but only a real engine can prove the pane scrolled to it rather than leaving
# it clipped below the fold.
function Test-CompareSearch([CdpSession]$cdp) {
    Write-Step "Each Compare pane's own search bar finds text and shows the active hit"
    $r = $cdp.Eval('__uiProbes.compareSearch()')
    if ($r.PSObject.Properties.Name -contains 'error' -and $r.error) {
        Assert-That -Name "each pane's search box renders and accepts a needle" -Condition $false `
            -Expected '#compare-search-original and #compare-search-anonymised in the pane captions' -Actual $r.error `
            -Hint 'views/anonymise.js paneCaption renders a paneSearchControls bar aligned right in each caption.'
        return
    }

    # Each pane is asserted the same way against its own bar: the two searches are
    # independent, so neither may borrow the other's hit or active state.
    foreach ($pane in @('original', 'anonymised')) {
        $p = $r.panes.$pane

        Assert-That -Name "the needle is found in the $pane pane" -Condition ($p.hits -gt 0) `
            -Expected "at least one .find-hit in #$pane-pane for '$($r.needle)'" `
            -Actual "$($p.hits) hits" `
            -Hint 'the pane renders its hits from its own paneWalk (renderPlainWithHits for the original, renderHighlighted search argument for the anonymised).'

        Assert-That -Name "the $pane pane has an active hit" -Condition ($p.hasActive) `
            -Expected 'a .find-hit.active inside the pane' -Actual 'no active hit was rendered' `
            -Hint 'paneWalk resolves this pane active index; the caption bar and the pane read the same walk.'

        Assert-That -Name "the $pane pane readout says the count and total" -Condition ($p.readout) `
            -Expected 'a readout naming the count and the total' -Actual 'an empty readout' `
            -Hint 'copy.js ANONYMISE.searchCount(index, total).'

        Assert-That -Name "the $pane pane navigation buttons are live when there are hits" `
            -Condition ($p.nextEnabled -and $p.prevEnabled) `
            -Expected 'next and previous enabled' `
            -Actual "next enabled: $($p.nextEnabled), previous enabled: $($p.prevEnabled)" `
            -Hint 'paneSearchControls disables them only when the pane walk is empty, and gives them a title saying why.'

        if (-not $p.visible) {
            Assert-That -Name "the $pane pane active hit sits inside the pane" -Condition $false `
                -Expected 'the active hit inside the .pane-body' -Actual 'the active hit has no .pane-body ancestor' `
                -Hint 'The pane renders its hits inside its own .pane-body.'
            continue
        }

        $hit = "$($p.visible.activeRect.left),$($p.visible.activeRect.top) to $($p.visible.activeRect.right),$($p.visible.activeRect.bottom)"
        $paneRect = "$($p.visible.paneRect.left),$($p.visible.paneRect.top) to $($p.visible.paneRect.right),$($p.visible.paneRect.bottom)"

        Assert-That -Name "the $pane pane active hit has a size" -Condition ($p.visible.hasSize) `
            -Expected 'a rendered span with width and height' -Actual "hit $hit" `
            -Hint 'An empty hit span means the slice offsets and the text disagree.'

        Assert-That -Name "the $pane pane active hit is visible INSIDE the pane, not scrolled out of sight" `
            -Condition ($p.visible.insidePane) `
            -Expected 'the active hit rect within its pane rect' -Actual "hit $hit, pane $paneRect" `
            -Hint 'views/anonymise.js scrollToActiveHit runs per pane after the paint and only when that pane active index changed, so it does not fight scroll.js restoring the offset.'

        Assert-That -Name "the $pane pane active hit is on screen" -Condition ($p.visible.inViewport) `
            -Expected 'the active hit rect within the viewport' -Actual "hit $hit" `
            -Hint 'scrollIntoView with block center on the pane .find-hit.active.'
    }
}

# The Compare pane's REPLACE SELECTION panel. This is the only layer that renders
# a native <select> and the only one that can open the panel against a real text
# selection, and both defects it guards against left the markup correct and the
# feature unusable: a dropdown whose option values were "person_names,Person names"
# produced Values the engine dropped before validation, and a field whose input
# handler repainted the view was destroyed mid-word and took exactly one letter.
function Test-SelectionPanel([CdpSession]$cdp) {
    Write-Step 'The Compare selection panel declares a Value the engine can apply'
    $r = $cdp.Eval('__uiProbes.selectionPanel()')
    if ($r.PSObject.Properties.Name -contains 'error' -and $r.error) {
        Assert-That -Name 'selecting text in the anonymised pane opens the panel' -Condition $false `
            -Expected '#selection-card beside the selection' -Actual $r.error `
            -Hint 'views/anonymise.js wireTextSelection listens for mouseup on .pane-body.'
        return
    }

    Assert-That -Name 'the panel names the text that was selected' -Condition ($r.selectedText) `
        -Expected 'the selected run echoed in .selection-text' -Actual 'an empty .selection-text' `
        -Hint 'views/anonymise.js selectionPanel renders the selection own text, escaped.'

    Assert-That -Name 'the panel is inside the Compare card' -Condition ($r.insideCompare) `
        -Expected 'the panel rect within #compare-card' -Actual 'the panel is outside it' `
        -Hint 'wireTextSelection clamps x to the card bounds, the same clamp the mark tooltip makes.'

    Assert-That -Name 'the panel is on screen' -Condition ($r.inViewport) `
        -Expected 'the panel rect within the viewport' -Actual 'the panel is off screen' `
        -Hint 'SELECTION_PANEL_WIDTH is the clamp half-width; the panel is translate(-50%, -100%).'

    Assert-That -Name 'the panel is painted' -Condition ($r.hasSize) `
        -Expected 'a panel with a width and a height' -Actual 'a zero-size panel' `
        -Hint '.selection-card in style.css sets its width and padding.'

    # The type list: every option value must be a category key the engine knows.
    $declarable = @(
        'entity_names', 'project_names', 'identifier_names',
        'person_names', 'product_names', 'brand_names', 'other_names')
    $bad = @($r.optionValues | Where-Object { $declarable -notcontains $_ })
    Assert-That -Name 'the type list offers only real category keys' `
        -Condition (($r.optionValues.Count -gt 0) -and ($bad.Count -eq 0)) `
        -Expected 'one option per declarable category, each value a category key' `
        -Actual "$($r.optionValues.Count) option(s), not a category: $($bad -join ', ')" `
        -Hint 'views/anonymise.js selectionStageFields uses the shared categorySelect builder; a hand-rolled copy read the key/label pair list as a list of strings.'

    Assert-That -Name 'a type is pre-selected' -Condition ($declarable -contains $r.selectedValue) `
        -Expected 'the panel current type selected in the list' -Actual "selected value '$($r.selectedValue)'" `
        -Hint 'categorySelect marks the option whose KEY equals the selected category.'

    # The spelling target: the field is typeable and it suggests.
    Assert-That -Name 'no native datalist is used for the suggestions' -Condition (-not $r.hasDatalist) `
        -Expected 'real pick buttons' -Actual 'a datalist in the page' `
        -Hint 'A platform popup is destroyed by every repaint and is empty on the render the user starts typing into.'

    Assert-That -Name 'the field survives the first keystroke' -Condition ($r.survivedFirstKeystroke) `
        -Expected 'the same input element still in the DOM' -Actual 'the element was replaced' `
        -Hint 'The input handler patches the suggestion list in place and never calls setState.'

    Assert-That -Name 'the field survives the second keystroke' -Condition ($r.survivedSecondKeystroke) `
        -Expected 'the same input element still in the DOM' -Actual 'the element was replaced' `
        -Hint 'This is the one-letter symptom: a repaint destroys the element the next keystroke needs.'

    Assert-That -Name 'both keystrokes are in the field' -Condition ($r.fieldValue -eq 'mar') `
        -Expected "the field holding 'mar'" -Actual "the field holding '$($r.fieldValue)'" `
        -Hint 'Nothing in the keystroke path resets the value.'

    Assert-That -Name 'the suggestions narrow to what was typed' -Condition ($r.picks.Count -gt 0) `
        -Expected 'at least one .selection-pick for the query' -Actual "$($r.picks.Count) pick(s)" `
        -Hint 'views/anonymise.js selectionPicks renders valueAutocomplete answer as buttons.'
}

function Test-ImageTab([CdpSession]$cdp) {
    Write-Step 'The IMAGE half: the list scrolls, the page does not, and a tile keeps its height'
    $r = $cdp.Eval('__uiProbes.imageTabGeometry()')
    if ($r.PSObject.Properties.Name -contains 'error' -and $r.error) {
        Assert-That -Name 'the IMAGE half renders over a seeded inventory' -Condition $false `
            -Expected '#image-card and #image-list on screen' -Actual $r.error `
            -Hint 'views/anonymise.js dispatches on state.anonymiseTab; views/anonymiseimages.js renders the surface.'
        return
    }

    Assert-That -Name 'the tiles view renders the whole seeded inventory' -Condition ($r.tileCount -eq 40) `
        -Expected '40 tiles' -Actual "$($r.tileCount) tile(s)" `
        -Hint 'A shorter list may not scroll at all, and then every check below measures nothing.'

    Assert-That -Name 'the tiles list is the element that scrolls' -Condition ($r.tilesListScrolls) `
        -Expected '#image-list taller than its box' -Actual 'nothing scrolls' `
        -Hint 'style.css .card-body.image-list is the scroll owner, and every link above it carries min-height: 0.'

    Assert-That -Name 'the page does not scroll with the tiles view on screen' `
        -Condition (($r.pageScrollsDown -le 1) -and ($r.pageScrollsAcross -le 1)) `
        -Expected '0px in both directions' `
        -Actual "$($r.pageScrollsDown)px down, $($r.pageScrollsAcross)px across" `
        -Hint 'The fixed-height layout contract: body and #app are 100vh and only a card body scrolls.'

    Assert-That -Name 'the measured pair really is one shared picture beside one that is not' `
        -Condition (($r.manyLocation -like '*more*') -and ($r.oneLocation -notlike '*more*')) `
        -Expected 'the first tile location carrying a "+N more" marker and the second not' `
        -Actual "first '$($r.manyLocation)', second '$($r.oneLocation)'" `
        -Hint 'views/anonymiseimages.js locationCell puts the first place in the cell and the rest behind the count.'

    Assert-That -Name 'a tile used in five places is the same height as one used in one' `
        -Condition (($r.manyHeight -gt 0) -and ($r.manyHeight -eq $r.oneHeight)) `
        -Expected "both $($r.oneHeight)px" -Actual "$($r.manyHeight)px beside $($r.oneHeight)px" `
        -Hint 'style.css .image-tile has a fixed height. A card that grows when it has more to say moves every card below it, and the reader loses their scroll position.'

    $wantHeadings = @('Preview', 'Name', 'Format', 'Dimensions', 'Size', 'Location', 'Status')
    Assert-That -Name 'the details view renders the seven headings in order' `
        -Condition ((($r.details.headings) -join '|') -eq ($wantHeadings -join '|')) `
        -Expected ($wantHeadings -join ', ') -Actual (($r.details.headings) -join ', ') `
        -Hint 'The header and the rows come from one shared column template, so they cannot drift apart.'

    Assert-That -Name 'a details row keeps its height whatever its location says' `
        -Condition (($r.details.manyRowHeight -gt 0) -and ($r.details.manyRowHeight -eq $r.details.oneRowHeight)) `
        -Expected "both $($r.details.oneRowHeight)px" `
        -Actual "$($r.details.manyRowHeight)px beside $($r.details.oneRowHeight)px" `
        -Hint 'style.css .image-grid .grid-row has a fixed height and every cell is one clipped line.'

    Assert-That -Name 'the details list is the element that scrolls' -Condition ($r.details.detailsListScrolls) `
        -Expected '#image-list taller than its box' -Actual 'nothing scrolls' -Hint ''

    Assert-That -Name 'the page does not scroll with the details view on screen' `
        -Condition (($r.details.pageScrollsDown -le 1) -and ($r.details.pageScrollsAcross -le 1)) `
        -Expected '0px in both directions' `
        -Actual "$($r.details.pageScrollsDown)px down, $($r.details.pageScrollsAcross)px across" `
        -Hint 'A seven-column grid must scroll inside its own container, never widen the page.'

    Assert-That -Name 'the filter stays reachable from the bottom of the list' -Condition (-not $r.bannerInsideList) `
        -Expected '.image-banner outside #image-list' -Actual 'the banner scrolls away with the list' `
        -Hint 'The banner sits between the card head and the card body.'

    Assert-That -Name 'the filter chips carry their counts' `
        -Condition (($r.filterChips.Count -eq 3) -and ($r.filterChips[0] -like '*40*')) `
        -Expected 'three chips, the first counting all 40 pictures' -Actual (($r.filterChips) -join ', ') `
        -Hint 'state.js imageStatusCounts feeds copy.js IMAGES.filterChip.'

    Assert-That -Name 'the card is inside the window' -Condition ($r.cardInsideViewport) `
        -Expected '#image-card between the top and bottom of the viewport' -Actual 'the card is clipped' -Hint ''
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

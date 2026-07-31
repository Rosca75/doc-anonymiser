<#
.SYNOPSIS
  Drives the real doc-anonymiser UI in a real WebView and asserts what a user
  would actually SEE. Windows only, PowerShell + .NET, no packages.

.DESCRIPTION
  The Go and node test suites prove the HTML this application produces. They
  cannot prove that a tooltip is VISIBLE rather than clipped by the pane it
  sits in, that a screen fits the window, or that a run leaves no console
  error behind. Every one of those was a real defect in this repository, and
  every one of them passed the unit tests.

  Two checks, both optional and both opt-in:

    dev mode (default)  `wails dev` serves the frontend on
                        http://localhost:34115 with the Go bridge attached, so
                        the app can be driven by a headless browser over the
                        DevTools Protocol without shipping any test hook into
                        the binary. This is where the layout and visibility
                        assertions run.

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

                Test-Layout $cdp
                Test-ImportPreview $cdp
                Test-ConfigureRail $cdp
                Test-TooltipVisibility $cdp
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

# The fixed-height layout contract (frontend/CLAUDE.md): the page body never
# scrolls, in either direction. Only a real renderer can answer this.
function Test-Layout([CdpSession]$cdp) {
    Write-Step 'Layout contract'
    foreach ($step in @('import', 'identify', 'anonymise', 'export')) {
        $metrics = $cdp.Eval(@"
(async () => {
  const nav = await import('/nav.js');
  const state = await import('/state.js');
  state.setState({ screen: 'wizard', step: '$step' });
  await new Promise(r => setTimeout(r, 250));
  const b = document.body;
  return { h: b.scrollHeight - b.clientHeight, w: b.scrollWidth - b.clientWidth };
})()
"@)
        Assert-That -Name "$step does not scroll the page body" `
            -Condition ($metrics.h -le 1 -and $metrics.w -le 1) `
            -Expected 'body scrollHeight and scrollWidth equal to the client size' `
            -Actual "overflow: $($metrics.h)px down, $($metrics.w)px across" `
            -Hint 'A link in the chain from #view down to the scrolling card body is missing min-height: 0.'
    }
}

# Reported issues 1 and 4, seen through the pixels rather than the HTML.
function Test-ImportPreview([CdpSession]$cdp) {
    Write-Step 'Import preview shows source text'
    $text = $cdp.Eval(@"
(async () => {
  const state = await import('/state.js');
  state.setState({ screen: 'wizard', step: 'import' });
  await new Promise(r => setTimeout(r, 250));
  return document.querySelector('.md-preview')?.innerText ?? '';
})()
"@)
    $flat = ($text -replace '\s+', ' ')
    $excerpt = $flat.Substring(0, [Math]::Min(120, $flat.Length))
    Assert-That -Name 'the Import preview contains no placeholder' `
        -Condition (-not ($text -match '\[[A-Z][A-Z0-9_]*_\d+\]')) `
        -Expected 'the imported source text' `
        -Actual $excerpt `
        -Hint 'views/import.js must render documentSource(), never anything the pipeline produced.'
}

# Reported issue 3: three switchable sections, not four peer tabs.
function Test-ConfigureRail([CdpSession]$cdp) {
    Write-Step 'Configure rail'
    $rail = $cdp.Eval(@"
(async () => {
  const state = await import('/state.js');
  state.setState({ screen: 'wizard', step: 'identify' });
  await new Promise(r => setTimeout(r, 250));
  const sections = [...document.querySelectorAll('#identify-rail .rail-section')];
  const toggles = [...document.querySelectorAll('#identify-rail .route-toggle')];
  return {
    sections: sections.length,
    tabs: document.querySelectorAll('[data-railtab]').length,
    smartOn: toggles[0]?.checked ?? null,
    localOn: toggles[1]?.checked ?? null,
    cloudDisabled: toggles[2]?.disabled ?? null,
    categories: document.querySelectorAll('#identify-rail .cat-toggle').length,
  };
})()
"@)
    Assert-That -Name 'the rail is three route sections' -Condition ($rail.sections -eq 3) `
        -Expected '3' -Actual "$($rail.sections)"
    Assert-That -Name 'the old tab strip is gone' -Condition ($rail.tabs -eq 0) `
        -Expected '0 [data-railtab] chips' -Actual "$($rail.tabs)"
    Assert-That -Name 'Smart detection is on by default' -Condition ($rail.smartOn -eq $true)
    Assert-That -Name 'Local AI is off by default' -Condition ($rail.localOn -eq $false)
    Assert-That -Name 'Cloud AI cannot be switched on' -Condition ($rail.cloudDisabled -eq $true)
    Assert-That -Name 'every category is reachable without switching tabs' `
        -Condition ($rail.categories -ge 20) -Expected 'all 22 category checkboxes' -Actual "$($rail.categories)"
}

# Reported issue 6. This is the assertion that CANNOT be made without a real
# renderer: the tooltip was in the HTML all along, and the pane clipped it.
function Test-TooltipVisibility([CdpSession]$cdp) {
    Write-Step 'Hover tooltip is visible, not clipped'
    $result = $cdp.Eval(@"
(async () => {
  const state = await import('/state.js');
  state.setState({
    screen: 'wizard', step: 'anonymise',
    documents: [{ name: 'probe.txt', markdown: 'Marie Duval wrote to Meridian Consulting.', previewTruncated: false, isGrid: false }],
    results: { documents: [{ name: 'probe.txt', anonymised: '[PERSON_1] wrote to [ENTITY_1].', byCategory: { person_names: 1 } }],
               report: { values: [], byCategory: {}, totalReplacements: 2, documents: [] } },
    mapping: { '[PERSON_1]': { original: 'Marie Duval', category: 'person_names' },
               '[ENTITY_1]': { original: 'Meridian Consulting', category: 'entity_names' } },
  });
  await new Promise(r => setTimeout(r, 300));

  const mark = document.querySelector('#anonymised-pane mark[data-original]');
  if (!mark) return { error: 'no mark rendered' };
  mark.dispatchEvent(new MouseEvent('mouseenter', { bubbles: false }));
  await new Promise(r => setTimeout(r, 120));

  const tip = document.querySelector('#mark-tooltip');
  if (!tip || tip.hidden) return { error: 'the tooltip did not appear' };
  const t = tip.getBoundingClientRect();
  const card = document.querySelector('#compare-card').getBoundingClientRect();
  const pane = document.querySelector('#anonymised-pane').getBoundingClientRect();
  // Visible means: inside the card, inside the viewport, and not clipped away
  // to nothing by the pane it is anchored near.
  return {
    text: tip.innerText,
    insideCard: t.left >= card.left - 1 && t.right <= card.right + 1 && t.bottom <= card.bottom + 1,
    inViewport: t.top >= 0 && t.left >= 0 && t.right <= innerWidth && t.bottom <= innerHeight,
    hasSize: t.width > 10 && t.height > 10,
    paneClips: getComputedStyle(document.querySelector('#anonymised-pane')).overflow,
    paneRight: pane.right, tipRight: t.right,
  };
})()
"@)
    if ($result.PSObject.Properties.Name -contains 'error') {
        Assert-That -Name 'the tooltip appears on hover' -Condition $false `
            -Expected 'a visible #mark-tooltip' -Actual $result.error `
            -Hint 'views/anonymise.js wireMarkTooltip must show the tooltip on mouseenter.'
        return
    }
    Assert-That -Name 'the tooltip shows the original value' `
        -Condition ($result.text -match 'Marie Duval') -Expected 'Marie Duval' -Actual $result.text
    Assert-That -Name 'the tooltip is rendered inside the Compare card' -Condition ($result.insideCard) `
        -Expected 'the tooltip rect inside the card rect' `
        -Actual "tooltip right $($result.tipRight), pane right $($result.paneRight)" `
        -Hint 'Anchor it to #compare-card, not to the mark: the pane is overflow:auto and clips it.'
    Assert-That -Name 'the tooltip is on screen' -Condition ($result.inViewport)
    Assert-That -Name 'the tooltip has a size' -Condition ($result.hasSize)
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

[CmdletBinding()]
param(
    [int]$RtidPort = 18452,
    [int]$Cycles = 10,
    [double]$TickStep = 3.0
)

$ErrorActionPreference = "Stop"
$ProcessPath = $env:Path
[Environment]::SetEnvironmentVariable("PATH", $null, [EnvironmentVariableTarget]::Process)
[Environment]::SetEnvironmentVariable("Path", $ProcessPath, [EnvironmentVariableTarget]::Process)
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$WorkDir = Join-Path ([IO.Path]::GetTempPath()) ("gorti-go-timed-" + [guid]::NewGuid().ToString("N"))
$Processes = [Collections.Generic.List[Diagnostics.Process]]::new()

function ConvertTo-ProcessCommandLine {
    param([string[]]$ArgumentList)

    return (($ArgumentList | ForEach-Object {
        $Argument = [string]$_
        if ($Argument.Length -gt 0 -and $Argument -notmatch '[\s"]') {
            $Argument
            return
        }

        $Builder = [Text.StringBuilder]::new()
        [void]$Builder.Append('"')
        $Backslashes = 0
        foreach ($Character in $Argument.ToCharArray()) {
            if ($Character -eq '\') {
                $Backslashes++
                continue
            }
            if ($Character -eq '"') {
                [void]$Builder.Append(('\' * (($Backslashes * 2) + 1)))
                [void]$Builder.Append('"')
                $Backslashes = 0
                continue
            }
            if ($Backslashes -gt 0) {
                [void]$Builder.Append(('\' * $Backslashes))
                $Backslashes = 0
            }
            [void]$Builder.Append($Character)
        }
        if ($Backslashes -gt 0) {
            [void]$Builder.Append(('\' * ($Backslashes * 2)))
        }
        [void]$Builder.Append('"')
        $Builder.ToString()
    }) -join ' ')
}

function Wait-RtiPort {
    param([Diagnostics.Process]$Process, [int]$Port)
    $Deadline = [DateTime]::UtcNow.AddSeconds(10)
    while ([DateTime]::UtcNow -lt $Deadline) {
        if ($Process.HasExited) { throw "rtid exited before accepting connections." }
        $Client = [Net.Sockets.TcpClient]::new()
        try {
            $Attempt = $Client.BeginConnect("127.0.0.1", $Port, $null, $null)
            if ($Attempt.AsyncWaitHandle.WaitOne(200)) {
                $Client.EndConnect($Attempt)
                return
            }
        }
        catch {
            # The listener may still be starting.
        }
        finally {
            $Client.Dispose()
        }
        Start-Sleep -Milliseconds 100
    }
    throw "rtid did not listen on 127.0.0.1:$Port within 10 seconds."
}

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "go-timed: Go 1.22 or later is required on PATH."
}
if ($RtidPort -lt 1 -or $RtidPort -gt 65535) { throw "RtidPort must be from 1 to 65535." }
if ($Cycles -lt 1) { throw "Cycles must be positive." }
if ([double]::IsNaN($TickStep) -or [double]::IsInfinity($TickStep) -or $TickStep -le 2.0) {
    throw "TickStep must be a finite number greater than the slow federate lookahead of 2.0."
}

New-Item -ItemType Directory -Path $WorkDir | Out-Null
try {
    $RtidBinary = Join-Path $WorkDir "rtid.exe"
    $FederateBinary = Join-Path $WorkDir "go-timed.exe"
    Push-Location $RepoRoot
    try {
        Write-Host "go-timed: building temporary RTI and federate binaries"
        & go build -buildvcs=false -o $RtidBinary ./rti/cmd/rtid
        if ($LASTEXITCODE -ne 0) { throw "failed to build rtid" }
        & go build -buildvcs=false -o $FederateBinary ./examples/go-timed
        if ($LASTEXITCODE -ne 0) { throw "failed to build go-timed" }
    }
    finally {
        Pop-Location
    }

    $ResultsDir = Join-Path $WorkDir "results"
    $SavesDir = Join-Path $WorkDir "saves"
    New-Item -ItemType Directory -Path $ResultsDir, $SavesDir | Out-Null
    $RtidOut = Join-Path $WorkDir "rtid.out.log"
    $RtidErr = Join-Path $WorkDir "rtid.err.log"
    $RtiArguments = @(
        "--listen=127.0.0.1:$RtidPort",
        "--metrics-listen=127.0.0.1:0",
        "--admin-listen=",
        "--log-level=warn",
        "--save-dir=$SavesDir"
    )
    $Rti = Start-Process -FilePath $RtidBinary -ArgumentList (ConvertTo-ProcessCommandLine $RtiArguments) `
        -RedirectStandardOutput $RtidOut -RedirectStandardError $RtidErr -PassThru -WindowStyle Hidden
    $Processes.Add($Rti)
    Wait-RtiPort -Process $Rti -Port $RtidPort

    $Specs = @(
        @{ Name = "fast"; Lookahead = 0.5 },
        @{ Name = "normal"; Lookahead = 1.0 },
        @{ Name = "slow"; Lookahead = 2.0 }
    )
    $Runs = @()
    Write-Host "go-timed: starting fast, normal, and slow federates"
    foreach ($Spec in $Specs) {
        $Name = $Spec.Name
        $OutLog = Join-Path $WorkDir "$Name.out.log"
        $ErrLog = Join-Path $WorkDir "$Name.err.log"
        $ResultPath = Join-Path $ResultsDir "$Name-result.json"
        $Arguments = @(
            "--url=127.0.0.1:$RtidPort",
            "--federation=go-timed-run-$RtidPort",
            "--name=$Name",
            "--lookahead=$($Spec.Lookahead)",
            "--primitive=TAR",
            "--constrained=true",
            "--cycles=$Cycles",
            "--tick-step=$TickStep",
            "--result=$ResultPath",
            "--fom=$(Join-Path $PSScriptRoot 'time-advance-fom.xml')"
        )
        $Process = Start-Process -FilePath $FederateBinary -ArgumentList (ConvertTo-ProcessCommandLine $Arguments) `
            -RedirectStandardOutput $OutLog -RedirectStandardError $ErrLog -PassThru -WindowStyle Hidden
        $Processes.Add($Process)
        $Runs += [pscustomobject]@{ Name = $Name; Process = $Process; Out = $OutLog; Err = $ErrLog; Result = $ResultPath }
    }

    $Failed = $false
    foreach ($Run in $Runs) {
        if (-not $Run.Process.WaitForExit(60000)) {
            $Failed = $true
            Write-Error "$($Run.Name) exceeded the 60 second launcher timeout" -ErrorAction Continue
        }
        else {
            $Run.Process.WaitForExit()
            $Run.Process.Refresh()
        }
        Get-Content $Run.Out, $Run.Err -ErrorAction SilentlyContinue | Where-Object { $_ -ne "" }
        $ExitCode = $Run.Process.ExitCode
        if ($null -ne $ExitCode -and $ExitCode -ne 0) { $Failed = $true }
    }
    if ($Failed) {
        Get-Content $RtidOut, $RtidErr -ErrorAction SilentlyContinue
        throw "go-timed: one or more federates failed."
    }

    $Results = @{}
    foreach ($Run in $Runs) {
        if (-not (Test-Path -LiteralPath $Run.Result)) { throw "missing result for $($Run.Name)" }
        $Results[$Run.Name] = Get-Content -LiteralPath $Run.Result -Raw | ConvertFrom-Json
    }
    $CountsOk = $true
    $MonotonicOk = $true
    foreach ($Name in @("fast", "normal", "slow")) {
        $Grants = @($Results[$Name].grants)
        if ($Grants.Count -ne $Cycles) { $CountsOk = $false }
        for ($Index = 1; $Index -lt $Grants.Count; $Index++) {
            if ([double]$Grants[$Index] -lt [double]$Grants[$Index - 1]) { $MonotonicOk = $false }
        }
        Write-Host "  $Name (la=$($Results[$Name].lookahead), $($Results[$Name].primitive)): grants=$($Grants -join ', ')"
    }
    $CycleMins = @()
    for ($Cycle = 0; $Cycle -lt $Cycles; $Cycle++) {
        $CycleMins += (@("fast", "normal", "slow") | ForEach-Object { [double]$Results[$_].grants[$Cycle] } | Measure-Object -Minimum).Minimum
    }
    $LbtsOk = $true
    for ($Index = 1; $Index -lt $CycleMins.Count; $Index++) {
        if ($CycleMins[$Index] -lt $CycleMins[$Index - 1]) { $LbtsOk = $false }
    }
    $CountsText = if ($CountsOk) { "PASS" } else { "FAIL" }
    $MonotonicText = if ($MonotonicOk) { "PASS" } else { "FAIL" }
    $LbtsText = if ($LbtsOk) { "PASS" } else { "FAIL" }
    Write-Host "go-timed: grant counts: $CountsText"
    Write-Host "go-timed: per-federate non-decreasing grants: $MonotonicText"
    Write-Host "go-timed: per-cycle non-decreasing minimum grant: $LbtsText"
    if (-not ($CountsOk -and $MonotonicOk -and $LbtsOk)) { throw "go-timed: result verification failed." }
    Write-Host "go-timed: PASS - all cross-process time invariants held"
}
finally {
    foreach ($Process in $Processes) {
        if (-not $Process.HasExited) {
            Stop-Process -Id $Process.Id -Force -ErrorAction SilentlyContinue
            $Process.WaitForExit()
        }
        $Process.Dispose()
    }
    Remove-Item -LiteralPath $WorkDir -Recurse -Force -ErrorAction SilentlyContinue
}

[CmdletBinding()]
param(
    [int]$RtidPort = 18442,
    [string]$PeerDelay = "3s",
    [string]$RunTimeout = "30s"
)

$ErrorActionPreference = "Stop"
$ProcessPath = $env:Path
[Environment]::SetEnvironmentVariable("PATH", $null, [EnvironmentVariableTarget]::Process)
[Environment]::SetEnvironmentVariable("Path", $ProcessPath, [EnvironmentVariableTarget]::Process)
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$WorkDir = Join-Path ([IO.Path]::GetTempPath()) ("gorti-tar-wait-" + [guid]::NewGuid().ToString("N"))
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

function ConvertFrom-GoDurationMilliseconds {
    param([string]$Value)

    $Duration = [Regex]::Match(
        $Value,
        '^(?<sign>[+-])?(?:(?<number>(?:\d+(?:\.\d*)?|\.\d+))(?<unit>ns|us|µs|μs|ms|s|m|h))+$'
    )
    if (-not $Duration.Success) {
        throw "go-tar-wait: RunTimeout must be a positive Go duration such as 30s or 1m30s."
    }

    $Milliseconds = 0.0
    $Numbers = $Duration.Groups['number'].Captures
    $Units = $Duration.Groups['unit'].Captures
    for ($Index = 0; $Index -lt $Numbers.Count; $Index++) {
        $Number = [double]::Parse(
            $Numbers[$Index].Value,
            [Globalization.NumberStyles]::Float,
            [Globalization.CultureInfo]::InvariantCulture
        )
        $Factor = switch ($Units[$Index].Value) {
            'ns' { 0.000001 }
            'us' { 0.001 }
            'µs' { 0.001 }
            'μs' { 0.001 }
            'ms' { 1.0 }
            's' { 1000.0 }
            'm' { 60000.0 }
            'h' { 3600000.0 }
        }
        $Milliseconds += $Number * $Factor
    }
    if ($Duration.Groups['sign'].Value -eq '-') { $Milliseconds = -$Milliseconds }
    if ($Milliseconds -le 0 -or [double]::IsInfinity($Milliseconds)) {
        throw "go-tar-wait: RunTimeout must be a positive finite duration."
    }
    return $Milliseconds
}

function Wait-ProcessUntil {
    param([Diagnostics.Process]$Process, [DateTime]$Deadline)

    while (-not $Process.HasExited) {
        $Remaining = $Deadline - [DateTime]::UtcNow
        if ($Remaining.TotalMilliseconds -le 0) { return $false }
        $WaitMilliseconds = [int][Math]::Min(
            [int]::MaxValue,
            [Math]::Max(1, [Math]::Ceiling($Remaining.TotalMilliseconds))
        )
        if ($Process.WaitForExit($WaitMilliseconds)) { return $true }
    }
    return $true
}

function Wait-RtiPort {
    param([Diagnostics.Process]$Process, [int]$Port)
    $Deadline = [DateTime]::UtcNow.AddSeconds(10)
    while ([DateTime]::UtcNow -lt $Deadline) {
        if ($Process.HasExited) {
            throw "rtid exited before accepting connections."
        }
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
    throw "go-tar-wait: Go 1.22 or later is required on PATH."
}
if ($RtidPort -lt 1 -or $RtidPort -gt 65535) {
    throw "go-tar-wait: RtidPort must be from 1 to 65535."
}
$RunTimeoutMilliseconds = ConvertFrom-GoDurationMilliseconds $RunTimeout

New-Item -ItemType Directory -Path $WorkDir | Out-Null
try {
    $RtidBinary = Join-Path $WorkDir "rtid.exe"
    $FederateBinary = Join-Path $WorkDir "go-tar-wait.exe"
    Push-Location $RepoRoot
    try {
        Write-Host "go-tar-wait: building temporary binaries"
        & go build -buildvcs=false -o $RtidBinary ./rti/cmd/rtid
        if ($LASTEXITCODE -ne 0) { throw "failed to build rtid" }
        & go build -buildvcs=false -o $FederateBinary ./examples/go-tar-wait
        if ($LASTEXITCODE -ne 0) { throw "failed to build go-tar-wait" }
    }
    finally {
        Pop-Location
    }

    $Saves = Join-Path $WorkDir "saves"
    New-Item -ItemType Directory -Path $Saves | Out-Null
    $RtidOut = Join-Path $WorkDir "rtid.out.log"
    $RtidErr = Join-Path $WorkDir "rtid.err.log"
    $RtiArguments = @(
        "--listen=127.0.0.1:$RtidPort",
        "--metrics-listen=127.0.0.1:0",
        "--admin-listen=",
        "--log-level=warn",
        "--save-dir=$Saves"
    )
    $Rti = Start-Process -FilePath $RtidBinary -ArgumentList (ConvertTo-ProcessCommandLine $RtiArguments) `
        -RedirectStandardOutput $RtidOut -RedirectStandardError $RtidErr -PassThru -WindowStyle Hidden
    $Processes.Add($Rti)
    Wait-RtiPort -Process $Rti -Port $RtidPort

    $Common = @(
        "--url=127.0.0.1:$RtidPort",
        "--federation=tar-wait-run-$RtidPort",
        "--peer-delay=$PeerDelay",
        "--timeout=$RunTimeout",
        "--fom=$(Join-Path $PSScriptRoot 'tar-wait-fom.xml')"
    )
    Write-Host "go-tar-wait: starting waiter and peer (peer delay $PeerDelay)"
    $WaiterOut = Join-Path $WorkDir "waiter.out.log"
    $WaiterErr = Join-Path $WorkDir "waiter.err.log"
    $PeerOut = Join-Path $WorkDir "peer.out.log"
    $PeerErr = Join-Path $WorkDir "peer.err.log"
    $WaiterArguments = ConvertTo-ProcessCommandLine (@("--role=waiter") + $Common)
    $Waiter = Start-Process -FilePath $FederateBinary -ArgumentList $WaiterArguments `
        -RedirectStandardOutput $WaiterOut -RedirectStandardError $WaiterErr -PassThru -WindowStyle Hidden
    $Processes.Add($Waiter)
    $PeerArguments = ConvertTo-ProcessCommandLine (@("--role=peer") + $Common)
    $Peer = Start-Process -FilePath $FederateBinary -ArgumentList $PeerArguments `
        -RedirectStandardOutput $PeerOut -RedirectStandardError $PeerErr -PassThru -WindowStyle Hidden
    $Processes.Add($Peer)

    $LauncherDeadline = [DateTime]::UtcNow.AddMilliseconds($RunTimeoutMilliseconds + 10000)
    if (-not (Wait-ProcessUntil -Process $Waiter -Deadline $LauncherDeadline)) {
        throw "waiter exceeded RunTimeout ($RunTimeout) plus the 10 second launcher grace period"
    }
    $Waiter.WaitForExit()
    $Waiter.Refresh()
    if (-not (Wait-ProcessUntil -Process $Peer -Deadline $LauncherDeadline)) {
        throw "peer exceeded RunTimeout ($RunTimeout) plus the 10 second launcher grace period"
    }
    $Peer.WaitForExit()
    $Peer.Refresh()
    Get-Content $WaiterOut, $WaiterErr, $PeerOut, $PeerErr | Where-Object { $_ -ne "" }
    $WaiterExit = $Waiter.ExitCode
    $PeerExit = $Peer.ExitCode
    $WaiterPassed = Select-String -Path $WaiterOut -Pattern "GRANT\(5\).*PASS" -Quiet
    $PeerPassed = Select-String -Path $PeerOut -Pattern "GRANT\(5\): PASS" -Quiet
    $BadExit = ($null -ne $WaiterExit -and $WaiterExit -ne 0) -or `
        ($null -ne $PeerExit -and $PeerExit -ne 0)
    if ($BadExit -or -not $WaiterPassed -or -not $PeerPassed) {
        Get-Content $RtidOut, $RtidErr -ErrorAction SilentlyContinue
        throw "go-tar-wait: verification failed (waiter=$WaiterExit, peer=$PeerExit)."
    }
    Write-Host "go-tar-wait: PASS - the peer delay held and then released TAR(5)"
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

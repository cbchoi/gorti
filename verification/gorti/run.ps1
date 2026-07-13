[CmdletBinding()]
param(
    [string]$Url = "grpc://127.0.0.1:8442",
    [int]$Seed = 20260712,
    [Alias("Iterations")]
    [int]$Count = 100,
    [double]$TimeoutSeconds = 15,
    [string]$OutputDirectory = (Join-Path $PSScriptRoot "artifacts"),
    [string]$Python = "python",
    [string]$RtidPath = "",
    [ValidateRange(1, 1024)][int]$OutboxBatchSize = 32,
    [string]$OutboxFlushInterval = "1ms",
    [ValidateSet("threaded", "async")][string]$TarTransport = "threaded",
    [ValidateSet("queue", "direct")][string]$CallbackTransport = "queue",
    [switch]$DisableEventLog
)

$ErrorActionPreference = "Stop"

# Managed hosts can inject both Path and PATH. Start-Process treats those as
# duplicate environment dictionary keys, so normalize to the Windows name.
$ProcessPath = [Environment]::GetEnvironmentVariable(
    "Path", [EnvironmentVariableTarget]::Process)
[Environment]::SetEnvironmentVariable(
    "PATH", $null, [EnvironmentVariableTarget]::Process)
[Environment]::SetEnvironmentVariable(
    "Path", $ProcessPath, [EnvironmentVariableTarget]::Process)

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
$RtidLogDirectory = Join-Path $OutputDirectory "rtid-eventlogs"
$SaveDirectory = Join-Path $OutputDirectory "rtid-saves"
$ServerStdout = Join-Path $OutputDirectory "rtid.stdout.log"
$ServerStderr = Join-Path $OutputDirectory "rtid.stderr.log"
$SemanticLog = Join-Path $OutputDirectory "canonical.ndjson"
$PerformanceLog = Join-Path $OutputDirectory "metrics.ndjson"
$ProvenanceLog = Join-Path $OutputDirectory "run-metadata.json"
$BenchmarkLog = Join-Path $OutputDirectory "benchmark.json"

New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
New-Item -ItemType Directory -Force -Path $RtidLogDirectory | Out-Null
New-Item -ItemType Directory -Force -Path $SaveDirectory | Out-Null

$RtiUri = [Uri]$Url
if ($RtiUri.Scheme -notin @("grpc", "grpcs")) {
    throw "Url must use grpc:// or grpcs://"
}
if ($RtiUri.Port -lt 1) {
    throw "Url must include a TCP port"
}
$ListenAddress = "{0}:{1}" -f $RtiUri.Host, $RtiUri.Port

$ServerArguments = @(
    "--listen=$ListenAddress",
    "--metrics-listen=127.0.0.1:0",
    "--admin-listen=",
    "--save-dir=$SaveDirectory",
    "--log-format=text",
    "--outbox-batch-size=$OutboxBatchSize",
    "--outbox-flush-interval=$OutboxFlushInterval"
)
if (-not $DisableEventLog) {
    $ServerArguments += "--log-dir=$RtidLogDirectory"
}

if (-not $RtidPath) {
    $BundledRtid = Join-Path $RepoRoot "bin\rtid.exe"
    if (Test-Path -LiteralPath $BundledRtid) {
        $RtidPath = $BundledRtid
    }
}

$PythonExecutable = (Get-Command $Python -ErrorAction Stop).Source
$LoggingMode = if ($DisableEventLog) { "discard" } else { "file" }
$ProvenanceExecutable = if ($RtidPath) {
    $RtidPath
} else {
    (Get-Command "go" -ErrorAction Stop).Source
}
$ProvenanceServerArguments = if ($RtidPath) {
    $ServerArguments
} else {
    @("run", "./rti/cmd/rtid") + $ServerArguments
}
$VerifierArguments = @(
    (Join-Path $PSScriptRoot "verifier.py"),
    "--url", $Url,
    "--seed", $Seed,
    "--count", $Count,
    "--timeout", $TimeoutSeconds,
    "--tar-transport", $TarTransport,
    "--callback-transport", $CallbackTransport,
    "--semantic-log", $SemanticLog,
    "--performance-log", $PerformanceLog,
    "--provenance-log", $ProvenanceLog,
    "--benchmark-log", $BenchmarkLog
)
$ProvenanceArguments = @(
    (Join-Path $RepoRoot "verification\common\provenance.py"),
    "capture",
    "--repo-root", $RepoRoot,
    "--rtid", $ProvenanceExecutable,
    "--python", $PythonExecutable,
    "--output", $ProvenanceLog,
    "--url", $Url,
    "--seed", $Seed,
    "--count", $Count,
    "--timeout", $TimeoutSeconds,
    "--outbox-batch-size", $OutboxBatchSize,
    "--outbox-flush-interval", $OutboxFlushInterval,
    "--tar-transport", $TarTransport,
    "--callback-transport", $CallbackTransport,
    "--logging-mode", $LoggingMode
)
foreach ($Argument in $ProvenanceServerArguments) {
    $ProvenanceArguments += "--server-arg=$Argument"
}
foreach ($Argument in $VerifierArguments) {
    $ProvenanceArguments += "--verifier-arg=$Argument"
}
& $PythonExecutable @ProvenanceArguments
if ($LASTEXITCODE -ne 0) {
    throw "unable to capture performance-run provenance"
}

$PreviousPythonPath = $env:PYTHONPATH
$RtidProcess = $null
$VerifierExitCode = 1
$RunOutcome = "failed"
try {
    if ($RtidPath) {
        $RtidProcess = Start-Process `
            -FilePath $RtidPath `
            -ArgumentList $ServerArguments `
            -WorkingDirectory $RepoRoot `
            -RedirectStandardOutput $ServerStdout `
            -RedirectStandardError $ServerStderr `
            -WindowStyle Hidden `
            -PassThru
    } else {
        $RtidProcess = Start-Process `
            -FilePath "go" `
            -ArgumentList (@("run", "./rti/cmd/rtid") + $ServerArguments) `
            -WorkingDirectory $RepoRoot `
            -RedirectStandardOutput $ServerStdout `
            -RedirectStandardError $ServerStderr `
            -WindowStyle Hidden `
            -PassThru
    }
    $ReadyDeadline = [DateTime]::UtcNow.AddSeconds(20)
    $Ready = $false
    while ([DateTime]::UtcNow -lt $ReadyDeadline) {
        if ($RtidProcess.HasExited) {
            throw "rtid exited during startup; see $ServerStderr"
        }
        $Client = [Net.Sockets.TcpClient]::new()
        try {
            $Client.Connect($RtiUri.Host, $RtiUri.Port)
            $Ready = $true
            break
        } catch {
            Start-Sleep -Milliseconds 100
        } finally {
            $Client.Dispose()
        }
    }
    if (-not $Ready) {
        throw "rtid did not listen on $ListenAddress within 20 seconds"
    }

    $env:PYTHONPATH = if ($PreviousPythonPath) {
        "$(Join-Path $RepoRoot 'pysdk');$PreviousPythonPath"
    } else {
        Join-Path $RepoRoot "pysdk"
    }
    & $PythonExecutable @VerifierArguments
    $VerifierExitCode = $LASTEXITCODE
    if ($VerifierExitCode -ne 0) {
        throw "gorti verifier failed with exit code $VerifierExitCode"
    }
    $RunOutcome = "passed"
} finally {
    $env:PYTHONPATH = $PreviousPythonPath
    if (($null -ne $RtidProcess) -and (-not $RtidProcess.HasExited)) {
        Stop-Process -Id $RtidProcess.Id -Force
        $RtidProcess.WaitForExit()
    }
    & $PythonExecutable (Join-Path $RepoRoot "verification\common\provenance.py") `
        finalize --output $ProvenanceLog --outcome $RunOutcome `
        --exit-code $VerifierExitCode
    if ($LASTEXITCODE -ne 0) {
        Write-Warning "unable to finalize performance-run provenance"
    }
}

Write-Host "PASS: $SemanticLog"
Write-Host "Performance: $PerformanceLog"
Write-Host "Provenance: $ProvenanceLog"
Write-Host "Benchmark: $BenchmarkLog"

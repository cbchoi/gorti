[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$RtidPath,
    [string]$Address = "127.0.0.1:8442",
    [string]$Federation = "gorti-go-production-benchmark",
    [UInt64]$Seed = 20260712,
    [Alias("Iterations")]
    [ValidateRange(1, 2147483647)]
    [int]$Count = 100,
    [ValidateRange(0.001, 3600.0)]
    [double]$TimeoutSeconds = 15,
    [ValidateRange(1, 1024)]
    [int]$OutboxBatchSize = 32,
    [string]$OutboxFlushInterval = "1ms",
    [string]$OutputDirectory = (Join-Path $PSScriptRoot "artifacts"),
    [string]$Go = ""
)

$ErrorActionPreference = "Stop"

# Start-Process rejects managed environments that inject both Path and PATH.
$ProcessPath = [Environment]::GetEnvironmentVariable(
    "Path", [EnvironmentVariableTarget]::Process)
[Environment]::SetEnvironmentVariable(
    "PATH", $null, [EnvironmentVariableTarget]::Process)
[Environment]::SetEnvironmentVariable(
    "Path", $ProcessPath, [EnvironmentVariableTarget]::Process)

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$RtidPath = (Resolve-Path -LiteralPath $RtidPath).Path
if (-not (Test-Path -LiteralPath $RtidPath -PathType Leaf)) {
    throw "RtidPath must identify an RTID executable file"
}
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
$FomPath = Join-Path $RepoRoot "verification\gorti\federation.fom.xml"
$BenchmarkPath = Join-Path $OutputDirectory "benchmark.json"
$ClientPath = Join-Path $OutputDirectory "gorti-go-benchmark.exe"
$SaveDirectory = Join-Path $OutputDirectory "rtid-saves"
$EventLogDirectory = Join-Path $OutputDirectory "rtid-eventlogs"
$ServerStdout = Join-Path $OutputDirectory "rtid.stdout.log"
$ServerStderr = Join-Path $OutputDirectory "rtid.stderr.log"
$EmptyGitIgnore = Join-Path $OutputDirectory ".empty-gitignore"

New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
New-Item -ItemType Directory -Force -Path $SaveDirectory | Out-Null
New-Item -ItemType Directory -Force -Path $EventLogDirectory | Out-Null
New-Item -ItemType File -Force -Path $EmptyGitIgnore | Out-Null

$GoExecutable = if ($Go) {
    (Get-Command $Go -ErrorAction Stop).Source
} else {
    $GoCommand = Get-Command "go" -ErrorAction SilentlyContinue
    if ($null -ne $GoCommand) {
        $GoCommand.Source
    } elseif (Test-Path -LiteralPath "C:\Program Files\Go\bin\go.exe") {
        "C:\Program Files\Go\bin\go.exe"
    } else {
        throw "Go was not found; pass -Go with the Go executable path"
    }
}

$AddressUri = [Uri]("tcp://{0}" -f $Address)
if (($AddressUri.Port -lt 1) -or (-not $AddressUri.Host)) {
    throw "Address must be a host:port"
}

$ServerArguments = @(
    "--listen=$Address",
    "--metrics-listen=127.0.0.1:0",
    "--admin-listen=",
    "--save-dir=$SaveDirectory",
    "--log-dir=$EventLogDirectory",
    "--log-format=text",
    "--outbox-batch-size=$OutboxBatchSize",
    "--outbox-flush-interval=$OutboxFlushInterval"
)

$RtidVersion = ((& $RtidPath --version 2>&1) -join "`n").Trim()
if ($LASTEXITCODE -ne 0 -or -not $RtidVersion) {
    throw "unable to read RTID version from $RtidPath"
}
$RtidSha256 = (Get-FileHash -LiteralPath $RtidPath -Algorithm SHA256).Hash.ToLowerInvariant()

$SourceCommit = ((& git -c "core.excludesFile=$EmptyGitIgnore" -C $RepoRoot rev-parse HEAD 2>$null) -join "").Trim()
if ($LASTEXITCODE -ne 0 -or -not $SourceCommit) {
    $SourceCommit = "unknown"
}
$SourceBranch = ((& git -c "core.excludesFile=$EmptyGitIgnore" -C $RepoRoot branch --show-current 2>$null) -join "").Trim()
if ($LASTEXITCODE -ne 0 -or -not $SourceBranch) {
    $SourceBranch = "unknown"
}
$DirtyOutput = (& git -c "core.excludesFile=$EmptyGitIgnore" -C $RepoRoot status --porcelain 2>$null)
$SourceDirty = ($LASTEXITCODE -eq 0) -and ($null -ne $DirtyOutput) -and (@($DirtyOutput).Count -gt 0)

& $GoExecutable build -trimpath -o $ClientPath ./verification/gorti-go
if ($LASTEXITCODE -ne 0) {
    throw "unable to build the Go benchmark client"
}

$Invariant = [Globalization.CultureInfo]::InvariantCulture
$TimeoutValue = [Convert]::ToString($TimeoutSeconds, $Invariant) + "s"
$ClientArguments = @(
    "--address=$Address",
    "--federation=$Federation",
    "--fom=$FomPath",
    "--count=$Count",
    "--seed=$Seed",
    "--timeout=$TimeoutValue",
    "--output=$BenchmarkPath",
    "--rtid-path=$RtidPath",
    "--rtid-version=$RtidVersion",
    "--source-commit=$SourceCommit",
    "--source-branch=$SourceBranch",
    "--source-dirty=$($SourceDirty.ToString().ToLowerInvariant())"
)
foreach ($Argument in $ServerArguments) {
    $ClientArguments += "--server-arg=$Argument"
}

$RtidProcess = $null
try {
    $RtidProcess = Start-Process `
        -FilePath $RtidPath `
        -ArgumentList $ServerArguments `
        -WorkingDirectory $RepoRoot `
        -RedirectStandardOutput $ServerStdout `
        -RedirectStandardError $ServerStderr `
        -WindowStyle Hidden `
        -PassThru

    $ReadyDeadline = [DateTime]::UtcNow.AddSeconds(20)
    $Ready = $false
    while ([DateTime]::UtcNow -lt $ReadyDeadline) {
        if ($RtidProcess.HasExited) {
            throw "RTID exited during startup; see $ServerStderr"
        }
        $Client = [Net.Sockets.TcpClient]::new()
        try {
            $Client.Connect($AddressUri.Host, $AddressUri.Port)
            $Ready = $true
            break
        } catch {
            Start-Sleep -Milliseconds 100
        } finally {
            $Client.Dispose()
        }
    }
    if (-not $Ready) {
        throw "RTID did not listen on $Address within 20 seconds"
    }

    & $ClientPath @ClientArguments
    if ($LASTEXITCODE -ne 0) {
        throw "gorti-go benchmark failed with exit code $LASTEXITCODE"
    }
} finally {
    if (($null -ne $RtidProcess) -and (-not $RtidProcess.HasExited)) {
        Stop-Process -Id $RtidProcess.Id -Force
        $RtidProcess.WaitForExit()
    }
}

Write-Host "RTID SHA-256: $RtidSha256"
Write-Host "Benchmark: $BenchmarkPath"

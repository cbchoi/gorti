[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Fom,
    [Parameter(Mandatory = $true)][ValidateSet('1516')][string]$Seed,
    [Parameter(Mandatory = $true)][ValidateRange(1, 1000000)][int]$Count,
    [Parameter(Mandatory = $true)][ValidateSet('off', 'file')][string]$ServerEventLog,
    [Parameter(Mandatory = $true)][string]$OutputDirectory,
    [Parameter(Mandatory = $true)][string]$RunId,
    [Parameter(Mandatory = $true)][string]$WorkloadContract,
    [string]$CrcAddress = '',
    [string]$PRTIHome = $env:PRTI1516E_HOME,
    [ValidateRange(1000, 600000)][int]$TimeoutMs = 30000,
    [string]$Python = 'python'
)

$ErrorActionPreference = 'Stop'
$Root = $PSScriptRoot
$Repo = [IO.Path]::GetFullPath((Join-Path $Root '..\..'))
$Fom = (Resolve-Path -LiteralPath $Fom).Path
$WorkloadContract = (Resolve-Path -LiteralPath $WorkloadContract).Path
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
$PythonExecutable = (Get-Command $Python -ErrorAction Stop).Source

$Workload = Get-Content -LiteralPath $WorkloadContract -Raw | ConvertFrom-Json
$FomSha256 = (Get-FileHash -LiteralPath $Fom -Algorithm SHA256).Hash.ToLowerInvariant()
if ($Workload.schema -ne 'gorti.fair-comparison/workload-v1' -or
    $Workload.fom_sha256 -ne $FomSha256 -or
    [string]$Workload.seed -ne $Seed -or
    [int]$Workload.count -ne $Count -or
    $Workload.two_process -ne $true -or
    $Workload.choreography -ne 'sequential_update_send_then_tar' -or
    $Workload.delivery_boundary -ne 'subscriber_pre_tar_to_both_callbacks' -or
    $Workload.callback -ne 'immediate' -or
    $Workload.server_event_log -ne $ServerEventLog) {
    throw 'Caller arguments do not match the supplied fair-comparison workload contract.'
}

New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null
if ([string]::IsNullOrWhiteSpace($CrcAddress)) {
    $PortProbe = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
    $PortProbe.Start()
    $Port = ([Net.IPEndPoint]$PortProbe.LocalEndpoint).Port
    $PortProbe.Stop()
    $CrcAddress = "127.0.0.1:$Port"
}
$ExactArgv = @(
    '-Fom', $Fom, '-Seed', $Seed, '-Count', [string]$Count,
    '-ServerEventLog', $ServerEventLog, '-OutputDirectory', $OutputDirectory,
    '-RunId', $RunId, '-WorkloadContract', $WorkloadContract,
    '-CrcAddress', $CrcAddress, '-TimeoutMs', [string]$TimeoutMs
)
if (-not [string]::IsNullOrWhiteSpace($PRTIHome)) {
    $ExactArgv += @('-PRTIHome', [IO.Path]::GetFullPath($PRTIHome))
}
$Utf8NoBom = New-Object System.Text.UTF8Encoding($false)
$ArgvPath = Join-Path $OutputDirectory 'pitch-exact-argv.json'
[IO.File]::WriteAllText($ArgvPath,
    (([ordered]@{ argv = $ExactArgv } | ConvertTo-Json -Depth 3) + "`n"), $Utf8NoBom)

$RunArguments = @(
    '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', (Join-Path $Root 'Run.ps1'),
    '-Fom', $Fom, '-Seed', $Seed, '-Count', [string]$Count,
    '-ServerEventLog', $ServerEventLog, '-OutputDirectory', $OutputDirectory,
    '-CrcAddress', $CrcAddress, '-TimeoutMs', [string]$TimeoutMs
)
if (-not [string]::IsNullOrWhiteSpace($PRTIHome)) {
    $RunArguments += @('-PRTIHome', $PRTIHome)
}
& powershell @RunArguments
if ($LASTEXITCODE -ne 0) {
    throw "Pitch fair run failed with exit code $LASTEXITCODE."
}

$ResultPath = Join-Path $OutputDirectory 'result.json'
& $PythonExecutable (Join-Path $Root 'build_fair_result.py') `
    --canonical (Join-Path $OutputDirectory 'canonical.ndjson') `
    --benchmark (Join-Path $OutputDirectory 'benchmark.json') `
    --evidence (Join-Path $OutputDirectory 'run-evidence.json') `
    --workload $WorkloadContract --argv $ArgvPath --run-id $RunId --output $ResultPath
if ($LASTEXITCODE -ne 0) {
    throw "Pitch result construction failed with exit code $LASTEXITCODE."
}

$Check = Join-Path $Repo 'verification\fair-comparison\check_contract.py'
if (Test-Path -LiteralPath $Check -PathType Leaf) {
    & $PythonExecutable $Check result $ResultPath --expected-workload $WorkloadContract `
        --implementation pitch --run-id $RunId
    if ($LASTEXITCODE -ne 0) {
        throw 'Pitch result failed the shared fair-comparison contract.'
    }
}

Write-Host "Pitch fair result: $ResultPath"

[CmdletBinding()]
param(
    [string]$PorticoHome = '',
    [string]$Fom = '',
    [string]$Workload = '',
    [ValidateRange(0, 1000000)][int]$Count = 0,
    [ValidateRange(0, 1000000)][int]$OperationWarmup = 128,
    [ValidateRange(0, 100)][int]$Warmup = 5,
    [ValidateRange(1, 100)][int]$Measured = 30,
    [string]$Seed = '1516',
    [ValidateRange(1000, 600000)][int]$TimeoutMs = 30000,
    [ValidateSet('local-lrc', 'confirmed')][string]$GortiTransport = 'local-lrc',
    [Alias('Config')][string]$TransportConfig = '',
    [ValidateSet('none', 'event-journal')][string]$GortiAuditReplayPlugin = 'none',
    [string]$OutputDirectory = '',
    [string]$Python = ''
)

$ErrorActionPreference = 'Stop'
$Root = $PSScriptRoot
$Repo = [IO.Path]::GetFullPath((Join-Path $Root '..\..'))
if ([string]::IsNullOrWhiteSpace($PorticoHome)) {
    $PorticoHome = Join-Path $Repo '.tools\portico-extracted\portico-2.1.4'
}
if ([string]::IsNullOrWhiteSpace($Fom)) {
    $Fom = Join-Path $Repo 'verification\commercial-rti\fom\CommercialRtiVerifier.xml'
}
if ([string]::IsNullOrWhiteSpace($Workload)) {
    $Workload = Join-Path $Repo 'benchmark\devstone\workload\workload.json'
}
if ($Count -ne 0) {
    throw '-Count is no longer accepted; callback count is defined by -Workload.'
}
if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $Timestamp = [DateTime]::UtcNow.ToString('yyyyMMdd-HHmmss')
    $OutputDirectory = Join-Path $Repo "verification\out\portico-comparison-$Timestamp"
}
if ([string]::IsNullOrWhiteSpace($Python)) {
    $VenvPython = Join-Path $Repo 'pysdk\.venv\Scripts\python.exe'
    $Python = if (Test-Path -LiteralPath $VenvPython -PathType Leaf) { $VenvPython } else { 'python' }
}

$PorticoHome = (Resolve-Path -LiteralPath $PorticoHome).Path
$Fom = (Resolve-Path -LiteralPath $Fom).Path
$Workload = (Resolve-Path -LiteralPath $Workload).Path
if (-not [string]::IsNullOrWhiteSpace($TransportConfig)) {
    $TransportConfig = (Resolve-Path -LiteralPath $TransportConfig).Path
}
$ApiJar = Join-Path $PorticoHome 'lib\portico.jar'
$BuildScript = Join-Path $Repo 'verification\commercial-rti\Build.ps1'
& powershell -NoProfile -ExecutionPolicy Bypass -File $BuildScript -ApiJar $ApiJar
if ($LASTEXITCODE -ne 0) { throw 'Unable to compile the Java verifier against Portico.' }

$VerifierJar = Join-Path $Repo 'verification\commercial-rti\build\reference_rti-verifier.jar'
$ComparisonScript = Join-Path $Root 'compare_receive_order.py'
$ComparisonArguments = @(
    $ComparisonScript,
    '--repo', $Repo,
    '--portico-home', $PorticoHome,
    '--verifier-jar', $VerifierJar,
    '--fom', $Fom,
    '--workload', $Workload,
    '--output', ([IO.Path]::GetFullPath($OutputDirectory)),
    '--seed', $Seed,
    '--operation-warmup', $OperationWarmup,
    '--warmup', $Warmup,
    '--measured', $Measured,
    '--timeout-ms', $TimeoutMs,
    '--gorti-audit-replay-plugin', $GortiAuditReplayPlugin
)
if (-not [string]::IsNullOrWhiteSpace($TransportConfig)) {
    $ComparisonArguments += @('--transport-config', $TransportConfig)
}
if ($PSBoundParameters.ContainsKey('GortiTransport')) {
    $ComparisonArguments += @('--gorti-transport', $GortiTransport)
}
& $Python @ComparisonArguments
if ($LASTEXITCODE -ne 0) { throw "Portico comparison failed with exit code $LASTEXITCODE." }

Write-Host "Comparison result: $(Join-Path ([IO.Path]::GetFullPath($OutputDirectory)) 'comparison.json')"

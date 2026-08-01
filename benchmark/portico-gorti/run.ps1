[CmdletBinding()]
param(
    [string]$Output = '',
    [string]$PorticoHome = '',
    [string]$Fom = '',
    [string]$VerifierJar = '',
    [string]$Experiment = '',
    [ValidateRange(1, 1000000)][int]$OperationWarmup = 128,
    [ValidateRange(1000, 600000)][int]$TimeoutMs = 300000,
    [ValidateRange(100, 1000000)][int]$BootstrapResamples = 10000,
    [string]$Python = 'python',
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$Orchestrator = Join-Path $PSScriptRoot 'orchestrator.py'
$CliArgs = @(
    $Orchestrator,
    '--operation-warmup', $OperationWarmup,
    '--timeout-ms', $TimeoutMs,
    '--bootstrap-resamples', $BootstrapResamples
)
if (-not [string]::IsNullOrWhiteSpace($Output)) {
    $CliArgs += @('--output', $Output)
}
if (-not [string]::IsNullOrWhiteSpace($PorticoHome)) {
    $CliArgs += @('--portico-home', $PorticoHome)
}
if (-not [string]::IsNullOrWhiteSpace($Fom)) {
    $CliArgs += @('--fom', $Fom)
}
if (-not [string]::IsNullOrWhiteSpace($VerifierJar)) {
    $CliArgs += @('--verifier-jar', $VerifierJar)
}
if (-not [string]::IsNullOrWhiteSpace($Experiment)) {
    $CliArgs += @('--experiment', $Experiment)
}
if ($DryRun) {
    $CliArgs += '--dry-run'
}

& $Python @CliArgs
if ($LASTEXITCODE -ne 0) {
    throw "DEVStone-HLA benchmark failed with exit code $LASTEXITCODE."
}

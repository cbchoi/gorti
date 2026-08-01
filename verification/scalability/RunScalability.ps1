[CmdletBinding()]
param(
    [string]$PorticoHome = '',
    [string]$Workload = '',
    [string]$OutputDirectory = '',
    [int[]]$Scales = @(2, 3, 4, 8, 16),
    [ValidateRange(0, 20)][int]$Warmup = 1,
    [ValidateRange(1, 30)][int]$Measured = 3,
    [ValidateRange(0, 60)][double]$LaunchIntervalSeconds = 0.5,
    [ValidateSet('udp', 'tcp-override')][string]$PorticoTransport = 'tcp-override',
    [ValidateSet('portico', 'gorti')][string[]]$Implementations = @('portico', 'gorti'),
    [ValidateRange(1000, 600000)][int]$TimeoutMs = 300000
)

$ErrorActionPreference = 'Stop'
$Repo = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
if ([string]::IsNullOrWhiteSpace($PorticoHome)) {
    $PorticoHome = Join-Path $Repo '.tools\portico-extracted\portico-2.1.4'
}
if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $OutputDirectory = Join-Path $Repo 'verification\out\scalability'
}
if ([string]::IsNullOrWhiteSpace($Workload)) {
    $Workload = Join-Path $Repo 'benchmark\devstone\workload\workload.json'
}
$ApiJar = Join-Path $PorticoHome 'lib\portico.jar'
& powershell -NoProfile -ExecutionPolicy Bypass `
    -File (Join-Path $Repo 'verification\commercial-rti\Build.ps1') -ApiJar $ApiJar
if ($LASTEXITCODE -ne 0) { throw 'Unable to build the Java verifier.' }

$Python = Join-Path $Repo 'pysdk\.venv\Scripts\python.exe'
& $Python (Join-Path $PSScriptRoot 'run_scalability.py') `
    --repo $Repo `
    --portico-home $PorticoHome `
    --verifier-jar (Join-Path $Repo 'verification\commercial-rti\build\reference_rti-verifier.jar') `
    --fom (Join-Path $Repo 'verification\commercial-rti\fom\CommercialRtiVerifier.xml') `
    --workload ([IO.Path]::GetFullPath($Workload)) `
    --output ([IO.Path]::GetFullPath($OutputDirectory)) `
    --scales $Scales `
    --warmup $Warmup `
    --measured $Measured `
    --launch-interval-seconds $LaunchIntervalSeconds `
    --portico-transport $PorticoTransport `
    --implementations $Implementations `
    --timeout-ms $TimeoutMs
if ($LASTEXITCODE -ne 0) { throw "Scalability comparison failed with exit code $LASTEXITCODE." }

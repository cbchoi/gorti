[CmdletBinding()]
param(
    [ValidateSet('1516')][string]$Seed = '1516',
    [ValidateRange(1, 1000000)][int]$Count = 100,
    [string]$ServerAddress = '',
    [string]$FederationName = '',
    [string]$OutputDirectory = '',
    [Parameter(Mandatory = $true)][string]$Fom,
    [string]$ApiJar = $env:REFERENCE_RTI_API_JAR,
    [string]$Java = $env:REFERENCE_RTI_JAVA,
    [string]$Launcher = $env:REFERENCE_RTI_LAUNCHER,
    [string[]]$LauncherArgument = @(),
    [ValidateRange(1000, 600000)][int]$TimeoutMs = 30000,
    [ValidateSet('off', 'file')][string]$ServerEventLog = 'off',
    [string]$RunId = 'manual',
    [string]$WorkloadContract = '',
    [string]$ObjectClass = 'VerifierEntity',
    [string]$InteractionClass = 'VerifierMessage',
    [string]$ObjectName = 'CommercialRtiVerifierEntity'
)

$ErrorActionPreference = 'Stop'
$Root = $PSScriptRoot

function Resolve-RequiredFile([string]$Value, [string]$Name) {
    if ([string]::IsNullOrWhiteSpace($Value)) {
        throw "$Name is required. Configure it outside the repository."
    }
    $Resolved = [IO.Path]::GetFullPath($Value)
    if (-not (Test-Path -LiteralPath $Resolved -PathType Leaf)) {
        throw "$Name does not exist: '$Resolved'."
    }
    return (Resolve-Path -LiteralPath $Resolved).Path
}

$Fom = Resolve-RequiredFile $Fom 'Fom'
$ApiJar = Resolve-RequiredFile $ApiJar 'REFERENCE_RTI_API_JAR'
$Launcher = Resolve-RequiredFile $Launcher 'REFERENCE_RTI_LAUNCHER'
if ([string]::IsNullOrWhiteSpace($Java)) {
    $Java = (Get-Command java -ErrorAction Stop).Source
} else {
    $Java = Resolve-RequiredFile $Java 'REFERENCE_RTI_JAVA'
}

if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $OutputDirectory = Join-Path $Root 'logs'
}
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null
if ([string]::IsNullOrWhiteSpace($FederationName)) {
    $FederationName = "GortiReferenceVerifier-$PID"
}
if (-not [string]::IsNullOrWhiteSpace($WorkloadContract)) {
    $WorkloadContract = Resolve-RequiredFile $WorkloadContract 'WorkloadContract'
}

& (Join-Path $Root 'Build.ps1') -ApiJar $ApiJar
if ($LASTEXITCODE -ne 0) {
    throw "IEEE 1516e verifier build failed with exit code $LASTEXITCODE."
}
$VerifierJar = Resolve-RequiredFile (
    (Join-Path $Root 'build\reference_rti-verifier.jar')) 'VerifierJar'

$Arguments = @(
    '--verifier-jar', $VerifierJar,
    '--api-jar', $ApiJar,
    '--java', $Java,
    '--fom', $Fom,
    '--seed', $Seed,
    '--count', [string]$Count,
    '--server-event-log', $ServerEventLog,
    '--output-directory', $OutputDirectory,
    '--federation-name', $FederationName,
    '--run-id', $RunId,
    '--timeout-ms', [string]$TimeoutMs
)
if (-not [string]::IsNullOrWhiteSpace($ServerAddress)) {
    $Arguments += @('--server-address', $ServerAddress)
}
if (-not [string]::IsNullOrWhiteSpace($WorkloadContract)) {
    $Arguments += @('--workload-contract', $WorkloadContract)
}
if (-not [string]::IsNullOrWhiteSpace($ObjectClass)) {
    $Arguments += @('--object-class', $ObjectClass)
}
if (-not [string]::IsNullOrWhiteSpace($InteractionClass)) {
    $Arguments += @('--interaction-class', $InteractionClass)
}
if (-not [string]::IsNullOrWhiteSpace($ObjectName)) {
    $Arguments += @('--object-name', $ObjectName)
}
$Arguments += $LauncherArgument

$Utf8NoBom = New-Object Text.UTF8Encoding($false)
$Invocation = [ordered]@{
    schema = 'gorti.reference-rti/local-launch-v1'
    launcher = $Launcher
    arguments = $Arguments
}
[IO.File]::WriteAllText(
    (Join-Path $OutputDirectory 'local-launch.json'),
    (($Invocation | ConvertTo-Json -Depth 8) + "`n"),
    $Utf8NoBom)

if ([IO.Path]::GetExtension($Launcher) -ieq '.ps1') {
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $Launcher @Arguments
} else {
    & $Launcher @Arguments
}
if ($LASTEXITCODE -ne 0) {
    throw "Local reference RTI launcher failed with exit code $LASTEXITCODE."
}

foreach ($Name in @('canonical.ndjson', 'benchmark.json', 'run-evidence.json')) {
    $Artifact = Join-Path $OutputDirectory $Name
    if (-not (Test-Path -LiteralPath $Artifact -PathType Leaf)) {
        throw "Local launcher did not produce required artifact '$Artifact'."
    }
}

Write-Host "Reference RTI artifacts: $OutputDirectory"

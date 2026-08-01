[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Fom,
    [Parameter(Mandatory = $true)][string]$Seed,
    [Parameter(Mandatory = $true)][int]$Count,
    [Parameter(Mandatory = $true)][ValidateSet('off', 'file')][string]$ServerEventLog,
    [Parameter(Mandatory = $true)][string]$OutputDirectory,
    [Parameter(Mandatory = $true)][string]$RunId,
    [Parameter(Mandatory = $true)][string]$WorkloadContract,
    [string]$ApiJar = $env:REFERENCE_RTI_API_JAR,
    [string]$Java = $env:REFERENCE_RTI_JAVA,
    [string]$Launcher = $env:REFERENCE_RTI_LAUNCHER,
    [string]$ServerAddress = $env:REFERENCE_RTI_SERVER_ADDRESS,
    [string[]]$LauncherArgument = @(),
    [string]$Python = $env:GORTI_FAIR_PYTHON,
    [ValidateRange(1000, 600000)][int]$TimeoutMs = 120000
)

$ErrorActionPreference = 'Stop'
$AdapterRoot = $PSScriptRoot
$RepoRoot = [IO.Path]::GetFullPath((Join-Path $AdapterRoot '..\..\..'))
$Runner = Join-Path $RepoRoot 'verification\commercial-rti\FairRun.ps1'

if ([string]::IsNullOrWhiteSpace($Python)) { $Python = 'python' }
if (-not (Test-Path -LiteralPath $Runner -PathType Leaf)) {
    throw "Reference RTI fair runner not found at '$Runner'."
}

$Arguments = @{
    Fom = $Fom
    Seed = $Seed
    Count = $Count
    ServerEventLog = $ServerEventLog
    OutputDirectory = $OutputDirectory
    RunId = $RunId
    WorkloadContract = $WorkloadContract
    TimeoutMs = $TimeoutMs
    Python = $Python
}
foreach ($Binding in @{
    ApiJar = $ApiJar
    Java = $Java
    Launcher = $Launcher
    ServerAddress = $ServerAddress
}.GetEnumerator()) {
    if (-not [string]::IsNullOrWhiteSpace([string]$Binding.Value)) {
        $Arguments[$Binding.Key] = [string]$Binding.Value
    }
}
if ($LauncherArgument.Count -gt 0) {
    $Arguments.LauncherArgument = $LauncherArgument
}

& $Runner @Arguments
if ($LASTEXITCODE -ne 0) {
    throw "Reference RTI fair adapter failed with exit code $LASTEXITCODE."
}

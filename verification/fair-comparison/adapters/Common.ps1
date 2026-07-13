$ErrorActionPreference = 'Stop'

# Start-Process rejects managed environments containing both Path and PATH.
$ProcessPath = [Environment]::GetEnvironmentVariable(
    'Path', [EnvironmentVariableTarget]::Process)
[Environment]::SetEnvironmentVariable(
    'PATH', $null, [EnvironmentVariableTarget]::Process)
[Environment]::SetEnvironmentVariable(
    'Path', $ProcessPath, [EnvironmentVariableTarget]::Process)

function Get-FairSha256([string]$Path) {
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Write-FairJson([string]$Path, [object]$Value) {
    $Utf8NoBom = New-Object Text.UTF8Encoding($false)
    [IO.File]::WriteAllText($Path, (($Value | ConvertTo-Json -Depth 20) + "`n"), $Utf8NoBom)
}

function Get-FairGitValue([string]$RepoRoot, [string[]]$Arguments, [string]$Fallback) {
    try {
        $Value = ((& git -C $RepoRoot @Arguments 2>$null) -join "`n").Trim()
        if ($LASTEXITCODE -eq 0 -and $Value) { return $Value }
    } catch { }
    return $Fallback
}

function Assert-FairAdapterInputs(
    [string]$Fom,
    [string]$Seed,
    [int]$Count,
    [string]$ServerEventLog,
    [string]$WorkloadContract,
    [string]$RepoRoot
) {
    $CanonicalFom = (Resolve-Path -LiteralPath (
        Join-Path $RepoRoot 'verification\pitch\fom\PitchVerifier.xml')).Path
    $ResolvedFom = (Resolve-Path -LiteralPath $Fom).Path
    if (-not [string]::Equals(
        $ResolvedFom, $CanonicalFom, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Fom must be the canonical PitchVerifier.xml path: '$CanonicalFom'."
    }
    if ($Seed -ne '1516') { throw 'Seed must be exactly 1516.' }
    if ($ServerEventLog -notin @('off', 'file')) {
        throw 'ServerEventLog must be exactly off or file.'
    }
    $WorkloadContract = (Resolve-Path -LiteralPath $WorkloadContract).Path
    $Workload = Get-Content -LiteralPath $WorkloadContract -Raw | ConvertFrom-Json
    if ($Workload.schema -ne 'gorti.fair-comparison/workload-v1' -or
        [long]$Workload.seed -ne 1516 -or [int]$Workload.count -ne $Count -or
        $Workload.two_process -ne $true -or
        $Workload.choreography -ne 'sequential_update_send_then_tar' -or
        $Workload.delivery_boundary -ne 'subscriber_pre_tar_to_both_callbacks' -or
        $Workload.callback -ne 'immediate' -or
        $Workload.server_event_log -ne $ServerEventLog) {
        throw 'WorkloadContract fields differ from the fixed fair-comparison workload.'
    }
    $FomHash = Get-FairSha256 $ResolvedFom
    if ($Workload.fom_sha256 -ne $FomHash) {
        throw 'Canonical FOM bytes differ from WorkloadContract.fom_sha256.'
    }
    return [pscustomobject]@{
        Fom = $ResolvedFom
        WorkloadContract = $WorkloadContract
        FomSha256 = $FomHash
    }
}

function Invoke-FairConverter(
    [string]$Python,
    [string]$AdapterRoot,
    [string]$OutputDirectory,
    [string]$WorkloadContract,
    [string]$Fom,
    [string]$Implementation,
    [string]$RunId
) {
    $PythonExecutable = (Get-Command $Python -ErrorAction Stop).Source
    & $PythonExecutable (Join-Path $AdapterRoot 'convert_result.py') `
        --benchmark (Join-Path $OutputDirectory 'benchmark.json') `
        --canonical (Join-Path $OutputDirectory 'canonical.ndjson') `
        --workload $WorkloadContract `
        --provenance (Join-Path $OutputDirectory 'adapter-provenance.json') `
        --fom $Fom --implementation $Implementation --run-id $RunId `
        --output (Join-Path $OutputDirectory 'result.json')
    if ($LASTEXITCODE -ne 0) {
        throw "$Implementation benchmark could not be converted to launcher-result-v1."
    }
}

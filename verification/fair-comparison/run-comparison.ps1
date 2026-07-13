[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ConfigPath,
    [ValidateRange(1, 2147483647)]
    [int]$Count = 100,
    [ValidateSet('off', 'file')]
    [string]$ServerEventLog = 'file',
    [string]$OutputDirectory = '',
    [ValidateRange(0, 100)]
    [int]$WarmupPairs = 5,
    [ValidateRange(2, 1000)]
    [int]$MeasuredPairs = 20,
    [ValidateRange(0, 2147483647)]
    [int]$OrderSeed = 1516,
    [ValidateRange(100, 1000000)]
    [int]$BootstrapResamples = 10000,
    [string]$Python = 'python',
    [switch]$ClaimGrade,
    [switch]$NoAnalyze
)

$ErrorActionPreference = 'Stop'
$ScriptRoot = $PSScriptRoot
$RepoRoot = [IO.Path]::GetFullPath((Join-Path $ScriptRoot '..\..'))
$CanonicalFom = [IO.Path]::GetFullPath(
    (Join-Path $RepoRoot 'verification\pitch\fom\PitchVerifier.xml'))
$CheckScript = Join-Path $ScriptRoot 'check_contract.py'
$AnalyzeScript = Join-Path $ScriptRoot 'analyze.py'
$Seed = 1516
$ClaimWarmupPairs = 5
$ClaimMeasuredPairs = 20

if ($ClaimGrade -and (
    $WarmupPairs -ne $ClaimWarmupPairs -or
    $MeasuredPairs -ne $ClaimMeasuredPairs -or
    $ServerEventLog -ne 'file')) {
    throw '-ClaimGrade requires exactly 5 warmup pairs, exactly 20 measured pairs, and ServerEventLog=file.'
}

if (-not (Test-Path -LiteralPath $CanonicalFom -PathType Leaf)) {
    throw "Canonical Pitch FOM not found at '$CanonicalFom'."
}
$ConfigPath = (Resolve-Path -LiteralPath $ConfigPath).Path
$PythonExecutable = (Get-Command $Python -ErrorAction Stop).Source

& $PythonExecutable $CheckScript config $ConfigPath
if ($LASTEXITCODE -ne 0) {
    throw 'Launcher configuration failed the fair-comparison contract.'
}
$Config = Get-Content -LiteralPath $ConfigPath -Raw | ConvertFrom-Json

$SessionId = 'fair-{0}-{1}' -f (
    [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssfffZ')), ([Guid]::NewGuid().ToString('N').Substring(0, 8))
if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $OutputDirectory = Join-Path $ScriptRoot (Join-Path 'out' $SessionId)
}
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
if (Test-Path -LiteralPath $OutputDirectory) {
    if (@(Get-ChildItem -LiteralPath $OutputDirectory -Force).Count -gt 0) {
        throw "OutputDirectory must be absent or empty: '$OutputDirectory'."
    }
}
New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null

function Get-UtcTimestamp {
    return [DateTime]::UtcNow.ToString('o')
}

function Get-Sha256([string]$Path) {
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Write-JsonAtomic([string]$Path, [object]$Value) {
    $Temporary = "$Path.tmp"
    $Json = $Value | ConvertTo-Json -Depth 30
    $Utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [IO.File]::WriteAllText($Temporary, $Json + "`n", $Utf8NoBom)
    Move-Item -LiteralPath $Temporary -Destination $Path -Force
}

function Expand-Template([string]$Value, [hashtable]$Tokens) {
    $Expanded = $Value
    foreach ($Name in $Tokens.Keys) {
        $Expanded = $Expanded.Replace("{$Name}", [string]$Tokens[$Name])
    }
    if ($Expanded -match '\{[a-z_]+\}') {
        throw "Unknown or unresolved command-template token in '$Expanded'."
    }
    return $Expanded
}

function Resolve-Executable([string]$Value, [string]$WorkingDirectory) {
    $Candidate = if ([IO.Path]::IsPathRooted($Value)) {
        $Value
    } else {
        Join-Path $WorkingDirectory $Value
    }
    if (Test-Path -LiteralPath $Candidate -PathType Leaf) {
        return (Resolve-Path -LiteralPath $Candidate).Path
    }
    return (Get-Command $Value -ErrorAction Stop).Source
}

function New-BalancedOrders([int]$PairCount, [System.Random]$Random) {
    $Orders = New-Object System.Collections.ArrayList
    $First = if ($Random.Next(2) -eq 0) { 'AB' } else { 'BA' }
    for ($Index = 0; $Index -lt $PairCount; $Index++) {
        $Order = if (($Index % 2) -eq 0) {
            $First
        } elseif ($First -eq 'AB') {
            'BA'
        } else {
            'AB'
        }
        [void]$Orders.Add($Order)
    }
    return @($Orders)
}

function Test-StrictAlternation([string[]]$Orders) {
    for ($Index = 1; $Index -lt $Orders.Count; $Index++) {
        if ($Orders[$Index] -eq $Orders[$Index - 1]) {
            return $false
        }
    }
    return $true
}

function Invoke-ConfiguredLauncher(
    [string]$Executable,
    [string[]]$Arguments,
    [string]$WorkingDirectory,
    [hashtable]$Environment,
    [string]$StdoutPath,
    [string]$StderrPath
) {
    $Previous = @{}
    foreach ($Name in $Environment.Keys) {
        $Previous[$Name] = [Environment]::GetEnvironmentVariable(
            $Name, [EnvironmentVariableTarget]::Process)
        [Environment]::SetEnvironmentVariable(
            $Name, [string]$Environment[$Name], [EnvironmentVariableTarget]::Process)
    }
    Push-Location $WorkingDirectory
    $PreviousErrorActionPreference = $ErrorActionPreference
    try {
        # Native launchers may write benign diagnostics to stderr. The process
        # exit code, result contract, and artifact validation decide success.
        $ErrorActionPreference = 'Continue'
        $global:LASTEXITCODE = 0
        & $Executable @Arguments 1> $StdoutPath 2> $StderrPath
        return [int]$LASTEXITCODE
    } finally {
        $ErrorActionPreference = $PreviousErrorActionPreference
        Pop-Location
        foreach ($Name in $Environment.Keys) {
            [Environment]::SetEnvironmentVariable(
                $Name, $Previous[$Name], [EnvironmentVariableTarget]::Process)
        }
    }
}

$FomSha256 = Get-Sha256 $CanonicalFom
$Workload = [ordered]@{
    schema = 'gorti.fair-comparison/workload-v1'
    fom_sha256 = $FomSha256
    seed = $Seed
    count = $Count
    two_process = $true
    choreography = 'sequential_update_send_then_tar'
    delivery_boundary = 'subscriber_pre_tar_to_both_callbacks'
    callback = 'immediate'
    server_event_log = $ServerEventLog
}
$WorkloadPath = Join-Path $OutputDirectory 'workload.json'
Write-JsonAtomic $WorkloadPath $Workload

$Random = [System.Random]::new($OrderSeed)
$SchedulePairs = @()
$AllOrders = @(New-BalancedOrders ($WarmupPairs + $MeasuredPairs) $Random)
for ($Index = 0; $Index -lt $WarmupPairs; $Index++) {
    $SchedulePairs += [ordered]@{
        phase = 'warmup'; pair_index = $Index + 1; order = $AllOrders[$Index]
    }
}
for ($Index = 0; $Index -lt $MeasuredPairs; $Index++) {
    $SchedulePairs += [ordered]@{
        phase = 'measured'; pair_index = $Index + 1
        order = $AllOrders[$WarmupPairs + $Index]
    }
}
$ScheduleOrders = @($SchedulePairs | ForEach-Object { [string]$_.order })
$MeasuredOrders = @($SchedulePairs | Where-Object { $_.phase -eq 'measured' } |
    ForEach-Object { [string]$_.order })
$StrictAlternation = Test-StrictAlternation $ScheduleOrders
$MeasuredAB = @($MeasuredOrders | Where-Object { $_ -eq 'AB' }).Count
$MeasuredBA = @($MeasuredOrders | Where-Object { $_ -eq 'BA' }).Count
if (-not $StrictAlternation) {
    throw 'Generated comparison schedule is not strictly AB/BA alternating.'
}
if ($ClaimGrade -and ($MeasuredAB -ne 10 -or $MeasuredBA -ne 10)) {
    throw "-ClaimGrade requires measured orientation counts AB=10 and BA=10; generated AB=$MeasuredAB and BA=$MeasuredBA."
}
$EvidenceGrade = if ($ClaimGrade) { 'claim' } else { 'smoke' }
$ScheduleAttestation = [ordered]@{
    grade = $EvidenceGrade
    claim_grade_requested = [bool]$ClaimGrade
    requirements = [ordered]@{
        warmup_pairs = $ClaimWarmupPairs
        measured_pairs = $ClaimMeasuredPairs
        server_event_log = 'file'
        strict_alternation = $true
        measured_orientation = [ordered]@{ AB = 10; BA = 10 }
    }
    observed = [ordered]@{
        warmup_pairs = $WarmupPairs
        measured_pairs = $MeasuredPairs
        server_event_log = $ServerEventLog
        strict_alternation = $StrictAlternation
        measured_orientation = [ordered]@{ AB = $MeasuredAB; BA = $MeasuredBA }
    }
    satisfied = [bool]($ClaimGrade -and
        $WarmupPairs -eq $ClaimWarmupPairs -and
        $MeasuredPairs -eq $ClaimMeasuredPairs -and
        $ServerEventLog -eq 'file' -and $StrictAlternation -and
        $MeasuredAB -eq 10 -and $MeasuredBA -eq 10)
}

function Get-GitValue([string[]]$Arguments, [string]$Fallback) {
    try {
        $Value = ((& git -C $RepoRoot -c core.excludesFile= @Arguments 2>$null) -join "`n").Trim()
        if ($LASTEXITCODE -eq 0 -and $Value) { return $Value }
    } catch { }
    return $Fallback
}

$Cpu = 'unavailable'
try {
    $Cpu = @(Get-CimInstance Win32_Processor | ForEach-Object {
        [ordered]@{
            name = $_.Name
            logical_processors = $_.NumberOfLogicalProcessors
            max_clock_mhz = $_.MaxClockSpeed
        }
    })
} catch { }
$PowerState = 'unavailable'
try {
    $PowerState = ((& powercfg /getactivescheme 2>$null) -join "`n").Trim()
} catch { }
$GitStatus = Get-GitValue -Arguments @('status', '--porcelain') -Fallback ''
$Dirty = -not [string]::IsNullOrWhiteSpace([string]$GitStatus)
$ManifestPath = Join-Path $OutputDirectory 'manifest.json'
$Manifest = [ordered]@{
    schema = 'gorti.fair-comparison/session-manifest-v1'
    session_id = $SessionId
    state = 'running'
    created_at = Get-UtcTimestamp
    finished_at = $null
    workload = $Workload
    schedule = [ordered]@{
        warmup_pairs = $WarmupPairs
        measured_pairs = $MeasuredPairs
        order_seed = $OrderSeed
        pairs = $SchedulePairs
    }
    orchestrator_provenance = [ordered]@{
        script_path = $MyInvocation.MyCommand.Path
        script_sha256 = Get-Sha256 $MyInvocation.MyCommand.Path
        config_path = $ConfigPath
        config_sha256 = Get-Sha256 $ConfigPath
        canonical_fom_path = $CanonicalFom
        canonical_fom_sha256 = $FomSha256
        canonical_fom_copied = $false
        git_commit = Get-GitValue -Arguments @('rev-parse', 'HEAD') -Fallback 'unknown'
        git_branch = Get-GitValue -Arguments @('branch', '--show-current') -Fallback 'unknown'
        git_dirty = $Dirty
        powershell = $PSVersionTable.PSVersion.ToString()
        python = $PythonExecutable
        os = [Environment]::OSVersion.VersionString
        machine = [Environment]::MachineName
        cpu = $Cpu
        power_state = $PowerState
        gomaxprocs = [Environment]::GetEnvironmentVariable('GOMAXPROCS')
        server_event_log = $ServerEventLog
        schedule_attestation = $ScheduleAttestation
    }
    runs = @()
    analysis_path = $(if ($NoAnalyze) { $null } else { 'analysis.json' })
}
Write-JsonAtomic $ManifestPath $Manifest

$GlobalIndex = 0
try {
    foreach ($Pair in $SchedulePairs) {
        $Implementations = if ($Pair.order -eq 'AB') { @('pitch', 'go') } else { @('go', 'pitch') }
        for ($SlotIndex = 0; $SlotIndex -lt 2; $SlotIndex++) {
            $GlobalIndex++
            $Slot = $SlotIndex + 1
            $Implementation = $Implementations[$SlotIndex]
            $RunId = '{0}-{1:d2}-s{2}-{3}' -f (
                $Pair.phase, $Pair.pair_index, $Slot, $Implementation)
            $RelativeOutput = '{0}/pair-{1:d2}/slot-{2}-{3}' -f (
                $Pair.phase, $Pair.pair_index, $Slot, $Implementation)
            $RunOutput = Join-Path $OutputDirectory $RelativeOutput
            New-Item -ItemType Directory -Path $RunOutput -Force | Out-Null
            $Tokens = @{
                repo = $RepoRoot
                fom = $CanonicalFom
                fom_sha256 = $FomSha256
                seed = $Seed.ToString([Globalization.CultureInfo]::InvariantCulture)
                count = $Count.ToString([Globalization.CultureInfo]::InvariantCulture)
                server_event_log = $ServerEventLog
                output = $RunOutput
                run_id = $RunId
                workload_file = $WorkloadPath
                phase = $Pair.phase
                pair = $Pair.pair_index.ToString([Globalization.CultureInfo]::InvariantCulture)
                slot = $Slot.ToString([Globalization.CultureInfo]::InvariantCulture)
            }
            $Launcher = $Config.launchers.$Implementation
            $WorkingTemplate = if ($null -eq $Launcher.working_directory) {
                '{repo}'
            } else { [string]$Launcher.working_directory }
            $WorkingDirectory = [IO.Path]::GetFullPath((Expand-Template $WorkingTemplate $Tokens))
            if (-not (Test-Path -LiteralPath $WorkingDirectory -PathType Container)) {
                throw "Launcher working directory does not exist: '$WorkingDirectory'."
            }
            $ExecutableTemplate = Expand-Template ([string]$Launcher.executable) $Tokens
            $Executable = Resolve-Executable $ExecutableTemplate $WorkingDirectory
            $Arguments = @($Launcher.arguments | ForEach-Object {
                Expand-Template ([string]$_) $Tokens
            })
            $Environment = @{}
            if ($null -ne $Launcher.environment) {
                foreach ($Property in $Launcher.environment.PSObject.Properties) {
                    $Environment[$Property.Name] = Expand-Template ([string]$Property.Value) $Tokens
                }
            }
            $ResultFile = Expand-Template ([string]$Launcher.result_file) $Tokens
            $ResultPath = [IO.Path]::GetFullPath((Join-Path $RunOutput $ResultFile))
            if (-not $ResultPath.StartsWith(
                $RunOutput + [IO.Path]::DirectorySeparatorChar,
                [StringComparison]::OrdinalIgnoreCase)) {
                throw 'Resolved result path escaped its run output directory.'
            }
            $RelativeResult = ($RelativeOutput + '/' + $ResultFile.Replace('\', '/'))
            $ExecutableSha = if (Test-Path -LiteralPath $Executable -PathType Leaf) {
                Get-Sha256 $Executable
            } else { 'unavailable' }
            $RunRecord = [ordered]@{
                global_index = $GlobalIndex
                phase = $Pair.phase
                pair_index = $Pair.pair_index
                slot = $Slot
                order = $Pair.order
                implementation = $Implementation
                run_id = $RunId
                output_directory = $RelativeOutput
                result_path = $RelativeResult
                command = [ordered]@{
                    executable = $Executable
                    executable_sha256 = $ExecutableSha
                    argv = $Arguments
                    working_directory = $WorkingDirectory
                    environment = $Environment
                }
                started_at = Get-UtcTimestamp
                finished_at = $null
                duration_ns = $null
                exit_code = $null
                status = 'running'
                result_sha256 = $null
            }
            $Manifest.runs = @($Manifest.runs) + @($RunRecord)
            Write-JsonAtomic $ManifestPath $Manifest

            $Stopwatch = [Diagnostics.Stopwatch]::StartNew()
            $ExitCode = Invoke-ConfiguredLauncher `
                $Executable $Arguments $WorkingDirectory $Environment `
                (Join-Path $RunOutput 'stdout.log') (Join-Path $RunOutput 'stderr.log')
            $Stopwatch.Stop()
            $RunRecord.exit_code = $ExitCode
            $RunRecord.duration_ns = [int64](
                $Stopwatch.ElapsedTicks * (1000000000.0 / [Diagnostics.Stopwatch]::Frequency))
            $RunRecord.finished_at = Get-UtcTimestamp
            if ($ExitCode -ne 0) {
                throw "$Implementation launcher failed for $RunId with exit code $ExitCode."
            }
            if (-not (Test-Path -LiteralPath $ResultPath -PathType Leaf)) {
                throw "$Implementation launcher did not produce '$ResultPath'."
            }
            & $PythonExecutable $CheckScript result $ResultPath `
                --expected-workload $WorkloadPath --implementation $Implementation --run-id $RunId
            if ($LASTEXITCODE -ne 0) {
                throw "$Implementation result failed the shared contract for $RunId."
            }
            $RunRecord.result_sha256 = Get-Sha256 $ResultPath
            $RunRecord.status = 'success'
            Write-JsonAtomic $ManifestPath $Manifest
        }
    }

    $Manifest.state = 'complete'
    $Manifest.finished_at = Get-UtcTimestamp
    Write-JsonAtomic $ManifestPath $Manifest
    if (-not $NoAnalyze) {
        $AnalysisPath = Join-Path $OutputDirectory 'analysis.json'
        & $PythonExecutable $AnalyzeScript $ManifestPath --output $AnalysisPath `
            --min-measured-pairs $MeasuredPairs --bootstrap-seed 1516 `
            --resamples $BootstrapResamples
        if ($LASTEXITCODE -ne 0) {
            throw 'Fair-comparison analysis rejected the completed session.'
        }
    }
} catch {
    if ($null -ne $RunRecord -and $RunRecord.status -eq 'running') {
        $RunRecord.status = 'failed'
        $RunRecord.finished_at = Get-UtcTimestamp
        $RunRecord.error = $_.Exception.Message
    }
    $Manifest.state = 'failed'
    $Manifest.finished_at = Get-UtcTimestamp
    $Manifest.failure = $_.Exception.Message
    Write-JsonAtomic $ManifestPath $Manifest
    throw
}

Write-Host "Fair comparison complete: $ManifestPath"
if (-not $NoAnalyze) {
    Write-Host "Analysis: $(Join-Path $OutputDirectory 'analysis.json')"
}

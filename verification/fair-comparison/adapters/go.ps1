[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Fom,
    [Parameter(Mandatory = $true)][string]$Seed,
    [Parameter(Mandatory = $true)][int]$Count,
    [Parameter(Mandatory = $true)][ValidateSet('off', 'file')][string]$ServerEventLog,
    [Parameter(Mandatory = $true)][string]$OutputDirectory,
    [Parameter(Mandatory = $true)][string]$RunId,
    [Parameter(Mandatory = $true)][string]$WorkloadContract,
    [string]$RtidPath = $env:GORTI_FAIR_RTID_PATH,
    [string]$Go = $env:GORTI_FAIR_GO,
    [string]$Python = $env:GORTI_FAIR_PYTHON,
    [string]$Address = $env:GORTI_FAIR_PERSISTENT_ADDRESS,
    [string]$PersistentPid = $env:GORTI_FAIR_PERSISTENT_PID,
    [ValidateRange(1000, 600000)][int]$TimeoutMs = 120000
)

$ErrorActionPreference = 'Stop'
$AdapterRoot = $PSScriptRoot
. (Join-Path $AdapterRoot 'Common.ps1')

function Get-KdrtiV2Header([string]$Path) {
    $HeaderBytes = New-Object byte[] 64
    $Stream = [IO.File]::Open(
        $Path, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
    try {
        $Offset = 0
        while ($Offset -lt $HeaderBytes.Length) {
            $Read = $Stream.Read($HeaderBytes, $Offset, $HeaderBytes.Length - $Offset)
            if ($Read -eq 0) { break }
            $Offset += $Read
        }
    } finally {
        $Stream.Dispose()
    }
    if ($Offset -ne $HeaderBytes.Length) {
        throw "Go event log '$Path' has a truncated kdrti/v2 header."
    }

    $Magic = @(
        [byte][char]'K', [byte][char]'D', [byte][char]'R', [byte][char]'T',
        [byte][char]'I', [byte]0, [byte]1, [byte]0
    )
    for ($Index = 0; $Index -lt $Magic.Count; $Index++) {
        if ($HeaderBytes[$Index] -ne $Magic[$Index]) {
            throw "Go event log '$Path' does not have kdrti magic."
        }
    }
    if ([BitConverter]::ToUInt32($HeaderBytes, 8) -ne 2) {
        throw "Go event log '$Path' is not kdrti/v2."
    }

    $FederationLength = 32
    while ($FederationLength -gt 0 -and $HeaderBytes[12 + $FederationLength - 1] -eq 0) {
        $FederationLength--
    }
    for ($Index = 0; $Index -lt $FederationLength; $Index++) {
        if ($HeaderBytes[12 + $Index] -eq 0) {
            throw "Go event log '$Path' has invalid federation padding."
        }
    }
    $StrictUtf8 = New-Object Text.UTF8Encoding($false, $true)
    try {
        $Federation = $StrictUtf8.GetString($HeaderBytes, 12, $FederationLength)
    } catch {
        throw "Go event log '$Path' has a non-UTF-8 federation header."
    }
    return [pscustomobject]@{
        Federation = $Federation
        Generation = [BitConverter]::ToUInt64($HeaderBytes, 44)
    }
}

$RepoRoot = (Resolve-Path (Join-Path $AdapterRoot '..\..\..')).Path
$Inputs = Assert-FairAdapterInputs `
    $Fom $Seed $Count $ServerEventLog $WorkloadContract $RepoRoot
$Fom = $Inputs.Fom
$WorkloadContract = $Inputs.WorkloadContract
if ([string]::IsNullOrWhiteSpace($RtidPath)) {
    $RtidPath = Join-Path $RepoRoot 'verification\out\fair-comparison\bin\rtid-fair.exe'
}
$RtidPath = (Resolve-Path -LiteralPath $RtidPath).Path
if (-not (Test-Path -LiteralPath $RtidPath -PathType Leaf)) {
    throw "RtidPath must identify an RTID executable: '$RtidPath'."
}
if ([string]::IsNullOrWhiteSpace($Go)) { $Go = 'go' }
$GoExecutable = (Get-Command $Go -ErrorAction Stop).Source
if ([string]::IsNullOrWhiteSpace($Python)) { $Python = 'python' }
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null
$ReuseRtid = $env:GORTI_FAIR_REUSE_RTID -eq '1'
$Federation = "GortiGoFair-$RunId"
$ServerLifecycle = if ($ReuseRtid) { 'persistent_session' } else { 'per_arm' }

if ([string]::IsNullOrWhiteSpace($Address)) {
    $PortProbe = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
    $PortProbe.Start()
    $Port = ([Net.IPEndPoint]$PortProbe.LocalEndpoint).Port
    $PortProbe.Stop()
    $Address = "127.0.0.1:$Port"
}
$AddressUri = [Uri]("tcp://{0}" -f $Address)
if (-not $AddressUri.Host -or $AddressUri.Port -lt 1) { throw 'Address must be host:port.' }

$SaveDirectory = if ($ReuseRtid) {
    [IO.Path]::GetFullPath($env:GORTI_FAIR_SHARED_SAVE_DIR)
} else {
    Join-Path $OutputDirectory 'rtid-saves'
}
$EventLogDirectory = if ($ReuseRtid) {
    [IO.Path]::GetFullPath($env:GORTI_FAIR_SHARED_EVENT_LOG_DIR)
} else {
    Join-Path $OutputDirectory 'rtid-eventlogs'
}
if (-not $ReuseRtid) {
    New-Item -ItemType Directory -Path $SaveDirectory -Force | Out-Null
}
$LogDirectoryArgument = if ($ServerEventLog -eq 'file') {
    New-Item -ItemType Directory -Path $EventLogDirectory -Force | Out-Null
    "--log-dir=$EventLogDirectory"
} else {
    '--log-dir='
}
$ServerLogRoot = if ($ReuseRtid) {
    Split-Path -Parent $EventLogDirectory
} else {
    $OutputDirectory
}
$ServerStdoutPath = Join-Path $ServerLogRoot $(if ($ReuseRtid) {
    'rtid.stdout.log'
} else {
    'adapter-rtid.stdout.log'
})
$ServerStderrPath = Join-Path $ServerLogRoot $(if ($ReuseRtid) {
    'rtid.stderr.log'
} else {
    'adapter-rtid.stderr.log'
})
$EventLogPathsBefore = @{}
foreach ($ExistingLog in @(
    Get-ChildItem -LiteralPath $EventLogDirectory -Filter '*.log' -Recurse -File `
        -ErrorAction SilentlyContinue)) {
    $EventLogPathsBefore[$ExistingLog.FullName.ToLowerInvariant()] = $true
}
$ServerArguments = @(
    "--listen=$Address",
    '--metrics-listen=127.0.0.1:0',
    '--admin-listen=',
    "--save-dir=$SaveDirectory",
    $LogDirectoryArgument,
    '--log-format=text'
)
$RtidProcess = $null
$ServerProcessEvidence = $null
try {
    if (-not $ReuseRtid) {
        $RtidProcess = Start-Process -FilePath $RtidPath -ArgumentList $ServerArguments `
            -WorkingDirectory $RepoRoot -WindowStyle Hidden -PassThru `
            -RedirectStandardOutput $ServerStdoutPath `
            -RedirectStandardError $ServerStderrPath
    }
    $Deadline = [DateTime]::UtcNow.AddSeconds(20)
    $Ready = $false
    while ([DateTime]::UtcNow -lt $Deadline) {
        if ($null -ne $RtidProcess -and $RtidProcess.HasExited) {
            throw "RTID exited during startup with code $($RtidProcess.ExitCode)."
        }
        $Client = New-Object Net.Sockets.TcpClient
        try {
            $Async = $Client.BeginConnect($AddressUri.Host, $AddressUri.Port, $null, $null)
            if ($Async.AsyncWaitHandle.WaitOne(200)) {
                $Client.EndConnect($Async)
                $Ready = $true
                break
            }
        } catch { } finally { $Client.Dispose() }
        Start-Sleep -Milliseconds 100
        if ($null -ne $RtidProcess) { $RtidProcess.Refresh() }
    }
    if (-not $Ready) { throw "RTID did not listen on $Address within 20 seconds." }

    $ObservedProcess = $RtidProcess
    if ($null -eq $ObservedProcess) {
        $ParsedPersistentPid = 0
        if (-not [int]::TryParse($PersistentPid, [ref]$ParsedPersistentPid) -or
                $ParsedPersistentPid -le 0) {
            throw 'Persistent RTID PID was not supplied by the session launcher.'
        }
        $ObservedProcess = Get-Process -Id $ParsedPersistentPid -ErrorAction Stop
    }
    $ObservedExecutable = [IO.Path]::GetFullPath($ObservedProcess.Path)
    if (-not [string]::Equals(
        $ObservedExecutable, $RtidPath, [StringComparison]::OrdinalIgnoreCase)) {
        throw "RTID listener executable '$ObservedExecutable' does not match '$RtidPath'."
    }
    $ServerProcessEvidence = [ordered]@{
        lifecycle = $ServerLifecycle
        pid = [int]$ObservedProcess.Id
        started_at = $ObservedProcess.StartTime.ToUniversalTime().ToString(
            'o', [Globalization.CultureInfo]::InvariantCulture)
        executable = $ObservedExecutable
        executable_sha256 = Get-FairSha256 $ObservedExecutable
        argv = @($RtidPath) + @($ServerArguments)
    }

    $Launcher = Join-Path $RepoRoot 'verification\gorti-go-fair\run.ps1'
    $LauncherArguments = @(
        '-Fom', $Fom,
        '-Seed', '1516',
        '-Count', $Count.ToString([Globalization.CultureInfo]::InvariantCulture),
        '-Federation', $Federation,
        '-OutputDirectory', $OutputDirectory,
        '-RtidPath', $RtidPath,
        '-Go', $GoExecutable,
        '-Address', $Address,
        '-TimeoutMs', $TimeoutMs.ToString([Globalization.CultureInfo]::InvariantCulture),
        '-ServerEventLog', $ServerEventLog,
        '-NoStartRtid'
    )
    $LauncherParameters = @{
        Fom = $Fom
        Seed = '1516'
        Count = $Count
        Federation = $Federation
        OutputDirectory = $OutputDirectory
        RtidPath = $RtidPath
        Go = $GoExecutable
        Address = $Address
        TimeoutMs = $TimeoutMs
        ServerEventLog = $ServerEventLog
        NoStartRtid = $true
    }
    & $Launcher @LauncherParameters
} finally {
    if ($null -ne $RtidProcess -and -not $RtidProcess.HasExited) {
        Stop-Process -Id $RtidProcess.Id -Force
        $RtidProcess.WaitForExit()
    }
}

$EventLogs = @(Get-ChildItem -LiteralPath $EventLogDirectory -Filter '*.log' -Recurse `
    -File -ErrorAction SilentlyContinue)
$NewEventLogs = @($EventLogs | Where-Object {
    -not $EventLogPathsBefore.ContainsKey($_.FullName.ToLowerInvariant())
})
if ($ServerEventLog -eq 'off' -and $NewEventLogs.Count -ne 0) {
    throw "Go RTID created event logs even though ServerEventLog=off."
}

$EventLogDescriptor = $null
if ($ServerEventLog -eq 'file') {
    $FederationDirectoryName = -join @(
        [Text.Encoding]::UTF8.GetBytes($Federation) | ForEach-Object { $_.ToString('x2') }
    )
    $ExpectedFederationDirectory = Join-Path $EventLogDirectory $FederationDirectoryName
    if ($NewEventLogs.Count -eq 0) {
        $StaleLogs = @(Get-ChildItem -LiteralPath $ExpectedFederationDirectory -Filter '*.log' `
            -File -ErrorAction SilentlyContinue)
        if ($StaleLogs.Count -gt 0) {
            throw "Go RTID produced no new event log; only stale generations exist for '$Federation'."
        }
        throw "Go RTID did not persist an event log for '$Federation'."
    }
    if ($NewEventLogs.Count -ne 1) {
        throw "Go RTID produced $($NewEventLogs.Count) new event logs; expected exactly one."
    }

    $EventLogFile = $NewEventLogs[0]
    if ($EventLogFile.Length -le 64) {
        throw "Go event log '$($EventLogFile.FullName)' contains no event records."
    }
    $EventLogHeader = Get-KdrtiV2Header $EventLogFile.FullName
    if ($EventLogHeader.Federation -ne $Federation) {
        throw "Go event-log federation '$($EventLogHeader.Federation)' is wrong for '$Federation'."
    }
    if ([uint64]$EventLogHeader.Generation -lt 1) {
        throw "Go event log '$($EventLogFile.FullName)' has an invalid generation."
    }
    $ExpectedEventLogPath = [IO.Path]::GetFullPath((Join-Path `
        $ExpectedFederationDirectory ('{0:x16}.log' -f [uint64]$EventLogHeader.Generation)))
    if (-not [string]::Equals(
        $EventLogFile.FullName, $ExpectedEventLogPath,
        [StringComparison]::OrdinalIgnoreCase)) {
        throw "Go event log is not at its exact generation-qualified path: '$ExpectedEventLogPath'."
    }
    if ($EventLogPathsBefore.ContainsKey($EventLogFile.FullName.ToLowerInvariant())) {
        throw "Go event log '$($EventLogFile.FullName)' is stale."
    }
    $EventLogDescriptor = [ordered]@{
        path = $EventLogFile.FullName
        header = [ordered]@{
            format = 'kdrti/v2'
            federation = $EventLogHeader.Federation
            generation = [uint64]$EventLogHeader.Generation
        }
        bytes = [long]$EventLogFile.Length
        sha256 = Get-FairSha256 $EventLogFile.FullName
    }
}
foreach ($ServerLogPath in @($ServerStdoutPath, $ServerStderrPath)) {
    if (-not (Test-Path -LiteralPath $ServerLogPath -PathType Leaf)) {
        throw "RTID server log was not created: '$ServerLogPath'."
    }
}
$Benchmark = Get-Content -LiteralPath (Join-Path $OutputDirectory 'benchmark.json') -Raw |
    ConvertFrom-Json
$Provenance = [ordered]@{
    commit = [string]$Benchmark.metadata.provenance.commit
    binary_sha256 = Get-FairSha256 $RtidPath
    runtime_versions = [ordered]@{
        go = ((& $GoExecutable version) -join ' ').Trim()
        rtid = ((& $RtidPath --version 2>&1) -join ' ').Trim()
    }
    build_flags = @('-trimpath')
    exact_argv = @($Launcher) + @($LauncherArguments)
    server_process = $ServerProcessEvidence
    server_logs = [ordered]@{
        stdout = [IO.Path]::GetFullPath($ServerStdoutPath)
        stderr = [IO.Path]::GetFullPath($ServerStderrPath)
    }
    environment = [ordered]@{
        adapter = 'go.ps1'
        rtid_path = $RtidPath
        go = $GoExecutable
        server_argv = @($RtidPath) + @($ServerArguments)
        server_lifecycle = $ServerLifecycle
        server_event_log = $ServerEventLog
        server_log_dir_argument_present = $true
        server_log_dir_argument = $LogDirectoryArgument
        server_log_dir_value = $(if ($ServerEventLog -eq 'file') { $EventLogDirectory } else { '' })
        server_log_sink = $(if ($ServerEventLog -eq 'file') { 'file' } else { 'discard' })
        canonical_fom_path = $Fom
        canonical_fom_sha256 = $Inputs.FomSha256
        client_binary_sha256 = [string]$Benchmark.metadata.environment.client_binary_sha256
    }
    notes = "Adapter records RTID process/log evidence and generated Go client hashes, uses $ServerLifecycle RTID lifecycle with $LogDirectoryArgument, and invokes gorti-go-fair with -NoStartRtid."
}
if ($null -ne $EventLogDescriptor) {
    $Provenance['event_log'] = $EventLogDescriptor
}
Write-FairJson (Join-Path $OutputDirectory 'adapter-provenance.json') $Provenance
Invoke-FairConverter $Python $AdapterRoot $OutputDirectory $WorkloadContract $Fom 'go' $RunId
if ($null -ne $EventLogDescriptor) {
    $SealedEventLog = Get-Item -LiteralPath $EventLogDescriptor.path -ErrorAction Stop
    if ([long]$SealedEventLog.Length -ne [long]$EventLogDescriptor.bytes -or
        (Get-FairSha256 $SealedEventLog.FullName) -ne $EventLogDescriptor.sha256) {
        throw "Go event log changed after its descriptor was sealed."
    }
}

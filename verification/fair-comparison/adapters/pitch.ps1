[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Fom,
    [Parameter(Mandatory = $true)][string]$Seed,
    [Parameter(Mandatory = $true)][int]$Count,
    [Parameter(Mandatory = $true)][ValidateSet('off', 'file')][string]$ServerEventLog,
    [Parameter(Mandatory = $true)][string]$OutputDirectory,
    [Parameter(Mandatory = $true)][string]$RunId,
    [Parameter(Mandatory = $true)][string]$WorkloadContract,
    [string]$PRTIHome = $env:PRTI1516E_HOME,
    [string]$Python = $env:GORTI_FAIR_PYTHON,
    [string]$CrcAddress = '',
    [ValidateRange(1000, 600000)][int]$TimeoutMs = 120000
)

$ErrorActionPreference = 'Stop'
$AdapterRoot = $PSScriptRoot
. (Join-Path $AdapterRoot 'Common.ps1')

function Assert-PitchArtifact([object]$Descriptor, [string]$Name) {
    if ($null -eq $Descriptor -or [string]::IsNullOrWhiteSpace([string]$Descriptor.path) -or
        -not [IO.Path]::IsPathRooted([string]$Descriptor.path)) {
        throw "$Name does not identify an absolute captured artifact."
    }
    $Path = [IO.Path]::GetFullPath([string]$Descriptor.path)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "$Name artifact is missing: '$Path'."
    }
    $File = Get-Item -LiteralPath $Path
    if ([long]$Descriptor.bytes -ne [long]$File.Length) {
        throw "$Name byte count does not match '$Path'."
    }
    $ActualSha256 = Get-FairSha256 $Path
    if ([string]$Descriptor.sha256 -cne $ActualSha256) {
        throw "$Name SHA-256 does not match '$Path'."
    }
}

function Assert-PitchProcess(
        [object]$Evidence,
        [string]$Name,
        [string]$ExpectedExecutable,
        [string[]]$RequiredArguments,
        [string]$ExpectedLifecycle = 'per_arm') {
    if ($null -eq $Evidence -or $Evidence.lifecycle -ne $ExpectedLifecycle -or
        [int]$Evidence.pid -lt 1 -or
        [string]::IsNullOrWhiteSpace([string]$Evidence.started_at)) {
        throw "$Name process evidence is incomplete."
    }
    $Executable = [IO.Path]::GetFullPath([string]$Evidence.executable)
    $ExpectedExecutable = [IO.Path]::GetFullPath($ExpectedExecutable)
    if (-not [string]::Equals(
        $Executable, $ExpectedExecutable, [StringComparison]::OrdinalIgnoreCase)) {
        throw "$Name executable '$Executable' does not match '$ExpectedExecutable'."
    }
    if (-not (Test-Path -LiteralPath $Executable -PathType Leaf) -or
        [string]$Evidence.executable_sha256 -cne (Get-FairSha256 $Executable)) {
        throw "$Name executable SHA-256 is absent or stale."
    }
    $Argv = @($Evidence.argv | ForEach-Object { [string]$_ })
    if ($Argv.Count -lt 1 -or -not [string]::Equals(
        $Argv[0], $Executable, [StringComparison]::OrdinalIgnoreCase)) {
        throw "$Name argv does not begin with its attested executable."
    }
    foreach ($RequiredArgument in $RequiredArguments) {
        if ($Argv -cnotcontains $RequiredArgument) {
            throw "$Name argv is missing required argument '$RequiredArgument'."
        }
    }
}

function Assert-PitchArgumentValue(
        [object]$Evidence, [string]$Name, [string]$Option, [string]$ExpectedValue) {
    $Argv = @($Evidence.argv | ForEach-Object { [string]$_ })
    $Indexes = @()
    for ($Index = 0; $Index -lt $Argv.Count; $Index++) {
        if ($Argv[$Index] -ceq $Option) { $Indexes += $Index }
    }
    if ($Indexes.Count -ne 1 -or $Indexes[0] + 1 -ge $Argv.Count -or
        $Argv[$Indexes[0] + 1] -cne $ExpectedValue) {
        throw "$Name argv does not bind $Option to '$ExpectedValue'."
    }
}
$RepoRoot = (Resolve-Path (Join-Path $AdapterRoot '..\..\..')).Path
$Inputs = Assert-FairAdapterInputs `
    $Fom $Seed $Count $ServerEventLog $WorkloadContract $RepoRoot
$Fom = $Inputs.Fom
$WorkloadContract = $Inputs.WorkloadContract
if ([string]::IsNullOrWhiteSpace($PRTIHome)) {
    $PRTIHome = 'C:\Program Files\prti1516e'
}
$PRTIHome = [IO.Path]::GetFullPath($PRTIHome)
$CrcJar = Join-Path $PRTIHome 'lib\prtifull.jar'
if (-not (Test-Path -LiteralPath $CrcJar -PathType Leaf)) {
    throw "Pitch CRC binary was not found at '$CrcJar'."
}
$CrcJar = (Resolve-Path -LiteralPath $CrcJar).Path
if ([string]::IsNullOrWhiteSpace($Python)) { $Python = 'python' }
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null
$ExternalCrcPid = 0
if (-not [string]::IsNullOrWhiteSpace($env:GORTI_FAIR_PITCH_CRC_PID)) {
    if (-not [int]::TryParse($env:GORTI_FAIR_PITCH_CRC_PID, [ref]$ExternalCrcPid) -or
        $ExternalCrcPid -lt 1) {
        throw 'GORTI_FAIR_PITCH_CRC_PID must be a positive process id.'
    }
}
$ExternalEventLogDirectory = $env:GORTI_FAIR_PITCH_EVENT_LOG_DIR
if ($ExternalCrcPid -gt 0 -and [string]::IsNullOrWhiteSpace($CrcAddress)) {
    $CrcAddress = $env:GORTI_FAIR_PITCH_CRC_ADDRESS
    if ([string]::IsNullOrWhiteSpace($CrcAddress)) { $CrcAddress = '127.0.0.1:8989' }
}
if ([string]::IsNullOrWhiteSpace($CrcAddress)) {
    $PortProbe = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
    $PortProbe.Start()
    $Port = ([Net.IPEndPoint]$PortProbe.LocalEndpoint).Port
    $PortProbe.Stop()
    $CrcAddress = "127.0.0.1:$Port"
}

$Launcher = Join-Path $RepoRoot 'verification\pitch\Run.ps1'
$LauncherArguments = @(
    '-Fom', $Fom,
    '-Seed', '1516',
    '-Count', $Count.ToString([Globalization.CultureInfo]::InvariantCulture),
    '-ServerEventLog', $ServerEventLog,
    '-OutputDirectory', $OutputDirectory,
    '-PRTIHome', $PRTIHome,
    '-CrcAddress', $CrcAddress,
    '-TimeoutMs', $TimeoutMs.ToString([Globalization.CultureInfo]::InvariantCulture)
)
$LauncherParameters = @{
    Fom = $Fom
    Seed = '1516'
    Count = $Count
    ServerEventLog = $ServerEventLog
    OutputDirectory = $OutputDirectory
    PRTIHome = $PRTIHome
    CrcAddress = $CrcAddress
    TimeoutMs = $TimeoutMs
}
if ($ExternalCrcPid -gt 0) {
    if ([string]::IsNullOrWhiteSpace($ExternalEventLogDirectory)) {
        throw 'GORTI_FAIR_PITCH_EVENT_LOG_DIR is required for a persistent Pitch CRC.'
    }
    $ExternalEventLogDirectory = (Resolve-Path -LiteralPath $ExternalEventLogDirectory).Path
    $LauncherArguments += @(
        '-NoStartCrc', '-ExternalCrcPid', [string]$ExternalCrcPid,
        '-ExternalEventLogDirectory', $ExternalEventLogDirectory)
    $LauncherParameters.NoStartCrc = $true
    $LauncherParameters.ExternalCrcPid = $ExternalCrcPid
    $LauncherParameters.ExternalEventLogDirectory = $ExternalEventLogDirectory
}
& $Launcher @LauncherParameters

$Benchmark = Get-Content -LiteralPath (Join-Path $OutputDirectory 'benchmark.json') -Raw |
    ConvertFrom-Json
$EvidencePath = Join-Path $OutputDirectory 'run-evidence.json'
if (-not (Test-Path -LiteralPath $EvidencePath -PathType Leaf)) {
    throw "Pitch launch is unattested: '$EvidencePath' was not produced."
}
$Evidence = Get-Content -LiteralPath $EvidencePath -Raw | ConvertFrom-Json
if ($Evidence.schema -ne 'gorti.pitch/run-evidence-v1' -or $Evidence.status -ne 'attested') {
    $Reason = [string]$Evidence.reason
    throw "Pitch launch is unattested. $Reason"
}
$RecordedEvidence = $Benchmark.metadata.provenance
if ([string]$RecordedEvidence.run_evidence_sha256 -cne (Get-FairSha256 $EvidencePath)) {
    throw 'Pitch run-evidence SHA-256 differs from the benchmark attestation.'
}
$VerifierJar = (Resolve-Path -LiteralPath (
    Join-Path $RepoRoot 'verification\pitch\build\pitch-verifier.jar')).Path
$ApiJar = (Resolve-Path -LiteralPath (Join-Path $PRTIHome 'lib\prti1516e.jar')).Path
$PitchJava = (Resolve-Path -LiteralPath (Join-Path $PRTIHome 'jre\bin\java.exe')).Path
$VerifierJava = [IO.Path]::GetFullPath([string]$Evidence.client_processes.publisher.executable)
if ($Evidence.server_process.lifecycle -eq 'persistent_session') {
    $PersistentExecutable = [IO.Path]::GetFullPath([string]$Evidence.server_process.executable)
    Assert-PitchProcess $Evidence.server_process 'Pitch CRC' `
        $PersistentExecutable @() 'persistent_session'
} else {
    Assert-PitchProcess $Evidence.server_process 'Pitch CRC' `
        $PitchJava @('-jar', $CrcJar, '-nogui')
}
Assert-PitchProcess $Evidence.client_processes.publisher 'Pitch publisher verifier' `
    $VerifierJava @('-cp', "$VerifierJar;$ApiJar", 'gorti.verification.pitch.PitchVerifier',
        '--role', 'publisher')
Assert-PitchProcess $Evidence.client_processes.subscriber 'Pitch subscriber verifier' `
    $VerifierJava @('-cp', "$VerifierJar;$ApiJar", 'gorti.verification.pitch.PitchVerifier',
        '--role', 'subscriber')
if ($Evidence.server_process.lifecycle -eq 'per_arm') {
    Assert-PitchArgumentValue $Evidence.server_process 'Pitch CRC' '-jar' $CrcJar
}
Assert-PitchArgumentValue $Evidence.client_processes.publisher `
    'Pitch publisher verifier' '-cp' "$VerifierJar;$ApiJar"
Assert-PitchArgumentValue $Evidence.client_processes.publisher `
    'Pitch publisher verifier' '--role' 'publisher'
Assert-PitchArgumentValue $Evidence.client_processes.subscriber `
    'Pitch subscriber verifier' '-cp' "$VerifierJar;$ApiJar"
Assert-PitchArgumentValue $Evidence.client_processes.subscriber `
    'Pitch subscriber verifier' '--role' 'subscriber'
Assert-PitchArtifact $Evidence.runtime_artifacts.crc_jar 'Pitch CRC JAR'
Assert-PitchArtifact $Evidence.runtime_artifacts.pitch_api_jar 'Pitch API JAR'
Assert-PitchArtifact $Evidence.runtime_artifacts.verifier_jar 'Pitch verifier JAR'
if (-not [string]::Equals(
        [string]$Evidence.runtime_artifacts.crc_jar.path,
        $CrcJar, [StringComparison]::OrdinalIgnoreCase) -or
    -not [string]::Equals(
        [string]$Evidence.runtime_artifacts.pitch_api_jar.path,
        $ApiJar, [StringComparison]::OrdinalIgnoreCase) -or
    -not [string]::Equals(
        [string]$Evidence.runtime_artifacts.verifier_jar.path,
        $VerifierJar, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Pitch runtime artifact paths differ from the launched classpath or CRC command.'
}
foreach ($Stream in @('stdout', 'stderr')) {
    Assert-PitchArtifact $Evidence.server_logs.$Stream "Pitch CRC $Stream"
    Assert-PitchArtifact $Evidence.client_logs.publisher.$Stream "Pitch publisher $Stream"
    Assert-PitchArtifact $Evidence.client_logs.subscriber.$Stream "Pitch subscriber $Stream"
}
$Provenance = [ordered]@{
    commit = [string]$Benchmark.metadata.provenance.commit
    binary_sha256 = Get-FairSha256 $CrcJar
    runtime_versions = $Benchmark.metadata.provenance.runtime_versions
    build_flags = @($Benchmark.metadata.provenance.build_flags)
    exact_argv = @($Launcher) + @($LauncherArguments)
    server_process = $Evidence.server_process
    server_logs = [ordered]@{
        stdout = [string]$Evidence.server_logs.stdout.path
        stderr = [string]$Evidence.server_logs.stderr.path
    }
    environment = [ordered]@{
        adapter = 'pitch.ps1'
        prti_home = $PRTIHome
        crc_event_log = $ServerEventLog
        crc_address = $CrcAddress
        canonical_fom_path = $Fom
        canonical_fom_sha256 = $Inputs.FomSha256
        launch_attestation = 'attested'
        run_evidence_path = [IO.Path]::GetFullPath($EvidencePath)
        run_evidence_sha256 = Get-FairSha256 $EvidencePath
        runtime_artifacts = $Evidence.runtime_artifacts
        client_processes = $Evidence.client_processes
        client_logs = $Evidence.client_logs
        server_log_artifacts = $Evidence.server_logs
    }
    notes = $(if ($Evidence.server_process.lifecycle -eq 'persistent_session') {
        'Adapter attests a session-persistent Pitch CRC and seals the event-log bytes appended by this arm.'
    } elseif ($ServerEventLog -eq 'file') {
        'Adapter owns an isolated file-logging Pitch CRC and seals its command and process logs.'
    } else {
        'Adapter owns an isolated Pitch CRC, seals its command and process logs, and verifies its event-log directory is empty.'
    })
}
Write-FairJson (Join-Path $OutputDirectory 'adapter-provenance.json') $Provenance
Invoke-FairConverter `
    $Python $AdapterRoot $OutputDirectory $WorkloadContract $Fom 'pitch' $RunId

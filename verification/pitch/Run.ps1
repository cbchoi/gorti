[CmdletBinding()]
param(
    [ValidateSet('1516')]
    [string]$Seed = '1516',
    [ValidateRange(1, 1000000)]
    [int]$Count = 100,
    [string]$CrcAddress = 'localhost:8989',
    [string]$FederationName = '',
    [string]$OutputDirectory = '',
    [Parameter(Mandatory = $true)]
    [string]$Fom,
    [string]$PRTIHome = $env:PRTI1516E_HOME,
    [ValidateRange(1000, 600000)]
    [int]$TimeoutMs = 30000,
    [ValidateSet('off', 'file')]
    [string]$ServerEventLog = 'off',
    [switch]$NoStartCrc,
    [ValidateRange(0, 2147483647)]
    [int]$ExternalCrcPid = 0,
    [string]$ExternalEventLogDirectory = ''
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
$StartedAt = [DateTime]::UtcNow.ToString('o')

# Some managed hosts inject both Path and PATH. Windows PowerShell's
# Start-Process treats those as duplicate dictionary keys unless normalized.
$ProcessPath = [Environment]::GetEnvironmentVariable(
    'Path', [EnvironmentVariableTarget]::Process)
[Environment]::SetEnvironmentVariable(
    'PATH', $null, [EnvironmentVariableTarget]::Process)
[Environment]::SetEnvironmentVariable(
    'Path', $ProcessPath, [EnvironmentVariableTarget]::Process)

if ([string]::IsNullOrWhiteSpace($PRTIHome)) {
    $PRTIHome = 'C:\Program Files\prti1516e'
}
$PRTIHome = [System.IO.Path]::GetFullPath($PRTIHome)
if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $OutputDirectory = Join-Path $Root 'logs'
}
if ([string]::IsNullOrWhiteSpace($FederationName)) {
    $FederationName = "GortiPitchVerifier-$PID"
}

$OutputDirectory = [System.IO.Path]::GetFullPath($OutputDirectory)
if ([string]::IsNullOrWhiteSpace($Fom)) {
    throw '-Fom must name the caller-supplied shared FOM module.'
}
$Fom = [System.IO.Path]::GetFullPath($Fom)
if (-not (Test-Path -LiteralPath $Fom -PathType Leaf)) {
    throw "Verification FOM not found at '$Fom'."
}
$VerifierJar = Join-Path $Root 'build\pitch-verifier.jar'
$ApiJar = Join-Path $PRTIHome 'lib\prti1516e.jar'
$NativePath = Join-Path $PRTIHome 'lib'
$PitchJava = Join-Path $PRTIHome 'jre\bin\java.exe'
$CrcJar = Join-Path $PRTIHome 'lib\prtifull.jar'
$Java = [System.IO.Path]::GetFullPath((Get-Command java -ErrorAction Stop).Source)

& (Join-Path $Root 'Build.ps1') -PRTIHome $PRTIHome
foreach ($RequiredRuntimeFile in @($VerifierJar, $ApiJar, $PitchJava, $CrcJar, $Java)) {
    if (-not (Test-Path -LiteralPath $RequiredRuntimeFile -PathType Leaf)) {
        throw "Pitch runtime evidence cannot be established; file not found: '$RequiredRuntimeFile'."
    }
}
$VerifierJar = (Resolve-Path -LiteralPath $VerifierJar).Path
$ApiJar = (Resolve-Path -LiteralPath $ApiJar).Path
$PitchJava = (Resolve-Path -LiteralPath $PitchJava).Path
$CrcJar = (Resolve-Path -LiteralPath $CrcJar).Path
New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null
foreach ($Name in @(
    'publisher-semantic.ndjson', 'subscriber-semantic.ndjson',
    'publisher-metrics.ndjson', 'subscriber-metrics.ndjson',
    'publisher-samples.ndjson', 'subscriber-samples.ndjson',
    'canonical.ndjson', 'metrics.ndjson', 'benchmark.json', 'run-evidence.json',
    'publisher.stdout.log', 'publisher.stderr.log',
    'subscriber.stdout.log', 'subscriber.stderr.log',
    'crc.stdout.log', 'crc.stderr.log')) {
    $Path = Join-Path $OutputDirectory $Name
    if (Test-Path -LiteralPath $Path) {
        Remove-Item -LiteralPath $Path -Force
    }
}

$HostName = $CrcAddress
$Port = 8989
if ($CrcAddress -match '^([^:]+):(\d+)$') {
    $HostName = $Matches[1]
    $Port = [int]$Matches[2]
}

function Write-JavaSettings([string]$Source, [string]$Destination,
        [hashtable]$Overrides) {
    if (-not (Test-Path -LiteralPath $Source -PathType Leaf)) {
        throw "Pitch settings template not found at '$Source'."
    }
    $Lines = New-Object 'System.Collections.Generic.List[string]'
    Get-Content -LiteralPath $Source | ForEach-Object { [void]$Lines.Add($_) }
    foreach ($Key in @($Overrides.Keys | Sort-Object)) {
        $Replacement = "$Key=$($Overrides[$Key])"
        $Found = $false
        for ($Index = 0; $Index -lt $Lines.Count; $Index++) {
            if ($Lines[$Index] -match ('^\s*' + [regex]::Escape([string]$Key) + '\s*=')) {
                $Lines[$Index] = $Replacement
                $Found = $true
            }
        }
        if (-not $Found) {
            [void]$Lines.Add($Replacement)
        }
    }
    $Parent = Split-Path -Parent $Destination
    New-Item -ItemType Directory -Path $Parent -Force | Out-Null
    $Utf8NoBomForSettings = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllLines($Destination, [string[]]$Lines, $Utf8NoBomForSettings)
}

function Get-JavaSetting([string]$Path, [string]$Key) {
    $Match = @(Get-Content -LiteralPath $Path | Where-Object {
        $_ -match ('^\s*' + [regex]::Escape($Key) + '\s*=')
    })
    if ($Match.Count -ne 1) {
        throw "Expected exactly one '$Key' setting in '$Path'."
    }
    return ($Match[0] -split '=', 2)[1]
}

$InstalledCrcSettings = Join-Path $PRTIHome 'prti1516eCRC.settings'
$InstalledLrcSettings = Join-Path $PRTIHome 'user.home\prti1516e\prti1516eLRC.settings'
$PitchHome = Join-Path $OutputDirectory "pitch-home-$PID"
$PitchSettingsDirectory = Join-Path $PitchHome 'prti1516e'
$PitchEventLogDirectory = Join-Path $PitchHome 'crc-eventlogs'
$HomeCrcSettings = Join-Path $PitchSettingsDirectory 'prti1516eCRC.settings'
$WorkingCrcSettings = Join-Path $PitchHome 'prti1516eCRC.settings'
$HomeLrcSettings = Join-Path $PitchSettingsDirectory 'prti1516eLRC.settings'
New-Item -ItemType Directory -Path $PitchEventLogDirectory -Force | Out-Null
$EventLogEnabled = if ($ServerEventLog -eq 'file') { 'true' } else { 'false' }
$CrcOverrides = @{
    'CRC.eventLog.enable' = $EventLogEnabled
    'CRC.eventLog.directory' = 'crc-eventlogs'
    'CRC.port' = [string]$Port
}
$LrcOverrides = @{
    'crcAddress' = ($HostName + '\:' + $Port)
}
Write-JavaSettings $InstalledCrcSettings $HomeCrcSettings $CrcOverrides
Write-JavaSettings $InstalledCrcSettings $WorkingCrcSettings $CrcOverrides
Write-JavaSettings $InstalledLrcSettings $HomeLrcSettings $LrcOverrides
if ((Get-JavaSetting $HomeCrcSettings 'CRC.eventLog.enable') -ne $EventLogEnabled -or
    (Get-JavaSetting $WorkingCrcSettings 'CRC.eventLog.enable') -ne $EventLogEnabled) {
    throw "Unable to verify CRC.eventLog.enable=$EventLogEnabled in the isolated Pitch settings."
}
$EffectiveTcpBundling = Get-JavaSetting $HomeLrcSettings 'LRC.TCP.enableBundling'
$EffectiveUdpBundling = Get-JavaSetting $HomeLrcSettings 'LRC.UDP.enableBundling'

function Test-CrcPort {
    try {
        $Client = New-Object System.Net.Sockets.TcpClient
        $Async = $Client.BeginConnect($HostName, $Port, $null, $null)
        if (-not $Async.AsyncWaitHandle.WaitOne(250)) {
            $Client.Close()
            return $false
        }
        $Client.EndConnect($Async)
        $Client.Close()
        return $true
    } catch {
        return $false
    }
}

function Quote-Argument([string]$Value) {
    return '"' + $Value.Replace('"', '\"') + '"'
}

function Get-Sha256([string]$Path) {
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function New-ArtifactDescriptor([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Required launch artifact was not captured: '$Path'."
    }
    $File = Get-Item -LiteralPath $Path
    return [pscustomobject][ordered]@{
        path = $File.FullName
        bytes = [long]$File.Length
        sha256 = Get-Sha256 $File.FullName
    }
}

function Copy-FileSegment(
        [string]$Source, [string]$Destination, [long]$StartOffset = 0) {
    $Input = [IO.File]::Open(
        $Source, [IO.FileMode]::Open, [IO.FileAccess]::Read,
        [IO.FileShare]::ReadWrite -bor [IO.FileShare]::Delete)
    try {
        if ($StartOffset -lt 0 -or $StartOffset -gt $Input.Length) {
            $StartOffset = 0
        }
        [void]$Input.Seek($StartOffset, [IO.SeekOrigin]::Begin)
        $Output = [IO.File]::Open(
            $Destination, [IO.FileMode]::Create, [IO.FileAccess]::Write,
            [IO.FileShare]::Read)
        try {
            $Input.CopyTo($Output)
            $Output.Flush($true)
        } finally {
            $Output.Dispose()
        }
    } finally {
        $Input.Dispose()
    }
}

function Get-SharedFileLength([string]$Path) {
    $Stream = [IO.File]::Open(
        $Path, [IO.FileMode]::Open, [IO.FileAccess]::Read,
        [IO.FileShare]::ReadWrite -bor [IO.FileShare]::Delete)
    try {
        return [long]$Stream.Length
    } finally {
        $Stream.Dispose()
    }
}

function New-ProcessEvidence(
        [System.Diagnostics.Process]$Process,
        [string]$ExpectedExecutable,
        [string[]]$Arguments,
        [ValidateSet('per_arm', 'persistent_session')][string]$Lifecycle = 'per_arm') {
    $Process.Refresh()
    $ExpectedExecutable = [IO.Path]::GetFullPath($ExpectedExecutable)
    $ObservedPath = $Process.Path
    $ObservedExecutable = if ([string]::IsNullOrWhiteSpace($ObservedPath)) {
        $ExpectedExecutable
    } else {
        [IO.Path]::GetFullPath($ObservedPath)
    }
    if (-not [string]::Equals(
        $ObservedExecutable, $ExpectedExecutable, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Launched executable '$ObservedExecutable' does not match '$ExpectedExecutable'."
    }
    return [pscustomobject][ordered]@{
        lifecycle = $Lifecycle
        pid = [int]$Process.Id
        started_at = $Process.StartTime.ToUniversalTime().ToString(
            'o', [Globalization.CultureInfo]::InvariantCulture)
        executable = $ObservedExecutable
        executable_sha256 = Get-Sha256 $ObservedExecutable
        argv = @($ObservedExecutable) + @($Arguments)
    }
}

function Wait-ForExit([System.Diagnostics.Process[]]$Processes, [int]$LimitMs) {
    $Deadline = [DateTime]::UtcNow.AddMilliseconds($LimitMs)
    foreach ($Process in $Processes) {
        while (-not $Process.HasExited) {
            if ([DateTime]::UtcNow -ge $Deadline) {
                foreach ($Candidate in $Processes) {
                    if (-not $Candidate.HasExited) {
                        Stop-Process -Id $Candidate.Id -Force
                    }
                }
                throw "Federates exceeded the $LimitMs ms process timeout."
            }
            Start-Sleep -Milliseconds 100
            $Process.Refresh()
        }
        $Process.WaitForExit()
        $Process.Refresh()
    }
}

$CrcProcess = $null
$ServerProcessEvidence = $null
$ClientProcessEvidence = [ordered]@{}
$UnattestedReason = $null
$ExternalEventLogFile = $null
$ExternalEventLogLength = 0L
$Federates = @()
try {
    if ($NoStartCrc -and -not (Test-CrcPort)) {
        throw "Pitch CRC is not reachable at $CrcAddress and -NoStartCrc was supplied."
    }
    if ($NoStartCrc -and $ServerEventLog -eq 'off') {
        throw '-NoStartCrc cannot verify logging=off; the fair Pitch arm must own its isolated CRC.'
    }
    if ($NoStartCrc -and $ExternalCrcPid -gt 0) {
        $ExternalProcess = Get-Process -Id $ExternalCrcPid -ErrorAction Stop
        $ExternalExecutable = [IO.Path]::GetFullPath($ExternalProcess.Path)
        $ServerProcessEvidence = New-ProcessEvidence `
            $ExternalProcess $ExternalExecutable @() 'persistent_session'
        if ([string]::IsNullOrWhiteSpace($ExternalEventLogDirectory)) {
            throw '-ExternalEventLogDirectory is required with -ExternalCrcPid.'
        }
        $ExternalEventLogDirectory = (Resolve-Path -LiteralPath $ExternalEventLogDirectory).Path
        $ExternalEventLogFile = @(Get-ChildItem -LiteralPath $ExternalEventLogDirectory `
            -Filter 'CRC-*.log' -File | Sort-Object LastWriteTimeUtc -Descending |
            Select-Object -First 1)
        if ($ExternalEventLogFile.Count -gt 0) {
            $ExternalEventLogFile = $ExternalEventLogFile[0]
            $ExternalEventLogLength = Get-SharedFileLength $ExternalEventLogFile.FullName
        } else {
            $ExternalEventLogFile = $null
        }
    }
    if ((Test-CrcPort) -and $ServerEventLog -eq 'off') {
        throw "A CRC is already reachable at $CrcAddress; choose a free port so logging=off can be verified."
    }
    if (-not (Test-CrcPort)) {
        if (-not (Test-Path -LiteralPath $PitchJava -PathType Leaf) -or
            -not (Test-Path -LiteralPath $CrcJar -PathType Leaf)) {
            throw "Pitch CRC launcher files were not found under '$PRTIHome'."
        }
        $CrcArguments = @(
            '-splash:', '-Xmx512m', ("-Duser.home=" + $PitchHome),
            '-jar', $CrcJar, '-nogui'
        )
        $CrcLaunchArguments = @(
            '-splash:', '-Xmx512m', (Quote-Argument ("-Duser.home=" + $PitchHome)),
            '-jar', (Quote-Argument $CrcJar), '-nogui'
        )
        $CrcProcess = Start-Process -FilePath $PitchJava -ArgumentList $CrcLaunchArguments `
          -WorkingDirectory $PitchHome -WindowStyle Hidden -PassThru `
          -RedirectStandardOutput (Join-Path $OutputDirectory 'crc.stdout.log') `
          -RedirectStandardError (Join-Path $OutputDirectory 'crc.stderr.log')
        $ServerProcessEvidence = New-ProcessEvidence $CrcProcess $PitchJava $CrcArguments

        $CrcDeadline = [DateTime]::UtcNow.AddSeconds(20)
        while (-not (Test-CrcPort)) {
            if ($CrcProcess.HasExited) {
                throw "Pitch CRC exited with code $($CrcProcess.ExitCode)."
            }
            if ([DateTime]::UtcNow -ge $CrcDeadline) {
                throw "Pitch CRC did not open $CrcAddress within 20 seconds."
            }
            Start-Sleep -Milliseconds 200
            $CrcProcess.Refresh()
        }
    } elseif ($null -eq $ServerProcessEvidence) {
        $UnattestedReason = "CRC at $CrcAddress was not launched by this run."
    }

    $ClassPath = "$VerifierJar;$ApiJar"
    $CommonArguments = @(
        ("-Djava.library.path=" + $NativePath),
        ("-Duser.home=" + $PitchHome),
        '-cp', $ClassPath,
        'gorti.verification.pitch.PitchVerifier',
        '--seed', $Seed,
        '--count', $Count.ToString(),
        '--crc', $CrcAddress,
        '--federation', $FederationName,
        '--fom', $Fom,
        '--output', $OutputDirectory,
        '--timeout-ms', $TimeoutMs.ToString()
    )
    $Common = @(
        (Quote-Argument ("-Djava.library.path=" + $NativePath)),
        (Quote-Argument ("-Duser.home=" + $PitchHome)),
        '-cp', (Quote-Argument $ClassPath),
        'gorti.verification.pitch.PitchVerifier',
        '--seed', $Seed,
        '--count', $Count.ToString(),
        '--crc', $CrcAddress,
        '--federation', $FederationName,
        '--fom', (Quote-Argument $Fom),
        '--output', (Quote-Argument $OutputDirectory),
        '--timeout-ms', $TimeoutMs.ToString()
    )

    $Subscriber = Start-Process -FilePath $Java -ArgumentList (@($Common) + @('--role', 'subscriber')) `
        -WorkingDirectory $Root -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput (Join-Path $OutputDirectory 'subscriber.stdout.log') `
        -RedirectStandardError (Join-Path $OutputDirectory 'subscriber.stderr.log')
    $Federates += $Subscriber
    $ClientProcessEvidence.subscriber = New-ProcessEvidence `
        $Subscriber $Java (@($CommonArguments) + @('--role', 'subscriber'))
    Start-Sleep -Milliseconds 300
    $Publisher = Start-Process -FilePath $Java -ArgumentList (@($Common) + @('--role', 'publisher')) `
        -WorkingDirectory $Root -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput (Join-Path $OutputDirectory 'publisher.stdout.log') `
        -RedirectStandardError (Join-Path $OutputDirectory 'publisher.stderr.log')
    $Federates += $Publisher
    $ClientProcessEvidence.publisher = New-ProcessEvidence `
        $Publisher $Java (@($CommonArguments) + @('--role', 'publisher'))

    Wait-ForExit $Federates ($TimeoutMs * 3)
    foreach ($Process in $Federates) {
        if ($null -ne $Process.ExitCode -and $Process.ExitCode -ne 0) {
            $Role = if ($Process.Id -eq $Subscriber.Id) { 'subscriber' } else { 'publisher' }
            $ErrorLog = Join-Path $OutputDirectory "$Role.stderr.log"
            $Detail = if (Test-Path -LiteralPath $ErrorLog) {
                ([string](Get-Content -LiteralPath $ErrorLog -Raw)).Trim()
            } else { '' }
            throw "$Role exited with code $($Process.ExitCode). $Detail"
        }
    }

    if ($null -ne $CrcProcess -and -not $CrcProcess.HasExited) {
        Stop-Process -Id $CrcProcess.Id -Force
        $CrcProcess.WaitForExit()
        $CrcProcess.Refresh()
    }

    if ($NoStartCrc -and $ExternalCrcPid -gt 0) {
        $LogDeadline = [DateTime]::UtcNow.AddSeconds(10)
        $CapturedExternalLog = $null
        $CapturedExternalOffset = 0L
        do {
            $Latest = @(Get-ChildItem -LiteralPath $ExternalEventLogDirectory `
                -Filter 'CRC-*.log' -File | Sort-Object LastWriteTimeUtc -Descending |
                Select-Object -First 1)
            if ($Latest.Count -gt 0) {
                $Candidate = $Latest[0]
                $SameFile = $null -ne $ExternalEventLogFile -and [string]::Equals(
                    $Candidate.FullName, $ExternalEventLogFile.FullName,
                    [StringComparison]::OrdinalIgnoreCase)
                $Offset = if ($SameFile) { $ExternalEventLogLength } else { 0L }
                $CandidateLength = Get-SharedFileLength $Candidate.FullName
                if ($CandidateLength -gt $Offset) {
                    $CapturedExternalLog = $Candidate
                    $CapturedExternalOffset = $Offset
                    break
                }
            }
            Start-Sleep -Milliseconds 200
        } while ([DateTime]::UtcNow -lt $LogDeadline)
        if ($null -eq $CapturedExternalLog) {
            throw 'Persistent Pitch CRC event log did not grow during the federation run.'
        }
        Copy-FileSegment $CapturedExternalLog.FullName `
            (Join-Path $OutputDirectory 'crc.stdout.log') $CapturedExternalOffset
        $ExternalStderr = Join-Path $ExternalEventLogDirectory 'CRCstderr.log'
        if (Test-Path -LiteralPath $ExternalStderr -PathType Leaf) {
            Copy-FileSegment $ExternalStderr (Join-Path $OutputDirectory 'crc.stderr.log') 0
        } else {
            [IO.File]::WriteAllText(
                (Join-Path $OutputDirectory 'crc.stderr.log'), '',
                (New-Object Text.UTF8Encoding($false)))
        }
    } elseif ($ServerEventLog -eq 'off') {
        $UnexpectedEventLogs = @(Get-ChildItem -LiteralPath $PitchEventLogDirectory -File -Recurse)
        if ($UnexpectedEventLogs.Count -ne 0) {
            throw "Pitch CRC event logging was not disabled; found $($UnexpectedEventLogs.Count) event-log files."
        }
    } else {
        $CapturedEventLogs = @(Get-ChildItem -LiteralPath $PitchEventLogDirectory -File -Recurse)
        if ($CapturedEventLogs.Count -eq 0) {
            throw 'Pitch CRC event logging was requested, but no event-log artifact was captured.'
        }
    }

    $Utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    $SemanticFiles = @(
        (Join-Path $OutputDirectory 'publisher-semantic.ndjson'),
        (Join-Path $OutputDirectory 'subscriber-semantic.ndjson'))
    $MetricFiles = @(
        (Join-Path $OutputDirectory 'publisher-metrics.ndjson'),
        (Join-Path $OutputDirectory 'subscriber-metrics.ndjson'))
    $SampleFiles = @(
        (Join-Path $OutputDirectory 'publisher-samples.ndjson'),
        (Join-Path $OutputDirectory 'subscriber-samples.ndjson'))
    $SemanticLines = @($SemanticFiles | ForEach-Object { Get-Content -LiteralPath $_ })
    $MetricLines = @($MetricFiles | ForEach-Object { Get-Content -LiteralPath $_ })
    $SampleLines = @($SampleFiles | ForEach-Object { Get-Content -LiteralPath $_ })

    foreach ($Line in $SemanticLines) {
        $Record = $Line | ConvertFrom-Json
        $Names = @($Record.PSObject.Properties.Name)
        if (($Names -join ',') -ne 'kind,seq,service,event,actor,data' -or
            $Record.kind -ne 'semantic' -or $Record.service -notin @('FM', 'DM', 'OM', 'TM')) {
            throw "Invalid canonical semantic record: $Line"
        }
    }
    foreach ($Actor in @('publisher', 'subscriber')) {
        $ActorRecords = @($SemanticLines | ForEach-Object { $_ | ConvertFrom-Json } |
            Where-Object { $_.actor -eq $Actor })
        $Last = $ActorRecords[-1]
        if ($Last.event -ne 'phase' -or $Last.data.phase -ne 'reflect' -or
            $Last.data.result -ne 'pass' -or $Last.seq -ne ($ActorRecords.Count - 1)) {
            throw "$Actor did not finish with a contiguous passing semantic log."
        }
    }
    foreach ($Line in $MetricLines) {
        $Record = $Line | ConvertFrom-Json
        $Names = @($Record.PSObject.Properties.Name)
        if (($Names -join ',') -ne 'kind,service,metric,unit,value' -or
            $Record.kind -ne 'metric' -or $Record.service -notin @('FM', 'DM', 'OM', 'TM')) {
            throw "Invalid metric record: $Line"
        }
    }

    $Samples = @($SampleLines | ForEach-Object { $_ | ConvertFrom-Json })
    for ($Index = 0; $Index -lt $Samples.Count; $Index++) {
        $Sample = $Samples[$Index]
        $Names = @($Sample.PSObject.Properties.Name)
        $DimensionNames = @($Sample.dimensions.PSObject.Properties.Name)
        if (($Names -join ',') -ne 'sequence,operation,duration_ns,dimensions' -or
            [string]::IsNullOrWhiteSpace([string]$Sample.operation) -or
            [long]$Sample.duration_ns -lt 0 -or
            ($DimensionNames -join ',') -ne 'sample_kind,service' -or
            $Sample.dimensions.service -notin @('OM', 'TM') -or
            $Sample.dimensions.sample_kind -notin @('call', 'delivery')) {
            throw "Invalid raw benchmark sample: $($SampleLines[$Index])"
        }
        $Sample.sequence = $Index
    }

    $ExpectedSampleCount = $Count * 5
    if ($Samples.Count -ne $ExpectedSampleCount) {
        throw "Expected $ExpectedSampleCount raw samples, received $($Samples.Count)."
    }

    function Get-NearestRank([long[]]$Ordered, [int]$Percentile) {
        $Rank = [Math]::Max(0, [Math]::Ceiling(($Percentile / 100.0) * $Ordered.Count) - 1)
        return [double]$Ordered[[int]$Rank]
    }

    $Summaries = @($Samples |
        Group-Object { "$($_.operation)|$($_.dimensions.service)|$($_.dimensions.sample_kind)" } |
        Sort-Object Name |
        ForEach-Object {
            $First = $_.Group[0]
            [long[]]$Ordered = @($_.Group | ForEach-Object { [long]$_.duration_ns } | Sort-Object)
            $Middle = [int]($Ordered.Count / 2)
            $Median = if (($Ordered.Count % 2) -eq 1) {
                [double]$Ordered[$Middle]
            } else {
                ([double]$Ordered[$Middle - 1] + [double]$Ordered[$Middle]) / 2.0
            }
            [pscustomobject][ordered]@{
                operation = [string]$First.operation
                dimensions = [pscustomobject][ordered]@{
                    sample_kind = [string]$First.dimensions.sample_kind
                    service = [string]$First.dimensions.service
                }
                count = $Ordered.Count
                median_ns = $Median
                p95_ns = Get-NearestRank $Ordered 95
                p99_ns = Get-NearestRank $Ordered 99
            }
        })

    $SemanticRecords = @($SemanticLines | ForEach-Object { $_ | ConvertFrom-Json })
    $Delivered = @($SemanticRecords | Where-Object {
        $_.actor -eq 'subscriber' -and
        $_.event -in @('attributes_reflected', 'interaction_received')
    }).Count
    $ExpectedFanout = $Count * 2
    if ($Delivered -gt $ExpectedFanout) {
        throw "Delivery accounting exceeded expected fanout: $Delivered > $ExpectedFanout."
    }

    $Repo = (Resolve-Path (Join-Path $Root '..\..')).Path
    $Commit = ([string](& git -C $Repo rev-parse HEAD)).Trim()
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($Commit)) { $Commit = 'unknown' }
    $Branch = ([string](& git -C $Repo branch --show-current)).Trim()
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($Branch)) { $Branch = 'unknown' }
    $Dirty = @(& git -C $Repo status --porcelain --untracked-files=normal).Count -gt 0
    $JavaVersion = ((& cmd.exe /d /s /c ('"' + $Java + '" -version 2>&1')) -join ' ').Trim()
    $CrcSha256 = Get-Sha256 $CrcJar
    $ApiSha256 = Get-Sha256 $ApiJar
    $VerifierSha256 = Get-Sha256 $VerifierJar
    $JavaSha256 = Get-Sha256 $Java
    $PitchJavaSha256 = Get-Sha256 $PitchJava
    $FomSha256 = Get-Sha256 $Fom
    $InstalledCrcSettingsSha256 = Get-Sha256 $InstalledCrcSettings
    $InstalledLrcSettingsSha256 = Get-Sha256 $InstalledLrcSettings
    $CrcSettingsSha256 = Get-Sha256 $HomeCrcSettings
    $LrcSettingsSha256 = Get-Sha256 $HomeLrcSettings

    $ClientLogsEvidence = [pscustomobject][ordered]@{
        publisher = [pscustomobject][ordered]@{
            stdout = New-ArtifactDescriptor (Join-Path $OutputDirectory 'publisher.stdout.log')
            stderr = New-ArtifactDescriptor (Join-Path $OutputDirectory 'publisher.stderr.log')
        }
        subscriber = [pscustomobject][ordered]@{
            stdout = New-ArtifactDescriptor (Join-Path $OutputDirectory 'subscriber.stdout.log')
            stderr = New-ArtifactDescriptor (Join-Path $OutputDirectory 'subscriber.stderr.log')
        }
    }
    $ServerLogsEvidence = $null
    if ($null -ne $ServerProcessEvidence) {
        $ServerLogsEvidence = [pscustomobject][ordered]@{
            stdout = New-ArtifactDescriptor (Join-Path $OutputDirectory 'crc.stdout.log')
            stderr = New-ArtifactDescriptor (Join-Path $OutputDirectory 'crc.stderr.log')
        }
    }
    $AttestationStatus = if ($null -ne $ServerProcessEvidence -and
        $null -ne $ServerLogsEvidence -and $ClientProcessEvidence.Count -eq 2) {
        'attested'
    } else {
        'unattested'
    }
    if ($AttestationStatus -eq 'unattested' -and [string]::IsNullOrWhiteSpace($UnattestedReason)) {
        $UnattestedReason = 'Required server or verifier process evidence is absent.'
    }
    $LaunchEvidence = [pscustomobject][ordered]@{
        schema = 'gorti.pitch/run-evidence-v1'
        status = $AttestationStatus
        reason = $(if ($AttestationStatus -eq 'attested') { $null } else { $UnattestedReason })
        server_process = $ServerProcessEvidence
        client_processes = [pscustomobject]$ClientProcessEvidence
        runtime_artifacts = [pscustomobject][ordered]@{
            crc_jar = New-ArtifactDescriptor $CrcJar
            pitch_api_jar = New-ArtifactDescriptor $ApiJar
            verifier_jar = New-ArtifactDescriptor $VerifierJar
        }
        server_logs = $ServerLogsEvidence
        client_logs = $ClientLogsEvidence
    }
    $RunEvidencePath = Join-Path $OutputDirectory 'run-evidence.json'
    [System.IO.File]::WriteAllText($RunEvidencePath,
        (($LaunchEvidence | ConvertTo-Json -Depth 20) + "`n"), $Utf8NoBom)
    $RunEvidenceSha256 = Get-Sha256 $RunEvidencePath
    $HostNameForMetadata = [System.Net.Dns]::GetHostName()
    $Benchmark = [pscustomobject][ordered]@{
        schema = 'gorti.production-benchmark/v1'
        metadata = [pscustomobject][ordered]@{
            run_id = "pitch-java-$Seed-$($StartedAt.Replace(':', '').Replace('-', ''))"
            benchmark = 'pitch-java-tso-lockstep'
            started_at = $StartedAt
            environment = [pscustomobject][ordered]@{
                host = $HostNameForMetadata
                os = [Environment]::OSVersion.VersionString
                architecture = $env:PROCESSOR_ARCHITECTURE
                logical_cpus = [Environment]::ProcessorCount
                branch = $Branch
                dirty = $Dirty
                crc_address = $CrcAddress
                federation = $FederationName
                fom_path = $Fom
                prti_home = [System.IO.Path]::GetFullPath($PRTIHome)
                api_sha256 = $ApiSha256
                verifier_sha256 = $VerifierSha256
                verifier_java_sha256 = $JavaSha256
                crc_java_sha256 = $PitchJavaSha256
                pitch_user_home = $PitchHome
                timeout_ns = [long]$TimeoutMs * 1000000L
                callback = 'HLA_IMMEDIATE'
                app_logging_mode = $ServerEventLog
                verifier_logging_mode = 'file'
                installed_crc_settings_sha256 = $InstalledCrcSettingsSha256
                installed_lrc_settings_sha256 = $InstalledLrcSettingsSha256
                effective_crc_settings_sha256 = $CrcSettingsSha256
                effective_lrc_settings_sha256 = $LrcSettingsSha256
                pitch_settings = [pscustomobject][ordered]@{
                    crc_event_log = ($ServerEventLog -eq 'file')
                    tracing = $false
                    tcp_bundling = [System.Convert]::ToBoolean($EffectiveTcpBundling)
                    udp_bundling = [System.Convert]::ToBoolean($EffectiveUdpBundling)
                }
            }
            workload = [pscustomobject][ordered]@{
                schema = 'gorti.fair-comparison/workload-v1'
                fom_sha256 = $FomSha256
                seed = [long]$Seed
                count = $Count
                two_process = $true
                choreography = 'sequential_update_send_then_tar'
                delivery_boundary = 'subscriber_pre_tar_to_both_callbacks'
                callback = 'immediate'
                server_event_log = $ServerEventLog
            }
            provenance = [pscustomobject][ordered]@{
                commit = $Commit
                binary_sha256 = $CrcSha256
                runtime_versions = [pscustomobject][ordered]@{
                    java = $JavaVersion
                    pitch = 'pRTI IEEE 1516e Java API'
                }
                build_flags = @('--release=8', '-nogui')
                launch_attestation = $LaunchEvidence
                run_evidence_path = [IO.Path]::GetFullPath($RunEvidencePath)
                run_evidence_sha256 = $RunEvidenceSha256
            }
        }
        samples = @($Samples)
        delivery_accounting = [pscustomobject][ordered]@{
            expected_fanout = $ExpectedFanout
            delivered = $Delivered
            explicitly_rejected = 0
            dropped = $ExpectedFanout - $Delivered
            duplicates = 0
            invalid = 0
        }
        summaries = @($Summaries)
    }

    if ($Benchmark.delivery_accounting.dropped -ne 0) {
        throw "Successful run reported $($Benchmark.delivery_accounting.dropped) dropped deliveries."
    }

    [System.IO.File]::WriteAllLines((Join-Path $OutputDirectory 'canonical.ndjson'),
        [string[]]$SemanticLines, $Utf8NoBom)
    [System.IO.File]::WriteAllLines((Join-Path $OutputDirectory 'metrics.ndjson'),
        [string[]]$MetricLines, $Utf8NoBom)
    [System.IO.File]::WriteAllText((Join-Path $OutputDirectory 'benchmark.json'),
        (($Benchmark | ConvertTo-Json -Depth 20) + "`n"), $Utf8NoBom)

    Write-Host "PASS: $Count deterministic exchanges per OM channel."
    Write-Host "Semantic log: $(Join-Path $OutputDirectory 'canonical.ndjson')"
    Write-Host "Performance log: $(Join-Path $OutputDirectory 'metrics.ndjson')"
    Write-Host "Benchmark artifact: $(Join-Path $OutputDirectory 'benchmark.json')"
    Write-Host "Launch evidence: $RunEvidencePath ($AttestationStatus)"
} finally {
    foreach ($Process in $Federates) {
        if ($null -ne $Process -and -not $Process.HasExited) {
            Stop-Process -Id $Process.Id -Force
        }
    }
    if ($null -ne $CrcProcess -and -not $CrcProcess.HasExited) {
        Stop-Process -Id $CrcProcess.Id -Force
    }
}

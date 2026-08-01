[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Fom,
    [Parameter(Mandatory = $true)]
    [ValidateRange(0, [long]::MaxValue)]
    [long]$Seed,
    [Parameter(Mandatory = $true)]
    [ValidateRange(1, 2147483647)]
    [int]$Count,
    [ValidateRange(0, 1000000)]
    [int]$OperationWarmup = 0,
    [string]$Address = '127.0.0.1:8442',
    [string]$Federation = '',
    [string]$OutputDirectory = '',
    [string]$RtidPath = '',
    [string]$Go = '',
    [ValidateRange(1000, 600000)]
    [int]$TimeoutMs = 30000,
    [ValidateSet('off', 'file')]
    [string]$ServerEventLog = 'off',
    [switch]$ReceiveOrder,
    [switch]$LocalLRC,
    [switch]$Confirmed,
    [switch]$AllowGrantBeforeCallbacks,
    [switch]$TMAdvanceOnly,
    [Alias('Config')][string]$TransportConfig = '',
    [ValidateRange(1, 1048576)]
    [int]$LocalLRCQueue = 1024,
    [ValidateRange(1, 4294967295)]
    [long]$LocalLRCAckEvery = 32,
    [ValidateSet(32, 64, 128, 256)]
    [int]$LocalLRCBatchSize = 32,
    [string]$ObjectClass = 'VerifierEntity',
    [string]$InteractionClass = 'VerifierMessage',
    [string]$ObjectName = 'CommercialRtiVerifierEntity',
    [ValidateSet('names', 'handles')]
    [string]$CallbackRepresentation = 'names',
    [ValidateRange(1, 1048576)]
    [int]$OutboxEventCapacity = 8192,
    [ValidateRange(1, 1024)]
    [int]$OutboxBatchSize = 32,
    [ValidateRange(1, 60000)]
    [int]$OutboxFlushIntervalMs = 1,
    [ValidateRange(0, 1024)]
    [int]$RtidGOMAXPROCS = 0,
    [ValidateRange(0, 1024)]
    [int]$FederateGOMAXPROCS = 0,
    [switch]$NoStartRtid
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot = (Resolve-Path (Join-Path $Root '..\..')).Path
$Fom = (Resolve-Path -LiteralPath $Fom).Path
if (-not [string]::IsNullOrWhiteSpace($TransportConfig)) {
    $TransportConfig = (Resolve-Path -LiteralPath $TransportConfig).Path
}
if (-not (Test-Path -LiteralPath $Fom -PathType Leaf)) {
    throw "Fom must identify the caller-selected FOM XML file."
}
if ([string]::IsNullOrWhiteSpace($Federation)) {
    $Federation = "GortiGoFair-$PID"
}
if ($LocalLRC -and -not $ReceiveOrder) {
    throw '-LocalLRC requires -ReceiveOrder.'
}
if ($Confirmed -and -not $ReceiveOrder) {
    throw '-Confirmed requires -ReceiveOrder.'
}
if ($LocalLRC -and $Confirmed) {
    throw '-LocalLRC and -Confirmed are mutually exclusive.'
}
if ($ReceiveOrder -and -not $Confirmed) {
    $LocalLRC = $true
}
if ($OperationWarmup -gt 0 -and -not $ReceiveOrder) {
    throw '-OperationWarmup requires -ReceiveOrder.'
}
if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $OutputDirectory = Join-Path $Root 'logs'
}
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null

# Start-Process rejects managed environments that inject both Path and PATH.
$ProcessPath = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::Process)
[Environment]::SetEnvironmentVariable('PATH', $null, [EnvironmentVariableTarget]::Process)
[Environment]::SetEnvironmentVariable('Path', $ProcessPath, [EnvironmentVariableTarget]::Process)

$GoExecutable = if ($Go) {
    (Get-Command $Go -ErrorAction Stop).Source
} else {
    (Get-Command go -ErrorAction Stop).Source
}
$ClientPath = Join-Path $OutputDirectory 'gorti-go-fair.exe'
$LocalRtidPath = Join-Path $OutputDirectory 'rtid.exe'
if ([string]::IsNullOrWhiteSpace($env:GOCACHE)) {
    $env:GOCACHE = Join-Path $OutputDirectory 'go-cache'
}
if ([string]::IsNullOrWhiteSpace($env:GOMODCACHE)) {
    $env:GOMODCACHE = Join-Path $RepoRoot '.tools\go-mod'
}
New-Item -ItemType Directory -Force -Path $env:GOCACHE, $env:GOMODCACHE | Out-Null

& $GoExecutable build -trimpath -o $ClientPath ./verification/gorti-go-fair
if ($LASTEXITCODE -ne 0) {
    throw 'Unable to build the gorti fair-comparison client.'
}

if (-not $NoStartRtid) {
    if ([string]::IsNullOrWhiteSpace($RtidPath)) {
        & $GoExecutable build -trimpath -o $LocalRtidPath ./rti/cmd/rtid
        if ($LASTEXITCODE -ne 0) {
            throw 'Unable to build rtid from the current source tree.'
        }
        $RtidPath = $LocalRtidPath
    }
    $RtidPath = (Resolve-Path -LiteralPath $RtidPath).Path
}

foreach ($Name in @(
    'publisher-semantic.ndjson', 'subscriber-semantic.ndjson',
    'publisher-metrics.ndjson', 'subscriber-metrics.ndjson',
    'publisher-samples.ndjson', 'subscriber-samples.ndjson',
    'canonical.ndjson', 'metrics.ndjson', 'samples.ndjson',
    'projected-canonical.ndjson', 'benchmark.json',
    'publisher.stdout.log', 'publisher.stderr.log',
    'subscriber.stdout.log', 'subscriber.stderr.log',
    'rtid.stdout.log', 'rtid.stderr.log')) {
    $Path = Join-Path $OutputDirectory $Name
    if (Test-Path -LiteralPath $Path) {
        Remove-Item -LiteralPath $Path -Force
    }
}

$AddressUri = [Uri]("tcp://{0}" -f $Address)
if (-not $AddressUri.Host -or $AddressUri.Port -lt 1) {
    throw 'Address must be a host:port.'
}

function Test-RtidPort {
    try {
        $Client = [Net.Sockets.TcpClient]::new()
        $Async = $Client.BeginConnect($AddressUri.Host, $AddressUri.Port, $null, $null)
        if (-not $Async.AsyncWaitHandle.WaitOne(250)) {
            $Client.Dispose()
            return $false
        }
        $Client.EndConnect($Async)
        $Client.Dispose()
        return $true
    } catch {
        return $false
    }
}

function Quote-Argument([string]$Value) {
    return '"' + $Value.Replace('"', '\"') + '"'
}

function Wait-Federates([System.Diagnostics.Process[]]$Processes, [int]$LimitMs) {
    $Deadline = [DateTime]::UtcNow.AddMilliseconds($LimitMs)
    while (@($Processes | Where-Object { -not $_.HasExited }).Count -gt 0) {
        if ([DateTime]::UtcNow -ge $Deadline) {
            foreach ($Process in $Processes) {
                if (-not $Process.HasExited) { Stop-Process -Id $Process.Id -Force }
            }
            throw "Federates exceeded the $LimitMs ms process timeout."
        }
        Start-Sleep -Milliseconds 100
        foreach ($Process in $Processes) { $Process.Refresh() }
    }
    foreach ($Process in $Processes) { $Process.WaitForExit(); $Process.Refresh() }
}

$StartedAt = [DateTime]::UtcNow.ToString('o')
$OriginalGOMAXPROCS = $env:GOMAXPROCS
$RtidProcess = $null
$Federates = @()
try {
    if (-not (Test-RtidPort)) {
        if ($NoStartRtid) {
            throw "rtid is not reachable at $Address and -NoStartRtid was supplied."
        }
        $SaveDirectory = Join-Path $OutputDirectory 'rtid-saves'
        New-Item -ItemType Directory -Force -Path $SaveDirectory | Out-Null
        $EventLogDirectory = Join-Path $OutputDirectory 'rtid-eventlogs'
        $LogDirectoryArgument = if ($ServerEventLog -eq 'file') {
            New-Item -ItemType Directory -Force -Path $EventLogDirectory | Out-Null
            "--log-dir=$EventLogDirectory"
        } else { '--log-dir=' }
        $AuditReplayPluginArgument = if ($ServerEventLog -eq 'file') {
            '--audit-replay-plugin=event-journal'
        } else { '--audit-replay-plugin=none' }
        $ServerArguments = @(
            "--listen=$Address", '--metrics-listen=127.0.0.1:0', '--admin-listen=',
            "--save-dir=$SaveDirectory", $AuditReplayPluginArgument, $LogDirectoryArgument, '--log-format=text',
            "--outbox-event-capacity=$OutboxEventCapacity",
            "--outbox-batch-size=$OutboxBatchSize",
            "--outbox-flush-interval=$($OutboxFlushIntervalMs)ms"
        )
        if ($RtidGOMAXPROCS -gt 0) { $env:GOMAXPROCS = [string]$RtidGOMAXPROCS }
        else { Remove-Item Env:GOMAXPROCS -ErrorAction SilentlyContinue }
        $RtidProcess = Start-Process -FilePath $RtidPath -ArgumentList $ServerArguments `
            -WorkingDirectory $RepoRoot -WindowStyle Hidden -PassThru `
            -RedirectStandardOutput (Join-Path $OutputDirectory 'rtid.stdout.log') `
            -RedirectStandardError (Join-Path $OutputDirectory 'rtid.stderr.log')
        if ($null -eq $OriginalGOMAXPROCS) { Remove-Item Env:GOMAXPROCS -ErrorAction SilentlyContinue }
        else { $env:GOMAXPROCS = $OriginalGOMAXPROCS }
        $Deadline = [DateTime]::UtcNow.AddSeconds(20)
        while (-not (Test-RtidPort)) {
            if ($RtidProcess.HasExited) { throw "rtid exited with code $($RtidProcess.ExitCode)." }
            if ([DateTime]::UtcNow -ge $Deadline) { throw "rtid did not open $Address within 20 seconds." }
            Start-Sleep -Milliseconds 100
            $RtidProcess.Refresh()
        }
    }

    $Common = @(
        "--address=$Address", "--federation=$Federation",
        ('--fom=' + (Quote-Argument $Fom)), "--seed=$Seed", "--count=$Count",
        "--operation-warmup=$OperationWarmup",
        ('--output=' + (Quote-Argument $OutputDirectory)), "--timeout=$($TimeoutMs)ms",
        "--object-class=$ObjectClass", "--interaction-class=$InteractionClass",
        "--object-name=$ObjectName",
        "--callback-representation=$CallbackRepresentation"
    )
    if ($ReceiveOrder) { $Common += '--receive-order=true' }
    if ($AllowGrantBeforeCallbacks) { $Common += '--allow-grant-before-callbacks=true' }
    if ($TMAdvanceOnly) { $Common += '--tm-advance-only=true' }
    if (-not [string]::IsNullOrWhiteSpace($TransportConfig)) {
        $Common += ('--transport-config=' + (Quote-Argument $TransportConfig))
    }
    if ($Confirmed) { $Common += '--confirmed=true' }
    if ($LocalLRC) {
        $Common += '--local-lrc=true'
        $Common += "--local-lrc-queue=$LocalLRCQueue"
        $Common += "--local-lrc-ack-every=$LocalLRCAckEvery"
        $Common += "--local-lrc-batch-size=$LocalLRCBatchSize"
    }
    if ($FederateGOMAXPROCS -gt 0) { $env:GOMAXPROCS = [string]$FederateGOMAXPROCS }
    else { Remove-Item Env:GOMAXPROCS -ErrorAction SilentlyContinue }
    $Subscriber = Start-Process -FilePath $ClientPath -ArgumentList (@($Common) + '--role=subscriber') `
        -WorkingDirectory $RepoRoot -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput (Join-Path $OutputDirectory 'subscriber.stdout.log') `
        -RedirectStandardError (Join-Path $OutputDirectory 'subscriber.stderr.log')
    $Federates += $Subscriber
    $Publisher = Start-Process -FilePath $ClientPath -ArgumentList (@($Common) + '--role=publisher') `
        -WorkingDirectory $RepoRoot -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput (Join-Path $OutputDirectory 'publisher.stdout.log') `
        -RedirectStandardError (Join-Path $OutputDirectory 'publisher.stderr.log')
    if ($null -eq $OriginalGOMAXPROCS) { Remove-Item Env:GOMAXPROCS -ErrorAction SilentlyContinue }
    else { $env:GOMAXPROCS = $OriginalGOMAXPROCS }
    $Federates += $Publisher

    Wait-Federates $Federates ($TimeoutMs * 3)
    foreach ($Process in $Federates) {
        if ($null -ne $Process.ExitCode -and $Process.ExitCode -ne 0) {
            $Role = if ($Process.Id -eq $Subscriber.Id) { 'subscriber' } else { 'publisher' }
            $DetailText = Get-Content -LiteralPath (Join-Path $OutputDirectory "$Role.stderr.log") -Raw
            $Detail = if ($null -eq $DetailText) { '' } else { ([string]$DetailText).Trim() }
            throw "$Role exited with code $($Process.ExitCode). $Detail"
        }
    }

    $SemanticFiles = @('publisher-semantic.ndjson', 'subscriber-semantic.ndjson') |
        ForEach-Object { Join-Path $OutputDirectory $_ }
    $MetricFiles = @('publisher-metrics.ndjson', 'subscriber-metrics.ndjson') |
        ForEach-Object { Join-Path $OutputDirectory $_ }
    $SampleFiles = @('publisher-samples.ndjson', 'subscriber-samples.ndjson') |
        ForEach-Object { Join-Path $OutputDirectory $_ }
    $SemanticLines = @($SemanticFiles | ForEach-Object { Get-Content -LiteralPath $_ })
    $MetricLines = @($MetricFiles | ForEach-Object { Get-Content -LiteralPath $_ })
    $SampleLines = @($SampleFiles | ForEach-Object { Get-Content -LiteralPath $_ })

    $SemanticRecords = @($SemanticLines | ForEach-Object { $_ | ConvertFrom-Json })
    foreach ($Actor in @('publisher', 'subscriber')) {
        $ActorRecords = @($SemanticRecords | Where-Object { $_.actor -eq $Actor })
        for ($Index = 0; $Index -lt $ActorRecords.Count; $Index++) {
            if ($ActorRecords[$Index].kind -ne 'semantic' -or
                [long]$ActorRecords[$Index].seq -ne $Index) {
                throw "$Actor semantic sequence is not canonical and contiguous."
            }
        }
        $Last = $ActorRecords[-1]
        if ($Last.event -ne 'phase' -or $Last.data.phase -ne 'reflect' -or $Last.data.result -ne 'pass') {
            throw "$Actor did not finish with a passing reflect record."
        }
        $ExpectedSyncLabels = @('VERIFY_READY')
        if ($OperationWarmup -gt 0) { $ExpectedSyncLabels += 'VERIFY_MEASURE' }
        $ExpectedSyncLabels += 'VERIFY_DONE'
        foreach ($Label in $ExpectedSyncLabels) {
            $Sync = @($ActorRecords | Where-Object {
                $_.event -eq 'federation_synchronized' -and $_.data.label -eq $Label
            })
            if ($Sync.Count -ne 1) { throw "$Actor did not synchronize exactly once at $Label." }
        }
        $Grants = @($ActorRecords | Where-Object { $_.event -eq 'time_advance_granted' } |
            ForEach-Object { [int]$_.data.logical_time } | Sort-Object)
        $ExpectedGrants = if ($ReceiveOrder) {
            @()
        } elseif ($TMAdvanceOnly) {
            @(1..$Count)
        } else {
            @(1..($Count + 1))
        }
        if (($Grants -join ',') -ne ($ExpectedGrants -join ',')) {
            throw "$Actor grants differ: $($Grants -join ',')."
        }
    }

    $Samples = @($SampleLines | ForEach-Object { $_ | ConvertFrom-Json })
    $ExpectedSampleTotal = if ($LocalLRC) {
        (2 * $Count) + 2
    } elseif ($ReceiveOrder) {
        (2 * $Count) + 1
    } elseif ($TMAdvanceOnly) {
        4 * $Count
    } else {
        7 * $Count
    }
    if ($Samples.Count -ne $ExpectedSampleTotal) {
        throw "Expected $ExpectedSampleTotal raw samples, received $($Samples.Count)."
    }
    $AllowedOperations = if ($LocalLRC) {
        @('queueAttributeValues', 'queueInteraction', 'flushLocalLRC', 'completedReceiveOrderBatch')
    } elseif ($ReceiveOrder) {
        @('updateAttributeValues', 'sendInteraction', 'completedReceiveOrderBatch')
    } elseif ($TMAdvanceOnly) {
        @('timeAdvanceRequest', 'timeAdvanceGrantLatency')
    } else {
        @('updateAttributeValues', 'sendInteraction', 'timeAdvanceRequest',
            'timeAdvanceGrantLatency', 'completed_delivery_batch_latency')
    }
    for ($Index = 0; $Index -lt $Samples.Count; $Index++) {
        $Sample = $Samples[$Index]
        $ValidBoundary = if ($Sample.operation -eq 'timeAdvanceGrantLatency') {
            $Sample.dimensions.boundary -eq 'grant'
        } else {
            $Sample.dimensions.sample_kind -in @('call', 'delivery')
        }
        if ([long]$Sample.duration_ns -lt 0 -or
            $Sample.operation -notin $AllowedOperations -or -not $ValidBoundary) {
            throw "Invalid raw sample: $($SampleLines[$Index])"
        }
        $Sample.sequence = $Index
    }

    $ExpectedSampleCounts = [ordered]@{}
    if ($LocalLRC) {
        $ExpectedSampleCounts['queueAttributeValues|OM|call'] = $Count
        $ExpectedSampleCounts['queueInteraction|OM|call'] = $Count
        $ExpectedSampleCounts['flushLocalLRC|OM|call'] = 1
        $ExpectedSampleCounts['completedReceiveOrderBatch|OM|delivery'] = 1
    } elseif ($ReceiveOrder) {
        $ExpectedSampleCounts['updateAttributeValues|OM|call'] = $Count
        $ExpectedSampleCounts['sendInteraction|OM|call'] = $Count
        $ExpectedSampleCounts['completedReceiveOrderBatch|OM|delivery'] = 1
    } elseif ($TMAdvanceOnly) {
        $ExpectedSampleCounts['timeAdvanceRequest|TM|call'] = 2 * $Count
        $ExpectedSampleCounts['timeAdvanceGrantLatency|TM|grant'] = 2 * $Count
    } else {
        $ExpectedSampleCounts['updateAttributeValues|OM|call'] = $Count
        $ExpectedSampleCounts['sendInteraction|OM|call'] = $Count
        $ExpectedSampleCounts['timeAdvanceRequest|TM|call'] = 2 * $Count
        $ExpectedSampleCounts['timeAdvanceGrantLatency|TM|grant'] = 2 * $Count
        $ExpectedSampleCounts['completed_delivery_batch_latency|OM|delivery'] = $Count
    }
    $ActualSampleCounts = @{}
    foreach ($Sample in $Samples) {
        $SampleBoundary = if ($Sample.operation -eq 'timeAdvanceGrantLatency') {
            $Sample.dimensions.boundary
        } else {
            $Sample.dimensions.sample_kind
        }
        $Key = '{0}|{1}|{2}' -f $Sample.operation, $Sample.dimensions.service, $SampleBoundary
        if (-not $ActualSampleCounts.ContainsKey($Key)) { $ActualSampleCounts[$Key] = 0 }
        $ActualSampleCounts[$Key]++
    }
    if ($ActualSampleCounts.Count -ne $ExpectedSampleCounts.Count) {
        throw "Raw sample metric identities differ: $($ActualSampleCounts.Keys -join ', ')."
    }
    foreach ($Key in $ExpectedSampleCounts.Keys) {
        if (-not $ActualSampleCounts.ContainsKey($Key) -or
            $ActualSampleCounts[$Key] -ne $ExpectedSampleCounts[$Key]) {
            throw "Raw sample count for $Key is $($ActualSampleCounts[$Key]); expected $($ExpectedSampleCounts[$Key])."
        }
    }

    function Get-NearestRank([long[]]$Ordered, [int]$Percentile) {
        $Rank = [Math]::Max(0, [Math]::Ceiling(($Percentile / 100.0) * $Ordered.Count) - 1)
        return [double]$Ordered[[int]$Rank]
    }
    $Summaries = @($Samples |
        Group-Object { "$($_.operation)|$($_.dimensions.service)|$($_.dimensions.sample_kind)" } |
        Sort-Object Name | ForEach-Object {
            $First = $_.Group[0]
            [long[]]$Ordered = @($_.Group | ForEach-Object { [long]$_.duration_ns } | Sort-Object)
            $Middle = [int]($Ordered.Count / 2)
            $Median = if (($Ordered.Count % 2) -eq 1) { [double]$Ordered[$Middle] } else {
                ([double]$Ordered[$Middle - 1] + [double]$Ordered[$Middle]) / 2.0
            }
            [pscustomobject][ordered]@{
                operation = [string]$First.operation
                dimensions = [pscustomobject][ordered]@{
                    sample_kind = [string]$First.dimensions.sample_kind
                    service = [string]$First.dimensions.service
                }
                count = $Ordered.Count; median_ns = $Median
                p95_ns = Get-NearestRank $Ordered 95; p99_ns = Get-NearestRank $Ordered 99
            }
        })

    $Delivered = @($SemanticRecords | Where-Object {
        $_.actor -eq 'subscriber' -and $_.event -in @('attributes_reflected', 'interaction_received')
    }).Count
    $ExpectedFanout = if ($TMAdvanceOnly) { 0 } else { $Count * 2 }
    if ($Delivered -ne $ExpectedFanout) { throw "Delivered $Delivered of $ExpectedFanout expected callbacks." }

    $Commit = ([string](& git -C $RepoRoot rev-parse HEAD)).Trim()
    if ($LASTEXITCODE -ne 0) { $Commit = 'unknown' }
    $Branch = ([string](& git -C $RepoRoot branch --show-current)).Trim()
    if ($LASTEXITCODE -ne 0) { $Branch = 'unknown' }
    $Dirty = @(& git -C $RepoRoot -c core.excludesFile= status --porcelain --untracked-files=normal).Count -gt 0
    $RtidSha = if ($RtidPath -and (Test-Path -LiteralPath $RtidPath)) {
        (Get-FileHash -LiteralPath $RtidPath -Algorithm SHA256).Hash.ToLowerInvariant()
    } else { 'external' }
    $ClientSha = (Get-FileHash -LiteralPath $ClientPath -Algorithm SHA256).Hash.ToLowerInvariant()
    $BenchmarkName = if ($LocalLRC) { 'gorti-local-lrc-receive-order' } elseif ($ReceiveOrder) { 'gorti-go-fair-receive-order' } elseif ($TMAdvanceOnly) { 'gorti-go-fair-tm-advance-only' } else { 'gorti-go-fair-tso-lockstep' }
    $Lookahead = if ($ReceiveOrder) { $null } else { 1 }
    $Choreography = if ($LocalLRC) { 'sequential_local_admission_update_send_then_flush' } elseif ($ReceiveOrder) { 'sequential_receive_order_update_send' } elseif ($TMAdvanceOnly) { 'sequential_tar_tag_without_om_traffic' } else { 'sequential_update_send_then_tar' }
    $DeliveryBoundary = if ($ReceiveOrder -and $OperationWarmup -gt 0) {
        'subscriber_after_measure_sync_to_final_validated_callback'
    } elseif ($ReceiveOrder) {
        'subscriber_ready_to_final_callback'
    } else { 'subscriber_pre_tar_to_both_callbacks' }
    $Benchmark = [pscustomobject][ordered]@{
        schema = 'gorti.production-benchmark/v1'
        metadata = [pscustomobject][ordered]@{
            run_id = "gorti-go-fair-$Seed-$($StartedAt.Replace(':', '').Replace('-', ''))"
            benchmark = $BenchmarkName; started_at = $StartedAt
            environment = [pscustomobject][ordered]@{
                host = [Net.Dns]::GetHostName(); os = [Environment]::OSVersion.VersionString
                architecture = $env:PROCESSOR_ARCHITECTURE; logical_cpus = [Environment]::ProcessorCount
				branch = $Branch; dirty = $Dirty; address = $Address; federation = $Federation; fom_path = $Fom
				client_binary_sha256 = $ClientSha
            }
            workload = [pscustomobject][ordered]@{
                schema = 'gorti.fair-comparison/workload-v1'
                count = $Count; operation_warmup = $OperationWarmup
                seed = [long]$Seed; timeout_ns = [long]$TimeoutMs * 1000000L
                lookahead = $Lookahead; fom_sha256 = (Get-FileHash -LiteralPath $Fom -Algorithm SHA256).Hash.ToLowerInvariant()
                object_class = $ObjectClass; interaction_class = $InteractionClass
                object_name = $ObjectName
                payload_encoding = 'HLAinteger32BE+HLAASCIIstring'; expected_fanout = $ExpectedFanout
                two_process = $true; choreography = $Choreography
                delivery_boundary = $DeliveryBoundary; callback = 'immediate'
                logging_mode = $ServerEventLog; server_event_log = $ServerEventLog
                local_lrc = [bool]$LocalLRC; local_lrc_queue = $LocalLRCQueue
                local_lrc_ack_every = $LocalLRCAckEvery
                rtid_gomaxprocs = $RtidGOMAXPROCS; federate_gomaxprocs = $FederateGOMAXPROCS
            }
            provenance = [pscustomobject][ordered]@{
                commit = $Commit; binary_sha256 = $RtidSha
                runtime_versions = [pscustomobject][ordered]@{ go = (& $GoExecutable version) }
                build_flags = @('-trimpath')
            }
        }
        samples = @($Samples)
        delivery_accounting = [pscustomobject][ordered]@{
            expected_fanout = $ExpectedFanout; delivered = $Delivered
            explicitly_rejected = 0; dropped = 0; duplicates = 0; invalid = 0
        }
        summaries = @($Summaries)
    }

    $Utf8NoBom = [Text.UTF8Encoding]::new($false)
    [IO.File]::WriteAllLines((Join-Path $OutputDirectory 'canonical.ndjson'), [string[]]$SemanticLines, $Utf8NoBom)
    [IO.File]::WriteAllLines((Join-Path $OutputDirectory 'metrics.ndjson'), [string[]]$MetricLines, $Utf8NoBom)
    [IO.File]::WriteAllLines((Join-Path $OutputDirectory 'samples.ndjson'),
        [string[]]($Samples | ForEach-Object { $_ | ConvertTo-Json -Compress -Depth 5 }), $Utf8NoBom)
    [IO.File]::WriteAllText((Join-Path $OutputDirectory 'benchmark.json'),
        (($Benchmark | ConvertTo-Json -Depth 10) + "`n"), $Utf8NoBom)

    $Projection = @(Get-Content -LiteralPath (Join-Path $OutputDirectory 'projected-canonical.ndjson'))
    if ($Projection.Count -ne 4) { throw 'Projected canonical summary must contain exactly four records.' }
    Write-Host "PASS: $Count deterministic exchanges per OM channel."
    Write-Host "Canonical log: $(Join-Path $OutputDirectory 'canonical.ndjson')"
    Write-Host "Projected summary: $(Join-Path $OutputDirectory 'projected-canonical.ndjson')"
    Write-Host "Benchmark artifact: $(Join-Path $OutputDirectory 'benchmark.json')"
} finally {
    if ($null -eq $OriginalGOMAXPROCS) { Remove-Item Env:GOMAXPROCS -ErrorAction SilentlyContinue }
    else { $env:GOMAXPROCS = $OriginalGOMAXPROCS }
    foreach ($Process in $Federates) {
        if ($null -ne $Process -and -not $Process.HasExited) { Stop-Process -Id $Process.Id -Force }
    }
    if ($null -ne $RtidProcess -and -not $RtidProcess.HasExited) {
        Stop-Process -Id $RtidProcess.Id -Force
        $RtidProcess.WaitForExit()
    }
}

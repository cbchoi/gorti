[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$ConfigPath,
    [Parameter(Mandatory = $true)][string]$OutputDirectory,
    [ValidateRange(1, 2147483647)][int]$Count = 100,
    [ValidateSet('off', 'file')][string]$ServerEventLog = 'file',
    [ValidateRange(0, 100)][int]$WarmupPairs = 5,
    [ValidateRange(2, 1000)][int]$MeasuredPairs = 20,
    [ValidateRange(0, 2147483647)][int]$OrderSeed = 1516,
    [ValidateRange(100, 1000000)][int]$BootstrapResamples = 10000,
    [string]$Python = 'python',
    [string]$RtidPath = $env:GORTI_FAIR_RTID_PATH,
    [switch]$ClaimGrade
)

$ErrorActionPreference = 'Stop'
$Root = $PSScriptRoot
$RepoRoot = [IO.Path]::GetFullPath((Join-Path $Root '..\..'))
if ($ClaimGrade -and (
    $WarmupPairs -ne 5 -or $MeasuredPairs -ne 20 -or $ServerEventLog -ne 'file')) {
    throw '-ClaimGrade requires exactly 5 warmup pairs, exactly 20 measured pairs, and ServerEventLog=file.'
}
$ConfigPath = (Resolve-Path -LiteralPath $ConfigPath).Path
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
$RtidPath = (Resolve-Path -LiteralPath $RtidPath).Path
if (Test-Path -LiteralPath $OutputDirectory) {
    if (@(Get-ChildItem -LiteralPath $OutputDirectory -Force).Count -gt 0) {
        throw "OutputDirectory must be absent or empty: '$OutputDirectory'."
    }
}

$ServerDirectory = "$OutputDirectory-go-rtid"
if (Test-Path -LiteralPath $ServerDirectory) {
    if (@(Get-ChildItem -LiteralPath $ServerDirectory -Force).Count -gt 0) {
        throw "Persistent RTID directory must be absent or empty: '$ServerDirectory'."
    }
}
New-Item -ItemType Directory -Path $ServerDirectory -Force | Out-Null
$SaveDirectory = Join-Path $ServerDirectory 'saves'
$EventLogDirectory = Join-Path $ServerDirectory 'eventlogs'
New-Item -ItemType Directory -Path $SaveDirectory -Force | Out-Null
$LogDirectoryArgument = if ($ServerEventLog -eq 'file') {
    New-Item -ItemType Directory -Path $EventLogDirectory -Force | Out-Null
    "--log-dir=$EventLogDirectory"
} else {
    '--log-dir='
}

$PortProbe = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
$PortProbe.Start()
$Port = ([Net.IPEndPoint]$PortProbe.LocalEndpoint).Port
$PortProbe.Stop()
$Address = "127.0.0.1:$Port"
$AddressUri = [Uri]("tcp://{0}" -f $Address)
$ServerArguments = @(
    "--listen=$Address", '--metrics-listen=127.0.0.1:0', '--admin-listen=',
    "--save-dir=$SaveDirectory", $LogDirectoryArgument, '--log-format=text'
)

# Start-Process rejects managed environments that inject both Path and PATH.
$ProcessPath = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::Process)
[Environment]::SetEnvironmentVariable('PATH', $null, [EnvironmentVariableTarget]::Process)
[Environment]::SetEnvironmentVariable('Path', $ProcessPath, [EnvironmentVariableTarget]::Process)

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

$PreviousEnvironment = @{}
foreach ($Name in @(
    'GORTI_FAIR_REUSE_RTID', 'GORTI_FAIR_PERSISTENT_ADDRESS',
    'GORTI_FAIR_PERSISTENT_PID',
    'GORTI_FAIR_SHARED_EVENT_LOG_DIR', 'GORTI_FAIR_SHARED_SAVE_DIR')) {
    $PreviousEnvironment[$Name] = [Environment]::GetEnvironmentVariable(
        $Name, [EnvironmentVariableTarget]::Process)
}
$RtidProcess = $null
try {
    $RtidProcess = Start-Process -FilePath $RtidPath -ArgumentList $ServerArguments `
        -WorkingDirectory $RepoRoot -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput (Join-Path $ServerDirectory 'rtid.stdout.log') `
        -RedirectStandardError (Join-Path $ServerDirectory 'rtid.stderr.log')
    $Deadline = [DateTime]::UtcNow.AddSeconds(20)
    while (-not (Test-RtidPort)) {
        $RtidProcess.Refresh()
        if ($RtidProcess.HasExited) {
            throw "Persistent RTID exited during startup with code $($RtidProcess.ExitCode)."
        }
        if ([DateTime]::UtcNow -ge $Deadline) {
            throw "Persistent RTID did not listen on $Address within 20 seconds."
        }
        Start-Sleep -Milliseconds 100
    }

    $env:GORTI_FAIR_REUSE_RTID = '1'
    $env:GORTI_FAIR_PERSISTENT_ADDRESS = $Address
    $env:GORTI_FAIR_PERSISTENT_PID = [string]$RtidProcess.Id
    $env:GORTI_FAIR_SHARED_EVENT_LOG_DIR = $EventLogDirectory
    $env:GORTI_FAIR_SHARED_SAVE_DIR = $SaveDirectory
    & (Join-Path $Root 'run-comparison.ps1') `
        -ConfigPath $ConfigPath -Count $Count -ServerEventLog $ServerEventLog `
        -OutputDirectory $OutputDirectory -WarmupPairs $WarmupPairs `
        -MeasuredPairs $MeasuredPairs -OrderSeed $OrderSeed `
        -BootstrapResamples $BootstrapResamples -Python $Python `
        -ClaimGrade:$ClaimGrade
    if ($LASTEXITCODE -ne 0) {
        throw "Persistent fair comparison failed with exit code $LASTEXITCODE."
    }
} finally {
    if ($null -ne $RtidProcess -and -not $RtidProcess.HasExited) {
        Stop-Process -Id $RtidProcess.Id -Force
        $RtidProcess.WaitForExit()
    }
    foreach ($Name in $PreviousEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable(
            $Name, $PreviousEnvironment[$Name], [EnvironmentVariableTarget]::Process)
    }
}

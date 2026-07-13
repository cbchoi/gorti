[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Seed,
    [Parameter(Mandatory = $true)][int]$Count,
    [Parameter(Mandatory = $true)][string]$Log,
    [Parameter(Mandatory = $true)][string]$RunDir,
    [string]$Python = ""
)

$ErrorActionPreference = "Stop"
$Repo = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
if (-not $Python) {
    $VenvPython = Join-Path $Repo "pysdk\.venv\Scripts\python.exe"
    $Python = if (Test-Path -LiteralPath $VenvPython) { $VenvPython } else { "python" }
}

$Listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
$Listener.Start()
$Port = ([Net.IPEndPoint]$Listener.LocalEndpoint).Port
$Listener.Stop()

powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot "run.ps1") `
    -Url "grpc://127.0.0.1:$Port" -Seed ([int]$Seed) -Iterations $Count `
    -OutputDirectory $RunDir -Python $Python -TimeoutSeconds 120
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

& $Python (Join-Path $Repo "verification\common\project_semantics.py") `
    --implementation gorti --seed $Seed --count $Count `
    (Join-Path $RunDir "canonical.ndjson") $Log
exit $LASTEXITCODE

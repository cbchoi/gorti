[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Seed,
    [Parameter(Mandatory = $true)][int]$Count,
    [Parameter(Mandatory = $true)][string]$Log,
    [Parameter(Mandatory = $true)][string]$RunDir,
    [Parameter(Mandatory = $true)][string]$Fom,
    [string]$Python = ""
)

$ErrorActionPreference = "Stop"
$Repo = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
if (-not $Python) {
    $VenvPython = Join-Path $Repo "pysdk\.venv\Scripts\python.exe"
    $Python = if (Test-Path -LiteralPath $VenvPython) { $VenvPython } else { "python" }
}

powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot "Run.ps1") `
    -Seed $Seed -Count $Count -OutputDirectory $RunDir -Fom $Fom
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

& $Python (Join-Path $Repo "verification\common\project_semantics.py") `
    --implementation pitch --seed $Seed --count $Count `
    (Join-Path $RunDir "canonical.ndjson") $Log
exit $LASTEXITCODE

[CmdletBinding()]
param(
    [string]$Python = $env:PYTHON
)

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path

if (-not $Python) {
    if (Get-Command python -ErrorAction SilentlyContinue) { $Python = "python" }
    elseif (Get-Command python3 -ErrorAction SilentlyContinue) { $Python = "python3" }
    else { throw "ieee1516e-ambassador-smoke: Python 3.11 or later is required on PATH." }
}
if (-not (Get-Command $Python -ErrorAction SilentlyContinue)) {
    throw "ieee1516e-ambassador-smoke: Python executable not found: $Python"
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "ieee1516e-ambassador-smoke: Go 1.22 or later is required to build rtid."
}

$GeneratedDir = Join-Path $RepoRoot "pysdk\rti1516e\_generated\rti\v1"
$Generated = Get-ChildItem -Path $GeneratedDir -Filter "*_pb2.py" -ErrorAction SilentlyContinue
if (-not $Generated) {
    Write-Host "ieee1516e-ambassador-smoke: generating gRPC bindings"
    & $Python (Join-Path $RepoRoot "pysdk\rti1516e\_proto.py")
    if ($LASTEXITCODE -ne 0) {
        throw "ieee1516e-ambassador-smoke: Python code generation failed; install with: $Python -m pip install -e '$RepoRoot\pysdk[dev]'"
    }
}

$PreviousErrorAction = $ErrorActionPreference
try {
    $ErrorActionPreference = "Continue"
    & $Python -c "import sys; assert sys.version_info >= (3, 11); import grpc, google.protobuf, pytest" 2>$null
    $DependencyStatus = $LASTEXITCODE
}
finally {
    $ErrorActionPreference = $PreviousErrorAction
}
if ($DependencyStatus -ne 0) {
    throw "ieee1516e-ambassador-smoke: install Python requirements with: $Python -m pip install -e '$RepoRoot\pysdk[dev]'"
}

Write-Host "ieee1516e-ambassador-smoke: running live publisher/subscriber verification"
Push-Location $RepoRoot
try {
    & $Python -m pytest -q -s "pysdk/tests/spec/m27/test_ieee1516e_ambassador_smoke_cross.py::test_spec_m27d_ieee1516e_ambassador_cross_federate"
    if ($LASTEXITCODE -ne 0) { throw "ieee1516e-ambassador-smoke: verification failed." }
}
finally {
    Pop-Location
}

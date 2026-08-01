[CmdletBinding()]
param(
    [string]$Python = $env:PYTHON
)

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path

if (-not $Python) {
    if (Get-Command python -ErrorAction SilentlyContinue) { $Python = "python" }
    elseif (Get-Command python3 -ErrorAction SilentlyContinue) { $Python = "python3" }
    else { throw "ieee1516e-typed-smoke: Python 3.11 or later is required on PATH." }
}
if (-not (Get-Command $Python -ErrorAction SilentlyContinue)) {
    throw "ieee1516e-typed-smoke: Python executable not found: $Python"
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "ieee1516e-typed-smoke: Go 1.22 or later is required to build rtid."
}

$GeneratedDir = Join-Path $RepoRoot "pysdk\rti1516e\_generated\rti\v1"
$Generated = Get-ChildItem -Path $GeneratedDir -Filter "*_pb2.py" -ErrorAction SilentlyContinue
if (-not $Generated) {
    Write-Host "ieee1516e-typed-smoke: generating gRPC bindings"
    & $Python (Join-Path $RepoRoot "pysdk\rti1516e\_proto.py")
    if ($LASTEXITCODE -ne 0) {
        throw "ieee1516e-typed-smoke: Python code generation failed; install with: $Python -m pip install -e '$RepoRoot\pysdk[dev]'"
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
    throw "ieee1516e-typed-smoke: install Python requirements with: $Python -m pip install -e '$RepoRoot\pysdk[dev]'"
}

Write-Host "ieee1516e-typed-smoke: running live typed-handle verification"
Push-Location $RepoRoot
try {
    & $Python -m pytest -q -s "pysdk/tests/spec/m28/test_ieee1516e_typed_smoke.py::test_spec_m28_ieee1516e_typed_smoke"
    if ($LASTEXITCODE -ne 0) { throw "ieee1516e-typed-smoke: verification failed." }
}
finally {
    Pop-Location
}

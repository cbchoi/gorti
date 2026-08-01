$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$RunnerArgs = @($args)
$ScriptDir = $PSScriptRoot
$RepoRoot = (Resolve-Path (Join-Path $ScriptDir "..\..")).Path
$Runner = Join-Path $ScriptDir "runner.py"
$DefaultRtid = Join-Path $ScriptDir ".run\bin\rtid.exe"

$PythonPrefix = @()
if ($env:PYTHON) {
    $PythonCommand = Get-Command $env:PYTHON -ErrorAction SilentlyContinue
} else {
    $PythonCommand = Get-Command python -ErrorAction SilentlyContinue
    if (-not $PythonCommand) {
        $PythonCommand = Get-Command py -ErrorAction SilentlyContinue
        if ($PythonCommand) {
            $PythonPrefix = @("-3")
        }
    }
}
if (-not $PythonCommand) {
    [Console]::Error.WriteLine("run.ps1: Python 3.11 or newer was not found.")
    exit 127
}
$PythonExe = $PythonCommand.Source

$OldPythonPath = $env:PYTHONPATH
$env:PYTHONPATH = (Join-Path $RepoRoot "pysdk")
if ($OldPythonPath) {
    $env:PYTHONPATH += [IO.Path]::PathSeparator + $OldPythonPath
}

try {
    if ($RunnerArgs -contains "-h" -or $RunnerArgs -contains "--help") {
        & $PythonExe @PythonPrefix $Runner @RunnerArgs
        exit $LASTEXITCODE
    }

    $DependencyCheck = @'
import sys
try:
    if sys.version_info < (3, 11):
        raise RuntimeError
    import grpc
    from rti1516e._transport import _ensure_generated_path
    _ensure_generated_path()
    from rti.v1 import declaration_pb2, federation_pb2, object_pb2, stream_pb2
except Exception:
    sys.exit(1)
'@
    & $PythonExe @PythonPrefix -c $DependencyCheck
    if ($LASTEXITCODE -ne 0) {
        [Console]::Error.WriteLine(@"
run.ps1: Python dependencies or generated gRPC bindings are unavailable.
From the repository root, run:
  python -m pip install -e './pysdk[dev]'
  python -m rti1516e._proto
"@)
        exit 1
    }

    $RtidTarget = $DefaultRtid
    for ($Index = 0; $Index -lt $RunnerArgs.Count; $Index++) {
        if ($RunnerArgs[$Index] -eq "--rtid-binary" -and $Index + 1 -lt $RunnerArgs.Count) {
            $RtidTarget = $RunnerArgs[$Index + 1]
            $Index++
        } elseif ($RunnerArgs[$Index] -like "--rtid-binary=*") {
            $RtidTarget = $RunnerArgs[$Index].Substring("--rtid-binary=".Length)
        }
    }

    if (-not (Test-Path -LiteralPath $RtidTarget -PathType Leaf) -and
        -not (Get-Command go -ErrorAction SilentlyContinue)) {
        [Console]::Error.WriteLine(@"
run.ps1: rtid was not found at $RtidTarget.
Install Go or pass --rtid-binary PATH to an existing rtid executable.
"@)
        exit 1
    }

    & $PythonExe @PythonPrefix $Runner --rtid-binary $DefaultRtid @RunnerArgs
    exit $LASTEXITCODE
} finally {
    $env:PYTHONPATH = $OldPythonPath
}

$ErrorActionPreference = "Stop"
$RunnerArgs = [Collections.Generic.List[string]]::new()
$ExplicitRtidBinary = $null
for ($i = 0; $i -lt $args.Count; $i++) {
    $arg = [string]$args[$i]
    if ($arg -in @("-RtidBinary", "--rtid-binary")) {
        if ($i + 1 -ge $args.Count) {
            [Console]::Error.WriteLine("pyjevsim-relay-cross-process: $arg requires a path.")
            exit 1
        }
        $ExplicitRtidBinary = [string]$args[++$i]
    } elseif ($arg.StartsWith("--rtid-binary=")) {
        $ExplicitRtidBinary = $arg.Substring("--rtid-binary=".Length)
    } else {
        $RunnerArgs.Add($arg)
    }
}
$Here = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot = [IO.Path]::GetFullPath((Join-Path $Here "..\.."))
$ExampleName = Split-Path -Leaf $Here

function Stop-Example([string] $Message) {
    [Console]::Error.WriteLine("${ExampleName}: $Message")
    exit 1
}

function Find-Python {
    $candidates = [Collections.Generic.List[string]]::new()
    if ($env:PYTHON) { $candidates.Add($env:PYTHON) }
    if ($env:RTID_VENV) {
        $candidates.Add((Join-Path $env:RTID_VENV "Scripts\python.exe"))
        $candidates.Add((Join-Path $env:RTID_VENV "bin\python"))
    }
    $candidates.Add((Join-Path $RepoRoot ".venv\Scripts\python.exe"))
    $candidates.Add((Join-Path $RepoRoot ".venv\bin\python"))
    $candidates.Add((Join-Path $RepoRoot ".m21-venv\Scripts\python.exe"))
    $candidates.Add((Join-Path $RepoRoot ".m21-venv\bin\python"))
    $candidates.Add("python3")
    $candidates.Add("python")

    foreach ($candidate in $candidates) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            return (Resolve-Path -LiteralPath $candidate).Path
        }
        $command = Get-Command $candidate -CommandType Application -ErrorAction SilentlyContinue |
            Select-Object -First 1
        if ($command) { return $command.Source }
    }
    return $null
}

$Python = Find-Python
if (-not $Python) { Stop-Example "Python 3.11 or newer was not found. Set PYTHON or RTID_VENV." }

& $Python -c "import sys; raise SystemExit(0 if sys.version_info >= (3, 11) else 1)"
if ($LASTEXITCODE -ne 0) { Stop-Example "Python 3.11 or newer is required (selected: $Python)." }

$PySdk = Join-Path $RepoRoot "pysdk"
& $Python -c @'
import sys
sys.path.insert(0, sys.argv[1])
try:
    import grpc
    import google.protobuf
    import rti1516e.connection
except (ImportError, RuntimeError) as exc:
    print(f'runtime dependency check failed: {exc}', file=sys.stderr)
    raise SystemExit(1)
'@ $PySdk
if ($LASTEXITCODE -ne 0) {
    Stop-Example "Install the SDK dependencies with: $Python -m pip install -e `"$PySdk`""
}

$RtidArgs = @()
if ($ExplicitRtidBinary) {
    if (-not (Test-Path -LiteralPath $ExplicitRtidBinary -PathType Leaf)) {
        Stop-Example "-RtidBinary does not exist: $ExplicitRtidBinary"
    }
    $RtidArgs = @("--rtid-binary", $ExplicitRtidBinary)
} elseif ($env:RTID_BINARY) {
    if (-not (Test-Path -LiteralPath $env:RTID_BINARY -PathType Leaf)) {
        Stop-Example "RTID_BINARY does not exist: $env:RTID_BINARY"
    }
    $RtidArgs = @("--rtid-binary", $env:RTID_BINARY)
} else {
    $knownBinaries = @(
        (Join-Path $RepoRoot "bin\rtid.exe"),
        (Join-Path $RepoRoot "bin\rtid"),
        (Join-Path $Here ".run\bin\rtid.exe"),
        (Join-Path $Here ".run\bin\rtid")
    )
    $hasRtid = $knownBinaries | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf }
    if (-not $hasRtid -and -not (Get-Command go -CommandType Application -ErrorAction SilentlyContinue)) {
        Stop-Example "rtid was not found and the Go toolchain is unavailable. Set RTID_BINARY or install Go."
    }
}

& $Python (Join-Path $Here "runner.py") @RtidArgs @RunnerArgs
exit $LASTEXITCODE

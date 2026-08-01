[CmdletBinding()]
param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$RunnerArgs
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$Here = $PSScriptRoot
$RepoRoot = (Resolve-Path (Join-Path $Here "..\..")).Path

function Find-Python {
    $candidates = [Collections.Generic.List[string]]::new()
    if ($env:PYTHON) { $candidates.Add($env:PYTHON) }
    if ($env:RTID_VENV) {
        $candidates.Add((Join-Path $env:RTID_VENV "Scripts\python.exe"))
        $candidates.Add((Join-Path $env:RTID_VENV "bin\python"))
    }
    $candidates.Add((Join-Path $RepoRoot ".venv\Scripts\python.exe"))
    $candidates.Add((Join-Path $RepoRoot ".venv\bin\python"))
    $candidates.Add("python3")
    $candidates.Add("python")

    foreach ($candidate in $candidates) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            $executable = (Resolve-Path -LiteralPath $candidate).Path
        } else {
            $command = Get-Command $candidate -CommandType Application `
                -ErrorAction SilentlyContinue | Select-Object -First 1
            if (-not $command) { continue }
            $executable = $command.Source
        }
        & $executable -c "import sys; raise SystemExit(sys.version_info < (3, 11))" `
            *> $null
        if ($LASTEXITCODE -eq 0) {
            return [PSCustomObject]@{ Executable = $executable; Prefix = @() }
        }
    }

    $launcher = Get-Command py -CommandType Application -ErrorAction SilentlyContinue |
        Select-Object -First 1
    if ($launcher) {
        & $launcher.Source -3.11 -c "import sys; raise SystemExit(sys.version_info < (3, 11))" `
            *> $null
        if ($LASTEXITCODE -eq 0) {
            return [PSCustomObject]@{
                Executable = $launcher.Source
                Prefix = @("-3.11")
            }
        }
    }
    return $null
}

$Python = Find-Python
if (-not $Python) {
    [Console]::Error.WriteLine(
        "pyjevsim-real-model: Python 3.11 or newer was not found. Set PYTHON or RTID_VENV."
    )
    exit 1
}
$PythonExe = $Python.Executable
$PythonPrefix = @($Python.Prefix)

if ($RunnerArgs -contains "-h" -or $RunnerArgs -contains "--help") {
    & $PythonExe @PythonPrefix (Join-Path $Here "runner.py") @RunnerArgs
    exit $LASTEXITCODE
}

& $PythonExe @PythonPrefix (Join-Path $Here "preflight.py")
if ($LASTEXITCODE -ne 0) { exit 1 }

& $PythonExe @PythonPrefix (Join-Path $Here "runner.py") @RunnerArgs
exit $LASTEXITCODE

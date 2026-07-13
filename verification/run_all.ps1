[CmdletBinding()]
param(
    [int]$Seed = 1516,
    [ValidateRange(1, 1000000)][int]$Count = 100,
    [ValidateRange(1, 20)][int]$MaxIterations = 3,
    [string]$OutputDirectory = "",
    [string]$Python = ""
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
$Repo = (Resolve-Path (Join-Path $Root "..")).Path
if (-not $Python) {
    $VenvPython = Join-Path $Repo "pysdk\.venv\Scripts\python.exe"
    $Python = if (Test-Path -LiteralPath $VenvPython) { $VenvPython } else { "python" }
}
if (-not $OutputDirectory) {
    $Stamp = [DateTime]::UtcNow.ToString("yyyyMMdd-HHmmss")
    $OutputDirectory = Join-Path $Root "out\ralph-$Seed-$Stamp"
}
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)

$PitchAdapter = Join-Path $Root "pitch\ralph_run.ps1"
$GortiAdapter = Join-Path $Root "gorti\ralph_run.ps1"
$PitchCommandArgs = @(
    "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", $PitchAdapter,
    "-Seed", "{seed}", "-Count", "$Count", "-Log", "{log}",
    "-RunDir", "{run_dir}\pitch-run", "-Python", $Python
)
$GortiCommandArgs = @(
    "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", $GortiAdapter,
    "-Seed", "{seed}", "-Count", "$Count", "-Log", "{log}",
    "-RunDir", "{run_dir}\gorti-run", "-Python", $Python
)

$RalphArgs = @(
    "--max-iterations", "$MaxIterations", "--seed", "$Seed",
    "--output-dir", $OutputDirectory, "--working-dir", $Repo,
    "--timeout", "600", "--review-mode", "semantic",
    "--pitch-command", "powershell.exe"
)
foreach ($Argument in $PitchCommandArgs) {
    $RalphArgs += "--pitch-arg=$Argument"
}
$RalphArgs += @("--gorti-command", "powershell.exe")
foreach ($Argument in $GortiCommandArgs) {
    $RalphArgs += "--gorti-arg=$Argument"
}
& $Python (Join-Path $Root "ralph\ralph.py") @RalphArgs
$RalphExit = $LASTEXITCODE

$SummaryPath = Join-Path $OutputDirectory "summary.json"
if (Test-Path -LiteralPath $SummaryPath) {
    $Summary = Get-Content -LiteralPath $SummaryPath -Raw | ConvertFrom-Json
    $Iteration = "iteration-{0:D3}" -f [int]$Summary.iterations
    $IterationDir = Join-Path $OutputDirectory $Iteration
    $PitchMetrics = Join-Path $IterationDir "pitch-run\metrics.ndjson"
    $GortiMetrics = Join-Path $IterationDir "gorti-run\metrics.ndjson"
    if ((Test-Path -LiteralPath $PitchMetrics) -and (Test-Path -LiteralPath $GortiMetrics)) {
        & $Python (Join-Path $Root "common\compare_performance.py") `
            $PitchMetrics $GortiMetrics --report (Join-Path $OutputDirectory "performance.json")
    }
}

& $Python (Join-Path $Root "check_service_usage.py") `
    --report (Join-Path $OutputDirectory "service-usage.json")
$UsageExit = $LASTEXITCODE

if ($RalphExit -ne 0) { exit $RalphExit }
exit $UsageExit

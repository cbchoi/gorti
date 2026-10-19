param(
    [string]$Output,
    [string]$Go = "go"
)
$ErrorActionPreference = "Stop"
$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
if (-not $Output) {
    $Output = Join-Path (Split-Path -Parent $repo) "benchmark-results/callback-report-builds"
}
if (-not (Get-Command $Go -ErrorAction SilentlyContinue)) {
    $Go = Join-Path $env:ProgramFiles "Go/bin/go.exe"
}
$outputPath = [System.IO.Path]::GetFullPath($Output)
New-Item -ItemType Directory -Force -Path $outputPath | Out-Null
Push-Location $repo
try {
    foreach ($profile in @("sync", "async")) {
        & $Go build -tags "callback_$profile" -o (Join-Path $outputPath "rtid-$profile.exe") ./rti/cmd/rtid
        if ($LASTEXITCODE -ne 0) { throw "rtid $profile build failed" }
        & $Go build -tags "callback_$profile" -o (Join-Path $outputPath "gorti-go-fair-$profile.exe") ./verification/gorti-go-fair
        if ($LASTEXITCODE -ne 0) { throw "federate $profile build failed" }
    }
} finally {
    Pop-Location
}
Write-Output "Built callback profiles in $outputPath"

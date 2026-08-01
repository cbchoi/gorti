[CmdletBinding()]
param(
    [int]$Rounds = 0
)

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path

if ($Rounds -eq 0) {
    $Rounds = if ($env:ROUNDS) { [int]$env:ROUNDS } else { 1000 }
}
if ($Rounds -lt 1) {
    throw "go-pingpong: Rounds must be a positive integer."
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "go-pingpong: Go 1.22 or later is required on PATH."
}

Write-Host "go-pingpong: running $Rounds verified round trips"
Push-Location $RepoRoot
try {
    & go run -buildvcs=false ./examples/go-pingpong --rounds $Rounds
    if ($LASTEXITCODE -ne 0) {
        throw "go-pingpong: verification failed with exit code $LASTEXITCODE."
    }
}
finally {
    Pop-Location
}

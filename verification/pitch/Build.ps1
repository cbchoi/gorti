[CmdletBinding()]
param(
    [string]$PRTIHome = $env:PRTI1516E_HOME
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path

if ([string]::IsNullOrWhiteSpace($PRTIHome)) {
    $PRTIHome = 'C:\Program Files\prti1516e'
}
$ApiJar = Join-Path $PRTIHome 'lib\prti1516e.jar'
if (-not (Test-Path -LiteralPath $ApiJar -PathType Leaf)) {
    throw "Pitch IEEE 1516e Java API not found at '$ApiJar'. Set PRTI1516E_HOME or pass -PRTIHome."
}

$Javac = (Get-Command javac -ErrorAction Stop).Source
$Jar = (Get-Command jar -ErrorAction Stop).Source
$Build = Join-Path $Root 'build'
$Classes = Join-Path $Build 'classes'
$Manifest = Join-Path $Build 'MANIFEST.MF'
$OutputJar = Join-Path $Build 'pitch-verifier.jar'
$Sources = @(Get-ChildItem -LiteralPath (Join-Path $Root 'src') -Recurse -Filter '*.java' |
    ForEach-Object { $_.FullName })

if ($Sources.Count -eq 0) {
    throw 'No Java sources were found.'
}
if (Test-Path -LiteralPath $Classes) {
    Remove-Item -LiteralPath $Classes -Recurse -Force
}
New-Item -ItemType Directory -Path $Classes -Force | Out-Null
New-Item -ItemType Directory -Path $Build -Force | Out-Null

& $Javac --release 8 -encoding UTF-8 -cp $ApiJar -d $Classes @Sources
if ($LASTEXITCODE -ne 0) {
    throw "javac failed with exit code $LASTEXITCODE."
}

$Utf8NoBom = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllLines($Manifest, @(
    'Manifest-Version: 1.0',
    'Main-Class: gorti.verification.pitch.PitchVerifier',
    ''
), $Utf8NoBom)

if (Test-Path -LiteralPath $OutputJar) {
    Remove-Item -LiteralPath $OutputJar -Force
}
& $Jar cfm $OutputJar $Manifest -C $Classes .
if ($LASTEXITCODE -ne 0) {
    throw "jar failed with exit code $LASTEXITCODE."
}

Write-Host "Built $OutputJar"

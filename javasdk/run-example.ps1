[CmdletBinding()]
param(
    [string]$Endpoint = $env:GORTI_ENDPOINT,
    [string]$Fom = $env:GORTI_FOM,
    [string]$Maven = $env:GORTI_MAVEN,
    [string]$MavenRepo = $env:GORTI_MAVEN_REPO,
    [ValidateRange(1, 300)]
    [int]$TimeoutSeconds = 10
)

$ErrorActionPreference = 'Stop'
if ([string]::IsNullOrWhiteSpace($Endpoint)) {
    throw 'Specify -Endpoint grpc://host:port or grpcs://host:port (or GORTI_ENDPOINT). Start the RTI server separately.'
}
if ([string]::IsNullOrWhiteSpace($Maven)) { $Maven = 'mvn' }
if ([string]::IsNullOrWhiteSpace($Fom)) {
    $Fom = Join-Path $PSScriptRoot '../examples/go-tar-wait/tar-wait-fom.xml'
}
if (-not (Test-Path -LiteralPath $Fom -PathType Leaf)) { throw "FOM file not found: $Fom" }
$Fom = (Resolve-Path -LiteralPath $Fom).Path
$Common = @('-B', '-ntp')
if (-not [string]::IsNullOrWhiteSpace($MavenRepo)) {
    $Common += "-Dmaven.repo.local=$([IO.Path]::GetFullPath($MavenRepo))"
}

& $Maven @Common '-f' (Join-Path $PSScriptRoot 'pom.xml') '-DskipTests' 'install'
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

& $Maven @Common '-f' (Join-Path $PSScriptRoot 'client/pom.xml') 'exec:java' `
    '-Dexec.mainClass=org.gorti.examples.TimeFederate' `
    "-Dgorti.endpoint=$Endpoint" "-Dgorti.fom=$Fom" "-Dgorti.timeoutSeconds=$TimeoutSeconds"
exit $LASTEXITCODE

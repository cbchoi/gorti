[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('publisher', 'receiver')]
    [string]$Role,
    [string]$Endpoint = $env:GORTI_ENDPOINT,
    [string]$Maven = $env:GORTI_MAVEN,
    [string]$MavenRepo = $env:GORTI_MAVEN_REPO,
    [ValidateNotNullOrEmpty()]
    [string]$Federation = 'java-exchange',
    [ValidateRange(1, 300)]
    [int]$TimeoutSeconds = 30
)

$ErrorActionPreference = 'Stop'
if ([string]::IsNullOrWhiteSpace($Endpoint)) {
    throw 'Specify -Endpoint grpc://host:port or grpcs://host:port (or GORTI_ENDPOINT). Start the RTI server separately.'
}
if ([string]::IsNullOrWhiteSpace($Maven)) { $Maven = 'mvn' }
$Role = $Role.ToLowerInvariant()
$Fom = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot 'exchange-fom.xml')).Path
$ClientPom = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '../../javasdk/client/pom.xml')).Path
$Common = @('-B', '-ntp')
if (-not [string]::IsNullOrWhiteSpace($MavenRepo)) {
    $Common += "-Dmaven.repo.local=$([IO.Path]::GetFullPath($MavenRepo))"
}

& $Maven @Common '-f' $ClientPom 'exec:java' `
    '-Dexec.mainClass=org.gorti.examples.ExchangeFederate' `
    "-Dgorti.role=$Role" "-Dgorti.endpoint=$Endpoint" "-Dgorti.federation=$Federation" `
    "-Dgorti.fom=$Fom" "-Dgorti.timeoutSeconds=$TimeoutSeconds"
exit $LASTEXITCODE

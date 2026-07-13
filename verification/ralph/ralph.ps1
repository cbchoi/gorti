[CmdletBinding()]
param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]] $RalphArguments
)

$ErrorActionPreference = "Stop"
$runner = Join-Path $PSScriptRoot "ralph.py"
$python = Get-Command python -ErrorAction SilentlyContinue

if ($null -ne $python) {
    & $python.Source $runner @RalphArguments
} else {
    $launcher = Get-Command py -ErrorAction SilentlyContinue
    if ($null -eq $launcher) {
        throw "Python 3 was not found on PATH."
    }
    & $launcher.Source -3 $runner @RalphArguments
}

exit $LASTEXITCODE

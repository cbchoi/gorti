param([Parameter(ValueFromRemainingArguments=$true)][string[]]$Arguments)
$ErrorActionPreference = 'Stop'
python (Join-Path $PSScriptRoot 'run.py') @Arguments
exit $LASTEXITCODE

# Statically validate the cross-platform entrypoints in each example directory.
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$ExamplesDir = Join-Path $RepoRoot 'examples'
$Failures = New-Object 'System.Collections.Generic.List[string]'
$StrictUtf8 = New-Object System.Text.UTF8Encoding($false, $true)

function Add-Failure {
    param([Parameter(Mandatory = $true)][string]$Message)

    $Failures.Add($Message)
    [Console]::Error.WriteLine("ERROR: $Message")
}

function Read-Utf8File {
    param([Parameter(Mandatory = $true)][string]$Path)

    try {
        return $StrictUtf8.GetString([System.IO.File]::ReadAllBytes($Path))
    }
    catch {
        Add-Failure "$(Resolve-Path -Relative $Path) is not valid UTF-8"
        return $null
    }
}

function Get-CodeWithoutLineComments {
    param([Parameter(Mandatory = $true)][string]$Text)

    return (($Text -split "`n") | Where-Object { $_ -notmatch '^\s*#' }) -join "`n"
}

function Test-MachinePath {
    param([Parameter(Mandatory = $true)][string]$Text)

    return $Text -match '(?<![A-Za-z0-9])[A-Za-z]:[\\/]|/(?:home|Users)/\S+'
}

if (-not (Test-Path -LiteralPath $ExamplesDir -PathType Container)) {
    [Console]::Error.WriteLine("ERROR: examples directory not found at $ExamplesDir")
    exit 1
}

$ExampleDirs = @(
    Get-ChildItem -LiteralPath $ExamplesDir -Directory |
        Where-Object { $_.Name -ne '__pycache__' } |
        Sort-Object Name
)
if ($ExampleDirs.Count -eq 0) {
    Add-Failure 'no immediate example directories found under examples/'
}

foreach ($ExampleDir in $ExampleDirs) {
    $ExampleName = $ExampleDir.Name
    $Readme = Join-Path $ExampleDir.FullName 'README.md'
    $ShellEntrypoint = Join-Path $ExampleDir.FullName 'run.sh'
    $PowerShellEntrypoint = Join-Path $ExampleDir.FullName 'run.ps1'

    foreach ($Required in @($Readme, $ShellEntrypoint, $PowerShellEntrypoint)) {
        $Leaf = Split-Path -Leaf $Required
        if (-not (Test-Path -LiteralPath $Required -PathType Leaf)) {
            Add-Failure "examples/$ExampleName/$Leaf is missing"
        }
        elseif ((Get-Item -LiteralPath $Required).Length -eq 0) {
            Add-Failure "examples/$ExampleName/$Leaf is empty"
        }
    }

    if ((Test-Path -LiteralPath $ShellEntrypoint -PathType Leaf) -and
        (Get-Item -LiteralPath $ShellEntrypoint).Length -gt 0) {
        $ShellText = Read-Utf8File $ShellEntrypoint
        if ($null -ne $ShellText) {
            $FirstLine = ($ShellText -split "`n", 2)[0]
            if ($FirstLine -ne '#!/usr/bin/env bash') {
                Add-Failure "examples/$ExampleName/run.sh must start with #!/usr/bin/env bash"
            }
            if ($ShellText.Contains("`r")) {
                Add-Failure "examples/$ExampleName/run.sh must use LF line endings"
            }

            $ShellCode = Get-CodeWithoutLineComments $ShellText
            if ($ShellCode -notmatch 'BASH_SOURCE|\$0') {
                Add-Failure "examples/$ExampleName/run.sh must resolve paths from its own location"
            }
            if ($ShellCode -notmatch '(?m)^\s*set\s+-\S*e') {
                Add-Failure "examples/$ExampleName/run.sh must enable fail-fast mode with set -e"
            }
            if ($ShellCode -match '(?im)^\s*(?:exec\s+)?(?:pwsh|powershell)(?:\.exe)?(?:\s|$)') {
                Add-Failure "examples/$ExampleName/run.sh must not invoke PowerShell"
            }
            if (Test-MachinePath $ShellCode) {
                Add-Failure "examples/$ExampleName/run.sh contains a machine-specific absolute path"
            }
        }
    }

    if ((Test-Path -LiteralPath $PowerShellEntrypoint -PathType Leaf) -and
        (Get-Item -LiteralPath $PowerShellEntrypoint).Length -gt 0) {
        $PowerShellText = Read-Utf8File $PowerShellEntrypoint
        if ($null -ne $PowerShellText) {
            $PowerShellCode = Get-CodeWithoutLineComments $PowerShellText
            if ($PowerShellCode -notmatch '(?i)\$PSScriptRoot|\$MyInvocation\.MyCommand\.(?:Path|Definition)') {
                Add-Failure "examples/$ExampleName/run.ps1 must resolve paths from its own location"
            }
            if ($PowerShellCode -notmatch '(?im)^\s*\$ErrorActionPreference\s*=\s*[''\"]Stop[''\"]') {
                Add-Failure "examples/$ExampleName/run.ps1 must set ErrorActionPreference to Stop"
            }
            if ($PowerShellCode -match '(?im)^\s*(?:&\s+)?(?:bash|sh)(?:\.exe)?(?:\s|$)') {
                Add-Failure "examples/$ExampleName/run.ps1 must not invoke a POSIX shell"
            }
            if (Test-MachinePath $PowerShellCode) {
                Add-Failure "examples/$ExampleName/run.ps1 contains a machine-specific absolute path"
            }

            $Tokens = $null
            $ParseErrors = $null
            [System.Management.Automation.Language.Parser]::ParseFile(
                $PowerShellEntrypoint,
                [ref]$Tokens,
                [ref]$ParseErrors
            ) | Out-Null
            foreach ($ParseError in $ParseErrors) {
                Add-Failure "examples/$ExampleName/run.ps1 syntax: $($ParseError.Message)"
            }
        }
    }
}

if ($Failures.Count -gt 0) {
    [Console]::Error.WriteLine(
        "check-example-entrypoints: FAILED ($($Failures.Count) issue(s))"
    )
    exit 1
}

Write-Output "check-example-entrypoints: OK ($($ExampleDirs.Count) example(s))"

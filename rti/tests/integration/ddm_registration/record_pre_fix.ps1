param()

$ErrorActionPreference = 'Stop'
$repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../../..'))
$evidence = [IO.Path]::GetFullPath((Join-Path $repo '../ddm-evidence-20260919'))
$generated = Join-Path $evidence 'pre-fix-overlay'
$null = New-Item -ItemType Directory -Path $generated, $evidence -Force
$utf8 = [Text.UTF8Encoding]::new($false)
$replacements = [ordered]@{}
$blobs = @(
    @{ Path = 'rti/internal/adapters/grpc/ddm.go'; Blob = '64d0a12d8fa73083eef3bcdc4cd399467d4b97e8'; File = 'grpc.go' },
    @{ Path = 'rti/internal/hla/ddm/manager.go'; Blob = 'dfdf485bd510dc99ff444d504d5e4b5162d06a37'; File = 'manager.go' }
)

# These are the pre-fix index blobs, not rewritten production files. Only
# import paths are relocated to match the repository's concurrent refactor.
foreach ($entry in $blobs) {
    $source = (& git -C $repo show $entry.Blob) -join "`n"
    if ($LASTEXITCODE -ne 0) { throw "Cannot read Git blob $($entry.Blob)" }
    $source = $source.Replace('github.com/cbchoi/gorti/rti/internal/core', 'github.com/cbchoi/gorti/rti/internal/hla/core')
    $source = $source.Replace('github.com/cbchoi/gorti/rti/internal/ddm', 'github.com/cbchoi/gorti/rti/internal/hla/ddm')
    $source = $source.Replace('github.com/cbchoi/gorti/rti/internal/genproto/rti/v1', 'github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1')
    $target = Join-Path $generated $entry.File
    [IO.File]::WriteAllText($target, $source + "`n", $utf8)
    $replacements[[IO.Path]::GetFullPath((Join-Path $repo $entry.Path))] = $target
}
$overlay = Join-Path $generated 'overlay.json'
[IO.File]::WriteAllText($overlay, (@{ Replace = $replacements } | ConvertTo-Json -Depth 4), $utf8)
$log = Join-Path $evidence 'red-pre-fix.txt'
$header = @(
    'PRE-FIX SOURCE REPLAY: two exact Git blobs, import-path relocation only.',
    'Two-file overlay on the checkout at replay time, not an untouched old release.',
    ('Recorded UTC: ' + [DateTime]::UtcNow.ToString('o')),
    'grpc/ddm.go blob: 64d0a12d8fa73083eef3bcdc4cd399467d4b97e8',
    'ddm/manager.go blob: dfdf485bd510dc99ff444d504d5e4b5162d06a37',
    'Command: go test -overlay=<generated overlay.json> ./rti/tests/integration/ddm_registration -count=1 -v',
    ''
) -join "`n"
Push-Location $repo
try {
    $lines = & go test "-overlay=$overlay" ./rti/tests/integration/ddm_registration -count=1 -v 2>&1
    $result = $LASTEXITCODE
    $output = $lines -join "`n"
    [IO.File]::WriteAllText($log, $header + $output + "`nExit code: $result`n", $utf8)
    $lines | ForEach-Object { Write-Output $_ }
    if ($result -ne 1 -or
        $output -notmatch 'returned zero object handle' -or
        $output -notmatch 'invalid registration accepted' -or
        $output -notmatch 'SetRangeBounds accepted invalid range \[40,30\]' -or
        $output -notmatch 'SetRangeBounds accepted invalid range \[30,1001\]') {
        throw 'The replay did not produce all expected behavioral RED signatures; inspect the evidence log.'
    }
} finally {
    Pop-Location
    # Remove only the three generated overlay files, never production sources.
    foreach ($name in @('grpc.go', 'manager.go', 'overlay.json')) {
        Remove-Item -LiteralPath (Join-Path $generated $name) -ErrorAction SilentlyContinue
    }
}

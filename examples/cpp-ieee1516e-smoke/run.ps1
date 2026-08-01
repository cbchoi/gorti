[CmdletBinding()]
param(
    [int]$RtidPort = 18080,
    [int]$HoldSeconds = 0,
    [string]$CppBuildDir = $env:CPP_BUILD_DIR,
    [string]$PublisherBinary = $env:PUBLISHER_BINARY,
    [string]$RtidBinary = $env:RTID_BINARY
)

$ErrorActionPreference = "Stop"
$ProcessPath = $env:Path
[Environment]::SetEnvironmentVariable("PATH", $null, [EnvironmentVariableTarget]::Process)
[Environment]::SetEnvironmentVariable("Path", $ProcessPath, [EnvironmentVariableTarget]::Process)
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$WorkDir = Join-Path ([IO.Path]::GetTempPath()) ("gorti-cpp-smoke-" + [guid]::NewGuid().ToString("N"))
$Rti = $null

function ConvertTo-ProcessCommandLine {
    param([string[]]$ArgumentList)

    return (($ArgumentList | ForEach-Object {
        $Argument = [string]$_
        if ($Argument.Length -gt 0 -and $Argument -notmatch '[\s"]') {
            $Argument
            return
        }

        $Builder = [Text.StringBuilder]::new()
        [void]$Builder.Append('"')
        $Backslashes = 0
        foreach ($Character in $Argument.ToCharArray()) {
            if ($Character -eq '\') {
                $Backslashes++
                continue
            }
            if ($Character -eq '"') {
                [void]$Builder.Append(('\' * (($Backslashes * 2) + 1)))
                [void]$Builder.Append('"')
                $Backslashes = 0
                continue
            }
            if ($Backslashes -gt 0) {
                [void]$Builder.Append(('\' * $Backslashes))
                $Backslashes = 0
            }
            [void]$Builder.Append($Character)
        }
        if ($Backslashes -gt 0) {
            [void]$Builder.Append(('\' * ($Backslashes * 2)))
        }
        [void]$Builder.Append('"')
        $Builder.ToString()
    }) -join ' ')
}

function Wait-RtiPort {
    param([Diagnostics.Process]$Process, [int]$Port)
    $Deadline = [DateTime]::UtcNow.AddSeconds(10)
    while ([DateTime]::UtcNow -lt $Deadline) {
        if ($Process.HasExited) { throw "rtid exited before accepting connections." }
        $Client = [Net.Sockets.TcpClient]::new()
        try {
            $Attempt = $Client.BeginConnect("127.0.0.1", $Port, $null, $null)
            if ($Attempt.AsyncWaitHandle.WaitOne(200)) {
                $Client.EndConnect($Attempt)
                return
            }
        }
        catch {
            # The listener may still be starting.
        }
        finally {
            $Client.Dispose()
        }
        Start-Sleep -Milliseconds 100
    }
    throw "rtid did not listen on 127.0.0.1:$Port within 10 seconds."
}

if ($RtidPort -lt 1 -or $RtidPort -gt 65535) { throw "RtidPort must be from 1 to 65535." }
if ($HoldSeconds -lt 0) { throw "HoldSeconds must not be negative." }
if (-not $CppBuildDir) { $CppBuildDir = Join-Path $RepoRoot "cppsdk\build" }
elseif (-not [IO.Path]::IsPathRooted($CppBuildDir)) { $CppBuildDir = Join-Path $RepoRoot $CppBuildDir }

New-Item -ItemType Directory -Path $WorkDir | Out-Null
try {
    if (-not $PublisherBinary) {
        if (-not (Get-Command cmake -ErrorAction SilentlyContinue)) {
            throw "cpp-ieee1516e-smoke: CMake 3.18 or later is required to build the publisher."
        }
        $GeneratedDir = Join-Path $RepoRoot "cppsdk\_generated\rti\v1"
        $Generated = Get-ChildItem -Path $GeneratedDir -Filter "*.cc" -ErrorAction SilentlyContinue
        if (-not $Generated) {
            if (-not (Get-Command buf -ErrorAction SilentlyContinue)) {
                throw "cpp-ieee1516e-smoke: generated C++ bindings are missing; install buf and run 'buf generate' from $RepoRoot."
            }
            Write-Host "cpp-ieee1516e-smoke: generating protobuf and gRPC bindings"
            Push-Location $RepoRoot
            try { & buf generate; if ($LASTEXITCODE -ne 0) { throw "buf generate failed" } }
            finally { Pop-Location }
        }

        if (-not (Test-Path (Join-Path $CppBuildDir "CMakeCache.txt"))) {
            $ConfigureArgs = @("-S", (Join-Path $RepoRoot "cppsdk"), "-B", $CppBuildDir, "-DCMAKE_BUILD_TYPE=Release")
            $Toolchain = $env:CMAKE_TOOLCHAIN_FILE
            if (-not $Toolchain) {
                $ConanToolchain = Join-Path $CppBuildDir "conan_toolchain.cmake"
                if (Test-Path $ConanToolchain) { $Toolchain = $ConanToolchain }
            }
            if ($Toolchain) { $ConfigureArgs += "-DCMAKE_TOOLCHAIN_FILE=$Toolchain" }
            if ($env:CMAKE_PREFIX_PATH) { $ConfigureArgs += "-DCMAKE_PREFIX_PATH=$($env:CMAKE_PREFIX_PATH)" }
            Write-Host "cpp-ieee1516e-smoke: configuring the C++ SDK"
            & cmake @ConfigureArgs
            if ($LASTEXITCODE -ne 0) {
                throw "CMake configure failed. Install gRPC++ and protobuf, or prepare $CppBuildDir with Conan as described in the README."
            }
        }

        Write-Host "cpp-ieee1516e-smoke: building cpp_ieee1516e_publisher"
        & cmake --build $CppBuildDir --config Release --target cpp_ieee1516e_publisher --parallel
        if ($LASTEXITCODE -ne 0) { throw "C++ publisher build failed." }
        $Publisher = Get-ChildItem -Path $CppBuildDir -Recurse -File -Filter "cpp_ieee1516e_publisher.exe" -ErrorAction SilentlyContinue | Select-Object -First 1
        if (-not $Publisher) {
            $Publisher = Get-ChildItem -Path $CppBuildDir -Recurse -File -Filter "cpp_ieee1516e_publisher" -ErrorAction SilentlyContinue | Select-Object -First 1
        }
        if (-not $Publisher) { throw "the build completed but the publisher binary was not found below $CppBuildDir." }
        $PublisherBinary = $Publisher.FullName
    }
    elseif (-not (Test-Path -LiteralPath $PublisherBinary -PathType Leaf)) {
        throw "cpp-ieee1516e-smoke: publisher binary not found: $PublisherBinary"
    }

    if (-not $RtidBinary) {
        if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
            throw "cpp-ieee1516e-smoke: Go 1.22 or later is required to build rtid."
        }
        $RtidBinary = Join-Path $WorkDir "rtid.exe"
        Write-Host "cpp-ieee1516e-smoke: building a temporary rtid"
        Push-Location $RepoRoot
        try { & go build -buildvcs=false -o $RtidBinary ./rti/cmd/rtid; if ($LASTEXITCODE -ne 0) { throw "rtid build failed" } }
        finally { Pop-Location }
    }
    elseif (-not (Test-Path -LiteralPath $RtidBinary -PathType Leaf)) {
        throw "cpp-ieee1516e-smoke: rtid binary not found: $RtidBinary"
    }

    $Saves = Join-Path $WorkDir "saves"
    New-Item -ItemType Directory -Path $Saves | Out-Null
    $RtidOut = Join-Path $WorkDir "rtid.out.log"
    $RtidErr = Join-Path $WorkDir "rtid.err.log"
    $RtiArguments = @(
        "--listen=127.0.0.1:$RtidPort",
        "--metrics-listen=127.0.0.1:0",
        "--admin-listen=",
        "--log-level=warn",
        "--save-dir=$Saves"
    )
    $Rti = Start-Process -FilePath $RtidBinary -ArgumentList (ConvertTo-ProcessCommandLine $RtiArguments) `
        -RedirectStandardOutput $RtidOut -RedirectStandardError $RtidErr -PassThru -WindowStyle Hidden
    Wait-RtiPort -Process $Rti -Port $RtidPort

    Write-Host "cpp-ieee1516e-smoke: running the publisher"
    $PublisherOutput = & $PublisherBinary `
        --url "grpc://127.0.0.1:$RtidPort" `
        --federation "cpp-ieee1516e-smoke-$RtidPort" `
        --fom (Join-Path $PSScriptRoot "federation.fom.xml") `
        --hold $HoldSeconds 2>&1
    $PublisherOutput | Write-Host
    if ($LASTEXITCODE -ne 0) {
        Get-Content $RtidOut, $RtidErr -ErrorAction SilentlyContinue
        throw "cpp-ieee1516e-smoke: publisher exited unsuccessfully."
    }
    if (($PublisherOutput | Out-String) -notmatch "publisher: done") {
        throw "cpp-ieee1516e-smoke: publisher exited without its completion marker."
    }
    Write-Host "cpp-ieee1516e-smoke: PASS - connect, publish, update, and interaction send completed"
}
finally {
    if ($Rti) {
        if (-not $Rti.HasExited) {
            Stop-Process -Id $Rti.Id -Force -ErrorAction SilentlyContinue
            $Rti.WaitForExit()
        }
        $Rti.Dispose()
    }
    Remove-Item -LiteralPath $WorkDir -Recurse -Force -ErrorAction SilentlyContinue
}

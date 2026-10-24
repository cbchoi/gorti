# Installation

## Requirements

| Goal | Required tools |
|---|---|
| Run a published `rtid` archive | No separate language runtime |
| Build `rtid`, `rti-top`, or the Go examples | Git and Go 1.22 or later |
| Use the Python SDK from this checkout | Python 3.11+ and `pip` |
| Build the Java SDK | JDK 11+ and Maven 3.9+ |

The portable `rtid` and `rti-top` builds do not require CGo or a DDS runtime.
Go dependencies require network access the first time they are downloaded.
Buf and its remote plugins are needed only for the all-language generation
path.

The server, Go SDK, and Python SDK are supported on Linux, macOS, and Windows.

## Build from source

Clone the repository, then keep the repository root as the working directory:

```bash
git clone https://github.com/cbchoi/gorti.git
cd gorti
```

=== "Linux and macOS"

    ```bash
    mkdir -p bin
    go build -o bin/rtid ./rti/cmd/rtid
    go build -o bin/rti-top ./rti/cmd/rti-top
    ```

=== "Windows PowerShell"

    ```powershell
    New-Item -ItemType Directory -Force bin | Out-Null
    go build -o bin\rtid.exe ./rti/cmd/rtid
    go build -o bin\rti-top.exe ./rti/cmd/rti-top
    ```

Verify the binaries from their build directory; adding `bin` to `PATH` is not
required for the quickstart.

=== "Linux and macOS"

    ```bash
    ./bin/rtid --version
    ./bin/rtid --help
    ./bin/rti-top --version
    ```

=== "Windows PowerShell"

    ```powershell
    .\bin\rtid.exe --version
    .\bin\rtid.exe --help
    .\bin\rti-top.exe --version
    ```

An untagged source build can report a development version. Record the Git
commit as well when a run must be reproducible. To exercise all Go packages in
the checkout, run `go test ./...` from the repository root.

## Release archives

This checkout is a v0.9.0 release candidate. Until the corresponding tag and
assets appear on GitHub Releases, build from source and cite the commit SHA.
The instructions below apply after a release is published.

The repository's release configuration produces `rtid` and `rti-top` archives
for Linux, macOS, and Windows on amd64 and arm64. Linux and macOS archives are
`.tar.gz`; Windows archives are `.zip`. Use the
[GitHub releases](https://github.com/cbchoi/gorti/releases) page as the source
of truth for published versions and assets.

Download the archive for the required operating system and architecture plus
the matching `gorti_<version>_SHA256SUMS` file. Verify the selected archive
before extracting it. For example, for a `0.9.0` amd64 archive:

=== "Linux"

    ```bash
    grep ' gorti_0.9.0_linux_amd64.tar.gz$' gorti_0.9.0_SHA256SUMS | sha256sum -c -
    tar -xzf gorti_0.9.0_linux_amd64.tar.gz
    ```

=== "macOS"

    ```bash
    grep ' gorti_0.9.0_darwin_amd64.tar.gz$' gorti_0.9.0_SHA256SUMS | shasum -a 256 -c -
    tar -xzf gorti_0.9.0_darwin_amd64.tar.gz
    ```

=== "Windows PowerShell"

    ```powershell
    Get-FileHash .\gorti_0.9.0_windows_amd64.zip -Algorithm SHA256
    Select-String 'gorti_0.9.0_windows_amd64.zip' .\gorti_0.9.0_SHA256SUMS
    Expand-Archive .\gorti_0.9.0_windows_amd64.zip -DestinationPath .\gorti
    ```

On Windows, compare the two displayed hashes before extraction. Replace
`amd64` with `arm64` when appropriate.

For a published release on Linux or macOS, the repository also provides a
checksum-verifying installer. Download and inspect it before running it. A
user-writable destination avoids requiring `sudo`:

```bash
curl -fsSLO https://raw.githubusercontent.com/cbchoi/gorti/main/scripts/install.sh
INSTALL_DIR="$HOME/.local/bin" sh ./install.sh
export PATH="$HOME/.local/bin:$PATH"
```

The installer supports Linux and macOS only and installs `rtid` and `rti-top`.
After the tag exists, set `VERSION=v0.9.0` before `sh ./install.sh` to request
that release instead of the latest published version.

## Python SDK from source

The generated Python gRPC bindings are not tracked in Git. The development
extra installs a protobuf-7-compatible `grpcio-tools`; generate the bindings
locally after installing the editable package. A virtual environment keeps the
SDK dependencies isolated.

=== "Linux and macOS"

    ```bash
    python3 -m venv .venv
    . .venv/bin/activate
    python -m pip install --upgrade pip
    python -m pip install -e './pysdk[dev]'
    python -m rti1516e._proto
    ```

=== "Windows PowerShell"

    ```powershell
    python -m venv .venv
    .\.venv\Scripts\Activate.ps1
    python -m pip install --upgrade pip
    python -m pip install -e '.\pysdk[dev]'
    python -m rti1516e._proto
    ```

The runtime package is `rti1516e`. Add the `pyjevsim` extra only for the DEVS
bridge: `python -m pip install -e './pysdk[dev,pyjevsim]'`.
The [model integration guide](model-integration.md) uses this extra. Full SDK
test execution is a separate step; see [verification](verification.md) for
live-server prerequisites and the recorded test selections.

## Java SDK from source

With JDK 11 or later and Maven 3.9 or later installed, run from the repository root:

```bash
mvn -f javasdk/pom.xml verify
```

Maven generates the Java protocol bindings and builds the `hla.rti1516e` API
and gorti service provider. Live-server tests require an explicit endpoint;
see the
[Java SDK guide](https://github.com/cbchoi/gorti/blob/main/javasdk/README.md)
for setup, test commands, and service coverage.

## Installation troubleshooting

| Symptom | Check |
|---|---|
| `go: go.mod file not found` | Run the command from the cloned repository root. |
| Go reports an older language version | Install Go 1.22 or later and confirm with `go version`. |
| Dependency or plugin download fails | Confirm network and proxy access for the Go module proxy, GitHub, and `buf.build`. |
| PowerShell blocks virtual-environment activation | Invoke `.\.venv\Scripts\python.exe -m pip ...` directly or use an execution policy approved for the machine. |
| The installer cannot write its destination | Set `INSTALL_DIR` to a user-writable directory and add that directory to `PATH`. |

The [quickstart](quickstart.md) runs a loopback-only two-federate federation.

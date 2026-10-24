# Installation

## Requirements

| Goal | Required tools |
|---|---|
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

Source builds may report a development version. Record the Git commit when
retaining experiment results.

## Python SDK from source

Install the Python SDK in a virtual environment, then generate its gRPC
bindings. The development extra includes the required generation tools.

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
The [model integration guide](model-integration.md) uses this extra.
See [verification](verification.md) for live-server test prerequisites.

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

The [quickstart](quickstart.md) runs a loopback-only two-federate federation.

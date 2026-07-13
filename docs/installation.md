# Installation

## Requirements

- Go 1.22 or later to build the RTI and Go SDK examples.
- Python 3.11 or later for the Python SDK and comparison tooling.
- A C++17 compiler for the C++ DLC SDK.

## Build from source

The v0.9 development version is currently installed from source:

```bash
git clone https://github.com/cbchoi/gorti.git
cd gorti
go build -o bin/rtid ./rti/cmd/rtid
go build -o bin/rti-top ./rti/cmd/rti-top
go test ./...
```

On Windows, use `.exe` output names. This source-build path is the documented
installation path until the v0.9 release assets are published and verified.

## Release binaries

The [GitHub releases](https://github.com/cbchoi/gorti/releases) page currently
contains earlier development releases. The v0.9 release checklist will publish
and verify Linux, macOS, and Windows archives for amd64 and arm64 together with
SHA-256 checksums. Do not use an untagged `main` build as archival evidence.

## Python SDK

The Python SDK is currently installed from the repository:

```bash
python -m pip install -e "./pysdk[dev]"
python -m pytest pysdk/tests
```

A PyPI wheel is a release gate, not a currently advertised installation path.

## Verify the installation

```bash
rtid --version
rtid --help
```

The default federate endpoint is `127.0.0.1:8442`. The admin endpoint is
loopback-only on port 8443 by default. Read [operations](operations.md) before
binding either endpoint to a non-loopback address.

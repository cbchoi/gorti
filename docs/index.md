# gorti documentation

gorti implements an IEEE 1516-2010 (HLA Evolved) RTI in Go. The distribution
contains the `rtid` server, Go/Python/C++ federate SDKs, and tools for logging
and cross-RTI verification.

## Start here

1. [Install gorti](installation.md). Building the server requires Go 1.22 or
   later; Python and C++ are optional.
2. Run the [two-federate quickstart](quickstart.md) from the repository root.
3. Read the [user guide](user-guide.md) for the federation, FOM, callback, and
   SDK lifecycle.
4. Check [operations](operations.md) before exposing a server endpoint outside
   the local machine.

[Time management](time-management.md) explains why a request from one federate
can remain blocked until another federate advances.

## Supported deployment

The v0.9 baseline is one authoritative `rtid` process with local or remote
federates. Bind all endpoints to `127.0.0.1` for a first run. The unqualified
federate and metrics defaults, `:8442` and `:9090`, listen on all interfaces;
the admin default is plaintext on `localhost:8443`.

Federation, declaration, object, time, ownership, data distribution,
synchronization, save/restore, and management object services are included.
The project does not claim formal IVCT certification, HLA 1.3 or IEEE
1516-2000 support, a Java SDK, or production multi-node failover. Tested
behavior and known limits are listed under [verification](verification.md).

## Reproducing results

Cross-RTI performance runs use the same FOM bytes, random seed, two-process
choreography, callbacks, logging, warmups, measurements, and AB/BA order. The
runner rejects results when semantics or delivery accounting differ. See the
[comparison protocol](reproducibility.md) and [performance notes](performance.md).

## Project links

- Source: <https://github.com/cbchoi/gorti>
- Releases: <https://github.com/cbchoi/gorti/releases>
- License: MIT
- Citation: [citation metadata](citation.md)

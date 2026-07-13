# Quickstart

This walkthrough starts a real `rtid` process and two independent Go federate
processes. The first federate requests logical time 5 and waits until the
second federate advances.

## Build

From the repository root:

=== "Linux and macOS"

    ```bash
    mkdir -p bin
    go build -o bin/rtid ./rti/cmd/rtid
    go build -o bin/go-tar-wait ./examples/go-tar-wait
    ```

=== "Windows PowerShell"

    ```powershell
    New-Item -ItemType Directory -Force bin | Out-Null
    go build -o bin/rtid.exe ./rti/cmd/rtid
    go build -o bin/go-tar-wait.exe ./examples/go-tar-wait
    ```

## Start the RTI

Open terminal 1:

=== "Linux and macOS"

    ```bash
    ./bin/rtid \
      --listen 127.0.0.1:8442 \
      --admin-listen 127.0.0.1:8443 \
      --metrics-listen 127.0.0.1:9090 \
      --log-dir ./eventlogs \
      --save-dir ./gorti-saves
    ```

=== "Windows PowerShell"

    ```powershell
    .\bin\rtid.exe `
      --listen 127.0.0.1:8442 `
      --admin-listen 127.0.0.1:8443 `
      --metrics-listen 127.0.0.1:9090 `
      --log-dir .\eventlogs `
      --save-dir .\gorti-saves
    ```

## Start two federates

In terminal 2, start the waiter:

=== "Linux and macOS"

    ```bash
    ./bin/go-tar-wait --role waiter
    ```

=== "Windows PowerShell"

    ```powershell
    .\bin\go-tar-wait.exe --role waiter
    ```

In terminal 3, start the peer:

=== "Linux and macOS"

    ```bash
    ./bin/go-tar-wait --role peer
    ```

=== "Windows PowerShell"

    ```powershell
    .\bin\go-tar-wait.exe --role peer
    ```

The waiter reports that `TAR(5)` is pending. After the peer's default
three-second delay, both federates receive `TimeAdvanceGrant(5)` and exit.
This is intentional: the finite example makes the wait and grant directly
testable.

## Check the result

Expected waiter records include:

```text
[waiter] TAR(5) requested; grant is now blocked by the peer at time 0
[waiter] GRANT(5) after 3s: PASS - peer TAR released the pending request
```

The wait demonstrates that a grant is derived from the federation-wide time
state, rather than returned immediately to one federate. Continue with
[time management](time-management.md) for the ordering and lookahead rules.

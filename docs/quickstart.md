# Quickstart

This walkthrough starts one real `rtid` process and two independent Go
federate processes. The waiter requests logical time 5 and remains blocked
until the peer advances.

## Before you start

- Install Git and Go 1.22 or later as described in
  [installation](installation.md).
- Use three terminals, all opened at the repository root. The example's
  default FOM path is relative to that directory.
- Keep the example on loopback. Read [operations](operations.md) before
  accepting remote connections.

## Optional automated check

The example includes native launchers that build temporary binaries, wait for
`rtid` to accept connections, run both federates, verify their exit status, and
stop all child processes. From the repository root:

=== "Linux and macOS"

    ```bash
    bash examples/go-tar-wait/run.sh
    ```

=== "Windows PowerShell"

    ```powershell
    & .\examples\go-tar-wait\run.ps1
    ```

A successful run ends with `go-tar-wait: PASS`. Continue below to run the same
scenario manually and observe the server lifecycle in separate terminals.

## Build

=== "Linux and macOS"

    ```bash
    mkdir -p bin
    go build -o bin/rtid ./rti/cmd/rtid
    go build -o bin/go-tar-wait ./examples/go-tar-wait
    ```

=== "Windows PowerShell"

    ```powershell
    New-Item -ItemType Directory -Force bin | Out-Null
    go build -o bin\rtid.exe ./rti/cmd/rtid
    go build -o bin\go-tar-wait.exe ./examples/go-tar-wait
    ```

## Start the RTI

In terminal 1, start the server in the foreground with all endpoints bound to
loopback:

=== "Linux and macOS"

    ```bash
    ./bin/rtid \
      --listen 127.0.0.1:8442 \
      --admin-listen 127.0.0.1:8443 \
      --metrics-listen 127.0.0.1:9090 \
      --audit-replay-plugin event-journal \
      --log-dir ./eventlogs \
      --save-dir ./gorti-saves \
      --log-format text
    ```

=== "Windows PowerShell"

    ```powershell
    .\bin\rtid.exe `
      --listen 127.0.0.1:8442 `
      --admin-listen 127.0.0.1:8443 `
      --metrics-listen 127.0.0.1:9090 `
      --audit-replay-plugin event-journal `
      --log-dir .\eventlogs `
      --save-dir .\gorti-saves `
      --log-format text
    ```

Wait for a log record containing `rtid serving`. It reports the actual gRPC,
metrics, and admin addresses. Leave this terminal running.

## Start two federates

In terminal 2, start the waiter first:

=== "Linux and macOS"

    ```bash
    ./bin/go-tar-wait --role waiter
    ```

=== "Windows PowerShell"

    ```powershell
    .\bin\go-tar-wait.exe --role waiter
    ```

The waiter pauses until its peer joins. In terminal 3, start the peer:

=== "Linux and macOS"

    ```bash
    ./bin/go-tar-wait --role peer
    ```

=== "Windows PowerShell"

    ```powershell
    .\bin\go-tar-wait.exe --role peer
    ```

The peer holds logical time 0 for three seconds. Both processes then receive
`TimeAdvanceGrant(5)`, resign, close their connections, and exit.

## Check the result

Expected waiter records include:

```text
[waiter] TAR(5) requested; grant is now blocked by the peer at time 0
[waiter] GRANT(5) after 3s: PASS - peer TAR released the pending request
```

The wait demonstrates that a grant is derived from federation-wide time state,
rather than returned immediately to one federate.

The server remains online after the examples finish. The metrics liveness page
is available at <http://127.0.0.1:9090/> and Prometheus metrics at
<http://127.0.0.1:9090/metrics> while it is running.

## Stop and run again

Return to terminal 1 and press `Ctrl-C`. `rtid` logs `rtid shutting down`,
gracefully stops its gRPC listeners, closes event-log resources, and exits.
The `eventlogs` and `gorti-saves` directories remain on disk.

For another run, start `rtid` again and repeat the two federate commands. The
server's live federation state is process-local. If a federate was interrupted
and its name appears to remain joined, restarting this development server is
the simplest clean reset.

## Quickstart troubleshooting

| Symptom | Resolution |
|---|---|
| The server reports that an address is already in use | Stop the process using ports 8442, 8443, or 9090, or choose unused ports and pass matching addresses to the clients. |
| A federate reports a connection error | Confirm terminal 1 reached `rtid serving` and that the client default `127.0.0.1:8442` matches `--listen`. |
| `read FOM` reports that the file does not exist | Run from the repository root or pass `--fom` with the correct path. |
| A federate reaches its deadline | Start both roles within the default 20-second timeout, or give both a larger value such as `--timeout 60s`. |
| A rerun reports that a federate name is already in use | Stop both example processes and restart `rtid` to clear interrupted in-memory membership. |

See [time management](time-management.md) for ordering and lookahead rules and
the [user guide](user-guide.md) for the normal SDK lifecycle.

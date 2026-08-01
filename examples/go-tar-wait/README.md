# Go TAR wait

This two-process federation demonstrates that `TimeAdvanceRequest(5)` remains
pending while another time-regulating federate stays at logical time zero.
After the peer's three-second delay, its own TAR raises the federation's LBTS
floor and both federates receive `TimeAdvanceGrant(5)`.

```text
waiter (lookahead 1)                 peer (lookahead 2)
        |                                    |
        |<------------ ready ----------------|
        | TAR(5)                             | time 0 for 3 seconds
        |             waits                  |
        |                                    | TAR(5)
        |<---------- GRANT(5) ---------------|
        |                         GRANT(5) -->|
```

## Prerequisites

- Go 1.22 or later on `PATH`.

## Run and verify

From any working directory:

```bash
bash examples/go-tar-wait/run.sh
```

```powershell
.\examples\go-tar-wait\run.ps1
```

The launcher builds temporary binaries, starts `rtid`, waits until its gRPC
listener is ready, starts the waiter and peer, and checks both exit statuses.
It always stops the daemon and any unfinished federates. Success ends with:

```text
[waiter] GRANT(5) after 3s: PASS - peer TAR released the pending request
[peer] GRANT(5): PASS
go-tar-wait: PASS - the peer delay held and then released TAR(5)
```

The Bash flow accepts `RTID_PORT`, `PEER_DELAY`, and `RUN_TIMEOUT` environment
overrides. PowerShell exposes the matching `-RtidPort`, `-PeerDelay`, and
`-RunTimeout` parameters. Use the same peer delay for both federates; the
entry scripts do this automatically.

# go-tar-wait example

This example demonstrates a `TimeAdvanceRequest(5)` that remains pending
until another time-regulating federate advances. It uses two real federate
processes connected to `rtid` over gRPC.

```text
waiter (lookahead 1)                    peer (lookahead 2)
        |                                       |
        |<----------- peer ready ---------------|
        |                                       |
        | TAR(5)                                | logical time 0
        | ... waits for TimeAdvanceGrant ...    | sleeps for 3 seconds
        |                                       |
        |                                       | TAR(5)
        |<------------ GRANT(5) ----------------|
        |                         GRANT(5) ------>|
```

The waiter's request is held because the peer contributes `0 + 2` to LBTS,
which does not cover logical time 5. When the peer also requests time 5, the
pending-request floors raise LBTS enough for both grants to fire at exactly 5.

## Build on Windows

From the repository root:

```powershell
go build -o bin\go-tar-wait.exe ./examples/go-tar-wait
```

## Run

Terminal 1 starts `rtid`:

```powershell
.\bin\rtid.exe `
  --listen 127.0.0.1:8442 `
  --admin-listen 127.0.0.1:8443 `
  --metrics-listen 127.0.0.1:9090 `
  --log-dir .\eventlogs `
  --save-dir .\gorti-saves `
  --log-format text
```

Terminal 2 starts the waiter. It can be started before the peer and will wait
at the readiness handshake:

```powershell
.\bin\go-tar-wait.exe --role waiter
```

Terminal 3 starts the peer:

```powershell
.\bin\go-tar-wait.exe --role peer
```

Expected waiter output:

```text
[waiter] TAR(5) requested; grant is now blocked by the peer at time 0
[waiter] GRANT(5) after 3s: PASS - peer TAR released the pending request
```

Expected peer output:

```text
[peer] waiter started TAR; holding logical time 0 for 3s
[peer] TAR(5) requested; LBTS can now cover the target
[peer] GRANT(5): PASS
```

Both processes exit after the grant because this is a finite verification
example. Use the same `--peer-delay` value on both processes when overriding
the default, for example `--peer-delay 5s`.

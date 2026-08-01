# Go ping-pong

Two in-process Go federates exchange interactions through the production
`rtid` federation, declaration, and event-log stack. The process exits zero
after every ping/pong round trip completes.

## Prerequisites

- Go 1.22 or later on `PATH`.

## Run and verify

The entry scripts resolve the repository root from their own location, so they
can be called from any working directory.

```bash
bash examples/go-pingpong/run.sh
```

```powershell
.\examples\go-pingpong\run.ps1
```

The default is 1000 round trips. Override it with `ROUNDS=100` on Bash or
`-Rounds 100` in PowerShell. Success includes output similar to:

```text
pingpong complete: 1000 rounds
go-pingpong: 1000 rounds in ...
```

No external daemon is left running: the example starts the required RTI demo
process, waits for its verification result, and exits with the same status.

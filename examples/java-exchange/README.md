# Java Publisher / Receiver Exchange

Two independent JVMs exchange one named `HLAobjectRoot.Item` object update and one
`HLAinteractionRoot.Pulse` interaction through the public IEEE 1516e Java API.
There are no test-class or SDK-internal dependencies. Each terminal runs exactly
one federate named `publisher` or `receiver`.

## Prerequisites

- JDK 11 or newer and Maven.
- A separately running GoRTI server with the Java SDK declaration, object,
  interaction, producing-federate lookup and synchronization services implemented.
- An explicit endpoint: `grpc://host:port` for plaintext or `grpcs://host:port` for
  TLS, with the server certificate trusted by the JVM. The scripts do not start a server.

Run these commands from the repository root. Build and install the SDK **once**,
before launching either federate, and repeat after SDK/example source changes:

```text
mvn -B -ntp -f javasdk/pom.xml -DskipTests install
```

Do not run that build concurrently in both terminals. The launchers only execute
`mvn -f javasdk/client/pom.xml exec:java`; they never compile, install or run tests.
Use the same Maven executable and local repository for installation and both launches.

## Windows PowerShell

Terminal 1:

```powershell
& .\examples\java-exchange\run.ps1 -Role receiver -Endpoint 'grpc://localhost:50051'
```

Terminal 2 (within 30 seconds of the first federate connecting):

```powershell
& .\examples\java-exchange\run.ps1 -Role publisher -Endpoint 'grpc://localhost:50051'
```

Either launch order works. Both scripts run directly in the current terminal and
return Maven's exit code. They do not open additional windows.

For executable/cache overrides, install once with the same settings first:

```powershell
$Maven = 'C:\tools\apache-maven\bin\mvn.cmd'
$MavenRepo = 'D:\maven-cache'
& $Maven '-B' '-ntp' "-Dmaven.repo.local=$MavenRepo" '-f' '.\javasdk\pom.xml' '-DskipTests' 'install'
& .\examples\java-exchange\run.ps1 -Role receiver -Endpoint 'grpc://localhost:50051' -Maven $Maven -MavenRepo $MavenRepo
```

Use `-Role publisher` in the other terminal with the same overrides. The scripts
also accept `GORTI_ENDPOINT`, `GORTI_MAVEN` and `GORTI_MAVEN_REPO` environment defaults.

## Linux / Bash

Terminal 1:

```bash
bash 'examples/java-exchange/run.sh' --role 'receiver' --endpoint 'grpc://localhost:50051'
```

Terminal 2:

```bash
bash 'examples/java-exchange/run.sh' --role 'publisher' --endpoint 'grpc://localhost:50051'
```

Optional Bash overrides are `--maven '/path/to/mvn'` and
`--maven-repo '/path/to/cache'`, or the same environment variables as PowerShell.
For the one-time install with those overrides:

```bash
'/path/to/mvn' '-B' '-ntp' '-Dmaven.repo.local=/path/to/cache' '-f' 'javasdk/pom.xml' '-DskipTests' 'install'
```

## Protocol And Results

1. The publisher creates the federation; the receiver retries joining only while
   it does not yet exist. Both join using their role as their federate name.
2. The publisher publishes `Item.value` and `Pulse`; the receiver subscribes to
   both before it can achieve `ready`. The publisher waits until
   `getFederateHandle("receiver")` succeeds before registering `ready`.
3. Both await the `ready` announcement, achieve it and await
   `federationSynchronized` before exchanging data.
4. The publisher reserves `java-exchange-item`, awaits reservation success,
   registers the object, updates `value` and sends `Pulse` using untimed overloads.
5. The receiver checks object discovery, exact bytes and tags, reliable receive
   order, and producing-federate metadata against the publisher's public handle.
   Only after both deliveries pass does it register `done`.
6. Both await/achieve `done` and await `federationSynchronized`, then resign.
   The publisher retries destruction while the receiver is still joined, and
   both disconnect. Failures still attempt cleanup, preserving the original error.

The canonical FOM for this example is the adjacent `exchange-fom.xml`, passed by
absolute path by both scripts. Its `Item.value` and `Pulse.payload` declarations
match the SDK exchange fixture: both are `HLAinteger32BE`. The declarations keep
`TimeStamp` order, but this demo deliberately uses only **receive-order (RO)**
service overloads and no time-management or timestamped object APIs.

| Delivery | Encoded bytes (hex) | Tag (hex) |
| --- | --- | --- |
| `Item.value` | `00 80 ff 7f` | `00 ff 11` |
| `Pulse.payload` | `ff 00 7f 80` | `80 00 22` |

Successful runs print `synchronized ready`, `synchronized done`, `resigned`,
`disconnected` and `exchange complete` with role prefixes. The receiver additionally
prints both `verified` deliveries; the publisher prints federation destruction.
Each launcher exits zero only when its federate and cleanup succeed.

## Settings And Failures

| Java system property | Default / requirement | Script option (PowerShell / Bash) |
| --- | --- | --- |
| `gorti.endpoint` | Required, explicit `grpc://` or `grpcs://` endpoint | `-Endpoint` / `--endpoint` |
| `gorti.role` | Required, `publisher` or `receiver` | `-Role` / `--role` |
| `gorti.federation` | `java-exchange` | `-Federation` / `--federation` |
| `gorti.fom` | Required path; scripts supply the adjacent FOM | Fixed by scripts |
| `gorti.timeoutSeconds` | `30`, range 1-300 | `-TimeoutSeconds` / `--timeout` |

Protocol waits share one timeout budget beginning after connection, including
join retries, peer discovery and both barriers. They block in `evokeCallback`
between attempts; there is no infinite or busy polling. Cleanup's destroy retry
has a separate timeout budget. In-flight SDK calls use the SDK's own RPC deadlines;
the setting is not a hard wall-clock limit for Maven startup, RPCs or all cleanup.
If a peer fails or is never launched, the other cannot wait indefinitely for it.
Failures produce a nonzero exit code and the failed stage or callback is reported.

Use the same endpoint and federation in both terminals. Only one pair should use
that federation at a time. An existing federation is treated as an error by the
publisher, not silently reused or destroyed. After an interrupted run that leaves
server state behind, choose a fresh shared `-Federation` / `--federation` name or
clean up that state separately. Increase the timeout in both terminals when you
need more time to launch the second process.

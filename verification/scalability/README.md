# Federate scalability comparison

This experiment runs one publisher and `N-1` subscribers in the same
federation for `N = 2, 3, 4, 8, 16`. Every subscriber validates the same DEVStone-HLA
attribute and interaction trace. The primary completion boundary is the final
expected callback at the slowest subscriber.

```powershell
verification\scalability\RunScalability.ps1
```

To run only the validated three- and four-federate Portico cases:

```powershell
& .\verification\scalability\RunScalability.ps1 `
  -Scales @(3, 4) `
  -Implementations portico
```

The default is one warm-up and three measured AB/BA runs per scale. Participants
are launched sequentially at 0.5-second intervals for both implementations.
Portico subscriber launches are additionally gated by a transient marker written
after the preceding subscriber has joined, resolved its handles, and declared
its interests. Each marker releases the next subscriber, preventing multiple
TCPPING participants from forming independent initial views. Above two
federates, the runner creates all Portico runtime containers before launching
their independent Java processes. This makes the complete TCPPING host set
resolvable before the first join.

For `N > 2`, an all-federate declaration barrier is followed by a declaration
reassertion and an HLA interaction handshake. Each subscriber publishes a
readiness interaction only after restoring its object and interaction
subscriptions. The publisher waits for every readiness message and returns an
acknowledgement before registering the measured object. The handshake prevents
late Portico federation-manifest updates from replacing a participant's local
declaration state. It is completed before object discovery and remains outside
the callback-completion measurement boundary.

Portico uses a five-second JGroups response limit through `N = 6`; larger
federations use a size-aware limit capped at 60 seconds. These controls are
outside the measured callback-completion boundary and are recorded in the
result metadata.
The publisher release follows a five-second control-plane settling interval
after the last subscriber readiness marker.
Multi-subscriber teardown uses one transient marker set per subscriber. Only
`scalability.json` is retained; process output and participant summaries are
transient.

The default Portico transport is the deterministic TCP override used by the
two-federate benchmark. Above two federates, every Java process receives the
complete TCPPING initial-host list.
`-PorticoTransport udp` remains available for diagnosis; same-IP multi-LRC UDP
does not provide a valid object-discovery lifecycle on the tested Portico 2.1.4
runtime.

Use `-Implementations gorti` to collect gorti-only scalability data when the
local Portico transport cannot complete the requested federation lifecycle.

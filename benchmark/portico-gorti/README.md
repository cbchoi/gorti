# Portico/gorti DEVStone-HLA benchmark

This runner applies a DEVStone-derived workload to the existing two-federate
Portico/gorti verifier. It is an HLA/RTI traffic comparison, not a DEVStone
score for a DEVS simulator kernel.

For every paired seed, the runner traverses the tracked DEVStone graph and
materializes one `DVSHLA1` binary plan. Both products read the same plan bytes.
With the tracked HI workload, one run projects 40,600 DEVStone atomic event
deliveries into:

- 40,600 `updateAttributeValues` calls and reflected callbacks; and
- 40,600 `sendInteraction` calls and received callbacks.

This mapping is named the **DEVStone-HLA paired OM projection**. The same FOM,
count, paired seed, callback boundary, and observable update-then-interaction
order are used for both implementations.

Two experiment contracts are available:

- `benchmark/environment/experiment.json` uses gorti's server-confirmed path.
- `benchmark/environment/experiment-local-lrc.json` uses gorti's bounded
  LocalLRC queue, a pipelined stream, a 1,024-operation capacity, cumulative
  ACKs every 32 operations, 32-operation frames, and a final drain.

Both contracts fix gorti's handle-oriented callback representation and server
outbox at 8,192 events, 32 events per internal batch, and a 1 ms partial-batch
flush bound. These values are passed explicitly to `rtid`, recorded in
`comparison.json`, and checked again before result publication.

The primary endpoint in both contracts is the subscriber's final validated
callback. Per-call return latency is diagnostic only: Portico returns at its
LRC service boundary, the confirmed gorti path waits for a server result, and
the LocalLRC path returns after bounded local admission. Those values do not
have the same completion guarantee and must not be used to rank the products.
For the LocalLRC profile, the decision rule is fixed before collection using
the gorti/Portico completed-batch ratio and a 10% practical margin. gorti is
faster when the upper 95% confidence limit is below `0.9091`; Portico is faster
when the lower limit is above `1.1`; an interval wholly inside those bounds is
practical equivalence. Other intervals are inconclusive.

## Preflight

The dry run validates the experiment and workload identity even when Portico,
Docker, Go, Java tools, output paths, or tracked artifact placeholders have not
yet been resolved:

```powershell
.\benchmark\portico-gorti\run.ps1 -DryRun
```

```sh
bash benchmark/portico-gorti/run.sh --dry-run
```

For a real run, place the Portico 2.1.4 Linux distribution under
`.tools/portico-extracted/portico-2.1.4`, or set `PORTICO_HOME` to another
directory inside the repository. Set `GORTI_BENCHMARK_OUTPUT` to a new
directory outside the repository. The tracked
`verification/commercial-rti/fom/CommercialRtiVerifier.xml` is used while the
configured DEVStone FOM path does not exist.

Every real run compiles the current Java verifier sources in its transient
workspace against the selected `PORTICO_HOME/lib/portico.jar`. Prebuilt
verifier overrides through `--verifier-jar` or `GORTI_VERIFIER_JAR` are
prohibited; a dry run reports either override as a failed runtime check. The
manifest records the verifier source and JAR hashes plus a deterministic build
input digest covering sorted source paths and contents, the Portico API JAR
hash, Java compiler version, and canonical compiler arguments.

```powershell
$env:PORTICO_HOME = 'D:\workspace\gorti\gorti\.tools\portico-extracted\portico-2.1.4'
$env:GORTI_BENCHMARK_OUTPUT = 'D:\benchmark-results\devstone-portico-gorti-001'
.\benchmark\portico-gorti\run.ps1
```

Run the LocalLRC profile with a different new output directory:

```powershell
.\benchmark\portico-gorti\run.ps1 `
  -Experiment benchmark\environment\experiment-local-lrc.json
```

```sh
export PORTICO_HOME="$PWD/.tools/portico-extracted/portico-2.1.4"
export GORTI_BENCHMARK_OUTPUT="$HOME/benchmark-results/devstone-portico-gorti-001"
bash benchmark/portico-gorti/run.sh
```

The comparator is invoked once with five warm-up pairs and 30 measured pairs.
Portico's JGroups lifecycle response timeout is fixed at 5000 ms. This
process-lifecycle control is outside the measured callback boundary and is recorded in
the result manifest. Before the Portico subscriber is launched, the comparator
also requires a transient readiness marker written only after the publisher has
joined the federation and registered its object. The marker is removed before
subscriber launch and is never retained. It is a startup control outside the
measurement boundary, not an execution log. The adapter accepts results only
when `protocol.portico_publisher_ready_gate` is exactly `true`; the manifest
records both startup controls and the marker lifecycle.

Portico teardown uses a three-phase transient handshake, in this exact order:

1. the subscriber resigns from the federation;
2. the publisher resigns from the federation; and
3. the subscriber disconnects from the RTI.

All three phases occur after the measured callback boundary. Their transient
control evidence is discarded after validation, is not retained in the result
directory, and is not an execution log. The adapter accepts results only when
`protocol.portico_ordered_teardown_gate` is exactly `true`. The manifest records
the ordered phases, the evidence policy, and the fixed 5000 ms JGroups
lifecycle response timeout.

Measured pair `i` uses `base_seed + i - 1`; warm-up pair `i` uses
`base_seed + measured_pair_count + i - 1`. Each successful record carries a
`pair_attempt` from 1 through 3. If a participant exits or times out after
verified cleanup, the complete pair may be retried with the same seed, plan,
and AB/BA order. Semantic, summary, plan, evidence, and cleanup failures abort
the experiment. Both successful records in a pair must report the same attempt,
and `discarded_pair_attempts` must equal the sum of `pair_attempt - 1` across
all 35 pairs.

The measurement contract is fixed at 128 untimed operations per channel,
sequential receive-order update then interaction choreography,
`HLA_IMMEDIATE` callbacks, excluded time management, and the tracked
`subscriber_timer_armed_before_VERIFY_START_release_to_final_callback_arrival`
timer boundary. gorti records must also attest that its server was alive before
shutdown, received an intentional shutdown request, exited, and was removed.
The subscriber also verifies one combined callback trace, so separate
attribute and interaction streams cannot pass after cross-channel reordering.
The adapter rejects anything other than 30 Portico runs, 30 gorti runs, 30
unique paired seeds, and a 15/15 AB/BA balance.

The output directory is published atomically and contains exactly:

- `manifest.json`
- `results.json`
- `analysis.json`
- `comparison.tex`

Process output and RTI event logs are never written. Per-participant compact
JSON summaries and binary plans exist only in the transient workspace and are
discarded after validation. The repository-local transient directory is
removed on success and failure. A dirty worktree is recorded and warned about;
it does not invalidate a run. The manifest retains only the replacement policy
and retry counts, never a failure transcript. It also records the generated
Portico transport override hash and the tracked builder, Portico JAR, and source
resource hashes used to construct it. A measured `results.json` run identifier
includes its accepted pair-attempt number.

## Tests

```powershell
python -m unittest discover -s benchmark\portico-gorti\tests -v
```

```sh
python3 -m unittest discover -s benchmark/portico-gorti/tests -v
```

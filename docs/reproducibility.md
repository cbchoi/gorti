# Reproducibility

A model rerun, a service test, and a table regenerated from retained reports
answer different questions. Choose the task before selecting commands.

| Task | Required inputs | Result |
|---|---|---|
| Rerun the Python-Go model | Checkout, Go, Python SDK, pyjevsim extra | New observed/reference trajectories |
| Rerun HLA service checks | Checkout, SDK toolchains, live-test fixtures and endpoints | New test execution reports |
| Regenerate requirement tables | Reconciled input, class manifest, retained reports | Aggregation of existing executions |
| Compare RTI performance | Matched workload, RTI installations, explicit measurement profile | New paired or crossover measurements |

## Rerun the model integration

Follow [model integration](model-integration.md) for environment setup. From
the repository root, after installing the Python extra and generating bindings:

=== "Linux"

    ```bash
    bash examples/multilanguage-queue/run.sh \
      --output /tmp/gorti-model-results \
      --jobs 100 --repeats 3 --seed 1516
    ```

=== "Windows PowerShell"

    ```powershell
    .\examples\multilanguage-queue\run.ps1 `
      --output D:\results\gorti-model-results `
      --jobs 100 --repeats 3 --seed 1516
    ```

The destination must be new. Retain the complete output, including inputs,
reference and observed trajectories, checks, process identities, and source
and binary hashes. The nine-run design checks deterministic model behavior;
it is not the Portico performance workload.

## Rerun service tests

Use the exact selections, build tags, fixtures, and environment settings in
the [Linux validation report](https://github.com/cbchoi/gorti/blob/main/engineering/verification/linux-publication-validation-20261019.md).
The [verification page](verification.md) summarizes its counts and skips.
Default developer commands do not enable every optional live-server case.

Record the checkout and any local changes before running. Keep test reports
with that source identity; a later documentation or aggregation date does not
turn an earlier service execution into a new campaign.

## Regenerate requirement tables

The repository contains the
[table exporter](https://github.com/cbchoi/gorti/blob/main/scripts/summarize_requirement_results.py),
its tests, and the
[reviewed class manifest](https://github.com/cbchoi/gorti/blob/main/engineering/verification/requirements2010-evidence-classes-20261023.json).
The reconciled execution input and original execution reports are retained
outside Git. A source checkout alone therefore does not contain every input
for regenerating the historical tables.

With a retained reproduction archive, extract it to a separate directory
and run its entry point from that directory:

```text
python reproduce_tables.py --output ../gorti-tables
```

The archive entry point requires a new destination and verifies the packaged
hashes, including the 74 linked execution reports. Python 3.11+ and its
standard library suffice. This command aggregates reviewed metadata; it
neither executes the RTI nor independently interprets the IEEE documents.
The repository does not currently identify a public download URL for that
archive.

Alternatively, use the exporter from the repository root with the retained
inputs. Substitute the path to an extracted archive for the example path:

=== "Linux"

    ```bash
    python scripts/summarize_requirement_results.py \
      --reconciled /path/to/archive/traceability/reconciled-input.json \
      --classes engineering/verification/requirements2010-evidence-classes-20261023.json \
      --artifact-root /path/to/archive \
      --output /tmp/gorti-requirement-tables
    ```

=== "Windows PowerShell"

    ```powershell
    python scripts/summarize_requirement_results.py `
      --reconciled D:\evidence\archive\traceability\reconciled-input.json `
      --classes engineering/verification/requirements2010-evidence-classes-20261023.json `
      --artifact-root D:\evidence\archive `
      --output D:\results\gorti-requirement-tables
    ```

The exporter checks classifications and, with `--artifact-root`, report
hashes. It writes `requirement-invocations.csv`, `requirement-status.csv`,
`requirements-summary.json`, and a separate `reproduction-checks.json`.
Use `--source-root` only with the matching assessed source to check linked
test-file hashes. Unlike the archive entry point, the direct exporter can
overwrite its output files; choose a fresh directory.

Its focused tests are:

```text
python scripts/tests/test_summarize_requirement_results.py
```

These tests exercise classification and aggregation, not HLA service behavior.

## Choose a performance experiment

| Runner | Design | Primary measurement |
|---|---|---|
| [Paired DEVStone-HLA](benchmark.md) | Five warm-up pairs, 30 measured AB/BA pairs | Receiver completion |
| [Callback-reporting comparison](https://github.com/cbchoi/gorti/blob/main/benchmark/callback-reporting/README.md) | Paired or crossover; October campaigns used five warm-up and 20 measured ABBA/BAAB blocks | Receiver completion with report accounting |
| [Service-level comparison](https://github.com/cbchoi/gorti/blob/main/verification/fair-comparison/README.md) | Service-specific sequential/parallel cases | Per-service latency and completed delivery |

Each crossover block contains two trials per RTI: 20 measured blocks yield
40 measurements per RTI. The [historical October report](https://github.com/cbchoi/gorti/blob/main/benchmark/callback-reporting/historical-results-20261007-08.md)
contains the retained conditions, identities, and result tables.
The [performance page](performance.md) explains the primary timing boundary
and interpretation.

## Shared comparison conditions

For each comparison, record:

- identical FOM bytes and SHA-256, workload inputs, and payload seed;
- two independent federate processes per RTI and the server topology;
- matching sequential or parallel choreography and delivery checks;
- the selected completion boundary, separate from API return or local admission;
- callback representation, logging, runtime profile, and final ACK/report drain;
- warm-up and measurement counts fixed before collection; and
- balanced AB/BA pairs or ABBA/BAAB blocks, with failures retained.

The October receiver timestamps are not identical code locations: Go records
event-pump dequeue before dispatch, while Java records callback entry before
payload validation. Both measure the last subsequently validated data receive;
final report drain is outside that interval. Preserve these definitions when
reproducing the historical comparison.

Validate counts, payload/order hashes, synchronization, terminal state, and
drain outcomes before accepting a run. Equal caller-return time does not imply
equal delivery semantics.

## Analyze and retain results

For a crossover design, resample whole four-trial seed blocks within ABBA/BAAB
order strata. The October analysis uses 10,000 bootstrap replicates and seed
1516 for 95% percentile intervals. Treating its individual trials as independent
pairs would change the uncertainty estimate.

At equal delivered counts, aggregate throughput is total callbacks divided by
total measured time. The gorti/Portico throughput ratio equals Portico/gorti
mean completion time for equal run counts. Do not substitute a mean of per-run
throughputs without labeling the different statistic.

Record the source commit and changes, executable hashes, RTI version, language
runtimes, CPU, operating system, power settings, FOM hash, and logging mode.
Keep the manifest and all structured trial results together, including failed
trials. The paired container runner does not retain process logs; other runners
have their own artifact policies described beside the code.

## Repository policy

Track source, FOMs, portable configurations, analysis code, and maintained
result summaries. Keep raw logs, binaries, machine-local launcher settings,
and generated experiment output outside Git. Licensed IEEE source documents
are not part of the repository.

Use [GitHub Issues](https://github.com/cbchoi/gorti/issues) to report a missing
reproduction input, including the command, source identity, and error.

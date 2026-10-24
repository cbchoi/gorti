# Reproducibility

| Task | Guide or input |
|---|---|
| Run the Python-Go model | [Model integration](model-integration.md) |
| Run service tests | [Verification](verification.md) |
| Regenerate requirement tables | Reconciled input, classification manifest, and execution reports |
| Compare RTI performance | [Benchmark](benchmark.md) and [performance results](performance.md) |

## Model and service runs

After [installing the model dependencies](model-integration.md), run from the
repository root:

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

Use a new output directory. Retain inputs, reference and observed trajectories,
checks, process identities, and source/binary hashes.

For service tests, match the selections, build tags, fixtures, and environment
in the [Linux validation report](https://github.com/cbchoi/gorti/blob/main/engineering/verification/linux-publication-validation-20261019.md).
Record the checkout, local changes, and execution date with each report.

## Requirement tables

The [exporter](https://github.com/cbchoi/gorti/blob/main/scripts/summarize_requirement_results.py)
aggregates reviewed metadata and existing execution results. It needs a
reconciled input and original reports, which are retained outside Git, plus
the tracked
[classification manifest](https://github.com/cbchoi/gorti/blob/main/engineering/verification/requirements2010-evidence-classes-20261023.json).
With those inputs, run:

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

Replace the input paths and use a fresh output directory; the exporter can
overwrite its output files. It checks classifications and report hashes,
then writes two CSV tables, a JSON summary, and `reproduction-checks.json`.
The optional `--source-root` checks test-file hashes against matching source.
It does not execute service tests.

## Performance designs

| Runner | Design | Primary measurement |
|---|---|---|
| [Paired DEVStone-HLA](benchmark.md) | Five warm-up pairs, 30 measured AB/BA pairs | Receiver completion |
| [Callback-reporting comparison](https://github.com/cbchoi/gorti/blob/main/benchmark/callback-reporting/README.md) | Paired or crossover; October runs used five warm-up and 20 measured ABBA/BAAB blocks | Receiver completion with report accounting |
| [Service comparison](https://github.com/cbchoi/gorti/blob/main/verification/fair-comparison/README.md) | Service-specific sequential/parallel cases | Service latency and completed delivery |

A crossover block runs each RTI twice. Twenty measured blocks therefore yield
40 measurements per RTI. Use the commands and binary identities in the
[October experiment record](https://github.com/cbchoi/gorti/blob/main/benchmark/callback-reporting/historical-results-20261007-08.md)
to reproduce that design.

## Comparison conditions

Record the same FOM bytes, workload, seed, process count, choreography, and
delivery checks for both RTIs. Also record runtime profiles, callback handling,
logging, completion boundaries, and final drains. Fix warm-up and measured-run
counts before collection and balance the execution order.

The October comparison timestamps Go event-pump dequeue before dispatch and
Java callback entry before payload validation. The final report drain is
outside the primary interval. See [performance](performance.md) for the
complete timing definition.

Accept a trial only after checking callback counts, payload/order hashes,
synchronization, terminal state, and drains. Retain failed trials.

## Analysis and records

For crossover experiments, resample whole four-trial seed blocks within
ABBA/BAAB strata. The October analysis uses 10,000 replicates and seed 1516
for 95% percentile intervals.

Aggregate throughput is total callbacks divided by total measured time.
At equal callback and run counts, the gorti/Portico throughput ratio equals
Portico/gorti mean completion time.

Retain source and executable hashes, RTI and language versions, CPU,
operating system, power settings, FOM hash, and logging mode with the results.
Store source, portable configurations, analysis code, and result summaries
in Git. Keep raw logs, binaries, machine-local settings, and generated output
outside it. Each runner documents its own output policy.

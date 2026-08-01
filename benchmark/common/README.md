# DEVStone-HLA benchmark analysis

This directory contains the format and analysis tools shared by the gorti and
Portico DEVStone-HLA benchmark runners. The runners are data producers; these tools
only consume structured JSON files. They never parse process output, standard
error, or event logs.

## Measurement contract

An input file must conform to `result-schema-v1.json` and the additional
cross-record checks implemented by `validate.py`. In particular, one document
contains:

- two implementations, ordered as baseline and candidate;
- exactly 30 measured runs for each implementation;
- 30 matched workload pairs with one fresh process per implementation;
- a unique seed for each pair and the same seed within a pair;
- 15 baseline-first and 15 candidate-first pairs; and
- one finite, non-negative scalar for every declared metric and run; and
- complete callback accounting with no rejected, dropped, unexpected, duplicate,
  or invalid callbacks;
- successful ready, start, measure, and done synchronization points;
- one static `workload_sha256` shared by all runs and a distinct
  `workload_instance_sha256` for each of the 30 paired seeds; and
- matching FOM, workload, callback-count, per-channel callback-order, and
  terminal-state evidence for the two implementations in every pair.

`attribute_callback_sha256` and `interaction_callback_sha256` preserve the
observed order within each callback channel. They are intentionally separate:
the relative interleaving of attribute and interaction callbacks is not part of
the DEVStone-HLA semantic contract. `terminal_state_sha256` covers the final
application-visible state after both channels complete.

Warm-up executions are described in experiment metadata but are not included
in the 60 measured runs. The schema intentionally has no fields for stdout,
stderr, or event-log content. A benchmark runner should write its completed
result document atomically.

## Commands

All tools use only the Python standard library. They require a file path for
input and never read JSON from a pipe.

```text
python benchmark/common/validate.py results.json
python benchmark/common/analyze.py results.json --output analysis.json
python benchmark/common/render_latex.py analysis.json --output comparison.tex
```

The analyzer reports median, p95, and p99 for each implementation. Every
reported percentile includes a deterministic percentile-bootstrap 95% CI.
The comparison is paired by workload seed and its bootstrap resampling is
stratified by execution order. It also reports an equal-weight, order-adjusted
estimate derived from the two AB/BA strata. The default is 10,000 bootstrap
resamples with seed 1516; both values are recorded in the analysis JSON.

The generated LaTeX uses `booktabs`, so the manuscript preamble must include:

```latex
\usepackage{booktabs}
```

## Tests

```text
python -m unittest discover -s benchmark/common/tests -v
```

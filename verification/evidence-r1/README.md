# Retained verification and benchmark evidence

The [evidence-r1 data release](https://github.com/cbchoi/gorti/releases/tag/evidence-r1)
provides `gorti-softwarex-evidence-r1.zip` as an attachment. It is separate from
the software 1.0.0 release and does not identify a new RTI implementation version.
Download the named evidence ZIP, not GitHub's automatically generated source ZIP.

Archive size: 72,380,707 bytes. SHA-256:

```text
f41baa119b20c2a90015c676684dfc37a022019ec52d99acf701c93d5dd37ed2
```

## Contents

| Evidence | Scope |
|---|---|
| Historical service tests | 75 retained report files; Go, Python, Java, and internal IVCT-inspired scenarios |
| Model integration | Nine runs, 900 jobs, 1,800 data callbacks |
| Performance | Two Portico/gorti sessions and a separate gorti client-build comparison; 300 trials including warm-up |
| Requirements mapping | Historical inventory, corrected inventory, and correction ledger; unresolved interpretations remain open |
| Provenance | Original/public hashes, path-redaction records, selected test sources, and recorded execution settings |

The archive has 1,979 files, including a manifest for 1,978 payload files. Local
paths and host metadata have been redacted in identified public copies. No RTI
binaries, JDK, dependency cache, IEEE PDFs, manuscript/reviewer PDFs, credentials,
student records, or private redaction map are included.

Its preparation-time README and rights note say "publication candidate" and
"not published". Those statements describe the preserved archive before the
author authorized publication. The data release records the subsequent deposit;
the approved archive bytes have not been rewritten to change those labels.
Existing software and third-party notices remain applicable. This release does
not assert a new blanket license over third-party material or IEEE standards.

## Recalculate the results

Extract the ZIP into a new directory. From the extracted
`gorti-softwarex-evidence-r1` folder, use Python 3.12+ and the standard library:

```text
python -B verify_package.py
python -B reanalyze.py --output reanalysis-output
```

Python 3.14.0 was used for the recorded check. Use a new output directory.
Neither command runs an RTI or downloads dependencies. The reanalysis recounts
test reports, checks model trajectories, recalculates the three performance
comparisons, and checks the mapping records. The output is `results.json` and
regenerated historical mapping tables. A failed hash or numerical comparison
returns a nonzero exit status.

For source inspection without downloading all raw reports, [`package/`](package/)
contains byte-identical copies of the archive's two entry-point scripts, retained
estimator, and each performance session's settings, schedule, workload, and
summary. These are selected files, not a complete extracted package. Run the
commands above from the full extraction; the scripts deliberately resolve
their inputs relative to that package root.

## Reading the results

Service suites overlap and must not be summed into a count of IEEE requirements.
A passing test establishes its assertions, not all conditions in a linked clause.
The corrected mapping retains 25 unresolved records and two applicable records
without supporting links; it is not a conformance certificate.

The two RTI sessions give different orderings. Their gorti/Portico throughput
ratios are 0.791331 and 1.607678, with within-session 95% intervals
[0.581295, 1.045097] and [1.319672, 1.924452]. The separate client-build comparison
gives a 4.88% difference [1.38%, 8.84%]. Complete source snapshots for the measured
modified builds are not retained; no single-change causal attribution follows.
See the [performance methods](../../benchmark/callback-reporting/historical-results-20261007-08.md).

The later [Portico Java service-test transfer](../cross-rti/README.md) is a
different experiment with its own reports. It is not part of this historical ZIP.

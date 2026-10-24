# Historical receive-order comparison: 7-8 October 2026

This maintained methods/result summary distinguishes historical performance
builds from the later RTI/SDK publication commit `4b11da4`. Original execution
artifacts remain outside Git. It consolidates the
[7 October report](../../engineering/verification/portico-fom-static-metadata-20261007.md)
and [8 October report](../../engineering/verification/portico-report-signal-20261008.md).

## Matched workload and measurement

Both campaigns used Windows 11 build 26300, an Intel Core i7-12700 with 20 logical
CPUs, and loopback TCP. Portico 2.1.4 ran Java federates on Temurin OpenJDK
11.0.31+11 with JGroups TCP/TCPPING discovery. gorti used Go 1.26.5,
`callback_async`, LocalLRC batches of 32, and default `GOMAXPROCS`.

The DEVStone-derived HLA HI workload had width/depth 10 and 100 external events:
40,600 reflections and 40,600 interactions per trial. FOM bytes, per-seed binary
plans, payloads, choreography and callback checks matched. This is an HLA
exchange workload, not a DEVS kernel score. Each RTI used two independent
federates; gorti additionally used its server. No per-message benchmark logs
were written; Portico logging was OFF and gorti server logging was error-level.

Five warm-up and twenty measured four-trial seed blocks used balanced ABBA/BAAB
orders randomized with seeds 20261007 and 20261008. Each RTI therefore had 50
trials per campaign, of which 40 were measured. Each fresh process also performed
128 warm-up operations per channel. All 100 trials per campaign passed, without
retries or exclusions. No tests, builds or profiling overlapped timed trials.

The primary interval starts at receiver arming before `VERIFY_START` and ends at
the final subsequently validated data-receive timestamp. Go timestamps at event
pump dequeue before `DispatchEvent`; Java timestamps at callback entry before
payload validation. Final validation, teardown and gorti report-ACK drain must
succeed, but drain is outside the timed interval. The endpoint is received-data
completion, not equal API-return or fully settled completion latency.

Aggregate throughput is total received callbacks divided by total measured
seconds, not the mean of individual rates. Percentile intervals use 10,000
bootstrap replicates of the twenty complete seed blocks, stratified by ABBA/BAAB,
with bootstrap seed 1516. Fresh JVMs have operation-count rather than long-running
steady-state warm-up, and host background activity was not isolated.

## Results

| Date | Portico mean s | gorti mean s | Portico callbacks/s | gorti callbacks/s | gorti/Portico ratio [95% CI] |
|---|---:|---:|---:|---:|---|
| 7 October | 0.920 | 1.162 | 88,284 | 69,862 | 0.791331 [0.581295, 1.045097] |
| 8 October | 2.272 | 1.413 | 35,742 | 57,462 | 1.607678 [1.319672, 1.924452] |

gorti was faster in 17 of 20 seed-block means on 8 October. These are
within-session intervals. Portico's JAR, Java runtime, all 50 Java classes, FOM,
and gorti server were unchanged across sessions; the Go SDK changed. Session and
fresh-JVM/host variation prevent attributing the rank reversal to that change
alone. The result is not a current-version, multi-host, time-management or
scalability ranking. Portico was not run against the complete gorti test inventory.

## Binary identity

| Artifact | SHA-256 |
|---|---|
| gorti asynchronous server, both campaigns | `25fa04118532a7638d64df9e9228879c4aa8777e052863e4a62f9b2b37cd3955` |
| Go SDK/verifier before lazy signals | `a13ece3f3e6b987a99efa7aa40b90d6006171b1ef63fb7c2fe8d91199da876cd` |
| Go SDK/verifier after lazy signals | `a93ae8c11cfa68262b1a1ac0f33a4def81af91689e7e8cf124a5b7708e5d01a1` |
| Portico JAR | `146dc0a7916b168396c6e595c42aaed0bdc69d64c5c54e728b7cb53fdd94ea84` |
| FOM | `633aaa61984966237f3675bcf005a5fb3722392f2c113ef41ca8b255e1a15040` |

The linked reports identify the external manifests, raw observations, seed
schedules and analyses. Do not replace these identities with the later source
commit when reproducing or citing a result.

## Isolated SDK optimization

A separate 8 October same-server experiment compared lazy callback-report
signals before/after the SDK change. Five warm-up and twenty measured ABBA/BAAB
blocks produced 40 measured trials per build; all 100 total trials passed.

| Metric | Before | After |
|---|---:|---:|
| Aggregate callbacks/s | 53,055.31 | 55,645.02 |
| Mean completion, seconds | 1.530478 | 1.459250 |
| Task-registration allocation, bytes | 288 | 64 |
| Task-registration allocations | 3 | 1 |

Throughput increased 4.88%, with ratio interval [1.013765, 1.088442]. Allocation
values describe task registration, not whole-process memory. Both builds used
the same server binary and retained callback reporting. This isolated comparison
is separate from the cross-RTI difference. Its manifest/analysis is identified
by the `report-signal-crossover-20261008` evidence root in the 8 October report.

The model/requirements reproduction archive contains this summary, not these
historical benchmark observations. Preserve and identify that historical data
archive separately. Source and experiment methods belong in Git; execution logs
and raw measurement artifacts do not.

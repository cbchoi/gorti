# Verification result

Run date: 2026-07-12 (Asia/Seoul)

Configuration:

- seed: `1516`
- timestamped attribute updates: `100`
- timestamped interactions: `100`
- logical-time grants per federate: `1..101`
- lookahead: `1`
- Ralph maximum iterations: `1`

Result:

- Pitch pRTI Java process: pass
- gorti Python process: pass
- FM/DM/OM/TM service-use gate: pass for both implementations
- canonical semantic event count: 4 versus 4
- canonical SHA-256: `6278ffeca201c603555f140416529c299bdc1ae7ac9c5590bed33a0a9b3747fd`
- semantic mismatches: 0
- Ralph decision: `complete` after one iteration

Performance snapshot:

| Metric | Pitch | gorti | Unit |
|---|---:|---:|---|
| Attribute update call mean | 0.247309 | 6.633987 | ms |
| Attribute update call p95 | 0.357600 | 16.632000 | ms |
| Interaction send call mean | 0.137542 | 4.767820 | ms |
| Interaction send call p95 | 0.224100 | 17.857600 | ms |
| Completed delivery batch mean | 1.007098 | 23.043308 | ms |
| Completed delivery batch p95 | 1.094400 | 71.365300 | ms |
| Sustained throughput | 70.393622 | 26.417577 | deliveries/s |

These performance numbers are one local snapshot, not a statistical product
benchmark. They are reported separately and never influence semantic pass or
fail. For stable performance claims, add warmup runs, alternate execution
order, and aggregate multiple measured repetitions.

Detailed local artifacts are under `verification/out/ralph-live-100/`.

## Claim-grade paired Pitch/Go result

The previous one-Pitch-run versus twenty-Go-run table is superseded. The final
session applied the same FOM bytes, seed, two-process topology, choreography,
timing boundary, callback mode, and file server logging to both arms. It used
five warm-up pairs followed by twenty measured AB/BA pairs.

- valid arms: 50/50
- measured order: 10 AB, 10 BA
- accounting: Pitch 4,000/4,000, Go 4,000/4,000
- rejected/dropped/duplicate/invalid: zero for both
- shared semantic projection SHA-256:
  `0f148007c8c8e394c398ca05636afda672938a80f7c15b2ed9f25ac4a7da6c42`

| Metric | Pitch median | Go median | paired Go/Pitch | paired 95% CI |
|---|---:|---:|---:|---:|
| Completed delivery batch | 0.6186 ms | 0.6014 ms | 0.973x | 0.957..0.997x |
| Attribute update call | 0.2198 ms | 0.1879 ms | 0.834x | 0.818..0.867x |
| Interaction send call | 0.1165 ms | 0.1911 ms | 1.612x | 1.548..1.657x |
| Time advance request | 0.1784 ms | 0.1717 ms | 0.958x | 0.940..0.984x |

gorti is Pitch-class on the completed-delivery median and faster for update
and TAR admission, but interaction-send admission remains materially slower.
The result does not support the withdrawn 73.8% overall-win claim.

Detailed evidence:
`verification/out/fair-comparison/claim-file-1516-v5-strict-tar/analysis.json`.

## Final persistent-lifecycle rerun

The v5 table above is superseded for interaction optimization. The final v12
session reused one RTI server per implementation across all arms, strictly
alternated AB and BA orders, and recorded both Go RTID and client hashes.

- valid arms: 50/50
- measured order: 10 AB, 10 BA, strictly alternating
- accounting: Pitch 4,000/4,000, Go 4,000/4,000
- rejected/dropped/duplicate/invalid: zero for both
- semantic projection SHA-256:
  `0f148007c8c8e394c398ca05636afda672938a80f7c15b2ed9f25ac4a7da6c42`

| Metric | Pitch median | Go median | paired Go/Pitch | paired 95% CI |
|---|---:|---:|---:|---:|
| Completed delivery batch | 0.7578 ms | 0.7342 ms | 0.954x | 0.782..1.016x |
| Attribute update call | 0.2249 ms | 0.2484 ms | 1.105x | 1.018..1.165x |
| Interaction send call | 0.1292 ms | 0.1626 ms | 1.290x | 1.209..1.312x |
| Time advance request | 0.1989 ms | 0.2418 ms | 1.216x | 1.142..1.299x |

Interaction improved materially from v5 but remains above the provisional
1.25x upper-CI gate. TAR p95 also fails its provisional gate at
`3.352x (2.486..4.514)`. Completed delivery's observed ratio is close and its
CI includes 1.0, but no formal equivalence claim is made. The Go process is
attested as persistent; the external Pitch endpoint PID and start time are not
captured. Detailed evidence:
`verification/out/fair-comparison/claim-file-1516-v12-final/analysis.json`.

## Accepted v14 claim-grade baseline

The v14 persistent-lifecycle session supersedes v12 and is the release
decision baseline. It retained the same FOM SHA-256, seed 1516, two independent
federate processes, sequential choreography, immediate callbacks, file server
logging, five warmup pairs, and twenty balanced alternating AB/BA measured
pairs. All 4,000 expected deliveries per implementation were observed with
zero rejection, drop, duplicate, or invalid records.

| Metric | Pitch median | Go median | paired Go/Pitch | paired 95% CI |
|---|---:|---:|---:|---:|
| Interaction send call | 121.275 us | 165.600 us | 1.218x | 1.120..1.327x |
| Attribute update call | 215.400 us | 212.700 us | 0.902x | 0.811..0.972x |
| Time advance request | 184.375 us | 219.475 us | 1.089x | 0.826..1.169x |
| Completed delivery batch | 726.525 us | 650.700 us | 0.871x | 0.715..0.941x |

The synchronous interaction caller boundary remains about 22% behind Pitch.
The completed-delivery boundary and paired object-update result favor gorti;
the TAR confidence interval includes parity. These values describe this fixed
machine and workload, not a general vendor ranking.

The local evidence is under
`verification/out/fair-comparison/claim-persistent-1516-v14-20260713/`. That
directory is intentionally ignored because it contains large raw logs and
machine-specific manifests. Before SoftwareX submission, publish the
permissible logs, manifests, checksums, and `analysis.json` in an immutable
research archive and add its DOI to the manuscript and citation metadata.

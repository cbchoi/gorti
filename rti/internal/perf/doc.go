// Package perf implements the M5 performance baseline harness.
//
// Per docs/srs.md §10.2 M5 exit criterion: reproducible perf measurements
// at federation sizes 2, 5, 25, 100. Output is JSON for downstream tooling.
//
// The harness is split:
//
//   - Manager (this package, baseline.go): pure measurement logic. Spins up
//     N federates against a real *rtid* server, drives a fixed workload,
//     records throughput + p50/p99 latency.
//
//   - Runner (examples/go-pingpong/perf_main.go, build tag `perf`): wraps
//     the Manager in a CLI binary so the perf run is reproducible from the
//     repo without test machinery. Records JSON output to stdout.
//
// JSON output schema (frozen at TASK-079; downstream agents read it):
//
//	{
//	  "schema_version": 1,
//	  "federation_size": <int>,
//	  "duration_seconds": <float>,
//	  "interactions_sent": <int>,
//	  "throughput_per_second": <float>,
//	  "latency_p50_ms": <float>,
//	  "latency_p99_ms": <float>,
//	  "notes": <string>
//	}
//
// FROZEN-shape: the Manager interface + JSON schema are M5 contract.
// Agent A implements the body in TASK-079; Agent B reads the JSON output
// in TASK-084 to make the conditional benchmark decision.
package perf

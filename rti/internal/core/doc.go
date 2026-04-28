// Package core defines the FROZEN interfaces and types that the RTI's
// internal services implement and depend on. Only the orchestrator may edit
// files in this package; agents work against these interfaces.
//
// Frozen contract: any modification triggers the contract-change-request
// workflow per docs/WORKFLOW.md §4.2.
//
// Concurrency: callers may invoke implementations concurrently; per-federation
// serialization happens inside each implementation (see docs/sdd.md §1.4).
package core

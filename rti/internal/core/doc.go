// Package core defines the stable interfaces and types that the RTI's
// internal services implement and depend on. Contract changes require review
// because all service packages compile against these interfaces.
//
// Frozen contract: any modification triggers the contract-change-request
// workflow per docs/WORKFLOW.md §4.2.
//
// Concurrency: callers may invoke implementations concurrently; per-federation
// serialization happens inside each implementation (see docs/sdd.md §1.4).
package core

// Package sync implements IEEE 1516.1-2010 §4.6-4.7 Synchronization
// Management — sync points (registerFederationSynchronizationPoint /
// synchronizationPointAchieved + announce/synchronized callbacks).
//
// M8 deliverable. FROZEN-shape per docs/srs.md FR-SYN-1..4.
//
// Per-(label, federate) achievement state is recorded in the event log
// so replay reproduces announce/achieve order byte-identically.
package sync

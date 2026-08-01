// Package m9spec contains the specification tests
// for milestone M9 — Federation save/restore.
// See docs/srs.md §10.3 (cut 2) for the milestone gate.
//
// Per FR-SR-1..5: requestFederationSave + initiateFederateSave
// aggregation + federationSaved emission. Save bundle written to
// pluggable Storage backend. Restore replays the bundle's event log
// to byte-identical state (FR-SR-5; same machinery as M2/M3 NFR-DET-2).
//
// These tests preserve the observable M9 contract.
package m9spec

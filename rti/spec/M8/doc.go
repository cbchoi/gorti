// Package m8spec contains the specification tests
// for milestone M8 — Synchronization Management + Ownership Management.
// See docs/srs.md §10.3 for the milestone gate.
//
// M8 brings cut-2 to two service groups not implemented in cut-1:
//
//   - Sync points (FR-SYN-1..4): registerFederationSynchronizationPoint,
//     synchronizationPointAchieved, announceSynchronizationPoint,
//     federationSynchronized.
//   - Ownership (FR-OWN-1..6): negotiated divest + acquire two-phase
//     protocol, cancel either side, divestIfWanted, queryOwnership.
//
// New tests may extend this package, but existing assertions must not be
// weakened or deleted.
package m8spec

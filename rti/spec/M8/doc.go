// Package m8spec contains the orchestrator-frozen specification tests
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
// Per docs/TDD.md §5, these tests are RED before the milestone is
// dispatched. Agent A turns them green in M8 W1.
//
// Agents may ADD tests here but must NEVER weaken or delete existing
// assertions.
package m8spec

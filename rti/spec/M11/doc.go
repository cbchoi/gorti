// Package m11spec contains the specification tests
// for milestone M11 — MOM runtime (HLAfederate / HLAfederation
// reflection per IEEE 1516-2010 §10).
//
// See docs/srs.md §10.3 (cut 2) for the milestone gate.
//
// M11 wires the runtime side of the Management Object Model: the
// standard MIM (already parsed in M1) declares HLAmanager.HLAfederate
// and HLAmanager.HLAfederation as object classes; M11 registers per-
// federate / per-federation MOM instances and updates their attributes
// on lifecycle events.
//
// Spec test scope:
//   - HLAfederation lifecycle: CreateFederation → MOM instance exists
//   - HLAfederate lifecycle: JoinFederation → MOM instance exists, with
//     name + handle attributes populated
//   - Time-state attributes update on EnableRegulation / EnableConstrained
//   - Federate counters increment on send_interaction / update_attributes
//   - Resign removes the HLAfederate instance from HLAfederation.HLAfederatesInFederation
//
// Not exercised at M11: subscribe-via-standard-pub/sub (the standard
// API path that real federates use). M11 uses Manager.QueryX accessors
// to verify state; the pub/sub round-trip is implicit by virtue of
// using the same outbox the cut-1 object code uses.
//
// These tests preserve the observable M11 contract.
package m11spec

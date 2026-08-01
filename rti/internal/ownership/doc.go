// Package ownership implements IEEE 1516.1-2010 §7 Ownership Management.
//
// M8 deliverable. FROZEN-shape per docs/srs.md FR-OWN-1..6.
//
// Cut 1 already had unconditional divest via federation resign. M8 adds:
//
//   - Negotiated divest + acquire: two-phase protocol with RTI as broker
//     (FR-OWN-2). Federate A calls negotiatedDivest → RTI announces
//     requestAttributeOwnershipAssumption to subscribers → some federate B
//     calls attributeOwnershipAcquisition → RTI confirms transfer to both
//     parties.
//   - Cancel of either side of the negotiation (FR-OWN-3).
//   - DivestitureIfWanted (FR-OWN-4) — opportunistic.
//   - Query: queryAttributeOwnership + isAttributeOwnedByFederate (FR-OWN-5).
//
// All transitions recorded in event log; replay byte-identical (FR-OWN-6).
package ownership

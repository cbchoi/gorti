// Package savepoint implements IEEE 1516.1-2010 §4.8-4.12 Federation
// Save/Restore.
//
// M9 deliverable. FROZEN-shape per docs/srs.md FR-SR-1..5.
//
// Save protocol:
//
//  1. Some federate calls requestFederationSave(label[, time]). The
//     RTI broadcasts initiateFederateSave to ALL joined federates at a
//     synchronization point.
//  2. Each federate eventually calls federateSaveComplete() or
//     federateSaveNotComplete(). The RTI aggregates responses.
//  3. When all responses are in, the RTI emits federationSaved (or
//     federationNotSaved if any federate reported failure).
//
// Save artifact format (FR-SR-4): a sealed bundle (tar.gz) of:
//
//	(a) FOM modules (XML files used at federation create)
//	(b) Federation manifest: federates joined, declarations, registered
//	    objects, attribute ownerships, sync-point state, MOM snapshot
//	(c) The event log up to the save point
//
// Restore protocol (FR-SR-3):
//  1. Federate calls requestFederationRestore(label).
//  2. RTI loads the save bundle, replays the event log to reconstruct
//     state, broadcasts initiateFederateRestore to all federates.
//  3. Each federate calls federateRestoreComplete().
//  4. RTI emits federationRestored.
//
// Restore byte-determinism (FR-SR-5): the restored state IS the saved
// state — replay is the same machinery as M2/M3 NFR-DET-2 replay.
//
// Spec test contract: rti/spec/M9/.
package savepoint

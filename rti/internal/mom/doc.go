// Package mom implements the runtime side of the IEEE 1516-2010
// Management Object Model (MOM) — federates can subscribe to MOM
// attributes via the standard pub/sub APIs to introspect federation
// state.
//
// M11 deliverable. FROZEN-shape per docs/srs.md FR-MOM-1..3.
//
// Cut-2 scope (FR-MOM-1, FR-MOM-2): the read-only MOM. Standard MIM
// already declares HLAmanager.HLAfederate and HLAmanager.HLAfederation
// as object classes (parsed in M1 / FR-FOM-2). M11 wires the runtime
// side: the RTI registers per-federate / per-federation MOM instances
// and updates their attributes on lifecycle events (federate join /
// resign, attribute publish / subscribe, sync-point register / achieve,
// ownership transfer).
//
// Out of scope at M11 (deferred to cut 3 per FR-MOM-3): MOM-driven
// control services — interactions like HLAsetSwitches / HLArequestFederationSave
// invoked AS interactions to control the RTI.
//
// Spec test contract: rti/spec/M11/.
package mom

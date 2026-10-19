// Package savepoint coordinates immediate and bounded scheduled checkpoints,
// participant reports, snapshot bundles and same-roster restoration. It does not
// establish complete IEEE 1516-2010 Save/Restore conformance.
//
// A nil save time starts immediate initiation. A non-nil time uses the optional
// ScheduledTime dependency. The production time coordinator implements a
// restrictive target-ceiling profile: a finite nonnegative target, at least one
// time-managed participant, and positive effective lookahead for each regulator.
// All captured time-managed participants must have been granted exactly the
// target, with no pending advance, no queued TSO at/below it, and GALT strictly
// above it before initiation. Untimed members join at that common barrier.
// Advancing wall time cannot satisfy readiness.
//
// This profile rejects new advance requests beyond the ceiling and reservations
// that would conflict with an existing beyond-target request. It does not retain
// and later resume such a request, does not emit intermediate synthetic grants,
// and does not implement the complete timed-save pending-request sequence.
// Zero or effectively-zero regulator lookahead is rejected, not left in a
// predictable deadlock. Roster, generation, FOM or time-mode changes invalidate a
// waiting reservation. A participant that simply does not advance leaves it
// pending until cancellation or another explicit failure. A second save request
// is rejected while a reservation exists, rather than replacing it.
//
// Waiting schedules permit ordinary services. Actual initiation fences them
// through checkpoint admission. The legacy label-level state remains Initiated
// during reservation; participant status remains NoSaveInProgress until the
// callbacks are issued. Participant SaveBegun is required before completion.
// Abort, failure, completion and destruction release the time reservation.
//
// Production progresses pending reservations only after releasing the service
// and membership leases, then takes exclusive checkpoint admission, the protocol
// lock and the time evaluator/state locks in that order. Direct in-process
// integrations must use the same admission and call ProgressScheduledSave after
// logical-time or lifecycle work. There is no background save timer.
//
// Production bundles contain configured RTI snapshot state and provenance, not
// an event-log replay. RequiredSnapshots enables prepared same-roster restore;
// identities are not rebound. Unread transport frames and client callback queues
// are not checkpointed, and no restored transport epoch is provided. Federates
// remain responsible for their own application state and cooperative draining.
//
// Successful-save history is current-execution metadata, separate from saved
// state. The pinned official MIM (standard-mim.xml, HLAlastSaveName and
// HLAlastSaveTime) conditions updates on successful Federation Saved invocation.
// This implementation linearizes that coordinator decision after bundle
// persistence and full success-fanout reservation, before guaranteed callback
// publication. It does not establish that any SDK invoked the callback. A crash
// after the durable decision can leave notifications undelivered; a crash before
// it can leave an unproven bundle, which never supplies history by itself.
// Unproven crash-left bundles remain readable snapshots and keep their labels
// occupied; they are not automatically deleted on restart. A reported history
// write failure can discard its just-written candidate for same-label retry
// when the bundle adapter supports UncommittedSaveDiscarder.
//
// Options.HistoryStore is optional. Production uses a versioned completion
// journal and a ReservableOutbox. Missing legacy history stays unknown. Explicit
// fresh execution initialization establishes known no-save-yet; failure must
// abort or roll back the owning creation lifecycle before exposing success.
// Initialization does not roll back federation or FOM state itself. Its caller
// pins the new generation, and must never recycle a generation after failed
// creation. Existing execution recovery does not create missing evidence.
//
// Without a store, embeddings retain process-local history only. Non-reservable
// embeddings update it only after every fallible successful callback Send has
// returned nil. Delivery failure is returned and preserves previous history.
// A new manager cannot reconstruct that volatile evidence from bundle metadata.
//
// Successful, failed and cross-generation restores preserve target execution
// history; they never rewind it to saved precompletion state or copy the source
// bundle label. This selected policy awaits full normative review. No manager
// snapshot entry is required for history, so optional participants are unchanged.
//
// MOMMetrics keeps LastSaveHistoryKnown and LastSaveExists separate from label
// and optional logical time. Empty labels are valid existing service inputs;
// label emptiness does not imply no save. A nil time is legitimately undefined
// for an immediate save or known no-save-yet. A zero logical time is defined.
// Wire adapters must map these states explicitly, not turn unknown into a known
// empty string or an invented zero timestamp. No protocol schema is owned here.
package savepoint

//go:build dds

// Package dds — Participant interface + Phase 1a stub.
//
// Phase 1b lands the real Cyclone DDS-backed implementation in
// cgo_dds.go (see doc.go for the phase split). The Phase 1a stub
// returns errors.ErrUnsupported on every method so the build-tagged
// `rtid-dds` binary compiles cleanly even on developer machines that
// don't yet have libcyclonedds-dev installed.

package dds

import (
	"errors"
)

// Participant is the gorti-side handle for a Cyclone DDS
// DomainParticipant. Each federate that runs in DDS mode owns one
// Participant scoped to a single federation; the federation's
// dds_domain_id (echoed via JoinFederationResponse) maps onto the
// DDS domain ID the underlying participant joins.
//
// Lifecycle: Join → CreateTopic per (interaction class | (object
// class, attribute)) → Close. The Phase 1b implementation owns the
// Cyclone DDS dds_entity_t handle; Phase 1a's stub stores nothing.
type Participant interface {
	// Join attaches the participant to the given DDS domain. Returns
	// errors.ErrUnsupported in the Phase 1a stub.
	Join(domainID int) error

	// CreateTopic creates a topic in the participant's domain with
	// the given name + QoS. Phase 1a stub returns ErrUnsupported.
	CreateTopic(name string, qos QoS) (Topic, error)

	// Close releases the participant + every topic/writer/reader
	// the participant owns. Idempotent. Phase 1a stub returns
	// ErrUnsupported on first call.
	Close() error
}

// NewParticipant returns a Phase 1a stub Participant. Phase 1b
// replaces the body with a CGo-backed constructor.
//
// The constructor signature stays the same across phases so callers
// (the federation runtime in Phase 2) don't change when the C
// interop lands.
func NewParticipant() Participant {
	return &defaultParticipant{}
}

// defaultParticipant is the Phase 1a stub. Every method returns
// errors.ErrUnsupported. Phase 1b grows this struct with a
// dds_entity_t handle and threads through real C calls.
type defaultParticipant struct{}

func (*defaultParticipant) Join(int) error {
	return errors.ErrUnsupported
}

func (*defaultParticipant) CreateTopic(string, QoS) (Topic, error) {
	return nil, errors.ErrUnsupported
}

func (*defaultParticipant) Close() error {
	return errors.ErrUnsupported
}

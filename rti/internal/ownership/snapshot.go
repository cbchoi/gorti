package ownership

import (
	"fmt"
	"slices"

	"google.golang.org/protobuf/proto"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// Marshal serializes the federation's ownership state into an
// OwnershipManagerState proto and returns its wire bytes. M13 thread C
// (docs/srs.md §10.4): consumed by the savepoint Manager to bundle
// state under the manifest's "ownership" key.
//
// Returns nil for federations the manager has never seen — Unmarshal
// treats nil/empty as no-op so round-trip is the identity.
//
// Iteration is sorted by (object, attribute) — and acquirer for
// pendingAcquires — so the encoded bytes are deterministic.
func (m *Manager) Marshal(fed core.FederationName) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return nil, nil
	}
	out := &rtiv1.OwnershipManagerState{
		Owners:          ownersToProto(st.owners),
		PendingDivests:  pendingDivestsToProto(st.pendingDivests),
		PendingAcquires: pendingAcquiresToProto(st.pendingAcquires),
	}
	return proto.Marshal(out)
}

// Unmarshal restores the federation's ownership state from
// wire-format bytes. M13 thread C: invoked by the savepoint Manager
// during RequestFederationRestore before the event-log replay runs.
//
// Empty/nil input is a no-op. The manager replaces any pre-existing
// state for fed with the bundled set; the production restore-bootstrap
// path constructs a fresh manager so this is just an idempotent
// rewrite.
func (m *Manager) Unmarshal(fed core.FederationName, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	var pb rtiv1.OwnershipManagerState
	if err := proto.Unmarshal(data, &pb); err != nil {
		return fmt.Errorf("ownership: unmarshal OwnershipManagerState for %q: %w", fed, err)
	}
	st := newFederationState()
	for _, e := range pb.GetOwners() {
		if e == nil {
			continue
		}
		st.owners[ownershipKey{
			obj:  core.ObjectHandle(e.GetObjectHandle()),
			attr: core.AttributeHandle(e.GetAttributeHandle()),
		}] = ownershipRecord{owner: core.FederateHandle(e.GetOwnerFederate())}
	}
	for _, e := range pb.GetPendingDivests() {
		if e == nil {
			continue
		}
		st.pendingDivests[ownershipKey{
			obj:  core.ObjectHandle(e.GetObjectHandle()),
			attr: core.AttributeHandle(e.GetAttributeHandle()),
		}] = pendingDivest{
			owner: core.FederateHandle(e.GetOwnerFederate()),
			tag:   append([]byte(nil), e.GetTag()...),
		}
	}
	for _, e := range pb.GetPendingAcquires() {
		if e == nil {
			continue
		}
		st.pendingAcquires[acquireKey{
			obj:      core.ObjectHandle(e.GetObjectHandle()),
			attr:     core.AttributeHandle(e.GetAttributeHandle()),
			acquirer: core.FederateHandle(e.GetAcquirerFederate()),
		}] = pendingAcquire{tag: append([]byte(nil), e.GetTag()...)}
	}
	m.mu.Lock()
	m.fed[fed] = st
	m.mu.Unlock()
	return nil
}

func ownersToProto(owners map[ownershipKey]ownershipRecord) []*rtiv1.OwnershipRecordEntry {
	out := make([]*rtiv1.OwnershipRecordEntry, 0, len(owners))
	for k, v := range owners {
		out = append(out, &rtiv1.OwnershipRecordEntry{
			ObjectHandle:    uint64(k.obj),
			AttributeHandle: uint64(k.attr),
			OwnerFederate:   uint64(v.owner),
		})
	}
	slices.SortFunc(out, func(a, b *rtiv1.OwnershipRecordEntry) int {
		if a.GetObjectHandle() != b.GetObjectHandle() {
			return cmpUint64(a.GetObjectHandle(), b.GetObjectHandle())
		}
		return cmpUint64(a.GetAttributeHandle(), b.GetAttributeHandle())
	})
	return out
}

func pendingDivestsToProto(pd map[ownershipKey]pendingDivest) []*rtiv1.PendingDivestEntry {
	out := make([]*rtiv1.PendingDivestEntry, 0, len(pd))
	for k, v := range pd {
		out = append(out, &rtiv1.PendingDivestEntry{
			ObjectHandle:    uint64(k.obj),
			AttributeHandle: uint64(k.attr),
			OwnerFederate:   uint64(v.owner),
			Tag:             append([]byte(nil), v.tag...),
		})
	}
	slices.SortFunc(out, func(a, b *rtiv1.PendingDivestEntry) int {
		if a.GetObjectHandle() != b.GetObjectHandle() {
			return cmpUint64(a.GetObjectHandle(), b.GetObjectHandle())
		}
		return cmpUint64(a.GetAttributeHandle(), b.GetAttributeHandle())
	})
	return out
}

func pendingAcquiresToProto(pa map[acquireKey]pendingAcquire) []*rtiv1.PendingAcquireEntry {
	out := make([]*rtiv1.PendingAcquireEntry, 0, len(pa))
	for k, v := range pa {
		out = append(out, &rtiv1.PendingAcquireEntry{
			ObjectHandle:     uint64(k.obj),
			AttributeHandle:  uint64(k.attr),
			AcquirerFederate: uint64(k.acquirer),
			Tag:              append([]byte(nil), v.tag...),
		})
	}
	slices.SortFunc(out, func(a, b *rtiv1.PendingAcquireEntry) int {
		if a.GetObjectHandle() != b.GetObjectHandle() {
			return cmpUint64(a.GetObjectHandle(), b.GetObjectHandle())
		}
		if a.GetAttributeHandle() != b.GetAttributeHandle() {
			return cmpUint64(a.GetAttributeHandle(), b.GetAttributeHandle())
		}
		return cmpUint64(a.GetAcquirerFederate(), b.GetAcquirerFederate())
	})
	return out
}

func cmpUint64(a, b uint64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

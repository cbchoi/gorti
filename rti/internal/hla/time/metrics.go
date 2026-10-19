package time

import "github.com/cbchoi/gorti/rti/internal/hla/core"

// MetricsSnapshot is a coherent observation of the actual time stores. The
// caller validates federation membership; an untouched member has initial state.
type MetricsSnapshotOf[T core.LogicalTimeScalar] struct {
	Regulating, Constrained, AsynchronousDelivery, Advancing bool
	CurrentTime                                              T
	Lookahead, GALT, LITS                                    *T
	GALTUnbounded, LITSUnbounded                             bool
	TSOQueued                                                uint64
	ROQueued                                                 uint64
}

func (m *ManagerOf[T]) Metrics(fed core.FederationName, h core.FederateHandle) MetricsSnapshotOf[T] {
	ext := extOfOf[T](m)
	lease := ext.evaluatorLock(fed)
	lease.Lock()
	defer lease.Unlock()
	m.states.mu.Lock()
	defer m.states.mu.Unlock()
	ext.mu.Lock()
	defer ext.mu.Unlock()
	var snapshot MetricsSnapshotOf[T]
	if fs := m.states.getLocked(fed, h); fs != nil {
		snapshot.Regulating, snapshot.Constrained = fs.regulating, fs.constrained
		if fs.regulating {
			value := fs.lookahead
			snapshot.Lookahead = &value
		}
	}
	if ns := ext.getLocked(fed, h); ns != nil {
		snapshot.CurrentTime = ns.currentTime
		snapshot.Advancing = ns.pendingNER
		snapshot.AsynchronousDelivery = ns.asyncDelivery
		snapshot.ROQueued = uint64(len(ns.roBuffer))
		for _, item := range ns.tsoBuffer {
			if internalTSO(item.event) {
				continue
			}
			snapshot.TSOQueued++
			if snapshot.LITS == nil || item.timestamp < *snapshot.LITS {
				value := item.timestamp
				snapshot.LITS = &value
			}
		}
	}
	if bound := m.availableHorizonLocked(ext, fed, h); bound != nil {
		value := horizonScalar(*bound)
		snapshot.GALT, snapshot.GALTUnbounded = &value, bound.Unbounded
	}
	if snapshot.GALT != nil && (snapshot.LITS == nil || !snapshot.GALTUnbounded && *snapshot.GALT < *snapshot.LITS) {
		value := *snapshot.GALT
		snapshot.LITS = &value
		snapshot.LITSUnbounded = snapshot.GALTUnbounded
	}
	return snapshot
}

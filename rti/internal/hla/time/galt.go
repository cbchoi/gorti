package time

import "github.com/cbchoi/gorti/rti/internal/hla/core"

// QueryGALT observes the same committed time state as MOM and QueryLITS.
// The definition indicator is false only when no other regulator exists.
func (m *ManagerOf[T]) QueryGALT(fed core.FederationName, h core.FederateHandle) (T, bool) {
	s := m.Metrics(fed, h)
	if s.GALT == nil {
		return 0, false
	}
	return *s.GALT, true
}

type availableRegulatorOf[T core.LogicalTimeScalar] struct {
	h     core.FederateHandle
	fs    *federateStateOf[T]
	ns    *nerStateOf[T]
	bound core.TimeBoundOf[T]
}

// availableHorizonLocked implements the 8.1.5 bound from committed requests and
// queues. Caller holds the evaluator lease and both time-state locks.
func (m *ManagerOf[T]) availableHorizonLocked(ext *nerStoreOf[T], fed core.FederationName, self core.FederateHandle) *core.TimeBoundOf[T] {
	var peers []availableRegulatorOf[T]
	for key, fs := range m.states.states {
		if key.fed != fed || !fs.regulating {
			continue
		}
		ns := ext.getLocked(fed, key.h)
		entry := availableRegulatorSnapshot(key.h, fs, ns)
		peers = append(peers, entry)
	}

	propagateFlushHorizons(peers)

	var result *core.TimeBoundOf[T]
	for _, peer := range peers {
		if peer.h != self && (result == nil || !peer.bound.Unbounded && (result.Unbounded || peer.bound.Limit < result.Limit)) {
			value := peer.bound
			result = &value
		}
	}
	// Future arrivals from another regulator cannot lower this minimum:
	// any resulting early grant can send no earlier than that arrival. The
	// caller's already-sent messages are covered by the peers' queues above;
	// its hypothetical future sends do not directly constrain its own GALT.
	return result
}

func availableSendHorizon[T core.LogicalTimeScalar](fs *federateStateOf[T], ns *nerStateOf[T]) core.TimeBoundOf[T] {
	// In the current Float64 representation, Nextafter in this adapter is
	// the least representable permitted timestamp at an exclusive zero-LA
	// bound. It is not the logical-time-interval epsilon from clause 12.3.
	bound := regulatingBoundSnapshotOf[T](0, fs, ns)
	if bound.Unbounded {
		return unboundedHorizon[T]()
	}
	return addHorizon(bound.Time, bound.Lookahead)
}

func availableHorizonAfterGrant[T core.LogicalTimeScalar](fs *federateStateOf[T], ns *nerStateOf[T], at T) core.TimeBoundOf[T] {
	at = max(ns.currentTime, min(at, ns.requestedTime))
	granted := *ns
	granted.recordGrantBound(at)
	granted.currentTime = at
	granted.pendingNER = false
	regulation := *fs
	regulation.lookahead = fs.projectedLookahead(at)
	return availableSendHorizon(&regulation, &granted)
}

// QueryGALTBound preserves the distinction between final time and overflow.
func (m *ManagerOf[T]) QueryGALTBound(fed core.FederationName, h core.FederateHandle) (core.TimeBoundOf[T], bool) {
	s := m.Metrics(fed, h)
	if s.GALT == nil {
		return core.TimeBoundOf[T]{}, false
	}
	return core.TimeBoundOf[T]{Limit: *s.GALT, Unbounded: s.GALTUnbounded}, true
}

func (m *ManagerOf[T]) QueryLITSBound(fed core.FederationName, h core.FederateHandle) (core.TimeBoundOf[T], bool) {
	s := m.Metrics(fed, h)
	if s.LITS == nil {
		return core.TimeBoundOf[T]{}, false
	}
	return core.TimeBoundOf[T]{Limit: *s.LITS, Unbounded: s.LITSUnbounded}, true
}

func availableRegulatorSnapshot[T core.LogicalTimeScalar](h core.FederateHandle, fs *federateStateOf[T], ns *nerStateOf[T]) availableRegulatorOf[T] {
	entry := availableRegulatorOf[T]{h: h, fs: fs, ns: ns, bound: availableSendHorizon(fs, ns)}
	if fs.constrained && ns != nil && ns.pendingNER && (ns.mode.usesNextMessageTarget() || ns.mode == ModeFQR) {
		if ns.flushMinimum != nil {
			entry.bound = minimumHorizon(entry.bound, availableHorizonAfterGrant(fs, ns, *ns.flushMinimum))
		}
		// NMR/NMRA and FQR may leave Time Advancing before the requested
		// horizon (8.10-8.12). A queued message can therefore lower the
		// peer's future send bound even while the request remains pending.
		for _, item := range ns.tsoBuffer {
			if !internalTSO(item.event) && item.timestamp < ns.requestedTime {
				entry.bound = minimumHorizon(entry.bound, availableHorizonAfterGrant(fs, ns, item.timestamp))
			}
		}
	}
	return entry
}

func propagateFlushHorizons[T core.LogicalTimeScalar](peers []availableRegulatorOf[T]) {
	// FQR may also grant at its own GALT without an incoming message (8.12).
	// Propagate those bounds, including the querying regulator as an input to
	// another federate's FQR. Nonnegative lookahead means a cycle cannot lower
	// its starting bound, so at most len(peers) passes cover every simple path.
	for pass := 0; pass < len(peers); pass++ {
		changed := false
		for i := range peers {
			peer := &peers[i]
			if !peer.fs.constrained || peer.ns == nil || !peer.ns.pendingNER || peer.ns.mode != ModeFQR {
				continue
			}
			incoming := unboundedHorizon[T]()
			for j := range peers {
				if i != j {
					incoming = minimumHorizon(incoming, peers[j].bound)
				}
			}
			if !incoming.Unbounded && incoming.Limit < peer.ns.requestedTime {
				bound := availableHorizonAfterGrant(peer.fs, peer.ns, incoming.Limit)
				if !bound.Unbounded && (peer.bound.Unbounded || bound.Limit < peer.bound.Limit) {
					peer.bound = bound
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
}

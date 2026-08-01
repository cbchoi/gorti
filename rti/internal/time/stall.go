package time

import (
	"context"
	"sort"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// checkStalls is the body of Manager.CheckStalls. Extracted so the
// exported method's body remains a one-liner and the stable signature in
// manager.go is preserved verbatim.
//
// Algorithm (deterministic per NFR-DET-1):
//
//  1. Collect the set of federations with at least one pending NER and
//     skip any federation already in the halted set. Iteration over the
//     side-table is materialised into a sorted-by-federation-name slice
//     so the order in which federations are halted within a single
//     CheckStalls call is reproducible.
//
//  2. For each candidate federation, sort its pending federates by
//     handle (NFR-DET-1). The FIRST pending federate (lowest handle)
//     whose pendingSince is older than the federation's StallTimeout
//     becomes the stall trigger; later peers in the same federation
//     piggy-back on the same halt notification.
//
//  3. Mark the federation halted under nerStore.haltedMu BEFORE any
//     emission. After this point the halted-checks at the top of the
//     state-mutating Manager methods reject further calls with
//     core.ErrFederationHalted.
//
//  4. Compose the federate-membership snapshot — the union of every
//     federate the time manager has seen in this federation
//     (regulating, constrained, or pending NER) — sorted by handle, and
//     emit one FederationHalted to each via Outbox. Write-ahead through
//     EventLog when non-nil; the write-ahead happens once per halt
//     (not once per recipient) because replay needs only the cause +
//     stalled-federate identity, not the fan-out list.
//
// Returns the number of federations halted in this invocation.
func (m *Manager) checkStalls(ctx context.Context) int {
	ext := extOf(m)
	now := m.opts.Clock.Now()
	timeout := m.opts.StallTimeout

	// (1) Snapshot pending NERs per federation under ext.mu, then
	// release the lock before doing any I/O. Map iteration order is
	// random; we materialise to slices and sort afterwards so the halt
	// order across federations is deterministic.
	type pendCand struct {
		h     core.FederateHandle
		since stdtime.Time
	}
	perFed := make(map[core.FederationName][]pendCand)
	ext.mu.Lock()
	for k, ns := range ext.states {
		if !ns.pendingNER {
			continue
		}
		perFed[k.fed] = append(perFed[k.fed], pendCand{h: k.h, since: ns.pendingSince})
	}
	ext.mu.Unlock()

	if len(perFed) == 0 {
		return 0
	}

	// Sort federation names so halt order is deterministic when more
	// than one federation stalls in the same poll.
	feds := make([]core.FederationName, 0, len(perFed))
	for fed := range perFed {
		feds = append(feds, fed)
	}
	sort.Slice(feds, func(i, j int) bool { return feds[i] < feds[j] })

	halted := 0
	for _, fed := range feds {
		// Skip already-halted federations: a stall poll must be
		// idempotent over halted federations.
		if ext.isHalted(fed) {
			continue
		}

		// (2) Find the lowest-handle pending federate whose pending
		// age has exceeded the timeout. Sort by handle so the choice is
		// deterministic when multiple federates have aged past timeout.
		cands := perFed[fed]
		sort.Slice(cands, func(i, j int) bool { return cands[i].h < cands[j].h })

		var trigger core.FederateHandle
		stalled := false
		for _, c := range cands {
			if c.since.IsZero() {
				continue
			}
			if now.Sub(c.since) >= timeout {
				trigger = c.h
				stalled = true
				break
			}
		}
		if !stalled {
			continue
		}

		// (3) Atomically mark halted. Subsequent state-mutating Manager
		// methods on this federation now return ErrFederationHalted
		// from their pre-flight check.
		ext.markHalted(fed)

		// (4a) Write-ahead the halt record once per federation.
		if m.opts.EventLog != nil {
			rec := &federationHaltedRecord{
				Cause:           HaltCauseStall,
				StalledFederate: trigger,
			}
			// EventLog.Append errors are surfaced through Outbox.Send
			// (no return path here); per cut-1 fixtures this never
			// fails. Continue emitting to peers so a partial halt is
			// still visible to the federation.
			_ = m.opts.EventLog.Append(ctx, fed, rec)
		}

		// (4b) Compose membership snapshot: every federate the time
		// manager has seen in this federation. Use union of regulation
		// state (W1A) and pending NER state (W2) — handles in either
		// set are members for halt-notification purposes.
		members := m.federationMembers(fed)
		evt := &FederationHalted{
			Cause:           HaltCauseStall,
			StalledFederate: trigger,
		}
		for _, h := range members {
			// Errors from a single recipient must not abort fan-out:
			// every other peer still needs the halt notification. The
			// cut-1 fakeOutbox never fails.
			_ = m.opts.Outbox.Send(ctx, fed, h, evt)
		}

		halted++
	}
	return halted
}

// federationMembers returns the deterministic, handle-sorted list of
// every federate the time manager has seen in fed (regulating,
// constrained, OR pending NER). Used by stall fan-out to address the
// halt notification to every peer.
//
// Iteration over Go maps is randomised; the returned slice is sorted
// to honour NFR-DET-1.
func (m *Manager) federationMembers(fed core.FederationName) []core.FederateHandle {
	seen := make(map[core.FederateHandle]struct{})

	// Regulation/constrained membership lives under stateStore.mu.
	m.states.mu.Lock()
	for k := range m.states.states {
		if k.fed != fed {
			continue
		}
		seen[k.h] = struct{}{}
	}
	m.states.mu.Unlock()

	// NER bookkeeping (which may include federates that are constrained-
	// only and so not in the regulating set) lives under nerStore.mu.
	ext := extOf(m)
	ext.mu.Lock()
	for k := range ext.states {
		if k.fed != fed {
			continue
		}
		seen[k.h] = struct{}{}
	}
	ext.mu.Unlock()

	out := make([]core.FederateHandle, 0, len(seen))
	for h := range seen {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// markHalted records that fed has entered the halted terminal state.
// Idempotent: marking an already-halted federation is a no-op. Lives
// on nerStore because the nerStore is the per-Manager extension table
// W2 already wired up; W3 reuses it rather than introducing a second
// side-table keyed by *Manager.
func (n *nerStore) markHalted(fed core.FederationName) {
	n.haltedMu.Lock()
	defer n.haltedMu.Unlock()
	n.halted[fed] = struct{}{}
}

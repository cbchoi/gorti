// time_history.go — fixed-size ring buffer of recent current_time
// samples used by the Time view's ASCII sparkline (docs/rtid-tui.md
// §3.3, "last 30 ticks, normalized"). Per-(federation, federate)
// tracking lets us draw one sparkline per federate row.
//
// Phase 2 keeps history resident in-memory; the rtid event log /
// MOM-counter persistence story is unchanged. Sparkline rendering
// is in views.go (renderSparkline).

package main

import rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"

// timeRingCap is the sparkline's window size — 30 samples matches
// the design-doc mockup ("last 30 ticks").
const timeRingCap = 30

// timeRing is a fixed-capacity FIFO of float64 samples.
type timeRing struct {
	buf  [timeRingCap]float64
	used int // count of populated slots; <= timeRingCap
	head int // next write position
}

// push appends a sample, evicting the oldest when full.
func (r *timeRing) push(v float64) {
	r.buf[r.head] = v
	r.head = (r.head + 1) % timeRingCap
	if r.used < timeRingCap {
		r.used++
	}
}

// values returns the samples in chronological order (oldest → newest).
// Allocates on each call — fine for the once-per-frame render
// frequency, called only when the Time view is active.
func (r *timeRing) values() []float64 {
	if r.used == 0 {
		return nil
	}
	out := make([]float64, r.used)
	start := (r.head - r.used + timeRingCap) % timeRingCap
	for i := 0; i < r.used; i++ {
		out[i] = r.buf[(start+i)%timeRingCap]
	}
	return out
}

// recordTimeHistory walks every (federation, federate) in the latest
// snapshot and pushes the federate's current_time onto the matching
// ring. Federates that come and go are tracked lazily; we never
// shrink the map (size is bounded by total federates ever seen,
// which is fine for the operator-debugging use case).
func (m *model) recordTimeHistory(snap *rtiv1.SnapshotResponse) {
	if snap == nil {
		return
	}
	for _, fed := range snap.GetFederations() {
		fname := fed.GetName()
		perFed := m.timeHistory[fname]
		if perFed == nil {
			perFed = map[uint64]*timeRing{}
			m.timeHistory[fname] = perFed
		}
		for _, f := range fed.GetFederates() {
			ring := perFed[f.GetHandle()]
			if ring == nil {
				ring = &timeRing{}
				perFed[f.GetHandle()] = ring
			}
			ring.push(f.GetCurrentTime())
		}
	}
}

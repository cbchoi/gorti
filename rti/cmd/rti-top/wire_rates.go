// wire_rates.go — client-side rate-window tracking for the Wire view
// (docs/rtid-tui.md §3.4). Phase 3 of the rtid-TUI plan: the proto
// only exposes "since federate join" cumulative counters; rate
// numbers (`SENDS/s`, `RECVS/s`, `DROPS/s`) are derived in rti-top
// by ringing the last N snapshots' totals and computing deltas.
//
// Window key `T` cycles three modes:
//
//	wireWindow1Tick — last-tick rate (instantaneous)
//	wireWindow5Tick — 5-tick average ("5s avg" at 1Hz refresh)
//	wireWindow60Tick — 60-tick average ("1m avg" at 1Hz refresh)
//
// "Average" means "last N ticks of refresh" — at 100ms refresh the
// 5-tick window covers 500ms of wall-clock, not five seconds. We
// surface this in the header status line + the Wire-view footer so
// operators are not surprised.
//
// Federate join cycles reset the ring: when a sample shows a
// counter LOWER than the prior tick (a federate resigned + a new
// one took the same handle), we treat it as a fresh start so the
// rate doesn't flash a giant negative spike.

package main

import (
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// wireColumnID enumerates the Wire-view's toggleable columns. Phase 3
// of the rtid-TUI plan: the user can hide any subset via the `C`
// keybinding's column-picker popup. New rate columns ship in the
// same toggleable set.
type wireColumnID int

const (
	wireColFederation wireColumnID = iota
	wireColFederate
	wireColSends
	wireColRecvs
	wireColDrops
	wireColQDepth
	wireColQMax
	wireColSendsRate
	wireColRecvsRate
	wireColDropsRate
	wireColCount
)

// wireColumnDef describes one toggleable column — header label, width,
// and the function that extracts the cell value from a wireRow.
type wireColumnDef struct {
	id    wireColumnID
	label string
	width int
	cell  func(wireRow) string
}

// wireColumnDefs returns the canonical column definitions in render
// order. Mirrors the §3.4 mockup column ordering plus the new
// per-row rate columns appended at the right.
func wireColumnDefs() []wireColumnDef {
	return []wireColumnDef{
		{wireColFederation, "FEDERATION", 16, func(r wireRow) string { return r.fed }},
		{wireColFederate, "FEDERATE", 14, func(r wireRow) string { return r.name }},
		{wireColSends, "SENDS", 9, func(r wireRow) string { return fmtUint(r.sends) }},
		{wireColRecvs, "RECVS", 9, func(r wireRow) string { return fmtUint(r.recvs) }},
		{wireColDrops, "DROPS", 8, func(r wireRow) string { return fmtUint(r.drops) }},
		{wireColQDepth, "Q-DEPTH", 9, func(r wireRow) string { return fmtUint32(r.q) }},
		{wireColQMax, "Q-MAX", 8, func(r wireRow) string { return fmtUint32(r.qmax) }},
		{wireColSendsRate, "SENDS/s", 9, func(r wireRow) string { return formatRate(r.sendsRate) }},
		{wireColRecvsRate, "RECVS/s", 9, func(r wireRow) string { return formatRate(r.recvsRate) }},
		{wireColDropsRate, "DROPS/s", 9, func(r wireRow) string { return formatRate(r.dropsRate) }},
	}
}

// fmtUint / fmtUint32 are tiny adapters around fmt.Sprintf so the
// closures in wireColumnDefs stay one-liners.
func fmtUint(v uint64) string   { return fmtSprintf64(v) }
func fmtUint32(v uint32) string { return fmtSprintf64(uint64(v)) }
func fmtSprintf64(v uint64) string {
	// inlined fmt.Sprintf("%d", v) without the import dance — keep
	// formatting consistent with the old renderWireView.
	return fmtItoa(v)
}

// fmtItoa is a tiny strconv.FormatUint wrapper. Avoids dragging
// strconv into wire_rates.go's import set when the call site only
// needs an unsigned-int → decimal-string conversion.
func fmtItoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// wireColumnSet is a bitfield of enabled wireColumnIDs. Compact
// (uint16 fits well over wireColCount) so the entire set lives in
// the model with no allocation.
type wireColumnSet uint16

// defaultWireColumns returns the all-columns-on default.
func defaultWireColumns() wireColumnSet {
	var s wireColumnSet
	for i := wireColumnID(0); i < wireColCount; i++ {
		s = s.with(i, true)
	}
	return s
}

// has reports whether column id is currently enabled.
func (s wireColumnSet) has(id wireColumnID) bool {
	return s&(1<<uint(id)) != 0
}

// with returns a copy of s with column id set to on/off.
func (s wireColumnSet) with(id wireColumnID, on bool) wireColumnSet {
	if on {
		return s | (1 << uint(id))
	}
	return s &^ (1 << uint(id))
}

// toggle returns a copy of s with column id flipped.
func (s wireColumnSet) toggle(id wireColumnID) wireColumnSet {
	return s ^ (1 << uint(id))
}

// wireWindow enumerates the three rate-window choices the `T` key
// cycles through in the Wire view.
type wireWindow int

const (
	wireWindow1Tick wireWindow = iota
	wireWindow5Tick
	wireWindow60Tick
	wireWindowCount
)

// wireWindowLabel renders the current window for the header status
// line. The text matches the documentation in the README so the
// operator knows what "5s avg" actually means in their refresh
// configuration.
func wireWindowLabel(w wireWindow) string {
	switch w {
	case wireWindow1Tick:
		return "1s (last 1 tick)"
	case wireWindow5Tick:
		return "5s avg (last 5 ticks)"
	case wireWindow60Tick:
		return "1m avg (last 60 ticks)"
	}
	return "?"
}

// wireWindowSpan returns the tick-count averaging window for w. Used
// internally by the rate computation.
func wireWindowSpan(w wireWindow) int {
	switch w {
	case wireWindow1Tick:
		return 1
	case wireWindow5Tick:
		return 5
	case wireWindow60Tick:
		return 60
	}
	return 1
}

// wireRingCap is the largest tick window we ever keep — sized to fit
// the 60-tick "1m avg" mode. Allocated per (federation, federate);
// at the operator-debugging scale (a few federations, ~tens of
// federates) the memory cost is negligible.
const wireRingCap = 60

// wireSample is one (sends, recvs, drops) tuple captured at a
// snapshot tick. Stamped with the snapshot's wall-clock so the
// rate denominator can use the actual elapsed interval rather than
// the configured refresh (which may differ when the user cycles `r`
// mid-stream).
type wireSample struct {
	at    time.Time
	sends uint64
	recvs uint64
	drops uint64
}

// wireRing is a fixed-capacity FIFO of wireSamples — the per-
// (federation, federate) rate history. Same shape as time_history.go.
type wireRing struct {
	buf  [wireRingCap]wireSample
	used int
	head int
}

// push appends a sample, evicting the oldest when full. Detects a
// federate-join cycle (any counter dropped vs the prior tick) and
// resets the ring so the rate doesn't flash a negative spike when a
// new federate reuses a stale handle slot.
func (r *wireRing) push(s wireSample) {
	if r.used > 0 {
		prev := r.buf[(r.head-1+wireRingCap)%wireRingCap]
		if s.sends < prev.sends || s.recvs < prev.recvs || s.drops < prev.drops {
			// Counters went backwards → new federate / restart.
			// Drop history; the next rate will be 0 and grow from there.
			r.used = 0
			r.head = 0
		}
	}
	r.buf[r.head] = s
	r.head = (r.head + 1) % wireRingCap
	if r.used < wireRingCap {
		r.used++
	}
}

// rate returns the (sends/s, recvs/s, drops/s) average over the
// last `span` ticks (or whatever subset is populated). When the
// ring has fewer than 2 samples or zero elapsed wall-clock, every
// rate is 0 — there's nothing to differentiate yet.
func (r *wireRing) rate(span int) (sendsPerSec, recvsPerSec, dropsPerSec float64) {
	if r.used < 2 || span < 1 {
		return 0, 0, 0
	}
	if span > r.used-1 {
		span = r.used - 1
	}
	// newest is the most recent sample; oldest is `span` ticks back
	// (so the delta covers `span` intervals).
	newestIdx := (r.head - 1 + wireRingCap) % wireRingCap
	oldestIdx := (r.head - 1 - span + wireRingCap) % wireRingCap
	newest := r.buf[newestIdx]
	oldest := r.buf[oldestIdx]
	dt := newest.at.Sub(oldest.at).Seconds()
	if dt <= 0 {
		return 0, 0, 0
	}
	dSends := float64(newest.sends - oldest.sends)
	dRecvs := float64(newest.recvs - oldest.recvs)
	dDrops := float64(newest.drops - oldest.drops)
	return dSends / dt, dRecvs / dt, dDrops / dt
}

// recordWireRates walks the latest snapshot and pushes one wireSample
// per (federation, federate) row. Mirrors recordTimeHistory's shape;
// called from Update on every snapshotMsg before the model.last
// pointer is advanced — so the Wire view always has rate data
// derived from the same snapshot it's rendering.
func (m *model) recordWireRates(snap *rtiv1.SnapshotResponse, at time.Time) {
	if snap == nil {
		return
	}
	for _, fed := range snap.GetFederations() {
		fname := fed.GetName()
		perFed := m.wireRates[fname]
		if perFed == nil {
			perFed = map[uint64]*wireRing{}
			m.wireRates[fname] = perFed
		}
		for _, f := range fed.GetFederates() {
			ring := perFed[f.GetHandle()]
			if ring == nil {
				ring = &wireRing{}
				perFed[f.GetHandle()] = ring
			}
			ring.push(wireSample{
				at:    at,
				sends: f.GetUpdatesSent() + f.GetInteractionsSent(),
				recvs: f.GetReflectionsReceived() + f.GetInteractionsReceived(),
				drops: f.GetDropsTotal(),
			})
		}
	}
}

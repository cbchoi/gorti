// Package main — examples/go-timed federate subprocess.
//
// regulator.go holds the per-federate model state. The federate is a
// minimal counter: each granted advance emits one Tick and bumps the
// counter. Real DEVS-style models would have output_handler /
// internal_transition methods; we keep this tight to focus on the
// time-management surface.

package main

import "encoding/binary"

// regulator holds per-federate state for one cycle of the demo.
type regulator struct {
	name      string
	lookahead float64
	grants    []float64 // logical times of grants received, in arrival order
	tickSeq   uint32    // monotonic per-federate Tick counter
}

// nextTickPayload returns the wire bytes for the next Tick interaction.
// Increments tickSeq as a side-effect.
func (r *regulator) nextTickPayload() []byte {
	r.tickSeq++
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, r.tickSeq)
	return b
}

// tickTimestamp returns the earliest valid TSO timestamp after a grant.
// A regulating federate may not send before current time + lookahead.
func (r *regulator) tickTimestamp(grantTime float64) float64 {
	return grantTime + r.lookahead
}

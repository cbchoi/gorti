// Scaffold owned by TASK-205 (M21) — see docs/M21_DISPATCH_PLAN.md §6.
//
// Verifies grant emission + stall→FederationHalted on the wire.
// Depends on TASK-204 (rtid wiring), TASK-204b (stream conversion),
// TASK-204c (resign hook).

package main

import "testing"

// 205.1 — 2 federates, both regulating+constrained, both NER(10).
// Both receive TimeAdvanceGrant{0.5} on their wire streams.
func TestNERTwoFederatesGrant(t *testing.T) {
	t.Skip("TODO: TASK-205")
}

// 205.2 — Same setup, A TAR(10).
func TestTARTwoFederates(t *testing.T) {
	t.Skip("TODO: TASK-205")
}

// 205.3 — NMRA(10) — NMRA inclusive boundary visible via wire.
func TestNMRATwoFederates(t *testing.T) {
	t.Skip("TODO: TASK-205")
}

// 205.4 — TARA(10).
func TestTARATwoFederates(t *testing.T) {
	t.Skip("TODO: TASK-205")
}

// 205.5 — Subscriber-only FQR(5) with 3 receive-events queued.
// Wire stream delivers 3 receives FIRST, then TimeAdvanceGrant.
func TestFQRDrainsBeforeGrant(t *testing.T) {
	t.Skip("TODO: TASK-205")
}

// 205.6 — Federate calls NER(5), then resigns before grant fires.
// No grant on closed stream; goroutine count stable across -race -count=10.
func TestResignDuringPendingNER(t *testing.T) {
	t.Skip("TODO: TASK-205")
}

// 205.7 — Resign during pending TAR.
func TestResignDuringPendingTAR(t *testing.T) {
	t.Skip("TODO: TASK-205")
}

// 205.8 — Resign during pending NMRA / TARA / FQR (sub-tests for each).
func TestResignDuringPendingOtherPrimitives(t *testing.T) {
	t.Skip("TODO: TASK-205 — sub-tests for NMRA, TARA, FQR")
}

// 205.9 — Federate's events stream drops mid-flight (client cancel).
// Manager's grant callback does NOT block; OTHER federates still receive.
func TestEventsStreamCancelDoesNotBlock(t *testing.T) {
	t.Skip("TODO: TASK-205")
}

// 205.10 — Stall → FederationHalted: A NER(10), B never advances.
// After StallTimeout (1s for the test), both receive FederationHalted on wire.
func TestStallEmitsFederationHalted(t *testing.T) {
	t.Skip("TODO: TASK-205 — requires StallTimeout flag override on cmd/rtid")
}

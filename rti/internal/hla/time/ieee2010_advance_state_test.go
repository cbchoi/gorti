package time

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestIEEE2010UnconstrainedAdvanceAllModes(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeTAR, ModeTARA, ModeNER, ModeNMRA, ModeFQR} {
		for _, regulating := range []bool{false, true} {
			name := mode.String() + "/unengaged"
			if regulating {
				name = mode.String() + "/regulating-only"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				m, out := newIEEE2010DeliveryManager(t)
				if err := m.EnableRegulation(ctx, "fed", 2, 1); err != nil {
					t.Fatal(err)
				}
				if regulating {
					if err := m.EnableRegulation(ctx, "fed", 1, 1); err != nil {
						t.Fatal(err)
					}
				}
				if err := m.dispatchAdvance(ctx, "fed", 1, 10, mode); err != nil {
					t.Fatalf("joined unconstrained requester rejected: %v", err)
				}
				if len(out.events) != 1 {
					t.Fatalf("unconstrained advance waited for a peer: %v", out.events)
				}
				grant, ok := out.events[0].(*TimeAdvanceGrant)
				if !ok || grant.Time != 10 {
					t.Fatalf("grant=%v, want TAG(10)", out.events[0])
				}
				state := m.Metrics("fed", 1)
				if state.CurrentTime != 10 || state.Advancing || state.Constrained || state.Regulating != regulating {
					t.Fatalf("advance changed engagement or failed to commit time: %+v", state)
				}
				if err := m.dispatchAdvance(ctx, "fed", 1, 9, mode); !errors.Is(err, core.ErrTimeRequestInPast) {
					t.Fatalf("past request=%v", err)
				}
				if len(out.events) != 1 || m.Metrics("fed", 1).CurrentTime != 10 {
					t.Fatal("rejected request mutated state or emitted a callback")
				}
			})
		}
	}
}

func TestIEEE2010UnengagedInvalidAdvanceDoesNotCreateState(t *testing.T) {
	for _, target := range []core.LogicalTime{-1, core.LogicalTime(math.NaN())} {
		m, out := newIEEE2010DeliveryManager(t)
		if err := m.TimeAdvanceRequest(context.Background(), "fed", 1, target); err == nil {
			t.Fatalf("invalid target %v accepted", target)
		}
		if len(m.Snapshot("fed").Federates) != 0 || len(out.events) != 0 {
			t.Fatalf("invalid target %v created state", target)
		}
	}
}

func TestIEEE2010SameTimeTSOWaitsForAdvance(t *testing.T) {
	for _, current := range []core.LogicalTime{0, 5} {
		for _, async := range []bool{false, true} {
			for _, mode := range []AdvanceMode{ModeTAR, ModeTARA, ModeNER, ModeNMRA, ModeFQR} {
				t.Run(testROStateName(true, false, async, false)+"/"+mode.String()+"/"+coreTimeName(current), func(t *testing.T) {
					ctx := context.Background()
					m, out := newIEEE2010DeliveryManager(t)
					if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
						t.Fatal(err)
					}
					if err := m.TimeAdvanceRequestAvailable(ctx, "fed", 1, current); err != nil {
						t.Fatal(err)
					}
					out.events = nil
					if async {
						if err := m.EnableAsynchronousDelivery(ctx, "fed", 1); err != nil {
							t.Fatal(err)
						}
					}
					event := &scheduledOrderedEvent{}
					r := m.ReserveTSO("fed", []core.MessageDelivery{{Recipient: 1, Timestamp: current, Event: event, Sender: 2, RetractionHandle: 7}})
					if len(r.Immediate()) != 0 || len(r.Buffered()) != 1 {
						r.Release()
						t.Fatal("same-time TSO bypassed the advancing-state requirement")
					}
					r.Commit(ctx)
					if m.ShouldDeliverNow("fed", 1, current) || len(out.events) != 0 || m.Metrics("fed", 1).TSOQueued != 1 {
						t.Fatal("granted receiver exposed a queued TSO message")
					}
					if err := m.dispatchAdvance(ctx, "fed", 1, current+10, mode); err != nil {
						t.Fatal(err)
					}
					want := current + 10
					if mode.usesNextMessageTarget() || mode == ModeFQR {
						want = current
					}
					if len(out.events) != 2 || out.events[0] != event || event.order != core.OrderTimestamp {
						t.Fatalf("same-time message must precede grant with TSO metadata: %v", out.events)
					}
					grant, ok := out.events[1].(*TimeAdvanceGrant)
					if !ok || grant.Time != want {
						t.Fatalf("grant=%v, want TAG(%v)", out.events[1], want)
					}
				})
			}
		}
	}
}

func coreTimeName(t core.LogicalTime) string {
	if t == 0 {
		return "initial"
	}
	return "advanced"
}

func TestIEEE2010SameTimeNextMessageTarget(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeNER, ModeNMRA} {
		d := decideGrant(mode, 5, 10, 11, true, 5, true)
		if !d.fire || d.time != 5 || !d.clearPending {
			t.Fatalf("%s skipped queued same-time message: %+v", mode, d)
		}
	}
}

func TestIEEE2010SameTimeTSORetractionBeforeAdvance(t *testing.T) {
	ctx := context.Background()
	m, out := newIEEE2010DeliveryManager(t)
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	r := m.ReserveTSO("fed", []core.MessageDelivery{{Recipient: 1, Timestamp: 0, Event: &scheduledOrderedEvent{}, Sender: 2, RetractionHandle: 7}})
	if len(r.Immediate()) != 0 {
		r.Release()
		t.Fatal("same-time event was delivered before it could be retracted")
	}
	r.Commit(ctx)
	if removed := m.RetractMessage("fed", 2, 7); removed != 1 {
		t.Fatalf("removed=%d", removed)
	}
	if err := m.NextMessageRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 1 {
		t.Fatalf("retracted callback escaped: %v", out.events)
	}
	if grant, ok := out.events[0].(*TimeAdvanceGrant); !ok || grant.Time != 10 {
		t.Fatalf("grant=%v", out.events[0])
	}
}

func TestIEEE2010SameTimeAvailableBoundaryWithPeer(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeNER, ModeNMRA} {
		t.Run(mode.String(), func(t *testing.T) {
			ctx := context.Background()
			m, out := newIEEE2010DeliveryManager(t)
			if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
				t.Fatal(err)
			}
			for _, h := range []core.FederateHandle{1, 2} {
				if err := m.TimeAdvanceRequestAvailable(ctx, "fed", h, 3); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.EnableRegulation(ctx, "fed", 2, 0); err != nil {
				t.Fatal(err)
			}
			out.events = nil
			event := &scheduledOrderedEvent{}
			r := m.ReserveTSO("fed", []core.MessageDelivery{{Recipient: 1, Timestamp: 3, Event: event, Sender: 2}})
			if len(r.Immediate()) != 0 {
				r.Release()
				t.Fatal("post-Available same-time TSO escaped while idle")
			}
			r.Commit(ctx)
			if err := m.dispatchAdvance(ctx, "fed", 1, 10, mode); err != nil {
				t.Fatal(err)
			}
			if mode == ModeNER {
				if len(out.events) != 0 || !m.Metrics("fed", 1).Advancing {
					t.Fatal("strict NMR granted at an equal peer bound")
				}
				if err := m.TimeAdvanceRequest(ctx, "fed", 2, 4); err != nil {
					t.Fatal(err)
				}
				// The peer request raises its bound before its own grant. Only
				// recipient-local message/grant order is prescribed here.
				if len(out.events) != 3 {
					t.Fatalf("callbacks after peer advanced: %v", out.events)
				}
				var receiverEvents []core.OutboundEvent
				for _, callback := range out.events {
					if grant, ok := callback.(*TimeAdvanceGrant); ok && grant.Time == 4 {
						continue
					}
					receiverEvents = append(receiverEvents, callback)
				}
				out.events = receiverEvents
			}
			if len(out.events) != 2 || out.events[0] != event {
				t.Fatalf("same-time callback missing before grant: %v", out.events)
			}
			if grant, ok := out.events[1].(*TimeAdvanceGrant); !ok || grant.Time != 3 {
				t.Fatalf("same-time target skipped: %v", out.events[1])
			}
		})
	}
}

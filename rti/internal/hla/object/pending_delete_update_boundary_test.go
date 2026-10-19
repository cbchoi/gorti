package object

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func pendingDeleteUpdateFixture[T core.LogicalTimeScalar](t *testing.T) (*RegistryOf[T], *timepkg.ManagerOf[T], *atomicUpdateOutbox, *recordingEventLog, core.ObjectHandle) {
	t.Helper()
	log := &recordingEventLog{}
	base, decl, _ := newTestRegistry(t, log)
	out := &atomicUpdateOutbox{}
	r, err := NewOf[T](OptionsOf[T]{Clock: base.opts.Clock, FOMs: base.opts.FOMs, Declarations: decl, Outbox: out, EventLog: log})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := decl.PublishObjectClassAttributes(ctx, "fed", 1, 1, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 1, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	reserveFixtureName(t, r, "fed", 1, "pending-delete-boundary")
	obj, _, err := r.Register(ctx, "fed", 1, 1, "pending-delete-boundary")
	if err != nil {
		t.Fatal(err)
	}
	tm, err := timepkg.NewOf[T](timepkg.OptionsOf[T]{Clock: r.opts.Clock, Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	tm.SetDeletionLifecycle(r)
	r.opts.TSOGate, r.opts.Retractions, r.opts.TSOValidator = tm, tm, tm
	if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	if err := tm.EnableRegulation(ctx, "fed", 1, T(1)); err != nil {
		t.Fatal(err)
	}
	out.records = nil
	return r, tm, out, log, obj
}

func clonePendingDeleteCallback[T core.LogicalTimeScalar](event core.OutboundEvent) (core.OutboundEvent, error) {
	e, ok := event.(*CallbackEventOf[T])
	if !ok {
		return nil, errors.New("unexpected queued callback")
	}
	cloned := *e
	cloned.LogicalTime = copiedLogicalTimeOf(e.LogicalTime)
	cloned.UserSuppliedTag = slices.Clone(e.UserSuppliedTag)
	cloned.Attributes = make(map[uint64][]byte, len(e.Attributes))
	for attr, value := range e.Attributes {
		cloned.Attributes[attr] = slices.Clone(value)
	}
	if e.Metadata != nil {
		metadata := *e.Metadata
		cloned.Metadata = &metadata
	}
	return &cloned, nil
}

// S1516.1 6.10.3(i)/6.14.4(c)(1): equality is admitted before retirement.
// Positive lookahead and no initial advance isolate this bound from Clause 8.
func TestPendingDeleteUpdateTimestampBoundary(t *testing.T) {
	t.Run("Float64", func(t *testing.T) { checkPendingDeleteUpdateTimestampBoundary(t, float64(10)) })
	t.Run("Integer64", func(t *testing.T) { checkPendingDeleteUpdateTimestampBoundary(t, int64(1<<53+17)) })
}

func checkPendingDeleteUpdateTimestampBoundary[T core.LogicalTimeScalar](t *testing.T, deletionTime T) {
	for _, wire := range []bool{false, true} {
		path := "typed"
		if wire {
			path = "wire"
		}
		for _, relation := range []string{"below", "equal", "above"} {
			t.Run(path+"/"+relation, func(t *testing.T) {
				r, tm, out, log, obj := pendingDeleteUpdateFixture[T](t)
				ctx := context.Background()
				if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &deletionTime, core.ObjectMessageOptions{Tag: []byte(testDelete), RetractionHandle: 200}); err != nil {
					t.Fatal(err)
				}
				beforeObject := r.ExportState("fed")
				beforeTime, err := tm.ExportState("fed", clonePendingDeleteCallback[T])
				if err != nil {
					t.Fatal(err)
				}
				beforeLog, beforeOut, beforeReservations := len(log.Records()), len(out.records), len(out.reservations)
				var sent int
				r.opts.OnUpdateSent = func(core.FederationName, core.FederateHandle) { sent++ }
				send := func(ts *T) error {
					if wire {
						return r.UpdateAttributesWire(ctx, "fed", 1, obj, map[uint64][]byte{1: {0, 255}}, ts)
					}
					return r.UpdateAttributesWithMetadata(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {0, 255}}, ts, core.ObjectMessageOptions{Tag: []byte("update"), RetractionHandle: 201})
				}
				ts := deletionTime
				if relation == "below" {
					ts--
				} else if relation == "above" {
					ts++
				}
				err = send(&ts)
				if relation == "above" {
					if !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
						t.Fatalf("later timestamp error = %v, want InvalidLogicalTime sentinel", err)
					}
					afterTime, snapshotErr := tm.ExportState("fed", clonePendingDeleteCallback[T])
					if snapshotErr != nil {
						t.Fatal(snapshotErr)
					}
					if !reflect.DeepEqual(beforeObject, r.ExportState("fed")) || !reflect.DeepEqual(beforeTime, afterTime) || len(log.Records()) != beforeLog || len(out.records) != beforeOut || len(out.reservations) != beforeReservations || sent != 0 {
						t.Fatal("rejected timestamp changed object/time state, journal, outbox or traffic")
					}
					ts = deletionTime - 1
					// Reuse the rejected call's ID to expose leaked retraction admission.
					err = send(&ts)
				}
				if err != nil {
					t.Fatalf("admissible update at %v, deletion %v: %v", ts, deletionTime, err)
				}
				if len(log.Records()) != beforeLog+1 || sent != 1 {
					t.Fatal("successful update was not journaled/counted exactly once")
				}
				queued, err := tm.ExportState("fed", clonePendingDeleteCallback[T])
				if err != nil {
					t.Fatal(err)
				}
				var reflections int
				for _, member := range queued.Federates {
					for _, delivery := range member.Queue {
						e := delivery.Event.(*CallbackEventOf[T])
						if e.Kind != CallbackReflect {
							continue
						}
						reflections++
						if member.Handle != 2 || delivery.Timestamp != ts || delivery.ReceiveOrder || e.LogicalTime == nil || *e.LogicalTime != ts || e.Object != obj || !slices.Equal(e.Attributes[1], []byte{0, 255}) || e.Metadata.SentOrder != core.OrderTimestamp {
							t.Fatalf("incorrect admitted reflection: %+v", delivery)
						}
						if !wire && (e.Metadata.RetractionHandle != 201 || string(e.UserSuppliedTag) != "update") {
							t.Fatal("typed update metadata lost")
						}
					}
				}
				if reflections != 1 || r.stateFor("fed").deletions[obj].Committed || r.stateFor("fed").instances[obj] == nil {
					t.Fatal("update did not preserve one reflection and the pending deletion")
				}
			})
		}
	}
}

func TestPendingDeleteUpdateProtection(t *testing.T) {
	t.Run("Float64", func(t *testing.T) { checkPendingDeleteUpdateProtection(t, float64(10)) })
	t.Run("Integer64", func(t *testing.T) { checkPendingDeleteUpdateProtection(t, int64(1<<53+17)) })
}

func TestPendingDeleteEqualUpdateDeliveredBeforeRemoval(t *testing.T) {
	t.Run("Float64", func(t *testing.T) { checkPendingDeleteEqualUpdateDelivery(t, float64(10)) })
	t.Run("Integer64", func(t *testing.T) { checkPendingDeleteEqualUpdateDelivery(t, int64(1<<53+17)) })
}

func checkPendingDeleteEqualUpdateDelivery[T core.LogicalTimeScalar](t *testing.T, deletionTime T) {
	for _, wire := range []bool{false, true} {
		path := "typed"
		if wire {
			path = "wire"
		}
		t.Run(path, func(t *testing.T) {
			r, tm, out, _, obj := pendingDeleteUpdateFixture[T](t)
			ctx := context.Background()
			if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &deletionTime, core.ObjectMessageOptions{Tag: []byte("delete-first"), RetractionHandle: 300}); err != nil {
				t.Fatal(err)
			}
			var err error
			if wire {
				err = r.UpdateAttributesWire(ctx, "fed", 1, obj, map[uint64][]byte{1: {0, 255}}, &deletionTime)
			} else {
				err = r.UpdateAttributesWithMetadata(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {0, 255}}, &deletionTime, core.ObjectMessageOptions{Tag: []byte("equal-update"), RetractionHandle: 301})
			}
			if err != nil {
				t.Fatalf("equal update admission: %v", err)
			}
			if len(out.records) != 0 {
				t.Fatal("timestamped callbacks escaped before advance")
			}
			if err := tm.TimeAdvanceRequest(ctx, "fed", 1, deletionTime); err != nil {
				t.Fatalf("sender advance: %v", err)
			}
			if err := tm.TimeAdvanceRequest(ctx, "fed", 2, deletionTime); err != nil {
				t.Fatalf("receiver advance: %v", err)
			}
			var received []core.OutboundEvent
			var sequence []string
			for _, record := range out.records {
				if record.Federate != 2 {
					continue
				}
				received = append(received, record.Event)
				switch event := record.Event.(type) {
				case *CallbackEventOf[T]:
					switch event.Kind {
					case CallbackReflect:
						sequence = append(sequence, "reflect")
					case CallbackRemove:
						sequence = append(sequence, testRemove)
					default:
						sequence = append(sequence, "other-object-callback")
					}
				case *timepkg.TimeAdvanceGrantOf[T]:
					sequence = append(sequence, "grant")
				default:
					sequence = append(sequence, "other-callback")
				}
			}
			if !slices.Equal(sequence, []string{"reflect", testRemove, "grant"}) {
				t.Fatalf("delete accepted before equal update: receiver sequence=%v, want [reflect remove grant]", sequence)
			}
			reflection := received[0].(*CallbackEventOf[T])
			removal := received[1].(*CallbackEventOf[T])
			grant := received[2].(*timepkg.TimeAdvanceGrantOf[T])
			if reflection.Object != obj || reflection.LogicalTime == nil || *reflection.LogicalTime != deletionTime || !slices.Equal(reflection.Attributes[1], []byte{0, 255}) || reflection.Metadata.SentOrder != core.OrderTimestamp || reflection.Metadata.ReceivedOrder != core.OrderTimestamp {
				t.Fatalf("equal update callback corrupted: %+v", reflection)
			}
			if !wire && (reflection.Metadata.RetractionHandle != 301 || string(reflection.UserSuppliedTag) != "equal-update") {
				t.Fatal("equal update metadata lost")
			}
			if removal.Object != obj || removal.LogicalTime == nil || *removal.LogicalTime != deletionTime || removal.Metadata.RetractionHandle != 300 || removal.Metadata.ReceivedOrder != core.OrderTimestamp || string(removal.UserSuppliedTag) != "delete-first" || grant.Time != deletionTime {
				t.Fatal("removal or final grant corrupted")
			}
		})
	}
}

func checkPendingDeleteUpdateProtection[T core.LogicalTimeScalar](t *testing.T, deletionTime T) {
	for _, mode := range []string{"untimestamped", "committed", testReceiveOrder} {
		t.Run(mode, func(t *testing.T) {
			r, _, out, log, obj := pendingDeleteUpdateFixture[T](t)
			ctx := context.Background()
			if mode == testReceiveOrder {
				r.opts.Orders = preferredOrderTable{attributes: map[core.AttributeHandle]Order{1: OrderReceive}}
			}
			if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &deletionTime, core.ObjectMessageOptions{Tag: []byte(testDelete)}); err != nil {
				t.Fatal(err)
			}
			if mode == "committed" {
				// Exercise the defensive guard even if a committed identity retains an instance.
				st := r.stateFor("fed")
				st.mu.Lock()
				d := st.deletions[obj]
				d.Committed = true
				st.deletions[obj] = d
				st.mu.Unlock()
			}
			before, records := len(log.Records()), len(out.records)
			ts := deletionTime - 1
			stamp := &ts
			if mode == "untimestamped" {
				stamp = nil
			}
			err := r.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {7}}, stamp)
			want := core.ErrObjectDeletionPending
			if mode == testReceiveOrder {
				want = core.ErrObjectNotFound
			}
			if !errors.Is(err, want) || len(log.Records()) != before || len(out.records) != records {
				t.Fatalf("%s deletion protection: error=%v, want %v, or visible effects", mode, err, want)
			}
		})
	}
}

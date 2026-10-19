package ownership

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type divestFollowupCall struct {
	name    string
	confirm bool
	call    func(*Manager, context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) error
}

func divestFollowupCalls() []divestFollowupCall {
	return []divestFollowupCall{
		{"unconditional", false, (*Manager).UnconditionalDivest},
		{"negotiated", false, func(m *Manager, ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) error {
			return m.NegotiatedDivestTwoPhase(ctx, fed, h, obj, attrs, []byte("offer"))
		}},
		{"legacy-negotiated", false, func(m *Manager, ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) error {
			return m.NegotiatedDivest(ctx, fed, h, obj, attrs, []byte("offer"))
		}},
		{"confirm", true, (*Manager).ConfirmDivestiture},
		{"tagged-confirm", true, func(m *Manager, ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) error {
			return m.ConfirmDivestitureWithTag(ctx, fed, h, obj, attrs, []byte("confirm"))
		}},
		{"if-wanted", false, (*Manager).DivestIfWanted},
	}
}

// IEEE 1516.1-2010 7.2.5(a-c), 7.3.5(b-d), 7.6.5(c-e),
// reviewed active redline PDF pp. 161-162, 165-166.
func TestIEEE2010DivestValidationRejectsWithoutEffects(t *testing.T) {
	ctx := context.Background()
	for _, op := range divestFollowupCalls() {
		for _, tc := range []struct {
			name       string
			attrs      []core.AttributeHandle
			prepareErr error
			want       error
		}{
			{"unknown-object", []core.AttributeHandle{1}, core.ErrObjectHandleInvalid, core.ErrObjectHandleInvalid},
			{"unknown-empty-set", nil, core.ErrObjectHandleInvalid, core.ErrObjectHandleInvalid},
			{"undefined", []core.AttributeHandle{999999}, core.ErrAttributeNotFound, core.ErrAttributeNotFound},
			{"mixed-undefined", []core.AttributeHandle{1, 999999}, core.ErrAttributeNotFound, core.ErrAttributeNotFound},
			{"undefined-first", []core.AttributeHandle{999999, 1}, core.ErrAttributeNotFound, core.ErrAttributeNotFound},
			{"nonowner", []core.AttributeHandle{2}, nil, core.ErrAttributeNotOwned},
			{"mixed-nonowner", []core.AttributeHandle{1, 2}, nil, core.ErrAttributeNotOwned},
			{"nonowner-first", []core.AttributeHandle{2, 1}, nil, core.ErrAttributeNotOwned},
		} {
			t.Run(op.name+"/"+tc.name, func(t *testing.T) {
				journal := &permissiveLog{}
				m, out := newMgr(t, Options{EventLog: journal, Subscribers: func(context.Context, core.FederationName, core.ObjectHandle, []core.AttributeHandle) []core.FederateHandle {
					return []core.FederateHandle{3}
				}})
				m.RegisterInitialOwnership("fed", 1, 10, []core.AttributeHandle{1})
				m.RegisterInitialOwnership("fed", 2, 10, []core.AttributeHandle{2})
				if err := m.Acquire(ctx, "fed", 3, 10, []core.AttributeHandle{1}, []byte("pending")); err != nil {
					t.Fatal(err)
				}
				if op.confirm {
					if err := m.NegotiatedDivestTwoPhase(ctx, "fed", 1, 10, []core.AttributeHandle{1}, []byte("original")); err != nil {
						t.Fatal(err)
					}
				}
				prepared, released, changed := 0, 0, 0
				m.opts.PrepareDivestiture = func(_ context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
					prepared++
					if fed != "fed" || h != 1 || obj != 10 || !reflect.DeepEqual(attrs, tc.attrs) {
						t.Fatal("preparation arguments changed")
					}
					if tc.prepareErr != nil {
						return nil, tc.prepareErr
					}
					return func() { released++ }, nil
				}
				m.SetOnOwnershipChanged(func(context.Context, core.FederationName, core.ObjectHandle) { changed++ })
				before, callbacks, logs := m.ExportState("fed"), len(out.Sent()), len(journal.records)
				if err := op.call(m, ctx, "fed", 1, 10, tc.attrs); !errors.Is(err, tc.want) {
					t.Errorf("error=%v, want %v", err, tc.want)
				}
				if !reflect.DeepEqual(before, m.ExportState("fed")) || len(out.Sent()) != callbacks || len(journal.records) != logs || changed != 0 {
					t.Error("rejected request changed state, journal or callbacks")
				}
				wantReleased := 0
				if tc.prepareErr == nil {
					wantReleased = 1
				}
				if prepared != 1 || released != wantReleased {
					t.Errorf("prepared/released=%d/%d, want 1/%d", prepared, released, wantReleased)
				}
			})
		}
	}
}

func TestIEEE2010DivestFailureDoesNotCreateFederationState(t *testing.T) {
	for _, op := range divestFollowupCalls() {
		t.Run(op.name, func(t *testing.T) {
			m, out := newMgr(t, Options{})
			if err := op.call(m, context.Background(), "missing", 1, 10, []core.AttributeHandle{1}); !errors.Is(err, core.ErrAttributeNotOwned) {
				t.Errorf("error=%v, want AttributeNotOwned", err)
			}
			if m.ExportState("missing") != nil || len(out.Sent()) != 0 {
				t.Fatal("failed divest created state or callbacks")
			}
		})
	}
}

func TestIEEE2010DivestReleasesObjectBeforeObservers(t *testing.T) {
	for _, op := range divestFollowupCalls() {
		t.Run(op.name, func(t *testing.T) {
			ctx := context.Background()
			m, out := newMgr(t, Options{})
			m.RegisterInitialOwnership("fed", 1, 10, []core.AttributeHandle{1})
			if err := m.Acquire(ctx, "fed", 2, 10, []core.AttributeHandle{1}, nil); err != nil {
				t.Fatal(err)
			}
			if op.confirm {
				if err := m.NegotiatedDivestTwoPhase(ctx, "fed", 1, 10, []core.AttributeHandle{1}, nil); err != nil {
					t.Fatal(err)
				}
			}
			pinned, prepared, released, observed := false, 0, 0, 0
			m.opts.PrepareDivestiture = func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (func(), error) {
				pinned = true
				prepared++
				return func() {
					state := m.ExportState("fed")
					switch op.name {
					case "unconditional":
						if len(state.Owners) != 1 || state.Owners[0].Owner != 2 || len(state.PendingAcquires) != 0 {
							t.Error("object released before divest commit")
						}
					case "negotiated":
						if len(state.PendingDivests) != 1 {
							t.Error("object released before negotiation commit")
						}
					default:
						if len(state.Owners) != 1 || state.Owners[0].Owner != 2 {
							t.Error("object released before transfer commit")
						}
					}
					pinned = false
					released++
				}, nil
			}
			observe := func() {
				observed++
				if pinned {
					t.Error("observer invoked while object pinned")
				}
				m.QueryOwnership("fed", 10, 1)
			}
			m.opts.Outbox = divestObserverOutbox{observe: observe, out: out}
			m.opts.EventLog = &divestObserverLog{observe: observe}
			m.opts.Subscribers = func(context.Context, core.FederationName, core.ObjectHandle, []core.AttributeHandle) []core.FederateHandle {
				observe()
				return []core.FederateHandle{2}
			}
			m.SetOnOwnershipChanged(func(context.Context, core.FederationName, core.ObjectHandle) { observe() })
			if err := op.call(m, ctx, "fed", 1, 10, []core.AttributeHandle{1}); err != nil {
				t.Fatal(err)
			}
			if pinned || prepared != 1 || released != 1 || observed == 0 {
				t.Fatalf("pinned=%v prepared=%d released=%d observed=%d", pinned, prepared, released, observed)
			}
		})
	}
}

type divestObserverOutbox struct {
	observe func()
	out     core.Outbox
}

func (o divestObserverOutbox) Send(ctx context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	o.observe()
	return o.out.Send(ctx, fed, h, evt)
}

type divestObserverLog struct {
	permissiveLog
	observe func()
}

func (l *divestObserverLog) Append(context.Context, core.FederationName, core.EventRecord) error {
	l.observe()
	return nil
}

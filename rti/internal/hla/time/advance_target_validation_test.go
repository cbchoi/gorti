package time

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestAdvanceTargetValidation_RawFloat64(t *testing.T) {
	tests := []struct {
		name      string
		current   float64
		requested float64
		wantErr   error
	}{
		{"nan_initial", 0, math.NaN(), core.ErrTimeInvalidLogicalTime},
		{"nan_advanced", 2, math.NaN(), core.ErrTimeInvalidLogicalTime},
		{"nan_final", math.Inf(1), math.NaN(), core.ErrTimeInvalidLogicalTime},
		{"negative_infinity", 0, math.Inf(-1), core.ErrTimeRequestInPast},
		{"negative", 0, -1, core.ErrTimeRequestInPast},
		{"smallest_negative", 0, -math.SmallestNonzeroFloat64, core.ErrTimeRequestInPast},
		{"one_ulp_in_past", 2, math.Nextafter(2, 0), core.ErrTimeRequestInPast},
		{"negative_zero", 0, math.Copysign(0, -1), nil},
		{"initial", 0, 0, nil},
		{"equal", 2, 2, nil},
		{"one_ulp_ahead", 2, math.Nextafter(2, math.Inf(1)), nil},
		{"largest_finite", 0, math.MaxFloat64, nil},
		{"positive_infinity", 0, math.Inf(1), nil},
		{"final_equal", math.Inf(1), math.Inf(1), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkAdvanceTarget(core.LogicalTime(tt.current), core.LogicalTime(tt.requested))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("checkAdvanceTarget(%g, %g) = %v, want %v", tt.current, tt.requested, err, tt.wantErr)
			}
		})
	}
}

type advanceTargetService struct {
	name string
	mode AdvanceMode
	call func(*Manager, context.Context, core.FederationName, core.FederateHandle, core.LogicalTime) error
}

var advanceTargetServices = []advanceTargetService{
	{"TAR", ModeTAR, (*Manager).TimeAdvanceRequest},
	{"TARA", ModeTARA, (*Manager).TimeAdvanceRequestAvailable},
	{"NMR", ModeNER, (*Manager).NextMessageRequest},
	{"NMRA", ModeNMRA, (*Manager).NextMessageRequestAvailable},
	{"FQR", ModeFQR, (*Manager).FlushQueueRequest},
}

func TestAdvanceTargetValidation_InvalidRequestsAreAtomic(t *testing.T) {
	invalid := []struct {
		name    string
		target  float64
		wantErr error
	}{
		{"nan", math.NaN(), core.ErrTimeInvalidLogicalTime},
		{"negative_infinity", math.Inf(-1), core.ErrTimeRequestInPast},
		{"negative", -1, core.ErrTimeRequestInPast},
		{"smallest_negative", -math.SmallestNonzeroFloat64, core.ErrTimeRequestInPast},
	}
	for _, service := range advanceTargetServices {
		for _, eligibility := range []string{testRegulatingMode, "constrained"} {
			for _, phase := range []string{"fresh", "advanced", "pending"} {
				for _, bad := range invalid {
					t.Run(service.name+"/"+eligibility+"/"+phase+"/"+bad.name, func(t *testing.T) {
						ctx := context.Background()
						const fed core.FederationName = "invalid-target"
						const h core.FederateHandle = 1
						clock := core.NewManualClock(zeroTime())
						out := &recordingOutbox{}
						log := &recordingGrantLog{}
						mgr, err := New(Options{Clock: clock, Outbox: out, EventLog: log})
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { managerExt.Delete(mgr) })
						if eligibility == testRegulatingMode {
							err = mgr.EnableRegulation(ctx, fed, h, 1)
						} else {
							err = mgr.EnableConstrained(ctx, fed, h)
						}
						if err != nil {
							t.Fatal(err)
						}
						if phase != "fresh" {
							if err := service.call(mgr, ctx, fed, h, 2); err != nil {
								t.Fatal(err)
							}
							assertAdvanceTargetGrant(t, mgr, out, log, fed, h, 2, 1)
						}
						if phase == "pending" {
							// Only constrained requesters wait for the peer; keep the
							// fresh/advanced regulating cases unconstrained.
							if eligibility == testRegulatingMode {
								if err := mgr.EnableConstrained(ctx, fed, h); err != nil {
									t.Fatal(err)
								}
							}
							// Inspect pending admission before evaluation. FQR is
							// always grantable, including at the current time.
							if err := mgr.EnableRegulation(ctx, fed, 2, 0); err != nil {
								t.Fatal(err)
							}
							if err := mgr.recordAdvance(fed, h, 5, service.mode); err != nil {
								t.Fatal(err)
							}
							ns := snapshotAdvanceTargetState(mgr, fed, h)
							if ns == nil || !ns.pendingNER || ns.currentTime != 2 || ns.requestedTime != 5 || ns.mode != service.mode {
								t.Fatalf("pending setup = %+v", ns)
							}
						}

						before := mgr.Snapshot(fed)
						beforeState := snapshotAdvanceTargetState(mgr, fed, h)
						beforeOut, beforeLog := out.snapshot(), log.snapshot()
						clock.Advance(stdtime.Second)
						err = service.call(mgr, ctx, fed, h, core.LogicalTime(bad.target))
						if !errors.Is(err, bad.wantErr) {
							t.Errorf("request(%g) = %v, want %v", bad.target, err, bad.wantErr)
						}
						if after := mgr.Snapshot(fed); !reflect.DeepEqual(after, before) {
							t.Errorf("exported snapshot changed: before=%+v after=%+v", before, after)
						}
						if after := snapshotAdvanceTargetState(mgr, fed, h); !reflect.DeepEqual(after, beforeState) {
							t.Errorf("request state changed: before=%+v after=%+v", beforeState, after)
						}
						if after := out.snapshot(); !reflect.DeepEqual(after, beforeOut) {
							t.Errorf("outbox changed: before=%+v after=%+v", beforeOut, after)
						}
						if after := log.snapshot(); !reflect.DeepEqual(after, beforeLog) {
							t.Errorf("grant log changed: before=%+v after=%+v", beforeLog, after)
						}

						wantTime := core.LogicalTime(3)
						wantGrants := len(beforeOut) + 1
						if phase == "pending" {
							if err := mgr.DisableRegulation(ctx, fed, 2); err != nil {
								t.Fatal(err)
							}
							assertAdvanceTargetGrant(t, mgr, out, log, fed, h, 5, wantGrants)
							wantTime = 6
							wantGrants++
						}
						if err := service.call(mgr, ctx, fed, h, wantTime); err != nil {
							t.Fatalf("valid request after rejected target: %v", err)
						}
						assertAdvanceTargetGrant(t, mgr, out, log, fed, h, wantTime, wantGrants)
					})
				}
			}
		}
	}
}

func TestAdvanceTargetValidation_PositiveInfinitySentinel(t *testing.T) {
	// The current C++ HLAfloat64Time::setFinal uses +Inf, also used for
	// unbounded LBTS. Preserve acceptance, without asserting final-time
	// grant semantics beyond a request blocked by a finite peer bound.
	for _, service := range advanceTargetServices {
		t.Run(service.name, func(t *testing.T) {
			ctx := context.Background()
			const fed core.FederationName = "final-target"
			out := &recordingOutbox{}
			log := &recordingGrantLog{}
			clock := core.NewManualClock(zeroTime())
			mgr, err := New(Options{Clock: clock, Outbox: out, EventLog: log})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { managerExt.Delete(mgr) })
			if err := mgr.EnableConstrained(ctx, fed, 1); err != nil {
				t.Fatal(err)
			}
			if err := mgr.EnableRegulation(ctx, fed, 2, 0); err != nil {
				t.Fatal(err)
			}
			if err := service.call(mgr, ctx, fed, 1, core.LogicalTime(math.Inf(1))); err != nil {
				t.Fatalf("+Inf final-time sentinel rejected: %v", err)
			}
			ns := snapshotAdvanceTargetState(mgr, fed, 1)
			if service.mode == ModeFQR {
				assertAdvanceTargetGrant(t, mgr, out, log, fed, 1, 0, 1)
				return
			}
			if ns == nil || !ns.pendingNER || ns.currentTime != 0 || !math.IsInf(float64(ns.requestedTime), 1) || ns.mode != service.mode || ns.pendingSince != clock.Now() {
				t.Fatalf("final-time request state = %+v", ns)
			}
			if len(out.snapshot()) != 0 || len(log.snapshot()) != 0 {
				t.Fatal("final-time request emitted a grant past the finite peer bound")
			}
		})
	}
}

func snapshotAdvanceTargetState(mgr *Manager, fed core.FederationName, h core.FederateHandle) *nerState {
	ext := extOf(mgr)
	ext.mu.Lock()
	defer ext.mu.Unlock()
	if ns := ext.getLocked(fed, h); ns != nil {
		copyState := *ns
		if ns.tsoBuffer != nil {
			copyState.tsoBuffer = append([]bufferedTSOEvent{}, ns.tsoBuffer...)
		}
		return &copyState
	}
	return nil
}

func assertAdvanceTargetGrant(t *testing.T, mgr *Manager, out *recordingOutbox, log *recordingGrantLog, fed core.FederationName, h core.FederateHandle, wantTime core.LogicalTime, wantCount int) {
	t.Helper()
	ns := snapshotAdvanceTargetState(mgr, fed, h)
	if ns == nil || ns.currentTime != wantTime || ns.pendingNER || !ns.pendingSince.IsZero() {
		t.Fatalf("after valid request: state=%+v, want current=%g with no pending request", ns, wantTime)
	}
	for name, effects := range map[string][]recordedSend{"outbox": out.snapshot(), "grant log": log.snapshot()} {
		if len(effects) != wantCount {
			t.Fatalf("%s count=%d, want %d: %+v", name, len(effects), wantCount, effects)
		}
		if got := effects[len(effects)-1]; got != (recordedSend{fed: fed, h: h, t: wantTime}) {
			t.Fatalf("last %s entry=%+v, want federate=%d time=%g", name, got, h, wantTime)
		}
	}
}

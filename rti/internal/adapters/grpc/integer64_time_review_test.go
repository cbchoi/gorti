package grpc

import (
	"context"
	"math"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle/storage"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func reviewIntegerAdvance(ctx context.Context, service *timeServiceOf[int64], mode string, target int64) error {
	switch mode {
	case "TAR":
		_, err := service.TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: wireV1(), FederationName: "integer", FederateHandle: 2, LogicalTimeValue: integerWire(target)})
		return err
	case "TARA":
		_, err := service.TimeAdvanceRequestAvailable(ctx, &rtiv1.TARARequest{WireVersion: wireV1(), FederationName: "integer", FederateHandle: 2, LogicalTimeValue: integerWire(target)})
		return err
	case "NER":
		_, err := service.NextMessageRequest(ctx, &rtiv1.NERRequest{WireVersion: wireV1(), FederationName: "integer", FederateHandle: 2, LogicalTimeValue: integerWire(target)})
		return err
	case "NMRA":
		_, err := service.NextMessageRequestAvailable(ctx, &rtiv1.NMRARequest{WireVersion: wireV1(), FederationName: "integer", FederateHandle: 2, LogicalTimeValue: integerWire(target)})
		return err
	case "FQR":
		_, err := service.FlushQueueRequest(ctx, &rtiv1.FQRRequest{WireVersion: wireV1(), FederationName: "integer", FederateHandle: 2, LogicalTimeValue: integerWire(target)})
		return err
	default:
		panic("invalid review advance mode")
	}
}

func TestReviewInteger64GRPCAllModesFiniteFinalAndUnbounded(t *testing.T) {
	for _, mode := range []string{"TAR", "TARA", "NER", "NMRA", "FQR"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			out := &integerTimeOutbox{}
			manager, err := timepkg.NewInteger(timepkg.IntegerOptions{Clock: core.NewManualClock(time.Unix(0, 0)), Outbox: out})
			if err != nil {
				t.Fatal(err)
			}
			service := newTimeServiceOf[int64](manager)
			if err := reviewIntegerAdvance(ctx, service, mode, 1<<53+1); err != nil {
				t.Fatal(err)
			}
			grant, err := toFederateEvent(out.events[len(out.events)-1])
			if err != nil || grant.GetGrant().GetLogicalTimeValue().GetInteger64Value() != 1<<53+1 || grant.GetGrant().GetLogicalTime() != 0 {
				t.Fatalf("adjacent exact grant=%v error=%v", grant, err)
			}
			state := &timepkg.IntegerManagerState{StallTimeout: timepkg.DefaultStallTimeout, Federates: []timepkg.IntegerFederateState{
				{Handle: 1, HasRegulationState: true, HasAdvanceState: true, Regulating: true, CurrentTime: math.MaxInt64 - 1, Lookahead: 1},
				{Handle: 2, HasRegulationState: true, HasAdvanceState: true, Constrained: true, CurrentTime: math.MaxInt64 - 1},
			}}
			if err := manager.ImportState("integer", state, nil); err != nil {
				t.Fatal(err)
			}
			query := &rtiv1.QueryFederateTimeRequest{WireVersion: wireV1(), FederationName: "integer", FederateHandle: 2}
			checkBounds := func(finite bool) {
				t.Helper()
				lbts, err := service.QueryLBTS(ctx, &rtiv1.QueryLBTSRequest{WireVersion: wireV1(), FederationName: "integer"})
				if err != nil || lbts.Finite != finite || finite && (lbts.Lbts != 0 || lbts.GetLbtsValue().GetInteger64Value() != math.MaxInt64) || !finite && lbts.LbtsValue != nil {
					t.Fatalf("LBTS=%v error=%v finite=%t", lbts, err, finite)
				}
				galt, err := service.QueryGALT(ctx, query)
				if err != nil || galt.Finite != finite || finite && (galt.Galt != 0 || galt.GetGaltValue().GetInteger64Value() != math.MaxInt64) || !finite && galt.GaltValue != nil {
					t.Fatalf("GALT=%v error=%v finite=%t", galt, err, finite)
				}
				lits, err := service.QueryLITS(ctx, query)
				if err != nil || lits.Finite != finite || finite && (lits.Lits != 0 || lits.GetLitsValue().GetInteger64Value() != math.MaxInt64) || !finite && lits.LitsValue != nil {
					t.Fatalf("LITS=%v error=%v finite=%t", lits, err, finite)
				}
			}
			checkBounds(true)
			out.events = nil
			if err := reviewIntegerAdvance(ctx, service, mode, math.MaxInt64); err != nil {
				t.Fatal(err)
			}
			inclusive := mode == "TARA" || mode == "NMRA" || mode == "FQR"
			if (len(out.events) != 0) != inclusive {
				t.Fatalf("finite final bound produced callbacks=%v", out.events)
			}
			if err := manager.DisableRegulation(ctx, "integer", 1); err != nil {
				t.Fatal(err)
			}
			checkBounds(false)
			current, err := service.QueryLogicalTime(ctx, query)
			if err != nil || current.LogicalTime != 0 || current.GetLogicalTimeValue().GetInteger64Value() != math.MaxInt64 {
				t.Fatalf("final current=%v error=%v", current, err)
			}
			if len(out.events) != 1 {
				t.Fatalf("final callback count=%d", len(out.events))
			}
			grant, err = toFederateEvent(out.events[0])
			if err != nil || grant.GetGrant().GetLogicalTimeValue().GetInteger64Value() != math.MaxInt64 {
				t.Fatalf("final grant=%v error=%v", grant, err)
			}
		})
	}
}

func TestReviewFloat64GRPCPreservesLegacyFinalTime(t *testing.T) {
	ctx := context.Background()
	t.Run("request", func(t *testing.T) {
		out := &integerTimeOutbox{}
		manager, err := timepkg.New(timepkg.Options{Clock: core.NewRealClock(), Outbox: out})
		if err != nil {
			t.Fatal(err)
		}
		_, err = newTimeService(manager).TimeAdvanceRequestAvailable(ctx, &rtiv1.TARARequest{WireVersion: wireV1(), FederationName: "float", FederateHandle: 1, LogicalTime: math.Inf(1)})
		if err != nil {
			t.Fatalf("legacy +Inf TARA rejected: %v", err)
		}
	})
	t.Run("query-and-callback", func(t *testing.T) {
		out := &integerTimeOutbox{}
		manager, err := timepkg.New(timepkg.Options{Clock: core.NewRealClock(), Outbox: out})
		if err != nil {
			t.Fatal(err)
		}
		if err := manager.TimeAdvanceRequestAvailable(ctx, "float", 1, core.LogicalTime(math.Inf(1))); err != nil {
			t.Fatalf("legacy domain no longer supports final time: %v", err)
		}
		response, err := newTimeService(manager).QueryLogicalTime(ctx, &rtiv1.QueryFederateTimeRequest{WireVersion: wireV1(), FederationName: "float", FederateHandle: 1})
		if err != nil || !math.IsInf(response.GetLogicalTime(), 1) || response.LogicalTimeValue != nil {
			t.Errorf("legacy final query=%v error=%v", response, err)
		}
		callback, err := toFederateEvent(out.events[len(out.events)-1])
		if err != nil || !math.IsInf(callback.GetGrant().GetLogicalTime(), 1) || callback.GetGrant().LogicalTimeValue != nil {
			t.Errorf("legacy final grant=%v error=%v", callback, err)
		}
	})
}

func TestReviewLegacyFinalCompatibilityIsFloat64Only(t *testing.T) {
	inf := math.Inf(1)
	if _, err := decodeOptionalScalarTime[core.LogicalTime](&inf, nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("optional object/DDM/save timestamp accepted infinity: %v", err)
	}
	finite := 3.0
	optional, err := decodeOptionalScalarTime[core.LogicalTime](&finite, nil)
	if err != nil || optional == nil || *optional != 3 {
		t.Fatalf("legacy finite optional time=%v error=%v", optional, err)
	}
	*optional = 0
	if finite != 3 {
		t.Fatal("decoded optional time aliased request field")
	}
	floatWire := func(value float64) *rtiv1.LogicalTimeValue {
		return &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Float64Value{Float64Value: value}}
	}
	for _, row := range []struct {
		name   string
		legacy float64
		exact  *rtiv1.LogicalTimeValue
	}{
		{"exact-infinity", 0, floatWire(inf)},
		{"legacy-and-exact-infinity", inf, floatWire(inf)},
		{"legacy-infinity-and-finite-exact", inf, floatWire(1)},
		{"legacy-infinity-and-integer-exact", inf, integerWire(math.MaxInt64)},
		{"negative-infinity", math.Inf(-1), floatWire(math.Inf(-1))},
		{"nan", math.NaN(), floatWire(math.NaN())},
	} {
		t.Run(row.name, func(t *testing.T) {
			if _, err := decodeScalarTime[core.LogicalTime](row.legacy, row.exact); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("invalid Float64 input accepted: %v", err)
			}
			if _, err := decodeOptionalScalarTime[core.LogicalTime](&row.legacy, row.exact); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("invalid optional Float64 input accepted: %v", err)
			}
		})
	}
	for _, exact := range []*rtiv1.LogicalTimeValue{nil, integerWire(math.MaxInt64), floatWire(inf)} {
		if _, err := decodeScalarTime[int64](inf, exact); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("Integer64 accepted infinite legacy time: %v", err)
		}
		if _, err := decodeOptionalScalarTime[int64](&inf, exact); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("Integer64 accepted optional infinite legacy time: %v", err)
		}
	}
	if _, err := decodeScalarInterval[core.LogicalTime](inf, nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Float64 infinite lookahead accepted: %v", err)
	}
	legacy, exact, err := encodeScalarTime[int64](math.MaxInt64)
	if err != nil || legacy != 0 || exact == nil || exact.GetInteger64Value() != math.MaxInt64 {
		t.Fatalf("finite Integer64 final time=%v/%v error=%v", legacy, exact, err)
	}
	legacy, exact, err = encodeScalarTime[core.LogicalTime](core.LogicalTime(math.MaxFloat64))
	if err != nil || legacy != math.MaxFloat64 || exact == nil || exact.GetFloat64Value() != math.MaxFloat64 {
		t.Fatalf("finite Float64 final time=%v/%v error=%v", legacy, exact, err)
	}
}

func TestReviewInteger64GRPCLookaheadDecreaseRemainsExact(t *testing.T) {
	ctx := context.Background()
	out := &integerTimeOutbox{}
	manager, err := timepkg.NewInteger(timepkg.IntegerOptions{Clock: core.NewRealClock(), Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	const base int64 = 1<<53 + 1
	const lookahead int64 = 1<<53 + 1
	if err := manager.ImportState("integer", &timepkg.IntegerManagerState{StallTimeout: timepkg.DefaultStallTimeout, Federates: []timepkg.IntegerFederateState{{Handle: 2, HasRegulationState: true, HasAdvanceState: true, Regulating: true, CurrentTime: base, Lookahead: lookahead}}}, nil); err != nil {
		t.Fatal(err)
	}
	service := newTimeServiceOf[int64](manager)
	if _, err := service.ModifyLookahead(ctx, &rtiv1.ModifyLookaheadRequest{WireVersion: wireV1(), FederationName: "integer", FederateHandle: 2, LookaheadValue: integerWire(1)}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ target, want int64 }{{base, lookahead}, {base + 1, lookahead - 1}, {base + lookahead - 1, 1}} {
		if err := reviewIntegerAdvance(ctx, service, "TARA", row.target); err != nil {
			t.Fatal(err)
		}
		got, err := service.QueryLookahead(ctx, &rtiv1.QueryFederateTimeRequest{WireVersion: wireV1(), FederationName: "integer", FederateHandle: 2})
		if err != nil || got.Lookahead != 0 || got.GetLookaheadValue().GetInteger64Value() != row.want {
			t.Fatalf("target=%d lookahead=%v error=%v want=%d", row.target, got, err, row.want)
		}
	}
}

func TestReviewInteger64GRPCActualEnableAndSaveCallbacks(t *testing.T) {
	ctx := context.Background()
	out := &integerTimeOutbox{}
	manager, err := timepkg.NewInteger(timepkg.IntegerOptions{Clock: core.NewRealClock(), Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	const at int64 = 1<<53 + 1
	if err := manager.ImportState("integer", &timepkg.IntegerManagerState{StallTimeout: timepkg.DefaultStallTimeout, Federates: []timepkg.IntegerFederateState{{Handle: 2, HasRegulationState: true, HasAdvanceState: true, CurrentTime: at}}}, nil); err != nil {
		t.Fatal(err)
	}
	service := newTimeServiceOf[int64](manager)
	if _, err := service.EnableTimeConstrained(ctx, &rtiv1.EnableConstrainedRequest{WireVersion: wireV1(), FederationName: "integer", FederateHandle: 2}); err != nil {
		t.Fatal(err)
	}
	callback, err := toFederateEvent(out.events[len(out.events)-1])
	if err != nil || callback.GetTimeConstrainedEnabled().GetLogicalTimeValue().GetInteger64Value() != at || callback.GetTimeConstrainedEnabled().GetLogicalTime() != 0 {
		t.Fatalf("constrained callback=%v error=%v", callback, err)
	}
	if _, err := service.EnableTimeRegulation(ctx, &rtiv1.EnableRegulationRequest{WireVersion: wireV1(), FederationName: "integer", FederateHandle: 2, LookaheadValue: integerWire(1)}); err != nil {
		t.Fatal(err)
	}
	callback, err = toFederateEvent(out.events[len(out.events)-1])
	if err != nil || callback.GetTimeRegulationEnabled().GetLogicalTimeValue().GetInteger64Value() != at || callback.GetTimeRegulationEnabled().GetLogicalTime() != 0 {
		t.Fatalf("regulation callback=%v error=%v", callback, err)
	}
	for _, target := range []int64{0, at, math.MaxInt64} {
		store, err := storage.NewFSStorage(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		coordinator, err := savepoint.NewOf(savepoint.OptionsOf[int64]{Outbox: out, BundleStore: store,
			Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{2} },
			ScheduledTime: func(core.FederationName, int64, []core.FederateHandle) (func() (bool, error), func(), error) {
				return func() (bool, error) { return true, nil }, func() {}, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := coordinator.RequestFederationSave(ctx, "integer", "exact", &target); err != nil {
			t.Fatal(err)
		}
		if err := coordinator.ProgressScheduledSave(ctx, "integer"); err != nil {
			t.Fatal(err)
		}
		callback, err := toFederateEvent(out.events[len(out.events)-1])
		if err != nil || callback.GetSaveInitiate().GetLabel() != "exact" || callback.GetSaveInitiate().SaveTime != nil || callback.GetSaveInitiate().SaveTimeValue == nil || callback.GetSaveInitiate().GetSaveTimeValue().GetInteger64Value() != target {
			t.Fatalf("save callback=%v error=%v target=%d", callback, err, target)
		}
	}
}

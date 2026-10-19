package grpc

import (
	"context"
	"math"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	grpcgo "google.golang.org/grpc"
)

type routingAdminFederations struct{ core.FederationStore }

func (routingAdminFederations) Snapshot() []core.FederationRoster {
	return []core.FederationRoster{
		{Name: "float", Federates: []core.FederateInfo{{Handle: 1, Name: "float-member"}}},
		{Name: "int", Federates: []core.FederateInfo{{Handle: 2, Name: "integer-member"}}},
	}
}

type routingAdminTimeOf[T core.LogicalTimeScalar] struct {
	core.TimeManagerOf[T]
	name     core.FederationName
	snapshot core.TimeSnapshotOf[T]
}

func (m routingAdminTimeOf[T]) Snapshot(name core.FederationName) core.TimeSnapshotOf[T] {
	if name != m.name {
		panic("admin read the wrong scalar backend")
	}
	return m.snapshot
}

type routingAdminObjectsOf[T core.LogicalTimeScalar] struct{ core.ObjectRegistryOf[T] }

func (routingAdminObjectsOf[T]) Snapshot(core.FederationName) core.ObjectSnapshot {
	return core.ObjectSnapshot{InstanceCount: 7}
}

func TestAdminPerFederationExactIntegerSnapshot(t *testing.T) {
	for _, test := range []struct {
		name      string
		value     int64
		unbounded bool
	}{
		{"zero", 0, false}, {"above-float-precision", 1<<53 + 1, false},
		{"finite-final", math.MaxInt64, false}, {"unbounded", math.MaxInt64, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			opts := AdminOptions{
				Federations:         routingAdminFederations{},
				Time:                routingAdminTimeOf[core.LogicalTime]{name: "float", snapshot: core.TimeSnapshot{LBTS: 3.5, Federates: []core.TimeFederateState{{Handle: 1, CurrentTime: 3.5, Lookahead: .5}}}},
				TimeImplementations: routingFactories{"float": float64TimeFactory, "int": integer64TimeFactory},
				Integer64:           &TypedBackends[int64]{Objects: routingAdminObjectsOf[int64]{}, Time: routingAdminTimeOf[int64]{name: "int", snapshot: core.TimeSnapshotOf[int64]{LBTS: test.value, LBTSUnbounded: test.unbounded, Federates: []core.TimeFederateStateOf[int64]{{Handle: 2, CurrentTime: test.value, Lookahead: 1, Regulating: true, HasPendingRequest: true, PendingRequestTime: math.MaxInt64}}}}},
			}
			conn := saveDDMExactRPCConn(t, func(gs *grpcgo.Server) {
				if err := RegisterAdminService(gs, opts); err != nil {
					t.Fatal(err)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			response, err := rtiv1.NewAdminServiceClient(conn).Snapshot(ctx, &rtiv1.SnapshotRequest{WireVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			if len(response.Federations) != 2 {
				t.Fatal("admin lost a federation")
			}
			if got := response.Federations[0].Federates[0].CurrentTime; got != 3.5 {
				t.Fatal("legacy Float64 admin value changed")
			}
			integer := response.Federations[1]
			row := integer.Federates[0]
			if row.CurrentTime != 0 || row.Lookahead != 0 || row.PendingRequestTime != nil || row.CurrentTimeValue.GetInteger64Value() != test.value || row.LookaheadValue.GetInteger64Value() != 1 || row.PendingRequestTimeValue.GetInteger64Value() != math.MaxInt64 {
				t.Fatalf("integer admin times are not exact: %v", row)
			}
			if row.CurrentTimeValue == nil || integer.ObjectInstanceCount != 7 {
				t.Fatal("missing integer zero or object snapshot")
			}
			if integer.Time.PendingGrants[0].RequestedTimeValue.GetInteger64Value() != math.MaxInt64 {
				t.Fatal("pending grant lost precision")
			}
			if test.unbounded {
				if integer.Time.LbtsValue != nil || !math.IsInf(integer.Time.Lbts, 1) {
					t.Fatal("unbounded horizon became finite final")
				}
			} else if integer.Time.Lbts != 0 || integer.Time.LbtsValue == nil || integer.Time.LbtsValue.GetInteger64Value() != test.value {
				t.Fatal("finite integer LBTS changed")
			}
		})
	}
}

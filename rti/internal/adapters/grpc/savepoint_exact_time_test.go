package grpc

import (
	"context"
	"math"
	"net"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func saveDDMExactRPCConn(t *testing.T, register func(*grpcgo.Server)) *grpcgo.ClientConn {
	t.Helper()
	server := grpcgo.NewServer()
	register(server)
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpcgo.NewClient("passthrough:///save-ddm-exact", grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

type savepointExactRecorderOf[T core.LogicalTimeScalar] struct {
	core.SavepointCoordinatorOf[T]
	calls chan *T
}

func (r *savepointExactRecorderOf[T]) RequestFederationSave(_ context.Context, _ core.FederationName, _ string, at *T) error {
	if at != nil {
		cloned := *at
		at = &cloned
	}
	r.calls <- at
	return nil
}

type optionalExactTimeCase[T core.LogicalTimeScalar] struct {
	name    string
	legacy  *float64
	exact   *rtiv1.LogicalTimeValue
	want    *T
	invalid bool
}

func floatTimeSelector(value float64) *rtiv1.LogicalTimeValue {
	return &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Float64Value{Float64Value: value}}
}

func integerTimeSelector(value int64) *rtiv1.LogicalTimeValue {
	return &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Integer64Value{Integer64Value: value}}
}

func exactTimePtr[T core.LogicalTimeScalar](value T) *T { return &value }

func integerOptionalTimeCases() []optionalExactTimeCase[int64] {
	zero, one := 0.0, 1.0
	return []optionalExactTimeCase[int64]{
		{name: "absent"},
		{name: "zero", exact: integerTimeSelector(0), want: exactTimePtr[int64](0)},
		{name: "above-float-precision", exact: integerTimeSelector(1<<53 + 1), want: exactTimePtr[int64](1<<53 + 1)},
		{name: "final", exact: integerTimeSelector(math.MaxInt64), want: exactTimePtr[int64](math.MaxInt64)},
		{name: "legacy-only-zero", legacy: &zero, invalid: true},
		{name: "legacy-only-one", legacy: &one, invalid: true},
		{name: "explicit-zero-conflict", legacy: &zero, exact: integerTimeSelector(1<<53 + 1), invalid: true},
		{name: "explicit-one-conflict", legacy: &one, exact: integerTimeSelector(1), invalid: true},
		{name: "wrong-float-selector", exact: floatTimeSelector(0), invalid: true},
		{name: "negative", exact: integerTimeSelector(-1), invalid: true},
		{name: "empty-selector", exact: &rtiv1.LogicalTimeValue{}, invalid: true},
	}
}

func floatOptionalTimeCases() []optionalExactTimeCase[core.LogicalTime] {
	zero, half, one := 0.0, 0.5, 1.0
	cases := []optionalExactTimeCase[core.LogicalTime]{
		{name: "absent"},
		{name: "legacy-zero", legacy: &zero, want: exactTimePtr(core.LogicalTime(0))},
		{name: "legacy-half", legacy: &half, want: exactTimePtr(core.LogicalTime(0.5))},
		{name: "exact-only", exact: floatTimeSelector(0.5), want: exactTimePtr(core.LogicalTime(0.5))},
		{name: "matching-pair", legacy: &half, exact: floatTimeSelector(0.5), want: exactTimePtr(core.LogicalTime(0.5))},
		{name: "zero-matching-pair", legacy: &zero, exact: floatTimeSelector(0), want: exactTimePtr(core.LogicalTime(0))},
		{name: "zero-conflict", legacy: &zero, exact: floatTimeSelector(0.5), invalid: true},
		{name: "nonzero-conflict", legacy: &one, exact: floatTimeSelector(0.5), invalid: true},
		{name: "wrong-integer-selector", exact: integerTimeSelector(0), invalid: true},
		{name: "empty-selector", exact: &rtiv1.LogicalTimeValue{}, invalid: true},
	}
	for name, value := range map[string]float64{"negative": -1, "nan": math.NaN(), "infinite": math.Inf(1)} {
		cases = append(cases, optionalExactTimeCase[core.LogicalTime]{name: "legacy-" + name, legacy: exactTimePtr(value), invalid: true}, optionalExactTimeCase[core.LogicalTime]{name: "exact-" + name, exact: floatTimeSelector(value), invalid: true})
	}
	return cases
}

func runSavepointExactTimeRPC[T core.LogicalTimeScalar](t *testing.T, cases []optionalExactTimeCase[T]) {
	t.Helper()
	recorder := &savepointExactRecorderOf[T]{calls: make(chan *T, 1)}
	conn := saveDDMExactRPCConn(t, func(server *grpcgo.Server) {
		rtiv1.RegisterSavepointServiceServer(server, newSavepointServiceOf[T](recorder))
	})
	client := rtiv1.NewSavepointServiceClient(conn)
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := client.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, Label: "save", SaveTime: test.legacy, SaveTimeValue: test.exact})
			if test.invalid {
				if status.Code(err) != codes.InvalidArgument {
					t.Fatalf("invalid request returned %v", err)
				}
				if len(recorder.calls) != 0 {
					t.Fatal("invalid timestamp reached save coordinator")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			at := <-recorder.calls
			if (at == nil) != (test.want == nil) || at != nil && *at != *test.want {
				t.Fatalf("timestamp changed: got %v want %v", at, test.want)
			}
		})
	}
}

func TestSavepointInteger64ExactTimeRPC(t *testing.T) {
	runSavepointExactTimeRPC(t, integerOptionalTimeCases())
}
func TestSavepointFloat64LegacyExactTimeRPC(t *testing.T) {
	runSavepointExactTimeRPC(t, floatOptionalTimeCases())
}

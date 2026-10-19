package federate

import (
	"context"
	"errors"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type timeSelectionClient struct {
	compensatingFederationClient
	createName, joinName   string
	createErr              error
	createRequest          *rtiv1.CreateFederationRequest
	createCalls, joinCalls int
}

func (c *timeSelectionClient) CreateFederation(_ context.Context, req *rtiv1.CreateFederationRequest, _ ...grpc.CallOption) (*rtiv1.CreateFederationResponse, error) {
	c.createCalls++
	c.createRequest = req
	return &rtiv1.CreateFederationResponse{EffectiveSeed: 7, FederationGeneration: 4, LogicalTimeImplementationName: c.createName}, c.createErr
}

func (c *timeSelectionClient) JoinFederation(context.Context, *rtiv1.JoinFederationRequest, ...grpc.CallOption) (*rtiv1.JoinFederationResponse, error) {
	c.joinCalls++
	return &rtiv1.JoinFederationResponse{FederateHandle: 9, LogicalTimeImplementationName: c.joinName}, nil
}

func (c *timeSelectionClient) ListFederations(context.Context, *rtiv1.ListFederationsRequest, ...grpc.CallOption) (*rtiv1.ListFederationsResponse, error) {
	return &rtiv1.ListFederationsResponse{Federations: []*rtiv1.FederationSummary{
		{Name: testFederationName, FederationGeneration: 4, LogicalTimeImplementationName: c.createName},
	}}, nil
}

func TestLogicalTimeSelectionCreateMetadata(t *testing.T) {
	for _, requested := range []string{"", HLAfloat64Time} {
		for _, returned := range []string{"", HLAfloat64Time} {
			t.Run(requested+"/"+returned, func(t *testing.T) {
				client := &timeSelectionClient{createName: returned}
				conn := &Connection{cc: &grpc.ClientConn{}, fed: client}
				created, err := conn.CreateFederation(context.Background(), FederationSpec{Name: testFederationName, LogicalTimeImplementationName: requested})
				if err != nil || created.LogicalTimeImplementationName != HLAfloat64Time || created.EffectiveSeed != 7 || created.FederationGeneration != 4 {
					t.Fatalf("create=%+v err=%v", created, err)
				}
				if client.createRequest.GetLogicalTimeImplementationName() != requested {
					t.Fatalf("wire selector=%q want %q", client.createRequest.GetLogicalTimeImplementationName(), requested)
				}
			})
		}
	}
}

func TestLogicalTimeSelectionRejectsBeforeAnyRPC(t *testing.T) {
	for _, name := range []string{"unsupportedTime", "custom", "hlafloat64time", " HLAfloat64Time "} {
		client := &timeSelectionClient{}
		conn := &Connection{cc: &grpc.ClientConn{}, fed: client}
		spec := FederationSpec{Name: testFederationName, LogicalTimeImplementationName: name}
		if _, err := conn.CreateFederation(context.Background(), spec); !errors.Is(err, ErrCouldNotCreateLogicalTimeFactory) {
			t.Fatalf("create %q: %v", name, err)
		}
		if _, err := conn.JoinFederation(context.Background(), spec, "alice"); !errors.Is(err, ErrCouldNotCreateLogicalTimeFactory) {
			t.Fatalf("create-or-join %q: %v", name, err)
		}
		if client.createCalls != 0 || client.joinCalls != 0 {
			t.Fatal("unsupported selector reached wire")
		}
	}
}

func TestLogicalTimeSelectionJoinMetadata(t *testing.T) {
	for _, returned := range []string{"", HLAfloat64Time} {
		for _, existing := range []bool{false, true} {
			client := &timeSelectionClient{joinName: returned}
			if existing {
				client.createErr = status.Error(codes.AlreadyExists, "exists")
			}
			stream := &callbackFallbackClient{batch: &scriptedCallbackBatchStream{batches: []*rtiv1.FederateEventBatch{{Ready: true}}}}
			conn := &Connection{cc: &grpc.ClientConn{}, fed: client, stream: stream}
			fed, err := conn.JoinFederation(context.Background(), FederationSpec{Name: testFederationName, LogicalTimeImplementationName: HLAfloat64Time}, "alice")
			if err != nil {
				t.Fatal(err)
			}
			if fed.LogicalTimeImplementationName() != HLAfloat64Time || client.createRequest.GetLogicalTimeImplementationName() != HLAfloat64Time {
				t.Fatal("join selection was dropped")
			}
			fed.streamCancel()
			fed.interactionCancel()
			<-fed.drainDone
		}
	}
}

func TestLogicalTimeSelectionUnsupportedJoinCompensates(t *testing.T) {
	client := &timeSelectionClient{joinName: "unsupportedTime"}
	conn := &Connection{cc: &grpc.ClientConn{}, fed: client}
	joined, err := conn.JoinFederation(context.Background(), FederationSpec{Name: testFederationName}, "alice")
	if joined != nil || !errors.Is(err, ErrCouldNotCreateLogicalTimeFactory) || client.resignCalls != 1 || client.resignRequest.GetFederateHandle() != 9 {
		t.Fatalf("joined=%v err=%v cleanup=%d", joined, err, client.resignCalls)
	}
}

func TestLogicalTimeSelectionListDoesNotInventSupport(t *testing.T) {
	for _, name := range []string{"", HLAfloat64Time, "HLAinteger64Time", "future"} {
		conn := &Connection{cc: &grpc.ClientConn{}, fed: &timeSelectionClient{createName: name}}
		rows, err := conn.ListFederations(context.Background())
		want := name
		if want == "" {
			want = HLAfloat64Time
		}
		if err != nil || len(rows) != 1 || rows[0].LogicalTimeImplementationName != want {
			t.Fatalf("list=%+v err=%v want=%q", rows, err, want)
		}
	}
}

func TestLogicalTimeSelectionServerErrorMapping(t *testing.T) {
	client := &timeSelectionClient{createErr: status.Error(codes.InvalidArgument, "could not create logical time factory: unsupported")}
	conn := &Connection{cc: &grpc.ClientConn{}, fed: client}
	if _, err := conn.CreateFederation(context.Background(), FederationSpec{Name: testFederationName}); !errors.Is(err, ErrCouldNotCreateLogicalTimeFactory) {
		t.Fatal(err)
	}
	if _, err := conn.JoinFederation(context.Background(), FederationSpec{Name: testFederationName}, "alice"); !errors.Is(err, ErrCouldNotCreateLogicalTimeFactory) {
		t.Fatal(err)
	}
}

func TestLogicalTimeSelectionServerLifecycle(t *testing.T) {
	rtid := newTestRtid(t)
	defer rtid.stop()
	conn := rtid.connect(t)
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i, name := range []string{"", HLAfloat64Time} {
		spec := FederationSpec{Name: []string{"default-time", "selected-time"}[i], LogicalTimeImplementationName: name}
		created, err := conn.CreateFederation(ctx, spec)
		if err != nil || created.LogicalTimeImplementationName != HLAfloat64Time {
			t.Fatalf("created=%+v err=%v", created, err)
		}
		rows, err := conn.ListFederations(ctx)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range rows {
			if row.Name == spec.Name {
				found = true
				if row.LogicalTimeImplementationName != HLAfloat64Time {
					t.Fatalf("row=%+v", row)
				}
			}
		}
		if !found {
			t.Fatal("created federation missing from list")
		}
		joined, err := conn.JoinFederation(ctx, spec, "alice")
		if err != nil {
			t.Fatal(err)
		}
		if joined.LogicalTimeImplementationName() != HLAfloat64Time {
			t.Fatal("joined type changed")
		}
		if err := joined.Resign(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"HLAinteger64Time", "custom"} {
		_, err := conn.fed.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: "rejected", LogicalTimeImplementationName: name})
		if !errors.Is(wrapStatusErr(err), ErrCouldNotCreateLogicalTimeFactory) {
			t.Fatalf("raw server selector %q: %v", name, err)
		}
	}
	rows, err := conn.ListFederations(ctx)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rejected creation changed list: %+v %v", rows, err)
	}
}

func TestLogicalTimeSelectionUnsupportedCreateMetadata(t *testing.T) {
	client := &timeSelectionClient{createName: "unsupportedTime"}
	conn := &Connection{cc: &grpc.ClientConn{}, fed: client}
	if _, err := conn.CreateFederation(context.Background(), FederationSpec{Name: testFederationName}); !errors.Is(err, ErrRTIInternal) || errors.Is(err, ErrCouldNotCreateLogicalTimeFactory) {
		t.Fatal(err)
	}
	if _, err := conn.JoinFederation(context.Background(), FederationSpec{Name: testFederationName}, "alice"); !errors.Is(err, ErrRTIInternal) || errors.Is(err, ErrCouldNotCreateLogicalTimeFactory) {
		t.Fatal(err)
	}
	if client.joinCalls != 0 {
		t.Fatal("unsupported created type allowed join")
	}
}

package ddm_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type completionDDM struct {
	t           *testing.T
	name        string
	ctx         [2]context.Context
	owner       [2]uint64
	ddm         rtiv1.DDMServiceClient
	support     rtiv1.SupportServiceClient
	space, x, y uint64
}

func completionCheck(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func completionDial(ctx context.Context, endpoint string) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient("passthrough:///"+endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	conn.Connect()
	for state := conn.GetState(); state != connectivity.Ready; state = conn.GetState() {
		if !conn.WaitForStateChange(ctx, state) {
			return nil, errors.Join(ctx.Err(), conn.Close())
		}
	}
	return conn, nil
}

func completionFederation(t *testing.T) *completionDDM {
	t.Helper()
	endpoint := os.Getenv("GORTI_COMPLETION_DDM_ENDPOINT")
	if endpoint == "" {
		t.Skip("requires an explicitly supplied real RTI endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	conn, err := completionDial(ctx, endpoint)
	completionCheck(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	fom, err := os.ReadFile("../../../tests/integration/ddm_registration/testdata/registration.xml")
	completionCheck(t, err)
	f := &completionDDM{t: t, name: fmt.Sprintf("completion-ddm-%d", time.Now().UnixNano()), ddm: rtiv1.NewDDMServiceClient(conn), support: rtiv1.NewSupportServiceClient(conn)} //nolint:forbidigo // Live endpoint tests need distinct federation names across processes.
	fm := rtiv1.NewFederationServiceClient(conn)
	created, err := fm.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: f.name, FomModules: []*rtiv1.FOMModule{{Path: "registration.xml", Xml: fom}}, Seed: 1701})
	completionCheck(t, err)
	for i := range f.owner {
		joined, err := fm.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: f.name, FederateName: fmt.Sprintf("owner-%d", i), FederateType: "completion-ddm", ExpectedFederationGeneration: &created.FederationGeneration})
		completionCheck(t, err)
		f.owner[i] = joined.FederateHandle
		f.ctx[i] = metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(joined.SessionToken))
	}
	space, err := f.ddm.LookupRoutingSpace(f.ctx[0], &rtiv1.LookupRoutingSpaceRequest{WireVersion: 1, FederationName: f.name, Name: "default"})
	completionCheck(t, err)
	if !space.Found || space.RoutingSpaceHandle == 0 {
		t.Fatal("missing default routing space")
	}
	f.space = space.RoutingSpaceHandle
	for name, dest := range map[string]*uint64{"X": &f.x, "Y": &f.y} {
		dim, err := f.ddm.LookupDimension(f.ctx[0], &rtiv1.LookupDimensionRequest{WireVersion: 1, FederationName: f.name, RoutingSpaceHandle: f.space, Name: name})
		completionCheck(t, err)
		if !dim.Found || dim.DimensionHandle == 0 {
			t.Fatalf("missing %s dimension", name)
		}
		*dest = dim.DimensionHandle
	}
	return f
}

func (f *completionDDM) region(owner int, dims ...uint64) uint64 {
	f.t.Helper()
	r, err := f.ddm.CreateRegion(f.ctx[owner], &rtiv1.CreateRegionRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[owner], RoutingSpaceHandle: f.space, DimensionHandles: dims})
	completionCheck(f.t, err)
	if r.RegionHandle == 0 {
		f.t.Fatal("zero region handle")
	}
	return r.RegionHandle
}

func (f *completionDDM) set(owner int, region, dim, lo, hi uint64) {
	f.t.Helper()
	_, err := f.ddm.SetRangeBounds(f.ctx[owner], &rtiv1.SetRangeBoundsRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[owner], RegionHandle: region, DimensionHandle: dim, Bounds: &rtiv1.Range{Lower: lo, Upper: hi}})
	completionCheck(f.t, err)
}

func (f *completionDDM) commit(owner int, regions ...uint64) {
	f.t.Helper()
	_, err := f.ddm.CommitRegionModifications(f.ctx[owner], &rtiv1.CommitRegionRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[owner], RegionHandles: regions})
	completionCheck(f.t, err)
}

func (f *completionDDM) bounds(owner int, region, dim, lo, hi uint64) {
	f.t.Helper()
	r, err := f.ddm.QueryBounds(f.ctx[owner], &rtiv1.QueryBoundsRequest{WireVersion: 1, FederationName: f.name, RegionHandle: region, DimensionHandle: dim})
	completionCheck(f.t, err)
	if !r.Found || r.Bounds == nil || r.Bounds.Lower != lo || r.Bounds.Upper != hi {
		f.t.Fatalf("bounds = %v; want [%d,%d)", r, lo, hi)
	}
}

func completionException(t *testing.T, err error, trailer metadata.MD, want string) {
	t.Helper()
	code := codes.NotFound
	if want == "RegionNotCreatedByThisFederate" {
		code = codes.PermissionDenied
	} else if want == "RegionInUseForUpdateOrSubscription" {
		code = codes.FailedPrecondition
	}
	values := trailer.Get("rti-spec-exception")
	if status.Code(err) != code || len(values) != 1 || values[0] != want {
		t.Fatalf("error=%v (%s), exception=%v; want %s", err, status.Code(err), values, want)
	}
}

func (f *completionDDM) invalid(region uint64) {
	f.t.Helper()
	var trailer metadata.MD
	r, err := f.ddm.QueryBounds(f.ctx[0], &rtiv1.QueryBoundsRequest{WireVersion: 1, FederationName: f.name, RegionHandle: region, DimensionHandle: f.x}, grpc.Trailer(&trailer))
	completionException(f.t, err, trailer, "InvalidRegion")
	if r != nil {
		f.t.Fatal("invalid region returned bounds")
	}
}

// S1516.1 9.1.3.1: setters remain pending and the last setter per dimension wins.
// Product QueryBounds observes committed state, not Java getRangeBounds drafts.
func TestCompletionDDMRepeatedSettersLastValue(t *testing.T) {
	f := completionFederation(t)
	r := f.region(0, f.x, f.y)
	f.set(0, r, f.x, 10, 20)
	f.set(0, r, f.x, 30, 40)
	f.set(0, r, f.y, 50, 60)
	f.set(0, r, f.y, 70, 80)
	f.invalid(r)
	f.commit(0, r)
	f.bounds(0, r, f.x, 30, 40)
	f.bounds(0, r, f.y, 70, 80)
	f.set(0, r, f.x, 100, 110)
	f.set(0, r, f.x, 120, 130)
	f.bounds(0, r, f.x, 30, 40)
	f.bounds(0, r, f.y, 70, 80)
	f.commit(0, r)
	f.bounds(0, r, f.x, 120, 130)
	f.bounds(0, r, f.y, 70, 80)
	f.commit(0, r)
	f.bounds(0, r, f.x, 120, 130)
}

// S1516.1 9.3 and 9.1.3.1: rejecting a mixed commit must not apply or lose valid drafts.
func TestCompletionDDMMixedCommitPreservesEveryRegion(t *testing.T) {
	for _, kind := range []string{"unknown", "deleted", "foreign", "incomplete"} {
		for _, invalidFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/invalid-first-%t", kind, invalidFirst), func(t *testing.T) {
				f := completionFederation(t)
				a, b := f.region(0, f.x), f.region(0, f.x)
				f.set(0, a, f.x, 10, 20)
				f.set(0, b, f.x, 30, 40)
				f.commit(0, a, b)
				f.set(0, a, f.x, 50, 60)
				f.set(0, a, f.x, 70, 80)
				f.set(0, b, f.x, 90, 100)
				bad, exception := ^uint64(0), "InvalidRegion"
				switch kind {
				case "deleted":
					bad = f.region(0, f.x)
					_, err := f.ddm.DeleteRegion(f.ctx[0], &rtiv1.DeleteRegionRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[0], RegionHandle: bad})
					completionCheck(t, err)
				case "foreign":
					bad = f.region(1, f.x)
					f.set(1, bad, f.x, 110, 120)
					f.commit(1, bad)
					f.set(1, bad, f.x, 130, 140)
					exception = "RegionNotCreatedByThisFederate"
				case "incomplete":
					bad = f.region(0, f.x, f.y)
					f.set(0, bad, f.x, 150, 160)
				}
				regions := []uint64{a, b, bad}
				if invalidFirst {
					regions = []uint64{bad, b, a}
				}
				var trailer metadata.MD
				_, err := f.ddm.CommitRegionModifications(f.ctx[0], &rtiv1.CommitRegionRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[0], RegionHandles: regions}, grpc.Trailer(&trailer))
				completionException(t, err, trailer, exception)
				f.bounds(0, a, f.x, 10, 20)
				f.bounds(0, b, f.x, 30, 40)
				f.commit(0, a, b)
				f.bounds(0, a, f.x, 70, 80)
				f.bounds(0, b, f.x, 90, 100)
				switch kind {
				case "foreign":
					f.bounds(1, bad, f.x, 110, 120)
					f.commit(1, bad)
					f.bounds(1, bad, f.x, 130, 140)
				case "incomplete":
					f.invalid(bad)
					f.set(0, bad, f.y, 170, 180)
					f.commit(0, bad)
					f.bounds(0, bad, f.x, 150, 160)
					f.bounds(0, bad, f.y, 170, 180)
				default:
					f.invalid(bad)
				}
			})
		}
	}
}

// S1516.1 9.4: subscription use prevents deletion; removing that use permits it.
func TestCompletionDDMRegionSubscriptionDeletionLifecycle(t *testing.T) {
	f := completionFederation(t)
	r := f.region(0, f.x)
	f.set(0, r, f.x, 10, 20)
	f.commit(0, r)
	cls, err := f.support.GetInteractionClassHandle(f.ctx[0], &rtiv1.GetInteractionClassHandleRequest{WireVersion: 1, FederationName: f.name, ClassName: "HLAinteractionRoot.RegionalSignal"})
	completionCheck(t, err)
	_, err = f.ddm.SubscribeInteractionClassWithRegions(f.ctx[0], &rtiv1.SubscribeICWithRegionsRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[0], InteractionClassHandle: cls.ClassHandle, RegionHandles: []uint64{r}})
	completionCheck(t, err)
	var trailer metadata.MD
	_, err = f.ddm.DeleteRegion(f.ctx[0], &rtiv1.DeleteRegionRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[0], RegionHandle: r}, grpc.Trailer(&trailer))
	completionException(t, err, trailer, "RegionInUseForUpdateOrSubscription")
	f.set(0, r, f.x, 30, 40)
	f.set(0, r, f.x, 50, 60)
	f.bounds(0, r, f.x, 10, 20)
	f.commit(0, r)
	f.bounds(0, r, f.x, 50, 60)
	_, err = f.ddm.UnsubscribeInteractionClassWithRegions(f.ctx[0], &rtiv1.UnsubscribeICWithRegionsRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[0], InteractionClassHandle: cls.ClassHandle, RegionHandles: []uint64{r}})
	completionCheck(t, err)
	_, err = f.ddm.DeleteRegion(f.ctx[0], &rtiv1.DeleteRegionRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[0], RegionHandle: r})
	completionCheck(t, err)
	f.invalid(r)
	replacement := f.region(0, f.x)
	if replacement == r {
		t.Fatal("deleted region handle reused")
	}
	f.set(0, replacement, f.x, 70, 80)
	f.commit(0, replacement)
	f.bounds(0, replacement, f.x, 70, 80)
	f.invalid(r)
}

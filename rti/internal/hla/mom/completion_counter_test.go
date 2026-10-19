package mom_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// S1516.1 8.1.1(b), p182 defines receipt as callback invocation; 11.6,
// Table 6 p297 and Tables 9/10 p307 specify HLAcount as integer32BE.
func TestMOMCompletionStandardCountersRequireAuthenticatedEntry(t *testing.T) {
	const completionReflectService = "reflectAttributeValues"
	for _, service := range []string{"receiveInteraction", completionReflectService} {
		t.Run(service, func(t *testing.T) {
			ctx := context.Background()
			const fed core.FederationName = "completion-counter"
			members, err := federation.New(federation.Options{
				Clock: core.NewManualClock(time.Unix(0, 0)),
				FOMs:  composition.NewFOMRepository(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := members.CreateFederation(ctx, core.CreateFederationRequest{
				Name: fed, FOMModules: []core.FOMModule{{Path: "counter.xml", XML: []byte(
					`<objectModel><objects><objectClass><name>HLAobjectRoot</name></objectClass></objects></objectModel>`)}},
			}); err != nil {
				t.Fatal(err)
			}
			var joined []core.FederateHandle
			t.Cleanup(func() {
				for _, h := range joined {
					if err := members.ResignFederation(ctx, fed, h, core.ResignActionNoAction); err != nil {
						t.Error(err)
					}
				}
				if err := members.DestroyFederation(ctx, fed); err != nil {
					t.Error(err)
				}
			})
			m, err := mom.New(mom.Options{Outbox: pendingEntryOutbox{}})
			if err != nil {
				t.Fatal(err)
			}
			for _, enable := range []func() error{
				func() error { return m.SetRuntimeMetricsEnabled(true) },
				func() error { return m.SetDataEntryMetricsEnabled(true) },
				func() error { return m.SetTrafficAccumulator(mom.NewTrafficAccumulator()) },
			} {
				if err := enable(); err != nil {
					t.Fatal(err)
				}
			}
			auth, err := session.New(session.Options{Membership: members})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(auth.Close)
			var receiver core.FederateHandle
			contexts := map[string]context.Context{}
			for _, name := range []string{"receiver", "foreign"} {
				h, err := members.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: name})
				if err != nil {
					t.Fatal(err)
				}
				joined = append(joined, h)
				id, err := members.MemberIdentity(fed, h)
				if err != nil {
					t.Fatal(err)
				}
				token, err := session.NewToken()
				if err != nil {
					t.Fatal(err)
				}
				auth.Register(id, token)
				contexts[name] = metadata.NewIncomingContext(ctx, metadata.Pairs(session.MetadataKey, string(token)))
				if err := m.FederateJoined(ctx, fed, h, name, "counter-test"); err != nil {
					t.Fatal(err)
				}
				if name == "receiver" {
					receiver = h
				}
			}
			observedEntries := 0
			receipts, err := grpcadapter.NewCallbackReceipts(grpcadapter.CallbackReceiptOptions{
				Authenticate: auth.Authenticate,
				Pending:      m.CallbackPending,
				ObserveEntry: func(_ context.Context, f core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent) error {
					entry := mom.DataCallbackEntry{Service: service, Transportation: core.TransportTypeReliable}
					if interaction := event.GetReceive(); interaction != nil {
						entry.InteractionClass = core.InteractionClassHandle(interaction.InteractionClassHandle)
					} else if reflection := event.GetReflect(); reflection != nil {
						entry.Object = core.ObjectHandle(reflection.ObjectHandle)
						entry.ObjectClass = core.ObjectClassHandle(reflection.ObjectClassHandle)
					} else {
						t.Fatal("unexpected callback kind")
					}
					if err := m.RecordDataCallbackEntry(f, h, entry); err != nil {
						return err
					}
					observedEntries++
					return nil
				},
				Observe: func(_ context.Context, f core.FederationName, h core.FederateHandle, _ *rtiv1.FederateEvent, _ bool, _ string) error {
					return m.RecordCallbackCompletion(f, h, mom.CallbackCompletion{
						Invocation:         mom.CallbackInvocation{Service: service},
						DataCountedAtEntry: true,
					})
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			// Literal standard payloads, never encoded with the implementation under test.
			counts := [][]byte{{0, 0, 0, 0}, {0, 0, 0, 1}, {0, 0, 0, 2}}
			assertCounters := func(phase string, invocations int) {
				t.Helper()
				values, err := m.FederateMetricValues(ctx, fed, receiver)
				if err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"HLAinteractionsReceived", "HLAreflectionsReceived", "HLAobjectInstancesReflected"} {
					want := counts[0]
					if name == "HLAinteractionsReceived" && service == "receiveInteraction" || name == "HLAreflectionsReceived" && service == completionReflectService {
						want = counts[invocations]
					} else if name == "HLAobjectInstancesReflected" && service == completionReflectService && invocations > 0 {
						want = counts[1]
					}
					if why := values.Unavailable[name]; why != "" {
						t.Fatalf("%s: %s unavailable: %s", phase, name, why)
					}
					if got := values.Values[name]; !bytes.Equal(got, want) {
						t.Fatalf("%s: %s payload=%x want literal=%x", phase, name, got, want)
					}
				}
			}
			assertCounters("before any delivery", 0)
			for index := 0; index < 2; index++ {
				event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{InteractionClassHandle: 9}}}
				if service == completionReflectService {
					// Two callbacks for the same object: two reflections, one reflected object.
					event = &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Reflect{Reflect: &rtiv1.ReflectAttributeValues{
						ObjectHandle: 5, ObjectClassHandle: 9, Attributes: map[uint64][]byte{11: {1}, 12: {2}},
					}}}
				}
				ticket, commit, err := receipts.PrepareRequired(contexts["receiver"], fed, receiver, event)
				if err != nil || len(ticket) != 32 {
					t.Fatalf("required receipt=%x err=%v", ticket, err)
				}
				commit(true)
				assertCounters("raw delivery committed without callback ENTRY", index)
				for _, denied := range []struct {
					ctx  context.Context
					code codes.Code
				}{{ctx, codes.Unauthenticated}, {contexts["foreign"], codes.PermissionDenied}} {
					if err := receipts.ReportEntry(denied.ctx, fed, receiver, ticket); status.Code(err) != denied.code {
						t.Fatalf("unauthorized ENTRY=%v want %v", err, denied.code)
					}
					assertCounters("rejected ENTRY", index)
				}
				if err := receipts.ReportEntry(contexts["receiver"], fed, receiver, ticket); err != nil {
					t.Fatal(err)
				}
				assertCounters("authenticated ENTRY", index+1)
				if err := receipts.ReportEntry(contexts["receiver"], fed, receiver, ticket); err != nil {
					t.Fatal(err)
				}
				assertCounters("duplicate ENTRY", index+1)
				success, exception := index == 0, ""
				if !success {
					exception = "user callback failed after entry"
				}
				for repeat := 0; repeat < 2; repeat++ {
					if err := receipts.Report(contexts["receiver"], fed, receiver, ticket, success, exception); err != nil {
						t.Fatal(err)
					}
					assertCounters("completion or repeated completion", index+1)
				}
			}
			if observedEntries != 2 {
				t.Fatalf("entry observer calls=%d want=2", observedEntries)
			}
		})
	}
}

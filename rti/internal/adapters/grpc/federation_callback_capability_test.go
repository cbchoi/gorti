package grpc

import (
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func checkJoinCallbackEntryCapability[T core.LogicalTimeScalar](t *testing.T, value T) {
	t.Helper()
	for _, test := range []struct {
		name                                                  string
		tracker, authenticate, observer, entry, support, want bool
	}{
		{name: resignModeLegacy, support: true},
		{name: "completion-only", tracker: true, authenticate: true, observer: true, support: true},
		{name: "missing-authentication", tracker: true, observer: true, entry: true, support: true},
		{name: "missing-completion", tracker: true, authenticate: true, entry: true, support: true},
		{name: "missing-support-service", tracker: true, authenticate: true, observer: true, entry: true},
		{name: "configured-entry", tracker: true, authenticate: true, observer: true, entry: true, support: true, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			options, _, _, _ := typedAssemblyFixture(value)
			options.Federations = &fakeFedStore{joinHandle: 7}
			if test.support {
				options.FOMs = struct{ core.FOMRepository }{}
			}
			if test.tracker {
				options.CallbackReceipts = &CallbackReceipts{}
				if test.authenticate {
					options.CallbackReceipts.opts.Authenticate = func(context.Context, core.FederationName, core.FederateHandle) error { return nil }
				}
				if test.observer {
					options.CallbackReceipts.opts.Observe = func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
						return nil
					}
				}
				if test.entry {
					options.CallbackReceipts.opts.ObserveEntry = func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent) error {
						return nil
					}
				}
			}
			server, err := NewServerOf(options)
			if err != nil {
				t.Fatal(err)
			}
			response, err := server.fedService.JoinFederation(context.Background(), &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: "f", FederateName: "a"})
			if err != nil {
				t.Fatal(err)
			}
			if response.GetCallbackInvocationEntrySupported() != test.want {
				t.Fatalf("callback entry advertised=%v, want %v", response.GetCallbackInvocationEntrySupported(), test.want)
			}
		})
	}
}

func TestJoinCallbackEntryCapabilityFloat64(t *testing.T) {
	checkJoinCallbackEntryCapability(t, core.LogicalTime(1))
}

func TestJoinCallbackEntryCapabilityInteger64(t *testing.T) {
	checkJoinCallbackEntryCapability(t, int64(1))
}

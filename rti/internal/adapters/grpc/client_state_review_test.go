package grpc

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

func TestClientStateReviewMOMDoesNotExposePrivateRuntimeCheckpoint(t *testing.T) {
	privateState := []byte("private-sdk-object-knowledge-and-region-bounds")
	assertPrivate := func(t *testing.T, observation ServiceObservation) {
		t.Helper()
		for _, argument := range observation.SuppliedArguments {
			if strings.Contains(argument, "client_state") || strings.Contains(argument, base64.StdEncoding.EncodeToString(privateState)) {
				t.Errorf("private SDK checkpoint leaked through MOM %s: %s", observation.Service, argument)
			}
		}
	}

	t.Run("save completion service report", func(t *testing.T) {
		var observations []ServiceObservation
		observer := NewMOMObserver(MOMObserverOptions{Observe: func(_ context.Context, observation ServiceObservation) error {
			observations = append(observations, observation)
			return nil
		}})
		observer.Observe(context.Background(), rtiv1.SavepointService_FederateSaveComplete_FullMethodName,
			&rtiv1.FederateSaveResponseRequest{FederationName: "fed", FederateHandle: 1, ClientState: privateState}, &rtiv1.Empty{}, nil)
		if len(observations) != 1 {
			t.Fatalf("got %d service observations, want 1", len(observations))
		}
		assertPrivate(t, observations[0])
		if len(observations[0].SuppliedArguments) != 0 {
			t.Errorf("save completion has no public arguments: %v", observations[0].SuppliedArguments)
		}
	})

	t.Run("restore initiation callback report", func(t *testing.T) {
		observation, ok := CallbackObservation("fed", 1, &rtiv1.FederateEvent{
			Event: &rtiv1.FederateEvent_RestoreInitiate{RestoreInitiate: &rtiv1.InitiateFederateRestore{
				Label: "saved", FederateHandle: 1, FederateName: "one", ClientState: privateState,
			}},
		}, true, "")
		if !ok {
			t.Fatal("restore initiation was not observed")
		}
		assertPrivate(t, observation)
		if got := strings.Join(observation.SuppliedArguments, ";"); got != `label="saved";federate_name="one"` {
			t.Errorf("restore arguments lost or changed: %s", got)
		}
	})
}

func TestClientStateReviewMOMRedactsNestedRuntimeCheckpoint(t *testing.T) {
	input := []byte(`{"callback":{"label":"saved","client_state":"private","ticket":"private-capability"},"items":[{"client_state":"private","activation_ticket":"private-capability","federate_handle":7}]}`)
	got := string(momRedactJSON(input))
	if strings.Contains(got, "client_state") || strings.Contains(got, "ticket") || strings.Contains(got, "private") {
		t.Fatalf("nested SDK checkpoint leaked through MOM: %s", got)
	}
	if !strings.Contains(got, `"label":"saved"`) || !strings.Contains(got, `"federate_handle":7`) {
		t.Fatalf("non-private nested arguments were removed: %s", got)
	}
}

func TestClientStateReviewMOMDoesNotExposeTransportationCapabilities(t *testing.T) {
	ticket := []byte("private-transportation-capability")
	t.Run("activation request", func(t *testing.T) {
		args := momArguments(&rtiv1.ActivateTransportationTypeChangeRequest{
			WireVersion: 1, FederationName: "fed", FederateHandle: 1,
			Ticket: ticket, ExpectedFederationGeneration: 3, ExpectedCallbackEpoch: 4,
		})
		if len(args) != 0 {
			t.Fatalf("activation request contains only private protocol arguments: %v", args)
		}
	})
	t.Run("returned capability", func(t *testing.T) {
		args := strings.Join(momArguments(&rtiv1.TransportationChange{
			Ticket: ticket, FederationGeneration: 3, CallbackEpoch: 4, ObjectHandle: 7, AttributeHandles: []uint64{8},
		}), ";")
		if strings.Contains(args, "ticket") || strings.Contains(args, base64.StdEncoding.EncodeToString(ticket)) {
			t.Fatalf("transportation capability leaked through MOM return arguments: %s", args)
		}
		if !strings.Contains(args, `object_handle="7"`) || !strings.Contains(args, `attribute_handles=["8"]`) {
			t.Fatalf("public transportation result removed: %s", args)
		}
	})
}

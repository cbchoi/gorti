package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

func TestCompactObjectReadyRunsRoleActionBeforeValidation(t *testing.T) {
	tests := []struct {
		name string
		role role
		want []string
	}{
		{name: "publisher", role: rolePublisher, want: []string{"register", "validate"}},
		{name: "subscriber", role: roleSubscriber, want: []string{"discover", "validate"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := compactObjectReadyConfig(test.role)
			var calls []string
			err := executeCompactObjectReady(context.Background(), cfg, compactObjectReadyActions{
				registerPublisher: func(context.Context) error {
					calls = append(calls, "register")
					return nil
				},
				awaitSubscriberDiscovery: func(context.Context) error {
					calls = append(calls, "discover")
					return nil
				},
				validate: func() error {
					calls = append(calls, "validate")
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, test.want) {
				t.Fatalf("calls = %v, want %v", calls, test.want)
			}
		})
	}
}

func TestLegacyModesSkipCompactObjectReady(t *testing.T) {
	legacy := []config{
		{Role: rolePublisher, ReceiveOrder: true},
		{Role: rolePublisher, ReceiveOrder: true, WorkloadPlan: "workload.dvshla"},
		{Role: roleSubscriber},
	}
	for index, cfg := range legacy {
		called := false
		action := func(context.Context) error {
			called = true
			return errors.New("legacy mode invoked compact object-ready action")
		}
		err := executeCompactObjectReady(context.Background(), cfg, compactObjectReadyActions{
			registerPublisher:        action,
			awaitSubscriberDiscovery: action,
			validate: func() error {
				called = true
				return errors.New("legacy mode invoked compact validation")
			},
		})
		if err != nil || called {
			t.Fatalf("legacy case %d: called=%t err=%v", index, called, err)
		}
	}
}

func TestCompactWarmupWaitsForReadyFederationSynchronization(t *testing.T) {
	cfg := compactObjectReadyConfig(rolePublisher)
	cfg.OperationWarmup = 1
	readyEntered := make(chan struct{})
	releaseReady := make(chan struct{})
	warmupStarted := make(chan struct{})
	result := make(chan error, 1)
	prepared := false

	go func() {
		result <- executeReadyPhase(context.Background(), cfg, readyPhaseActions{
			prepareCompactObject: func(context.Context) error {
				prepared = true
				return nil
			},
			synchronize: func(_ context.Context, label string, registrar bool) error {
				switch label {
				case readySync:
					if !prepared {
						return errors.New("VERIFY_READY started before compact object-ready preparation")
					}
					if registrar {
						return errors.New("publisher registered VERIFY_READY")
					}
					close(readyEntered)
					<-releaseReady
				case measureSync:
					if !registrar {
						return errors.New("publisher did not register VERIFY_MEASURE")
					}
				default:
					return errors.New("unexpected synchronization label " + label)
				}
				return nil
			},
			registerPublisherObject: func(context.Context) error {
				return errors.New("compact publisher registered the object after VERIFY_READY")
			},
			publishWarmup: func(context.Context) error {
				close(warmupStarted)
				return nil
			},
			waitForWarmup: func(context.Context) error {
				return errors.New("publisher waited for subscriber warmup callbacks")
			},
		})
	}()

	select {
	case <-readyEntered:
	case <-time.After(time.Second):
		t.Fatal("ready synchronization was not entered")
	}
	select {
	case <-warmupStarted:
		t.Fatal("warmup started before VERIFY_READY federation synchronization completed")
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseReady)
	select {
	case <-warmupStarted:
	case <-time.After(time.Second):
		t.Fatal("warmup did not start after VERIFY_READY federation synchronization completed")
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestLegacyWarmupKeepsRegistrationAfterReady(t *testing.T) {
	cfg := config{
		Role:            rolePublisher,
		ReceiveOrder:    true,
		OperationWarmup: 1,
	}
	var calls []string
	err := executeReadyPhase(context.Background(), cfg, readyPhaseActions{
		prepareCompactObject: func(context.Context) error {
			calls = append(calls, "prepare-noop")
			return nil
		},
		synchronize: func(_ context.Context, label string, _ bool) error {
			calls = append(calls, "sync:"+label)
			return nil
		},
		registerPublisherObject: func(context.Context) error {
			calls = append(calls, "register")
			return nil
		},
		publishWarmup: func(context.Context) error {
			calls = append(calls, "warmup")
			return nil
		},
		waitForWarmup: func(context.Context) error {
			return errors.New("publisher waited for subscriber warmup callbacks")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"prepare-noop", "sync:" + readySync, "register", "warmup", "sync:" + measureSync}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestSubscriberObjectReadyWaitsForValidatedDiscovery(t *testing.T) {
	cfg := compactObjectReadyConfig(roleSubscriber)
	cfg.Count = 1
	cfg.Seed = "1516"
	cfg.Timeout = time.Second
	state := newEventState(cfg)
	p := participant{cfg: cfg, state: state}
	result := make(chan error, 1)
	go func() {
		result <- p.awaitPublisherObjectDiscovery(context.Background())
	}()

	select {
	case err := <-result:
		t.Fatalf("discovery wait returned before a callback: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	state.accept(federate.DiscoverObjectInstance{
		ObjectHandle: 17,
		ClassName:    objectClass,
		ObjectName:   objectName,
	}, counterNow())
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("validated discovery did not release the object-ready wait")
	}
	if err := p.validateCompactReceiveOrderReady(); err != nil {
		t.Fatal(err)
	}
}

func TestSubscriberObjectReadyRejectsInvalidDiscovery(t *testing.T) {
	cfg := compactObjectReadyConfig(roleSubscriber)
	cfg.Count = 1
	cfg.Seed = "1516"
	cfg.Timeout = time.Second
	state := newEventState(cfg)
	state.accept(federate.DiscoverObjectInstance{
		ObjectHandle: 17,
		ClassName:    "UnexpectedClass",
		ObjectName:   objectName,
	}, counterNow())
	p := participant{cfg: cfg, state: state}
	err := p.awaitPublisherObjectDiscovery(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unexpected object discovery") {
		t.Fatalf("invalid discovery error = %v", err)
	}
}

func compactObjectReadyConfig(participantRole role) config {
	return config{
		Role:           participantRole,
		ReceiveOrder:   true,
		WorkloadPlan:   "workload.dvshla",
		CompactSummary: true,
	}
}

package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

const startupLabel = "go-timed-ready"

type startupFederate interface {
	Handle() uint64
	ListFederationMembers(context.Context) ([]federate.FederationMember, error)
	RegisterFederationSynchronizationPoint(context.Context, string, []byte, []uint64) error
	SynchronizationPointAchieved(context.Context, string, bool) error
	Events() <-chan federate.Event
	DispatchEvent(context.Context, federate.Event, func(federate.Event) error) (error, error)
}

func waitForStartup(ctx context.Context, fed startupFederate, participants int) error {
	if participants < 1 {
		return errors.New("participants must be positive")
	}
	if participants == 1 {
		return nil
	}
	handles, err := waitForMembers(ctx, fed, participants)
	if err != nil {
		return err
	}
	leader := handles[0]
	for _, handle := range handles[1:] {
		if handle < leader {
			leader = handle
		}
	}
	if fed.Handle() == leader {
		if err := fed.RegisterFederationSynchronizationPoint(ctx, startupLabel, nil, handles); err != nil {
			return err
		}
	}
	// No participant advances until every member has enabled time management
	// and acknowledged the same synchronization point through its callback.
	for {
		select {
		case event, ok := <-fed.Events():
			if !ok {
				return errors.New("events closed before startup synchronization")
			}
			complete := false
			callbackErr, reportErr := fed.DispatchEvent(ctx, event, func(event federate.Event) error {
				var callbackErr error
				complete, callbackErr = handleStartupEvent(ctx, fed, event)
				return callbackErr
			})
			if err := errors.Join(callbackErr, reportErr); err != nil {
				return err
			}
			if complete {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func handleStartupEvent(ctx context.Context, fed startupFederate, event federate.Event) (bool, error) {
	switch value := event.(type) {
	case federate.SynchronizationPointAnnounced:
		if value.Label == startupLabel {
			return false, fed.SynchronizationPointAchieved(ctx, startupLabel, true)
		}
	case federate.FederationSynchronized:
		if value.Label == startupLabel {
			if len(value.FailedToSync) != 0 {
				return false, fmt.Errorf("startup participants failed: %v", value.FailedToSync)
			}
			return true, nil
		}
	case federate.SynchronizationPointRegistrationFailed:
		if value.Label == startupLabel {
			return false, fmt.Errorf("startup registration failed: %d", value.Reason)
		}
	case federate.FederationHalted:
		return false, fmt.Errorf("federation halted: %s", value.Reason)
	}
	return false, nil
}

func waitForMembers(ctx context.Context, fed startupFederate, participants int) ([]uint64, error) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		members, err := fed.ListFederationMembers(ctx)
		if err != nil {
			return nil, err
		}
		if len(members) > participants {
			return nil, fmt.Errorf("expected %d participants, found %d", participants, len(members))
		}
		if len(members) == participants {
			handles := make([]uint64, len(members))
			for i, member := range members {
				handles[i] = member.Handle
			}
			return handles, nil
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

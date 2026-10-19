package federate

import (
	"context"
	"fmt"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc/connectivity"
)

type SaveState int32

const (
	SaveStateUnspecified SaveState = iota
	SaveStateIdle
	SaveStateInitiated
	SaveStateSaved
	SaveStateNotSaved
)

type RestoreState int32

const (
	RestoreStateUnspecified RestoreState = iota
	RestoreStateIdle
	RestoreStateLoading
	RestoreStateInitiated
	RestoreStateCompleted
	RestoreStateFailed
)

type SaveStatus int32

const (
	SaveStatusUnspecified SaveStatus = iota
	NoSaveInProgress
	FederateInstructedToSave
	FederateSaving
	FederateWaitingForFederationToSave
)

type RestoreStatus int32

const (
	RestoreStatusUnspecified RestoreStatus = iota
	NoRestoreInProgress
	FederateRestoreRequestPending
	FederateWaitingForRestoreToBegin
	FederatePreparedToRestore
	FederateRestoring
	FederateWaitingForFederationToRestore
)

// FederateSaveStatus retains the published SDK type name.
//
//revive:disable-next-line:exported // Renaming this public type would break API compatibility.
type FederateSaveStatus struct {
	FederateHandle uint64
	Status         SaveStatus
}

// FederateRestoreStatus retains the published SDK type name.
//
//revive:disable-next-line:exported // Renaming this public type would break API compatibility.
type FederateRestoreStatus struct {
	PreRestoreHandle  uint64
	PostRestoreHandle uint64
	Status            RestoreStatus
}
type SaveStatusResponse struct {
	ActiveLabel      string
	FederateStatuses []FederateSaveStatus
}
type RestoreStatusResponse struct {
	ActiveLabel      string
	FederateStatuses []FederateRestoreStatus
}

// RequestFederationSave requests an immediate save when saveTime is nil.
// Scheduled saves are forwarded, but are currently unsupported by the server.
func (f *Federate) RequestFederationSave(ctx context.Context, label string, saveTime *float64) error {
	copied, exact, encodeErr := f.optionalTimeWire(saveTime)
	if encodeErr != nil {
		return encodeErr
	}
	_, err := f.conn.savepoint.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: f.federationName,
		FederateHandle: f.CurrentHandle(), Label: label, SaveTime: copied, SaveTimeValue: exact,
	})
	return wrapStatusErr(err)
}

func (f *Federate) RequestFederationRestore(ctx context.Context, label string) error {
	f.resetRuntimeRestoreReadiness(true)
	_, err := f.conn.savepoint.RequestFederationRestore(ctx, &rtiv1.RequestFederationRestoreRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: f.federationName,
		FederateHandle: f.CurrentHandle(), Label: label,
	})
	return wrapStatusErr(err)
}

// FederateSaveBegun explicitly reports that this federate has begun saving.
func (f *Federate) FederateSaveBegun(ctx context.Context) error {
	_, err := f.conn.savepoint.FederateSaveBegun(ctx, &rtiv1.FederateSaveResponseRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: f.federationName,
		FederateHandle: f.CurrentHandle(),
	})
	return wrapStatusErr(err)
}

// FederateSaveComplete reports success; it never implicitly calls FederateSaveBegun.
func (f *Federate) FederateSaveComplete(ctx context.Context) error {
	state, err := f.snapshotRuntime()
	if err != nil {
		return err
	}
	_, err = f.conn.savepoint.FederateSaveComplete(ctx, &rtiv1.FederateSaveResponseRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: f.federationName,
		FederateHandle: f.CurrentHandle(), ClientState: state,
	})
	return wrapStatusErr(err)
}

// FederateSaveNotComplete reports participant failure after an explicit begun call.
func (f *Federate) FederateSaveNotComplete(ctx context.Context) error {
	_, err := f.conn.savepoint.FederateSaveNotComplete(ctx, &rtiv1.FederateSaveResponseRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: f.federationName,
		FederateHandle: f.CurrentHandle(),
	})
	return wrapStatusErr(err)
}

// FederateRestoreComplete reports this participant's successful restore.
func (f *Federate) FederateRestoreComplete(ctx context.Context) error {
	if f.conn.cc != nil && f.conn.cc.GetState() == connectivity.Shutdown {
		f.resetRuntimeRestoreReadiness(false)
	}
	f.checkpointMu.Lock()
	ready, stagingErr := f.runtimeRestoreReady, f.runtimeRestoreError
	f.checkpointMu.Unlock()
	if stagingErr != nil {
		return fmt.Errorf("federate: cannot report restore success: %w", stagingErr)
	}
	if !ready {
		return fmt.Errorf("federate: restore runtime has not been validated")
	}
	_, err := f.conn.savepoint.FederateRestoreComplete(ctx, &rtiv1.FederateRestoreResponseRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: f.federationName,
		FederateHandle: f.CurrentHandle(),
	})
	return wrapStatusErr(err)
}

// FederateRestoreNotComplete reports participant failure, not a federation abort.
func (f *Federate) FederateRestoreNotComplete(ctx context.Context) error {
	f.resetRuntimeRestoreReadiness(true)
	_, err := f.conn.savepoint.FederateRestoreNotComplete(ctx, &rtiv1.FederateRestoreResponseRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: f.federationName,
		FederateHandle: f.CurrentHandle(),
	})
	return wrapStatusErr(err)
}

func (f *Federate) AbortFederationSave(ctx context.Context) error {
	handle := f.CurrentHandle()
	_, err := f.conn.savepoint.AbortFederationSave(ctx, &rtiv1.AbortFederationSaveRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: f.federationName,
		FederateHandle: &handle,
	})
	return wrapStatusErr(err)
}

// QuerySaveState returns the legacy label-specific outcome.
func (f *Federate) QuerySaveState(ctx context.Context, label string) (SaveState, error) {
	response, err := f.conn.savepoint.QuerySaveState(ctx, &rtiv1.QuerySaveStateRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: f.federationName, Label: label,
	})
	if err != nil {
		return SaveStateUnspecified, wrapStatusErr(err)
	}
	return SaveState(response.GetState()), nil
}

// QueryFederationSaveStatus returns the current per-federate vector.
func (f *Federate) QueryFederationSaveStatus(ctx context.Context) (SaveStatusResponse, error) {
	response, err := f.conn.savepoint.QuerySaveState(ctx, &rtiv1.QuerySaveStateRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: f.federationName,
	})
	if err != nil {
		return SaveStatusResponse{}, wrapStatusErr(err)
	}
	result := SaveStatusResponse{ActiveLabel: response.GetActiveLabel(), FederateStatuses: make([]FederateSaveStatus, 0, len(response.GetFederateStatuses()))}
	for _, entry := range response.GetFederateStatuses() {
		result.FederateStatuses = append(result.FederateStatuses, FederateSaveStatus{
			FederateHandle: entry.GetFederateHandle(),
			Status:         SaveStatus(entry.GetStatus()),
		})
	}
	return result, nil
}

func (f *Federate) AbortFederationRestore(ctx context.Context) error {
	f.resetRuntimeRestoreReadiness(false)
	handle := f.CurrentHandle()
	_, err := f.conn.savepoint.AbortFederationRestore(ctx, &rtiv1.AbortFederationRestoreRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: f.federationName,
		FederateHandle: &handle,
	})
	return wrapStatusErr(err)
}

// QueryRestoreState returns the legacy label-specific outcome.
func (f *Federate) QueryRestoreState(ctx context.Context, label string) (RestoreState, error) {
	response, err := f.conn.savepoint.QueryRestoreState(ctx, &rtiv1.QueryRestoreStateRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: f.federationName, Label: label,
	})
	if err != nil {
		return RestoreStateUnspecified, wrapStatusErr(err)
	}
	return RestoreState(response.GetState()), nil
}

// QueryFederationRestoreStatus returns the current per-federate vector.
func (f *Federate) QueryFederationRestoreStatus(ctx context.Context) (RestoreStatusResponse, error) {
	response, err := f.conn.savepoint.QueryRestoreState(ctx, &rtiv1.QueryRestoreStateRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: f.federationName,
	})
	if err != nil {
		return RestoreStatusResponse{}, wrapStatusErr(err)
	}
	result := RestoreStatusResponse{ActiveLabel: response.GetActiveLabel(), FederateStatuses: make([]FederateRestoreStatus, 0, len(response.GetFederateStatuses()))}
	for _, entry := range response.GetFederateStatuses() {
		result.FederateStatuses = append(result.FederateStatuses, FederateRestoreStatus{
			PreRestoreHandle: entry.GetPreRestoreHandle(), PostRestoreHandle: entry.GetPostRestoreHandle(),
			Status: RestoreStatus(entry.GetStatus()),
		})
	}
	return result, nil
}

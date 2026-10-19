package org.gorti.rti;

import hla.rti1516e.*;
import hla.rti1516e.exceptions.RTIexception;
import hla.rti1516e.exceptions.RTIinternalError;
import org.gorti.rti.types.Handles;
import org.gorti.rti.types.TimeValues;
import rti.v1.Savepoint;
import rti.v1.Stream;

/** Stateless standard callbacks, invoked only after the facade's lifecycle hooks. */
final class SaveRestoreCallbacks {
    private SaveRestoreCallbacks() { }

    static boolean deliver(Stream.FederateEvent event, FederateAmbassador target) throws RTIexception {
        return deliver(event, target, TimeWire.FLOAT);
    }
    static boolean deliver(Stream.FederateEvent event, FederateAmbassador target, String selected) throws RTIexception {
        switch (event.getEventCase()) {
            case SAVE_INITIATE: {
                Stream.InitiateFederateSave value = event.getSaveInitiate();
                if (!TimeWire.hasTime(value, "save_time")) target.initiateFederateSave(value.getLabel());
                else {
                    LogicalTime time;
                    try { time = TimeWire.read(value, "save_time", selected); }
                    catch (IllegalArgumentException failure) { throw new RTIinternalError("Invalid save callback logical time", failure); }
                    target.initiateFederateSave(value.getLabel(), time);
                }
                return true;
            }
            case SAVE_COMPLETED:
                target.federationSaved(); return true;
            case SAVE_FAILED:
                if (!event.getSaveFailed().hasReason()) throw new RTIinternalError("FederationNotSaved has no wire failure reason");
                target.federationNotSaved(saveFailure(event.getSaveFailed().getReason())); return true;
            case RESTORE_REQUEST_SUCCEEDED:
                target.requestFederationRestoreSucceeded(event.getRestoreRequestSucceeded().getLabel()); return true;
            case RESTORE_REQUEST_FAILED:
                target.requestFederationRestoreFailed(event.getRestoreRequestFailed().getLabel()); return true;
            case RESTORE_BEGUN:
                target.federationRestoreBegun(); return true;
            case RESTORE_INITIATE: {
                Stream.InitiateFederateRestore value = event.getRestoreInitiate();
                target.initiateFederateRestore(value.getLabel(), value.getFederateName(), federate(value.getFederateHandle()));
                return true;
            }
            case RESTORE_COMPLETED:
                target.federationRestored(); return true;
            case RESTORE_FAILED:
                if (!event.getRestoreFailed().hasReason()) throw new RTIinternalError("FederationNotRestored has no wire failure reason");
                target.federationNotRestored(restoreFailure(event.getRestoreFailed().getReason())); return true;
            default: return false;
        }
    }

    static FederateHandleSaveStatusPair[] saveStatuses(Savepoint.QuerySaveStateResponse response) throws RTIinternalError {
        FederateHandleSaveStatusPair[] result = new FederateHandleSaveStatusPair[response.getFederateStatusesCount()];
        for (int i = 0; i < result.length; i++) {
            Savepoint.FederateSaveStatus entry = response.getFederateStatuses(i);
            result[i] = new FederateHandleSaveStatusPair(federate(entry.getFederateHandle()), saveStatus(entry.getStatus()));
        }
        return result;
    }

    static FederateRestoreStatus[] restoreStatuses(Savepoint.QueryRestoreStateResponse response) throws RTIinternalError {
        FederateRestoreStatus[] result = new FederateRestoreStatus[response.getFederateStatusesCount()];
        for (int i = 0; i < result.length; i++) {
            Savepoint.FederateRestoreStatus entry = response.getFederateStatuses(i);
            result[i] = new FederateRestoreStatus(federate(entry.getPreRestoreHandle()),
                    entry.getPostRestoreHandle() == 0 ? null : federate(entry.getPostRestoreHandle()), restoreStatus(entry.getStatus()));
        }
        return result;
    }

    private static SaveStatus saveStatus(Savepoint.FederateSaveProgress value) throws RTIinternalError {
        switch (value) {
            case FEDERATE_SAVE_PROGRESS_NO_SAVE_IN_PROGRESS: return SaveStatus.NO_SAVE_IN_PROGRESS;
            case FEDERATE_SAVE_PROGRESS_INSTRUCTED_TO_SAVE: return SaveStatus.FEDERATE_INSTRUCTED_TO_SAVE;
            case FEDERATE_SAVE_PROGRESS_SAVING: return SaveStatus.FEDERATE_SAVING;
            case FEDERATE_SAVE_PROGRESS_WAITING_FOR_FEDERATION_TO_SAVE: return SaveStatus.FEDERATE_WAITING_FOR_FEDERATION_TO_SAVE;
            default: throw new RTIinternalError("Unknown wire save progress: " + value);
        }
    }

    private static RestoreStatus restoreStatus(Savepoint.FederateRestoreProgress value) throws RTIinternalError {
        switch (value) {
            case FEDERATE_RESTORE_PROGRESS_NO_RESTORE_IN_PROGRESS: return RestoreStatus.NO_RESTORE_IN_PROGRESS;
            case FEDERATE_RESTORE_PROGRESS_RESTORE_REQUEST_PENDING: return RestoreStatus.FEDERATE_RESTORE_REQUEST_PENDING;
            case FEDERATE_RESTORE_PROGRESS_WAITING_FOR_RESTORE_TO_BEGIN: return RestoreStatus.FEDERATE_WAITING_FOR_RESTORE_TO_BEGIN;
            case FEDERATE_RESTORE_PROGRESS_PREPARED_TO_RESTORE: return RestoreStatus.FEDERATE_PREPARED_TO_RESTORE;
            case FEDERATE_RESTORE_PROGRESS_RESTORING: return RestoreStatus.FEDERATE_RESTORING;
            case FEDERATE_RESTORE_PROGRESS_WAITING_FOR_FEDERATION_TO_RESTORE: return RestoreStatus.FEDERATE_WAITING_FOR_FEDERATION_TO_RESTORE;
            default: throw new RTIinternalError("Unknown wire restore progress: " + value);
        }
    }

    private static FederateHandle federate(long value) throws RTIinternalError {
        if (value == 0) throw new RTIinternalError("Save/restore callback has an invalid zero federate handle");
        return Handles.federate(value);
    }

    private static SaveFailureReason saveFailure(int value) throws RTIinternalError {
        switch (value) {
            case 1: return SaveFailureReason.RTI_UNABLE_TO_SAVE;
            case 2: return SaveFailureReason.FEDERATE_REPORTED_FAILURE_DURING_SAVE;
            case 3: return SaveFailureReason.FEDERATE_RESIGNED_DURING_SAVE;
            case 4: return SaveFailureReason.RTI_DETECTED_FAILURE_DURING_SAVE;
            case 5: return SaveFailureReason.SAVE_TIME_CANNOT_BE_HONORED;
            case 6: return SaveFailureReason.SAVE_ABORTED;
            default: throw new RTIinternalError("Unknown wire save failure reason: " + value);
        }
    }

    private static RestoreFailureReason restoreFailure(int value) throws RTIinternalError {
        switch (value) {
            case 1: return RestoreFailureReason.RTI_UNABLE_TO_RESTORE;
            case 2: return RestoreFailureReason.FEDERATE_REPORTED_FAILURE_DURING_RESTORE;
            case 3: return RestoreFailureReason.FEDERATE_RESIGNED_DURING_RESTORE;
            case 4: return RestoreFailureReason.RTI_DETECTED_FAILURE_DURING_RESTORE;
            case 5: return RestoreFailureReason.RESTORE_ABORTED;
            default: throw new RTIinternalError("Unknown wire restore failure reason: " + value);
        }
    }
}

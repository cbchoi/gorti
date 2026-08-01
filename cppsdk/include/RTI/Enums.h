// IEEE 1516.1-2010 §4.2 / Annex A — RTI/Enums.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// CRITICAL — FR-DLC-16: every enum in this header is UNSCOPED (`enum X { ... }`,
// NOT `enum class X { ... }`). Federate code accesses enumerators bare:
//
//     amb->connect(fed, HLA_IMMEDIATE);           // OK
//     amb->connect(fed, CallbackModel::HLA_IMMEDIATE);  // works too, but spec
//                                                       // shape is the unscoped
//                                                       // access syntax.
//
// `enum class` would break source-compat with every reference_rti federate.

#ifndef RTI_Enums_h
#define RTI_Enums_h

#include <RTI/SpecificConfig.h>

namespace rti1516e {

// §4.2 connect — callback delivery model.
enum CallbackModel {
  HLA_IMMEDIATE,
  HLA_EVOKED
};

// §6 / §8 — message ordering classes.
enum OrderType {
  RECEIVE = 1,
  TIMESTAMP = 2
};

// §6 — transport reliability classes.
enum TransportationType {
  RELIABLE = 1,
  BEST_EFFORT = 2
};

// §4.10 resignFederationExecution — mandatory ResignAction argument.
enum ResignAction {
  UNCONDITIONALLY_DIVEST_ATTRIBUTES,
  DELETE_OBJECTS,
  CANCEL_PENDING_OWNERSHIP_ACQUISITIONS,
  DELETE_OBJECTS_THEN_DIVEST,
  CANCEL_THEN_DELETE_THEN_DIVEST,
  NO_ACTION
};

// §4.20 federationNotSaved reason.
enum SaveFailureReason {
  RTI_UNABLE_TO_SAVE,
  FEDERATE_REPORTED_FAILURE_DURING_SAVE,
  FEDERATE_RESIGNED_DURING_SAVE,
  RTI_DETECTED_FAILURE_DURING_SAVE,
  SAVE_TIME_CANNOT_BE_HONORED,
  SAVE_ABORTED
};

// §4.23 federationSaveStatusResponse — per-federate status.
enum SaveStatus {
  NO_SAVE_IN_PROGRESS,
  FEDERATE_INSTRUCTED_TO_SAVE,
  FEDERATE_SAVING,
  FEDERATE_WAITING_FOR_FEDERATION_TO_SAVE
};

// §4.29 federationNotRestored reason.
enum RestoreFailureReason {
  RTI_UNABLE_TO_RESTORE,
  FEDERATE_REPORTED_FAILURE_DURING_RESTORE,
  FEDERATE_RESIGNED_DURING_RESTORE,
  RTI_DETECTED_FAILURE_DURING_RESTORE,
  RESTORE_ABORTED
};

// §4.32 federationRestoreStatusResponse — per-federate status.
enum RestoreStatus {
  NO_RESTORE_IN_PROGRESS,
  FEDERATE_RESTORE_REQUEST_PENDING,
  FEDERATE_WAITING_FOR_RESTORE_TO_BEGIN,
  FEDERATE_PREPARED_TO_RESTORE,
  FEDERATE_RESTORING,
  FEDERATE_WAITING_FOR_FEDERATION_TO_RESTORE
};

// §10.32 normalizeServiceGroup — 7-value tag group.
enum ServiceGroup {
  FEDERATION_MANAGEMENT,
  DECLARATION_MANAGEMENT,
  OBJECT_MANAGEMENT,
  OWNERSHIP_MANAGEMENT,
  TIME_MANAGEMENT,
  DATA_DISTRIBUTION_MANAGEMENT,
  SUPPORT_SERVICES
};

// §4.12 synchronizationPointRegistrationFailed reason.
enum SynchronizationPointFailureReason {
  SYNCHRONIZATION_POINT_LABEL_NOT_UNIQUE,
  SYNCHRONIZATION_SET_MEMBER_NOT_JOINED
};

}  // namespace rti1516e

#endif  // RTI_Enums_h

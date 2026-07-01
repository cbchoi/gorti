// Exception hierarchy for the rti1516e C++ SDK.
//
// Mirrors IEEE 1516.1-2010 Annex C exception list. Every RTIambassador
// call may throw one of these. The base class is `RTIinternalError`
// per the spec convention (every spec exception derives from it for
// catch-all-and-log workflows). Each subclass carries a human-readable
// message; structured fields (which class, which attribute, etc.) are
// reserved for a future cut.
//
// M34 Agent AA — every M17 exception class lives inside `rti1516e::m17`
// so its mangled ctor/vtable symbols are DISTINCT from the identically
// named DLC spec classes in <RTI/Exception.h> (both would otherwise be
// `rti1516e::RTIinternalError` etc., colliding at link time and causing
// silent memory corruption when both librti1516e.a and librti1516e_dlc.a
// end up in the same binary — the DLC ctor takes std::wstring, the M17
// ctor inherits from std::runtime_error which takes std::string, so
// picking the wrong ctor via linker gives an ABI mismatch, and the
// symptom in practice is a runaway allocation → std::bad_alloc). The
// `using` re-exports below keep every existing M17 consumer working;
// alias uses do NOT emit new mangled symbols, only the qualified
// `rti1516e::m17::X` names appear in object files.

#pragma once

#include <stdexcept>
#include <string>

namespace rti1516e {
namespace m17 {

class RTIinternalError : public std::runtime_error {
 public:
  using std::runtime_error::runtime_error;
};

// §C.1 Federation / federate lifecycle.
class FederationExecutionAlreadyExists : public RTIinternalError {
  using RTIinternalError::RTIinternalError;
};
class FederationExecutionDoesNotExist : public RTIinternalError {
  using RTIinternalError::RTIinternalError;
};
class FederateAlreadyExecutionMember : public RTIinternalError {
  using RTIinternalError::RTIinternalError;
};
class FederateNotExecutionMember : public RTIinternalError {
  using RTIinternalError::RTIinternalError;
};

// §C.2 Connection.
class ConnectionFailed : public RTIinternalError {
  using RTIinternalError::RTIinternalError;
};
class NotConnected : public RTIinternalError {
  using RTIinternalError::RTIinternalError;
};
class AlreadyConnected : public RTIinternalError {
  using RTIinternalError::RTIinternalError;
};

// §C.3 Handle / name resolution.
class NameNotFound : public RTIinternalError {
  using RTIinternalError::RTIinternalError;
};
class InvalidObjectClassHandle : public RTIinternalError {
  using RTIinternalError::RTIinternalError;
};
class InvalidAttributeHandle : public RTIinternalError {
  using RTIinternalError::RTIinternalError;
};
class InvalidInteractionClassHandle : public RTIinternalError {
  using RTIinternalError::RTIinternalError;
};
class InvalidParameterHandle : public RTIinternalError {
  using RTIinternalError::RTIinternalError;
};

// §C.4 Object / interaction surface.
class ObjectClassNotPublished : public RTIinternalError {
  using RTIinternalError::RTIinternalError;
};
class ObjectInstanceNotKnown : public RTIinternalError {
  using RTIinternalError::RTIinternalError;
};
class InteractionClassNotPublished : public RTIinternalError {
  using RTIinternalError::RTIinternalError;
};

}  // namespace m17

// M34 Agent AA — Pitch-parity compat aliases. Every existing M17 user
// (tests, examples, the M17 impl in src/RtiAmbassador.cpp) still writes
// `rti1516e::NotConnected`, `throw AlreadyConnected(...)` etc.; those
// name lookups resolve through these aliases to `rti1516e::m17::*`, and
// the mangled symbols emitted at throw/catch sites are the `m17::*`
// ones — no collision with the DLC exception classes of the same short
// name in <RTI/Exception.h>. DLC TUs never include this header, so the
// DLC translation units keep seeing `rti1516e::NotConnected` = the DLC
// spec class.
using RTIinternalError = m17::RTIinternalError;
using FederationExecutionAlreadyExists = m17::FederationExecutionAlreadyExists;
using FederationExecutionDoesNotExist = m17::FederationExecutionDoesNotExist;
using FederateAlreadyExecutionMember = m17::FederateAlreadyExecutionMember;
using FederateNotExecutionMember = m17::FederateNotExecutionMember;
using ConnectionFailed = m17::ConnectionFailed;
using NotConnected = m17::NotConnected;
using AlreadyConnected = m17::AlreadyConnected;
using NameNotFound = m17::NameNotFound;
using InvalidObjectClassHandle = m17::InvalidObjectClassHandle;
using InvalidAttributeHandle = m17::InvalidAttributeHandle;
using InvalidInteractionClassHandle = m17::InvalidInteractionClassHandle;
using InvalidParameterHandle = m17::InvalidParameterHandle;
using ObjectClassNotPublished = m17::ObjectClassNotPublished;
using ObjectInstanceNotKnown = m17::ObjectInstanceNotKnown;
using InteractionClassNotPublished = m17::InteractionClassNotPublished;

}  // namespace rti1516e

// Exception hierarchy for the rti1516e C++ SDK.
//
// Mirrors IEEE 1516.1-2010 Annex C exception list. Every RTIambassador
// call may throw one of these. The base class is `RTIinternalError`
// per the spec convention (every spec exception derives from it for
// catch-all-and-log workflows). Each subclass carries a human-readable
// message; structured fields (which class, which attribute, etc.) are
// reserved for a future cut.

#pragma once

#include <stdexcept>
#include <string>

namespace rti1516e {

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

}  // namespace rti1516e

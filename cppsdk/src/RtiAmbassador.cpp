// rti1516e::RTIambassador impl — M17.1 + M17.2.
//
// The pimpl owns:
//   - the gRPC channel to rtid (lazy by default)
//   - service stubs (federation, etc.) instantiated on first use
//   - federate-side bookkeeping (federation name, federate handle,
//     handle caches) — fields added in later M17 milestones
//
// The header keeps this hidden so federate users never transitively
// pick up grpc++ / protobuf headers from `#include <rti1516e/RtiAmbassador.h>`.

#include "rti1516e/RtiAmbassador.h"

#include <grpcpp/grpcpp.h>

#include <fstream>
#include <memory>
#include <sstream>
#include <string>
#include <string_view>

#include "rti/v1/common.pb.h"
#include "rti/v1/federation.grpc.pb.h"
#include "rti/v1/federation.pb.h"
#include "rti1516e/Exceptions.h"

namespace rti1516e {

namespace {

// Parse "grpc://host:port" or "grpcs://host:port" into the gRPC
// target form ("host:port"). Returns the unwrapped target on success,
// or throws ConnectionFailed on a malformed URL.
//
// TLS variant (`grpcs://`) is accepted but currently builds a
// plaintext channel — M14 mTLS wiring lands in a Cut-2 milestone.
std::string parseGrpcUrl(const std::string& url) {
  constexpr std::string_view kPlain = "grpc://";
  constexpr std::string_view kTls = "grpcs://";
  std::string_view sv{url};
  if (sv.substr(0, kPlain.size()) == kPlain) {
    return std::string(sv.substr(kPlain.size()));
  }
  if (sv.substr(0, kTls.size()) == kTls) {
    return std::string(sv.substr(kTls.size()));
  }
  throw ConnectionFailed(
      "URL must start with 'grpc://' or 'grpcs://' (got: " + url + ")");
}

// Read a FOM module file into bytes. Throws RTIinternalError on I/O
// error so a federate gets a clear failure when the FOM path is
// wrong (rather than a confusing wire error from rtid).
std::string readFomBytes(const std::string& path) {
  std::ifstream f(path, std::ios::binary);
  if (!f) {
    throw RTIinternalError("createFederationExecution: cannot read FOM module '" +
                           path + "'");
  }
  std::ostringstream out;
  out << f.rdbuf();
  return out.str();
}

// Translate a gRPC status into the matching IEEE 1516.1 Annex C
// exception. The detail string is the server-side error sentinel
// (rti/internal/core/errors.go); substring-match keeps the table
// compact at the cost of being slightly approximate.
//
// Unmatched codes fall through to RTIinternalError carrying the
// original gRPC code + message.
[[noreturn]] void throwFromStatus(const grpc::Status& s,
                                  std::string_view operation) {
  const auto detail = s.error_message();
  const auto code = s.error_code();
  std::string msg = std::string(operation) + ": " + detail;

  // Detail-string sniffing for the per-error-class dispatch. Mirrors
  // pysdk's _grpc_errors.translate_rpc_error.
  if (detail.find("federation already exists") != std::string::npos) {
    throw FederationExecutionAlreadyExists(msg);
  }
  if (detail.find("federation not found") != std::string::npos) {
    throw FederationExecutionDoesNotExist(msg);
  }
  if (detail.find("federate already joined") != std::string::npos) {
    throw FederateAlreadyExecutionMember(msg);
  }
  if (detail.find("federate not joined") != std::string::npos) {
    throw FederateNotExecutionMember(msg);
  }

  switch (code) {
    case grpc::StatusCode::ALREADY_EXISTS:
      throw FederationExecutionAlreadyExists(msg);
    case grpc::StatusCode::NOT_FOUND:
      throw FederationExecutionDoesNotExist(msg);
    case grpc::StatusCode::FAILED_PRECONDITION:
      // Could be a not-joined error or other state issue. The detail
      // string already shipped above for the joined variant; this
      // catches the general case.
      throw FederateNotExecutionMember(msg);
    case grpc::StatusCode::UNAVAILABLE:
      throw ConnectionFailed(msg);
    default:
      throw RTIinternalError(msg);
  }
}

}  // namespace

class RTIambassadorImpl {
 public:
  std::shared_ptr<grpc::Channel> channel;
  std::unique_ptr<rti::v1::FederationService::Stub> federation_stub;
  std::string url;
  bool connected = false;

  // Federate-bound state (set on joinFederationExecution, cleared on
  // resignFederationExecution). The Cut-1 ambassador supports one
  // active federate-membership at a time; a future cut may revisit
  // this if multi-federation host code emerges.
  std::string joined_federation;
  FederateHandle federate_handle{};
  bool joined = false;

  void requireConnected() const {
    if (!connected) {
      throw NotConnected(
          "RTIambassador: operation requires a prior connect()");
    }
  }
};

RTIambassador::RTIambassador() : impl_(std::make_unique<RTIambassadorImpl>()) {}

RTIambassador::~RTIambassador() = default;

RTIambassador::RTIambassador(RTIambassador&&) noexcept = default;
RTIambassador& RTIambassador::operator=(RTIambassador&&) noexcept = default;

void RTIambassador::connect(const std::string& url) {
  if (impl_->connected) {
    throw AlreadyConnected(
        "RTIambassador::connect: already connected to " + impl_->url);
  }
  const auto target = parseGrpcUrl(url);
  impl_->channel = grpc::CreateChannel(target, grpc::InsecureChannelCredentials());
  impl_->federation_stub = rti::v1::FederationService::NewStub(impl_->channel);
  impl_->url = url;
  impl_->connected = true;
}

void RTIambassador::disconnect() {
  impl_->federation_stub.reset();
  impl_->channel.reset();
  impl_->url.clear();
  impl_->connected = false;
  impl_->joined_federation.clear();
  impl_->federate_handle = FederateHandle{};
  impl_->joined = false;
}

bool RTIambassador::isConnected() const noexcept {
  return impl_->connected;
}

void RTIambassador::createFederationExecution(
    const std::string& federation_name,
    const FomModuleList& fom_modules) {
  impl_->requireConnected();

  rti::v1::CreateFederationRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(federation_name);
  for (const auto& path : fom_modules) {
    auto* m = req.add_fom_modules();
    m->set_path(path);
    m->set_xml(readFomBytes(path));
  }

  grpc::ClientContext ctx;
  rti::v1::CreateFederationResponse resp;
  const auto status = impl_->federation_stub->CreateFederation(&ctx, req, &resp);
  if (!status.ok()) {
    throwFromStatus(status, "createFederationExecution");
  }
}

void RTIambassador::destroyFederationExecution(
    const std::string& federation_name) {
  impl_->requireConnected();

  rti::v1::DestroyFederationRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(federation_name);

  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto status = impl_->federation_stub->DestroyFederation(&ctx, req, &resp);
  if (!status.ok()) {
    throwFromStatus(status, "destroyFederationExecution");
  }
}

FederateHandle RTIambassador::joinFederationExecution(
    const std::string& federate_name,
    const std::string& federation_name) {
  impl_->requireConnected();
  if (impl_->joined) {
    throw FederateAlreadyExecutionMember(
        "joinFederationExecution: already joined to " +
        impl_->joined_federation +
        " (resign before joining another federation)");
  }

  rti::v1::JoinFederationRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(federation_name);
  req.set_federate_name(federate_name);

  grpc::ClientContext ctx;
  rti::v1::JoinFederationResponse resp;
  const auto status = impl_->federation_stub->JoinFederation(&ctx, req, &resp);
  if (!status.ok()) {
    throwFromStatus(status, "joinFederationExecution");
  }

  impl_->federate_handle = FederateHandle(resp.federate_handle());
  impl_->joined_federation = federation_name;
  impl_->joined = true;
  return impl_->federate_handle;
}

void RTIambassador::resignFederationExecution() {
  impl_->requireConnected();
  if (!impl_->joined) {
    throw FederateNotExecutionMember(
        "resignFederationExecution: not currently joined to any federation");
  }

  rti::v1::ResignFederationRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  // Cut-1: default action mirrors Pitch's pre-M24 default. The wider
  // ResignAction surface is a Cut-2 task.
  req.set_action(rti::v1::RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES);

  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto status = impl_->federation_stub->ResignFederation(&ctx, req, &resp);
  if (!status.ok()) {
    throwFromStatus(status, "resignFederationExecution");
  }

  impl_->joined = false;
  impl_->joined_federation.clear();
  impl_->federate_handle = FederateHandle{};
}

}  // namespace rti1516e

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

#include <atomic>
#include <chrono>
#include <condition_variable>
#include <deque>
#include <fstream>
#include <memory>
#include <sstream>
#include <string>
#include <string_view>
#include <thread>

#include <mutex>
#include <unordered_map>
#include <utility>

#include "rti/v1/common.pb.h"
#include "rti/v1/declaration.grpc.pb.h"
#include "rti/v1/declaration.pb.h"
#include "rti/v1/federation.grpc.pb.h"
#include "rti/v1/object.grpc.pb.h"
#include "rti/v1/object.pb.h"
#include "rti/v1/stream.grpc.pb.h"
#include "rti/v1/stream.pb.h"
#include "rti1516e/FederateAmbassador.h"
#include "rti/v1/federation.pb.h"
#include "rti/v1/support.grpc.pb.h"
#include "rti/v1/support.pb.h"
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
      // M17.3 — SupportService returns NotFound for unknown class /
      // attribute / parameter names. The Annex C exception is
      // NameNotFound.
      if (operation.find("get") != std::string::npos &&
          (operation.find("Handle") != std::string::npos ||
           operation.find("Name") != std::string::npos)) {
        throw NameNotFound(msg);
      }
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
  std::unique_ptr<rti::v1::SupportService::Stub> support_stub;
  std::unique_ptr<rti::v1::DeclarationService::Stub> declaration_stub;
  std::unique_ptr<rti::v1::ObjectService::Stub> object_stub;
  std::unique_ptr<rti::v1::StreamService::Stub> stream_stub;

  // --- M17.6 callback state ---
  // Bound FederateAmbassador (nullable). tickCallback dispatches
  // queued events onto this. Set via setFederateAmbassador.
  FederateAmbassador* fed_ambassador = nullptr;

  // Event queue + the background thread that drains the streaming RPC.
  std::mutex event_mu;
  std::condition_variable event_cv;
  std::deque<rti::v1::FederateEvent> event_queue;
  std::atomic<bool> stream_running{false};
  std::thread stream_thread;
  std::unique_ptr<grpc::ClientContext> stream_ctx;

  void startEventStream();
  void stopEventStream();
  std::string url;
  bool connected = false;

  // Federate-bound state (set on joinFederationExecution, cleared on
  // resignFederationExecution). The Cut-1 ambassador supports one
  // active federate-membership at a time; a future cut may revisit
  // this if multi-federation host code emerges.
  std::string joined_federation;
  FederateHandle federate_handle{};
  bool joined = false;

  // M17.3 — handle / name caches. The cache key composes federation
  // name with the lookup target so resignFederationExecution +
  // rejoin to a different federation can't reuse stale handles.
  // Locking is coarse (one mutex for all caches); acceptable because
  // a federate typically resolves handles at init time, not in the
  // hot path.
  mutable std::mutex cache_mu;

  using ObjClassByName = std::unordered_map<std::string, HandleValue>;
  using ObjClassByHandle = std::unordered_map<HandleValue, std::string>;
  using IntClassByName = std::unordered_map<std::string, HandleValue>;
  using IntClassByHandle = std::unordered_map<HandleValue, std::string>;
  using AttrKey = std::pair<HandleValue, std::string>;  // (class, attr_name)
  using ParamKey = std::pair<HandleValue, std::string>;
  struct PairHash {
    size_t operator()(const std::pair<HandleValue, std::string>& p) const noexcept {
      return std::hash<HandleValue>{}(p.first) ^
             (std::hash<std::string>{}(p.second) << 1);
    }
  };
  using AttrByName = std::unordered_map<AttrKey, HandleValue, PairHash>;
  using AttrByHandle = std::unordered_map<AttrKey, std::string, PairHash>;
  using ParamByName = std::unordered_map<ParamKey, HandleValue, PairHash>;
  using ParamByHandle = std::unordered_map<ParamKey, std::string, PairHash>;

  ObjClassByName obj_class_by_name;
  ObjClassByHandle obj_class_by_handle;
  IntClassByName int_class_by_name;
  IntClassByHandle int_class_by_handle;
  AttrByName attr_by_name;
  AttrByHandle attr_by_handle;
  ParamByName param_by_name;
  ParamByHandle param_by_handle;

  void requireConnected() const {
    if (!connected) {
      throw NotConnected(
          "RTIambassador: operation requires a prior connect()");
    }
  }

  void clearHandleCaches() {
    std::lock_guard<std::mutex> g(cache_mu);
    obj_class_by_name.clear();
    obj_class_by_handle.clear();
    int_class_by_name.clear();
    int_class_by_handle.clear();
    attr_by_name.clear();
    attr_by_handle.clear();
    param_by_name.clear();
    param_by_handle.clear();
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
  impl_->support_stub = rti::v1::SupportService::NewStub(impl_->channel);
  impl_->declaration_stub = rti::v1::DeclarationService::NewStub(impl_->channel);
  impl_->object_stub = rti::v1::ObjectService::NewStub(impl_->channel);
  impl_->stream_stub = rti::v1::StreamService::NewStub(impl_->channel);
  impl_->url = url;
  impl_->connected = true;
}

void RTIambassador::disconnect() {
  impl_->stopEventStream();
  impl_->federation_stub.reset();
  impl_->support_stub.reset();
  impl_->declaration_stub.reset();
  impl_->object_stub.reset();
  impl_->stream_stub.reset();
  impl_->channel.reset();
  impl_->url.clear();
  impl_->connected = false;
  impl_->joined_federation.clear();
  impl_->federate_handle = FederateHandle{};
  impl_->joined = false;
  impl_->clearHandleCaches();
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
  // Start the background stream drain so callbacks queue up
  // immediately. tickCallback will pop them.
  impl_->startEventStream();
  return impl_->federate_handle;
}

void RTIambassador::resignFederationExecution() {
  impl_->requireConnected();
  if (!impl_->joined) {
    throw FederateNotExecutionMember(
        "resignFederationExecution: not currently joined to any federation");
  }
  // Stop draining the event stream before the wire resign. The
  // server closes the stream once the federate resigns; tearing
  // down our side first avoids a benign cancellation log line.
  impl_->stopEventStream();

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
  // Drop handle caches — a subsequent join may be to a different
  // federation with different FOM, so stale caches would lie.
  impl_->clearHandleCaches();
}

// --- M17.3 §10.2 handle services ------------------------------------------

ObjectClassHandle RTIambassador::getObjectClassHandle(
    const std::string& name) {
  impl_->requireConnected();
  {
    std::lock_guard<std::mutex> g(impl_->cache_mu);
    const auto it = impl_->obj_class_by_name.find(name);
    if (it != impl_->obj_class_by_name.end()) {
      return ObjectClassHandle(it->second);
    }
  }

  rti::v1::GetObjectClassHandleRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_class_name(name);
  grpc::ClientContext ctx;
  rti::v1::GetObjectClassHandleResponse resp;
  const auto status = impl_->support_stub->GetObjectClassHandle(&ctx, req, &resp);
  if (!status.ok()) {
    throwFromStatus(status, "getObjectClassHandle(" + name + ")");
  }
  const auto h = resp.class_handle();
  std::lock_guard<std::mutex> g(impl_->cache_mu);
  impl_->obj_class_by_name[name] = h;
  impl_->obj_class_by_handle[h] = name;
  return ObjectClassHandle(h);
}

std::string RTIambassador::getObjectClassName(ObjectClassHandle handle) {
  impl_->requireConnected();
  {
    std::lock_guard<std::mutex> g(impl_->cache_mu);
    const auto it = impl_->obj_class_by_handle.find(handle.raw());
    if (it != impl_->obj_class_by_handle.end()) {
      return it->second;
    }
  }
  rti::v1::GetObjectClassNameRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_class_handle(handle.raw());
  grpc::ClientContext ctx;
  rti::v1::GetObjectClassNameResponse resp;
  const auto status = impl_->support_stub->GetObjectClassName(&ctx, req, &resp);
  if (!status.ok()) {
    throwFromStatus(status, "getObjectClassName");
  }
  const auto& name = resp.class_name();
  std::lock_guard<std::mutex> g(impl_->cache_mu);
  impl_->obj_class_by_name[name] = handle.raw();
  impl_->obj_class_by_handle[handle.raw()] = name;
  return name;
}

AttributeHandle RTIambassador::getAttributeHandle(ObjectClassHandle cls,
                                                  const std::string& name) {
  impl_->requireConnected();
  const RTIambassadorImpl::AttrKey key{cls.raw(), name};
  {
    std::lock_guard<std::mutex> g(impl_->cache_mu);
    const auto it = impl_->attr_by_name.find(key);
    if (it != impl_->attr_by_name.end()) {
      return AttributeHandle(it->second);
    }
  }
  rti::v1::GetAttributeHandleRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_class_handle(cls.raw());
  req.set_attribute_name(name);
  grpc::ClientContext ctx;
  rti::v1::GetAttributeHandleResponse resp;
  const auto status = impl_->support_stub->GetAttributeHandle(&ctx, req, &resp);
  if (!status.ok()) {
    throwFromStatus(status, "getAttributeHandle(" + name + ")");
  }
  const auto h = resp.attribute_handle();
  std::lock_guard<std::mutex> g(impl_->cache_mu);
  impl_->attr_by_name[key] = h;
  impl_->attr_by_handle[{cls.raw(), std::to_string(h)}] = name;  // cheap reverse
  return AttributeHandle(h);
}

std::string RTIambassador::getAttributeName(ObjectClassHandle cls,
                                            AttributeHandle handle) {
  impl_->requireConnected();
  const RTIambassadorImpl::AttrKey rk{cls.raw(), std::to_string(handle.raw())};
  {
    std::lock_guard<std::mutex> g(impl_->cache_mu);
    const auto it = impl_->attr_by_handle.find(rk);
    if (it != impl_->attr_by_handle.end()) {
      return it->second;
    }
  }
  rti::v1::GetAttributeNameRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_class_handle(cls.raw());
  req.set_attribute_handle(handle.raw());
  grpc::ClientContext ctx;
  rti::v1::GetAttributeNameResponse resp;
  const auto status = impl_->support_stub->GetAttributeName(&ctx, req, &resp);
  if (!status.ok()) {
    throwFromStatus(status, "getAttributeName");
  }
  const auto& name = resp.attribute_name();
  std::lock_guard<std::mutex> g(impl_->cache_mu);
  impl_->attr_by_handle[rk] = name;
  impl_->attr_by_name[{cls.raw(), name}] = handle.raw();
  return name;
}

InteractionClassHandle RTIambassador::getInteractionClassHandle(
    const std::string& name) {
  impl_->requireConnected();
  {
    std::lock_guard<std::mutex> g(impl_->cache_mu);
    const auto it = impl_->int_class_by_name.find(name);
    if (it != impl_->int_class_by_name.end()) {
      return InteractionClassHandle(it->second);
    }
  }
  rti::v1::GetInteractionClassHandleRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_class_name(name);
  grpc::ClientContext ctx;
  rti::v1::GetInteractionClassHandleResponse resp;
  const auto status =
      impl_->support_stub->GetInteractionClassHandle(&ctx, req, &resp);
  if (!status.ok()) {
    throwFromStatus(status, "getInteractionClassHandle(" + name + ")");
  }
  const auto h = resp.class_handle();
  std::lock_guard<std::mutex> g(impl_->cache_mu);
  impl_->int_class_by_name[name] = h;
  impl_->int_class_by_handle[h] = name;
  return InteractionClassHandle(h);
}

std::string RTIambassador::getInteractionClassName(
    InteractionClassHandle handle) {
  impl_->requireConnected();
  {
    std::lock_guard<std::mutex> g(impl_->cache_mu);
    const auto it = impl_->int_class_by_handle.find(handle.raw());
    if (it != impl_->int_class_by_handle.end()) {
      return it->second;
    }
  }
  rti::v1::GetInteractionClassNameRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_class_handle(handle.raw());
  grpc::ClientContext ctx;
  rti::v1::GetInteractionClassNameResponse resp;
  const auto status =
      impl_->support_stub->GetInteractionClassName(&ctx, req, &resp);
  if (!status.ok()) {
    throwFromStatus(status, "getInteractionClassName");
  }
  const auto& name = resp.class_name();
  std::lock_guard<std::mutex> g(impl_->cache_mu);
  impl_->int_class_by_name[name] = handle.raw();
  impl_->int_class_by_handle[handle.raw()] = name;
  return name;
}

ParameterHandle RTIambassador::getParameterHandle(
    InteractionClassHandle cls, const std::string& name) {
  impl_->requireConnected();
  const RTIambassadorImpl::ParamKey key{cls.raw(), name};
  {
    std::lock_guard<std::mutex> g(impl_->cache_mu);
    const auto it = impl_->param_by_name.find(key);
    if (it != impl_->param_by_name.end()) {
      return ParameterHandle(it->second);
    }
  }
  rti::v1::GetParameterHandleRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_class_handle(cls.raw());
  req.set_parameter_name(name);
  grpc::ClientContext ctx;
  rti::v1::GetParameterHandleResponse resp;
  const auto status = impl_->support_stub->GetParameterHandle(&ctx, req, &resp);
  if (!status.ok()) {
    throwFromStatus(status, "getParameterHandle(" + name + ")");
  }
  const auto h = resp.parameter_handle();
  std::lock_guard<std::mutex> g(impl_->cache_mu);
  impl_->param_by_name[key] = h;
  impl_->param_by_handle[{cls.raw(), std::to_string(h)}] = name;
  return ParameterHandle(h);
}

std::string RTIambassador::getParameterName(InteractionClassHandle cls,
                                            ParameterHandle handle) {
  impl_->requireConnected();
  const RTIambassadorImpl::ParamKey rk{cls.raw(), std::to_string(handle.raw())};
  {
    std::lock_guard<std::mutex> g(impl_->cache_mu);
    const auto it = impl_->param_by_handle.find(rk);
    if (it != impl_->param_by_handle.end()) {
      return it->second;
    }
  }
  rti::v1::GetParameterNameRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_class_handle(cls.raw());
  req.set_parameter_handle(handle.raw());
  grpc::ClientContext ctx;
  rti::v1::GetParameterNameResponse resp;
  const auto status = impl_->support_stub->GetParameterName(&ctx, req, &resp);
  if (!status.ok()) {
    throwFromStatus(status, "getParameterName");
  }
  const auto& name = resp.parameter_name();
  std::lock_guard<std::mutex> g(impl_->cache_mu);
  impl_->param_by_handle[rk] = name;
  impl_->param_by_name[{cls.raw(), name}] = handle.raw();
  return name;
}

// --- M17.9 §6.30 / §6.31 runtime instance handle services ----------------

ObjectInstanceHandle RTIambassador::getObjectInstanceHandle(
    const std::string& name) {
  impl_->requireConnected();
  if (!impl_->joined) {
    throw FederateNotExecutionMember(
        "getObjectInstanceHandle: federate not joined to any federation");
  }
  rti::v1::GetObjectInstanceHandleRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_object_name(name);
  grpc::ClientContext ctx;
  rti::v1::GetObjectInstanceHandleResponse resp;
  const auto s = impl_->support_stub->GetObjectInstanceHandle(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "getObjectInstanceHandle(" + name + ")");
  return ObjectInstanceHandle(resp.object_handle());
}

std::string RTIambassador::getObjectInstanceName(ObjectInstanceHandle handle) {
  impl_->requireConnected();
  if (!impl_->joined) {
    throw FederateNotExecutionMember(
        "getObjectInstanceName: federate not joined to any federation");
  }
  rti::v1::GetObjectInstanceNameRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_object_handle(handle.raw());
  grpc::ClientContext ctx;
  rti::v1::GetObjectInstanceNameResponse resp;
  const auto s = impl_->support_stub->GetObjectInstanceName(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "getObjectInstanceName");
  return resp.object_name();
}

// --- M17.4 §5 publish / subscribe declarations -----------------------------

namespace {

// Helper — the four object-class declaration RPCs share a request
// shape (federation/federate/class_handle + attribute_handles[]).
// Templated wrapper that returns the gRPC Status; caller dispatches
// to the appropriate stub method via a function pointer.
template <typename Req>
void fillObjAttrReq(Req& req,
                    const std::string& federation_name,
                    HandleValue federate_handle,
                    HandleValue class_handle,
                    const AttributeHandleSet& attrs) {
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(federation_name);
  req.set_federate_handle(federate_handle);
  req.set_object_class_handle(class_handle);
  for (const auto& a : attrs) {
    req.add_attribute_handles(a.raw());
  }
}

void requireJoined(const RTIambassadorImpl& impl, std::string_view op) {
  impl.requireConnected();
  if (!impl.joined) {
    throw FederateNotExecutionMember(
        std::string(op) + ": federate not joined to any federation");
  }
}

}  // namespace

void RTIambassador::publishObjectClassAttributes(
    ObjectClassHandle cls, const AttributeHandleSet& attributes) {
  requireJoined(*impl_, "publishObjectClassAttributes");
  rti::v1::PubObjAttrsRequest req;
  fillObjAttrReq(req, impl_->joined_federation,
                 impl_->federate_handle.raw(), cls.raw(), attributes);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->declaration_stub->PublishObjectClassAttributes(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "publishObjectClassAttributes");
}

void RTIambassador::unpublishObjectClassAttributes(
    ObjectClassHandle cls, const AttributeHandleSet& attributes) {
  requireJoined(*impl_, "unpublishObjectClassAttributes");
  rti::v1::UnpubObjAttrsRequest req;
  fillObjAttrReq(req, impl_->joined_federation,
                 impl_->federate_handle.raw(), cls.raw(), attributes);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->declaration_stub->UnpublishObjectClassAttributes(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "unpublishObjectClassAttributes");
}

void RTIambassador::subscribeObjectClassAttributes(
    ObjectClassHandle cls, const AttributeHandleSet& attributes) {
  requireJoined(*impl_, "subscribeObjectClassAttributes");
  rti::v1::SubObjAttrsRequest req;
  fillObjAttrReq(req, impl_->joined_federation,
                 impl_->federate_handle.raw(), cls.raw(), attributes);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->declaration_stub->SubscribeObjectClassAttributes(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "subscribeObjectClassAttributes");
}

void RTIambassador::unsubscribeObjectClassAttributes(
    ObjectClassHandle cls, const AttributeHandleSet& attributes) {
  requireJoined(*impl_, "unsubscribeObjectClassAttributes");
  rti::v1::UnsubObjAttrsRequest req;
  fillObjAttrReq(req, impl_->joined_federation,
                 impl_->federate_handle.raw(), cls.raw(), attributes);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->declaration_stub->UnsubscribeObjectClassAttributes(
      &ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "unsubscribeObjectClassAttributes");
}

void RTIambassador::publishInteractionClass(InteractionClassHandle cls) {
  requireJoined(*impl_, "publishInteractionClass");
  rti::v1::PubInterRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_interaction_class_handle(cls.raw());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->declaration_stub->PublishInteractionClass(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "publishInteractionClass");
}

void RTIambassador::unpublishInteractionClass(InteractionClassHandle cls) {
  requireJoined(*impl_, "unpublishInteractionClass");
  rti::v1::UnpubInterRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_interaction_class_handle(cls.raw());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->declaration_stub->UnpublishInteractionClass(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "unpublishInteractionClass");
}

void RTIambassador::subscribeInteractionClass(InteractionClassHandle cls) {
  requireJoined(*impl_, "subscribeInteractionClass");
  rti::v1::SubInterRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_interaction_class_handle(cls.raw());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->declaration_stub->SubscribeInteractionClass(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "subscribeInteractionClass");
}

void RTIambassador::unsubscribeInteractionClass(InteractionClassHandle cls) {
  requireJoined(*impl_, "unsubscribeInteractionClass");
  rti::v1::UnsubInterRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_interaction_class_handle(cls.raw());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->declaration_stub->UnsubscribeInteractionClass(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "unsubscribeInteractionClass");
}

// --- M17.5 §6 register / update / send -----------------------------------

ObjectInstanceHandle RTIambassador::registerObjectInstance(
    ObjectClassHandle cls, const std::string& instance_name) {
  requireJoined(*impl_, "registerObjectInstance");
  rti::v1::RegisterObjectRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_object_class_handle(cls.raw());
  req.set_object_name(instance_name);
  grpc::ClientContext ctx;
  rti::v1::RegisterObjectResponse resp;
  const auto s = impl_->object_stub->RegisterObjectInstance(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "registerObjectInstance");
  return ObjectInstanceHandle(resp.object_handle());
}

void RTIambassador::updateAttributeValues(
    ObjectInstanceHandle obj, const AttributeHandleValueMap& values) {
  requireJoined(*impl_, "updateAttributeValues");
  rti::v1::UpdateAttributeValuesRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_object_handle(obj.raw());
  // proto map<uint64, bytes> — copy each value into the map entry.
  auto* m = req.mutable_attributes();
  for (const auto& kv : values) {
    (*m)[kv.first.raw()] = std::string(kv.second.begin(), kv.second.end());
  }
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->object_stub->UpdateAttributeValues(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "updateAttributeValues");
}

void RTIambassador::sendInteraction(
    InteractionClassHandle cls, const ParameterHandleValueMap& parameters) {
  requireJoined(*impl_, "sendInteraction");
  rti::v1::SendInteractionRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_interaction_class_handle(cls.raw());
  auto* m = req.mutable_parameters();
  for (const auto& kv : parameters) {
    (*m)[kv.first.raw()] = std::string(kv.second.begin(), kv.second.end());
  }
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->object_stub->SendInteraction(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "sendInteraction");
}

// --- M17.6 §10.4 tickCallback + FederateAmbassador ------------------------

void RTIambassador::setFederateAmbassador(FederateAmbassador* fed) {
  impl_->fed_ambassador = fed;
}

void RTIambassadorImpl::startEventStream() {
  if (stream_running.exchange(true)) {
    return;  // already running
  }
  stream_ctx = std::make_unique<grpc::ClientContext>();
  rti::v1::EventsRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(joined_federation);
  req.set_federate_handle(federate_handle.raw());

  // Capture by value where the lambda needs to outlive the stack
  // frame; the request + reader live inside the thread.
  stream_thread = std::thread([this, req]() {
    auto reader = stream_stub->Events(stream_ctx.get(), req);
    rti::v1::FederateEvent evt;
    while (reader->Read(&evt)) {
      {
        std::lock_guard<std::mutex> g(event_mu);
        event_queue.push_back(evt);
      }
      event_cv.notify_one();
    }
    // Reader::Finish discards Status — the stream closes on
    // resign/disconnect/federation-halt. M17.6 doesn't surface
    // halt to user code; a future cut adds federationHalted.
    static_cast<void>(reader->Finish());
  });
}

void RTIambassadorImpl::stopEventStream() {
  if (!stream_running.exchange(false)) {
    return;
  }
  if (stream_ctx) {
    stream_ctx->TryCancel();
  }
  if (stream_thread.joinable()) {
    stream_thread.join();
  }
  stream_ctx.reset();
  {
    std::lock_guard<std::mutex> g(event_mu);
    event_queue.clear();
  }
}

bool RTIambassador::tickCallback(double approx_min_time,
                                 double approx_max_time) {
  using clock = std::chrono::steady_clock;
  using std::chrono::duration;
  using std::chrono::duration_cast;
  using std::chrono::milliseconds;

  if (approx_max_time < approx_min_time) approx_max_time = approx_min_time;
  const auto start = clock::now();
  const auto min_deadline =
      start + duration_cast<clock::duration>(duration<double>(approx_min_time));
  const auto max_deadline =
      start + duration_cast<clock::duration>(duration<double>(approx_max_time));

  bool any_fired = false;

  auto drainOne = [&]() -> bool {
    rti::v1::FederateEvent evt;
    {
      std::unique_lock<std::mutex> lk(impl_->event_mu);
      if (impl_->event_queue.empty()) return false;
      evt = std::move(impl_->event_queue.front());
      impl_->event_queue.pop_front();
    }
    if (impl_->fed_ambassador == nullptr) {
      // No ambassador bound — drop. The cppsdk doesn't buffer events
      // for a late-bound callback target in Cut-1.
      return true;
    }
    switch (evt.event_case()) {
      case rti::v1::FederateEvent::kDiscover: {
        const auto& d = evt.discover();
        impl_->fed_ambassador->discoverObjectInstance(
            ObjectInstanceHandle(d.object_handle()),
            ObjectClassHandle(d.object_class_handle()),
            d.object_name());
        return true;
      }
      case rti::v1::FederateEvent::kReflect: {
        const auto& r = evt.reflect();
        AttributeHandleValueMap values;
        for (const auto& kv : r.attributes()) {
          VariableLengthData v(kv.second.begin(), kv.second.end());
          values.emplace(AttributeHandle(kv.first), std::move(v));
        }
        std::optional<double> ts =
            r.has_logical_time() ? std::optional<double>(r.logical_time())
                                 : std::nullopt;
        impl_->fed_ambassador->reflectAttributeValues(
            ObjectInstanceHandle(r.object_handle()), values, ts);
        return true;
      }
      case rti::v1::FederateEvent::kReceive: {
        const auto& i = evt.receive();
        ParameterHandleValueMap params;
        for (const auto& kv : i.parameters()) {
          VariableLengthData v(kv.second.begin(), kv.second.end());
          params.emplace(ParameterHandle(kv.first), std::move(v));
        }
        std::optional<double> ts =
            i.has_logical_time() ? std::optional<double>(i.logical_time())
                                 : std::nullopt;
        impl_->fed_ambassador->receiveInteraction(
            InteractionClassHandle(i.interaction_class_handle()), params, ts);
        return true;
      }
      default:
        // Cut-1 ignores events outside the discover/reflect/receive
        // set (remove/provide/sync/ownership/save) — they're queued
        // by the stream but not dispatched. Cut-2 adds matching slots.
        return true;
    }
  };

  // Honor the min/max window. Sleep in small increments so a callback
  // arriving early can still fire promptly.
  while (clock::now() < min_deadline) {
    if (drainOne()) any_fired = true;
    if (clock::now() < min_deadline) {
      std::unique_lock<std::mutex> lk(impl_->event_mu);
      impl_->event_cv.wait_for(lk, milliseconds(5),
                               [&] { return !impl_->event_queue.empty(); });
    }
  }
  // Drain anything else queued.
  while (drainOne()) any_fired = true;
  // If max_time > min_time and nothing has fired yet, wait until
  // either an event arrives or the max deadline elapses.
  while (!any_fired && clock::now() < max_deadline) {
    std::unique_lock<std::mutex> lk(impl_->event_mu);
    impl_->event_cv.wait_for(lk, milliseconds(5),
                             [&] { return !impl_->event_queue.empty(); });
    lk.unlock();
    if (drainOne()) any_fired = true;
  }

  return any_fired;
}

}  // namespace rti1516e

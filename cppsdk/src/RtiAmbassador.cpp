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
#include "rti/v1/time.grpc.pb.h"
#include "rti/v1/time.pb.h"
#include "rti/v1/mom.grpc.pb.h"
#include "rti/v1/mom.pb.h"
#include "rti/v1/sync.grpc.pb.h"
#include "rti/v1/sync.pb.h"
#include "rti/v1/ownership.grpc.pb.h"
#include "rti/v1/ownership.pb.h"
#include "rti/v1/savepoint.grpc.pb.h"
#include "rti/v1/savepoint.pb.h"
#include "rti/v1/ddm.grpc.pb.h"
#include "rti/v1/ddm.pb.h"
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
  std::unique_ptr<rti::v1::TimeService::Stub> time_stub;
  std::unique_ptr<rti::v1::MomService::Stub> mom_stub;
  std::unique_ptr<rti::v1::SyncService::Stub> sync_stub;
  std::unique_ptr<rti::v1::OwnershipService::Stub> ownership_stub;
  std::unique_ptr<rti::v1::SavepointService::Stub> savepoint_stub;
  std::unique_ptr<rti::v1::DDMService::Stub> ddm_stub;

  // --- M17.6 callback state ---
  // Bound FederateAmbassador (nullable). tickCallback dispatches
  // queued events onto this. Set via setFederateAmbassador.
  FederateAmbassador* fed_ambassador = nullptr;

  // Event queue + the background thread that drains the streaming RPC.
  std::mutex event_mu;
  std::condition_variable event_cv;
  std::deque<rti::v1::FederateEvent> event_queue;
  std::atomic<bool> stream_running{false};
  // M17.18 — when false, tick/evoke variants return without
  // draining. The background reader keeps pushing into event_queue;
  // re-enable to resume dispatch (no events lost).
  std::atomic<bool> callbacks_enabled{true};

  std::thread stream_thread;
  std::unique_ptr<grpc::ClientContext> stream_ctx;

  void startEventStream();
  void stopEventStream();

  // M17.21 — pop ONE event and dispatch via the bound
  // FederateAmbassador. Returns true if an event was popped
  // (regardless of whether the case had a matching slot —
  // unsupported cases drop silently). Holds event_mu only while
  // popping; the FederateAmbassador slot runs OUTSIDE the lock.
  bool dispatchOneEvent();
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

M17RTIambassador::M17RTIambassador() : impl_(std::make_unique<RTIambassadorImpl>()) {}

// M35 Agent BH — explicit destructor: stop the background event stream
// BEFORE the RTIambassadorImpl unique_ptr default-destructs its
// `std::thread stream_thread`. If stream_thread is still joinable at
// destruction time (e.g. the federate threw between join and disconnect)
// the std::thread dtor invokes std::terminate — killing exception
// unwind through the fixture's `catch (Exception&)` block.
//
// stopEventStream() is idempotent (guarded by stream_running.exchange),
// so calling it here has no side-effect when the caller already
// disconnected. The impl unique_ptr then default-destructs safely.
M17RTIambassador::~M17RTIambassador() {
  if (impl_) {
    impl_->stopEventStream();
  }
}

M17RTIambassador::M17RTIambassador(M17RTIambassador&&) noexcept = default;
M17RTIambassador& M17RTIambassador::operator=(M17RTIambassador&&) noexcept = default;

void M17RTIambassador::connect(const std::string& url) {
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
  impl_->time_stub = rti::v1::TimeService::NewStub(impl_->channel);
  impl_->mom_stub = rti::v1::MomService::NewStub(impl_->channel);
  impl_->sync_stub = rti::v1::SyncService::NewStub(impl_->channel);
  impl_->ownership_stub = rti::v1::OwnershipService::NewStub(impl_->channel);
  impl_->savepoint_stub = rti::v1::SavepointService::NewStub(impl_->channel);
  impl_->ddm_stub = rti::v1::DDMService::NewStub(impl_->channel);
  impl_->url = url;
  impl_->connected = true;
}

void M17RTIambassador::disconnect() {
  impl_->stopEventStream();
  impl_->federation_stub.reset();
  impl_->support_stub.reset();
  impl_->declaration_stub.reset();
  impl_->object_stub.reset();
  impl_->stream_stub.reset();
  impl_->time_stub.reset();
  impl_->mom_stub.reset();
  impl_->sync_stub.reset();
  impl_->ownership_stub.reset();
  impl_->savepoint_stub.reset();
  impl_->ddm_stub.reset();
  impl_->channel.reset();
  impl_->url.clear();
  impl_->connected = false;
  impl_->joined_federation.clear();
  impl_->federate_handle = FederateHandle{};
  impl_->joined = false;
  impl_->clearHandleCaches();
}

bool M17RTIambassador::isConnected() const noexcept {
  return impl_->connected;
}

void M17RTIambassador::createFederationExecution(
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

void M17RTIambassador::destroyFederationExecution(
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

FederateHandle M17RTIambassador::joinFederationExecution(
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

void M17RTIambassador::resignFederationExecution() {
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

ObjectClassHandle M17RTIambassador::getObjectClassHandle(
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

std::string M17RTIambassador::getObjectClassName(ObjectClassHandle handle) {
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

AttributeHandle M17RTIambassador::getAttributeHandle(ObjectClassHandle cls,
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

std::string M17RTIambassador::getAttributeName(ObjectClassHandle cls,
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

InteractionClassHandle M17RTIambassador::getInteractionClassHandle(
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

std::string M17RTIambassador::getInteractionClassName(
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

ParameterHandle M17RTIambassador::getParameterHandle(
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

std::string M17RTIambassador::getParameterName(InteractionClassHandle cls,
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

ObjectInstanceHandle M17RTIambassador::getObjectInstanceHandle(
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

std::string M17RTIambassador::getObjectInstanceName(ObjectInstanceHandle handle) {
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

// --- M17.10 §6.1-5 object instance name reservation ----------------------

void M17RTIambassador::reserveObjectInstanceName(const std::string& name) {
  impl_->requireConnected();
  if (!impl_->joined) {
    throw FederateNotExecutionMember(
        "reserveObjectInstanceName: federate not joined to any federation");
  }
  rti::v1::ReserveObjectInstanceNameRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_object_name(name);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->object_stub->ReserveObjectInstanceName(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "reserveObjectInstanceName");
}

void M17RTIambassador::reserveMultipleObjectInstanceNames(
    const std::vector<std::string>& names) {
  impl_->requireConnected();
  if (!impl_->joined) {
    throw FederateNotExecutionMember(
        "reserveMultipleObjectInstanceNames: federate not joined");
  }
  rti::v1::ReserveMultipleObjectInstanceNamesRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  for (const auto& n : names) {
    req.add_object_names(n);
  }
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->object_stub->ReserveMultipleObjectInstanceNames(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "reserveMultipleObjectInstanceNames");
}

void M17RTIambassador::releaseObjectInstanceName(const std::string& name) {
  impl_->requireConnected();
  if (!impl_->joined) {
    throw FederateNotExecutionMember(
        "releaseObjectInstanceName: federate not joined to any federation");
  }
  rti::v1::ReleaseObjectInstanceNameRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_object_name(name);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->object_stub->ReleaseObjectInstanceName(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "releaseObjectInstanceName");
}

// --- M17.11 §8 Time Management ---------------------------------------------
//
// Each method (a) checks join, (b) fills a typed request with
// federation_name / federate_handle (and time / lookahead where
// applicable), (c) invokes the TimeService stub, (d) translates a
// non-OK status into an Annex C exception via throwFromStatus.
//
// Grants are async — TAR / TARA / NER / NMRA / FQR return Empty;
// the manager emits a TimeAdvanceGrant on the event stream which
// tickCallback dispatches as timeAdvanceGrant().

namespace {
// All time RPCs share the same join precondition; this helper keeps
// each method readable. Inlined rather than calling requireJoined()
// because that helper sits later in the file as an anon-namespace
// member (visibility scope from M17.9).
void requireJoinedForTime(bool joined, const char* method) {
  if (!joined) {
    throw FederateNotExecutionMember(std::string(method) +
                                     ": federate not joined to any federation");
  }
}
}  // namespace

void M17RTIambassador::enableTimeRegulation(double lookahead) {
  impl_->requireConnected();
  requireJoinedForTime(impl_->joined, "enableTimeRegulation");
  rti::v1::EnableRegulationRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_lookahead(lookahead);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->time_stub->EnableTimeRegulation(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "enableTimeRegulation");
}

void M17RTIambassador::disableTimeRegulation() {
  impl_->requireConnected();
  requireJoinedForTime(impl_->joined, "disableTimeRegulation");
  rti::v1::DisableRegulationRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->time_stub->DisableTimeRegulation(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "disableTimeRegulation");
}

void M17RTIambassador::enableTimeConstrained() {
  impl_->requireConnected();
  requireJoinedForTime(impl_->joined, "enableTimeConstrained");
  rti::v1::EnableConstrainedRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->time_stub->EnableTimeConstrained(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "enableTimeConstrained");
}

void M17RTIambassador::disableTimeConstrained() {
  impl_->requireConnected();
  requireJoinedForTime(impl_->joined, "disableTimeConstrained");
  rti::v1::DisableConstrainedRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->time_stub->DisableTimeConstrained(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "disableTimeConstrained");
}

void M17RTIambassador::modifyLookahead(double lookahead) {
  impl_->requireConnected();
  requireJoinedForTime(impl_->joined, "modifyLookahead");
  rti::v1::ModifyLookaheadRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_lookahead(lookahead);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->time_stub->ModifyLookahead(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "modifyLookahead");
}

void M17RTIambassador::timeAdvanceRequest(double time) {
  impl_->requireConnected();
  requireJoinedForTime(impl_->joined, "timeAdvanceRequest");
  rti::v1::TARRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_logical_time(time);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->time_stub->TimeAdvanceRequest(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "timeAdvanceRequest");
}

void M17RTIambassador::timeAdvanceRequestAvailable(double time) {
  impl_->requireConnected();
  requireJoinedForTime(impl_->joined, "timeAdvanceRequestAvailable");
  rti::v1::TARARequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_logical_time(time);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->time_stub->TimeAdvanceRequestAvailable(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "timeAdvanceRequestAvailable");
}

void M17RTIambassador::nextMessageRequest(double time) {
  impl_->requireConnected();
  requireJoinedForTime(impl_->joined, "nextMessageRequest");
  rti::v1::NERRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_logical_time(time);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->time_stub->NextMessageRequest(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "nextMessageRequest");
}

void M17RTIambassador::nextMessageRequestAvailable(double time) {
  impl_->requireConnected();
  requireJoinedForTime(impl_->joined, "nextMessageRequestAvailable");
  rti::v1::NMRARequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_logical_time(time);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->time_stub->NextMessageRequestAvailable(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "nextMessageRequestAvailable");
}

void M17RTIambassador::flushQueueRequest(double time) {
  impl_->requireConnected();
  requireJoinedForTime(impl_->joined, "flushQueueRequest");
  rti::v1::FQRRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_logical_time(time);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->time_stub->FlushQueueRequest(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "flushQueueRequest");
}

double M17RTIambassador::queryLogicalTime() {
  impl_->requireConnected();
  requireJoinedForTime(impl_->joined, "queryLogicalTime");
  rti::v1::QueryFederateTimeRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  grpc::ClientContext ctx;
  rti::v1::QueryFederateTimeResponse resp;
  const auto s = impl_->time_stub->QueryLogicalTime(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "queryLogicalTime");
  return resp.logical_time();
}

double M17RTIambassador::queryLookahead() {
  impl_->requireConnected();
  requireJoinedForTime(impl_->joined, "queryLookahead");
  rti::v1::QueryFederateTimeRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  grpc::ClientContext ctx;
  rti::v1::QueryLookaheadResponse resp;
  const auto s = impl_->time_stub->QueryLookahead(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "queryLookahead");
  return resp.lookahead();
}

M17RTIambassador::LBTSResult RTIambassador::queryLBTS() {
  impl_->requireConnected();
  requireJoinedForTime(impl_->joined, "queryLBTS");
  rti::v1::QueryLBTSRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  grpc::ClientContext ctx;
  rti::v1::QueryLBTSResponse resp;
  const auto s = impl_->time_stub->QueryLBTS(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "queryLBTS");
  return LBTSResult{resp.lbts(), resp.finite()};
}

M17RTIambassador::GALTResult RTIambassador::queryGALT() {
  impl_->requireConnected();
  requireJoinedForTime(impl_->joined, "queryGALT");
  rti::v1::QueryFederateTimeRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  grpc::ClientContext ctx;
  rti::v1::QueryGALTResponse resp;
  const auto s = impl_->time_stub->QueryGALT(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "queryGALT");
  return GALTResult{resp.galt(), resp.finite()};
}

M17RTIambassador::LITSResult RTIambassador::queryLITS() {
  impl_->requireConnected();
  requireJoinedForTime(impl_->joined, "queryLITS");
  rti::v1::QueryFederateTimeRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  grpc::ClientContext ctx;
  rti::v1::QueryLITSResponse resp;
  const auto s = impl_->time_stub->QueryLITS(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "queryLITS");
  return LITSResult{resp.lits(), resp.finite()};
}

void M17RTIambassador::enableAsynchronousDelivery() {
  impl_->requireConnected();
  requireJoinedForTime(impl_->joined, "enableAsynchronousDelivery");
  rti::v1::EnableAsynchronousDeliveryRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->time_stub->EnableAsynchronousDelivery(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "enableAsynchronousDelivery");
}

void M17RTIambassador::disableAsynchronousDelivery() {
  impl_->requireConnected();
  requireJoinedForTime(impl_->joined, "disableAsynchronousDelivery");
  rti::v1::DisableAsynchronousDeliveryRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->time_stub->DisableAsynchronousDelivery(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "disableAsynchronousDelivery");
}

// --- M17.13 §11 MOM ambassador delegates ---------------------------------
//
// Read-only introspection of the HLAfederation + per-federate
// HLAfederate MOM objects. The Go server's MomService is itself a
// snapshot of cut-2 mom.Manager state; the C++ surface returns
// typed result structs (FederationAttributes / FederateAttributes /
// MomInstance) so callers don't see the proto types.
//
// All three RPCs require the ambassador to be connected; per-
// federate join is NOT required (a federate-handle-less observer
// could in principle introspect a federation it isn't joined to,
// matching the pysdk M27 D.1 behavior).

M17RTIambassador::FederationAttributes
M17RTIambassador::queryFederationAttributes() {
  impl_->requireConnected();
  if (impl_->joined_federation.empty()) {
    throw FederateNotExecutionMember(
        "queryFederationAttributes: federate not joined to any federation");
  }
  rti::v1::QueryFederationAttributesRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  grpc::ClientContext ctx;
  rti::v1::QueryFederationAttributesResponse resp;
  const auto s = impl_->mom_stub->QueryFederationAttributes(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "queryFederationAttributes");
  FederationAttributes out;
  out.federation_name = resp.federation_name();
  out.federate_handles.reserve(resp.federate_handles_size());
  for (auto h : resp.federate_handles()) {
    out.federate_handles.emplace_back(h);
  }
  out.fom_module_names.assign(resp.fom_module_names().begin(),
                              resp.fom_module_names().end());
  return out;
}

M17RTIambassador::FederateAttributes
M17RTIambassador::queryFederateAttributes(FederateHandle federate) {
  impl_->requireConnected();
  if (impl_->joined_federation.empty()) {
    throw FederateNotExecutionMember(
        "queryFederateAttributes: federate not joined to any federation");
  }
  rti::v1::QueryFederateAttributesRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(federate.raw());
  grpc::ClientContext ctx;
  rti::v1::QueryFederateAttributesResponse resp;
  const auto s = impl_->mom_stub->QueryFederateAttributes(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "queryFederateAttributes");
  FederateAttributes out;
  out.found = resp.found();
  out.federate_handle = FederateHandle(resp.federate_handle());
  out.federate_name = resp.federate_name();
  out.federate_type = resp.federate_type();
  out.time_regulating = resp.time_regulating();
  out.time_constrained = resp.time_constrained();
  // LogicalTime wrapper — server returns the typed message; we
  // unwrap to double. Empty message → 0.0.
  out.logical_time =
      resp.has_logical_time() ? resp.logical_time().value() : 0.0;
  out.lookahead =
      resp.has_lookahead() ? resp.lookahead().value() : 0.0;
  out.interactions_sent = resp.interactions_sent();
  out.interactions_received = resp.interactions_received();
  out.updates_sent = resp.updates_sent();
  out.reflections_received = resp.reflections_received();
  return out;
}

std::vector<M17RTIambassador::MomInstance>
M17RTIambassador::enumerateMomInstances() {
  impl_->requireConnected();
  if (impl_->joined_federation.empty()) {
    throw FederateNotExecutionMember(
        "enumerateMomInstances: federate not joined to any federation");
  }
  rti::v1::EnumerateMomInstancesRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  grpc::ClientContext ctx;
  rti::v1::EnumerateMomInstancesResponse resp;
  const auto s = impl_->mom_stub->EnumerateMomInstances(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "enumerateMomInstances");
  std::vector<MomInstance> out;
  out.reserve(resp.instances_size());
  for (const auto& inst : resp.instances()) {
    MomInstance m;
    m.class_name = inst.class_name();
    m.federate_handle = FederateHandle(inst.federate_handle());
    m.instance_name = inst.instance_name();
    out.push_back(std::move(m));
  }
  return out;
}

// --- M17.14 §4.7 Synchronization points ----------------------------------
//
// Register → server broadcasts SynchronizationPointAnnounced to each
// required federate. Each federate eventually calls Achieve →
// once the last required federate achieves, server broadcasts
// FederationSynchronized to the whole required set.
//
// Both events drop into tickCallback dispatch as
// announceSynchronizationPoint(label, tag) and
// federationSynchronized(label) overrides.

void M17RTIambassador::registerFederationSynchronizationPoint(
    const std::string& label,
    const VariableLengthData& tag,
    const std::vector<FederateHandle>& required_federates) {
  impl_->requireConnected();
  if (!impl_->joined) {
    throw FederateNotExecutionMember(
        "registerFederationSynchronizationPoint: federate not joined");
  }
  rti::v1::RegisterSyncPointRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_label(label);
  req.set_tag(tag.data(), tag.size());
  for (auto h : required_federates) {
    req.add_required_federates(h.raw());
  }
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->sync_stub->RegisterFederationSynchronizationPoint(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "registerFederationSynchronizationPoint");
}

void M17RTIambassador::synchronizationPointAchieved(const std::string& label) {
  impl_->requireConnected();
  if (!impl_->joined) {
    throw FederateNotExecutionMember(
        "synchronizationPointAchieved: federate not joined");
  }
  rti::v1::AchieveSyncPointRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_label(label);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->sync_stub->SynchronizationPointAchieved(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "synchronizationPointAchieved");
}

// --- M17.15 §7 Ownership Management ---------------------------------------
//
// Eight RPCs over the OwnershipService. All require join; all
// fill a (federation/federate/object/attributes) request and
// translate non-OK status into Annex C exceptions. The negotiated
// transfer outcomes arrive on the event stream as
// kOwnershipAssumption / kOwnershipAcquired / kOwnershipDivestConfirmed.

namespace {

template <typename Req>
void fillObjectAttrsReq(Req& req,
                        const std::string& federation,
                        HandleValue federate,
                        HandleValue object,
                        const AttributeHandleSet& attrs) {
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(federation);
  req.set_federate_handle(federate);
  req.set_object_handle(object);
  for (const auto& a : attrs) req.add_attribute_handles(a.raw());
}

void requireJoinedForOwnership(bool joined, const char* method) {
  if (!joined) {
    throw FederateNotExecutionMember(std::string(method) +
                                     ": federate not joined to any federation");
  }
}

}  // namespace

void M17RTIambassador::unconditionalAttributeOwnershipDivestiture(
    ObjectInstanceHandle object,
    const AttributeHandleSet& attributes) {
  impl_->requireConnected();
  requireJoinedForOwnership(impl_->joined,
                            "unconditionalAttributeOwnershipDivestiture");
  rti::v1::UnconditionalDivestRequest req;
  fillObjectAttrsReq(req, impl_->joined_federation,
                     impl_->federate_handle.raw(), object.raw(), attributes);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->ownership_stub->UnconditionalAttributeOwnershipDivestiture(
      &ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "unconditionalAttributeOwnershipDivestiture");
}

void M17RTIambassador::negotiatedAttributeOwnershipDivestiture(
    ObjectInstanceHandle object,
    const AttributeHandleSet& attributes,
    const VariableLengthData& tag) {
  impl_->requireConnected();
  requireJoinedForOwnership(impl_->joined,
                            "negotiatedAttributeOwnershipDivestiture");
  rti::v1::NegotiatedDivestRequest req;
  fillObjectAttrsReq(req, impl_->joined_federation,
                     impl_->federate_handle.raw(), object.raw(), attributes);
  req.set_tag(tag.data(), tag.size());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->ownership_stub->NegotiatedAttributeOwnershipDivestiture(
      &ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "negotiatedAttributeOwnershipDivestiture");
}

void M17RTIambassador::attributeOwnershipAcquisition(
    ObjectInstanceHandle object,
    const AttributeHandleSet& attributes) {
  impl_->requireConnected();
  requireJoinedForOwnership(impl_->joined, "attributeOwnershipAcquisition");
  rti::v1::AcquireRequest req;
  fillObjectAttrsReq(req, impl_->joined_federation,
                     impl_->federate_handle.raw(), object.raw(), attributes);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->ownership_stub->AttributeOwnershipAcquisition(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "attributeOwnershipAcquisition");
}

void M17RTIambassador::cancelNegotiatedAttributeOwnershipDivestiture(
    ObjectInstanceHandle object,
    const AttributeHandleSet& attributes) {
  impl_->requireConnected();
  requireJoinedForOwnership(impl_->joined,
                            "cancelNegotiatedAttributeOwnershipDivestiture");
  rti::v1::CancelDivestRequest req;
  fillObjectAttrsReq(req, impl_->joined_federation,
                     impl_->federate_handle.raw(), object.raw(), attributes);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->ownership_stub->CancelNegotiatedAttributeOwnershipDivestiture(
          &ctx, req, &resp);
  if (!s.ok())
    throwFromStatus(s, "cancelNegotiatedAttributeOwnershipDivestiture");
}

void M17RTIambassador::cancelAttributeOwnershipAcquisition(
    ObjectInstanceHandle object,
    const AttributeHandleSet& attributes) {
  impl_->requireConnected();
  requireJoinedForOwnership(impl_->joined,
                            "cancelAttributeOwnershipAcquisition");
  rti::v1::CancelAcquireRequest req;
  fillObjectAttrsReq(req, impl_->joined_federation,
                     impl_->federate_handle.raw(), object.raw(), attributes);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->ownership_stub->CancelAttributeOwnershipAcquisition(
      &ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "cancelAttributeOwnershipAcquisition");
}

void M17RTIambassador::attributeOwnershipDivestitureIfWanted(
    ObjectInstanceHandle object,
    const AttributeHandleSet& attributes) {
  impl_->requireConnected();
  requireJoinedForOwnership(impl_->joined,
                            "attributeOwnershipDivestitureIfWanted");
  rti::v1::DivestIfWantedRequest req;
  fillObjectAttrsReq(req, impl_->joined_federation,
                     impl_->federate_handle.raw(), object.raw(), attributes);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->ownership_stub->AttributeOwnershipDivestitureIfWanted(
      &ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "attributeOwnershipDivestitureIfWanted");
}

M17RTIambassador::OwnershipQueryResult RTIambassador::queryAttributeOwnership(
    ObjectInstanceHandle object,
    AttributeHandle attribute) {
  impl_->requireConnected();
  requireJoinedForOwnership(impl_->joined, "queryAttributeOwnership");
  rti::v1::QueryOwnershipRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_object_handle(object.raw());
  req.set_attribute_handle(attribute.raw());
  grpc::ClientContext ctx;
  rti::v1::QueryOwnershipResponse resp;
  const auto s = impl_->ownership_stub->QueryAttributeOwnership(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "queryAttributeOwnership");
  return OwnershipQueryResult{FederateHandle(resp.owner_federate_handle()),
                              resp.owned()};
}

bool M17RTIambassador::isAttributeOwnedByFederate(
    ObjectInstanceHandle object,
    AttributeHandle attribute) {
  impl_->requireConnected();
  requireJoinedForOwnership(impl_->joined, "isAttributeOwnedByFederate");
  rti::v1::IsOwnedRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_object_handle(object.raw());
  req.set_attribute_handle(attribute.raw());
  grpc::ClientContext ctx;
  rti::v1::IsOwnedResponse resp;
  const auto s = impl_->ownership_stub->IsAttributeOwnedByFederate(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "isAttributeOwnedByFederate");
  return resp.owned();
}

// --- M17.16 §4.8-15 Save / Restore -----------------------------------------
//
// Save: requester→manager (RequestFederationSave); manager fans out
// InitiateFederateSave events; federates respond
// (FederateSaveComplete/NotComplete); manager fans out
// FederationSaved/NotSaved. Restore: federates can drive the RPCs +
// query state, but gorti's stream.proto only emits SAVE events —
// the restore-side InitiateFederateRestore / FederationRestored
// events aren't wired server-side yet.

namespace {
void requireJoinedForSavepoint(bool joined, const char* method) {
  if (!joined) {
    throw FederateNotExecutionMember(std::string(method) +
                                     ": federate not joined to any federation");
  }
}
}  // namespace

void M17RTIambassador::requestFederationSave(
    const std::string& label,
    std::optional<double> save_time) {
  impl_->requireConnected();
  requireJoinedForSavepoint(impl_->joined, "requestFederationSave");
  rti::v1::RequestFederationSaveRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_label(label);
  if (save_time.has_value()) req.set_save_time(*save_time);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->savepoint_stub->RequestFederationSave(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "requestFederationSave");
}

void M17RTIambassador::federateSaveComplete() {
  impl_->requireConnected();
  requireJoinedForSavepoint(impl_->joined, "federateSaveComplete");
  rti::v1::FederateSaveResponseRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->savepoint_stub->FederateSaveComplete(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "federateSaveComplete");
}

void M17RTIambassador::federateSaveNotComplete() {
  impl_->requireConnected();
  requireJoinedForSavepoint(impl_->joined, "federateSaveNotComplete");
  rti::v1::FederateSaveResponseRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->savepoint_stub->FederateSaveNotComplete(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "federateSaveNotComplete");
}

void M17RTIambassador::abortFederationSave() {
  impl_->requireConnected();
  requireJoinedForSavepoint(impl_->joined, "abortFederationSave");
  rti::v1::AbortFederationSaveRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->savepoint_stub->AbortFederationSave(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "abortFederationSave");
}

M17RTIambassador::SaveState RTIambassador::querySaveState(const std::string& label) {
  impl_->requireConnected();
  requireJoinedForSavepoint(impl_->joined, "querySaveState");
  rti::v1::QuerySaveStateRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_label(label);
  grpc::ClientContext ctx;
  rti::v1::QuerySaveStateResponse resp;
  const auto s = impl_->savepoint_stub->QuerySaveState(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "querySaveState");
  return static_cast<SaveState>(resp.state());
}

void M17RTIambassador::requestFederationRestore(const std::string& label) {
  impl_->requireConnected();
  requireJoinedForSavepoint(impl_->joined, "requestFederationRestore");
  rti::v1::RequestFederationRestoreRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_label(label);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->savepoint_stub->RequestFederationRestore(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "requestFederationRestore");
}

void M17RTIambassador::federateRestoreComplete() {
  impl_->requireConnected();
  requireJoinedForSavepoint(impl_->joined, "federateRestoreComplete");
  rti::v1::FederateRestoreResponseRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->savepoint_stub->FederateRestoreComplete(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "federateRestoreComplete");
}

void M17RTIambassador::abortFederationRestore() {
  impl_->requireConnected();
  requireJoinedForSavepoint(impl_->joined, "abortFederationRestore");
  rti::v1::AbortFederationRestoreRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->savepoint_stub->AbortFederationRestore(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "abortFederationRestore");
}

M17RTIambassador::RestoreState RTIambassador::queryRestoreState(
    const std::string& label) {
  impl_->requireConnected();
  requireJoinedForSavepoint(impl_->joined, "queryRestoreState");
  rti::v1::QueryRestoreStateRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_label(label);
  grpc::ClientContext ctx;
  rti::v1::QueryRestoreStateResponse resp;
  const auto s = impl_->savepoint_stub->QueryRestoreState(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "queryRestoreState");
  return static_cast<RestoreState>(resp.state());
}

// --- M17.17 §9 Data Distribution Management --------------------------------
//
// 16 RPCs over DDMService. Routing-space and dimension lookups are
// O(1) FOM-name resolution (similar to support_stub class/attr
// handle services but no client-side cache — DDM names are
// typically resolved once at federate startup). Regions are
// runtime objects scoped to the calling federate; commit/delete
// require join.

namespace {

void requireJoinedForDDM(bool joined, const char* method) {
  if (!joined) {
    throw FederateNotExecutionMember(std::string(method) +
                                     ": federate not joined to any federation");
  }
}

// Pack an rti1516e::AttributeRegionMap into the repeated
// AttributeRegions proto field used by Register / Associate /
// Unassociate.
template <typename Req>
void packAttributeRegions(Req& req, const AttributeRegionMap& map) {
  for (const auto& [attr, regions] : map) {
    auto* ar = req.add_attribute_regions();
    ar->set_attribute_handle(attr.raw());
    for (const auto& r : regions) ar->add_region_handles(r.raw());
  }
}

}  // namespace

RoutingSpaceHandle M17RTIambassador::getRoutingSpaceHandle(
    const std::string& name) {
  impl_->requireConnected();
  requireJoinedForDDM(impl_->joined, "getRoutingSpaceHandle");
  rti::v1::LookupRoutingSpaceRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_name(name);
  grpc::ClientContext ctx;
  rti::v1::LookupRoutingSpaceResponse resp;
  const auto s = impl_->ddm_stub->LookupRoutingSpace(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "getRoutingSpaceHandle");
  if (!resp.found()) {
    throw NameNotFound("getRoutingSpaceHandle: unknown routing space '" +
                       name + "'");
  }
  return RoutingSpaceHandle(resp.routing_space_handle());
}

DimensionHandle M17RTIambassador::getDimensionHandle(
    RoutingSpaceHandle routing_space,
    const std::string& name) {
  impl_->requireConnected();
  requireJoinedForDDM(impl_->joined, "getDimensionHandle");
  rti::v1::LookupDimensionRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_routing_space_handle(routing_space.raw());
  req.set_name(name);
  grpc::ClientContext ctx;
  rti::v1::LookupDimensionResponse resp;
  const auto s = impl_->ddm_stub->LookupDimension(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "getDimensionHandle");
  if (!resp.found()) {
    throw NameNotFound("getDimensionHandle: unknown dimension '" + name +
                       "' in routing space");
  }
  return DimensionHandle(resp.dimension_handle());
}

RegionHandle M17RTIambassador::createRegion(
    RoutingSpaceHandle routing_space,
    const std::vector<DimensionHandle>& dimensions) {
  impl_->requireConnected();
  requireJoinedForDDM(impl_->joined, "createRegion");
  rti::v1::CreateRegionRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_routing_space_handle(routing_space.raw());
  for (auto d : dimensions) req.add_dimension_handles(d.raw());
  grpc::ClientContext ctx;
  rti::v1::CreateRegionResponse resp;
  const auto s = impl_->ddm_stub->CreateRegion(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "createRegion");
  return RegionHandle(resp.region_handle());
}

void M17RTIambassador::setRangeBounds(RegionHandle region,
                                   DimensionHandle dimension,
                                   const DimensionRange& bounds) {
  impl_->requireConnected();
  requireJoinedForDDM(impl_->joined, "setRangeBounds");
  rti::v1::SetRangeBoundsRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_region_handle(region.raw());
  req.set_dimension_handle(dimension.raw());
  auto* r = req.mutable_bounds();
  r->set_lower(bounds.lower);
  r->set_upper(bounds.upper);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->ddm_stub->SetRangeBounds(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "setRangeBounds");
}

void M17RTIambassador::commitRegionModifications(
    const std::vector<RegionHandle>& regions) {
  impl_->requireConnected();
  requireJoinedForDDM(impl_->joined, "commitRegionModifications");
  rti::v1::CommitRegionRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  for (auto r : regions) req.add_region_handles(r.raw());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->ddm_stub->CommitRegionModifications(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "commitRegionModifications");
}

void M17RTIambassador::deleteRegion(RegionHandle region) {
  impl_->requireConnected();
  requireJoinedForDDM(impl_->joined, "deleteRegion");
  rti::v1::DeleteRegionRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_region_handle(region.raw());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->ddm_stub->DeleteRegion(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "deleteRegion");
}

M17RTIambassador::QueryBoundsResult RTIambassador::queryBounds(
    RegionHandle region, DimensionHandle dimension) {
  impl_->requireConnected();
  requireJoinedForDDM(impl_->joined, "queryBounds");
  rti::v1::QueryBoundsRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_region_handle(region.raw());
  req.set_dimension_handle(dimension.raw());
  grpc::ClientContext ctx;
  rti::v1::QueryBoundsResponse resp;
  const auto s = impl_->ddm_stub->QueryBounds(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "queryBounds");
  QueryBoundsResult out;
  out.found = resp.found();
  if (resp.has_bounds()) {
    out.bounds.lower = resp.bounds().lower();
    out.bounds.upper = resp.bounds().upper();
  } else {
    out.bounds = {0, 0};
  }
  return out;
}

void M17RTIambassador::subscribeObjectClassAttributesWithRegions(
    ObjectClassHandle object_class,
    const AttributeHandleSet& attributes,
    const RegionHandleSet& regions) {
  impl_->requireConnected();
  requireJoinedForDDM(impl_->joined,
                      "subscribeObjectClassAttributesWithRegions");
  rti::v1::SubscribeOCAWithRegionsRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_object_class_handle(object_class.raw());
  for (auto a : attributes) req.add_attribute_handles(a.raw());
  for (auto r : regions) req.add_region_handles(r.raw());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->ddm_stub->SubscribeObjectClassAttributesWithRegions(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "subscribeObjectClassAttributesWithRegions");
}

void M17RTIambassador::subscribeInteractionClassWithRegions(
    InteractionClassHandle interaction_class,
    const RegionHandleSet& regions) {
  impl_->requireConnected();
  requireJoinedForDDM(impl_->joined,
                      "subscribeInteractionClassWithRegions");
  rti::v1::SubscribeICWithRegionsRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_interaction_class_handle(interaction_class.raw());
  for (auto r : regions) req.add_region_handles(r.raw());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->ddm_stub->SubscribeInteractionClassWithRegions(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "subscribeInteractionClassWithRegions");
}

void M17RTIambassador::unsubscribeObjectClassAttributesWithRegions(
    ObjectClassHandle object_class,
    const AttributeHandleSet& attributes,
    const RegionHandleSet& regions) {
  impl_->requireConnected();
  requireJoinedForDDM(impl_->joined,
                      "unsubscribeObjectClassAttributesWithRegions");
  rti::v1::UnsubscribeOCAWithRegionsRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_object_class_handle(object_class.raw());
  for (auto a : attributes) req.add_attribute_handles(a.raw());
  for (auto r : regions) req.add_region_handles(r.raw());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->ddm_stub->UnsubscribeObjectClassAttributesWithRegions(&ctx, req, &resp);
  if (!s.ok())
    throwFromStatus(s, "unsubscribeObjectClassAttributesWithRegions");
}

void M17RTIambassador::unsubscribeInteractionClassWithRegions(
    InteractionClassHandle interaction_class,
    const RegionHandleSet& regions) {
  impl_->requireConnected();
  requireJoinedForDDM(impl_->joined,
                      "unsubscribeInteractionClassWithRegions");
  rti::v1::UnsubscribeICWithRegionsRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_interaction_class_handle(interaction_class.raw());
  for (auto r : regions) req.add_region_handles(r.raw());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->ddm_stub->UnsubscribeInteractionClassWithRegions(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "unsubscribeInteractionClassWithRegions");
}

M17RTIambassador::RegisterWithRegionsResult
M17RTIambassador::registerObjectInstanceWithRegions(
    ObjectClassHandle object_class,
    const AttributeRegionMap& attribute_regions,
    const std::string& object_name) {
  impl_->requireConnected();
  requireJoinedForDDM(impl_->joined, "registerObjectInstanceWithRegions");
  rti::v1::RegisterObjectWithRegionsRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_object_class_handle(object_class.raw());
  req.set_object_name(object_name);
  packAttributeRegions(req, attribute_regions);
  grpc::ClientContext ctx;
  rti::v1::RegisterObjectWithRegionsResponse resp;
  const auto s =
      impl_->ddm_stub->RegisterObjectInstanceWithRegions(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "registerObjectInstanceWithRegions");
  return RegisterWithRegionsResult{
      ObjectInstanceHandle(resp.object_handle()), resp.object_name()};
}

void M17RTIambassador::associateRegionsForUpdates(
    ObjectInstanceHandle object,
    const AttributeRegionMap& attribute_regions) {
  impl_->requireConnected();
  requireJoinedForDDM(impl_->joined, "associateRegionsForUpdates");
  rti::v1::AssociateRegionsForUpdatesRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_object_handle(object.raw());
  packAttributeRegions(req, attribute_regions);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->ddm_stub->AssociateRegionsForUpdates(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "associateRegionsForUpdates");
}

void M17RTIambassador::unassociateRegionsForUpdates(
    ObjectInstanceHandle object,
    const AttributeRegionMap& attribute_regions) {
  impl_->requireConnected();
  requireJoinedForDDM(impl_->joined, "unassociateRegionsForUpdates");
  rti::v1::UnassociateRegionsForUpdatesRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_object_handle(object.raw());
  packAttributeRegions(req, attribute_regions);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->ddm_stub->UnassociateRegionsForUpdates(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "unassociateRegionsForUpdates");
}

void M17RTIambassador::sendInteractionWithRegions(
    InteractionClassHandle interaction_class,
    const ParameterHandleValueMap& parameters,
    const RegionHandleSet& regions,
    std::optional<double> logical_time) {
  impl_->requireConnected();
  requireJoinedForDDM(impl_->joined, "sendInteractionWithRegions");
  rti::v1::SendInteractionWithRegionsRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_interaction_class_handle(interaction_class.raw());
  for (const auto& [ph, bytes] : parameters) {
    (*req.mutable_parameters())[ph.raw()] =
        std::string(bytes.begin(), bytes.end());
  }
  for (auto r : regions) req.add_region_handles(r.raw());
  if (logical_time.has_value()) req.set_logical_time(*logical_time);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->ddm_stub->SendInteractionWithRegions(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "sendInteractionWithRegions");
}

void M17RTIambassador::requestAttributeValueUpdateWithRegions(
    ObjectClassHandle object_class,
    const AttributeHandleSet& attributes,
    const RegionHandleSet& regions,
    const VariableLengthData& tag) {
  impl_->requireConnected();
  requireJoinedForDDM(impl_->joined,
                      "requestAttributeValueUpdateWithRegions");
  rti::v1::RequestAttributeValueUpdateWithRegionsRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_object_class_handle(object_class.raw());
  for (auto a : attributes) req.add_attribute_handles(a.raw());
  for (auto r : regions) req.add_region_handles(r.raw());
  req.set_user_supplied_tag(tag.data(), tag.size());
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->ddm_stub->RequestAttributeValueUpdateWithRegions(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "requestAttributeValueUpdateWithRegions");
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

void M17RTIambassador::publishObjectClassAttributes(
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

void M17RTIambassador::unpublishObjectClassAttributes(
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

void M17RTIambassador::subscribeObjectClassAttributes(
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

void M17RTIambassador::unsubscribeObjectClassAttributes(
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

void M17RTIambassador::publishInteractionClass(InteractionClassHandle cls) {
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

void M17RTIambassador::unpublishInteractionClass(InteractionClassHandle cls) {
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

void M17RTIambassador::subscribeInteractionClass(InteractionClassHandle cls) {
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

void M17RTIambassador::unsubscribeInteractionClass(InteractionClassHandle cls) {
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

ObjectInstanceHandle M17RTIambassador::registerObjectInstance(
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

void M17RTIambassador::updateAttributeValues(
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

void M17RTIambassador::sendInteraction(
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

// --- M36 Agent DA — §6 TSO variants + delete / request-update wire ---------

void M17RTIambassador::updateAttributeValues(
    ObjectInstanceHandle obj, const AttributeHandleValueMap& values,
    double logical_time) {
  requireJoined(*impl_, "updateAttributeValues");
  rti::v1::UpdateAttributeValuesRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_object_handle(obj.raw());
  auto* m = req.mutable_attributes();
  for (const auto& kv : values) {
    (*m)[kv.first.raw()] = std::string(kv.second.begin(), kv.second.end());
  }
  // Presence of logical_time flips the server's RO/TSO branch — see
  // UpdateAttributeValuesRequest.logical_time in proto/rti/v1/object.proto.
  req.set_logical_time(logical_time);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->object_stub->UpdateAttributeValues(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "updateAttributeValues");
}

void M17RTIambassador::sendInteraction(
    InteractionClassHandle cls, const ParameterHandleValueMap& parameters,
    double logical_time) {
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
  req.set_logical_time(logical_time);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->object_stub->SendInteraction(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "sendInteraction");
}

void M17RTIambassador::deleteObjectInstance(
    ObjectInstanceHandle obj, const VariableLengthData& tag,
    std::optional<double> logical_time) {
  requireJoined(*impl_, "deleteObjectInstance");
  rti::v1::DeleteObjectInstanceRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_object_handle(obj.raw());
  req.set_user_supplied_tag(std::string(tag.begin(), tag.end()));
  if (logical_time.has_value()) req.set_logical_time(*logical_time);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->object_stub->DeleteObjectInstance(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "deleteObjectInstance");
}

void M17RTIambassador::requestAttributeValueUpdate(
    ObjectInstanceHandle obj, const AttributeHandleSet& attributes,
    const VariableLengthData& tag) {
  requireJoined(*impl_, "requestAttributeValueUpdate");
  rti::v1::RequestAttributeValueUpdateRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_object_handle(obj.raw());
  for (const auto& a : attributes) req.add_attribute_handles(a.raw());
  req.set_user_supplied_tag(std::string(tag.begin(), tag.end()));
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->object_stub->RequestAttributeValueUpdate(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "requestAttributeValueUpdate");
}

void M17RTIambassador::requestAttributeValueUpdate(
    ObjectClassHandle cls, const AttributeHandleSet& attributes,
    const VariableLengthData& tag) {
  requireJoined(*impl_, "requestAttributeValueUpdate");
  rti::v1::RequestClassAttributeValueUpdateRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_object_class_handle(cls.raw());
  for (const auto& a : attributes) req.add_attribute_handles(a.raw());
  req.set_user_supplied_tag(std::string(tag.begin(), tag.end()));
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s =
      impl_->object_stub->RequestClassAttributeValueUpdate(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "requestAttributeValueUpdate(class)");
}

// --- M36 Agent DA — §10.24/§10.25 federate name<->handle -------------------

FederateHandle M17RTIambassador::getFederateHandle(
    const std::string& federate_name) {
  requireJoined(*impl_, "getFederateHandle");
  rti::v1::ListFederationMembersRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  grpc::ClientContext ctx;
  rti::v1::ListFederationMembersResponse resp;
  const auto s =
      impl_->federation_stub->ListFederationMembers(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "getFederateHandle");
  for (const auto& m : resp.members()) {
    if (m.federate_name() == federate_name) {
      return FederateHandle(m.federate_handle());
    }
  }
  throw NameNotFound("getFederateHandle: no joined federate named '" +
                     federate_name + "'");
}

std::string M17RTIambassador::getFederateName(FederateHandle handle) {
  requireJoined(*impl_, "getFederateName");
  rti::v1::ListFederationMembersRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  grpc::ClientContext ctx;
  rti::v1::ListFederationMembersResponse resp;
  const auto s =
      impl_->federation_stub->ListFederationMembers(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "getFederateName");
  for (const auto& m : resp.members()) {
    if (m.federate_handle() == handle.raw()) return m.federate_name();
  }
  throw NameNotFound("getFederateName: no joined federate with handle " +
                     std::to_string(handle.raw()));
}

// --- M20.2 §8.21 retract ---------------------------------------------------

void M17RTIambassador::retract(MessageRetractionHandle handle) {
  requireJoined(*impl_, "retract");
  rti::v1::RetractRequest req;
  req.set_wire_version(rti::v1::WIRE_VERSION_V1);
  req.set_federation_name(impl_->joined_federation);
  req.set_federate_handle(impl_->federate_handle.raw());
  req.set_message_retraction_handle(handle);
  grpc::ClientContext ctx;
  rti::v1::Empty resp;
  const auto s = impl_->object_stub->Retract(&ctx, req, &resp);
  if (!s.ok()) throwFromStatus(s, "retract");
}

// --- M17.6 §10.4 tickCallback + FederateAmbassador ------------------------

void M17RTIambassador::setFederateAmbassador(FederateAmbassador* fed) {
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

bool M17RTIambassador::tickCallback(double approx_min_time,
                                 double approx_max_time) {
  using clock = std::chrono::steady_clock;
  using std::chrono::duration;
  using std::chrono::duration_cast;
  using std::chrono::milliseconds;

  // M17.18 — when callbacks are disabled, drain nothing. Events
  // keep buffering in event_queue; enableCallbacks() resumes.
  if (!impl_->callbacks_enabled.load()) return false;
  if (approx_max_time < approx_min_time) approx_max_time = approx_min_time;
  const auto start = clock::now();
  const auto min_deadline =
      start + duration_cast<clock::duration>(duration<double>(approx_min_time));
  const auto max_deadline =
      start + duration_cast<clock::duration>(duration<double>(approx_max_time));

  bool any_fired = false;

  // Honor the min/max window. Sleep in small increments so a callback
  // arriving early can still fire promptly.
  while (clock::now() < min_deadline) {
    if (impl_->dispatchOneEvent()) any_fired = true;
    if (clock::now() < min_deadline) {
      std::unique_lock<std::mutex> lk(impl_->event_mu);
      impl_->event_cv.wait_for(lk, milliseconds(5),
                               [&] { return !impl_->event_queue.empty(); });
    }
  }
  // Drain anything else queued.
  while (impl_->dispatchOneEvent()) any_fired = true;
  // If max_time > min_time and nothing has fired yet, wait until
  // either an event arrives or the max deadline elapses.
  while (!any_fired && clock::now() < max_deadline) {
    std::unique_lock<std::mutex> lk(impl_->event_mu);
    impl_->event_cv.wait_for(lk, milliseconds(5),
                             [&] { return !impl_->event_queue.empty(); });
    lk.unlock();
    if (impl_->dispatchOneEvent()) any_fired = true;
  }

  return any_fired;
}

// M17.21 — single-event dispatch helper. Shared by tickCallback's
// drain loop and (M17.22) the strict at-most-one evokeCallback.
bool RTIambassadorImpl::dispatchOneEvent() {
  rti::v1::FederateEvent evt;
  {
    std::unique_lock<std::mutex> lk(event_mu);
    if (event_queue.empty()) return false;
    evt = std::move(event_queue.front());
    event_queue.pop_front();
  }
  if (fed_ambassador == nullptr) {
    // No ambassador bound — drop. The cppsdk doesn't buffer events
    // for a late-bound callback target.
    return true;
  }
  switch (evt.event_case()) {
    case rti::v1::FederateEvent::kDiscover: {
      const auto& d = evt.discover();
      fed_ambassador->discoverObjectInstance(
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
      fed_ambassador->reflectAttributeValues(
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
      fed_ambassador->receiveInteraction(
          InteractionClassHandle(i.interaction_class_handle()), params, ts);
      return true;
    }
    case rti::v1::FederateEvent::kReservationSucceeded:
      fed_ambassador->objectInstanceNameReservationSucceeded(
          evt.reservation_succeeded().object_name());
      return true;
    case rti::v1::FederateEvent::kReservationFailed:
      fed_ambassador->objectInstanceNameReservationFailed(
          evt.reservation_failed().object_name());
      return true;
    case rti::v1::FederateEvent::kReservationMultiSucceeded: {
      const auto& m = evt.reservation_multi_succeeded();
      std::vector<std::string> names(m.object_names().begin(),
                                     m.object_names().end());
      fed_ambassador->multipleObjectInstanceNameReservationSucceeded(names);
      return true;
    }
    case rti::v1::FederateEvent::kReservationMultiFailed: {
      const auto& m = evt.reservation_multi_failed();
      std::vector<std::string> req_names(m.requested_names().begin(),
                                         m.requested_names().end());
      std::vector<std::string> col_names(m.colliding_names().begin(),
                                         m.colliding_names().end());
      fed_ambassador->multipleObjectInstanceNameReservationFailed(
          req_names, col_names);
      return true;
    }
    case rti::v1::FederateEvent::kGrant: {
      const auto& g = evt.grant();
      fed_ambassador->timeAdvanceGrant(g.logical_time());
      return true;
    }
    case rti::v1::FederateEvent::kSyncAnnounced: {
      const auto& a = evt.sync_announced();
      VariableLengthData tag(a.tag().begin(), a.tag().end());
      fed_ambassador->announceSynchronizationPoint(a.label(), tag);
      return true;
    }
    case rti::v1::FederateEvent::kSyncSynchronized:
      fed_ambassador->federationSynchronized(
          evt.sync_synchronized().label());
      return true;
    case rti::v1::FederateEvent::kOwnershipAssumption: {
      const auto& a = evt.ownership_assumption();
      AttributeHandleSet attrs;
      for (auto h : a.attribute_handles()) attrs.emplace(h);
      VariableLengthData tag(a.tag().begin(), a.tag().end());
      fed_ambassador->requestAttributeOwnershipAssumption(
          ObjectInstanceHandle(a.object_handle()),
          attrs,
          FederateHandle(a.divesting_federate()),
          tag);
      return true;
    }
    case rti::v1::FederateEvent::kOwnershipAcquired: {
      const auto& a = evt.ownership_acquired();
      AttributeHandleSet attrs;
      for (auto h : a.attribute_handles()) attrs.emplace(h);
      fed_ambassador->attributeOwnershipAcquisitionNotification(
          ObjectInstanceHandle(a.object_handle()),
          attrs,
          FederateHandle(a.owning_federate()));
      return true;
    }
    case rti::v1::FederateEvent::kOwnershipDivestConfirmed: {
      const auto& a = evt.ownership_divest_confirmed();
      AttributeHandleSet attrs;
      for (auto h : a.attribute_handles()) attrs.emplace(h);
      fed_ambassador->requestDivestitureConfirmation(
          ObjectInstanceHandle(a.object_handle()), attrs);
      return true;
    }
    case rti::v1::FederateEvent::kSaveInitiate: {
      const auto& s = evt.save_initiate();
      std::optional<double> save_time =
          s.has_save_time() ? std::optional<double>(s.save_time())
                            : std::nullopt;
      fed_ambassador->initiateFederateSave(s.label(), save_time);
      return true;
    }
    case rti::v1::FederateEvent::kSaveCompleted:
      fed_ambassador->federationSaved(evt.save_completed().label());
      return true;
    case rti::v1::FederateEvent::kSaveFailed:
      fed_ambassador->federationNotSaved(evt.save_failed().label());
      return true;
    case rti::v1::FederateEvent::kRestoreInitiate: {
      const auto& r = evt.restore_initiate();
      fed_ambassador->initiateFederateRestore(
          r.label(), FederateHandle(r.federate_handle()));
      return true;
    }
    case rti::v1::FederateEvent::kRestoreCompleted:
      fed_ambassador->federationRestored(evt.restore_completed().label());
      return true;
    case rti::v1::FederateEvent::kRestoreFailed:
      fed_ambassador->federationNotRestored(evt.restore_failed().label());
      return true;
    case rti::v1::FederateEvent::kRemove: {
      // §6.15 — M36 Agent DA. Timestamp presence selects RO vs TSO at
      // the DLC bridge layer.
      const auto& r = evt.remove();
      std::optional<double> ts =
          r.has_logical_time() ? std::optional<double>(r.logical_time())
                               : std::nullopt;
      VariableLengthData tag(r.user_supplied_tag().begin(),
                             r.user_supplied_tag().end());
      fed_ambassador->removeObjectInstance(
          ObjectInstanceHandle(r.object_handle()), ts, tag);
      return true;
    }
    case rti::v1::FederateEvent::kProvideUpdate: {
      // §6.20 — M36 Agent DA.
      const auto& p = evt.provide_update();
      AttributeHandleSet attrs;
      for (auto h : p.attribute_handles()) attrs.emplace(h);
      VariableLengthData tag(p.user_supplied_tag().begin(),
                             p.user_supplied_tag().end());
      fed_ambassador->provideAttributeValueUpdate(
          ObjectInstanceHandle(p.object_handle()), attrs, tag);
      return true;
    }
    default:
      // Unsupported events drop silently. Cut-4+ adds remaining
      // slots (halted, etc.).
      return true;
  }
}

// --- M17.18 / M17.22 §10.4 HLA_EVOKED + callback toggle ------------------
//
// evokeMultipleCallbacks: same as tickCallback. Drains the
// entire buffered queue up to approx_min_time, then waits up to
// approx_max_time for an additional event if none fired yet.
//
// evokeCallback: strict at-most-one (M17.22). Waits up to
// approx_max_time for at least one event AND for approx_min_time
// to elapse, then dispatches EXACTLY ONE event. Returns true if
// a callback fired AND more events remain queued — federate uses
// this to loop with explicit per-event control.
//
// disableCallbacks/enableCallbacks toggle the dispatch gate.
// Background reader keeps filling the event queue when disabled;
// no events are lost across the toggle.

bool M17RTIambassador::evokeMultipleCallbacks(double approx_min_time,
                                           double approx_max_time) {
  return tickCallback(approx_min_time, approx_max_time);
}

bool M17RTIambassador::evokeCallback(double approx_min_time,
                                  double approx_max_time) {
  using clock = std::chrono::steady_clock;
  using std::chrono::duration;
  using std::chrono::duration_cast;
  using std::chrono::milliseconds;

  if (!impl_->callbacks_enabled.load()) return false;
  if (approx_max_time < approx_min_time) approx_max_time = approx_min_time;
  const auto start = clock::now();
  const auto min_deadline =
      start + duration_cast<clock::duration>(duration<double>(approx_min_time));
  const auto max_deadline =
      start + duration_cast<clock::duration>(duration<double>(approx_max_time));

  // Wait for at least one event OR for the max deadline.
  while (true) {
    {
      std::unique_lock<std::mutex> lk(impl_->event_mu);
      if (!impl_->event_queue.empty()) break;
    }
    if (clock::now() >= max_deadline) return false;
    std::unique_lock<std::mutex> lk(impl_->event_mu);
    impl_->event_cv.wait_for(lk, milliseconds(5),
                             [&] { return !impl_->event_queue.empty(); });
  }
  // Hold off dispatch until min_deadline elapses (Pitch contract:
  // evokeCallback blocks at least min_time before returning).
  while (clock::now() < min_deadline) {
    std::this_thread::sleep_for(milliseconds(1));
  }
  // Dispatch exactly one event.
  const bool fired = impl_->dispatchOneEvent();
  // Return true iff a callback fired AND more events remain — the
  // signal Pitch federates use to keep looping.
  if (!fired) return false;
  std::lock_guard<std::mutex> g(impl_->event_mu);
  return !impl_->event_queue.empty();
}

void M17RTIambassador::disableCallbacks() {
  impl_->callbacks_enabled.store(false);
}

void M17RTIambassador::enableCallbacks() {
  impl_->callbacks_enabled.store(true);
  // Wake any thread waiting in tickCallback so it re-checks the
  // gate. (Pre-existing tickCallback paths poll with a 5 ms
  // wait_for timeout, so this notify is a latency optimization.)
  impl_->event_cv.notify_all();
}

}  // namespace rti1516e

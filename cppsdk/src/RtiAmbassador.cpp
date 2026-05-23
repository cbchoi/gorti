// rti1516e::RTIambassador impl — M17 Cut-1.
//
// The pimpl owns:
//   - the gRPC channel to rtid (lazy by default)
//   - service stubs (cluster + admin) instantiated on first use
//   - federate-side bookkeeping (federation name, federate handle,
//     handle caches) — fields added in later M17 milestones
//
// The header keeps this hidden so federate users never transitively
// pick up grpc++ / protobuf headers from `#include <rti1516e/RtiAmbassador.h>`.

#include "rti1516e/RtiAmbassador.h"

#include <grpcpp/grpcpp.h>

#include <memory>
#include <string>
#include <string_view>

#include "rti1516e/Exceptions.h"

namespace rti1516e {

class RTIambassadorImpl {
 public:
  std::shared_ptr<grpc::Channel> channel;
  std::string url;
  bool connected = false;
};

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

}  // namespace

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
  // CreateChannel doesn't block; an actual connection attempt happens
  // on the first RPC. This means connect() returns success even if
  // the rtid is down — the first lifecycle call (e.g.
  // createFederationExecution) will surface that as the appropriate
  // status.
  impl_->channel = grpc::CreateChannel(target, grpc::InsecureChannelCredentials());
  impl_->url = url;
  impl_->connected = true;
}

void RTIambassador::disconnect() {
  // Channel shutdown is implicit — releasing the shared_ptr lets
  // the gRPC core close the channel once outstanding RPCs (if any)
  // finish. Subsequent calls hit the connected==false guard.
  impl_->channel.reset();
  impl_->url.clear();
  impl_->connected = false;
}

bool RTIambassador::isConnected() const noexcept {
  return impl_->connected;
}

}  // namespace rti1516e

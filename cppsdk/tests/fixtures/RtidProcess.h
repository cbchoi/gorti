// RAII helper that spawns rtid on a free port for integration tests.
//
// Usage:
//   TEST(SomeFixture, RoundTrip) {
//     RtidProcess rtid;
//     rti1516e::RTIambassador amb;
//     amb.connect(rtid.url());
//     ...
//   }  // rtid SIGTERM'd here
//
// The fixture spawns rtid as a child process, waits for the gRPC
// port to accept connections (up to 10 s), and tears it down with
// SIGTERM on destruction (SIGKILL if it doesn't exit in 5 s).
//
// Port allocation: bind a SO_REUSEADDR socket to port 0, read back
// the kernel-assigned port, close the socket, hand the port to
// rtid. Same race-window-acceptable approach the Python helpers
// use. CI parallel runs may collide but the rate is low.
//
// rtid binary location: REPO_ROOT/bin/rtid. The test runner builds
// this via `go build` in a pre-test hook, or the user runs
// `make build` before invoking ctest.

#pragma once

#include <fcntl.h>
#include <netinet/in.h>
#include <signal.h>
#include <sys/socket.h>
#include <sys/wait.h>
#include <unistd.h>

#include <chrono>
#include <cstdlib>
#include <fstream>
#include <stdexcept>
#include <string>
#include <thread>

#include <grpcpp/grpcpp.h>

namespace rti1516e_test {

class RtidProcess {
 public:
  RtidProcess() {
    listen_port_ = pickFreePort();
    metrics_port_ = pickFreePort();
    if (metrics_port_ == listen_port_) metrics_port_ = pickFreePort();
    admin_port_ = pickFreePort();
    if (admin_port_ == listen_port_ || admin_port_ == metrics_port_) {
      admin_port_ = pickFreePort();
    }
    const auto bin = resolveRtidBinary();
    pid_ = fork();
    if (pid_ < 0) {
      throw std::runtime_error("RtidProcess: fork failed");
    }
    if (pid_ == 0) {
      // Child — run rtid. Stdout/stderr go to /dev/null so the test
      // output stays clean; a future cut can tee to a log file when
      // debugging.
      const int devnull = open("/dev/null", 1);
      if (devnull >= 0) {
        dup2(devnull, 1);
        dup2(devnull, 2);
      }
      const auto listen = ":" + std::to_string(listen_port_);
      const auto metrics = ":" + std::to_string(metrics_port_);
      const auto admin = "127.0.0.1:" + std::to_string(admin_port_);
      execl(bin.c_str(), bin.c_str(),
            "--listen", listen.c_str(),
            "--metrics-listen", metrics.c_str(),
            "--admin-listen", admin.c_str(),
            "--log-level", "warn",
            static_cast<char*>(nullptr));
      _exit(127);
    }
    waitForGrpc();
  }

  ~RtidProcess() {
    if (pid_ <= 0) return;
    kill(pid_, SIGTERM);
    for (int i = 0; i < 50; ++i) {
      int status = 0;
      const auto w = waitpid(pid_, &status, WNOHANG);
      if (w == pid_) {
        pid_ = -1;
        return;
      }
      std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
    kill(pid_, SIGKILL);
    int status = 0;
    waitpid(pid_, &status, 0);
    pid_ = -1;
  }

  RtidProcess(const RtidProcess&) = delete;
  RtidProcess& operator=(const RtidProcess&) = delete;

  std::string url() const {
    return "grpc://127.0.0.1:" + std::to_string(listen_port_);
  }

  int port() const { return listen_port_; }

 private:
  static int pickFreePort() {
    const int s = socket(AF_INET, SOCK_STREAM, 0);
    if (s < 0) throw std::runtime_error("RtidProcess: socket failed");
    sockaddr_in addr{};
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
    addr.sin_port = 0;
    if (bind(s, reinterpret_cast<sockaddr*>(&addr), sizeof(addr)) < 0) {
      close(s);
      throw std::runtime_error("RtidProcess: bind failed");
    }
    socklen_t len = sizeof(addr);
    if (getsockname(s, reinterpret_cast<sockaddr*>(&addr), &len) < 0) {
      close(s);
      throw std::runtime_error("RtidProcess: getsockname failed");
    }
    close(s);
    return ntohs(addr.sin_port);
  }

  // Resolve the rtid binary. Allows override via env var so CI can
  // point at a prebuilt artifact; otherwise we assume the standard
  // repo layout (bin/rtid relative to the build dir's parent).
  std::string resolveRtidBinary() {
    if (const char* env = std::getenv("RTID_BINARY")) {
      return std::string(env);
    }
    // CMake's CMAKE_SOURCE_DIR sets RTID_REPO_ROOT at test compile
    // time; pre-set via add_definitions in tests/CMakeLists.txt.
#ifdef RTID_REPO_ROOT
    return std::string(RTID_REPO_ROOT) + "/bin/rtid";
#else
    return "../../bin/rtid";
#endif
  }

  void waitForGrpc() {
    using namespace std::chrono;
    const auto deadline = steady_clock::now() + seconds(10);
    while (steady_clock::now() < deadline) {
      // Cheap TCP probe — open a socket, see if connect succeeds.
      const int s = socket(AF_INET, SOCK_STREAM, 0);
      if (s < 0) {
        std::this_thread::sleep_for(milliseconds(100));
        continue;
      }
      sockaddr_in a{};
      a.sin_family = AF_INET;
      a.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
      a.sin_port = htons(static_cast<uint16_t>(listen_port_));
      const auto rc =
          connect(s, reinterpret_cast<sockaddr*>(&a), sizeof(a));
      close(s);
      if (rc == 0) return;
      std::this_thread::sleep_for(milliseconds(100));
    }
    throw std::runtime_error(
        "RtidProcess: rtid did not accept connections on port " +
        std::to_string(listen_port_) + " within 10s");
  }

  pid_t pid_ = -1;
  int listen_port_ = 0;
  int metrics_port_ = 0;
  int admin_port_ = 0;
};

}  // namespace rti1516e_test

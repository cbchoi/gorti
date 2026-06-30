// _harness/rtid_runner.h — RAII wrapper that starts `bin/rtid` on a free port
// for the duration of a conformance test and tears it down in the dtor.
//
// M31 status: header-only utility. The actual rtid binary is gorti's
// existing M17 daemon (`./bin/rtid`); fixtures connect to it via
// `crcAddress=127.0.0.1:<port>` per IEEE 1516.1-2010 §10.6.1 connect()
// localSettings convention.
//
// Used by every conformance fixture's test_<name>.cpp gtest driver.
//
// Per docs/M31_DISPATCH_PLAN.md §2.2 (TASK-348) the harness is shared
// utility code; no impl needed beyond the RAII wrapper below.

#pragma once

#include <cerrno>
#include <csignal>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <netinet/in.h>
#include <stdexcept>
#include <string>
#include <sys/socket.h>
#include <sys/wait.h>
#include <unistd.h>

namespace gorti_dlc_harness {

// Pick a free TCP port by binding to :0 and reading back the chosen port.
inline int pickFreePort() {
  int s = ::socket(AF_INET, SOCK_STREAM, 0);
  if (s < 0) throw std::runtime_error("socket() failed");
  sockaddr_in addr{};
  addr.sin_family = AF_INET;
  addr.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
  addr.sin_port = 0;
  if (::bind(s, reinterpret_cast<sockaddr*>(&addr), sizeof(addr)) < 0) {
    ::close(s);
    throw std::runtime_error("bind(:0) failed");
  }
  socklen_t len = sizeof(addr);
  if (::getsockname(s, reinterpret_cast<sockaddr*>(&addr), &len) < 0) {
    ::close(s);
    throw std::runtime_error("getsockname() failed");
  }
  int port = ntohs(addr.sin_port);
  ::close(s);
  return port;
}

class RtidRunner {
 public:
  // `rtid_binary` defaults to ./bin/rtid (relative to test cwd).
  // `log_path` is where rtid's stdout/stderr is tee'd to.
  explicit RtidRunner(const std::string& log_path = "/tmp/gorti_dlc_rtid.log",
                      const std::string& rtid_binary = "./bin/rtid")
      : port_(pickFreePort()), log_path_(log_path), rtid_binary_(rtid_binary) {
    pid_ = ::fork();
    if (pid_ < 0) throw std::runtime_error("fork() failed");
    if (pid_ == 0) {
      // Child: redirect stdout+stderr to log_path, exec rtid.
      ::freopen(log_path_.c_str(), "w", stdout);
      ::freopen(log_path_.c_str(), "a", stderr);
      const std::string port_str = std::to_string(port_);
      ::execl(rtid_binary_.c_str(), rtid_binary_.c_str(),
              "--listen", ("127.0.0.1:" + port_str).c_str(),
              static_cast<char*>(nullptr));
      // exec failed
      std::fprintf(stderr, "exec %s: %s\n", rtid_binary_.c_str(),
                   std::strerror(errno));
      ::_exit(127);
    }
    // Parent: give rtid a moment to come up. M31 stub — production version
    // would poll the port until accept() succeeds.
    ::usleep(300 * 1000);
  }

  ~RtidRunner() {
    if (pid_ > 0) {
      ::kill(pid_, SIGTERM);
      int status = 0;
      ::waitpid(pid_, &status, 0);
    }
  }

  // crcAddress= form per DLC spec §10.6.1 connect() localSettings convention.
  std::string crcAddress() const {
    return "crcAddress=127.0.0.1:" + std::to_string(port_);
  }

  int port() const { return port_; }
  const std::string& logPath() const { return log_path_; }

  RtidRunner(const RtidRunner&) = delete;
  RtidRunner& operator=(const RtidRunner&) = delete;

 private:
  pid_t pid_ = -1;
  int port_ = 0;
  std::string log_path_;
  std::string rtid_binary_;
};

}  // namespace gorti_dlc_harness

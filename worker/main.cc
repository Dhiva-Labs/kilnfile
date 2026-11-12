// kilnfile-worker: reads one framed request on stdin, writes one framed
// response on stdout. Locks itself down before reading any input.
#include <unistd.h>

#include <cerrno>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <exception>
#include <memory>
#include <new>
#include <string>

#include "engine.h"
#include "protocol.h"
#include "sandbox.h"

namespace {

constexpr int kExitUsage = 64;
constexpr int kExitFailure = 1;

[[noreturn]] void Die(int code, const std::string& msg) {
  std::string line = "kilnfile-worker: " + msg + "\n";
  ssize_t ignored = ::write(STDERR_FILENO, line.data(), line.size());
  (void)ignored;
  _exit(code);
}

bool ParseU64(const char* s, uint64_t* out) {
  if (!s || !*s || *s == '-') return false;
  errno = 0;
  char* end = nullptr;
  unsigned long long v = std::strtoull(s, &end, 10);
  if (errno != 0 || *end != '\0') return false;
  *out = v;
  return true;
}

struct Flags {
  bool have_protocol = false;
  std::string engine = "echo";
  bool no_sandbox = false;
  bool sandbox_log = false;
  uint64_t max_payload = kilnfile::kDefaultMaxPayloadBytes;
};

Flags ParseFlags(int argc, char** argv) {
  Flags f;
  for (int i = 1; i < argc; i++) {
    std::string a = argv[i];
    auto value = [&](const char* name) -> const char* {
      if (i + 1 >= argc) Die(kExitUsage, std::string(name) + " needs a value");
      return argv[++i];
    };
    if (a == "--protocol") {
      uint64_t v;
      if (!ParseU64(value("--protocol"), &v) || v != 1)
        Die(kExitUsage, "unsupported protocol version (only 1)");
      f.have_protocol = true;
    } else if (a == "--engine") {
      f.engine = value("--engine");
    } else if (a == "--no-sandbox") {
      f.no_sandbox = true;
    } else if (a == "--sandbox-log") {
      f.sandbox_log = true;
    } else if (a == "--max-payload-bytes") {
      if (!ParseU64(value("--max-payload-bytes"), &f.max_payload))
        Die(kExitUsage, "bad --max-payload-bytes");
    } else {
      Die(kExitUsage, "unknown flag: " + a);
    }
  }
  if (!f.have_protocol) Die(kExitUsage, "--protocol 1 is required");
  return f;
}

}  // namespace

int main(int argc, char** argv) {
  Flags flags = ParseFlags(argc, argv);

  std::unique_ptr<kilnfile::Engine> engine;
  if (flags.engine == "echo") {
    engine = std::make_unique<kilnfile::EchoEngine>();
  } else {
    Die(kExitUsage, "unknown engine: " + flags.engine);
  }

  try {
    engine->Warmup();  // before the sandbox: files, lazy init

    if (flags.no_sandbox) {
      std::fputs(
          "kilnfile-worker: WARNING: --no-sandbox is set. The worker is NOT "
          "isolated. Debug use only; never run untrusted files like this.\n",
          stderr);
    } else {
      kilnfile::SandboxOptions opts;
      opts.seccomp_log_only = flags.sandbox_log;
      kilnfile::SandboxStatus st = kilnfile::EnterSandbox(opts);
      if (!st.supported) {
        std::fputs(
            "kilnfile-worker: WARNING: no sandbox on this platform; "
            "running unsandboxed.\n",
            stderr);
      } else if (!st.ok) {
        Die(kExitFailure, st.Describe());
      } else if (flags.sandbox_log) {
        std::fprintf(stderr, "kilnfile-worker: %s\n", st.Describe().c_str());
      }
    }

    std::string err;
    kilnfile::Request req;
    if (!kilnfile::ReadRequest(STDIN_FILENO, flags.max_payload, &req, &err))
      Die(kExitFailure, err);

    kilnfile::EngineResult res = engine->Process(req.header_json, req.payload);

    if (!kilnfile::WriteResponse(STDOUT_FILENO, res.report_json, res.output,
                                 &err))
      Die(kExitFailure, err);
  } catch (const std::bad_alloc&) {
    Die(kExitFailure, "out of memory");
  } catch (const std::exception& e) {
    Die(kExitFailure, std::string("exception: ") + e.what());
  }
  return 0;
}

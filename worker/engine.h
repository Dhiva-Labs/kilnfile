// Pluggable engine interface. The real engine (C++ over Rust parsers) comes
// from another track; EchoEngine exists for tests.
#ifndef KILNFILE_WORKER_ENGINE_H_
#define KILNFILE_WORKER_ENGINE_H_

#include <string>
#include <string_view>

namespace kilnfile {

struct EngineResult {
  std::string report_json;
  std::string output;
};

class Engine {
 public:
  virtual ~Engine();
  // Runs BEFORE the sandbox is entered. Do anything that needs files, dlopen,
  // lazy initialization or extra threads here.
  virtual void Warmup() {}
  // Runs inside the sandbox: no files, no network, no new processes.
  virtual EngineResult Process(std::string_view header_json,
                               std::string_view payload) = 0;
};

// Returns the payload unchanged. The sha256 fields are empty for now.
class EchoEngine : public Engine {
 public:
  EngineResult Process(std::string_view header_json,
                       std::string_view payload) override;
};

}  // namespace kilnfile

#endif  // KILNFILE_WORKER_ENGINE_H_

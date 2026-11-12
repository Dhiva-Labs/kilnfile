#include "engine.h"

namespace kilnfile {

Engine::~Engine() = default;

EngineResult EchoEngine::Process(std::string_view /*header_json*/,
                                 std::string_view payload) {
  const std::string n = std::to_string(payload.size());
  EngineResult r;
  r.output.assign(payload);
  r.report_json =
      "{\"kilnfile_version\":\"0.1.0\",\"result\":\"unchanged_semantics\","
      "\"reason\":\"\","
      "\"input\":{\"detected_type\":\"unknown\",\"bytes\":" + n +
      ",\"sha256\":\"\"},"
      "\"output\":{\"type\":\"unknown\",\"bytes\":" + n +
      ",\"sha256\":\"\"},"
      "\"actions\":[],\"limits_hit\":[]}";
  return r;
}

}  // namespace kilnfile

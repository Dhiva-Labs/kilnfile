#include "bridge/kilnfile_bridge.h"

#include <cstdint>
#include <string>
#include <utility>

#include "crubit/kilnfile_rs.h"
#include "absl/status/status.h"
#include "absl/status/statusor.h"
#include "absl/types/span.h"
#include "support/rs_std/result.h"

namespace kilnfile::bridge {

namespace rs = ::kilnfile_rs::kilnfile_bridge;

namespace {
std::string BytesToString(const rs::PngBytes& b) {
  return std::string(reinterpret_cast<const char*>(b.as_ptr()), b.len());
}
}  // namespace

absl::StatusOr<std::string> EncodePngRgba8(uint32_t width, uint32_t height,
                                           absl::Span<const uint8_t> rgba) {
  auto result = rs::encode_png_rgba8(width, height, rgba);
  if (!result.has_value()) {
    rs::BridgeError err = std::move(result).err();
    std::string msg = BytesToString(err.message);
    switch (err.code.tag) {
      case rs::BridgeErrorCode::Tag::InvalidArgument:
        return absl::InvalidArgumentError(msg);
      case rs::BridgeErrorCode::Tag::Internal:
        return absl::InternalError(msg);
    }
    return absl::InternalError(msg);
  }
  return BytesToString(std::move(result).value());
}

}  // namespace kilnfile::bridge

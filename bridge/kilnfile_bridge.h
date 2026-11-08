#ifndef KILNFILE_BRIDGE_KILNFILE_BRIDGE_H_
#define KILNFILE_BRIDGE_KILNFILE_BRIDGE_H_

#include <cstdint>
#include <string>

#include "absl/status/statusor.h"
#include "absl/types/span.h"

namespace kilnfile::bridge {

// Encodes an RGBA8 image (`rgba.size() == width * height * 4`, row-major) as
// PNG, using the Rust `image` crate through Crubit.
absl::StatusOr<std::string> EncodePngRgba8(uint32_t width, uint32_t height,
                                           absl::Span<const uint8_t> rgba);

}  // namespace kilnfile::bridge

#endif  // KILNFILE_BRIDGE_KILNFILE_BRIDGE_H_

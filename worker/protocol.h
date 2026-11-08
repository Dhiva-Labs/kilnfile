// Framing for the kilnfile worker protocol (docs/PROTOCOL.md).
//
//   request  = u32 header_len | header | u64 payload_len | payload
//   response = u32 report_len | report | u64 output_len  | output
//
// All integers are little-endian. Header and report are opaque JSON strings
// here; this layer never parses them.
#ifndef KILNFILE_WORKER_PROTOCOL_H_
#define KILNFILE_WORKER_PROTOCOL_H_

#include <cstdint>
#include <string>
#include <string_view>

namespace kilnfile {

inline constexpr uint32_t kMaxHeaderBytes = 1u << 20;   // 1 MiB
inline constexpr uint32_t kMaxReportBytes = 1u << 20;   // 1 MiB
inline constexpr uint64_t kDefaultMaxPayloadBytes = 256ull << 20;  // 256 MiB

struct Request {
  std::string header_json;
  std::string payload;
};

// Reads exactly `len` bytes, retrying on EINTR and partial reads. Returns
// false on EOF before `len` bytes (short read) or on error; `err` is set.
bool ReadFull(int fd, void* buf, size_t len, std::string* err);

// Writes exactly `len` bytes, retrying on EINTR and partial writes.
bool WriteFull(int fd, const void* buf, size_t len, std::string* err);

// Reads one request. The payload length is checked against `max_payload_bytes`
// before any payload byte is read or allocated. On failure returns false and
// sets `err`; nothing is written anywhere.
bool ReadRequest(int fd, uint64_t max_payload_bytes, Request* out,
                 std::string* err);

// Writes one response. Validates the report size first and writes nothing if
// it is over kMaxReportBytes.
bool WriteResponse(int fd, std::string_view report_json,
                   std::string_view output, std::string* err);

}  // namespace kilnfile

#endif  // KILNFILE_WORKER_PROTOCOL_H_

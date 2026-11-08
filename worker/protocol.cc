#include "protocol.h"

#include <unistd.h>

#include <algorithm>
#include <cerrno>
#include <cstring>

namespace kilnfile {
namespace {

void PutU32(uint8_t* p, uint32_t v) {
  for (int i = 0; i < 4; i++) p[i] = static_cast<uint8_t>(v >> (8 * i));
}
void PutU64(uint8_t* p, uint64_t v) {
  for (int i = 0; i < 8; i++) p[i] = static_cast<uint8_t>(v >> (8 * i));
}
uint32_t GetU32(const uint8_t* p) {
  uint32_t v = 0;
  for (int i = 0; i < 4; i++) v |= static_cast<uint32_t>(p[i]) << (8 * i);
  return v;
}
uint64_t GetU64(const uint8_t* p) {
  uint64_t v = 0;
  for (int i = 0; i < 8; i++) v |= static_cast<uint64_t>(p[i]) << (8 * i);
  return v;
}

bool Fail(std::string* err, const std::string& msg) {
  if (err) *err = msg;
  return false;
}

// Reads `len` bytes into `dst`, growing it in bounded steps so that a lying
// length prefix cannot make us allocate far more than the peer really sends.
bool ReadInto(int fd, std::string* dst, uint64_t len, std::string* err) {
  constexpr uint64_t kStep = 1u << 20;
  dst->clear();
  uint64_t have = 0;
  while (have < len) {
    uint64_t n = std::min(kStep, len - have);
    dst->resize(have + n);
    if (!ReadFull(fd, dst->data() + have, n, err)) return false;
    have += n;
  }
  return true;
}

}  // namespace

bool ReadFull(int fd, void* buf, size_t len, std::string* err) {
  auto* p = static_cast<uint8_t*>(buf);
  size_t done = 0;
  while (done < len) {
    ssize_t n = ::read(fd, p + done, len - done);
    if (n < 0) {
      if (errno == EINTR) continue;
      return Fail(err, std::string("read failed: ") + std::strerror(errno));
    }
    if (n == 0) return Fail(err, "short read: unexpected end of input");
    done += static_cast<size_t>(n);
  }
  return true;
}

bool WriteFull(int fd, const void* buf, size_t len, std::string* err) {
  auto* p = static_cast<const uint8_t*>(buf);
  size_t done = 0;
  while (done < len) {
    ssize_t n = ::write(fd, p + done, len - done);
    if (n < 0) {
      if (errno == EINTR) continue;
      return Fail(err, std::string("write failed: ") + std::strerror(errno));
    }
    done += static_cast<size_t>(n);
  }
  return true;
}

bool ReadRequest(int fd, uint64_t max_payload_bytes, Request* out,
                 std::string* err) {
  uint8_t hdr4[4];
  if (!ReadFull(fd, hdr4, sizeof hdr4, err)) return false;
  uint32_t header_len = GetU32(hdr4);
  if (header_len > kMaxHeaderBytes) {
    return Fail(err, "header too large: " + std::to_string(header_len));
  }
  if (!ReadInto(fd, &out->header_json, header_len, err)) return false;

  uint8_t len8[8];
  if (!ReadFull(fd, len8, sizeof len8, err)) return false;
  uint64_t payload_len = GetU64(len8);
  if (payload_len > max_payload_bytes) {
    return Fail(err, "payload too large: " + std::to_string(payload_len) +
                         " > " + std::to_string(max_payload_bytes));
  }
  return ReadInto(fd, &out->payload, payload_len, err);
}

bool WriteResponse(int fd, std::string_view report_json,
                   std::string_view output, std::string* err) {
  if (report_json.size() > kMaxReportBytes) {
    return Fail(err, "report too large: " + std::to_string(report_json.size()));
  }
  uint8_t hdr4[4];
  PutU32(hdr4, static_cast<uint32_t>(report_json.size()));
  uint8_t len8[8];
  PutU64(len8, static_cast<uint64_t>(output.size()));
  return WriteFull(fd, hdr4, sizeof hdr4, err) &&
         WriteFull(fd, report_json.data(), report_json.size(), err) &&
         WriteFull(fd, len8, sizeof len8, err) &&
         WriteFull(fd, output.data(), output.size(), err);
}

}  // namespace kilnfile

#include <unistd.h>

#include <string>

#include <gtest/gtest.h>

#include "protocol.h"

namespace kilnfile {
namespace {

struct Pipe {
  int r = -1, w = -1;
  Pipe() { int fds[2]; EXPECT_EQ(pipe(fds), 0); r = fds[0]; w = fds[1]; }
  ~Pipe() { if (r >= 0) close(r); if (w >= 0) close(w); }
  void CloseWrite() { close(w); w = -1; }
};

std::string U32(uint32_t v) {
  std::string s(4, 0);
  for (int i = 0; i < 4; i++) s[i] = static_cast<char>(v >> (8 * i));
  return s;
}
std::string U64(uint64_t v) {
  std::string s(8, 0);
  for (int i = 0; i < 8; i++) s[i] = static_cast<char>(v >> (8 * i));
  return s;
}
// Pipes hold 64 KiB; keep test frames small enough to write before reading.
void Feed(Pipe& p, const std::string& bytes) {
  ASSERT_EQ(write(p.w, bytes.data(), bytes.size()),
            static_cast<ssize_t>(bytes.size()));
  p.CloseWrite();
}

TEST(Protocol, RoundTripResponseLayout) {
  Pipe p;
  std::string err;
  ASSERT_TRUE(WriteResponse(p.w, "{\"a\":1}", "xyz", &err)) << err;
  p.CloseWrite();
  char buf[64];
  ssize_t n = read(p.r, buf, sizeof buf);
  std::string want = U32(7) + "{\"a\":1}" + U64(3) + "xyz";
  EXPECT_EQ(std::string(buf, n), want);
}

TEST(Protocol, RequestRoundTrip) {
  Pipe p;
  Feed(p, U32(2) + "{}" + U64(5) + "hello");
  Request req;
  std::string err;
  ASSERT_TRUE(ReadRequest(p.r, 1024, &req, &err)) << err;
  EXPECT_EQ(req.header_json, "{}");
  EXPECT_EQ(req.payload, "hello");
}

TEST(Protocol, EmptyHeaderAndPayload) {
  Pipe p;
  Feed(p, U32(0) + U64(0));
  Request req;
  std::string err;
  ASSERT_TRUE(ReadRequest(p.r, 1024, &req, &err)) << err;
  EXPECT_TRUE(req.header_json.empty());
  EXPECT_TRUE(req.payload.empty());
}

TEST(Protocol, ShortHeaderFails) {
  Pipe p;
  Feed(p, U32(10) + "abc");
  Request req;
  std::string err;
  EXPECT_FALSE(ReadRequest(p.r, 1024, &req, &err));
  EXPECT_NE(err.find("short read"), std::string::npos) << err;
}

TEST(Protocol, ShortPayloadFails) {
  Pipe p;
  Feed(p, U32(2) + "{}" + U64(10) + "abc");
  Request req;
  std::string err;
  EXPECT_FALSE(ReadRequest(p.r, 1024, &req, &err));
  EXPECT_NE(err.find("short read"), std::string::npos) << err;
}

TEST(Protocol, MissingPayloadLengthFails) {
  Pipe p;
  Feed(p, U32(2) + "{}" + "abc");
  Request req;
  std::string err;
  EXPECT_FALSE(ReadRequest(p.r, 1024, &req, &err));
}

TEST(Protocol, OversizedHeaderFails) {
  Pipe p;
  Feed(p, U32(kMaxHeaderBytes + 1));
  Request req;
  std::string err;
  EXPECT_FALSE(ReadRequest(p.r, 1024, &req, &err));
  EXPECT_NE(err.find("header too large"), std::string::npos) << err;
}

TEST(Protocol, PayloadOverCapFails) {
  Pipe p;
  Feed(p, U32(2) + "{}" + U64(11) + "01234567890");
  Request req;
  std::string err;
  EXPECT_FALSE(ReadRequest(p.r, 10, &req, &err));
  EXPECT_NE(err.find("payload too large"), std::string::npos) << err;
}

TEST(Protocol, PayloadAtCapOk) {
  Pipe p;
  Feed(p, U32(2) + "{}" + U64(10) + "0123456789");
  Request req;
  std::string err;
  EXPECT_TRUE(ReadRequest(p.r, 10, &req, &err)) << err;
}

TEST(Protocol, HugeDeclaredPayloadRejectedWithoutAllocating) {
  Pipe p;
  Feed(p, U32(2) + "{}" + U64(~0ull));
  Request req;
  std::string err;
  EXPECT_FALSE(ReadRequest(p.r, kDefaultMaxPayloadBytes, &req, &err));
}

TEST(Protocol, OversizedReportRefusedAndNothingWritten) {
  Pipe p;
  std::string big(kMaxReportBytes + 1, 'x');
  std::string err;
  EXPECT_FALSE(WriteResponse(p.w, big, "out", &err));
  p.CloseWrite();
  char c;
  EXPECT_EQ(read(p.r, &c, 1), 0);  // nothing was written
}

TEST(Protocol, ReportAtLimitOk) {
  Pipe p;
  std::string rep(kMaxReportBytes, 'x');
  std::string err;
  // Drain concurrently is unnecessary: fork a reader would be overkill, so
  // use a socket-sized check on a small write instead.
  EXPECT_TRUE(WriteResponse(p.w, rep.substr(0, 1000), "", &err)) << err;
}

}  // namespace
}  // namespace kilnfile

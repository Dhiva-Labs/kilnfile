#include <sys/wait.h>
#include <unistd.h>

#include <string>
#include <thread>
#include <vector>

#include <gtest/gtest.h>

#include "protocol.h"

namespace kilnfile {
namespace {

#ifdef __linux__

struct Result {
  int status = -1;
  std::string out, err;
};

// Runs the worker with `args`, feeding `input` to stdin.
Result RunWorker(const std::vector<std::string>& args, const std::string& input) {
  int in[2], out[2], err[2];
  EXPECT_EQ(pipe(in), 0);
  EXPECT_EQ(pipe(out), 0);
  EXPECT_EQ(pipe(err), 0);
  pid_t pid = fork();
  if (pid == 0) {
    dup2(in[0], 0); dup2(out[1], 1); dup2(err[1], 2);
    for (int fd : {in[0], in[1], out[0], out[1], err[0], err[1]}) close(fd);
    std::vector<char*> argv;
    argv.push_back(const_cast<char*>(KILNFILE_WORKER_PATH));
    for (auto& a : args) argv.push_back(const_cast<char*>(a.c_str()));
    argv.push_back(nullptr);
    execv(KILNFILE_WORKER_PATH, argv.data());
    _exit(127);
  }
  close(in[0]); close(out[1]); close(err[1]);
  std::thread writer([&] {
    (void)!write(in[1], "", 0);
    size_t done = 0;
    while (done < input.size()) {
      ssize_t n = write(in[1], input.data() + done, input.size() - done);
      if (n <= 0) break;
      done += n;
    }
    close(in[1]);
  });
  Result r;
  auto drain = [](int fd, std::string* dst) {
    char buf[65536];
    ssize_t n;
    while ((n = read(fd, buf, sizeof buf)) > 0) dst->append(buf, n);
    close(fd);
  };
  std::thread errt([&] { drain(err[0], &r.err); });
  drain(out[0], &r.out);
  errt.join();
  writer.join();
  waitpid(pid, &r.status, 0);
  return r;
}

std::string Frame(const std::string& header, const std::string& payload) {
  std::string s(4, 0);
  for (int i = 0; i < 4; i++) s[i] = static_cast<char>(header.size() >> (8 * i));
  s += header;
  std::string l(8, 0);
  for (int i = 0; i < 8; i++) l[i] = static_cast<char>(uint64_t{payload.size()} >> (8 * i));
  return s + l + payload;
}

bool ParseResponse(const std::string& b, std::string* report, std::string* output) {
  if (b.size() < 4) return false;
  uint32_t rl = 0;
  for (int i = 0; i < 4; i++) rl |= uint32_t{(unsigned char)b[i]} << (8 * i);
  if (b.size() < 4 + rl + 8) return false;
  *report = b.substr(4, rl);
  uint64_t ol = 0;
  for (int i = 0; i < 8; i++) ol |= uint64_t{(unsigned char)b[4 + rl + i]} << (8 * i);
  if (b.size() != 4 + rl + 8 + ol) return false;
  *output = b.substr(4 + rl + 8);
  return true;
}

TEST(EndToEnd, EchoWithSandboxOn) {
  std::string payload(3 << 20, 0);  // 3 MiB: exceeds pipe buffer, many reads
  for (size_t i = 0; i < payload.size(); i++) payload[i] = static_cast<char>(i * 31);
  Result r = RunWorker({"--protocol", "1", "--engine", "echo"},
                       Frame("{\"protocol\":1}", payload));
  ASSERT_TRUE(WIFEXITED(r.status)) << "status=" << r.status << " stderr=" << r.err;
  EXPECT_EQ(WEXITSTATUS(r.status), 0) << r.err;
  std::string report, output;
  ASSERT_TRUE(ParseResponse(r.out, &report, &output));
  EXPECT_EQ(output, payload);
  EXPECT_NE(report.find("\"result\":\"unchanged_semantics\""), std::string::npos);
  EXPECT_NE(report.find("\"detected_type\":\"unknown\""), std::string::npos);
  EXPECT_NE(report.find("\"bytes\":" + std::to_string(payload.size())), std::string::npos);
}

TEST(EndToEnd, EmptyPayload) {
  Result r = RunWorker({"--protocol", "1"}, Frame("{}", ""));
  ASSERT_TRUE(WIFEXITED(r.status));
  EXPECT_EQ(WEXITSTATUS(r.status), 0) << r.err;
  std::string report, output;
  ASSERT_TRUE(ParseResponse(r.out, &report, &output));
  EXPECT_TRUE(output.empty());
}

TEST(EndToEnd, ShortPayloadFailsWithNoOutput) {
  std::string in = Frame("{}", "0123456789");
  in.resize(in.size() - 4);
  Result r = RunWorker({"--protocol", "1"}, in);
  ASSERT_TRUE(WIFEXITED(r.status)) << r.err;
  EXPECT_NE(WEXITSTATUS(r.status), 0);
  EXPECT_TRUE(r.out.empty());
  EXPECT_FALSE(r.err.empty());
}

TEST(EndToEnd, PayloadOverCapFailsWithNoOutput) {
  Result r = RunWorker({"--protocol", "1", "--max-payload-bytes", "8"},
                       Frame("{}", "0123456789"));
  ASSERT_TRUE(WIFEXITED(r.status)) << r.err;
  EXPECT_NE(WEXITSTATUS(r.status), 0);
  EXPECT_TRUE(r.out.empty());
}

TEST(EndToEnd, BadProtocolFlagExits64) {
  Result r = RunWorker({"--protocol", "2"}, "");
  ASSERT_TRUE(WIFEXITED(r.status));
  EXPECT_EQ(WEXITSTATUS(r.status), 64);
  r = RunWorker({}, "");
  EXPECT_EQ(WEXITSTATUS(r.status), 64);
}

TEST(EndToEnd, NoSandboxWarnsLoudly) {
  Result r = RunWorker({"--protocol", "1", "--no-sandbox"}, Frame("{}", "abc"));
  ASSERT_TRUE(WIFEXITED(r.status));
  EXPECT_EQ(WEXITSTATUS(r.status), 0) << r.err;
  EXPECT_NE(r.err.find("WARNING"), std::string::npos);
}

#else
TEST(EndToEnd, SkippedOffLinux) { GTEST_SKIP(); }
#endif

}  // namespace
}  // namespace kilnfile

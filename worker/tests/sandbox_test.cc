#include <fcntl.h>
#include <sys/mman.h>
#include <sys/resource.h>
#include <sys/socket.h>
#include <sys/wait.h>
#include <unistd.h>

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <sys/syscall.h>
#include <functional>
#include <new>
#include <string>
#include <vector>

#include <gtest/gtest.h>

#include "sandbox.h"

namespace kilnfile {
namespace {

#if defined(__linux__)

// Runs `body` in a forked child. `report_fd` is a pipe the child can write to
// (even after the sandbox). Returns the wait status; fills `child_output`.
int RunInChild(const std::function<void(int report_fd)>& body,
               std::string* child_output = nullptr) {
  int fds[2];
  EXPECT_EQ(pipe(fds), 0);
  fflush(nullptr);
  pid_t pid = fork();
  if (pid == 0) {
    close(fds[0]);
    body(fds[1]);
    _exit(0);
  }
  close(fds[1]);
  std::string out;
  char buf[256];
  ssize_t n;
  while ((n = read(fds[0], buf, sizeof buf)) > 0) out.append(buf, n);
  close(fds[0]);
  int status = 0;
  EXPECT_EQ(waitpid(pid, &status, 0), pid);
  if (child_output) *child_output = out;
  return status;
}

void Enter(const SandboxOptions& o = SandboxOptions{}) {
  SandboxStatus st = EnterSandbox(o);
  if (!st.ok) {
    std::string m = "EnterSandbox failed: " + st.Describe() + "\n";
    (void)!write(2, m.data(), m.size());
    _exit(90);
  }
}

bool KilledBy(int status, int sig) {
  return WIFSIGNALED(status) && WTERMSIG(status) == sig;
}

TEST(Sandbox, OpenIsBlocked) {
  int st = RunInChild([](int) {
    Enter();
    int fd = open("/etc/hostname", O_RDONLY);
    _exit(fd < 0 ? 0 : 91);  // reaching here with a valid fd is a failure
  });
  // Killed by seccomp, or (defence in depth) the call failed.
  EXPECT_TRUE(KilledBy(st, SIGSYS) || (WIFEXITED(st) && WEXITSTATUS(st) == 0))
      << "status=" << st;
}

TEST(Sandbox, OpenatIsKilled) {
  int st = RunInChild([](int) {
    Enter();
    int fd = static_cast<int>(syscall(SYS_openat, AT_FDCWD, "/etc/hostname", 0));
    _exit(fd < 0 ? 0 : 91);
  });
  EXPECT_TRUE(KilledBy(st, SIGSYS)) << "status=" << st;
}

TEST(Sandbox, LandlockBlocksOpenWhenSeccompIsLogOnly) {
  // Seccomp in log mode lets open through, so only Landlock can stop it.
  std::string out;
  int st = RunInChild(
      [](int rfd) {
        SandboxOptions o;
        o.seccomp_log_only = true;
        SandboxStatus s = EnterSandbox(o);
        if (!s.ok) _exit(90);
        if (!s.landlock_enforced) {
          (void)!write(rfd, "nolandlock", 10);
          _exit(0);
        }
        int fd = open("/etc/hostname", O_RDONLY);
        _exit(fd < 0 && errno == EACCES ? 0 : 91);
      },
      &out);
  ASSERT_TRUE(WIFEXITED(st)) << "status=" << st;
  EXPECT_EQ(WEXITSTATUS(st), 0);
  if (out == "nolandlock") GTEST_SKIP() << "Landlock unavailable";
}

TEST(Sandbox, SocketIsKilled) {
  int st = RunInChild([](int) {
    Enter();
    socket(AF_INET, SOCK_STREAM, 0);
    _exit(0);
  });
  EXPECT_TRUE(KilledBy(st, SIGSYS)) << "status=" << st;
}

TEST(Sandbox, ExecveIsKilled) {
  int st = RunInChild([](int) {
    Enter();
    char* const argv[] = {const_cast<char*>("/bin/true"), nullptr};
    char* const envp[] = {nullptr};
    execve("/bin/true", argv, envp);
    _exit(0);
  });
  EXPECT_TRUE(KilledBy(st, SIGSYS)) << "status=" << st;
}

TEST(Sandbox, ForkIsKilled) {
  int st = RunInChild([](int) {
    Enter();
    fork();
    _exit(0);
  });
  EXPECT_TRUE(KilledBy(st, SIGSYS)) << "status=" << st;
}

TEST(Sandbox, ExecutableMmapIsKilled) {
  int st = RunInChild([](int) {
    Enter();
    mmap(nullptr, 4096, PROT_READ | PROT_EXEC, MAP_PRIVATE | MAP_ANONYMOUS, -1, 0);
    _exit(0);
  });
  EXPECT_TRUE(KilledBy(st, SIGSYS)) << "status=" << st;
}

TEST(Sandbox, WriteToInheritedPipeWorks) {
  std::string out;
  int st = RunInChild(
      [](int rfd) {
        Enter();
        const char msg[] = "hello from the sandbox";
        _exit(write(rfd, msg, sizeof msg - 1) == sizeof msg - 1 ? 0 : 92);
      },
      &out);
  ASSERT_TRUE(WIFEXITED(st)) << "status=" << st;
  EXPECT_EQ(WEXITSTATUS(st), 0);
  EXPECT_EQ(out, "hello from the sandbox");
}

TEST(Sandbox, AllocationBeyondRlimitAsFailsCleanly) {
  std::string out;
  int st = RunInChild(
      [](int rfd) {
        // Current VM size, so the limit sits just above what we already use.
        long pages = 0;
        if (FILE* f = fopen("/proc/self/statm", "r")) {
          if (fscanf(f, "%ld", &pages) != 1) pages = 0;
          fclose(f);
        }
        SandboxOptions o;
        o.address_space_bytes =
            static_cast<uint64_t>(pages) * sysconf(_SC_PAGESIZE) + (64ull << 20);
        Enter(o);
        void* p = malloc(512ull << 20);
        asm volatile("" : : "r"(p) : "memory");
        bool threw = false;
        try {
          std::vector<char> v(512ull << 20);
          asm volatile("" : : "r"(v.data()) : "memory");
        } catch (const std::bad_alloc&) {
          threw = true;
        }
        const char* msg = (p == nullptr && threw) ? "clean" : "unclean";
        (void)!write(rfd, msg, strlen(msg));
      },
      &out);
  ASSERT_TRUE(WIFEXITED(st)) << "status=" << st;
  EXPECT_EQ(out, "clean");
}

TEST(Sandbox, OneMiBAllocationsStillWork) {
  std::string out;
  int st = RunInChild(
      [](int rfd) {
        Enter();
        std::string s(1 << 20, 'a');
        s[12345] = 'b';
        std::vector<unsigned char> v(1 << 20, 7);
        for (size_t i = 0; i < v.size(); i += 4096) v[i] = 9;
        for (int i = 0; i < 100; i++) {  // churn the allocator
          std::string t(64 * 1024 + i, 'x');
          s += t.substr(0, 10);
        }
        bool ok = s[12345] == 'b' && v[4096] == 9 && v.back() == 7;
        (void)!write(rfd, ok ? "ok" : "bad", ok ? 2 : 3);
      },
      &out);
  ASSERT_TRUE(WIFEXITED(st)) << "status=" << st;
  EXPECT_EQ(out, "ok");
}

TEST(Sandbox, RlimitsAreApplied) {
  std::string out;
  int st = RunInChild(
      [](int rfd) {
        Enter();
        rlimit fs, nf, core;
        getrlimit(RLIMIT_FSIZE, &fs);
        getrlimit(RLIMIT_NOFILE, &nf);
        getrlimit(RLIMIT_CORE, &core);
        bool ok = fs.rlim_cur == 0 && core.rlim_cur == 0 && nf.rlim_cur <= 16;
        (void)!write(rfd, ok ? "ok" : "bad", ok ? 2 : 3);
      },
      &out);
  ASSERT_TRUE(WIFEXITED(st)) << "status=" << st;
  EXPECT_EQ(out, "ok");
}

TEST(Sandbox, ReportsLandlockStatus) {
  std::string out;
  int st = RunInChild(
      [](int rfd) {
        SandboxStatus s = EnterSandbox(SandboxOptions{});
        std::string d = s.Describe();
        (void)!write(rfd, d.data(), d.size());
        _exit(s.ok ? 0 : 1);
      },
      &out);
  ASSERT_TRUE(WIFEXITED(st)) << "status=" << st;
  EXPECT_EQ(WEXITSTATUS(st), 0) << out;
  RecordProperty("sandbox_status", out);
  std::printf("[ landlock ] %s\n", out.c_str());
  EXPECT_NE(out.find("landlock_abi="), std::string::npos);
}

#else

TEST(Sandbox, UnsupportedPlatformReportsUnsupported) {
  SandboxStatus st = EnterSandbox(SandboxOptions{});
  EXPECT_FALSE(st.supported);
  EXPECT_FALSE(st.ok);
}

#endif

}  // namespace
}  // namespace kilnfile

#include "sandbox.h"

#include <string>
#include <utility>
#include <vector>

#if defined(__linux__)
#include <linux/audit.h>
#include <linux/filter.h>
#include <linux/seccomp.h>
#include <sys/prctl.h>
#include <sys/resource.h>
#include <sys/syscall.h>
#include <unistd.h>

#include <cerrno>
#include <cstddef>
#include <csignal>
#include <cstring>
#endif

namespace kilnfile {

std::string SandboxStatus::Describe() const {
  if (!supported) return "sandbox: unsupported on this platform";
  std::string s = "sandbox: ";
  s += ok ? "ok" : "FAILED (" + error + ")";
  s += ", no_new_privs=" + std::string(no_new_privs ? "yes" : "no");
  s += ", rlimits=" + std::string(rlimits_applied ? "yes" : "no");
  s += ", landlock_abi=" + std::to_string(landlock_abi);
  s += ", landlock=" + std::string(landlock_enforced ? "enforced" : "off");
  s += ", seccomp=";
  s += seccomp_enforced ? (seccomp_log_only ? "log-only" : "kill") : "off";
  for (const auto& w : warnings) s += "; warning: " + w;
  return s;
}

#if !defined(__linux__)

SandboxStatus EnterSandbox(const SandboxOptions&) {
  SandboxStatus st;
  st.supported = false;
  st.error = "unsupported platform";
  return st;
}

#else  // __linux__

namespace {

// ---------------------------------------------------------------------------
// Landlock (raw syscalls, so no libc or header support is required).
// ---------------------------------------------------------------------------
#ifndef __NR_landlock_create_ruleset
#define __NR_landlock_create_ruleset 444
#define __NR_landlock_add_rule 445
#define __NR_landlock_restrict_self 446
#endif

constexpr uint32_t kLandlockCreateRulesetVersion = 1u << 0;

// Filesystem rights per ABI version (bit positions from linux/landlock.h).
constexpr uint64_t kFsAbi1 = (1ull << 13) - 1;  // EXECUTE .. MAKE_SYM
constexpr uint64_t kFsRefer = 1ull << 13;       // ABI 2
constexpr uint64_t kFsTruncate = 1ull << 14;    // ABI 3
constexpr uint64_t kFsIoctlDev = 1ull << 15;    // ABI 5
constexpr uint64_t kNetAll = 3;                 // ABI 4: bind + connect TCP
constexpr uint64_t kScopeAll = 3;               // ABI 6: abstract unix + signal

struct LandlockAttr {  // superset of landlock_ruleset_attr; size is per ABI
  uint64_t handled_access_fs;
  uint64_t handled_access_net;
  uint64_t scoped;
};

uint64_t FsRightsForAbi(int abi) {
  uint64_t r = kFsAbi1;
  if (abi >= 2) r |= kFsRefer;
  if (abi >= 3) r |= kFsTruncate;
  if (abi >= 5) r |= kFsIoctlDev;
  return r;
}

void EnableLandlock(SandboxStatus* st) {
  long abi = syscall(__NR_landlock_create_ruleset, nullptr, 0,
                     kLandlockCreateRulesetVersion);
  if (abi < 1) {
    st->landlock_abi = 0;
    st->warnings.push_back(std::string("landlock unavailable: ") +
                           std::strerror(errno));
    return;
  }
  st->landlock_abi = static_cast<int>(abi);

  LandlockAttr attr{FsRightsForAbi(st->landlock_abi), 0, 0};
  size_t size = 8;
  if (abi >= 4) { attr.handled_access_net = kNetAll; size = 16; }
  if (abi >= 6) { attr.scoped = kScopeAll; size = 24; }

  int fd = static_cast<int>(
      syscall(__NR_landlock_create_ruleset, &attr, size, 0));
  if (fd < 0) {
    st->warnings.push_back(std::string("landlock_create_ruleset failed: ") +
                           std::strerror(errno));
    return;
  }
  // No rules are added: every handled right is denied everywhere.
  long rc = syscall(__NR_landlock_restrict_self, fd, 0);
  int saved = errno;
  close(fd);
  if (rc != 0) {
    st->warnings.push_back(std::string("landlock_restrict_self failed: ") +
                           std::strerror(saved));
    return;
  }
  st->landlock_enforced = true;
}

// ---------------------------------------------------------------------------
// rlimits
// ---------------------------------------------------------------------------
bool SetLimit(int resource, uint64_t value, std::string* err,
              const char* name) {
  rlimit rl;
  rl.rlim_cur = rl.rlim_max = static_cast<rlim_t>(value);
  if (setrlimit(resource, &rl) != 0) {
    *err = std::string("setrlimit ") + name + ": " + std::strerror(errno);
    return false;
  }
  return true;
}

// ---------------------------------------------------------------------------
// seccomp-bpf
//
// ALLOWLIST (everything else kills the process with SIGSYS):
//   I/O on inherited fds : read write readv writev close lseek fstat
//                          newfstatat(*) 
//   memory               : mmap(*) munmap mremap mprotect(*) brk madvise
//   synchronization/time : futex clock_gettime clock_getres clock_nanosleep
//                          nanosleep gettimeofday time sched_yield
//   randomness           : getrandom
//   signals              : rt_sigaction rt_sigprocmask rt_sigreturn sigaltstack
//   identity             : gettid getpid
//   runtime              : rseq set_robust_list prlimit64(*) restart_syscall
//   exit                 : exit exit_group
//
//   (*) argument-restricted:
//     mmap, mprotect : PROT_EXEC is refused, so no new executable memory
//                      can be created after warmup (no JIT, no dlopen).
//     newfstatat     : flags must be exactly AT_EMPTY_PATH (the glibc fstat
//                      idiom). BPF cannot read the path string, so a
//                      non-empty absolute path would still be stat()ed;
//                      Landlock does not cover stat. Acceptable: metadata
//                      only, no file open.
//     prlimit64      : pid must be 0 and new_limit must be NULL (read-only).
//
// Explicitly NOT allowed (default-deny, listed for the record): open openat
// openat2 creat socket connect bind execve execveat fork vfork clone clone3
// ptrace kill tkill tgkill (abort() therefore dies by SIGSYS instead of
// SIGABRT) mount chdir unlink process_vm_readv/writev io_uring_* bpf
// perf_event_open userfaultfd.
//
// HOW TO EXTEND: run the worker with --sandbox-log, exercise the failing flow,
// then read the denied syscall names with `journalctl -k | grep SECCOMP` or
// `dmesg | grep audit` (syscall= numbers; `ausyscall <n>` names them). Add a
// SIMPLE(SYS_x) line to kSimple below (or an argument-checked block in
// BuildFilter), re-run without --sandbox-log, and add a test.
// ---------------------------------------------------------------------------

#if defined(__x86_64__)
constexpr uint32_t kAuditArch = AUDIT_ARCH_X86_64;
#elif defined(__aarch64__)
constexpr uint32_t kAuditArch = AUDIT_ARCH_AARCH64;
#else
constexpr uint32_t kAuditArch = 0;
#endif

// Syscalls allowed with no argument checks. Hot ones first.
const int kSimple[] = {
    SYS_read, SYS_write, SYS_futex, SYS_close, SYS_readv, SYS_writev,
    SYS_lseek, SYS_fstat, SYS_munmap, SYS_mremap, SYS_brk, SYS_madvise,
    SYS_getrandom, SYS_clock_gettime, SYS_clock_getres, SYS_clock_nanosleep,
    SYS_nanosleep, SYS_sched_yield, SYS_rt_sigaction, SYS_rt_sigprocmask,
    SYS_rt_sigreturn, SYS_sigaltstack, SYS_gettid, SYS_getpid, SYS_rseq,
    SYS_set_robust_list, SYS_restart_syscall, SYS_exit, SYS_exit_group,
#ifdef SYS_gettimeofday
    SYS_gettimeofday,
#endif
#ifdef SYS_time
    SYS_time,
#endif
};

// Argument-checked syscalls, each routed to its own block.
enum Checked { kMmap, kMprotect, kFstatat, kPrlimit, kNumChecked };
const int kCheckedNr[kNumChecked] = {SYS_mmap, SYS_mprotect, SYS_newfstatat,
                                     SYS_prlimit64};

// Tiny assembler with symbolic jump targets (BPF jumps are 8-bit relative).
class Bpf {
 public:
  int NewLabel() { labels_.push_back(-1); return static_cast<int>(labels_.size()) - 1; }
  void Bind(int label) { labels_[label] = static_cast<int>(prog_.size()); }
  void Load(uint32_t offset) {
    prog_.push_back(BPF_STMT(BPF_LD | BPF_W | BPF_ABS, offset));
    refs_.push_back({-1, -1});
  }
  void Ret(uint32_t action) {
    prog_.push_back(BPF_STMT(BPF_RET | BPF_K, action));
    refs_.push_back({-1, -1});
  }
  // if (A == k) goto jt else goto jf;  (-1 = fall through)
  void JeqK(uint32_t k, int jt, int jf) { Jump(BPF_JEQ, k, jt, jf); }
  void JsetK(uint32_t k, int jt, int jf) { Jump(BPF_JSET, k, jt, jf); }
  void JgeK(uint32_t k, int jt, int jf) { Jump(BPF_JGE, k, jt, jf); }

  bool Finish(std::vector<sock_filter>* out) {
    for (size_t i = 0; i < prog_.size(); i++) {
      auto [jt, jf] = refs_[i];
      if (jt >= 0) {
        long d = labels_[jt] - static_cast<long>(i) - 1;
        if (labels_[jt] < 0 || d < 0 || d > 255) return false;
        prog_[i].jt = static_cast<uint8_t>(d);
      }
      if (jf >= 0) {
        long d = labels_[jf] - static_cast<long>(i) - 1;
        if (labels_[jf] < 0 || d < 0 || d > 255) return false;
        prog_[i].jf = static_cast<uint8_t>(d);
      }
    }
    *out = prog_;
    return true;
  }

 private:
  void Jump(uint16_t op, uint32_t k, int jt, int jf) {
    prog_.push_back(BPF_JUMP(BPF_JMP | op | BPF_K, k, 0, 0));
    refs_.push_back({jt, jf});
  }
  std::vector<sock_filter> prog_;
  std::vector<std::pair<int, int>> refs_;
  std::vector<int> labels_;
};

constexpr uint32_t kArgOffset = offsetof(seccomp_data, args);
constexpr uint32_t ArgLo(int i) { return kArgOffset + 8 * i; }   // little-endian
constexpr uint32_t ArgHi(int i) { return kArgOffset + 8 * i + 4; }

bool BuildFilter(bool log_only, std::vector<sock_filter>* out) {
  const uint32_t deny =
      log_only ? SECCOMP_RET_LOG : SECCOMP_RET_KILL_PROCESS;
  Bpf b;
  int l_allow = b.NewLabel(), l_deny = b.NewLabel(), l_deny2 = b.NewLabel();
  int l_blocks[kNumChecked];
  for (auto& l : l_blocks) l = b.NewLabel();

  // Wrong architecture is always fatal, even in log-only mode.
  int l_arch_ok = b.NewLabel();
  b.Load(offsetof(seccomp_data, arch));
  b.JeqK(kAuditArch, l_arch_ok, -1);
  b.Ret(SECCOMP_RET_KILL_PROCESS);
  b.Bind(l_arch_ok);
  b.Load(offsetof(seccomp_data, nr));
#if defined(__x86_64__)
  // Refuse the x32 syscall ABI (nr has bit 30 set).
  b.JgeK(0x40000000u, l_deny, -1);
#endif
  for (int nr : kSimple) b.JeqK(static_cast<uint32_t>(nr), l_allow, -1);
  for (int i = 0; i < kNumChecked; i++)
    b.JeqK(static_cast<uint32_t>(kCheckedNr[i]), l_blocks[i], -1);
  b.Bind(l_deny);  // default action
  b.Ret(deny);

  // mmap(addr, len, prot, ...): refuse PROT_EXEC.
  b.Bind(l_blocks[kMmap]);
  b.Load(ArgLo(2));
  b.JsetK(0x4u /* PROT_EXEC */, l_deny2, l_allow);
  // mprotect(addr, len, prot): refuse PROT_EXEC.
  b.Bind(l_blocks[kMprotect]);
  b.Load(ArgLo(2));
  b.JsetK(0x4u /* PROT_EXEC */, l_deny2, l_allow);
  // newfstatat(dirfd, path, buf, flags): flags must be AT_EMPTY_PATH.
  b.Bind(l_blocks[kFstatat]);
  b.Load(ArgLo(3));
  b.JeqK(0x1000u /* AT_EMPTY_PATH */, l_allow, l_deny2);
  // prlimit64(pid, resource, new_limit, old_limit): pid 0, new_limit NULL.
  b.Bind(l_blocks[kPrlimit]);
  b.Load(ArgLo(0));
  b.JeqK(0, -1, l_deny2);
  b.Load(ArgLo(2));
  b.JeqK(0, -1, l_deny2);
  b.Load(ArgHi(2));
  b.JeqK(0, l_allow, l_deny2);
  b.Bind(l_allow);
  b.Ret(SECCOMP_RET_ALLOW);
  b.Bind(l_deny2);
  b.Ret(deny);
  return b.Finish(out);
}

bool InstallSeccomp(bool log_only, std::string* err) {
  if (kAuditArch == 0) { *err = "unsupported architecture"; return false; }
  std::vector<sock_filter> prog;
  if (!BuildFilter(log_only, &prog)) {
    *err = "seccomp filter jump out of range";
    return false;
  }
  sock_fprog fprog{static_cast<unsigned short>(prog.size()), prog.data()};
  if (syscall(SYS_seccomp, SECCOMP_SET_MODE_FILTER, 0, &fprog) != 0) {
    *err = std::string("seccomp: ") + std::strerror(errno);
    return false;
  }
  return true;
}

}  // namespace

SandboxStatus EnterSandbox(const SandboxOptions& o) {
  SandboxStatus st;
  st.supported = (kAuditArch != 0);
  if (!st.supported) { st.error = "unsupported architecture"; return st; }
  st.seccomp_log_only = o.seccomp_log_only;

  // a. no_new_privs
  if (prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != 0) {
    st.error = std::string("PR_SET_NO_NEW_PRIVS: ") + std::strerror(errno);
    return st;
  }
  st.no_new_privs = true;

  // b. rlimits. With RLIMIT_FSIZE=0 a write to a regular file (stderr may be
  // one) would raise SIGXFSZ and kill us; make it fail with EFBIG instead.
  std::signal(SIGXFSZ, SIG_IGN);
  std::string err;
  bool ok = SetLimit(RLIMIT_AS, o.address_space_bytes, &err, "AS") &&
            SetLimit(RLIMIT_CPU, o.cpu_seconds, &err, "CPU") &&
            SetLimit(RLIMIT_FSIZE, 0, &err, "FSIZE") &&
            SetLimit(RLIMIT_NOFILE, o.max_open_files, &err, "NOFILE") &&
            SetLimit(RLIMIT_CORE, 0, &err, "CORE");
  if (ok && o.limit_processes) ok = SetLimit(RLIMIT_NPROC, 0, &err, "NPROC");
  if (!ok) { st.error = err; return st; }
  st.rlimits_applied = true;

  // c. Landlock: best effort, seccomp still blocks open.
  if (o.use_landlock) EnableLandlock(&st);
  else st.warnings.push_back("landlock disabled by options");

  // d. seccomp
  if (!InstallSeccomp(o.seccomp_log_only, &err)) { st.error = err; return st; }
  st.seccomp_enforced = true;
  st.ok = true;
  return st;
}

#endif  // __linux__

}  // namespace kilnfile

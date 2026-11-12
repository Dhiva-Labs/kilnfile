// Linux sandbox for the worker: no_new_privs, rlimits, Landlock (no
// filesystem access) and a seccomp-bpf allowlist. See docs/SANDBOX.md.
#ifndef KILNFILE_WORKER_SANDBOX_H_
#define KILNFILE_WORKER_SANDBOX_H_

#include <cstdint>
#include <string>
#include <vector>

namespace kilnfile {

struct SandboxOptions {
  uint64_t address_space_bytes = 4ull << 30;  // RLIMIT_AS
  uint64_t cpu_seconds = 60;                  // RLIMIT_CPU
  uint64_t max_open_files = 16;               // RLIMIT_NOFILE
  bool limit_processes = true;                // RLIMIT_NPROC = 0
  bool use_landlock = true;
  // Debug: SECCOMP_RET_LOG instead of SECCOMP_RET_KILL_PROCESS as the default
  // action, so missing syscalls show up in the audit log / dmesg.
  bool seccomp_log_only = false;
};

struct SandboxStatus {
  bool supported = false;       // false on non-Linux or unknown architecture
  bool ok = false;              // every mandatory step succeeded
  bool no_new_privs = false;
  bool rlimits_applied = false;
  // Landlock ABI version reported by the kernel; 0 when unavailable.
  int landlock_abi = 0;
  bool landlock_enforced = false;
  bool seccomp_enforced = false;
  bool seccomp_log_only = false;
  std::string error;                  // set when ok == false and supported
  std::vector<std::string> warnings;  // non-fatal degradations

  std::string Describe() const;
};

// Applies the sandbox to the calling process (irreversible). On failure the
// process must not read untrusted input. Call it single-threaded.
SandboxStatus EnterSandbox(const SandboxOptions& options);

}  // namespace kilnfile

#endif  // KILNFILE_WORKER_SANDBOX_H_

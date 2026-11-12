# Worker sandbox (Linux)

`kilnfile-worker` calls `EnterSandbox()` after `Engine::Warmup()` and before it
reads any input. Steps, in order:

1. `PR_SET_NO_NEW_PRIVS`.
2. rlimits: `AS` (4 GiB), `CPU` (60 s), `FSIZE=0`, `NOFILE` (16), `NPROC=0`,
   `CORE=0`. `SIGXFSZ` is ignored so a write to a regular-file stderr fails
   with `EFBIG` instead of killing the worker.
3. Landlock: handles every filesystem right the kernel's ABI knows (plus TCP
   bind/connect from ABI 4 and the abstract-socket and signal scopes from
   ABI 6) and adds no rules. If Landlock is missing the status says so and
   the worker continues; seccomp still blocks `open`.
4. seccomp-bpf, default action `SECCOMP_RET_KILL_PROCESS` (the process dies by
   `SIGSYS`). The allowlist is documented at the top of the seccomp section in
   `worker/sandbox.cc`. `mmap` and `mprotect` refuse `PROT_EXEC`.

Anything the sandbox cannot set up (other than Landlock) makes the worker exit
non-zero before reading input.

## Debugging a missing syscall

Run with `--sandbox-log`. Denied syscalls are logged instead of killing the
process (`journalctl -k | grep SECCOMP`, or `dmesg | grep audit`; the
`syscall=` number is the architecture's syscall number). Add the syscall to
`kSimple` in `worker/sandbox.cc` (or add an argument-checked block), then add
a test. Note that stderr is not logged by seccomp; the worker prints the
sandbox status line to stderr in this mode.

`--no-sandbox` disables everything. Debug only.

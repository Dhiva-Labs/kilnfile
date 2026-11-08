#!/usr/bin/env bash
# Local dev helper. Configures and builds with the extra flags this machine
# needs; override via env. Not used by CI.
#   KILN_GCC_INSTALL_DIR  e.g. /usr/lib/gcc/x86_64-linux-gnu/15 (optional)
#   CRUBIT_DIR            Crubit checkout (default ~/.cache/cdr-build/crubit)
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=/dev/null
source "${root}/toolchain.env"
export PATH="${HOME}/.cargo/bin:${PATH}" RUSTUP_TOOLCHAIN="${RUST_NIGHTLY}"
build="${BUILD_DIR:-${HOME}/.cache/kilnfile-build/main}"
flags=()
if [[ -n "${KILN_GCC_INSTALL_DIR:-}" ]]; then
  f="--gcc-install-dir=${KILN_GCC_INSTALL_DIR}"
  flags=("-DCMAKE_C_FLAGS=${f}" "-DCMAKE_CXX_FLAGS=${f}"
         "-DCMAKE_EXE_LINKER_FLAGS=${f}" "-DCMAKE_SHARED_LINKER_FLAGS=${f}")
fi
cmake -G Ninja -B "${build}" -S "${root}" \
  -DCMAKE_C_COMPILER=clang -DCMAKE_CXX_COMPILER=clang++ "${flags[@]}" \
  -DFETCHCONTENT_SOURCE_DIR_CRUBIT="${CRUBIT_DIR:-${HOME}/.cache/cdr-build/crubit}"
cmake --build "${build}" --parallel "${JOBS:-6}"
ctest --test-dir "${build}" --output-on-failure

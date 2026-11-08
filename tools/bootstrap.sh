#!/usr/bin/env bash
# Installs the pinned toolchain: rustup (if missing), the pinned Rust nightly
# with the components Crubit needs, and Crubit at the pinned commit.
# Idempotent: safe to re-run. Does not use sudo or install system packages;
# it only checks that cmake, clang and git exist.
#
# Env overrides:
#   CRUBIT_DIR   where to keep the Crubit checkout
#                (default: ${XDG_CACHE_HOME:-~/.cache}/kilnfile/crubit)
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=/dev/null
source "${root}/toolchain.env"

for tool in git cmake clang curl; do
  command -v "${tool}" >/dev/null || { echo "missing required tool: ${tool}" >&2; exit 1; }
done

export PATH="${HOME}/.cargo/bin:${PATH}"
if ! command -v rustup >/dev/null; then
  echo "installing rustup"
  curl --proto '=https' --tlsv1.2 -fsSL https://sh.rustup.rs | sh -s -- -y --profile minimal --default-toolchain none
  export PATH="${HOME}/.cargo/bin:${PATH}"
fi

# Re-running with the same toolchain is a no-op apart from checking components.
rustup toolchain install "${RUST_NIGHTLY}" --profile minimal \
  --component rust-src --component rustc-dev --component llvm-tools-preview

crubit_dir="${CRUBIT_DIR:-${XDG_CACHE_HOME:-${HOME}/.cache}/kilnfile/crubit}"
if [[ "$(git -C "${crubit_dir}" rev-parse HEAD 2>/dev/null || true)" != "${CRUBIT_COMMIT}" ]]; then
  echo "fetching Crubit ${CRUBIT_COMMIT} into ${crubit_dir}"
  rm -rf "${crubit_dir}"
  mkdir -p "${crubit_dir}"
  git init -q "${crubit_dir}"
  git -C "${crubit_dir}" fetch -q --depth 1 https://github.com/google/crubit.git "${CRUBIT_COMMIT}"
  git -C "${crubit_dir}" checkout -q FETCH_HEAD
fi

cat <<MSG
toolchain ready:
  export PATH="\$HOME/.cargo/bin:\$PATH" RUSTUP_TOOLCHAIN=${RUST_NIGHTLY}
  cmake -B build -S . -DCMAKE_C_COMPILER=clang -DCMAKE_CXX_COMPILER=clang++ \\
        -DFETCHCONTENT_SOURCE_DIR_CRUBIT=${crubit_dir}
MSG

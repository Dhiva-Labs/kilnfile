# M0 report: build spike

Date: 2026-11-09. Toolchain: see `toolchain.env` (safe-bindings 47b1f4b, Crubit
f144a4f, nightly-2026-11-07), clang 21, CMake 4.2, Ninja, Linux x86_64.

## Decision: strategy A (one umbrella crate)

Zip and xml from safe-bindings link into one C++ executable, together with
kilnfile's own Rust bridge, through a single Rust staticlib and a single Crubit
header. Strategy B is not needed. The submodule stays pristine; everything is
patched in copies made at configure time.

## 1. Two bindings in one binary: worked

How it works (`tools/vendor_bindings.sh <out_dir>`, run by CMake at configure):

- Creates the crate `kilnfile_rs` with one module per binding: `sb_zip` (from
  `zip/rust`), `sb_xml` (from `xml/rust`) and `kilnfile_bridge` (from
  `bridge/src`). Each binding's `lib.rs` becomes `mod.rs`; `crate::` is rewritten
  to `crate::<module>::`.
- Merges the `[dependencies]` of the three manifests into one `Cargo.toml`.
- Copies each binding's C++ wrappers into `<build>/vendored/cpp/{zip,xml}` and
  rewrites `rust::` to `::kilnfile_rs::sb_zip::` / `::kilnfile_rs::sb_xml::` and
  `crubit/rust.h` to `crubit/kilnfile_rs.h`. Upstream `main.cc` examples are
  skipped. The C++ namespaces (`security::zip`, `security::xml`) don't collide,
  so they are unchanged.
- Crubit maps nested Rust modules to nested C++ namespaces, so the rename is
  just a namespace prefix. No `pub use` re-exports were needed.

Evidence (`tools/local_build.sh`, which wraps the commands below):

```
cmake -G Ninja -B build -S . -DCMAKE_C_COMPILER=clang -DCMAKE_CXX_COMPILER=clang++ \
      -DFETCHCONTENT_SOURCE_DIR_CRUBIT=<crubit>
cmake --build build --parallel 6 && ctest --test-dir build --output-on-failure
```

```
entry 0: [Content_Types].xml
entry 1: word/document.xml
entry 2: word/media/image1.png
two_bindings_test ok: root=document text="hello kiln"
100% tests passed, 0 tests failed out of 2
```

`spike/two_bindings_test.cc` builds an OOXML-shaped zip with the zip binding
(including a PNG made by the bridge), reopens it, lists entries, and parses the
XML entry with the xml binding. One `libkilnfile_rs.a` (346 MB, debug) holds all
Rust code; one copy of Rust std.

Build time from an empty build directory (warm cargo registry, local Crubit
checkout, Corrosion and Abseil fetched by git): 1m32s wall, 4m38s CPU, 6 jobs.
Incremental rebuild after a C++-only change: about 20s.

Problems hit and fixes:

1. `absl::status_macros` not found. safe-bindings' `xml/CMakeLists.txt` pins
   Abseil 20250127.1, which has no such target; it only works there because
   Crubit's CMake declares Abseil 20260526.0 first (first FetchContent
   declaration wins). Our top-level CMake declares Abseil after
   `corrosion_import_crate`, pinned to 20260526.0.
2. Crubit `cargo cpp_api_from_rust` failed with `error: duplicate key` in its
   generated `Cargo.toml` (`miniz_oxide = "0.8.9"` and `"0.9.1"`). Cause: two
   versions of one crate in the dependency graph; Crubit writes one entry per
   crate name. Pulled in by `flate2` 1.1.10+ (miniz_oxide 0.9) against png/zip
   (0.8). Fix: `flate2 = ">=1.1, <1.1.10"` in `bridge/Cargo.toml`, the same
   workaround safe-bindings' pixel_bridge uses (its note: b/554658975).
3. `ABSL_DEFINE_UNQUALIFIED_STATUS_MACROS` is needed for the xml wrappers
   (set on `sb_xml`).

## 2. kilnfile's own Crubit bridge: worked

`bridge/src/lib.rs` exposes `encode_png_rgba8(width, height, &[u8]) ->
Result<PngBytes, BridgeError>` using `image` 0.25 with only the `png` feature.
`bridge/kilnfile_bridge.{h,cc}` wraps it as `absl::StatusOr<std::string>
EncodePngRgba8(...)`. It is part of the same umbrella staticlib and header
(`crubit/kilnfile_rs.h`, namespace `kilnfile_rs::kilnfile_bridge`).
`spike/bridge_png_test.cc` checks the PNG signature, the IHDR chunk, IEND, and
that a wrongly sized buffer returns `INVALID_ARGUMENT` instead of crashing:

```
bridge_png_test ok: 119 PNG bytes; error path: INVALID_ARGUMENT: buffer is 5 bytes, expected 48 for 4x3 RGBA8
```

Crubit's native support for slices, `Result` and plain structs/enums was enough.
We followed safe-bindings' pattern of a `repr(C)` byte-buffer wrapper and an
error struct with an enum code (C++ side switches on `err.code.tag`).

## 3. Android NDK (aarch64-linux-android): partial, fails in Crubit

NDK 28.2.13676358, platform 30, target added with
`rustup target add aarch64-linux-android --toolchain nightly-2026-11-07`.

- Rust only: worked. `cargo build --target aarch64-linux-android` of the whole
  umbrella crate (zip, xml, image, with zstd-sys and bzip2 C code compiled by the
  NDK clang via `CC_aarch64_linux_android`) finished in about 8s.
- Through CMake (NDK `android.toolchain.cmake`, `ANDROID_ABI=arm64-v8a`,
  `-DRust_CARGO_TARGET=aarch64-linux-android`): the Rust build step passes, then
  Crubit's `cargo cpp_api_from_rust` fails while generating bindings for the
  standard library crates:

  ```
  error[E0461]: couldn't find crate `core` with expected target triple x86_64-unknown-linux-gnu
   --> <lib.rs>:4:1
    4 | extern crate r#core;
    = note: the following crate versions were found:
      crate `core`, target triple aarch64-linux-android: .../lib/rustlib/aarch64-linux-android/lib/libcore-....rmeta
  Error: Crubit failed to generate C++ bindings for Rust crate `core` due to the following errors:
  ```

  So at this Crubit commit the bindings generator runs its internal rustc for
  `core` with the host triple while the sysroot only has the Android one, i.e.
  cross-compilation is not wired through. The C++ compile and link for Android
  were therefore not reached.
- Not tried (time-box): generating the header on the host and compiling it for
  Android. Risky because Crubit headers embed type layouts and size/alignment
  assertions for the target. Other options: report upstream, or check newer
  Crubit commits.

## CI

`.github/workflows/ci.yml` mirrors safe-bindings' CI (actions pinned by commit
SHA, same setup-rust-toolchain with `rustflags: ""`, Crubit fetched at the pinned
commit into `RUNNER_TEMP`), but reads the pins from `toolchain.env` instead of
resolving Crubit's `latest` tag. It checks the submodule commit, configures,
builds and runs ctest. It could not be run locally and has not been seen green.
`tools/bootstrap.sh` (rustup, pinned nightly and components, pinned Crubit) was
not run end to end on a clean machine; it was only checked to be idempotent in
design (re-running skips what is present).

## Remaining risks for M1

- Crubit is tied to a nightly; a bump means re-pinning both together. Dependency
  graphs with two versions of one crate break Crubit (item 2 above). Adding
  pixel_bridge, jxl, lzma, deflate, saphyr and serde_json to the umbrella will
  hit this again and may need more version pins. pixel_bridge also overlaps with
  the bridge's `image` dependency (feature unification is fine, versions are not
  guaranteed).
- The vendoring script rewrites sources with regexes (`crate::`, `rust::`). Any
  binding with `pub use` re-exports of other crates, `#[path]` tricks, `$crate`,
  macros or `rust::` inside string literals needs a check when it is added. New
  safe-bindings commits can break the rewrite; the submodule pin limits this.
- Each added binding has more name collisions to resolve (generic file names such
  as `file.h`, `converters.h`, and differing Abseil expectations). They are kept
  apart by per-binding directories today.
- Android SDK: Crubit cross-compilation fails (item 3). The SDK needs either a
  Crubit fix, a different Android integration (host-generated headers verified
  against the target), or a C ABI layer instead of Crubit for Android. Not yet
  tested: iOS, macOS, Windows.
- Size and speed: the debug staticlib is 346 MB and the test binary 151 MB; a
  release build with LTO and a size check are untested. Crubit headers emit many
  `-Wnontrivial-memcall` warnings (expected noise) and the header is large, which
  slows every C++ file that includes it.
- Not measured: release-mode behavior, Linux sandbox (seccomp) compatibility of
  the Rust std in a worker, sanitizer builds for fuzzing.

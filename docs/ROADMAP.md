# kilnfile roadmap

Each milestone ends with a pull request to `Dhiva-Labs/kilnfile` and has exit
criteria that are checked before it's called done. See [DESIGN.md](DESIGN.md)
for how the pieces fit.

## M0: prove the build (spike)

Answer the questions everything else depends on, before writing the engine.

- Repository skeleton, `toolchain.env` (safe-bindings commit, Crubit commit,
  Rust nightly) and a bootstrap script that installs the pinned toolchain.
- safe-bindings vendored as a submodule at a pinned commit.
- Two bindings in one binary: zip and xml linked together (strategy A, the
  umbrella crate). If A fails, prove strategy B (one worker per binding) and
  write down why A failed.
- kilnfile_bridge: one Rust function (PNG encode) exposed to C++ through
  Crubit and called from a test.
- Android NDK cross-build of one binding (risk check for the SDK; failure here
  is allowed but must be documented).
- CI on GitHub Actions builds all of the above on Linux.

Exit: a written decision on strategy A or B, with build logs, and CI green.

## M1: engine core, CLI, images and structured data

- libkilnfile: type detection by content, the policy model, the report
  builder, limits.
- Image pipeline (PNG, JPEG, GIF, WebP, BMP, TIFF, ICO, JPEG XL): decode with
  limits, apply orientation, re-encode, drop metadata.
- Structured pipeline: JSON, YAML, XML with depth, size and alias limits.
- kilnfile-worker with the framed protocol (no sandbox yet).
- Go CLI: `kilnfile sanitize <in> -o <out> [--policy p.yaml] [--report r.json]`.
- Corpus generators and tests for these formats. First fuzz targets.

Exit: the CLI rebuilds every benign sample and rejects or neutralizes every
hostile sample for these formats; tests and a short fuzz run pass in CI.

## M2: Office documents and archives

- OOXML pipeline (DOCX, XLSX, PPTX and macro-enabled variants), as specified
  in DESIGN.md.
- Archive pipeline (ZIP, gzip, xz) with recursion, path normalization and
  cumulative limits.
- Corpus generators for macro documents, external templates, DDE fields, zip
  bombs, path traversal, decompression bombs.

Exit: the sanitized Office output opens cleanly in LibreOffice (checked
headlessly in CI) and contains none of the removed part types; every archive
bomb is rejected within its limits.

## M3: HTTP API and sandbox

- `kilnfile serve`: `POST /v1/sanitize` (raw body or multipart), the report in
  a response header or as JSON, configurable limits, a pool of pre-started
  workers, Prometheus metrics, health endpoints, an OpenAPI spec.
- Worker sandbox on Linux: no-new-privileges, rlimits, Landlock, seccomp
  allowlist.
- Container image and release binaries for linux/amd64 and linux/arm64.

Exit: the end-to-end suite passes against the API; a deliberately crashing or
looping worker is contained and reported, and the server keeps serving.

## M4: C API, Android and Flutter (Android)

- Stable C API and shared library.
- Android AAR: Kotlin API, JNI, sanitizing inside an isolated-process service.
- Flutter plugin over the C API (Android first).

Exit: a sample Android app sanitizes the corpus on an emulator in CI.

## M5: iOS and Flutter (iOS)

- XCFramework and Swift package, built and tested on GitHub Actions macOS
  runners.
- Flutter plugin iOS support.

Exit: the iOS tests pass on the simulator in CI.

## Later

- PDF through render-to-pixels (in the spirit of Dangerzone), once a
  memory-safe renderer path exists.
- Hosted SaaS on top of the self-hosted API.
- Desktop integrations (KDE file indexer, Qt image plugin, GNOME indexer) on
  the same engine.
- Upstream contributions to safe-bindings for anything kilnfile needs (unique
  crate names, image encoding, missing liblzma functions).

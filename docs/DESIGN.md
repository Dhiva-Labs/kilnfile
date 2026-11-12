# kilnfile design

kilnfile is a content disarm and reconstruction (CDR) engine. It takes an
untrusted file, parses it only with memory-safe parsers, and builds a new file
that keeps the useful content and drops everything that can carry an attack:
macros, embedded objects, external links, hidden metadata, malformed
structure. Every parser it uses comes from Rust crates, reached from C++
through [google/safe-bindings](https://github.com/google/safe-bindings).

Status: design. Nothing here is implemented yet; see [ROADMAP.md](ROADMAP.md).

## Goals

- Rebuild, don't detect. Output is constructed from parsed content, so an
  unknown exploit in the input has nothing to ride on. No signatures, no
  updates needed to handle a new attack.
- Memory-safe parsing end to end. No C or C++ parser ever touches attacker
  bytes. Parsing and encoding happen in Rust crates; the C++ layer only moves
  already-parsed values around.
- Contain what can't be prevented. Each file is processed in a sandboxed worker
  process with hard resource limits, so a logic bug or resource bomb takes down
  one worker, not the service.
- One engine, several front ends: CLI, self-hosted HTTP API, and an app SDK
  (C, Android, iOS, Flutter).

## Non-goals (v1)

- PDF. safe-bindings has no PDF parser. A render-to-pixels fallback is planned
  later (see roadmap).
- Detecting malware or classifying files. kilnfile sanitizes; it doesn't judge.
- Preserving every feature of the input. When a feature can't be rebuilt
  safely, it is removed and reported.
- A hosted SaaS. The self-hosted API is designed so one can be built on top
  later.

## Threat model

- The attacker fully controls the input bytes, the declared file name and the
  declared MIME type. kilnfile ignores the name and MIME type for decisions and
  detects the type from content.
- The attacker's goals: code execution in kilnfile, code execution in whatever
  opens the output, data exfiltration through the output (external links,
  tracking pixels, remote templates), or denial of service (zip bombs, billion
  laughs, giant images).
- Defences, in order:
  1. Memory-safe parsers (safe-bindings) for every format.
  2. Rebuild: the output contains only content kilnfile understood and chose to
     keep, re-serialized by kilnfile.
  3. Explicit limits: bytes, entries, nesting depth, compression ratio, pixel
     count, decode memory.
  4. Sandbox: the worker drops privileges before reading input (Linux: seccomp,
     Landlock, rlimits, no network, no new privileges).
- Out of scope: attacks on the host outside the worker, and side channels.

## Architecture

```
            ┌────────────────────────── kilnfile (Go) ───────────────────────────┐
 file  ───▶ │  CLI: kilnfile sanitize      HTTP API: kilnfile serve              │
            │  limits, policy, worker pool, metrics                              │
            └───────────────┬────────────────────────────────────────────────────┘
                            │ framed request/response over stdin/stdout
                            ▼
            ┌──────────── kilnfile-worker (C++20, sandboxed process) ─────────────┐
            │  sandbox setup → read request → libkilnfile → write response → exit │
            └───────────────┬─────────────────────────────────────────────────────┘
                            ▼
            ┌──────────────────────── libkilnfile (C++20) ────────────────────────┐
            │  detect type → pipeline (image │ ooxml │ archive │ structured)      │
            │  policy engine, report builder, limits                             │
            └───────┬───────────────────────────────────────────┬────────────────┘
                    ▼                                           ▼
     google/safe-bindings (vendored, pinned)        kilnfile_bridge (Rust, Crubit)
     zip, deflate, lzma, xml, kamadak_exif,         capabilities safe-bindings lacks:
     pixel_bridge, jxl, serde_json, saphyr          image encoding (PNG, JPEG, GIF)
```

The app SDK links libkilnfile directly through its C API instead of going
through the Go layer and a worker process (see SDK below).

### Why Go plus a C++ worker

- The parsers are reachable only from C++, so the engine is C++.
- Everything that isn't parsing (HTTP, configuration, pooling, metrics, CLI)
  is written in Go.
- The process boundary is the sandbox boundary. Go never links the engine, so
  a worker crash or compromise can't touch the API process.

### Worker protocol

One request per worker process in v1 (fork-per-file; a pool of pre-started
workers hides the startup cost). Frames are little-endian and length-prefixed
over stdin/stdout; stderr is for logs.

```
request:  u32 header_len | header JSON | u64 payload_len | payload bytes
response: u32 report_len | report JSON | u64 output_len  | output bytes
```

The header carries the policy and limits. The report is described below. A
worker that exits without a full response, exceeds its time budget, or is
killed by the sandbox is treated as `rejected: worker_failure`.

## Format pipelines (v1)

| Input | Parsed with | What happens | Output |
| --- | --- | --- | --- |
| PNG, JPEG, GIF, WebP, BMP, TIFF, ICO | pixel_bridge (`image` crate) | Decode with a memory cap and pixel limit, apply the EXIF orientation to the pixels, re-encode. All metadata (EXIF, XMP, IPTC, comments) is dropped. | Same format when an encoder exists (PNG, JPEG, GIF); PNG otherwise |
| JPEG XL | jxl (via pixel_bridge) | Same as above. | PNG (no JPEG XL encoder in the stack) |
| DOCX, XLSX, PPTX (and the macro-enabled `m` variants) | zip, xml | See "Office documents" below. | DOCX, XLSX or PPTX (never the macro-enabled variant) |
| ZIP | zip | Unpack with limits, sanitize every entry recursively, repack deterministically. | ZIP |
| gzip | deflate | Decompress with limits, sanitize the content, recompress. | gzip |
| xz | lzma | Decompress with a memory limit, sanitize the content, recompress. | xz |
| JSON | serde_json | Parse with depth and size limits, re-serialize canonically. | JSON |
| YAML | saphyr | Parse with depth, size and alias-expansion limits, re-serialize. | YAML |
| XML | xml (quick-xml, xmltree) | Parse with depth and size limits; no DTD, no entities, no processing instructions; re-serialize. | XML |

Anything else is an unknown type and is rejected by default (tar, for example,
has no parser in safe-bindings, so `.tar.gz` is rejected after decompression
unless policy allows passing tar through untouched).

### Office documents (OOXML)

1. Open the ZIP with limits (entry count, uncompressed size, ratio).
2. Parse `[Content_Types].xml` and every `_rels/*.rels` part.
3. Keep only parts whose content type is on an allowlist for the document type
   (body, styles, numbering, settings, themes, fonts, comments, headers and
   footers, images).
4. Remove, and report:
   - VBA projects (`vbaProject.bin`, `vbaData.xml`) and macro-enabled content
     types. `docm`/`xlsm`/`pptm` become `docx`/`xlsx`/`pptx`.
   - ActiveX controls, OLE embeddings and `oleObject` parts.
   - Every relationship with `TargetMode="External"`: remote templates,
     external images, linked data. Hyperlinks are kept as text, and their
     targets are kept only if policy allows `http`/`https` links.
   - DDE and field codes that run commands (`DDE`, `DDEAUTO`, `INCLUDETEXT`).
   - Custom UI (`customUI*.xml`), which can call macros.
5. Re-serialize every remaining XML part through the quick-xml reader and
   writer. That drops DOCTYPE, entity declarations, processing instructions and
   anything that doesn't parse.
6. Send every embedded image through the image pipeline.
7. Repack with deterministic entry order and timestamps.

### Archives

- Entry names are normalized. Absolute paths, `..`, drive letters, backslashes,
  control characters, symlinks and device entries are rejected.
- Limits apply per archive and cumulatively across nesting: maximum entries,
  total uncompressed bytes, per-entry compression ratio, nesting depth
  (default 3).
- Every entry goes back through type detection and its own pipeline. Entries
  of unknown type are removed (default) or the whole archive is rejected
  (strict policy).

## Policy

The default policy, chosen for v1, is "remove it and keep the rest":

- Dangerous parts (macros, embedded objects, external links, active fields)
  are removed and the clean remainder is returned with a report.
- A file that can't be parsed at all, or that hits a hard limit, is rejected.
- Policies can be overridden per file type (`remove`, `reject`, `allow`) in a
  YAML policy file, and per request through the API.

## Report

Every result carries a JSON report:

```json
{
  "kilnfile_version": "0.1.0",
  "result": "sanitized",
  "input": {"detected_type": "docm", "bytes": 48213, "sha256": "…"},
  "output": {"type": "docx", "bytes": 40977, "sha256": "…"},
  "actions": [
    {"action": "removed", "part": "word/vbaProject.bin", "reason": "vba_macro"},
    {"action": "removed", "part": "word/_rels/settings.xml.rels#rId1", "reason": "external_template"},
    {"action": "rewritten", "part": "word/media/image1.png", "reason": "image_reencoded"}
  ],
  "limits_hit": []
}
```

`result` is one of `sanitized`, `unchanged_semantics` (rebuilt with nothing
removed) or `rejected` (with a `reason`).

## Sandbox

- Linux (the v1 server target): the worker calls `prctl(PR_SET_NO_NEW_PRIVS)`,
  applies rlimits (address space, CPU time, file size, open files), enables
  Landlock with no filesystem access, and installs a seccomp-bpf allowlist
  (read and write on the inherited pipes, memory management, futex, clock,
  exit) before reading a single byte of input. It has no network.
- macOS and Windows: the CLI works, but without a sandbox in v1; it prints a
  warning.
- Android: the SDK runs the engine in an `android:isolatedProcess` service, the
  closest Android equivalent of iOS BlastDoor.
- iOS: third-party apps can't spawn sandboxed helper processes, so the engine
  runs in-process. Memory-safe parsing is the main defence there, and the SDK
  docs say so.

## App SDK

- C API (`kilnfile.h`): `kf_sanitize(input, len, policy_json, &output,
  &output_len, &report_json)` plus free functions. This is the stable ABI that
  every other SDK wraps.
- Android: AAR with a Kotlin API and a JNI layer. Sanitizing happens in an
  isolated-process service.
- iOS: XCFramework plus a Swift package, built and tested on GitHub Actions
  macOS runners.
- Flutter: a Dart FFI plugin over the C API, using the Android and iOS
  libraries.

## Build

- safe-bindings is vendored as a git submodule pinned to a commit. Crubit is
  pinned to a commit, and the Rust nightly to the date Crubit's CI passed
  with. All three live in one `toolchain.env`, the way safe-bindings' own CI
  resolves them.
- A CMake superbuild compiles the safe-bindings wrappers, kilnfile_bridge and
  libkilnfile. A bootstrap script installs the pinned toolchain.
- **Known problem, resolved in milestone 0:** every safe-bindings crate is
  named `rust`, so the generated C++ bindings all live in namespace `rust::`
  and each binding is its own staticlib with its own copy of the Rust standard
  library. Two bindings can't be linked into one binary as they are. The
  candidate fixes:
  - A. One umbrella crate that depends on the binding crates (renamed) and
    produces a single staticlib and Crubit header, with the vendored C++
    wrappers patched to the new namespaces. Needed for the SDK, which has to
    be one library.
  - B. One worker binary per binding, chained by the Go layer (for example:
    zip worker unpacks, xml worker sanitizes parts, zip worker repacks). No
    patching, stronger isolation, more IPC. Works for CLI and API only.
  - Upstream: ask safe-bindings to give each crate a unique name, which would
    make A a no-op.

## Repository layout

```
engine/      libkilnfile (C++20): detect, pipelines, policy, report
bridge/      kilnfile_bridge (Rust + Crubit): image encoding
worker/      kilnfile-worker (C++20): sandbox + protocol
cmd/         Go: kilnfile CLI and HTTP server
internal/    Go: worker pool, protocol, policy loading
sdk/         c/, android/, ios/, flutter/
third_party/ safe-bindings (submodule)
testdata/    generated corpus: benign and hostile samples
fuzz/        fuzz targets for the worker entry point and each pipeline
docs/        this document, roadmap, policy reference
```

## Testing

- Unit tests per pipeline (GoogleTest) and per Go package.
- A generated corpus of hostile samples: macro documents, external templates,
  DDE fields, zip bombs (flat and nested), path traversal archives, billion
  laughs in YAML and XML, deeply nested JSON, decompression bombs in gzip and
  xz, images with oversized dimensions, polyglot files. Generators are checked
  in, not the samples, so nothing dangerous sits in the repo as-is.
- Fuzzing: libFuzzer targets for the worker entry point and each pipeline, run
  briefly in CI and longer on a schedule.
- End-to-end tests drive the CLI and the HTTP API against the corpus and
  assert on both the output and the report.

## Legal

- License: Apache-2.0, the same as safe-bindings.
- Patents: Glasswall holds a CDR patent family ("Resisting the spread of
  unwanted code and data", priority 2005). Several members have lapsed; at
  least one later US member is marked active. Get a freedom-to-operate opinion
  from a patent attorney before any commercial or SaaS use.
- Name: "kilnfile" was checked against GitHub, crates.io, pub.dev, npm and web
  and trademark aggregators, with no conflict found. A formal trademark search
  (USPTO, IP India, WIPO; classes 9 and 42) is still to be done.

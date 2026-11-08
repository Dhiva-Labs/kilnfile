# kilnfile

Memory-safe content disarm and reconstruction (CDR).

kilnfile takes an untrusted file, such as an upload, an email attachment or a
message sticker, and rebuilds it from scratch. Macros, embedded objects,
external links, hidden metadata and malformed structure don't survive the
rebuild. Every parser it uses is a Rust crate, reached from C++ through
[google/safe-bindings](https://github.com/google/safe-bindings), so no C or C++
code ever parses attacker-controlled bytes.

**Status: design stage.** Nothing is implemented yet.

- [Design](docs/DESIGN.md): architecture, threat model, format pipelines,
  sandbox, SDK.
- [Roadmap](docs/ROADMAP.md): milestones and exit criteria.

## Planned v1

- Formats: images (PNG, JPEG, GIF, WebP, BMP, TIFF, ICO, JPEG XL), Office
  documents (DOCX, XLSX, PPTX), archives (ZIP, gzip, xz), and JSON, YAML, XML.
- Front ends: a CLI, a self-hosted HTTP API, and an app SDK (C, Android, iOS,
  Flutter).
- Default policy: remove what's dangerous, keep the rest, and report exactly
  what was removed. Files that can't be parsed are rejected.

## License

Apache-2.0. See [LICENSE](LICENSE).

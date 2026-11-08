# Test corpus

The repo checks in generators, not samples. `tools/gencorpus` writes benign and
hostile files at test time, plus a `manifest.json` that says what kilnfile
should do with each one. Nothing generated is functional malware: macros and
OLE/ActiveX parts are dummy bytes, commands are inert strings, URLs use the
`.invalid` TLD.

```
go run ./tools/gencorpus -out testdata/generated        # bounded bombs, about 3 MB
go run ./tools/gencorpus -out testdata/generated -large # full-size bombs (GBs uncompressed, about 10 MB on disk, ~20 s)
```

`testdata/generated/` is gitignored. Output is byte-for-byte deterministic
(fixed timestamps, no randomness). The tool is its own Go module; run its
tests with `cd tools/gencorpus && go test ./...`.

## Manifest

A JSON list of `{path, family, description, expected}`. `family` is `image`,
`ooxml`, `archive` or `structured`. `expected`:

- `result`: `sanitized`, `unchanged_semantics` or `rejected` (PROTOCOL.md).
- `reason`: the rejection reason, empty otherwise.
- `must_remove`: action reasons the report must contain.
- `must_not_contain`: byte strings or part names absent from the output. A
  plain string is its UTF-8 bytes; `hex:` followed by hex digits is raw bytes.
  For ZIP-based outputs, check part names in the central directory and parse
  the unpacked parts for content strings.
- `must_contain` (optional): the same, but must be present (kept content).

Action reasons not already in DESIGN.md/PROTOCOL.md: `activex`,
`ole_embedding`, `dde_field`, `custom_ui`, `dtd`, `processing_instruction`,
`metadata_removed`, `trailing_data`, `unsafe_path`, `symlink`, `unknown_type`
(an archive entry with no pipeline). The engine should adopt these names or the
manifest should be changed to match.

## Samples

- images (10): benign PNG/JPEG/GIF/WebP/BMP, EXIF GPS+model JPEG, EXIF
  orientation 6 JPEG, 100000x100000 PNG, truncated PNG, JPEG+ZIP polyglot.
- ooxml (11): benign DOCX/XLSX/PPTX, DOCM with dummy VBA, external
  attachedTemplate, DDEAUTO, ActiveX, OLE object, customUI DOCM, DOCTYPE/XXE,
  DOCX with a PNG.
- archives (15): benign ZIPs, flat and nested ZIP bombs, four path traversal
  ZIPs, symlink ZIP, 20000-entry ZIP, gzip and xz (benign and bomb), `.tar.gz`.
- structured (9): benign JSON/YAML/XML, 10000-deep JSON, YAML alias bomb, XML
  billion laughs, XXE, processing instruction, malformed JSON.

# Worker protocol

This is the contract between the Go front end (`kilnfile` CLI and server) and
`kilnfile-worker` (the sandboxed C++ process). Both sides implement exactly
this. The SDK doesn't use it; it calls the C API in-process.

## Process model

- The Go side starts one worker process per file, with stdin and stdout as
  pipes and stderr captured for logs. The command line is
  `kilnfile-worker --protocol 1`.
- The worker reads one request from stdin, writes one response to stdout, and
  exits with status 0.
- Anything else counts as a failure, and the Go side reports it as
  `rejected` with reason `worker_failure`:
  - a non-zero exit, a signal, or a timeout (the Go side kills the worker);
  - a short or oversized frame, or invalid JSON;
  - an output larger than `limits.max_output_bytes`.

## Framing

All integers are unsigned, little-endian.

```
request  = u32 header_len | header (JSON, UTF-8) | u64 payload_len | payload
response = u32 report_len | report (JSON, UTF-8) | u64 output_len  | output
```

- `header_len` and `report_len` must be at most 1 MiB (1048576).
- `payload_len` must be at most `limits.max_input_bytes`.
- `output_len` must be at most `limits.max_output_bytes`, and is 0 when the
  result is `rejected`.
- The worker reads exactly the declared bytes. It treats a short read (EOF
  early) as a failure and exits non-zero without writing a response.

## Request header

```json
{
  "protocol": 1,
  "policy": {
    "on_dangerous": "remove",
    "on_unknown_type": "reject",
    "allow_http_links": false,
    "keep_icc_profile": false,
    "animated_images": "first_frame",
    "types": {
      "ooxml": {"on_dangerous": "reject"}
    }
  },
  "limits": {
    "max_input_bytes": 104857600,
    "max_output_bytes": 209715200,
    "max_decoded_pixels": 100000000,
    "max_decode_memory_bytes": 536870912,
    "max_nesting_depth": 3,
    "max_archive_entries": 10000,
    "max_total_uncompressed_bytes": 1073741824,
    "max_compression_ratio": 100,
    "max_structured_depth": 256
  }
}
```

- `on_dangerous`: `remove` (default) strips the dangerous part and keeps the
  rest; `reject` rejects the whole file; `allow` keeps it. `allow` exists for
  testing and trusted pipelines only.
- `on_unknown_type`: `reject` (default) or `allow` (pass the bytes through
  untouched; only for trusted pipelines).
- `types` overrides `on_dangerous` per type family: `image`, `ooxml`,
  `archive`, `structured`.
- `animated_images`: `first_frame` (default) or `keep_animation`.
- Unknown fields are ignored, so either side can grow without breaking.

The values above are the defaults. The Go side always sends a complete header,
with defaults filled in, so the worker never has to guess.

## Report

```json
{
  "kilnfile_version": "0.1.0",
  "result": "sanitized",
  "reason": "",
  "input": {"detected_type": "docm", "bytes": 48213, "sha256": "…"},
  "output": {"type": "docx", "bytes": 40977, "sha256": "…"},
  "actions": [
    {"action": "removed", "part": "word/vbaProject.bin", "reason": "vba_macro"}
  ],
  "limits_hit": []
}
```

- `result`: `sanitized` (something was removed or rewritten),
  `unchanged_semantics` (rebuilt with nothing removed), or `rejected`.
- `reason`: empty unless `result` is `rejected`. Values include
  `unknown_type`, `unparseable`, `limit_exceeded`, `dangerous_content`
  (policy `reject`) and `worker_failure` (set by the Go side only).
- `output` is `null` when rejected.
- `actions[].action`: `removed`, `rewritten`, or `rejected` (for an archive
  entry rejected inside an archive that was otherwise kept).
- `limits_hit`: names of the `limits` fields that were reached.

# kilnfile CLI

```
kilnfile sanitize <input> -o <output> [--policy p.yaml] [--report r.json]
                  [--worker PATH] [--timeout 30s] [--force]
kilnfile version
```

- Worker lookup: `--worker`, then `$KILNFILE_WORKER`, then `kilnfile-worker`
  next to the `kilnfile` executable, then `PATH`.
- The policy file is YAML with the same snake_case names as the request header
  (see PROTOCOL.md), merged over the defaults. Limits go under a `limits:` key.
  Unknown fields and invalid values are errors.
- The output is written atomically and never replaces an existing file unless
  `--force` is given. Nothing is written when the file is rejected.
- `--report` writes the JSON report (also for rejected files). A one-line
  summary always goes to stderr.
- An input larger than `max_input_bytes` is rejected with `limit_exceeded`.

Exit codes: 0 sanitized or unchanged_semantics, 2 rejected (including
`worker_failure`), 1 usage or internal error.

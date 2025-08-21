# Function Reference

List of functions exposed by `mcp-shell`.

| Function | Arguments | Output | Description |
| --- | --- | --- | --- |
| `GET /healthz` | none | `{status:"ok", name, version, uptime}` | Basic liveness probe |
| `GET /readyz` | none | `{status:"ok", name, version, uptime}` | Readiness probe |
| `GET /mcp/health` | none | `{status:"ok", name, version, uptime}` | MCP-native health endpoint |
| `shell.exec` | `cmd` (string, required), `cwd?`, `env?`, `timeout_ms?`, `stdin?`, `max_bytes?`, `dry_run?` | `{stdout, stderr, exit_code, duration_ms, stdout_truncated, stderr_truncated, error?}` | Execute a shell command in the container |
| `fs.list` | `path` (string), `glob?`, `include_hidden?`, `max_entries?` | `{entries:[{name,type,size,mtime,mode}], duration_ms, error?}` | List directory entries |
| `fs.stat` | `path` (string) | `{type,size,mode,mtime,uid,gid,symlink_target?,duration_ms,error?}` | File or directory metadata |
| `fs.read` | `path` (string), `max_bytes?`, `start_offset?` | `{content, truncated, duration_ms, error?}` | Read UTF-8 text file |
| `fs.read_b64` | `path` (string), `max_bytes?`, `start_offset?` | `{content_b64, truncated, duration_ms, error?}` | Read file as base64 |
| `fs.write` | `path`, `content?`, `content_b64?`, `mode?`, `create_parents?`, `append?`, `dry_run?` | `{bytes_written, duration_ms, error?}` | Write a file |
| `fs.remove` | `path`, `recursive?` | `{removed, duration_ms, error?}` | Remove file or directory |
| `fs.mkdir` | `path`, `parents?`, `mode?` | `{created, duration_ms, error?}` | Create directory |
| `fs.move` | `src`, `dest`, `overwrite?`, `parents?` | `{moved, duration_ms, error?}` | Move or rename a file |
| `fs.copy` | `src`, `dest`, `overwrite?`, `parents?`, `recursive?` | `{copied, duration_ms, error?}` | Copy a file or directory |
| `fs.search` | `path`, `query`, `regex?`, `glob?`, `case_sensitive?`, `max_results?` | `{matches:[{file,line,byte_offset,preview}], duration_ms, error?}` | Search file contents using ripgrep (requires `rg`) |
| `text.diff` | `a`, `b`, `algo?` (`myers`\|`patience`) | `{unified_diff, duration_ms, error?}` | Compute unified diff between two strings |
| `text.apply_patch` | `path`, `unified_diff`, `dry_run?` | `{patched, hunks_applied, hunks_failed, duration_ms, error?}` | Apply a unified diff patch to a file |

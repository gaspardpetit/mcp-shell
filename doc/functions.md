# Function Reference

List of functions exposed by `mcp-shell`.

| Function | Arguments | Output | Description |
| --- | --- | --- | --- |
| `GET /healthz` | none | `{status:"ok", name, version, uptime}` | Basic liveness probe |
| `GET /readyz` | none | `{status:"ok", name, version, uptime}` | Readiness probe |
| `GET /mcp/health` | none | `{status:"ok", name, version, uptime}` | MCP-native health endpoint |
| `shell.exec` | `cmd` (string, required), `cwd?`, `env?`, `timeout_ms?`, `stdin?`, `max_bytes?`, `dry_run?` | `{stdout, stderr, exit_code, duration_ms, stdout_truncated, stderr_truncated, error?}` | Execute a shell command in the container |


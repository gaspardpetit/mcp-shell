# mcp-shell

`mcp-shell` is a containerized **Model Context Protocol (MCP) server** exposing a rich set of tools an LLM can call. It’s built to be capable (document/code tooling, search/convert, shell) yet contained inside a Docker sandbox you control.

**Transports:** `http` (streamable, default) · `sse` · `stdio`  
**Presets:** `minimal` · `standard` · `full`

---

## Overview

- **Purpose**: Let an LLM run commands, inspect/transform files, process documents (Word/Excel/PowerPoint/PDF), and use common CLIs (Python, Node.js, Git, jq/yq, ripgrep, ImageMagick, ffmpeg, Tesseract, Pandoc, Poppler, DuckDB CLI, etc.).
- **Primary tools**: `shell.exec`, `python.run`, `node.run`, `sh.script.write_and_run`, package managers (`apt.install`, `pip.install`, `npm.install`), `git.*` (clone, status, commit, pull, push, etc.), `fs.*` (list, stat, read, write, search, hash, etc.), `archive.*`, text utilities like `text.diff` and `text.apply_patch`, document helpers such as `doc.convert`, `pdf.extract_text`, `spreadsheet.to_csv`, `doc.metadata`, media tools like `image.convert`, `video.transcode`, `ocr.extract`, and web tools like `http.request`, `web.download`, `web.search`, and `md.fetch`, and process tools like `proc.spawn`, `proc.stdin`, `proc.wait`, `proc.kill`, `proc.list`.
- **Dependencies**: `fs.search` relies on the `rg` binary (ripgrep); document tools rely on `pandoc`.
  Development dependencies are listed in `scripts/deps.txt` and can be installed via `scripts/install-deps.sh`.
- **Function reference**: see [doc/functions.md](doc/functions.md) for supported functions.
- **Security model**: Execution is confined to a non-root user in a container. You control:
  - Host mounts (read-only vs read-write).
  - Network egress (enable/disable at run-time).
  - Resource limits (CPU, RAM, pids).
- **Auditability**: Tool calls are JSONL-logged to `/logs/mcp-shell.log` (when `/logs` is mounted). Default caps: timeout 60s; 1 MiB per stream (stdout/stderr).

---

## Build

Tool layers are prebuilt and published with a `tools-` tag prefix. The main
Dockerfile simply adds the compiled server on top of one of these layers:

```bash
# Standard image using the latest tool layer
docker build -t mcp-shell:std \
  --build-arg BASE_IMAGE=ghcr.io/<owner>/mcp-shell:tools-std-latest .

# Light image
docker build -t mcp-shell:mini \
  --build-arg BASE_IMAGE=ghcr.io/<owner>/mcp-shell:tools-light-latest .

# Full image
docker build -t mcp-shell:full \
  --build-arg BASE_IMAGE=ghcr.io/<owner>/mcp-shell:tools-full-latest .
```

To rebuild the underlying tool layer itself (usually only needed when the
tooling changes), use `Dockerfile.tools`:

```bash
docker build -f Dockerfile.tools -t ghcr.io/<owner>/mcp-shell:tools-std \
  --build-arg PRESET=standard .
```

Optional OCR languages or locales (meaningful for `standard`/`full`) apply when
building the tool layer:

```bash
# Extra OCR languages (English `eng` is always installed)
docker build -f Dockerfile.tools -t ghcr.io/<owner>/mcp-shell:tools-std \
  --build-arg PRESET=standard \
  --build-arg TESS_LANGS_EXTRA="tesseract-ocr-fra tesseract-ocr-deu" .

# Extra locales (English en_US.UTF-8 is always enabled)
docker build -f Dockerfile.tools -t ghcr.io/<owner>/mcp-shell:tools-std \
  --build-arg PRESET=standard \
  --build-arg EXTRA_LOCALES="fr_CA.UTF-8 de_DE.UTF-8" .
```

---

## Run

### A) Service mode (HTTP, default)

The image defaults to `--transport=http`, listening on `${PORT:-3333}` and serving `/mcp` plus `/healthz`.

```bash
docker run --rm -d --name mcp-shell \
  --user 10001:10001 \
  --read-only \
  --tmpfs /tmp:rw,nosuid,nodev,size=512m \
  --tmpfs /run:rw,nosuid,nodev,size=64m \
  --cap-drop=ALL \
  --security-opt no-new-privileges \
  --pids-limit=2048 \
  --memory=4g --cpus=2 \
  --init \
  -e EGRESS=1 \
  -p 3333:3333 \
  -v "$PWD/workspace":/workspace:rw \
  -v "$PWD/logs":/logs:rw \
  mcp-shell:std
```

Notes:
- Mount something into `/workspace` if you want `shell.exec` to `ls` real files.
- `EGRESS=1` just sets intent for your server/tools; actual network policy is up to how you run Docker.
- Package managers (`apt.install`, `pip.install`, `npm.install`) are disabled unless `EGRESS=1` or the server is started with `--allow-pkg`.

### B) Air-gapped mode (STDIO)

If you prefer no network at all, keep a container idling and start MCP sessions via stdio:

```bash
# Idle container with no network
docker run -d --name mcp-stdio --rm \
  --user 10001:10001 \
  --read-only \
  --tmpfs /tmp:rw,nosuid,nodev,size=512m \
  --tmpfs /run:rw,nosuid,nodev,size=64m \
  --cap-drop=ALL \
  --security-opt no-new-privileges \
  --pids-limit=2048 \
  --memory=4g --cpus=2 \
  --network=none \
  --init \
  -e EGRESS=0 \
  -v "$PWD/workspace":/workspace:rw \
  -v "$PWD/logs":/logs:rw \
  mcp-shell:std tail -f /dev/null

# Start a one-off MCP stdio session (single client)
docker exec -i mcp-stdio /app/mcp-server --transport=stdio
```

---

## Healthcheck

In HTTP mode the container exposes:

- `GET /healthz` (top-level)
- `GET /mcp/health` (built-in)

The Docker `HEALTHCHECK` probes those endpoints internally; no action required.

Quick manual check:
```bash
curl -fsS http://127.0.0.1:3333/healthz | jq
```

---

## Testing (HTTP / streamable)

Streamable HTTP is **session-based**. First call `initialize` to get a `Mcp-Session-Id`, then send `notifications/initialized`, and reuse the session for subsequent requests.

```bash
# 1) Initialize (creates session)
INIT_RES_HEADERS=$(mktemp)
curl -sS -D "$INIT_RES_HEADERS" \
  -H 'content-type: application/json' \
  -H 'accept: application/json' \
  -X POST http://127.0.0.1:3333/mcp/ \
  -d '{
    "jsonrpc":"2.0",
    "id":"1",
    "method":"initialize",
    "params": { "protocolVersion": "2025-06-18", "capabilities": {} }
  }' >/dev/null

# Extract session id from response headers
SID=$(awk -F': ' 'BEGIN{IGNORECASE=1} /^Mcp-Session-Id:/ {print $2}' "$INIT_RES_HEADERS" | tr -d '\r')
echo "Session: $SID"

# 2) Notify ready
curl -sS -H 'content-type: application/json' \
  -H "Mcp-Session-Id: $SID" \
  -X POST http://127.0.0.1:3333/mcp/ \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized"}' >/dev/null

# 3) List tools (expect "shell.exec" and "fs.*" tools)
curl -sS -H 'content-type: application/json' -H "Mcp-Session-Id: $SID" \
  -X POST http://127.0.0.1:3333/mcp/ \
  -d '{"jsonrpc":"2.0","id":"2","method":"tools/list"}' | jq .

# 4) Run a command
curl -sS -H 'content-type: application/json' -H "Mcp-Session-Id: $SID" \
  -X POST http://127.0.0.1:3333/mcp/ \
  -d '{
    "jsonrpc":"2.0","id":"3","method":"tools/call",
    "params":{"name":"shell.exec","arguments":{"cmd":"ls -la","cwd":"/workspace"}}
  }' | jq .
```

**SSE** variant: start with `--transport=sse`, open the event stream (`GET /mcp/sse`), then POST messages to `/mcp/message` with the same session id.

**STDIO** smoke test:
```bash
docker run -i --rm mcp-shell:std --transport=stdio <<'JSON'
{"jsonrpc":"2.0","id":"1","method":"tools/list"}
{"jsonrpc":"2.0","id":"2","method":"tools/call","params":{"name":"shell.exec","arguments":{"cmd":"ls -la","cwd":"/workspace"}}}
JSON
```

---

## Project Purpose

A practical **LLM toolbox** MCP server:
- **Capability**: broad, pragmatic CLIs and libraries that cover 90% of real tasks.
- **Safety**: non-root user, read-only rootfs option, least privilege, and explicit mounts.
- **Extensibility**: add new tools or apt/pip/npm packages as needed.

---

## Disclaimer

Use at your own risk — you decide what mounts, network, and limits to grant. If you enable network access, the agent may reach internal hosts visible to the container. Keep it in a container; don’t run it directly on your host unless you truly know what you’re doing.

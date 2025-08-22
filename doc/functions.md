# Function Reference

List of functions exposed by `mcp-shell`.

| Function | Arguments | Output | Description |
| --- | --- | --- | --- |
| `GET /healthz` | none | `{status:"ok", name, version, uptime}` | Basic liveness probe |
| `GET /readyz` | none | `{status:"ok", name, version, uptime}` | Readiness probe |
| `GET /mcp/health` | none | `{status:"ok", name, version, uptime}` | MCP-native health endpoint |
| `shell.exec` | `cmd` (string, required), `cwd?`, `env?`, `timeout_ms?`, `stdin?`, `max_bytes?`, `dry_run?` | `{stdout, stderr, exit_code, duration_ms, stdout_truncated, stderr_truncated, error?}` | Execute a shell command in the container |
| `python.run` | `code` (string, required), `args?`, `stdin?`, `venv?{name?,create_if_missing?}`, `packages?`, `timeout_ms?`, `max_bytes?` | `{stdout, stderr, exit_code, duration_ms, stdout_truncated, stderr_truncated, artifacts?, error?}` | Execute Python code, optionally in a virtual environment |
| `node.run` | `code` (string, required), `args?`, `stdin?`, `packages?`, `timeout_ms?`, `max_bytes?` | `{stdout, stderr, exit_code, duration_ms, stdout_truncated, stderr_truncated, artifacts?, error?}` | Execute Node.js code |
| `sh.script.write_and_run` | `shebang` (string, required), `content` (string, required), `cwd?`, `env?`, `timeout_ms?`, `max_bytes?` | `{stdout, stderr, exit_code, duration_ms, stdout_truncated, stderr_truncated, error?}` | Write a script to a temp file and run it |
| `apt.install` | `packages` (array, required), `update?`, `assume_yes?`, `timeout_ms?`, `max_bytes?`, `dry_run?` | `{installed, stdout, stderr, exit_code, duration_ms, stdout_truncated, stderr_truncated, error?}` | Install system packages via apt-get |
| `pip.install` | `packages` (array, required), `venv?{name?,create_if_missing?}`, `timeout_ms?`, `max_bytes?`, `dry_run?` | `{installed, stdout, stderr, exit_code, duration_ms, stdout_truncated, stderr_truncated, error?}` | Install Python packages via pip |
| `npm.install` | `packages` (array, required), `global?`, `timeout_ms?`, `max_bytes?`, `dry_run?` | `{installed, stdout, stderr, exit_code, duration_ms, stdout_truncated, stderr_truncated, error?}` | Install Node.js packages via npm |
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
| `fs.hash` | `path`, `algo` (`sha256`\|`sha1`\|`md5`) | `{hash, duration_ms, error?}` | Compute a file checksum |
| `archive.zip` | `src`, `dest`, `include?`, `exclude?` | `{archive_path, files, duration_ms, error?}` | Create a zip archive |
| `archive.unzip` | `src`, `dest`, `include?`, `exclude?` | `{extracted, files, duration_ms, error?}` | Extract a zip archive |
| `archive.tar` | `src`, `dest`, `include?`, `exclude?` | `{archive_path, files, duration_ms, error?}` | Create a tar archive |
| `archive.untar` | `src`, `dest`, `include?`, `exclude?` | `{extracted, files, duration_ms, error?}` | Extract a tar archive |
| `text.diff` | `a`, `b`, `algo?` (`myers`\|`patience`) | `{unified_diff, duration_ms, error?}` | Compute unified diff between two strings |
| `text.apply_patch` | `path`, `unified_diff`, `dry_run?` | `{patched, hunks_applied, hunks_failed, duration_ms, error?}` | Apply a unified diff patch to a file |
| `doc.convert` | `src_path`, `dest_format`, `options?` | `{dest_path,size,duration_ms,error?}` | Convert documents via LibreOffice or Pandoc |
| `pdf.extract_text` | `path`, `layout?` (`raw`\|`layout`\|`html`), `max_bytes?` | `{text,truncated,duration_ms,error?}` | Extract text from a PDF |
| `spreadsheet.to_csv` | `path`, `sheet?` (name or index), `max_bytes?` | `{csv,truncated,duration_ms,error?}` | Convert a spreadsheet sheet to CSV |
| `doc.metadata` | `path` | `{mime,pages?,words?,created?,modified?,duration_ms,error?}` | Retrieve document metadata |
| `image.convert` | `src_path`, `dest_path`, `ops?[{resize?,crop?,format?,quality?}]` | `{dest_path,duration_ms,error?}` | Convert or transform images via ImageMagick |
| `video.transcode` | `src`, `dest`, `codec?`, `crf?`, `start?`, `duration?` | `{dest,duration_ms,error?}` | Transcode video files via ffmpeg |
| `ocr.extract` | `path`, `lang?`, `max_bytes?` | `{text,truncated,duration_ms,error?}` | Extract text from images via Tesseract |
| `git.clone` | `repo` (string, required), `dir?`, `depth?`, `timeout_ms?`, `max_bytes?`, `dry_run?` | `{stdout, stderr, exit_code, duration_ms, stdout_truncated, stderr_truncated, error?}` | Clone a git repository |
| `git.status` | `path` (string, required), `timeout_ms?`, `max_bytes?` | `{stdout, stderr, exit_code, duration_ms, stdout_truncated, stderr_truncated, error?}` | Git status (porcelain) |
| `git.commit` | `path` (string, required), `message` (string, required), `all?`, `timeout_ms?`, `max_bytes?`, `dry_run?` | `{stdout, stderr, exit_code, duration_ms, stdout_truncated, stderr_truncated, commit?, error?}` | Commit changes |
| `git.pull` | `path` (string, required), `rebase?`, `timeout_ms?`, `max_bytes?`, `dry_run?` | `{stdout, stderr, exit_code, duration_ms, stdout_truncated, stderr_truncated, error?}` | Pull latest changes |
| `git.push` | `path` (string, required), `remote?`, `branch?`, `timeout_ms?`, `max_bytes?`, `dry_run?` | `{stdout, stderr, exit_code, duration_ms, stdout_truncated, stderr_truncated, error?}` | Push commits (requires `GIT_ALLOW_PUSH=1`) |
| `git.checkout` | `path` (string, required), `ref` (string, required), `create?`, `timeout_ms?`, `max_bytes?` | `{stdout, stderr, exit_code, duration_ms, stdout_truncated, stderr_truncated, error?}` | Checkout a git ref |
| `git.branch` | `path` (string, required), `name?`, `delete?`, `list?`, `timeout_ms?`, `max_bytes?` | `{stdout, stderr, exit_code, duration_ms, stdout_truncated, stderr_truncated, branches?, error?}` | Manage branches |
| `git.tag` | `path` (string, required), `name?`, `delete?`, `list?`, `timeout_ms?`, `max_bytes?` | `{stdout, stderr, exit_code, duration_ms, stdout_truncated, stderr_truncated, tags?, error?}` | Manage tags |
| `git.lfs.install` | `path` (string, required), `timeout_ms?`, `max_bytes?`, `dry_run?` | `{stdout, stderr, exit_code, duration_ms, stdout_truncated, stderr_truncated, error?}` | Install Git LFS in a repository |
| `http.request` | `method` (string), `url` (string), `headers?`, `body?`, `body_b64?`, `timeout_ms?`, `max_bytes?`, `allow_insecure_tls?` | `{status, headers, body?, body_b64?, truncated, duration_ms, error?}` | Perform an HTTP request |
| `web.download` | `url` (string), `dest_path` (string), `expected_sha256?`, `timeout_ms?`, `allow_insecure_tls?` | `{path, size, sha256, duration_ms, error?}` | Download a file from the web |
| `web.search` | `query` (string), `num_results?`, `engines?`, `safesearch?`, `time_range?`, `language?`, `timeout_ms?` | `{results:[{title,url,snippet,published?,source}],duration_ms,error?}` | Metasearch via SearxNG |
| `md.fetch` | `url` (string), `timeout_ms?`, `max_bytes?`, `allow_insecure_tls?`, `render_js?`, `save_artifacts?` | `{title?,byline?,site_name?,published?,canonical_url?,markdown,truncated,artifacts?,duration_ms,error?}` | Fetch webpage and extract main content as Markdown |
| `proc.spawn` | `cmd` (string, required), `args?`, `cwd?`, `env?`, `tty?` | `{pid, duration_ms, error?}` | Spawn a long-running process |
| `proc.stdin` | `pid` (int, required), `data` (string, required) | `{bytes_written, duration_ms, error?}` | Write to stdin of a spawned process |
| `proc.wait` | `pid` (int, required), `timeout_ms?` | `{exit_code, stdout?, stderr?, truncated, duration_ms, error?}` | Wait for a spawned process to exit |
| `proc.kill` | `pid` (int, required), `signal?` (int) | `{killed, duration_ms, error?}` | Send a signal to a spawned process |
| `proc.list` | none | `{processes:[{pid,cmdline,start_time,cwd}], duration_ms, error?}` | List spawned processes |

// internal/shell/tools.go
package shell

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Tunables
const (
	DefaultTimeout  = 60 * time.Second
	DefaultMaxIO    = 1 << 20 // 1 MiB per stream (stdout/stderr)
	DefaultMaxStdin = 1 << 20 // 1 MiB stdin cap
	LogPath         = "/logs/mcp-shell.log"
)

var (
	denyPatterns  []*regexp.Regexp
	allowPatterns []*regexp.Regexp
)

func init() {
	if v := os.Getenv("SHELL_EXEC_DENY"); v != "" {
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if re, err := regexp.Compile(p); err == nil {
				denyPatterns = append(denyPatterns, re)
			}
		}
	}
	if v := os.Getenv("SHELL_EXEC_ALLOW"); v != "" {
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if re, err := regexp.Compile(p); err == nil {
				allowPatterns = append(allowPatterns, re)
			}
		}
	}
}

func allowed(cmd string) bool {
	if len(allowPatterns) > 0 {
		ok := false
		for _, re := range allowPatterns {
			if re.MatchString(cmd) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, re := range denyPatterns {
		if re.MatchString(cmd) {
			return false
		}
	}
	return true
}

type ExecRequest struct {
	Cmd       string            `json:"cmd"` // required
	Cwd       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	TimeoutMs int               `json:"timeout_ms,omitempty"`
	Stdin     string            `json:"stdin,omitempty"`
	MaxBytes  int64             `json:"max_bytes,omitempty"` // per stream (stdout/stderr)
	DryRun    bool              `json:"dry_run,omitempty"`
}

type ExecResponse struct {
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	DurationMs      int64  `json:"duration_ms"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
	Error           string `json:"error,omitempty"`
}

func Run(ctx context.Context, in ExecRequest) ExecResponse {
	if in.Cmd == "" {
		return ExecResponse{ExitCode: 127, Error: "cmd is required"}
	}

	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	limit := DefaultMaxIO
	if in.MaxBytes > 0 {
		limit = int(in.MaxBytes)
	}
	stdinCap := DefaultMaxStdin

	start := time.Now()
	if !allowed(in.Cmd) {
		resp := ExecResponse{
			Stderr:     "command blocked by policy",
			ExitCode:   126,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      "command blocked",
		}
		_ = audit(in, resp, "")
		return resp
	}
	if in.DryRun {
		resp := ExecResponse{
			Stdout:     "[dry_run] would execute: " + in.Cmd,
			ExitCode:   0,
			DurationMs: time.Since(start).Milliseconds(),
		}
		_ = audit(in, resp, "")
		return resp
	}

	// Deadline-bound context for the subprocess
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-lc", in.Cmd)

	// Working directory: default to $WORKSPACE if not provided
	if in.Cwd != "" {
		cmd.Dir = filepath.Clean(in.Cwd)
	} else if ws := os.Getenv("WORKSPACE"); ws != "" {
		cmd.Dir = ws
	}

	// Merge environment
	if len(in.Env) > 0 {
		env := os.Environ()
		for k, v := range in.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = env
	}

	// Set a separate process group so we can kill the whole tree on timeout
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Stdin (capped)
	if in.Stdin != "" {
		stdin := []byte(in.Stdin)
		if len(stdin) > stdinCap {
			stdin = stdin[:stdinCap]
		}
		cmd.Stdin = bytes.NewReader(stdin)
	}

	// Stdout/stderr (capped)
	var (
		stdoutBuf, stderrBuf     bytes.Buffer
		stdoutTrunc, stderrTrunc bool
	)
	cmd.Stdout = &limitedWriter{buf: &stdoutBuf, limit: limit, truncated: &stdoutTrunc}
	cmd.Stderr = &limitedWriter{buf: &stderrBuf, limit: limit, truncated: &stderrTrunc}

	exit := 0
	runErr := cmd.Run()
	if runErr != nil {
		// If the context timed out/cancelled, nuke the whole process group
		// (negative PGID targets the group)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			exit = 124 // conventional timeout code
		} else {
			var ee *exec.ExitError
			if errors.As(runErr, &ee) {
				exit = ee.ExitCode()
			} else {
				exit = 1
			}
		}
	}

	resp := ExecResponse{
		Stdout:          stdoutBuf.String(),
		Stderr:          stderrBuf.String(),
		ExitCode:        exit,
		DurationMs:      time.Since(start).Milliseconds(),
		StdoutTruncated: stdoutTrunc,
		StderrTruncated: stderrTrunc,
	}
	if exit == 124 && resp.Stderr == "" {
		resp.Stderr = "timed out"
	}

	_ = audit(in, resp, cmd.Dir) // best-effort
	return resp
}

// limitedWriter caps the number of bytes written into an underlying buffer.
// When the cap is exceeded, it discards the remainder and marks as truncated.
type limitedWriter struct {
	buf       *bytes.Buffer
	limit     int
	truncated *bool
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.limit <= 0 {
		return w.buf.Write(p)
	}
	remain := w.limit - w.buf.Len()
	if remain <= 0 {
		*w.truncated = true
		return len(p), nil
	}
	if len(p) <= remain {
		return w.buf.Write(p)
	}
	_, _ = w.buf.Write(p[:remain])
	*w.truncated = true
	return len(p), nil
}

// audit writes a single JSONL line; failures are ignored by design.
func audit(in ExecRequest, out ExecResponse, cwd string) error {
	if LogPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(LogPath), 0o755); err != nil {
		return nil
	}
	f, err := os.OpenFile(LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	defer f.Close()

	rec := struct {
		TS              string `json:"ts"`
		Tool            string `json:"tool"`
		Cmd             string `json:"cmd"`
		Cwd             string `json:"cwd,omitempty"`
		Exit            int    `json:"exit"`
		DurationMs      int64  `json:"duration_ms"`
		BytesOut        int    `json:"bytes_out"`
		StdoutTruncated bool   `json:"stdout_truncated"`
		StderrTruncated bool   `json:"stderr_truncated"`
		TimeoutMs       int    `json:"timeout_ms,omitempty"`
	}{
		TS:              time.Now().UTC().Format(time.RFC3339),
		Tool:            "shell.exec",
		Cmd:             in.Cmd,
		Cwd:             cwd,
		Exit:            out.ExitCode,
		DurationMs:      out.DurationMs,
		BytesOut:        len(out.Stdout) + len(out.Stderr),
		StdoutTruncated: out.StdoutTruncated,
		StderrTruncated: out.StderrTruncated,
		TimeoutMs:       in.TimeoutMs,
	}

	enc := json.NewEncoder(f)
	return enc.Encode(rec)
}

package pkgmgr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	rt "github.com/gaspardpetit/mcp-shell/internal/runtime"
)

const (
	DefaultTimeout = 60 * time.Second
	DefaultMaxIO   = 1 << 20
	LogPath        = "/logs/mcp-shell.log"
)

// AdminOverride allows package installs even when EGRESS!=1
var AdminOverride bool

func egressAllowed() bool {
	if os.Getenv("EGRESS") == "1" {
		return true
	}
	return AdminOverride
}

// workspace root for venvs and npm installs
func workspaceRoot() string {
	if ws := os.Getenv("WORKSPACE"); ws != "" {
		return filepath.Clean(ws)
	}
	return "/workspace"
}

// limitedWriter caps bytes written and marks truncation

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

// run executes a command with timeout/limits
func run(ctx context.Context, name string, args []string, timeout time.Duration, limit int, env []string) (stdout, stderr string, exit int, durationMs int64, stdoutTrunc, stderrTrunc bool) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &limitedWriter{buf: &stdoutBuf, limit: limit, truncated: &stdoutTrunc}
	cmd.Stderr = &limitedWriter{buf: &stderrBuf, limit: limit, truncated: &stderrTrunc}
	err := cmd.Run()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			exit = 124
		} else {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				exit = ee.ExitCode()
			} else {
				exit = 1
			}
		}
	}
	durationMs = time.Since(start).Milliseconds()
	return stdoutBuf.String(), stderrBuf.String(), exit, durationMs, stdoutTrunc, stderrTrunc
}

func audit(tool string, pkgs []string, exit int, durationMs int64, bytesOut int, stdoutTrunc, stderrTrunc bool) {
	if LogPath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(LogPath), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	rec := struct {
		TS              string   `json:"ts"`
		Tool            string   `json:"tool"`
		Packages        []string `json:"packages"`
		Exit            int      `json:"exit"`
		DurationMs      int64    `json:"duration_ms"`
		BytesOut        int      `json:"bytes_out"`
		StdoutTruncated bool     `json:"stdout_truncated"`
		StderrTruncated bool     `json:"stderr_truncated"`
	}{
		time.Now().UTC().Format(time.RFC3339),
		tool,
		pkgs,
		exit,
		durationMs,
		bytesOut,
		stdoutTrunc,
		stderrTrunc,
	}
	_ = json.NewEncoder(f).Encode(rec)
}

// ---- apt.install ----

type AptInstallRequest struct {
	Packages  []string `json:"packages"`
	Update    bool     `json:"update,omitempty"`
	AssumeYes bool     `json:"assume_yes,omitempty"`
	TimeoutMs int      `json:"timeout_ms,omitempty"`
	MaxBytes  int64    `json:"max_bytes,omitempty"`
	DryRun    bool     `json:"dry_run,omitempty"`
}

type InstallResponse struct {
	Installed       []string `json:"installed"`
	Stdout          string   `json:"stdout"`
	Stderr          string   `json:"stderr"`
	ExitCode        int      `json:"exit_code"`
	DurationMs      int64    `json:"duration_ms"`
	StdoutTruncated bool     `json:"stdout_truncated"`
	StderrTruncated bool     `json:"stderr_truncated"`
	Error           string   `json:"error,omitempty"`
}

func AptInstall(ctx context.Context, in AptInstallRequest) InstallResponse {
	start := time.Now()
	if len(in.Packages) == 0 {
		return InstallResponse{ExitCode: 1, Error: "packages is required"}
	}
	if !egressAllowed() {
		return InstallResponse{ExitCode: 1, Error: "package install disabled"}
	}
	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	limit := DefaultMaxIO
	if in.MaxBytes > 0 {
		limit = int(in.MaxBytes)
	}
	if in.DryRun {
		resp := InstallResponse{Installed: in.Packages, ExitCode: 0, DurationMs: time.Since(start).Milliseconds(), Stdout: fmt.Sprintf("[dry_run] apt-get install %s", strings.Join(in.Packages, " "))}
		audit("apt.install", in.Packages, resp.ExitCode, resp.DurationMs, len(resp.Stdout), false, false)
		return resp
	}
	if in.Update {
		run(ctx, "apt-get", []string{"update"}, timeout, limit, []string{"DEBIAN_FRONTEND=noninteractive"})
	}
	args := []string{"install"}
	if in.AssumeYes {
		args = append(args, "-y")
	}
	args = append(args, in.Packages...)
	stdout, stderr, exit, dur, outTrunc, errTrunc := run(ctx, "apt-get", args, timeout, limit, []string{"DEBIAN_FRONTEND=noninteractive"})
	resp := InstallResponse{
		Installed:       nil,
		Stdout:          stdout,
		Stderr:          stderr,
		ExitCode:        exit,
		DurationMs:      dur,
		StdoutTruncated: outTrunc,
		StderrTruncated: errTrunc,
	}
	if exit == 0 {
		resp.Installed = in.Packages
	} else {
		resp.Error = "apt install failed"
	}
	audit("apt.install", in.Packages, exit, dur, len(stdout)+len(stderr), outTrunc, errTrunc)
	return resp
}

// ---- pip.install ----

type PipInstallRequest struct {
	Packages  []string     `json:"packages"`
	Venv      *rt.VenvSpec `json:"venv,omitempty"`
	TimeoutMs int          `json:"timeout_ms,omitempty"`
	MaxBytes  int64        `json:"max_bytes,omitempty"`
	DryRun    bool         `json:"dry_run,omitempty"`
}

func PipInstall(ctx context.Context, in PipInstallRequest) InstallResponse {
	start := time.Now()
	if len(in.Packages) == 0 {
		return InstallResponse{ExitCode: 1, Error: "packages is required"}
	}
	if !egressAllowed() {
		return InstallResponse{ExitCode: 1, Error: "package install disabled"}
	}
	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	limit := DefaultMaxIO
	if in.MaxBytes > 0 {
		limit = int(in.MaxBytes)
	}
	if in.DryRun {
		resp := InstallResponse{Installed: in.Packages, ExitCode: 0, DurationMs: time.Since(start).Milliseconds(), Stdout: fmt.Sprintf("[dry_run] pip install %s", strings.Join(in.Packages, " "))}
		audit("pip.install", in.Packages, resp.ExitCode, resp.DurationMs, len(resp.Stdout), false, false)
		return resp
	}
	pipPath := "pip"
	if in.Venv != nil {
		name := in.Venv.Name
		if name == "" {
			name = "default"
		}
		venvPath := filepath.Join(workspaceRoot(), ".venvs", name)
		if _, err := os.Stat(venvPath); errors.Is(err, os.ErrNotExist) {
			if in.Venv.CreateIfMissing {
				_, _, exit, _, _, _ := run(ctx, "python3", []string{"-m", "venv", venvPath}, timeout, limit, nil)
				if exit != 0 {
					dur := time.Since(start).Milliseconds()
					audit("pip.install", in.Packages, exit, dur, 0, false, false)
					return InstallResponse{ExitCode: exit, DurationMs: dur, Error: "venv create failed"}
				}
			} else {
				return InstallResponse{ExitCode: 1, Error: "venv not found"}
			}
		}
		pipPath = filepath.Join(venvPath, "bin", "pip")
	}
	args := append([]string{"install"}, in.Packages...)
	stdout, stderr, exit, dur, outTrunc, errTrunc := run(ctx, pipPath, args, timeout, limit, []string{"PIP_DISABLE_PIP_VERSION_CHECK=1"})
	resp := InstallResponse{
		Installed:       nil,
		Stdout:          stdout,
		Stderr:          stderr,
		ExitCode:        exit,
		DurationMs:      dur,
		StdoutTruncated: outTrunc,
		StderrTruncated: errTrunc,
	}
	if exit == 0 {
		resp.Installed = in.Packages
	} else {
		resp.Error = "pip install failed"
	}
	audit("pip.install", in.Packages, exit, dur, len(stdout)+len(stderr), outTrunc, errTrunc)
	return resp
}

// ---- npm.install ----

type NpmInstallRequest struct {
	Packages  []string `json:"packages"`
	Global    bool     `json:"global,omitempty"`
	TimeoutMs int      `json:"timeout_ms,omitempty"`
	MaxBytes  int64    `json:"max_bytes,omitempty"`
	DryRun    bool     `json:"dry_run,omitempty"`
}

func NpmInstall(ctx context.Context, in NpmInstallRequest) InstallResponse {
	start := time.Now()
	if len(in.Packages) == 0 {
		return InstallResponse{ExitCode: 1, Error: "packages is required"}
	}
	if !egressAllowed() {
		return InstallResponse{ExitCode: 1, Error: "package install disabled"}
	}
	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	limit := DefaultMaxIO
	if in.MaxBytes > 0 {
		limit = int(in.MaxBytes)
	}
	if in.DryRun {
		resp := InstallResponse{Installed: in.Packages, ExitCode: 0, DurationMs: time.Since(start).Milliseconds(), Stdout: fmt.Sprintf("[dry_run] npm install %s", strings.Join(in.Packages, " "))}
		audit("npm.install", in.Packages, resp.ExitCode, resp.DurationMs, len(resp.Stdout), false, false)
		return resp
	}
	args := []string{"install"}
	if in.Global {
		args = append(args, "-g")
	}
	args = append(args, in.Packages...)
	stdout, stderr, exit, dur, outTrunc, errTrunc := run(ctx, "npm", args, timeout, limit, nil)
	resp := InstallResponse{
		Installed:       nil,
		Stdout:          stdout,
		Stderr:          stderr,
		ExitCode:        exit,
		DurationMs:      dur,
		StdoutTruncated: outTrunc,
		StderrTruncated: errTrunc,
	}
	if exit == 0 {
		resp.Installed = in.Packages
	} else {
		resp.Error = "npm install failed"
	}
	audit("npm.install", in.Packages, exit, dur, len(stdout)+len(stderr), outTrunc, errTrunc)
	return resp
}

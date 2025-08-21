package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const (
	DefaultTimeout = 60 * time.Second
	DefaultMaxIO   = 1 << 20 // 1 MiB
	LogPath        = "/logs/mcp-shell.log"
)

// ---- helpers ----

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

func audit(rec any) {
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
	_ = json.NewEncoder(f).Encode(rec)
}

// workspace root for venvs
func workspaceRoot() string {
	if ws := os.Getenv("WORKSPACE"); ws != "" {
		return filepath.Clean(ws)
	}
	return "/workspace"
}

// ---- python.run ----

type VenvSpec struct {
	Name            string `json:"name,omitempty"`
	CreateIfMissing bool   `json:"create_if_missing,omitempty"`
}

type Artifact struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type PythonRunRequest struct {
	Code      string    `json:"code"`
	Args      []string  `json:"args,omitempty"`
	Stdin     string    `json:"stdin,omitempty"`
	Venv      *VenvSpec `json:"venv,omitempty"`
	Packages  []string  `json:"packages,omitempty"`
	TimeoutMs int       `json:"timeout_ms,omitempty"`
	MaxBytes  int64     `json:"max_bytes,omitempty"`
}

type RunResponse struct {
	Stdout          string     `json:"stdout"`
	Stderr          string     `json:"stderr"`
	ExitCode        int        `json:"exit_code"`
	DurationMs      int64      `json:"duration_ms"`
	StdoutTruncated bool       `json:"stdout_truncated"`
	StderrTruncated bool       `json:"stderr_truncated"`
	Artifacts       []Artifact `json:"artifacts,omitempty"`
	Error           string     `json:"error,omitempty"`
}

func PythonRun(ctx context.Context, in PythonRunRequest) RunResponse {
	start := time.Now()
	if in.Code == "" {
		return RunResponse{ExitCode: 1, Error: "code is required"}
	}
	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	limit := DefaultMaxIO
	if in.MaxBytes > 0 {
		limit = int(in.MaxBytes)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// temp dir for script
	tmpDir, err := os.MkdirTemp("", "python-run-*")
	if err != nil {
		return RunResponse{ExitCode: 1, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	scriptPath := filepath.Join(tmpDir, "script.py")
	if err := os.WriteFile(scriptPath, []byte(in.Code), 0o700); err != nil {
		return RunResponse{ExitCode: 1, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}

	pythonBin := "python3"
	if in.Venv != nil {
		name := in.Venv.Name
		if name == "" {
			name = "default"
		}
		venvPath := filepath.Join(workspaceRoot(), ".venvs", name)
		if _, err := os.Stat(venvPath); errors.Is(err, os.ErrNotExist) {
			if in.Venv.CreateIfMissing {
				if err := exec.CommandContext(ctx, "python3", "-m", "venv", venvPath).Run(); err != nil {
					return RunResponse{ExitCode: 1, Error: fmt.Sprintf("venv create failed: %v", err), DurationMs: time.Since(start).Milliseconds()}
				}
			} else {
				return RunResponse{ExitCode: 1, Error: "venv not found"}
			}
		}
		if len(in.Packages) > 0 {
			cmd := exec.CommandContext(ctx, filepath.Join(venvPath, "bin", "python"), append([]string{"-m", "pip", "install"}, in.Packages...)...)
			var stderr bytes.Buffer
			cmd.Stdout = io.Discard
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				return RunResponse{ExitCode: 1, Error: fmt.Sprintf("pip install: %s", stderr.String()), DurationMs: time.Since(start).Milliseconds()}
			}
		}
		pythonBin = filepath.Join(venvPath, "bin", "python")
	}

	args := append([]string{scriptPath}, in.Args...)
	cmd := exec.CommandContext(ctx, pythonBin, args...)
	cmd.Dir = tmpDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if in.Stdin != "" {
		cmd.Stdin = bytes.NewBufferString(in.Stdin)
	}
	var stdoutBuf, stderrBuf bytes.Buffer
	var stdoutTrunc, stderrTrunc bool
	cmd.Stdout = &limitedWriter{buf: &stdoutBuf, limit: limit, truncated: &stdoutTrunc}
	cmd.Stderr = &limitedWriter{buf: &stderrBuf, limit: limit, truncated: &stderrTrunc}

	exit := 0
	if err := cmd.Run(); err != nil {
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

	var artifacts []Artifact
	entries, _ := os.ReadDir(tmpDir)
	for _, e := range entries {
		if e.Name() == "script.py" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		artifacts = append(artifacts, Artifact{Path: filepath.Join(tmpDir, e.Name()), Size: info.Size()})
	}

	resp := RunResponse{
		Stdout:          stdoutBuf.String(),
		Stderr:          stderrBuf.String(),
		ExitCode:        exit,
		DurationMs:      time.Since(start).Milliseconds(),
		StdoutTruncated: stdoutTrunc,
		StderrTruncated: stderrTrunc,
		Artifacts:       artifacts,
	}
	if exit == 124 && resp.Stderr == "" {
		resp.Stderr = "timed out"
	}
	audit(struct {
		TS         string   `json:"ts"`
		Tool       string   `json:"tool"`
		Venv       string   `json:"venv,omitempty"`
		Exit       int      `json:"exit"`
		DurationMs int64    `json:"duration_ms"`
		BytesOut   int      `json:"bytes_out"`
		Packages   []string `json:"packages,omitempty"`
	}{time.Now().UTC().Format(time.RFC3339), "python.run", func() string {
		if in.Venv != nil {
			return in.Venv.Name
		} else {
			return ""
		}
	}(), exit, resp.DurationMs, len(resp.Stdout) + len(resp.Stderr), in.Packages})
	return resp
}

// ---- node.run ----

type NodeRunRequest struct {
	Code      string   `json:"code"`
	Args      []string `json:"args,omitempty"`
	Stdin     string   `json:"stdin,omitempty"`
	Packages  []string `json:"packages,omitempty"`
	TimeoutMs int      `json:"timeout_ms,omitempty"`
	MaxBytes  int64    `json:"max_bytes,omitempty"`
}

func NodeRun(ctx context.Context, in NodeRunRequest) RunResponse {
	start := time.Now()
	if in.Code == "" {
		return RunResponse{ExitCode: 1, Error: "code is required"}
	}
	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	limit := DefaultMaxIO
	if in.MaxBytes > 0 {
		limit = int(in.MaxBytes)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "node-run-*")
	if err != nil {
		return RunResponse{ExitCode: 1, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	scriptPath := filepath.Join(tmpDir, "script.js")
	if err := os.WriteFile(scriptPath, []byte(in.Code), 0o700); err != nil {
		return RunResponse{ExitCode: 1, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	if len(in.Packages) > 0 {
		npmInit := exec.CommandContext(ctx, "npm", "init", "-y")
		npmInit.Dir = tmpDir
		npmInit.Stdout = io.Discard
		npmInit.Stderr = io.Discard
		_ = npmInit.Run()
		npmInstall := exec.CommandContext(ctx, "npm", append([]string{"install"}, in.Packages...)...)
		npmInstall.Dir = tmpDir
		npmInstall.Stdout = io.Discard
		var errBuf bytes.Buffer
		npmInstall.Stderr = &errBuf
		if err := npmInstall.Run(); err != nil {
			return RunResponse{ExitCode: 1, Error: fmt.Sprintf("npm install: %s", errBuf.String()), DurationMs: time.Since(start).Milliseconds()}
		}
	}
	args := append([]string{scriptPath}, in.Args...)
	cmd := exec.CommandContext(ctx, "node", args...)
	cmd.Dir = tmpDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if in.Stdin != "" {
		cmd.Stdin = bytes.NewBufferString(in.Stdin)
	}
	var stdoutBuf, stderrBuf bytes.Buffer
	var stdoutTrunc, stderrTrunc bool
	cmd.Stdout = &limitedWriter{buf: &stdoutBuf, limit: limit, truncated: &stdoutTrunc}
	cmd.Stderr = &limitedWriter{buf: &stderrBuf, limit: limit, truncated: &stderrTrunc}

	exit := 0
	if err := cmd.Run(); err != nil {
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
	var artifacts []Artifact
	entries, _ := os.ReadDir(tmpDir)
	for _, e := range entries {
		if e.Name() == "script.js" || e.Name() == "node_modules" || e.Name() == "package.json" || e.Name() == "package-lock.json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		artifacts = append(artifacts, Artifact{Path: filepath.Join(tmpDir, e.Name()), Size: info.Size()})
	}
	resp := RunResponse{
		Stdout:          stdoutBuf.String(),
		Stderr:          stderrBuf.String(),
		ExitCode:        exit,
		DurationMs:      time.Since(start).Milliseconds(),
		StdoutTruncated: stdoutTrunc,
		StderrTruncated: stderrTrunc,
		Artifacts:       artifacts,
	}
	if exit == 124 && resp.Stderr == "" {
		resp.Stderr = "timed out"
	}
	audit(struct {
		TS         string   `json:"ts"`
		Tool       string   `json:"tool"`
		Exit       int      `json:"exit"`
		DurationMs int64    `json:"duration_ms"`
		BytesOut   int      `json:"bytes_out"`
		Packages   []string `json:"packages,omitempty"`
	}{time.Now().UTC().Format(time.RFC3339), "node.run", exit, resp.DurationMs, len(resp.Stdout) + len(resp.Stderr), in.Packages})
	return resp
}

// ---- sh.script.write_and_run ----

type ShRequest struct {
	Shebang   string            `json:"shebang"`
	Content   string            `json:"content"`
	Cwd       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	TimeoutMs int               `json:"timeout_ms,omitempty"`
	MaxBytes  int64             `json:"max_bytes,omitempty"`
}

func ShScriptWriteAndRun(ctx context.Context, in ShRequest) RunResponse {
	start := time.Now()
	if in.Shebang == "" || in.Content == "" {
		return RunResponse{ExitCode: 1, Error: "shebang and content required"}
	}
	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	limit := DefaultMaxIO
	if in.MaxBytes > 0 {
		limit = int(in.MaxBytes)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "sh-run-*")
	if err != nil {
		return RunResponse{ExitCode: 1, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	scriptPath := filepath.Join(tmpDir, "script.sh")
	content := fmt.Sprintf("#!%s\n%s", in.Shebang, in.Content)
	if err := os.WriteFile(scriptPath, []byte(content), 0o700); err != nil {
		return RunResponse{ExitCode: 1, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	cmd := exec.CommandContext(ctx, scriptPath)
	if in.Cwd != "" {
		cmd.Dir = filepath.Clean(in.Cwd)
	}
	if len(in.Env) > 0 {
		env := os.Environ()
		for k, v := range in.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = env
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdoutBuf, stderrBuf bytes.Buffer
	var stdoutTrunc, stderrTrunc bool
	cmd.Stdout = &limitedWriter{buf: &stdoutBuf, limit: limit, truncated: &stdoutTrunc}
	cmd.Stderr = &limitedWriter{buf: &stderrBuf, limit: limit, truncated: &stderrTrunc}
	exit := 0
	if err := cmd.Run(); err != nil {
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
	resp := RunResponse{
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
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Exit       int    `json:"exit"`
		DurationMs int64  `json:"duration_ms"`
		BytesOut   int    `json:"bytes_out"`
	}{time.Now().UTC().Format(time.RFC3339), "sh.script.write_and_run", exit, resp.DurationMs, len(resp.Stdout) + len(resp.Stderr)})
	return resp
}

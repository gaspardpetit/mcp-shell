package git

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
)

const (
	DefaultTimeout = 60 * time.Second
	DefaultMaxIO   = 1 << 20
	LogPath        = "/logs/mcp-shell.log"
)

func workspaceRoot() string {
	if ws := os.Getenv("WORKSPACE"); ws != "" {
		return filepath.Clean(ws)
	}
	return "/workspace"
}

func normalizePath(p string) (string, error) {
	if p == "" {
		return "", errors.New("path is required")
	}
	root := workspaceRoot()
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	p = filepath.Clean(p)
	rel, err := filepath.Rel(root, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path %q escapes workspace", p)
	}
	return p, nil
}

func egressAllowed() bool {
	return os.Getenv("EGRESS") == "1"
}

func pushAllowed() bool {
	return os.Getenv("GIT_ALLOW_PUSH") == "1"
}

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

func run(ctx context.Context, cwd string, args []string, timeout time.Duration, limit int) (stdout, stderr string, exit int, durationMs int64, stdoutTrunc, stderrTrunc bool) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	if cwd != "" {
		cmd.Dir = cwd
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
	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()
	return
}

func audit(tool, path string, args []string, exit int, durationMs int64, bytesOut int, stdoutTrunc, stderrTrunc bool) {
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
		Path            string   `json:"path"`
		Args            []string `json:"args"`
		Exit            int      `json:"exit"`
		DurationMs      int64    `json:"duration_ms"`
		BytesOut        int      `json:"bytes_out"`
		StdoutTruncated bool     `json:"stdout_truncated"`
		StderrTruncated bool     `json:"stderr_truncated"`
	}{
		time.Now().UTC().Format(time.RFC3339),
		tool,
		path,
		args,
		exit,
		durationMs,
		bytesOut,
		stdoutTrunc,
		stderrTrunc,
	}
	_ = json.NewEncoder(f).Encode(rec)
}

// ---- git.clone ----

type CloneRequest struct {
	Repo      string `json:"repo"`
	Dir       string `json:"dir,omitempty"`
	Depth     int    `json:"depth,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
	MaxBytes  int64  `json:"max_bytes,omitempty"`
	DryRun    bool   `json:"dry_run,omitempty"`
}

type CloneResponse struct {
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	DurationMs      int64  `json:"duration_ms"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
	Error           string `json:"error,omitempty"`
}

func Clone(ctx context.Context, in CloneRequest) CloneResponse {
	start := time.Now()
	if in.Repo == "" {
		return CloneResponse{ExitCode: 1, Error: "repo is required"}
	}
	if !egressAllowed() && !in.DryRun {
		return CloneResponse{ExitCode: 1, Error: "git clone requires egress"}
	}
	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	limit := DefaultMaxIO
	if in.MaxBytes > 0 {
		limit = int(in.MaxBytes)
	}
	cwd := workspaceRoot()
	args := []string{"clone"}
	if in.Depth > 0 {
		args = append(args, fmt.Sprintf("--depth=%d", in.Depth))
	}
	args = append(args, in.Repo)
	if in.Dir != "" {
		args = append(args, in.Dir)
	}
	if in.DryRun {
		resp := CloneResponse{Stdout: fmt.Sprintf("[dry_run] git %s", strings.Join(args, " ")), ExitCode: 0, DurationMs: time.Since(start).Milliseconds()}
		audit("git.clone", cwd, args, resp.ExitCode, resp.DurationMs, len(resp.Stdout)+len(resp.Stderr), false, false)
		return resp
	}
	stdout, stderr, exit, dur, outTrunc, errTrunc := run(ctx, cwd, args, timeout, limit)
	resp := CloneResponse{
		Stdout:          stdout,
		Stderr:          stderr,
		ExitCode:        exit,
		DurationMs:      dur,
		StdoutTruncated: outTrunc,
		StderrTruncated: errTrunc,
	}
	if exit != 0 {
		resp.Error = "git clone failed"
	}
	audit("git.clone", cwd, args, exit, dur, len(stdout)+len(stderr), outTrunc, errTrunc)
	return resp
}

// ---- git.status ----

type StatusRequest struct {
	Path      string `json:"path"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
	MaxBytes  int64  `json:"max_bytes,omitempty"`
}

type StatusResponse struct {
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	DurationMs      int64  `json:"duration_ms"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
	Error           string `json:"error,omitempty"`
}

func Status(ctx context.Context, in StatusRequest) StatusResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return StatusResponse{ExitCode: 1, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	limit := DefaultMaxIO
	if in.MaxBytes > 0 {
		limit = int(in.MaxBytes)
	}
	args := []string{"status", "--porcelain"}
	stdout, stderr, exit, dur, outTrunc, errTrunc := run(ctx, path, args, timeout, limit)
	resp := StatusResponse{
		Stdout:          stdout,
		Stderr:          stderr,
		ExitCode:        exit,
		DurationMs:      dur,
		StdoutTruncated: outTrunc,
		StderrTruncated: errTrunc,
	}
	if exit != 0 {
		resp.Error = "git status failed"
	}
	audit("git.status", path, args, exit, dur, len(stdout)+len(stderr), outTrunc, errTrunc)
	return resp
}

// ---- git.commit ----

type CommitRequest struct {
	Path      string `json:"path"`
	Message   string `json:"message"`
	All       bool   `json:"all,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
	MaxBytes  int64  `json:"max_bytes,omitempty"`
	DryRun    bool   `json:"dry_run,omitempty"`
}

type CommitResponse struct {
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	DurationMs      int64  `json:"duration_ms"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
	Commit          string `json:"commit,omitempty"`
	Error           string `json:"error,omitempty"`
}

func Commit(ctx context.Context, in CommitRequest) CommitResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return CommitResponse{ExitCode: 1, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	if in.Message == "" {
		return CommitResponse{ExitCode: 1, Error: "message is required", DurationMs: time.Since(start).Milliseconds()}
	}
	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	limit := DefaultMaxIO
	if in.MaxBytes > 0 {
		limit = int(in.MaxBytes)
	}
	args := []string{"commit", "-m", in.Message}
	if in.All {
		args = append(args, "-a")
	}
	if in.DryRun {
		resp := CommitResponse{Stdout: fmt.Sprintf("[dry_run] git %s", strings.Join(args, " ")), ExitCode: 0, DurationMs: time.Since(start).Milliseconds()}
		audit("git.commit", path, args, resp.ExitCode, resp.DurationMs, len(resp.Stdout)+len(resp.Stderr), false, false)
		return resp
	}
	stdout, stderr, exit, dur, outTrunc, errTrunc := run(ctx, path, args, timeout, limit)
	resp := CommitResponse{
		Stdout:          stdout,
		Stderr:          stderr,
		ExitCode:        exit,
		DurationMs:      dur,
		StdoutTruncated: outTrunc,
		StderrTruncated: errTrunc,
	}
	if exit == 0 {
		revArgs := []string{"rev-parse", "HEAD"}
		revStdout, _, _, _, _, _ := run(ctx, path, revArgs, timeout, limit)
		resp.Commit = strings.TrimSpace(revStdout)
	} else {
		resp.Error = "git commit failed"
	}
	audit("git.commit", path, args, exit, dur, len(stdout)+len(stderr), outTrunc, errTrunc)
	return resp
}

// ---- git.pull ----

type PullRequest struct {
	Path      string `json:"path"`
	Rebase    bool   `json:"rebase,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
	MaxBytes  int64  `json:"max_bytes,omitempty"`
	DryRun    bool   `json:"dry_run,omitempty"`
}

type PullResponse struct {
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	DurationMs      int64  `json:"duration_ms"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
	Error           string `json:"error,omitempty"`
}

func Pull(ctx context.Context, in PullRequest) PullResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return PullResponse{ExitCode: 1, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	if !egressAllowed() && !in.DryRun {
		return PullResponse{ExitCode: 1, Error: "git pull requires egress"}
	}
	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	limit := DefaultMaxIO
	if in.MaxBytes > 0 {
		limit = int(in.MaxBytes)
	}
	args := []string{"pull"}
	if in.Rebase {
		args = append(args, "--rebase")
	}
	if in.DryRun {
		resp := PullResponse{Stdout: fmt.Sprintf("[dry_run] git %s", strings.Join(args, " ")), ExitCode: 0, DurationMs: time.Since(start).Milliseconds()}
		audit("git.pull", path, args, resp.ExitCode, resp.DurationMs, len(resp.Stdout)+len(resp.Stderr), false, false)
		return resp
	}
	stdout, stderr, exit, dur, outTrunc, errTrunc := run(ctx, path, args, timeout, limit)
	resp := PullResponse{
		Stdout:          stdout,
		Stderr:          stderr,
		ExitCode:        exit,
		DurationMs:      dur,
		StdoutTruncated: outTrunc,
		StderrTruncated: errTrunc,
	}
	if exit != 0 {
		resp.Error = "git pull failed"
	}
	audit("git.pull", path, args, exit, dur, len(stdout)+len(stderr), outTrunc, errTrunc)
	return resp
}

// ---- git.push ----

type PushRequest struct {
	Path      string `json:"path"`
	Remote    string `json:"remote,omitempty"`
	Branch    string `json:"branch,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
	MaxBytes  int64  `json:"max_bytes,omitempty"`
	DryRun    bool   `json:"dry_run,omitempty"`
}

type PushResponse struct {
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	DurationMs      int64  `json:"duration_ms"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
	Error           string `json:"error,omitempty"`
}

func Push(ctx context.Context, in PushRequest) PushResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return PushResponse{ExitCode: 1, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	if !pushAllowed() && !in.DryRun {
		return PushResponse{ExitCode: 1, Error: "git push disabled"}
	}
	if !egressAllowed() && !in.DryRun {
		return PushResponse{ExitCode: 1, Error: "git push requires egress"}
	}
	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	limit := DefaultMaxIO
	if in.MaxBytes > 0 {
		limit = int(in.MaxBytes)
	}
	args := []string{"push"}
	if in.Remote != "" {
		args = append(args, in.Remote)
	}
	if in.Branch != "" {
		args = append(args, in.Branch)
	}
	if in.DryRun {
		resp := PushResponse{Stdout: fmt.Sprintf("[dry_run] git %s", strings.Join(args, " ")), ExitCode: 0, DurationMs: time.Since(start).Milliseconds()}
		audit("git.push", path, args, resp.ExitCode, resp.DurationMs, len(resp.Stdout)+len(resp.Stderr), false, false)
		return resp
	}
	stdout, stderr, exit, dur, outTrunc, errTrunc := run(ctx, path, args, timeout, limit)
	resp := PushResponse{
		Stdout:          stdout,
		Stderr:          stderr,
		ExitCode:        exit,
		DurationMs:      dur,
		StdoutTruncated: outTrunc,
		StderrTruncated: errTrunc,
	}
	if exit != 0 {
		resp.Error = "git push failed"
	}
	audit("git.push", path, args, exit, dur, len(stdout)+len(stderr), outTrunc, errTrunc)
	return resp
}

// ---- git.checkout ----

type CheckoutRequest struct {
	Path      string `json:"path"`
	Ref       string `json:"ref"`
	Create    bool   `json:"create,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
	MaxBytes  int64  `json:"max_bytes,omitempty"`
}

type CheckoutResponse struct {
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	DurationMs      int64  `json:"duration_ms"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
	Error           string `json:"error,omitempty"`
}

func Checkout(ctx context.Context, in CheckoutRequest) CheckoutResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return CheckoutResponse{ExitCode: 1, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	if in.Ref == "" {
		return CheckoutResponse{ExitCode: 1, Error: "ref is required", DurationMs: time.Since(start).Milliseconds()}
	}
	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	limit := DefaultMaxIO
	if in.MaxBytes > 0 {
		limit = int(in.MaxBytes)
	}
	args := []string{"checkout"}
	if in.Create {
		args = append(args, "-b")
	}
	args = append(args, in.Ref)
	stdout, stderr, exit, dur, outTrunc, errTrunc := run(ctx, path, args, timeout, limit)
	resp := CheckoutResponse{
		Stdout:          stdout,
		Stderr:          stderr,
		ExitCode:        exit,
		DurationMs:      dur,
		StdoutTruncated: outTrunc,
		StderrTruncated: errTrunc,
	}
	if exit != 0 {
		resp.Error = "git checkout failed"
	}
	audit("git.checkout", path, args, exit, dur, len(stdout)+len(stderr), outTrunc, errTrunc)
	return resp
}

// ---- git.branch ----

type BranchRequest struct {
	Path      string `json:"path"`
	Name      string `json:"name,omitempty"`
	Delete    bool   `json:"delete,omitempty"`
	List      bool   `json:"list,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
	MaxBytes  int64  `json:"max_bytes,omitempty"`
}

type BranchResponse struct {
	Stdout          string   `json:"stdout"`
	Stderr          string   `json:"stderr"`
	ExitCode        int      `json:"exit_code"`
	DurationMs      int64    `json:"duration_ms"`
	StdoutTruncated bool     `json:"stdout_truncated"`
	StderrTruncated bool     `json:"stderr_truncated"`
	Branches        []string `json:"branches,omitempty"`
	Error           string   `json:"error,omitempty"`
}

func Branch(ctx context.Context, in BranchRequest) BranchResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return BranchResponse{ExitCode: 1, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	limit := DefaultMaxIO
	if in.MaxBytes > 0 {
		limit = int(in.MaxBytes)
	}
	args := []string{"branch"}
	if in.List || in.Name == "" {
		args = append(args, "--list")
	} else {
		if in.Delete {
			args = append(args, "-d", in.Name)
		} else {
			args = append(args, in.Name)
		}
	}
	stdout, stderr, exit, dur, outTrunc, errTrunc := run(ctx, path, args, timeout, limit)
	resp := BranchResponse{
		Stdout:          stdout,
		Stderr:          stderr,
		ExitCode:        exit,
		DurationMs:      dur,
		StdoutTruncated: outTrunc,
		StderrTruncated: errTrunc,
	}
	if (in.List || in.Name == "") && exit == 0 {
		lines := strings.Split(stdout, "\n")
		for _, l := range lines {
			l = strings.TrimSpace(strings.TrimPrefix(l, "*"))
			if l != "" {
				resp.Branches = append(resp.Branches, l)
			}
		}
	}
	if exit != 0 {
		resp.Error = "git branch failed"
	}
	audit("git.branch", path, args, exit, dur, len(stdout)+len(stderr), outTrunc, errTrunc)
	return resp
}

// ---- git.tag ----

type TagRequest struct {
	Path      string `json:"path"`
	Name      string `json:"name,omitempty"`
	Delete    bool   `json:"delete,omitempty"`
	List      bool   `json:"list,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
	MaxBytes  int64  `json:"max_bytes,omitempty"`
}

type TagResponse struct {
	Stdout          string   `json:"stdout"`
	Stderr          string   `json:"stderr"`
	ExitCode        int      `json:"exit_code"`
	DurationMs      int64    `json:"duration_ms"`
	StdoutTruncated bool     `json:"stdout_truncated"`
	StderrTruncated bool     `json:"stderr_truncated"`
	Tags            []string `json:"tags,omitempty"`
	Error           string   `json:"error,omitempty"`
}

func Tag(ctx context.Context, in TagRequest) TagResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return TagResponse{ExitCode: 1, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	limit := DefaultMaxIO
	if in.MaxBytes > 0 {
		limit = int(in.MaxBytes)
	}
	args := []string{"tag"}
	if in.List || in.Name == "" {
		args = append(args, "--list")
	} else {
		if in.Delete {
			args = append(args, "-d", in.Name)
		} else {
			args = append(args, in.Name)
		}
	}
	stdout, stderr, exit, dur, outTrunc, errTrunc := run(ctx, path, args, timeout, limit)
	resp := TagResponse{
		Stdout:          stdout,
		Stderr:          stderr,
		ExitCode:        exit,
		DurationMs:      dur,
		StdoutTruncated: outTrunc,
		StderrTruncated: errTrunc,
	}
	if (in.List || in.Name == "") && exit == 0 {
		lines := strings.Split(stdout, "\n")
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l != "" {
				resp.Tags = append(resp.Tags, l)
			}
		}
	}
	if exit != 0 {
		resp.Error = "git tag failed"
	}
	audit("git.tag", path, args, exit, dur, len(stdout)+len(stderr), outTrunc, errTrunc)
	return resp
}

// ---- git.lfs.install ----

type LFSInstallRequest struct {
	Path      string `json:"path"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
	MaxBytes  int64  `json:"max_bytes,omitempty"`
	DryRun    bool   `json:"dry_run,omitempty"`
}

type LFSInstallResponse struct {
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	DurationMs      int64  `json:"duration_ms"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
	Error           string `json:"error,omitempty"`
}

func LFSInstall(ctx context.Context, in LFSInstallRequest) LFSInstallResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return LFSInstallResponse{ExitCode: 1, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	limit := DefaultMaxIO
	if in.MaxBytes > 0 {
		limit = int(in.MaxBytes)
	}
	args := []string{"lfs", "install"}
	if in.DryRun {
		resp := LFSInstallResponse{Stdout: fmt.Sprintf("[dry_run] git %s", strings.Join(args, " ")), ExitCode: 0, DurationMs: time.Since(start).Milliseconds()}
		audit("git.lfs.install", path, args, resp.ExitCode, resp.DurationMs, len(resp.Stdout)+len(resp.Stderr), false, false)
		return resp
	}
	stdout, stderr, exit, dur, outTrunc, errTrunc := run(ctx, path, args, timeout, limit)
	resp := LFSInstallResponse{
		Stdout:          stdout,
		Stderr:          stderr,
		ExitCode:        exit,
		DurationMs:      dur,
		StdoutTruncated: outTrunc,
		StderrTruncated: errTrunc,
	}
	if exit != 0 {
		resp.Error = "git lfs install failed"
	}
	audit("git.lfs.install", path, args, exit, dur, len(stdout)+len(stderr), outTrunc, errTrunc)
	return resp
}

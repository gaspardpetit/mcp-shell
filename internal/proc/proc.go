package proc

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
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

const (
	DefaultTimeout  = 60 * time.Second
	DefaultMaxIO    = 1 << 20 // 1 MiB
	DefaultMaxStdin = 1 << 20 // 1 MiB
	LogPath         = "/logs/mcp-shell.log"
)

type SpawnRequest struct {
	Cmd  string            `json:"cmd"`
	Args []string          `json:"args,omitempty"`
	Cwd  string            `json:"cwd,omitempty"`
	Env  map[string]string `json:"env,omitempty"`
	TTY  bool              `json:"tty,omitempty"`
}

type SpawnResponse struct {
	Pid        int    `json:"pid,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type StdinRequest struct {
	Pid  int    `json:"pid"`
	Data string `json:"data"`
}

type StdinResponse struct {
	BytesWritten int    `json:"bytes_written"`
	DurationMs   int64  `json:"duration_ms"`
	Error        string `json:"error,omitempty"`
}

type WaitRequest struct {
	Pid       int `json:"pid"`
	TimeoutMs int `json:"timeout_ms,omitempty"`
}

type WaitResponse struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	Truncated  bool   `json:"truncated"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type KillRequest struct {
	Pid    int `json:"pid"`
	Signal int `json:"signal,omitempty"`
}

type KillResponse struct {
	Killed     bool   `json:"killed"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}
type ListRequest struct{}

type ListResponse struct {
	Processes  []ProcInfo `json:"processes"`
	DurationMs int64      `json:"duration_ms"`
}

type ProcInfo struct {
	Pid       int    `json:"pid"`
	Cmdline   string `json:"cmdline"`
	StartTime string `json:"start_time"`
	Cwd       string `json:"cwd,omitempty"`
}

type process struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdoutBuf   *bytes.Buffer
	stderrBuf   *bytes.Buffer
	stdoutTrunc *bool
	stderrTrunc *bool
	done        chan struct{}
	exitCode    int
	start       time.Time
	cwd         string
	tty         bool
}

var (
	procMu    sync.Mutex
	processes = make(map[int]*process)
)

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

func Spawn(ctx context.Context, in SpawnRequest) SpawnResponse {
	start := time.Now()
	if in.Cmd == "" {
		return SpawnResponse{Error: "cmd is required", DurationMs: time.Since(start).Milliseconds()}
	}

	cmd := exec.Command(in.Cmd, in.Args...)
	if in.Cwd != "" {
		cmd.Dir = filepath.Clean(in.Cwd)
	} else if ws := os.Getenv("WORKSPACE"); ws != "" {
		cmd.Dir = ws
	}
	if len(in.Env) > 0 {
		env := os.Environ()
		for k, v := range in.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = env
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var (
		stdoutBuf, stderrBuf     bytes.Buffer
		stdoutTrunc, stderrTrunc bool
		stdin                    io.WriteCloser
		err                      error
	)

	if in.TTY {
		var f *os.File
		f, err = pty.Start(cmd)
		if err != nil {
			return SpawnResponse{Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
		}
		stdin = f
		go func() {
			_, _ = io.Copy(&limitedWriter{buf: &stdoutBuf, limit: DefaultMaxIO, truncated: &stdoutTrunc}, f)
		}()
	} else {
		var stdoutPipe, stderrPipe io.ReadCloser
		stdoutPipe, err = cmd.StdoutPipe()
		if err != nil {
			return SpawnResponse{Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
		}
		stderrPipe, err = cmd.StderrPipe()
		if err != nil {
			return SpawnResponse{Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
		}
		stdin, err = cmd.StdinPipe()
		if err != nil {
			return SpawnResponse{Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
		}
		go func() {
			_, _ = io.Copy(&limitedWriter{buf: &stdoutBuf, limit: DefaultMaxIO, truncated: &stdoutTrunc}, stdoutPipe)
		}()
		go func() {
			_, _ = io.Copy(&limitedWriter{buf: &stderrBuf, limit: DefaultMaxIO, truncated: &stderrTrunc}, stderrPipe)
		}()
	}

	if err = cmd.Start(); err != nil {
		return SpawnResponse{Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}

	p := &process{
		cmd:         cmd,
		stdin:       stdin,
		stdoutBuf:   &stdoutBuf,
		stderrBuf:   &stderrBuf,
		stdoutTrunc: &stdoutTrunc,
		stderrTrunc: &stderrTrunc,
		done:        make(chan struct{}),
		start:       time.Now(),
		cwd:         cmd.Dir,
		tty:         in.TTY,
	}

	procMu.Lock()
	processes[cmd.Process.Pid] = p
	procMu.Unlock()

	go func() {
		err := cmd.Wait()
		exit := 0
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				exit = ee.ExitCode()
			} else {
				exit = 1
			}
		}
		p.exitCode = exit
		close(p.done)
	}()

	audit(struct {
		TS   string `json:"ts"`
		Tool string `json:"tool"`
		Cmd  string `json:"cmd"`
		PID  int    `json:"pid"`
		Cwd  string `json:"cwd"`
	}{time.Now().UTC().Format(time.RFC3339), "proc.spawn", cmd.String(), cmd.Process.Pid, cmd.Dir})

	return SpawnResponse{Pid: cmd.Process.Pid, DurationMs: time.Since(start).Milliseconds()}
}

func Stdin(ctx context.Context, in StdinRequest) StdinResponse {
	start := time.Now()
	procMu.Lock()
	p := processes[in.Pid]
	procMu.Unlock()
	if p == nil {
		return StdinResponse{Error: "unknown pid", DurationMs: time.Since(start).Milliseconds()}
	}
	data := []byte(in.Data)
	if len(data) > DefaultMaxStdin {
		data = data[:DefaultMaxStdin]
	}
	n, err := p.stdin.Write(data)
	if err != nil {
		return StdinResponse{BytesWritten: n, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	audit(struct {
		TS    string `json:"ts"`
		Tool  string `json:"tool"`
		PID   int    `json:"pid"`
		Bytes int    `json:"bytes"`
	}{time.Now().UTC().Format(time.RFC3339), "proc.stdin", in.Pid, n})
	return StdinResponse{BytesWritten: n, DurationMs: time.Since(start).Milliseconds()}
}

func Wait(ctx context.Context, in WaitRequest) WaitResponse {
	start := time.Now()
	procMu.Lock()
	p := processes[in.Pid]
	procMu.Unlock()
	if p == nil {
		return WaitResponse{ExitCode: 1, Error: "unknown pid", DurationMs: time.Since(start).Milliseconds()}
	}
	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case <-p.done:
	case <-ctx.Done():
		return WaitResponse{ExitCode: 124, Stdout: p.stdoutBuf.String(), Stderr: p.stderrBuf.String(), Truncated: *p.stdoutTrunc || *p.stderrTrunc, DurationMs: time.Since(start).Milliseconds(), Error: "timeout"}
	}
	resp := WaitResponse{
		ExitCode:   p.exitCode,
		Stdout:     p.stdoutBuf.String(),
		Stderr:     p.stderrBuf.String(),
		Truncated:  *p.stdoutTrunc || *p.stderrTrunc,
		DurationMs: time.Since(start).Milliseconds(),
	}
	audit(struct {
		TS       string `json:"ts"`
		Tool     string `json:"tool"`
		PID      int    `json:"pid"`
		Exit     int    `json:"exit"`
		BytesOut int    `json:"bytes_out"`
	}{time.Now().UTC().Format(time.RFC3339), "proc.wait", in.Pid, resp.ExitCode, len(resp.Stdout) + len(resp.Stderr)})
	procMu.Lock()
	delete(processes, in.Pid)
	procMu.Unlock()
	return resp
}

func Kill(ctx context.Context, in KillRequest) KillResponse {
	start := time.Now()
	procMu.Lock()
	p := processes[in.Pid]
	procMu.Unlock()
	if p == nil || p.cmd.Process == nil {
		return KillResponse{Error: "unknown pid", DurationMs: time.Since(start).Milliseconds()}
	}
	sig := syscall.SIGTERM
	if in.Signal != 0 {
		sig = syscall.Signal(in.Signal)
	}
	err := syscall.Kill(-p.cmd.Process.Pid, sig)
	if err != nil {
		return KillResponse{Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	audit(struct {
		TS     string `json:"ts"`
		Tool   string `json:"tool"`
		PID    int    `json:"pid"`
		Signal int    `json:"signal"`
	}{time.Now().UTC().Format(time.RFC3339), "proc.kill", in.Pid, int(sig)})
	return KillResponse{Killed: true, DurationMs: time.Since(start).Milliseconds()}
}

func List(ctx context.Context, _ ListRequest) ListResponse {
	start := time.Now()
	procMu.Lock()
	defer procMu.Unlock()
	res := ListResponse{DurationMs: time.Since(start).Milliseconds()}
	for pid, p := range processes {
		res.Processes = append(res.Processes, ProcInfo{
			Pid:       pid,
			Cmdline:   p.cmd.String(),
			StartTime: p.start.UTC().Format(time.RFC3339),
			Cwd:       p.cwd,
		})
	}
	audit(struct {
		TS    string `json:"ts"`
		Tool  string `json:"tool"`
		Count int    `json:"count"`
	}{time.Now().UTC().Format(time.RFC3339), "proc.list", len(res.Processes)})
	return res
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

package shell

import (
	"context"
	"strings"
	"testing"
)

func TestRunSuccess(t *testing.T) {
	resp := Run(context.Background(), ExecRequest{Cmd: "echo hi"})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", resp.ExitCode)
	}
	if strings.TrimSpace(resp.Stdout) != "hi" {
		t.Fatalf("unexpected stdout: %q", resp.Stdout)
	}
}

func TestRunFailure(t *testing.T) {
	resp := Run(context.Background(), ExecRequest{Cmd: "exit 7"})
	if resp.ExitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", resp.ExitCode)
	}
}

func TestRunTimeout(t *testing.T) {
	resp := Run(context.Background(), ExecRequest{Cmd: "sleep 1", TimeoutMs: 100})
	if resp.ExitCode != 124 {
		t.Fatalf("expected exit code 124, got %d", resp.ExitCode)
	}
}

func TestRunStdoutTruncation(t *testing.T) {
	resp := Run(context.Background(), ExecRequest{Cmd: "yes | head -c 2000", MaxBytes: 100})
	if !resp.StdoutTruncated {
		t.Fatalf("expected stdout truncated")
	}
	if len(resp.Stdout) > 100 {
		t.Fatalf("stdout length %d exceeds limit", len(resp.Stdout))
	}
}

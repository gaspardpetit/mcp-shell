package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestPythonRun(t *testing.T) {
	resp := PythonRun(context.Background(), PythonRunRequest{Code: "print(1+1)"})
	if resp.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", resp.ExitCode)
	}
	if strings.TrimSpace(resp.Stdout) != "2" {
		t.Fatalf("unexpected stdout %q", resp.Stdout)
	}
}

func TestNodeRun(t *testing.T) {
	resp := NodeRun(context.Background(), NodeRunRequest{Code: "console.log(1+2)"})
	if resp.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", resp.ExitCode)
	}
	if strings.TrimSpace(resp.Stdout) != "3" {
		t.Fatalf("unexpected stdout %q", resp.Stdout)
	}
}

func TestShScriptWriteAndRun(t *testing.T) {
	resp := ShScriptWriteAndRun(context.Background(), ShRequest{Shebang: "/bin/bash", Content: "echo hi"})
	if resp.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", resp.ExitCode)
	}
	if strings.TrimSpace(resp.Stdout) != "hi" {
		t.Fatalf("unexpected stdout %q", resp.Stdout)
	}
}

func TestPythonRunError(t *testing.T) {
	resp := PythonRun(context.Background(), PythonRunRequest{Code: "raise ValueError('x')"})
	if resp.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code")
	}
	if !strings.Contains(resp.Stderr, "ValueError") {
		t.Fatalf("stderr missing ValueError: %s", resp.Stderr)
	}
}

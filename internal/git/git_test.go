package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCloneDisabled(t *testing.T) {
	os.Setenv("EGRESS", "0")
	resp := Clone(context.Background(), CloneRequest{Repo: "https://example.com/foo.git"})
	if resp.ExitCode == 0 {
		t.Fatalf("expected failure when egress disabled")
	}
}

func TestStatusAndCommit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE", root)
	dir, err := os.MkdirTemp(root, "git-test-")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cmd := exec.Command("git", "init", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, out)
	}
	// configure user identity for commits
	cmd = exec.Command("git", "-C", dir, "config", "user.email", "test@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config email: %v (%s)", err, out)
	}
	cmd = exec.Command("git", "-C", dir, "config", "user.name", "Test User")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config name: %v (%s)", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "foo.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	// stage file
	cmd = exec.Command("git", "-C", dir, "add", "foo.txt")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v (%s)", err, out)
	}
	resp := Commit(context.Background(), CommitRequest{Path: dir, Message: "init"})
	if resp.ExitCode != 0 {
		t.Fatalf("commit failed: %v", resp.Error)
	}
	stat := Status(context.Background(), StatusRequest{Path: dir})
	if stat.ExitCode != 0 {
		t.Fatalf("status exit %d: %v", stat.ExitCode, stat.Error)
	}
	if stat.Stdout != "" {
		t.Fatalf("expected clean repo, got %q", stat.Stdout)
	}
}

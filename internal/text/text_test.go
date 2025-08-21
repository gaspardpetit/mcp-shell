package text

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiffAndPatch(t *testing.T) {
	ctx := context.Background()
	a := "line1\nline2\n"
	b := "line1_changed\nline2\n"
	d := Diff(ctx, DiffRequest{A: a, B: b})
	if d.Error != "" || d.UnifiedDiff == "" {
		t.Fatalf("diff resp %+v", d)
	}
	ws := t.TempDir()
	t.Setenv("WORKSPACE", ws)
	path := filepath.Join(ws, "file.txt")
	if err := os.WriteFile(path, []byte(a), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	p := ApplyPatch(ctx, ApplyPatchRequest{Path: path, UnifiedDiff: d.UnifiedDiff})
	if p.Error != "" || !p.Patched || p.HunksFailed != 0 {
		t.Fatalf("patch resp %+v", p)
	}
	data, _ := os.ReadFile(path)
	if string(data) != b {
		t.Fatalf("patched content %q", data)
	}
}

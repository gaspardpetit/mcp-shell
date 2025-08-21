package archive

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestZipUnzip(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	t.Setenv("WORKSPACE", ws)
	srcDir := filepath.Join(ws, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "b.txt"), []byte("world"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	zipPath := filepath.Join(ws, "out.zip")
	if resp := Zip(ctx, ZipRequest{Src: srcDir, Dest: zipPath}); resp.Error != "" || resp.Files != 2 {
		t.Fatalf("zip resp %+v", resp)
	}
	destDir := filepath.Join(ws, "unz")
	if resp := Unzip(ctx, UnzipRequest{Src: zipPath, Dest: destDir}); resp.Error != "" || resp.Files != 2 {
		t.Fatalf("unzip resp %+v", resp)
	}
	if _, err := os.Stat(filepath.Join(destDir, "a.txt")); err != nil {
		t.Fatalf("stat a.txt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "b.txt")); err != nil {
		t.Fatalf("stat b.txt: %v", err)
	}
}

func TestTarUntar(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	t.Setenv("WORKSPACE", ws)
	srcDir := filepath.Join(ws, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tarPath := filepath.Join(ws, "out.tar")
	if resp := Tar(ctx, TarRequest{Src: srcDir, Dest: tarPath}); resp.Error != "" || resp.Files != 1 {
		t.Fatalf("tar resp %+v", resp)
	}
	destDir := filepath.Join(ws, "unt")
	if resp := Untar(ctx, UntarRequest{Src: tarPath, Dest: destDir}); resp.Error != "" || resp.Files != 1 {
		t.Fatalf("untar resp %+v", resp)
	}
	if _, err := os.Stat(filepath.Join(destDir, "a.txt")); err != nil {
		t.Fatalf("stat a.txt: %v", err)
	}
}

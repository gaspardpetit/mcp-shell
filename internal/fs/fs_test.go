package fs

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestFSRoundTrip(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	t.Setenv("WORKSPACE", ws)

	// Write a file
	if resp := Write(ctx, WriteRequest{Path: "dir/file.txt", Content: "hello", CreateParents: true}); resp.Error != "" {
		t.Fatalf("write error: %v", resp.Error)
	}

	// Read the file
	if resp := Read(ctx, ReadRequest{Path: "dir/file.txt"}); resp.Error != "" || resp.Content != "hello" {
		t.Fatalf("read got %+v", resp)
	}

	// Read base64
	if resp := ReadB64(ctx, ReadRequest{Path: "dir/file.txt"}); resp.Error != "" {
		t.Fatalf("read_b64 error: %v", resp.Error)
	} else if b, _ := base64.StdEncoding.DecodeString(resp.ContentB64); string(b) != "hello" {
		t.Fatalf("read_b64 content %q", b)
	}

	// Stat directory
	if resp := Stat(ctx, StatRequest{Path: "dir"}); resp.Error != "" || resp.Type != "dir" {
		t.Fatalf("stat dir got %+v", resp)
	}

	// List directory
	if resp := List(ctx, ListRequest{Path: "dir"}); resp.Error != "" || len(resp.Entries) != 1 {
		t.Fatalf("list got %+v", resp)
	}

	// Copy file
	if resp := Copy(ctx, CopyRequest{Src: "dir/file.txt", Dest: "dir/copy.txt"}); resp.Error != "" {
		t.Fatalf("copy error: %v", resp.Error)
	}

	// Move file
	if resp := Move(ctx, MoveRequest{Src: "dir/copy.txt", Dest: "dir/moved.txt"}); resp.Error != "" {
		t.Fatalf("move error: %v", resp.Error)
	}

	// Remove files
	if resp := Remove(ctx, RemoveRequest{Path: "dir/moved.txt"}); resp.Error != "" {
		t.Fatalf("remove error: %v", resp.Error)
	}
	if resp := Remove(ctx, RemoveRequest{Path: "dir/file.txt"}); resp.Error != "" {
		t.Fatalf("remove error: %v", resp.Error)
	}

	// Mkdir
	if resp := Mkdir(ctx, MkdirRequest{Path: "dir/sub/sub2", Parents: true}); resp.Error != "" || !resp.Created {
		t.Fatalf("mkdir got %+v", resp)
	}

	// Remove directory recursively
	if resp := Remove(ctx, RemoveRequest{Path: "dir", Recursive: true}); resp.Error != "" {
		t.Fatalf("remove dir error: %v", resp.Error)
	}
}

func TestPathEscapeDenied(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	t.Setenv("WORKSPACE", ws)
	outside := filepath.Join(ws, "..", "haxx")
	if resp := Write(ctx, WriteRequest{Path: outside, Content: "hi"}); resp.Error == "" {
		t.Fatalf("expected error for outside path")
	}
}

func TestReadBinaryFails(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	t.Setenv("WORKSPACE", ws)
	bin := filepath.Join(ws, "bin.dat")
	if err := os.WriteFile(bin, []byte{0xff, 0x00, 0x01}, 0o644); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	if resp := Read(ctx, ReadRequest{Path: "bin.dat"}); resp.Error == "" {
		t.Fatalf("expected utf-8 error")
	}
}

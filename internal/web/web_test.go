package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHTTPRequestTool(t *testing.T) {
	os.Setenv("EGRESS", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "1" {
			t.Errorf("missing header")
		}
		w.Write([]byte("abcdef"))
	}))
	defer srv.Close()
	resp := HTTPRequestTool(context.Background(), HTTPRequest{Method: "GET", URL: srv.URL, Headers: map[string]string{"X-Test": "1"}, MaxBytes: 3})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if resp.Status != 200 {
		t.Fatalf("expected status 200, got %d", resp.Status)
	}
	if resp.Body != "abc" || !resp.Truncated {
		t.Fatalf("unexpected body %q truncated=%v", resp.Body, resp.Truncated)
	}
}

func TestDownload(t *testing.T) {
	t.Setenv("EGRESS", "1")
	t.Setenv("WORKSPACE", t.TempDir())
	data := []byte("download me")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}))
	defer srv.Close()
	sum := sha256.Sum256(data)
	dest := filepath.Join(workspaceRoot(), "download.test")
	defer os.Remove(dest)
	resp := Download(context.Background(), DownloadRequest{URL: srv.URL, DestPath: dest, ExpectedSHA256: hex.EncodeToString(sum[:])})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if resp.Size != int64(len(data)) {
		t.Fatalf("expected size %d, got %d", len(data), resp.Size)
	}
	if resp.Sha256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha mismatch")
	}
}

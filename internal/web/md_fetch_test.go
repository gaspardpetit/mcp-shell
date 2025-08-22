package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestFetchMarkdown(t *testing.T) {
	t.Setenv("EGRESS", "1")
	ws := t.TempDir()
	t.Setenv("WORKSPACE", ws)
	html := `<!doctype html><html><head><title>Example Domain</title></head><body><article><h1>Example Domain</h1><p>This domain is for use in illustrative examples.</p></article></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(html))
	}))
	defer srv.Close()
	resp := FetchMarkdown(context.Background(), MDFetchRequest{URL: srv.URL, SaveArtifacts: true})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if !strings.Contains(resp.Markdown, "Example Domain") {
		t.Fatalf("markdown missing content: %s", resp.Markdown)
	}
	if resp.Truncated {
		t.Fatalf("unexpected truncation")
	}
	if resp.Artifacts == nil || resp.Artifacts.HTMLPath == "" || resp.Artifacts.MDPath == "" {
		t.Fatalf("missing artifact paths")
	}
	if _, err := os.Stat(resp.Artifacts.HTMLPath); err != nil {
		t.Fatalf("html artifact not found")
	}
	if _, err := os.Stat(resp.Artifacts.MDPath); err != nil {
		t.Fatalf("md artifact not found")
	}
}

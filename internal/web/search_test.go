package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearch(t *testing.T) {
	t.Setenv("EGRESS", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		resp := struct {
			Results []map[string]string `json:"results"`
		}{Results: []map[string]string{
			{"title": "Example Domain", "url": "https://example.com", "content": "Example Domain snippet", "published": "2024-01-01T00:00:00Z", "engine": "duckduckgo"},
			{"title": "Other", "url": "https://other.com", "content": "Other snippet", "engine": "bing"},
		}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	t.Setenv("SEARXNG_URL", srv.URL)
	resp := Search(context.Background(), SearchRequest{Query: "example", NumResults: 1})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0].Title != "Example Domain" {
		t.Fatalf("unexpected title: %s", resp.Results[0].Title)
	}
}

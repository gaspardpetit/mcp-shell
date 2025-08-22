package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SearchRequest defines parameters for the web.search tool.
type SearchRequest struct {
	Query      string   `json:"query"`
	NumResults int      `json:"num_results,omitempty"`
	Engines    []string `json:"engines,omitempty"`
	Safesearch string   `json:"safesearch,omitempty"`
	TimeRange  string   `json:"time_range,omitempty"`
	Language   string   `json:"language,omitempty"`
	TimeoutMs  int      `json:"timeout_ms,omitempty"`
}

// SearchResult represents a single search hit.
type SearchResult struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Snippet   string `json:"snippet"`
	Published string `json:"published,omitempty"`
	Source    string `json:"source"`
}

// SearchResponse wraps the results of a search query.
type SearchResponse struct {
	Results    []SearchResult `json:"results"`
	DurationMs int64          `json:"duration_ms"`
	Error      string         `json:"error,omitempty"`
}

// Search performs a SearxNG query and returns normalized results.
func Search(ctx context.Context, in SearchRequest) SearchResponse {
	start := time.Now()
	if !egressAllowed() {
		return SearchResponse{DurationMs: time.Since(start).Milliseconds(), Error: "egress disabled"}
	}
	if strings.TrimSpace(in.Query) == "" {
		return SearchResponse{DurationMs: time.Since(start).Milliseconds(), Error: "query is required"}
	}
	base := os.Getenv("SEARXNG_BASE")
	if base == "" {
		base = os.Getenv("SEARXNG_URL")
	}
	if base == "" {
		base = "http://localhost:8080"
	}
	u, err := url.Parse(base)
	if err != nil {
		return SearchResponse{DurationMs: time.Since(start).Milliseconds(), Error: "invalid searxng base url"}
	}
	u.Path = "/search"
	q := u.Query()
	q.Set("format", "json")
	q.Set("q", in.Query)
	if len(in.Engines) > 0 {
		q.Set("engines", strings.Join(in.Engines, ","))
	}
	if in.Language != "" {
		q.Set("language", in.Language)
	}
	switch strings.ToLower(in.Safesearch) {
	case "off":
		q.Set("safesearch", "0")
	case "moderate":
		q.Set("safesearch", "1")
	case "strict":
		q.Set("safesearch", "2")
	}
	switch strings.ToLower(in.TimeRange) {
	case "day", "week", "month", "year":
		q.Set("time_range", strings.ToLower(in.TimeRange))
	}
	u.RawQuery = q.Encode()

	timeout := 10 * time.Second
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return SearchResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return SearchResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	defer resp.Body.Close()
	var body struct {
		Results []struct {
			Title     string `json:"title"`
			URL       string `json:"url"`
			Content   string `json:"content"`
			Published string `json:"published"`
			Engine    string `json:"engine"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return SearchResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	limit := in.NumResults
	if limit <= 0 || limit > len(body.Results) {
		limit = len(body.Results)
	}
	out := SearchResponse{Results: make([]SearchResult, 0, limit)}
	for i := 0; i < limit; i++ {
		r := body.Results[i]
		out.Results = append(out.Results, SearchResult{
			Title:     r.Title,
			URL:       r.URL,
			Snippet:   r.Content,
			Published: r.Published,
			Source:    r.Engine,
		})
	}
	out.DurationMs = time.Since(start).Milliseconds()
	auditSearch(in, out)
	return out
}

func auditSearch(in SearchRequest, out SearchResponse) {
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
	rec := struct {
		TS       string `json:"ts"`
		Tool     string `json:"tool"`
		Query    string `json:"query"`
		Results  int    `json:"results"`
		Duration int64  `json:"duration_ms"`
	}{time.Now().UTC().Format(time.RFC3339), "web.search", in.Query, len(out.Results), out.DurationMs}
	_ = json.NewEncoder(f).Encode(rec)
}

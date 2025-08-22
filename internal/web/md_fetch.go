package web

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	markdown "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/go-shiori/go-readability"
)

const (
	defaultFetchTimeout        = 15 * time.Second
	defaultFetchMaxBytes int64 = 2 * 1024 * 1024 // 2 MiB
)

// MDFetchRequest defines the input for md.fetch.
type MDFetchRequest struct {
	URL              string `json:"url"`
	TimeoutMs        int    `json:"timeout_ms,omitempty"`
	MaxBytes         int64  `json:"max_bytes,omitempty"`
	AllowInsecureTLS bool   `json:"allow_insecure_tls,omitempty"`
	RenderJS         bool   `json:"render_js,omitempty"`
	SaveArtifacts    bool   `json:"save_artifacts,omitempty"`
}

// MDFetchResponse is the output for md.fetch.
type MDFetchResponse struct {
	Title        string `json:"title,omitempty"`
	Byline       string `json:"byline,omitempty"`
	SiteName     string `json:"site_name,omitempty"`
	Published    string `json:"published,omitempty"`
	CanonicalURL string `json:"canonical_url,omitempty"`
	Markdown     string `json:"markdown"`
	Truncated    bool   `json:"truncated"`
	Artifacts    *struct {
		HTMLPath string `json:"html_path,omitempty"`
		MDPath   string `json:"md_path,omitempty"`
	} `json:"artifacts,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// FetchMarkdown retrieves a page and converts the main content to Markdown.
func FetchMarkdown(ctx context.Context, in MDFetchRequest) MDFetchResponse {
	start := time.Now()
	if !egressAllowed() {
		return MDFetchResponse{DurationMs: time.Since(start).Milliseconds(), Error: "egress disabled"}
	}
	if in.RenderJS {
		return MDFetchResponse{DurationMs: time.Since(start).Milliseconds(), Error: "render_js not supported"}
	}
	if in.URL == "" {
		return MDFetchResponse{DurationMs: time.Since(start).Milliseconds(), Error: "url is required"}
	}
	timeout := defaultFetchTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	maxBytes := defaultFetchMaxBytes
	if in.MaxBytes > 0 {
		maxBytes = in.MaxBytes
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if in.AllowInsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	client := &http.Client{Timeout: timeout, Transport: transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.URL, nil)
	if err != nil {
		return MDFetchResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return MDFetchResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return MDFetchResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	truncated := int64(len(data)) > maxBytes
	if truncated {
		data = data[:int(maxBytes)]
	}
	u, err := url.Parse(in.URL)
	if err != nil {
		return MDFetchResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	doc, err := readability.FromReader(strings.NewReader(string(data)), u)
	if err != nil {
		return MDFetchResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	converter := markdown.NewConverter("", true, nil)
	md, err := converter.ConvertString(doc.Content)
	if err != nil {
		return MDFetchResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	if doc.Title != "" && !strings.Contains(md, doc.Title) {
		md = "# " + doc.Title + "\n\n" + md
	}
	out := MDFetchResponse{
		Title:        doc.Title,
		Byline:       doc.Byline,
		SiteName:     doc.SiteName,
		Markdown:     md,
		Truncated:    truncated,
		DurationMs:   time.Since(start).Milliseconds(),
		CanonicalURL: in.URL,
	}
	if doc.PublishedTime != nil {
		out.Published = doc.PublishedTime.Format(time.RFC3339)
	}
	if in.SaveArtifacts {
		cacheDir := filepath.Join(workspaceRoot(), ".cache", "web")
		_ = os.MkdirAll(cacheDir, 0o755)
		hash := sha256.Sum256([]byte(in.URL))
		prefix := hex.EncodeToString(hash[:8])
		htmlPath := filepath.Join(cacheDir, prefix+".html")
		mdPath := filepath.Join(cacheDir, prefix+".md")
		_ = os.WriteFile(htmlPath, data, 0o644)
		_ = os.WriteFile(mdPath, []byte(md), 0o644)
		out.Artifacts = &struct {
			HTMLPath string `json:"html_path,omitempty"`
			MDPath   string `json:"md_path,omitempty"`
		}{HTMLPath: htmlPath, MDPath: mdPath}
	}
	auditMDFetch(in, out)
	return out
}

func auditMDFetch(in MDFetchRequest, out MDFetchResponse) {
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
		URL      string `json:"url"`
		Duration int64  `json:"duration_ms"`
		Trunc    bool   `json:"truncated"`
	}{time.Now().UTC().Format(time.RFC3339), "md.fetch", in.URL, out.DurationMs, out.Truncated}
	_ = json.NewEncoder(f).Encode(rec)
}

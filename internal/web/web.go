package web

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	DefaultTimeout       = 60 * time.Second
	DefaultMaxBody int64 = 1 << 20 // 1 MiB
	LogPath              = "/logs/mcp-shell.log"
)

func egressAllowed() bool {
	return os.Getenv("EGRESS") == "1"
}

// workspace root for downloads
func workspaceRoot() string {
	if ws := os.Getenv("WORKSPACE"); ws != "" {
		return filepath.Clean(ws)
	}
	return "/workspace"
}

func normalizePath(p string) (string, error) {
	if p == "" {
		return "", errors.New("dest_path is required")
	}
	root := workspaceRoot()
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	p = filepath.Clean(p)
	rel, err := filepath.Rel(root, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", errors.New("path escapes workspace")
	}
	return p, nil
}

// ---- http.request ----

type HTTPRequest struct {
	Method           string            `json:"method"`
	URL              string            `json:"url"`
	Headers          map[string]string `json:"headers,omitempty"`
	Body             string            `json:"body,omitempty"`
	BodyB64          string            `json:"body_b64,omitempty"`
	TimeoutMs        int               `json:"timeout_ms,omitempty"`
	MaxBytes         int64             `json:"max_bytes,omitempty"`
	AllowInsecureTLS bool              `json:"allow_insecure_tls,omitempty"`
}

type HTTPResponse struct {
	Status     int                 `json:"status"`
	Headers    map[string][]string `json:"headers"`
	Body       string              `json:"body,omitempty"`
	BodyB64    string              `json:"body_b64,omitempty"`
	Truncated  bool                `json:"truncated"`
	DurationMs int64               `json:"duration_ms"`
	Error      string              `json:"error,omitempty"`
}

func HTTPRequestTool(ctx context.Context, in HTTPRequest) HTTPResponse {
	start := time.Now()
	if !egressAllowed() {
		return HTTPResponse{DurationMs: time.Since(start).Milliseconds(), Error: "egress disabled"}
	}
	if in.Method == "" {
		in.Method = http.MethodGet
	}
	if in.URL == "" {
		return HTTPResponse{DurationMs: time.Since(start).Milliseconds(), Error: "url is required"}
	}
	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	limit := DefaultMaxBody
	if in.MaxBytes > 0 {
		limit = in.MaxBytes
	}
	var bodyReader io.Reader
	if in.Body != "" && in.BodyB64 != "" {
		return HTTPResponse{DurationMs: time.Since(start).Milliseconds(), Error: "body and body_b64 are mutually exclusive"}
	}
	if in.Body != "" {
		bodyReader = strings.NewReader(in.Body)
	} else if in.BodyB64 != "" {
		b, err := base64.StdEncoding.DecodeString(in.BodyB64)
		if err != nil {
			return HTTPResponse{DurationMs: time.Since(start).Milliseconds(), Error: "invalid body_b64"}
		}
		bodyReader = strings.NewReader(string(b))
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, in.Method, in.URL, bodyReader)
	if err != nil {
		return HTTPResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	for k, v := range in.Headers {
		req.Header.Set(k, v)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if in.AllowInsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	client := &http.Client{Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		return HTTPResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return HTTPResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	truncated := int64(len(data)) > limit
	if truncated {
		data = data[:int(limit)]
	}
	out := HTTPResponse{Status: resp.StatusCode, Headers: resp.Header, Truncated: truncated}
	if utf8.Valid(data) {
		out.Body = string(data)
	} else {
		out.BodyB64 = base64.StdEncoding.EncodeToString(data)
	}
	out.DurationMs = time.Since(start).Milliseconds()
	auditHTTPRequest(in, out, len(data))
	return out
}

// ---- web.download ----

type DownloadRequest struct {
	URL              string `json:"url"`
	DestPath         string `json:"dest_path"`
	ExpectedSHA256   string `json:"expected_sha256,omitempty"`
	TimeoutMs        int    `json:"timeout_ms,omitempty"`
	AllowInsecureTLS bool   `json:"allow_insecure_tls,omitempty"`
}

type DownloadResponse struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Sha256     string `json:"sha256"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func Download(ctx context.Context, in DownloadRequest) DownloadResponse {
	start := time.Now()
	if !egressAllowed() {
		return DownloadResponse{DurationMs: time.Since(start).Milliseconds(), Error: "egress disabled"}
	}
	if in.URL == "" || in.DestPath == "" {
		return DownloadResponse{DurationMs: time.Since(start).Milliseconds(), Error: "url and dest_path are required"}
	}
	dest, err := normalizePath(in.DestPath)
	if err != nil {
		return DownloadResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	timeout := DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.URL, nil)
	if err != nil {
		return DownloadResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if in.AllowInsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	client := &http.Client{Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		return DownloadResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return DownloadResponse{DurationMs: time.Since(start).Milliseconds(), Error: resp.Status}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return DownloadResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	f, err := os.Create(dest)
	if err != nil {
		return DownloadResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	defer f.Close()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(f, hash), resp.Body)
	if err != nil {
		return DownloadResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	if in.ExpectedSHA256 != "" && !strings.EqualFold(sum, in.ExpectedSHA256) {
		return DownloadResponse{DurationMs: time.Since(start).Milliseconds(), Error: "sha256 mismatch"}
	}
	out := DownloadResponse{Path: dest, Size: size, Sha256: sum, DurationMs: time.Since(start).Milliseconds()}
	auditDownload(in, out)
	return out
}

func auditHTTPRequest(in HTTPRequest, out HTTPResponse, bytesOut int) {
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
		TS        string `json:"ts"`
		Tool      string `json:"tool"`
		Method    string `json:"method"`
		URL       string `json:"url"`
		Status    int    `json:"status"`
		Duration  int64  `json:"duration_ms"`
		BytesOut  int    `json:"bytes_out"`
		Truncated bool   `json:"truncated"`
	}{time.Now().UTC().Format(time.RFC3339), "http.request", in.Method, in.URL, out.Status, out.DurationMs, bytesOut, out.Truncated}
	_ = json.NewEncoder(f).Encode(rec)
}

func auditDownload(in DownloadRequest, out DownloadResponse) {
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
		Dest     string `json:"dest"`
		Size     int64  `json:"size"`
		Sha256   string `json:"sha256"`
		Duration int64  `json:"duration_ms"`
	}{time.Now().UTC().Format(time.RFC3339), "web.download", in.URL, out.Path, out.Size, out.Sha256, out.DurationMs}
	_ = json.NewEncoder(f).Encode(rec)
}

package doc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	LogPath         = "/logs/mcp-shell.log"
	defaultMaxBytes = 1 << 20 // 1 MiB
)

func workspaceRoot() string {
	if ws := os.Getenv("WORKSPACE"); ws != "" {
		return filepath.Clean(ws)
	}
	return "/workspace"
}

func allowOutside() bool {
	v := os.Getenv("FS_ALLOW_OUTSIDE_WORKSPACE")
	return v == "1" || strings.EqualFold(v, "true")
}

func normalizePath(p string) (string, error) {
	if p == "" {
		return "", errors.New("path is required")
	}
	root := workspaceRoot()
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	p = filepath.Clean(p)
	if allowOutside() {
		return p, nil
	}
	rel, err := filepath.Rel(root, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", errors.New("path escapes workspace")
	}
	return p, nil
}

func audit(rec any) {
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
	_ = json.NewEncoder(f).Encode(rec)
}

// ---- doc.convert ----

type ConvertRequest struct {
	SrcPath    string            `json:"src_path"`
	DestFormat string            `json:"dest_format"`
	Options    map[string]string `json:"options,omitempty"`
}

type ConvertResponse struct {
	DestPath   string `json:"dest_path"`
	Size       int64  `json:"size"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func Convert(ctx context.Context, in ConvertRequest) ConvertResponse {
	start := time.Now()
	src, err := normalizePath(in.SrcPath)
	if err != nil {
		return ConvertResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	if in.DestFormat == "" {
		return ConvertResponse{DurationMs: time.Since(start).Milliseconds(), Error: "dest_format is required"}
	}
	destFormat := strings.ToLower(in.DestFormat)
	dir := filepath.Dir(src)
	base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	dest := filepath.Join(dir, base+"."+destFormat)

	var cmd *exec.Cmd
	switch destFormat {
	case "md":
		cmd = exec.CommandContext(ctx, "pandoc", src, "-o", dest)
	default:
		args := []string{"--headless", "--convert-to", destFormat, "--outdir", dir, src}
		cmd = exec.CommandContext(ctx, "libreoffice", args...)
	}
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return ConvertResponse{DurationMs: time.Since(start).Milliseconds(), Error: stderr.String()}
	}
	info, err := os.Stat(dest)
	if err != nil {
		return ConvertResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	resp := ConvertResponse{DestPath: dest, Size: info.Size()}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Src        string `json:"src"`
		Dest       string `json:"dest"`
		DurationMs int64  `json:"duration_ms"`
		Size       int64  `json:"size"`
	}{time.Now().UTC().Format(time.RFC3339), "doc.convert", src, dest, resp.DurationMs, resp.Size})
	return resp
}

// ---- pdf.extract_text ----

type PDFExtractRequest struct {
	Path     string `json:"path"`
	Layout   string `json:"layout,omitempty"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
}

type PDFExtractResponse struct {
	Text       string `json:"text"`
	Truncated  bool   `json:"truncated"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func ExtractText(ctx context.Context, in PDFExtractRequest) PDFExtractResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return PDFExtractResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	limit := defaultMaxBytes
	if in.MaxBytes > 0 {
		limit = int(in.MaxBytes)
	}
	layout := strings.ToLower(in.Layout)
	var cmd *exec.Cmd
	switch layout {
	case "layout":
		cmd = exec.CommandContext(ctx, "pdftotext", "-layout", path, "-")
	case "html":
		cmd = exec.CommandContext(ctx, "pdftohtml", "-i", "-stdout", "-noframes", path, "-")
	default:
		cmd = exec.CommandContext(ctx, "pdftotext", path, "-")
	}
	var stdout bytes.Buffer
	lw := &limitedWriter{buf: &stdout, limit: limit}
	var stderr bytes.Buffer
	cmd.Stdout = lw
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return PDFExtractResponse{DurationMs: time.Since(start).Milliseconds(), Error: stderr.String()}
	}
	resp := PDFExtractResponse{Text: stdout.String(), Truncated: lw.truncated}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Path       string `json:"path"`
		DurationMs int64  `json:"duration_ms"`
		BytesOut   int    `json:"bytes_out"`
	}{time.Now().UTC().Format(time.RFC3339), "pdf.extract_text", path, resp.DurationMs, len(resp.Text)})
	return resp
}

// ---- spreadsheet.to_csv ----

type ToCSVRequest struct {
	Path     string          `json:"path"`
	Sheet    json.RawMessage `json:"sheet,omitempty"`
	MaxBytes int64           `json:"max_bytes,omitempty"`
}

type ToCSVResponse struct {
	Csv        string `json:"csv"`
	Truncated  bool   `json:"truncated"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func SpreadsheetToCSV(ctx context.Context, in ToCSVRequest) ToCSVResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return ToCSVResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	limit := defaultMaxBytes
	if in.MaxBytes > 0 {
		limit = int(in.MaxBytes)
	}
	dir := filepath.Dir(path)
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	dest := filepath.Join(dir, base+".csv")
	args := []string{"--headless", "--convert-to", "csv", "--outdir", dir}
	if len(in.Sheet) > 0 {
		var name string
		if err := json.Unmarshal(in.Sheet, &name); err == nil {
			if name != "" {
				args = append(args, "--calc-sheets", name)
			}
		} else {
			var idx int
			if err := json.Unmarshal(in.Sheet, &idx); err == nil && idx > 0 {
				args = append(args, "--calc-sheets", strconv.Itoa(idx))
			}
		}
	}
	args = append(args, path)
	cmd := exec.CommandContext(ctx, "libreoffice", args...)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return ToCSVResponse{DurationMs: time.Since(start).Milliseconds(), Error: stderr.String()}
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		return ToCSVResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	truncated := false
	if len(data) > limit {
		data = data[:limit]
		truncated = true
	}
	resp := ToCSVResponse{Csv: string(data), Truncated: truncated}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string          `json:"ts"`
		Tool       string          `json:"tool"`
		Path       string          `json:"path"`
		DurationMs int64           `json:"duration_ms"`
		BytesOut   int             `json:"bytes_out"`
		Sheet      json.RawMessage `json:"sheet,omitempty"`
	}{time.Now().UTC().Format(time.RFC3339), "spreadsheet.to_csv", path, resp.DurationMs, len(resp.Csv), in.Sheet})
	return resp
}

// ---- doc.metadata ----

type MetadataRequest struct {
	Path string `json:"path"`
}

type MetadataResponse struct {
	Mime       string `json:"mime"`
	Pages      int    `json:"pages,omitempty"`
	Words      int    `json:"words,omitempty"`
	Created    string `json:"created,omitempty"`
	Modified   string `json:"modified,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func Metadata(ctx context.Context, in MetadataRequest) MetadataResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return MetadataResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	mimeCmd := exec.CommandContext(ctx, "file", "-b", "--mime-type", path)
	var mimeOut bytes.Buffer
	mimeCmd.Stdout = &mimeOut
	if err := mimeCmd.Run(); err != nil {
		return MetadataResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	mime := strings.TrimSpace(mimeOut.String())
	resp := MetadataResponse{Mime: mime}
	if strings.Contains(mime, "pdf") {
		infoCmd := exec.CommandContext(ctx, "pdfinfo", path)
		var infoBuf bytes.Buffer
		infoCmd.Stdout = &infoBuf
		if err := infoCmd.Run(); err == nil {
			for _, line := range strings.Split(infoBuf.String(), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "Pages:") {
					fmt.Sscanf(line, "Pages: %d", &resp.Pages)
				} else if strings.HasPrefix(line, "CreationDate:") {
					resp.Created = strings.TrimSpace(strings.TrimPrefix(line, "CreationDate:"))
				} else if strings.HasPrefix(line, "ModDate:") {
					resp.Modified = strings.TrimSpace(strings.TrimPrefix(line, "ModDate:"))
				}
			}
		}
		txtCmd := exec.CommandContext(ctx, "pdftotext", path, "-")
		var txtBuf bytes.Buffer
		txtCmd.Stdout = &txtBuf
		_ = txtCmd.Run()
		resp.Words = len(strings.Fields(txtBuf.String()))
	}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Path       string `json:"path"`
		DurationMs int64  `json:"duration_ms"`
		Mime       string `json:"mime"`
	}{time.Now().UTC().Format(time.RFC3339), "doc.metadata", path, resp.DurationMs, resp.Mime})
	return resp
}

type limitedWriter struct {
	buf       *bytes.Buffer
	limit     int
	truncated bool
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.limit <= 0 {
		return w.buf.Write(p)
	}
	if len(p) <= w.limit-w.buf.Len() {
		return w.buf.Write(p)
	}
	need := w.limit - w.buf.Len()
	if need > 0 {
		w.buf.Write(p[:need])
	}
	w.truncated = true
	return len(p), nil
}

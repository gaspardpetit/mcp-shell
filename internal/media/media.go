package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// ---- image.convert ----

type ImageOp struct {
	Resize  string `json:"resize,omitempty"`
	Crop    string `json:"crop,omitempty"`
	Format  string `json:"format,omitempty"`
	Quality int    `json:"quality,omitempty"`
}

type ImageConvertRequest struct {
	SrcPath  string    `json:"src_path"`
	DestPath string    `json:"dest_path"`
	Ops      []ImageOp `json:"ops,omitempty"`
}

type ImageConvertResponse struct {
	DestPath   string `json:"dest_path"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func ImageConvert(ctx context.Context, in ImageConvertRequest) ImageConvertResponse {
	start := time.Now()
	src, err := normalizePath(in.SrcPath)
	if err != nil {
		return ImageConvertResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	dest, err := normalizePath(in.DestPath)
	if err != nil {
		return ImageConvertResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	args := []string{src}
	for _, op := range in.Ops {
		if op.Resize != "" {
			args = append(args, "-resize", op.Resize)
		}
		if op.Crop != "" {
			args = append(args, "-crop", op.Crop)
		}
		if op.Quality > 0 {
			args = append(args, "-quality", strconv.Itoa(op.Quality))
		}
	}
	args = append(args, dest)
	cmd := exec.CommandContext(ctx, "convert", args...)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return ImageConvertResponse{DurationMs: time.Since(start).Milliseconds(), Error: stderr.String()}
	}
	resp := ImageConvertResponse{DestPath: dest}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Src        string `json:"src"`
		Dest       string `json:"dest"`
		DurationMs int64  `json:"duration_ms"`
	}{time.Now().UTC().Format(time.RFC3339), "image.convert", src, dest, resp.DurationMs})
	return resp
}

// ---- video.transcode ----

type VideoTranscodeRequest struct {
	Src      string `json:"src"`
	Dest     string `json:"dest"`
	Codec    string `json:"codec,omitempty"`
	Crf      int    `json:"crf,omitempty"`
	Start    string `json:"start,omitempty"`
	Duration string `json:"duration,omitempty"`
}

type VideoTranscodeResponse struct {
	DestPath   string `json:"dest"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func VideoTranscode(ctx context.Context, in VideoTranscodeRequest) VideoTranscodeResponse {
	start := time.Now()
	src, err := normalizePath(in.Src)
	if err != nil {
		return VideoTranscodeResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	dest, err := normalizePath(in.Dest)
	if err != nil {
		return VideoTranscodeResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	args := []string{"-y"}
	if in.Start != "" {
		args = append(args, "-ss", in.Start)
	}
	args = append(args, "-i", src)
	if in.Duration != "" {
		args = append(args, "-t", in.Duration)
	}
	if in.Codec != "" {
		args = append(args, "-c:v", in.Codec)
	}
	if in.Crf > 0 {
		args = append(args, "-crf", strconv.Itoa(in.Crf))
	}
	args = append(args, dest)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return VideoTranscodeResponse{DurationMs: time.Since(start).Milliseconds(), Error: stderr.String()}
	}
	resp := VideoTranscodeResponse{DestPath: dest}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Src        string `json:"src"`
		Dest       string `json:"dest"`
		DurationMs int64  `json:"duration_ms"`
	}{time.Now().UTC().Format(time.RFC3339), "video.transcode", src, dest, resp.DurationMs})
	return resp
}

// ---- ocr.extract ----

type OCRRequest struct {
	Path     string `json:"path"`
	Lang     string `json:"lang,omitempty"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
}

type OCRResponse struct {
	Text       string `json:"text"`
	Truncated  bool   `json:"truncated"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func OCRExtract(ctx context.Context, in OCRRequest) OCRResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return OCRResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	args := []string{path, "stdout"}
	lang := in.Lang
	if lang == "" {
		lang = "eng"
	}
	args = append(args, "-l", lang)
	cmd := exec.CommandContext(ctx, "tesseract", args...)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return OCRResponse{DurationMs: time.Since(start).Milliseconds(), Error: stderr.String()}
	}
	data := out.Bytes()
	limit := in.MaxBytes
	if limit <= 0 {
		limit = defaultMaxBytes
	}
	truncated := false
	if int64(len(data)) > limit {
		data = data[:limit]
		truncated = true
	}
	resp := OCRResponse{Text: string(data), Truncated: truncated}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Path       string `json:"path"`
		DurationMs int64  `json:"duration_ms"`
		BytesOut   int    `json:"bytes_out"`
	}{time.Now().UTC().Format(time.RFC3339), "ocr.extract", path, resp.DurationMs, len(resp.Text)})
	return resp
}

package archive

import (
	"archive/tar"
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const LogPath = "/logs/mcp-shell.log"

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

func shouldInclude(name string, include, exclude []string) bool {
	for _, ex := range exclude {
		if ok, _ := filepath.Match(ex, name); ok {
			return false
		}
	}
	if len(include) == 0 {
		return true
	}
	for _, in := range include {
		if ok, _ := filepath.Match(in, name); ok {
			return true
		}
	}
	return false
}

// ---- archive.zip

type ZipRequest struct {
	Src     string   `json:"src"`
	Dest    string   `json:"dest"`
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

type ZipResponse struct {
	ArchivePath string `json:"archive_path"`
	Files       int    `json:"files"`
	DurationMs  int64  `json:"duration_ms"`
	Error       string `json:"error,omitempty"`
}

func Zip(ctx context.Context, in ZipRequest) ZipResponse {
	start := time.Now()
	src, err := normalizePath(in.Src)
	if err != nil {
		return ZipResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	dest, err := normalizePath(in.Dest)
	if err != nil {
		return ZipResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return ZipResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	out, err := os.Create(dest)
	if err != nil {
		return ZipResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	var count int
	err = filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if !shouldInclude(rel, in.Include, in.Exclude) {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = rel
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, f); err != nil {
			return err
		}
		count++
		return nil
	})
	if err != nil {
		zw.Close()
		return ZipResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	if err := zw.Close(); err != nil {
		return ZipResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	resp := ZipResponse{ArchivePath: dest, Files: count}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Src        string `json:"src"`
		Dest       string `json:"dest"`
		Files      int    `json:"files"`
		DurationMs int64  `json:"duration_ms"`
	}{time.Now().UTC().Format(time.RFC3339), "archive.zip", src, dest, count, resp.DurationMs})
	return resp
}

// ---- archive.unzip

type UnzipRequest struct {
	Src     string   `json:"src"`
	Dest    string   `json:"dest"`
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

type UnzipResponse struct {
	Extracted  bool   `json:"extracted"`
	Files      int    `json:"files"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func Unzip(ctx context.Context, in UnzipRequest) UnzipResponse {
	start := time.Now()
	src, err := normalizePath(in.Src)
	if err != nil {
		return UnzipResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	dest, err := normalizePath(in.Dest)
	if err != nil {
		return UnzipResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	r, err := zip.OpenReader(src)
	if err != nil {
		return UnzipResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	defer r.Close()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return UnzipResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	var count int
	for _, f := range r.File {
		if !shouldInclude(f.Name, in.Include, in.Exclude) {
			continue
		}
		fp := filepath.Join(dest, f.Name)
		if !allowOutside() {
			rel, err := filepath.Rel(workspaceRoot(), fp)
			if err != nil || strings.HasPrefix(rel, "..") {
				return UnzipResponse{DurationMs: time.Since(start).Milliseconds(), Error: "path escapes workspace"}
			}
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(fp, 0o755); err != nil {
				return UnzipResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			return UnzipResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
		}
		rc, err := f.Open()
		if err != nil {
			return UnzipResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
		}
		out, err := os.OpenFile(fp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return UnzipResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return UnzipResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
		}
		out.Close()
		rc.Close()
		count++
	}
	resp := UnzipResponse{Extracted: true, Files: count}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Src        string `json:"src"`
		Dest       string `json:"dest"`
		Files      int    `json:"files"`
		DurationMs int64  `json:"duration_ms"`
	}{time.Now().UTC().Format(time.RFC3339), "archive.unzip", src, dest, count, resp.DurationMs})
	return resp
}

// ---- archive.tar

type TarRequest struct {
	Src     string   `json:"src"`
	Dest    string   `json:"dest"`
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

type TarResponse struct {
	ArchivePath string `json:"archive_path"`
	Files       int    `json:"files"`
	DurationMs  int64  `json:"duration_ms"`
	Error       string `json:"error,omitempty"`
}

func Tar(ctx context.Context, in TarRequest) TarResponse {
	start := time.Now()
	src, err := normalizePath(in.Src)
	if err != nil {
		return TarResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	dest, err := normalizePath(in.Dest)
	if err != nil {
		return TarResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return TarResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	out, err := os.Create(dest)
	if err != nil {
		return TarResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	defer out.Close()
	tw := tar.NewWriter(out)
	var count int
	err = filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if !shouldInclude(rel, in.Include, in.Exclude) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !d.IsDir() {
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			if _, err := io.Copy(tw, f); err != nil {
				f.Close()
				return err
			}
			f.Close()
			count++
		}
		return nil
	})
	if err != nil {
		tw.Close()
		return TarResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	if err := tw.Close(); err != nil {
		return TarResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	resp := TarResponse{ArchivePath: dest, Files: count}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Src        string `json:"src"`
		Dest       string `json:"dest"`
		Files      int    `json:"files"`
		DurationMs int64  `json:"duration_ms"`
	}{time.Now().UTC().Format(time.RFC3339), "archive.tar", src, dest, count, resp.DurationMs})
	return resp
}

// ---- archive.untar

type UntarRequest struct {
	Src     string   `json:"src"`
	Dest    string   `json:"dest"`
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

type UntarResponse struct {
	Extracted  bool   `json:"extracted"`
	Files      int    `json:"files"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func Untar(ctx context.Context, in UntarRequest) UntarResponse {
	start := time.Now()
	src, err := normalizePath(in.Src)
	if err != nil {
		return UntarResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	dest, err := normalizePath(in.Dest)
	if err != nil {
		return UntarResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	f, err := os.Open(src)
	if err != nil {
		return UntarResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	defer f.Close()
	tr := tar.NewReader(f)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return UntarResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	var count int
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return UntarResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
		}
		if !shouldInclude(hdr.Name, in.Include, in.Exclude) {
			continue
		}
		fp := filepath.Join(dest, hdr.Name)
		if !allowOutside() {
			rel, err := filepath.Rel(workspaceRoot(), fp)
			if err != nil || strings.HasPrefix(rel, "..") {
				return UntarResponse{DurationMs: time.Since(start).Milliseconds(), Error: "path escapes workspace"}
			}
		}
		if hdr.FileInfo().IsDir() {
			if err := os.MkdirAll(fp, hdr.FileInfo().Mode()); err != nil {
				return UntarResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			return UntarResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
		}
		out, err := os.OpenFile(fp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, hdr.FileInfo().Mode())
		if err != nil {
			return UntarResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return UntarResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
		}
		out.Close()
		count++
	}
	resp := UntarResponse{Extracted: true, Files: count}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Src        string `json:"src"`
		Dest       string `json:"dest"`
		Files      int    `json:"files"`
		DurationMs int64  `json:"duration_ms"`
	}{time.Now().UTC().Format(time.RFC3339), "archive.untar", src, dest, count, resp.DurationMs})
	return resp
}

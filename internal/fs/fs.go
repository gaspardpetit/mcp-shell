package fs

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	stdfs "io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const LogPath = "/logs/mcp-shell.log"

// workspaceRoot returns the root directory for filesystem operations.
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

// normalizePath cleans the path and ensures it stays within the workspace root
// unless FS_ALLOW_OUTSIDE_WORKSPACE is set.
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
		return "", fmt.Errorf("path %q escapes workspace", p)
	}
	return p, nil
}

// audit writes a JSONL record to LogPath; failures are ignored.
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

// ---- fs.list

type ListRequest struct {
	Path          string `json:"path"`
	Glob          string `json:"glob,omitempty"`
	IncludeHidden bool   `json:"include_hidden,omitempty"`
	MaxEntries    int    `json:"max_entries,omitempty"`
}

type ListEntry struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
	Mode  string `json:"mode"`
}

type ListResponse struct {
	Entries    []ListEntry `json:"entries"`
	DurationMs int64       `json:"duration_ms"`
	Error      string      `json:"error,omitempty"`
}

func List(ctx context.Context, in ListRequest) ListResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return ListResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return ListResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	resp := ListResponse{}
	for _, e := range entries {
		name := e.Name()
		if !in.IncludeHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if in.Glob != "" {
			match, _ := filepath.Match(in.Glob, name)
			if !match {
				continue
			}
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		typ := "file"
		if info.IsDir() {
			typ = "dir"
		} else if info.Mode()&os.ModeSymlink != 0 {
			typ = "symlink"
		}
		resp.Entries = append(resp.Entries, ListEntry{
			Name:  name,
			Type:  typ,
			Size:  info.Size(),
			Mtime: info.ModTime().Unix(),
			Mode:  fmt.Sprintf("%#o", info.Mode().Perm()),
		})
		if in.MaxEntries > 0 && len(resp.Entries) >= in.MaxEntries {
			break
		}
	}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Path       string `json:"path"`
		DurationMs int64  `json:"duration_ms"`
		Count      int    `json:"count"`
	}{time.Now().UTC().Format(time.RFC3339), "fs.list", path, resp.DurationMs, len(resp.Entries)})
	return resp
}

// ---- fs.stat

type StatRequest struct {
	Path string `json:"path"`
}

type StatResponse struct {
	Type       string `json:"type"`
	Size       int64  `json:"size"`
	Mode       string `json:"mode"`
	Mtime      int64  `json:"mtime"`
	UID        uint32 `json:"uid"`
	GID        uint32 `json:"gid"`
	Target     string `json:"symlink_target,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func Stat(ctx context.Context, in StatRequest) StatResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return StatResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return StatResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	typ := "file"
	if info.IsDir() {
		typ = "dir"
	} else if info.Mode()&os.ModeSymlink != 0 {
		typ = "symlink"
	}
	var uid, gid uint32
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		uid = stat.Uid
		gid = stat.Gid
	}
	resp := StatResponse{
		Type:  typ,
		Size:  info.Size(),
		Mode:  fmt.Sprintf("%#o", info.Mode().Perm()),
		Mtime: info.ModTime().Unix(),
		UID:   uid,
		GID:   gid,
	}
	if typ == "symlink" {
		if target, err := os.Readlink(path); err == nil {
			resp.Target = target
		}
	}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Path       string `json:"path"`
		DurationMs int64  `json:"duration_ms"`
	}{time.Now().UTC().Format(time.RFC3339), "fs.stat", path, resp.DurationMs})
	return resp
}

// ---- fs.read

type ReadRequest struct {
	Path        string `json:"path"`
	MaxBytes    int64  `json:"max_bytes,omitempty"`
	StartOffset int64  `json:"start_offset,omitempty"`
}

type ReadResponse struct {
	Content    string `json:"content"`
	Truncated  bool   `json:"truncated"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func Read(ctx context.Context, in ReadRequest) ReadResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return ReadResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	f, err := os.Open(path)
	if err != nil {
		return ReadResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ReadResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	if in.StartOffset > 0 {
		if _, err := f.Seek(in.StartOffset, io.SeekStart); err != nil {
			return ReadResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
		}
	}
	limit := in.MaxBytes
	if limit <= 0 {
		limit = info.Size() - in.StartOffset
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return ReadResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	truncated := in.StartOffset+int64(len(data)) < info.Size()
	if !utf8.Valid(data) {
		return ReadResponse{DurationMs: time.Since(start).Milliseconds(), Error: "file is not valid UTF-8"}
	}
	resp := ReadResponse{Content: string(data), Truncated: truncated}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Path       string `json:"path"`
		DurationMs int64  `json:"duration_ms"`
		BytesOut   int    `json:"bytes_out"`
	}{time.Now().UTC().Format(time.RFC3339), "fs.read", path, resp.DurationMs, len(data)})
	return resp
}

// ---- fs.read_b64

type ReadB64Response struct {
	ContentB64 string `json:"content_b64"`
	Truncated  bool   `json:"truncated"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func ReadB64(ctx context.Context, in ReadRequest) ReadB64Response {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return ReadB64Response{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	f, err := os.Open(path)
	if err != nil {
		return ReadB64Response{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ReadB64Response{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	if in.StartOffset > 0 {
		if _, err := f.Seek(in.StartOffset, io.SeekStart); err != nil {
			return ReadB64Response{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
		}
	}
	limit := in.MaxBytes
	if limit <= 0 {
		limit = info.Size() - in.StartOffset
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return ReadB64Response{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	truncated := in.StartOffset+int64(len(data)) < info.Size()
	resp := ReadB64Response{ContentB64: base64.StdEncoding.EncodeToString(data), Truncated: truncated}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Path       string `json:"path"`
		DurationMs int64  `json:"duration_ms"`
		BytesOut   int    `json:"bytes_out"`
	}{time.Now().UTC().Format(time.RFC3339), "fs.read_b64", path, resp.DurationMs, len(resp.ContentB64)})
	return resp
}

// ---- fs.write

type WriteRequest struct {
	Path          string `json:"path"`
	Content       string `json:"content,omitempty"`
	ContentB64    string `json:"content_b64,omitempty"`
	Mode          string `json:"mode,omitempty"`
	CreateParents bool   `json:"create_parents,omitempty"`
	Append        bool   `json:"append,omitempty"`
	DryRun        bool   `json:"dry_run,omitempty"`
}

type WriteResponse struct {
	BytesWritten int    `json:"bytes_written"`
	DurationMs   int64  `json:"duration_ms"`
	Error        string `json:"error,omitempty"`
}

func Write(ctx context.Context, in WriteRequest) WriteResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return WriteResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	var data []byte
	switch {
	case in.ContentB64 != "":
		b, err := base64.StdEncoding.DecodeString(in.ContentB64)
		if err != nil {
			return WriteResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
		}
		data = b
	default:
		data = []byte(in.Content)
	}
	perm := os.FileMode(0o644)
	if in.Mode != "" {
		if v, err := strconv.ParseUint(in.Mode, 8, 32); err == nil {
			perm = os.FileMode(v)
		}
	}
	if in.CreateParents {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return WriteResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
		}
	}
	if in.DryRun {
		resp := WriteResponse{BytesWritten: len(data)}
		resp.DurationMs = time.Since(start).Milliseconds()
		audit(struct {
			TS           string `json:"ts"`
			Tool         string `json:"tool"`
			Path         string `json:"path"`
			DurationMs   int64  `json:"duration_ms"`
			BytesWritten int    `json:"bytes_written"`
			DryRun       bool   `json:"dry_run"`
		}{time.Now().UTC().Format(time.RFC3339), "fs.write", path, resp.DurationMs, resp.BytesWritten, true})
		return resp
	}
	flags := os.O_CREATE | os.O_WRONLY
	if in.Append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, perm)
	if err != nil {
		return WriteResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	defer f.Close()
	n, err := f.Write(data)
	if err != nil {
		return WriteResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	resp := WriteResponse{BytesWritten: n}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS           string `json:"ts"`
		Tool         string `json:"tool"`
		Path         string `json:"path"`
		DurationMs   int64  `json:"duration_ms"`
		BytesWritten int    `json:"bytes_written"`
	}{time.Now().UTC().Format(time.RFC3339), "fs.write", path, resp.DurationMs, n})
	return resp
}

// ---- fs.remove

type RemoveRequest struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive,omitempty"`
}

type RemoveResponse struct {
	Removed    bool   `json:"removed"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func Remove(ctx context.Context, in RemoveRequest) RemoveResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return RemoveResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	var rerr error
	if in.Recursive {
		rerr = os.RemoveAll(path)
	} else {
		rerr = os.Remove(path)
	}
	resp := RemoveResponse{Removed: rerr == nil}
	if rerr != nil {
		resp.Error = rerr.Error()
	}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Path       string `json:"path"`
		DurationMs int64  `json:"duration_ms"`
		Removed    bool   `json:"removed"`
	}{time.Now().UTC().Format(time.RFC3339), "fs.remove", path, resp.DurationMs, resp.Removed})
	return resp
}

// ---- fs.mkdir

type MkdirRequest struct {
	Path    string `json:"path"`
	Parents bool   `json:"parents,omitempty"`
	Mode    string `json:"mode,omitempty"`
}

type MkdirResponse struct {
	Created    bool   `json:"created"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func Mkdir(ctx context.Context, in MkdirRequest) MkdirResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return MkdirResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	perm := os.FileMode(0o755)
	if in.Mode != "" {
		if v, err := strconv.ParseUint(in.Mode, 8, 32); err == nil {
			perm = os.FileMode(v)
		}
	}
	var merr error
	if in.Parents {
		merr = os.MkdirAll(path, perm)
	} else {
		merr = os.Mkdir(path, perm)
	}
	resp := MkdirResponse{Created: merr == nil}
	if merr != nil {
		resp.Error = merr.Error()
	}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Path       string `json:"path"`
		DurationMs int64  `json:"duration_ms"`
		Created    bool   `json:"created"`
	}{time.Now().UTC().Format(time.RFC3339), "fs.mkdir", path, resp.DurationMs, resp.Created})
	return resp
}

// ---- fs.move

type MoveRequest struct {
	Src       string `json:"src"`
	Dest      string `json:"dest"`
	Overwrite bool   `json:"overwrite,omitempty"`
	Parents   bool   `json:"parents,omitempty"`
}

type MoveResponse struct {
	Moved      bool   `json:"moved"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func Move(ctx context.Context, in MoveRequest) MoveResponse {
	start := time.Now()
	src, err := normalizePath(in.Src)
	if err != nil {
		return MoveResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	dest, err := normalizePath(in.Dest)
	if err != nil {
		return MoveResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	if !in.Overwrite {
		if _, err := os.Stat(dest); err == nil {
			return MoveResponse{DurationMs: time.Since(start).Milliseconds(), Error: "destination exists"}
		}
	}
	if in.Parents {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return MoveResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
		}
	}
	err = os.Rename(src, dest)
	resp := MoveResponse{Moved: err == nil}
	if err != nil {
		resp.Error = err.Error()
	}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Src        string `json:"src"`
		Dest       string `json:"dest"`
		DurationMs int64  `json:"duration_ms"`
		Moved      bool   `json:"moved"`
	}{time.Now().UTC().Format(time.RFC3339), "fs.move", src, dest, resp.DurationMs, resp.Moved})
	return resp
}

// ---- fs.copy

type CopyRequest struct {
	Src       string `json:"src"`
	Dest      string `json:"dest"`
	Overwrite bool   `json:"overwrite,omitempty"`
	Parents   bool   `json:"parents,omitempty"`
	Recursive bool   `json:"recursive,omitempty"`
}

type CopyResponse struct {
	Copied     bool   `json:"copied"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func Copy(ctx context.Context, in CopyRequest) CopyResponse {
	start := time.Now()
	src, err := normalizePath(in.Src)
	if err != nil {
		return CopyResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	dest, err := normalizePath(in.Dest)
	if err != nil {
		return CopyResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	if !in.Overwrite {
		if _, err := os.Stat(dest); err == nil {
			return CopyResponse{DurationMs: time.Since(start).Milliseconds(), Error: "destination exists"}
		}
	}
	if in.Parents {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return CopyResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
		}
	}
	info, err := os.Lstat(src)
	if err != nil {
		return CopyResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	if info.IsDir() {
		if !in.Recursive {
			return CopyResponse{DurationMs: time.Since(start).Milliseconds(), Error: "source is a directory"}
		}
		err = filepath.WalkDir(src, func(path string, d stdfs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(src, path)
			if err != nil {
				return err
			}
			target := filepath.Join(dest, rel)
			if d.IsDir() {
				return os.MkdirAll(target, 0o755)
			}
			if d.Type()&os.ModeSymlink != 0 {
				linkTarget, err := os.Readlink(path)
				if err != nil {
					return err
				}
				return os.Symlink(linkTarget, target)
			}
			return copyFile(path, target, info.Mode())
		})
	} else {
		err = copyFile(src, dest, info.Mode())
	}
	resp := CopyResponse{Copied: err == nil}
	if err != nil {
		resp.Error = err.Error()
	}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Src        string `json:"src"`
		Dest       string `json:"dest"`
		DurationMs int64  `json:"duration_ms"`
		Copied     bool   `json:"copied"`
	}{time.Now().UTC().Format(time.RFC3339), "fs.copy", src, dest, resp.DurationMs, resp.Copied})
	return resp
}

func copyFile(src, dest string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

// ---- fs.search

type SearchRequest struct {
	Path          string `json:"path"`
	Query         string `json:"query"`
	Regex         bool   `json:"regex,omitempty"`
	Glob          string `json:"glob,omitempty"`
	CaseSensitive *bool  `json:"case_sensitive,omitempty"`
	MaxResults    int    `json:"max_results,omitempty"`
}

type SearchMatch struct {
	File       string `json:"file"`
	Line       int    `json:"line"`
	ByteOffset int    `json:"byte_offset"`
	Preview    string `json:"preview"`
}

type SearchResponse struct {
	Matches    []SearchMatch `json:"matches"`
	DurationMs int64         `json:"duration_ms"`
	Error      string        `json:"error,omitempty"`
}

func Search(ctx context.Context, in SearchRequest) SearchResponse {
	start := time.Now()
	if in.Query == "" {
		return SearchResponse{DurationMs: time.Since(start).Milliseconds(), Error: "query is required"}
	}
	path, err := normalizePath(in.Path)
	if err != nil {
		return SearchResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	if _, err := exec.LookPath("rg"); err != nil {
		return SearchResponse{DurationMs: time.Since(start).Milliseconds(), Error: "ripgrep (rg) not found"}
	}
	args := []string{"--json"}
	if !in.Regex {
		args = append(args, "--fixed-strings")
	}
	if in.Glob != "" {
		args = append(args, "--glob", in.Glob)
	}
	if in.CaseSensitive != nil {
		if *in.CaseSensitive {
			args = append(args, "--case-sensitive")
		} else {
			args = append(args, "--ignore-case")
		}
	}
	args = append(args, in.Query, path)
	cmd := exec.CommandContext(ctx, "rg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return SearchResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return SearchResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	scanner := bufio.NewScanner(stdout)
	resp := SearchResponse{}
	for scanner.Scan() {
		line := scanner.Bytes()
		var evt struct {
			Type string `json:"type"`
			Data struct {
				Path struct {
					Text string `json:"text"`
				} `json:"path"`
				Lines struct {
					Text string `json:"text"`
				} `json:"lines"`
				LineNumber     int `json:"line_number"`
				AbsoluteOffset int `json:"absolute_offset"`
			} `json:"data"`
		}
		if err := json.Unmarshal(line, &evt); err != nil {
			continue
		}
		if evt.Type == "match" {
			resp.Matches = append(resp.Matches, SearchMatch{
				File:       evt.Data.Path.Text,
				Line:       evt.Data.LineNumber,
				ByteOffset: evt.Data.AbsoluteOffset,
				Preview:    evt.Data.Lines.Text,
			})
			if in.MaxResults > 0 && len(resp.Matches) >= in.MaxResults {
				_ = cmd.Process.Kill()
				break
			}
		}
	}
	_ = cmd.Wait()
	if err := scanner.Err(); err != nil {
		resp.Error = err.Error()
	}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Path       string `json:"path"`
		Query      string `json:"query"`
		DurationMs int64  `json:"duration_ms"`
		Count      int    `json:"count"`
	}{time.Now().UTC().Format(time.RFC3339), "fs.search", path, in.Query, resp.DurationMs, len(resp.Matches)})
	if resp.Error == "" {
		if exitErr, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
			if exitErr.ExitStatus() > 1 {
				resp.Error = stderr.String()
			}
		}
	}
	return resp
}

// ---- fs.hash

type HashRequest struct {
	Path string `json:"path"`
	Algo string `json:"algo"`
}

type HashResponse struct {
	Hash       string `json:"hash"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func Hash(ctx context.Context, in HashRequest) HashResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return HashResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	f, err := os.Open(path)
	if err != nil {
		return HashResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	defer f.Close()
	var h hash.Hash
	switch strings.ToLower(in.Algo) {
	case "", "sha256":
		h = sha256.New()
	case "sha1":
		h = sha1.New()
	case "md5":
		h = md5.New()
	default:
		return HashResponse{DurationMs: time.Since(start).Milliseconds(), Error: "unsupported algo"}
	}
	if _, err := io.Copy(h, f); err != nil {
		return HashResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	hashStr := hex.EncodeToString(h.Sum(nil))
	resp := HashResponse{Hash: hashStr}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Path       string `json:"path"`
		Algo       string `json:"algo"`
		DurationMs int64  `json:"duration_ms"`
	}{time.Now().UTC().Format(time.RFC3339), "fs.hash", path, in.Algo, resp.DurationMs})
	return resp
}

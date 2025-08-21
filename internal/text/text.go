package text

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
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

// ---- text.diff

type DiffRequest struct {
	A    string `json:"a"`
	B    string `json:"b"`
	Algo string `json:"algo,omitempty"`
}

type DiffResponse struct {
	UnifiedDiff string `json:"unified_diff"`
	DurationMs  int64  `json:"duration_ms"`
	Error       string `json:"error,omitempty"`
}

func Diff(ctx context.Context, in DiffRequest) DiffResponse {
	start := time.Now()
	dir, err := os.MkdirTemp("", "diff")
	if err != nil {
		return DiffResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	defer os.RemoveAll(dir)
	aPath := filepath.Join(dir, "a.txt")
	bPath := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(aPath, []byte(in.A), 0o644); err != nil {
		return DiffResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	if err := os.WriteFile(bPath, []byte(in.B), 0o644); err != nil {
		return DiffResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	args := []string{"diff", "--no-index", "--unified=3"}
	switch strings.ToLower(in.Algo) {
	case "patience":
		args = append(args, "--patience")
	case "myers", "":
		// default algorithm is myers; no flag needed
	default:
		// unrecognized algorithm: default to myers
	}
	args = append(args, aPath, bPath)
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			if ee.ExitCode() > 1 {
				return DiffResponse{DurationMs: time.Since(start).Milliseconds(), Error: stderr.String()}
			}
			// exit code 1 means diff exists; treat as success
		} else {
			return DiffResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
		}
	}
	resp := DiffResponse{UnifiedDiff: stdout.String()}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS         string `json:"ts"`
		Tool       string `json:"tool"`
		Algo       string `json:"algo"`
		DurationMs int64  `json:"duration_ms"`
		BytesOut   int    `json:"bytes_out"`
	}{time.Now().UTC().Format(time.RFC3339), "text.diff", in.Algo, resp.DurationMs, len(resp.UnifiedDiff)})
	return resp
}

// ---- text.apply_patch

type ApplyPatchRequest struct {
	Path        string `json:"path"`
	UnifiedDiff string `json:"unified_diff"`
	DryRun      bool   `json:"dry_run,omitempty"`
}

type ApplyPatchResponse struct {
	Patched      bool   `json:"patched"`
	HunksApplied int    `json:"hunks_applied"`
	HunksFailed  int    `json:"hunks_failed"`
	DurationMs   int64  `json:"duration_ms"`
	Error        string `json:"error,omitempty"`
}

func ApplyPatch(ctx context.Context, in ApplyPatchRequest) ApplyPatchResponse {
	start := time.Now()
	path, err := normalizePath(in.Path)
	if err != nil {
		return ApplyPatchResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	tmp, err := os.CreateTemp("", "patch")
	if err != nil {
		return ApplyPatchResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	if _, err := tmp.WriteString(in.UnifiedDiff); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return ApplyPatchResponse{DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	args := []string{"--batch", "--verbose"}
	if in.DryRun {
		args = append(args, "--dry-run")
	}
	args = append(args, path, tmp.Name())
	cmd := exec.CommandContext(ctx, "patch", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	output := out.String()
	applied := strings.Count(output, "succeeded at")
	failed := strings.Count(output, "FAILED")
	resp := ApplyPatchResponse{
		Patched:      err == nil && failed == 0,
		HunksApplied: applied,
		HunksFailed:  failed,
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 && failed > 0 {
			// hunk failures reported separately
		} else {
			resp.Error = output
		}
	}
	resp.DurationMs = time.Since(start).Milliseconds()
	audit(struct {
		TS           string `json:"ts"`
		Tool         string `json:"tool"`
		Path         string `json:"path"`
		DurationMs   int64  `json:"duration_ms"`
		PatchBytes   int    `json:"patch_bytes"`
		HunksApplied int    `json:"hunks_applied"`
		HunksFailed  int    `json:"hunks_failed"`
		DryRun       bool   `json:"dry_run,omitempty"`
	}{time.Now().UTC().Format(time.RFC3339), "text.apply_patch", path, resp.DurationMs, len(in.UnifiedDiff), resp.HunksApplied, resp.HunksFailed, in.DryRun})
	return resp
}

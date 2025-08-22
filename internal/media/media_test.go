package media

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageConvert(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WORKSPACE", dir)
	src := filepath.Join(dir, "src.png")
	dest := filepath.Join(dir, "dest.jpg")
	cmd := exec.Command("convert", "-size", "10x10", "xc:red", src)
	if err := cmd.Run(); err != nil {
		t.Fatalf("create src: %v", err)
	}
	resp := ImageConvert(context.Background(), ImageConvertRequest{SrcPath: src, DestPath: dest, Ops: []ImageOp{{Resize: "5x5"}}})
	if resp.Error != "" {
		t.Fatalf("ImageConvert error: %v", resp.Error)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("dest not created: %v", err)
	}
}

func TestOCRExtract(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WORKSPACE", dir)
	img := filepath.Join(dir, "text.png")
	cmd := exec.Command("convert", "-background", "white", "-fill", "black", "-size", "200x60", "-pointsize", "32", "label:hello", img)
	if err := cmd.Run(); err != nil {
		t.Fatalf("create image: %v", err)
	}
	resp := OCRExtract(context.Background(), OCRRequest{Path: img, Lang: "eng"})
	if resp.Error != "" {
		t.Fatalf("OCRExtract error: %v", resp.Error)
	}
	if !strings.Contains(strings.ToLower(resp.Text), "hello") {
		t.Fatalf("expected 'hello' in output, got %q", resp.Text)
	}
}

func TestVideoTranscode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WORKSPACE", dir)
	src := filepath.Join(dir, "in.mp4")
	dest := filepath.Join(dir, "out.mkv")
	cmd := exec.Command("ffmpeg", "-f", "lavfi", "-i", "color=c=blue:s=16x16:d=1", src)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		t.Fatalf("create video: %v", err)
	}
	resp := VideoTranscode(context.Background(), VideoTranscodeRequest{Src: src, Dest: dest, Codec: "libx264", Crf: 23})
	if resp.Error != "" {
		t.Fatalf("VideoTranscode error: %v", resp.Error)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("dest not created: %v", err)
	}
}

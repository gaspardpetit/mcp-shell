package doc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertAndMetadataAndExtract(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	docx := filepath.Join(dir, "sample.docx")
	cmd := exec.Command("pandoc", "-o", docx)
	cmd.Stdin = strings.NewReader("Hello world\n")
	if err := cmd.Run(); err != nil {
		t.Skip("pandoc not available", err)
	}
	pdfResp := Convert(ctx, ConvertRequest{SrcPath: docx, DestFormat: "pdf"})
	if pdfResp.Error != "" {
		t.Fatalf("convert to pdf: %v", pdfResp.Error)
	}
	if _, err := os.Stat(pdfResp.DestPath); err != nil {
		t.Fatalf("pdf not created: %v", err)
	}
	backResp := Convert(ctx, ConvertRequest{SrcPath: pdfResp.DestPath, DestFormat: "docx"})
	if backResp.Error != "" {
		t.Fatalf("convert back to docx: %v", backResp.Error)
	}
	if _, err := os.Stat(backResp.DestPath); err != nil {
		t.Fatalf("docx not created: %v", err)
	}
	ext := ExtractText(ctx, PDFExtractRequest{Path: pdfResp.DestPath})
	if ext.Error != "" {
		t.Fatalf("extract text: %v", ext.Error)
	}
	if !strings.Contains(ext.Text, "Hello world") {
		t.Fatalf("unexpected text: %q", ext.Text)
	}
	meta := Metadata(ctx, MetadataRequest{Path: pdfResp.DestPath})
	if meta.Error != "" {
		t.Fatalf("metadata: %v", meta.Error)
	}
	if meta.Mime != "application/pdf" {
		t.Fatalf("unexpected mime: %s", meta.Mime)
	}
	if meta.Pages < 1 {
		t.Fatalf("expected pages >=1, got %d", meta.Pages)
	}
}

func TestSpreadsheetToCSV(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	xlsx := filepath.Join(dir, "wb.xlsx")
	script := fmt.Sprintf(`import openpyxl\nwb=openpyxl.Workbook()\nws1=wb.active\nws1.title='Sheet1'\nws1.append(['A','B'])\nws2=wb.create_sheet('Data')\nws2.append(['C','D'])\nwb.save('%s')\n`, xlsx)
	if err := exec.Command("python3", "-c", script).Run(); err != nil {
		t.Skip("python or openpyxl missing", err)
	}
	// by name
	respName := SpreadsheetToCSV(ctx, ToCSVRequest{Path: xlsx, Sheet: json.RawMessage(`"Data"`)})
	if respName.Error != "" {
		t.Fatalf("to_csv name: %v", respName.Error)
	}
	if !strings.Contains(respName.Csv, "C,D") {
		t.Fatalf("expected Data sheet, got %q", respName.Csv)
	}
	// by index (1-based)
	respIdx := SpreadsheetToCSV(ctx, ToCSVRequest{Path: xlsx, Sheet: json.RawMessage("1")})
	if respIdx.Error != "" {
		t.Fatalf("to_csv index: %v", respIdx.Error)
	}
	if !strings.Contains(respIdx.Csv, "A,B") {
		t.Fatalf("expected first sheet, got %q", respIdx.Csv)
	}
}

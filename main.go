package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/gaspardpetit/mcp-shell/internal/archive"
	"github.com/gaspardpetit/mcp-shell/internal/fs"
	"github.com/gaspardpetit/mcp-shell/internal/shell"
	"github.com/gaspardpetit/mcp-shell/internal/text"
)

var (
	buildName    = "mcp-shell"
	buildVersion = "0.1.0"
	startedAt    = time.Now()
)

func main() {
	// ---- flags
	transport := flag.String("transport", "http", "Transport: stdio | sse | http")
	addr := flag.String("addr", ":3333", "Listen address for HTTP/SSE transports")
	basePath := flag.String("base-path", "/mcp", "Base path for HTTP/SSE endpoints")
	baseURL := flag.String("base-url", "", "Public base URL (SSE only, optional)")
	flag.Parse()

	// ---- server
	s := server.NewMCPServer(
		buildName,
		buildVersion,
		server.WithToolCapabilities(true),
		server.WithLogging(),
		server.WithRecovery(),
	)

	// tool definition
	tool := mcp.NewTool(
		"shell.exec",
		mcp.WithDescription("Run a shell command inside the container"),
		mcp.WithInputSchema[shell.ExecRequest](),
	)
	handler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args shell.ExecRequest) (*mcp.CallToolResult, error) {
		resp := shell.Run(ctx, args)
		return mcp.NewToolResultStructured(resp, "shell.exec result"), nil
	})
	s.AddTool(tool, handler)

	// filesystem tools
	// fs.list
	fsListTool := mcp.NewTool(
		"fs.list",
		mcp.WithDescription("List directory entries"),
		mcp.WithInputSchema[fs.ListRequest](),
	)
	fsListHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args fs.ListRequest) (*mcp.CallToolResult, error) {
		resp := fs.List(ctx, args)
		return mcp.NewToolResultStructured(resp, "fs.list result"), nil
	})
	s.AddTool(fsListTool, fsListHandler)

	// fs.stat
	fsStatTool := mcp.NewTool(
		"fs.stat",
		mcp.WithDescription("Get file or directory metadata"),
		mcp.WithInputSchema[fs.StatRequest](),
	)
	fsStatHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args fs.StatRequest) (*mcp.CallToolResult, error) {
		resp := fs.Stat(ctx, args)
		return mcp.NewToolResultStructured(resp, "fs.stat result"), nil
	})
	s.AddTool(fsStatTool, fsStatHandler)

	// fs.read
	fsReadTool := mcp.NewTool(
		"fs.read",
		mcp.WithDescription("Read a UTF-8 text file"),
		mcp.WithInputSchema[fs.ReadRequest](),
	)
	fsReadHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args fs.ReadRequest) (*mcp.CallToolResult, error) {
		resp := fs.Read(ctx, args)
		return mcp.NewToolResultStructured(resp, "fs.read result"), nil
	})
	s.AddTool(fsReadTool, fsReadHandler)

	// fs.read_b64
	fsReadB64Tool := mcp.NewTool(
		"fs.read_b64",
		mcp.WithDescription("Read a file and return base64 content"),
		mcp.WithInputSchema[fs.ReadRequest](),
	)
	fsReadB64Handler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args fs.ReadRequest) (*mcp.CallToolResult, error) {
		resp := fs.ReadB64(ctx, args)
		return mcp.NewToolResultStructured(resp, "fs.read_b64 result"), nil
	})
	s.AddTool(fsReadB64Tool, fsReadB64Handler)

	// fs.write
	fsWriteTool := mcp.NewTool(
		"fs.write",
		mcp.WithDescription("Write a file"),
		mcp.WithInputSchema[fs.WriteRequest](),
	)
	fsWriteHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args fs.WriteRequest) (*mcp.CallToolResult, error) {
		resp := fs.Write(ctx, args)
		return mcp.NewToolResultStructured(resp, "fs.write result"), nil
	})
	s.AddTool(fsWriteTool, fsWriteHandler)

	// fs.remove
	fsRemoveTool := mcp.NewTool(
		"fs.remove",
		mcp.WithDescription("Remove a file or directory"),
		mcp.WithInputSchema[fs.RemoveRequest](),
	)
	fsRemoveHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args fs.RemoveRequest) (*mcp.CallToolResult, error) {
		resp := fs.Remove(ctx, args)
		return mcp.NewToolResultStructured(resp, "fs.remove result"), nil
	})
	s.AddTool(fsRemoveTool, fsRemoveHandler)

	// fs.mkdir
	fsMkdirTool := mcp.NewTool(
		"fs.mkdir",
		mcp.WithDescription("Create a directory"),
		mcp.WithInputSchema[fs.MkdirRequest](),
	)
	fsMkdirHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args fs.MkdirRequest) (*mcp.CallToolResult, error) {
		resp := fs.Mkdir(ctx, args)
		return mcp.NewToolResultStructured(resp, "fs.mkdir result"), nil
	})
	s.AddTool(fsMkdirTool, fsMkdirHandler)

	// fs.move
	fsMoveTool := mcp.NewTool(
		"fs.move",
		mcp.WithDescription("Move or rename a file"),
		mcp.WithInputSchema[fs.MoveRequest](),
	)
	fsMoveHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args fs.MoveRequest) (*mcp.CallToolResult, error) {
		resp := fs.Move(ctx, args)
		return mcp.NewToolResultStructured(resp, "fs.move result"), nil
	})
	s.AddTool(fsMoveTool, fsMoveHandler)

	// fs.copy
	fsCopyTool := mcp.NewTool(
		"fs.copy",
		mcp.WithDescription("Copy a file or directory"),
		mcp.WithInputSchema[fs.CopyRequest](),
	)
	fsCopyHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args fs.CopyRequest) (*mcp.CallToolResult, error) {
		resp := fs.Copy(ctx, args)
		return mcp.NewToolResultStructured(resp, "fs.copy result"), nil
	})
	s.AddTool(fsCopyTool, fsCopyHandler)

	// fs.search
	fsSearchTool := mcp.NewTool(
		"fs.search",
		mcp.WithDescription("Search for text in files"),
		mcp.WithInputSchema[fs.SearchRequest](),
	)
	fsSearchHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args fs.SearchRequest) (*mcp.CallToolResult, error) {
		resp := fs.Search(ctx, args)
		return mcp.NewToolResultStructured(resp, "fs.search result"), nil
	})
	s.AddTool(fsSearchTool, fsSearchHandler)

	// fs.hash
	fsHashTool := mcp.NewTool(
		"fs.hash",
		mcp.WithDescription("Compute file checksum"),
		mcp.WithInputSchema[fs.HashRequest](),
	)
	fsHashHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args fs.HashRequest) (*mcp.CallToolResult, error) {
		resp := fs.Hash(ctx, args)
		return mcp.NewToolResultStructured(resp, "fs.hash result"), nil
	})
	s.AddTool(fsHashTool, fsHashHandler)

	// archive.zip
	archiveZipTool := mcp.NewTool(
		"archive.zip",
		mcp.WithDescription("Create a zip archive"),
		mcp.WithInputSchema[archive.ZipRequest](),
	)
	archiveZipHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args archive.ZipRequest) (*mcp.CallToolResult, error) {
		resp := archive.Zip(ctx, args)
		return mcp.NewToolResultStructured(resp, "archive.zip result"), nil
	})
	s.AddTool(archiveZipTool, archiveZipHandler)

	// archive.unzip
	archiveUnzipTool := mcp.NewTool(
		"archive.unzip",
		mcp.WithDescription("Extract a zip archive"),
		mcp.WithInputSchema[archive.UnzipRequest](),
	)
	archiveUnzipHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args archive.UnzipRequest) (*mcp.CallToolResult, error) {
		resp := archive.Unzip(ctx, args)
		return mcp.NewToolResultStructured(resp, "archive.unzip result"), nil
	})
	s.AddTool(archiveUnzipTool, archiveUnzipHandler)

	// archive.tar
	archiveTarTool := mcp.NewTool(
		"archive.tar",
		mcp.WithDescription("Create a tar archive"),
		mcp.WithInputSchema[archive.TarRequest](),
	)
	archiveTarHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args archive.TarRequest) (*mcp.CallToolResult, error) {
		resp := archive.Tar(ctx, args)
		return mcp.NewToolResultStructured(resp, "archive.tar result"), nil
	})
	s.AddTool(archiveTarTool, archiveTarHandler)

	// archive.untar
	archiveUntarTool := mcp.NewTool(
		"archive.untar",
		mcp.WithDescription("Extract a tar archive"),
		mcp.WithInputSchema[archive.UntarRequest](),
	)
	archiveUntarHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args archive.UntarRequest) (*mcp.CallToolResult, error) {
		resp := archive.Untar(ctx, args)
		return mcp.NewToolResultStructured(resp, "archive.untar result"), nil
	})
	s.AddTool(archiveUntarTool, archiveUntarHandler)

	// text.diff
	textDiffTool := mcp.NewTool(
		"text.diff",
		mcp.WithDescription("Compute a unified diff between two strings"),
		mcp.WithInputSchema[text.DiffRequest](),
	)
	textDiffHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args text.DiffRequest) (*mcp.CallToolResult, error) {
		resp := text.Diff(ctx, args)
		return mcp.NewToolResultStructured(resp, "text.diff result"), nil
	})
	s.AddTool(textDiffTool, textDiffHandler)

	// text.apply_patch
	textPatchTool := mcp.NewTool(
		"text.apply_patch",
		mcp.WithDescription("Apply a unified diff patch to a file"),
		mcp.WithInputSchema[text.ApplyPatchRequest](),
	)
	textPatchHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args text.ApplyPatchRequest) (*mcp.CallToolResult, error) {
		resp := text.ApplyPatch(ctx, args)
		return mcp.NewToolResultStructured(resp, "text.apply_patch result"), nil
	})
	s.AddTool(textPatchTool, textPatchHandler)

	// ---- context & signals
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch *transport {
	case "stdio":
		// Simple: block on stdio
		if err := server.ServeStdio(s); err != nil && ctx.Err() == nil {
			log.Fatalf("stdio server error: %v", err)
		}
		return

	case "sse":
		// SSE: mount handlers + /healthz on a mux
		sse := server.NewSSEServer(s,
			server.WithStaticBasePath(*basePath),
			server.WithKeepAliveInterval(30*time.Second),
			server.WithBaseURL(*baseURL),
		)

		mux := http.NewServeMux()
		// SSE endpoints: e.g. /mcp/sse and /mcp/message
		mux.Handle(sse.CompleteSsePath(), sse.SSEHandler())
		mux.Handle(sse.CompleteMessagePath(), sse.MessageHandler())

		// Health endpoints
		addHealthRoutes(mux, *basePath, "sse")

		srv := &http.Server{
			Addr:    *addr,
			Handler: mux,
		}
		go func() {
			log.Printf("SSE server listening on %s (basePath=%s)", *addr, *basePath)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("sse listen error: %v", err)
			}
		}()
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
		return

	case "http":
		// StreamableHTTP: use it as an http.Handler and add /healthz.
		// Note: WithEndpointPath only affects Start(); as a Handler we just mount it under basePath. :contentReference[oaicite:3]{index=3}
		httpSrv := server.NewStreamableHTTPServer(s)

		mux := http.NewServeMux()
		// Mount all MCP endpoints under /mcp (the handler will route internally)
		mux.Handle(*basePath+"/", httpSrv)

		// Built-in health lives at /mcp/health; we also expose /healthz
		addHealthRoutes(mux, *basePath, "http")

		srv := &http.Server{
			Addr:    *addr,
			Handler: mux,
		}
		go func() {
			log.Printf("HTTP server listening on %s (basePath=%s)", *addr, *basePath)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("http listen error: %v", err)
			}
		}()
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
		return

	default:
		log.Fatalf("unknown --transport=%q (use stdio|sse|http)", *transport)
	}
}

func addHealthRoutes(mux *http.ServeMux, basePath, transport string) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeHealth(w, transport)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		writeHealth(w, transport)
	})
	// Note: StreamableHTTP already exposes GET basePath + "/health". :contentReference[oaicite:4]{index=4}
}

func writeHealth(w http.ResponseWriter, transport string) {
	uptime := time.Since(startedAt).Round(time.Millisecond)
	resp := map[string]any{
		"status":    "ok",
		"name":      buildName,
		"version":   buildVersion,
		"transport": transport,
		"uptime":    uptime.String(),
		"startedAt": startedAt.UTC().Format(time.RFC3339),
		"pid":       os.Getpid(),
		"time":      time.Now().UTC().Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

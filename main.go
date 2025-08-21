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

	"github.com/gaspardpetit/mcp-shell/internal/shell"
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

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
	"github.com/gaspardpetit/mcp-shell/internal/doc"
	"github.com/gaspardpetit/mcp-shell/internal/fs"
	"github.com/gaspardpetit/mcp-shell/internal/git"
	"github.com/gaspardpetit/mcp-shell/internal/pkgmgr"
	rt "github.com/gaspardpetit/mcp-shell/internal/runtime"
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
	allowPkg := flag.Bool("allow-pkg", false, "Allow package installation tools even when EGRESS=0")
	flag.Parse()

	pkgmgr.AdminOverride = *allowPkg

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

	// python.run
	pyTool := mcp.NewTool(
		"python.run",
		mcp.WithDescription("Execute Python code, optionally in a virtual environment"),
		mcp.WithInputSchema[rt.PythonRunRequest](),
	)
	pyHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args rt.PythonRunRequest) (*mcp.CallToolResult, error) {
		resp := rt.PythonRun(ctx, args)
		return mcp.NewToolResultStructured(resp, "python.run result"), nil
	})
	s.AddTool(pyTool, pyHandler)

	// node.run
	nodeTool := mcp.NewTool(
		"node.run",
		mcp.WithDescription("Execute Node.js code"),
		mcp.WithInputSchema[rt.NodeRunRequest](),
	)
	nodeHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args rt.NodeRunRequest) (*mcp.CallToolResult, error) {
		resp := rt.NodeRun(ctx, args)
		return mcp.NewToolResultStructured(resp, "node.run result"), nil
	})
	s.AddTool(nodeTool, nodeHandler)

	// sh.script.write_and_run
	shTool := mcp.NewTool(
		"sh.script.write_and_run",
		mcp.WithDescription("Write a script to a temp file and run it"),
		mcp.WithInputSchema[rt.ShRequest](),
	)
	shHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args rt.ShRequest) (*mcp.CallToolResult, error) {
		resp := rt.ShScriptWriteAndRun(ctx, args)
		return mcp.NewToolResultStructured(resp, "sh.script.write_and_run result"), nil
	})
	s.AddTool(shTool, shHandler)

	// package management tools
	aptTool := mcp.NewTool(
		"apt.install",
		mcp.WithDescription("Install system packages via apt"),
		mcp.WithInputSchema[pkgmgr.AptInstallRequest](),
	)
	aptHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args pkgmgr.AptInstallRequest) (*mcp.CallToolResult, error) {
		resp := pkgmgr.AptInstall(ctx, args)
		return mcp.NewToolResultStructured(resp, "apt.install result"), nil
	})
	s.AddTool(aptTool, aptHandler)

	pipTool := mcp.NewTool(
		"pip.install",
		mcp.WithDescription("Install Python packages via pip"),
		mcp.WithInputSchema[pkgmgr.PipInstallRequest](),
	)
	pipHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args pkgmgr.PipInstallRequest) (*mcp.CallToolResult, error) {
		resp := pkgmgr.PipInstall(ctx, args)
		return mcp.NewToolResultStructured(resp, "pip.install result"), nil
	})
	s.AddTool(pipTool, pipHandler)

	npmTool := mcp.NewTool(
		"npm.install",
		mcp.WithDescription("Install Node packages via npm"),
		mcp.WithInputSchema[pkgmgr.NpmInstallRequest](),
	)
	npmHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args pkgmgr.NpmInstallRequest) (*mcp.CallToolResult, error) {
		resp := pkgmgr.NpmInstall(ctx, args)
		return mcp.NewToolResultStructured(resp, "npm.install result"), nil
	})
	s.AddTool(npmTool, npmHandler)

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

	// doc.convert
	docConvertTool := mcp.NewTool(
		"doc.convert",
		mcp.WithDescription("Convert documents between formats"),
		mcp.WithInputSchema[doc.ConvertRequest](),
	)
	docConvertHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args doc.ConvertRequest) (*mcp.CallToolResult, error) {
		resp := doc.Convert(ctx, args)
		return mcp.NewToolResultStructured(resp, "doc.convert result"), nil
	})
	s.AddTool(docConvertTool, docConvertHandler)

	// pdf.extract_text
	pdfExtractTool := mcp.NewTool(
		"pdf.extract_text",
		mcp.WithDescription("Extract text from a PDF file"),
		mcp.WithInputSchema[doc.PDFExtractRequest](),
	)
	pdfExtractHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args doc.PDFExtractRequest) (*mcp.CallToolResult, error) {
		resp := doc.ExtractText(ctx, args)
		return mcp.NewToolResultStructured(resp, "pdf.extract_text result"), nil
	})
	s.AddTool(pdfExtractTool, pdfExtractHandler)

	// spreadsheet.to_csv
	sheetCSVTool := mcp.NewTool(
		"spreadsheet.to_csv",
		mcp.WithDescription("Convert a spreadsheet sheet to CSV"),
		mcp.WithInputSchema[doc.ToCSVRequest](),
	)
	sheetCSVHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args doc.ToCSVRequest) (*mcp.CallToolResult, error) {
		resp := doc.SpreadsheetToCSV(ctx, args)
		return mcp.NewToolResultStructured(resp, "spreadsheet.to_csv result"), nil
	})
	s.AddTool(sheetCSVTool, sheetCSVHandler)

	// doc.metadata
	docMetaTool := mcp.NewTool(
		"doc.metadata",
		mcp.WithDescription("Retrieve document metadata"),
		mcp.WithInputSchema[doc.MetadataRequest](),
	)
	docMetaHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args doc.MetadataRequest) (*mcp.CallToolResult, error) {
		resp := doc.Metadata(ctx, args)
		return mcp.NewToolResultStructured(resp, "doc.metadata result"), nil
	})
	s.AddTool(docMetaTool, docMetaHandler)

	// git.clone
	cloneTool := mcp.NewTool(
		"git.clone",
		mcp.WithDescription("Clone a git repository"),
		mcp.WithInputSchema[git.CloneRequest](),
	)
	cloneHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args git.CloneRequest) (*mcp.CallToolResult, error) {
		resp := git.Clone(ctx, args)
		return mcp.NewToolResultStructured(resp, "git.clone result"), nil
	})
	s.AddTool(cloneTool, cloneHandler)

	statusTool := mcp.NewTool(
		"git.status",
		mcp.WithDescription("Get git status"),
		mcp.WithInputSchema[git.StatusRequest](),
	)
	statusHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args git.StatusRequest) (*mcp.CallToolResult, error) {
		resp := git.Status(ctx, args)
		return mcp.NewToolResultStructured(resp, "git.status result"), nil
	})
	s.AddTool(statusTool, statusHandler)

	commitTool := mcp.NewTool(
		"git.commit",
		mcp.WithDescription("Commit changes in a git repository"),
		mcp.WithInputSchema[git.CommitRequest](),
	)
	commitHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args git.CommitRequest) (*mcp.CallToolResult, error) {
		resp := git.Commit(ctx, args)
		return mcp.NewToolResultStructured(resp, "git.commit result"), nil
	})
	s.AddTool(commitTool, commitHandler)

	pullTool := mcp.NewTool(
		"git.pull",
		mcp.WithDescription("Pull latest changes"),
		mcp.WithInputSchema[git.PullRequest](),
	)
	pullHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args git.PullRequest) (*mcp.CallToolResult, error) {
		resp := git.Pull(ctx, args)
		return mcp.NewToolResultStructured(resp, "git.pull result"), nil
	})
	s.AddTool(pullTool, pullHandler)

	pushTool := mcp.NewTool(
		"git.push",
		mcp.WithDescription("Push commits to remote"),
		mcp.WithInputSchema[git.PushRequest](),
	)
	pushHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args git.PushRequest) (*mcp.CallToolResult, error) {
		resp := git.Push(ctx, args)
		return mcp.NewToolResultStructured(resp, "git.push result"), nil
	})
	s.AddTool(pushTool, pushHandler)

	checkoutTool := mcp.NewTool(
		"git.checkout",
		mcp.WithDescription("Checkout a git ref"),
		mcp.WithInputSchema[git.CheckoutRequest](),
	)
	checkoutHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args git.CheckoutRequest) (*mcp.CallToolResult, error) {
		resp := git.Checkout(ctx, args)
		return mcp.NewToolResultStructured(resp, "git.checkout result"), nil
	})
	s.AddTool(checkoutTool, checkoutHandler)

	branchTool := mcp.NewTool(
		"git.branch",
		mcp.WithDescription("Manage git branches"),
		mcp.WithInputSchema[git.BranchRequest](),
	)
	branchHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args git.BranchRequest) (*mcp.CallToolResult, error) {
		resp := git.Branch(ctx, args)
		return mcp.NewToolResultStructured(resp, "git.branch result"), nil
	})
	s.AddTool(branchTool, branchHandler)

	tagTool := mcp.NewTool(
		"git.tag",
		mcp.WithDescription("Manage git tags"),
		mcp.WithInputSchema[git.TagRequest](),
	)
	tagHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args git.TagRequest) (*mcp.CallToolResult, error) {
		resp := git.Tag(ctx, args)
		return mcp.NewToolResultStructured(resp, "git.tag result"), nil
	})
	s.AddTool(tagTool, tagHandler)

	lfsTool := mcp.NewTool(
		"git.lfs.install",
		mcp.WithDescription("Install Git LFS in a repository"),
		mcp.WithInputSchema[git.LFSInstallRequest](),
	)
	lfsHandler := mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args git.LFSInstallRequest) (*mcp.CallToolResult, error) {
		resp := git.LFSInstall(ctx, args)
		return mcp.NewToolResultStructured(resp, "git.lfs.install result"), nil
	})
	s.AddTool(lfsTool, lfsHandler)

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

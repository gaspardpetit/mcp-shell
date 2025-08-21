# Developer Guide

This repository contains `mcp-shell`, a Model Context Protocol (MCP) server written in Go.

## Project structure
- `main.go`: entry point that configures transports and registers tools.
- `internal/`: Go packages implementing each tool (e.g. `internal/shell` for `shell.exec`).
- `doc/`: documentation such as the function catalogue.
- `.github/workflows/`: CI configuration.

## Development workflow
- Use `go fmt ./...` for formatting.
- Use `go build ./...` to ensure the project builds.
- Use `go test ./...` to run tests (even if none yet exist).
- A convenience `Makefile` is provided; `make fmt build test` runs all of the above.

Before submitting code, run the commands above and ensure they succeed.

## Documentation
Keep `doc/functions.md` up to date whenever tools or endpoints are added or changed.


# Stage 1: Build Go executable
FROM golang:1.24-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -trimpath -ldflags="-s -w" -o /out/mcp-server ./main.go

# Stage 2: Final image based on prebuilt tools
ARG BASE_IMAGE
FROM ${BASE_IMAGE}

COPY --from=builder /out/mcp-server /app/mcp-server

# Healthcheck matches server endpoints
HEALTHCHECK --interval=30s --timeout=4s --start-period=10s --retries=3 \
  CMD curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null \
    || curl -fsS "http://127.0.0.1:${PORT}/mcp/health" >/dev/null \
    || exit 1

ENTRYPOINT ["/app/mcp-server"]
CMD ["--transport=http", "--addr=0.0.0.0:3333", "--log-dir=/logs", "--egress=0"]

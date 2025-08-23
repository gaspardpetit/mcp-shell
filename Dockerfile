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

# Install and configure local SearxNG instance
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    python3-dev \
    libxml2-dev \
    libxslt1-dev \
    libffi-dev \
    libssl-dev \
  && rm -rf /var/lib/apt/lists/* \
  && pip install --no-cache-dir pyyaml setuptools \
  && pip install --no-cache-dir --no-build-isolation git+https://github.com/searxng/searxng.git \
  && mkdir -p /etc/searxng \
  && python3 - <<'PY'
import os, shutil, searx
shutil.copyfile(os.path.join(os.path.dirname(searx.__file__), 'settings.yml'), '/etc/searxng/settings.yml')
PY
  && sed -i 's/ultrasecretkey/mcp-shell-secret/' /etc/searxng/settings.yml \
  && sed -i 's/port: 8888/port: 8080/' /etc/searxng/settings.yml \
  && sed -i 's/bind_address: "127.0.0.1"/bind_address: 0.0.0.0/' /etc/searxng/settings.yml \
  && sed -i 's/public_instance: false/public_instance: true/' /etc/searxng/settings.yml

COPY --from=builder /out/mcp-server /app/mcp-server

# Start SearxNG alongside the MCP server
RUN printf '#!/bin/bash\nset -e\nSEARXNG_SETTINGS_PATH=/etc/searxng/settings.yml searxng-run &\nexec /app/mcp-server "$@"\n' > /start.sh \
  && chmod +x /start.sh

# Healthcheck matches server endpoints
HEALTHCHECK --interval=30s --timeout=4s --start-period=10s --retries=3 \
  CMD curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null \
    || curl -fsS "http://127.0.0.1:${PORT}/mcp/health" >/dev/null \
    || exit 1

ENTRYPOINT ["/start.sh"]
CMD ["--transport=http", "--addr=0.0.0.0:3333"]

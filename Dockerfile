# =========================
# BUILD EXAMPLES:
#
# --- Default Build:
# docker build .
#
# --- Build with support for French and German OCR:
# docker build --build-arg TESS_LANGS_EXTRA="tesseract-ocr-fra tesseract-ocr-deu" .
#
# --- Add extra locales (English is always enabled):
# docker build --build-arg EXTRA_LOCALES="fr_CA.UTF-8 de_DE.UTF-8" .

# =========================
# RUN EXAMPLES:
# --- STDIO (interactive)
# docker run -i --rm mcp-shell --transport=stdio
#
# --- Streamable HTTP
# docker run --rm -p 3333:3333 mcp-shell --transport=http --addr=0.0.0.0:3333
#
# --- SSE
# docker run --rm -p 3333:3333 mcp-shell --transport=sse --addr=0.0.0.0:3333

# =========================
#  Stage 1: Go builder
# =========================
FROM golang:1.24-bookworm AS builder

# Enable reliable, reproducible builds
ENV CGO_ENABLED=0 \
    GO111MODULE=on

WORKDIR /src

# Copy go.mod/sum first for better caching; then the rest
COPY go.mod go.sum ./
RUN go mod download


# Copy the whole repo (assumes your main is at ./cmd/mcp-shell.go)
COPY . .

# Build the MCP server binary
# Adjust -X flags as you like (version, commit, build time)
RUN go build -trimpath -ldflags="-s -w" -o /out/mcp-server ./main.go


# =========================
#  Stage 2: Runtime base
# =========================
FROM debian:bookworm-slim AS runtime

# ---- Build-time knobs (choose your preset) ----
# light  -> smallest, shell + core utils + python basics + poppler + pandoc
# standard -> adds LibreOffice, ImageMagick, ffmpeg, tesseract, DuckDB, Node.js LTS
# full     -> standard + TeX subset for high-quality PDF via pandoc/xelatex
ARG PRESET=standard

# ---- Runtime knobs (can be overridden at run-time) ----
ENV APP_USER=user \
    APP_UID=10001 \
    APP_GID=10001 \
    APP_HOME=/home/user \
    WORKSPACE=/workspace \
    LOG_DIR=/logs \
    PORT=3333 \
    LISTEN_ADDR=0.0.0.0 \
    EGRESS=0 \
    DEBIAN_FRONTEND=noninteractive

# Base OS setup
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
      ca-certificates curl gnupg; \
    rm -rf /var/lib/apt/lists/*

# ---- Package sets (assembled by PRESET) ----
# Core across all presets
ENV PKGS_CORE="\
  bash coreutils findutils grep sed gawk procps psmisc \
  zip unzip tar gzip xz-utils p7zip-full zstd brotli \
  ripgrep fd-find jq yq xmlstarlet jo miller python3-csvkit bat \
  moreutils parallel \
  file tree \
  httpie \
  bind9-dnsutils iputils-ping traceroute netcat-openbsd socat \
  openssh-client rsync \
  locales \
  make cmake pkg-config build-essential \
  git git-lfs \
  openssl gpg \
  poppler-utils pandoc \
  python3 python3-pip python3-venv python3-distutils \
  sqlite3"

# Standard extras
ENV PKGS_STANDARD_EXTRA="\
  libreoffice \
  imagemagick \
  ffmpeg \
  tesseract-ocr tesseract-ocr-eng \
  ocrmypdf qpdf pdfgrep ghostscript \
  fonts-noto fonts-noto-cjk fonts-noto-color-emoji \
  libimage-exiftool-perl \
  nodejs"

# Full extras (TeX subset; sizable)
ENV PKGS_FULL_EXTRA="\
  texlive-xetex"

  # Node 22.x LTS (only if standard/full chosen)
RUN set -eux; \
    if [ "$PRESET" != "light" ]; then \
      curl -fsSL https://deb.nodesource.com/setup_22.x | bash -; \
      apt-get update; \
    fi

# Optional extra OCR languages (space-separated apt package names)
ARG TESS_LANGS_EXTRA=""

# Install packages based on PRESET (with preset-aware OCR defaults)
RUN set -eux; \
    apt-get update; \
    case "$PRESET" in \
      light)  tess_extra="${TESS_LANGS_EXTRA:-}";; \
      standard) tess_extra="${TESS_LANGS_EXTRA:-"tesseract-ocr-fra tesseract-ocr-deu tesseract-ocr-spa tesseract-ocr-ita tesseract-ocr-por"}";; \
      full)     tess_extra="${TESS_LANGS_EXTRA:-"tesseract-ocr-fra tesseract-ocr-deu tesseract-ocr-spa tesseract-ocr-ita tesseract-ocr-por tesseract-ocr-nld tesseract-ocr-swe tesseract-ocr-dan tesseract-ocr-nor tesseract-ocr-pol tesseract-ocr-ces tesseract-ocr-rus tesseract-ocr-chi-sim tesseract-ocr-chi-tra tesseract-ocr-jpn tesseract-ocr-kor"}";; \
      *)        echo "Unknown PRESET=$PRESET"; exit 1 ;; \
    esac; \
    case "$PRESET" in \
      light)  apt-get install -y --no-install-recommends $PKGS_CORE ;; \
      standard) apt-get install -y --no-install-recommends $PKGS_CORE $PKGS_STANDARD_EXTRA $tess_extra ;; \
      full)     apt-get install -y --no-install-recommends $PKGS_CORE $PKGS_STANDARD_EXTRA $PKGS_FULL_EXTRA $tess_extra ;; \
    esac; \
    apt-get autoremove -y; \
    rm -rf /var/lib/apt/lists/*


## ----- Locales (always English; optional extras) -----
# EXTRA_LOCALES: space-separated list like "fr_CA.UTF-8 de_DE.UTF-8"
ARG EXTRA_LOCALES=""

RUN set -eux; \
    # Determine default extras if none provided
    extras="$EXTRA_LOCALES"; \
    if [ -z "$extras" ]; then \
      case "$PRESET" in \
        light)  extras="";; \
        standard) extras="en_GB.UTF-8 en_CA.UTF-8 fr_CA.UTF-8 de_DE.UTF-8 es_ES.UTF-8 it_IT.UTF-8 pt_BR.UTF-8";; \
        full)     extras="en_GB.UTF-8 en_CA.UTF-8 fr_CA.UTF-8 de_DE.UTF-8 es_ES.UTF-8 it_IT.UTF-8 pt_BR.UTF-8 nl_NL.UTF-8 sv_SE.UTF-8 da_DK.UTF-8 nb_NO.UTF-8 pl_PL.UTF-8 cs_CZ.UTF-8 ru_RU.UTF-8 zh_CN.UTF-8 zh_TW.UTF-8 ja_JP.UTF-8 ko_KR.UTF-8";; \
        *)        echo "Unknown PRESET=$PRESET"; exit 1;; \
      esac; \
    fi; \
    # Always enable en_US.UTF-8
    sed -i 's/^# \?\(en_US\.UTF-8\)/\1/' /etc/locale.gen; \
    # Enable extras
    for loc in $extras; do \
      esc="$(printf '%s\n' "$loc" | sed 's/[.[\*^$]/\\&/g')"; \
      if ! grep -q "^$esc" /etc/locale.gen; then echo "$loc UTF-8" >> /etc/locale.gen; fi; \
      sed -i "s/^# \?$esc/$esc/" /etc/locale.gen; \
    done; \
    locale-gen

# Keep default process language in English
ENV LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8


ARG DUCKDB_VERSION=1.3.2
ARG TARGETARCH

RUN set -eux; \
  case "$TARGETARCH" in \
    amd64) duck_arch='linux-amd64' ;; \
    arm64) duck_arch='linux-arm64' ;; \
    *) echo "Unsupported arch: $TARGETARCH" >&2; exit 1 ;; \
  esac; \
  curl -fsSL -o /tmp/duckdb_cli.zip \
    "https://github.com/duckdb/duckdb/releases/download/v${DUCKDB_VERSION}/duckdb_cli-${duck_arch}.zip"; \
  unzip -q /tmp/duckdb_cli.zip -d /usr/local/bin; \
  chmod +x /usr/local/bin/duckdb; \
  rm -f /tmp/duckdb_cli.zip

# Python QoL libs in an isolated venv (PEP 668 friendly)
RUN set -eux; \
    python3 -m venv /opt/venv; \
    /opt/venv/bin/python -m pip install --no-cache-dir --upgrade pip setuptools wheel; \
    /opt/venv/bin/pip install --no-cache-dir \
        requests beautifulsoup4 lxml \
        numpy pandas pyarrow duckdb \
        openpyxl python-docx python-pptx \
        pypdf2 pdfminer.six \
        jinja2 tqdm

# Make the venv the default Python
ENV PATH="/opt/venv/bin:${PATH}"

# Alias debian specific names
RUN set -eux; \
    ln -sf /usr/bin/batcat /usr/local/bin/bat; \
    ln -sf /usr/bin/fdfind /usr/local/bin/fd

# Create non-root user and dirs
RUN set -eux; \
    groupadd -g "$APP_GID" "$APP_USER"; \
    useradd -m -u "$APP_UID" -g "$APP_GID" -s /bin/bash "$APP_USER"; \
    mkdir -p "$WORKSPACE" "$LOG_DIR" /app; \
    chown -R "$APP_UID:$APP_GID" "$WORKSPACE" "$LOG_DIR" /app

# Copy built server
COPY --from=builder /out/mcp-server /app/mcp-server

# Reasonable defaults: read-only rootfs + tmpfs are run-time flags (see sample run below)
USER $APP_USER
WORKDIR $WORKSPACE

# Healthcheck (lightweight)
# Replace with a real flag/endpoint from your server if available.
HEALTHCHECK --interval=30s --timeout=4s --start-period=10s --retries=3 \
   CMD curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null \
    || curl -fsS "http://127.0.0.1:${PORT}/mcp/health" >/dev/null \
    || exit 1


EXPOSE ${PORT}

# Logs directory (bind-mount on host for auditing)
ENV MCP_LOG_DIR=${LOG_DIR}
  
# Default entrypoint
# Adjust flags to match your server (e.g., --addr, --port, --egress, etc.)
ENTRYPOINT ["/app/mcp-server"]
CMD ["--transport=http", "--addr=0.0.0.0:3333", "--log-dir=/logs", "--egress=0"]

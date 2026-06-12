# ==============================================================================
# OCTOR MONOLITH DOCKERFILE
# ==============================================================================

# --- Stage 1: Go Builder ---
FROM golang:1.26.4-bookworm AS go-builder
WORKDIR /app

# Copy workspace configuration and module files first to cache dependencies
COPY go.work go.work.sum ./
COPY abuse-store/go.mod abuse-store/go.sum ./abuse-store/
COPY claims-provider/go.mod claims-provider/go.sum ./claims-provider/
COPY common-services/go.mod common-services/go.sum ./common-services/
COPY content-prober/go.mod content-prober/go.sum ./content-prober/
COPY content-transcoder/go.mod content-transcoder/go.sum ./content-transcoder/
COPY external-proxy/go.mod external-proxy/go.sum ./external-proxy/
COPY image-transformer/go.mod image-transformer/go.sum ./image-transformer/
COPY lazymap/go.mod ./lazymap/
COPY magnet2torrent/go.mod magnet2torrent/go.sum ./magnet2torrent/
COPY rest-api/go.mod rest-api/go.sum ./rest-api/
COPY s3-gateway/go.mod ./s3-gateway/
COPY srt2vtt/go.mod srt2vtt/go.sum ./srt2vtt/
COPY torrent-archiver/go.mod torrent-archiver/go.sum ./torrent-archiver/
COPY torrent-http-proxy/go.mod torrent-http-proxy/go.sum ./torrent-http-proxy/
COPY torrent-store/go.mod torrent-store/go.sum ./torrent-store/
COPY torrent-web-seeder/go.mod torrent-web-seeder/go.sum ./torrent-web-seeder/
COPY torrent-web-seeder-cleaner/go.mod torrent-web-seeder-cleaner/go.sum ./torrent-web-seeder-cleaner/
COPY url-store/go.mod url-store/go.sum ./url-store/
COPY vault/go.mod vault/go.sum ./vault/
COPY video-info/go.mod video-info/go.sum ./video-info/
COPY video-thumbnails-generator/go.mod video-thumbnails-generator/go.sum ./video-thumbnails-generator/
COPY web-ui/go.mod web-ui/go.sum ./web-ui/

# Download and cache Go dependencies
RUN go work sync && go mod download

# Copy the rest of the Go source files
COPY . .

# Build all Go services
RUN mkdir -p /app/bin && \
    SERVICES="rest-api web-ui vault abuse-store claims-provider torrent-store url-store video-info torrent-archiver srt2vtt content-transcoder magnet2torrent torrent-web-seeder content-prober torrent-http-proxy torrent-web-seeder-cleaner s3-gateway" && \
    for svc in $SERVICES; do \
        echo "Building $svc..."; \
        if [ -d "$svc/server" ]; then \
            cd "$svc/server" && go build -ldflags="-s -w" -o /app/bin/$svc . && cd /app || exit 1; \
        else \
            cd "$svc" && go build -ldflags="-s -w" -o /app/bin/$svc . && cd /app || exit 1; \
        fi; \
    done && \
    echo "Building create_nats_stream..." && \
    go build -ldflags="-s -w" -o /app/bin/create_nats_stream scripts/create_nats_stream.go && \
    echo "Building recover_db..." && \
    go build -ldflags="-s -w" -o /app/bin/recover_db scripts/recover_db.go && \
    echo "Building clean_orphans..." && \
    go build -ldflags="-s -w" -o /app/bin/clean_orphans scripts/clean_orphans.go


# --- Stage 2: Web UI & Node Builder ---
FROM node:24-bookworm AS node-builder
WORKDIR /app

# Copy Web UI package configs first to cache npm dependencies
COPY web-ui/package.json web-ui/package-lock.json ./web-ui/
RUN cd web-ui && npm ci

# Copy the rest of the Web UI source code and build it
COPY web-ui/ ./web-ui/
RUN cd web-ui && npm run build


# --- Stage 2b: Python Builder ---
FROM ubuntu:24.04 AS python-builder
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 \
    python3-pip \
    python3-venv \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app/sidecar
COPY sidecar/requirements.txt .
RUN python3 -m venv venv && \
    venv/bin/pip install --no-cache-dir -r requirements.txt


# --- Stage 3: Final Production Image ---
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive

# Install core runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    ffmpeg \
    supervisor \
    python3 \
    && rm -rf /var/lib/apt/lists/*

# Install Node.js v24.x
RUN curl -fsSL https://deb.nodesource.com/setup_24.x | bash - && \
    apt-get install -y --no-install-recommends nodejs && \
    rm -rf /var/lib/apt/lists/*

# Setup application directories
WORKDIR /app
RUN mkdir -p bin sidecar/venv ai-proxy web-ui/templates web-ui/locales web-ui/pub web-ui/assets/dist web-ui/migrations infra-data/badger /mnt/seeder-cache && \
    ln -s /app /srv/octor

# Copy runtime assets and application files (excluding source code and raw dev files)
COPY sidecar/ sidecar/
COPY ai-proxy/ ai-proxy/
COPY web-ui/templates/ web-ui/templates/
COPY web-ui/locales/ web-ui/locales/
COPY web-ui/pub/ web-ui/pub/
COPY web-ui/migrations/ web-ui/migrations/
COPY abuse-store/migrations/ abuse-store/migrations/
COPY url-store/migrations/ url-store/migrations/
COPY vault/migrations/ vault/migrations/
COPY torrent-http-proxy/services.yaml torrent-http-proxy/services.yaml

# Copy Go binaries
COPY --from=go-builder /app/bin/ bin/

# Copy compiled Web UI assets from Node builder stage
COPY --from=node-builder /app/web-ui/assets/dist/ web-ui/assets/dist/

# Copy Python virtual environment from Python Builder stage
COPY --from=python-builder /app/sidecar/venv/ sidecar/venv/

# Copy Supervisor configuration
COPY deploy/supervisord.conf /etc/supervisor/conf.d/supervisord.conf

# Expose public ports
EXPOSE 8080 8082 8086 9000 50052

# Start supervisord in foreground
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]

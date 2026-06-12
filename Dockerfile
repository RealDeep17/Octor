# ==============================================================================
# OCTOR MONOLITH DOCKERFILE
# ==============================================================================

# --- Stage 1: Go Builder ---
FROM golang:1.26.4-bookworm AS go-builder
WORKDIR /app
COPY . .
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
COPY . .
RUN cd web-ui && npm install && npm run build

# --- Stage 2b: Python Builder ---
FROM ubuntu:24.04 AS python-builder
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 \
    python3-pip \
    python3-venv \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY sidecar/requirements.txt .
RUN python3 -m venv venv && \
    venv/bin/pip install --no-cache-dir -r requirements.txt

# --- Stage 3: Final Production Image ---
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive

# Install core runtime dependencies (build-essential, pip, venv removed to reduce size, and --no-install-recommends added)
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

# Copy Go binaries
COPY --from=go-builder /app/bin/ bin/

# Copy Web UI templates, locales, public assets, compiled webpack assets, and migrations
# This avoids copying the massive node_modules directory and raw source files into the production image.
COPY --from=node-builder /app/web-ui/templates/ web-ui/templates/
COPY --from=node-builder /app/web-ui/locales/ web-ui/locales/
COPY --from=node-builder /app/web-ui/pub/ web-ui/pub/
COPY --from=node-builder /app/web-ui/assets/dist/ web-ui/assets/dist/
COPY --from=node-builder /app/web-ui/migrations/ web-ui/migrations/
COPY --from=node-builder /app/ai-proxy/ ai-proxy/

# Copy Python virtual environment from Python Builder stage
COPY --from=python-builder /app/venv/ sidecar/venv/

# Copy python sidecar code and other microservices configuration/assets
COPY sidecar/ sidecar/
COPY torrent-http-proxy/ torrent-http-proxy/
COPY abuse-store/migrations/ abuse-store/migrations/
COPY url-store/migrations/ url-store/migrations/
COPY vault/migrations/ vault/migrations/

# Copy Supervisor configuration
COPY deploy/supervisord.conf /etc/supervisor/conf.d/supervisord.conf

# Expose public ports
EXPOSE 8080 8082 8086 9000 50052

# Start supervisord in foreground
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]

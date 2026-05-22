# Octor

Octor is a high-performance, microservice-based media streaming platform. It accepts torrents or magnet links, resolves rich metadata, and provides a unified streaming experience through a modern Web UI and an optimized streaming edge proxy.

## 🚀 Key Features

- **Microservice Architecture:** Decoupled Go and Python services for scalability and reliability.
- **Unified Streaming Edge:** Optimized Torrent HTTP Proxy with HLS support and direct-play fallbacks.
- **Rich Metadata Enrichment:** Multi-tier fallback system (TMDB, Sidecar/OMDB, AI-based resolution).
- **Hybrid Storage Layer:** Support for local SSD/RAM caching and long-term cloud storage (Rclone/S3).
- **Zero-Leak S3 Gateway:** Custom high-performance S3 gateway optimized for buffered writes to cloud remotes.
- **Smart Recommendations:** AI-powered discovery and content analysis.
- **Universal WebDAV:** Administrative and user-scoped WebDAV access to all media.

## 📁 Repository Layout

| Path | Purpose |
|------|---------|
| `abuse-store/` | gRPC service for managing abuse reports and blacklists. |
| `ai-proxy/` | Node.js proxy translating Anthropic SDK calls to Google Gemini. |
| `claims-provider/` | Service for generating and validating JWT claims. |
| `content-prober/` | Media probing service (FFprobe-based). |
| `content-transcoder/` | HLS transcoding and VOD session management. |
| `magnet2torrent/` | Magnet-to-torrent metadata resolver. |
| `rest-api/` | Core REST API for resource and library management. |
| `s3-gateway/` | Custom high-performance S3 gateway for Rclone mounts. |
| `sidecar/` | Python-based metadata sidecar for adult and obscure content. |
| `srt2vtt/` | Subtitle conversion service. |
| `torrent-archiver/` | Service for generating ZIP archives of torrent contents. |
| `torrent-http-proxy/` | The primary streaming edge proxy and routing engine. |
| `torrent-store/` | Persistent store for torrent metainfo (BadgerDB backed). |
| `torrent-web-seeder/` | BitTorrent-backed HTTP/gRPC seeder. |
| `url-store/` | URL shortening and management service. |
| `vault/` | Long-term storage management and S3 archival worker. |
| `video-info/` | Unified video and subtitle metadata service. |
| `web-ui/` | The main user interface and library management frontend. |

## 🛠 Getting Started

### 📦 Technologies
- **Backend:** Go (Microservices), Python (FastAPI/Sidecar), Node.js (AI Proxy)
- **Frontend:** TypeScript, React/Svelte (Web UI)
- **Infrastructure:** Postgres, Redis, NATS, Docker, systemd
- **Storage:** Rclone, FUSE, Custom S3 Gateway
- **AI:** Google Gemini, Anthropic Claude

### 1. Infrastructure Setup
Octor requires Postgres, Redis, and NATS. Use the provided Docker Compose configuration to start these dependencies:

```sh
docker compose -f docker-compose.infra.yml up -d
```

### 2. Service Orchestration
Octor uses `systemd` for service management. The `switch_mode.sh` script is the primary tool for starting the stack and choosing performance profiles:

```sh
# Start the stack in RAM-disk performance mode (Recommended for speed)
./switch_mode.sh 1
```

### 3. Development Mode
For local development, you can choice to build all services using the `Makefile`:

```sh
make build
```

## ⚙️ Configuration

Configuration is managed via environment variables. Copy `example.env` to `custom.env` and modify it with your credentials.

### Key Environment Variables

| Variable | Description |
|----------|-------------|
| `PG_*` | Postgres connection details (Host, Port, User, Password, Database). |
| `REDIS_HOST`/`PORT` | Redis connection details (Default: 127.0.0.1:6380). |
| `NATS_HOST`/`PORT` | NATS connection details (Default: 127.0.0.1:4222). |
| `AWS_ENDPOINT` | Endpoint for the custom S3 Gateway (Default: http://localhost:9000). |
| `OCTOR_DOMAIN` | The public/local URL of your Octor instance. |
| `GEMINI_API_KEY` | API key for AI-powered recommendations and enrichment. |

## 📐 Architecture & Ports

Octor uses a standardized port mapping system to avoid collisions:

- **8xxx Range:** Public-facing entry points (Web UI: 8082, REST API: 8080, Sidecar: 8000, Vault: 8086).
- **500xx Range:** Internal gRPC and streaming services.
- **52xxx Range:** Health probes (Liveness/Readiness).
- **9000:** Custom S3 Gateway.
- **3456:** AI Proxy.

## 📄 Documentation

For more detailed information, refer to the following guides:

- [Development Guide](DEVELOPMENT.md): Technical architecture and local workflow.
- [Changelog](CHANGELOG.md): Record of all significant changes.
- [TODO List](TODO.md): Current task queue and roadmap.

## ⚖️ License

All rights reserved.

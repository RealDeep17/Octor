# Octor Development Guide

This document serves as the primary technical reference for Octor (Forkedv2), a self-hosted media streaming suite. It merges and replaces the legacy masterplan and handover documents.

## 🏗 Architecture Overview

Octor is a microservice-based platform consisting of Go and Python services.

### Core Services
- **Web UI**: Main frontend and entry point.
- **Rest API**: Core business logic and resource management.
- **Torrent Store**: Manages torrent metainfo storage.
- **Web Seeder**: Performs the actual seeding and data retrieval.
- **Torrent HTTP Proxy**: Edge service that routes streaming traffic.
- **Vault**: Manages persistent storage of downloaded torrents.
- **Sidecar (Python)**: NSFW metadata enrichment using TPDB and StashDB.

### Infrastructure
- **Postgres**: Main database.
- **Redis**: Caching and job queues (Host port: `6380` to avoid collisions).
- **NATS**: Event bus.
- **MinIO/S3**: Large file and torrent storage.

### Database Bootstrap
`docker-compose.infra.yml` mounts `init-db.sql` into Postgres so the local `url_store`, `abuse_store`, and `claims_provider` databases are created on first startup.

## 🛠 Hybrid Development Environment

To enable rapid iteration on macOS/ARM64:

1.  **Infrastructure in Docker**: Run `docker-compose.infra.yml` to start DBs and queues.
2.  **Services on Host**: Run `run_dev.sh` to start all microservices natively. This avoids Docker overhead and allows for fast rebuilds.
    - *Note*: In production, these should be migrated to `systemd` units (see TODO).

If Docker Desktop uses a non-default socket, run `./find_docker.sh` to locate the working socket and export the suggested `DOCKER_HOST`.

### Port Mapping Strategy (5-Digit System)
To prevent collisions, all services follow this mapping:
- **8xxx**: Entry points (API=8080, UI=8081, Sidecar=8000, Vault=8086).
- **500xx**: Primary GRPC/Service ports.
- **51xxx**: Pprof.
- **52xxx**: Probes (Liveness/Readiness).
- **53xxx**: Prometheus (Metrics).

Refer to `Forkedv2/review.md` for the full detailed map.

## 🔞 Metadata Enrichment

Enrichment is managed by `web-ui`. It uses a multi-tier fallback system:
1.  **TMDB**: Primary source for mainstream media.
2.  **Sidecar (OMDB Proxy)**: Falls back to local Python sidecar for adult content (ThePornDB/StashDB).
3.  **Kinopoisk**: Tertiary source for Russian content.
4.  **AI Resolver**: Last-resort identification using Claude (Anthropic).

## 🚀 Deployment (VPS)

The target production environment is an OCI-A1 VPS (ARM64).
- **Domain**: `octor.duckdns.org` (managed via DuckDNS updater in `run_dev.sh`).
- **Proxy**: Custom Caddy build with DuckDNS module for SSL.
- **Storage**: Hybrid Local + Rclone (Google Drive/OneDrive).

## 📝 Developer Workflows

### Bug Fixes & Edits
Track all bug fixes and file modifications in `CHANGELOG.md`.

### Tasks & Roadmap
The current work queue is maintained in `TODO.md`.

### Localizing Go Modules
If you modify `common-services` or other sub-repos, run `python3 localize_repos.py` to update `go.mod` files to use local paths instead of fetching from GitHub.

### Runtime Data
Badger database files, logs, editor swap files, build outputs, and local environment files are intentionally ignored. Do not commit generated runtime data such as `torrent-store/badger_data/`.

### Documentation Consolidation
`DEVELOPMENT.md`, `AUDIT.md`, `CHANGELOG.md`, and `TODO.md` replace the older planning and handover notes. Keep new operational decisions in these files so project history stays easy to scan.

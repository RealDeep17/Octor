# Octor Development Guide

This document serves as the primary technical reference for Octor, a self-hosted media streaming suite. It merges and replaces the legacy masterplan and handover documents.

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
- **S3 Gateway**: Custom lightweight S3-compatible gateway for large file and torrent storage.

### Database Bootstrap
`docker-compose.infra.yml` mounts `init-db.sql` into Postgres so the local `url_store`, `abuse_store`, and `claims_provider` databases are created on first startup.

## 🛠 Hybrid Development Environment

To enable rapid iteration on macOS/ARM64:

1.  **Infrastructure in Docker**: Run `docker-compose.infra.yml` to start DBs and queues.
2.  **Services Managed via Systemd**: Use `./switch_mode.sh` to start and manage microservices. This script handles starting all services in the correct dependency order and allows switching between performance modes (RAM vs SSD).
    - *Note*: Services are defined as `systemd` units (e.g., `octor-rest-api.service`).

If Docker Desktop uses a non-default socket, run `./find_docker.sh` to locate the working socket and export the suggested `DOCKER_HOST`.

### Port Mapping Strategy (5-Digit System)
To prevent collisions, all services follow this mapping:
- **8xxx**: Entry points (REST API=8080, Web UI=8082, Sidecar=8000, Vault=8086).
- **500xx**: Primary GRPC/Service ports.
- **51xxx**: Pprof.
- **52xxx**: Probes (Liveness/Readiness).
- **53xxx**: Prometheus (Metrics).

Refer to the port mapping section in the root README for the full detailed map.

## 🔞 Metadata Enrichment

Enrichment is managed by `web-ui`. It uses a multi-tier fallback system:
1.  **TMDB**: Primary source for mainstream media.
2.  **Sidecar (OMDB Proxy)**: Falls back to local Python sidecar for adult content (ThePornDB/StashDB).
3.  **Kinopoisk**: Tertiary source for Russian content.
4.  **AI Resolver**: Last-resort identification using Claude (Anthropic).

## 🔵 Admin Dashboard & Universal WebDAV Integration

The `WebDav-Web-UI_Universal_Viewing-Admin` branch extends Octor with user-scoped administration controls and universal WebDAV client integrations.

### Admin Octor UI Routing
The administration panel integrates seamlessly with standard Octor-style views under `/admin/*`:
- `/admin/library`: Shows all users' torrents with resource-level deduplication, including owner email tags on metadata rows and user-count badges on media cards.
- `/admin/library/movies` & `/admin/library/series`: Lists all media records filtered with real metadata, posters, years, and ratings.
- `/admin/vault`: Displays all users' active pledges and resources.
- *Scoping:* Admin views support dynamic user scoping via a `?user=<uuid>` parameter. The administrators list is specified by the `ADMIN_EMAILS` variable inside `custom.env`.

### Model Modifications
To support multi-user admin visibility, `web-ui/models/library.go` links library records with a database user relation:
```go
User *User `pg:"rel:has-one,fk:user_id"`
```

### WebDAV Universal Virtual Directories
WebDAV exposes an administrative virtual tree structure for authenticated administrators:
```text
admin/
├── torrents/        (Read-Only, deduped global library torrents)
├── movies/          (Read-Only, deduped global movies)
├── tvseries/        (Read-Only, deduped global series)
├── all/             (Read-Only, deduped all-user combined files)
└── users/
    └── {email}/
        ├── torrents (Read-Write, scoped to user library)
        ├── movies   (Read-Write, scoped to user movies)
        ├── tvseries (Read-Write, scoped to user series)
        └── all      (Read-Write, scoped to user combined files)
```
*Security:* The `admin/` directory path is entirely blocked/hidden from standard users. Per-user moves are strictly scoped and blocked from crossing boundary limits between different users.

### Testing and Verification
Run targeted unit tests using the standard protobuf registration warning workaround:
```bash
GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn /usr/bin/go test ./web-ui/handlers/admin ./web-ui/handlers/webdav ./web-ui/services/admin ./web-ui/models
```

## 🚀 Deployment (VPS)

The target production environment is an OCI-A1 VPS (ARM64).
- **Domain**: `octor.duckdns.org`
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
`DEVELOPMENT.md`, `CHANGELOG.md`, and `TODO.md` replace the older planning and handover notes. Keep new operational decisions in these files so project history stays easy to scan.

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

### Disaster Recovery & Storage Mapping
Octor employs a dual-storage mapping strategy to balance internal performance with human usability and disaster resilience:
1.  **Deduplicated System Storage (`vault/`)**: The `vault` worker stores raw torrent files hashed by their content (e.g., `vault/hash/hash`). This ensures perfect cross-user deduplication.
2.  **Human-Readable Mapping (`media/` & `torrents/`)**: When storing a file, the `vault` worker passes an `X-Amz-Meta-Human-Path` header to the custom `s3-gateway`. The gateway automatically creates symlinks on the Google Drive mount (e.g., `media/Movie Name [hash]/Video.mp4`) pointing back to the raw hashes. Additionally, a `.json` metadata receipt containing the exact file hashes and the user's `session_id` is saved alongside the `.torrent` file.
3.  **Self-Healing Recovery**: If the Postgres database is ever wiped, the `scripts/recover_db.go` script can be run. It connects to the `vault` and `octor` databases, scans the `torrents/` and `metadata/` folders on S3, and fully reconstructs all database tables, automatically re-assigning torrents back to the correct users' libraries based on the matched `session_id`. When users delete a torrent, its backup is safely moved to `torrents/.archived/` rather than being permanently destroyed.

## 🛠 Hybrid Development Environment

To enable rapid iteration on macOS/ARM64:

1.  **Unified Management**: Use `./run.sh` for all operational tasks. This script handles compiling binaries, installing systemd units, starting infrastructure, and managing microservices in the correct dependency order.
    - **Build**: `./run.sh build`
    - **Install Services**: `./run.sh install`
    - **Start/Switch Modes**: `./run.sh` (Interactive) or `./run.sh mode [X]`
    - **Status**: `./run.sh status`

2.  **Infrastructure**: Docker containers for Postgres, Redis, and NATS are managed automatically when using the `d` (Docker) flag with `./run.sh mode`.

If Docker Desktop uses a non-default socket, run `./run.sh doctor` to locate the working socket and export the suggested `DOCKER_HOST`.

### Port Mapping Strategy (5-Digit System)
To prevent collisions, all services follow this mapping:
- **8xxx Range:** Public-facing entry points (Web UI: `8082`, REST API: `8080`, Sidecar: `8000`, Vault: `8086`).
- **500xx Range:** Internal gRPC and streaming services.
- **51xxx Range:** pprof profiling endpoints.
- **52xxx Range:** Health probes (Liveness/Readiness).
- **53xxx Range:** Prometheus metrics endpoints.
- **54xxx Range:** Internal secondary Web interfaces.

Refer to the port mapping section in the root README for the full detailed map.

## 🔞 Metadata Enrichment

Enrichment is managed by `web-ui`. It uses a multi-tier fallback system:
1.  **TMDB**: Primary source for mainstream media.
2.  **Sidecar (OMDB Proxy)**: Falls back to local Python sidecar for adult content (ThePornDB/StashDB).
3.  **Kinopoisk**: Tertiary source for Russian content.
4.  **AI Resolver**: Last-resort identification using Claude (Anthropic).


## 🔞 Metadata Enrichment & Parity Testing

NSFW Metadata Enrichment is managed by `web-ui` and resolved using Go/Python sidecar proxies targeting TPDB/StashDB APIs. To test parity and accuracy between Go and Python sidecars, a comparison suite is available under `sidecar/test/` including the cached comparison script [compare.go](file:///Users/deepanshukumar/Downloads/Octor/sidecar/test/compare.go) and the live comparison script [compare_live.go](file:///Users/deepanshukumar/Downloads/Octor/sidecar/test/compare_live.go).

### Active Testsheets & Reports (Adult Content)
The following testsheets and reports are used for validation and incremental parity refinement:
*   **Baseline Adult Titles**: [testsheet691-adult.json](file:///Users/deepanshukumar/Downloads/Octor/sidecar/test/testsheet691-adult.json) (691 adult titles testsheet).
*   **Mismatches Baseline**: [testsheet447-mismatches.json](file:///Users/deepanshukumar/Downloads/Octor/sidecar/test/testsheet447-mismatches.json) (447 mismatch titles evaluated from the baseline run).
*   **Active Parity Delta**: [testsheet275-alpha1.json](file:///Users/deepanshukumar/Downloads/Octor/sidecar/test/testsheet275-alpha1.json) (275 remaining parities/mismatches/non-enriched titles for active refinement).
*   **Latest Comparison Results**: [compare_live_results.md](file:///Users/deepanshukumar/Downloads/Octor/sidecar/test/compare_live_results.md) (detailed categorization of mismatches, subcategorized with direct source DB links).
*   **Latest Raw Comparison Log**: [compare_live_output.log](file:///Users/deepanshukumar/Downloads/Octor/sidecar/test/compare_live_output.log) (raw execution logs).
*   **Parity Handover Summary**: [handover.md](file:///Users/deepanshukumar/Downloads/Octor/sidecar/test/handover.md) (current execution statistics, context summary, and next steps).

### Parity Refinement & Testing Rules
When refining the Go sidecar behavior:
1.  **Incremental Testsheets**: After each refinement or set of refinements, generate a new testsheet JSON in the `sidecar/test/` directory containing only the remaining non-parities. These must follow the naming convention `testsheet+$totalnum+testname.json` (e.g. [testsheet275-alpha1.json](file:///Users/deepanshukumar/Downloads/Octor/sidecar/test/testsheet275-alpha1.json)).
2.  **Exclude Perfect Matches**: Running subsequent comparison tests should exclude previously verified perfect matches (the list of titles will decrease over time as parity is achieved).
3.  **Exclude Non-Enriched**: Ignore titles that neither side could enrich (`NEITHER_ENRICHED`), as they are typically junk/poorly named files and not useful for parity tracing.

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
Run targeted unit tests normally; abuse-store and torrent-store now use distinct protobuf packages, so the old `GOLANG_PROTOBUF_REGISTRATION_CONFLICT` workaround is no longer required for web-ui tests:
```bash
go -C /srv/octor/web-ui test ./...
go -C /srv/octor/rest-api test ./...
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

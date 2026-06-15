# Octor Project Remediation Log

This document serves as the official record of all stabilization, security, and feature completion efforts performed on the Octor codebase.

## Table of Contents
1. [Phase 1: Critical System Triage](#phase-1-critical-system-triage)
2. [Phase 2: Security & Architecture Sanitization](#phase-2-security--architecture-sanitization)
3. [Phase 3: Resource Management & Stability](#phase-3-resource-management--stability)
4. [Phase 4: Storage Integrity & Concurrent I/O](#phase-4-storage-integrity--concurrent-io)
5. [Phase 5: Functional Backlog & Heuristics](#phase-5-functional-backlog--heuristics)
6. [Phase 6: Core Feature Completion & Compliance](#phase-6-core-feature-completion--compliance)
7. [Phase 7: Knowledge Base & CI Sync](#phase-7-knowledge-base--ci-sync)
8. [Phase 8: Critical Infrastructure Deaths](#phase-8-critical-infrastructure-deaths)
9. [Stremio Integration Hardening (Tier 1)](#stremio-integration-hardening-tier-1)
10. [Stremio Integration Hardening (Tier 2)](#stremio-integration-hardening-tier-2)
11. [Stremio Integration Hardening (Tier 3)](#stremio-integration-hardening-tier-3)
12. [Stremio Integration Hardening (Tier 4)](#stremio-integration-hardening-tier-4)

---

## Phase 1: Critical System Triage
**Goal:** Stabilize host OS resources, eliminate infinite crash loops, and remove global database lockouts.

*   **Bug 136 (Transcoder CPU Starvation):** Re-injected `-re` flag into FFmpeg command builder (`content-transcoder/services/transcode_run.go`) to enforce hardware rate-limiting.
*   **Bug 111 (Abuse-Store BadgerDB Global Write-Lock):** Replaced serialized `s.b.Update` with read-only `s.b.View` inside the `Check` function (`abuse-store/services/store.go`).
*   **Bug 137 (Edge-Proxy Out-Of-Bounds Panic):** Added safe length boundary checks to `torrent-http-proxy/services/service_location.go` and `torrent-web-seeder/server/services/common.go`.
*   **Bug 179 (SQLite SQLITE_BUSY Fatal Panic):** Replaced `panic(err)` with graceful error logging in `torrent-web-seeder/server/services/mmap.go`.
*   **Bug 147 (Subtitle Language Matcher Fatal Panic):** Inserted `len(langs) == 0` validation in `web-ui/handlers/action/helper.go`.
*   **Bug 162 (BadgerDB Garbage Collection Silent Suicide):** Fixed suicidal return statement in `abuse-store/services/badger.go` with `continue`.
*   **Bug 122 (Blind Interface Casting Panic):** Refactored type assertions to use the comma-ok idiom across several models (`web-ui/models/...`).

---

## Phase 2: Security & Architecture Sanitization
**Goal:** Eradicate unauthenticated internal routing, sanitize SSRF gateways, and remove brittle localhost assumptions.

*   **Bug 200 (SSRF via Export URL Parsing):** Implemented strict URL validation in `web-ui/services/api/api.go` to reject internal/private URLs.
*   **Bug 183 (Internal SSRF via Stremio Addons):** Sanitized `needsProxy` function in `web-ui/assets/src/js/lib/discover/client.js` to remove internal IP leaks.
*   **Bugs 202 & 188 (Metrics Infrastructure):** Refactored `web-ui/handlers/admin/status.go` to use environment-driven configuration and pooled connections.
*   **Bug 97 (Redis Public Exposure):** Updated `docker-compose.yml` to bind Redis strictly to `127.0.0.1`.

---

## Phase 3: Resource Management & Stability
**Goal:** Eradicate goroutine leaks, OOM memory bombs, and unbounded allocations.

*   **Bug 181 (50 MiB Torrent OOM):** Capped torrent payload size to 5MB in `torrent-store/services/server.go`.
*   **Bug 171 (S3 Gateway Memory Pressure):** Implemented a global concurrent upload limiter (64 slots) in `s3-gateway/main.go`.
*   **Bug 148 (Detached Goroutine DDoS):** Implemented singleton lock pattern using `TryLock()` for background enrichment tasks.
*   **Bug 172 (Infinite Redis Ping Leak):** Refactored lifecycle management in `torrent-http-proxy` and `lazymap` to ensure proper resource cleanup on cancellation.

---

## Phase 4: Storage Integrity & Concurrent I/O
**Goal:** Eliminate cloud storage leaks and resolve database deadlocks.

### Cloud Storage & S3
*   **Bug 105 (Orphaned S3 Multipart Uploads):** Updated `web-ui/services/vault/vault.go` and `vault/services/worker.go` to ensure S3 multipart uploads are explicitly aborted even when contexts are cancelled.

### Graceful I/O
*   **Bug 189 / 30 (NATS Event Loss):** Replaced `nc.Close()` with `nc.Drain()` in `common-services/nats.go` to prevent DMCA event loss during shutdown.

### Database Concurrency
*   **Bug 45 (Tracker Survival):** Ensured file completion tracker in `torrent-web-seeder` survives `SQLITE_BUSY` errors.
*   **Bug 66 (Mutex Starvation):** Implemented an atomic bitset for piece completion tracking, eliminating O(N) mutex locks.

---

## Phase 5: Functional Backlog & Heuristics
**Goal:** Fix brittle heuristics, logic bombs, and UI edge cases.

*   **Bug 150 (Regex Over-Truncation):** Injected fallback logic in `web-ui/services/parse_torrent_name/main.go` to preserve filenames when parser fails.
*   **Bug 37 (Dummy File Corruption):** Hardened dummy file generation in `rest-api/services/transmission.go` to prevent 0-byte file corruption.
*   **Bug 95 (Discovery Timeouts):** Increased hardcoded discovery timeout to 15s and added environment variable support (`SEARCH_TIMEOUT`).
*   **Bug 73 (Stream Deduplication):** Capped deduplication processing to 50 streams per infohash to prevent CPU exhaustion.
*   **Bug 87 (XSS Protection):** Refactored script generation in `web-ui/handlers/embed/get.go` to use JSON marshalling for safe injection.

---

## Phase 6: Core Feature Completion & Compliance
**Goal:** Implement privacy features, dashboard interactivity, and UX enhancements.

*   **Vault Dashboard:** Added clickable stat filters and async polling for real-time progress updates.
*   **GDPR Compliance:** Implemented full data portability (JSON export) and permanent viewing history erasure.
*   **Subtitle Controls:** Added persistent subtitle size selection (Small -> Extra Large) with `localStorage` persistence and cross-browser CSS overrides.

---

## Phase 7: Knowledge Base & CI Sync
**Goal:** Ensure technical parity between local codebase and AI analysis environments.

*   **Codebase Concatenation:** Developed `concat.sh` for surgical, partitioned codebase exports (1.5MB chunks) while respecting exclusions (binaries, junk, metadata).
*   **NotebookLM Integration:** Automated the synchronization of the full codebase and remediation logs to NotebookLM for deep technical auditing.
*   **Exclusion Logic:** Implemented strict exclusion rules for sensitive or irrelevant content (adult/streamio patterns) during the concatenation process.
*   **Documentation Finalization:** Restructured `REMEDIATION.md` into a comprehensive architectural log.

---

## Stremio Integration Hardening (Tier 1)
**Goal:** Eradicate SSRF gateways, prevent JSON-based OOM attacks, and move secrets to environment configuration.

*   **JSON OOM Protection:** Wrapped manifest response bodies in `io.LimitReader` (5MB) in `addon_validator.go` to prevent infinite stream memory exhaustion.
*   **SSRF Prevention:** Injected a custom `net.Dialer` into the Stremio HTTP client to explicitly reject all internal/private IP ranges (RFC1918).
*   **Secret Management:** Migrated hardcoded cryptographic signatures from `manifest.go` to the `STREMIO_ADDONS_SIGNATURE` environment variable.
*   **Discovery Reliability:** Increased stream discovery timeouts to 20s and enabled environment overrides to support high-latency DHT scraping.

---

## Stremio Integration Hardening (Tier 2)
**Goal:** Prevent CPU exhaustion during deduplication, optimize duplicate manifest parsing, and preserve private tracker swarms.

*   **Stream Deduplication Guard:** Capped stream list processing in `dedup_stream.go` to 50 streams per infohash, eliminating O(N^2) CPU/memory stalls from rogue/malicious addons.
*   **Flexible ID Normalization:** Expanded the addon ID normalization suffix check in `AddonWizard.jsx` from `{3,5}` to `{3,10}` to cover longer auto-generated instances securely, stopping duplicate catalogs and redundant queries.
*   **Private Tracker Swarm Rescue:** For library streams (`library.go`), parsed announce trackers from the S3 MagnetURI and injected them as `sources` (prefixed with `tracker:`) to prevent stalled playback and disconnection of private tracker swarms.

---

## Stremio Integration Hardening (Tier 3)
**Goal:** Address Mixed Content and Private Network blocks via clean browser headers and ensure asynchronous community manifest resilience.

*   **CORS & Private Network Access (PNA) Preflight:** Added a global `CORSPNAMiddleware` in `middleware.go` (registered in `web.go`) that handles OPTIONS preflight and `Access-Control-Request-Private-Network` requests securely, returning `Access-Control-Allow-Private-Network: true` to support self-hosters and Stremio Web seamlessly.
*   **Asynchronous Manifest Cache Refresher:** Implemented a background cron-like refresher daemon (`startSnapshotRefresher` running every 6 hours) in `handlers/stremio/stremio_addon_url/handler.go` that asynchronously updates `ManifestSnapshot` for all active URLs. If a remote addon is down, the system preserves the last functional cached manifest, avoiding empty Discover pages.

---

## Stremio Integration Hardening (Tier 4)
**Goal:** Native configuration arrays, robust browser LocalStorage protection, and zombie addon eradication.

*   **Dynamic User Configuration:** Registered parametric routes under `/:config` in `web-ui/handlers/stremio/handler.go` with `withConfig` middleware to parse and apply user-configured resolution/language settings natively.
*   **LocalStorage Schema Safeguard:** Hardened cache pruning in `manifestCache.js` with strict JSON schema validation to protect third-party or unparseable keys under the `stremio.manifest.` prefix.
*   **Zombie Addon Eradication:** Configured `StremioClient` constructor to strictly verify seeds against the active URLs list, and updated profile `handleDeleteAddon` to immediately clear deleted addons from `window._addons` and localStorage caches.

---

## Phase 8: Critical Infrastructure Deaths
**Goal:** Eradicate container boot crash loops, block path-traversal-based arbitrary file overwrites, prevent Billion Laughs DoS, stop S3 multipart orphaned-parts storage leaks, and eliminate FFmpeg transcoding OOM triggers.

*   **Abuse-Store Synchronous Boot Crash (O(N) Scan):** Abandoned global O(N) full-table db scans in `abuse-store/services/store.go` and migrated to a watermark-based incremental synchronization strategy using a new `sync_watermarks` database table with index support on `abuse(created_at)`. Additionally, moved initial boot synchronization inside a background goroutine in `abuse-store/serve.go` to guarantee container boot-up is instantaneous, preventing Kubernetes Liveness/Readiness probe timeouts.
*   **Path Traversal Vulnerability (S3 Gateway & WebDAV):** Hardened custom `s3-gateway/main.go` and WebDAV `web-ui/handlers/webdav/local.go` to clean path boundaries and enforce strict `strings.HasPrefix` parent-directory boundary verification on user-controlled inputs (such as the `X-Amz-Meta-Human-Path` header and WebDAV paths), fully eliminating arbitrary host file overwrites or unauthorized path access.
*   **Billion Laughs XML DoS:** Wrapped incoming WebDAV XML request bodies in `web-ui/services/webdav/internal/server.go` using an `io.LimitReader` explicitly capped to 1MB, ensuring recursive entity expansion attacks cannot exhaust memory resources and trigger container OOM kills.
*   **500GB Orphaned S3 Part Leaks:** Implemented dynamic S3 multipart upload part size calculation in `vault/services/worker.go` based on total file size / 10000 (rounded up to the nearest 5MB), allowing the worker to safely upload files larger than 500GB without hitting the S3 10,000 part limit. Also fixed the garbage collection sweep logic to skip database row deletions if an active S3 multipart upload abort attempt fails with a real error, preventing invisible storage leaks.
*   **FFmpeg Transcoder Scrubbing CPU/RAM pegging:** Implemented strict per-session concurrency in `content-transcoder` (`services/run_manager.go`, `services/session.go`). During user seeking or scrubbing across the video timeline, any previous FFmpeg run owned by the session is immediately force-terminated via `ReleaseForce`, bypassing the grace period and preventing concurrent process build-up and Linux OOM kills.

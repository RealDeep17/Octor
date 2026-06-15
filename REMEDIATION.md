# Full Project Remediation Log: Phases 1-4

## Phase 1: The "Bleeding Neck" Triage
We stabilized the host OS resources, eliminated infinite crash loops, and removed global database lockouts by patching the following:

- **Bug 136 (Transcoder CPU Starvation):** The agent re-injected the `-re` flag into the FFmpeg command builder in `content-transcoder/services/transcode_run.go` to enforce hardware rate-limiting and prevent CPU starvation.
- **Bug 111 (Abuse-Store BadgerDB Global Write-Lock):** The agent successfully targeted `abuse-store/services/store.go` and physically replaced the serialized `s.b.Update` method with the read-only `s.b.View` inside the `Check` function.
- **Bug 137 (Edge-Proxy Out-Of-Bounds Panic):** Safe length boundary checks (`if len(src.InfoHash) < 5`) were successfully added to `torrent-http-proxy/services/service_location.go` and `torrent-web-seeder/server/services/common.go` to prevent slice panics.
- **Bug 179 (SQLite SQLITE_BUSY Fatal Panic):** The fatal `panic(err)` invocation triggered by standard database locks inside `torrent-web-seeder/server/services/mmap.go` was stripped out and replaced with graceful error logging.
- **Bug 147 (Subtitle Language Matcher Fatal Panic):** An `if len(langs) == 0` validation check was successfully inserted into `web-ui/handlers/action/helper.go` before invoking the standard language matcher on empty arrays.
- **Bug 162 (BadgerDB Garbage Collection Silent Suicide):** The suicidal return statement in `abuse-store/services/badger.go` was replaced with a `continue` statement to ensure the garbage collection routine stays alive in the background.
- **Bug 122 (Blind Interface Casting Panic):** Blind type assertions in `web-ui/models/kinopoisk_unofficial/info.go`, `web-ui/models/omdb/info.go`, and `web-ui/models/video_stream.go` were safely refactored to use the comma-ok idiom.

---

## Phase 2: The SaaS-to-Self-Hosted Exorcism
We eradicated unauthenticated internal routing, sanitized Server-Side Request Forgery (SSRF) gateways, and removed brittle localhost assumptions by patching the following:

- **Bug 200 (SSRF via Export URL Parsing):** Strict URL validation was implemented in `web-ui/services/api/api.go` within the `Download` and `DownloadWithRange` methods to prevent SSRF by explicitly rejecting internal URLs.
- **Bug 183 (Internal SSRF via Stremio Addons):** Regex rules targeting internal IP addresses and domains were successfully removed from the `needsProxy` function in `web-ui/assets/src/js/lib/discover/client.js`.
- **Bugs 202 & 188 (Hardcoded Localhost Metrics & Prometheus DDoS):** Metrics fetching in `web-ui/handlers/admin/status.go` was refactored to use environment variables and a connection-pooled `metricsClient`.
- **Bug 97 (Redis Unauthenticated Public Exposure):** The Redis service port mapping in `docker-compose.yml` was updated to bind strictly to the `127.0.0.1` localhost interface.

---

## Phase 3: Goroutine Leaks & OOM Memory Bombs
We eradicated unbounded memory allocations, enforced strict context timeouts, and implemented lifecycle management for background threads by patching the following:

- **Bug 181 (50 MiB Unbounded AST Parsing OOM):** The torrent payload size was strictly capped to 5MB in `torrent-store/services/server.go` to prevent unbounded in-memory Abstract Syntax Tree parsing and OS-level memory kills.
- **Bug 171 (S3 Gateway Global RAM Buffer OOM Kill):** A global concurrent upload limiter with 64 slots was implemented in `s3-gateway/main.go` to cap active uploads and bound memory usage.
- **Bug 148 (24-Hour Detached Goroutine API DDoS):** A singleton lock pattern using `TryLock()` was implemented in `web-ui/handlers/admin/enrichment.go` and `web-ui/handlers/admin/handler.go` to eradicate detached 24-hour enrichment threads from destroying API quotas.
- **Bug 172 (Torrent-HTTP-Proxy Infinite Redis Ping Leak):** The `probeRedis` lifecycle in `torrent-http-proxy/services/hybrid_bucket.go` was refactored to implement `io.Closer` and utilize a `select` block listening for context cancellation. Additionally, `lazymap/lazymap.go` was updated to call `Close()` on evicted and canceled items.

---

## Phase 4: Storage Collapse & Data Corruption
We eliminated cloud storage leaks, fixed violent socket teardowns dropping DMCA compliance events, and resolved synchronous database locking routines that permanently kill state trackers.

### Architectural Cluster 1: Cloud Storage Leaks & Billing Explosions
- **Bug 105 (Orphaned S3 Multipart Uploads - AWS Bill Explosion):** 
  - **Target Files:** `web-ui/services/vault/vault.go` and `vault/services/worker.go`
  - **Remediation:** In `vault.go` (`defundPledge`), added an explicit call to `vaultApi.DeleteResource` when funding drops below the required amount. This natively queues the resource for deletion, causing the worker's lease heartbeat to fail and subsequently cancelling the active worker context. In `worker.go`, modified the `storeFile` loop to explicitly check `ctx.Err() != nil`, ensuring the loop breaks. Crucially, updated the `AbortMultipartUploadWithContext` routine in `worker.go` to use a fresh `context.Background()` with a timeout so that even when the parent context is dead, the API call to abort the S3 upload successfully transmits to AWS and deletes the orphaned chunks.

### Architectural Cluster 2: Violent I/O Teardowns & Event Loss
- **Bug 189 / 30 (Dangling NATS Connections & DMCA Event Loss):**
  - **Target File:** `common-services/nats.go`
  - **Remediation:** Replaced the fatal `nc.Close()` invocation with `nc.Drain()`. This graceful shutdown procedure allows buffered messages (like `resource.banned` DMCA events) to safely transmit before the container exits, ensuring critical DMCA events are not permanently dropped during Kubernetes pod termination.

### Architectural Cluster 3: Database Deadlocks & Tracker Suicides
- **Bug 45 (SQLite File Completion Tracker Permanent Death):**
  - **Target File:** `torrent-web-seeder/server/services/piece_completion.go`
  - **Remediation:** Located the `ret.CompleteFile(f)` invocation inside the detached background goroutine used to flush completed files. Replaced the fatal `return` statement with a `continue` statement and graceful error logging. This ensures the file completion tracker survives transient `SQLITE_BUSY` database locks instead of permanently exiting.

- **Bug 66 (Piece Completion O(N) Mutex Starvation):**
  - **Target File:** `torrent-web-seeder/server/services/piece_completion.go`
  - **Remediation:** Refactored `GetCompletedFiles()` and the completions struct. Extracted the `s.pieces` verification into a lock-free structure using an atomic bitset (`[]int32` manipulated via `sync/atomic`). This drastically narrowed the scope of `s.mux.Lock()` so it no longer wraps the O(N) piece iteration array, successfully eliminating the severe mutex starvation that starved other concurrent status and stream queries.
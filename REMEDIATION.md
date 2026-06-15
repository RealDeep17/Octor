# Full Project Remediation Log: Phases 1-4

## Phase 1: The "Bleeding Neck" Triage
We stabilized the host OS resources, eliminated infinite crash loops, and removed global database lockouts by patching the following:

- **Bug 136 (Transcoder CPU Starvation):** The agent re-injected the -re flag into the FFmpeg command builder in content-transcoder/services/transcode_run.go to enforce hardware rate-limiting and prevent CPU starvation.
- **Bug 111 (Abuse-Store BadgerDB Global Write-Lock):** The agent successfully targeted abuse-store/services/store.go and physically replaced the serialized s.b.Update method with the read-only s.b.View inside the Check function.
- **Bug 137 (Edge-Proxy Out-Of-Bounds Panic):** Safe length boundary checks (if len(src.InfoHash) < 5) were successfully added to torrent-http-proxy/services/service_location.go and torrent-web-seeder/server/services/common.go to prevent slice panics.
- **Bug 179 (SQLite SQLITE_BUSY Fatal Panic):** The fatal panic(err) invocation triggered by standard database locks inside torrent-web-seeder/server/services/mmap.go was stripped out and replaced with graceful error logging.
- **Bug 147 (Subtitle Language Matcher Fatal Panic):** An if len(langs) == 0 validation check was successfully inserted into web-ui/handlers/action/helper.go before invoking the standard language matcher on empty arrays.
- **Bug 162 (BadgerDB Garbage Collection Silent Suicide):** The suicidal return statement in abuse-store/services/badger.go was replaced with a continue statement to ensure the garbage collection routine stays alive in the background.
- **Bug 122 (Blind Interface Casting Panic):** Blind type assertions in web-ui/models/kinopoisk_unofficial/info.go, web-ui/models/omdb/info.go, and web-ui/models/video_stream.go were safely refactored to use the comma-ok idiom.

---

## Phase 2: The SaaS-to-Self-Hosted Exorcism
We eradicated unauthenticated internal routing, sanitized Server-Side Request Forgery (SSRF) gateways, and removed brittle localhost assumptions by patching the following:

- **Bug 200 (SSRF via Export URL Parsing):** Strict URL validation was implemented in web-ui/services/api/api.go within the Download and DownloadWithRange methods to prevent SSRF by explicitly rejecting internal URLs.
- **Bug 183 (Internal SSRF via Stremio Addons):** Regex rules targeting internal IP addresses and domains were successfully removed from the needsProxy function in web-ui/assets/src/js/lib/discover/client.js.
- **Bugs 202 & 188 (Hardcoded Localhost Metrics & Prometheus DDoS):** Metrics fetching in web-ui/handlers/admin/status.go was refactored to use environment variables and a connection-pooled metricsClient.
- **Bug 97 (Redis Unauthenticated Public Exposure):** The Redis service port mapping in docker-compose.yml was updated to bind strictly to the 127.0.0.1 localhost interface.

---

## Phase 3: Goroutine Leaks & OOM Memory Bombs
We eradicated unbounded memory allocations, enforced strict context timeouts, and implemented lifecycle management for background threads by patching the following:

- **Bug 181 (50 MiB Unbounded AST Parsing OOM):** The torrent payload size was strictly capped to 5MB in torrent-store/services/server.go to prevent unbounded in-memory Abstract Syntax Tree parsing and OS-level memory kills.
- **Bug 171 (S3 Gateway Global RAM Buffer OOM Kill):** A global concurrent upload limiter with 64 slots was implemented in s3-gateway/main.go to cap active uploads and bound memory usage.
- **Bug 148 (24-Hour Detached Goroutine API DDoS):** A singleton lock pattern using TryLock() was implemented in web-ui/handlers/admin/enrichment.go and web-ui/handlers/admin/handler.go to eradicate detached 24-hour enrichment threads from destroying API quotas.
- **Bug 172 (Torrent-HTTP-Proxy Infinite Redis Ping Leak):** The probeRedis lifecycle in torrent-http-proxy/services/hybrid_bucket.go was refactored to implement io.Closer and utilize a select block listening for context cancellation. Additionally, lazymap/lazymap.go was updated to call Close() on evicted and canceled items.

---

## Phase 4: Storage Collapse & Data Corruption
We eliminated cloud storage leaks, fixed violent socket teardowns dropping DMCA compliance events, and resolved synchronous database locking routines that permanently kill state trackers.

### Architectural Cluster 1: Cloud Storage Leaks & Billing Explosions
- **Bug 105 (Orphaned S3 Multipart Uploads - AWS Bill Explosion):** 
  - **Target Files:** web-ui/services/vault/vault.go and vault/services/worker.go
  - **Remediation:** In vault.go (defundPledge), added an explicit call to vaultApi.DeleteResource when funding drops below the required amount. This natively queues the resource for deletion, causing the worker's lease heartbeat to fail and subsequently cancelling the active worker context. In worker.go, modified the storeFile loop to explicitly check ctx.Err() != nil, ensuring the loop breaks. Crucially, updated the AbortMultipartUploadWithContext routine in worker.go to use a fresh context.Background() with a timeout so that even when the parent context is dead, the API call to abort the S3 upload successfully transmits to AWS and deletes the orphaned chunks.

### Architectural Cluster 2: Violent I/O Teardowns & Event Loss
- **Bug 189 / 30 (Dangling NATS Connections & DMCA Event Loss):**
  - **Target File:** common-services/nats.go
  - **Remediation:** Replaced the fatal nc.Close() invocation with nc.Drain(). This graceful shutdown procedure allows buffered messages (like resource.banned DMCA events) to safely transmit before the container exits, ensuring critical DMCA events are not permanently dropped during Kubernetes pod termination.

### Architectural Cluster 3: Database Deadlocks & Tracker Suicides
- **Bug 45 (SQLite File Completion Tracker Permanent Death):**
  - **Target File:** torrent-web-seeder/server/services/piece_completion.go
  - **Remediation:** Located the ret.CompleteFile(f) invocation inside the detached background goroutine used to flush completed files. Replaced the fatal return statement with a continue statement and graceful error logging. This ensures the file completion tracker survives transient SQLITE_BUSY database locks instead of permanently exiting.

- **Bug 66 (Piece Completion O(N) Mutex Starvation):**
  - **Target File:** torrent-web-seeder/server/services/piece_completion.go
  - **Remediation:** Refactored GetCompletedFiles() and the completions struct. Extracted the s.pieces verification into a lock-free structure using an atomic bitset ([]int32 manipulated via sync/atomic). This drastically narrowed the scope of s.mux.Lock() so it no longer wraps the O(N) piece iteration array, successfully eliminating the severe mutex starvation that starved other concurrent status and stream queries.

---

## Phase 5: Non-Essential Backlog
We fixed brittle heuristics, API constraints, logic bombs, and UI/UX edge cases that break functionality without bringing down the host OS.

### Architectural Cluster 1: Brittle Heuristics & Regex Over-Truncation
- **Bug 150 (Torrent Parser Over-Truncation Database Brick):**
  - **Target File:** web-ui/services/parse_torrent_name/main.go
  - **Remediation:** Physically injected the fallback logic `if tor.Title == "" { tor.Title = filename }` immediately below the `tor.Map(ms)` invocation. This ensures that if the regex parser strips the title entirely (e.g., for movies titled "2012"), the original filename is preserved to satisfy the database NOT NULL constraint.

- **Bug 37 (Dummy File Panic & Corruption):**
  - **Target File:** rest-api/services/transmission.go
  - **Remediation:** Updated dummyTemplatePath to return an error if no template is found. Modified ensureDummyFiles to return an error immediately (`if createErr != nil { return createErr }`) if template creation fails. Completely removed all logic that attempted to create 0-byte or empty dummy files, protecting downstream ARR ingestion pipelines from corrupted headerless assets.

### Architectural Cluster 2: External API Constraints & DoS Fallbacks
- **Bug 95 (8-Second Hardcoded Stream Discovery DoS):**
  - **Target File:** web-ui/assets/src/js/lib/discover/client.js
  - **Remediation:** Increased the hardcoded search timeout from 8 seconds to 15 seconds to accommodate slower, high-quality third-party indexers. Added support for overrides via the window._env.SEARCH_TIMEOUT environment variable.

- **Bug 73 (Dedup Stream Service O(N^2) Exhaustion):**
  - **Target File:** web-ui/services/stremio/dedup_stream.go
  - **Remediation:** Implemented a hard limit of 50 streams processed per infohash in the deduplication logic to prevent O(N^2) memory and CPU exhaustion.

### Architectural Cluster 3: UI Glitches & Frontend Injections
- Bug 87 (Cross-Site Scripting (XSS) via Unsanitized fmt.Sprintf Injection):
  - Target File: web-ui/handlers/embed/get.go
  - Remediation: Refactored generateCheckScript to use json.Marshal for all strings injected into the raw JavaScript template, preventing arbitrary JavaScript execution.

---

## Phase 6: Core Feature Completion & Compliance
We implemented the remaining high-value features requested by the project owners, focusing on user privacy, dashboard interactivity, and video player enhancements.

### 1. Vault Dashboard Interactivity
- **Target Files:** `web-ui/assets/src/js/app/vault/progress.js`, `web-ui/templates/partials/vault/stats.html`, `web-ui/templates/views/vault/index.html`
- **Remediation:** 
  - Added clickable stat cards (Vaulted, Processing, Expiring) that automatically filter the Vault pledges list.
  - Implemented a 10-second `setInterval` polling loop utilizing the `data-async-layout` pattern to dynamically auto-refresh the "Loading/Processing" metric.
  - Added a new "Expiring" status filter for torrents that have lost their pledge funding.

### 2. GDPR Data Portability & Erasure
- **Target Files:** `web-ui/models/video_status_gdpr.go`, `web-ui/handlers/profile/handler.go`, `web-ui/templates/views/profile/get.html`
- **Remediation:** 
  - Built an "Export Data" endpoint that serializes the user's `MovieStatus`, `SeriesStatus`, `EpisodeStatus`, and `WatchHistory` telemetry into a downloadable JSON file.
  - Built a "Clear Watch History" endpoint with a UI confirmation modal that permanently hard-deletes all viewing telemetry (including the highly sensitive `watch_history` table) from the database to comply with the Right to Erasure.

### 3. Video Player Subtitle Size Control
- **Target Files:** `web-ui/assets/src/js/lib/player/Player.jsx`, `web-ui/templates/views/action/stream_video.html`, `web-ui/assets/src/styles/player.css`
- **Remediation:** 
  - Added a "Subtitle Size" selection block (Small, Medium, Large, Extra Large) to the video player's subtitles modal.
  - Implemented the `wireSubtitleSizeHandlers` JavaScript logic to persist the user's size preference in `localStorage` and apply it to the player container.
  - Added CSS overrides using the `video::cue` pseudo-element and the `data-subtitle-size` attribute to scale the subtitle text across all browsers and HLS implementations.
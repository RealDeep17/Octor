# CHANGELOG

## [Unreleased] - 2026-06-10

### Fixed
- **Web UI Protobuf Loading**: Added unique protobuf package namespaces for abuse-store and torrent-store so full `web-ui test ./...` no longer needs conflict-policy workarounds.
- **WebDAV Admin Root**: Normal users can no longer list or resolve the `admin/` WebDAV child.
- **REST API Transmission RPC**: ARR request classification now reads all configured Transmission download clients from Sonarr/Radarr/Whisparr SQLite DBs instead of only the first row.
- **REST API Dummy Templates**: Fresh clones now include small tracked dummy media templates under `rest-api/assets/dummy/`, with runtime fallback from `/srv/octor/infra-data`.
- **Embed SDK Build**: Fixed the SDK entrypoint import so `npm --prefix embed-sdk-js run build` succeeds after `npm ci`.
- **Sidecar Tests**: Replaced the live API stress script with deterministic pytest coverage for filename parsing and adult-studio detection.
- **NSFW/Adult Discover Watchlist & Badges**: Resolved issues preventing the watchlist, watched status, and rating badges from working for NSFW (Adult, Porn, JAV) content.
  - Updated backend watchlist validation to accept `tpdb:`, `tpdb_jav:`, and `stash:` video ID prefixes.
  - Updated backend content type parsing to map `"adult"`, `"porn"`, and `"jav"` types to `ContentTypeMovie`.
  - Updated frontend `discoverUtils.js` to normalize the type parameter to `"movie"` when making API calls for adult/porn/jav content, and corrected the status check filter to allow `tpdb_jav:` IDs.
  - Updated frontend layout toggle type mapping in `ItemGrid.jsx` to map adult/porn/jav items to `"movie"`.

## [2.3.0] - 2026-05-19

### Fixed
- **FUSE Block-Write Queue Latency Bottleneck (R&D)**: Resolved the severe write performance crawl (30 KB/s limit) over FUSE Union mounts (`--vfs-cache-mode off`) to Google Drive by implementing an optimized 16MB sequential RAM write buffer via Go's `bufio.Writer` inside `s3-gateway`. Skyrocketed write speed to **23+ MB/s** and eliminated microservice timeouts/stuck queues.
- **Seeder SSD Cache Piece Thrashing**: Wiped out stale SQLite `.torrent.db` indexing issues that served corrupted/zeroed blocks (SHA-1 mismatches). Scaled up `PER_TORRENT_CACHE_BUDGET` from `1.5GB` test limit to `25GB` production limit in `custom.env` to prevent LRU eviction from thrashing active streaming/reading blocks of large files.
- **Microservice Port Collisions & Routing**: Wired up `custom.env` loading in `octor-s3-gateway.service` systemd configuration using `EnvironmentFile` to securely bind dynamic buffer size controls.
- **Video Fallback & Audio Silence**: Resolved standard browser audio silence by configuring HLS transcode configurations to support high-performance HEVC/H.265 video-copy HLS streaming with real-time audio transcoding to stereo AAC on the fly.
- **Default AAC Encoder Crash**: Fixed content-transcoder runtime crashes by switching the default audio encoder from `libfdk_aac` to standard `aac` in the system FFmpeg configurations.

### Added
- **Human-Readable Cloud Storage**: The `vault` worker and `s3-gateway` now automatically map and symlink raw deduplicated file hashes into human-readable directories (e.g., `media/Movie Name [hash]/Video.mp4`) directly on the Google Drive mount, preserving cross-user deduplication while allowing native browsing.
- **Self-Healing Disaster Recovery**: Added `scripts/recover_db.go`, a one-click fully automated recovery tool that scans cloud metadata receipts and `.torrent` files to instantly reconstruct the Postgres `vault` and `octor` (Web UI) databases, correctly re-assigning torrents to user libraries based on matched `session_id`. Torrents removed by users are now safely relocated to `torrents/.archived/` rather than permanently destroyed.
- **Dynamic Gateway Write Buffer Size**: Exposed `S3_GATEWAY_WRITE_BUFFER_SIZE` inside `custom.env`, allowing full runtime control over the RAM sequential write buffer size (defaults to 16MB; set to `0` to cleanly disable).
- **Admin Universal WebDAV Virtual Directories**: Implemented high-level `admin/` scoped WebDAV directories: read-only global directories (`admin/torrents`, `admin/movies`, `admin/tvseries`, `admin/all`) and per-user read-write scoped subdirectories (`admin/users/{email}/all`). WebDAV moves are strictly validated and user-scoped.
- **Universal Admin Octor UI**: Added dedicated admin analytics and library views (`/admin/library`, `/admin/library/movies`, `/admin/library/series`, `/admin/vault`) displaying all users' active media card listings and active pledges with owner email identification, deduped at the resource level.
- **Library User Relation Binding**: Integrated pg relation model mapping `User *User pg:"rel:has-one,fk:user_id"` to link all torrent library records to user tables dynamically.
- **Multi-Branch Root Markdown Rule**: Configured root `.gitignore` rules to completely ignore temporary root markdown files while preserving core documentation (`README.md`, `CHANGELOG.md`, `DEVELOPMENT.md`, `TODO.md`) across all active branches.

### Changed
- **Developer Documentation Consolidation**: Remade `TODO.md` with a comprehensive multi-branch specification matrix, updated `DEVELOPMENT.md` by fully merging `ADMIN_UNIVERSAL_VIEW_PLAN.md` into it, and deleted the redundant `AUDIT.md`, `OCTOR_VPS_BUNDLE.md`, and `ADMIN_UNIVERSAL_VIEW_PLAN.md` files.
- **SuperTokens User ID Caching**: Integrated an in-memory LRU cache in `GetUserByID` calls to eliminate page load lag and redundant database query overhead.

---

## [Unreleased] - 2026-05-16

### Removed
- **Legacy Dev Scripts**: Deleted `run_dev.sh` and `run_dev_skip.sh` in favor of `run.sh mode` and systemd-managed services.
- **MinIO Container**: Fully deprecated and removed MinIO from the infrastructure stack in favor of the lightweight custom `s3-gateway`.

### Fixed
- **HLS Streaming Pipeline**: Resolved persistent 404/502/504 errors in the video player.
- **Content Transcoder**: Added legacy compatibility layer for `~vod/hls` path handling.
- **Audio Encoding**: Switched default AAC encoder from `libfdk_aac` to `aac` for compatibility with standard FFmpeg builds.
- **Microservice Mesh**: Unified port mapping (80xx for web, 500xx for APIs, 52xxx for probes) to eliminate port collisions.
- **Header Propagation**: Fixed `X-Source-Url` injection by properly configuring proxy host/port identification.
- **SRT2VTT**: Corrected invalid struct embedding in subtitle service.
- **Proto Packages**: Regenerated/fixed proto package names so service clients import `proto` packages consistently instead of invalid generated package names.
- **LazyMap Embedding**: Updated services to embed `*lazymap.LazyMap[...]` pointers where required by the current shared helper implementation.
- **Magnet2Torrent Wiring**: Updated REST API imports and default service port to match the local `magnet2torrent` proto package and port map.

### Added
- **Proxy Self-Identification**: Added `--torrent-http-proxy-host` and `--port` flags to `run_dev.sh`.
- **Legacy HLS Handler**: Compatibility route in `content-transcoder` to support older API URL formats.
- **Infra DB Bootstrap**: Added `init-db.sql` and mounted it into `docker-compose.infra.yml` to create the local service databases automatically.
- **Docker Socket Helper**: Added `run.sh doctor` to detect the active Docker socket on macOS/Linux host setups.
- **Ignore Rules**: Ignored local Badger runtime data and editor swap/temp files.
- **Cache Lifetimes**: Added `.env`-configurable media cache cleanup and metadata/temp cache expiry controls.
- **Project README**: Added a top-level factual README with the local architecture, run flow, runtime state meanings, and environment-variable customization reference.
- **Torrent Store Badger Config**: Added `BADGER_PATH` support and made `BADGER_EXPIRE=0` disable Badger TTL.

### Changed
- **Development Docs**: Consolidated the active development guide, audit, changelog, and task list into `DEVELOPMENT.md`, `AUDIT.md`, `CHANGELOG.md`, and `TODO.md`.
- **Local Tooling**: Made `localize_repos.py` and `master_fix.py` resolve paths relative to the repo instead of a hardcoded Windows path.
- **Resource Status Checks**: Made passive media status checks non-activating, so opening a media page or removing from vault does not start seeder caching by itself.
- **Vault Progress UI**: Show full live vaulting/caching details on the vault page, while keeping resource-page status compact with percent and seed count.
- **Streaming Startup UI**: Reveal the player as soon as the stream template renders instead of waiting forever for a late/missing player-ready event.
- **Vault Removal Cleanup**: Removing a resource from vault now also purges the active seeder cache path and drops the running torrent session.
- **Status SSE Cadence**: Reduced resource/vault status SSE polling to 200 ms and made seeder stat diffs include seed/leech/status changes.

### Removed
- **Superseded Notes**: Removed legacy planning and handover documents now covered by the consolidated docs (`TO-DO.md`, `VPS_HANDOVER.md`, `custom_octor_plan.md`, `octor_masterplan.md`).

---

## [2.2.0] - 2026-05-17

### Fixed
- **Vault Remove Re-cache**: Removing an item from vault no longer triggers automatic background caching; item correctly returns to idle state.
- **Zero-VP Deletion Bug**: Fixed `newFundedVP < resource.RequiredVP` check to also handle zero-required-VP resources so vault entry is always deleted when all pledges are removed.
- **Poster/Still S3 Cache**: S3 cache read/write failures are now non-fatal; poster renders directly without a hard 500 when MinIO bucket is missing or misconfigured.
- **Subtitles Job Ordering**: Fixed `j.Done()` being called inside the error branch for OpenSubtitles, preventing the job from stalling when subtitle fetch fails.
- **item_id Missing from SSE Widget**: Status SSE endpoint now receives the correct `item_id` from the media page template, fixing stale passive cache detection.
- **Adult Metadata False Positives**: TMDB and Kinopoisk are skipped for adult-flagged torrents, routing lookups exclusively through the OMDB/adult sidecar to prevent SFW movie mismatches (e.g. adult title matching an unrelated mainstream film).

### Added
- **HLS Direct Fallback**: Player now exposes a `data-direct-fallback-url` attribute; if HLS fails within timeout, player falls back to direct MP4 without a visible error.
- **Vault Speed/ETA on Vault Page**: Vault progress SSE now computes and exposes `speed_bytes`, `remaining_bytes`, and `eta_seconds` for the vault page live display.
- **Seeder Vault Cache Drop**: Added `Drop(hash)` to the torrent-web-seeder vault client to immediately invalidate stale 60-second HEAD/GET results after vault deletion.
- **Microservice Server Packages**: Added standalone server entry points for `content-prober`, `magnet2torrent`, and `torrent-web-seeder` (including full seeder services: mmap, piece LRU, vault, stats, web seeder).
- **`example.env`**: Reference configuration template documenting all environment variables for new developer onboarding.
- **Adult Metadata Sidecar**: Python sidecar routes adult torrent lookups to StashDB/ThePornDB instead of TMDB; includes TMDB `imdb_id` index migration (migration 54).

### Changed
- **Status SSE Architecture**: Separated passive cache polling (`done=true` export check, no seeder connection) from active stats SSE (`active=1`), triggered only after explicit user action like Stream or Store.
- **Vault Worker**: Improved vault worker lifecycle, error handling, and resource claiming logic.
- **Run Scripts**: Synced `run_dev.sh` and `run_dev_skip.sh` with updated port maps and environment variable loading.
- **`.gitignore`**: Anchored top-level paths, added `custom.env`, `badger-data/`, and OS/editor noise rules.
- **Status Badge Labels**: Changed "(N peers)" label to "N seeds" in all status badge templates.

---

## [2.1.0] - Streaming & Proxy Optimization

### Added
- **Global Transcoding Toggle**: Added `FORCE_DIRECT_PLAY` (.env) to completely bypass transcoding for weak VPS instances.
- **Download Filename Injection**: Fixed "bad names" (UUIDs) by force-injecting `Content-Disposition` in `torrent-http-proxy`.
- **HLS Timeout Safety**: Implemented a universal 30-second buffering timeout that falls back to direct play if transcoding hangs.
- **Resolution-Based Auto-Fallback**: Automatically force-redirects 4K and high-bitrate 1080p content to direct play to preserve CPU.

# CHANGELOG

## [Unreleased] - 2026-05-16

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
- **Docker Socket Helper**: Added `find_docker.sh` to detect the active Docker socket on macOS/Linux host setups.
- **Ignore Rules**: Ignored local Badger runtime data and editor swap/temp files.

### Changed
- **Development Docs**: Consolidated the active development guide, audit, changelog, and task list into `DEVELOPMENT.md`, `AUDIT.md`, `CHANGELOG.md`, and `TODO.md`.
- **Local Tooling**: Made `localize_repos.py` and `master_fix.py` resolve paths relative to the repo instead of a hardcoded Windows path.

### Removed
- **Superseded Notes**: Removed legacy planning and handover documents now covered by the consolidated docs (`TO-DO.md`, `VPS_HANDOVER.md`, `custom_webtor_plan.md`, `octor_masterplan.md`).

### **v2.1.0 - Streaming & Proxy Optimization**
- **Global Transcoding Toggle**: Added `FORCE_DIRECT_PLAY` (.env) to completely bypass transcoding for weak VPS instances.
- **Download Filename Injection**: Fixed "bad names" (UUIDs) by force-injecting `Content-Disposition` in `torrent-http-proxy`.
- **HLS Timeout Safety**: Implemented a universal 30-second buffering timeout that falls back to direct play if transcoding hangs.
- **Resolution-Based Auto-Fallback**: Automatically force-redirects 4K and high-bitrate 1080p content to direct play to preserve CPU.

# TODO

## High Priority
- [x] Fix HLS streaming 404/502/504 issues
- [x] Standardize microservice port mapping
- [x] Resolve proxy header propagation (X-Source-Url)
- [x] Fix SRT2VTT compilation error
- [x] Bridge Legacy HLS paths in content-transcoder
- [x] Replace incompatible libfdk_aac encoder
- [x] Implement URL downloading for Sintel (fixed filename issues)
- [x] Verify Magnet magnetization reliability (stable)
- [x] Fix generated proto package/import mismatches
- [x] Update LazyMap embedding for current shared helper API

## Infrastructure
- [x] Correct Caddy reverse proxy logic
- [x] Isolate GRPC/Probe/Pprof ports
- [x] Add Postgres init SQL for local infra bootstrap
- [x] Ignore Badger runtime data and editor temp files
- [x] Consolidate legacy planning/handover notes into active docs
- [ ] Add health-check monitoring to run_dev.sh
- [ ] Replace broad local `go.mod` replace churn with a cleaner `go.work`-based workflow where possible.
- [TIP] Run ./run_dev.sh in an external terminal to avoid IDE crashes due to log flooding.

## 🟡 Features & Stabilization
- [ ] **Real-time Metrics**: Accelerate stats refresh rate to ~200ms.
- [ ] **Rclone Integration**: Stabilize the merged cloud storage layer.
- [ ] **Turbo Vaulting**: Implement non-sequential piece selection for faster vaulting (as toggle)
- [ ] **Metadata Bridge**: Implement the Cinemeta/Stremio enrichment bridge (fallback for TMDB).
- [ ] **ALL metadata to work as expected** hardening metadata behavior and stuff for tmbd/sidecar-ombd proxy.(work in harmoney and work as intended)
- [ ] **Restructure WebDAV folder layout/expose vault**: Replace current 4-folder structure (`all`, `movies`, `tv series`, `torrents`) with a new 3-root layout — `torrents/` (unchanged), `movies/` (with `library/` and `vault/` subfolders), and `tv series/` (with `library/` and `vault/` subfolders) — where `library/` contains torrents added to library and `vault/` contains completed/vaulted torrents (a torrent can appear in both if it qualifies for both).


## ⚪ Long Term
- [ ] **Monolithic Freeze**: Create a single ARM64-optimized Docker image for one-click deployment.

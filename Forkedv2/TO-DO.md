# 📋 Octor Stabilization TO-DO

## 🔴 Critical & Bugs (Priority)
- [x] **Fix Stuck Links**: `torrent-http-proxy` integrated into `run_dev.sh`. (Verify in browser)
- [x] **Fix Player Initialization**: Resolved by proxy-first architecture. (Verify in browser)
- [x] **Route Speedtest Correctly**: `/speedtest` now routed through `torrent-http-proxy`.

## 🟡 Self-Hosted De-monetization
- [x] **Remove "Frozen" State**: `freezePeriod` set to `0` in `web-ui`.
- [x] **Rename "Vault Pledge"**: Rebranded to "Storage" across UI and `en.json`.
- [x] **Clean Profile UI**: Removed monetization banners and granted "Pro" access by default.
- [ ] **Footer Alignment**: Fix the TOOLS vs RESOURCES columns alignment.

## 🟢 Features & Storage
- [x] **True Storage Metrics**: Implemented dynamic disk-space based `Used / Total` calculation.
- [ ] **Vault -> Library Auto-Add**: Ensure torrents added to vault appear in Library immediately without reload.
- [ ] **Real-time Metrics**: Accelerate stats refresh rate to ~200ms for speedometers.
- [ ] **Rclone Integration**: Merged cloud storage layer.

## ⚪ Documentation
- [ ] **Update README.md**: Reflect new local-first architecture and deployment steps.
- [ ] **Update Masterplan**: Sync status with current progress.

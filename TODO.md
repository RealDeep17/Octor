# Octor Architectural Master Plan & 1TB Vaulting Checklist
**Target Specification:** Flawlessly vault a single 1TB torrent under tight VPS constraints (16GB RAM max, 100GB SSD max) with zero disk leaks, zero memory leaks, and seamless real-time UI progress updates.

> [!IMPORTANT]
> **CRITICAL RULE FOR ALL FUTURE AGENTS:** Do not alter the zero-buffer direct-stream S3 Gateway architecture (`s3-gateway/main.go`), do not reintroduce intermediate `.uploads` disk buffering, do not reintroduce synchronous HTTP GET queries in `stat.go`, and do not overwrite Vault's true stored byte progress in `status.go`.

---

## 🚀 1. The Zero-Buffer Direct-Stream Architecture
- **Pipeline Flow**: Incoming BitTorrent pieces -> `torrent-web-seeder` (RAM/SSD LRU cache) -> Vault Worker HTTP Range Stream -> 50MB RAM Buffer -> `s3-gateway` -> `io.Copy` Direct Stream -> `rclone` Union Mount (`/srv/octor/infra-data/drive-mount`) -> HTTPS Network Stream -> Google Drive.
- **Rclone Configuration**: Must maintain `--vfs-cache-mode off`, `--buffer-size 128M`, `--drive-chunk-size 256M` (buffer and chuck size can be adjusted to improve performance if they are bottlenecks). Rclone buffers chunks in RAM and applies HTTP backpressure across the entire pipeline when the network upload is saturated. Zero intermediate SSD part files are created.

---

## 📋 2. Execution & Verification Roadmap

### Phase 1: Small-Torrent LRU Eviction Verification (5-10GB)
- [x] Configure `custom.env` with tight test limits (`RAM_CACHE_SIZE=2G`, `PER_TORRENT_CACHE_BUDGET=1500M`).
- [x] Recompile and restart all daemons (`octor-seeder-cache`, `octor-torrent-web-seeder`, `octor-web-ui`, `octor-s3-gateway`).
- [x] Restore `stat.go` to pristine high-performance state (removed synchronous HTTP GET bottleneck).
- [x] Update `resolveStatus` in `status.go` to correctly prioritize Vault's stored byte progress (`apiResource.StoredSize`) during active vaulting.
- [ ] **Live Test Execution**: Initiate a 5-10GB test torrent via Web UI / Vault API.
- [ ] **Verify Storage Cap**: Confirm seeder storage strictly remains under `1.5GB` via LRU piece eviction and `FALLOC_FL_PUNCH_HOLE`.
- [ ] **Verify UI Continuity**: Confirm Web UI progress bar seamlessly advances past 1.5GB to 100%.

### Phase 2: Production Scaling (1TB File Vaulting)
- [ ] Update `custom.env` to production limits (`RAM_CACHE_SIZE=16G`, `PER_TORRENT_CACHE_BUDGET=15G` for RAM mode; or `90G` for SSD mode).
- [ ] Initiate a 1TB torrent vaulting job.
- [ ] Verify stable memory usage (<16GB) and zero local disk exhaustion over prolonged transfer.

### Phase 3: Monolithic Locking
- [ ] Stage and commit all validated changes on branch `expermintal-R&D`.

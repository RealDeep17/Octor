# Octor Roadmap & TODO

**Core Vision:** Perfect a microservice-based media streaming architecture capable of direct, high-performance HEVC/H.265 playback with zero CPU transcoding overhead, while simultaneously supporting zero-disk-leak sequential vaulting of massive files directly to Google Drive.

---

## 🚀 Future Features & Ideas

### Idea 1: Top Menu Bar & Profile Refactoring
1. **Language Selector:** Relocate language selection to the Profile Page as a compact dropdown.
2. **Unified Minimalist Navbar:** Replace center navigation with a single bar for Magnet/Hash input and Torrent uploading.
3. **In-Place Processing:** Implement a progress overlay within the bar for magnetization and enrichment.
4. **Responsive Design:** Ensure navbar collapses gracefully to icons on small screens.

### Idea 2: The "Discovery Engine" (Search & Monitor Layer)
1. **Prowlarr Integration:** Centralize torrent trackers (Indexers).
2. **Autobrr (The Scout):** Configure 24/7 monitoring filters for automated grabbing.
3. **The *Arr Stack (Sonarr, Radarr, Whisparr):** Full integration for automated TV, Movie, and Adult content management.
4. **Comet Bridge:** Translate search results into Stremio-compatible formats for Octor.
5. **Secure Webhook Ingest:** Create an API for the stack to push magnets directly into user libraries using an `AUTOMATION_API_KEY`.

---

## 🛠️ Infrastructure & DevOps (Pending)

### Full Docker Containerization (Hybrid Approach)
*   **Goal:** Move all 15+ microservices into a single `docker-compose.yml` for unified management.
*   **Strategy:** Keep the **Storage Layer** (Rclone mount and Seeder Cache) on the host for performance/stability, and pass paths into containers via volumes.
*   **Crucial Step:** **Audit and Update all Dockerfiles.** Most are inherited from upstream and are currently stale/broken due to local architectural changes (e.g., Go workspace setup, `common-services` local linking).
*   **Networking:** Switch service communication from `localhost` to internal Docker service names.

---

## ✅ Completed Tasks

- [x] **Dynamic Media Cards (Horizontal & Vertical):** Implemented enrichment logic for both poster orientations and a responsive grid system that supports mixed-aspect-ratio cards with manual toggle support.
- [x] **Surgical Benchmark Cleanup:** Fixed `run.sh` to prevent aggressive `rclone purge` of the entire remote bucket.
- [x] **Storage Architecture Consolidation:** Removed legacy `drive1-index` references and consolidated all storage (Vault, Torrent-Store, Recovery, Media) onto the `ALPHA_UNION` mount.
- [x] **S3 Gateway Routing Fix:** Corrected the `storage` bucket mapping to point to the unified `recovery/` folder.
- [x] **Env File Safety Warnings:** Added critical warnings to `example.env` and `custom.env` regarding the `S3_GATEWAY_HUMAN_READABLE` toggle.
- [x] **Env File Reorganization:** Prioritized user-input settings (Keys, Domains, DBs) at the top of the environment files for better setup experience.
- [x] **Vault Selective Vaulting:** Implemented the ability for users to select specific files from a torrent when pledging to the vault.
- [x] **Selective Vaulting Cleanup:** Integrated the vault worker to honor file selection and prune unselected files from storage.

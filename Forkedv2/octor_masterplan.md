# 🏆 Octor Masterplan: Local-First Self-Hosted Webtor

This masterplan details the path to restoring and evolving your custom Webtor instance (`octor.duckdns.org`). We are moving to a **100% local build pipeline** to prevent regressions and preserve your "hotpatches."

## 📌 Objectives
1.  **Stop Regressions**: Build exclusively from local `/octor` folders, never fetching from `webtor.io` GitHub during build.
2.  **Phase 1 (Route 3)**: Establish a Hybrid Development environment (Infra in Docker, Services on Host) for rapid iteration and bug fixing.
3.  **Phase 2 (Route 1)**: Freeze the build into a monolithic, ARM64-optimized Docker image.
4.  **Restore Hotpatches**: Use `REFERENCE-ONLY` to identify and re-apply lost modifications.

---

## 🛠 Phase 1: Route 3 - Hybrid Development Setup
**Goal**: Get the system running locally so we can test the NSFW sidecar and AI features.

### 1. Infrastructure (Docker)
Create a `docker-compose.infra.yml` to run the required backends:
*   **PostgreSQL**: Core data storage.
*   **Redis**: Caching and job queues.
*   **NATS**: Internal event bus.
*   **SuperTokens**: Auth (configured to `https://try.supertokens.com` or local).

### 2. Service Execution (Host)
Run services directly on the host using your local Go environment.
*   **Order of operations**:
    1.  Start `nsfw-sidecar` (Python).
    2.  Start `torrent-store` (Go).
    3.  Start `torrent-web-seeder` (Go).
    4.  Start `magnet2torrent` (Go).
    5.  Start `torrent-http-proxy` (Go) - **NEW: Critical for streams/downloads/speedtest**.
    6.  Start `rest-api` (Go).
    7.  Start `web-ui` (Go).
    8.  Start `vault` (Go).

---

## 🏗 Phase 2: Route 1 - Monolithic Freeze
**Goal**: Create a single, portable Docker image for production.

---

## 🔞 Feature Integration: NSFW & AI
*   **Sidecar**: Point `web-ui` to your local `sidecar` folder via `OMDB_API_HOST`.
*   **AI Recommendations**: Ensure `GEMINI_API_KEY` and `AI_RECOMMENDATIONS_ENABLED` are set in the environment.

---

## 🔍 Recovery Plan: Re-applying Hotpatches
Systematically compare the `octor` repositories against the `REFERENCE-ONLY` code.

---

## 💎 Premium Enhancements & Refinements
**Goal**: Transition from a generic Webtor fork to a premium, polished personal media suite.

### 1. UI & Aesthetics
*   [x] **Donate Removal**: Strip the "Donate" button from the header.
*   [x] **Tier Clean-up**: Hide "Free Tier," "Unlimited Bandwidth," and "Ads" labels.
*   [x] **Terminology Shift**: Rename "Vault Pledge" to "Store in Vault" or "Vault Storage".
*   [x] **Footer Reconstruction**: Align columns (TOOLS vs RESOURCES) and fix links.
*   [x] **Real-time UX**: Metrics refresh rate ~200ms.

### 2. Vault & Storage Strategy
*   [x] **No Freeze Period**: Set `freezePeriod` to 0 for instant vault removals.
*   [ ] **Vault -> Library Auto-Sync**: 
    *   Automatically add vaulted resources to Library.
    *   Zero-reload UI updates.
*   [x] **True Storage Metrics**:
    *   `used / (free + used)` calculation using `statfs`.
    *   "Turbo" Vaulting: Non-Sequential piece selection.

### 3. Hybrid Cloud Storage (Phase 3)
*   [ ] **Rclone Integration**: 
    *   Mount Google Drive/ onedrive accounts into a single mount point.(can mount more in future) - (not part of octor code, script to make... web search on web/github for ref)
    *   **RAM-First Seeking**: Utilize VPS RAM as a VFS cache (2GB+ per stream) to enable instant 4K seeking.
    *   Bridge local MinIO storage with the Rclone mount for massive capacity.

### 4. Enrichment Stability
*   [ ] **TMDB Internal Proxy**: Implement a Stremio/Cinemeta fallback in the Enricher to provide "nice posters" and metadata without requiring personal TMDB API keys.
*   [ ] **Sidecar Aggression**: Improve the NSFW sidecar matcher to handle messy filenames more effectively.

---

## 🚀 Execution Steps (Current)
1.  [x] **Infra Start**: Integrated MinIO and Caddy SSL.
2.  [x] **UI Refinement**: Remove donate button.
3.  [x] **Fix Routing**: Anchored to `octor.duckdns.org` and fixed `localhost` admin bypass.
4.  [x] **Vault Upgrade**: GB/TB metrics and "Vault" rebranding complete.
5.  [ ] **Metadata Fix**: Implement the Cinemeta/Stremio enrichment bridge. (PENDING)



## Idea / suggestion / feedback
1. turbo vaulting and auto add to library to be toggeable in user profile. (to implement but ask me first bwefore move these idea to to-do)
2. webdav exposes 4 folders. tv series/movies/torrents/alls.. but can we add another one? for Vault. so if someone mounts via webdav.. they can directly access.. this helps differecate between bookmarked torrents(movies/video folder) and downloaded torrents (vault folder)
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

### Idea 3: Adult Content Discovery Mode (Admin-Only)

> **Polished implementation prompt (for future execution):**
>
> *Implement an adult content discovery section in the Discover UI, gated to admin accounts only. It mirrors the existing Cinemeta-powered poster-grid UX but is backed by **TPDB (The Porn DB)** and **StashDB** instead of Cinemeta, and optionally **JAV** metadata sources for Japanese adult video.*
>
> **Catalogue tabs to expose (equivalent to Cinemeta's popular/new/featured):**
>
> - **Trending** → maps to TPDB `trending` / StashDB `recent_scenes` (equivalent of Cinemeta "popular")
> - **New Releases** → maps to TPDB `recently_released` / StashDB `new_releases` (equivalent of Cinemeta "new")
> - **Top Rated** → maps to TPDB `top_rated` / StashDB `top_rated` (equivalent of Cinemeta "featured")
> - **JAV** → separate tab, sourced from a dedicated JAV metadata API (e.g., javlibrary or javdb)
>
> **Poster caching:** All four category feeds must pre-fetch and cache posters into the same image proxy/cache pipeline that the existing Cinemeta catalog uses — no direct client-side fetches to TPDB/StashDB domains.
>
> **Stream flow (identical to normal discover):** Clicking a poster opens the standard StreamModal. The modal queries Stremio stream addons (primarily Comet) for torrents matching the scene/movie ID, then sends the selected infoHash to the Transmission/download queue — business as usual.
>
> **Access control:** The adult section is **only visible and accessible to users with the `admin` role**. All API endpoints under `/discover/adult/*` must enforce the admin role server-side. No client-side-only gating.
>
> **Routing:** Add an `adult` type alongside `movie`/`series` in the existing type-tab system, shown only for admins. Reuse all existing reducer, ItemGrid, and StreamModal machinery — only the metadata source and access guard differ.

---

## 🛠️ Infrastructure & DevOps (Pending)

### Full Docker Containerization (Hybrid Approach)

- **Goal:** Move all 15+ microservices into a single `docker-compose.yml` for unified management.
- **Strategy:** Keep the **Storage Layer** (Rclone mount and Seeder Cache) on the host for performance/stability, and pass paths into containers via volumes.
- **Crucial Step:** **Audit and Update all Dockerfiles.** Most are inherited from upstream and are currently stale/broken due to local architectural changes (e.g., Go workspace setup, `common-services` local linking).
- **Networking:** Switch service communication from `localhost` to internal Docker service names.


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
- [x] **Prowlarr Tracker — Persistent Panel Fix:** Removed tracker from tab-switching logic. "Trackers" button is now pinned far-right in the search tab bar and toggles a results panel **below** the Cinemeta poster grid — Cinemeta results are never hidden. Panel persists until search is cleared. Backed by `showTrackerSection` / `TOGGLE_TRACKER_SECTION` in `discoverReducer.js`.
- [x] **Accurate Season & Pack Labeling:** Unified `Season` and `Pack` labels under a single `seasonPack` group for clean logical `OR` filtering. Promoted both as high-value quick pills next to search and sorting bar in both Stream Modal and Direct Search App.
- [x] **Non-Blocking Poster & Still Caching:** Replaced slow synchronous backend resizing on cache misses with immediate `302` client redirects to raw CDN URLs, while asynchronously downloading, resizing, and caching images to S3 in a background goroutine. Removes 10-second delays across all environments.
- [x] **Direct Search Publish Time Exposure:** Exposed relative upload/publish ages (e.g. `3d ago`, `2mo ago`) dynamically as `📅 {ageStr}` in Direct Search result rows right beside size and indexer info tags.

---

## 🔲 Pending: Adult Content Discovery (Idea 3)

- [ ] **TPDB/StashDB/JAV API clients** — Server-side Go clients for The Porn DB, StashDB, and a JAV metadata source (javlibrary/javdb).
- [ ] **Adult catalog endpoints** — `/discover/adult/trending`, `/discover/adult/new`, `/discover/adult/top-rated`, `/discover/adult/jav` — enforced admin-role gate server-side.
- [ ] **Poster cache pipeline** — Route TPDB/StashDB/JAV poster URLs through the existing image proxy/cache so clients never hit external domains directly.
- [ ] **Admin-only type tab in Discover UI** — Add `adult` as a type tab, rendered only for admin-role users; reuse existing `TypeTabs` component.
- [ ] **Reducer/catalog wiring** — Wire the four adult catalogs into `buildCatalogs` / `discoverReducer` machinery using the same `baseUrl` + `id` pattern.
- [ ] **JAV sub-tab** — Separate tab within the adult section for JAV content with its own metadata source.
- [ ] **Stream flow validation** — Confirm Comet (Stremio addon) resolves adult scene/movie IDs to torrents correctly end-to-end through `StreamModal`.

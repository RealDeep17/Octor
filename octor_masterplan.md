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
    2.  Start `rest-api` (Go).
    3.  Start `web-ui` (Go).
    4.  Start supporting services (`torrent-web-seeder`, `content-transcoder`) as needed.

---

## 🏗 Phase 2: Route 1 - Monolithic Freeze
**Goal**: Create a single, portable Docker image for production.

### 1. The Local-Only Dockerfile
We will modify the `Dockerfile` to use **Local Context** only.
*   **Change**: Replace all `RUN git clone ...` with `COPY ./[folder] /app/src/[folder]`.
*   **Optimization**: Ensure the build is ARM64 compatible (as seen in your reference).

### 2. The Build Script
A `rebuild.sh` script that:
1.  Performs `go mod tidy` in all local folders.
2.  Builds the monolithic image using the local folders as context.
3.  Tags the image as `webtor-custom:latest`.

---

## 🔞 Feature Integration: NSFW & AI
*   **Sidecar**: Point `web-ui` to your local `sidecar` folder via `OMDB_API_HOST`.
*   **AI Recommendations**: Ensure `GEMINI_API_KEY` and `AI_RECOMMENDATIONS_ENABLED` are set in the environment.
*   **Persistence**: Use `BADGER_PATH` to ensure torrent metadata survives restarts (365-day expiry as per your `custom.env`).

---

## 🔍 Recovery Plan: Re-applying Hotpatches
We will systematically compare the `octor` repositories against the `REFERENCE-ONLY` code to find:
1.  **Web-UI Mods**: Any custom CSS/HTML for the NSFW catalog.
2.  **API Tweaks**: Logic changes in `rest-api` or `web-ui` that bypass auth or enrich metadata.
3.  **Config Fixes**: Specific ports or URLs that were "just working" before.

---

## 🚀 Execution Steps (Immediate)
1.  [ ] **Infra Start**: Create and run `docker-compose.infra.yml`.
2.  [ ] **Environment Sync**: Create a `.env` file in the root using values from `REFERENCE-ONLY/custom.env`.
3.  [ ] **Service Check**: Verify `web-ui` can connect to the local infra.
4.  [ ] **Patch Audit**: Compare `web-ui` templates with the reference.

# Octor Roadmap & TODO List

This document tracks upcoming architectural cleanups, migrations, and performance optimizations.

## 🚀 Priority Refactoring

### 1. Refactor Discover Handler (`adult.go`)
Deconstruct the massive [adult.go](file:///Users/deepanshukumar/Downloads/Octor/web-ui/handlers/discover/adult.go) file under the `discover` package into modular, testable, and maintainable files:

- [ ] **`routes.go` (Route Registration & Middleware):**
  - Register adult endpoints, route groups, and auth checks.
- [ ] **`stashdb.go` (StashDB Client & Query Orchestrator):**
  - GraphQL clients, dynamic studio resolver job (`resolveStudiosJob`), and scene search query formatting.
- [ ] **`javguru.go` (Jav.guru WP-JSON Client):**
  - HTML parsing, REST API requests, and JAV-specific studio normalization (`getJavStudioName`, `cleanOriginalImageURL`).
- [ ] **`posters.go` (Poster Management & Local Image Processing):**
  - Poster caching, resizing, downscaling (`handlePoster`), and catalog warmer/pruning disk jobs.
- [ ] **`directsearch.go` (Direct Search Stream Proxy & Torrent Provider):**
  - Stream proxy logic (`handleDirectSearchProxy`), torrent stream mappings, and Prowlarr indexer queries.

---

## ⚡ Runtime Migrations & Optimizations

### 2. Rewrite Python Sidecar in Go
Port the Python-based REST API service (`sidecar/main.py`) to a Go microservice (using `gin-gonic/gin` or standard `net/http`):

- [ ] **Size & Runtime Optimization:**
  - Remove Python 3, pip, and venv from the Docker monolith to save **~200–250 MB** of container image size.
  - Delete `sidecar/venv/` on the host, saving **~100 MB** of host filesystem space in hybrid mode.
- [ ] **Resource Optimization:**
  - Bring idle memory down from ~80 MB (Python/Uvicorn) to **~5 MB** (Go).
  - Enable instant service startup (milliseconds vs seconds).
- [ ] **Integration & Dependencies:**
  - Replace `rapidfuzz` and `Unidecode` with native Go fuzzy-matching and unidecode packages.
  - Eliminate the Python-to-Go JSON IPC boundary for faster queries.

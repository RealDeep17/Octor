# Octor Roadmap & TODO List

This document tracks upcoming architectural cleanups, migrations, and performance optimizations.

## 🚀 Priority Refactoring

## ⚡ Runtime Migrations & Optimizations

### 1. Rewrite Python Sidecar in Go
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

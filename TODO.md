# Octor Roadmap & TODO List

This document tracks upcoming architectural cleanups, migrations, and performance optimizations.

## 🚀 Priority Refactoring

- [ ] **Monolith Docker Image Size Reduction (Target: ~200-350 MB total):**
  - [ ] **Unified Go Multi-Call Binary (BusyBox Style):**
    - Consolidate 18 statically linked Go service binaries into a single, unified dispatcher binary (`octor`).
    - *How it works:* Inspects `os.Args[0]` or `os.Args[1]` to launch the respective microservice entry point. Since common packages (gin, gorm, logrus, torrent library) and Go runtime are linked only once, size drops from **651 MB → ~50 MB**.
    - *Required modifications:* Update `run.sh` (ghost process names, execution paths), `supervisord.conf`, and systemd service templates to execute services via `octor <service>` (or create symlinks inside the container).
  - [ ] **Static FFmpeg/FFprobe Build:**
    - Replace the full `apt-get install -y ffmpeg` package inside the Dockerfile with pre-compiled static FFmpeg binaries (copied from a static build helper stage like `mwader/static-ffmpeg`).
    - Since we only use FFmpeg for basic video probing and thumbnails, this drops all graphical/X11/audio library dependencies in `/usr/lib/x86_64-linux-gnu` (saving **~300-400 MB**).
  - [ ] **Go Rewrite of AI Proxy & Removing Node.js:**
    - Port the single JavaScript file `ai-proxy/proxy.js` to Go (compiled into the unified binary) and remove Node.js runtime from the production container completely (saving **~150 MB**).
  - [ ] **Switch Base OS to `debian-slim`:**
    - Use `debian:12-slim` (size **~74 MB**) instead of `ubuntu:24.04` (size **~75 MB**).
    - *Compatibility:* Highly compatible with Ubuntu (both use GNU C Library `glibc` dynamically). Alpine uses `musl` which breaks CGO compiled Go binaries; `debian-slim` is 100% binary compatible with Ubuntu and completely safe.

## ⚡ Runtime Migrations & Optimizations

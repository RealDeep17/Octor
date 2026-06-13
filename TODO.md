# Octor Roadmap & TODO List

This document tracks upcoming architectural cleanups, migrations, and performance optimizations.

## 🚀 Priority Refactoring

- [x] **Monolith Docker Image Size Reduction (Target: ~200-350 MB total):**
  - [x] **Unified Go Multi-Call Binary (BusyBox Style):**
    - Consolidate 19 statically linked Go service binaries into a single, unified dispatcher binary (`octor`).
    - *How it works:* Inspects `os.Args[0]` or `os.Args[1]` to launch the respective microservice entry point. Since common packages (gin, gorm, logrus, torrent library) and Go runtime are linked only once, size drops from **651 MB → ~50 MB**.
    - *Required modifications:* Update `run.sh` (ghost process names, execution paths), `supervisord.conf`, and systemd service templates to execute services via `octor <service>` (or create symlinks inside the container).
  - [x] **Static FFmpeg/FFprobe Build:**
    - Replace the full `apt-get install -y ffmpeg` package inside the Dockerfile with pre-compiled static FFmpeg binaries (copied from a static build helper stage like `mwader/static-ffmpeg`).
    - Since we only use FFmpeg for basic video probing and thumbnails, this drops all graphical/X11/audio library dependencies in `/usr/lib/x86_64-linux-gnu` (saving **~300-400 MB**).
  - [x] **Go Rewrite of AI Proxy & Removing Node.js:**
    - Port the single JavaScript file `ai-proxy/proxy.js` to Go (compiled into the unified binary) and remove Node.js runtime from the production container completely (saving **~150 MB**).


## ⚡ Runtime Migrations & Optimizations

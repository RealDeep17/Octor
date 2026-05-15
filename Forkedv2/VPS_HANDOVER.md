# Octor VPS Handover Notes

Welcome to the OCI-A1 (Ubuntu ARM64) environment! This document serves as a brain-dump of the current state of the server, architecture, and recent changes to help you get up to speed immediately.

## 📁 Critical Paths
- **Octor Project Root**: `/srv/Octor`
- **Autolycus Root**: `/srv/autolycus`
- **Infrastructure Data**: `/srv/Octor/infra-data` (Moved here to clean up the `server` folder).
- **Global Caddy Config**: `/etc/caddy/Caddyfile`

## 🛠 Toolchain & Environment
- **OS**: Ubuntu 24.04 LTS (Noble) - ARM64
- **Go**: `1.26.3` (Installed in `/usr/local/go/bin`, linked to `/usr/bin/go`).
- **Node.js**: `v24.15.0` (Latest, installed via NodeSource).
- **Proxy**: Systemwide `caddy` service natively managing ACME/HTTPS.

## 🌐 Routing & Port Map (Caddy)
Caddy routes incoming traffic from DuckDNS directly to local ports. 
- **Octor Web UI**: `https://octor.duckdns.org` ➔ `localhost:8081`
- **Octor Streaming**: `https://octor.duckdns.org/seeder/*` ➔ `localhost:50052`
- **OmniRoute**: `https://orgate.duckdns.org` ➔ `localhost:20128`
- **Autolycus/Seedio**: `https://seedio.duckdns.org` ➔ `localhost:8089`

## ⚙️ Process Management
1. **Infrastructure (Docker)**: Postgres, Redis, Minio, and NATS are managed via `docker-compose.infra.yml`.
   - *Note*: Redis was shifted to `6380` on the host to avoid colliding with Autolycus.
2. **Octor Services (Native)**: Currently launched via `./run_dev.sh`. 
   - *Important*: Because it is a bash script, it must be run with `nohup` (e.g., `nohup sudo bash ./run_dev.sh > nohup.out 2>&1 &`) to survive session disconnects. **Migrating Octor to proper `systemd` services should be a high priority.**

## ✅ Recent Code Fixes to Note
1. **Streaming Fix**: Modified `web_seeder.go` to dynamically strip `Content-Disposition: attachment` headers during video streaming so browsers play natively instead of forcing file downloads.
2. **Speedtest Calibration**: Throttled `speedtest.go` to ~100Mbps so it measures actual network throughput instead of raw memory speeds.

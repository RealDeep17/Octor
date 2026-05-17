# Octor Infrastructure Audit (Forkedv2)

## 📋 Summary
This audit identifies technical debt, port conflicts, and legacy SaaS remnants in the Octor self-hosted codebase.

## 🛠 Critical Findings

### 1. Port Mapping Conflicts
Microservices currently rely on `common-services` defaults which collide when running natively on the host.
- **Recommendation**: Standardize all services to use the 5-digit port mapping (8xxx/50xxx/51xxx/52xxx/53xxx).
- **Status**: ✅ STABLE. `run_dev.sh` and `run_dev_skip.sh` are synchronized and use the 5-digit map consistently. Protobuf conflicts are suppressed via env vars.

### 2. Legacy SaaS Logic (SaaS Leftovers)
The codebase is still entangled with monetization and multi-tier features intended for a hosted service.
- **`claims-provider`**: `models/claims.go` still defines `TierID`, `VaultPoints`, and `NoAds` flags. The `public.get_member_claims_by_email` function likely still logic-gates these.
- **`rest-api`**: `url_builder.go` checks for `premiumDomain` and `role != "free"`.
- **`web-ui`**: Sidecar enrichment is toggled by `SidecarEnrichment` in `user_stremio_settings`.
- **Recommendation**: Scrub these fields or default them to "unlimited" in the database layer. All self-hosted users should be `premium` by default.

### 3. Hardcoded Infrastructure References
- **Domain**: `octor.duckdns.org` is hardcoded in `Caddyfile`, `run_dev.sh`, and several Go files.
- **Upstream Modules**: `go.mod` files were pointing to `github.com/webtor-io/` which fetched remote code.
- **Status**: `localize_repos.py` and `master_fix.py` now resolve paths from the repo location instead of a hardcoded Windows path. Local module `replace` entries still need a cleaner long-term workflow, preferably `go.work`.

### 4. JWT & Claims Fragmentation
`torrent-http-proxy` uses a custom `claims.go` implementation, while other services rely on `claims-provider`.
- **Recommendation**: Unify the JWT verification logic and use a shared claims model across all services.

### 5. Inconsistent Observability Flags
Some services (e.g., `rest-api`, `torrent-http-proxy`) have Prometheus metrics registration, while others (e.g., `abuse-store`, `url-store`) are missing them or have them partially implemented.
- **Recommendation**: Ensure every service calls `cs.RegisterPromFlags` and `cs.NewProm(c)`.

### 6. Kubernetes Reliance in Host Services
`torrent-http-proxy` and `rest-api` initialize K8s clients. While they have fallbacks for local mode, they still attempt to find kubeconfigs.
- **Recommendation**: Add a `--host-mode` flag to skip K8s initialization entirely and avoid "kubeconfig not found" warnings in logs.

## 🗂 File-Specific Issues

| File | Issue | Severity |
| :--- | :--- | :--- |
| `claims-provider/models/claims.go` | Contains `VaultPoints` and `Tiers`. | Medium |
| `rest-api/services/url_builder.go` | Hardcoded domain logic and role checks. | Medium |
| `abuse-store/serve.go` | Duplicate flag registration (lines 28, 32). | Low |
| `localize_repos.py` | (FIXED) Hardcoded Windows paths. | High |
| `master_fix.py` | (FIXED) Hardcoded Windows paths. | High |
| `torrent-store/badger_data/` | (FIXED) Runtime database directory is now ignored. | Low |
| `rest-api/.!*!*` | (FIXED) Editor temp files are now ignored. | Low |
| `content-transcoder/services/web.go` | (FIXED) Legacy HLS URLs are routed to the session handlers. | High |
| `*/proto/*.pb.go` | (FIXED) Generated package names no longer use invalid `__` package references. | High |

## 📅 Next Steps
1. Scrub SaaS logic from `claims-provider` and `rest-api` URL builder.
2. Implement native build pipeline (Makefile) for VPS deployment to avoid `go run` overhead.
3. Consolidate `Caddyfile` logic into a standard `/etc/caddy/Caddyfile` for Ubuntu 24.04.
4. Move local multi-module development from committed `replace` churn toward a `go.work` file.
5. Standardize observability across all services (some are still missing Prometheus endpoints).

## 🚀 VPS Migration Checklist (Ubuntu 24.04)
- [ ] Setup `/srv/octor` directory structure.
- [ ] Configure `custom.env` with production domain and S3 keys.
- [ ] Build ARM64 binaries for OCI-A1.
- [ ] Install native Caddy with DNS-01 (DuckDNS) support.
- [ ] Configure Systemd units for all 15+ microservices.

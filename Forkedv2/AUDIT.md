# Octor Infrastructure Audit (Forkedv2)

## 📋 Summary
This audit identifies technical debt, port conflicts, and legacy SaaS remnants in the Octor self-hosted codebase.

## 🛠 Critical Findings

### 1. Port Mapping Conflicts
Microservices currently rely on `common-services` defaults which collide when running natively on the host.
- **Recommendation**: Standardize all services to use the 5-digit port mapping (8xxx/50xxx/51xxx/52xxx/53xxx).
- **Status**: Partially fixed. `run_dev.sh` uses overrides and recent service wiring updates moved Magnet2Torrent and HLS routing toward the local port map. Remaining `go.mod` and default-port churn should be reviewed service by service.

### 2. Legacy SaaS Logic (SaaS Leftovers)
The codebase is still entangled with monetization and multi-tier features intended for a hosted service.
- **`claims-provider`**: Contains `Tier` and `VaultPoints` logic in `models/claims.go` and `services/store.go`.
- **`rest-api`**: `url_builder.go` checks for `premiumDomain` and different base domains based on roles.
- **`web-ui`**: UI still displays some tier-based info and restricts features.
- **Recommendation**: Scrub these fields and default all users to "premium/unrestricted" roles for the self-hosted version.

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
1. Scrub SaaS logic from `claims-provider`.
2. Move hardcoded domains to `.env`.
3. Standardize Prometheus flags across all services.
4. Move local multi-module development from committed `replace` churn toward a `go.work` file.

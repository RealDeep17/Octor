# Octor

Octor is a local development workspace for a Webtor-derived media streaming stack. The active application code lives under `Forkedv2/` and is split into Go microservices plus a Python sidecar. The stack accepts torrents or magnet links, resolves metadata, exposes a web UI, streams media through a torrent HTTP proxy, can transcode/probe content, and can store selected media long-term through the Vault service.

This repository also contains local runner scripts and Docker infrastructure for running the stack on a development machine. Runtime data, logs, local secrets, and generated caches are intentionally kept out of git.

## Repository Layout

| Path | Purpose |
|------|---------|
| `.env` | Local, ignored environment file loaded by `Forkedv2/run_dev.sh` and `Forkedv2/run_dev_skip.sh`. |
| `Forkedv2/` | Main application workspace. |
| `Forkedv2/run_dev.sh` | Starts local services after checking Docker infrastructure. |
| `Forkedv2/run_dev_skip.sh` | Starts local services while skipping infrastructure checks. |
| `Forkedv2/docker-compose.infra.yml` | Local Postgres, Redis, NATS, and MinIO infrastructure. |
| `Forkedv2/web-ui/` | Web interface, library, discover, vault UI, metadata enrichment, and user-facing routes. |
| `Forkedv2/rest-api/` | REST API for resource/torrent URL generation, metadata/resource mapping, export links, and cache status. |
| `Forkedv2/torrent-http-proxy/` | Streaming edge proxy that routes media/HLS/archive requests to internal services. |
| `Forkedv2/torrent-web-seeder/` | BitTorrent-backed HTTP/gRPC seeder. |
| `Forkedv2/torrent-web-seeder-cleaner/` | Cleaner for torrent-web-seeder media cache. |
| `Forkedv2/vault/` | Long-term S3/MinIO-backed storage worker and API. |
| `Forkedv2/content-transcoder/` | HLS/transcoding service. |
| `Forkedv2/content-prober/` | Content probing service used before/around playback. |
| `Forkedv2/torrent-store/` | Torrent metadata store. |
| `Forkedv2/magnet2torrent/` | Magnet-to-torrent metadata resolver. |
| `Forkedv2/video-info/` | Video/subtitle metadata service. |
| `Forkedv2/torrent-archiver/` | Archive/zip service for directory downloads. |
| `Forkedv2/abuse-store/`, `Forkedv2/claims-provider/`, `Forkedv2/url-store/` | Supporting stores/services used by the stack. |
| `Forkedv2/sidecar/` | Python OMDB/NSFW metadata sidecar. |
| `server/` | Local ignored runtime/infra data location. |
| `REFERENCE-ONLY/` | Reference material, not active runtime code. |

## Local Development

From the repository root:

```sh
cd Forkedv2
docker compose -f docker-compose.infra.yml up -d
./run_dev.sh
```

Use `./run_dev_skip.sh` when the infrastructure is already running and you want to skip the Docker container checks. Both runner scripts load `../.env`, derive `PG_*` variables from `POSTGRES_*`, and start services with the local port map.

Default local entry points used by the current scripts:

| Service | URL/Port |
|---------|----------|
| Caddy public entry | `http://localhost:8081` through `OCTOR_DOMAIN` |
| Web UI service | `http://localhost:8082` |
| REST API | `http://localhost:8080` |
| Python sidecar | `http://localhost:8000` |
| Vault | `http://localhost:8086` |
| Torrent HTTP proxy | `50052` |
| Torrent store | `50051` |
| Magnet2Torrent | `50053` |
| Torrent web seeder | `50054` |
| Content transcoder | `50055` |
| Video info | `50056` |
| Torrent archiver | `50057` |
| SRT2VTT | `50058` |
| Abuse store | `50059` |
| Claims provider | `50060` |
| URL store | `50061` |
| Content prober HTTP/gRPC | `50062` / `50063` |

## Runtime State

The current local behavior separates three kinds of storage state:

| State | Meaning |
|-------|---------|
| `idle` | Media is not currently cached or vaulting. |
| `caching` | A user action such as stream/download/save long-term has caused seeder activity for the media. |
| `cached` | Media is available from the seeder cache. |
| `vaulting` | Vault is transferring media into long-term S3/MinIO storage. |
| `vaulted` | Vault has a long-term stored copy. |

Passive page status checks should not start caching by themselves. Active user actions such as streaming, URL download, directory download, or saving long-term may activate seeder/proxy work.

## Environment Files

The local `.env` file is ignored by git because it can contain credentials. Do not commit real API keys, OAuth secrets, S3 keys, DuckDNS tokens, sudo passwords, or private endpoints.

The runner scripts currently parse `.env` with shell `export $(grep -v '^#' ../.env | xargs)`, so keep values simple: `KEY=value`, no unescaped spaces, and comments on their own lines.

Duration values used by Go services accept Go duration syntax such as `30s`, `5m`, `23h`, or `168h`.

## Recommended Local `.env` Shape

Use real secrets only in your local ignored `.env`.

```env
# Infrastructure used by run_dev.sh to derive PG_* values.
POSTGRES_USER=webtor
POSTGRES_PASSWORD=webtor
POSTGRES_DB=webtor
POSTGRES_HOST=127.0.0.1
POSTGRES_PORT=5433

REDIS_HOST=127.0.0.1
REDIS_PORT=6379
NATS_HOST=127.0.0.1
NATS_PORT=4222

# Public/local app URL.
OCTOR_DOMAIN=http://localhost:8081
DOMAIN=localhost:8081
EXTERNAL_URL=http://localhost:8081

# Core service endpoints for local runner.
REST_API_SERVICE_HOST=localhost
REST_API_SERVICE_PORT=8080
REST_API_SECURE=false

# Media cache lifetime and metadata/temp cache lifetimes.
CACHED_MEDIA_MAX_AGE=23h
CACHED_MEDIA_CLEAN_INTERVAL=5m
CACHED_METADATA_EXPIRE=1m
CACHED_TEMP_EXPIRE=30s
CACHE_INDEX_EXPIRE=23h

# Optional metadata/AI providers.
OMDB_API_KEY=
TMDB_API_KEY=
ANTHROPIC_API_KEY=
GEMINI_API_KEY=
STASHDB_API_KEY=
THEPORNDB_API_KEY=
SIDECAR_ENRICHMENT_ENABLED=true

# Optional deployment helpers used by the current Caddy/DuckDNS runner section.
DUCKDNS_TOKEN=
SUDO_PASSWORD=
```

## Cache Customization

| Variable | Default in code | What it controls |
|----------|-----------------|------------------|
| `CACHED_MEDIA_MAX_AGE` / `CLEANER_MAX_AGE` | `23h` | Maximum inactive age for torrent-web-seeder media cache directories before the cleaner deletes them. Set `0` to disable age cleanup. |
| `CACHED_MEDIA_CLEAN_INTERVAL` / `CLEANER_INTERVAL` | `5m` | How often the media cache cleaner runs. |
| `CLEANER_KEEP_FREE` | `25%` | Free-space threshold that triggers space-pressure cleanup. |
| `CLEANER_FREE` | `35%` | Free-space target after space-pressure cleanup. |
| `DATA_DIR` | system temp dir | Directory where torrent-web-seeder media cache is stored. The cleaner reads the same directory. |
| `CACHED_METADATA_EXPIRE` / `WEB_UI_API_CACHE_EXPIRE` | `1m` | Web UI temporary API caches for torrent/resource/list metadata. |
| `CACHED_METADATA_EXPIRE` / `REST_API_RESOURCE_CACHE_EXPIRE` | `10m` | REST API resource metadata cache lifetime. |
| `CACHED_TEMP_EXPIRE` / `REST_API_CACHE_STATUS_EXPIRE` | `30s` | REST API temporary cached-media status lookup lifetime. |
| `CACHE_INDEX_EXPIRE` | `12h` | Web UI cache-index metadata expiration in Postgres. For short-lived media caches, keep this no longer than `CACHED_MEDIA_MAX_AGE`. |

## Complete Environment Reference

This table is based on the current `EnvVar` declarations in the Go services, the Python sidecar, `docker-compose.infra.yml`, and the local runner scripts. Some variables are aliases for the same flag; aliases are shown with `/`.

### Local Runner And Infrastructure

| Variable | Used by | What it does |
|----------|---------|--------------|
| `POSTGRES_USER` | `.env`, Docker/local runner | Postgres username. `run_dev*.sh` maps this to `PG_USER`. |
| `POSTGRES_PASSWORD` | `.env`, Docker/local runner | Postgres password. `run_dev*.sh` maps this to `PG_PASSWORD`. |
| `POSTGRES_DB` | `.env`, Docker/local runner | Main Postgres database. `run_dev*.sh` maps this to `PG_DATABASE`. |
| `POSTGRES_HOST` | `.env`, local runner | Postgres host. `run_dev*.sh` maps this to `PG_HOST`. |
| `POSTGRES_PORT` | `.env`, local runner | Postgres port. `run_dev*.sh` maps this to `PG_PORT`. |
| `REDIS_HOST` | `.env` | Local Redis host value used as a convenience variable. Go services use `REDIS_MASTER_SERVICE_HOST` / `REDIS_SERVICE_HOST`. |
| `REDIS_PORT` | `.env` | Local Redis port value used as a convenience variable. Go services use `REDIS_MASTER_SERVICE_PORT` / `REDIS_SERVICE_PORT`. |
| `NATS_HOST` | `.env` | Local NATS host convenience variable. Go services use `NATS_SERVICE_HOST`. |
| `NATS_PORT` | `.env` | Local NATS port convenience variable. Go services use `NATS_SERVICE_PORT`. |
| `MINIO_ROOT_USER` | Docker compose | MinIO root user in `docker-compose.infra.yml`. |
| `MINIO_ROOT_PASSWORD` | Docker compose | MinIO root password in `docker-compose.infra.yml`. |
| `OCTOR_DOMAIN` | local runner | Public/local base URL passed to REST API export domain and Web UI domain. |
| `OCTOR_HOST` | local runner/Caddy | Hostname derived from `OCTOR_DOMAIN` for the Caddyfile. |
| `DOMAIN` | Web UI | Web UI domain setting. |
| `EXTERNAL_URL` | `.env` | Local external URL convention used by this workspace. |
| `DUCKDNS_TOKEN` | local runner/Caddy | DuckDNS token used by Caddy DNS and the updater loop. |
| `SUDO_PASSWORD` | local runner | Password piped to `sudo` for Caddy stop/run in the current scripts. |
| `GO_FLAGS` | `run_dev.sh` | Extra flags inserted before `go run . serve` for REST API only in the current script. |
| `GOLANG_PROTOBUF_REGISTRATION_CONFLICT` | local runner | Set to `ignore` by scripts to avoid protobuf registration crashes while mixing local modules. |
| `DOCKER_HOST` | developer shell | Optional Docker socket override discovered by `Forkedv2/find_docker.sh`. |

### Common Service Infrastructure

| Variable | Used by | What it does |
|----------|---------|--------------|
| `PG_HOST` | common-services | Postgres host. |
| `PG_PORT` | common-services | Postgres port. |
| `PG_USER` | common-services | Postgres user. |
| `PG_PASSWORD` | common-services | Postgres password. |
| `PG_DATABASE` | common-services | Postgres database. |
| `PG_SSL` | common-services | Enables Postgres SSL. |
| `PG_POOL_SIZE` | common-services | Postgres connection pool size. |
| `PG_MIN_IDLE_CONNS` | common-services | Minimum idle Postgres connections. |
| `PG_MAX_CONN_AGE` | common-services | Maximum Postgres connection age. |
| `PG_IDLE_TIMEOUT` | common-services | Postgres idle connection timeout. |
| `PG_MAX_RETRIES` | common-services | Retry count for transient Postgres errors. |
| `PG_MIN_RETRY_BACKOFF` | common-services | Minimum Postgres retry backoff. |
| `PG_MAX_RETRY_BACKOFF` | common-services | Maximum Postgres retry backoff. |
| `REDIS_MASTER_SERVICE_HOST` / `REDIS_SERVICE_HOST` | common-services, Web UI sessions | Redis host. |
| `REDIS_MASTER_SERVICE_PORT` / `REDIS_SERVICE_PORT` | common-services, Web UI sessions | Redis port. |
| `REDIS_PASS` | common-services, Web UI sessions | Redis password. |
| `REDIS_PASSWORD` | content-prober | Redis password alias used by the content prober server. |
| `REDIS_USER` | common-services | Redis username. |
| `REDIS_DB` | content-prober | Redis database number for content probe cache. |
| `REDIS_SERVICE_PORT_REDIS_SENTINEL` | common-services | Redis Sentinel port. |
| `REDIS_SERVICE_SENTINEL_MASTER_NAME` | common-services | Redis Sentinel master name. |
| `NATS_SERVICE_HOST` | common-services | NATS host. Empty disables NATS clients in services that tolerate nil clients. |
| `NATS_SERVICE_PORT` | common-services | NATS port. |
| `WEB_HOST` | many HTTP services | HTTP listen host. |
| `WEB_PORT` | many HTTP services | HTTP listen port. |
| `GRPC_HOST` | gRPC services | gRPC listen host. |
| `GRPC_PORT` | gRPC services | gRPC listen port. |
| `LISTEN_HOST` | Magnet2Torrent | Magnet2Torrent gRPC listen host. |
| `LISTEN_PORT` | Magnet2Torrent | Magnet2Torrent gRPC listen port. |
| `HTTP_PORT` | content-prober | Content prober HTTP listen port. |
| `PROBE_HOST` | common-services | Health probe listen host. |
| `PROBE_PORT` | common-services | Health probe listen port. |
| `USE_PROBE` | common-services | Enables health probe server. |
| `PPROF_HOST` | common-services | pprof listen host. |
| `PPROF_PORT` | common-services | pprof listen port. |
| `USE_PPROF` | common-services | Enables pprof server. |
| `PROM_HOST` | common-services | Prometheus metrics listen host. |
| `PROM_PORT` | common-services | Prometheus metrics listen port. |
| `USE_PROM` | common-services | Enables Prometheus metrics server. |

### S3, Vault, And Long-Term Storage

| Variable | Used by | What it does |
|----------|---------|--------------|
| `AWS_ACCESS_KEY_ID` | common-services S3 client | S3 access key. |
| `AWS_SECRET_ACCESS_KEY` | common-services S3 client | S3 secret key. |
| `AWS_ENDPOINT` | common-services S3 client | S3/MinIO endpoint. |
| `AWS_REGION` | common-services S3 client | S3 region. |
| `AWS_NO_SSL` | common-services S3 client | Disables SSL for S3 client. Useful for local MinIO over HTTP. |
| `AWS_BUCKET` | torrent-store, vault, video services | S3 bucket name. Meaning depends on the service using it. |
| `AWS_POSTER_CACHE_BUCKET` | Web UI library | Bucket for poster cache. |
| `AWS_USER_SUBTITLE_BUCKET` | Web UI user subtitles | Bucket for user-uploaded subtitle blobs. |
| `AWS_UPLOAD_CONCURRENCY` | Vault worker | Number of concurrent S3 upload workers. |
| `AWS_UPLOAD_PART_SIZE` | Vault worker | Multipart upload part size. |
| `WORKERS` | Vault worker | Number of vault worker goroutines. |
| `RESOURCE_ID` | Vault worker | Process one resource ID for debugging. |
| `VAULT_SERVICE_HOST` | Web UI | Vault API host. |
| `VAULT_SERVICE_PORT` | Web UI | Vault API port. |
| `VAULT_SECURE` | Web UI | Use HTTPS for Vault API. |
| `VAULT_STORAGE_PATH` | Web UI vault service | Local path used to calculate free storage. |
| `VAULT_PLEDGE_FREEZE_PERIOD` | Web UI vault service | Freeze period for vault pledges. |
| `VAULT_RESOURCE_ABANDONED_EXPIRE_PERIOD` | Web UI vault service | Time after which expired resources without pledges are removed from vault. |
| `VAULT_RESOURCE_EXPIRE_PERIOD` | Web UI vault service | Time after which unfunded resources with pledges are removed from vault. |
| `VAULT_RESOURCE_TRANSFER_TIMEOUT_PERIOD` | Web UI vault service | Time after which transfer attempts stop and a resource is removed. |
| `VAULT_VERIFY_INTEGRITY` | Vault worker | Verify stored files against torrent piece hashes. |
| `VAULT_VERIFY_THRESHOLD` | Vault verify command | Minimum resource size for verification. |
| `VAULT_VERIFY_LIMIT` | Vault verify command | Maximum resources to verify in one run. |
| `VAULT_VERIFY_DRY_RUN` | Vault verify command | Report mismatches without invalidating/requeueing. |
| `VAULT_VERIFY_RESOURCE_ID` | Vault verify command | Verify one resource by infohash. |
| `GC_GRACE_PERIOD` | Vault GC command | Skip files newer than this grace period. |
| `GC_BATCH_LIMIT` | Vault GC command | Maximum orphan files processed per run. |
| `GC_DRY_RUN` | Vault GC command | Report what GC would delete without deleting. |
| `METRICS_REFRESH_INTERVAL` | Vault metrics | How often vault metrics are recomputed from the database. |

### Torrent, Streaming, Proxy, And Cache Control

| Variable | Used by | What it does |
|----------|---------|--------------|
| `TORRENT_HTTP_PROXY_SERVICE_HOST` | REST API, Web UI, Vault, torrent-http-proxy | Torrent HTTP proxy host. |
| `TORRENT_HTTP_PROXY_SERVICE_PORT` | REST API, Web UI, Vault, torrent-http-proxy | Torrent HTTP proxy port. |
| `TORRENT_WEB_SEEDER_SERVICE_HOST` | local runner/service discovery | Torrent web seeder gRPC host used by proxy/service discovery. |
| `TORRENT_WEB_SEEDER_SERVICE_PORT` | local runner/service discovery | Torrent web seeder gRPC port used by proxy/service discovery. |
| `TORRENT_WEB_SEEDER_HOST` | torrent-web-seeder client | Torrent web seeder client host. |
| `TORRENT_WEB_SEEDER_PORT` | torrent-web-seeder client | Torrent web seeder client port. |
| `STAT_HOST` | torrent-web-seeder | Seeder stat gRPC listen host. |
| `STAT_PORT` | torrent-web-seeder | Seeder stat gRPC listen port. |
| `USE_STAT` | torrent-web-seeder | Enables seeder stat gRPC service. |
| `TORRENT_STORE_SERVICE_HOST` / `TORRENT_STORE_HOST` | REST API, archiver, clients | Torrent store host. |
| `TORRENT_STORE_SERVICE_PORT` / `TORRENT_STORE_PORT` | REST API, archiver, clients | Torrent store port. |
| `MAGNET2TORRENT_SERVICE_HOST` / `MAGNET2TORRENT_HOST` | REST API | Magnet2Torrent service host. |
| `MAGNET2TORRENT_SERVICE_PORT` / `MAGNET2TORRENT_PORT` | REST API | Magnet2Torrent service port. |
| `CONTENT_TRANSCODER_SERVICE_HOST` | local runner/service discovery | Content transcoder host used by proxy/service discovery. |
| `CONTENT_TRANSCODER_SERVICE_PORT` | local runner/service discovery | Content transcoder port used by proxy/service discovery. |
| `CONTENT_PROBER_SERVICE_HOST` / `CONTENT_PROBER_HOST` | transcoder, content-prober client, local runner | Content prober gRPC host. |
| `CONTENT_PROBER_SERVICE_PORT` / `CONTENT_PROBER_PORT` | transcoder, content-prober client, local runner | Content prober gRPC port. |
| `CONTENT_PROBER_HTTP_SERVICE_HOST` | local runner/service discovery | Content prober HTTP host. |
| `CONTENT_PROBER_HTTP_SERVICE_PORT` | local runner/service discovery | Content prober HTTP port. |
| `CONTENT_PROBER_TIMEOUT` | content-transcoder | Probe timeout in seconds. |
| `VIDEO_INFO_SERVICE_HOST` | REST API, local runner/service discovery | Video info service host. |
| `VIDEO_INFO_SERVICE_PORT` | REST API, local runner/service discovery | Video info service port. |
| `TORRENT_ARCHIVER_SERVICE_HOST` | local runner/service discovery | Torrent archiver host. |
| `TORRENT_ARCHIVER_SERVICE_PORT` | local runner/service discovery | Torrent archiver port. |
| `SRT2VTT_SERVICE_HOST` | local runner/service discovery | SRT2VTT host. |
| `SRT2VTT_SERVICE_PORT` | local runner/service discovery | SRT2VTT port. |
| `CONFIG_PATH` | torrent-http-proxy | Services YAML config path. |
| `TORRENT_PROXY_URL` | torrent-archiver | Torrent proxy URL used by archiver. |
| `EXPORT_DOMAIN` | REST API | Export/public domain for generated links. |
| `EXPORT_PREMIUM_DOMAIN` | REST API | Premium export domain. |
| `EXPORT_API_KEY` | REST API | Export API key. |
| `EXPORT_API_SECRET` | REST API | Export API secret. |
| `EXPORT_API_ROLE` | REST API | Export role used in generated claims. |
| `EXPORT_USE_SUBDOMAINS` | REST API | Generate subdomain-based export URLs. |
| `EXPORT_K8S_POOL` | REST API | Kubernetes pool label for exported jobs. |
| `EXPORT_PATH_PREFIX` | REST API | Prefix applied to exported paths. |
| `REST_API_SERVICE_HOST` | Web UI, Vault | REST API host. |
| `REST_API_SERVICE_PORT` | Web UI, Vault | REST API port. |
| `REST_API_SECURE` | Web UI, Vault | Use HTTPS for REST API. |
| `REST_API_EXPIRE` | Web UI, Vault | REST API generated link expiration in days. |
| `WEBTOR_API_KEY` | Web UI, Vault | Webtor API key. |
| `WEBTOR_API_SECRET` | Web UI, Vault | Webtor API secret. |
| `USE_INTERNAL_TORRENT_HTTP_PROXY` | REST API, Web UI, Vault | Route through internal torrent HTTP proxy. |
| `USE_BANDWIDTH_LIMIT` | torrent-http-proxy | Enables bandwidth limiting. |
| `ENFORCE_SESSION_IP` | torrent-http-proxy | Reject requests whose client IP does not match the JWT claim after normalization. |
| `MAX_CONC_TOTAL` | torrent-http-proxy | Max concurrent requests per session. |
| `MAX_CONC_PER_PATH` | torrent-http-proxy | Max concurrent requests per session per torrent/path. |
| `MAX_BIG_FILES_PER_HASH` | torrent-http-proxy | Max distinct large files active per session per torrent. |
| `BIG_FILE_THRESHOLD_BYTES` | torrent-http-proxy | Size threshold for counting a file as large. |
| `LIGHT_EXTS` | torrent-http-proxy | File extensions excluded from the big-file cap. |
| `MAX_IPS_PER_SESSION` | torrent-http-proxy | Max distinct client IPs per session per torrent/path within the tracking window. |
| `FILE_SIZE_CACHE_CAPACITY` | torrent-http-proxy | Upstream file-size cache capacity. |
| `PROXY_READ_BUFFER_SIZE` | torrent-http-proxy | HTTP proxy read buffer size. |
| `PROXY_WRITE_BUFFER_SIZE` | torrent-http-proxy | HTTP proxy write buffer size. |
| `RETRY_MAX_ATTEMPTS` | torrent-http-proxy | Upstream retry attempts. |
| `RETRY_DELAY_MS` | torrent-http-proxy | Delay between upstream retry attempts. |
| `CLICKHOUSE_DSN` | torrent-http-proxy | ClickHouse DSN. |
| `CLICKHOUSE_BATCH_SIZE` | torrent-http-proxy | ClickHouse insert batch size. |
| `CLICKHOUSE_REPLICATED` | torrent-http-proxy | Enables replicated ClickHouse mode. |
| `CLICKHOUSE_SHARDED` | torrent-http-proxy | Enables sharded ClickHouse mode. |
| `MY_NODE_NAME` | torrent-http-proxy | Node name used by proxy common config. |
| `ENDPOINTS_NAMESPACE` | torrent-http-proxy | Kubernetes endpoints namespace. |
| `NODE_LABEL_PREFIX` | REST API, torrent-http-proxy | Kubernetes node label prefix. |
| `INPUT` | torrent-web-seeder | Local torrent file or directory input. |
| `MAX_READAHEAD` | torrent-web-seeder | HTTP read-ahead buffer size. |
| `DOWNLOAD_RATE` | torrent-web-seeder | Torrent download rate limit. |
| `PER_TORRENT_CACHE_BUDGET` | torrent-web-seeder | Per-torrent LRU cache budget. |
| `HTTP_PROXY` | torrent-web-seeder | HTTP proxy for torrent client tracker/webseed requests. |
| `USER_AGENT` | torrent-web-seeder | Torrent client user agent. |
| `NO_UPLOAD` | torrent-web-seeder | Disables uploading. |
| `SEED` | torrent-web-seeder | Continue seeding after download. |
| `DISABLE_UTP` | torrent-web-seeder | Disables uTP. |
| `DISABLE_WEBTORRENT` | torrent-web-seeder | Disables WebTorrent. |
| `DISABLE_WEBSEEDS` | torrent-web-seeder | Disables webseeds. |
| `TORRENT_CLIENT_DEBUG` | torrent-web-seeder | Verbose anacrolix torrent client logging. |
| `ESTABLISHED_CONNS_PER_TORRENT` | torrent-web-seeder | Established connection limit per torrent. |
| `HALF_OPEN_CONNS_PER_TORRENT` | torrent-web-seeder | Half-open connection limit per torrent. |
| `TOTAL_HALF_OPEN_CONNS` | torrent-web-seeder | Total half-open connection limit. |
| `TORRENT_PEERS_HIGH_WATER` | torrent-web-seeder | Torrent peer high-water setting. |
| `TORRENT_PEERS_LOW_WATER` | torrent-web-seeder | Torrent peer low-water setting. |
| `DIAL_RATE_LIMIT` | torrent-web-seeder | Torrent dial rate limit. |
| `MIN_DIAL_TIMEOUT` | torrent-web-seeder | Minimum dial timeout. |
| `NOMINAL_DIAL_TIMEOUT` | torrent-web-seeder | Nominal dial timeout. |
| `HANDSHAKE_TIMEOUT` | torrent-web-seeder | Torrent handshake timeout. |
| `KEEPALIVE_TIMEOUT` | torrent-web-seeder | Torrent keepalive timeout. |
| `MAX_UNVERIFIED_BYTES` | torrent-web-seeder | Maximum unverified bytes. |
| `PIECE_HASHERS_PER_TORRENT` | torrent-web-seeder | Piece hasher count per torrent. |
| `SOURCE_URL` | content/video/image services | Source URL for transformer/info/thumbnail services and transcoder jobs. |
| `OUTPUT` | content-transcoder | Local output path. |
| `DEBUG` | content-transcoder | Enables debug logging. |
| `CLEAN_ON_STARTUP` | content-transcoder | Cleans transcoder output directory on startup. |
| `HLS_AAC_CODEC` | content-transcoder | AAC codec used for HLS audio. |
| `DISABLE_VIDEO_TRANSCODING` | content-transcoder | Disables video transcoding. |
| `PLAYER` | content-transcoder | Enables player mode in its web service. |
| `FORCE_DIRECT_PLAY` | Web UI jobs | Forces direct play instead of transcoding to reduce CPU usage. |
| `WARMUP_TIMEOUT_MIN` | Web UI jobs | Warmup timeout in minutes. |
| `WARMUP_NO_PEERS_TIMEOUT_SEC` | Web UI jobs | Early no-peers cutoff. |
| `WARMUP_SLOW_PEERS_TIMEOUT_SEC` | Web UI jobs | Early slow-peer cutoff. |
| `GRACE_RULES_ENABLED` | Web UI jobs | Enables grace rules. |
| `GRACE_DURATION_SEC` | Web UI jobs | Grace duration. |
| `GRACE_RATE` | Web UI jobs | Grace rate. |
| `DATA_DIR` | torrent-web-seeder-cleaner | Seeder media cache directory. |
| `CLEANER_KEEP_FREE` | torrent-web-seeder-cleaner | Free-space cleanup trigger threshold. |
| `CLEANER_FREE` | torrent-web-seeder-cleaner | Free-space cleanup target threshold. |
| `CACHED_MEDIA_MAX_AGE` / `CLEANER_MAX_AGE` | torrent-web-seeder-cleaner | Maximum inactive media cache age. |
| `CACHED_MEDIA_CLEAN_INTERVAL` / `CLEANER_INTERVAL` | torrent-web-seeder-cleaner | Media cache cleanup interval. |
| `CACHED_METADATA_EXPIRE` / `WEB_UI_API_CACHE_EXPIRE` | Web UI | Web UI metadata/list/torrent cache expiration. |
| `CACHED_METADATA_EXPIRE` / `REST_API_RESOURCE_CACHE_EXPIRE` | REST API | REST API resource metadata cache expiration. |
| `CACHED_TEMP_EXPIRE` / `REST_API_CACHE_STATUS_EXPIRE` | REST API | Cached-media status lookup expiration. |
| `CACHE_INDEX_EXPIRE` | Web UI cache index | Cache-index metadata expiration. |

### Web UI, Auth, Library, Embed, And Integrations

| Variable | Used by | What it does |
|----------|---------|--------------|
| `ASSETS_PATH` | Web UI static handler | Static asset directory. |
| `WEB_ASSETS_HOST` | Web UI static handler | External/static assets host. |
| `DEMO_MAGNET` | Web UI common config | Demo magnet link. |
| `DEMO_TORRENT` | Web UI common config | Demo torrent URL. |
| `USE_DIRECT_LINKS` | Web UI common config | Enables direct links. |
| `SESSION_SECRET` | Web UI common config | Session signing secret. |
| `DISABLE_WEBDAV` | Web UI common config | Disables WebDAV integration. |
| `DISABLE_EMBED` | Web UI common config | Disables embed integration. |
| `SUPERTOKENS_SERVICE_HOST` | Web UI auth | SuperTokens host. |
| `SUPERTOKENS_SERVICE_PORT` | Web UI auth | SuperTokens port. |
| `GOOGLE_CLIENT_ID` | Web UI auth | Google OAuth client ID. |
| `GOOGLE_CLIENT_SECRET` | Web UI auth | Google OAuth client secret. |
| `OVERRIDE_USER_EMAIL` | Web UI auth | Overrides user email. Useful for local debugging. |
| `PATREON_CLIENT_ID` | `.env` convention | Patreon client ID placeholder used by this local config. |
| `PATREON_CLIENT_SECRET` | `.env` convention | Patreon client secret placeholder used by this local config. |
| `CLAIMS_PROVIDER_SERVICE_HOST` | Web UI claims client | Claims provider host. |
| `CLAIMS_PROVIDER_SERVICE_PORT` | Web UI claims client | Claims provider port. |
| `USE_ABUSE_STORE` | Web UI abuse client | Enables abuse store client. |
| `ABUSE_STORE_SERVICE_HOST` | Web UI, torrent-store, url-store | Abuse store host. |
| `ABUSE_STORE_SERVICE_PORT` | Web UI, torrent-store, url-store | Abuse store port. |
| `USE_EVENT_HANDLER` | Web UI event handler | Enables event handler. |
| `USE_EMBED` | Web UI embed settings | Enables embed. |
| `EMBED_USE_ADS` | Web UI embed settings | Enables ads in embeds. |
| `EMBED_ONLY_AUTHORIZED` | Web UI embed settings | Restricts embed to authorized domains/users. |
| `USE_UMAMI` | Web UI Umami integration | Enables Umami analytics. |
| `UMAMI_HOST_URL` | Web UI Umami integration | Umami host URL. |
| `UMAMI_WEBSITE_ID` | Web UI Umami integration | Umami website ID. |
| `TURNSTILE_SITE_KEY` | Web UI Turnstile | Cloudflare Turnstile site key. |
| `TURNSTILE_SECRET_KEY` | Web UI Turnstile | Cloudflare Turnstile secret key. |
| `GEOIP_API_SERVICE_HOST` | Web UI GeoIP | GeoIP API host. |
| `GEOIP_API_SERVICE_PORT` | Web UI GeoIP | GeoIP API port. |
| `USE_GEOIP_API` | Web UI GeoIP | Enables GeoIP API. |
| `REQUEST_URL_MAPPINGS` | Web UI request URL mapper | JSON mapping of external URLs to internal URLs for request optimization. |
| `STREMIO_ADDON_USER_AGENT` | Web UI Stremio client | User agent for Stremio addon HTTP client. |
| `STREMIO_ADDON_PROXY` | Web UI Stremio client | Proxy URL for Stremio addon HTTP client. |

### Metadata, Subtitles, And AI

| Variable | Used by | What it does |
|----------|---------|--------------|
| `OMDB_API_HOST` | Web UI OMDB client | OMDB API host. |
| `OMDB_API_PORT` | Web UI OMDB client | OMDB API port. |
| `OMDB_API_SECURE` | Web UI OMDB client | Use HTTPS for OMDB. |
| `OMDB_API_KEY` | Web UI OMDB client, sidecar | OMDB API key. |
| `TMDB_API_HOST` | Web UI TMDB client | TMDB API host. |
| `TMDB_API_PORT` | Web UI TMDB client | TMDB API port. |
| `TMDB_API_SECURE` | Web UI TMDB client | Use HTTPS for TMDB. |
| `TMDB_API_KEY` | Web UI TMDB client | TMDB API key. |
| `TMDB_IMAGE_BASE_URL` | Web UI TMDB client | TMDB image base URL. |
| `KINOPOISK_UNOFFICIAL_API_HOST` | Web UI Kinopoisk client | Kinopoisk API host. |
| `KINOPOISK_UNOFFICIAL_API_PORT` | Web UI Kinopoisk client | Kinopoisk API port. |
| `KINOPOISK_UNOFFICIAL_API_SECURE` | Web UI Kinopoisk client | Use HTTPS for Kinopoisk. |
| `KINOPOISK_UNOFFICIAL_API_KEY` | Web UI Kinopoisk client | Kinopoisk API key. |
| `OSDB_API_KEY` | video-info | OpenSubtitles API key. |
| `OSDB_API_USER_AGENT` | video-info | OpenSubtitles user agent. |
| `OSDB_API_URL` | video-info | OpenSubtitles API URL. |
| `OSDB_USER` | video-info | OpenSubtitles username. |
| `OSDB_PASS` | video-info | OpenSubtitles password. |
| `ANTHROPIC_API_KEY` | Web UI AI clients | Anthropic API key shared by AI recommendations and AI enrichment. |
| `AI_ENRICH_ENABLED` | Web UI AI enrichment | Enables AI fallback for torrent metadata enrichment. |
| `AI_ENRICH_MODEL` | Web UI AI enrichment | Claude model ID for enrichment fallback. |
| `AI_ENRICH_TIMEOUT_SECONDS` | Web UI AI enrichment | Timeout per AI enrichment call. |
| `AI_ENRICH_MAX_CANDIDATES` | Web UI AI enrichment | Candidate count requested from AI enrichment. |
| `AI_RECOMMENDATIONS_ENABLED` | Web UI recommendations | Enables AI recommendations in Discover. |
| `AI_RECOMMENDATIONS_MODEL` | Web UI recommendations | Legacy/fallback Claude model ID. |
| `AI_RECOMMENDATIONS_FREE_MODEL` | Web UI recommendations | Claude model ID for free-tier users. |
| `AI_RECOMMENDATIONS_PAID_MODEL` | Web UI recommendations | Claude model ID for paid-tier users. |
| `AI_RECOMMENDATIONS_CHIPS_MODEL` | Web UI recommendations | Claude model ID for recommendation chip generation. |
| `AI_RECOMMENDATIONS_FREE_DAILY_QUOTA` | Web UI recommendations | Daily AI recommendation quota for free-tier users. |
| `AI_RECOMMENDATIONS_PAID_DAILY_QUOTA` | Web UI recommendations | Daily AI recommendation quota for paid-tier users. |
| `AI_RECOMMENDATIONS_MAX_QUERY_LENGTH` | Web UI recommendations | Max recommendation query length. |
| `AI_RECOMMENDATIONS_HISTORY_LIMIT` | Web UI recommendations | Recent watched/rated items included in prompt context. |
| `AI_RECOMMENDATIONS_CHIPS_TTL_SECONDS` | Web UI recommendations | Per-user chip cache TTL. |
| `AI_RECOMMENDATIONS_RECS_TTL_SECONDS` | Web UI recommendations | Per-user/query recommendation cache TTL. |
| `AI_RECOMMENDATIONS_FRESH_RELEASES_MIN_YEAR` | Web UI recommendations | Minimum year for fresh releases injected into prompts. |
| `AI_RECOMMENDATIONS_FRESH_RELEASES_LIMIT` | Web UI recommendations | Max recent films loaded into AI prompt context. |
| `AI_RECOMMENDATIONS_FRESH_RELEASES_CACHE_TTL_SECONDS` | Web UI recommendations | In-memory fresh-release prompt block cache TTL. |
| `GEMINI_API_KEY` | `.env` convention | Gemini API key placeholder used by this local config. Current Go AI flags in this repo declare Anthropic/Claude variables. |
| `ENRICH_POPULAR_RELEASE_DATE_GTE` | Web UI enrich command | Minimum release date for popular enrichment fetches. |
| `ENRICH_POPULAR_LIMIT` | Web UI enrich command | Max films fetched by popular enrichment command. |
| `STASHDB_API_KEY` | Python sidecar | StashDB API key. |
| `STASHDB_ENDPOINT` | Python sidecar | StashDB GraphQL endpoint. |
| `THEPORNDB_API_KEY` / `TPDB_API_KEY` | Python sidecar | ThePornDB API key. |
| `TPDB_BASE` | Python sidecar | ThePornDB API base URL. |
| `SIDECAR_ENRICHMENT_ENABLED` | Python sidecar | Enables sidecar enrichment unless set to `false`. |
| `SIDECAR_DATA_DIR` | Python sidecar | Directory containing sidecar `settings.json`. |

### Stores, Abuse, Claims, Mail, And Miscellaneous

| Variable | Used by | What it does |
|----------|---------|--------------|
| `USE_S3` | torrent-store, video-info | Enables S3-backed storage. |
| `USE_REDIS` | torrent-store | Enables Redis-backed storage. |
| `REDIS_EXPIRE` | torrent-store | Redis provider expiration in seconds. |
| `BADGER_PATH` | torrent-store | Local Badger database path. |
| `BADGER_EXPIRE` | torrent-store | Badger provider expiration in seconds. Set `0` for no Badger TTL. |
| `STOPLIST_PATH` | torrent-store | Stoplist path. |
| `USE_ABUSE` | torrent-store | Enables abuse checking. |
| `STORE_SYNC_INTERVAL` | abuse-store | Store sync interval in minutes. |
| `STORE_CACHE_CAPACITY` | claims-provider | Claims store cache capacity. |
| `STORE_CACHE_CONCURRENCY` | claims-provider | Maximum concurrent cache builders. |
| `STORE_CACHE_EXPIRE` | claims-provider | Cache expiration for successful entries. |
| `STORE_CACHE_ERROR_EXPIRE` | claims-provider | Cache expiration for error entries. |
| `STORE_DB_TIMEOUT` | claims-provider | Database query timeout. |
| `MAIL_SENDER` | abuse-store | Sender email. |
| `MAIL_SUPPORT` | abuse-store | Support email. |
| `SMTP_HOST` | abuse-store, Web UI | SMTP host. |
| `SMTP_USER` | abuse-store, Web UI | SMTP user. |
| `SMTP_PASS` | abuse-store, Web UI | SMTP password. |
| `SMTP_PORT` | abuse-store, Web UI | SMTP port. |
| `SMTP_SECURE` | Web UI | Enables secure SMTP in Web UI. |
| `SMTP_TLS` | abuse-store | Enables SMTP TLS. |
| `SMTP_TLS_SECURE` | abuse-store | Enables secure TLS behavior. |
| `SMTP_STARTTLS` | abuse-store | Enables SMTP STARTTLS. |
| `API_KEY` | torrent-archiver, torrent-http-proxy claims | API key for service authentication. |
| `API_SECRET` | torrent-archiver, torrent-http-proxy claims | API secret for service authentication. |

## Documentation Notes

Service-specific READMEs still exist under their own directories and may include command examples or historical upstream details. This top-level README is the customization map for the local Octor workspace; if a service-specific README conflicts with a current `EnvVar` declaration, trust the code and update the docs.

## Development Documents

| File | Purpose |
|------|---------|
| `Forkedv2/DEVELOPMENT.md` | Local architecture and workflow notes. |
| `Forkedv2/CHANGELOG.md` | Human-readable record of changes made in this workspace. |
| `Forkedv2/TODO.md` | Current task queue. |
| `Forkedv2/AUDIT.md` | Audit notes. |

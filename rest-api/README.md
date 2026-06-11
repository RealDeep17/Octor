# rest-api

REST-API for Octor can:
1. Store resource to Octor (torrent/magnet-uri)
2. List content of stored resource
3. Export urls to content for downloading and streaming

## Basic usage

The service is managed via `systemd` and configured using `custom.env`.

```bash
# Start the service
./run.sh mode production

# Check status
systemctl status octor-rest-api
```

## Configuration

Primary configuration is managed via environment variables (usually loaded from `custom.env`) or CLI flags.

| Flag | Environment Variable | Default | Description |
|------|----------------------|---------|-------------|
| `--port` | `WEB_PORT` | `8080` | HTTP listening port |
| `--pprof-port` | `PPROF_PORT` | `8080` | pprof listening port (Octor default: `51080`) |
| `--probe-port` | `PROBE_PORT` | `8081` | probe listening port (Octor default: `52080`) |
| `--torrent-store-host` | `TORRENT_STORE_HOST` | `127.0.0.1` | Torrent Store host |
| `--torrent-store-port` | `TORRENT_STORE_PORT` | `50051` | Torrent Store gRPC port |
| `--magnet2torrent-host` | `MAGNET2TORRENT_HOST` | `127.0.0.1` | Magnet2Torrent host |
| `--magnet2torrent-port` | `MAGNET2TORRENT_PORT` | `50051` | Magnet2Torrent gRPC port (Octor default: `50053`) |
| `--video-info-host` | `VIDEO_INFO_HOST` | `127.0.0.1` | Video Info host |
| `--video-info-port` | `VIDEO_INFO_PORT` | `50051` | Video Info gRPC port (Octor default: `50056`) |
| `--export-domain` | `OCTOR_DOMAIN` | `""` | Public domain for generated URLs |

### Standard Octor Ports

When managed by `run.sh`, the following ports are used:
- **HTTP:** `8080`
- **Pprof:** `51080`
- **Probe:** `52080`

## Swagger (OpenAPI)

Once running, the Swagger UI is available at:
`http://localhost:8080/swagger/index.html`
## Transmission RPC compatibility

The REST API includes a Transmission RPC-compatible endpoint for automation and ARR clients. It tracks added torrents in `/srv/octor/infra-data/transmission_torrents.json` by default and creates that file's parent directory dynamically.

Selective imports honor both `files-wanted` and `files-unwanted` when those fields are provided by a client. Stock ARR clients usually do not send file-selection fields on `torrent-add`; the handling is defensive for compatible Transmission RPC clients and duplicate/revised requests.

ARR classification reads all configured Transmission download-client usernames from Sonarr, Radarr, and Whisparr SQLite databases, then matches the request BasicAuth username or the ARR user-agent. This avoids misclassification when an ARR instance has more than one Transmission client configured.

When a completed vault import needs placeholder files for ARR import workflows, runtime lookup prefers `/srv/octor/infra-data/dummy*.mkv` and falls back to the tracked seed templates in `rest-api/assets/dummy/`. Keep those small files in the repository for fresh clone/setup compatibility.

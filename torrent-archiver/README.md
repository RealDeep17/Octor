# torrent-archiver

The `torrent-archiver` service generates ZIP archives from torrent content on-the-fly.

## Features
- Dynamic ZIP generation for selected torrent files.
- Integration with `torrent-store` to retrieve torrent metadata.
- Integrated health probes, Pprof, and Prometheus metrics.

## Configuration

Configuration can be provided via command-line flags or environment variables (recommended to use `custom.env`).

| Flag | Environment Variable | Default | Description |
|------|----------------------|---------|-------------|
| `--host` | `WEB_HOST` | `""` | listening host |
| `--port` | `WEB_PORT` | `8080` | HTTP listening port (Octor default: `50057`) |
| `--pprof-port` | `PPROF_PORT` | `8080` | pprof listening port (Octor default: `51057`) |
| `--probe-port` | `PROBE_PORT` | `8081` | probe listening port (Octor default: `52057`) |
| `--prom-port` | `PROM_PORT` | `0` | Prometheus metrics port (Octor default: `53057`) |
| `--torrent-store-host` | `TORRENT_STORE_HOST` | `127.0.0.1` | Torrent Store host |
| `--torrent-store-port` | `TORRENT_STORE_PORT` | `50051` | Torrent Store gRPC port |

### Standard Octor Ports

When managed by `run.sh`, the following ports are used:
- **HTTP:** `50057`
- **Pprof:** `51057`
- **Probe:** `52057`
- **Prometheus:** `53057`

## Build & Run

### Local
```bash
go run .
```

### Docker
```bash
docker build -t octor/torrent-archiver .
docker run --rm -p 50057:50057 -p 8081:8081 octor/torrent-archiver
```

## Service Management
Managed via `systemd` using the `run.sh mode` script in the project root.

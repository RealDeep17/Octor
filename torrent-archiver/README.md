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
| `--port` | `PORT` | `50057` | HTTP listening port |
| `--torrent-store-host` | `TORRENT_STORE_HOST` | `""` | Torrent Store host |
| `--torrent-store-port` | `TORRENT_STORE_PORT` | `50051` | Torrent Store gRPC port |

*Probe and metrics options are provided by the `common-services` package.*

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
Managed via `systemd` using the `switch_mode.sh` script in the project root.

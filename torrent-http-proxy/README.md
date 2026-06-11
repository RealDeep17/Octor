# torrent-http-proxy

Special HTTP-proxy for the Octor platform that routes requests and manages service chaining.

## Usage

The service is managed via `systemd` and configured using `custom.env`.

```bash
# Start the service
./run.sh mode production

# Check status
systemctl status octor-torrent-http-proxy
```

| Flag | Environment Variable | Default | Description |
|------|----------------------|---------|-------------|
| `--host` | `WEB_HOST` | `""` | listening host |
| `--port` | `WEB_PORT` | `8080` | HTTP listening port (Octor default: `50052`) |
| `--pprof-port` | `PPROF_PORT` | `8080` | pprof listening port (Octor default: `51052`) |
| `--probe-port` | `PROBE_PORT` | `8081` | probe listening port (Octor default: `52052`) |
| `--config` | `CONFIG` | `services.yaml` | configuration file |

### Standard Octor Ports

When managed by `run.sh`, the following ports are used:
- **HTTP:** `50052`
- **Pprof:** `51052`
- **Probe:** `52052`

## Configuration

Primary configuration is stored in `custom.env` and `services.yaml`.

- **Redis:** `REDIS_HOST`, `REDIS_PORT` (Octor default: `6380`)
- **Sidecar:** `SIDECAR_HOST`, `SIDECAR_PORT` (Octor default: `8000`)
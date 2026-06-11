# Vault

Permanent, deduplicated storage layer for Octor.

## Quick start

The service is managed via `systemd` and configured using `custom.env`.

```bash
# Start the service
./run.sh mode production

# Check status
systemctl status octor-vault
```

| Flag | Environment Variable | Default | Description |
|------|----------------------|---------|-------------|
| `--host` | `WEB_HOST` | `""` | listening host |
| `--port` | `WEB_PORT` | `8080` | HTTP listening port (Octor default: `8086`) |
| `--pprof-port` | `PPROF_PORT` | `8080` | pprof listening port (Octor default: `51086`) |
| `--probe-port` | `PROBE_PORT` | `8081` | probe listening port (Octor default: `52086`) |
| `--prom-port` | `PROM_PORT` | `0` | Prometheus metrics port (Octor default: `53086`) |

### Standard Octor Ports

When managed by `run.sh`, the following ports are used:
- **HTTP:** `8086`
- **Pprof:** `51086`
- **Probe:** `52086`
- **Prometheus:** `53086`

Swagger UI: http://localhost:8086/swagger/index.html

## API (short)

- PUT `/resource/{id}` — queue store, returns 202 with resource
- GET `/resource/{id}` — fetch resource or 404
- DELETE `/resource/{id}` — queue delete or cancel queued store
- GET/HEAD `/webseed/{id}/{path}` — serve stored file with Range support

## License

See LICENSE.
## Workers and status semantics

`WORKERS` controls how many vault worker goroutines poll and claim queue jobs. `VAULT_MAX_CONCURRENT_JOBS` caps how many store/upload jobs may actively run at once. Keeping `WORKERS >= VAULT_MAX_CONCURRENT_JOBS` leaves capacity for retries, deletes, and lease recovery while uploads are busy.

User-facing status terms:

- `waiting` means a funded resource is queued or an old processing claim has expired and is waiting to be claimed.
- `vaulting` means a funded resource is actively processing/uploading.
- `vaulted` means storage completed.

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

The proxy listens on port **50052**.

## Configuration

Primary configuration is stored in `custom.env`.

- **Port:** 50052 (default)
- **Redis:** `REDIS_HOST`, `REDIS_PORT=6380`
- **Sidecar:** `SIDECAR_HOST`, `SIDECAR_PORT=8000`

Example `custom.env`:
```env
PORT=50052
REDIS_PORT=6380
SIDECAR_PORT=8000
```
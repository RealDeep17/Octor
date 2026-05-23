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

The Vault listens on port **8086**.

## Configuration

Primary configuration is stored in `custom.env`. 

- **Web:** `WEB_PORT=8086`
- **Postgres:** `PG_HOST`, `PG_PORT=5433`, `PG_USER`, `PG_PASSWORD`, `PG_DATABASE`
- **Redis:** `REDIS_HOST`, `REDIS_PORT=6380`
- **S3 Gateway:** `S3_ENDPOINT=http://localhost:9000`, `S3_REGION`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`

Example `custom.env`:
```env
WEB_PORT=8086
PG_PORT=5433
REDIS_PORT=6380
S3_ENDPOINT=http://localhost:9000
```

Swagger UI: http://localhost:8086/swagger/index.html

## API (short)

- PUT `/resource/{id}` — queue store, returns 202 with resource
- GET `/resource/{id}` — fetch resource or 404
- DELETE `/resource/{id}` — queue delete or cancel queued store
- GET/HEAD `/webseed/{id}/{path}` — serve stored file with Range support

## License

See LICENSE.

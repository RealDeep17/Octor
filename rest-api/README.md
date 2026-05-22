# rest-api

REST-API for Octor can:
1. Store resource to Octor (torrent/magnet-uri)
2. List content of stored resource
3. Export urls to content for downloading and streaming

## Basic usage

The service is managed via `systemd` and configured using `custom.env`.

```bash
# Start the service
./switch_mode.sh production

# Check status
systemctl status octor-rest-api
```

## Configuration

Primary configuration is stored in `custom.env`. 

- **Port:** 8080 (default)
- **Torrent Store:** 50051
- **Magnet2Torrent:** 50051

Example environment variables:
```env
WEB_PORT=8080
TORRENT_STORE_HOST=localhost
TORRENT_STORE_PORT=50051
```

## Swagger (OpenAPI)

Once running, the Swagger UI is available at:
http://localhost:8080/swagger/index.html

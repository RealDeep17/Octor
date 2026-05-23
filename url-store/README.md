# url-store

The `url-store` service is responsible for storing and managing URLs within the Octor platform. It provides both a gRPC API and a Web interface.

## Features
- Store and retrieve URLs.
- Integration with `abuse-store` to check for banned content.
- PostgreSQL for persistent storage.
- Integrated health probes.
- gRPC and Web interfaces.

## Configuration

Configuration can be provided via command-line flags or environment variables (recommended to use `custom.env`).

| Flag | Environment Variable | Default | Description |
|------|----------------------|---------|-------------|
| `--grpc-host` | `GRPC_HOST` | `""` | gRPC listening host |
| `--grpc-port` | `GRPC_PORT` | `50061` | gRPC listening port |
| `--port` | `PORT` | `8080` | Web listening port (Internal) |
| `--postgres-host` | `PG_HOST` | `""` | PostgreSQL host |
| `--postgres-port` | `PG_PORT` | `5433` | PostgreSQL port |

*PostgreSQL and Probe options are provided by the `common-services` package.*

## API

### gRPC Service
Standard gRPC interface for URL management.

### Web Interface
HTTP interface for URL access.

## Build & Run

### Local
```bash
go run . serve
```

### Docker
```bash
docker build -t octor/url-store .
docker run --rm -p 50061:50061 -p 8081:8081 octor/url-store
```

## Service Management
Managed via `systemd` using the `run.sh mode` script in the project root.

# claims-provider

Provides user claims by email via gRPC for the Octor platform.

## Features
- gRPC API for retrieving claims.
- In-memory caching with configurable concurrency and expiration.
- Graceful shutdown support.
- Integrated health probes.

## Configuration

Configuration can be provided via command-line flags or environment variables (recommended to use `custom.env`).

| Flag | Environment Variable | Default | Description |
|------|----------------------|---------|-------------|
| `--grpc-host` | `GRPC_HOST` | `""` | gRPC listening host |
| `--grpc-port` | `GRPC_PORT` | `50060` | gRPC listening port |
| `--store-cache-concurrency` | `STORE_CACHE_CONCURRENCY` | `10` | Maximum concurrent cache builders |
| `--store-cache-expire` | `STORE_CACHE_EXPIRE` | `60s` | Cache expiration for successful entries |
| `--store-cache-error-expire` | `STORE_CACHE_ERROR_EXPIRE` | `10s` | Cache expiration for errors |
| `--store-cache-capacity` | `STORE_CACHE_CAPACITY` | `1000` | Cache capacity |
| `--store-db-timeout` | `STORE_DB_TIMEOUT` | `5s` | DB query timeout |
| `--postgres-host` | `PG_HOST` | `""` | PostgreSQL host |
| `--postgres-port` | `PG_PORT` | `5433` | PostgreSQL port |

*PostgreSQL and Probe options are provided by the `common-services` package (HTTP probe defaults to `8081`).*

## API

### gRPC Service: `ClaimsProvider`
**Method:** `Get(GetRequest{email}) -> GetResponse{context, claims}`

## Build & Run

### Local
```bash
go run . serve
```

### Docker
```bash
docker build -t octor/claims-provider .
docker run --rm -p 50060:50060 -p 8081:8081 octor/claims-provider
```

## Client CLI

A test client is available in the `client/` directory.

### Build
```bash
go build -o claims-client ./client
```

### Run
```bash
./claims-client --grpc-host 127.0.0.1 --grpc-port 50060 --email user@example.com
```

## Service Management
Managed via `systemd` using the `run.sh mode` script in the project root.

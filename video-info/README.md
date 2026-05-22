# video-info

The `video-info` service gathers additional metadata for torrent video content from public sources such as OpenSubtitles (OSDB).

## Features
- Fetches video metadata and subtitles.
- IMDB search integration.
- S3 storage for cached subtitle files.
- Redis-based caching for performance.
- Integrated health probes.

## Configuration

Configuration can be provided via command-line flags or environment variables (recommended to use `custom.env`).

| Flag | Environment Variable | Default | Description |
|------|----------------------|---------|-------------|
| `--port` | `PORT` | `50056` | HTTP listening port |
| `--redis-host` | `REDIS_HOST` | `localhost` | Redis host |
| `--redis-port` | `REDIS_PORT` | `6380` | Redis port |
| `--s3-endpoint` | `S3_ENDPOINT` | `http://localhost:9000` | S3 Gateway endpoint |
| `--osdb-user` | `OSDB_USER` | `""` | OpenSubtitles username |
| `--osdb-pass` | `OSDB_PASS` | `""` | OpenSubtitles password |

*S3 and Redis options are provided by the `common-services` package.*

## Build & Run

### Local
```bash
go run .
```

### Docker
```bash
docker build -t octor/video-info .
docker run --rm -p 50056:50056 -p 8081:8081 octor/video-info
```

## Service Management
Managed via `systemd` using the `switch_mode.sh` script in the project root.

# Content Prober

A high-performance media probing service that wraps `ffprobe`. It provides gRPC and HTTP interfaces for analyzing media content and caches results in Redis for rapid subsequent lookups.

## 🚀 Key Features

- **FFprobe Integration:** Deep analysis of media streams, formats, and metadata.
- **Redis Caching:** Transparently caches probe results to minimize redundant processing.
- **Dual Interface:** Supports both gRPC (internal service mesh) and HTTP (external/utility) access.

## ⚙️ Configuration

Configuration is managed via environment variables or CLI flags.

| Variable | Flag | Default |
|----------|------|---------|
| `LISTEN_HOST` | `--host` | `0.0.0.0` |
| `HTTP_PORT` | `--http-port` | `50062` |
| `GRPC_PORT` | `--port` | `50063` |
| `REDIS_HOST` | `--redis-host` | `127.0.0.1` |
| `REDIS_PORT` | `--redis-port` | `6380` |

## 🛠 Usage

### Service Management
Content Prober is managed via systemd and orchestrated through `run.sh mode`.

```sh
# Check status
sudo systemctl status octor-content-prober
```

### Manual Run
```sh
./bin/content-prober \
    --http-port 50062 \
    --port 50063 \
    --redis-host localhost \
    --redis-port 6380
```

## 📐 Architecture

Content Prober acts as an internal utility service. It is primarily used by the `content-transcoder` and `web-ui` to identify stream layouts (audio/video/subtitles) before playback begins.

## ⚖️ License

All rights reserved.

# Content Transcoder

A robust transcoding service that converts HTTP media streams into HLS (HTTP Live Streaming) on-demand. It features intelligent session management, automatic inactivity shutdown, and integrated VOD playback capabilities.

## 🚀 Key Features

- **On-Demand Transcoding:** Starts transcoding processes only when a user initiates a stream.
- **HLS Segmenting:** Optimized for low-latency playback with flexible preset support.
- **Auto-Cleanup:** Automatically terminates transcoding jobs and cleans up temporary segments after periods of inactivity.
- **Session-Aware:** Tracks active streaming sessions to prevent redundant transcoding of the same resource.

## ⚙️ Configuration

| Variable | Flag | Default | Description |
|----------|------|---------|-------------|
| `WEB_PORT` | `--port` | `8080` | HTTP listening port (Octor default: `50055`) |
| `PPROF_PORT` | `--pprof-port` | `8080` | pprof listening port (Octor default: `51055`) |
| `PROBE_PORT` | `--probe-port` | `8081` | probe listening port (Octor default: `52055`) |
| `SOURCE_URL` | `--input` | (Required) | Source media URL |
| `OUTPUT` | `--output` | `out` | Output directory for segments |
| `CONTENT_PROBER_SERVICE_HOST` | `--content-prober-host` | `127.0.0.1` | Content Prober host |
| `CONTENT_PROBER_SERVICE_PORT` | `--content-prober-port` | `50051` | Content Prober gRPC port (Octor default: `50063`) |
| `PRESET` | `--preset` | `ultrafast` | FFmpeg encoding preset |

### Standard Octor Ports

When managed by `run.sh`, the following ports are used:
- **HTTP:** `50055`
- **Pprof:** `51055`
- **Probe:** `52055`

## 🛠 Usage

### Service Management
Managed via systemd and orchestrated through `run.sh mode`.

```sh
# Check status
sudo systemctl status octor-content-transcoder
```

### Manual Run Example
```sh
./bin/content-transcoder \
    --port 50055 \
    --input 'http://example.com/movie.mkv' \
    --player=true
```

## 📐 Architecture

Content Transcoder is a core component of the streaming pipeline. It retrieves media via the `torrent-http-proxy`, probes it using `content-prober`, and generates HLS playlists and segments that are served to the `web-ui` player.

## ⚖️ License

All rights reserved.

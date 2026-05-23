# Content Transcoder

A robust transcoding service that converts HTTP media streams into HLS (HTTP Live Streaming) on-demand. It features intelligent session management, automatic inactivity shutdown, and integrated VOD playback capabilities.

## 🚀 Key Features

- **On-Demand Transcoding:** Starts transcoding processes only when a user initiates a stream.
- **HLS Segmenting:** Optimized for low-latency playback with flexible preset support.
- **Auto-Cleanup:** Automatically terminates transcoding jobs and cleans up temporary segments after periods of inactivity.
- **Session-Aware:** Tracks active streaming sessions to prevent redundant transcoding of the same resource.

## ⚙️ Configuration

| Variable | Flag | Default |
|----------|------|---------|
| `WEB_PORT` | `--port` | `50055` |
| `PROBE_PORT` | `--probe-port` | `52055` |
| `SOURCE_URL` | `--input` | (Required) |
| `OUTPUT` | `--output` | `out` |
| `CONTENT_PROBER_SERVICE_HOST` | `--content-prober-host` | `127.0.0.1` |
| `CONTENT_PROBER_SERVICE_PORT` | `--content-prober-port` | `50063` |
| `PRESET` | `--preset` | `ultrafast` |

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

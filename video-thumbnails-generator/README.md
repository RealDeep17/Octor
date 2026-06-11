# Video Thumbnails Generator

A utility service that generates image thumbnails and previews from video content on-the-fly using FFmpeg.

## 🚀 Key Features

- **On-the-Fly Generation:** Extract frames from any position in a video stream.
- **Dynamic Offsets:** Specify the exact time offset for the thumbnail using the `offset` query parameter.
- **Preview Support:** Supports generating short video previews if a `length` parameter is provided.
- **S3 Caching:** Optionally caches generated thumbnails in S3 to speed up subsequent requests.

## ⚙️ Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `--host` | `""` | listening host |
| `--port` | `8080` | HTTP listening port |
| `--source-url` | `""` | Default source URL (overridden by `X-Source-Url` header) |

## 🛠 Usage

### Example Request
To generate a thumbnail at 5 minutes into the video:
- **Headers:**
  - `X-Source-Url`: `http://example.com/movie.mkv`
- **Request:** `GET /?offset=300`

## 📐 Architecture

The generator uses a pool of workers to manage FFmpeg processes. It is typically invoked by the `web-ui` or `rest-api` to provide visual previews of torrent content before or during playback.

## ⚖️ License

All rights reserved.

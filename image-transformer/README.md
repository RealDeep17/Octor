# Image Transformer

A utility service for on-the-fly image transformation. It can resize images and convert them to different formats (like WebP) based on request headers and query parameters.

## 🚀 Key Features

- **On-the-Fly Resizing:** Resize images by providing a `width` query parameter.
- **Format Conversion:** Converts images to the requested format based on the file extension (defaults to WebP).
- **Source Flexibility:** Retrieves source images from URLs provided in the `X-Source-Url` header.

## ⚙️ Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `--host` | `""` | listening host |
| `--port` | `8080` | HTTP listening port |
| `--source-url` | `""` | Default source URL (overridden by `X-Source-Url` header) |

## 🛠 Usage

### Example Request
To resize and convert an image to WebP:
- **Headers:**
  - `X-Source-Url`: `https://example.com/large-image.jpg`
- **Request:** `GET /image.webp?width=300`

## 📐 Architecture

Image Transformer uses a pool of transformers to handle requests efficiently. It is typically used as a backend service for the `web-ui` to serve optimized thumbnails and posters.

## ⚖️ License

All rights reserved.

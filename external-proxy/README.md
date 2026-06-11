# External Proxy

A simple utility service that proxies external URLs and data URLs. It is used to bypass CORS restrictions or to serve content from external sources through a unified endpoint.

## 🚀 Key Features

- **URL Proxying:** Proxies standard HTTP/HTTPS URLs.
- **Data URL Support:** Decodes and serves content from `data:` URLs.
- **Base64 Encoding:** Expects the target URL to be base64-encoded in the path.

## ⚙️ Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `--host` | `""` | listening host |
| `--port` | `8080` | HTTP listening port |
| `--probe-port` | `8081` | Probe listening port |

## 🛠 Usage

### Example Request
To proxy `https://example.com/image.png`:
1. Base64 encode the URL: `aHR0cHM6Ly9leGFtcGxlLmNvbS9pbWFnZS5wbmc=`
2. Request: `GET /aHR0cHM6Ly9leGFtcGxlLmNvbS9pbWFnZS5wbmc=/image.png`

## ⚖️ License

All rights reserved.

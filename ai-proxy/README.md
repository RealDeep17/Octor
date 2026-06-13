# AI Proxy

A lightweight Go proxy that translates Anthropic SDK calls to Google Gemini. This allows services (like `web-ui`) to use the Anthropic SDK while actually communicating with Google's Gemini models.

## 🚀 Key Features

- **Protocol Translation:** Seamlessly converts Anthropic's `POST /v1/messages` format to Google Gemini's `generateContent` API.
- **Streaming Support:** Supports Server-Sent Events (SSE) for real-time streaming responses.
- **Tool-Use Translation:** Maps Anthropic tool schemas to Gemini function declarations.
- **Header Passthrough:** Handles prompt-caching and other beta features by stripping or mapping relevant headers.

## ⚙️ Configuration

Configuration is managed via environment variables (usually loaded from `custom.env`).

| Variable | Default | Description |
|----------|---------|-------------|
| `GEMINI_API_KEY` | `""` | **Required.** Your Google AI Studio API key. |
| `AI_RECOMMENDATIONS_MODEL` | `gemini-3.1-flash-lite` | The Gemini model to use. |
| `ANTHROPIC_PROXY_PORT` | `3456` | Port to listen on. |

## 🛠 Usage

### Service Management
Managed via systemd and orchestrated through `run.sh mode`.

```sh
# Check status
sudo systemctl status octor-ai-proxy
```

### Manual Run
```sh
export GEMINI_API_KEY="your-key"
go run ai-proxy/main.go
# or run the compiled binary:
./bin/ai-proxy
```

## 📐 API

The proxy listens on `127.0.0.1:3456` (by default) and expects Anthropic-formatted requests.

- **Endpoint:** `POST /v1/messages`
- **Health Check:** `GET /health`

## ⚖️ License

All rights reserved.

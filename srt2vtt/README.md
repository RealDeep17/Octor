# srt2vtt

The `srt2vtt` service converts SRT subtitles to VTT format on-the-fly.

## Features
- On-the-fly subtitle conversion.
- Automatic input encoding detection and UTF-8 conversion.
- Integrated health probes.

## Configuration

Configuration can be provided via command-line flags or environment variables (recommended to use `custom.env`).

| Flag | Environment Variable | Default | Description |
|------|----------------------|---------|-------------|
| `--host` | `WEB_HOST` | `""` | listening host |
| `--port` | `WEB_PORT` | `8080` | HTTP listening port (Octor default: `50058`) |
| `--probe-port` | `PROBE_PORT` | `8081` | probe listening port (Octor default: `52058`) |

### Standard Octor Ports

When managed by `run.sh`, the following ports are used:
- **HTTP:** `50058`
- **Probe:** `52058`

## Usage Example

```bash
curl -H 'X-Source-Url: https://raw.githubusercontent.com/octor/srt2vtt/master/samples/greek.srt' 'http://localhost:50058'
```

## Build & Run

### Local
```bash
go run .
```

### Docker
```bash
docker build -t octor/srt2vtt .
docker run --rm -p 50058:50058 -p 8081:8081 octor/srt2vtt
```

## Service Management
Managed via `systemd` using the `run.sh mode` script in the project root.

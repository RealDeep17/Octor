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
| `--port` | `PORT` | `50058` | HTTP listening port |
| `--probe-port` | `PROBE_PORT` | `8081` | Probe listening port |

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
Managed via `systemd` using the `switch_mode.sh` script in the project root.

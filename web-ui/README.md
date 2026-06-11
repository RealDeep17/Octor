# web-ui

Some features to mention:

1. Lightweight - less JavaScript code, no frontend frameworks, fewer bytes sent to the client.
2. Based on [octor REST-API](https://github.com/webtor-io/rest-api).

## Roadmap

- [x] Torrent/magnet upload
- [x] Torrent listing
- [x] Direct file download
- [x] Direct folder download as ZIP-archive
- [x] Picture preview
- [x] Audio streaming
- [x] Video streaming
  - [x] Base player
  - [x] Subtitles support
  - [x] OpenSubtitles support
  - [x] Chromecast support
  - [x] Embed control
  - [x] Subtitle uploading support
  - [ ] Subtitle size control
- [x] Authentication
  - [x] Passwordless authentication
  - [x] Google authentication
- [x] Ads and statistic integration support
- [x] Misc
  - [x] Feedback form
  - [x] Allow magnet-url as query string
  - [x] Add dark/light theme switch
- [x] Chrome extension integration
- [x] Embed support
  - [x] Base version
  - [x] Extended version
- [x] Library
  - [x] Torrents
  - [x] Movies
  - [x] Series
  - [x] Watched/Rate
- [x] Content enrichment
    - [x] OMDB
    - [x] Kinopoisk Unofficial
    - [x] TMDB
- [x] Vault
- [x] Discover
  - [x] AI Recommendations
- [x] Integrations 
  - [x] Stremio
  - [x] Stremio Addons
  - [x] WebDAV
  - [x] RealDebrid
  - [x] TorBox
- [x] i18n

| Flag | Environment Variable | Default | Description |
|------|----------------------|---------|-------------|
| `--host` | `WEB_HOST` | `""` | listening host |
| `--port` | `WEB_PORT` | `8080` | HTTP listening port (Octor default: `8082`) |
| `--pprof-port` | `PPROF_PORT` | `8080` | pprof listening port (Octor default: `51081`) |
| `--probe-port` | `PROBE_PORT` | `8081` | probe listening port (Octor default: `52081`) |
| `--octor-rest-api-host` | `REST_API_SERVICE_HOST` | `127.0.0.1` | Octor REST API host |
| `--octor-rest-api-port` | `REST_API_SERVICE_PORT` | `8080` | Octor REST API port |
| `--vault-service-host` | `VAULT_SERVICE_HOST` | `127.0.0.1` | Vault service host |
| `--vault-service-port` | `VAULT_SERVICE_PORT` | `8086` | Vault service port |

### Standard Octor Ports

When managed by `run.sh`, the following ports are used:
- **HTTP:** `8082`
- **Pprof:** `51081`
- **Probe:** `52081`

## Development

```bash
npm install
npm start
```

## Building

```shell
make build
```
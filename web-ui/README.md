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

## Setting up connection to Octor REST-API

The Web UI connects to the Octor REST-API. Ensure the REST-API is running on port `8080`.

Configuration is managed via `custom.env`:
```env
REST_API_SERVICE_HOST=localhost
REST_API_SERVICE_PORT=8080
```

## Usage

The service is managed via `systemd` and configured using `custom.env`.

```bash
# Start the service
./switch_mode.sh production

# Check status
systemctl status octor-web-ui
```

The Web UI listens on port **8082**.

## Development

```bash
npm install
npm start
```

## Building

```shell
make build
```
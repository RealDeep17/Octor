# Magnet2Torrent

A specialized gRPC service for resolving Magnet URIs into full BitTorrent metainfo (.torrent files). It utilizes the BitTorrent DHT network to discover peers and retrieve metadata for any valid infohash.

## 🚀 Key Features

- **Fast Resolution:** Optimized for low-latency infohash discovery.
- **gRPC Interface:** Seamless integration with other Go microservices.
- **Standalone Client:** Includes a utility client for manual testing and validation.

## ⚙️ Configuration

| Variable | Flag | Default |
|----------|------|---------|
| `LISTEN_HOST` | `--host` | `0.0.0.0` |
| `LISTEN_PORT` | `--port` | `50053` |

## 🛠 Usage

### Service Management
Managed via systemd and orchestrated through `run.sh mode`.

```sh
# Check status
sudo systemctl status octor-magnet2torrent
```

### Manual Run (Server)
```sh
./bin/magnet2torrent --port 50053
```

### Manual Run (Client)
```sh
./bin/magnet2torrent-client "magnet:?xt=urn:btih:..."
```

## 📐 Architecture

Magnet2Torrent acts as the first step in the media ingestion pipeline. When a user provides a magnet link, the `rest-api` calls this service to obtain the torrent metadata required by the `torrent-store` and `torrent-web-seeder`.

## ⚖️ License

All rights reserved.

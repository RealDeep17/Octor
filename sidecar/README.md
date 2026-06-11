# Sidecar

A Python-based metadata enrichment service for Octor. it provides an OMDB-compatible API with specialized support for adult content databases (TPDB, StashDB) and intelligent filename parsing.

## 🚀 Key Features

- **Multi-Source Enrichment:** Integrates with ThePornDatabase (TPDB), StashDB, and OMDB.
- **Intelligent Parsing:** Automatically extracts site, date, and name from filenames using sophisticated regex patterns.
- **Fuzzy Matching:** Uses `rapidfuzz` to correlate filenames with database entries even when naming isn't perfect.
- **High Performance:** Features an in-memory LRU cache and parallelized search execution for low-latency responses.
- **OMDB Compatible:** Can act as a drop-in proxy for services expecting the OMDB API.

## ⚙️ Configuration

Configuration is managed via environment variables.

| Variable | Default | Description |
|----------|---------|-------------|
| `TPDB_API_KEY` | `""` | API key for ThePornDatabase. |
| `STASHDB_API_KEY` | `""` | API key for StashDB. |
| `STASHDB_ENDPOINT` | `https://stashdb.org/graphql` | StashDB GraphQL endpoint. |
| `SIDECAR_DATA_DIR` | `./data` | Directory for persistent settings. |
| `SIDECAR_ENRICHMENT_ENABLED` | `true` | Globally enable/disable enrichment. |

## 🛠 Usage

### Service Management
Managed via systemd and orchestrated through `run.sh mode`.

```sh
# Check status
sudo systemctl status octor-sidecar
```

### Manual Run
```sh
# Using venv
./sidecar/venv/bin/uvicorn main:app --host 0.0.0.0 --port 8000
```

## 📐 Architecture

Sidecar is used by the `rest-api` and `web-ui` to resolve rich metadata (posters, descriptions, performers) for media content that might not be available on mainstream databases like TMDB.

## ⚖️ License

All rights reserved.

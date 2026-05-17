# OCTOR VPS MIGRATION BUNDLE (Ubuntu 24.04 / OCI-A1)

This file contains EVERYTHING needed to rebuild Octor on a fresh Ubuntu 24.04 VPS.

---

## 1. Tool & Dependency Mapping

| Tool | macOS | Ubuntu 24.04 VPS (OCI-A1) |
| :--- | :--- | :--- |
| **Shell** | `zsh` | `bash` (Update shebangs to `#!/bin/bash`) |
| **Caddy** | Custom binary | `xcaddy` (Domain: octor.duckdns.org) |
| **Go** | Homebrew | `sudo apt install golang` |
| **Node** | Homebrew | `sudo apt install nodejs npm` |
| **Python venv** | Built-in | `sudo apt install python3-venv` |
| **ffmpeg** | Homebrew | `sudo apt install ffmpeg` |
| **lsof** | Built-in | `sudo apt install lsof` |
| **Docker** | Docker Desktop | `sudo apt install docker.io` |

---

## 2. Infrastructure Installation (Shell Commands)

```bash
# Update and Install Packages
sudo apt update && sudo apt upgrade -y
sudo apt install -y git wget curl build-essential python3-venv python3-pip golang nodejs npm ffmpeg lsof docker.io postgresql-client redis-tools

# Install Caddy
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update && sudo apt install caddy
```

---

## 3. Production Environment (`custom.env`)
**Rebuild this file at `/srv/octor/custom.env`**

```env
# --- CRITICAL SYSTEM FLAGS ---
GOLANG_PROTOBUF_REGISTRATION_CONFLICT=ignore

# --- INFRA ---
POSTGRES_USER=webtor
POSTGRES_PASSWORD=webtor
POSTGRES_DB=webtor
POSTGRES_HOST=127.0.0.1
POSTGRES_PORT=5433
REDIS_HOST=127.0.0.1
REDIS_PORT=6379
NATS_HOST=127.0.0.1
NATS_PORT=4222

# --- STORAGE ---
AWS_ENDPOINT=http://localhost:9000
AWS_REGION=us-east-1
AWS_ACCESS_KEY_ID=octoradmin
AWS_SECRET_ACCESS_KEY=octorpassword
VAULT_AWS_BUCKET=vault
TORRENT_STORE_AWS_BUCKET=torrent-store
AWS_NO_SSL=true

# --- DOMAIN ---
DOMAIN=octor.duckdns.org
OCTOR_DOMAIN=https://octor.duckdns.org
EXTERNAL_URL=https://octor.duckdns.org
EXPORT_USE_SUBDOMAINS=false

# --- METADATA KEYS ---
TMDB_API_KEY=a4b3ed7071fe0ff754c57853bc109acb
TMDB_API_READ_ACCESS_TOKEN=eyJhbGciOiJIUzI1NiJ9.eyJhdWQiOiJhNGIzZWQ3MDcxZmUwZmY3NTRjNTc4NTNiYzEwOWFjYiIsIm5iZiI6MTc3ODkzOTYwMC45MzYsInN1YiI6IjZhMDg3NmQwN2UxMzY4MWJlNzY3OTNjNyIsInNjb3BlcyI6WyJhcGlfcmVhZCJdLCJ2ZXJzaW9uIjoxfQ.ZhsXv2oTQ6KbcK0QXSGNXodaUSJ2UZHrT_oM64CMpBM
OMDB_API_KEY=1799aaa7
STASHDB_API_KEY=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1aWQiOiIwMTlkZmZkYS0yZGVmLTdlN2UtYWQ4Zi0yN2FkZjc1MDI1NmYiLCJzdWIiOiJBUElLZXkiLCJpYXQiOjE3NzgxMTM5ODF9.GgudiUnFvNXpQic158c3QtheEkYY2rTLtEu5PuYn2xY
THEPORNDB_API_KEY=4MODCdLTeVcKDx28wTWiW86sF2IRqlnmVe0XVkGG55696daf
KINOPOISK_UNOFFICIAL_API_KEY=72d643a3-1157-41cd-8b67-33a5ab9bec70
GEMINI_API_KEY=AIzaSyAUbCUn355DY_uyC3EhyDtqlXqZtGYctaQ

# --- ALL MICROSERVICE PORTS ---
PORT_sidecar=8000
PORT_rest-api=8080
PORT_web-ui=8082
PORT_vault=8086
PORT_torrent-store=50051
PORT_torrent-http-proxy=50052
PORT_magnet2torrent=50053
PORT_torrent-web-seeder=50054
PORT_content-transcoder=50055
PORT_video-info=50056
PORT_torrent-archiver=50057
PORT_srt2vtt=50058
PORT_abuse-store=50059
PORT_claims-provider=50060
PORT_url-store=50061
PORT_content-prober=50062
PORT_content-prober-grpc=50063
```

---

## 4. Systemd Units (Execution Logic)

### A. Template Unit (`/etc/systemd/system/octor-@.service`)
Used for: `rest-api`, `web-ui`, `vault`, `abuse-store`, `claims-provider`, `torrent-store`, `url-store`.

```ini
[Unit]
Description=Octor Service %i
After=network.target docker.service

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/%i
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/bin/%i serve --port ${PORT_%i}
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

### B. Direct Unit Overrides (No `serve` subcommand)
**`octor-magnet2torrent.service`**: `ExecStart=/srv/octor/bin/magnet2torrent --port ${PORT_magnet2torrent}`
**`octor-content-transcoder.service`**: `ExecStart=/srv/octor/bin/content-transcoder --port ${PORT_content-transcoder}`
**`octor-video-info.service`**: `ExecStart=/srv/octor/bin/video-info --port ${PORT_video-info}`
**`octor-torrent-web-seeder.service`**: `ExecStart=/srv/octor/bin/torrent-web-seeder --port ${PORT_torrent-web-seeder}`

**`octor-content-prober.service`**:
```ini
[Unit]
Description=Octor Content Prober
After=network.target redis.service

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/content-prober
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/bin/content-prober --port ${PORT_content-prober-grpc} --http-port ${PORT_content-prober} --probe-port 52062 --redis-host localhost --redis-port 6379
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

---

## 5. FULL Port Mapping Table (Single Source of Truth)

| Service | Port | Category |
| :--- | :--- | :--- |
| **Sidecar** | 8000 | Entry (HTTP) |
| **Rest API** | 8080 | Entry (HTTP) |
| **Web UI** | 8082 | Entry (HTTP) |
| **Vault** | 8086 | Entry (HTTP) |
| **Torrent Store** | 50051 | Core (GRPC) |
| **Torrent HTTP Proxy**| 50052 | Edge (HTTP) |
| **Magnet2Torrent** | 50053 | Core (GRPC) |
| **Web Seeder** | 50054 | Content (HTTP) |
| **Transcoder** | 50055 | Content (HTTP) |
| **Video Info** | 50056 | Content (HTTP) |
| **Archiver** | 50057 | Content (HTTP) |
| **SRT2VTT** | 50058 | Content (HTTP) |
| **Abuse Store** | 50059 | Core (GRPC) |
| **Claims Provider** | 50060 | Core (GRPC) |
| **URL Store** | 50061 | Core (GRPC) |
| **Content Prober** | 50062 | Content (HTTP) |
| **Content Prober GRPC**| 50063 | Content (GRPC) |

---

## 6. Build Script (`build_all.sh`)

```bash
#!/bin/bash
mkdir -p /srv/octor/bin
SERVICES=(rest-api web-ui vault abuse-store claims-provider torrent-store url-store video-info torrent-archiver srt2vtt content-transcoder magnet2torrent torrent-web-seeder content-prober)

for SVC in "${SERVICES[@]}"; do
    echo "🔨 Building $SVC..."
    if [ -d "$SVC/server" ]; then cd "$SVC/server"; else cd "$SVC"; fi
    go build -o /srv/octor/bin/$SVC .
    cd - > /dev/null
done

# Python Sidecar
cd sidecar && python3 -m venv venv && ./venv/bin/pip install -r requirements.txt && cd ..
```

---

## 7. Caddyfile & Gotchas Checklist

### Caddyfile (`/etc/caddy/Caddyfile`)
```caddy
octor.duckdns.org {
    route {
        @seeder_services { path /speedtest/download* /webseed* /torrent* /stat* /ext* }
        handle @seeder_services { reverse_proxy localhost:50052 }
        @seeder_content { path_regexp ^/[0-9a-fA-F]{40}/.+; not path */status }
        handle @seeder_content { reverse_proxy localhost:50052 }
        handle { reverse_proxy localhost:8082 }
    }
}
```

### Checklist
- [ ] Initialize Postgres schemas via `init-db.sql`.
- [ ] Create `vault` and `torrent-store` buckets in MinIO.
- [ ] Point Sidecar unit to `sidecar/venv/bin/uvicorn`.
- [ ] Ensure `lsof` and `ffmpeg` are in `$PATH`.

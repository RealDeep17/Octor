#!/bin/zsh

# Octor Hybrid Dev Runner (ARM64 Optimized)
# Runs infra in Docker and services on Host.

# Protobuf conflict ignore
export GOLANG_PROTOBUF_REGISTRATION_CONFLICT=ignore

# 1. Load Environment
if [ -f ../.env ]; then
    export $(grep -v '^#' ../.env | xargs)
    echo "✅ Loaded environment from ../.env"
    export PG_HOST=$POSTGRES_HOST
    export PG_PORT=$POSTGRES_PORT
    export PG_USER=$POSTGRES_USER
    export PG_PASSWORD=$POSTGRES_PASSWORD
    export PG_DATABASE=$POSTGRES_DB
else
    echo "❌ .env file not found in root!"
    exit 1
fi

# 2. Check Infra
echo "🔍 Checking infrastructure..."
docker ps --format "{{.Names}}" | grep -q "octor-postgres" || { echo "❌ Postgres is not running!"; exit 1; }
docker ps --format "{{.Names}}" | grep -q "octor-redis" || { echo "❌ Redis is not running!"; exit 1; }
docker ps --format "{{.Names}}" | grep -q "octor-nats" || { echo "❌ NATS is not running!"; exit 1; }
echo "✅ Infrastructure is UP."

# 3. Cleanup on Exit
cleanup() {
    echo "\n🛑 Shutting down services..."
    kill $(jobs -p) 2>/dev/null
    exit
}
trap cleanup SIGINT SIGTERM

# 4. Start Services
echo "🚀 Starting Octor Services..."

# A. Sidecar (Python)
echo "   -> Starting Sidecar (8000)..."
cd sidecar
if [ ! -d "venv" ]; then
    python3 -m venv venv
    ./venv/bin/pip install -r requirements.txt
fi
./venv/bin/uvicorn main:app --host 0.0.0.0 --port 8000 > sidecar.log 2>&1 &
cd ..

# B. Torrent Store (Go)
echo "   -> Starting Torrent Store (50051)..."
cd torrent-store
go run $GO_FLAGS . serve \
    --grpc-port 50051 \
    --pprof-port 50061 \
    --probe-port 50071 > torrent-store.log 2>&1 &
cd ..

# C. Torrent Web Seeder (Go)
echo "   -> Starting Torrent Web Seeder (50054)..."
cd torrent-web-seeder/server
go run $GO_FLAGS . \
    --port 50054 \
    --pprof-port 50064 \
    --probe-port 50074 \
    --prom-port 50084 > torrent-web-seeder.log 2>&1 &
cd ../..

# D. Magnet2Torrent (Go)
echo "   -> Starting Magnet2Torrent (50053)..."
cd magnet2torrent/server
go run $GO_FLAGS . \
    --port 50053 \
    --pprof-port 50063 \
    --probe-port 50073 > magnet2torrent.log 2>&1 &
cd ../..

# E. Content Transcoder (Go)
echo "   -> Starting Content Transcoder (50055)..."
cd content-transcoder
go run $GO_FLAGS . \
    --port 50055 \
    --pprof-port 50065 \
    --probe-port 50075 > content-transcoder.log 2>&1 &
cd ..

# F. Video Info (Go)
echo "   -> Starting Video Info (50056)..."
cd video-info
go run $GO_FLAGS . \
    --port 50056 \
    --pprof-port 50066 \
    --probe-port 50076 > video-info.log 2>&1 &
cd ..

# G. Torrent Archiver (Go)
echo "   -> Starting Torrent Archiver (50057)..."
cd torrent-archiver
go run $GO_FLAGS . \
    --port 50057 \
    --pprof-port 50067 \
    --probe-port 50077 \
    --prom-port 50087 > torrent-archiver.log 2>&1 &
cd ..

# H. SRT2VTT (Go)
echo "   -> Starting SRT2VTT (50058)..."
cd srt2vtt
go run $GO_FLAGS . \
    --port 50058 \
    --pprof-port 50068 \
    --probe-port 50078 > srt2vtt.log 2>&1 &
cd ..

# I. Torrent HTTP Proxy (Go) - CRITICAL EDGE SERVICE
echo "   -> Starting Torrent HTTP Proxy (50052)..."
cd torrent-http-proxy
export TORRENT_WEB_SEEDER_SERVICE_HOST=127.0.0.1
export TORRENT_WEB_SEEDER_SERVICE_PORT=50054
export CONTENT_TRANSCODER_SERVICE_HOST=127.0.0.1
export CONTENT_TRANSCODER_SERVICE_PORT=50055
export VIDEO_INFO_SERVICE_HOST=127.0.0.1
export VIDEO_INFO_SERVICE_PORT=50056
export TORRENT_ARCHIVER_SERVICE_HOST=127.0.0.1
export TORRENT_ARCHIVER_SERVICE_PORT=50057
export SRT2VTT_SERVICE_HOST=127.0.0.1
export SRT2VTT_SERVICE_PORT=50058
go run $GO_FLAGS . \
    --port 50052 \
    --config services.yaml \
    --pprof-port 50062 \
    --probe-port 50072 \
    --prom-port 50082 > torrent-http-proxy.log 2>&1 &
cd ..

# J. Rest API (Go)
echo "   -> Starting Rest API (8080)..."
cd rest-api
GIN_MODE=release go run $GO_FLAGS . serve \
    --port 8080 \
    --pprof-port 50182 \
    --probe-port 50184 \
    --export-domain https://octor.duckdns.org \
    --torrent-store-host 127.0.0.1 \
    --torrent-store-port 50051 \
    --magnet2torrent-host 127.0.0.1 \
    --magnet2torrent-port 50053 \
    --export-use-subdomains false > rest-api.log 2>&1 &
cd ..

# K. Web UI (Go)
echo "   -> Starting Web UI (8081)..."
export VAULT_STORAGE_PATH=$(pwd)/server/infra-data/minio
mkdir -p $VAULT_STORAGE_PATH
cd web-ui
if [ ! -d "node_modules" ]; then
    echo "      (First run: Installing npm modules...)"
    npm install
fi
if [ ! -d "assets/dist" ]; then
    echo "      (Building assets...)"
    npm run build
fi
GIN_MODE=release go run . serve --port 8081 --pprof-port 50183 --probe-port 50185 --webtor-rest-api-host localhost --webtor-rest-api-port 8080 --vault-service-host localhost --vault-service-port 8086 --vault-storage-path $VAULT_STORAGE_PATH --domain "https://octor.duckdns.org" > web-ui.log 2>&1 &
cd ..

# L. Vault (Go)
echo "   -> Starting Vault (8086)..."
cd vault
GIN_MODE=release go run $GO_FLAGS . serve \
    --port 8086 \
    --pprof-port 8087 \
    --probe-port 8088 \
    --prom-port 8089 \
    --webtor-rest-api-host localhost \
    --webtor-rest-api-port 8080 \
    --s3-endpoint http://localhost:9000 \
    --s3-access-key-id octoradmin \
    --s3-secret-access-key octorpassword \
    --s3-bucket vault \
    --s3-region us-east-1 > vault.log 2>&1 &
cd ..

# M. Reverse Proxy (Caddy with DuckDNS plugin)
echo "   -> Starting Custom Caddy Reverse Proxy (needs sudo for 80/443)..."
export DuckDNS_Token="2b8b9c5c-005e-418b-8449-3c75e9bd727d"
sudo ./caddy-custom stop > /dev/null 2>&1
sudo ./caddy-custom start --config Caddyfile > caddy.log 2>&1

# N. DuckDNS IP Auto-Updater
echo "   -> Starting DuckDNS IP Auto-Updater..."
(
    while true; do
        curl -s "https://www.duckdns.org/update?domains=octor&token=2b8b9c5c-005e-418b-8449-3c75e9bd727d&ip=" > /dev/null
        sleep 300
    done
) &

echo "🎉 All services starting in background!"
echo "   - Secure URL: https://absorption-chief-slowly-dining.trycloudflare.com"
echo "   - External UI: https://octor.duckdns.org"
echo "   - Internal Web UI: http://localhost:8081"
echo "   - Rest API: http://localhost:8080"
echo "   - Sidecar: http://localhost:8000"
echo "   - Vault: http://localhost:8086"
echo "Check logs (*.log) for output."

# Keep script alive
wait

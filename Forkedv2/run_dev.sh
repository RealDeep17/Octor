#!/bin/zsh

# Octor Hybrid Dev Runner (ARM64 Optimized)
# Runs infra in Docker and services on Host.

# Protobuf conflict ignore flag
GO_FLAGS="-ldflags=-X=google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=ignore"

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
echo "   -> Starting Torrent Web Seeder (50052)..."
cd torrent-web-seeder/server
go run $GO_FLAGS . \
    --port 50052 \
    --pprof-port 50062 \
    --probe-port 50072 \
    --prom-port 50082 > torrent-web-seeder.log 2>&1 &
cd ../..

# D. Magnet2Torrent (Go)
echo "   -> Starting Magnet2Torrent (50053)..."
cd magnet2torrent/server
go run $GO_FLAGS . \
    --port 50053 \
    --pprof-port 50063 \
    --probe-port 50073 > magnet2torrent.log 2>&1 &
cd ../..

# E. Rest API (Go)
echo "   -> Starting Rest API (8080)..."
cd rest-api
GIN_MODE=release go run $GO_FLAGS . serve \
    --port 8080 \
    --pprof-port 8082 \
    --probe-port 8084 \
    --export-domain http://localhost:50052 \
    --torrent-store-host 127.0.0.1 \
    --torrent-store-port 50051 \
    --magnet2torrent-host 127.0.0.1 \
    --magnet2torrent-port 50053 \
    --export-use-subdomains false > rest-api.log 2>&1 &
cd ..

# F. Web UI (Go)
echo "   -> Starting Web UI (8081)..."
cd web-ui
if [ ! -d "node_modules" ]; then
    echo "      (First run: Installing npm modules...)"
    npm install
fi
if [ ! -d "assets/dist" ]; then
    echo "      (Building assets...)"
    npm run build
fi
GIN_MODE=release go run $GO_FLAGS . serve \
    --port 8081 \
    --pprof-port 8083 \
    --probe-port 8085 \
    --webtor-rest-api-host localhost \
    --webtor-rest-api-port 8080 \
    --vault-service-host localhost \
    --vault-service-port 8086 \
    --domain $EXTERNAL_URL > web-ui.log 2>&1 &
cd ..

# G. Vault (Go)
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

# H. Reverse Proxy (Caddy with DuckDNS plugin)
echo "   -> Starting Custom Caddy Reverse Proxy..."
export DuckDNS_Token="2b8b9c5c-005e-418b-8449-3c75e9bd727d"
./caddy-custom stop > /dev/null 2>&1
./caddy-custom start --config Caddyfile > caddy.log 2>&1

# I. DuckDNS IP Auto-Updater
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

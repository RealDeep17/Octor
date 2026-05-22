#!/bin/bash

# Octor Hybrid Dev Runner (ARM64 Optimized)
# Runs infra in Docker and services on Host.

# Protobuf conflict ignore (CRITICAL for mixing microservices)
export GOLANG_PROTOBUF_REGISTRATION_CONFLICT=ignore

# 0. Prepare Logs
mkdir -p logs
rm -f logs/*.log > /dev/null 2>&1 || true

# 1. Load Environment
if [ -f custom.env ]; then
    set -a
    source custom.env
    set +a
    echo "✅ Loaded environment from custom.env"
    export OCTOR_DOMAIN=${OCTOR_DOMAIN:-http://localhost:8081}
    export OCTOR_HOST=$(echo $OCTOR_DOMAIN | sed -e 's|^[^/]*//||' -e 's|/.*$||')
    export PG_HOST=$POSTGRES_HOST
    export PG_PORT=$POSTGRES_PORT
    export PG_USER=$POSTGRES_USER
    export PG_PASSWORD=$POSTGRES_PASSWORD
    export PG_DATABASE=$POSTGRES_DB
else
    echo "❌ custom.env file not found! Please copy example.env to custom.env"
    exit 1
fi

# 2. Check Infra
if [[ "$1" == "--force" ]]; then
    echo "⚠️  Forcing startup: Skipping infrastructure checks..."
else
    echo "🔍 Checking infrastructure..."
    docker ps --format "{{.Names}}" | grep -q "octor-postgres" || { echo "❌ Postgres is not running! (Use --force to skip check)"; exit 1; }
    docker ps --format "{{.Names}}" | grep -q "octor-redis" || { echo "❌ Redis is not running! (Use --force to skip check)"; exit 1; }
    docker ps --format "{{.Names}}" | grep -q "octor-nats" || { echo "❌ NATS is not running! (Use --force to skip check)"; exit 1; }
    docker ps --format "{{.Names}}" | grep -q "octor-minio" || { echo "❌ MinIO is not running! (Use --force to skip check)"; exit 1; }
    echo "✅ Infrastructure is UP."
fi

# 3. Cleanup existing processes
echo "🧹 Cleaning up existing Octor processes..."
# Kill by port ranges
ports=(8000 8080 8081 8086)
for port in $ports; do
    lsof -ti tcp:$port | xargs kill -9 2>/dev/null
done
# Kill GRPC and internal ports (50000-53999)
lsof -ti tcp:50000-53999 | xargs kill -9 2>/dev/null

cleanup() {
    echo "\n🛑 Shutting down services..."
    kill $(jobs -p) 2>/dev/null
    exit
}
trap cleanup SIGINT SIGTERM

# 4. Start Services
echo "🚀 Starting Octor Services with Unified Port Mapping..."

# --- AI PROXY (must start before web-ui) ---
echo "   -> Starting Anthropic→Gemini Proxy (3456)..."
node ai-proxy/proxy.js > logs/ai-proxy.log 2>&1 &
sleep 1  # give it a moment to bind before web-ui starts

# --- 8xxx RANGE: Entry Points ---

# A. Sidecar (Python)
echo "   -> Starting Sidecar (8000)..."
cd sidecar
if [ ! -d "venv" ]; then
    python3 -m venv venv
    ./venv/bin/pip install -r requirements.txt
fi
./venv/bin/uvicorn main:app --host 0.0.0.0 --port 8000 > ../logs/sidecar.log 2>&1 &
cd ..

# J. Rest API (Go)
echo "   -> Starting Rest API (8080)..."
cd rest-api
GIN_MODE=release go run $GO_FLAGS . serve \
    --port 8080 \
    --pprof-port 51080 \
    --probe-port 52080 \
    --export-domain ${OCTOR_DOMAIN} \
    --torrent-store-host 127.0.0.1 \
    --torrent-store-port 50051 \
    --magnet2torrent-host 127.0.0.1 \
    --magnet2torrent-port 50053 \
    --video-info-host 127.0.0.1 \
    --video-info-port 50056 \
    --export-use-subdomains false > ../logs/rest-api.log 2>&1 &
cd ..

# C. Web UI (8082)
echo "   -> Starting Web UI (8082)..."
cd web-ui
if [ ! -d "node_modules" ]; then
    npm install
fi
if [ ! -d "assets/dist" ]; then
    npm run build
fi
GIN_MODE=release go run . serve \
    --port 8082 \
    --pprof-port 51081 \
    --probe-port 52081 \
    --webtor-rest-api-host localhost \
    --webtor-rest-api-port 8080 \
    --vault-service-host localhost \
    --vault-service-port 8086 \
    --use-internal-torrent-http-proxy=true \
    --torrent-http-proxy-host localhost \
    --torrent-http-proxy-port 50052 \
    --domain "${OCTOR_DOMAIN:-http://localhost:8081}" > ../logs/web-ui.log 2>&1 &
cd ..

# D. Vault (8086)
echo "   -> Starting Vault (8086)..."
cd vault
GIN_MODE=release go run . serve \
    --port 8086 \
    --pprof-port 51086 \
    --probe-port 52086 \
    --prom-port 53086 \
    --webtor-rest-api-host localhost \
    --webtor-rest-api-port 8080 \
    --aws-endpoint ${AWS_ENDPOINT} \
    --aws-region ${AWS_REGION} \
    --aws-access-key-id ${AWS_ACCESS_KEY_ID} \
    --aws-secret-access-key ${AWS_SECRET_ACCESS_KEY} \
    --aws-bucket ${VAULT_AWS_BUCKET} \
    --aws-no-ssl \
    --max-concurrent-jobs ${VAULT_MAX_CONCURRENT_JOBS:-3} \
    --postgres-database vault > ../logs/vault.log 2>&1 &
cd ..

# --- 500xx RANGE: Core GRPC Services ---

# E. Abuse Store (50059)
echo "   -> Starting Abuse Store (50059)..."
cd abuse-store
go run . serve \
    --grpc-port 50059 \
    --pprof-port 51059 \
    --probe-port 52059 \
    --postgres-database abuse_store > ../logs/abuse-store.log 2>&1 &
cd ..

# F. Claims Provider (50060)
echo "   -> Starting Claims Provider (50060)..."
cd claims-provider
go run . serve \
    --grpc-port 50060 \
    --pprof-port 51060 \
    --probe-port 52060 \
    --prom-port 53060 \
    --postgres-database claims_provider > ../logs/claims-provider.log 2>&1 &
cd ..

# G. Torrent Store (50051)
echo "   -> Starting Torrent Store (50051)..."
cd torrent-store
go run . serve \
    --grpc-port 50051 \
    --pprof-port 51051 \
    --probe-port 52051 \
    --abuse-host 127.0.0.1 \
    --abuse-port 50059 \
    --use-abuse \
    --use-s3 \
    --aws-endpoint ${AWS_ENDPOINT} \
    --aws-access-key-id ${AWS_ACCESS_KEY_ID} \
    --aws-secret-access-key ${AWS_SECRET_ACCESS_KEY} \
    --aws-bucket ${TORRENT_STORE_AWS_BUCKET} \
    --aws-region ${AWS_REGION} \
    --aws-no-ssl > ../logs/torrent-store.log 2>&1 &
cd ..

# H. Magnet2Torrent (50053)
echo "   -> Starting Magnet2Torrent (50053)..."
cd magnet2torrent/server
go run . \
    --port 50053 \
    --pprof-port 51053 \
    --probe-port 52053 > ../../logs/magnet2torrent.log 2>&1 &
cd ../..

# I. URL Store (50061)
echo "   -> Starting URL Store (50061)..."
cd url-store
go run . serve \
    --port 54061 \
    --grpc-port 50061 \
    --probe-port 52061 \
    --postgres-database url_store > ../logs/url-store.log 2>&1 &
cd ..

# J. Torrent Web Seeder (50054)
echo "   -> Starting Torrent Web Seeder (50054)..."
cd torrent-web-seeder/server
go run . \
    --port 50054 \
    --pprof-port 51054 \
    --probe-port 52054 \
    --prom-port 53054 > ../../logs/torrent-web-seeder.log 2>&1 &
cd ../..

# J2. Torrent Web Seeder Cleaner
echo "   -> Starting Torrent Web Seeder Cleaner..."
cd torrent-web-seeder-cleaner
go run . serve > ../logs/torrent-web-seeder-cleaner.log 2>&1 &
cd ..

# K. Content Transcoder (50055)
echo "   -> Starting Content Transcoder (50055)..."
cd content-transcoder
go run . \
    --port 50055 \
    --pprof-port 51055 \
    --probe-port 52055 > ../logs/content-transcoder.log 2>&1 &
cd ..

# L. Video Info (50056)
echo "   -> Starting Video Info (50056)..."
cd video-info
go run . \
    --port 50056 \
    --probe-port 52056 \
    --redis-host localhost \
    --redis-port 6379 > ../logs/video-info.log 2>&1 &
cd ..

# M. Torrent Archiver (50057)
echo "   -> Starting Torrent Archiver (50057)..."
cd torrent-archiver
go run . \
    --port 50057 \
    --pprof-port 51057 \
    --probe-port 52057 \
    --torrent-store-host localhost \
    --torrent-store-port 50051 \
    --prom-port 53057 > ../logs/torrent-archiver.log 2>&1 &
cd ..

# N. SRT2VTT (50058)
echo "   -> Starting SRT2VTT (50058)..."
cd srt2vtt
go run . \
    --port 50058 \
    --probe-port 52058 > ../logs/srt2vtt.log 2>&1 &
cd ..
 
# Q. Content Prober (50062)
echo "   -> Starting Content Prober (50062)..."
cd content-prober/server
go run . \
    --port 50063 \
    --http-port 50062 \
    --probe-port 52062 \
    --redis-host localhost \
    --redis-port 6379 > ../../logs/content-prober.log 2>&1 &
cd ../..

# O. Torrent HTTP Proxy (50052) - CRITICAL EDGE SERVICE
echo "   -> Starting Torrent HTTP Proxy (50052)..."
cd torrent-http-proxy
go run . \
    --port 50052 \
    --torrent-http-proxy-host 127.0.0.1 \
    --torrent-http-proxy-port 50052 \
    --pprof-port 51052 \
    --probe-port 52052 \
    --config services.yaml > ../logs/torrent-http-proxy.log 2>&1 &
cd ..

# --- INFRA & PROXY ---

# P. Reverse Proxy (Caddy)
echo "   -> Starting Custom Caddy..."
export DuckDNS_Token="${DUCKDNS_TOKEN}"
echo "${SUDO_PASSWORD}" | sudo -S -E ./caddy-custom stop > /dev/null 2>&1
echo "${SUDO_PASSWORD}" | sudo -S -E ./caddy-custom run --config Caddyfile > logs/caddy.log 2>&1 &

# Q. DuckDNS IP Auto-Updater (DISABLED to prevent changing octor.duckdns.org IP)
# (
#     while true; do
#         curl -s "https://www.duckdns.org/update?domains=octor&token=${DUCKDNS_TOKEN}&ip=" > /dev/null
#         sleep 300
#     done
# ) &

echo "\n🎉 All services starting with non-conflicting ports!"
echo "   - Primary: ${OCTOR_DOMAIN}"
echo "   - Rest API: http://localhost:8080"
echo "   - Sidecar: http://localhost:8000"
echo "Check logs (*.log) for output."

# Keep script alive
wait

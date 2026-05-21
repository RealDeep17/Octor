#!/bin/bash
# ==============================================================================
# OCTOR UNIVERSAL PERFORMANCE MODE SWITCHER (FIXED)
# ==============================================================================
# Usage: ./switch_mode.sh <mode>
# ==============================================================================

set -euo pipefail

MODE="${1:-}"

if [[ -z "$MODE" ]]; then
    echo "===================================================="
    echo "   OCTOR UNIVERSAL PERFORMANCE MODE SELECTOR"
    echo "===================================================="
    echo "Please choose a performance configuration mode:"
    echo ""
    echo "RAM Disk Staging Modes (Extremely Fast, Zero SSD Wear):"
    echo "  1) sp-perf-ram  (Direct Stream, High Performance - Recommended)"
    echo "  2) sp-norm-ram  (Direct Stream, Normal Configuration)"
    echo "  3) sp-eco-ram   (Direct Stream, Low-Resource Eco Mode)"
    echo "  4) pp-perf-ram  (Parallel Path Chunker, High Performance)"
    echo "  5) pp-norm-ram  (Parallel Path Chunker, Normal Configuration)"
    echo ""
    echo "SSD Staging Modes (Persistent Cache, Survives Reboots):"
    echo "  6) sp-perf-ssd  (Direct Stream SSD, High Performance)"
    echo "  7) sp-norm-ssd  (Direct Stream SSD, Normal Configuration)"
    echo "  8) sp-eco-ssd   (Direct Stream SSD, Low-Resource Eco Mode)"
    echo "  9) pp-perf-ssd  (Parallel Path Chunker SSD, High Performance)"
    echo " 10) pp-norm-ssd  (Parallel Path Chunker SSD, Normal Configuration)"
    echo ""
    
    # Prompt the user for selection
    read -p "Select option (1-10) [1]: " CHOICE
    CHOICE="${CHOICE:-1}"
    
    case "$CHOICE" in
        1) MODE="sp-perf-ram" ;;
        2) MODE="sp-norm-ram" ;;
        3) MODE="sp-eco-ram" ;;
        4) MODE="pp-perf-ram" ;;
        5) MODE="pp-norm-ram" ;;
        6) MODE="sp-perf-ssd" ;;
        7) MODE="sp-norm-ssd" ;;
        8) MODE="sp-eco-ssd" ;;
        9) MODE="pp-perf-ssd" ;;
        10) MODE="pp-norm-ssd" ;;
        *)
            echo "❌ Error: Invalid selection."
            exit 1
            ;;
    esac
    echo "✓ Selected mode: $MODE"
fi

ENV_FILE="/srv/octor/custom.env"
if [ ! -f "$ENV_FILE" ]; then
    echo "❌ Error: $ENV_FILE not found!"
    exit 1
fi

run_sudo() {
    # We assume the user has sudo rights or SUDO_PASSWORD is in env
    sudo "$@"
}

# ---------------------------------------------------------------------------
# Helper: wait for a systemd service to reach 'active' state.
# Usage: wait_for_service <unit> <timeout_seconds>
# ---------------------------------------------------------------------------
wait_for_service() {
    local svc="$1"
    local timeout="${2:-30}"
    local elapsed=0
    while [ $elapsed -lt $timeout ]; do
        local state
        state=$(systemctl is-active "$svc" 2>/dev/null || echo "unknown")
        if [ "$state" = "active" ]; then
            return 0
        fi
        sleep 1
        elapsed=$((elapsed + 1))
    done
    echo "⚠️  Service $svc did not reach 'active' within ${timeout}s (state: $(systemctl is-active "$svc" 2>/dev/null))"
    return 1
}

# ---------------------------------------------------------------------------
# Helper: start a service with up to MAX_RETRIES restart attempts.
# Usage: start_service_with_retry <unit> [max_retries] [wait_per_attempt]
# ---------------------------------------------------------------------------
start_service_with_retry() {
    local svc="$1"
    local max_retries="${2:-3}"
    local wait_secs="${3:-10}"
    local attempt=1
    while [ $attempt -le $max_retries ]; do
        echo -n "  Starting $svc (attempt $attempt/$max_retries)... "
        run_sudo systemctl restart "$svc" 2>/dev/null || true
        if wait_for_service "$svc" "$wait_secs"; then
            echo "✓ running"
            return 0
        fi
        echo "✗ not ready — checking logs:"
        run_sudo journalctl -u "$svc" -n 10 --no-pager 2>/dev/null | sed 's/^/    /'
        attempt=$((attempt + 1))
    done
    echo "❌ $svc failed to start after $max_retries attempts."
    return 1
}

# ---------------------------------------------------------------------------
# Helper: probe a URL, restart the owning service if it fails, retry loop.
# Usage: probe_with_restart <url> <owning_service> [max_rounds] [round_pause]
# ---------------------------------------------------------------------------
probe_with_restart() {
    local url="$1"
    local owner_svc="$2"
    local max_rounds="${3:-5}"
    local pause="${4:-10}"
    local round=1
    while [ $round -le $max_rounds ]; do
        echo -n "  Probe $url (round $round/$max_rounds)... "
        if curl -sf --max-time 5 "$url" > /dev/null 2>&1; then
            echo "✓ OK"
            return 0
        fi
        echo "⚠️  FAILED"
        if [ $round -lt $max_rounds ]; then
            # Check if the service has actually crashed vs just warming up
            local svc_state
            svc_state=$(systemctl is-active "$owner_svc" 2>/dev/null || true)
            if [ "$svc_state" = "active" ] || [ "$svc_state" = "activating" ]; then
                # Service is running or starting up, just wait
                echo "  ⏳  $owner_svc is $svc_state, waiting ${pause}s..."
            else
                # Service has crashed or is inactive — restart it
                echo "  ↺  $owner_svc is $svc_state, restarting and waiting ${pause}s..."
                run_sudo systemctl restart "$owner_svc" 2>/dev/null || true
            fi
            sleep "$pause"
        fi
        round=$((round + 1))
    done
    echo "❌ $url still unreachable after $max_rounds rounds — ${owner_svc} logs:"
    run_sudo journalctl -u "$owner_svc" -n 20 --no-pager 2>/dev/null | sed 's/^/    /'
    return 1
}

# ---------------------------------------------------------------------------
# Helper: block until a FUSE mount is live, with timeout + optional restart.
# Usage: wait_for_mount <mountpoint> <timeout_seconds> [rclone_service]
# ---------------------------------------------------------------------------
wait_for_mount() {
    local mnt="$1"
    local timeout="${2:-60}"
    local rclone_svc="${3:-}"
    local elapsed=0
    echo -n "  Waiting for mount at $mnt"
    while [ $elapsed -lt $timeout ]; do
        if mountpoint -q "$mnt" 2>/dev/null; then
            echo " ✓ mounted (${elapsed}s)"
            return 0
        fi
        printf "."
        sleep 2
        elapsed=$((elapsed + 2))
    done
    echo " ⚠️  NOT mounted after ${timeout}s"
    if [ -n "$rclone_svc" ]; then
        echo "  ↺  Restarting $rclone_svc and waiting another ${timeout}s..."
        run_sudo systemctl restart "$rclone_svc" 2>/dev/null || true
        sleep 2
        elapsed=0
        while [ $elapsed -lt $timeout ]; do
            if mountpoint -q "$mnt" 2>/dev/null; then
                echo "  ✓ mounted after restart (${elapsed}s)"
                return 0
            fi
            sleep 2
            elapsed=$((elapsed + 2))
        done
        echo "❌ Mount at $mnt still absent after restart — rclone logs:"
        run_sudo journalctl -u "$rclone_svc" -n 20 --no-pager 2>/dev/null | sed 's/^/    /'
    fi
    return 1
}

# ---------------------------------------------------------------------------
# Helper: ensure Docker infra containers (postgres, redis, nats) are running.
# These must be up before any app-layer service can connect to them.
# ---------------------------------------------------------------------------
ensure_infra_containers() {
    local containers=("octor-postgres" "octor-redis" "octor-nats")
    local any_started=false
    for ctr in "${containers[@]}"; do
        local state
        state=$(docker inspect -f '{{.State.Status}}' "$ctr" 2>/dev/null || echo "missing")
        if [ "$state" != "running" ]; then
            echo -n "  Starting $ctr (was: $state)... "
            docker start "$ctr" > /dev/null 2>&1 && echo "✓" || echo "❌ FAILED to start $ctr"
            any_started=true
        else
            echo "  $ctr already running ✓"
        fi
    done
    # Always wait for postgres to accept connections
    echo -n "  Waiting for postgres (127.0.0.1:5433)"
    local elapsed=0
    while [ $elapsed -lt 30 ]; do
        if docker exec octor-postgres pg_isready -U webtor -q 2>/dev/null; then
            echo " ✓ ready (${elapsed}s)"
            return 0
        fi
        printf "."
        sleep 1
        elapsed=$((elapsed + 1))
    done
    echo ""
    echo "❌ postgres not ready after 30s — vault and rest-api will fail!"
    return 1
}

# Mapping Logic
case "$MODE" in
    pp-perf-ram|perf|ram-perf|chunker-vfs-ram|ram|ram-chunker)
        MODE="chunker-vfs-ram"; RAM_CACHE_ENABLED=true; BUDGET="2000M"; VFS_LIMIT="3G"; VFS_CACHE_MODE="writes"
        STORAGE_PATH="/srv/octor/infra-data/drive-mount"; USE_CHUNKER=true; POLL_INTERVAL="1s"; CHUNK_SIZE="256M"
        CONCURRENCY=4; RCLONE_BUFFER="64M"; RCLONE_DRIVE_CHUNK="128M"; RCLONE_XFERS=4
        ;;
    pp-norm-ram)
        MODE="chunker-vfs-ram"; RAM_CACHE_ENABLED=true; BUDGET="1500M"; VFS_LIMIT="2G"; VFS_CACHE_MODE="writes"
        STORAGE_PATH="/srv/octor/infra-data/drive-mount"; USE_CHUNKER=true; POLL_INTERVAL="1s"; CHUNK_SIZE="256M"
        CONCURRENCY=4; RCLONE_BUFFER="32M"; RCLONE_DRIVE_CHUNK="64M"; RCLONE_XFERS=4
        ;;
    sp-perf-ram|steady|ram-steady|non-chunker-vfs-ram|ram-vfs)
        MODE="non-chunker-vfs-ram"; RAM_CACHE_ENABLED=true; BUDGET="2000M"; VFS_LIMIT="off"; VFS_CACHE_MODE="off"
        STORAGE_PATH="/srv/octor/infra-data/drive-mount-vfs"; USE_CHUNKER=false; POLL_INTERVAL="1h"; CHUNK_SIZE=""
        CONCURRENCY=1; RCLONE_BUFFER="128M"; RCLONE_DRIVE_CHUNK="256M"; RCLONE_XFERS=2
        ;;
    sp-norm-ram)
        MODE="non-chunker-vfs-ram"; RAM_CACHE_ENABLED=true; BUDGET="1500M"; VFS_LIMIT="off"; VFS_CACHE_MODE="off"
        STORAGE_PATH="/srv/octor/infra-data/drive-mount-vfs"; USE_CHUNKER=false; POLL_INTERVAL="1h"; CHUNK_SIZE=""
        CONCURRENCY=1; RCLONE_BUFFER="64M"; RCLONE_DRIVE_CHUNK="128M"; RCLONE_XFERS=2
        ;;
    sp-eco-ram|pp-eco-ram|eco|ram-eco|eco-ram)
        MODE="non-chunker-vfs-ram"; RAM_CACHE_ENABLED=true; BUDGET="1000M"; VFS_LIMIT="off"; VFS_CACHE_MODE="off"
        STORAGE_PATH="/srv/octor/infra-data/drive-mount-vfs"; USE_CHUNKER=false; POLL_INTERVAL="1h"; CHUNK_SIZE=""
        CONCURRENCY=1; RCLONE_BUFFER="32M"; RCLONE_DRIVE_CHUNK="64M"; RCLONE_XFERS=2
        ;;
    pp-perf-ssd|ssd-perf|chunker-vfs-ssd|ssd|ssd-chunker)
        MODE="chunker-vfs-ssd"; RAM_CACHE_ENABLED=false; BUDGET="2000M"; VFS_LIMIT="8G"; VFS_CACHE_MODE="writes"
        STORAGE_PATH="/srv/octor/infra-data/drive-mount"; USE_CHUNKER=true; POLL_INTERVAL="10s"; CHUNK_SIZE="1G"
        CONCURRENCY=4; RCLONE_BUFFER="64M"; RCLONE_DRIVE_CHUNK="128M"; RCLONE_XFERS=4
        ;;
    pp-norm-ssd)
        MODE="chunker-vfs-ssd"; RAM_CACHE_ENABLED=false; BUDGET="1500M"; VFS_LIMIT="4G"; VFS_CACHE_MODE="writes"
        STORAGE_PATH="/srv/octor/infra-data/drive-mount"; USE_CHUNKER=true; POLL_INTERVAL="10s"; CHUNK_SIZE="512M"
        CONCURRENCY=4; RCLONE_BUFFER="32M"; RCLONE_DRIVE_CHUNK="64M"; RCLONE_XFERS=4
        ;;
    sp-perf-ssd|ssd-steady|non-chunker-vfs-ssd|ssd-vfs)
        MODE="non-chunker-vfs-ssd"; RAM_CACHE_ENABLED=false; BUDGET="2000M"; VFS_LIMIT="off"; VFS_CACHE_MODE="off"
        STORAGE_PATH="/srv/octor/infra-data/drive-mount-vfs"; USE_CHUNKER=false; POLL_INTERVAL="1h"; CHUNK_SIZE=""
        CONCURRENCY=1; RCLONE_BUFFER="128M"; RCLONE_DRIVE_CHUNK="256M"; RCLONE_XFERS=2
        ;;
    sp-norm-ssd)
        MODE="non-chunker-vfs-ssd"; RAM_CACHE_ENABLED=false; BUDGET="1500M"; VFS_LIMIT="off"; VFS_CACHE_MODE="off"
        STORAGE_PATH="/srv/octor/infra-data/drive-mount-vfs"; USE_CHUNKER=false; POLL_INTERVAL="1h"; CHUNK_SIZE=""
        CONCURRENCY=1; RCLONE_BUFFER="64M"; RCLONE_DRIVE_CHUNK="128M"; RCLONE_XFERS=2
        ;;
    sp-eco-ssd|pp-eco-ssd|ssd-eco)
        MODE="non-chunker-vfs-ssd"; RAM_CACHE_ENABLED=false; BUDGET="1000M"; VFS_LIMIT="off"; VFS_CACHE_MODE="off"
        STORAGE_PATH="/srv/octor/infra-data/drive-mount-vfs"; USE_CHUNKER=false; POLL_INTERVAL="1h"; CHUNK_SIZE=""
        CONCURRENCY=1; RCLONE_BUFFER="32M"; RCLONE_DRIVE_CHUNK="64M"; RCLONE_XFERS=2
        ;;
    *)
        echo "❌ Error: Invalid mode '$MODE'."; exit 1 ;;
esac

echo "=== 1. STOPPING ALL OCTOR SERVICES ==="
run_sudo systemctl stop \
    octor-web-ui octor-ai-proxy octor-sidecar octor-claims-provider \
    octor-torrent-http-proxy octor-content-prober octor-content-transcoder \
    octor-srt2vtt octor-torrent-archiver octor-torrent-web-seeder-cleaner \
    octor-torrent-web-seeder octor-vault octor-s3-gateway octor-rest-api \
    octor-magnet2torrent octor-video-info octor-url-store octor-abuse-store \
    octor-torrent-store \
    octor-rclone-chunker octor-rclone-mount octor-seeder-cache \
    2>/dev/null || true
echo "✓ Services stopped."

echo "=== 2. SAFE UNMOUNT OF STORAGE PATHS ==="
for path in "/srv/octor/infra-data/drive-mount" "/srv/octor/infra-data/drive-mount-vfs"; do
    if mountpoint -q "$path"; then
        echo "Unmounting $path..."
        run_sudo fusermount -uz "$path" || true
    fi
done

echo "=== 3. UPDATING ENVIRONMENT (custom.env) ==="
run_sudo sed -i "s|^RAM_CACHE_ENABLED=.*|RAM_CACHE_ENABLED=$RAM_CACHE_ENABLED|" "$ENV_FILE"
run_sudo sed -i "s|^PER_TORRENT_CACHE_BUDGET=.*|PER_TORRENT_CACHE_BUDGET=$BUDGET|" "$ENV_FILE"
run_sudo sed -i "s|^RCLONE_VFS_CACHE_MAX_SIZE=.*|RCLONE_VFS_CACHE_MAX_SIZE=$VFS_LIMIT|" "$ENV_FILE"
run_sudo sed -i "s|^RCLONE_VFS_CACHE_POLL_INTERVAL=.*|RCLONE_VFS_CACHE_POLL_INTERVAL=$POLL_INTERVAL|" "$ENV_FILE"
run_sudo sed -i "s|^RCLONE_VFS_CACHE_MODE=.*|RCLONE_VFS_CACHE_MODE=$VFS_CACHE_MODE|" "$ENV_FILE"
run_sudo sed -i "s|^VAULT_STORAGE_PATH=.*|VAULT_STORAGE_PATH=$STORAGE_PATH|" "$ENV_FILE"
run_sudo sed -i "s|^S3_GATEWAY_STORAGE_DIR=.*|S3_GATEWAY_STORAGE_DIR=$STORAGE_PATH|" "$ENV_FILE"
run_sudo sed -i "s|^OCTOR_PERFORMANCE_MODE=.*|OCTOR_PERFORMANCE_MODE=$MODE|" "$ENV_FILE"
run_sudo sed -i "s|^AWS_UPLOAD_CONCURRENCY=.*|AWS_UPLOAD_CONCURRENCY=$CONCURRENCY|" "$ENV_FILE"
run_sudo sed -i "s|^RCLONE_BUFFER_SIZE=.*|RCLONE_BUFFER_SIZE=$RCLONE_BUFFER|" "$ENV_FILE"
run_sudo sed -i "s|^RCLONE_DRIVE_CHUNK_SIZE=.*|RCLONE_DRIVE_CHUNK_SIZE=$RCLONE_DRIVE_CHUNK|" "$ENV_FILE"
run_sudo sed -i "s|^RCLONE_TRANSFERS=.*|RCLONE_TRANSFERS=$RCLONE_XFERS|" "$ENV_FILE"

echo "=== 4. WIPING LOCAL METADATA CACHE (BADGER) ==="
run_sudo rm -rf /srv/octor/torrent-store/badger-data/* || true

echo "=== 5. ORCHESTRATING STORAGE CACHE (systemd-driven) ==="
# Instead of manual mount logic here, we rely on the seeder-cache service 
# which I've already optimized to handle SSD wipes and RAM image creation.
run_sudo systemctl stop octor-seeder-cache || true
start_service_with_retry "octor-seeder-cache" 3 60

echo "=== 6. RESTARTING ALL OCTOR MICROSERVICES ==="
if [ "$USE_CHUNKER" = "true" ]; then
    echo "Updating chunker size in rclone.conf to $CHUNK_SIZE..."
    sed -i "s/chunk_size = .*/chunk_size = ${CHUNK_SIZE}/g" /home/ubuntu/.config/rclone/rclone.conf
fi

run_sudo systemctl daemon-reload

# --- Storage layer: start rclone mount and block until it is truly mounted ---
# NOTE: octor-rclone-mount ALWAYS mounts to drive-mount-vfs (the base VFS layer).
#       drive-mount is provided by octor-rclone-chunker (which sits on top of drive-mount-vfs).
echo "Starting Storage Layer..."
start_service_with_retry "octor-rclone-mount" 3 30
# Always wait for the base VFS mount (drive-mount-vfs) first regardless of mode
wait_for_mount "/srv/octor/infra-data/drive-mount-vfs" 60 "octor-rclone-mount"

if [ "$USE_CHUNKER" = "true" ]; then
    start_service_with_retry "octor-rclone-chunker" 3 30
    wait_for_mount "/srv/octor/infra-data/drive-mount" 60 "octor-rclone-chunker"
fi

# --- Ensure infra containers (postgres, redis, nats) are up ---
echo "Ensuring infrastructure containers are running..."
ensure_infra_containers

# --- Application layer: start in dependency order ---
# Base layer first (backends rest-api depends on), then rest-api, then vault, then UI.
echo "Starting Application Layer..."
if [ "${BENCHMARK_MODE:-}" = "true" ]; then
    echo "⚡ BENCHMARK MODE ACTIVE: Starting core benchmark services only."
    CORE_SERVICES=(
        "octor-torrent-store"
        "octor-magnet2torrent"
        "octor-abuse-store"         # required by rest-api abuse-check on every resource POST
        "octor-rest-api"
        "octor-torrent-http-proxy"  # vault fetches source.torrent via Caddy → localhost:50052
        "octor-s3-gateway"
        "octor-vault"
        "octor-torrent-web-seeder"
        "octor-torrent-web-seeder-cleaner"
        "octor-sidecar"
    )
else
    CORE_SERVICES=(
        # Tier 1: standalone data stores & lookup services
        "octor-torrent-store"
        "octor-magnet2torrent"
        "octor-video-info"
        "octor-abuse-store"
        "octor-url-store"
        # Tier 2: main API (depends on tier 1)
        "octor-rest-api"
        # Tier 3: vault & gateway (depend on rest-api and S3)
        "octor-vault"
        "octor-s3-gateway"
        # Tier 4: media services
        "octor-torrent-web-seeder"
        "octor-torrent-web-seeder-cleaner"
        "octor-torrent-http-proxy"
        "octor-content-prober"
        "octor-content-transcoder"
        "octor-srt2vtt"
        "octor-torrent-archiver"
        # Tier 5: auxiliary
        "octor-claims-provider"
        "octor-ai-proxy"
        "octor-sidecar"
    )
fi
for svc in "${CORE_SERVICES[@]}"; do
    start_service_with_retry "$svc" 3 15
done

# Always restart web-ui last so the domain is never left with a blank page,
# even during benchmark mode where it is excluded from CORE_SERVICES above.
start_service_with_retry "octor-web-ui" 3 15

echo "=== 7. VERIFYING SYSTEM HEALTH ==="
# Vault's probe internally checks connectivity to rest-api.
# We MUST probe in dependency order: rest-api first, then vault, then web-seeder.
# Also give services a warmup window to fully bind their ports after systemd reports active.
echo "  Giving services 3s to fully bind ports..."
sleep 3

# Ordered probe list: [url, owning_service]
# Using a plain array (not associative map) to guarantee order.
# IMPORTANT: Use the dedicated probe ports (5208x/5205x), NOT the web ports.
#   rest-api:  web=8080,  probe=52080
#   vault:     web=8086,  probe=52086
#   web-seeder:web=50054, probe=52054
#   web-ui:    web=8082,  probe=52081
PROBE_ORDER=(
    "http://localhost:52080/liveness" "octor-rest-api"
    "http://localhost:52086/liveness" "octor-vault"
    "http://localhost:52054/liveness" "octor-torrent-web-seeder"
    "http://localhost:52081/liveness" "octor-web-ui"
)

ALL_HEALTHY=true
i=0
while [ $i -lt ${#PROBE_ORDER[@]} ]; do
    url="${PROBE_ORDER[$i]}"
    owner="${PROBE_ORDER[$((i+1))]}"
    i=$((i + 2))

    # For vault: if rest-api already failed, restart rest-api instead of vault
    if [ "$url" = "http://localhost:52086/liveness" ] && [ "$ALL_HEALTHY" = "false" ]; then
        echo "  ⏭  Skipping vault probe — rest-api dependency not healthy yet."
        continue
    fi

    if ! probe_with_restart "$url" "$owner" 5 3; then
        ALL_HEALTHY=false
    fi
done

if [ "$ALL_HEALTHY" = "true" ]; then
    echo "✅ All services healthy."
else
    echo "⚠️  One or more services failed final health check — see logs above."
fi

echo "🎉 Performance mode successfully switched to: [$MODE]"

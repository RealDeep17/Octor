#!/bin/bash
# ==============================================================================
# OCTOR UNIVERSAL RUNNER (run.sh)
# ==============================================================================
# Unified management script for Octor: Build, Mode Switching, Benchmarking,
# Maintenance, and Service Orchestration.
# ==============================================================================

set -euo pipefail

# --- Configuration & Paths ---
# Auto-detect project root based on script location
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$PROJECT_ROOT/custom.env"
BACKUP_DIR="$PROJECT_ROOT/.env_backups"
LOG_FILE="$PROJECT_ROOT/benchmark_results.log"
BIN_DIR="$PROJECT_ROOT/bin"

# Ensure environment exists
if [[ ! -f "$ENV_FILE" ]]; then
    echo "❌ Error: $ENV_FILE not found!"
    exit 1
fi

# Extract key paths from custom.env or set defaults
RCLONE_CONFIG=$(grep '^VAULT_RCLONE_CONFIG=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "$HOME/.config/rclone/rclone.conf")
DATA_DIR=$(grep '^DATA_DIR=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "/mnt/seeder-cache")
STORAGE_BASE=$(grep '^VAULT_STORAGE_PATH=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' | sed "s|/drive-mount-vfs||" || echo "$PROJECT_ROOT/infra-data")

run_sudo() {
    sudo "$@"
}

# ------------------------------------------------------------------------------
# HELPERS: Service & System Management
# ------------------------------------------------------------------------------

wait_for_service() {
    local svc="$1"
    local timeout="${2:-30}"
    local elapsed=0
    while [ $elapsed -lt $timeout ]; do
        if systemctl is-active "$svc" >/dev/null 2>&1; then return 0; fi
        sleep 1
        elapsed=$((elapsed + 1))
    done
    return 1
}

start_service_with_retry() {
    local svc="$1"
    local max_retries="${2:-3}"
    local wait_secs="${3:-10}"
    local attempt=1
    while [ $attempt -le $max_retries ]; do
        echo -n "  Starting $svc ($attempt/$max_retries)... "
        run_sudo systemctl restart "$svc" 2>/dev/null || true
        if wait_for_service "$svc" "$wait_secs"; then
            echo "✓"
            return 0
        fi
        echo "✗"
        attempt=$((attempt + 1))
    done
    return 1
}

probe_with_restart() {
    local url="$1"
    local owner_svc="$2"
    local max_rounds="${3:-5}"
    local pause="${4:-5}"
    local round=1
    while [ $round -le $max_rounds ]; do
        echo -n "  Probe $url ($round/$max_rounds)... "
        if curl -sf --max-time 5 "$url" > /dev/null 2>&1; then
            echo "✓"
            return 0
        fi
        echo "⚠️"
        if [ $round -lt $max_rounds ]; then
            run_sudo systemctl restart "$owner_svc" 2>/dev/null || true
            sleep "$pause"
        fi
        round=$((round + 1))
    done
    return 1
}

wait_for_mount() {
    local mnt="$1"
    local timeout="${2:-60}"
    local elapsed=0
    echo -n "  Waiting for mount at $mnt"
    while [ $elapsed -lt $timeout ]; do
        if mountpoint -q "$mnt" 2>/dev/null; then
            echo " ✓"
            return 0
        fi
        printf "."
        sleep 2
        elapsed=$((elapsed + 2))
    done
    echo " ❌"
    return 1
}

ensure_infra_containers() {
    local containers=("octor-postgres" "octor-redis" "octor-nats")
    for ctr in "${containers[@]}"; do
        if [ "$(docker inspect -f '{{.State.Status}}' "$ctr" 2>/dev/null)" != "running" ]; then
            echo -n "  Starting $ctr... "
            docker start "$ctr" > /dev/null 2>&1 && echo "✓" || echo " ❌"
        fi
    done
    local elapsed=0
    while [ $elapsed -lt 30 ]; do
        if docker exec octor-postgres pg_isready -U octor -q 2>/dev/null; then return 0; fi
        sleep 1
        elapsed=$((elapsed + 1))
    done
    return 1
}

stop_all_octor() {
    echo "=== STOPPING ALL OCTOR SERVICES ==="
    run_sudo systemctl stop octor-* 2>/dev/null || true
    echo "✓ Services stopped."
}

kill_ghosts() {
    echo "⚠️  Hunting ghost processes and zombie mounts..."
    GHOST_NAMES=("rest-api" "web-ui" "vault" "abuse-store" "claims-provider" "torrent-store" "url-store" "video-info" "torrent-archiver" "srt2vtt" "content-transcoder" "magnet2torrent" "torrent-web-seeder" "content-prober" "torrent-http-proxy" "torrent-web-seeder-cleaner" "s3-gateway")
    # Use exact name matching instead of greedy command-line matching (-f) to prevent accidental system kills
    for name in "${GHOST_NAMES[@]}"; do run_sudo pkill -9 "^${name}$" 2>/dev/null || true; done
    run_sudo pkill -9 "^rclone$" 2>/dev/null || true
    run_sudo pkill -9 "^uvicorn$" 2>/dev/null || true
    run_sudo pkill -9 -f "proxy.js" 2>/dev/null || true
    
    # Target specific directories only. 
    # CRITICAL: Do NOT use 'fuser -m' on a non-mountpoint directory, as it will target the entire parent partition (root)!
    for dir in "$DATA_DIR" "/mnt/seeder-cache-backing"; do
        if [ -d "$dir" ]; then
            if mountpoint -q "$dir" 2>/dev/null; then
                run_sudo fuser -k -9 -m "$dir" >/dev/null 2>&1 || true
            else
                # If not a mountpoint, only kill processes with open files specifically in that folder
                run_sudo fuser -k -9 "$dir" >/dev/null 2>&1 || true
            fi
        fi
    done
    for loop in $(losetup -a | grep "seeder-cache.img" | cut -d: -f1); do run_sudo losetup -d "$loop" || true; done
    if command -v fuser >/dev/null; then
        run_sudo fuser -k -n tcp 8080 8082 8086 >/dev/null 2>&1 || true
        for port in {50051..50063}; do run_sudo fuser -k -n tcp "$port" >/dev/null 2>&1 || true; done
    fi
}

unmount_paths() {
    echo "=== SAFE UNMOUNT OF STORAGE PATHS ==="
    for path in "$PROJECT_ROOT/infra-data/drive-mount" "$PROJECT_ROOT/infra-data/drive-mount-vfs" "$DATA_DIR"; do
        if mountpoint -q "$path"; then
            echo "Unmounting $path..."
            run_sudo fusermount -uz "$path" 2>/dev/null || run_sudo umount -l "$path" || true
        fi
    done
}

backup_env() {
    mkdir -p "$BACKUP_DIR"
    cp "$ENV_FILE" "$BACKUP_DIR/custom.env.bak.$(date +%Y%m%d%H%M%S)"
    # Keep only last 20 backups
    ls -dt "$BACKUP_DIR"/* | tail -n +21 | xargs rm -f -- 2>/dev/null || true
}

# ------------------------------------------------------------------------------
# SUBCOMMAND: mode (Performance Mode Switching)
# ------------------------------------------------------------------------------

cmd_mode() {
    # If no arguments provided, show menu and accept interactive input with flags
    if [[ $# -eq 0 ]]; then
        echo "===================================================="
        echo "   OCTOR PERFORMANCE MODE SELECTOR"
        echo "===================================================="
        echo "RAM Modes:  1) sp-perf-ram  2) sp-norm-ram  3) sp-eco-ram"
        echo "            4) pp-perf-ram  5) pp-norm-ram"
        echo "SSD Modes:  6) sp-perf-ssd  7) sp-norm-ssd  8) sp-eco-ssd"
        echo "            9) pp-perf-ssd 10) pp-norm-ssd"
        echo "----------------------------------------------------"
        echo "Optional Flags (can be combined):"
        echo "  f = Force kill ghosts    d = Rebuild Docker"
        echo "  r = Clear Rclone cache   s = Sync Systemd"
        echo "  b = Run in Bench Mode    n = Restart Nginx"
        echo "  c = Compile Binaries"
        echo "===================================================="
        read -p "Select option and flags (e.g. '1 f r d n c'): " INPUT
        # Split input into positional parameters
        set -- $INPUT
    fi

    local MODE_ARG="${1:-}"
    local MODE=""
    local CLEAN_RCLONE=false
    local FORCE=false
    local REBUILD_DOCKER=false
    local SYNC_SERVICES=false
    local BENCH_FLAG=false
    local RESTART_NGINX=false
    local COMPILE_BINARIES=false

    # 1. Determine Mode
    if [[ "$MODE_ARG" =~ ^[0-9]+$ ]]; then
        case "$MODE_ARG" in
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
            *) echo "❌ Invalid selection: $MODE_ARG"; exit 1 ;;
        esac
        shift || true
    else
        MODE="$MODE_ARG"
        shift || true
    fi

    # 2. Parse remaining Flags
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --rclone|-r|r) CLEAN_RCLONE=true; shift ;;
            --force|-f|--f|f) FORCE=true; shift ;;
            --docker|-d|d) REBUILD_DOCKER=true; shift ;;
            --sync|-s|s) SYNC_SERVICES=true; shift ;;
            --bench|-b|--b|b) BENCH_FLAG=true; shift ;;
            --nginx|-n|n) RESTART_NGINX=true; shift ;;
            --build|-c|c) COMPILE_BINARIES=true; shift ;;
            *) echo "❌ Unknown option: $1"; exit 1 ;;
        esac
    done

    if [[ -z "$MODE" ]]; then echo "❌ No mode specified"; exit 1; fi

    # 3. Mode Mapping Logic
    local MODE_NAME=""
    local RAM_CACHE_ENABLED=false
    local BUDGET=""
    local VFS_LIMIT=""
    local VFS_CACHE_MODE=""
    local STORAGE_PATH=""
    local USE_CHUNKER=false
    local POLL_INTERVAL=""
    local CHUNK_SIZE=""
    local CONCURRENCY=1
    local RCLONE_BUFFER=""
    local RCLONE_DRIVE_CHUNK=""
    local RCLONE_XFERS=2

    case "$MODE" in
        pp-perf-ram|perf|ram-perf|chunker-vfs-ram|ram|ram-chunker) MODE_NAME="chunker-vfs-ram"; RAM_CACHE_ENABLED=true; BUDGET="2000M"; VFS_LIMIT="3G"; VFS_CACHE_MODE="writes"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount"; USE_CHUNKER=true; POLL_INTERVAL="1s"; CHUNK_SIZE="256M"; CONCURRENCY=4; RCLONE_BUFFER="64M"; RCLONE_DRIVE_CHUNK="128M"; RCLONE_XFERS=4 ;;
        pp-norm-ram) MODE_NAME="chunker-vfs-ram"; RAM_CACHE_ENABLED=true; BUDGET="1500M"; VFS_LIMIT="2G"; VFS_CACHE_MODE="writes"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount"; USE_CHUNKER=true; POLL_INTERVAL="1s"; CHUNK_SIZE="256M"; CONCURRENCY=4; RCLONE_BUFFER="32M"; RCLONE_DRIVE_CHUNK="64M"; RCLONE_XFERS=4 ;;
        sp-perf-ram|steady|ram-steady|non-chunker-vfs-ram|ram-vfs) MODE_NAME="non-chunker-vfs-ram"; RAM_CACHE_ENABLED=true; BUDGET="2000M"; VFS_LIMIT="off"; VFS_CACHE_MODE="off"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount-vfs"; USE_CHUNKER=false; POLL_INTERVAL="1h"; CHUNK_SIZE=""; CONCURRENCY=1; RCLONE_BUFFER="128M"; RCLONE_DRIVE_CHUNK="256M"; RCLONE_XFERS=2 ;;
        sp-norm-ram) MODE_NAME="non-chunker-vfs-ram"; RAM_CACHE_ENABLED=true; BUDGET="1500M"; VFS_LIMIT="off"; VFS_CACHE_MODE="off"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount-vfs"; USE_CHUNKER=false; POLL_INTERVAL="1h"; CHUNK_SIZE=""; CONCURRENCY=1; RCLONE_BUFFER="64M"; RCLONE_DRIVE_CHUNK="128M"; RCLONE_XFERS=2 ;;
        sp-eco-ram|pp-eco-ram|eco|ram-eco|eco-ram) MODE_NAME="non-chunker-vfs-ram"; RAM_CACHE_ENABLED=true; BUDGET="1000M"; VFS_LIMIT="off"; VFS_CACHE_MODE="off"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount-vfs"; USE_CHUNKER=false; POLL_INTERVAL="1h"; CHUNK_SIZE=""; CONCURRENCY=1; RCLONE_BUFFER="32M"; RCLONE_DRIVE_CHUNK="64M"; RCLONE_XFERS=2 ;;
        pp-perf-ssd|ssd-perf|chunker-vfs-ssd|ssd|ssd-chunker) MODE_NAME="chunker-vfs-ssd"; RAM_CACHE_ENABLED=false; BUDGET="2000M"; VFS_LIMIT="8G"; VFS_CACHE_MODE="writes"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount"; USE_CHUNKER=true; POLL_INTERVAL="10s"; CHUNK_SIZE="1G"; CONCURRENCY=4; RCLONE_BUFFER="64M"; RCLONE_DRIVE_CHUNK="128M"; RCLONE_XFERS=4 ;;
        pp-norm-ssd) MODE_NAME="chunker-vfs-ssd"; RAM_CACHE_ENABLED=false; BUDGET="1500M"; VFS_LIMIT="4G"; VFS_CACHE_MODE="writes"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount"; USE_CHUNKER=true; POLL_INTERVAL="10s"; CHUNK_SIZE="512M"; CONCURRENCY=4; RCLONE_BUFFER="32M"; RCLONE_DRIVE_CHUNK="64M"; RCLONE_XFERS=4 ;;
        sp-perf-ssd|ssd-steady|non-chunker-vfs-ssd|ssd-vfs) MODE_NAME="non-chunker-vfs-ssd"; RAM_CACHE_ENABLED=false; BUDGET="2000M"; VFS_LIMIT="off"; VFS_CACHE_MODE="off"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount-vfs"; USE_CHUNKER=false; POLL_INTERVAL="1h"; CHUNK_SIZE=""; CONCURRENCY=1; RCLONE_BUFFER="128M"; RCLONE_DRIVE_CHUNK="256M"; RCLONE_XFERS=2 ;;
        sp-norm-ssd) MODE_NAME="non-chunker-vfs-ssd"; RAM_CACHE_ENABLED=false; BUDGET="1500M"; VFS_LIMIT="off"; VFS_CACHE_MODE="off"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount-vfs"; USE_CHUNKER=false; POLL_INTERVAL="1h"; CHUNK_SIZE=""; CONCURRENCY=1; RCLONE_BUFFER="64M"; RCLONE_DRIVE_CHUNK="128M"; RCLONE_XFERS=2 ;;
        sp-eco-ssd|pp-eco-ssd|ssd-eco) MODE_NAME="non-chunker-vfs-ssd"; RAM_CACHE_ENABLED=false; BUDGET="1000M"; VFS_LIMIT="off"; VFS_CACHE_MODE="off"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount-vfs"; USE_CHUNKER=false; POLL_INTERVAL="1h"; CHUNK_SIZE=""; CONCURRENCY=1; RCLONE_BUFFER="32M"; RCLONE_DRIVE_CHUNK="64M"; RCLONE_XFERS=2 ;;
        *) echo "❌ Invalid mode: $MODE"; exit 1 ;;
    esac

    # 3b. Warn about Chunker Mode Switches and transition consequences
    local CURRENT_MODE_NAME=""
    if [[ -f "$ENV_FILE" ]]; then
        CURRENT_MODE_NAME=$(grep '^OCTOR_PERFORMANCE_MODE=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)
    fi

    if [[ -n "$CURRENT_MODE_NAME" && "$CURRENT_MODE_NAME" != "$MODE_NAME" ]]; then
        local CURRENT_IS_CHUNKER=false
        if [[ "$CURRENT_MODE_NAME" =~ ^chunker-vfs- ]]; then
            CURRENT_IS_CHUNKER=true
        fi

        if [[ "$CURRENT_IS_CHUNKER" = "true" && "$USE_CHUNKER" = "false" ]]; then
            echo "========================================================================"
            echo -e "\033[1;31m🛑 CRITICAL WARNING: PERFORMANCE MODE DOWNGRADE DETECTED\033[0m"
            echo "========================================================================"
            echo -e "You are switching from a \033[1;33mChunker Mode (pp-)\033[0m to a \033[1;33mNon-Chunker Mode (sp-)\033[0m."
            echo -e "  Current Mode: \033[1;36m$CURRENT_MODE_NAME\033[0m"
            echo -e "  Target Mode:  \033[1;36m$MODE_NAME\033[0m"
            echo "------------------------------------------------------------------------"
            echo -e "\033[1;31m⚠️  CRITICAL CONSEQUENCES:\033[0m"
            echo "  1. Any files uploaded while Chunker mode was active were split into"
            echo "     part files (e.g., .001, .002, etc.) on the remote Google Drive."
            echo "  2. Non-Chunker modes bypass the chunker overlay entirely."
            echo -e "  3. \033[1;31mRESULT:\033[0m These previously uploaded chunked files will appear on your"
            echo "     filesystem as raw, fragmented pieces. They will be UNPLAYABLE and"
            echo "     corrupted in Octor, WebDAV, or downstream media players!"
            echo "------------------------------------------------------------------------"
            echo -e "\033[1;32m💡 REMEDIATION:\033[0m"
            echo "  To access those files, you must switch back to a Chunker (pp-) mode,"
            echo "  or manually download, merge, and re-upload them without chunking."
            echo "========================================================================"
            
            if [[ "$FORCE" = "false" && -t 0 ]]; then
                read -p "Are you absolutely sure you want to proceed with this switch? [y/N]: " CONFIRM
                if [[ ! "$CONFIRM" =~ ^[yY](es)?$ ]]; then
                    echo "❌ Mode switch aborted."
                    exit 1
                fi
            else
                echo "⚠️  Non-interactive or --force flag detected. Proceeding automatically..."
            fi

        elif [[ "$CURRENT_IS_CHUNKER" = "false" && "$USE_CHUNKER" = "true" ]]; then
            echo "========================================================================"
            echo -e "\033[1;32mℹ️  INFORMATION: PERFORMANCE MODE UPGRADE DETECTED\033[0m"
            echo "========================================================================"
            echo -e "You are switching from a \033[1;33mNon-Chunker Mode (sp-)\033[0m to a \033[1;33mChunker Mode (pp-)\033[0m."
            echo -e "  Current Mode: \033[1;36m$CURRENT_MODE_NAME\033[0m"
            echo -e "  Target Mode:  \033[1;36m$MODE_NAME\033[0m"
            echo "------------------------------------------------------------------------"
            echo -e "\033[1;32m✅ WHAT HAPPENS TO EXISTING DATA?\033[0m"
            echo "  - Existing single-part/plain files remain fully readable and accessible"
            echo "    through the chunker overlay. No immediate action is required!"
            echo "------------------------------------------------------------------------"
            echo -e "\033[1;33m⚠️  FUTURE UPLOAD CONSEQUENCES:\033[0m"
            echo "  - Any NEW files uploaded in this Chunker mode will be split into chunks."
            echo "  - If you switch back to a Non-Chunker (sp-) mode in the future,"
            echo "    those new files will become fragmented and unplayable."
            echo "========================================================================"

            if [[ "$FORCE" = "false" && -t 0 ]]; then
                read -p "Do you want to proceed with this switch? [y/N]: " CONFIRM
                if [[ ! "$CONFIRM" =~ ^[yY](es)?$ ]]; then
                    echo "❌ Mode switch aborted."
                    exit 1
                fi
            else
                echo "⚠️  Non-interactive or --force flag detected. Proceeding automatically..."
            fi

        else
            echo "========================================================================"
            echo -e "\033[1;36m🔄 CHANGING PERFORMANCE MODE PROFILE\033[0m"
            echo "========================================================================"
            echo -e "Adjusting cache size and VFS behavior profiles within the same storage type."
            echo -e "  Current Profile: \033[1;36m$CURRENT_MODE_NAME\033[0m"
            echo -e "  Target Profile:  \033[1;36m$MODE_NAME\033[0m"
            echo "------------------------------------------------------------------------"
            echo "  - No storage architecture changes are required."
            echo "  - Existing cached files are preserved."
            echo "========================================================================"
        fi
    fi

    if [ "$SYNC_SERVICES" = "true" ]; then
        echo "=== SYNCING SERVICES ==="
        run_sudo cp "$PROJECT_ROOT"/octor-*.service /etc/systemd/system/
        run_sudo systemctl daemon-reload
    fi

    stop_all_octor
    if [ "$FORCE" = "true" ]; then 
        kill_ghosts
        echo "" # Reset line after ghost hunting
    fi
    unmount_paths

    if [ "$COMPILE_BINARIES" = "true" ]; then
        cmd_build
    fi

    if [ "$REBUILD_DOCKER" = "true" ]; then
        echo "=== REBUILDING DOCKER ==="
        (cd "$PROJECT_ROOT" && run_sudo docker-compose --ansi never down)
        (cd "$PROJECT_ROOT" && run_sudo docker-compose --ansi never up -d --build --force-recreate)
        
        # Wait for NATS and Postgres
        echo -n "  Waiting for infrastructure connectivity"
        local INFRA_READY=false
        for i in {1..30}; do
            if nc -z localhost 4222 >/dev/null 2>&1 && docker exec octor-postgres pg_isready -U octor -q >/dev/null 2>&1; then
                echo " ✓"
                INFRA_READY=true
                break
            fi
            printf "."
            sleep 1
        done
        [[ "$INFRA_READY" = "false" ]] && echo " ❌"

        echo "=== INITIALIZING NATS JETSTREAM ==="
        (cd "$PROJECT_ROOT" && go run scripts/create_nats_stream.go >/dev/null) || echo "⚠️  NATS Init failed - services might crash!"
    fi

    backup_env
    echo "=== UPDATING ENV FOR MODE: $MODE ==="
    run_sudo sed -i "s|^RAM_CACHE_ENABLED=.*|RAM_CACHE_ENABLED=$RAM_CACHE_ENABLED|" "$ENV_FILE"
    run_sudo sed -i "s|^PER_TORRENT_CACHE_BUDGET=.*|PER_TORRENT_CACHE_BUDGET=$BUDGET|" "$ENV_FILE"
    run_sudo sed -i "s|^RCLONE_VFS_CACHE_MAX_SIZE=.*|RCLONE_VFS_CACHE_MAX_SIZE=$VFS_LIMIT|" "$ENV_FILE"
    run_sudo sed -i "s|^RCLONE_VFS_CACHE_POLL_INTERVAL=.*|RCLONE_VFS_CACHE_POLL_INTERVAL=$POLL_INTERVAL|" "$ENV_FILE"
    run_sudo sed -i "s|^RCLONE_VFS_CACHE_MODE=.*|RCLONE_VFS_CACHE_MODE=$VFS_CACHE_MODE|" "$ENV_FILE"
    run_sudo sed -i "s|^VAULT_STORAGE_PATH=.*|VAULT_STORAGE_PATH=$STORAGE_PATH|" "$ENV_FILE"
    run_sudo sed -i "s|^S3_GATEWAY_STORAGE_DIR=.*|S3_GATEWAY_STORAGE_DIR=$STORAGE_PATH|" "$ENV_FILE"
    run_sudo sed -i "s|^OCTOR_PERFORMANCE_MODE=.*|OCTOR_PERFORMANCE_MODE=$MODE_NAME|" "$ENV_FILE"
    run_sudo sed -i "s|^AWS_UPLOAD_CONCURRENCY=.*|AWS_UPLOAD_CONCURRENCY=$CONCURRENCY|" "$ENV_FILE"
    run_sudo sed -i "s|^RCLONE_BUFFER_SIZE=.*|RCLONE_BUFFER_SIZE=$RCLONE_BUFFER|" "$ENV_FILE"
    run_sudo sed -i "s|^RCLONE_DRIVE_CHUNK_SIZE=.*|RCLONE_DRIVE_CHUNK_SIZE=$RCLONE_DRIVE_CHUNK|" "$ENV_FILE"
    run_sudo sed -i "s|^RCLONE_TRANSFERS=.*|RCLONE_TRANSFERS=$RCLONE_XFERS|" "$ENV_FILE"

    if [ "$CLEAN_RCLONE" = "true" ]; then
        echo "=== CLEARING RCLONE CACHE ==="
        R_CACHE_DIR=$(grep -E '^RCLONE_CACHE_DIR=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)
        if [[ -n "$R_CACHE_DIR" && "$R_CACHE_DIR" != "/" && "$R_CACHE_DIR" != "/srv" && "$R_CACHE_DIR" != "$PROJECT_ROOT" ]]; then 
            run_sudo rm -rf "${R_CACHE_DIR:?}"/* || true
        fi
    fi
    
    echo "=== PREPARING SEEDER CACHE ==="
    run_sudo systemctl start octor-seeder-cache
    wait_for_service "octor-seeder-cache" 60

    if [ "$USE_CHUNKER" = "true" ]; then
        sed -i "s/chunk_size = .*/chunk_size = ${CHUNK_SIZE}/g" "$RCLONE_CONFIG"
    fi

    run_sudo systemctl daemon-reload
    echo "=== STARTING STORAGE LAYER ==="
    if ! start_service_with_retry "octor-rclone-mount" 3 30 || ! wait_for_mount "$PROJECT_ROOT/infra-data/drive-mount-vfs" 60; then
        echo -e "\n\033[1;33m⚠️  WARNING: Storage mount (octor-rclone-mount) failed to boot or mount successfully!\033[0m"
        echo "   This is often due to an expired OAuth token, missing network connection, or rclone lock."
        echo "   We will continue starting the other microservices so that the Web UI and sidecar"
        echo "   remain accessible, but storage operations will fail until resolved.\n"
    fi

    if [ "$USE_CHUNKER" = "true" ]; then
        if ! start_service_with_retry "octor-rclone-chunker" 3 30 || ! wait_for_mount "$PROJECT_ROOT/infra-data/drive-mount" 60; then
            echo -e "\n\033[1;33m⚠️  WARNING: Chunker mount (octor-rclone-chunker) failed to boot or mount successfully!\033[0m"
            echo "   This can happen if the underlying rclone-mount is unavailable or if the path is locked."
            echo "   We will proceed starting the microservices, but chunker operations will be offline.\n"
        fi
    fi

    ensure_infra_containers

    if [ "$BENCH_FLAG" = "true" ] || [ "${BENCHMARK_MODE:-false}" = "true" ]; then
        echo "⚡ BENCHMARK MODE ACTIVE"
        SERVICES=("octor-torrent-store" "octor-magnet2torrent" "octor-rest-api" "octor-vault" "octor-s3-gateway" "octor-torrent-web-seeder")
    else
        SERVICES=("octor-torrent-store" "octor-magnet2torrent" "octor-video-info" "octor-abuse-store" "octor-url-store" "octor-rest-api" "octor-vault" "octor-s3-gateway" "octor-torrent-web-seeder" "octor-torrent-web-seeder-cleaner" "octor-torrent-http-proxy" "octor-content-prober" "octor-content-transcoder" "octor-srt2vtt" "octor-torrent-archiver" "octor-claims-provider" "octor-ai-proxy" "octor-sidecar" "octor-web-ui")
    fi

    echo "=== STARTING MICROSERVICES ==="
    for svc in "${SERVICES[@]}"; do start_service_with_retry "$svc" 2 10; done

    if [ "$RESTART_NGINX" = "true" ]; then
        echo "=== RESTARTING NGINX (Reverse Proxy) ==="
        run_sudo systemctl restart nginx || echo "⚠️  Nginx restart failed (is it installed?)"
    fi

    sleep 3
    HEALTH_URLS=("http://localhost:52080/liveness" "http://localhost:52086/liveness" "http://localhost:52054/liveness")
    OWNERS=("octor-rest-api" "octor-vault" "octor-torrent-web-seeder")
    ALL_OK=true
    for i in "${!HEALTH_URLS[@]}"; do
        if ! probe_with_restart "${HEALTH_URLS[$i]}" "${OWNERS[$i]}" 3 2; then ALL_OK=false; fi
    done
    echo "=== SWITCH COMPLETE: $MODE ($([ "$ALL_OK" = "true" ] && echo "HEALTHY" || echo "UNHEALTHY")) ==="
}

# ------------------------------------------------------------------------------
# SUBCOMMAND: benchmark (Performance Testing)
# ------------------------------------------------------------------------------

cmd_benchmark() {
    export BENCHMARK_MODE=true
    local TARGET="all"
    local CUSTOM_MAGNET=""
    local TEST_DURATION=300
    local TELEMETRY_INTERVAL=10
    
    # Parse Benchmark Arguments
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --magnet|-m) CUSTOM_MAGNET="${2:-}"; shift 2 || true ;;
            *) TARGET="$1"; shift || true ;;
        esac
    done

    # Default Magnet (Project Hail Mary)
    local RESOURCE_ID="792b3577fed6dd95dbb03f5f0972e821230b834f"
    local MAGNET="magnet:?xt=urn:btih:792b3577fed6dd95dbb03f5f0972e821230b834f&dn=Project.Hail.Mary.2026.2160p.WEB-DL.DDP5.1.Atmos.H.265-RDNYB.mkv&xl=25055439307&tr=udp://tracker.opentrackr.org:1337/announce&tr=udp://opentracker.io:6969/announce&tr=udp://open.stealth.si:80/announce&tr=udp://tracker.opentrackr.org:1337/&tr=udp://tracker.torrent.eu.org:451/announce&tr=udp://bandito.byterunner.io:6969/announce&tr=udp://tracker.qu.ax:6969/announce&tr=http://tracker.renfei.net:8080/announce&tr=udp://open.free-tracker.ga:6969/announce&tr=http://tracker.ipv6tracker.org/announce&tr=udp://tracker2.dler.org:80/announce&tr=udp://exodus.desync.com:6969/announce&tr=udp://open.tracker.cl:1337/announce&tr=udp://open.demonii.com:1337"

    if [[ -n "$CUSTOM_MAGNET" ]]; then
        MAGNET="$CUSTOM_MAGNET"
        # Extract 40-char hex hash from magnet link
        RESOURCE_ID=$(echo "$MAGNET" | grep -oEi 'btih:[a-f0-9]{40}' | cut -d: -f2 | tr '[:upper:]' '[:lower:]' || true)
        if [[ -z "$RESOURCE_ID" ]]; then
            echo "❌ Error: Could not extract valid 40-char info hash from magnet link."
            exit 1
        fi
        echo "🧲 Custom Magnet Detected: $RESOURCE_ID"
    fi

    MODES_LIST=("sp-perf-ram" "sp-norm-ram" "sp-eco-ram" "pp-perf-ram" "pp-norm-ram" "sp-perf-ssd" "sp-norm-ssd" "sp-eco-ssd" "pp-perf-ssd" "pp-norm-ssd")
    
    local ACTIVE_MODES=()
    if [[ "$TARGET" == "all" || "$TARGET" == "bench-all" ]]; then
        ACTIVE_MODES=("${MODES_LIST[@]}")
    elif [[ "$TARGET" =~ ^bench-([0-9]+)$ || "$TARGET" =~ ^([0-9]+)$ ]]; then
        local IDX="${BASH_REMATCH[1]}"
        if [[ $IDX -ge 1 && $IDX -le 10 ]]; then
            ACTIVE_MODES=("${MODES_LIST[$((IDX-1))]}")
        else
            echo "❌ Invalid benchmark index: $IDX"; exit 1
        fi
    else
        ACTIVE_MODES=("$TARGET")
    fi

    # Detect SSD for I/O tracking
    local DEV_PATH=$(df "$PROJECT_ROOT" | tail -n 1 | awk '{print $1}')
    local DISK_NAME=$(lsblk -no pkname "$DEV_PATH" | tr -d '\r' | head -n 1)
    [ -z "$DISK_NAME" ] && DISK_NAME=$(basename "$DEV_PATH")
    local STAT_FILE="/sys/class/block/${DISK_NAME}/stat"

    echo "# OCTOR PERFORMANCE BENCHMARK REPORT ($(date))" > "$LOG_FILE"
    echo "| Mode | Speed Avg (MB/s) | Speed Peak (MB/s) | CPU Avg (%) | CPU Peak (%) | RAM Avg (MB) | RAM Peak (MB) | SSD I/O Avg | Total (MB) |" >> "$LOG_FILE"
    echo "| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |" >> "$LOG_FILE"

    for MODE in "${ACTIVE_MODES[@]}"; do
        echo "🚀 BENCHMARKING: $MODE"
        ensure_infra_containers
        
        # 1. Database Cleanup
        docker exec -i octor-postgres psql -U octor -d vault -c "DELETE FROM resource_file WHERE resource_id = '$RESOURCE_ID';" || true
        docker exec -i octor-postgres psql -U octor -d vault -c "DELETE FROM resource WHERE resource_id = '$RESOURCE_ID';" || true
        # Best-effort orphan file cleanup (removes files with no other references)
        docker exec -i octor-postgres psql -U octor -d vault -c "DELETE FROM file WHERE hash NOT IN (SELECT file_hash FROM resource_file);" || true
        
        # 2. Remote Storage Cleanup (Before Round)
        echo "🧹 Surgical cleanup of remote S3/Rclone storage for resource $RESOURCE_ID..."
        local RCLONE_REMOTE=$(grep '^VAULT_RCLONE_REMOTE=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' | xargs || echo "ALPHA_UNION:")
        local BUCKET_NAME=$(grep '^VAULT_AWS_BUCKET=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' | xargs || echo "vault")
        local REMOTE_PATH="${RCLONE_REMOTE}${BUCKET_NAME}"
        
        if [[ "$REMOTE_PATH" == *":"* ]]; then
            # Delete metadata JSON
            rclone --config "$RCLONE_CONFIG" deletefile "${REMOTE_PATH}/metadata/resources/${RESOURCE_ID}.json" --quiet || true
            # Delete torrent files
            rclone --config "$RCLONE_CONFIG" delete "${REMOTE_PATH}/torrents/" --include "* [${RESOURCE_ID}].torrent" --quiet || true
            rclone --config "$RCLONE_CONFIG" deletefile "${REMOTE_PATH}/torrents/${RESOURCE_ID}.torrent" --quiet || true
            # Delete human-readable media folder
            rclone --config "$RCLONE_CONFIG" purge "${REMOTE_PATH}/media/" --include "* [${RESOURCE_ID}]/**" --quiet || true
        fi

        cmd_mode "$MODE" --bench --force --rclone

        curl -s -X POST -H "Content-Type: text/plain" -d "$MAGNET" http://localhost:8080/resource/ || true
        echo "Waiting for metadata..."
        for i in {1..20}; do
            if curl -s "http://localhost:8080/resource/${RESOURCE_ID}/list" | grep -q '"items"'; then break; fi
            sleep 5
        done
        curl -sf -X PUT "http://localhost:8086/resource/$RESOURCE_ID" || continue

        # Telemetry loop
        local start_t=$(date +%s); local last_t=$start_t; local last_s=0
        local speed_sum=0; local speed_peak=0; local cpu_sum=0; local cpu_peak=0
        local ram_sum=0; local ram_peak=0; local ssd_sum=0; local ssd_peak=0; local count=0

        # Initial SSD sectors
        local init_r=0; local init_w=0
        if [ -f "$STAT_FILE" ]; then read -r _ _ init_r _ _ _ init_w _ < "$STAT_FILE"; fi
        local prev_r=$init_r; local prev_w=$init_w

        while [ $(($(date +%s) - start_t)) -lt "$TEST_DURATION" ]; do
            sleep "$TELEMETRY_INTERVAL"
            local now=$(date +%s); local elapsed=$((now - last_t))
            
            # 1. Speed
            local cur_s=$(curl -s "http://localhost:8086/resource/$RESOURCE_ID" | jq -r .stored_size 2>/dev/null || echo "0")
            [ "$cur_s" = "null" ] && cur_s=0
            local inst_speed=$(echo "scale=2; ($cur_s - $last_s) / 1048576 / $elapsed" | bc)
            
            # 2. CPU/RAM
            local cpu=$(top -bn1 | grep "Cpu(s)" | sed "s/.*, *\([0-9.]*\)%* id.*/\1/" | awk '{print 100 - $1}')
            local ram=$(free -m | awk '/Mem:/ {print $3}')
            
            # 3. SSD I/O
            local cur_r=0; local cur_w=0; local ssd_inst=0
            if [ -f "$STAT_FILE" ]; then
                read -r _ _ cur_r _ _ _ cur_w _ < "$STAT_FILE"
                ssd_inst=$(echo "scale=2; (($cur_r - $prev_r) + ($cur_w - $prev_w)) * 512 / 1048576 / $elapsed" | bc)
                prev_r=$cur_r; prev_w=$cur_w
            fi

            # Update Peaks & Sums
            (( $(echo "$inst_speed > $speed_peak" | bc -l) )) && speed_peak=$inst_speed
            (( $(echo "$cpu > $cpu_peak" | bc -l) )) && cpu_peak=$cpu
            (( $(echo "$ram > $ram_peak" | bc -l) )) && ram_peak=$ram
            (( $(echo "$ssd_inst > $ssd_peak" | bc -l) )) && ssd_peak=$ssd_inst
            
            speed_sum=$(echo "$speed_sum + $inst_speed" | bc)
            cpu_sum=$(echo "$cpu_sum + $cpu" | bc)
            ram_sum=$(echo "$ram_sum + $ram" | bc)
            ssd_sum=$(echo "$ssd_sum + $ssd_inst" | bc)
            count=$((count + 1))

            echo "  $((now - start_t))s | Speed: ${inst_speed}MB/s | CPU: ${cpu}% | RAM: ${ram}MB | SSD: ${ssd_inst}MB/s"
            last_s=$cur_s; last_t=$now
        done
        
        # Averages
        local speed_avg=$(echo "scale=2; $speed_sum / $count" | bc)
        local cpu_avg=$(echo "scale=2; $cpu_sum / $count" | bc)
        local ram_avg=$(echo "scale=2; $ram_sum / $count" | bc)
        local ssd_avg=$(echo "scale=2; $ssd_sum / $count" | bc)
        local total_tx=$(echo "scale=2; $last_s / 1048576" | bc)

        # Diagnostic Logs
        local LOG_DIR="$PROJECT_ROOT/scratch/test/$MODE"
        mkdir -p "$LOG_DIR"
        run_sudo journalctl --since "$TEST_DURATION seconds ago" --no-pager | grep "octor-" > "$LOG_DIR/systemd.log" || true
        docker exec -i octor-postgres psql -U octor -d vault -c "SELECT hash, status, stored_size FROM file WHERE hash IN (SELECT file_hash FROM resource_file WHERE resource_id = '$RESOURCE_ID');" > "$LOG_DIR/db.log" || true
        curl -s "http://localhost:53054/metrics" > "$LOG_DIR/seeder-metrics.log" || true

        echo "| $MODE | $speed_avg | $speed_peak | $cpu_avg | $cpu_peak | $ram_avg | $ram_peak | $ssd_avg | $total_tx |" >> "$LOG_FILE"
        
        # 3. Remote Storage Cleanup (After Round) - Prevent Bloat
        echo "🧹 Surgical cleanup of remote storage for resource $RESOURCE_ID..."
        if [[ "$REMOTE_PATH" == *":"* ]]; then
            rclone --config "$RCLONE_CONFIG" deletefile "${REMOTE_PATH}/metadata/resources/${RESOURCE_ID}.json" --quiet || true
            rclone --config "$RCLONE_CONFIG" delete "${REMOTE_PATH}/torrents/" --include "* [${RESOURCE_ID}].torrent" --quiet || true
            rclone --config "$RCLONE_CONFIG" deletefile "${REMOTE_PATH}/torrents/${RESOURCE_ID}.torrent" --quiet || true
            rclone --config "$RCLONE_CONFIG" purge "${REMOTE_PATH}/media/" --include "* [${RESOURCE_ID}]/**" --quiet || true
        fi
    done
    echo "🎉 Benchmark complete. Results in $LOG_FILE"
}

# ------------------------------------------------------------------------------
# SUBCOMMAND: prune (Cleanup)
# ------------------------------------------------------------------------------

cmd_prune() {
    local ALL=false
    if [[ "${1:-}" == "--all" || "${1:-}" == "-a" ]]; then ALL=true; fi
    echo "🧹 Pruning caches..."
    go clean -cache -testcache -fuzzcache -v
    if [ "$ALL" = "true" ]; then go clean -modcache -v; fi
    
    if [[ -n "$BIN_DIR" && "$BIN_DIR" != "/" && "$BIN_DIR" != "/srv" && "$BIN_DIR" != "$PROJECT_ROOT" && -d "$BIN_DIR" ]]; then
        rm -rf "${BIN_DIR:?}"/* || true
    fi
    
    sudo journalctl --vacuum-time=3d || true
    echo "✓ Cleanup complete."
}

# ------------------------------------------------------------------------------
# SUBCOMMAND: install (Systemd Setup)
# ------------------------------------------------------------------------------

cmd_install() {
    echo "🔄 Installing/Updating systemd services..."
    
    # Define services with dynamic paths and users
    declare -A SERVICES

    local COMMON_HEADER="[Unit]
After=network.target

[Service]
Type=simple
User=$USER
Group=$(id -gn)
EnvironmentFile=$ENV_FILE
Restart=always
RestartSec=3"

    SERVICES[octor-ai-proxy]="$COMMON_HEADER
Description=Octor AI Proxy
WorkingDirectory=$PROJECT_ROOT
ExecStart=/usr/bin/node ai-proxy/proxy.js

[Install]
WantedBy=multi-user.target"

    SERVICES[octor-sidecar]="$COMMON_HEADER
Description=Octor Sidecar
WorkingDirectory=$PROJECT_ROOT/sidecar
ExecStart=$PROJECT_ROOT/sidecar/venv/bin/uvicorn main:app --host 0.0.0.0 --port 8000

[Install]
WantedBy=multi-user.target"

    SERVICES[octor-rest-api]="$COMMON_HEADER
Description=Octor REST API
WorkingDirectory=$PROJECT_ROOT/rest-api
ExecStart=$BIN_DIR/rest-api serve --port 8080 --pprof-port 51080 --probe-port 52080 --export-domain \${OCTOR_DOMAIN} --torrent-store-host 127.0.0.1 --torrent-store-port 50051 --magnet2torrent-host 127.0.0.1 --magnet2torrent-port 50053 --video-info-host 127.0.0.1 --video-info-port 50056 --export-use-subdomains false

[Install]
WantedBy=multi-user.target"

    SERVICES[octor-web-ui]="$COMMON_HEADER
Description=Octor Web UI
WorkingDirectory=$PROJECT_ROOT/web-ui
ExecStart=$BIN_DIR/web-ui serve --port 8082 --pprof-port 51081 --probe-port 52081 --octor-rest-api-host localhost --octor-rest-api-port 8080 --vault-service-host localhost --vault-service-port 8086 --use-internal-torrent-http-proxy=true --torrent-http-proxy-host localhost --torrent-http-proxy-port 50052 --domain \${OCTOR_DOMAIN}

[Install]
WantedBy=multi-user.target"

    SERVICES[octor-vault]="$COMMON_HEADER
Description=Octor Vault
WorkingDirectory=$PROJECT_ROOT/vault
ExecStart=$BIN_DIR/vault serve --port 8086 --pprof-port 51086 --probe-port 52086 --prom-port 53086 --octor-rest-api-host localhost --octor-rest-api-port 8080 --aws-endpoint \${AWS_ENDPOINT} --aws-region \${AWS_REGION} --aws-access-key-id \${AWS_ACCESS_KEY_ID} --aws-secret-access-key \${AWS_SECRET_ACCESS_KEY} --aws-bucket \${VAULT_AWS_BUCKET} --aws-no-ssl --max-concurrent-jobs \${VAULT_MAX_CONCURRENT_JOBS} --postgres-database vault

[Install]
WantedBy=multi-user.target"

    SERVICES[octor-abuse-store]="$COMMON_HEADER
Description=Octor Abuse Store
WorkingDirectory=$PROJECT_ROOT/abuse-store
ExecStart=$BIN_DIR/abuse-store serve --grpc-port 50059 --pprof-port 51059 --probe-port 52059 --postgres-database abuse_store

[Install]
WantedBy=multi-user.target"

    SERVICES[octor-claims-provider]="$COMMON_HEADER
Description=Octor Claims Provider
WorkingDirectory=$PROJECT_ROOT/claims-provider
ExecStart=$BIN_DIR/claims-provider serve --grpc-port 50060 --pprof-port 51060 --probe-port 52060 --prom-port 53060 --postgres-database claims_provider

[Install]
WantedBy=multi-user.target"

    SERVICES[octor-torrent-store]="$COMMON_HEADER
Description=Octor Torrent Store
WorkingDirectory=$PROJECT_ROOT/torrent-store
ExecStart=$BIN_DIR/torrent-store serve --grpc-port 50051 --pprof-port 51051 --probe-port 52051 --abuse-host 127.0.0.1 --abuse-port 50059 --use-abuse --use-s3 --aws-endpoint \${AWS_ENDPOINT} --aws-access-key-id \${AWS_ACCESS_KEY_ID} --aws-secret-access-key \${AWS_SECRET_ACCESS_KEY} --aws-bucket \${TORRENT_STORE_AWS_BUCKET} --aws-region \${AWS_REGION} --aws-no-ssl

[Install]
WantedBy=multi-user.target"

    SERVICES[octor-magnet2torrent]="$COMMON_HEADER
Description=Octor Magnet2Torrent
WorkingDirectory=$PROJECT_ROOT/magnet2torrent/server
ExecStart=$BIN_DIR/magnet2torrent --port 50053 --pprof-port 51053 --probe-port 52053

[Install]
WantedBy=multi-user.target"

    SERVICES[octor-url-store]="$COMMON_HEADER
Description=Octor URL Store
WorkingDirectory=$PROJECT_ROOT/url-store
ExecStart=$BIN_DIR/url-store serve --port 54061 --grpc-port 50061 --probe-port 52061 --postgres-database url_store

[Install]
WantedBy=multi-user.target"

    SERVICES[octor-torrent-web-seeder]="$COMMON_HEADER
Description=Octor Torrent Web Seeder
WorkingDirectory=$PROJECT_ROOT/torrent-web-seeder/server
ExecStart=$BIN_DIR/torrent-web-seeder --port 50054 --pprof-port 51054 --probe-port 52054 --prom-port 53054

[Install]
WantedBy=multi-user.target"

    SERVICES[octor-torrent-web-seeder-cleaner]="$COMMON_HEADER
Description=Octor Torrent Web Seeder Cleaner
WorkingDirectory=$PROJECT_ROOT/torrent-web-seeder-cleaner
ExecStart=$BIN_DIR/torrent-web-seeder-cleaner serve

[Install]
WantedBy=multi-user.target"

    SERVICES[octor-content-transcoder]="$COMMON_HEADER
Description=Octor Content Transcoder
WorkingDirectory=$PROJECT_ROOT/content-transcoder
ExecStart=$BIN_DIR/content-transcoder --port 50055 --pprof-port 51055 --probe-port 52055

[Install]
WantedBy=multi-user.target"

    SERVICES[octor-video-info]="$COMMON_HEADER
Description=Octor Video Info
WorkingDirectory=$PROJECT_ROOT/video-info
ExecStart=$BIN_DIR/video-info --port 50056 --probe-port 52056 --redis-host \${REDIS_HOST} --redis-port \${REDIS_PORT}

[Install]
WantedBy=multi-user.target"

    SERVICES[octor-torrent-archiver]="$COMMON_HEADER
Description=Octor Torrent Archiver
WorkingDirectory=$PROJECT_ROOT/torrent-archiver
ExecStart=$BIN_DIR/torrent-archiver --port 50057 --pprof-port 51057 --probe-port 52057 --torrent-store-host localhost --torrent-store-port 50051 --prom-port 53057

[Install]
WantedBy=multi-user.target"

    SERVICES[octor-srt2vtt]="$COMMON_HEADER
Description=Octor SRT2VTT
WorkingDirectory=$PROJECT_ROOT/srt2vtt
ExecStart=$BIN_DIR/srt2vtt --port 50058 --probe-port 52058

[Install]
WantedBy=multi-user.target"

    SERVICES[octor-content-prober]="$COMMON_HEADER
Description=Octor Content Prober
WorkingDirectory=$PROJECT_ROOT/content-prober/server
ExecStart=$BIN_DIR/content-prober --port 50063 --http-port 50062 --probe-port 52062 --redis-host \${REDIS_HOST} --redis-port \${REDIS_PORT}

[Install]
WantedBy=multi-user.target"

    SERVICES[octor-torrent-http-proxy]="$COMMON_HEADER
Description=Octor Torrent HTTP Proxy
WorkingDirectory=$PROJECT_ROOT/torrent-http-proxy
ExecStart=$BIN_DIR/torrent-http-proxy --port 50052 --torrent-http-proxy-host 127.0.0.1 --torrent-http-proxy-port 50052 --pprof-port 51052 --probe-port 52052 --config services.yaml

[Install]
WantedBy=multi-user.target"

    for name in "${!SERVICES[@]}"; do
        echo "   -> Writing /etc/systemd/system/${name}.service..."
        echo "${SERVICES[$name]}" | run_sudo tee "/etc/systemd/system/${name}.service" > /dev/null
    done

    echo "🔄 Reloading systemd daemon..."
    run_sudo systemctl daemon-reload

    echo "🚀 Enabling all Octor services..."
    for name in "${!SERVICES[@]}"; do
        run_sudo systemctl enable "${name}.service" > /dev/null 2>&1 || true
    done

    echo "✅ Systemd services installed successfully!"
}

# ------------------------------------------------------------------------------
# SUBCOMMAND: build (Make)
# ------------------------------------------------------------------------------

cmd_build() {
    echo "🔨 Building Octor..."
    (cd "$PROJECT_ROOT" && make all)
}

# ------------------------------------------------------------------------------
# SUBCOMMAND: status (System Info)
# ------------------------------------------------------------------------------

cmd_status() {
    echo "=== OCTOR SYSTEM STATUS ==="
    echo "Mode: $(grep '^OCTOR_PERFORMANCE_MODE=' "$ENV_FILE" | cut -d= -f2-)"
    echo "Services: $(systemctl list-units "octor-*" --state=active --no-legend | wc -l) active"
    echo "Containers: $(docker ps --format '{{.Names}}' | grep octor | wc -l) running"
    echo "Disk: $(df -h "$PROJECT_ROOT" | tail -1 | awk '{print $5}') usage"
    echo "RAM: $(free -m | awk '/Mem:/ {print $3}')MB used"
}

# ------------------------------------------------------------------------------
# SUBCOMMAND: doctor (Troubleshooting)
# ------------------------------------------------------------------------------

cmd_doctor() {
    echo "=== OCTOR DOCTOR: SYSTEM DIAGNOSTICS ==="
    PATHS=("$HOME/.docker/run/docker.sock" "/var/run/docker.sock" "$HOME/.docker/desktop/docker.sock")
    echo "Searching for active Docker socket..."
    for p in "${PATHS[@]}"; do
        if [ -S "$p" ]; then
            if DOCKER_HOST="unix://$p" docker ps > /dev/null 2>&1; then
                echo "✅ FOUND WORKING DOCKER SOCKET: $p"
                return 0
            fi
        fi
    done
    echo "❌ No working Docker socket found."
}

# ------------------------------------------------------------------------------
# SUBCOMMAND: enrich (Metadata Refresh)
# ------------------------------------------------------------------------------

cmd_enrich() {
    local SUB="${1:-help}"
    # Source env so the binary has all required vars
    export $(grep -v '^#' "$ENV_FILE" | xargs)
    # Suppress proto registration warnings from shared proto packages
    export GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn

    case "$SUB" in
        refresh)
            local DAYS="${2:-7}"
            echo "♻️  Smart Refresh — stale/missing metadata (>${DAYS}d)..."
            echo "    (Respects 1h cooldown on NoMetadata/Error. Skips Abandoned.)"
            "$BIN_DIR"/web-ui enrich refresh --days "$DAYS"
            ;;
        run)
            echo "▶️  Enriching resources without metadata..."
            "$BIN_DIR"/web-ui enrich run
            ;;
        force-all|force)
            echo "⚡ Force All — re-enriching EVERY resource (including Abandoned)..."
            echo "    (Resets retry_count. Use after a major pipeline fix.)"
            "$BIN_DIR"/web-ui enrich run --force
            ;;
        *)
            echo "Usage:"
            echo "  ./run.sh enrich refresh [DAYS]   Smart refresh — stale/missing (default 7d)"
            echo "  ./run.sh enrich run              Enrich resources missing metadata only"
            echo "  ./run.sh enrich force-all        Force re-enrich everything (incl. abandoned)"
            ;;
    esac
}

# ------------------------------------------------------------------------------
# ------------------------------------------------------------------------------
# SUBCOMMAND: reset (Factory Reset)
# ------------------------------------------------------------------------------

cmd_factory_reset() {
    echo "========================================================================"
    echo -e "\033[1;31m🛑 🚨 WARNING: OCTOR FACTORY RESET INITIATED 🚨 🛑\033[0m"
    echo "========================================================================"
    echo -e "\033[1;33mThis action is EXTREMELY DESTRUCTIVE and CANNOT BE UNDONE!\033[0m"
    echo "------------------------------------------------------------------------"
    echo -e "\033[1;31mTHE FOLLOWING WILL BE PERMANENTLY DELETED:\033[0m"
    echo "  1. All local Octor Docker containers and databases (Postgres, Redis)."
    echo "  2. All local BadgerDB metadata key-value stores."
    echo "  3. All local disk and RAM caches (Seeder, SSD, Rclone VFS cache, S3)."
    echo "  4. All compiled local binaries."
    echo -e "  5. \033[1;31m🛑 ALL REMOTE GOOGLE DRIVE FILES AND FOLDERS\033[0m connected to Octor"
    echo "     (Entire remote directory will be purged/emptied!)."
    echo "------------------------------------------------------------------------"
    echo -e "\033[1;32mAFTER DELETION, OCTOR WILL BE REBUILT FROM SCRATCH:\033[0m"
    echo "  - Recompile all Go microservices and clean build cache."
    echo "  - Rebuild and recreate all Docker containers and databases from clean state."
    echo "  - Reinitialize clean NATS streams."
    echo "  - Remount storage layers and start all services healthy."
    echo "========================================================================"

    # Prompt 1
    read -p "Type 'factory-reset' to proceed with the FIRST stage of confirmation: " CONFIRM_1
    if [[ "$CONFIRM_1" != "factory-reset" ]]; then
        echo "❌ Reset cancelled (First check failed)."
        exit 1
    fi

    # Prompt 2
    echo -e "\n\033[1;31m⚠️  FINAL DANGER ZONE: This will wipe your Google Drive alpha union vault! ⚠️\033[0m"
    read -p "Type 'DELETE ALL DATA' to proceed with the SECOND stage of confirmation: " CONFIRM_2
    if [[ "$CONFIRM_2" != "DELETE ALL DATA" ]]; then
        echo "❌ Reset cancelled (Second check failed)."
        exit 1
    fi

    echo -e "\n\033[1;32m🚀 Starting factory reset process...\033[0m"

    # Step 1. Stop all services and unmount paths
    stop_all_octor
    kill_ghosts
    unmount_paths

    # Step 2. Remove Docker infrastructure containers and persistent volumes
    echo "🐳 Wiping Docker infrastructure and volumes..."
    (cd "$PROJECT_ROOT" && run_sudo docker-compose --ansi never down -v --remove-orphans || true)
    
    # Step 3. Retrieve configuration paths
    local CURRENT_MODE=$(grep '^OCTOR_PERFORMANCE_MODE=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "non-chunker-vfs-ram")
    local BADGER_PATH=$(grep '^BADGER_PATH=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' | xargs || echo "./badger-data")
    [[ "$BADGER_PATH" != /* ]] && BADGER_PATH="$PROJECT_ROOT/$BADGER_PATH"
    
    local DATA_DIR=$(grep '^DATA_DIR=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' | xargs || echo "/mnt/seeder-cache")
    local SSD_DATA_DIR=$(grep '^SSD_DATA_DIR=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' | xargs || echo "/srv/octor/infra-data/seeder-cache")
    local RCLONE_CACHE_DIR=$(grep '^RCLONE_CACHE_DIR=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' | xargs || echo "/mnt/seeder-cache/rclone-vfs")
    local S3_GATEWAY_TEMP_UPLOADS_DIR=$(grep '^S3_GATEWAY_TEMP_UPLOADS_DIR=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' | xargs || echo "/mnt/seeder-cache/s3-gateway-uploads")

    # Step 4. Delete local data & databases
    echo "🧹 Wiping local databases and persistent caches..."
    run_sudo rm -rf "$BADGER_PATH" || true
    
    if [[ -d "$DATA_DIR" && "$DATA_DIR" != "/" && "$DATA_DIR" != "/srv" && "$DATA_DIR" != "$PROJECT_ROOT" ]]; then
        run_sudo rm -rf "${DATA_DIR:?}"/* || true
    fi
    if [[ -d "$SSD_DATA_DIR" && "$SSD_DATA_DIR" != "/" && "$SSD_DATA_DIR" != "/srv" && "$SSD_DATA_DIR" != "$PROJECT_ROOT" ]]; then
        run_sudo rm -rf "${SSD_DATA_DIR:?}"/* || true
    fi
    if [[ -d "$RCLONE_CACHE_DIR" && "$RCLONE_CACHE_DIR" != "/" && "$RCLONE_CACHE_DIR" != "/srv" && "$RCLONE_CACHE_DIR" != "$PROJECT_ROOT" ]]; then
        run_sudo rm -rf "${RCLONE_CACHE_DIR:?}"/* || true
    fi
    if [[ -d "$S3_GATEWAY_TEMP_UPLOADS_DIR" && "$S3_GATEWAY_TEMP_UPLOADS_DIR" != "/" && "$S3_GATEWAY_TEMP_UPLOADS_DIR" != "/srv" && "$S3_GATEWAY_TEMP_UPLOADS_DIR" != "$PROJECT_ROOT" ]]; then
        run_sudo rm -rf "${S3_GATEWAY_TEMP_UPLOADS_DIR:?}"/* || true
    fi

    # Step 5. Run full go prune
    echo "🧹 Surgical cleanup of build caches..."
    cmd_prune --all || true

    # Step 6. Remote Google Drive Storage Purge
    echo "🌐 Purging remote Google Drive storage..."
    local RCLONE_REMOTE=$(grep '^VAULT_RCLONE_REMOTE=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' | xargs || echo "ALPHA_UNION:")
    local BUCKET_NAME=$(grep '^VAULT_AWS_BUCKET=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' | xargs || echo "vault")
    local REMOTE_PATH="${RCLONE_REMOTE}${BUCKET_NAME}"
    
    if [[ "$REMOTE_PATH" == *":"* ]]; then
        echo "   -> Running rclone purge on remote path: $REMOTE_PATH..."
        rclone --config "$RCLONE_CONFIG" purge "$REMOTE_PATH" || true
        rclone --config "$RCLONE_CONFIG" mkdir "$REMOTE_PATH" || true
    else
        echo "⚠️  SAFEGUARD: Refusing to purge non-remote path: '$REMOTE_PATH'"
    fi

    echo -e "\n\033[1;32m✅ Local databases, caches, and remote Google Drive successfully purged!\033[0m"
    echo -e "\033[1;36m🔄 Initiating Octor rebuilding phase...\033[0m\n"

    # Step 7. Trigger a full cmd_mode switch with rebuild, docker up, rclone cache clear, and force options!
    cmd_mode "$CURRENT_MODE" --docker --build --rclone --force
}

# ------------------------------------------------------------------------------
# ENTRY POINT
# ------------------------------------------------------------------------------

# Reset terminal state to fix rendering issues
stty sane 2>/dev/null || true

COMMAND="${1:-mode}"

case "$COMMAND" in
    mode) [[ $# -gt 0 ]] && shift; cmd_mode "$@" ;;
    benchmark|bench) [[ $# -gt 0 ]] && shift; cmd_benchmark "$@" ;;
    prune) [[ $# -gt 0 ]] && shift; cmd_prune "$@" ;;
    reset|factory-reset|--factory-reset) cmd_factory_reset ;;
    install) cmd_install ;;
    build) cmd_build ;;
    status) cmd_status ;;
    doctor) cmd_doctor ;;
    enrich) [[ $# -gt 0 ]] && shift; cmd_enrich "$@" ;;
    stop) stop_all_octor ;;
    start) cmd_mode "$(grep '^OCTOR_PERFORMANCE_MODE=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r')" ;;
    help|-h|--help)
        echo "Usage: ./run.sh [COMMAND] [ARGS]"
        echo "Commands:"
        echo "  mode [1-10|NAME] [FLAGS]   Switch performance mode"
        echo "                             (Interactive: just run './run.sh mode')"
        echo "                             (e.g.: './run.sh mode 1 f r d')"
        echo "  bench [all|1-10|NAME] [--magnet \"LINK\"]"
        echo "                             Run performance benchmark"
        echo "                             (e.g.: './run.sh bench all -m \"magnet:...\"')"
        echo "  prune [--all]              Clean Go caches and local binaries"
        echo "  enrich refresh [DAYS]      Smart refresh — stale/missing (default 7d)"
        echo "  enrich run                 Enrich resources missing metadata only"
        echo "  enrich force-all           Force re-enrich everything (incl. abandoned)"
        echo "  reset                      🛑 Perform a DESTRUCTIVE Factory Reset on local"
        echo "                             databases, caches, and remote Google Drive, and"
        echo "                             recompile/rebuild everything cleanly."
        echo "  install                    Setup/Update systemd service files"
        echo "  build                      Compile all microservices"
        echo "  status                     Show current system health"
        echo "  doctor                     Troubleshoot Docker/System issues"
        echo "  stop                       Emergency stop for all Octor components"
        echo "  start                      Start Octor in the currently configured mode"
        echo ""
        echo "Flags (for 'mode'):"
        echo "  -f, --force, --f, f        Force kill ghosts & zombie mounts"
        echo "  -r, --rclone, r            Clear local rclone VFS cache"
        echo "  -d, --docker, d            Rebuild/Restart Docker infrastructure"
        echo "  -s, --sync, s              Sync systemd service files"
        echo "  -b, --bench, --b, b        Enable benchmark mode for the selection"
        echo "  -n, --nginx, n             Restart host Nginx (Fixes 502 errors)"
        echo "  -c, --build, c             Compile binaries from source"
        ;;
    *) echo "❌ Unknown command: $COMMAND. Use './run.sh help'"; exit 1 ;;
esac

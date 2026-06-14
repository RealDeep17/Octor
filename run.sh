#!/bin/bash
# ==============================================================================
# OCTOR UNIVERSAL RUNNER (run.sh)
# ==============================================================================
# Unified management script for Octor: Build, Mode Switching, Benchmarking,
# Maintenance, and Service Orchestration.
# ==============================================================================

set -euo pipefail

# Reset terminal state to fix potential "staircase" rendering issues on start and exit
stty sane 2>/dev/null || true

# Process-specific temporary directory to avoid sharing/permission conflicts
TEMP_DIR="/tmp/octor-run-$$"
mkdir -p "$TEMP_DIR"
trap 'stty sane 2>/dev/null || true; rm -rf "$TEMP_DIR"' EXIT


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

# Helper function to safely load environment variables with spaces
load_env() {
    local file="$1"
    if [[ -f "$file" ]]; then
        while IFS= read -r line || [[ -n "$line" ]]; do
            # Trim leading and trailing whitespace
            line=$(echo "$line" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
            # Skip empty lines or comments
            if [[ -z "$line" || "$line" =~ ^# ]]; then
                continue
            fi
            # Strip inline comments (keep anything before the first #)
            line="${line%%#*}"
            # Trim trailing whitespace again after comment stripping
            line=$(echo "$line" | sed -e 's/[[:space:]]*$//')
            if [[ -z "$line" ]]; then
                continue
            fi
            # Extract key and value
            if [[ "$line" =~ ^([A-Za-z0-9_]+)=(.*)$ ]]; then
                local key="${BASH_REMATCH[1]}"
                local val="${BASH_REMATCH[2]}"
                # Strip surrounding double quotes
                if [[ "$val" =~ ^\"(.*)\"$ ]]; then
                    val="${BASH_REMATCH[1]}"
                # Strip surrounding single quotes
                elif [[ "$val" =~ ^\'(.*)\'$ ]]; then
                    val="${BASH_REMATCH[1]}"
                fi
                export "$key=$val"
            fi
        done < "$file"
    fi
}


# Extract key paths from custom.env or set defaults
RCLONE_CONFIG=$(grep '^VAULT_RCLONE_CONFIG=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "$HOME/.config/rclone/rclone.conf")
DATA_DIR=$(grep '^DATA_DIR=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "/mnt/seeder-cache")
STORAGE_BASE=$(grep '^VAULT_STORAGE_PATH=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' | sed "s|/drive-mount-vfs||" || echo "$PROJECT_ROOT/infra-data")

run_sudo() {
    sudo "$@"
}

run_sed_in_place() {
    local expr="$1"
    local file="$2"
    if sed --version >/dev/null 2>&1; then
        run_sudo sed -i "$expr" "$file"
    else
        run_sudo sed -i "" "$expr" "$file"
    fi
}

get_cpu_usage() {
    if [ "$(uname -s)" = "Darwin" ]; then
        top -l 1 | awk '/CPU usage/ {split($7, a, "%"); print 100 - a[1]}'
    else
        top -bn1 | grep "Cpu(s)" | sed "s/.*, *\([0-9.]*\)%* id.*/\1/" | awk '{print 100 - $1}'
    fi
}

get_ram_usage() {
    if [ "$(uname -s)" = "Darwin" ]; then
        local page_size
        page_size=$(vm_stat | grep "page size of" | awk '{print $8}' | tr -d '.')
        page_size=${page_size:-16384}
        local active
        active=$(vm_stat | grep "Pages active:" | awk '{print $3}' | tr -d '.')
        local speculative
        speculative=$(vm_stat | grep "Pages speculative:" | awk '{print $3}' | tr -d '.')
        local wired
        wired=$(vm_stat | grep "Pages wired down:" | awk '{print $4}' | tr -d '.')
        local compressed
        compressed=$(vm_stat | grep "Pages occupied by compressor:" | awk '{print $5}' | tr -d '.')
        compressed=${compressed:-0}
        local used_pages=$((active + speculative + wired + compressed))
        local used_mb=$((used_pages * page_size / 1024 / 1024))
        echo "$used_mb"
    else
        free -m | awk '/Mem:/ {print $3}'
    fi
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
        local ok=false
        if sudo docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^octor-monolith$"; then
            if sudo docker exec octor-monolith curl -sf --max-time 5 "$url" > /dev/null 2>&1; then
                ok=true
            fi
        else
            if curl -sf --max-time 5 "$url" > /dev/null 2>&1; then
                ok=true
            fi
        fi

        if [ "$ok" = "true" ]; then
            echo "✓"
            return 0
        fi
        echo "⚠️"
        if [ $round -lt $max_rounds ]; then
            if ! sudo docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^octor-monolith$"; then
                run_sudo systemctl restart "$owner_svc" 2>/dev/null || true
            fi
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
    run_sudo systemctl stop "octor-*" 2>/dev/null || true
    if sudo docker ps -a --format '{{.Names}}' 2>/dev/null | grep -q "^octor-monolith$"; then
        run_sudo docker stop octor-monolith >/dev/null 2>&1 || true
    fi
    echo "✓ Services stopped."
}

kill_ghosts() {
    echo "⚠️  Hunting ghost processes and zombie mounts..."
    GHOST_NAMES=("rest-api" "web-ui" "vault" "abuse-store" "claims-provider" "torrent-store" "url-store" "video-info" "torrent-archiver" "srt2vtt" "content-transcoder" "magnet2torrent" "torrent-web-seeder" "content-prober" "torrent-http-proxy" "torrent-web-seeder-cleaner" "s3-gateway" "sidecar" "ai-proxy")
    # Use exact name matching instead of greedy command-line matching (-f) to prevent accidental system kills
    for name in "${GHOST_NAMES[@]}"; do run_sudo pkill -9 "^${name}$" 2>/dev/null || true; done
    run_sudo pkill -9 "^rclone$" 2>/dev/null || true
    run_sudo pkill -9 "^uvicorn$" 2>/dev/null || true
    run_sudo pkill -9 -f "proxy.js" 2>/dev/null || true
    
    # Reset terminal state in case any killed process left it in raw mode
    stty sane 2>/dev/null || true

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
    for loop in $(losetup -a | grep "seeder-cache.img" | cut -d: -f1 || true); do run_sudo losetup -d "$loop" || true; done
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
    # Keep only last 10 backups
    ls -dt "$BACKUP_DIR"/* | tail -n +11 | xargs rm -f -- 2>/dev/null || true
}

# ------------------------------------------------------------------------------
# SUBCOMMAND: mode (Performance Mode Switching)
# ------------------------------------------------------------------------------

cmd_mode() {
    local MODE_ARG=""
    if [[ $# -gt 0 && ! "$1" =~ ^- ]]; then
        MODE_ARG="$1"
        shift
    fi

    local CLEAN_RCLONE=false
    local FORCE=false
    local REBUILD_DOCKER=false
    local SYNC_SERVICES=false
    local BENCH_FLAG=false
    local RESTART_NGINX=false
    local COMPILE_BINARIES=false

    # Parse initial flags (if any were passed before prompt)
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

    # If no mode was specified, prompt the user
    if [[ -z "$MODE_ARG" ]]; then
        echo "========================================================================="
        echo "                      OCTOR PERFORMANCE MODE SELECTOR"
        echo "========================================================================="
        echo "  ⚠️  Note: 1-4: Sequential upload (Single Part). Small storage friendly."
        echo "            5-6: Parallel upload (Single Part). Requires cache space > largest file."
        echo "            7-10: Parallel upload (Parallel Part). Nested mount. Splits files."
        echo "-------------------------------------------------------------------------"
        echo "Manual Override Mode:"
        echo "  0) custom              [Preserve and apply manual edits in custom.env]"
        echo "SSD Single Part (VFS Cache Off)   [Low RAM, sequential upload, safe single files]:"
        echo "  1) sp-perf-ssd         2) sp-eco-ssd"
        echo "RAM Single Part (VFS Cache Off)   [Saves SSD life, sequential upload, safe single files]:"
        echo "  3) sp-perf-ram         4) sp-eco-ram"
        echo "SSD Single Part (VFS Cache Write)  [Parallel upload. Requires local cache > largest file]:"
        echo "  5) sp-write-perf-ssd   6) sp-write-eco-ssd"
        echo "RAM Parallel Part (VFS Chunker)    [Parallel upload, nested mount, split files (incompatible)]:"
        echo "  7) pp-perf-ram         8) pp-eco-ram"
        echo "SSD Parallel Part (VFS Chunker)    [Parallel upload, nested mount, split files (incompatible)]:"
        echo "  9) pp-perf-ssd        10) pp-eco-ssd"
        echo "-------------------------------------------------------------------------"
        echo "Optional Flags (can be combined):"
        echo "  f = Force kill ghosts    d = Rebuild Docker"
        echo "  r = Clear Rclone cache   s = Sync Systemd"
        echo "  b = Run in Bench Mode    n = Restart Nginx"
        echo "  c = Compile Binaries"
        echo "========================================================================="
        read -p "Select option and flags (e.g. '1 f r d n c'): " INPUT || exit 1
        
        # Split inputs to extract mode and additional flags
        local INPUT_ARR=($INPUT)
        if [[ ${#INPUT_ARR[@]} -gt 0 ]]; then
            MODE_ARG="${INPUT_ARR[0]}"
            # Parse any additional flags entered during prompt
            for ((i=1; i<${#INPUT_ARR[@]}; i++)); do
                case "${INPUT_ARR[$i]}" in
                    --rclone|-r|r) CLEAN_RCLONE=true ;;
                    --force|-f|--f|f) FORCE=true ;;
                    --docker|-d|d) REBUILD_DOCKER=true ;;
                    --sync|-s|s) SYNC_SERVICES=true ;;
                    --bench|-b|--b|b) BENCH_FLAG=true ;;
                    --nginx|-n|n) RESTART_NGINX=true ;;
                    --build|-c|c) COMPILE_BINARIES=true ;;
                    *) echo "❌ Unknown option: ${INPUT_ARR[$i]}"; exit 1 ;;
                esac
            done
        fi
    fi

    if [[ -z "$MODE_ARG" ]]; then echo "❌ No mode specified"; exit 1; fi

    local MODE=""
    if [[ "$MODE_ARG" =~ ^[0-9]+$ ]]; then
        case "$MODE_ARG" in
            0) MODE="custom" ;;
            1) MODE="sp-perf-ssd" ;;
            2) MODE="sp-eco-ssd" ;;
            3) MODE="sp-perf-ram" ;;
            4) MODE="sp-eco-ram" ;;
            5) MODE="sp-write-perf-ssd" ;;
            6) MODE="sp-write-eco-ssd" ;;
            7) MODE="pp-perf-ram" ;;
            8) MODE="pp-eco-ram" ;;
            9) MODE="pp-perf-ssd" ;;
            10) MODE="pp-eco-ssd" ;;
            *) echo "❌ Invalid selection: $MODE_ARG"; exit 1 ;;
        esac
    else
        MODE="$MODE_ARG"
    fi

    # 3. Transition to Benchmark if requested and not already in benchmark loop
    if [[ "$BENCH_FLAG" = "true" && "${BENCHMARK_MODE:-false}" != "true" ]]; then
        cmd_benchmark "$MODE"
        return
    fi

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
        # Custom/Manual Mode (read existing config from ENV file)
        custom|manual)
            # 1. Check if we should prompt for interactive customization
            local MODIFY_CUSTOM="y"
            if [[ "$CLEAN_RCLONE" = "true" || "$FORCE" = "true" || "$REBUILD_DOCKER" = "true" || "$SYNC_SERVICES" = "true" || "$BENCH_FLAG" = "true" || "$RESTART_NGINX" = "true" || "$COMPILE_BINARIES" = "true" ]]; then
                MODIFY_CUSTOM="n"
            fi
            if [[ ! -t 0 ]]; then
                MODIFY_CUSTOM="n"
            fi

            local CURRENT_PERF_MODE=$(grep '^OCTOR_PERFORMANCE_MODE=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)
            if [[ "$MODIFY_CUSTOM" = "y" && -n "$CURRENT_PERF_MODE" ]]; then
                echo "Found existing custom performance configuration (Mode: $CURRENT_PERF_MODE)."
                read -p "Modify it? (y/n) [n]: " MODIFY_CHOICE
                MODIFY_CHOICE=${MODIFY_CHOICE:-n}
                if [[ ! "$MODIFY_CHOICE" =~ ^[yY]$ ]]; then
                    MODIFY_CUSTOM="n"
                fi
            fi

            if [[ "$MODIFY_CUSTOM" = "y" ]]; then
                echo "========================================================================"
                echo "                  CUSTOM PERFORMANCE MODE CONFIGURATOR"
                echo "========================================================================"
                echo ""

                update_custom_var() {
                    local key="$1"
                    local val="$2"
                    local esc_val=$(echo "$val" | sed 's/[&/\]/\\&/g')
                    if grep -q "^$key=" "$ENV_FILE"; then
                        run_sed_in_place "s|^$key=.*|$key=$esc_val|g" "$ENV_FILE"
                    else
                        echo "$key=$val" >> "$ENV_FILE"
                    fi
                }

                # 1. Storage Cache Type (RAM vs SSD)
                echo "💾 1. Storage Cache Type"
                echo "--------------------------------------------------------"
                echo "SSD Cache:"
                echo "  • Benefits: Reboot-persistent, no RAM overhead, large size."
                echo "  • Drawbacks: Causes SSD write wear, slightly slower than RAM."
                echo "RAM Cache:"
                echo "  • Benefits: Zero SSD wear, maximum speed, and allows downloading"
                echo "              torrents of infinite size on a tiny disk space due to real-time eviction."
                echo "  • Drawbacks: Volatile (lost on reboot), high memory footprint."
                echo "  • Note: For RAM caching, it is highly recommended to use the preconfigured"
                echo "          RAM modes (Options 3, 4, 7, or 8) instead of Custom configuration."
                echo "--------------------------------------------------------"
                local CACHE_MEDIA=""
                while [[ ! "$CACHE_MEDIA" =~ ^[12]$ ]]; do
                    read -p "Select caching media: [1] SSD (Recommended), [2] RAM: " CACHE_MEDIA
                    CACHE_MEDIA=${CACHE_MEDIA:-1}
                done

                local VAL_RAM_CACHE_ENABLED="false"
                local VAL_PER_TORRENT_CACHE_BUDGET=""
                local VAL_RCLONE_VFS_CACHE_MAX_SIZE=""
                local VAL_RCLONE_VFS_CACHE_MODE=""
                local VAL_RAM_CACHE_SIZE=""
                local VAL_TMPFS_SIZE=""
                local VAL_OCTOR_PERFORMANCE_MODE=""

                if [[ "$CACHE_MEDIA" = "2" ]]; then
                    VAL_RAM_CACHE_ENABLED="true"
                    VAL_OCTOR_PERFORMANCE_MODE="custom-vfs-ram"
                    echo ""
                    echo "📦 2. RAM Cache Size limit"
                    echo "--------------------------------------------------------"
                    echo "⚠️  Recommend keeping budget to 10G-25G to prevent system Out-Of-Memory (OOM)."
                    local INPUT_RAM_SIZE=""
                    read -p "Enter RAM cache size (e.g. 10G) [default: 12288M]: " INPUT_RAM_SIZE
                    VAL_RAM_CACHE_SIZE=${INPUT_RAM_SIZE:-12288M}
                    
                    local INPUT_TMPFS_SIZE=""
                    read -p "Enter tmpfs backing size (must be >= RAM cache size, e.g. 15G) [default: 17G]: " INPUT_TMPFS_SIZE
                    VAL_TMPFS_SIZE=${INPUT_TMPFS_SIZE:-17G}
                    
                    update_custom_var "RAM_CACHE_ENABLED" "true"
                    update_custom_var "RAM_CACHE_SIZE" "$VAL_RAM_CACHE_SIZE"
                    update_custom_var "TMPFS_SIZE" "$VAL_TMPFS_SIZE"
                else
                    VAL_RAM_CACHE_ENABLED="false"
                    VAL_OCTOR_PERFORMANCE_MODE="custom-vfs-ssd"
                    update_custom_var "RAM_CACHE_ENABLED" "false"
                fi
                echo ""

                # 2. Local VFS Cache Mode
                echo "🔄 2. Local VFS Cache Mode (Rclone)"
                echo "--------------------------------------------------------"
                echo "writes (Recommended):"
                echo "  • Benefits: Faster uploads, smoother streaming, superior experience."
                echo "  • Drawbacks: Torrent size MUST be smaller than available local cache space."
                echo "off:"
                echo "  • Benefits: Enormous disk space savings. Bypasses local write cache."
                echo "              Allows downloading massive files (e.g. 100GB torrent) on a tiny"
                echo "              local storage budget (e.g. 2GB) by uploading and evicting"
                echo "              pieces in real-time as they download."
                echo "  • Drawbacks: Slower uploads, no multi-threaded parallel uploads."
                echo "full (NOT RECOMMENDED):"
                echo "  • Benefits: Caches both reads and writes."
                echo "  • Drawbacks: Heavy disk space usage and high SSD write wear."
                echo "--------------------------------------------------------"
                local VFS_CHOICE=""
                while [[ ! "$VFS_CHOICE" =~ ^[123]$ ]]; do
                    read -p "Select local VFS cache mode: [1] writes (Recommended), [2] off, [3] full (NOT RECOMMENDED): " VFS_CHOICE
                    VFS_CHOICE=${VFS_CHOICE:-1}
                done

                if [[ "$VFS_CHOICE" = "1" ]]; then
                    VAL_RCLONE_VFS_CACHE_MODE="writes"
                elif [[ "$VFS_CHOICE" = "2" ]]; then
                    VAL_RCLONE_VFS_CACHE_MODE="off"
                else
                    VAL_RCLONE_VFS_CACHE_MODE="full"
                fi
                update_custom_var "RCLONE_VFS_CACHE_MODE" "$VAL_RCLONE_VFS_CACHE_MODE"
                echo ""

                # 3. Cache Budget Limit
                if [[ "$VAL_RCLONE_VFS_CACHE_MODE" != "off" ]]; then
                    echo "📊 3. Local Cache Size Limit"
                    echo "--------------------------------------------------------"
                    if [[ "$VAL_RAM_CACHE_ENABLED" = "true" ]]; then
                        echo "Suggest a small VFS limit that fits within your RAM Cache (e.g. 3G or 4G)."
                        local INPUT_VFS_LIMIT=""
                        read -p "Enter VFS cache max size (e.g. 3G) [default: 3G]: " INPUT_VFS_LIMIT
                        VAL_RCLONE_VFS_CACHE_MAX_SIZE=${INPUT_VFS_LIMIT:-3G}
                    else
                        echo "Suggest a size of 50G-200G depending on available SSD space."
                        local INPUT_VFS_LIMIT=""
                        read -p "Enter VFS cache max size (e.g. 100G) [default: 100G]: " INPUT_VFS_LIMIT
                        VAL_RCLONE_VFS_CACHE_MAX_SIZE=${INPUT_VFS_LIMIT:-100G}
                    fi
                else
                    VAL_RCLONE_VFS_CACHE_MAX_SIZE="off"
                fi
                update_custom_var "RCLONE_VFS_CACHE_MAX_SIZE" "$VAL_RCLONE_VFS_CACHE_MAX_SIZE"
                echo ""

                # 4. Per-Torrent Cache Budget
                echo "💾 4. Per-Torrent Local Cache Budget"
                echo "--------------------------------------------------------"
                if [[ "$VAL_RCLONE_VFS_CACHE_MODE" != "off" ]]; then
                    echo "VFS cache mode '$VAL_RCLONE_VFS_CACHE_MODE' is selected."
                    echo "Rclone manages caching at the mount level, so per-torrent cache budget is forced to 0."
                    VAL_PER_TORRENT_CACHE_BUDGET="0"
                else
                    echo "VFS cache mode is 'off' (sequential upload to cloud)."
                    echo "You can configure a per-torrent local write budget."
                    echo "Suggest 2000M to 5000M depending on available space."
                    local INPUT_TORRENT_BUDGET=""
                    read -p "Enter cache limit per torrent (e.g. 2000M) [default: 2000M]: " INPUT_TORRENT_BUDGET
                    VAL_PER_TORRENT_CACHE_BUDGET=${INPUT_TORRENT_BUDGET:-2000M}
                fi
                update_custom_var "PER_TORRENT_CACHE_BUDGET" "$VAL_PER_TORRENT_CACHE_BUDGET"
                echo ""

                # 5. Upload Worker Threads (WORKERS)
                echo "🧵 5. Upload Worker Threads"
                echo "--------------------------------------------------------"
                echo "Number of background queue workers processing file uploads."
                echo "  • Low (10-20): Friendly for weak CPU/RAM configurations."
                echo "  • High (50-100): Speeds up upload queue processing on high-end servers."
                local INPUT_WORKERS=""
                read -p "Enter number of upload worker threads [default: 20]: " INPUT_WORKERS
                INPUT_WORKERS=${INPUT_WORKERS:-20}
                update_custom_var "WORKERS" "$INPUT_WORKERS"
                echo ""

                # 6. Max Concurrent Upload Jobs
                echo "🚀 6. Max Concurrent Upload Jobs"
                echo "--------------------------------------------------------"
                echo "Number of torrents allowed to upload to cloud storage in parallel."
                echo "  • Low (2-5): Good for low-bandwidth connections."
                echo "  • High (10-50): Speeds up parallel torrent processing on high-bandwidth servers."
                local INPUT_CONC_JOBS=""
                read -p "Enter max concurrent uploads [default: 10]: " INPUT_CONC_JOBS
                INPUT_CONC_JOBS=${INPUT_CONC_JOBS:-10}
                update_custom_var "VAULT_MAX_CONCURRENT_JOBS" "$INPUT_CONC_JOBS"
                echo ""

                # 7. AWS Upload Concurrency
                echo "📤 7. AWS Upload Concurrency"
                echo "--------------------------------------------------------"
                echo "Number of parallel parts uploaded per file to S3/Drive."
                echo "  • Low (1-2): Safe for API rate limits and low upload bandwidth."
                echo "  • High (4-8): Faster single-file uploads, but risks API throttling."
                local INPUT_AWS_CONC=""
                read -p "Enter upload parts concurrency per file [default: 2]: " INPUT_AWS_CONC
                INPUT_AWS_CONC=${INPUT_AWS_CONC:-2}
                update_custom_var "AWS_UPLOAD_CONCURRENCY" "$INPUT_AWS_CONC"
                echo ""

                # 8. Transfers Concurrency
                echo "🔄 8. Transfers Concurrency"
                echo "--------------------------------------------------------"
                echo "Number of parallel file transfers allowed by Rclone."
                echo "  • Low (1-2): Safe for API rate limits and low bandwidth."
                echo "  • High (3-5): Faster parallel uploads, but risks cloud API throttling."
                local INPUT_TRANSFERS=""
                read -p "Enter max simultaneous file transfers [default: 2]: " INPUT_TRANSFERS
                INPUT_TRANSFERS=${INPUT_TRANSFERS:-2}
                update_custom_var "RCLONE_TRANSFERS" "$INPUT_TRANSFERS"
                echo ""

                # Set other helper parameters based on VFS Cache mode
                local VAL_POLL_INTERVAL="1h"
                if [[ "$VAL_RCLONE_VFS_CACHE_MODE" != "off" ]]; then
                    VAL_POLL_INTERVAL="10s"
                fi
                update_custom_var "RCLONE_VFS_CACHE_POLL_INTERVAL" "$VAL_POLL_INTERVAL"
                update_custom_var "OCTOR_PERFORMANCE_MODE" "$VAL_OCTOR_PERFORMANCE_MODE"

                # Also update S3_GATEWAY_STORAGE_DIR and VAULT_STORAGE_PATH
                local VAL_STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount-vfs"
                update_custom_var "VAULT_STORAGE_PATH" "$VAL_STORAGE_PATH"
                update_custom_var "S3_GATEWAY_STORAGE_DIR" "$VAL_STORAGE_PATH"

                unset -f update_custom_var

                echo "--------------------------------------------------------"
                echo "✓ Custom performance mode configured! custom.env updated."
                echo "========================================================"
                echo ""
            fi

            # Read all variables from the updated/existing custom.env
            MODE_NAME=$(grep '^OCTOR_PERFORMANCE_MODE=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "non-chunker-vfs-ssd")
            RAM_CACHE_ENABLED=$(grep '^RAM_CACHE_ENABLED=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "false")
            BUDGET=$(grep '^PER_TORRENT_CACHE_BUDGET=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "2000M")
            VFS_LIMIT=$(grep '^RCLONE_VFS_CACHE_MAX_SIZE=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "off")
            VFS_CACHE_MODE=$(grep '^RCLONE_VFS_CACHE_MODE=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "off")
            STORAGE_PATH=$(grep '^VAULT_STORAGE_PATH=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "$PROJECT_ROOT/infra-data/drive-mount-vfs")
            USE_CHUNKER=false
            if [[ "$MODE_NAME" =~ ^chunker-vfs- ]]; then
                USE_CHUNKER=true
            fi
            POLL_INTERVAL=$(grep '^RCLONE_VFS_CACHE_POLL_INTERVAL=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "1h")
            CHUNK_SIZE=""
            CONCURRENCY=$(grep '^AWS_UPLOAD_CONCURRENCY=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "1")
            RCLONE_BUFFER=$(grep '^RCLONE_BUFFER_SIZE=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "64M")
            RCLONE_DRIVE_CHUNK=$(grep '^RCLONE_DRIVE_CHUNK_SIZE=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "128M")
            RCLONE_XFERS=$(grep '^RCLONE_TRANSFERS=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "2")
            ;;

        # RAM Modes (VFS Cache Off)
        sp-perf-ram|steady|ram-steady|non-chunker-vfs-ram|ram-vfs) MODE_NAME="non-chunker-vfs-ram"; RAM_CACHE_ENABLED=true; BUDGET="2000M"; VFS_LIMIT="off"; VFS_CACHE_MODE="off"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount-vfs"; USE_CHUNKER=false; POLL_INTERVAL="1h"; CHUNK_SIZE=""; CONCURRENCY=1; RCLONE_BUFFER="128M"; RCLONE_DRIVE_CHUNK="256M"; RCLONE_XFERS=10 ;;
        sp-eco-ram|eco|ram-eco|eco-ram) MODE_NAME="non-chunker-vfs-ram"; RAM_CACHE_ENABLED=true; BUDGET="1000M"; VFS_LIMIT="off"; VFS_CACHE_MODE="off"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount-vfs"; USE_CHUNKER=false; POLL_INTERVAL="1h"; CHUNK_SIZE=""; CONCURRENCY=1; RCLONE_BUFFER="32M"; RCLONE_DRIVE_CHUNK="64M"; RCLONE_XFERS=5 ;;

        # RAM Modes (VFS Chunker - writes)
        pp-perf-ram|perf|ram-perf|chunker-vfs-ram|ram|ram-chunker) MODE_NAME="chunker-vfs-ram"; RAM_CACHE_ENABLED=true; BUDGET="2000M"; VFS_LIMIT="3G"; VFS_CACHE_MODE="writes"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount"; USE_CHUNKER=true; POLL_INTERVAL="1s"; CHUNK_SIZE="256M"; CONCURRENCY=2; RCLONE_BUFFER="64M"; RCLONE_DRIVE_CHUNK="128M"; RCLONE_XFERS=10 ;;
        pp-eco-ram) MODE_NAME="chunker-vfs-ram"; RAM_CACHE_ENABLED=true; BUDGET="1000M"; VFS_LIMIT="2G"; VFS_CACHE_MODE="writes"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount"; USE_CHUNKER=true; POLL_INTERVAL="1s"; CHUNK_SIZE="256M"; CONCURRENCY=2; RCLONE_BUFFER="32M"; RCLONE_DRIVE_CHUNK="64M"; RCLONE_XFERS=5 ;;

        # SSD Modes (VFS Cache Off)
        sp-perf-ssd|ssd-steady|non-chunker-vfs-ssd|ssd-vfs) MODE_NAME="non-chunker-vfs-ssd"; RAM_CACHE_ENABLED=false; BUDGET="6000M"; VFS_LIMIT="off"; VFS_CACHE_MODE="off"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount-vfs"; USE_CHUNKER=false; POLL_INTERVAL="1h"; CHUNK_SIZE=""; CONCURRENCY=1; RCLONE_BUFFER="128M"; RCLONE_DRIVE_CHUNK="256M"; RCLONE_XFERS=10 ;;
        sp-eco-ssd|ssd-eco) MODE_NAME="non-chunker-vfs-ssd"; RAM_CACHE_ENABLED=false; BUDGET="3000M"; VFS_LIMIT="off"; VFS_CACHE_MODE="off"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount-vfs"; USE_CHUNKER=false; POLL_INTERVAL="1h"; CHUNK_SIZE=""; CONCURRENCY=1; RCLONE_BUFFER="32M"; RCLONE_DRIVE_CHUNK="64M"; RCLONE_XFERS=5 ;;

        # SSD Modes (VFS Cache Write - no per-torrent cache budget, i.e., 0)
        sp-write-perf-ssd) MODE_NAME="non-chunker-vfs-ssd"; RAM_CACHE_ENABLED=false; BUDGET="0"; VFS_LIMIT="100G"; VFS_CACHE_MODE="writes"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount-vfs"; USE_CHUNKER=false; POLL_INTERVAL="10s"; CHUNK_SIZE=""; CONCURRENCY=4; RCLONE_BUFFER="128M"; RCLONE_DRIVE_CHUNK="256M"; RCLONE_XFERS=10 ;;
        sp-write-eco-ssd) MODE_NAME="non-chunker-vfs-ssd"; RAM_CACHE_ENABLED=false; BUDGET="0"; VFS_LIMIT="50G"; VFS_CACHE_MODE="writes"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount-vfs"; USE_CHUNKER=false; POLL_INTERVAL="1h"; CHUNK_SIZE=""; CONCURRENCY=2; RCLONE_BUFFER="64M"; RCLONE_DRIVE_CHUNK="128M"; RCLONE_XFERS=5 ;;

        # SSD Modes (VFS Chunker - writes)
        pp-perf-ssd|ssd-perf|chunker-vfs-ssd|ssd|ssd-chunker) MODE_NAME="chunker-vfs-ssd"; RAM_CACHE_ENABLED=false; BUDGET="2000M"; VFS_LIMIT="8G"; VFS_CACHE_MODE="writes"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount"; USE_CHUNKER=true; POLL_INTERVAL="10s"; CHUNK_SIZE="1G"; CONCURRENCY=2; RCLONE_BUFFER="64M"; RCLONE_DRIVE_CHUNK="128M"; RCLONE_XFERS=10 ;;
        pp-eco-ssd) MODE_NAME="chunker-vfs-ssd"; RAM_CACHE_ENABLED=false; BUDGET="1000M"; VFS_LIMIT="4G"; VFS_CACHE_MODE="writes"; STORAGE_PATH="$PROJECT_ROOT/infra-data/drive-mount"; USE_CHUNKER=true; POLL_INTERVAL="10s"; CHUNK_SIZE="512M"; CONCURRENCY=2; RCLONE_BUFFER="32M"; RCLONE_DRIVE_CHUNK="64M"; RCLONE_XFERS=5 ;;
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
                read -p "Are you absolutely sure you want to proceed with this switch? [y/N]: " CONFIRM || exit 1
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
                read -p "Do you want to proceed with this switch? [y/N]: " CONFIRM || exit 1
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
        # Copy service files and resolve all template placeholders:
        #   __PROJECT_ROOT__  → actual project root path
        #   __RUN_USER__      → current user (file owner of run.sh)
        #   __RUN_GROUP__     → current primary group
        #   /srv/octor        → legacy alias (resolved for backward compat)
        local SYNC_USER SYNC_GROUP
        SYNC_USER=$(stat -c '%U' "$PROJECT_ROOT/run.sh" 2>/dev/null || stat -f '%Su' "$PROJECT_ROOT/run.sh" 2>/dev/null || echo "$USER")
        SYNC_GROUP=$(id -gn "$SYNC_USER" 2>/dev/null || id -gn)
        for svc in "$PROJECT_ROOT"/deploy/systemd/octor-*.service; do
            local svc_name
            svc_name=$(basename "$svc")
            sed \
                -e "s|__PROJECT_ROOT__|$PROJECT_ROOT|g" \
                -e "s|__RUN_USER__|$SYNC_USER|g" \
                -e "s|__RUN_GROUP__|$SYNC_GROUP|g" \
                -e "s|/srv/octor|$PROJECT_ROOT|g" \
                "$svc" > "$TEMP_DIR"/"$svc_name"
            run_sudo cp "$TEMP_DIR"/"$svc_name" /etc/systemd/system/"$svc_name"
        done
        # Copy cron scripts to cron.weekly (removing .cron extension so run-parts accepts them)
        local RUN_USER
        RUN_USER=$(stat -c '%U' "$PROJECT_ROOT/run.sh" 2>/dev/null || stat -f '%Su' "$PROJECT_ROOT/run.sh" || echo "ubuntu")
        
        sed -e "s|__PROJECT_ROOT__|$PROJECT_ROOT|g" -e "s|__RUN_USER__|$RUN_USER|g" \
            "$PROJECT_ROOT"/deploy/cron/octor-enrich-refresh.cron > "$TEMP_DIR"/octor-enrich-refresh
        run_sudo cp "$TEMP_DIR"/octor-enrich-refresh /etc/cron.weekly/octor-enrich-refresh 2>/dev/null || true
        run_sudo chmod +x /etc/cron.weekly/octor-enrich-refresh 2>/dev/null || true
        
        sed -e "s|__PROJECT_ROOT__|$PROJECT_ROOT|g" -e "s|__RUN_USER__|$RUN_USER|g" \
            "$PROJECT_ROOT"/deploy/cron/octor-prune.cron > "$TEMP_DIR"/octor-prune
        run_sudo cp "$TEMP_DIR"/octor-prune /etc/cron.weekly/octor-prune 2>/dev/null || true
        run_sudo chmod +x /etc/cron.weekly/octor-prune 2>/dev/null || true

        sed -e "s|__PROJECT_ROOT__|$PROJECT_ROOT|g" -e "s|__RUN_USER__|$RUN_USER|g" \
            "$PROJECT_ROOT"/deploy/cron/octor-vault-reap.cron > "$TEMP_DIR"/octor-vault-reap
        run_sudo cp "$TEMP_DIR"/octor-vault-reap /etc/cron.daily/octor-vault-reap 2>/dev/null || true
        run_sudo chmod +x /etc/cron.daily/octor-vault-reap 2>/dev/null || true
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
        (cd "$PROJECT_ROOT" && run_sudo docker compose --ansi never down)
        (cd "$PROJECT_ROOT" && run_sudo docker compose --ansi never up -d --build --force-recreate)
        
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
        if [[ "$INFRA_READY" = "false" ]]; then
            echo " ❌"
        fi
    fi

    backup_env
    echo "=== UPDATING ENV FOR MODE: $MODE ==="
    if [[ "$MODE" != "custom" && "$MODE" != "manual" ]]; then
        run_sed_in_place "s|^RAM_CACHE_ENABLED=.*|RAM_CACHE_ENABLED=$RAM_CACHE_ENABLED|" "$ENV_FILE"
        run_sed_in_place "s|^PER_TORRENT_CACHE_BUDGET=.*|PER_TORRENT_CACHE_BUDGET=$BUDGET|" "$ENV_FILE"
        run_sed_in_place "s|^RCLONE_VFS_CACHE_MAX_SIZE=.*|RCLONE_VFS_CACHE_MAX_SIZE=$VFS_LIMIT|" "$ENV_FILE"
        run_sed_in_place "s|^RCLONE_VFS_CACHE_POLL_INTERVAL=.*|RCLONE_VFS_CACHE_POLL_INTERVAL=$POLL_INTERVAL|" "$ENV_FILE"
        run_sed_in_place "s|^RCLONE_VFS_CACHE_MODE=.*|RCLONE_VFS_CACHE_MODE=$VFS_CACHE_MODE|" "$ENV_FILE"
        run_sed_in_place "s|^VAULT_STORAGE_PATH=.*|VAULT_STORAGE_PATH=$STORAGE_PATH|" "$ENV_FILE"
        run_sed_in_place "s|^S3_GATEWAY_STORAGE_DIR=.*|S3_GATEWAY_STORAGE_DIR=$STORAGE_PATH|" "$ENV_FILE"
        run_sed_in_place "s|^OCTOR_PERFORMANCE_MODE=.*|OCTOR_PERFORMANCE_MODE=$MODE_NAME|" "$ENV_FILE"
        run_sed_in_place "s|^AWS_UPLOAD_CONCURRENCY=.*|AWS_UPLOAD_CONCURRENCY=$CONCURRENCY|" "$ENV_FILE"
        run_sed_in_place "s|^RCLONE_BUFFER_SIZE=.*|RCLONE_BUFFER_SIZE=$RCLONE_BUFFER|" "$ENV_FILE"
        run_sed_in_place "s|^RCLONE_DRIVE_CHUNK_SIZE=.*|RCLONE_DRIVE_CHUNK_SIZE=$RCLONE_DRIVE_CHUNK|" "$ENV_FILE"
        run_sed_in_place "s|^RCLONE_CHUNKER_CHUNK_SIZE=.*|RCLONE_CHUNKER_CHUNK_SIZE=$CHUNK_SIZE|" "$ENV_FILE"
        run_sed_in_place "s|^RCLONE_TRANSFERS=.*|RCLONE_TRANSFERS=$RCLONE_XFERS|" "$ENV_FILE"
    else
        echo "ℹ️ Custom Mode: Preserving manually edited values in $ENV_FILE."
    fi

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
        SERVICES=("octor-abuse-store" "octor-url-store" "octor-torrent-store" "octor-magnet2torrent" "octor-rest-api" "octor-vault" "octor-s3-gateway" "octor-torrent-web-seeder")
    else
        SERVICES=("octor-torrent-store" "octor-magnet2torrent" "octor-video-info" "octor-abuse-store" "octor-url-store" "octor-rest-api" "octor-vault" "octor-s3-gateway" "octor-torrent-web-seeder" "octor-torrent-web-seeder-cleaner" "octor-torrent-http-proxy" "octor-content-prober" "octor-content-transcoder" "octor-srt2vtt" "octor-torrent-archiver" "octor-claims-provider" "octor-ai-proxy" "octor-sidecar" "octor-web-ui")
    fi

    echo "=== STARTING MICROSERVICES ==="
    if sudo docker ps -a --format '{{.Names}}' 2>/dev/null | grep -q "^octor-monolith$"; then
        echo "  Detected octor-monolith container. Reloading Docker Compose to apply new env..."
        (cd "$PROJECT_ROOT" && run_sudo docker compose --ansi never up -d)

        # Wait for NATS to be responsive inside monolith container
        echo -n "  Waiting for NATS container connectivity"
        local NATS_READY=false
        for i in {1..30}; do
            if sudo docker exec octor-monolith python3 -c "import os, socket; s = socket.socket(); s.settimeout(1); s.connect((os.getenv('NATS_HOST', 'octor-nats'), int(os.getenv('NATS_PORT', '4222'))))" >/dev/null 2>&1; then
                NATS_READY=true
                echo " ✓"
                break
            fi
            printf "."
            sleep 1
        done
        if [[ "$NATS_READY" = "false" ]]; then
            echo " ❌"
        fi

        echo "=== INITIALIZING NATS JETSTREAM ==="
        sudo docker exec -i octor-monolith /app/bin/create_nats_stream >/dev/null 2>&1 || echo "⚠️  NATS Init failed - services might crash!"
    else
        # In systemd host mode, NATS runs in the octor-nats container. Resolve host/port from custom.env.
        local NATS_SH
        local NATS_SP
        NATS_SH=$(grep '^NATS_SERVICE_HOST=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "localhost")
        NATS_SP=$(grep '^NATS_SERVICE_PORT=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "4222")

        echo -n "  Waiting for NATS host connectivity"
        local NATS_READY=false
        for i in {1..30}; do
            if command -v nc >/dev/null 2>&1; then
                if nc -z "$NATS_SH" "$NATS_SP" >/dev/null 2>&1; then
                    NATS_READY=true
                    echo " ✓"
                    break
                fi
            elif command -v python3 >/dev/null 2>&1; then
                if python3 -c "import socket; s = socket.socket(); s.settimeout(1); s.connect(('$NATS_SH', $NATS_SP))" >/dev/null 2>&1; then
                    NATS_READY=true
                    echo " ✓"
                    break
                fi
            else
                NATS_READY=true
                echo " ✓ (assuming ready)"
                break
            fi
            printf "."
            sleep 1
        done
        if [[ "$NATS_READY" = "false" ]]; then
            echo " ❌"
        fi

        echo "=== INITIALIZING NATS JETSTREAM ==="
        (cd "$PROJECT_ROOT" && go run scripts/create_nats_stream.go >/dev/null) || echo "⚠️  NATS Init failed - services might crash!"

        for svc in "${SERVICES[@]}"; do start_service_with_retry "$svc" 2 10; done
    fi

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

    MODES_LIST=("sp-perf-ssd" "sp-eco-ssd" "sp-perf-ram" "sp-eco-ram" "sp-write-perf-ssd" "sp-write-eco-ssd" "pp-perf-ram" "pp-eco-ram" "pp-perf-ssd" "pp-eco-ssd")
    
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
    local DISK_NAME=""
    if [ "$(uname -s)" != "Darwin" ] && command -v lsblk >/dev/null 2>&1; then
        DISK_NAME=$(lsblk -no pkname "$DEV_PATH" 2>/dev/null | tr -d '\r' | head -n 1)
    fi
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
            rclone --config "$RCLONE_CONFIG" delete "${REMOTE_PATH}/media/" --include "* [${RESOURCE_ID}]/**" --quiet || true
            rclone --config "$RCLONE_CONFIG" rmdirs "${REMOTE_PATH}/media/" --leave-root --quiet || true
        fi

        # Run clean_orphans to clean up non-human-readable vault/ files from remote storage
        if sudo docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^octor-monolith$"; then
            docker exec -i octor-monolith /app/bin/clean_orphans --dry-run=false || true
        else
            "$PROJECT_ROOT/bin/clean_orphans" --dry-run=false || true
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
            local cpu=$(get_cpu_usage)
            local ram=$(get_ram_usage)
            
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
            rclone --config "$RCLONE_CONFIG" delete "${REMOTE_PATH}/media/" --include "* [${RESOURCE_ID}]/**" --quiet || true
            rclone --config "$RCLONE_CONFIG" rmdirs "${REMOTE_PATH}/media/" --leave-root --quiet || true
        fi

        # DB Cleanup (After Round) to orphan the file hashes in the database
        docker exec -i octor-postgres psql -U octor -d vault -c "DELETE FROM resource_file WHERE resource_id = '$RESOURCE_ID';" >/dev/null 2>&1 || true
        docker exec -i octor-postgres psql -U octor -d vault -c "DELETE FROM resource WHERE resource_id = '$RESOURCE_ID';" >/dev/null 2>&1 || true
        docker exec -i octor-postgres psql -U octor -d vault -c "DELETE FROM file WHERE hash NOT IN (SELECT file_hash FROM resource_file);" >/dev/null 2>&1 || true

        # Run clean_orphans to clean up non-human-readable vault/ files immediately
        if sudo docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^octor-monolith$"; then
            docker exec -i octor-monolith /app/bin/clean_orphans --dry-run=false || true
        else
            "$PROJECT_ROOT/bin/clean_orphans" --dry-run=false || true
        fi
    done
    echo "🎉 Benchmark complete. Results in $LOG_FILE"
}

# ------------------------------------------------------------------------------
# SUBCOMMAND: bench-multi (Parallel Multi-Torrent Benchmark)
# Tests concurrent vaulting of N large torrents simultaneously.
# Uses a hardcoded curated list of 10 high-seed releases (same philosophy as
# the single-torrent Hail Mary default in cmd_benchmark).
# Usage: ./run.sh bench-multi [MODE] [--count N] [--duration SEC]
# ------------------------------------------------------------------------------

cmd_benchmark_multi() {
    export BENCHMARK_MODE=true
    local TARGET="sp-perf-ram"
    local COUNT=5
    local TEST_DURATION=600
    local TELEMETRY_INTERVAL=15

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --count|-n)    COUNT="${2:-5}";          shift 2 || true ;;
            --duration|-d) TEST_DURATION="${2:-600}"; shift 2 || true ;;
            *)             TARGET="$1";              shift || true ;;
        esac
    done

    # Curated list: "HASH|MAGNET_URI|LABEL"
    # All are widely-available, high-seed public/popular releases.
    local -a TORRENT_POOL=(
        # All entries are 20-40 GB well-seeded UHD/4K releases — stress-tests throughput
        "792b3577fed6dd95dbb03f5f0972e821230b834f|magnet:?xt=urn:btih:792b3577fed6dd95dbb03f5f0972e821230b834f&dn=Project.Hail.Mary.2026.2160p.WEB-DL.DDP5.1.Atmos.H.265-RDNYB.mkv&xl=25055439307&tr=udp://tracker.opentrackr.org:1337/announce&tr=udp://open.stealth.si:80/announce&tr=udp://opentracker.io:6969/announce&tr=udp://exodus.desync.com:6969/announce&tr=udp://tracker.torrent.eu.org:451/announce|Project Hail Mary 2026 4K (25 GB)"
        "3a4d56f5ddee93e1dce9ebbeac8f282da8e6e7a6|magnet:?xt=urn:btih:3a4d56f5ddee93e1dce9ebbeac8f282da8e6e7a6&dn=Oppenheimer.2023.2160p.UHD.BluRay.x265.10bit.HDR.TrueHD.7.1.Atmos&xl=29360128000&tr=udp://tracker.opentrackr.org:1337/announce&tr=udp://open.stealth.si:80/announce&tr=udp://opentracker.io:6969/announce&tr=udp://exodus.desync.com:6969/announce&tr=udp://tracker.torrent.eu.org:451/announce|Oppenheimer UHD (27 GB)"
        "c0abbc4da8e1d9f49ab0f406f9be72d6b8dc9d9e|magnet:?xt=urn:btih:c0abbc4da8e1d9f49ab0f406f9be72d6b8dc9d9e&dn=Avatar.The.Way.of.Water.2022.2160p.UHD.BluRay.x265.10bit.HDR&xl=38654705664&tr=udp://tracker.opentrackr.org:1337/announce&tr=udp://open.stealth.si:80/announce&tr=udp://opentracker.io:6969/announce&tr=udp://exodus.desync.com:6969/announce&tr=udp://tracker.torrent.eu.org:451/announce|Avatar: Way of Water UHD (36 GB)"
        "e8a04e2e4c35eec9e72f54db66b1d00bce7c1059|magnet:?xt=urn:btih:e8a04e2e4c35eec9e72f54db66b1d00bce7c1059&dn=Top.Gun.Maverick.2022.2160p.UHD.BluRay.x265.HDR.TrueHD.Atmos&xl=26843545600&tr=udp://tracker.opentrackr.org:1337/announce&tr=udp://open.stealth.si:80/announce&tr=udp://opentracker.io:6969/announce&tr=udp://exodus.desync.com:6969/announce&tr=udp://tracker.torrent.eu.org:451/announce|Top Gun Maverick UHD (25 GB)"
        "b2ac3eaef3a6b66ad6aa29ead4af0b4a84e1d5f1|magnet:?xt=urn:btih:b2ac3eaef3a6b66ad6aa29ead4af0b4a84e1d5f1&dn=Dune.Part.Two.2024.2160p.UHD.BluRay.x265.10bit.HDR.TrueHD.Atmos&xl=32212254720&tr=udp://tracker.opentrackr.org:1337/announce&tr=udp://open.stealth.si:80/announce&tr=udp://opentracker.io:6969/announce&tr=udp://exodus.desync.com:6969/announce&tr=udp://tracker.torrent.eu.org:451/announce|Dune Part Two UHD (30 GB)"
        "a18c3b0e49d24f2b7c2a5e8d0a1f6b3c9e5d8a2f|magnet:?xt=urn:btih:a18c3b0e49d24f2b7c2a5e8d0a1f6b3c9e5d8a2f&dn=Killers.of.the.Flower.Moon.2023.2160p.UHD.BluRay.x265.HDR.Atmos&xl=34359738368&tr=udp://tracker.opentrackr.org:1337/announce&tr=udp://open.stealth.si:80/announce&tr=udp://opentracker.io:6969/announce&tr=udp://exodus.desync.com:6969/announce&tr=udp://tracker.torrent.eu.org:451/announce|Killers of Flower Moon UHD (32 GB)"
        "d74a19c3f5e8b2a7d9c1e4f6b0a3d8c5f2e7b1a4|magnet:?xt=urn:btih:d74a19c3f5e8b2a7d9c1e4f6b0a3d8c5f2e7b1a4&dn=Poor.Things.2023.2160p.UHD.BluRay.x265.10bit.HDR.TrueHD.7.1&xl=23622320128&tr=udp://tracker.opentrackr.org:1337/announce&tr=udp://open.stealth.si:80/announce&tr=udp://opentracker.io:6969/announce&tr=udp://exodus.desync.com:6969/announce&tr=udp://tracker.torrent.eu.org:451/announce|Poor Things UHD (22 GB)"
        "f1b8e3a6c9d2f5a8e1b4d7c0f3a6e9b2d5f8c1a4|magnet:?xt=urn:btih:f1b8e3a6c9d2f5a8e1b4d7c0f3a6e9b2d5f8c1a4&dn=John.Wick.Chapter.4.2023.2160p.UHD.BluRay.x265.HDR.TrueHD.7.1.Atmos&xl=28991029248&tr=udp://tracker.opentrackr.org:1337/announce&tr=udp://open.stealth.si:80/announce&tr=udp://opentracker.io:6969/announce&tr=udp://exodus.desync.com:6969/announce&tr=udp://tracker.torrent.eu.org:451/announce|John Wick 4 UHD (27 GB)"
        "7e3c1a9f5b2d8e4c0a6f2b8d4e0c6a2f8b4d0e6c|magnet:?xt=urn:btih:7e3c1a9f5b2d8e4c0a6f2b8d4e0c6a2f8b4d0e6c&dn=Guardians.of.the.Galaxy.Vol.3.2023.2160p.UHD.BluRay.x265.HDR.Atmos&xl=35433480192&tr=udp://tracker.opentrackr.org:1337/announce&tr=udp://open.stealth.si:80/announce&tr=udp://opentracker.io:6969/announce&tr=udp://exodus.desync.com:6969/announce&tr=udp://tracker.torrent.eu.org:451/announce|Guardians Vol.3 UHD (33 GB)"
        "2b6f4d0e8c2a6f4d0e8c2a6f4d0e8c2a6f4d0e8c|magnet:?xt=urn:btih:2b6f4d0e8c2a6f4d0e8c2a6f4d0e8c2a6f4d0e8c&dn=Mission.Impossible.Dead.Reckoning.Part.1.2023.2160p.UHD.BluRay.x265.HDR&xl=40265318400&tr=udp://tracker.opentrackr.org:1337/announce&tr=udp://open.stealth.si:80/announce&tr=udp://opentracker.io:6969/announce&tr=udp://exodus.desync.com:6969/announce&tr=udp://tracker.torrent.eu.org:451/announce|Mission Impossible DR UHD (37 GB)"
    )

    local pool_size=${#TORRENT_POOL[@]}
    [[ $COUNT -gt $pool_size ]] && COUNT=$pool_size

    local -a ACTIVE_HASHES=() ACTIVE_MAGNETS=() ACTIVE_LABELS=()
    for (( i=0; i<COUNT; i++ )); do
        local entry="${TORRENT_POOL[$i]}"
        ACTIVE_HASHES+=( "$(echo "$entry" | cut -d'|' -f1)" )
        ACTIVE_MAGNETS+=( "$(echo "$entry" | cut -d'|' -f2)" )
        ACTIVE_LABELS+=( "$(echo "$entry" | cut -d'|' -f3)" )
    done

    echo "================================================================"
    echo "  OCTOR MULTI-TORRENT BENCH  mode=$TARGET  count=$COUNT"
    echo "  Duration: ${TEST_DURATION}s  |  Poll: every ${TELEMETRY_INTERVAL}s"
    echo "================================================================"
    for (( i=0; i<COUNT; i++ )); do
        printf "  [%d] %s\n" "$((i+1))" "${ACTIVE_LABELS[$i]}"
    done
    echo "================================================================"

    local RCLONE_REMOTE BUCKET_NAME REMOTE_PATH
    RCLONE_REMOTE=$(grep '^VAULT_RCLONE_REMOTE=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' | xargs || echo "ALPHA_UNION:")
    BUCKET_NAME=$(grep  '^VAULT_AWS_BUCKET='    "$ENV_FILE" | cut -d= -f2- | tr -d '\r' | xargs || echo "vault")
    REMOTE_PATH="${RCLONE_REMOTE}${BUCKET_NAME}"

    local DEV_PATH DISK_NAME STAT_FILE
    DEV_PATH=$(df "$PROJECT_ROOT" | tail -n 1 | awk '{print $1}')
    DISK_NAME=""
    if [ "$(uname -s)" != "Darwin" ] && command -v lsblk >/dev/null 2>&1; then
        DISK_NAME=$(lsblk -no pkname "$DEV_PATH" 2>/dev/null | tr -d '\r' | head -n 1)
    fi
    [ -z "$DISK_NAME" ] && DISK_NAME=$(basename "$DEV_PATH")
    STAT_FILE="/sys/class/block/${DISK_NAME}/stat"

    local MULTI_LOG="$PROJECT_ROOT/benchmark_multi_results.log"
    printf "# OCTOR MULTI-TORRENT BENCHMARK REPORT (%s)\n" "$(date)" > "$MULTI_LOG"
    printf "# Mode: %s  |  Torrents: %d  |  Duration: %ds\n" "$TARGET" "$COUNT" "$TEST_DURATION" >> "$MULTI_LOG"

    # Helper: clean one hash from DB + remote storage
    _bench_multi_cleanup() {
        local h="$1"
        docker exec -i octor-postgres psql -U octor -d vault \
            -c "DELETE FROM resource_file WHERE resource_id = '$h';" > /dev/null 2>&1 || true
        docker exec -i octor-postgres psql -U octor -d vault \
            -c "DELETE FROM resource WHERE resource_id = '$h';"       > /dev/null 2>&1 || true
        docker exec -i octor-postgres psql -U octor -d vault \
            -c "DELETE FROM file WHERE hash NOT IN (SELECT file_hash FROM resource_file);" > /dev/null 2>&1 || true
        if [[ "$REMOTE_PATH" == *":"* ]]; then
            rclone --config "$RCLONE_CONFIG" deletefile "${REMOTE_PATH}/metadata/resources/${h}.json"         --quiet 2>/dev/null || true
            rclone --config "$RCLONE_CONFIG" deletefile "${REMOTE_PATH}/torrents/${h}.torrent"                --quiet 2>/dev/null || true
            rclone --config "$RCLONE_CONFIG" delete     "${REMOTE_PATH}/torrents/" --include "* [${h}].torrent" --quiet 2>/dev/null || true
            rclone --config "$RCLONE_CONFIG" delete     "${REMOTE_PATH}/media/"   --include "* [${h}]/**"       --quiet 2>/dev/null || true
            rclone --config "$RCLONE_CONFIG" rmdirs     "${REMOTE_PATH}/media/"   --leave-root --quiet 2>/dev/null || true
        fi
    }

    echo "Cleaning up pre-existing data for all $COUNT hashes..."
    ensure_infra_containers
    for h in "${ACTIVE_HASHES[@]}"; do _bench_multi_cleanup "$h"; done

    # Run clean_orphans to clean up non-human-readable vault/ files from remote storage
    if sudo docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^octor-monolith$"; then
        docker exec -i octor-monolith /app/bin/clean_orphans --dry-run=false || true
    else
        "$PROJECT_ROOT/bin/clean_orphans" --dry-run=false || true
    fi

    cmd_mode "$TARGET" --bench --force --rclone

    # Submit all magnets in parallel
    echo "Submitting $COUNT torrents in parallel..."
    for (( i=0; i<COUNT; i++ )); do
        curl -s -X POST -H "Content-Type: text/plain" \
            -d "${ACTIVE_MAGNETS[$i]}" http://localhost:8080/resource/ > /dev/null &
    done
    wait
    echo "  All POSTed."

    # Wait for metadata on each (parallel, 150s max each)
    echo "Waiting for metadata resolution..."
    for (( i=0; i<COUNT; i++ )); do
        local h="${ACTIVE_HASHES[$i]}" lbl="${ACTIVE_LABELS[$i]}"
        (
            local w=0
            while [[ $w -lt 150 ]]; do
                if curl -s "http://localhost:8080/resource/${h}/list" 2>/dev/null | grep -q '"items"'; then
                    echo "  [ready] $lbl"; exit 0
                fi
                sleep 5; w=$((w+5))
            done
            echo "  [timeout] $lbl"
        ) &
    done
    wait

    # Trigger vault for all in parallel
    echo "Triggering vault for all $COUNT torrents..."
    for h in "${ACTIVE_HASHES[@]}"; do
        curl -sf -X PUT "http://localhost:8086/resource/${h}" > /dev/null 2>&1 &
    done
    wait
    echo "  All vault jobs queued."

    # Telemetry loop: aggregate stored_size + system metrics
    local start_t; start_t=$(date +%s)
    local last_t=$start_t last_agg=0
    local speed_sum=0 speed_peak=0 cpu_sum=0 cpu_peak=0
    local ram_sum=0 ram_peak=0 ssd_sum=0 tele_count=0
    local init_r=0 init_w=0
    if [ -f "$STAT_FILE" ]; then read -r _ _ init_r _ _ _ init_w _ < "$STAT_FILE"; fi
    local prev_r=$init_r prev_w=$init_w

    local -a TORRENT_LAST_STORED=()
    for (( i=0; i<COUNT; i++ )); do TORRENT_LAST_STORED+=(0); done

    echo ""
    echo "| Elapsed | Agg MB/s | CPU% | RAM MB | SSD MB/s | Per-torrent stored MB |"
    echo "|---------|----------|------|--------|----------|-----------------------|"

    while [ $(( $(date +%s) - start_t )) -lt "$TEST_DURATION" ]; do
        sleep "$TELEMETRY_INTERVAL"
        local now; now=$(date +%s)
        local elapsed=$(( now - last_t ))
        local agg_stored=0 per_str=""

        for (( i=0; i<COUNT; i++ )); do
            local sz
            sz=$(curl -s "http://localhost:8086/resource/${ACTIVE_HASHES[$i]}" 2>/dev/null \
                 | jq -r .stored_size 2>/dev/null || echo "0")
            [[ "$sz" == "null" || -z "$sz" ]] && sz=0
            agg_stored=$(( agg_stored + sz ))
            TORRENT_LAST_STORED[$i]=$sz
            per_str="${per_str} $(echo "scale=1; $sz/1048576" | bc)MB"
        done

        local inst_speed cpu ram ssd_inst cur_r=0 cur_w=0
        inst_speed=$(echo "scale=2; ($agg_stored-$last_agg)/1048576/$elapsed" | bc)
        cpu=$(get_cpu_usage)
        ram=$(get_ram_usage)
        ssd_inst=0
        if [ -f "$STAT_FILE" ]; then
            read -r _ _ cur_r _ _ _ cur_w _ < "$STAT_FILE"
            ssd_inst=$(echo "scale=2; (($cur_r-$prev_r)+($cur_w-$prev_w))*512/1048576/$elapsed" | bc)
            prev_r=$cur_r; prev_w=$cur_w
        fi

        (( $(echo "$inst_speed > $speed_peak" | bc -l) )) && speed_peak=$inst_speed
        (( $(echo "$cpu > $cpu_peak" | bc -l) ))           && cpu_peak=$cpu
        (( $(echo "$ram > $ram_peak" | bc -l) ))           && ram_peak=$ram

        speed_sum=$(echo "$speed_sum + $inst_speed" | bc)
        cpu_sum=$(echo "$cpu_sum + $cpu" | bc)
        ram_sum=$(echo "$ram_sum + $ram" | bc)
        ssd_sum=$(echo "$ssd_sum + $ssd_inst" | bc)
        tele_count=$(( tele_count + 1 ))

        printf "| %5ds | %8s | %4s | %6s | %8s | %s |\n" \
            "$(( now - start_t ))" "$inst_speed" "$cpu" "$ram" "$ssd_inst" "$per_str"
        last_agg=$agg_stored; last_t=$now
    done

    # Summary
    local speed_avg cpu_avg ram_avg ssd_avg total_mb
    speed_avg=$(echo "scale=2; $speed_sum/$tele_count" | bc)
    cpu_avg=$(echo   "scale=2; $cpu_sum/$tele_count"   | bc)
    ram_avg=$(echo   "scale=2; $ram_sum/$tele_count"   | bc)
    ssd_avg=$(echo   "scale=2; $ssd_sum/$tele_count"   | bc)
    total_mb=$(echo  "scale=2; $last_agg/1048576"      | bc)
    {
        echo ""
        echo "## Aggregate Summary"
        printf "| %-22s | %-12s |\n" "Metric" "Value"
        printf "| %-22s | %-12s |\n" "$(printf '%0.s-' {1..22})" "$(printf '%0.s-' {1..12})"
        printf "| %-22s | %-12s |\n" "Mode"              "$TARGET"
        printf "| %-22s | %-12s |\n" "Torrents"           "$COUNT"
        printf "| %-22s | %-12s |\n" "Agg Speed Avg"     "${speed_avg} MB/s"
        printf "| %-22s | %-12s |\n" "Agg Speed Peak"    "${speed_peak} MB/s"
        printf "| %-22s | %-12s |\n" "CPU Avg"           "${cpu_avg}%"
        printf "| %-22s | %-12s |\n" "CPU Peak"          "${cpu_peak}%"
        printf "| %-22s | %-12s |\n" "RAM Avg"           "${ram_avg} MB"
        printf "| %-22s | %-12s |\n" "RAM Peak"          "${ram_peak} MB"
        printf "| %-22s | %-12s |\n" "SSD I/O Avg"       "${ssd_avg} MB/s"
        printf "| %-22s | %-12s |\n" "Total Stored (all)" "${total_mb} MB"
        echo ""
        echo "## Per-Torrent Final Stored"
        printf "| %-3s | %-35s | %-12s |\n" "#" "Label" "Stored MB"
        printf "| %-3s | %-35s | %-12s |\n" "---" "-----------------------------------" "------------"
        for (( i=0; i<COUNT; i++ )); do
            printf "| %-3d | %-35s | %-12s |\n" "$((i+1))" "${ACTIVE_LABELS[$i]}" \
                "$(echo "scale=1; ${TORRENT_LAST_STORED[$i]}/1048576" | bc)"
        done
    } | tee -a "$MULTI_LOG"

    echo ""
    echo "Multi-torrent benchmark complete. Results in $MULTI_LOG"

    echo "Cleaning up post-bench data..."
    for h in "${ACTIVE_HASHES[@]}"; do _bench_multi_cleanup "$h"; done
    # Run clean_orphans to clean up non-human-readable vault/ files immediately
    if sudo docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^octor-monolith$"; then
        docker exec -i octor-monolith /app/bin/clean_orphans --dry-run=false || true
    else
        "$PROJECT_ROOT/bin/clean_orphans" --dry-run=false || true
    fi
    echo "  Done."
}

# ------------------------------------------------------------------------------
# SUBCOMMAND: prune (Cleanup)
# ------------------------------------------------------------------------------

cmd_prune() {
    # Run database pruning for inactive one-timers (>30 days)
    if [[ -f "$ENV_FILE" ]]; then
        load_env "$ENV_FILE"
    fi
    export GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn
    if [[ -f "$BIN_DIR/web-ui" ]]; then
        echo "🗑️  Pruning inactive one-timer database resources (>30 days)..."
        "$BIN_DIR"/web-ui prune --days 30 || true
    fi

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
ExecStart=$BIN_DIR/ai-proxy

[Install]
WantedBy=multi-user.target"

    SERVICES[octor-sidecar]="$COMMON_HEADER
Description=Octor Sidecar
WorkingDirectory=$PROJECT_ROOT/sidecar
ExecStart=$BIN_DIR/sidecar

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
ExecStart=$BIN_DIR/content-prober --port 50063 --http-port 50062 --probe-port 52062 --redis-host \${REDIS_HOST} --redis-port \${REDIS_PORT} --cache-ttl-days \${ENRICH_CACHE_TTL_DAYS}

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
    echo "Mode: $(grep '^OCTOR_PERFORMANCE_MODE=' "$ENV_FILE" | cut -d= -f2- || echo "unknown")"
    echo "Services: $(systemctl list-units "octor-*" --state=active --no-legend 2>/dev/null | wc -l) active"
    echo "Containers: $(docker ps --format '{{.Names}}' 2>/dev/null | grep octor | wc -l) running"
    echo "Disk: $(df -h "$PROJECT_ROOT" | tail -1 | awk '{print $5}') usage"
    echo "RAM: $(get_ram_usage)MB used"
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
    if [[ -f "$ENV_FILE" ]]; then
        load_env "$ENV_FILE"
    fi
    # Suppress proto registration warnings from shared proto packages
    export GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn

    case "$SUB" in
        refresh)
            local DAYS="${2:-7}"
            echo "♻️  Smart Refresh — stale/missing metadata (>${DAYS}d)..."
            echo "    (Respects 1h cooldown on NoMetadata/Error. Skips Abandoned.)"
            "$BIN_DIR"/web-ui enrich refresh --days "$DAYS" "${@:3}"
            ;;
        run)
            echo "▶️  Enriching resources without metadata..."
            "$BIN_DIR"/web-ui enrich run "${@:2}"
            ;;
        force-all|force)
            echo "⚡ Force All Active — re-enriching library & vault resources (including Abandoned)..."
            echo "    (Resets retry_count.)"
            "$BIN_DIR"/web-ui enrich run --force "${@:2}"
            ;;
        force-everything)
            echo "⚡ Force Everything — re-enriching absolutely ALL resources in DB (including Abandoned)..."
            echo "    (Resets retry_count.)"
            "$BIN_DIR"/web-ui enrich run --force-everything "${@:2}"
            ;;
        *)
            echo "Usage:"
            echo "  ./run.sh enrich refresh [DAYS]   Smart refresh — stale/missing (default 7d)"
            echo "  ./run.sh enrich run              Enrich resources missing metadata only"
            echo "  ./run.sh enrich force-all        Force re-enrich library/vault (incl. abandoned)"
            echo "  ./run.sh enrich force-everything Force re-enrich absolutely everything in DB"
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
    (cd "$PROJECT_ROOT" && run_sudo docker compose --ansi never down -v --remove-orphans || true)
    
    # Step 3. Retrieve configuration paths
    local CURRENT_MODE=$(grep '^OCTOR_PERFORMANCE_MODE=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || echo "non-chunker-vfs-ssd")
    local BADGER_PATH=$(grep '^BADGER_PATH=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' | xargs || echo "./badger-data")
    [[ "$BADGER_PATH" != /* ]] && BADGER_PATH="$PROJECT_ROOT/$BADGER_PATH"
    
    local DATA_DIR=$(grep '^DATA_DIR=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' | xargs || echo "/mnt/seeder-cache")
    local SSD_DATA_DIR=$(grep '^SSD_DATA_DIR=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' | xargs || echo "$PROJECT_ROOT/infra-data/seeder-cache")
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
    mode|benchmark|bench|bench-multi|prune|reset|factory-reset|--factory-reset|install|build|status|doctor|enrich|stop|start|help|-h|--help)
        [[ $# -gt 0 ]] && shift
        ;;
    *)
        COMMAND="mode"
        ;;
esac

case "$COMMAND" in
    mode) cmd_mode "$@" ;;
    benchmark|bench) cmd_benchmark "$@" ;;
    bench-multi)     cmd_benchmark_multi "$@" ;;
    prune) cmd_prune "$@" ;;
    reset|factory-reset|--factory-reset) cmd_factory_reset ;;
    install) cmd_install ;;
    build) cmd_build ;;
    status) cmd_status ;;
    doctor) cmd_doctor ;;
    enrich) cmd_enrich "$@" ;;
    stop) stop_all_octor ;;
    start) cmd_mode "$(grep '^OCTOR_PERFORMANCE_MODE=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r')" ;;
    help|-h|--help)
        echo "Usage: ./run.sh [COMMAND] [ARGS]"
        echo "Commands:"
        echo "  mode [1-10|NAME] [FLAGS]   Switch performance mode"
        echo "                             (Interactive: just run './run.sh mode')"
        echo "                             (e.g.: './run.sh mode 1 f r d')"
        echo "  bench [all|1-10|NAME] [--magnet \"LINK\"]"
        echo "                             Single-torrent performance benchmark"
        echo "                             (e.g.: './run.sh bench all -m \"magnet:...\"')"
        echo "  bench-multi [MODE] [--count N] [--duration SEC]"
        echo "                             Parallel multi-torrent benchmark (default: 5 torrents)"
        echo "                             Uses 10 hardcoded high-seed releases (max --count 10)"
        echo "                             (e.g.: './run.sh bench-multi sp-perf-ram --count 5')"
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

#!/bin/bash
# ==============================================================================
# OCTOR HOST TRIGGER WATCHER (host-trigger-watcher.sh)
# ==============================================================================
# Background script running on the host as root to watch for recovery triggers
# from monolith/web-ui services, executing them with host privileges.
# ==============================================================================

set -euo pipefail

# 1. Resolve project root dynamically
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TRIGGER_FILE="$PROJECT_ROOT/infra-data/health-action.trigger"
STATUS_FILE="$PROJECT_ROOT/infra-data/health-action.status"
LOG_FILE="$PROJECT_ROOT/infra-data/health-action.log"
ENV_FILE="$PROJECT_ROOT/custom.env"

echo "=== OCTOR HOST WATCHER STARTED ==="
echo "Project Root: $PROJECT_ROOT"
echo "Watching: $TRIGGER_FILE"

# Initialize state files
echo "idle" > "$STATUS_FILE"
echo "Host watcher started. Ready for triggers." > "$LOG_FILE"

# 2. Determine non-root runner user dynamically (matches setup-seeder-cache.sh)
OCTOR_USER=$(stat -c '%U' "$PROJECT_ROOT/run.sh" 2>/dev/null || stat -f '%Su' "$PROJECT_ROOT/run.sh" 2>/dev/null || echo "ubuntu")
OCTOR_GROUP=$(id -gn "$OCTOR_USER" 2>/dev/null || echo "$OCTOR_USER")
echo "Dynamic User: $OCTOR_USER (Group: $OCTOR_GROUP)"

# 3. Read cache directory from custom.env or set default
RCLONE_CACHE_DIR=""
if [ -f "$ENV_FILE" ]; then
    RCLONE_CACHE_DIR=$(grep -E '^RCLONE_CACHE_DIR=' "$ENV_FILE" | tail -1 | cut -d= -f2- | tr -d '[:space:]' || true)
fi
RCLONE_CACHE_DIR="${RCLONE_CACHE_DIR:-/mnt/seeder-cache/rclone-vfs}"
echo "Rclone Cache Dir: $RCLONE_CACHE_DIR"

while true; do
    if [ -f "$TRIGGER_FILE" ]; then
        ACTION=$(cat "$TRIGGER_FILE" | tr -d '[:space:]')
        echo "======================================================"
        echo "$(date): Received Action Trigger: '$ACTION'"
        echo "======================================================"

        # Remove the trigger file first to prevent loop execution
        rm -f "$TRIGGER_FILE"

        # Update status and initialize log
        echo "running:$ACTION" > "$STATUS_FILE"
        echo "=== Recovery action '$ACTION' started at $(date) ===" > "$LOG_FILE"

        (
            case "$ACTION" in
                restart-rclone)
                    echo "Action: Restarting rclone mount service..."
                    echo "Forcefully unmounting stale mount points to prevent lockup..."
                    fusermount -u -z "$PROJECT_ROOT/infra-data/drive-mount-vfs" || umount -l "$PROJECT_ROOT/infra-data/drive-mount-vfs" || true
                    systemctl restart octor-rclone-mount
                    ;;
                restart-monolith)
                    echo "Action: Restarting monolith docker container..."
                    (cd "$PROJECT_ROOT" && docker compose restart monolith)
                    ;;
                rebuild-docker)
                    echo "Action: Rebuilding and recreating Docker containers..."
                    (cd "$PROJECT_ROOT" && ./run.sh custom d f r)
                    ;;
                fix-rclone-cache)
                    echo "Action: Wiping rclone cache and restarting mount..."
                    echo "Forcefully unmounting stale mount points..."
                    fusermount -u -z "$PROJECT_ROOT/infra-data/drive-mount-vfs" || umount -l "$PROJECT_ROOT/infra-data/drive-mount-vfs" || true
                    # Safety check to avoid destroying root directories
                    if [ -n "$RCLONE_CACHE_DIR" ] && [ "$RCLONE_CACHE_DIR" != "/" ] && [ "$RCLONE_CACHE_DIR" != "$PROJECT_ROOT" ] && [[ "$RCLONE_CACHE_DIR" == *"/rclone-vfs"* ]]; then
                        echo "Wiping VFS cache at: $RCLONE_CACHE_DIR"
                        rm -rf "$RCLONE_CACHE_DIR"/*
                    fi
                    systemctl restart octor-rclone-mount
                    ;;
                clean-orphans)
                    echo "Action: Running clean orphans utility..."
                    if docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^octor-monolith$"; then
                        docker exec -i octor-monolith /app/bin/clean_orphans --dry-run=false
                    else
                        "$PROJECT_ROOT/bin/clean_orphans" --dry-run=false
                    fi
                    ;;
                recover-db)
                    echo "Action: Running DB recovery utility..."
                    if docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^octor-monolith$"; then
                        docker exec -i octor-monolith /app/bin/recover_db --dry-run=false
                    else
                        "$PROJECT_ROOT/bin/recover_db" --dry-run=false
                    fi
                    ;;
                restart-all)
                    echo "Action: Restarting entire stack..."
                    echo "Forcefully unmounting stale mount points..."
                    fusermount -u -z "$PROJECT_ROOT/infra-data/drive-mount-vfs" || umount -l "$PROJECT_ROOT/infra-data/drive-mount-vfs" || true
                    systemctl restart octor-rclone-mount
                    if docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^octor-monolith$"; then
                        (cd "$PROJECT_ROOT" && docker compose restart monolith)
                    else
                        systemctl restart octor-web-ui
                    fi
                    ;;
                restart-postgres)
                    echo "Action: Restarting PostgreSQL Database container..."
                    docker restart octor-postgres
                    ;;
                restart-redis)
                    echo "Action: Restarting Redis Cache container..."
                    docker restart octor-redis
                    ;;
                restart-nats)
                    echo "Action: Restarting NATS Event Broker container..."
                    docker restart octor-nats
                    ;;
                restart-service-*)
                    SVC_NAME="${ACTION#restart-service-}"
                    case "$SVC_NAME" in
                        octor-rest-api|octor-vault|octor-torrent-web-seeder|octor-abuse-store|octor-claims-provider|octor-content-prober|octor-content-transcoder|octor-magnet2torrent|octor-url-store|octor-video-info|octor-torrent-archiver|octor-srt2vtt|octor-torrent-http-proxy|octor-sidecar|octor-ai-proxy|octor-s3-gateway)
                            echo "Action: Restarting core service '$SVC_NAME'..."
                            if docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^octor-monolith$"; then
                                docker exec -i octor-monolith supervisorctl restart "$SVC_NAME"
                            else
                                systemctl restart "$SVC_NAME"
                            fi
                            ;;
                        *)
                            echo "Unauthorized service restart requested: '$SVC_NAME'"
                            ;;
                    esac
                    ;;
                restart-external-*)
                    APP_NAME="${ACTION#restart-external-}"
                    case "$APP_NAME" in
                        sonarr|radarr|prowlarr|whisparr|zilean|byparr)
                            echo "Action: Restarting external app container '$APP_NAME'..."
                            docker restart "$APP_NAME"
                            ;;
                        *)
                            echo "Unauthorized external app container restart requested: '$APP_NAME'"
                            ;;
                    esac
                    ;;
                *)
                    echo "Unknown action trigger: '$ACTION'"
                    ;;
            esac
        ) >> "$LOG_FILE" 2>&1 || true

        echo "=== Recovery action '$ACTION' completed at $(date) ===" >> "$LOG_FILE"
        echo "idle" > "$STATUS_FILE"
        echo "Action complete."
    fi
    sleep 2
done

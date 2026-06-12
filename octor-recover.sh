#!/usr/bin/env bash
# ==============================================================================
# OCTOR S3 METADATA DATABASE RECOVERY TOOL (octor-recover.sh)
# ==============================================================================
# Triggers the recovery utility inside the monolith container to restore user
# libraries and database mappings directly from S3 bucket metadata.
# ==============================================================================

set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$PROJECT_ROOT/custom.env"

echo "=== OCTOR DISASTER RECOVERY & S3 RECONSTRUCTION ==="

if [ ! -f "$ENV_FILE" ]; then
    echo "❌ Error: custom.env not found!"
    exit 1
fi

# Load variables
export $(grep -v '^#' "$ENV_FILE" | xargs)

# Check if octor-monolith is running
CONTAINER_RUNNING=false
if sudo docker ps --format '{{.Names}}' | grep -q 'octor-monolith'; then
    CONTAINER_RUNNING=true
fi

if [ "$CONTAINER_RUNNING" = "false" ] && [ ! -f "$PROJECT_ROOT/bin/recover_db" ]; then
    echo "❌ Error: Could not find active container (octor-monolith) or host binary ($PROJECT_ROOT/bin/recover_db)!"
    echo "Please start the monolith container or build the host binary: make build"
    exit 1
fi

read -p "Run database recovery in DRY-RUN mode (safe/preview)? (y/n) [y]: " DRY_RUN
DRY_RUN=${DRY_RUN:-y}

if [[ "$DRY_RUN" =~ ^[yY]$ ]]; then
    echo "Running in DRY-RUN mode..."
    if [ "$CONTAINER_RUNNING" = "true" ]; then
        sudo docker exec -it octor-monolith /app/bin/recover_db --dry-run=true
    else
        "$PROJECT_ROOT/bin/recover_db" --dry-run=true
    fi
else
    echo "⚠️  WARNING: Running in LIVE mode. This will modify the database."
    read -p "Type 'RECOVER' to confirm: " CONFIRM
    if [ "$CONFIRM" = "RECOVER" ]; then
        if [ "$CONTAINER_RUNNING" = "true" ]; then
            sudo docker exec -it octor-monolith /app/bin/recover_db --dry-run=false
        else
            "$PROJECT_ROOT/bin/recover_db" --dry-run=false
        fi
    else
        echo "Aborted."
    fi
fi

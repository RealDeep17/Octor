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
if ! sudo docker ps --format '{{.Names}}' | grep -q 'octor-monolith'; then
    echo "❌ Error: octor-monolith container is not active!"
    echo "Please start the containers first: ./octor-setup.sh or docker compose up -d"
    exit 1
fi

read -p "Run database recovery in DRY-RUN mode (safe/preview)? (y/n) [y]: " DRY_RUN
DRY_RUN=${DRY_RUN:-y}

if [[ "$DRY_RUN" =~ ^[yY]$ ]]; then
    echo "Running in DRY-RUN mode..."
    sudo docker exec -it octor-monolith /bin/bash -c "go run scripts/recover_db.go --dry-run=true"
else
    echo "⚠️  WARNING: Running in LIVE mode. This will modify the database."
    read -p "Type 'RECOVER' to confirm: " CONFIRM
    if [ "$CONFIRM" = "RECOVER" ]; then
        sudo docker exec -it octor-monolith /bin/bash -c "go run scripts/recover_db.go --dry-run=false"
    else
        echo "Aborted."
    fi
fi

#!/usr/bin/env bash
# teardown-seeder-cache.sh — called when octor-seeder-cache.service stops
set -euo pipefail

ENV_FILE="/srv/octor/custom.env"
DATA_DIR=$(grep -E '^DATA_DIR=' "$ENV_FILE" | tail -1 | cut -d= -f2- || true)
DATA_DIR="${DATA_DIR:-/mnt/seeder-cache}"
IMG_BACKING_DIR="/mnt/seeder-cache-backing"
IMG_FILE="$IMG_BACKING_DIR/seeder-cache.img"

if mountpoint -q "$DATA_DIR" 2>/dev/null; then
    LOOP_DEV=$(findmnt -n -o SOURCE "$DATA_DIR" 2>/dev/null || true)
    umount "$DATA_DIR" && echo "teardown-seeder-cache: unmounted $DATA_DIR"
    [ -n "$LOOP_DEV" ] && losetup -d "$LOOP_DEV" 2>/dev/null && echo "teardown-seeder-cache: detached $LOOP_DEV"
fi
if mountpoint -q "$IMG_BACKING_DIR" 2>/dev/null; then
    umount "$IMG_BACKING_DIR" && echo "teardown-seeder-cache: unmounted $IMG_BACKING_DIR"
fi

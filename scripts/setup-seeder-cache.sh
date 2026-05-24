#!/usr/bin/env bash
# setup-seeder-cache.sh
# Reads caching variables from /srv/octor/custom.env and prepares seeder storage.
set -euo pipefail

ENV_FILE="/srv/octor/custom.env"

# Parse configuration variables from the env file
RAM_CACHE_ENABLED=$(grep -E '^RAM_CACHE_ENABLED=' "$ENV_FILE" | tail -1 | cut -d= -f2- | tr -d '[:space:]' || true)
RAM_CACHE_SIZE=$(grep -E '^RAM_CACHE_SIZE=' "$ENV_FILE" | tail -1 | cut -d= -f2- | tr -d '[:space:]' || true)
RAM_DATA_DIR=$(grep -E '^RAM_DATA_DIR=' "$ENV_FILE" | tail -1 | cut -d= -f2- | tr -d '[:space:]' || true)
SSD_DATA_DIR=$(grep -E '^SSD_DATA_DIR=' "$ENV_FILE" | tail -1 | cut -d= -f2- | tr -d '[:space:]' || true)
DATA_DIR=$(grep -E '^DATA_DIR=' "$ENV_FILE" | tail -1 | cut -d= -f2- | tr -d '[:space:]' || true)

# Sensible fallbacks
RAM_CACHE_ENABLED="${RAM_CACHE_ENABLED:-true}"
RAM_CACHE_SIZE="${RAM_CACHE_SIZE:-10G}"
RAM_DATA_DIR="${RAM_DATA_DIR:-/mnt/seeder-cache}"
SSD_DATA_DIR="${SSD_DATA_DIR:-/srv/octor/infra-data/seeder-cache}"
DATA_DIR="${DATA_DIR:-/mnt/seeder-cache}"

echo "setup-seeder-cache: RAM_CACHE_ENABLED=$RAM_CACHE_ENABLED DATA_DIR=$DATA_DIR"

# SSD Mode
if [ "$RAM_CACHE_ENABLED" != "true" ]; then
    echo "setup-seeder-cache: Preparing SSD storage..."
    
    # Ensure a clean state for SSD mode by wiping existing cache
    if [ -d "$SSD_DATA_DIR" ]; then
        if [[ "$SSD_DATA_DIR" == "/" || "$SSD_DATA_DIR" == "/srv" || "$SSD_DATA_DIR" == "/srv/" || "$SSD_DATA_DIR" == "/home/"* ]]; then
            echo "❌ Error: SSD_DATA_DIR is set to a protected path ($SSD_DATA_DIR). Refusing to wipe."
            exit 1
        fi
        echo "setup-seeder-cache: Wiping existing SSD cache at $SSD_DATA_DIR..."
        rm -rf "${SSD_DATA_DIR:?}"/*
    fi
    
    mkdir -p "$SSD_DATA_DIR"
    chown -R ubuntu:ubuntu "$SSD_DATA_DIR"

    # Make sure we clean up any active RAM mount if we are switching to SSD
    if mountpoint -q "$DATA_DIR" 2>/dev/null; then
        echo "setup-seeder-cache: Unmounting stale RAM cache at $DATA_DIR..."
        umount -f "$DATA_DIR" || true
    fi

    # Create symlink from DATA_DIR to the SSD storage directory
    if [ "$DATA_DIR" != "$SSD_DATA_DIR" ]; then
        if [ -d "$DATA_DIR" ] && [ ! -L "$DATA_DIR" ]; then
            if ! rmdir "$DATA_DIR" 2>/dev/null; then
                echo "❌ Error: $DATA_DIR is not empty and cannot be safely converted to a symlink."
                echo "Please manually empty or remove $DATA_DIR first."
                exit 1
            fi
        fi
        ln -sfn "$SSD_DATA_DIR" "$DATA_DIR"
    fi
    echo "setup-seeder-cache: SSD cache ready at $SSD_DATA_DIR (symlinked from $DATA_DIR)"
    exit 0
fi

# RAM Mode
IMG_BACKING_DIR="/mnt/seeder-cache-backing"  # dedicated tmpfs
IMG_FILE="$IMG_BACKING_DIR/seeder-cache.img"

# If DATA_DIR is currently a symlink (leftover from SSD mode), remove the symlink
# so that the mount syscall mounts onto a real directory, not following the link to SSD.
if [ -L "$DATA_DIR" ]; then
    echo "setup-seeder-cache: Removing SSD symlink at $DATA_DIR to restore RAM mode..."
    rm -f "$DATA_DIR"
fi

# If already mounted, ensure it's healthy
if mountpoint -q "$DATA_DIR" 2>/dev/null; then
    echo "setup-seeder-cache: $DATA_DIR already mounted and active."
    exit 0
fi

# Ensure directories exist
mkdir -p "$DATA_DIR" "$IMG_BACKING_DIR"

# Mount a dedicated tmpfs to back the sparse image (avoids root disk/shm space limits)
if ! mountpoint -q "$IMG_BACKING_DIR" 2>/dev/null; then
    mount -t tmpfs -o size="$RAM_CACHE_SIZE",mode=0750 tmpfs "$IMG_BACKING_DIR"
fi

# Remove stale image if present
[ -f "$IMG_FILE" ] && rm -f "$IMG_FILE"

echo "setup-seeder-cache: creating ${RAM_CACHE_SIZE} sparse image at $IMG_FILE ..."
truncate -s "$RAM_CACHE_SIZE" "$IMG_FILE"

echo "setup-seeder-cache: formatting ext4 (no journal for maximum speed)..."
mkfs.ext4 -F -O ^has_journal -m 0 -E lazy_itable_init=0,lazy_journal_init=0 "$IMG_FILE" >/dev/null 2>&1

echo "setup-seeder-cache: attaching loop device with direct-io..."
LOOP_DEV=$(losetup --find --show --direct-io=on "$IMG_FILE")

echo "setup-seeder-cache: mounting $LOOP_DEV at $DATA_DIR with discard..."
# We explicitly mount with 'discard' (TRIM support) so that hole-punching (FALLOC_FL_PUNCH_HOLE)
# is instantly passed down through the loop device to free physical blocks in the backing tmpfs.
mount -o noatime,nodiratime,discard "$LOOP_DEV" "$DATA_DIR"
chown -R ubuntu:ubuntu "$DATA_DIR"

echo "setup-seeder-cache: RAM ext4 ready at $DATA_DIR (size=$RAM_CACHE_SIZE, discard enabled)"


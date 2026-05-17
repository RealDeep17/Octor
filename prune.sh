#!/bin/bash

# ==============================================================================
# Industry Standard Go & System Pruning Script (Octor Maintenance Tool)
# ==============================================================================
# Ref: Standard Go Toolchain Cache Management & Production Pruning Best Practices
# ==============================================================================

set -euo pipefail

# Ensure we are in a Go-friendly environment
export GOCACHE=$(go env GOCACHE)
export GOMODCACHE=$(go env GOMODCACHE)

echo -e "\033[1;36m===================================================\033[0m"
echo -e "\033[1;36m    🧹 Octor Workspace & System Pruning Tool       \033[0m"
echo -e "\033[1;36m===================================================\033[0m"

# --- 1. Disk Space Statistics (Before) ---
echo -e "\n\033[1;33m[1/4] Calculating Current Disk Space Usage...\033[0m"
before_cache_size=$(du -sh "$GOCACHE" 2>/dev/null | cut -f1 || echo "0B")
before_mod_size=$(du -sh "$GOMODCACHE" 2>/dev/null | cut -f1 || echo "0B")
before_bin_size=$(du -sh "/srv/octor/bin" 2>/dev/null | cut -f1 || echo "0B")

echo -e "  • Go Build Cache Size : \033[1;35m$before_cache_size\033[0m ($GOCACHE)"
echo -e "  • Go Module Cache Size: \033[1;35m$before_mod_size\033[0m ($GOMODCACHE)"
echo -e "  • Local Binaries Size : \033[1;35m$before_bin_size\033[0m (/srv/octor/bin)"

# --- 2. Clean Go Build & Test Caches ---
echo -e "\n\033[1;33m[2/4] Executing Go Toolchain Clean Operations...\033[0m"
echo "  • Safely clearing Go build, test, and fuzz caches..."
go clean -cache -testcache -fuzzcache -v

# Check for `--all` flag to safely clean modcache
clean_modcache=false
for arg in "$@"; do
    if [ "$arg" == "--all" ] || [ "$arg" == "-a" ]; then
        clean_modcache=true
    fi
done

if [ "$clean_modcache" = true ]; then
    echo -e "  • \033[1;31mWarning: cleaning global Go module dependency cache (--all flag detected)\033[0m"
    go clean -modcache -v
else
    echo -e "  • \033[1;32mInfo: Skipped global Module Cache (run with '--all' or '-a' to prune dependencies)\033[0m"
fi

# --- 3. Clean Workspace Binaries & Logs ---
echo -e "\n\033[1;33m[3/4] Cleaning Workspace Binaries & Logs...\033[0m"
if [ -d "/srv/octor/bin" ]; then
    echo "  • Removing compiled Go binaries in /srv/octor/bin..."
    rm -rf /srv/octor/bin/*
    echo -e "    \033[1;32m✔ Local binaries cleaned.\033[0m"
else
    echo "  • No local /srv/octor/bin directory found."
fi

# Vacuum old journal logs older than 3 days
if command -v journalctl &> /dev/null; then
    echo "  • Pruning systemd journal logs older than 3 days..."
    sudo journalctl --vacuum-time=3d || echo "    ⚠ Failed to vacuum journal logs (requires sudo privileges)."
fi

# --- 4. Disk Space Statistics (After) ---
echo -e "\n\033[1;33m[4/4] Calculating Reclaimed Disk Space...\033[0m"
after_cache_size=$(du -sh "$GOCACHE" 2>/dev/null | cut -f1 || echo "0B")
after_mod_size=$(du -sh "$GOMODCACHE" 2>/dev/null | cut -f1 || echo "0B")
after_bin_size=$(du -sh "/srv/octor/bin" 2>/dev/null | cut -f1 || echo "0B")

echo -e "\033[1;32m===================================================\033[0m"
echo -e "\033[1;32m🎉 Cleanup Complete!\033[0m"
echo -e "\033[1;32m===================================================\033[0m"
echo -e "  • Go Build Cache  : $before_cache_size ➔ \033[1;32m$after_cache_size\033[0m"
echo -e "  • Go Module Cache : $before_mod_size ➔ \033[1;32m$after_mod_size\033[0m"
echo -e "  • Local Binaries  : $before_bin_size ➔ \033[1;32m$after_bin_size\033[0m"
echo -e "\033[1;32m===================================================\033[0m"

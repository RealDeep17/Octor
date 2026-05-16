#!/bin/bash
PATHS=(
    "$HOME/.docker/run/docker.sock"
    "/var/run/docker.sock"
    "$HOME/.docker/desktop/docker.sock"
)

echo "Searching for active Docker socket..."
for p in "${PATHS[@]}"; do
    if [ -S "$p" ]; then
        echo "Testing $p..."
        if DOCKER_HOST="unix://$p" docker ps > /dev/null 2>&1; then
            echo "✅ FOUND WORKING SOCKET: $p"
            echo "Run this to fix your environment:"
            echo "export DOCKER_HOST=unix://$p"
            exit 0
        else
            echo "❌ $p exists but connection failed."
        fi
    else
        echo "⚪ $p does not exist."
    fi
done

echo "❌ No working Docker socket found. Please ensure Docker Desktop is fully started."

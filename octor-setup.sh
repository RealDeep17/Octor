#!/usr/bin/env bash
# ==============================================================================
# OCTOR UNIVERSAL MONOLITH RUNNER & INSTALLER (octor-setup.sh)
# ==============================================================================
# Orchestrates host network configuration, runtime verification, systemd setups,
# monolith docker builds, automated *arr stack integration, and Nginx reverse proxy.
# ==============================================================================

set -euo pipefail

# --- Setup Paths ---
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$PROJECT_ROOT/custom.env"

echo "=== OCTOR UNIVERSAL MONOLITH DEPLOYMENT INITIALIZED ==="
echo "Working Directory: $PROJECT_ROOT"

# Ensure environment file exists
if [[ ! -f "$ENV_FILE" ]]; then
    if [[ -f "$PROJECT_ROOT/example.env" ]]; then
        echo "Creating custom.env from example.env template..."
        sed "s|__PROJECT_ROOT__|$PROJECT_ROOT|g" "$PROJECT_ROOT/example.env" > "$ENV_FILE"
    else
        echo "❌ Error: custom.env or example.env not found!"
        exit 1
    fi
fi

# Load variables
export $(grep -v '^#' "$ENV_FILE" | xargs)

# ------------------------------------------------------------------------------
# 1. HOST ENVIRONMENT OPTIMIZATION (Interactive)
# ------------------------------------------------------------------------------
echo "--- Step 1: Host System Optimizations ---"

if [ "$(uname -s)" != "Linux" ]; then
    echo "⚠️  System optimizations (BBR, TCP buffers, swap) are Linux-only and not supported on $(uname -s). Skipping..."
else
    # Prompt BBR
    read -p "Enable Google BBR TCP congestion control? (y/n) [y]: " CONFIRM_BBR
    CONFIRM_BBR=${CONFIRM_BBR:-y}
    if [[ "$CONFIRM_BBR" =~ ^[yY]$ ]]; then
        echo "Configuring Google BBR..."
        if ! sysctl net.ipv4.tcp_congestion_control | grep -q "bbr"; then
            sudo bash -c 'echo "net.core.default_qdisc=fq" >> /etc/sysctl.conf'
            sudo bash -c 'echo "net.ipv4.tcp_congestion_control=bbr" >> /etc/sysctl.conf'
            sudo sysctl -p
            echo "✓ BBR congestion control enabled successfully."
        else
            echo "✓ BBR congestion control is already active."
        fi
    fi

    # Prompt TCP Buffers
    read -p "Configure optimized TCP buffers and file limits? (y/n) [y]: " CONFIRM_TCP
    CONFIRM_TCP=${CONFIRM_TCP:-y}
    if [[ "$CONFIRM_TCP" =~ ^[yY]$ ]]; then
        echo "Tuning TCP buffers and open files limit..."
        if ! grep -q "net.core.rmem_max" /etc/sysctl.conf; then
            sudo bash -c 'cat >> /etc/sysctl.conf << EOF
# Octor custom network tuning
net.core.rmem_max=67108864
net.core.wmem_max=67108864
net.ipv4.tcp_rmem=4096 87380 67108864
net.ipv4.tcp_wmem=4096 65536 67108864
fs.file-max=2097152
EOF'
            sudo sysctl -p
            echo "✓ Network socket buffers optimized successfully."
        else
            echo "✓ Network socket buffers are already optimized."
        fi
    fi

    # Prompt Swap
    read -p "Create/Extend a swap file? (y/n) [n]: " CONFIRM_SWAP
    CONFIRM_SWAP=${CONFIRM_SWAP:-n}
    if [[ "$CONFIRM_SWAP" =~ ^[yY]$ ]]; then
        read -p "Enter swap size in GB [4]: " SWAP_SIZE
        SWAP_SIZE=${SWAP_SIZE:-4}
        if [ ! -f /swapfile ]; then
            echo "Creating a ${SWAP_SIZE}GB swap file..."
            sudo fallocate -l "${SWAP_SIZE}G" /swapfile
            sudo chmod 600 /swapfile
            sudo mkswap /swapfile
            sudo swapon /swapfile
            sudo bash -c 'echo "/swapfile swap swap defaults 0 0" >> /etc/fstab'
            echo "✓ Swap file created successfully."
        else
            echo "✓ Swap file already exists."
        fi
    fi
fi

# ------------------------------------------------------------------------------
# 2. RUNTIME DEPENDENCY CHECK
# ------------------------------------------------------------------------------
echo "--- Step 2: Runtime Dependency Verification ---"
RUNTIMES=("docker" "rclone" "nginx" "jq" "curl")
for cmd in "${RUNTIMES[@]}"; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "⚠️  Missing required runtime: $cmd"
        echo "Attempting to install $cmd automatically..."
        if command -v apt-get >/dev/null 2>&1; then
            if [ "$cmd" = "rclone" ]; then
                sudo apt-get install -y unzip && curl https://rclone.org/install.sh | sudo bash
            elif [ "$cmd" = "docker" ]; then
                sudo apt-get install -y docker.io docker-buildx && sudo usermod -aG docker "$USER"
            else
                sudo apt-get install -y "$cmd"
            fi
        elif [ "$(uname -s)" = "Darwin" ] && command -v brew >/dev/null 2>&1; then
            if [ "$cmd" = "docker" ]; then
                echo "Please install Docker Desktop for macOS: https://www.docker.com/products/docker-desktop"
                exit 1
            else
                brew install "$cmd"
            fi
        else
            echo "❌ Error: Cannot install $cmd automatically. Package manager (apt-get or brew) not found."
            echo "Please install $cmd manually before proceeding."
            exit 1
        fi
    fi
    echo "✓ runtime check passed: $cmd"
done

# Ensure docker daemon is active
if command -v systemctl >/dev/null 2>&1; then
    if ! sudo systemctl is-active docker >/dev/null 2>&1; then
        echo "Starting Docker service..."
        sudo systemctl start docker
    fi
fi

# ------------------------------------------------------------------------------
# 3. STORAGE LAYER WIRING (Systemd configuration)
# ------------------------------------------------------------------------------
echo "--- Step 3: Storage Layer & Cache Systemd Setup ---"

# 1. Seeder cache service
echo "Configuring octor-seeder-cache.service..."
sed "s|__PROJECT_ROOT__|$PROJECT_ROOT|g" << 'EOF' > /tmp/octor-seeder-cache.service
[Unit]
Description=Octor Seeder Cache Setup (SSD or RAM ext4)
Before=docker.service
After=local-fs.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=__PROJECT_ROOT__/scripts/setup-seeder-cache.sh
ExecStop=__PROJECT_ROOT__/scripts/teardown-seeder-cache.sh

[Install]
WantedBy=multi-user.target
EOF
sudo cp /tmp/octor-seeder-cache.service /etc/systemd/system/octor-seeder-cache.service

# 2. Rclone Google Drive union mount service
echo "Configuring octor-rclone-mount.service..."
sed -e "s|__PROJECT_ROOT__|$PROJECT_ROOT|g" -e "s|__ENV_FILE__|$ENV_FILE|g" << 'EOF' > /tmp/octor-rclone-mount.service
[Unit]
Description=Octor Rclone Google Drive Union Mount
After=network.target octor-seeder-cache.service
Requires=octor-seeder-cache.service
Before=docker.service

[Service]
Type=simple
User=root
Group=root
EnvironmentFile=__ENV_FILE__
ExecStartPre=/usr/bin/mkdir -p __PROJECT_ROOT__/infra-data/drive-mount-vfs
ExecStartPre=/usr/bin/mkdir -p ${RCLONE_CACHE_DIR}
ExecStart=/usr/bin/rclone --config ${VAULT_RCLONE_CONFIG} mount ${VAULT_RCLONE_REMOTE} __PROJECT_ROOT__/infra-data/drive-mount-vfs \
    --vfs-cache-mode ${RCLONE_VFS_CACHE_MODE} \
    --vfs-cache-max-size ${RCLONE_VFS_CACHE_MAX_SIZE} \
    --vfs-cache-max-age ${RCLONE_VFS_CACHE_MAX_AGE} \
    --vfs-cache-poll-interval ${RCLONE_VFS_CACHE_POLL_INTERVAL} \
    --cache-dir ${RCLONE_CACHE_DIR} \
    --buffer-size ${RCLONE_BUFFER_SIZE} \
    --drive-chunk-size ${RCLONE_DRIVE_CHUNK_SIZE} \
    --transfers ${RCLONE_TRANSFERS} \
    --drive-use-trash=false \
    --links \
    --direct-io \
    --allow-other \
    --umask 000 --allow-non-empty
ExecStop=/usr/bin/fusermount -uz __PROJECT_ROOT__/infra-data/drive-mount-vfs
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
sudo cp /tmp/octor-rclone-mount.service /etc/systemd/system/octor-rclone-mount.service

# 3. Reload daemon and enable services
sudo systemctl daemon-reload
sudo systemctl enable octor-seeder-cache.service octor-rclone-mount.service

# 4. Start caching and mount layer
echo "Starting storage mount layers..."
sudo systemctl restart octor-seeder-cache.service
sudo systemctl restart octor-rclone-mount.service

# Check mount point
echo "Verifying mount point..."
MOUNT_PATH="$PROJECT_ROOT/infra-data/drive-mount-vfs"
ELAPSED=0
while [ $ELAPSED -lt 30 ]; do
    if mountpoint -q "$MOUNT_PATH" 2>/dev/null; then
        echo "✓ Storage mounted successfully at $MOUNT_PATH"
        break
    fi
    printf "."
    sleep 2
    ELAPSED=$((ELAPSED + 2))
done
if ! mountpoint -q "$MOUNT_PATH" 2>/dev/null; then
    echo "⚠️  Storage mount is taking longer than expected. Outbound seeding might be delayed."
fi

# ------------------------------------------------------------------------------
# 4. BUILD & RUN MONOLITH CONTAINER
# ------------------------------------------------------------------------------
echo "--- Step 4: Building & Launching Octor Monolith Container ---"

# Build and start container services via docker-compose
echo "Rebuilding and restarting Octor Monolith Docker container..."
sudo docker compose down
sudo docker compose up -d --build --force-recreate

# Wait for database/infrastructure readiness
echo -n "Waiting for database and queue connectivity"
for i in {1..30}; do
    if nc -z localhost 4222 >/dev/null 2>&1 && sudo docker exec octor-postgres pg_isready -U octor -q >/dev/null 2>&1; then
        echo " ✓ Connectivity OK."
        break
    fi
    printf "."
    sleep 1
done

# Initialize NATS streams
echo "Initializing NATS JetStream..."
sudo docker exec -i octor-monolith /bin/bash -c "/app/bin/create_nats_stream" || echo "⚠️  NATS stream initialization failed."

# ------------------------------------------------------------------------------
# 5. HOST REVERSE PROXY & SSL (Nginx Configuration)
# ------------------------------------------------------------------------------
echo "--- Step 5: Configuring Host Nginx & Let's Encrypt SSL ---"

NGINX_TEMPLATE="$PROJECT_ROOT/deploy/nginx/octor.nginx"
NGINX_CONF="/etc/nginx/sites-available/octor"
NGINX_LINK="/etc/nginx/sites-enabled/octor"

# 1. Update upstream block and local path mappings in Nginx config
echo "Generating site configuration block..."
# Replace octor.duckdns.org with current DOMAIN, and /srv/octor with actual PROJECT_ROOT
sed -e "s|/srv/octor|$PROJECT_ROOT|g" \
    -e "s|octor.duckdns.org|$DOMAIN|g" \
    "$NGINX_TEMPLATE" > /tmp/octor.nginx

# Handle SSL Certs Chicken-and-egg problem:
# If real Lets Encrypt files don't exist yet, we replace Let's Encrypt configuration with self-signed certificate path
# to allow Nginx to start successfully, then let certbot configure them properly.
REAL_CERT_DIR="/etc/letsencrypt/live/$DOMAIN"
if [ ! -f "$REAL_CERT_DIR/fullchain.pem" ]; then
    echo "Let's Encrypt certificate not found for $DOMAIN. Bootstrapping temporary self-signed SSL cert..."
    sudo mkdir -p /etc/ssl/certs /etc/ssl/private
    sudo openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
        -keyout "/etc/ssl/private/$DOMAIN.key" \
        -out "/etc/ssl/certs/$DOMAIN.crt" \
        -subj "/CN=$DOMAIN"
    
    # Rewrite template cert path to self-signed path
    sed -i "s|/etc/letsencrypt/live/.*/fullchain.pem|/etc/ssl/certs/$DOMAIN.crt|g" /tmp/octor.nginx
    sed -i "s|/etc/letsencrypt/live/.*/privkey.pem|/etc/ssl/private/$DOMAIN.key|g" /tmp/octor.nginx
else
    # Correct path to domain cert
    sed -i "s|/etc/letsencrypt/live/.*/fullchain.pem|$REAL_CERT_DIR/fullchain.pem|g" /tmp/octor.nginx
    sed -i "s|/etc/letsencrypt/live/.*/privkey.pem|$REAL_CERT_DIR/privkey.pem|g" /tmp/octor.nginx
fi

sudo cp /tmp/octor.nginx "$NGINX_CONF"
sudo ln -sf "$NGINX_CONF" "$NGINX_LINK"
sudo rm -f "/etc/nginx/sites-enabled/default" || true

# Test and reload Nginx
if sudo nginx -t; then
    echo "Reloading Nginx service..."
    sudo systemctl restart nginx
else
    echo "❌ Error: Nginx configuration test failed!"
    exit 1
fi

# Run Certbot if we are using self-signed certs to configure Let's Encrypt properly
if [ ! -f "$REAL_CERT_DIR/fullchain.pem" ]; then
    read -p "Let's Encrypt certs are missing. Run certbot for domain $DOMAIN now? (y/n) [y]: " RUN_CERTBOT
    RUN_CERTBOT=${RUN_CERTBOT:-y}
    if [[ "$RUN_CERTBOT" =~ ^[yY]$ ]]; then
        if ! command -v certbot >/dev/null 2>&1; then
            echo "Installing certbot..."
            if command -v apt-get >/dev/null 2>&1; then
                sudo apt-get install -y certbot python3-certbot-nginx
            elif [ "$(uname -s)" = "Darwin" ] && command -v brew >/dev/null 2>&1; then
                brew install certbot
            else
                echo "❌ Error: Cannot install certbot automatically. Please install it manually."
                exit 1
            fi
        fi
        if [ -z "${LETSENCRYPT_EMAIL:-}" ]; then
            read -p "Enter Let's Encrypt notification email [admin@$DOMAIN]: " USER_EMAIL
            LETSENCRYPT_EMAIL=${USER_EMAIL:-admin@$DOMAIN}
        fi
        echo "Running Certbot for $DOMAIN with email $LETSENCRYPT_EMAIL..."
        sudo certbot --nginx -d "$DOMAIN" --non-interactive --agree-tos -m "$LETSENCRYPT_EMAIL" || echo "⚠️  Certbot SSL verification failed. Using self-signed cert for now."
    fi
fi

# ------------------------------------------------------------------------------
# 6. TRIGGER SERVICES HEALTHCHECK
# ------------------------------------------------------------------------------
echo "--- Step 6: Verifying Octor Deployment ---"
sleep 5
echo "Octor Web UI endpoint:"
curl -sfI "http://localhost:8082/" | head -n 1 || echo "⚠️  Web UI is not responding on 8082"
echo "Octor REST API endpoint:"
curl -sfI "http://localhost:8080/liveness" | head -n 1 || echo "⚠️  REST API is not responding on 52080/8080"
echo "Octor Vault endpoint:"
curl -sfI "http://localhost:8086/liveness" | head -n 1 || echo "⚠️  Vault service is not responding on 52086/8086"

echo "======================================================================"
echo "🎉 OCTOR MONOLITH INSTALLED AND RUNNING AT: https://$DOMAIN"
echo "======================================================================"

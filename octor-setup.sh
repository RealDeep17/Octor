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

# Process-specific temporary directory to avoid sharing/permission conflicts
TEMP_DIR="/tmp/octor-setup-$$"
mkdir -p "$TEMP_DIR"
trap 'rm -rf "$TEMP_DIR"' EXIT

# Helper function to run sed -i portably across GNU (Linux) and BSD (macOS)
run_sed_in_place() {
    local expr="$1"
    local file="$2"
    if sed --version >/dev/null 2>&1; then
        sed -i "$expr" "$file"
    else
        sed -i "" "$expr" "$file"
    fi
}


echo "=== OCTOR UNIVERSAL MONOLITH DEPLOYMENT INITIALIZED ==="
echo "Working Directory: $PROJECT_ROOT"

# Helper function to safely update key=value pairs in custom.env
update_env_var() {
    local key="$1"
    local val="$2"
    # Escape special regex characters for sed replacement including pipe delimiter
    local esc_val=$(echo "$val" | sed 's/[&/\|]/\\&/g')
    if grep -q "^$key=" "$ENV_FILE"; then
        run_sed_in_place "s|^$key=.*|$key=$esc_val|g" "$ENV_FILE"
    else
        echo "$key=$val" >> "$ENV_FILE"
    fi
}

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


# Run the interactive configuration wizard
run_config_wizard() {
    # Check if custom.env exists. If not, copy example.env
    if [[ ! -f "$ENV_FILE" ]]; then
        if [[ -f "$PROJECT_ROOT/example.env" ]]; then
            echo "Creating custom.env from example.env template..."
            sed "s|__PROJECT_ROOT__|$PROJECT_ROOT|g" "$PROJECT_ROOT/example.env" > "$ENV_FILE"
        else
            echo "❌ Error: example.env template not found!"
            exit 1
        fi
    fi

    # Load existing variables so we can show current defaults
    # Use || true to prevent set -e from exiting if grep returns no matches
    local CURRENT_DOMAIN=$(grep '^DOMAIN=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)
    local CURRENT_LETSENCRYPT_EMAIL=$(grep '^LETSENCRYPT_EMAIL=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)
    local CURRENT_ADMIN_EMAILS=$(grep '^ADMIN_EMAILS=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)
    local CURRENT_GOOGLE_CLIENT_ID=$(grep '^GOOGLE_CLIENT_ID=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)
    local CURRENT_GOOGLE_CLIENT_SECRET=$(grep '^GOOGLE_CLIENT_SECRET=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)
    local CURRENT_GEMINI_API_KEY=$(grep '^GEMINI_API_KEY=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)
    local CURRENT_TMDB_API_KEY=$(grep '^TMDB_API_KEY=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)
    local CURRENT_TMDB_API_READ_ACCESS_TOKEN=$(grep '^TMDB_API_READ_ACCESS_TOKEN=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)
    local CURRENT_VAULT_RCLONE_CONFIG=$(grep '^VAULT_RCLONE_CONFIG=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)
    local CURRENT_VAULT_RCLONE_REMOTE=$(grep '^VAULT_RCLONE_REMOTE=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)
    local CURRENT_OCTOR_ARR_CONFIG_DIR=$(grep '^OCTOR_ARR_CONFIG_DIR=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)
    local CURRENT_SESSION_SECRET=$(grep '^SESSION_SECRET=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)
    local CURRENT_AUTOMATION_API_KEY=$(grep '^AUTOMATION_API_KEY=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)

    local RUN_WIZARD="y"
    if [[ -n "$CURRENT_DOMAIN" ]]; then
        echo "✓ Found existing Octor configuration in custom.env (Domain: $CURRENT_DOMAIN)."
        read -p "Run interactive configuration wizard? (y/n) [n]: " CONFIRM_WIZARD
        CONFIRM_WIZARD=${CONFIRM_WIZARD:-n}
        if [[ ! "$CONFIRM_WIZARD" =~ ^[yY]$ ]]; then
            RUN_WIZARD="n"
        fi
    fi

    if [[ "$RUN_WIZARD" = "n" ]]; then
        echo "Skipping interactive wizard. Loading existing configuration..."
        load_env "$ENV_FILE"
        return 0
    fi

    echo "========================================================"
    echo "         OCTOR INTERACTIVE CONFIGURATION WIZARD"
    echo "========================================================"
    
    # 1. Domain
    echo "🌐 Domain Configuration"
    echo "--------------------------------------------------------"
    echo "If you don't have a public domain, get a free subdomain at: https://www.duckdns.org/"
    local INPUT_DOMAIN=""
    while [[ -z "$INPUT_DOMAIN" ]]; do
        read -p "Enter public domain name (e.g. yourname.duckdns.org) [$CURRENT_DOMAIN]: " INPUT_DOMAIN
        INPUT_DOMAIN=${INPUT_DOMAIN:-$CURRENT_DOMAIN}
        if [[ -z "$INPUT_DOMAIN" ]]; then
            echo "❌ Domain is mandatory. Please enter a valid domain."
        fi
    done
    update_env_var "DOMAIN" "$INPUT_DOMAIN"
    update_env_var "OCTOR_DOMAIN" "https://$INPUT_DOMAIN"
    update_env_var "EXTERNAL_URL" "https://$INPUT_DOMAIN"
    echo "✓ Set DOMAIN=$INPUT_DOMAIN"
    echo "✓ Set OCTOR_DOMAIN=https://$INPUT_DOMAIN"
    echo "✓ Set EXTERNAL_URL=https://$INPUT_DOMAIN"
    echo ""

    # 2. Let's Encrypt Email
    echo "🔒 Let's Encrypt SSL Setup"
    echo "--------------------------------------------------------"
    local INPUT_SSL=""
    while [[ -z "$INPUT_SSL" ]]; do
        read -p "Enter email for certbot SSL registrations [$CURRENT_LETSENCRYPT_EMAIL]: " INPUT_SSL
        INPUT_SSL=${INPUT_SSL:-$CURRENT_LETSENCRYPT_EMAIL}
        if [[ -z "$INPUT_SSL" ]]; then
            echo "❌ SSL Email is mandatory."
        fi
    done
    update_env_var "LETSENCRYPT_EMAIL" "$INPUT_SSL"
    echo "✓ Set LETSENCRYPT_EMAIL=$INPUT_SSL"
    echo ""

    # 3. Admin Access
    echo "👤 Administrator Access"
    echo "--------------------------------------------------------"
    local INPUT_ADMIN=""
    read -p "Enter Google admin email addresses (comma-separated) [$CURRENT_ADMIN_EMAILS]: " INPUT_ADMIN
    INPUT_ADMIN=${INPUT_ADMIN:-$CURRENT_ADMIN_EMAILS}
    update_env_var "ADMIN_EMAILS" "$INPUT_ADMIN"
    echo "✓ Set ADMIN_EMAILS=$INPUT_ADMIN"
    echo ""

    # 4. Adult Metadata Setup (conditional on admin email)
    if [[ -n "$INPUT_ADMIN" ]]; then
        read -p "Configure Adult Search & Enrichment API keys? (y/n) [y]: " CONFIRM_ADULT
        CONFIRM_ADULT=${CONFIRM_ADULT:-y}
        if [[ "$CONFIRM_ADULT" =~ ^[yY]$ ]]; then
            local CURRENT_STASH=$(grep '^STASHDB_API_KEY=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)
            local CURRENT_TPDB=$(grep '^THEPORNDB_API_KEY=' "$ENV_FILE" | cut -d= -f2- | tr -d '\r' || true)

            echo "Get a free StashDB scene key at: https://stashdb.org/"
            read -p "Enter StashDB API Key [$CURRENT_STASH]: " INPUT_STASH
            INPUT_STASH=${INPUT_STASH:-$CURRENT_STASH}
            update_env_var "STASHDB_API_KEY" "$INPUT_STASH"

            echo "Get a free ThePornDB studio key at: https://theporndb.net/"
            read -p "Enter ThePornDB API Key [$CURRENT_TPDB]: " INPUT_TPDB
            INPUT_TPDB=${INPUT_TPDB:-$CURRENT_TPDB}
            update_env_var "THEPORNDB_API_KEY" "$INPUT_TPDB"
            
            echo "✓ Adult metadata keys configured."
        fi
    else
        echo "Skipping Adult Search API keys setup (no administrator emails configured)."
    fi
    echo ""

    # 5. Google Sign-In / OAuth
    echo "🔑 Google Login / OAuth Setup"
    echo "--------------------------------------------------------"
    echo "To log in to the Octor admin panel, you must configure Google Sign-In:"
    echo "1. Go to: https://console.cloud.google.com/apis/credentials"
    echo "2. Click 'Create Credentials' -> 'OAuth Client ID' (Web Application)."
    echo "3. Enable Google People API in API Library for profile details."
    echo "4. Under 'Authorized redirect URIs', add this exact URI:"
    echo "   👉 https://$INPUT_DOMAIN/auth/callback/google"
    echo "--------------------------------------------------------"
    local INPUT_G_ID=""
    read -p "Enter Google OAuth Client ID [$CURRENT_GOOGLE_CLIENT_ID]: " INPUT_G_ID
    INPUT_G_ID=${INPUT_G_ID:-$CURRENT_GOOGLE_CLIENT_ID}
    update_env_var "GOOGLE_CLIENT_ID" "$INPUT_G_ID"

    local INPUT_G_SECRET=""
    read -p "Enter Google OAuth Client Secret [$CURRENT_GOOGLE_CLIENT_SECRET]: " INPUT_G_SECRET
    INPUT_G_SECRET=${INPUT_G_SECRET:-$CURRENT_GOOGLE_CLIENT_SECRET}
    update_env_var "GOOGLE_CLIENT_SECRET" "$INPUT_G_SECRET"
    echo "✓ Google OAuth credentials saved."
    echo ""

    # 6. Gemini AI
    echo "🤖 Gemini AI Recommendations & Enrichment Setup"
    echo "--------------------------------------------------------"
    read -p "Configure AI Recommendations & Enrichment features? (y/n) [y]: " CONFIRM_GEMINI
    CONFIRM_GEMINI=${CONFIRM_GEMINI:-y}
    if [[ "$CONFIRM_GEMINI" =~ ^[yY]$ ]]; then
        echo "Register for a free Gemini API key at: https://aistudio.google.com/"
        local INPUT_GEMINI=""
        read -p "Enter Gemini API Key [$CURRENT_GEMINI_API_KEY]: " INPUT_GEMINI
        INPUT_GEMINI=${INPUT_GEMINI:-$CURRENT_GEMINI_API_KEY}
        
        if [[ -n "$INPUT_GEMINI" ]]; then
            update_env_var "GEMINI_API_KEY" "$INPUT_GEMINI"
            update_env_var "AI_RECOMMENDATIONS_ENABLED" "true"
            update_env_var "AI_ENRICH_ENABLED" "true"
            echo "✓ AI features enabled with Gemini API key."
        else
            update_env_var "AI_RECOMMENDATIONS_ENABLED" "false"
            update_env_var "AI_ENRICH_ENABLED" "false"
            echo "⚠️  Gemini API key empty. AI features disabled to prevent container errors."
        fi
    else
        update_env_var "AI_RECOMMENDATIONS_ENABLED" "false"
        update_env_var "AI_ENRICH_ENABLED" "false"
        echo "✓ AI features disabled."
    fi
    echo ""

    # 7. Rclone & Storage Pools
    echo "💾 Rclone & Storage Pools Setup"
    echo "--------------------------------------------------------"
    local INPUT_R_CONFIG=""
    local DEFAULT_R_CONFIG="/home/ubuntu/.config/rclone/rclone.conf"
    if [[ -n "$CURRENT_VAULT_RCLONE_CONFIG" ]]; then
        DEFAULT_R_CONFIG="$CURRENT_VAULT_RCLONE_CONFIG"
    fi
    read -p "Enter path to rclone.conf [$DEFAULT_R_CONFIG]: " INPUT_R_CONFIG
    INPUT_R_CONFIG=${INPUT_R_CONFIG:-$DEFAULT_R_CONFIG}
    update_env_var "VAULT_RCLONE_CONFIG" "$INPUT_R_CONFIG"

    # Auto-create directory if config path parent doesn't exist
    mkdir -p "$(dirname "$INPUT_R_CONFIG")"
    if [[ ! -f "$INPUT_R_CONFIG" ]]; then
        touch "$INPUT_R_CONFIG"
    fi

    # Prompt to run rclone config in foreground
    read -p "Run 'rclone config' in the foreground to manage cloud remotes? (y/n) [n]: " CONFIRM_R_CFG
    CONFIRM_R_CFG=${CONFIRM_R_CFG:-n}
    if [[ "$CONFIRM_R_CFG" =~ ^[yY]$ ]]; then
        rclone --config "$INPUT_R_CONFIG" config
    fi

    # Read active remotes from rclone.conf (exclude ALPHA_UNION, ALPHA_CHUNKER, and comments)
    local REMOTES=($(grep -E '^\[[a-zA-Z0-9_-]+\]' "$INPUT_R_CONFIG" | tr -d '[]' | grep -vE 'ALPHA_UNION|ALPHA_CHUNKER' || true))
    local REMOTE_COUNT=${#REMOTES[@]}

    local R_REMOTE=""
    if [[ $REMOTE_COUNT -eq 0 ]]; then
        echo "⚠️  No cloud drives found in $INPUT_R_CONFIG."
        read -p "Enter name of rclone remote (must configure it later): " R_REMOTE
        update_env_var "VAULT_RCLONE_REMOTE" "$R_REMOTE:"
    elif [[ $REMOTE_COUNT -eq 1 ]]; then
        local DETECTED_REMOTE="${REMOTES[0]}"
        echo "Single cloud drive detected: $DETECTED_REMOTE"
        echo "1) Use directly"
        echo "2) Set up a multi-drive storage pool (pre-configures a pool so you can easily add more cloud accounts later)"
        local SINGLE_DRIVE_CHOICE=""
        while [[ ! "$SINGLE_DRIVE_CHOICE" =~ ^[12]$ ]]; do
            read -p "Select option [1]: " SINGLE_DRIVE_CHOICE
            SINGLE_DRIVE_CHOICE=${SINGLE_DRIVE_CHOICE:-1}
        done

        if [[ "$SINGLE_DRIVE_CHOICE" -eq 1 ]]; then
            R_REMOTE="$DETECTED_REMOTE"
            update_env_var "VAULT_RCLONE_REMOTE" "$R_REMOTE:"
            echo "✓ Configured Vault remote to direct drive: $R_REMOTE:"
        else
            # Pre-configure ALPHA_UNION with single drive
            echo "Generating multi-drive union storage pool (ALPHA_UNION)..."
            python3 -c "
import configparser
config = configparser.ConfigParser()
config.read('$INPUT_R_CONFIG')
if 'ALPHA_UNION' in config:
    config.remove_section('ALPHA_UNION')
config['ALPHA_UNION'] = {
    'type': 'union',
    'upstreams': '$DETECTED_REMOTE:',
    'action_policy': 'mfs',
    'create_policy': 'mfs',
    'search_policy': 'ff',
    'cache_time': '600'
}
with open('$INPUT_R_CONFIG', 'w') as f:
    config.write(f)
"
            R_REMOTE="ALPHA_UNION"
            update_env_var "VAULT_RCLONE_REMOTE" "ALPHA_UNION:"
            echo "✓ pre-configured storage pool (ALPHA_UNION) with drive: $DETECTED_REMOTE:"
        fi
    else
        # Multiple remotes exist, configure storage pool (ALPHA_UNION)
        echo "Available cloud storage remotes:"
        for i in "${!REMOTES[@]}"; do
            echo "  $((i+1))) ${REMOTES[$i]}"
        done
        echo "  c) Custom name (enter custom remote name manually)"

        read -p "Select which drives to combine into a storage pool (comma-separated numbers, e.g. 1,2) [c]: " UNION_SELECT
        UNION_SELECT=${UNION_SELECT:-c}

        if [[ "$UNION_SELECT" != "c" ]]; then
            # Build list of upstreams
            local UPSTREAMS=""
            IFS=',' read -ra ADDR <<< "$UNION_SELECT"
            for idx in "${ADDR[@]}"; do
                idx=$(echo "$idx" | xargs)
                if [[ $idx -ge 1 && $idx -le $REMOTE_COUNT ]]; then
                    local r_name="${REMOTES[$((idx-1))]}"
                    UPSTREAMS="$UPSTREAMS $r_name:"
                fi
            done
            UPSTREAMS=$(echo "$UPSTREAMS" | xargs)

            if [[ -n "$UPSTREAMS" ]]; then
                echo "Generating union storage pool (ALPHA_UNION) with upstreams: $UPSTREAMS"
                python3 -c "
import configparser
config = configparser.ConfigParser()
config.read('$INPUT_R_CONFIG')
if 'ALPHA_UNION' in config:
    config.remove_section('ALPHA_UNION')
config['ALPHA_UNION'] = {
    'type': 'union',
    'upstreams': '$UPSTREAMS',
    'action_policy': 'mfs',
    'create_policy': 'mfs',
    'search_policy': 'ff',
    'cache_time': '600'
}
with open('$INPUT_R_CONFIG', 'w') as f:
    config.write(f)
"
                R_REMOTE="ALPHA_UNION"
                update_env_var "VAULT_RCLONE_REMOTE" "ALPHA_UNION:"
                echo "✓ preconfigured storage pool (ALPHA_UNION) successfully."
            else
                echo "⚠️  No valid remotes selected. Defaulting to custom name..."
                UNION_SELECT="c"
            fi
        fi

        if [[ "$UNION_SELECT" = "c" ]]; then
            read -p "Enter Rclone remote name manually (e.g. GDrive:) [$CURRENT_VAULT_RCLONE_REMOTE]: " R_REMOTE
            R_REMOTE=${R_REMOTE:-$CURRENT_VAULT_RCLONE_REMOTE}
            if [[ "$R_REMOTE" != *":" ]]; then R_REMOTE="${R_REMOTE}:"; fi
            update_env_var "VAULT_RCLONE_REMOTE" "$R_REMOTE"
            echo "✓ Vault remote set to: $R_REMOTE"
        fi
    fi
    echo ""

    # 8. TMDB API
    echo "🎬 TMDB Movie/TV Metadata Setup"
    echo "--------------------------------------------------------"
    echo "Get a free TMDB API key at: https://www.themoviedb.org/"
    local INPUT_TMDB=""
    read -p "Enter TMDB API Key [$CURRENT_TMDB_API_KEY]: " INPUT_TMDB
    INPUT_TMDB=${INPUT_TMDB:-$CURRENT_TMDB_API_KEY}
    update_env_var "TMDB_API_KEY" "$INPUT_TMDB"

    local INPUT_TMDB_READ=""
    read -p "Enter TMDB Read Access Token (Optional) [$CURRENT_TMDB_API_READ_ACCESS_TOKEN]: " INPUT_TMDB_READ
    INPUT_TMDB_READ=${INPUT_TMDB_READ:-$CURRENT_TMDB_API_READ_ACCESS_TOKEN}
    update_env_var "TMDB_API_READ_ACCESS_TOKEN" "$INPUT_TMDB_READ"
    echo "✓ TMDB API key saved."
    echo ""

    # 9. Octor Arr Config Dir
    echo "📂 Arr Application Integration"
    echo "--------------------------------------------------------"
    local DEFAULT_ARR_DIR="$PROJECT_ROOT/../Big ARRS/config"
    if [[ -n "$CURRENT_OCTOR_ARR_CONFIG_DIR" ]]; then
        DEFAULT_ARR_DIR="$CURRENT_OCTOR_ARR_CONFIG_DIR"
    fi
    local INPUT_ARR_DIR=""
    read -p "Enter absolute path to Arr config directory [$DEFAULT_ARR_DIR]: " INPUT_ARR_DIR
    INPUT_ARR_DIR=${INPUT_ARR_DIR:-$DEFAULT_ARR_DIR}
    update_env_var "OCTOR_ARR_CONFIG_DIR" "$INPUT_ARR_DIR"
    echo "✓ Arr config directory path saved."
    echo ""

    # 10. Secrets generation
    echo "⚙️  Generating Security Credentials"
    echo "--------------------------------------------------------"
    if [[ -z "$CURRENT_SESSION_SECRET" ]]; then
        local RAND_SESSION=$(openssl rand -hex 32)
        update_env_var "SESSION_SECRET" "$RAND_SESSION"
        echo "✓ Generated new secure SESSION_SECRET."
    else
        echo "✓ Preserved existing SESSION_SECRET."
    fi

    if [[ -z "$CURRENT_AUTOMATION_API_KEY" ]]; then
        local RAND_AUTO=$(openssl rand -hex 16)
        update_env_var "AUTOMATION_API_KEY" "$RAND_AUTO"
        echo "✓ Generated new secure AUTOMATION_API_KEY (Transmission client RPC token)."
    else
        echo "✓ Preserved existing AUTOMATION_API_KEY."
    fi
    echo "--------------------------------------------------------"
    echo "✓ Configuration wizard finished! custom.env updated."
    echo "========================================================"
    echo ""

    # Reload variables from custom.env for the rest of setup.sh
    load_env "$ENV_FILE"
}

# Ensure environment file exists and run wizard
run_config_wizard


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
# INTERACTIVE GOOGLE OAUTH / SIGN-IN CONFIGURATION
# ------------------------------------------------------------------------------
PROMPT_OAUTH="n"
if [ -z "${GOOGLE_CLIENT_ID:-}" ] || [ -z "${GOOGLE_CLIENT_SECRET:-}" ]; then
    PROMPT_OAUTH="y"
else
    echo "✓ Found existing Google Sign-In / OAuth credentials in custom.env."
    read -p "Do you want to update Google Sign-In / OAuth credentials? (y/n) [n]: " CONFIRM_UPDATE
    CONFIRM_UPDATE=${CONFIRM_UPDATE:-n}
    if [[ "$CONFIRM_UPDATE" =~ ^[yY]$ ]]; then
        PROMPT_OAUTH="y"
    fi
fi

if [ "$PROMPT_OAUTH" = "y" ]; then
    echo "--------------------------------------------------------"
    echo "🔑 Google Sign-In / OAuth Credentials Configuration"
    echo "--------------------------------------------------------"
    echo "To log in to your Octor, you must configure Google Sign-In."
    echo "Please configure your OAuth 2.0 Client credentials:"
    echo "1. Go to: https://console.cloud.google.com/apis/credentials"
    echo "2. Create a new OAuth 2.0 Client ID (Web Application type)."
    echo "3. Under 'Authorized redirect URIs', add this exact URI:"
    echo "   👉 https://$DOMAIN/auth/callback/google"
    echo "--------------------------------------------------------"
    read -p "Enter Google OAuth Client ID: " INPUT_CLIENT_ID
    read -p "Enter Google OAuth Client Secret: " INPUT_CLIENT_SECRET

    if [ -n "$INPUT_CLIENT_ID" ] && [ -n "$INPUT_CLIENT_SECRET" ]; then
        # Escape potential special characters for sed
        ESC_CLIENT_ID=$(echo "$INPUT_CLIENT_ID" | sed 's/[&/\]/\\&/g')
        ESC_CLIENT_SECRET=$(echo "$INPUT_CLIENT_SECRET" | sed 's/[&/\]/\\&/g')
        
        # Replace the placeholders in custom.env
        run_sed_in_place "s|^GOOGLE_CLIENT_ID=.*|GOOGLE_CLIENT_ID=$ESC_CLIENT_ID|g" "$ENV_FILE"
        run_sed_in_place "s|^GOOGLE_CLIENT_SECRET=.*|GOOGLE_CLIENT_SECRET=$ESC_CLIENT_SECRET|g" "$ENV_FILE"
        
        # Reload/export the new variables
        export GOOGLE_CLIENT_ID="$INPUT_CLIENT_ID"
        export GOOGLE_CLIENT_SECRET="$INPUT_CLIENT_SECRET"
        echo "✓ Google OAuth credentials saved to custom.env successfully."
    else
        echo "⚠️  Credentials not entered. You must configure GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET in custom.env manually."
    fi
    echo "--------------------------------------------------------"
fi

# ------------------------------------------------------------------------------
# 3. STORAGE LAYER WIRING (Systemd configuration)
# ------------------------------------------------------------------------------
echo "--- Step 3: Storage Layer & Cache Systemd Setup ---"

# 1. Seeder cache service
echo "Configuring octor-seeder-cache.service..."
sed "s|__PROJECT_ROOT__|$PROJECT_ROOT|g" << 'EOF' > "$TEMP_DIR"/octor-seeder-cache.service
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
sudo cp "$TEMP_DIR"/octor-seeder-cache.service /etc/systemd/system/octor-seeder-cache.service

# 2. Rclone Google Drive union mount service
echo "Configuring octor-rclone-mount.service..."
RCLONE_BIN=$(which rclone 2>/dev/null || echo "/usr/bin/rclone")
sed -e "s|__PROJECT_ROOT__|$PROJECT_ROOT|g" -e "s|__ENV_FILE__|$ENV_FILE|g" -e "s|__RCLONE_BIN__|$RCLONE_BIN|g" << 'EOF' > "$TEMP_DIR"/octor-rclone-mount.service
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
ExecStart=__RCLONE_BIN__ --config ${VAULT_RCLONE_CONFIG} mount ${VAULT_RCLONE_REMOTE} __PROJECT_ROOT__/infra-data/drive-mount-vfs \
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
    --umask 000 --allow-non-empty \
    --rc \
    --rc-addr 127.0.0.1:5572 \
    --rc-no-auth \
    --rc-web-gui \
    --rc-web-gui-no-open-browser
ExecStop=/usr/bin/fusermount -uz __PROJECT_ROOT__/infra-data/drive-mount-vfs
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
sudo cp "$TEMP_DIR"/octor-rclone-mount.service /etc/systemd/system/octor-rclone-mount.service

# 3. Host trigger watcher service
echo "Configuring octor-host-watcher.service..."
sed "s|__PROJECT_ROOT__|$PROJECT_ROOT|g" << 'EOF' > "$TEMP_DIR"/octor-host-watcher.service
[Unit]
Description=Octor Host Recovery Trigger Watcher
After=network.target local-fs.target
StartLimitIntervalSec=0

[Service]
Type=simple
User=root
Group=root
ExecStart=/bin/bash __PROJECT_ROOT__/scripts/host-trigger-watcher.sh
WorkingDirectory=__PROJECT_ROOT__
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
sudo cp "$TEMP_DIR"/octor-host-watcher.service /etc/systemd/system/octor-host-watcher.service

# 4. Reload daemon and enable services
sudo systemctl daemon-reload
sudo systemctl enable octor-seeder-cache.service octor-rclone-mount.service octor-host-watcher.service

# 5. Start caching and mount layer
echo "Starting storage mount layers and host watcher..."
sudo systemctl restart octor-seeder-cache.service
sudo systemctl restart octor-rclone-mount.service
sudo systemctl restart octor-host-watcher.service

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

# Hand off the final launch to run.sh mode with sync, docker, build, nginx, and force options!
echo "Handing off to Octor Performance Mode Selector to configure performance mode..."
(cd "$PROJECT_ROOT" && ./run.sh mode --sync --docker --build --nginx --force)

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
    "$NGINX_TEMPLATE" > "$TEMP_DIR"/octor.nginx

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
    run_sed_in_place "s|/etc/letsencrypt/live/.*/fullchain.pem|/etc/ssl/certs/$DOMAIN.crt|g" "$TEMP_DIR"/octor.nginx
    run_sed_in_place "s|/etc/letsencrypt/live/.*/privkey.pem|/etc/ssl/private/$DOMAIN.key|g" "$TEMP_DIR"/octor.nginx
else
    # Correct path to domain cert
    run_sed_in_place "s|/etc/letsencrypt/live/.*/fullchain.pem|$REAL_CERT_DIR/fullchain.pem|g" "$TEMP_DIR"/octor.nginx
    run_sed_in_place "s|/etc/letsencrypt/live/.*/privkey.pem|$REAL_CERT_DIR/privkey.pem|g" "$TEMP_DIR"/octor.nginx
fi

sudo cp "$TEMP_DIR"/octor.nginx "$NGINX_CONF"
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
echo ""
echo "🔑 IMPORTANT: Google OAuth / SSO Setup"
echo "--------------------------------------------------------"
echo "To enable Google SSO login, you must add the following Authorized"
echo "Redirect URI to your OAuth 2.0 client credentials in the Google Cloud Console:"
echo "👉 https://$DOMAIN/auth/callback/google"
echo ""
echo "Create credentials at: https://console.cloud.google.com/apis/credentials"
echo "And ensure they are configured in your custom.env:"
echo "  GOOGLE_CLIENT_ID=<your-client-id>"
echo "  GOOGLE_CLIENT_SECRET=<your-client-secret>"
echo "======================================================================"


#!/bin/bash
set -e

# GAFAM VPC Installation Script
# Auto-install (Manager): curl -sSL …/deploy-vpc.sh | bash
# Tout le nécessaire (swap, Qwen) est auto-contenu ou tiré depuis GitHub raw.

echo "=========================================="
echo "🚀 GAFAM VPC Node Deployment"
echo "=========================================="

REPO_RAW="${REPO_RAW:-https://raw.githubusercontent.com/Garletz/gafam/main}"

# 1. Check for Docker
if ! command -v docker &> /dev/null; then
    echo "[!] Docker is not installed. Attempting to install Docker..."
    curl -fsSL https://get.docker.com -o get-docker.sh
    sh get-docker.sh
    rm get-docker.sh
    echo "[+] Docker installed successfully."
else
    echo "[+] Docker is already installed."
fi

# 2. Start the Docker daemon if it's not running
if ! docker info > /dev/null 2>&1; then
    echo "[!] Docker daemon is not running. Starting Docker..."
    sudo systemctl start docker || true
fi

# 3. Generate secrets if not set
if [ -z "$JWT_SECRET" ]; then
    export JWT_SECRET=$(openssl rand -hex 32)
    echo "[+] Generated random JWT_SECRET for this node."
fi

if [ -z "$WATCHTOWER_TOKEN" ]; then
    export WATCHTOWER_TOKEN=$(openssl rand -hex 16)
    echo "[+] Generated WATCHTOWER_TOKEN for manual updates from gafam.cloud."
fi

# Shared Docker network (gafam-api ↔ watchtower ↔ gafam-qwen)
docker network create gafam-net 2>/dev/null || true

# 3b. Swap 4 Go — INLINE (obligatoire pour curl|bash : pas de scripts/ locaux)
# Rêve 1 Go RAM + Qwen : filet OOM, idempotent, persisté dans fstab.
ensure_vpc_swap() {
    local SWAPFILE="${SWAPFILE:-/swapfile}"
    local SWAP_SIZE="${SWAP_SIZE:-4G}"

    if swapon --show 2>/dev/null | grep -q .; then
        echo "[+] Swap already active:"
        swapon --show
        return 0
    fi

    if [ ! -f "$SWAPFILE" ]; then
        echo "[*] Creating ${SWAP_SIZE} swap at ${SWAPFILE}..."
        if ! fallocate -l "$SWAP_SIZE" "$SWAPFILE" 2>/dev/null; then
            local mb=4096
            case "$SWAP_SIZE" in
                *G|*g) mb=$(( ${SWAP_SIZE%[Gg]} * 1024 )) ;;
                *M|*m) mb=${SWAP_SIZE%[Mm]} ;;
            esac
            dd if=/dev/zero of="$SWAPFILE" bs=1M count="$mb" status=progress
        fi
        chmod 600 "$SWAPFILE"
        mkswap "$SWAPFILE"
    fi

    swapon "$SWAPFILE" || true

    if ! grep -qE "^${SWAPFILE}[[:space:]]" /etc/fstab 2>/dev/null; then
        echo "$SWAPFILE none swap sw 0 0" >> /etc/fstab
        echo "[+] Swap persisted in /etc/fstab"
    fi

    mkdir -p /etc/sysctl.d
    cat > /etc/sysctl.d/99-gafam-swap.conf <<'SYSCTL'
vm.swappiness=40
vm.vfs_cache_pressure=50
SYSCTL
    sysctl -p /etc/sysctl.d/99-gafam-swap.conf >/dev/null 2>&1 || true

    echo "[+] Swap ready for 1 Go VPC + Qwen"
    swapon --show || true
    free -h || true
}

if [ "${SKIP_SWAP:-0}" != "1" ]; then
    echo "[*] Ensuring 4G swap (auto VPC config)..."
    ensure_vpc_swap || echo "[!] Swap setup failed (non-fatal)."
fi

# 4. Pull the pre-built GAFAM API image from GitHub Container Registry
echo "[*] Downloading GAFAM API..."
docker pull ghcr.io/garletz/gafam:latest

# 5. Watchtower first (HTTP API on host loopback — reliable vs Docker DNS)
echo "[*] Setting up Watchtower for automatic updates..."
docker rm -f watchtower 2>/dev/null || true
docker run -d \
  --name watchtower \
  --network gafam-net \
  --restart always \
  -p 127.0.0.1:8080:8080 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e WATCHTOWER_HTTP_API_TOKEN="${WATCHTOWER_TOKEN}" \
  nickfedor/watchtower:latest \
  --cleanup \
  --interval 300 \
  --http-api-update \
  --http-api-periodic-polls \
  --http-api-token "${WATCHTOWER_TOKEN}" \
  gafam-api

# 6. Deploy GAFAM API
echo "[*] Starting GAFAM VPC services..."
docker rm -f gafam-api 2>/dev/null || true

docker run -d \
  --name gafam-api \
  --network gafam-net \
  --add-host=host.docker.internal:host-gateway \
  --restart always \
  -p 5150:5150 \
  -p 5151:5151 \
  -v /root/vpc-relay:/app/certs \
  -v /root/gafam_data:/app/data \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e PORT="5150" \
  -e TLS_PORT="5151" \
  -e TLS_CERT="/app/certs/cert.pem" \
  -e TLS_KEY="/app/certs/key.pem" \
  -e JWT_SECRET="${JWT_SECRET}" \
  -e WATCHTOWER_TOKEN="${WATCHTOWER_TOKEN}" \
  -e WATCHTOWER_URL="http://watchtower:8080/v1/update" \
  -e QWEN_URL="http://gafam-qwen:8080" \
  -e QWEN_MODEL_PATH="/app/data/qwen/Qwen3-0.6B-Q4_K_M.gguf" \
  ghcr.io/garletz/gafam:latest

# 7. Sidecar Qwen — scripts depuis le clone local OU GitHub raw (curl|bash)
install_qwen_sidecar() {
    local work="/root/gafam-setup"
    mkdir -p "$work/vpc-relay/scripts"

    local SCRIPT_DIR
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd)" || SCRIPT_DIR=""

    if [ -n "$SCRIPT_DIR" ] && [ -f "$SCRIPT_DIR/vpc-relay/scripts/qwen-install.sh" ]; then
        echo "[*] Using local vpc-relay/scripts..."
        cp -f "$SCRIPT_DIR/vpc-relay/scripts/"*.sh "$work/vpc-relay/scripts/" 2>/dev/null || true
        cp -f "$SCRIPT_DIR/vpc-relay/docker-compose.qwen.yml" "$work/vpc-relay/" 2>/dev/null || true
    else
        echo "[*] Fetching Qwen install scripts from GitHub..."
        curl -fsSL "$REPO_RAW/vpc-relay/scripts/qwen-install.sh" -o "$work/vpc-relay/scripts/qwen-install.sh"
        curl -fsSL "$REPO_RAW/vpc-relay/scripts/qwen-ctl.sh" -o "$work/vpc-relay/scripts/qwen-ctl.sh"
        curl -fsSL "$REPO_RAW/vpc-relay/docker-compose.qwen.yml" -o "$work/vpc-relay/docker-compose.qwen.yml"
    fi

    chmod +x "$work/vpc-relay/scripts/"*.sh
    # qwen-install résout RELAY_DIR = parent de scripts/
    bash "$work/vpc-relay/scripts/qwen-install.sh"
}

if [ "${INSTALL_QWEN:-1}" = "1" ]; then
    echo "[*] Installing Qwen sidecar (stopped until analysis)..."
    install_qwen_sidecar || echo "[!] Qwen install skipped/failed (non-fatal)."
fi

# 8. Sidecar Browser — Vātāyana (remote Firefox via noVNC)
install_browser_sidecar() {
    local work="/root/gafam-setup"
    mkdir -p "$work/vpc-relay"

    local SCRIPT_DIR
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd)" || SCRIPT_DIR=""

    if [ -n "$SCRIPT_DIR" ] && [ -f "$SCRIPT_DIR/vpc-relay/Dockerfile.browser" ]; then
        echo "[*] Using local vpc-relay/ for browser build..."
        cp -f "$SCRIPT_DIR/vpc-relay/Dockerfile.browser" "$work/vpc-relay/" 2>/dev/null || true
        cp -rf "$SCRIPT_DIR/vpc-relay/firefox-profiles" "$work/vpc-relay/" 2>/dev/null || true
        cp -f "$SCRIPT_DIR/vpc-relay/entrypoint.sh" "$work/vpc-relay/" 2>/dev/null || true
    else
        echo "[*] Fetching browser Dockerfile from GitHub..."
        curl -fsSL "$REPO_RAW/vpc-relay/Dockerfile.browser" -o "$work/vpc-relay/Dockerfile.browser"
        curl -fsSL "$REPO_RAW/vpc-relay/entrypoint.sh" -o "$work/vpc-relay/entrypoint.sh"
        mkdir -p "$work/vpc-relay/firefox-profiles/profile_main" "$work/vpc-relay/firefox-profiles/profile_agent"
        curl -fsSL "$REPO_RAW/vpc-relay/firefox-profiles/profiles.ini" -o "$work/vpc-relay/firefox-profiles/profiles.ini"
        curl -fsSL "$REPO_RAW/vpc-relay/firefox-profiles/profile_main/user.js" -o "$work/vpc-relay/firefox-profiles/profile_main/user.js"
        curl -fsSL "$REPO_RAW/vpc-relay/firefox-profiles/profile_agent/user.js" -o "$work/vpc-relay/firefox-profiles/profile_agent/user.js"
    fi

    chmod +x "$work/vpc-relay/entrypoint.sh"

    echo "[*] Building gafam-browser image..."
    docker build -t gafam-browser -f "$work/vpc-relay/Dockerfile.browser" "$work/vpc-relay"

    echo "[*] Creating gafam-browser container (stopped)..."
    docker rm -f gafam-browser 2>/dev/null || true
    docker run -d \
      --name gafam-browser \
      --network gafam-net \
      --memory=600m \
      --memory-swap=2g \
      --tmpfs /tmp:size=128m \
      --tmpfs /dev/shm:size=128m \
      --restart no \
      -v /root/gafam_data/browser:/home/browser/data \
      gafam-browser
    docker stop gafam-browser
}

if [ "${INSTALL_BROWSER:-1}" = "1" ]; then
    echo "[*] Installing Browser sidecar (stopped until wake)..."
    install_browser_sidecar || echo "[!] Browser install skipped/failed (non-fatal)."
fi

echo ""
echo "=========================================="
echo "✅ GAFAM VPC successfully deployed!"
echo "=========================================="
echo "🌐 API is running on port 5150 (HTTPS, self-signed TLS)"
echo "🔑 Your JWT Secret (save this): $JWT_SECRET"
echo "🔄 Auto-updates: Watchtower polls GHCR every 5 minutes."
echo "🖱️  Manual update: Settings → VPS Node on gafam.cloud."
echo "🪶 Qwen: stopped by default (1 Go). Auto wake via Suparna API."
echo "🌐 Browser: stopped by default. Wake via Browser tab on gafam.cloud."
echo "   Swap 4G: enabled by this script (SKIP_SWAP=1 to skip)."
echo "=========================================="

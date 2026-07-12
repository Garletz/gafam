#!/bin/bash
set -e

# GAFAM VPC Installation Script
# Inspired by Outline Server deployment logic

echo "=========================================="
echo "🚀 GAFAM VPC Node Deployment"
echo "=========================================="

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

# Shared Docker network (gafam-api ↔ watchtower)
docker network create gafam-net 2>/dev/null || true

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
  -e PORT="5150" \
  -e TLS_PORT="5151" \
  -e TLS_CERT="/app/certs/cert.pem" \
  -e TLS_KEY="/app/certs/key.pem" \
  -e JWT_SECRET="${JWT_SECRET}" \
  -e WATCHTOWER_TOKEN="${WATCHTOWER_TOKEN}" \
  -e WATCHTOWER_URL="http://watchtower:8080/v1/update" \
  ghcr.io/garletz/gafam:latest

echo ""
echo "=========================================="
echo "✅ GAFAM VPC successfully deployed!"
echo "=========================================="
echo "🌐 API is running on port 5150 (HTTPS, self-signed TLS)"
echo "🔑 Your JWT Secret (save this): $JWT_SECRET"
echo "🔄 Auto-updates: Watchtower polls GHCR every 5 minutes."
echo "🖱️  Manual update: Settings → VPS Node on gafam.cloud."
echo "=========================================="

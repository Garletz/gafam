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

# 3. Generate a random JWT secret if not set
if [ -z "$JWT_SECRET" ]; then
    export JWT_SECRET=$(openssl rand -hex 32)
    echo "[+] Generated random JWT_SECRET for this node."
fi

# 4. Pull the pre-built GAFAM API image from GitHub Container Registry
echo "[*] Downloading GAFAM API..."
docker pull ghcr.io/garletz/gafam:latest

# 5. Build and Deploy
echo "[*] Starting GAFAM VPC services..."
# Stop and remove existing container if it exists
docker rm -f gafam-api 2>/dev/null || true

# Run the new container
# Port 5150: GAFAM HTTPS relay (self-signed TLS, TCP-Socket-compatible, no Cloudflare port restriction)
docker run -d \
  --name gafam-api \
  --restart always \
  -p 5150:5150 \
  -e PORT="5150" \
  -e JWT_SECRET="${JWT_SECRET}" \
  ghcr.io/garletz/gafam:latest

echo ""
echo "=========================================="
echo "✅ GAFAM VPC successfully deployed!"
echo "=========================================="
echo "🌐 API is running on port 5150 (HTTPS, self-signed TLS)"
echo "🔑 Your JWT Secret (save this): $JWT_SECRET"
echo "=========================================="

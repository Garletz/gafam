#!/bin/bash
set -e

echo "=========================================="
echo "  Vātāyana — Remote Browser Install"
echo "=========================================="

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RELAY_DIR="$(dirname "$SCRIPT_DIR")"

if ! command -v docker &> /dev/null; then
    echo "[!] Docker is required but not found."
    exit 1
fi

echo "[*] Building gafam-browser image..."
docker build -t gafam-browser -f "$RELAY_DIR/Dockerfile.browser" "$RELAY_DIR"

echo "[*] Removing old container (if any)..."
docker rm -f gafam-browser 2>/dev/null || true

echo "[*] Creating gafam-browser container (stopped)..."
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

echo "[*] Stopping container (will start on demand via API)..."
docker stop gafam-browser

echo ""
echo "=========================================="
echo "  Vātāyana installed (stopped)"
echo "  Wake via: POST /api/web/browser/wake"
echo "  Stop via: POST /api/web/browser/stop"
echo "=========================================="

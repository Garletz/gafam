#!/bin/bash
set -e

echo "=========================================="
echo "  Vātāyana — Remote Browser Install"
echo "=========================================="

BROWSER_IMAGE="${BROWSER_IMAGE:-ghcr.io/garletz/gafam-browser:latest}"

if ! command -v docker &> /dev/null; then
    echo "[!] Docker is required but not found."
    exit 1
fi

if ! docker network inspect gafam-net >/dev/null 2>&1; then
    echo "[!] Docker network gafam-net missing — run deploy-vpc.sh first."
    exit 1
fi

mkdir -p /root/gafam_data/browser

echo "[*] Pulling $BROWSER_IMAGE from GHCR..."
if ! docker pull "$BROWSER_IMAGE"; then
    echo "[!] GHCR pull failed — falling back to local Dockerfile.browser build"
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    RELAY_DIR="$(dirname "$SCRIPT_DIR")"
    docker build -t gafam-browser -f "$RELAY_DIR/Dockerfile.browser" "$RELAY_DIR"
    BROWSER_IMAGE="gafam-browser"
fi

echo "[*] Removing old container (if any)..."
docker rm -f gafam-browser 2>/dev/null || true

echo "[*] Creating gafam-browser container (stopped until Wake)..."
docker run -d \
  --name gafam-browser \
  --network gafam-net \
  --memory=600m \
  --memory-swap=2g \
  --tmpfs /tmp:size=128m \
  --tmpfs /dev/shm:size=128m \
  --restart no \
  -v /root/gafam_data/browser:/home/browser/data \
  "$BROWSER_IMAGE"

echo "[*] Stopping container (Watchtower can update stopped image; Wake starts it)..."
docker stop gafam-browser

echo ""
echo "=========================================="
echo "  Vātāyana installed (stopped)"
echo "  Image: $BROWSER_IMAGE"
echo "  Wake via: POST /api/web/browser/wake"
echo "  Stop via: POST /api/web/browser/stop"
echo "  Updates: Watchtower polls GHCR (~5 min)"
echo "=========================================="

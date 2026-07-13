#!/bin/bash
# Installe le sidecar Qwen dans le stack vpc-relay (VPS GAFAM).
# Usage (sur le droplet, en root):
#   bash vpc-relay/scripts/qwen-install.sh
set -euo pipefail

RELAY_DIR="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE_FILE="${RELAY_DIR}/docker-compose.qwen.yml"
MODEL_DIR="${MODEL_DIR:-/root/gafam_data/qwen}"
MODEL_FILE="${MODEL_DIR}/Qwen3-0.6B-Q4_K_M.gguf"
MODEL_URL="${MODEL_URL:-https://huggingface.co/unsloth/Qwen3-0.6B-GGUF/resolve/main/Qwen3-0.6B-Q4_K_M.gguf}"

echo "=========================================="
echo "🪶 vpc-relay · Qwen sidecar install"
echo "=========================================="

if ! command -v docker >/dev/null 2>&1; then
  echo "[!] Docker required. Run deploy-vpc.sh first."
  exit 1
fi

docker network create gafam-net 2>/dev/null || true
mkdir -p "$MODEL_DIR"

if [ ! -f "$MODEL_FILE" ]; then
  echo "[*] Downloading Qwen3-0.6B Q4_K_M (~380 Mo) → $MODEL_FILE"
  curl -fL --progress-bar -o "$MODEL_FILE.partial" "$MODEL_URL"
  mv "$MODEL_FILE.partial" "$MODEL_FILE"
else
  echo "[+] Model already present: $MODEL_FILE"
fi

echo "[*] Pulling llama.cpp server image..."
docker pull ghcr.io/ggml-org/llama.cpp:server

echo "[*] Creating container gafam-qwen (stopped — wake on demand)..."
docker rm -f gafam-qwen 2>/dev/null || true
docker compose -f "$COMPOSE_FILE" create

echo ""
echo "[+] Done. Container is STOPPED (1 Go VPS)."
echo "    Start:  bash vpc-relay/scripts/qwen-ctl.sh start"
echo "    Stop:   bash vpc-relay/scripts/qwen-ctl.sh stop"
echo "    From gafam-api (vpc-relay): http://gafam-qwen:8080"
echo "=========================================="

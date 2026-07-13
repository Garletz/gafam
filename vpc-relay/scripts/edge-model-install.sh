#!/bin/bash
# Télécharge le modèle ONNX Qwen3-0.6B sur le disque VPC (servi à l'APK).
# Usage (sur le droplet, en root):
#   bash vpc-relay/scripts/edge-model-install.sh
set -euo pipefail

MODEL_DIR="${MODEL_DIR:-/root/gafam_data/edge/qwen3-onnx}"
BASE_URL="${BASE_URL:-https://huggingface.co/onnx-community/Qwen3-0.6B-ONNX/resolve/main/onnxruntime/cpu_and_mobile/cpu-int4-kld-block-128}"
FILES=(
  chat_template.jinja
  config.json
  genai_config.json
  model.onnx
  tokenizer.json
  tokenizer_config.json
)

echo "=========================================="
echo "📱 GAFAM Edge ONNX model install (VPC disk)"
echo "=========================================="
mkdir -p "$MODEL_DIR"

for f in "${FILES[@]}"; do
  dest="$MODEL_DIR/$f"
  if [ -f "$dest" ] && [ -s "$dest" ]; then
    echo "[+] Already present: $f"
    continue
  fi
  echo "[*] Downloading $f → $dest"
  curl -fL --progress-bar -o "$dest.partial" "$BASE_URL/$f"
  mv "$dest.partial" "$dest"
done

echo ""
echo "[+] Edge ONNX model ready in $MODEL_DIR"
echo "    APK will download from GET /api/auth/edge/model/{file}"
echo "    Run edge-model-install once; files persist in gafam_data volume."

#!/bin/bash
# Download / build Suparna model (Qwen3-0.6B int4 ONNX) on the VPS.
# Run on the droplet: bash scripts/download-suparna-model.sh

set -e
MODEL_DIR="${SUPARNA_MODEL_DIR:-/root/gafam_data/suparna/model}"
mkdir -p "$MODEL_DIR"

echo "[*] Suparna — building Qwen3-0.6B ONNX (int4, CPU) into $MODEL_DIR"
echo "[*] This may take 10–20 minutes and ~600MB disk…"

docker exec gafam-api python3 -m onnxruntime_genai.models.builder \
  -m Qwen/Qwen3-0.6B \
  -o /app/data/suparna/model \
  -e cpu \
  -p int4

echo "[+] Suparna model ready. Invoke from Logs → Invoquer Suparna on gafam.cloud"

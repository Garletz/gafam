#!/bin/bash
set -euo pipefail
DIR="$(cd "$(dirname "$0")/.." && pwd)/app/libs"
mkdir -p "$DIR"
curl -fsSL -o "$DIR/onnxruntime-genai-android-0.8.1.aar" \
  "https://github.com/microsoft/onnxruntime-genai/releases/download/v0.8.1/onnxruntime-genai-android-0.8.1.aar"
echo "OK: $DIR/onnxruntime-genai-android-0.8.1.aar"

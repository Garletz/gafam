#!/bin/bash
set -euo pipefail
DIR="$(cd "$(dirname "$0")/.." && pwd)/app/libs"
mkdir -p "$DIR"
curl -fsSL -o "$DIR/onnxruntime-genai-android-0.14.0.aar" \
  "https://github.com/microsoft/onnxruntime-genai/releases/download/v0.14.0/onnxruntime-genai-android-0.14.0.aar"
echo "OK: $DIR/onnxruntime-genai-android-0.14.0.aar"

#!/bin/bash
set -e

DISPLAY_NUM=99
SCREEN_RES="1024x576x24"

export BROWSER_WIDTH=1024
export BROWSER_HEIGHT=576
export BROWSER_FPS=8
export BROWSER_JPEG_Q=12
export STREAM_PORT=6080

echo "[chromium] Starting Xvfb on :${DISPLAY_NUM}..."
Xvfb :${DISPLAY_NUM} -screen 0 ${SCREEN_RES} -ac +extension GLX +render -noreset &
sleep 1

export DISPLAY=:${DISPLAY_NUM}

CHROMIUM_FLAGS="
  --headless
  --disable-gpu
  --no-sandbox
  --disable-dev-shm-usage
  --single-process
  --disable-background-networking
  --disable-sync
  --no-first-run
  --user-data-dir=/home/browser/chromium-data
  --remote-debugging-port=9222
  --remote-debugging-address=0.0.0.0
  --window-size=${BROWSER_WIDTH},${BROWSER_HEIGHT}
  about:blank
"

echo "[chromium] Starting Chromium..."
chromium $CHROMIUM_FLAGS &
CHROMIUM_PID=$!
sleep 2

echo "[chromium] Starting stream server (JPEG over HTTP on port ${STREAM_PORT})..."
python3 /stream.py &
STREAM_PID=$!

echo "[chromium] Ready. Stream: http://localhost:${STREAM_PORT}/stream  CDP: http://localhost:9222"

wait ${STREAM_PID}

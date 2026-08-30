#!/bin/bash
set -e

DISPLAY_NUM=99
# Lighter defaults for 1 Go VPS — still readable, much less CPU/bandwidth.
SCREEN_RES="1024x576x24"

export BROWSER_WIDTH=1024
export BROWSER_HEIGHT=576
export BROWSER_FPS=10
export BROWSER_JPEG_Q=10
export STREAM_PORT=6080

echo "[vatayana] Starting Xvfb on :${DISPLAY_NUM}..."
Xvfb :${DISPLAY_NUM} -screen 0 ${SCREEN_RES} -ac +extension GLX +render -noreset &
sleep 1

export DISPLAY=:${DISPLAY_NUM}

echo "[vatayana] Starting openbox window manager..."
openbox &
sleep 0.5

echo "[vatayana] Starting PulseAudio..."
pulseaudio --start --exit-idle-time=-1 --daemonize=yes 2>/dev/null || true
sleep 0.5

PROFILE_DIR="/home/browser/data/profile"
mkdir -p "${PROFILE_DIR}"

# CDP binds 0.0.0.0 INSIDE the container only: reachable on gafam-net, never
# published to the host. Do NOT add --enable-automation: it would set
# navigator.webdriver=true and get the session flagged as a bot.
echo "[vatayana] Starting Chrome for Testing (persistent profile, CDP on 9222)..."
/opt/chrome/chrome \
  --user-data-dir="${PROFILE_DIR}" \
  --remote-debugging-port=9222 \
  --remote-debugging-address=0.0.0.0 \
  --no-sandbox \
  --no-first-run \
  --no-default-browser-check \
  --disable-background-networking \
  --disable-component-update \
  --disable-sync \
  --metrics-recording-only \
  --disable-domain-reliability \
  --disable-dev-shm-usage \
  --password-store=basic \
  --disable-features=OptimizationHints,Translate,MediaRouter \
  --window-size=${BROWSER_WIDTH},${BROWSER_HEIGHT} \
  "about:blank" &
CHROME_PID=$!
sleep 3

echo "[vatayana] Starting stream server (JPEG over HTTP on port ${STREAM_PORT})..."
python3 /stream.py &
STREAM_PID=$!

echo "[vatayana] Ready. Chrome on :${DISPLAY_NUM}, CDP 9222, stream ${STREAM_PORT}"

wait ${STREAM_PID}

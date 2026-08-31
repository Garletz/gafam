#!/bin/bash
set -e

DISPLAY_NUM=99
# Virtual screen is the max Chrome can grow into. The MJPEG stream captures
# the Chrome window only; the front resizes that window to the dashboard frame.
SCREEN_RES="1920x1080x24"

export BROWSER_WIDTH=1920
export BROWSER_HEIGHT=1080
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
  --hide-crash-restore-bubble \
  --window-position=0,0 \
  --window-size=1280,720 \
  "about:blank" &
CHROME_PID=$!
sleep 3

# Chrome 136+ binds the DevTools port to 127.0.0.1 ONLY (hardened) and rejects
# HTTP requests whose Host header is not an IP address (DNS-rebinding guard).
# Bridge loopback:9222 onto the container's 0.0.0.0:9223 so the MCP sidecar
# can attach over gafam-net using the container's IP as host. Never published
# to the host — internal docker network only.
echo "[vatayana] Bridging CDP (127.0.0.1:9222 → 0.0.0.0:9223)..."
socat TCP-LISTEN:9223,bind=0.0.0.0,fork,reuseaddr TCP:127.0.0.1:9222 &
SOCAT_PID=$!

echo "[vatayana] Starting stream server (JPEG over HTTP on port ${STREAM_PORT})..."
python3 /stream.py &
STREAM_PID=$!

echo "[vatayana] Ready. Chrome on :${DISPLAY_NUM}, CDP 9222, stream ${STREAM_PORT}"

wait ${STREAM_PID}

#!/bin/bash
set -e

DISPLAY_NUM=99
SCREEN_RES="1280x720x24"

export BROWSER_WIDTH=1280
export BROWSER_HEIGHT=720
export BROWSER_FPS=12
export STREAM_PORT=6080

echo "[vatayana] Starting Xvfb on :${DISPLAY_NUM}..."
Xvfb :${DISPLAY_NUM} -screen 0 ${SCREEN_RES} -ac +extension GLX +render -noreset &
XVFB_PID=$!
sleep 1

export DISPLAY=:${DISPLAY_NUM}

echo "[vatayana] Starting openbox window manager..."
openbox &
sleep 0.5

echo "[vatayana] Starting PulseAudio..."
pulseaudio --start --exit-idle-time=-1 --daemonize=yes 2>/dev/null || true
sleep 0.5

echo "[vatayana] Starting Firefox ESR (main profile)..."
firefox-esr -P main --no-remote --new-instance "about:blank" &
sleep 2

echo "[vatayana] Starting stream server (JPEG over HTTP on port ${STREAM_PORT})..."
python3 /stream.py &
STREAM_PID=$!

echo "[vatayana] Ready. Stream at http://localhost:${STREAM_PORT}/stream"

wait ${STREAM_PID}

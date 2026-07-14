#!/bin/bash
set -e

DISPLAY_NUM=99
SCREEN_RES="1920x1080x24"

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

echo "[vatayana] Starting x11vnc on display :${DISPLAY_NUM}..."
x11vnc -display :${DISPLAY_NUM} -nopw -listen 0.0.0.0 -xkb -ncache 10 -ncache_cr -forever -shared -rfbport 5900 &
sleep 1

echo "[vatayana] Starting websockify + noVNC on port 6080..."
websockify --web=/usr/share/novnc 6080 localhost:5900 &
WEBSOCKIFY_PID=$!

echo "[vatayana] Ready. noVNC available at http://localhost:6080/vnc.html?autoconnect=true"

wait ${WEBSOCKIFY_PID}

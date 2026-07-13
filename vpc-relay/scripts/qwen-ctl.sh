#!/bin/bash
# Wake/sleep du sidecar Qwen (vpc-relay). Jamais en parallèle avec scrcpy.
# Usage: bash vpc-relay/scripts/qwen-ctl.sh start|stop|status
set -euo pipefail

CMD="${1:-status}"

case "$CMD" in
  start)
    echo "[*] Starting gafam-qwen..."
    docker start gafam-qwen
    for i in $(seq 1 60); do
      if docker logs gafam-qwen 2>&1 | tail -30 | grep -qiE 'listening|model loaded'; then
        echo "[+] Qwen ready → http://gafam-qwen:8080 (env QWEN_URL)"
        exit 0
      fi
      sleep 2
    done
    echo "[!] Started but not ready yet — docker logs -f gafam-qwen"
    ;;
  stop)
    echo "[*] Stopping gafam-qwen (free ~400–500 Mo)..."
    docker stop gafam-qwen 2>/dev/null || true
    echo "[+] Stopped"
    ;;
  status)
    if docker ps --format '{{.Names}}' | grep -qx gafam-qwen; then
      echo "status: running"
      docker stats gafam-qwen --no-stream 2>/dev/null || true
    elif docker ps -a --format '{{.Names}}' | grep -qx gafam-qwen; then
      echo "status: stopped"
    else
      echo "status: missing (run vpc-relay/scripts/qwen-install.sh)"
      exit 1
    fi
    free -h 2>/dev/null || true
    ;;
  *)
    echo "Usage: $0 {start|stop|status}"
    exit 1
    ;;
esac

#!/bin/bash
# Swap 4 Go pour VPS 1 Go (filet OOM). Idempotent.
# Usage: bash vpc-relay/scripts/setup-vpc-swap.sh
set -euo pipefail

SWAPFILE="${SWAPFILE:-/swapfile}"
SWAP_SIZE="${SWAP_SIZE:-4G}"

if swapon --show | grep -q .; then
  echo "[+] Swap already active:"
  swapon --show
  free -h
  exit 0
fi

if [ ! -f "$SWAPFILE" ]; then
  echo "[*] Creating $SWAP_SIZE swap at $SWAPFILE ..."
  fallocate -l "$SWAP_SIZE" "$SWAPFILE" || dd if=/dev/zero of="$SWAPFILE" bs=1M count=4096 status=progress
  chmod 600 "$SWAPFILE"
  mkswap "$SWAPFILE"
fi

swapon "$SWAPFILE"

if ! grep -qE "^$SWAPFILE" /etc/fstab 2>/dev/null; then
  echo "$SWAPFILE none swap sw 0 0" >> /etc/fstab
  echo "[+] Persisted in /etc/fstab"
fi

mkdir -p /etc/sysctl.d
cat > /etc/sysctl.d/99-gafam-swap.conf <<'EOF'
vm.swappiness=40
vm.vfs_cache_pressure=50
EOF
sysctl -p /etc/sysctl.d/99-gafam-swap.conf >/dev/null

echo "[+] Swap ready"
swapon --show
free -h

#!/usr/bin/env bash
# One-shot seed: Protomaps world basemap (z0–10 ≈ 3.7 GiB) onto VPC volume.
# No CDN at runtime — file is stored locally under /app/data/geo/basemap.pmtiles
set -euo pipefail
OUT="${1:-/app/data/geo/basemap.pmtiles}"
SRC="${PMTILES_SOURCE:-https://data.source.coop/protomaps/openstreetmap/v4.pmtiles}"
MAXZOOM="${PMTILES_MAXZOOM:-10}"
mkdir -p "$(dirname "$OUT")"
if [[ -f "$OUT" && $(stat -f%z "$OUT" 2>/dev/null || stat -c%s "$OUT") -gt 1000000000 ]]; then
  echo "already present: $OUT ($(du -h "$OUT" | awk '{print $1}'))"
  exit 0
fi
command -v pmtiles >/dev/null || {
  echo "install go-pmtiles first: https://github.com/protomaps/go-pmtiles/releases"
  exit 1
}
echo "extracting $SRC → $OUT (maxzoom=$MAXZOOM)…"
pmtiles extract "$SRC" "$OUT" --maxzoom="$MAXZOOM" --download-threads=8
ls -lh "$OUT"
echo "done"

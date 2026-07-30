#!/usr/bin/env bash
# Upload geo-data/pmtiles-release/* to GitHub Release tag (default geo-basemap-v1).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="$ROOT/geo-data/pmtiles-release"
TAG="${PMTILES_RELEASE_TAG:-geo-basemap-v1}"
REPO="${PMTILES_GITHUB_REPO:-Garletz/gafam}"

if [[ ! -d "$OUT_DIR" ]] || ! ls "$OUT_DIR"/part-*.bin >/dev/null 2>&1; then
  echo "run scripts/split_pmtiles.sh first (missing $OUT_DIR/part-*.bin)" >&2
  exit 1
fi
command -v gh >/dev/null || { echo "gh CLI required" >&2; exit 1; }

NOTES="Protomaps OSM basemap (PMTiles, maxzoom 10). Shards <=95 MiB for GitHub Release limits.
VPC assembles to /app/data/geo/basemap.pmtiles via POST /api/web/geo/pmtiles/sync."

if gh release view "$TAG" --repo "$REPO" >/dev/null 2>&1; then
  echo "release ${TAG} exists - uploading/replacing assets..."
else
  echo "creating release ${TAG}..."
  gh release create "$TAG" --repo "$REPO" --title "$TAG" --notes "$NOTES"
fi

# Upload in batches (gh can choke on huge argv); clobber for re-publish
gh release upload "$TAG" "$OUT_DIR/manifest.json" --repo "$REPO" --clobber
for f in $(ls "$OUT_DIR"/part-*.bin | sort); do
  echo "upload $(basename "$f")..."
  gh release upload "$TAG" "$f" --repo "$REPO" --clobber
done
echo "published https://github.com/${REPO}/releases/tag/${TAG}"

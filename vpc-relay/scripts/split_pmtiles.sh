#!/usr/bin/env bash
# Split basemap.pmtiles into ~95 MiB shards for a GitHub Release (≤100 MiB/file).
# Writes: geo-data/pmtiles-release/part-XX.bin + geo-data/pmtiles-manifest.json
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="${1:-$ROOT/geo-data/basemap.pmtiles}"
OUT_DIR="$ROOT/geo-data/pmtiles-release"
MANIFEST="$ROOT/geo-data/pmtiles-manifest.json"
TAG="${PMTILES_RELEASE_TAG:-geo-basemap-v1}"
REPO="${PMTILES_GITHUB_REPO:-Garletz/gafam}"
PART_SIZE=$((95 * 1024 * 1024))

if [[ ! -f "$SRC" ]]; then
  echo "missing source: $SRC" >&2
  exit 1
fi

python3 - "$SRC" "$OUT_DIR" "$MANIFEST" "$TAG" "$REPO" "$PART_SIZE" <<'PY'
import hashlib, json, os, sys

src, out_dir, manifest, tag, repo, part_size = sys.argv[1:7]
part_size = int(part_size)

os.makedirs(out_dir, exist_ok=True)
for name in os.listdir(out_dir):
    if name.startswith("part-") or name == "manifest.json":
        os.remove(os.path.join(out_dir, name))

h = hashlib.sha256()
parts = []
idx = 0
total = 0
with open(src, "rb") as f:
    while True:
        chunk = f.read(part_size)
        if not chunk:
            break
        name = f"part-{idx:02d}.bin"
        path = os.path.join(out_dir, name)
        with open(path, "wb") as out:
            out.write(chunk)
        h.update(chunk)
        ph = hashlib.sha256(chunk).hexdigest()
        parts.append({"name": name, "bytes": len(chunk), "sha256": ph})
        total += len(chunk)
        print(f"  {name} ({len(chunk)} bytes)")
        idx += 1

doc = {
    "version": tag,
    "tag": tag,
    "base_url": f"https://github.com/{repo}/releases/download/{tag}",
    "filename": "basemap.pmtiles",
    "bytes": total,
    "sha256": h.hexdigest(),
    "part_size": part_size,
    "parts": parts,
}
text = json.dumps(doc, indent=2) + "\n"
with open(manifest, "w") as f:
    f.write(text)
with open(os.path.join(out_dir, "manifest.json"), "w") as f:
    f.write(text)
print(f"wrote {manifest} ({len(parts)} parts, {total} bytes, sha256={doc['sha256']})")
PY

echo "next: bash scripts/publish_pmtiles_release.sh"

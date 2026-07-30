# Protomaps basemap (self-hosted, no CDN)

Runtime tiles: **`/app/data/geo/basemap.pmtiles`** on the VPC volume.

## Distribution (GitHub Release)

Shards live on release tag **`geo-basemap-v1`** (not in git, not in Docker):

- Assets: `manifest.json` + `part-00.bin` … `part-NN.bin` (~95 MiB each)
- Repo ships only [`pmtiles-manifest.json`](pmtiles-manifest.json) (small pointer)
- VPC downloads + assembles on boot (if missing) or via **Settings → Sync basemap**

### Maintainer: publish once

```bash
# extract world z0–10 locally first (see seed_pmtiles.sh)
bash scripts/split_pmtiles.sh geo-data/basemap.pmtiles
bash scripts/publish_pmtiles_release.sh
# commit geo-data/pmtiles-manifest.json, push — Watchtower picks up sync code
```

### Auto-deployers

After Watchtower updates `gafam-api`, if `basemap.pmtiles` is absent the node pulls shards from:

`https://github.com/Garletz/gafam/releases/download/geo-basemap-v1/part-XX.bin`

API:

- `GET /api/web/geo/pmtiles/status` — ready / syncing / progress
- `POST /api/web/geo/pmtiles/sync` — start sync (idempotent)
- `GET|HEAD /api/web/geo/pmtiles` — Range serving for MapLibre

## Notes

- Format: PMTiles (Protomaps / OpenStreetMap), target ~3.4–3.7 GiB at `maxzoom=10`
- Soft geo quota: 5 GiB
- Places / lat-lon: GeoNames SQLite (unchanged)
- Do **not** commit `basemap.pmtiles` or `pmtiles-release/` (gitignored)

# Protomaps basemap (self-hosted, no CDN)

Runtime tiles: **`/app/data/geo/basemap.pmtiles`** on the VPC volume.

- Format: PMTiles (Protomaps / OpenStreetMap)
- Target size: ~3.7 GiB at `maxzoom=10` (fits 5 GiB soft quota)
- Source (one-time extract only): `https://data.source.coop/protomaps/openstreetmap/v4.pmtiles`
- Seed: `bash scripts/seed_pmtiles.sh /app/data/geo/basemap.pmtiles`
- API: `GET/HEAD /api/web/geo/pmtiles` with HTTP **Range**
- Front: MapLibre GL + `pmtiles` protocol via Wrangler proxy (`action=pmtiles`)
- Places / lat-lon search: existing GeoNames SQLite (unchanged)

Do **not** commit `basemap.pmtiles` (gitignored).

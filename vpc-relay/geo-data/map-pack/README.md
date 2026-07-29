# Map pack (VPC)

Vendored offline basemap + vector overlays. On boot, `initGeoMapPack()` copies this
directory into `/app/data/geo/map` (persistent volume). Soft quota for all of
`/app/data/geo` (pack + OSM tile cache): **1 GiB**.

| File | Role |
|------|------|
| `basemap.jpg` | NASA Blue Marble / topo-bathy ~12k×6k equirectangular |
| `rivers.geojson.gz` | Natural Earth 10m major rivers |
| `roads.geojson.gz` | Major roads worldwide + denser Western Europe |
| `cities.geojson.gz` | City points / labels |
| `manifest.json` | Version metadata |

APIs: `GET /api/web/geo/basemap`, `/layers`, `/layers/{rivers|roads|cities}`.

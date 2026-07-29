# Map pack v2

Soft quota for `/app/data/geo` (pack + OSM tile cache): **2 GiB**.

| Asset | Role |
|-------|------|
| `basemap.jpg` | NASA Blue Marble ~12k equirectangular |
| `roads.geojson.gz` | Natural Earth 10m **densified** world arteries |
| `rivers.geojson.gz` | Natural Earth rivers densified |
| `cities.geojson.gz` | City labels |
| `streets/*.geojson.gz` | OSM important streets for FR metros (load at zoom ≥ 8) |

Rebuild: `python3 scripts/build_map_pack_v2.py`

APIs: `/basemap`, `/layers`, `/layers/{rivers\|roads\|cities}`, `/streets/{city\|index}`

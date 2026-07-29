#!/usr/bin/env python3
"""Rebuild vpc-relay/geo-data/map-pack (basemap HD + NE overlays).

Requires: Pillow, network for Natural Earth + NASA source (or local /tmp/bm2160.jpg).
"""
from __future__ import annotations

import gzip
import json
import time
import urllib.request
from pathlib import Path

from PIL import Image

Image.MAX_IMAGE_PIXELS = None

ROOT = Path(__file__).resolve().parents[1]
PACK = ROOT / "geo-data" / "map-pack"
FRONT = ROOT.parent / "frontend" / "static" / "geo"
BUILD = Path("/tmp/map_pack_build")
NASA = Path("/tmp/bm2160.jpg")
NASA_URL = "https://eoimages.gsfc.nasa.gov/images/imagerecords/73000/73909/world.topo.bathy.200412.3x21600x10800.jpg"
WE = (-12.0, 35.0, 20.0, 60.0)


def dl(url: str, dest: Path) -> None:
    print("GET", url)
    urllib.request.urlretrieve(url, dest)


def simplify_ring(coords, step=2):
    if not coords or len(coords) < 4:
        return coords
    out = coords[:: max(1, step)]
    if out[-1] != coords[-1]:
        out.append(coords[-1])
    return out


def simplify_geom(geom, step=2):
    t = geom.get("type")
    c = geom.get("coordinates")
    if t == "LineString":
        return {"type": t, "coordinates": simplify_ring(c, step)}
    if t == "MultiLineString":
        return {"type": t, "coordinates": [simplify_ring(line, step) for line in c]}
    return geom


def round_coords(obj, nd=5):
    if isinstance(obj, list):
        if obj and isinstance(obj[0], (int, float)):
            return [round(float(x), nd) for x in obj]
        return [round_coords(x, nd) for x in obj]
    return obj


def write_gz(path: Path, fc: dict) -> None:
    raw = json.dumps(fc, ensure_ascii=False, separators=(",", ":")).encode()
    with gzip.open(path, "wb", compresslevel=9) as g:
        g.write(raw)
    print(f"wrote {path.name} raw={len(raw)/1e6:.2f}MB gz={path.stat().st_size/1e6:.2f}MB n={len(fc['features'])}")


def main() -> None:
    BUILD.mkdir(parents=True, exist_ok=True)
    PACK.mkdir(parents=True, exist_ok=True)
    FRONT.mkdir(parents=True, exist_ok=True)

    base = "https://raw.githubusercontent.com/nvkelso/natural-earth-vector/master/geojson"
    files = {
        "rivers.geojson": f"{base}/ne_10m_rivers_lake_centerlines.geojson",
        "roads.geojson": f"{base}/ne_10m_roads.geojson",
        "places.geojson": f"{base}/ne_10m_populated_places.geojson",
    }
    for name, url in files.items():
        dest = BUILD / name
        if not dest.exists() or dest.stat().st_size < 1000:
            dl(url, dest)

    if not NASA.exists():
        dl(NASA_URL, NASA)

    rivers = json.loads((BUILD / "rivers.geojson").read_text())
    roads = json.loads((BUILD / "roads.geojson").read_text())
    places = json.loads((BUILD / "places.geojson").read_text())

    riv_out = []
    for ft in rivers["features"]:
        p = ft.get("properties") or {}
        try:
            sr = int(p.get("scalerank", 99) or 99)
        except Exception:
            sr = 99
        name = p.get("name") or p.get("name_en") or ""
        if sr > 5:
            continue
        geom = simplify_geom(ft.get("geometry"), step=2 if sr <= 2 else 3)
        if not geom:
            continue
        geom["coordinates"] = round_coords(geom["coordinates"])
        riv_out.append({"type": "Feature", "properties": {"name": name, "sr": sr}, "geometry": geom})
    write_gz(PACK / "rivers.geojson.gz", {"type": "FeatureCollection", "features": riv_out})

    plc_out = []
    for ft in places["features"]:
        p = ft.get("properties") or {}
        geom = ft.get("geometry") or {}
        if geom.get("type") != "Point":
            continue
        lon, lat = float(geom["coordinates"][0]), float(geom["coordinates"][1])
        try:
            pop = int(p.get("POP_MAX") or 0)
        except Exception:
            pop = 0
        try:
            sr = int(p.get("SCALERANK") or 99)
        except Exception:
            sr = 99
        name = p.get("NAME") or p.get("NAMEASCII") or ""
        if not name:
            continue
        in_we = WE[0] <= lon <= WE[2] and WE[1] <= lat <= WE[3]
        keep = sr <= 3 or pop >= 100000 or (in_we and (pop >= 25000 or sr <= 6)) or pop >= 250000
        if not keep:
            continue
        plc_out.append(
            {
                "type": "Feature",
                "properties": {"name": name, "pop": pop, "sr": sr},
                "geometry": {"type": "Point", "coordinates": [round(lon, 5), round(lat, 5)]},
            }
        )
    plc_out.sort(key=lambda f: (-(f["properties"]["pop"] or 0), f["properties"]["sr"]))
    write_gz(PACK / "cities.geojson.gz", {"type": "FeatureCollection", "features": plc_out[:4500]})

    road_out = []
    for ft in roads["features"]:
        p = ft.get("properties") or {}
        try:
            sr = int(p.get("scalerank", 99) or 99)
        except Exception:
            sr = 99
        rtype = (p.get("type") or "").lower()
        name = p.get("name") or ""
        geom = ft.get("geometry")
        if not geom:
            continue
        coords = geom.get("coordinates")
        lon = lat = None
        try:
            if geom["type"] == "LineString":
                lon, lat = coords[0][0], coords[0][1]
            elif geom["type"] == "MultiLineString":
                lon, lat = coords[0][0][0], coords[0][0][1]
        except Exception:
            pass
        in_we = lon is not None and WE[0] <= lon <= WE[2] and WE[1] <= lat <= WE[3]
        keep = sr <= 4 or (in_we and sr <= 8) or ("motorway" in rtype or "trunk" in rtype)
        if not keep:
            continue
        step = 1 if (in_we and sr >= 5) else (2 if in_we or sr <= 2 else 4)
        g2 = simplify_geom(geom, step=step)
        g2["coordinates"] = round_coords(g2["coordinates"], 5)
        road_out.append(
            {"type": "Feature", "properties": {"name": name, "sr": sr, "t": rtype[:28]}, "geometry": g2}
        )
    write_gz(PACK / "roads.geojson.gz", {"type": "FeatureCollection", "features": road_out})

    img = Image.open(NASA)
    img.load()
    hi = img.resize((12288, 6144), Image.Resampling.LANCZOS).convert("RGB")
    hi.save(PACK / "basemap.jpg", "JPEG", quality=80, optimize=True, progressive=True)
    mid = img.resize((10240, 5120), Image.Resampling.LANCZOS).convert("RGB")
    mid.save(FRONT / "world.jpg", "JPEG", quality=80, optimize=True, progressive=True)

    manifest = {
        "version": "map-pack-v1",
        "created": time.strftime("%Y-%m-%d"),
        "quota_bytes": 1_000_000_000,
        "layers": [
            {"id": "basemap", "file": "basemap.jpg", "type": "raster", "crs": "EPSG:4326", "bounds": [-180, -90, 180, 90]},
            {"id": "rivers", "file": "rivers.geojson.gz", "type": "geojson"},
            {"id": "roads", "file": "roads.geojson.gz", "type": "geojson"},
            {"id": "cities", "file": "cities.geojson.gz", "type": "geojson"},
        ],
        "note": "NASA Blue Marble + Natural Earth 10m. Tile cache uses remaining 1GiB geo quota.",
    }
    (PACK / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print("pack MB", sum(p.stat().st_size for p in PACK.iterdir()) / 1e6)


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Build map-pack-v2: denser Natural Earth + OSM important streets for FR metros.

Outputs under geo-data/map-pack/ and copies overlays to frontend/static/geo/.
"""
from __future__ import annotations

import gzip
import json
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PACK = ROOT / "geo-data" / "map-pack"
STREETS = PACK / "streets"
FRONT = ROOT.parent / "frontend" / "static" / "geo"
BUILD = Path("/tmp/map_pack_build")
OVERPASS = "https://overpass-api.de/api/interpreter"

# Important streets (no footways/paths/service)
HW = "motorway|trunk|primary|secondary|tertiary|residential|unclassified"

CITIES = {
    "paris": (48.815, 2.225, 48.902, 2.470),
    "lyon": (45.70, 4.77, 45.81, 4.92),
    "marseille": (43.23, 5.30, 43.38, 5.48),
    "toulouse": (43.54, 1.36, 43.67, 1.52),
    "bordeaux": (44.79, -0.68, 44.90, -0.50),
    "lille": (50.59, 2.98, 50.69, 3.15),
    "nantes": (47.18, -1.63, 47.28, -1.48),
    "nice": (43.65, 7.20, 43.75, 7.32),
    "strasbourg": (48.54, 7.68, 48.63, 7.82),
    "montpellier": (43.57, 3.80, 43.66, 3.95),
}


def simplify_ring(coords, step=2):
    if not coords or len(coords) < 4:
        return coords
    out = coords[:: max(1, step)]
    if out[-1] != coords[-1]:
        out.append(coords[-1])
    return out


def round_coords(obj, nd=5):
    if isinstance(obj, list):
        if obj and isinstance(obj[0], (int, float)):
            return [round(float(x), nd) for x in obj]
        return [round_coords(x, nd) for x in obj]
    return obj


def write_gz(path: Path, fc: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    raw = json.dumps(fc, ensure_ascii=False, separators=(",", ":")).encode()
    with gzip.open(path, "wb", compresslevel=9) as g:
        g.write(raw)
    print(f"  {path.name}: feats={len(fc['features'])} raw={len(raw)/1e6:.2f}MB gz={path.stat().st_size/1e6:.2f}MB")


def densify_ne() -> None:
    print("== densify Natural Earth ==")
    roads = json.loads((BUILD / "roads.geojson").read_text())
    rivers = json.loads((BUILD / "rivers.geojson").read_text())
    places = json.loads((BUILD / "places.geojson").read_text())

    # Roads: keep almost all (scalerank <= 10), lighter simplify outside Europe
    WE = (-12.0, 35.0, 25.0, 62.0)
    road_out = []
    for ft in roads["features"]:
        p = ft.get("properties") or {}
        try:
            sr = int(p.get("scalerank", 99) or 99)
        except Exception:
            sr = 99
        if sr > 10:
            continue
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
        step = 1 if in_we else (2 if sr <= 5 else 3)
        if geom["type"] == "LineString":
            g2 = {"type": "LineString", "coordinates": simplify_ring(coords, step)}
        elif geom["type"] == "MultiLineString":
            g2 = {"type": "MultiLineString", "coordinates": [simplify_ring(l, step) for l in coords]}
        else:
            continue
        g2["coordinates"] = round_coords(g2["coordinates"], 5)
        road_out.append(
            {
                "type": "Feature",
                "properties": {
                    "name": p.get("name") or "",
                    "sr": sr,
                    "t": (p.get("type") or "")[:28],
                },
                "geometry": g2,
            }
        )
    write_gz(PACK / "roads.geojson.gz", {"type": "FeatureCollection", "features": road_out})
    FRONT.mkdir(parents=True, exist_ok=True)
    write_gz(FRONT / "roads.geojson.gz", {"type": "FeatureCollection", "features": road_out})

    riv_out = []
    for ft in rivers["features"]:
        p = ft.get("properties") or {}
        try:
            sr = int(float(p.get("scalerank", 99) or 99))
        except Exception:
            sr = 99
        if sr > 7:
            continue
        geom = ft.get("geometry")
        if not geom:
            continue
        step = 1 if sr <= 3 else 2
        if geom["type"] == "LineString":
            g2 = {"type": "LineString", "coordinates": simplify_ring(geom["coordinates"], step)}
        elif geom["type"] == "MultiLineString":
            g2 = {
                "type": "MultiLineString",
                "coordinates": [simplify_ring(l, step) for l in geom["coordinates"]],
            }
        else:
            continue
        g2["coordinates"] = round_coords(g2["coordinates"], 5)
        riv_out.append(
            {
                "type": "Feature",
                "properties": {"name": p.get("name") or p.get("name_en") or "", "sr": sr},
                "geometry": g2,
            }
        )
    write_gz(PACK / "rivers.geojson.gz", {"type": "FeatureCollection", "features": riv_out})
    write_gz(FRONT / "rivers.geojson.gz", {"type": "FeatureCollection", "features": riv_out})

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
        if not (sr <= 4 or pop >= 50000 or (in_we and pop >= 15000) or pop >= 150000):
            continue
        plc_out.append(
            {
                "type": "Feature",
                "properties": {"name": name, "pop": pop, "sr": sr},
                "geometry": {"type": "Point", "coordinates": [round(lon, 5), round(lat, 5)]},
            }
        )
    plc_out.sort(key=lambda f: (-(f["properties"]["pop"] or 0), f["properties"]["sr"]))
    write_gz(PACK / "cities.geojson.gz", {"type": "FeatureCollection", "features": plc_out[:6000]})
    write_gz(FRONT / "cities.geojson.gz", {"type": "FeatureCollection", "features": plc_out[:6000]})


def overpass_city(name: str, south: float, west: float, north: float, east: float) -> dict | None:
    # Prefer major+collector first; residential can explode — keep it but simplify hard
    q = f"""[out:json][timeout:120];
way["highway"~"^({HW})$"]({south},{west},{north},{east});
out geom;"""
    data = urllib.parse.urlencode({"data": q}).encode()
    req = urllib.request.Request(OVERPASS, data=data, method="POST")
    req.add_header("User-Agent", "GAFAM-map-pack/2.0")
    for attempt in range(3):
        try:
            with urllib.request.urlopen(req, timeout=180) as resp:
                payload = json.loads(resp.read().decode())
            break
        except Exception as e:
            print(f"  {name}: overpass retry {attempt+1}: {e}")
            time.sleep(8 * (attempt + 1))
    else:
        return None

    feats = []
    for el in payload.get("elements") or []:
        if el.get("type") != "way":
            continue
        geom = el.get("geometry") or []
        if len(geom) < 2:
            continue
        tags = el.get("tags") or {}
        hw = tags.get("highway", "")
        coords = [[round(p["lon"], 5), round(p["lat"], 5)] for p in geom]
        # simplify residential harder
        step = 3 if hw in ("residential", "unclassified") else 2 if hw in ("tertiary", "secondary") else 1
        coords = simplify_ring(coords, step)
        if len(coords) < 2:
            continue
        feats.append(
            {
                "type": "Feature",
                "properties": {"name": tags.get("name") or "", "hw": hw},
                "geometry": {"type": "LineString", "coordinates": coords},
            }
        )
    return {"type": "FeatureCollection", "features": feats}


def build_city_streets() -> list[dict]:
    print("== OSM city streets (Overpass) ==")
    STREETS.mkdir(parents=True, exist_ok=True)
    index = []
    for name, bbox in CITIES.items():
        print(f"fetch {name}…")
        fc = overpass_city(name, *bbox)
        if not fc or not fc["features"]:
            print(f"  {name}: EMPTY")
            continue
        # Cap huge cities
        if len(fc["features"]) > 25000:
            # keep non-residential first
            major = [f for f in fc["features"] if f["properties"]["hw"] not in ("residential", "unclassified")]
            resid = [f for f in fc["features"] if f["properties"]["hw"] in ("residential", "unclassified")]
            fc["features"] = major + resid[: max(0, 25000 - len(major))]
        path = STREETS / f"{name}.geojson.gz"
        write_gz(path, fc)
        # also expose on Wrangler for fallback
        write_gz(FRONT / "streets" / f"{name}.geojson.gz", fc)
        s, w, n, e = bbox
        index.append(
            {
                "id": name,
                "file": f"streets/{name}.geojson.gz",
                "bbox": [w, s, e, n],
                "features": len(fc["features"]),
            }
        )
        time.sleep(2)  # be nice to Overpass
    (STREETS / "index.json").write_text(json.dumps({"cities": index}, indent=2) + "\n")
    (FRONT / "streets" / "index.json").write_text(json.dumps({"cities": index}, indent=2) + "\n")
    return index


def write_manifest(cities: list[dict]) -> None:
    manifest = {
        "version": "map-pack-v2",
        "created": time.strftime("%Y-%m-%d"),
        "quota_bytes": 2_000_000_000,
        "layers": [
            {"id": "basemap", "file": "basemap.jpg", "type": "raster", "crs": "EPSG:4326"},
            {"id": "rivers", "file": "rivers.geojson.gz", "type": "geojson"},
            {"id": "roads", "file": "roads.geojson.gz", "type": "geojson", "note": "Natural Earth 10m densified (world arteries)"},
            {"id": "cities", "file": "cities.geojson.gz", "type": "geojson"},
            {"id": "streets", "type": "geojson-index", "file": "streets/index.json", "cities": cities},
        ],
        "note": "NE densified world + OSM important streets for FR metros. Soft geo quota 2GiB.",
    }
    (PACK / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print("pack MB", sum(p.stat().st_size for p in PACK.rglob("*") if p.is_file()) / 1e6)


def main() -> None:
    PACK.mkdir(parents=True, exist_ok=True)
    if not (BUILD / "roads.geojson").exists():
        raise SystemExit("missing /tmp/map_pack_build/*.geojson — re-download NE first")
    densify_ne()
    cities = build_city_streets()
    write_manifest(cities)


if __name__ == "__main__":
    main()
